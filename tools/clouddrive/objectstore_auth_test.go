package clouddrive

// 假后端的「独立重算签名」校验器（计划-云盘对象存储对接 PR-C ①）。
//
// 为什么要有这个文件：前两次 PR 各漏了一处**真机必炸**的签名缺陷——正文摘要写成
// SHA256(MD5(body))、canonical query 双重转义——而单测一路全绿，因为 httptest 假后端
// 只看请求形状、从不重算签名。本环境没有真实 bucket 与 AK/SK，能把真机校验前移的
// 最接近做法，就是让假后端按 OSS V1 / AWS SigV4 的规范**另写一套**签名重算，对不上
// 就回 403。
//
// 关键纪律：这里刻意**不调用** signer.go 的 signQuery / ossCanonicalResource /
// sortedKV / uriEscape / hmacHash / sha256Hex —— 复用生产代码的规范化助手等于自己证
// 自己，正是前两次漏检的原因。转义、排序、子资源筛选、四级派生密钥在此按规范重新
// 实现一遍，两边任何一处理解不一致都会立刻变红。
//
// 仍然要如实声明：这**只是 mock 层**。时钟偏移、TLS、bucket policy、真实服务的字段
// 差异（以及预签名 URL 里「正文哈希那一行到底写 UNSIGNED-PAYLOAD 还是空串 SHA256」
// 这类各家约定不一致的地方）都得靠真机挂 bucket 才能确认。

import (
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
)

// 假后端认可的凭证（与测试里建账号用的 AK/SK 一致）。
const (
	fakeAkID   = "AKID-TEST"
	fakeSecret = "SECRET-TEST"
)

/* ============================ 独立实现的摘要 / 编码原语 ============================ */

func fakeSHA256Hex(bs []byte) string {
	s := sha256.Sum256(bs)
	return hex.EncodeToString(s[:])
}

func fakeMD5B64(bs []byte) string {
	s := md5.Sum(bs)
	return base64.StdEncoding.EncodeToString(s[:])
}

func fakeHMACSHA1B64(secret, data string) string {
	m := hmac.New(sha1.New, []byte(secret))
	m.Write([]byte(data))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

func fakeHMACSHA256Hex(key []byte, data string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return hex.EncodeToString(m.Sum(nil))
}

// fakeSigV4Key 四级派生：AWS4<secret> → date → region → s3 → aws4_request。
func fakeSigV4Key(secret, date, region string) []byte {
	k := []byte("AWS4" + secret)
	for _, step := range []string{date, region, "s3", "aws4_request"} {
		m := hmac.New(sha256.New, k)
		m.Write([]byte(step))
		k = m.Sum(nil)
	}
	return k
}

// fakeEncode 按 RFC3986 逐字节转义：只留未保留字符（字母数字与 - . _ ~），其余写成
// 大写百分号十六进制（AWS 的 URI 编码定义；Go 的 QueryEscape 规则并不完全等价）。
func fakeEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
			c == '-' || c == '.' || c == '_' || c == '~' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

// fakeCanonicalQuery canonical query string：每个参数各自解码后**再各转义一次** key 与
// value，按（编码后的 key，编码后的 value）字典序排，空值写 `k=`。skip 里的键整条丢掉
// （预签名时要排除 X-Amz-Signature 本身）。
func fakeCanonicalQuery(raw string, skip map[string]bool) string {
	var pairs [][2]string
	if raw != "" {
		for _, part := range strings.Split(raw, "&") {
			if part == "" {
				continue
			}
			k, v, hasEq := strings.Cut(part, "=")
			dk, err := url.QueryUnescape(k)
			if err != nil {
				dk = k
			}
			dv := ""
			if hasEq {
				dv = v
				if u, err := url.QueryUnescape(v); err == nil {
					dv = u
				}
			}
			if skip[dk] {
				continue
			}
			pairs = append(pairs, [2]string{dk, dv})
		}
	}
	sort.SliceStable(pairs, func(i, j int) bool {
		ki, kj := fakeEncode(pairs[i][0]), fakeEncode(pairs[j][0])
		if ki != kj {
			return ki < kj
		}
		return fakeEncode(pairs[i][1]) < fakeEncode(pairs[j][1])
	})
	var b strings.Builder
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(fakeEncode(p[0]))
		b.WriteByte('=')
		b.WriteString(fakeEncode(p[1]))
	}
	return b.String()
}

