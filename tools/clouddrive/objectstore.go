package clouddrive

// 对象存储通用 Provider 基座（计划-云盘对象存储对接 PR-A）。
//
// 阿里 OSS / 腾讯 COS / 通用 S3 三家在协议层都是「REST + XML + 一套签名」，差异只在
// 签名算法与 endpoint/region 默认值，所以这里只写一份 S3 风格的
// list/get/put/copy/delete/head 实现，签名交给可插拔的 signer（见 signer.go）：
//   - aliyun_oss  → ossSigner（OSS 原生 V1，HMAC-SHA1）
//   - tencent_cos → s3Signer（AWS SigV4，service=s3）
//   - s3          → s3Signer（同上，region 由用户填）
//
// 语义映射：Entry.ID 直接就是完整 object key（即路径），目录是「以 / 结尾的 key」——
// 虚拟目录用零字节 `<dir>/` 占位对象表达；Rename/Move 因对象存储无原生改名，一律
// 「服务端 copy + delete 原 key」，目录则按前缀逐个对象搬（见 copyPrefixTree，非原子）。
//
// PR-B 补上的三件事：List 按 marker 翻页；大文件走原生 Multipart Upload（内存里
// 一次只驻留一片，不再整段缓冲）；Quota 对 COS 也试 `?stat`，三家拿不到就回零值。
//
// PR-C 再补三件事：目录整体改名/移动按前缀逐个对象搬（copyPrefixTree，删目录也
// 递归）；分块上传会话落盘 uploads.json，进程重启后按同一 UploadId 续传（先 ListParts
// 与云端核对）；copy-source 改写为 URL 编码态（带空格/中文的源在真机不会被打回
// InvalidArgument，且 `+` 不编码会被服务端当空格）。
//
// 跨平台零 cgo、无 build tag：只有 net/http、encoding/xml、crypto/*；单测全部走
// httptest 假后端，只验证「请求形态 + 响应解析 + 签名结构」这层契约。

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// objectStore 三家对象存储共用的 Provider 实现。
type objectStore struct {
	acctID string
	hc     *http.Client
	bucket string
	host   string // 访问域名（虚拟主机式为 <bucket>.<endpoint host>；IP endpoint 不含 bucket）
	region string
	kind   string
	name   string
	signer signer
}

// osHost 按 kind 从 endpoint/region 推导访问域名（不含 scheme）。endpoint 允许填
// `oss-cn-hangzhou.aliyuncs.com`、带 scheme 的完整 URL、或已含 bucket 前缀的
// `<bucket>.oss-cn-hangzhou.aliyuncs.com`（后者不再重复拼）；IP 形式（自建 MinIO 等）
// 不能加 bucket 子域，保持原样，寻址退化为 path-style。
func osHost(kind, bucket, endpoint, region string) (string, error) {
	ep := strings.TrimSpace(endpoint)
	if ep != "" {
		h, err := normalizeHost(ep)
		if err != nil || h == "" {
			return "", fmt.Errorf("endpoint 格式不正确: %s", endpoint)
		}
		ep = h
	}
	if ep == "" {
		ep = ossHostPattern(kind, region)
	}
	if ep == "" {
		return "", fmt.Errorf("未填 endpoint，也无法从 region 推导默认域名（region=%s）", region)
	}
	if virtualHostDisabled(ep) || strings.HasPrefix(ep, bucket+".") {
		return ep, nil
	}
	return bucket + "." + ep, nil
}

// normalizeHost 去掉 scheme 与路径，只留主机名。
func normalizeHost(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", nil
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", err
	}
	if u.Host == "" {
		return "", fmt.Errorf("缺少主机名")
	}
	return u.Host, nil
}

// objectStoreScheme 访问协议前缀（测试里改成 http 就能把流量引到 httptest 假后端）。
var objectStoreScheme = "https"

// osBase host → 完整访问前缀。
func osBase(host string) string { return objectStoreScheme + "://" + host }

// virtualHostDisabled IP 形式的访问域名不能加 bucket 子域，只能走 path-style。
func virtualHostDisabled(host string) bool {
	return ipLikeHost.MatchString(strings.Split(host, ":")[0])
}

// ipLikeHost 判断是不是点分数字 IP。
var ipLikeHost = regexp.MustCompile(`^\d{1,3}(\.\d{1,3}){3}$`)

// ossHostPattern 各家按 region 推导访问域名。
func ossHostPattern(kind, region string) string {
	switch kind {
	case KindAliyunOSS:
		return fmt.Sprintf("oss-%s.aliyuncs.com", region)
	case KindTencentCOS:
		return fmt.Sprintf("cos.%s.myqcloud.com", region)
	}
	return ""
}

