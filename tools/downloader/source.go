package downloader

import (
	"context"
	"io"
	"sort"
	"sync"
)

// SourceResolver 一个「外部流源」解析器：把一笔任务携带的不透明参数（params）变成一条
// 只读字节流。下载器引擎本身只认 HTTP URL，云盘这类「只有 SDK、拿不到可 Range 的直链」
// 的源，就靠实现本接口并 RegisterSource 挂进来，复用引擎的队列 / 进度 / 取消 / 持久化。
//
// 约定：
//   - size 未知时返回 -1，搬运侧按「读到 EOF 为止」处理；
//   - 返回的 ReadCloser 由引擎负责 Close；
//   - Open 应尊重 ctx 取消（拉起的网络请求要带 ctx），否则暂停 / 停止无法中断。
//
// 之所以传 map[string]string 而不是具体类型：引擎不能反向依赖任何一个消费者（如云盘），
// params 对引擎是黑盒，只有对应 kind 的实现知道它的字段含义。
type SourceResolver interface {
	Open(ctx context.Context, params map[string]string) (rc io.ReadCloser, size int64, err error)
}

var (
	sourceMu       sync.RWMutex
	sourceRegistry = map[string]SourceResolver{}
)

// RegisterSource 登记一类外部流源。kind 是任务里 SourceKind 字段取值（如 "cloud"），
// 重复登记同一 kind 会覆盖——供各消费者在 init / 启动时幂等地挂自己那套。
func RegisterSource(kind string, r SourceResolver) {
	if kind == "" || r == nil {
		return
	}
	sourceMu.Lock()
	sourceRegistry[kind] = r
	sourceMu.Unlock()
}

// lookupSource 取某 kind 的解析器；ok=false 表示没登记（任务会在入队时被拒）。
func lookupSource(kind string) (SourceResolver, bool) {
	sourceMu.RLock()
	r, ok := sourceRegistry[kind]
	sourceMu.RUnlock()
	return r, ok
}

// sortedParamKeys 把 params 的键排序，供拼稳定的 ID 种子（map 遍历无序，不排会每次不同）。
func sortedParamKeys(params map[string]string) []string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
