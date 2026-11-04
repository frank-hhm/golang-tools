package clouddrive

// 阿里云 OSS 对象存储适配器（计划-云盘对象存储对接 PR-A）。
//
// 与「阿里云盘」（aliyun.go，个人网盘）是两件东西：这里走的是 OSS 的 REST + XML
// 接口，签名用 OSS 原生 V1（HMAC-SHA1），凭证是 AccessKey ID/Secret（在控制台
// 「对象存储 → 访问控制」里拿）。bucket / endpoint / region 属非敏感配置存账号表，
// AK/SK 走 tools/secret 加密落盘、永不回前端。
//
// region 缺失时从 endpoint（`oss-cn-hangzhou.aliyuncs.com` 或带 bucket 前缀的
// `<bucket>.oss-cn-hangzhou.aliyuncs.com`）反解。跨平台零 cgo、无 build tag；
// 具体读写逻辑全在 objectstore.go 基座，本文件只补齐配置并登记。

import "regexp"

// ossRegionFromHost 从 endpoint 反解 OSS region。
var ossRegionFromHost = regexp.MustCompile(`oss-([a-z0-9-]+)\.aliyuncs\.com`)

func init() {
	register(KindAliyunOSS, func(acctID string) (Provider, error) {
		a, b, err := loadObjectStoreConfig(acctID)
		if err != nil {
			return nil, err
		}
		if a.Region == "" {
			if m := ossRegionFromHost.FindStringSubmatch(a.Endpoint); m != nil {
				a.Region = m[1]
			}
		}
		return newObjectStoreAcct(KindAliyunOSS, "阿里云 OSS", acctID, a, b)
	})
}