// newObjectStoreAcct 用已读出的账号配置构造基座（各家注册文件可在传入前补齐 region）。
func newObjectStoreAcct(kind, name, acctID string, a Account, b tokenBlob) (Provider, error) {
	host, err := osHost(kind, a.Bucket, a.Endpoint, a.Region)
	if err != nil {
		return nil, err
	}
	o := &objectStore{
		acctID: acctID,
		hc:     &http.Client{Timeout: 5 * time.Minute},
		bucket: a.Bucket,
		host:   host,
		region: a.Region,
		kind:   kind,
		name:   name,
	}
	ak, sk := b.AkID, b.AkSecret
	if kind == KindAliyunOSS {
		o.signer = newOSSSigner(host, ak, sk)
		return o, nil
	}
	if o.region == "" {
		o.region = "us-east-1" // SigV4 必须有 scope region，S3 兼容服务普遍接受该值
	}
	o.signer = newS3Signer(host, o.region, ak, sk)
	return o, nil
}

func (o *objectStore) Name() string { return o.name }

/* ============================ 请求构造 ============================ */

// resource 签名用的资源路径：始终是 /<bucket>/<key>（两种寻址形式同规则）。
func (o *objectStore) resource(key string) string {
	return "/" + o.bucket + "/" + strings.TrimPrefix(key, "/")
}

// urlFor 拼访问 URL（含查询串）。key 里保留 `/` 作层级，其余按百分号编码。
// 虚拟主机式下 host 已含 bucket 子域（真实服务会把它归一到 path），path-style
// （IP endpoint）则由 host 原样 + path 携带 bucket。
func (o *objectStore) urlFor(key, query string) string {
	u := osBase(o.host) + uriPathFromResource(o.resource(key))
	if query != "" {
		u += "?" + query
	}
	return u
}

// do 发一次已签名的请求：payload 是正文（nil 表示无正文），extra 是额外请求头。
// 非 2xx 会读出 body 里的 XML Error/文本拼进错误并关闭响应。
func (o *objectStore) do(ctx context.Context, method, key, query string, payload []byte, extra map[string]string) (*http.Response, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, o.urlFor(key, query), body)
	if err != nil {
		return nil, err
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}
	if err := o.signer.sign(req, o.resource(key), payload); err != nil {
		return nil, err
	}
	resp, err := o.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s 请求失败: %w", o.name, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bs, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		_ = resp.Body.Close()
		return nil, fmt.Errorf("%s 返回 %d：%s", o.name, resp.StatusCode, osErrMsg(bs))
	}
	return resp, nil
}

// osErrMsg 从 XML 错误体里取 Code/Message（非 XML 时截断原样返回）。
func osErrMsg(bs []byte) string {
	var e struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	if xml.Unmarshal(bs, &e) == nil && (e.Code != "" || e.Message != "") {
		return firstNonEmptyStr(e.Message, e.Code, "无详情")
	}
	s := strings.TrimSpace(string(bs))
	if s == "" {
		return "无详情"
	}
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

/* ============================ XML 响应 ============================ */

// osListResp GetBucket（ListObjects V1）响应。
type osListResp struct {
	CommonPrefixes []struct {
		Prefix string `xml:"Prefix"`
	} `xml:"CommonPrefixes"`
	Contents []struct {
		Key          string `xml:"Key"`
		LastModified string `xml:"LastModified"`
		ETag         string `xml:"ETag"`
		Size         int64  `xml:"Size"`
	} `xml:"Contents"`
	IsTruncated bool   `xml:"IsTruncated"`
	NextMarker  string `xml:"NextMarker"`
}

// osStatResp GetBucketStat 响应（Storage 为字节）。
type osStatResp struct {
	Storage int64 `xml:"Storage"`
}

// osInitResp InitiateMultipartUpload 响应。
type osInitResp struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
	UploadID string   `xml:"UploadId"`
}

// osUploadPart 一个已上传分片的服务端凭据（ETag 带引号原样回传）。
type osUploadPart struct {
	PartNumber int    `xml:"PartNumber"`
	ETag       string `xml:"ETag"`
}

// osCompleteReq CompleteMultipartUpload 请求体。
type osCompleteReq struct {
	XMLName xml.Name       `xml:"CompleteMultipartUpload"`
	Parts   []osUploadPart `xml:"Part"`
}

// osListPartsResp ListParts 响应（只取续传需要的三项）。
type osListPartsResp struct {
	XMLName     xml.Name `xml:"ListPartsResult"`
	IsTruncated bool     `xml:"IsTruncated"`
	Parts       []struct {
		PartNumber int    `xml:"PartNumber"`
		ETag       string `xml:"ETag"`
		Size       int64  `xml:"Size"`
	} `xml:"Part"`
}

// keyBase 取 key 的文件名部分。
func keyBase(key string) string {
	key = strings.TrimSuffix(key, "/")
	if i := strings.LastIndex(key, "/"); i >= 0 {
		return key[i+1:]
	}
	return key
}

