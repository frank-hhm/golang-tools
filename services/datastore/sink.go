package datastore

import (
	"context"
	"encoding/json"
)

// DocMeta 远端替一份文档记的元数据。digest 是**本地明文内容**的 sha256——加密外存时
// payload 是密文，可比对的只有明文摘要，恢复时要靠它判断本地是不是已经一样了。
type DocMeta struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"sizeBytes"` // 本地明文大小
	Encrypted bool   `json:"encrypted"`
	SyncedAt  int64  `json:"syncedAt"` // UnixMilli
}

// Sink 一个外存位置。契约只有五条，MySQL 与 Redis 各给一种实现，单测再给一种内存实现：
//   - Prepare：探活并保证库/表/键空间可用。失败就等于"这个位置今天不可用"，
//     引擎会把它记成不可达并推迟重试，而不是每条文档都撞一次墙。
//   - Put：写一份文档。同 key 覆盖（副本语义里没有"多版本"，历史交给备份工具）。
//   - Fetch：取回内容；远端没有这份时返回 (nil, nil, nil)，不报错。
//   - Metas：列出远端已有文档的元数据，用于"远端有几份 / 谁比谁新"。
//   - Close：释放连接。
//
// 实现必须可并发调用：引擎的定时扫描、手动「立即同步」、退出前 flush 可能同时命中。
type Sink interface {
	Kind() string
	Prepare(ctx context.Context) error
	Put(ctx context.Context, key string, payload []byte, meta DocMeta) error
	Fetch(ctx context.Context, key string) ([]byte, *DocMeta, error)
	Metas(ctx context.Context) (map[string]DocMeta, error)
	Close() error
}

// metaJSON 是 Redis 侧把 DocMeta 塞进 hash value 的编码方式；MySQL 侧不需要（拆列存）。
func metaJSON(m DocMeta) string {
	b, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func metaFromJSON(s string) (DocMeta, bool) {
	var m DocMeta
	if s == "" || json.Unmarshal([]byte(s), &m) != nil {
		return m, false
	}
	return m, true
}
