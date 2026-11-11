package datastore

import (
	"context"
	"fmt"
	"sync"
	"time"

	"Tools/tools/redis"
)

// redisPrefix 键前缀。整份外存都挂在这一个前缀下，用户在 Redis 页里一眼能认出
// 这批键是谁写的、也能按前缀单独清掉。
const redisPrefix = "Tools:data:"

// redisMetaKey 元数据哈希：field=文档键，value=DocMeta 的 JSON。
// 内容另存独立字符串键（Tools:data:<key>），不把 payload 塞进 hash——hash 里塞大值
// 会让 HGETALL 变成一次全量搬运，而"远端有几份、各自摘要"这个查询本该是轻量的。
const redisMetaKey = redisPrefix + "__meta__"

// redisSink 把文档副本写进本机某个 Redis 实例的一个库。
//
// 连接借 tools/redis 自己的连接池（id 固定 datastore:<实例 id>）：那边已经把
// "实例被停 → socket 复位 → 连接踢出池"这套处理好了，这里不必再造一遍死连接。
type redisSink struct {
	instID string
	db     int

	mu       sync.Mutex
	openedAt time.Time
}

func newRedisSink(instID string, db int) *redisSink {
	return &redisSink{instID: instID, db: db}
}

func (s *redisSink) Kind() string { return "redis" }

// Prepare 建（或重建）连接。Redis 没有"建库"这一步：SELECT n 就是切库，
// 库序号由实例配置决定上限（默认 16），越界在设置层已经回落成 0。
func (s *redisSink) Prepare(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.openedAt.IsZero() && time.Since(s.openedAt) < 15*time.Second {
		return nil
	}
	cfg, err := redis.InstanceConnConfig(s.instID)
	if err != nil {
		return err
	}
	cfg.DB = s.db
	if err := redis.Open(cfg); err != nil {
		return fmt.Errorf("Redis 实例无响应（服务没启动还是密码不对？）: %w", err)
	}
	s.openedAt = time.Now()
	return nil
}

func (s *redisSink) Put(ctx context.Context, key string, payload []byte, meta DocMeta) error {
	if err := s.Prepare(ctx); err != nil {
		return err
	}
	if _, err := s.do("SET", redisPrefix+key, string(payload)); err != nil {
		return fmt.Errorf("写入 %s 失败: %w", key, err)
	}
	if _, err := s.do("HSET", redisMetaKey, key, metaJSON(meta)); err != nil {
		return fmt.Errorf("写入 %s 的元数据失败: %w", key, err)
	}
	return nil
}

func (s *redisSink) Fetch(ctx context.Context, key string) ([]byte, *DocMeta, error) {
	if err := s.Prepare(ctx); err != nil {
		return nil, nil, err
	}
	raw, err := s.do("GET", redisPrefix+key)
	if err != nil {
		return nil, nil, fmt.Errorf("读取 %s 失败: %w", key, err)
	}
	payload, ok := replyString(raw)
	if !ok {
		return nil, nil, nil // 批量 nil 回复 = 远端还没这一份
	}
	mv, _ := s.do("HGET", redisMetaKey, key)
	m, parsed := metaFromJSON(stringOfReply(mv))
	if !parsed {
		return []byte(payload), nil, nil
	}
	return []byte(payload), &m, nil
}

func (s *redisSink) Metas(ctx context.Context) (map[string]DocMeta, error) {
	if err := s.Prepare(ctx); err != nil {
		return nil, err
	}
	raw, err := s.do("HGETALL", redisMetaKey)
	if err != nil {
		return nil, err
	}
	out := map[string]DocMeta{}
	// RESP 的 HGETALL 回复是 [field, value, field, value, ...] 的扁平数组
	arr, ok := raw.([]any)
	if !ok {
		return out, nil
	}
	for i := 0; i+1 < len(arr); i += 2 {
		k := stringOfReply(arr[i])
		if k == "" {
			continue
		}
		if m, ok := metaFromJSON(stringOfReply(arr[i+1])); ok {
			out[k] = m
		}
	}
	return out, nil
}

// ListDBSize 报告当前库的键数，给设置页一句"这个库里现在有多少键"。
func (s *redisSink) ListDBSize(ctx context.Context) (int64, error) {
	if err := s.Prepare(ctx); err != nil {
		return 0, err
	}
	raw, err := s.do("DBSIZE")
	if err != nil {
		return 0, err
	}
	n, _ := raw.(int64)
	return n, nil
}

func (s *redisSink) Close() error {
	s.mu.Lock()
	s.openedAt = time.Time{}
	s.mu.Unlock()
	cfg, err := redis.InstanceConnConfig(s.instID)
	if err != nil {
		return nil
	}
	return redis.Close(cfg.ID)
}

func (s *redisSink) do(args ...string) (any, error) {
	cfg, err := redis.InstanceConnConfig(s.instID)
	if err != nil {
		return nil, err
	}
	return redis.Command(cfg.ID, args...)
}

// replyString 批量字符串回复 → (内容, 是否有值)。nil 回复表示键不存在。
func replyString(v any) (string, bool) {
	switch t := v.(type) {
	case nil:
		return "", false
	case string:
		return t, true
	case int64:
		return fmt.Sprint(t), true
	default:
		return fmt.Sprint(t), true
	}
}

func stringOfReply(v any) string {
	s, _ := replyString(v)
	return s
}