// keyDir 取 key 的目录前缀（含尾斜杠；根返回空串）。
func keyDir(key string) string {
	i := strings.LastIndex(strings.TrimSuffix(key, "/"), "/")
	if i < 0 {
		return ""
	}
	return key[:i+1]
}

// dirKey 拼 `<parent>/<name>`；parentID 为空即根。
func dirKey(parentID, name string) string {
	p := strings.Trim(parentID, "/")
	if p == "" {
		return name
	}
	return p + "/" + name
}

// isDirKey 目录（占位对象）的 key 以 / 结尾。
func isDirKey(key string) bool { return strings.HasSuffix(key, "/") }

// unescKey 还原响应里被 URL 编码的 key（encoding-type=utf-8 时服务端按此回传）。
func unescKey(s string) string {
	if v, err := url.QueryUnescape(s); err == nil {
		return v
	}
	return s
}

/* ============================ Provider 方法 ============================ */

// Test 列一根（max-keys=1）探活：能通就说明 bucket + AK/SK + 签名都成立。
func (o *objectStore) Test(ctx context.Context) error {
	resp, err := o.do(ctx, http.MethodGet, "", "max-keys=1", nil, nil)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	return resp.Body.Close()
}

// osListMaxPages 单次 List 最多翻多少页（每页 1000 条 → 上限 5 万条），服务端
// 游标异常时不至于死循环。
const osListMaxPages = 50

// osListMaxPageKeys 一页取多少条（三家都上限 1000）。
const osListMaxPageKeys = 1000

// listBaseQuery 列表请求的公共参数骨架（prefix + 编码声明 + 页大小）。
func listBaseQuery(prefix string) url.Values {
	q := url.Values{}
	q.Set("prefix", prefix)
	q.Set("encoding-type", "utf-8")
	q.Set("max-keys", strconv.Itoa(osListMaxPageKeys))
	return q
}

// listPage 发一次列表请求并解析 XML。
func (o *objectStore) listPage(ctx context.Context, q url.Values) (*osListResp, error) {
	resp, err := o.do(ctx, http.MethodGet, "", q.Encode(), nil, nil)
	if err != nil {
		return nil, err
	}
	bs, rerr := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	_ = resp.Body.Close()
	if rerr != nil {
		return nil, rerr
	}
	var out osListResp
	if err := xml.Unmarshal(bs, &out); err != nil {
		return nil, fmt.Errorf("解析 %s 列表失败: %w", o.name, err)
	}
	return &out, nil
}

// nextListMarker 算下一页游标：encoding-type=utf-8 时 NextMarker 也是编码态，还原后才
// 能当游标；服务端没回 NextMarker 时退回本页最后一个 key。回空串表示游标拿不到或没有
// 前进，调用方应停在已取到的部分，不冒死循环风险。
func nextListMarker(out *osListResp, marker string) string {
	next := unescKey(out.NextMarker)
	if next == "" {
		next = lastListKey(out)
	}
	if next == marker {
		return ""
	}
	return next
}

// List dirID 是目录前缀（空 = 根）；以 delimiter=/ 逐层列，CommonPrefixes 归目录、
// Contents 归文件（目录自身的零字节占位对象不重复展示）。按 marker 翻页取全。
func (o *objectStore) List(ctx context.Context, dirID string) ([]Entry, error) {
	prefix := strings.Trim(dirID, "/")
	if prefix != "" {
		prefix += "/"
	}
	var entries []Entry
	marker := ""
	for page := 0; page < osListMaxPages; page++ {
		q := listBaseQuery(prefix)
		q.Set("delimiter", "/")
		if marker != "" {
			q.Set("marker", marker)
		}
		out, err := o.listPage(ctx, q)
		if err != nil {
			return nil, err
		}
		entries = append(entries, classifyListPage(out, prefix)...)
		if !out.IsTruncated {
			break
		}
		marker = nextListMarker(out, marker)
		if marker == "" {
			break
		}
	}
	return entries, nil
}