// fakeCanonicalHeaders 按客户端声明的 SignedHeaders 重建 canonical headers：名转小写、
// 按字典序、值去首尾空白并把连续空白压成一个空格，每行 `key:value\n`。host 必须参与
// 签名（AWS 硬性要求），声明列表本身也必须已排序。
func fakeCanonicalHeaders(r *http.Request, signedHeaders string) (string, error) {
	if !strings.Contains(signedHeaders, "host") {
		return "", fmt.Errorf("SignedHeaders 里没有 host: %q", signedHeaders)
	}
	declared := strings.Split(signedHeaders, ";")
	norm := make([]string, 0, len(declared))
	for _, n := range declared {
		norm = append(norm, strings.ToLower(strings.TrimSpace(n)))
	}
	peek := append([]string(nil), norm...)
	sort.Strings(peek)
	if strings.Join(peek, ";") != strings.Join(norm, ";") {
		return "", fmt.Errorf("SignedHeaders 未按字典序: %q", signedHeaders)
	}
	var b strings.Builder
	for _, n := range norm {
		v := r.Host
		if n != "host" {
			v = strings.Join(r.Header.Values(n), ",")
		}
		b.WriteString(n)
		b.WriteByte(':')
		b.WriteString(strings.Join(strings.Fields(v), " "))
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// fakeOSSSubres OSS V1 需要进 CanonicalizedResource 的子资源。按官方文档另抄一遍，
// 不引用 signer.go 的 ossSubresources：生产侧漏一个，这里就是一个真机 403 点。
var fakeOSSSubres = map[string]bool{
	"acl": true, "cors": true, "delete": true, "lifecycle": true, "location": true,
	"partNumber": true, "policy": true, "requestPayment": true, "stat": true,
	"uploadId": true, "uploads": true, "versionId": true, "versioning": true,
	"versions": true, "website": true,
	"response-cache-control": true, "response-content-disposition": true,
	"response-content-encoding": true, "response-content-language": true,
	"response-content-type": true, "response-expires": true,
}

// fakeOSSResource CanonicalizedResource = /<bucket>/<key>（**解码后**的 path，
// path-style 下请求 path 天然带着 bucket）+ 命中的子资源按字典序拼 `?k=v`（空值只写 k）。
func fakeOSSResource(r *http.Request) string {
	res := "/" + strings.TrimPrefix(r.URL.Path, "/")
	q := r.URL.Query()
	var names []string
	for k := range q {
		if fakeOSSSubres[k] {
			names = append(names, k)
		}
	}
	sort.Strings(names)
	if len(names) == 0 {
		return res
	}
	var b strings.Builder
	b.WriteString(res)
	for i, k := range names {
		if i == 0 {
			b.WriteByte('?')
		} else {
			b.WriteByte('&')
		}
		b.WriteString(k)
		if v := q.Get(k); v != "" {
			b.WriteByte('=')
			b.WriteString(v)
		}
	}
	return b.String()
}

// fakeOSSHeaders CanonicalizedOSSHeaders：所有 x-oss-* 头小写名、按字典序、`k:v\n`。
func fakeOSSHeaders(r *http.Request) string {
	names := make([]string, 0, 2)
	vals := map[string]string{}
	for k := range r.Header {
		lk := strings.ToLower(k)
		if !strings.HasPrefix(lk, "x-oss-") {
			continue
		}
		names = append(names, lk)
		vals[lk] = strings.Join(strings.Fields(r.Header.Get(k)), " ")
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(n)
		b.WriteByte(':')
		b.WriteString(vals[n])
		b.WriteByte('\n')
	}
	return b.String()
}

/* ============================ 入口 ============================ */

// verifyAuth 重算并核对一次请求的签名。回 ("", "") 表示通过，否则回（错误码, 文案），
// 由假后端包成 403 的 XML Error——于是「签名算错」这一整类缺陷在 mock 层就会变红。
func verifyAuth(r *http.Request, body []byte, akID, secret string) (code, msg string) {
	auth := r.Header.Get("Authorization")
	switch {
	case strings.HasPrefix(auth, "OSS "):
		return verifyOSSV1Header(r, body, auth, akID, secret)
	case strings.HasPrefix(auth, "AWS4-HMAC-SHA256 "):
		return verifySigV4Header(r, body, auth, akID, secret)
	}
	q := r.URL.Query()
	if q.Has("X-Amz-Signature") {
		return verifySigV4Query(r, akID, secret)
	}
	if q.Has("Signature") && q.Has("OSSAccessKeyId") {
		return verifyOSSV1Query(r, akID, secret)
	}
	return "AccessDenied", "请求既没有 Authorization 头，也没有预签名参数"
}

func verifyOSSV1Header(r *http.Request, body []byte, auth, akID, secret string) (string, string) {
	id, sig, ok := strings.Cut(strings.TrimPrefix(auth, "OSS "), ":")
	if !ok || id == "" || sig == "" {
		return "AuthorizationArgumentError", "Authorization 形态不对: " + auth
	}
	if id != akID {
		return "InvalidAccessKeyId", "不认识的 AccessKey ID: " + id
	}
	if r.Header.Get("Date") == "" || r.Header.Get("x-oss-date") == "" {
		return "RequestTimeout", "缺 Date / x-oss-date"
	}
	cm := r.Header.Get("Content-MD5")
	if cm != "" && cm != fakeMD5B64(body) {
		return "BadDigest", fmt.Sprintf("Content-MD5=%s 与正文 MD5 %s 不一致", cm, fakeMD5B64(body))
	}
	stringToSign := strings.Join([]string{
		r.Method,
		cm,
		r.Header.Get("Content-Type"),
		r.Header.Get("Date"),
		fakeOSSHeaders(r) + fakeOSSResource(r),
	}, "\n")
	want := fakeHMACSHA1B64(secret, stringToSign)
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return "SignatureDoesNotMatch", "OSS V1 签名重算不一致，canonical:\n" + stringToSign
	}
	return "", ""
}

func verifyOSSV1Query(r *http.Request, akID, secret string) (string, string) {
	q := r.URL.Query()
	id, expires, sig := q.Get("OSSAccessKeyId"), q.Get("Expires"), q.Get("Signature")
	if id == "" || expires == "" || sig == "" {
		return "AuthorizationArgumentError", "预签名参数不全: " + r.URL.RawQuery
	}
	if id != akID {
		return "InvalidAccessKeyId", "不认识的 AccessKey ID: " + id
	}
	sec, err := strconv.ParseInt(expires, 10, 64)
	if err != nil {
		return "AuthorizationArgumentError", "Expires 不是数字: " + expires
	}
	if time.Now().Unix() > sec {
		return "AccessDenied", "预签名 URL 已过期"
	}
	// 无请求头参与签名：CanonicalizedOSSHeaders 是空串，但那一个空行必须留着。
	stringToSign := strings.Join([]string{strings.ToUpper(r.Method), "", "", expires, fakeOSSResource(r)}, "\n")
	want := fakeHMACSHA1B64(secret, stringToSign)
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return "SignatureDoesNotMatch", "OSS 预签名重算不一致，canonical:\n" + stringToSign
	}
	return "", ""
}

