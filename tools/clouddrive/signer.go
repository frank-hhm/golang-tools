package clouddrive

// 对象存储签名器（计划-云盘对象存储对接 PR-A）。
//
// 只依赖 stdlib（crypto/hmac、crypto/sha、net/http、net/url、sort、strings、time），
// 不引入任何云 SDK：跨平台零 cgo、Linux 面板构建同样存在（AGENTS 硬约束）。
//
// 两套签名并存，由 objectStore 按账号 kind 挂不同实现：
//   - ossSigner：阿里云 OSS 原生 V1 —— Authorization: OSS <akid>:<base64(hmac-sha1)>
//   - s3Signer ：AWS Signature V4（HMAC-SHA256 派生密钥）—— 腾讯云 COS 与通用 S3 共用，
//     只差 endpoint / region / service 默认值。
//
// 寻址约定：本包统一用**虚拟主机式**（https://<bucket>.<endpoint host>/<key>），
// CanonicalizedResource / canonical URI 一律写成 /<bucket>/<key>（path-style 的 endpoint
// 也按同一规则签，OSS 与 S3 都接受）。

import (
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// signer 一次请求的签名与预签名 URL 生成。
// resource 是 /<bucket>/<key> 形式的签名用资源路径（key 未转义）；
// payload 是请求正文原字节（nil/空 表示无正文）——两家各自从中取自己需要的摘要：
// OSS 取 MD5 作 Content-MD5，SigV4 取 SHA256 作 x-amz-content-sha256。
type signer interface {
	sign(req *http.Request, resource string, payload []byte) error
	presign(method, resource string, expiry time.Duration) (string, error)
}

/* ============================ 公共小工具 ============================ */

// emptySHA256Hex 空正文的 SHA256（SigV4 里 GET/DELETE 等无正文请求的正文哈希）。
const emptySHA256Hex = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

// unsignedPayload 预签名 URL 的正文哈希占位串：签名时正文还不存在（PUT）或根本没有
// 正文（GET），且这一行不会随 URL 发给服务端，各家（AWS SDK / MinIO / 阿里 S3 兼容
// 文档）在查询串签名场景一律用这个字面量，而不是空串的 SHA256。
const unsignedPayload = "UNSIGNED-PAYLOAD"

// httpDate RFC1123 的 GMT 格式（OSS V1 要求标准 Date 头；SigV4 只用 x-amz-date）。
func httpDate(t time.Time) string { return t.UTC().Format(http.TimeFormat) }

// amzDate SigV4 的 ISO8601 基本格式（20130524T000000Z）。
func amzDate(t time.Time) string { return t.UTC().Format("20060102T150405Z") }

// ymdDate SigV4 凭证 scope 里的日期段（20130524）。
func ymdDate(t time.Time) string { return t.UTC().Format("20060102") }

// sha256Hex 字节串的十六进制 SHA256。
func sha256Hex(bs []byte) string {
	sum := sha256.Sum256(bs)
	return hex.EncodeToString(sum[:])
}

// hmacHash 算一次 HMAC 并以十六进制编码。
func hmacHash(h func() hash.Hash, key, data string) string {
	m := hmac.New(h, []byte(key))
	m.Write([]byte(data))
	return hex.EncodeToString(m.Sum(nil))
}

// signQuery 解析查询串成 [key,value] 列表（保留空值参数，如 `?uploads`）。
// 值统一解码回原形：两家都从「原始值」出发做自己那套转义（SigV4 在 sortedKV.encode 里
// URI-encode 一次，OSS 直接拼原值）。若不解码就变成「把 URL 里的 %2F 再转一次」的
// 双重编码，凡是 marker/prefix/uploadId 带 `/`、空格或非 ASCII 就会与重算签名的服务端对不上。
func signQuery(raw string) [][2]string {
	var out [][2]string
	if raw == "" {
		return out
	}
	for _, part := range strings.Split(raw, "&") {
		if part == "" {
			continue
		}
		k, v, _ := strings.Cut(part, "=")
		if dec, err := url.QueryUnescape(v); err == nil {
			v = dec
		}
		out = append(out, [2]string{k, v})
	}
	return out
}

// rawQueryEscape 按 AWS SigV4 的要求转义查询参数：未保留字符（字母数字 `-_.~`）之外
// 一律百分号编码（大写十六进制），空格用 `%20` 而不是 `+`。
func rawQueryEscape(v string) string {
	e := url.QueryEscape(v)
	e = strings.ReplaceAll(e, "+", "%20")
	e = strings.ReplaceAll(e, "*", "%2A")
	e = strings.ReplaceAll(e, "%7E", "~")
	return e
}

// sortedKV 一个「已按 key、再按 value 排序」的查询参数集合，附带编码态渲染。
type sortedKV struct{ items [][2]string }

func newSortedKV(pairs [][2]string) sortedKV {
	cp := append([][2]string(nil), pairs...)
	sort.SliceStable(cp, func(i, j int) bool {
		if cp[i][0] != cp[j][0] {
			return cp[i][0] < cp[j][0]
		}
		return cp[i][1] < cp[j][1]
	})
	return sortedKV{cp}
}

// encode 渲染成 canonical query（key=value&...，key 与 value 各按 SigV4 规则转义一次）。
// 所有参数一视同仁：X-Amz-Credential 里的 `/` 也必须写成 `%2F`（服务端就是这么重算的，
// 跳过转义会让预签名 URL 永远 SignatureDoesNotMatch）。
func (s sortedKV) encode() string {
	var b strings.Builder
	for _, p := range s.items {
		if b.Len() > 0 {
			b.WriteByte('&')
		}
		b.WriteString(rawQueryEscape(p[0]))
		b.WriteByte('=')
		b.WriteString(rawQueryEscape(p[1]))
	}
	return b.String()
}

/* ============================ 阿里 OSS V1 ============================ */

// ossSubresources 参与 CanonicalizedResource 的子资源参数（按字典序拼接）。
var ossSubresources = []string{
	"acl", "cors", "delete", "lifecycle", "location", "partNumber", "policy",
	"requestPayment", "response-cache-control", "response-content-disposition",
	"response-content-encoding", "response-content-language", "response-content-type",
	"response-expires", "stat", "uploadId", "uploads", "versionId", "versioning",
	"versions", "website",
}

// ossSigner 阿里云 OSS 原生签名 V1（HMAC-SHA1）。
type ossSigner struct {
	host   string // bucket.<endpoint host>（presign 时重建 URL 用）
	akID   string
	secret string
}

func newOSSSigner(host, akID, secret string) *ossSigner {
	return &ossSigner{host: host, akID: akID, secret: secret}
}

// ossCanonicalResource 拼 /<bucket>/<key> + 命中的子资源（含值），照 OSS V1 规范。
func ossCanonicalResource(resource string, q [][2]string) string {
	var keep [][2]string
	for _, key := range ossSubresources {
		for _, p := range q {
			if p[0] == key {
				keep = append(keep, p)
			}
		}
	}
	if len(keep) == 0 {
		return resource
	}
	var b strings.Builder
	b.WriteString(resource)
	b.WriteByte('?')
	for i, p := range keep {
		if i > 0 {
			b.WriteByte('&')
		}
		b.WriteString(p[0])
		if p[1] != "" {
			b.WriteByte('=')
			b.WriteString(p[1])
		}
	}
	return b.String()
}

// ossCanonicalHeaders 收集 x-oss-* 头：小写 key、同 key 值合并、按 key 字典序，
// 每行 `key:value\n`。
func ossCanonicalHeaders(req *http.Request) string {
	vals := map[string]string{}
	var order []string
	for k, v := range req.Header {
		lk := strings.ToLower(k)
		if !strings.HasPrefix(lk, "x-oss-") {
			continue
		}
		if _, seen := vals[lk]; !seen {
			order = append(order, lk)
		}
		vals[lk] = strings.Join(trimSpaceList(v), ",")
	}
	sort.Strings(order)
	var b strings.Builder
	for _, k := range order {
		b.WriteString(k)
		b.WriteByte(':')
		b.WriteString(vals[k])
		b.WriteByte('\n')
	}
	return b.String()
}

// trimSpaceList 逐项去掉首尾空白（头值可能有多行折行）。
func trimSpaceList(vs []string) []string {
	out := make([]string, 0, len(vs))
	for _, v := range vs {
		out = append(out, strings.TrimSpace(v))
	}
	return out
}

// sign 写入 Date / x-oss-date / Content-MD5 与 Authorization 头。
func (s *ossSigner) sign(req *http.Request, resource string, payload []byte) error {
	if s.secret == "" || s.akID == "" {
		return fmt.Errorf("对象存储凭证不完整（AccessKey 缺失）")
	}
	now := httpDate(time.Now())
	req.Header.Set("Date", now)
	req.Header.Set("x-oss-date", now)
	if len(payload) > 0 {
		req.Header.Set("Content-MD5", base64.StdEncoding.EncodeToString(md5Bytes(payload)))
	}
	canonHeaders := ossCanonicalHeaders(req)
	stringToSign := strings.Join([]string{
		req.Method,
		req.Header.Get("Content-MD5"),
		req.Header.Get("Content-Type"),
		now,
		canonHeaders + ossCanonicalResource(resource, signQuery(req.URL.RawQuery)),
	}, "\n")
	m := hmac.New(sha1.New, []byte(s.secret))
	m.Write([]byte(stringToSign))
	req.Header.Set("Authorization", "OSS "+s.akID+":"+base64.StdEncoding.EncodeToString(m.Sum(nil)))
	return nil
}

// presign 生成带 OSSAccessKeyId / Expires / Signature 的临时下载 URL。
func (s *ossSigner) presign(method, resource string, expiry time.Duration) (string, error) {
	if s.secret == "" || s.akID == "" {
		return "", fmt.Errorf("对象存储凭证不完整（AccessKey 缺失）")
	}
	if expiry <= 0 {
		expiry = 24 * time.Hour
	}
	expires := time.Now().UTC().Add(expiry).Unix()
	// 无请求头参与签名：CanonicalizedOSSHeaders 为空串（保留那个空行）。
	stringToSign := strings.Join([]string{
		strings.ToUpper(method),
		"", "",
		fmt.Sprintf("%d", expires),
		resource,
	}, "\n")
	m := hmac.New(sha1.New, []byte(s.secret))
	m.Write([]byte(stringToSign))
	q := url.Values{}
	q.Set("OSSAccessKeyId", s.akID)
	q.Set("Expires", fmt.Sprintf("%d", expires))
	q.Set("Signature", base64.StdEncoding.EncodeToString(m.Sum(nil)))
	return osBase(s.host) + uriPathFromResource(resource) + "?" + q.Encode(), nil
}

/* ============================ AWS Signature V4 ============================ */

// s3Signer AWS SigV4（COS 与通用 S3 共用，service 固定 "s3"）。
type s3Signer struct {
	host   string
	region string
	akID   string
	secret string
}

func newS3Signer(host, region, akID, secret string) *s3Signer {
	return &s3Signer{host: host, region: region, akID: akID, secret: secret}
}

// sigv4CanonicalURI 对 /<bucket>/<key> 的每个路径段做安全 URI 编码（已编码的段保持不变）。
func sigv4CanonicalURI(resource string) string {
	segs := strings.Split(strings.TrimPrefix(resource, "/"), "/")
	out := make([]string, 0, len(segs))
	for _, s := range segs {
		out = append(out, uriEscape(s))
	}
	return "/" + strings.Join(out, "/")
}

// uriEscape 按 SigV4 规则编码路径段（未保留字符全转大写十六进制）。
func uriEscape(v string) string {
	var b strings.Builder
	for i := 0; i < len(v); i++ {
		c := v[i]
		if isUnreserved(c) || c == '/' {
			b.WriteByte(c)
			continue
		}
		fmt.Fprintf(&b, "%%%02X", c)
	}
	return b.String()
}

func isUnreserved(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') ||
		c == '-' || c == '.' || c == '_' || c == '~'
}

// sigv4Headers 收集参与签名的头（host + x-amz-* + content-type）：
// 返回 canonical 串与已签名头列表。时刻只用 x-amz-date 表达（与 AWS 官方一致）。
func sigv4Headers(req *http.Request) (string, string) {
	keys := []string{"host"}
	if ct := req.Header.Get("Content-Type"); ct != "" {
		keys = append(keys, "content-type")
	}
	for k := range req.Header {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "x-amz-") {
			keys = append(keys, lk)
		}
	}
	sort.Strings(keys)
	var b strings.Builder
	var signed []string
	for _, k := range keys {
		var val string
		switch k {
		case "host":
			val = req.URL.Host
		case "content-type":
			val = req.Header.Get("Content-Type")
		default:
			val = req.Header.Get(k)
		}
		b.WriteString(k)
		b.WriteByte(':')
		b.WriteString(strings.Join(trimSpaceList(strings.Split(val, ",")), ","))
		b.WriteByte('\n')
		signed = append(signed, k)
	}
	return b.String(), strings.Join(signed, ";")
}