// listAllKeys 列前缀下的**全部** key（不带 delimiter，因此不折叠层级，含目录自身的
// 零字节占位对象）。目录整体改名/移动要用它拿到每一个待搬的对象。
func (o *objectStore) listAllKeys(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	marker := ""
	for page := 0; page < osListMaxPages; page++ {
		q := listBaseQuery(prefix)
		if marker != "" {
			q.Set("marker", marker)
		}
		out, err := o.listPage(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, c := range out.Contents {
			if k := unescKey(c.Key); k != "" {
				keys = append(keys, k)
			}
		}
		if !out.IsTruncated {
			break
		}
		marker = nextListMarker(out, marker)
		if marker == "" {
			break
		}
	}
	return keys, nil
}

// classifyListPage 把一页响应拆成条目：CommonPrefixes 归目录、Contents 归文件。
func classifyListPage(out *osListResp, prefix string) []Entry {
	entries := make([]Entry, 0, len(out.CommonPrefixes)+len(out.Contents))
	for _, p := range out.CommonPrefixes {
		k := unescKey(p.Prefix)
		name := strings.TrimSuffix(strings.TrimPrefix(k, prefix), "/")
		if name == "" {
			continue
		}
		entries = append(entries, Entry{
			ID: k, Name: name, Path: prefix + name,
			IsDir: true, MIME: "application/x-directory",
		})
	}
	for _, c := range out.Contents {
		k := unescKey(c.Key)
		// 目录占位对象（key 以 / 结尾，含当前目录自身）不当文件展示。
		if k == "" || k == prefix || strings.HasSuffix(k, "/") {
			continue
		}
		name := strings.TrimPrefix(k, prefix)
		if name == "" {
			continue
		}
		var mt int64
		if t, err := time.Parse(time.RFC3339, c.LastModified); err == nil {
			mt = t.UnixMilli()
		}
		entries = append(entries, Entry{
			ID: k, Name: name, Path: prefix + name,
			Size: c.Size, MTime: mt, MIME: "application/octet-stream",
		})
	}
	return entries
}

// lastListKey 取本页字典序最后一个 key（含目录占位对象），作 NextMarker 缺失时的
// 翻页游标回落——占位对象虽不展示，但仍是合法的服务端游标。
func lastListKey(out *osListResp) string {
	last := ""
	for _, p := range out.CommonPrefixes {
		if k := unescKey(p.Prefix); k > last {
			last = k
		}
	}
	for _, c := range out.Contents {
		if k := unescKey(c.Key); k > last {
			last = k
		}
	}
	return last
}

// Mkdir PUT 一个 `<parent>/<name>/` 零字节对象作虚拟目录。
func (o *objectStore) Mkdir(ctx context.Context, parentID, name string) (Entry, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") {
		return Entry{}, fmt.Errorf("目录名不合法")
	}
	key := dirKey(parentID, name) + "/"
	if _, err := o.do(ctx, http.MethodPut, key, "", []byte{}, nil); err != nil {
		return Entry{}, err
	}
	return Entry{
		ID: key, Name: name, Path: strings.TrimSuffix(key, "/"),
		IsDir: true, MIME: "application/x-directory", MTime: time.Now().UnixMilli(),
	}, nil
}

// osPartSize 分块上传的片大小（8 MiB）。测试里临时调小即可低成本触发多片流程。
var osPartSize int64 = 8 << 20

// osMultipartThreshold 超过该大小就改走分块上传，避免把整个文件缓冲进内存。
var osMultipartThreshold int64 = 8 << 20

// osMaxParts 单对象的分片数上限（OSS/COS/S3 一致）。
const osMaxParts = 10000

// Upload 不超过阈值的走整段单 PUT；超过阈值（或大小未知）走原生分块上传，
// 内存里一次只驻留一片（PR-B）。
func (o *objectStore) Upload(ctx context.Context, parentID, name string, r io.Reader, size int64) (Entry, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, "/") {
		return Entry{}, fmt.Errorf("文件名不合法")
	}
	key := dirKey(parentID, name)
	// 已知大小且不超过阈值（含 size=0 的空文件）→ 整段单 PUT；大小未知（<0）或偏大
	// → 分块，不把未知长度的流整段读进内存。
	if size >= 0 && size <= osMultipartThreshold {
		data, err := io.ReadAll(r)
		if err != nil {
			return Entry{}, fmt.Errorf("读取上传内容失败: %w", err)
		}
		extra := map[string]string{"Content-Type": "application/octet-stream"}
		resp, err := o.do(ctx, http.MethodPut, key, "", data, extra)
		if err != nil {
			return Entry{}, err
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
		_ = resp.Body.Close()
		return o.statEntry(ctx, key, int64(len(data))), nil
	}
	return o.uploadMultipart(ctx, key, r, size)
}