func verifySigV4Header(r *http.Request, body []byte, auth, akID, secret string) (string, string) {
	fields := map[string]string{}
	for _, part := range strings.Split(strings.TrimPrefix(auth, "AWS4-HMAC-SHA256"), ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(part), "=")
		fields[k] = v
	}
	cred, signedHeaders, sig := fields["Credential"], fields["SignedHeaders"], fields["Signature"]
	if cred == "" || signedHeaders == "" || sig == "" {
		return "AuthorizationHeaderMalformed", "Authorization 缺字段: " + auth
	}
	segs := strings.Split(cred, "/")
	if len(segs) != 5 || segs[3] != "s3" || segs[4] != "aws4_request" {
		return "AuthorizationHeaderMalformed", "Credential scope 形态不对: " + cred
	}
	if segs[0] != akID {
		return "InvalidAccessKeyId", "不认识的 AccessKey ID: " + segs[0]
	}
	ymd, region := segs[1], segs[2]
	amzDate := r.Header.Get("X-Amz-Date")
	if len(amzDate) != 16 || !strings.HasPrefix(amzDate, ymd) || !strings.HasSuffix(amzDate, "Z") {
		return "InvalidArgument", "x-amz-date 与 credential scope 的日期对不上: " + amzDate
	}
	declared := r.Header.Get("X-Amz-Content-Sha256")
	switch {
	case declared == "":
		return "InvalidArgument", "缺 x-amz-content-sha256"
	case declared != "UNSIGNED-PAYLOAD" && declared != fakeSHA256Hex(body):
		return "XAmzContentSHA256Mismatch", fmt.Sprintf("x-amz-content-sha256=%s 与正文实际哈希 %s 不一致",
			declared, fakeSHA256Hex(body))
	}
	canonHeaders, err := fakeCanonicalHeaders(r, signedHeaders)
	if err != nil {
		return "InvalidRequest", err.Error()
	}
	canonicalRequest := strings.Join([]string{
		r.Method,
		r.URL.EscapedPath(),
		fakeCanonicalQuery(r.URL.RawQuery, nil),
		canonHeaders,
		signedHeaders,
		declared,
	}, "\n")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		ymd + "/" + region + "/s3/aws4_request",
		fakeSHA256Hex([]byte(canonicalRequest)),
	}, "\n")
	want := fakeHMACSHA256Hex(fakeSigV4Key(secret, ymd, region), stringToSign)
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return "SignatureDoesNotMatch", "SigV4 签名重算不一致，canonical:\n" + canonicalRequest
	}
	return "", ""
}

