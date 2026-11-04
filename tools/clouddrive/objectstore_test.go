package clouddrive

// 对象存储（阿里 OSS / 腾讯 COS / 通用 S3）的 httptest 假后端单测
// （计划-云盘对象存储对接 PR-A）。
//
// 只验证「签名头结构 + 请求形态 + 响应解析 + 凭证流转」这层契约，不代表真网可用：
// 假后端是个内存 KV（key → 内容），按 S3/OSS 的 REST 形状回 XML。
// 测试把 objectStoreScheme 指到 http、endpoint 填假服务的 IP:端口（IP 形式天然走
// path-style，请求 path 就是 /<bucket>/<key>，省掉 DNS）。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// bytesReader 小助手（上传入参统一走 io.Reader）。
func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

/* ============================ 假对象存储后端 ============================ */

type fakeOS struct {
	srv    *httptest.Server
	bucket string
	akID   string // 假后端认可的 AccessKey ID（签名校验用）
	secret string // 对应的 SK（仅测试内存里，不落盘）

	mu         sync.Mutex
	objs       map[string][]byte         // key → 内容
	written    map[string]time.Time      // key → 写入时间
	uploads    map[string]map[int][]byte // UploadId → 片号 → 内容（分块上传）
	uploadSeq  int                       // 生成 UploadId 用
	seq        []string                  // 依次记录的 "METHOD /path[?query]"（验调用序与分块参数）
	auths      []string                  // 依次记录的 Authorization 头
	lastHead   http.Header               // 最近一次 PUT 的请求头
	copySrc    string                    // 最近一次复制的源 header 值
	failNext   bool                      // 置 true 则下一个写请求回 200 + XML Error
	failPart   int                       // 非 0 则该片号的分片 PUT 回 500（验 Abort / 续传）
	noStat     bool                      // 置 true 则 `?stat` 回 404（验配额回落）
	noListPart bool                      // 置 true 则 `?uploadId` 列表回 404（验续传回落）
	truncParts bool                      // 置 true 则 ListParts 只回第一片 + IsTruncated（验续传拒接）
	brokenCopy string                    // 非空则「复制到这个 key」时回 200 + XML Error（验搬目录中途失败）
}

func newFakeOS(t *testing.T, bucket string) *fakeOS {
	t.Helper()
	f := &fakeOS{
		bucket:  bucket,
		akID:    fakeAkID,
		secret:  fakeSecret,
		objs:    map[string][]byte{},
		written: map[string]time.Time{},
		uploads: map[string]map[int][]byte{},
	}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	old := objectStoreScheme
	objectStoreScheme = "http"
	t.Cleanup(func() {
		objectStoreScheme = old
		f.srv.Close()
	})
	return f
}

// host 假服务的 IP:端口（当作 endpoint 填进账号）。
func (f *fakeOS) host() string { return strings.TrimPrefix(f.srv.URL, "http://") }

// record 记下请求形态；有查询串时一并带上（分块上传靠 partNumber/uploadId 区分）。
func (f *fakeOS) record(r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	entry := r.Method + " " + r.URL.Path
	if r.URL.RawQuery != "" {
		entry += "?" + r.URL.RawQuery
	}
	f.seq = append(f.seq, entry)
	if a := r.Header.Get("Authorization"); a != "" {
		f.auths = append(f.auths, a)
	}
}

func (f *fakeOS) put(k string, v []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objs[k] = v
	f.written[k] = time.Now()
}

func (f *fakeOS) get(k string) ([]byte, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.objs[k]
	return v, ok
}

func (f *fakeOS) mtime(k string) time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.written[k]
}

func (f *fakeOS) del(k string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objs, k)
	delete(f.written, k)
}

func (f *fakeOS) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.objs))
	for k := range f.objs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (f *fakeOS) seqList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.seq...)
}

func (f *fakeOS) lastAuth() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.auths) == 0 {
		return ""
	}
	return f.auths[len(f.auths)-1]
}

func (f *fakeOS) setHead(h http.Header) {
	f.mu.Lock()
	f.lastHead = h.Clone()
	f.mu.Unlock()
}

func (f *fakeOS) headOf(k string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastHead.Get(k)
}

func (f *fakeOS) setCopySrc(v string) {
	f.mu.Lock()
	f.copySrc = v
	f.mu.Unlock()
}

func (f *fakeOS) gotCopySrc() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.copySrc
}

// seed 预置对象（测试准备数据用）。
func (f *fakeOS) seed(k string, v []byte) { f.put(k, v) }

func (f *fakeOS) failNextWrite() {
	f.mu.Lock()
	f.failNext = true
	f.mu.Unlock()
}

// failPartWrite 让指定片号的分片 PUT 失败（验失败后必须 Abort）。
func (f *fakeOS) failPartWrite(num int) {
	f.mu.Lock()
	f.failPart = num
	f.mu.Unlock()
}

// disableStat 假装服务端没实现 GetBucketStat（MinIO 等 S3 兼容服务的常态）。
func (f *fakeOS) disableStat() {
	f.mu.Lock()
	f.noStat = true
	f.mu.Unlock()
}

// disableListParts 假装服务端不回答 ListParts（续传前核对失败应回落到重头传）。
func (f *fakeOS) disableListParts() {
	f.mu.Lock()
	f.noListPart = true
	f.mu.Unlock()
}

// truncateListParts 假装已传分片多到一页列不完（ListParts 回 IsTruncated）。
func (f *fakeOS) truncateListParts() {
	f.mu.Lock()
	f.truncParts = true
	f.mu.Unlock()
}