// signingKey 按 date/region/s3/aws4_request 逐级 HMAC-SHA256 派生签名密钥。
func (s *s3Signer) signingKey(date string) []byte {
	k := []byte("AWS4" + s.secret)
	steps := []string{date, s.region, "s3", "aws4_request"}
	for _, st := range steps {
		m := hmac.New(sha256.New, k)
		m.Write([]byte(st))
		k = m.Sum(nil)
	}
	return k
}

// sign 写入 x-amz-date / x-amz-content-sha256 / Authorization 头。
// 正文哈希必须是**正文本体的 SHA256**（服务端会重算比对，错了就是 SignatureDoesNotMatch）；
// 调用方已显式给了 x-amz-content-sha256（如 UNSIGNED-PAYLOAD）则尊重该值。
func (s *s3Signer) sign(req *http.Request, resource string, payload []byte) error {
	if s.secret == "" || s.akID == "" {
		return fmt.Errorf("对象存储凭证不完整（AccessKey 缺失）")
	}
	now := time.Now().UTC()
	bodyHash := req.Header.Get("X-Amz-Content-Sha256")
	if bodyHash == "" {
		bodyHash = emptySHA256Hex
		if len(payload) > 0 {
			bodyHash = sha256Hex(payload)
		}
	}
	req.Header.Set("X-Amz-Content-Sha256", bodyHash)
	req.Header.Set("X-Amz-Date", amzDate(now))

	canonHeaders, signedHeaders := sigv4Headers(req)
	canonicalRequest := strings.Join([]string{
		req.Method,
		sigv4CanonicalURI(resource),
		newSortedKV(signQuery(req.URL.RawQuery)).encode(),
		canonHeaders,
		signedHeaders,
		bodyHash,
	}, "\n")
	scope := ymdDate(now) + "/" + s.region + "/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate(now),
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	sig := hmacHash(sha256.New, string(s.signingKey(ymdDate(now))), stringToSign)
	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		s.akID, scope, signedHeaders, sig))
	return nil
}