func verifySigV4Query(r *http.Request, akID, secret string) (string, string) {
	q := r.URL.Query()
	if q.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" {
		return "InvalidRequest", "X-Amz-Algorithm 不对: " + q.Get("X-Amz-Algorithm")
	}
	sig := q.Get("X-Amz-Signature")
	signedHeaders := q.Get("X-Amz-SignedHeaders")
	amzDate := q.Get("X-Amz-Date")
	segs := strings.Split(q.Get("X-Amz-Credential"), "/")
	if len(segs) != 5 || segs[3] != "s3" || segs[4] != "aws4_request" {
		return "AuthorizationQueryParametersError", "X-Amz-Credential 形态不对: " + q.Get("X-Amz-Credential")
	}
	if segs[0] != akID {
		return "InvalidAccessKeyId", "不认识的 AccessKey ID: " + segs[0]
	}
	if sig == "" || signedHeaders == "" || len(amzDate) != 16 || segs[1] != amzDate[:8] {
		return "AuthorizationQueryParametersError", "预签名参数不全或日期对不上: " + r.URL.RawQuery
	}
	expSec, err := strconv.Atoi(q.Get("X-Amz-Expires"))
	if err != nil || expSec <= 0 {
		return "AuthorizationQueryParametersError", "X-Amz-Expires 不对: " + q.Get("X-Amz-Expires")
	}
	at, err := time.Parse("20060102T150405Z", amzDate)
	if err != nil {
		return "AuthorizationQueryParametersError", "X-Amz-Date 解析失败: " + amzDate
	}
	if time.Now().UTC().After(at.Add(time.Duration(expSec) * time.Second)) {
		return "AccessDenied", "预签名 URL 已过期"
	}
	canonHeaders, err := fakeCanonicalHeaders(r, signedHeaders)
	if err != nil {
		return "InvalidRequest", err.Error()
	}
	canonicalRequest := strings.Join([]string{
		strings.ToUpper(r.Method),
		r.URL.EscapedPath(),
		fakeCanonicalQuery(r.URL.RawQuery, map[string]bool{"X-Amz-Signature": true}),
		canonHeaders,
		signedHeaders,
		// 预签名 URL 不随请求传正文哈希：这一行各家一律写字面量 UNSIGNED-PAYLOAD
		// （AWS SDK / MinIO / 阿里 S3 兼容文档同此）。这是本校验器唯一要向生产
		// 约定看齐的一行，其余全部独立重算。
		"UNSIGNED-PAYLOAD",
	}, "\n")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		segs[1] + "/" + segs[2] + "/s3/aws4_request",
		fakeSHA256Hex([]byte(canonicalRequest)),
	}, "\n")
	want := fakeHMACSHA256Hex(fakeSigV4Key(secret, segs[1], segs[2]), stringToSign)
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return "SignatureDoesNotMatch", "SigV4 预签名重算不一致，canonical:\n" + canonicalRequest
	}
	return "", ""
}