// failCopyToKey 假装复制到某个目标 key 时被服务端拒（回 200 + XML Error，对象存储的
// 常态）。用来验「搬目录必须先把全部对象 copy 完才开始删源」。
func (f *fakeOS) failCopyToKey(k string) {
	f.mu.Lock()
	f.brokenCopy = k
	f.mu.Unlock()
}

// allowCopy 撤掉复制故障（验搬目录可幂等重跑）。
func (f *fakeOS) allowCopy() {
	f.mu.Lock()
	f.brokenCopy = ""
	f.mu.Unlock()
}

func (f *fakeOS) handle(w http.ResponseWriter, r *http.Request) {
	// r.URL.Path 已由 Go 按百分号解码过一次，等价于服务端看到的 path：**不能再解第二遍**。
	// 之前这里又套了一次 QueryUnescape，于是 `+` 被当空格（path 里 `+` 不是空格）、`%25`
	// 被多解一层，带 `+`/空格的 key 存进去和取出来不是同一个对象——真机无此行为。
	key := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/"+f.bucket), "/")
	f.record(r)
	w.Header().Set("Content-Type", "application/xml")

	// 先把正文整段拿下来：签名重算要看正文摘要，下面各分支也复用这份字节。
	body, _ := io.ReadAll(r.Body)
	_ = r.Body.Close()

	f.mu.Lock()
	akID, sk := f.akID, f.secret
	fail := f.failNext
	f.failNext = false
	f.mu.Unlock()
	// **每一个**请求都要过签名重算（没有「关掉校验」的后门）：假后端不认「形状对但
	// 签名错」的请求，否则真机必炸的那一类又溜过去了。
	if code, msg := verifyAuth(r, body, akID, sk); code != "" {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>` + code + `</Code><Message>` + msg + `</Message></Error>`))
		return
	}
	if fail && r.Method != http.MethodGet && r.Method != http.MethodHead {
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>AccessDenied</Code><Message>signature dose not match</Message></Error>`))
		return
	}

	q := r.URL.Query()
	switch {
	case r.Method == http.MethodPost && key != "" && q.Has("uploads"):
		f.mu.Lock()
		f.uploadSeq++
		id := fmt.Sprintf("upload-%d", f.uploadSeq)
		f.uploads[id] = map[int][]byte{}
		f.mu.Unlock()
		_, _ = w.Write([]byte(`<?xml version="1.0"?><InitiateMultipartUploadResult><Bucket>` +
			f.bucket + `</Bucket><Key>` + key + `</Key><UploadId>` + id + `</UploadId></InitiateMultipartUploadResult>`))
		return
	case r.Method == http.MethodPut && key != "" && q.Has("uploadId") && q.Has("partNumber"):
		num, _ := strconv.Atoi(q.Get("partNumber"))
		f.mu.Lock()
		failPart := f.failPart == num
		if failPart {
			f.failPart = 0
		}
		f.mu.Unlock()
		if failPart {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InternalError</Code><Message>分片被拒</Message></Error>`))
			return
		}
		bs := body
		f.mu.Lock()
		up, ok := f.uploads[q.Get("uploadId")]
		if ok {
			up[num] = bs
		}
		f.mu.Unlock()
		if !ok {
			f.notFound(w)
			return
		}
		w.Header().Set("ETag", fmt.Sprintf(`"p%d-%d"`, num, len(bs)))
		return
	case r.Method == http.MethodPost && key != "" && q.Has("uploadId"):
		id := q.Get("uploadId")
		var req osCompleteReq
		if err := xml.Unmarshal(body, &req); err != nil {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>MalformedXML</Code><Message>complete 体不是合法清单</Message></Error>`))
			return
		}
		f.mu.Lock()
		up, ok := f.uploads[id]
		f.mu.Unlock()
		if !ok || len(req.Parts) != len(up) {
			_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InvalidPart</Code><Message>分片清单与已上传分片不匹配</Message></Error>`))
			return
		}
		nums := make([]int, 0, len(up))
		for n := range up {
			nums = append(nums, n)
		}
		sort.Ints(nums)
		var merged []byte
		for _, n := range nums {
			merged = append(merged, up[n]...)
		}
		f.put(key, merged)
		f.mu.Lock()
		delete(f.uploads, id)
		f.mu.Unlock()
		_, _ = w.Write([]byte(`<?xml version="1.0"?><CompleteMultipartUploadResult><Bucket>` +
			f.bucket + `</Bucket><Key>` + key + `</Key><ETag>"mp"</ETag></CompleteMultipartUploadResult>`))
		return
	case r.Method == http.MethodDelete && key != "" && q.Has("uploadId"):
		f.mu.Lock()
		delete(f.uploads, q.Get("uploadId"))
		f.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	case r.Method == http.MethodGet && key != "" && q.Has("uploadId"):
		// ListParts：续传前用来核对云端实际收到了哪几片。
		id := q.Get("uploadId")
		f.mu.Lock()
		noListPart, trunc, up := f.noListPart, f.truncParts, f.uploads[id]
		f.mu.Unlock()
		if noListPart || up == nil {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>NoSuchUpload</Code><Message>UploadId 不存在</Message></Error>`))
			return
		}
		nums := make([]int, 0, len(up))
		for n := range up {
			nums = append(nums, n)
		}
		sort.Ints(nums)
		shown := nums
		truncated := false
		if trunc && len(nums) > 1 { // 只回第一片，并声明后面还有（客户端不该猜）
			shown, truncated = nums[:1], true
		}
		var lb strings.Builder
		lb.WriteString(`<?xml version="1.0"?><ListPartsResult>`)
		for _, n := range shown {
			lb.WriteString(fmt.Sprintf(`<Part><PartNumber>%d</PartNumber><ETag>"p%d-%d"</ETag><Size>%d</Size></Part>`,
				n, n, len(up[n]), len(up[n])))
		}
		lb.WriteString(fmt.Sprintf(`<IsTruncated>%v</IsTruncated></ListPartsResult>`, truncated))
		_, _ = w.Write([]byte(lb.String()))
		return
	case r.Method == http.MethodGet && key == "" && q.Has("stat"):
		f.mu.Lock()
		noStat := f.noStat
		f.mu.Unlock()
		if noStat {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>NotImplemented</Code><Message>无配额接口</Message></Error>`))
			return
		}
		var used int64
		for k, v := range f.snapshot() {
			if !strings.HasSuffix(k, "/") {
				used += int64(len(v))
			}
		}
		_, _ = w.Write([]byte(fmt.Sprintf(`<?xml version="1.0"?><BucketStat><Storage>%d</Storage></BucketStat>`, used)))
		return
	case r.Method == http.MethodGet && key == "" && (q.Has("prefix") || q.Has("delimiter") || q.Has("max-keys")):
		esc := q.Get("encoding-type") != ""
		maxKeys, _ := strconv.Atoi(q.Get("max-keys"))
		f.list(w, q.Get("prefix"), q.Get("delimiter"), esc, q.Get("marker"), maxKeys)
		return
	case r.Method == http.MethodHead && key != "":
		v, ok := f.get(key)
		if !ok {
			f.notFound(w)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(v)))
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Last-Modified", f.mtime(key).UTC().Format(http.TimeFormat))
		w.Header().Set("ETag", `"abc123"`)
		return
	case r.Method == http.MethodGet && key != "":
		v, ok := f.get(key)
		if !ok {
			f.notFound(w)
			return
		}
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(v)))
		_, _ = w.Write(v)
		return
	case r.Method == http.MethodPut && key != "":
		f.setHead(r.Header)
		src := r.Header.Get("x-amz-copy-source")
		if src == "" {
			src = r.Header.Get("x-oss-copy-source")
		}
		if src != "" {
			f.setCopySrc(src)
			f.mu.Lock()
			broken := f.brokenCopy
			f.mu.Unlock()
			if broken == key {
				_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>InternalError</Code><Message>复制被拒</Message></Error>`))
				return
			}
			// copy-source 必须是 URL 编码态（与真机一致）：这里按真机的解码方式当
			// query 串解（`+` 解成空格），所以生产侧漏编码时带 `+` 的源会找不到对象。
			from := strings.TrimPrefix(strings.TrimPrefix(src, "/"), f.bucket+"/")
			if dec, e := url.QueryUnescape(from); e == nil {
				from = dec
			}
			v, ok := f.get(from)
			if !ok {
				f.notFound(w)
				return
			}
			f.put(key, v)
			_, _ = w.Write([]byte(`<?xml version="1.0"?><CopyObjectResult><ETag>"copied"</ETag></CopyObjectResult>`))
			return
		}
		f.put(key, body)
		w.Header().Set("ETag", `"up"`)
		_, _ = w.Write([]byte(`<?xml version="1.0"?><Ok/>`))
		return
	case r.Method == http.MethodDelete && key != "":
		f.del(key)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	f.notFound(w)
}