// uploadMultipart 走对象存储原生 Multipart Upload：Initiate → 逐片 PUT → Complete。
//
// 源是本地文件时（CloudUpload 那条路径）会建一条可续传会话写进 uploads.json：进程被杀
// 后再传同一文件到同一 key，先 ListParts 与云端核对，只补传缺的分片（PR-C ②）。
// 因此分片写失败时**保留会话**不 Abort（云端已传的分片下次还要用）；源不是本地文件的
// （内存流、跨盘转存）无从校验续传内容，一律立刻 Abort——未 Abort 的分片会持续占
// bucket 容量并计费。
func (o *objectStore) uploadMultipart(ctx context.Context, key string, r io.Reader, size int64) (Entry, error) {
	partSize := osPartSize
	if size > partSize*osMaxParts {
		partSize = size/osMaxParts + partSize // 超大对象摊薄，保证片数不越界
	}
	src, resumable := localSource(r)
	sess, uploadID, skip, err := o.beginMultipart(ctx, key, src, partSize, resumable)
	if err != nil {
		return Entry{}, err
	}
	// fail 统一收尾：keep=true 留下会话等下次续传，否则 Abort 并删记。
	fail := func(cause error, keep bool) (Entry, error) {
		if sess != nil && keep {
			return Entry{}, cause
		}
		_ = o.abortMultipart(ctx, key, uploadID)
		if sess != nil {
			_ = removeUploadSession(o.acctID, uploadID)
		}
		return Entry{}, cause
	}
	var parts []osUploadPart
	if sess != nil {
		parts = sess.partList()
	}
	if skip > 0 {
		if err := discardBytes(r, skip); err != nil {
			return fail(fmt.Errorf("跳过已上传分片失败: %w", err), false)
		}
	}
	num := len(parts) + 1
	for {
		buf := make([]byte, partSize)
		n, rerr := io.ReadFull(r, buf)
		if n > 0 {
			etag, perr := o.uploadPart(ctx, key, num, uploadID, buf[:n])
			if perr != nil {
				// 源是本地文件：留着会话等下次续传（第 num-1 片之前都已在云端）。
				return fail(perr, sess != nil)
			}
			parts = append(parts, osUploadPart{PartNumber: num, ETag: etag})
			if sess != nil {
				sess.setPart(num, etag, int64(n))
				// 落盘失败不影响本次上传（只是下次没法续传），不把它当错。
				_ = putUploadSession(*sess)
			}
			num++
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			return fail(fmt.Errorf("读取上传内容失败: %w", rerr), sess != nil)
		}
		if len(parts) >= osMaxParts {
			return fail(fmt.Errorf("%s 文件过大（分片数超过 %d）", o.name, osMaxParts), false)
		}
	}
	if len(parts) == 0 { // 空文件：Complete 空片列表会被服务端拒，退回单 PUT 零字节
		_ = o.abortMultipart(ctx, key, uploadID)
		if sess != nil {
			_ = removeUploadSession(o.acctID, uploadID)
		}
		if _, err := o.do(ctx, http.MethodPut, key, "", []byte{}, nil); err != nil {
			return Entry{}, err
		}
		return o.statEntry(ctx, key, 0), nil
	}
	if err := o.completeMultipart(ctx, key, uploadID, parts); err != nil {
		// 清单被拒最常见的原因是云端分片已被清理：丢掉会话，下次从第 1 片重传。
		return fail(err, false)
	}
	if sess != nil {
		_ = removeUploadSession(o.acctID, uploadID)
		return o.statEntry(ctx, key, sess.SrcSize), nil
	}
	return o.statEntry(ctx, key, 0), nil
}

// beginMultipart 拿一个可用的 UploadId：优先复用盘上的会话（先与云端 ListParts 核对），
// 否则发一个新的。回 skipBytes = 云端已有的连续前缀分片占多少源字节（续传要跳过）。
func (o *objectStore) beginMultipart(ctx context.Context, key string, src *os.File, partSize int64, resumable bool) (*osUploadSession, string, int64, error) {
	if !resumable || src == nil {
		id, err := o.initiateMultipart(ctx, key)
		return nil, id, 0, err
	}
	path, srcSize, mtime, err := fileFingerprint(src)
	if err != nil {
		id, e2 := o.initiateMultipart(ctx, key)
		return nil, id, 0, e2
	}
	live, expired, err := loadUploadSessions(o.acctID, osUploadSessionTTL)
	if err == nil {
		o.abortExpired(expired)
		for i := range live {
			s := live[i]
			if !s.matches(key, path, srcSize, mtime, partSize) {
				continue
			}
			remote, lerr := o.listUploadedParts(ctx, key, s.UploadID)
			if lerr != nil {
				// 云端已无此 UploadId（或它没实现 ListParts）：不能拿旧片号冒险，重头传。
				_ = removeUploadSession(o.acctID, s.UploadID)
				continue
			}
			skip := s.syncParts(remote)
			_ = putUploadSession(s)
			if skip > 0 {
				return &s, s.UploadID, skip, nil
			}
			// 云端一片都没了（上次崩在写完盘之前也可能）：同一 UploadId 接着用，从头传。
			return &s, s.UploadID, 0, nil
		}
	}
	id, err := o.initiateMultipart(ctx, key)
	if err != nil {
		return nil, "", 0, err
	}
	now := time.Now().UnixMilli()
	s := &osUploadSession{
		AcctID: o.acctID, Key: key, UploadID: id, PartSize: partSize,
		SrcPath: path, SrcSize: srcSize, SrcMTime: mtime, CreatedAt: now, UpdatedAt: now,
	}
	_ = putUploadSession(*s)
	return s, id, 0, nil
}