/* ============================ 用例：签名真能被判错 ============================ */

// doRaw 绕开 provider，手工发一个请求，回状态码与响应体（验假后端确实会拒）。
func doRaw(t *testing.T, req *http.Request) (int, string) {
	t.Helper()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求发不出去: %v", err)
	}
	defer resp.Body.Close()
	bs, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return resp.StatusCode, string(bs)
}

// TestFakeBackendRejectsTamperedRequests 是这套校验器自身的回归锁：签名对不上、
// 声明的正文摘要与真实正文不一致，都必须被 403 拒掉。少了这一层，「假后端会重算签名」
// 只是一句口头保证。
func TestFakeBackendRejectsTamperedRequests(t *testing.T) {
	ctx := context.Background()

	t.Run("错签名-OSS", func(t *testing.T) {
		f := newFakeOS(t, "tamper-oss")
		o, _ := newTestOS(t, KindAliyunOSS, "OSS", f)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.urlFor("", "max-keys=1"), nil)
		if err != nil {
			t.Fatal(err)
		}
		now := time.Now().UTC().Format(http.TimeFormat)
		req.Header.Set("Date", now)
		req.Header.Set("x-oss-date", now)
		req.Header.Set("Authorization", "OSS "+fakeAkID+":AAAAAAAAAAAAAAAAAAAAAAAAAAA=")
		code, body := doRaw(t, req)
		if code != http.StatusForbidden || !strings.Contains(body, "SignatureDoesNotMatch") {
			t.Fatalf("错签名应 403 SignatureDoesNotMatch，实际 %d %s", code, body)
		}
	})

	t.Run("错签名-SigV4", func(t *testing.T) {
		f := newFakeOS(t, "tamper-s3")
		o, _ := newTestOS(t, KindS3, "S3", f)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, o.urlFor("", "max-keys=1"), nil)
		if err != nil {
			t.Fatal(err)
		}
		amz := time.Now().UTC().Format("20060102T150405Z")
		req.Header.Set("X-Amz-Date", amz)
		req.Header.Set("X-Amz-Content-Sha256",
			"e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
		req.Header.Set("Authorization", fmt.Sprintf(
			"AWS4-HMAC-SHA256 Credential=%s/%s/us-east-1/s3/aws4_request, SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=%s",
			fakeAkID, amz[:8], strings.Repeat("0", 64)))
		code, body := doRaw(t, req)
		if code != http.StatusForbidden || !strings.Contains(body, "SignatureDoesNotMatch") {
			t.Fatalf("错签名应 403 SignatureDoesNotMatch，实际 %d %s", code, body)
		}
	})

	// 下面两条正是 PR-A 那次真机缺陷的形状：摘要不是由正文本体算出的。
	t.Run("正文摘要不符-SigV4", func(t *testing.T) {
		f := newFakeOS(t, "digest-s3")
		o, _ := newTestOS(t, KindS3, "S3", f)
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, o.urlFor("a.txt", ""), strings.NewReader("two"))
		if err != nil {
			t.Fatal(err)
		}
		if err := o.signer.sign(req, o.resource("a.txt"), []byte("one")); err != nil { // 签的是另一段正文
			t.Fatal(err)
		}
		code, body := doRaw(t, req)
		if code != http.StatusForbidden || !strings.Contains(body, "XAmzContentSHA256Mismatch") {
			t.Fatalf("正文与 x-amz-content-sha256 不一致应被拒，实际 %d %s", code, body)
		}
	})

	t.Run("正文摘要不符-OSS", func(t *testing.T) {
		f := newFakeOS(t, "digest-oss")
		o, _ := newTestOS(t, KindAliyunOSS, "OSS", f)
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, o.urlFor("a.txt", ""), strings.NewReader("two"))
		if err != nil {
			t.Fatal(err)
		}
		if err := o.signer.sign(req, o.resource("a.txt"), []byte("one")); err != nil {
			t.Fatal(err)
		}
		code, body := doRaw(t, req)
		if code != http.StatusForbidden || !strings.Contains(body, "BadDigest") {
			t.Fatalf("Content-MD5 与正文不一致应被拒，实际 %d %s", code, body)
		}
	})

	t.Run("签名与实际发出的 path 不一致", func(t *testing.T) {
		// 「签一份、发另一份」就是双重转义那一类缺陷的形状：服务端只会按**收到的**
		// path 重算 canonical URI，所以发出去之前被改过一笔就必然 403。
		f := newFakeOS(t, "encode-s3")
		o, _ := newTestOS(t, KindS3, "S3", f)
		body := []byte("x")
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, o.urlFor("a.txt", ""), bytesReader(body))
		if err != nil {
			t.Fatal(err)
		}
		if err := o.signer.sign(req, o.resource("a.txt"), body); err != nil {
			t.Fatal(err)
		}
		req.URL.RawPath = ""
		req.URL.Path = o.resource("b.txt") // 签的 a.txt，发的 b.txt
		code, msg := doRaw(t, req)
		if code != http.StatusForbidden || !strings.Contains(msg, "SignatureDoesNotMatch") {
			t.Fatalf("path 与签名不一致应被拒，实际 %d %s", code, msg)
		}
	})
}