func (f *fakeOS) snapshot() map[string][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make(map[string][]byte, len(f.objs))
	for k, v := range f.objs {
		out[k] = v
	}
	return out
}

// list 按 S3/OSS 形状回 ListBucketResult：delimiter=/ 时按一层折叠成 CommonPrefixes，
// 并支持 marker + max-keys 翻页（回 IsTruncated / NextMarker 模拟真实服务端的游标）。
func (f *fakeOS) list(w http.ResponseWriter, prefix, delimiter string, esc bool, marker string, maxKeys int) {
	enc := func(s string) string {
		if esc {
			return url.QueryEscape(s)
		}
		return s
	}
	// 先把本页可见项按字典序摊平（目录项写成 `<dir>/`，与服务端 CommonPrefixes 同名）。
	var items []string
	seen := map[string]bool{}
	for _, k := range f.keys() {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := strings.TrimPrefix(k, prefix)
		if delimiter == "/" {
			if i := strings.Index(rest, "/"); i >= 0 {
				cp := prefix + rest[:i+1]
				if !seen[cp] {
					seen[cp] = true
					items = append(items, cp)
				}
				continue
			}
		}
		items = append(items, k)
	}
	sort.Strings(items)
	if marker != "" {
		var after []string
		for _, it := range items {
			if it > marker {
				after = append(after, it)
			}
		}
		items = after
	}
	truncated := maxKeys > 0 && len(items) > maxKeys
	if truncated {
		items = items[:maxKeys]
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0"?><ListBucketResult><Name>` + f.bucket + `</Name>`)
	for _, it := range items {
		// 只有请求带 delimiter 时，以 / 结尾的项才是服务端折叠出的 CommonPrefixes；
		// 不带 delimiter 时目录占位对象就是个**普通对象**，必须写进 Contents（否则按前缀
		// 全量列举的调用方永远看不到占位对象，搬目录/删目录会漏掉它）。
		if delimiter == "/" && strings.HasSuffix(it, "/") {
			b.WriteString(`<CommonPrefixes><Prefix>` + enc(it) + `</Prefix></CommonPrefixes>`)
			continue
		}
		mt := time.Time{}
		if _, ok := f.get(it); ok {
			mt = f.mtime(it)
		}
		b.WriteString(`<Contents><Key>` + enc(it) + `</Key><LastModified>` +
			mt.UTC().Format(time.RFC3339) + `</LastModified><ETag>"e"</ETag><Size>` +
			fmt.Sprintf("%d", len(f.mustGet(it))) + `</Size></Contents>`)
	}
	if truncated {
		b.WriteString(`<IsTruncated>true</IsTruncated><NextMarker>` +
			enc(items[len(items)-1]) + `</NextMarker>`)
	} else {
		b.WriteString(`<IsTruncated>false</IsTruncated>`)
	}
	b.WriteString(`</ListBucketResult>`)
	_, _ = w.Write([]byte(b.String()))
}

func (f *fakeOS) mustGet(k string) []byte {
	v, _ := f.get(k)
	return v
}

func (f *fakeOS) notFound(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte(`<?xml version="1.0"?><Error><Code>NoSuchKey</Code><Message>not found</Message></Error>`))
}

