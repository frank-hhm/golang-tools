package clouddrive

// 通用 S3 兼容对象存储适配器（计划-云盘对象存储对接 PR-A）。
//
// 覆盖 MinIO / 华为 OBS / 七牛 Kodo / AWS S3 等一切讲 S3 协议的服务：签名用
// AWS SigV4（与腾讯云 COS 同一个 s3Signer），endpoint 由用户自填（可以带
// scheme，也可以填 `IP:端口` 形式的自建服务）。region 留空时按 SigV4 惯例
// 回落 `us-east-1`（多数自建服务接受任意值；AWS 真服务请按实际填写）。
//
// 跨平台零 cgo、无 build tag；读写逻辑全在 objectstore.go 基座，本文件只登记。

func init() {
	register(KindS3, func(acctID string) (Provider, error) {
		a, b, err := loadObjectStoreConfig(acctID)
		if err != nil {
			return nil, err
		}
		return newObjectStoreAcct(KindS3, "S3 兼容存储", acctID, a, b)
	})
}