// TestPresignedURLVerifiesAgainstFake 把「分享链接」这条路也拉到可验证层面：签完的 URL
// 直接发给假后端必须能取回原对象。改一个字节都拿不到（各家对预签名的要求都是这样）。
func TestPresignedURLVerifiesAgainstFake(t *testing.T) {
	body := []byte("分享 内容 test")
	key := "分享 目录/a b.txt"
	for _, tc := range []struct{ kind, name string }{
		{KindAliyunOSS, "OSS"},
		{KindTencentCOS, "COS"},
		{KindS3, "S3"},
	} {
		f := newFakeOS(t, "presign-"+tc.kind)
		o, _ := newTestOS(t, tc.kind, tc.name, f)
		if _, err := o.Upload(context.Background(), "分享 目录", "a b.txt", bytesReader(body), int64(len(body))); err != nil {
			t.Fatalf("%s 预置对象失败: %v", tc.kind, err)
		}
		u, err := o.ShareLink(context.Background(), key)
		if err != nil {
			t.Fatalf("%s 生成分享链接失败: %v", tc.kind, err)
		}
		resp, err := http.Get(u)
		if err != nil {
			t.Fatalf("%s 预签名 URL 请求失败: %v", tc.kind, err)
		}
		bs, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(bs) != string(body) {
			t.Fatalf("%s 预签名 URL 应能直接下载: %d %s (%s)", tc.kind, resp.StatusCode, string(bs), u)
		}

		// 篡改有效期：canonical 里带着它，签名立刻对不上。
		tampered := u
		if tc.kind == KindAliyunOSS {
			tampered = strings.Replace(u, "Expires=", "Expires=1", 1)
		} else {
			tampered = strings.Replace(u, "X-Amz-Expires=", "X-Amz-Expires=1", 1)
		}
		if tampered == u {
			t.Fatalf("%s 没找到可篡改的有效期参数: %s", tc.kind, u)
		}
		resp2, err := http.Get(tampered)
		if err != nil {
			t.Fatal(err)
		}
		bs2, _ := io.ReadAll(resp2.Body)
		resp2.Body.Close()
		if resp2.StatusCode != http.StatusForbidden || !strings.Contains(string(bs2), "SignatureDoesNotMatch") {
			t.Fatalf("%s 篡改后的预签名应被拒: %d %s", tc.kind, resp2.StatusCode, string(bs2))
		}
	}
}

// TestSignatureCoversNonASCIIQuery 三家都过一次带空格/中文/加号的游标：这类值在
// canonical query 里转义次数错一次，真机就是 403（本层现在能当场判出来）。
func TestSignatureCoversNonASCIIQuery(t *testing.T) {
	for _, kind := range []string{KindAliyunOSS, KindTencentCOS, KindS3} {
		f := newFakeOS(t, "q-"+kind)
		o, _ := newTestOS(t, kind, kind, f)
		f.seed("报告 v1+2/a b.txt", []byte("x"))
		f.seed("报告 v1+2/中文.bin", []byte("y"))
		entries, err := o.List(context.Background(), "报告 v1+2")
		if err != nil {
			t.Fatalf("%s 带非 ASCII 前缀的列表应通过签名校验: %v", kind, err)
		}
		if len(entries) != 2 {
			t.Fatalf("%s 列表条目不对: %+v", kind, entries)
		}
	}
}