/* ============================ 构造测试账号 ============================ */

// newTestOS 建一个对象存储账号（kind 可为三家），bucket/endpoint 指向假后端。
func newTestOS(t *testing.T, kind, name string, f *fakeOS) (*objectStore, *fakeOS) {
	t.Helper()
	v, err := AddObjectStore(kind, name, f.bucket, f.host(), "cn-hangzhou", "AKID-TEST", "SECRET-TEST")
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	o, ok := p.(*objectStore)
	if !ok {
		t.Fatalf("New 应回 *objectStore，got %T", p)
	}
	return o, f
}

/* ============================ 签名头结构 ============================ */

var (
	sigv4AuthRe = regexp.MustCompile(`^AWS4-HMAC-SHA256 Credential=AKID-TEST/\d{8}/(us-east-1|cn-hangzhou)/s3/aws4_request, SignedHeaders=[a-z0-9;+-]*host[a-z0-9;+-]*, Signature=[0-9a-f]{64}$`)
	ossAuthRe   = regexp.MustCompile(`^OSS AKID-TEST:[A-Za-z0-9+/=]{28}$`)
)

func TestOSSSignerHeaders(t *testing.T) {
	f := newFakeOS(t, "oss-bucket")
	o, _ := newTestOS(t, KindAliyunOSS, "OSS", f)
	if _, err := o.List(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	auth := f.lastAuth()
	if !ossAuthRe.MatchString(auth) {
		t.Fatalf("OSS Authorization 形态不对: %q", auth)
	}
	// base64 解出来必须是 SHA-1 的 20 字节摘要。
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(auth, "OSS AKID-TEST:"))
	if err != nil || len(raw) != 20 {
		t.Fatalf("OSS 签名 base64/长度异常: err=%v len=%d", err, len(raw))
	}
}

