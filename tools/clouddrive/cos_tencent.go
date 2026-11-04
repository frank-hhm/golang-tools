package clouddrive

// 腾讯云 COS（对象存储）适配器（计划-云盘对象存储对接 PR-A）。
//
// COS 在协议层就是 S3 兼容 + 一点自家头（x-cos-*），签名直接用 AWS SigV4
// （service=s3、region 如 ap-guangzhou），因此与通用 S3 共用 objectstore.go 基座
// 与 s3Signer，只差 endpoint 默认值与 region 推导。
//
// 用户「粘贴式」填的凭证两栏对应控制台「访问管理 → API 密钥管理」的 SecretId /
// SecretKey。跨平台零 cgo、无 build tag。

import (
	"regexp"
	"strings"
)

// cosRegionFromHost 从 endpoint 反解 region：兼容 `cos.ap-guangzhou.myqcloud.com`
// 与带 bucket 前缀的 `<bucket>.cos.ap-guangzhou.myqcloud.com` 两种写法。
var cosRegionFromHost = regexp.MustCompile(`cos\.([a-z0-9-]+)\.myqcloud\.com`)

func init() {
	register(KindTencentCOS, func(acctID string) (Provider, error) {
		a, b, err := loadObjectStoreConfig(acctID)
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(a.Region) == "" {
			if m := cosRegionFromHost.FindStringSubmatch(a.Endpoint); m != nil {
				a.Region = m[1]
			}
		}
		return newObjectStoreAcct(KindTencentCOS, "腾讯云 COS", acctID, a, b)
	})
}