// presign 生成 X-Amz-* 系列查询参数签名的临时下载 URL。
func (s *s3Signer) presign(method, resource string, expiry time.Duration) (string, error) {
	if s.secret == "" || s.akID == "" {
		return "", fmt.Errorf("对象存储凭证不完整（AccessKey 缺失）")
	}
	if expiry <= 0 {
		expiry = 24 * time.Hour
	}
	now := time.Now().UTC()
	scope := ymdDate(now) + "/" + s.region + "/s3/aws4_request"
	pairs := [][2]string{
		{"X-Amz-Algorithm", "AWS4-HMAC-SHA256"},
		{"X-Amz-Credential", s.akID + "/" + scope},
		{"X-Amz-Date", amzDate(now)},
		{"X-Amz-Expires", fmt.Sprintf("%d", int(expiry.Seconds()))},
		{"X-Amz-SignedHeaders", "host"},
	}
	kv := newSortedKV(pairs)
	canonicalRequest := strings.Join([]string{
		strings.ToUpper(method),
		sigv4CanonicalURI(resource),
		kv.encode(),
		"host:" + s.host + "\n",
		"host",
		unsignedPayload, // 预签名 URL 不传正文哈希，这一行是字面量（见 const 注释）
	}, "\n")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate(now),
		scope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	sig := hmacHash(sha256.New, string(s.signingKey(ymdDate(now))), stringToSign)
	q := url.Values{}
	for _, p := range kv.items {
		q.Set(p[0], p[1]) // 存的是原始值，Encode 会按 SigV4 要求把 credential 的 `/` 转成 %2F
	}
	q.Set("X-Amz-Signature", sig)
	return osBase(s.host) + uriPathFromResource(resource) + "?" + q.Encode(), nil
}

/* ============================ 共用辅助 ============================ */

// uriPathFromResource 把 /<bucket>/<key> 转成 URL path（各段按 OSS/S3 习惯编码，
// 保留 `/` 作为层级分隔，并保留目录 key 的尾斜杠）。
func uriPathFromResource(resource string) string {
	segs := strings.Split(resource, "/")
	out := make([]string, 0, len(segs))
	for _, s := range segs {
		if s == "" {
			continue
		}
		out = append(out, uriEncodeSegment(s))
	}
	p := "/" + strings.Join(out, "/")
	if strings.HasSuffix(resource, "/") { // 目录占位对象靠尾斜杠区分，不能丢
		p += "/"
	}
	return p
}

// uriEncodeSegment 用 PathEscape 编码单个路径段（空格转 %20 而非 +）。
func uriEncodeSegment(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// md5Bytes 正文的 MD5 摘要（OSS 的 Content-MD5 原料）。
func md5Bytes(bs []byte) []byte {
	sum := md5.Sum(bs)
	return sum[:]
}