func TestS3SignerHeaders(t *testing.T) {
	f := newFakeOS(t, "cos-bucket")
	o, _ := newTestOS(t, KindTencentCOS, "COS", f)
	if _, err := o.List(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	auth := f.lastAuth()
	if !sigv4AuthRe.MatchString(auth) {
		t.Fatalf("SigV4 Authorization 形态不对: %q", auth)
	}
	if !strings.Contains(auth, "cn-hangzhou") {
		t.Fatalf("scope region 应取账号填的 region: %q", auth)
	}
}

func TestS3SignerEmptyBodyHash(t *testing.T) {
	f := newFakeOS(t, "s3-bucket")
	o, _ := newTestOS(t, KindS3, "S3", f)
	if _, _, err := o.Open(context.Background(), "none.txt"); err == nil {
		t.Fatal("不存在的 key 应报错")
	}
	// region 未填时回落 us-east-1（这里账号填了 cn-hangzhou，改用手工账号验回落）。
	v, err := AddObjectStore(KindS3, "无 region", "b2", f.host(), "", "AKID-TEST", "SECRET-TEST")
	if err != nil {
		t.Fatal(err)
	}
	o2, err := New(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = o2.List(context.Background(), "")
	if !strings.Contains(f.lastAuth(), "/us-east-1/s3/aws4_request") {
		t.Fatalf("S3 未填 region 应回落 us-east-1: %q", f.lastAuth())
	}
}

func TestUploadSetsContentMD5AndCopyFailsLoudly(t *testing.T) {
	f := newFakeOS(t, "oss-bucket")
	o, _ := newTestOS(t, KindAliyunOSS, "OSS", f)
	if _, err := o.Upload(context.Background(), "", "a.txt", strings.NewReader("hello"), 5); err != nil {
		t.Fatal(err)
	}
	if f.headOf("Content-MD5") == "" {
		t.Fatal("OSS 上传应带 Content-MD5")
	}
	// 复制失败（200 + XML Error）必须透出，不能当成功。
	f.failNextWrite()
	if err := o.Rename(context.Background(), "a.txt", "b.txt"); err == nil {
		t.Fatal("复制被拒时 Rename 应报错")
	} else if !strings.Contains(err.Error(), "signature dose not match") {
		t.Fatalf("错误应透出云端 Message: %v", err)
	}
}

// TestUploadBodyHashes 锁死一个真机必踩的点：正文摘要必须由正文本体算出——
// SigV4 的 x-amz-content-sha256 是正文 SHA256（不是 MD5 再哈希一次），OSS 的
// Content-MD5 是正文 MD5 的 base64。假后端不会重算签名，所以错了在本层发现不了，
// 真实 COS/S3 会直接 SignatureDoesNotMatch。
func TestUploadBodyHashes(t *testing.T) {
	body := []byte("hello")

	fc := newFakeOS(t, "hash-cos")
	oc, _ := newTestOS(t, KindTencentCOS, "COS", fc)
	if _, err := oc.Upload(context.Background(), "", "a.txt", bytesReader(body), int64(len(body))); err != nil {
		t.Fatal(err)
	}
	if got := fc.headOf("X-Amz-Content-Sha256"); got != sha256Hex(body) {
		t.Fatalf("SigV4 正文哈希应为正文 SHA256: got %q want %q", got, sha256Hex(body))
	}

	fo := newFakeOS(t, "hash-oss")
	oo, _ := newTestOS(t, KindAliyunOSS, "OSS", fo)
	if _, err := oo.Upload(context.Background(), "", "a.txt", bytesReader(body), int64(len(body))); err != nil {
		t.Fatal(err)
	}
	want := base64.StdEncoding.EncodeToString(md5Bytes(body))
	if got := fo.headOf("Content-MD5"); got != want {
		t.Fatalf("OSS Content-MD5 应为正文 MD5 的 base64: got %q want %q", got, want)
	}
	if got := string(fo.mustGet("a.txt")); got != "hello" {
		t.Fatalf("假后端存下的正文应与上传一致: got %q", got)
	}
}

/* ============================ 浏览 / 读写 ============================ */

func TestListClassifiesDirsAndFiles(t *testing.T) {
	f := newFakeOS(t, "oss-bucket")
	o, _ := newTestOS(t, KindAliyunOSS, "OSS", f)
	f.seed("notes/", []byte{})
	f.seed("notes/readme.txt", []byte("r"))
	f.seed("notes/报告 v1+2.pdf", []byte("pdf"))
	f.seed("pic.png", []byte("png"))

	entries, err := o.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("根目录期望 1 目录 + 1 文件，got %d: %+v", len(entries), entries)
	}
	dir := entries[0]
	if !dir.IsDir || dir.Name != "notes" || dir.ID != "notes/" {
		t.Fatalf("目录归类不对: %+v", dir)
	}
	file := entries[1]
	if file.IsDir || file.Name != "pic.png" || file.Size != 3 {
		t.Fatalf("文件归类不对: %+v", file)
	}

	sub, err := o.List(context.Background(), "notes/")
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 2 {
		t.Fatalf("notes 下期望 2 条，got %d: %+v", len(sub), entries)
	}
	var plus string
	for _, e := range sub {
		if e.IsDir {
			t.Fatalf("子层不该有目录: %+v", e)
		}
		if strings.Contains(e.Name, "+") {
			plus = e.Name
		}
	}
	if plus != "报告 v1+2.pdf" {
		t.Fatalf("encoding-type=utf-8 的 key 未正确还原: %q", plus)
	}
}

func TestUploadOpenRoundTrip(t *testing.T) {
	f := newFakeOS(t, "cos-bucket")
	o, _ := newTestOS(t, KindTencentCOS, "COS", f)
	body := []byte(strings.Repeat("对象存储", 100))
	e, err := o.Upload(context.Background(), "", "中文 名.bin", bytesReader(body), int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "中文 名.bin" || e.Name != "中文 名.bin" {
		t.Fatalf("上传返回条目不对: %+v", e)
	}
	if e.Size != int64(len(body)) {
		t.Fatalf("上传后大小不对: %+v", e)
	}
	rc, size, err := o.Open(context.Background(), e.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if size != int64(len(body)) || string(got) != string(body) {
		t.Fatalf("下载字节与上传不一致: size=%d got=%d", size, len(got))
	}
}

func TestMkdirRenameMoveDelete(t *testing.T) {
	f := newFakeOS(t, "s3-bucket")
	o, _ := newTestOS(t, KindS3, "S3", f)

	d, err := o.Mkdir(context.Background(), "", "docs")
	if err != nil {
		t.Fatal(err)
	}
	if !d.IsDir || d.ID != "docs/" {
		t.Fatalf("Mkdir 应回虚拟目录条目: %+v", d)
	}
	if _, ok := f.get("docs/"); !ok {
		t.Fatal("Mkdir 应 PUT 零字节目录占位对象")
	}

	if _, err := o.Upload(context.Background(), "docs", "a.txt", bytesReader([]byte("A")), 1); err != nil {
		t.Fatal(err)
	}
	// 改名 = 服务端 copy + delete 原 key，且顺序为先 copy 后 delete。
	if err := o.Rename(context.Background(), "docs/a.txt", "b.txt"); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.get("docs/b.txt"); !ok {
		t.Fatalf("改名后新 key 不存在，实际 keys=%v", f.keys())
	}
	if _, ok := f.get("docs/a.txt"); ok {
		t.Fatal("改名后原 key 应已删除")
	}
	assertSeqHas(t, f, "PUT /s3-bucket/docs/b.txt", "DELETE /s3-bucket/docs/a.txt")
	if got := f.gotCopySrc(); got != "/s3-bucket/docs/a.txt" {
		t.Fatalf("复制源 header 形态不对: %q", got)
	}

	// 移动 = copy 到目标目录 + 删原 key。
	if _, err := o.Mkdir(context.Background(), "", "archive"); err != nil {
		t.Fatal(err)
	}
	if err := o.Move(context.Background(), "docs/b.txt", "archive"); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.get("archive/b.txt"); !ok {
		t.Fatalf("移动后目标 key 不存在，实际 keys=%v", f.keys())
	}
	if _, ok := f.get("docs/b.txt"); ok {
		t.Fatal("移动后原 key 应已删除")
	}

	if err := o.Delete(context.Background(), "archive/b.txt"); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.get("archive/b.txt"); ok {
		t.Fatal("Delete 后对象应消失")
	}
}

func assertSeqHas(t *testing.T, f *fakeOS, want ...string) {
	t.Helper()
	seq := f.seqList()
	idx := 0
	for _, s := range seq {
		if idx < len(want) && s == want[idx] {
			idx++
		}
	}
	if idx != len(want) {
		t.Fatalf("调用序未按 %v 出现，实际 %v", want, seq)
	}
}

/* ============================ 预签名 / 配额 / 探活 ============================ */

func TestShareLinkPresign(t *testing.T) {
	f := newFakeOS(t, "oss-bucket")
	o, _ := newTestOS(t, KindAliyunOSS, "OSS", f)
	u, err := o.ShareLink(context.Background(), "a b.txt")
	if err != nil {
		t.Fatal(err)
	}
	pu, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"OSSAccessKeyId", "Expires", "Signature"} {
		if pu.Query().Get(k) == "" {
			t.Fatalf("OSS 预签名 URL 缺 %s: %s", k, u)
		}
	}
	if !strings.Contains(u, "/a%20b.txt") {
		t.Fatalf("预签名 URL 路径编码不对: %s", u)
	}

	f2 := newFakeOS(t, "cos-bucket")
	o2, _ := newTestOS(t, KindTencentCOS, "COS", f2)
	u2, err := o2.ShareLink(context.Background(), "k/1.bin")
	if err != nil {
		t.Fatal(err)
	}
	p2, _ := url.Parse(u2)
	for _, k := range []string{"X-Amz-Algorithm", "X-Amz-Credential", "X-Amz-Date", "X-Amz-Expires", "X-Amz-SignedHeaders", "X-Amz-Signature"} {
		if p2.Query().Get(k) == "" {
			t.Fatalf("SigV4 预签名 URL 缺 %s: %s", k, u2)
		}
	}
	if p2.Query().Get("X-Amz-Expires") != "86400" {
		t.Fatalf("分享链接默认应 24 小时: %s", u2)
	}
}

func TestQuotaAndTest(t *testing.T) {
	f := newFakeOS(t, "oss-bucket")
	o, _ := newTestOS(t, KindAliyunOSS, "OSS", f)
	f.seed("x.txt", []byte("12345"))
	if err := o.Test(context.Background()); err != nil {
		t.Fatalf("Test 探活应通过: %v", err)
	}
	q, err := o.Quota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if q.Used != 5 {
		t.Fatalf("OSS 配额应读 GetBucketStat 的 Storage，got %d", q.Used)
	}

	f2 := newFakeOS(t, "s3-bucket")
	o2, _ := newTestOS(t, KindS3, "S3", f2)
	q2, err := o2.Quota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if q2.Used != 0 || q2.Total != 0 {
		t.Fatalf("S3 无统一配额接口，应回零值: %+v", q2)
	}
	if err := o2.Test(context.Background()); err != nil {
		t.Fatalf("S3 Test 探活应通过: %v", err)
	}
}

/* ============================ 账号表 / 注册表 ============================ */

func TestAddObjectStoreKeepsSecretEncrypted(t *testing.T) {
	f := newFakeOS(t, "oss-bucket")
	v, err := AddObjectStore(KindAliyunOSS, "我的桶", f.bucket, f.host(), "cn-hangzhou", "AKID-TEST", "SECRET-PLAINTEXT")
	if err != nil {
		t.Fatal(err)
	}
	if v.Bucket != f.bucket || v.Region != "cn-hangzhou" || !v.HasToken {
		t.Fatalf("AccountView 应带出非敏感配置: %+v", v)
	}
	if v.Kind != KindAliyunOSS {
		t.Fatalf("kind 不对: %+v", v)
	}
	p, err := New(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "阿里云 OSS" {
		t.Fatalf("Name() 不对: %s", p.Name())
	}
	// 密文往返：适配器解出来的 AK/SK 必须与录入一致。
	a, b, err := loadObjectStoreConfig(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if a.Bucket != f.bucket || b.AkID != "AKID-TEST" || b.AkSecret != "SECRET-PLAINTEXT" {
		t.Fatalf("AK/SK 密文往返不一致: %+v %+v", a, b)
	}
	// 落盘文件里不得出现明文 SK，且回前端的视图也不得含任何密钥字段。
	bs, err := os.ReadFile(storePathOf(t))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(bs), "SECRET-PLAINTEXT") || strings.Contains(string(bs), "AKID-TEST") {
		t.Fatal("账号文件里出现明文凭证")
	}
	out, err := ListAccounts()
	if err != nil {
		t.Fatal(err)
	}
	if len(out) == 0 {
		t.Fatal("ListAccounts 为空")
	}
}

func TestAddObjectStoreValidation(t *testing.T) {
	if _, err := AddObjectStore(KindBaidu, "x", "b", "", "", "ak", "sk"); err == nil {
		t.Fatal("非对象存储 kind 应被拒")
	}
	if _, err := AddObjectStore(KindS3, "x", "", "", "", "ak", "sk"); err == nil {
		t.Fatal("缺 bucket 应被拒")
	}
	if _, err := AddObjectStore(KindS3, "x", "b", "", "", "ak", ""); err == nil {
		t.Fatal("缺 AccessKey Secret 应被拒")
	}
}

func TestRegistryObjectStoreKinds(t *testing.T) {
	for _, k := range []string{KindAliyunOSS, KindTencentCOS, KindS3} {
		if !KindSupported(k) {
			t.Fatalf("%s 应已注册", k)
		}
	}
	f := newFakeOS(t, "reg-bucket")
	for _, tc := range []struct{ kind, name string }{
		{KindAliyunOSS, "阿里云 OSS"},
		{KindTencentCOS, "腾讯云 COS"},
		{KindS3, "S3 兼容存储"},
	} {
		v, err := AddObjectStore(tc.kind, tc.name, f.bucket, f.host(), "cn-hangzhou", "AKID-TEST", "SECRET-TEST")
		if err != nil {
			t.Fatal(err)
		}
		p, err := New(v.ID)
		if err != nil {
			t.Fatal(err)
		}
		o, ok := p.(*objectStore)
		if !ok {
			t.Fatalf("%s 应走 objectStore 基座", tc.kind)
		}
		if o.Name() != tc.name {
			t.Fatalf("展示名不对: %s vs %s", o.Name(), tc.name)
		}
		wantOSS := tc.kind == KindAliyunOSS
		if _, isOSS := o.signer.(*ossSigner); isOSS != wantOSS {
			t.Fatalf("%s 签名器选择不对（期望 ossSigner=%v）", tc.kind, wantOSS)
		}
	}
}

func TestOSHostDerivation(t *testing.T) {
	cases := []struct {
		kind, bucket, endpoint, region, want string
	}{
		{KindAliyunOSS, "myb", "", "cn-hangzhou", "myb.oss-cn-hangzhou.aliyuncs.com"},
		{KindTencentCOS, "myb", "", "ap-guangzhou", "myb.cos.ap-guangzhou.myqcloud.com"},
		{KindAliyunOSS, "myb", "https://oss-cn-beijing.aliyuncs.com", "oss-cn-beijing", "myb.oss-cn-beijing.aliyuncs.com"},
		{KindS3, "myb", "127.0.0.1:9000", "", "127.0.0.1:9000"},
		{KindS3, "myb", "myb.s3.example.com", "", "myb.s3.example.com"},
	}
	for _, c := range cases {
		got, err := osHost(c.kind, c.bucket, c.endpoint, c.region)
		if err != nil {
			t.Fatalf("%s/%s/%s: %v", c.kind, c.bucket, c.endpoint, err)
		}
		if got != c.want {
			t.Fatalf("%s endpoint=%q 期望 %q，got %q", c.kind, c.endpoint, c.want, got)
		}
	}
	if _, err := osHost(KindS3, "b", "", ""); err == nil {
		t.Fatal("S3 既无 endpoint 又无 region 时应报错")
	}
}

func TestRegionFromEndpoint(t *testing.T) {
	f := newFakeOS(t, "reg-bucket")
	// COS：region 留空时从带 region 的域名反解（这里用真域名形状，仅验解析）。
	if m := cosRegionFromHost.FindStringSubmatch("bucket-1250.cos.ap-shanghai.myqcloud.com"); m == nil || m[1] != "ap-shanghai" {
		t.Fatalf("COS region 反解失败: %v", m)
	}
	if m := ossRegionFromHost.FindStringSubmatch("myb.oss-cn-shenzhen.aliyuncs.com"); m == nil || m[1] != "cn-shenzhen" {
		t.Fatalf("OSS region 反解失败: %v", m)
	}
	_ = f
}

func TestListXMLEscapingSafe(t *testing.T) {
	// 后端回的是 XML，解析路径必须真走一遍 encoding/xml（防止手拼字符串误判）。
	bs := []byte(`<ListBucketResult><CommonPrefixes><Prefix>a/</Prefix></CommonPrefixes>` +
		`<Contents><Key>a/b.txt</Key><Size>2</Size><LastModified>2026-01-02T03:04:05Z</LastModified></Contents></ListBucketResult>`)
	var out osListResp
	if err := xml.Unmarshal(bs, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.CommonPrefixes) != 1 || out.CommonPrefixes[0].Prefix != "a/" {
		t.Fatalf("CommonPrefixes 解析不对: %+v", out.CommonPrefixes)
	}
	if out.Contents[0].Size != 2 {
		t.Fatalf("Contents 解析不对: %+v", out.Contents)
	}
}

/* ============================ PR-B：翻页 / 分块 / 配额 ============================ */

// shrinkParts 临时调小分块阈值与片大小，让测试不必造 8MiB 数据。
func shrinkParts(t *testing.T, partSize, threshold int64) {
	t.Helper()
	oldP, oldT := osPartSize, osMultipartThreshold
	osPartSize, osMultipartThreshold = partSize, threshold
	t.Cleanup(func() { osPartSize, osMultipartThreshold = oldP, oldT })
}

func TestMultipartUploadFlow(t *testing.T) {
	shrinkParts(t, 1<<10, 1<<10)
	f := newFakeOS(t, "mp-bucket")
	o, _ := newTestOS(t, KindS3, "S3", f)
	body := bytes.Repeat([]byte("x"), 2500) // 1024 + 1024 + 452 → 三片
	if _, err := o.Upload(context.Background(), "docs", "big.bin", bytesReader(body), int64(len(body))); err != nil {
		t.Fatal(err)
	}
	assertSeqHas(t, f,
		"POST /mp-bucket/docs/big.bin?uploads",
		"PUT /mp-bucket/docs/big.bin?partNumber=1&uploadId=upload-1",
		"PUT /mp-bucket/docs/big.bin?partNumber=2&uploadId=upload-1",
		"PUT /mp-bucket/docs/big.bin?partNumber=3&uploadId=upload-1",
		"POST /mp-bucket/docs/big.bin?uploadId=upload-1",
	)
	if got := f.mustGet("docs/big.bin"); !bytes.Equal(got, body) {
		t.Fatalf("分块合并后字节不一致: got %d want %d", len(got), len(body))
	}
	for _, s := range f.seqList() {
		if strings.HasPrefix(s, "DELETE ") {
			t.Fatalf("成功路径不应 Abort: %v", f.seqList())
		}
	}
	f.mu.Lock()
	left := len(f.uploads)
	f.mu.Unlock()
	if left != 0 {
		t.Fatalf("Complete 后应清掉分块会话，残留 %d 个", left)
	}
}

func TestMultipartAbortsOnPartFailure(t *testing.T) {
	shrinkParts(t, 1<<10, 1<<10)
	f := newFakeOS(t, "mp-bucket")
	o, _ := newTestOS(t, KindAliyunOSS, "OSS", f)
	f.failPartWrite(2) // 第二片被拒
	body := bytes.Repeat([]byte("y"), 2500)
	if _, err := o.Upload(context.Background(), "", "big.bin", bytesReader(body), int64(len(body))); err == nil {
		t.Fatal("分片被拒时 Upload 应报错")
	}
	assertSeqHas(t, f,
		"POST /mp-bucket/big.bin?uploads",
		"PUT /mp-bucket/big.bin?partNumber=2&uploadId=upload-1",
		"DELETE /mp-bucket/big.bin?uploadId=upload-1",
	)
	f.mu.Lock()
	left := len(f.uploads)
	f.mu.Unlock()
	if left != 0 {
		t.Fatalf("Abort 后不应残留分片，实际 %d 个", left)
	}
}

func TestSmallUploadStaysSinglePut(t *testing.T) {
	f := newFakeOS(t, "mp-bucket")
	o, _ := newTestOS(t, KindS3, "S3", f)
	if _, err := o.Upload(context.Background(), "", "s.txt", strings.NewReader("hi"), 2); err != nil {
		t.Fatal(err)
	}
	assertSeqHas(t, f, "PUT /mp-bucket/s.txt")
	for _, s := range f.seqList() {
		if strings.Contains(s, "?uploads") {
			t.Fatalf("小文件不该走分块: %v", f.seqList())
		}
	}
}

func TestListPagination(t *testing.T) {
	f := newFakeOS(t, "page-bucket")
	const total = 2500
	for i := 1; i <= total; i++ {
		f.seed(fmt.Sprintf("docs/f%04d.txt", i), []byte("x"))
	}
	f.seed("docs/sub/a.txt", []byte("y")) // 混一个目录（折叠成 CommonPrefixes）
	o, _ := newTestOS(t, KindTencentCOS, "COS", f)
	entries, err := o.List(context.Background(), "docs")
	if err != nil {
		t.Fatal(err)
	}
	var dirs, files int
	seen := map[string]bool{}
	for _, e := range entries {
		if seen[e.ID] {
			t.Fatalf("翻页出现重复条目: %s", e.ID)
		}
		seen[e.ID] = true
		if e.IsDir {
			dirs++
		} else {
			files++
		}
	}
	if files != total {
		t.Fatalf("翻页后应取回 %d 个文件，实得 %d", total, files)
	}
	if dirs != 1 {
		t.Fatalf("目录项应识别为 1 个，实得 %d", dirs)
	}
	seq := f.seqList()
	var pages int
	for _, s := range seq {
		if strings.HasPrefix(s, "GET /page-bucket/?") {
			pages++
		}
	}
	if pages != 3 { // 1000 + 1000 + 501
		t.Fatalf("期望 3 次列表请求，实得 %d：%v", pages, seq)
	}
	// marker 里的 `/` 必须只转义一次（否则 SigV4 与服务端重算的 canonical 对不上）。
	if !strings.Contains(seq[len(seq)-1], "marker=docs%2Ff2000.txt") {
		t.Fatalf("末页游标不对: %q", seq[len(seq)-1])
	}
}

func TestQuotaForCosAndS3Fallback(t *testing.T) {
	f := newFakeOS(t, "quota-bucket")
	f.seed("a.txt", []byte("12345"))
	oc, _ := newTestOS(t, KindTencentCOS, "COS", f)
	q, err := oc.Quota(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if q.Used != 5 {
		t.Fatalf("COS 的 ?stat 应取回已用容量，got %+v", q)
	}
	fs := newFakeOS(t, "nostat-bucket")
	fs.seed("a.txt", []byte("12345"))
	os3, _ := newTestOS(t, KindS3, "S3", fs)
	fs.disableStat()
	q2, err := os3.Quota(context.Background())
	if err != nil {
		t.Fatalf("服务端没有配额接口时不该报错: %v", err)
	}
	if q2.Used != 0 || q2.Total != 0 {
		t.Fatalf("配额取不到应回落零值，got %+v", q2)
	}
}

// TestCanonicalQuerySingleEncode 锁死查询参数的转义次数：URL 里的 %2F 解码回原始值
// 后只能再转义一次，否则服务端重算 canonical 时永远对不上（翻页 marker 必带 `/`）。
func TestCanonicalQuerySingleEncode(t *testing.T) {
	got := newSortedKV(signQuery("marker=docs%2Fa%20b&partNumber=1&uploadId=abc%3D%3D")).encode()
	want := "marker=docs%2Fa%20b&partNumber=1&uploadId=abc%3D%3D"
	if got != want {
		t.Fatalf("SigV4 canonical query 应只转义一次: got %q want %q", got, want)
	}
	if res := ossCanonicalResource("/b/k", signQuery("partNumber=1&uploadId=abc%3D%3D")); res != "/b/k?partNumber=1&uploadId=abc==" {
		t.Fatalf("OSS canonical resource 应拼原始值: %q", res)
	}
	if bare := ossCanonicalResource("/b/k", signQuery("uploads")); bare != "/b/k?uploads" {
		t.Fatalf("空值子资源不该写成 uploads=: %q", bare)
	}
}

func storePathOf(t *testing.T) string {
	t.Helper()
	p, err := storePath()
	if err != nil {
		t.Fatal(err)
	}
	return p
}