// abortExpired 对已过期的会话尽力 Abort 云端残留分片。失败不报：过期分片最终由
// bucket 的生命周期规则清理，这里只是顺手省钱，不能因此阻断本次上传。
func (o *objectStore) abortExpired(expired []osUploadSession) {
	if len(expired) == 0 {
		return
	}
	for _, s := range expired {
		gctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_ = o.abortMultipart(gctx, s.Key, s.UploadID)
		cancel()
	}
}

// listUploadedParts 问云端某 UploadId 实际收到了哪些分片。续传前必须核对：本地记的
// ETag 可能已被服务端清掉，或上次崩在写盘前后不一致。回 IsTruncated（>1000 片）时
// 不猜，直接报错让调用方重头传。
func (o *objectStore) listUploadedParts(ctx context.Context, key, uploadID string) ([]osSessionPart, error) {
	q := "uploadId=" + rawQueryEscape(uploadID)
	resp, err := o.do(ctx, http.MethodGet, key, q, nil, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	bs, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if strings.Contains(string(bs), "<Error>") {
		return nil, fmt.Errorf("%s 查询已传分片失败：%s", o.name, osErrMsg(bs))
	}
	var out osListPartsResp
	if err := xml.Unmarshal(bs, &out); err != nil {
		return nil, fmt.Errorf("解析 %s 分片列表失败: %w", o.name, err)
	}
	if out.IsTruncated {
		return nil, fmt.Errorf("%s 已传分片过多，无法安全续传", o.name)
	}
	parts := make([]osSessionPart, 0, len(out.Parts))
	for _, p := range out.Parts {
		etag := p.ETag
		if etag != "" && !strings.HasPrefix(etag, "\"") {
			etag = `"` + etag + `"`
		}
		parts = append(parts, osSessionPart{Num: p.PartNumber, ETag: etag, Size: p.Size})
	}
	return parts, nil
}

// localSource 从（可能被进度计数壳包了几层的）读取流里取出本地源文件。只有拿到源
// 文件，才谈得上「重启后校验内容没变、按同一 UploadId 续传」；内存流、跨盘转存来的
// 远端流不具备这个前提，不走续传路径。
func localSource(r io.Reader) (*os.File, bool) {
	for r != nil {
		if f, ok := r.(*os.File); ok {
			return f, true
		}
		u, ok := r.(interface{ underlying() io.Reader })
		if !ok {
			return nil, false
		}
		r = u.underlying()
	}
	return nil, false
}

// fileFingerprint 源文件坐标：绝对路径 + 大小 + mtime（毫秒）。
func fileFingerprint(f *os.File) (string, int64, int64, error) {
	st, err := f.Stat()
	if err != nil {
		return "", 0, 0, err
	}
	if !st.Mode().IsRegular() {
		return "", 0, 0, fmt.Errorf("上传源不是普通文件")
	}
	p, err := filepath.Abs(f.Name())
	if err != nil {
		return "", 0, 0, err
	}
	return p, st.Size(), st.ModTime().UnixMilli(), nil
}

// discardBytes 跳过源的前 n 字节（云端已有的分片不再重读）。能 Seek 的直接跳，
// 否则只能读着丢。
func discardBytes(r io.Reader, n int64) error {
	if s, ok := r.(io.Seeker); ok {
		if _, err := s.Seek(n, io.SeekCurrent); err == nil {
			return nil
		}
	}
	if _, err := io.CopyN(io.Discard, r, n); err != nil {
		return fmt.Errorf("源文件已变化（应跳过 %d 字节却读不完）: %w", n, err)
	}
	return nil
}

// initiateMultipart POST /<key>?uploads 取 UploadId。
func (o *objectStore) initiateMultipart(ctx context.Context, key string) (string, error) {
	resp, err := o.do(ctx, http.MethodPost, key, "uploads", nil, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	bs, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out osInitResp
	if err := xml.Unmarshal(bs, &out); err != nil || out.UploadID == "" {
		return "", fmt.Errorf("%s 发起分块上传失败：%s", o.name, osErrMsg(bs))
	}
	return out.UploadID, nil
}

// uploadPart PUT 单片并回传服务端 ETag（Complete 时必须原样带回，含引号）。
func (o *objectStore) uploadPart(ctx context.Context, key string, num int, uploadID string, data []byte) (string, error) {
	q := "partNumber=" + strconv.Itoa(num) + "&uploadId=" + rawQueryEscape(uploadID)
	resp, err := o.do(ctx, http.MethodPut, key, q, data, nil)
	if err != nil {
		return "", fmt.Errorf("%s 上传第 %d 片失败: %w", o.name, num, err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	etag := resp.Header.Get("ETag")
	if etag == "" {
		return "", fmt.Errorf("%s 第 %d 片未返回 ETag", o.name, num)
	}
	if !strings.HasPrefix(etag, "\"") {
		etag = `"` + etag + `"`
	}
	return etag, nil
}

// completeMultipart 提交分片清单，服务端据此拼装对象。
func (o *objectStore) completeMultipart(ctx context.Context, key, uploadID string, parts []osUploadPart) error {
	body, err := xml.Marshal(osCompleteReq{Parts: parts})
	if err != nil {
		return err
	}
	q := "uploadId=" + rawQueryEscape(uploadID)
	resp, err := o.do(ctx, http.MethodPost, key, q, append([]byte(xml.Header), body...),
		map[string]string{"Content-Type": "application/xml"})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	bs, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	// 合并失败同样可能回 200 + XML Error 体，这里一并判掉。
	if strings.Contains(string(bs), "<Error>") {
		return fmt.Errorf("%s 合并分片失败：%s", o.name, osErrMsg(bs))
	}
	return nil
}

// abortMultipart 放弃分块上传，清掉已上传的碎片。
func (o *objectStore) abortMultipart(ctx context.Context, key, uploadID string) error {
	q := "uploadId=" + rawQueryEscape(uploadID)
	resp, err := o.do(ctx, http.MethodDelete, key, q, nil, nil)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	return resp.Body.Close()
}

// statEntry 组装文件条目：优先 HEAD 拿真实 Content-Length / Content-Type /
// Last-Modified，HEAD 不可用则回落到已有信息（上传时已知的大小）。
func (o *objectStore) statEntry(ctx context.Context, key string, size int64) Entry {
	e := Entry{
		ID: key, Name: keyBase(key), Path: key,
		Size: size, MIME: "application/octet-stream",
	}
	resp, err := o.do(ctx, http.MethodHead, key, "", nil, nil)
	if err != nil {
		return e // HEAD 不可用（如权限受限）时回落到已有信息，不阻断上传结果
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	if cl := resp.ContentLength; cl >= 0 {
		e.Size = cl
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		e.MIME = ct
	}
	if lm := resp.Header.Get("Last-Modified"); lm != "" {
		if t, err := http.ParseTime(lm); err == nil {
			e.MTime = t.UnixMilli()
		}
	}
	return e
}

// copyObject 服务端复制（同 bucket）。meta 非空时作为覆盖后的元数据
// （对象存储要求复制时改元数据必须同时带 metadata-directive=REPLACE）。
// copy-source 必须写**已 URL 编码**的 /<bucket>/<key>（三家均如此）：直接拼原始 key
// 时，带空格/中文的源会被当成非法 header（Go 客户端还会拒绝换行），真机报 InvalidArgument。
func (o *objectStore) copyObject(ctx context.Context, srcKey, dstKey string, meta map[string]string) error {
	extra := map[string]string{}
	for k, v := range meta {
		extra[k] = v
	}
	src := uriPathFromResource(o.resource(srcKey))
	if o.kind == KindAliyunOSS {
		extra["x-oss-copy-source"] = src
		if len(meta) > 0 {
			extra["x-oss-metadata-directive"] = "REPLACE"
		}
	} else {
		extra["x-amz-copy-source"] = src
		if len(meta) > 0 {
			extra["x-amz-metadata-directive"] = "REPLACE"
		}
	}
	resp, err := o.do(ctx, http.MethodPut, dstKey, "", nil, extra)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	bs, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	// 对象存储复制失败常回 200 + XML Error 体，这里一并判掉。
	if strings.Contains(string(bs), "<Error>") {
		return fmt.Errorf("%s 复制失败：%s", o.name, osErrMsg(bs))
	}
	return nil
}

// Rename 同目录内 copy 到新 key 再删旧 key（对象存储无原生改名）。
// 目录走 copyPrefixTree：前缀下每一个对象（含占位对象）都搬，不只是一个空目录文件。
func (o *objectStore) Rename(ctx context.Context, id, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" || strings.Contains(newName, "/") {
		return fmt.Errorf("名称不合法")
	}
	if isDirKey(id) {
		newName += "/"
	}
	dst := keyDir(id) + newName
	if dst == id {
		return nil
	}
	if isDirKey(id) {
		return o.copyPrefixTree(ctx, id, dst)
	}
	if err := o.copyObject(ctx, id, dst, nil); err != nil {
		return err
	}
	return o.Delete(ctx, id)
}

// Move copy 到目标目录下（保留原名）再删原 key；目录走 copyPrefixTree。
func (o *objectStore) Move(ctx context.Context, id, dstParentID string) error {
	name := keyBase(id)
	if isDirKey(id) {
		name += "/"
	}
	dst := dirKey(dstParentID, name)
	if dst == id {
		return nil
	}
	if isDirKey(id) {
		return o.copyPrefixTree(ctx, id, dst)
	}
	if err := o.copyObject(ctx, id, dst, nil); err != nil {
		return err
	}
	return o.Delete(ctx, id)
}

// copyPrefixTree 目录整体改名/移动：先把 srcPrefix 下每一个 key（含目录占位对象）
// 按相同后缀 copy 到 dstPrefix，再逐个删源。对象存储没有原生目录语义，这一过程
// **不是原子**的：中途失败会留下「新旧两份都在」的半完成状态，重试同一操作会把新
// 前缀下的同名对象覆盖后再删源，可幂等重跑（已在交付说明里标注）。
// 另：单次服务端 copy 三家都有 5 GiB 上限，超过它的大文件需分块复制（未实现）。
func (o *objectStore) copyPrefixTree(ctx context.Context, srcPrefix, dstPrefix string) error {
	if srcPrefix == dstPrefix {
		return nil
	}
	if strings.HasPrefix(dstPrefix, srcPrefix) {
		return fmt.Errorf("目标路径在源目录内部，不能移动到自己之下")
	}
	keys, err := o.listAllKeys(ctx, srcPrefix)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		return fmt.Errorf("%s 目录不存在或已为空", o.name)
	}
	moved := 0
	for _, k := range keys {
		dst := dstPrefix + strings.TrimPrefix(k, srcPrefix)
		if dst == k {
			continue
		}
		if err := o.copyObject(ctx, k, dst, nil); err != nil {
			return fmt.Errorf("%s 迁移目录内容失败（已搬 %d/%d 个）: %w", o.name, moved, len(keys), err)
		}
		moved++
	}
	for _, k := range keys {
		dst := dstPrefix + strings.TrimPrefix(k, srcPrefix)
		if dst == k {
			continue
		}
		if err := o.deleteKey(ctx, k); err != nil {
			return fmt.Errorf("%s 清理源目录失败（新位置已有 %d 个对象，请手动删除旧目录）: %w", o.name, moved, err)
		}
	}
	return nil
}

// Delete 删对象（**彻底删除**，对象存储没有回收站语义）。目录则按前缀逐个删掉全部
// 子对象（含占位对象）：只删占位对象的话，剩下的子对象会让目录在列表里原地复活。
func (o *objectStore) Delete(ctx context.Context, id string) error {
	if !isDirKey(id) {
		return o.deleteKey(ctx, id)
	}
	keys, err := o.listAllKeys(ctx, id)
	if err != nil {
		return err
	}
	for i, k := range keys {
		if err := o.deleteKey(ctx, k); err != nil {
			return fmt.Errorf("%s 删除目录内容失败（已删 %d/%d 个）: %w", o.name, i, len(keys), err)
		}
	}
	return nil
}

// deleteKey 删单个对象（不递归）。对象存储对不存在的 key 也回 2xx，幂等。
func (o *objectStore) deleteKey(ctx context.Context, key string) error {
	resp, err := o.do(ctx, http.MethodDelete, key, "", nil, nil)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<10))
	return resp.Body.Close()
}

// ShareLink 生成预签名 GET URL（默认 24 小时）。对象存储没有「分享页」，
// 这条 URL 就是可直接下载的临时链接。
func (o *objectStore) ShareLink(ctx context.Context, id string) (string, error) {
	u, err := o.signer.presign(http.MethodGet, o.resource(id), 24*time.Hour)
	if err != nil {
		return "", err
	}
	return u, nil
}

// Open GET 流式返回，size 取 Content-Length。调用方 Close。
func (o *objectStore) Open(ctx context.Context, id string) (io.ReadCloser, int64, error) {
	resp, err := o.do(ctx, http.MethodGet, id, "", nil, nil)
	if err != nil {
		return nil, 0, err
	}
	size := resp.ContentLength
	if size < 0 {
		size = -1
	}
	return resp.Body, size, nil
}

// Quota 取 bucket 已用容量：OSS 与 COS 都有 GetBucketStat（GET /?stat 回 Storage）；
// 通用 S3（MinIO 等）多数未实现，请求失败或解析不出即回零值——前端 Total=0 就
// 不展示总量，不把它当错误。
func (o *objectStore) Quota(ctx context.Context) (Quota, error) {
	resp, err := o.do(ctx, http.MethodGet, "", "stat", nil, nil)
	if err != nil {
		return Quota{}, nil
	}
	defer resp.Body.Close()
	bs, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out osStatResp
	if err := xml.Unmarshal(bs, &out); err != nil {
		return Quota{}, nil
	}
	return Quota{Used: out.Storage}, nil
}
