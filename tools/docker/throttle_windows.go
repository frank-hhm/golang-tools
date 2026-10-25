//go:build windows

package docker

// ============================================================
// 引擎访问闸门：并发上限 + 短缓存 + 失败熔断
//
// 背景：面板里多个页面（Docker 管理、PHP/MySQL 实例面板）各自定时轮询，
// 而 Engine API 走命名管道，每次请求都要占一个阻塞的 Win32 IO。引擎卡住时
// （起容器、拉镜像、Desktop 切后端都会让 daemon 短暂不应答），每一路轮询都会
// 挂到「等待响应头」超时上：轮询定时器照旧触发，请求层层叠加，最后几百个
// 请求同时阻塞在管道上，把 Docker Desktop 与本进程一起拖到 CPU 100%。
//
// 这里做三件事：
//  1. 并发闸门：同时在飞的 Engine API 请求不超过 apiMaxConcurrent 个；排不上队
//     直接返回「引擎响应缓慢」，不排队、不堆栈（宁可这一帧没数据，也不雪崩）；
//  2. 短缓存：列表/状态/日志在 1 秒级窗口内复用同一份结果，多页面共享一次请求；
//  3. 熔断：请求超时或连不上后安静 apiCooldown，期间读接口直接给上一条缓存
//     （≤ apiStaleMax），不再反复去拨一个打不通的管道。
// ============================================================

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// apiMaxConcurrent 同时在飞的 Engine API 请求上限：命名管道单条连接一次
	// 只能跑一个请求，开太多只会让 daemon 与本进程一起排队
	apiMaxConcurrent = 6
	// apiQueueWait 排队等名额的最长时间，超过即判引擎拥堵
	apiQueueWait = 1200 * time.Millisecond
	// apiCooldown 失败后的静默窗口：期间读接口不再发请求
	apiCooldown = 3 * time.Second
	// apiStaleMax 熔断/失败时允许回落的旧数据最大年龄
	apiStaleMax = 15 * time.Second
)

// errEngineBusy 闸门排队超时：引擎已堵住，本次直接跳过
var errEngineBusy = errors.New("Docker 引擎响应缓慢（请求排队超时），本次读取已跳过；引擎恢复后自动继续")

var (
	apiSlots = make(chan struct{}, apiMaxConcurrent)

	apiDownMu  sync.Mutex
	apiDownAt  time.Time
	apiDownMsg string
)

// apiEnter 取一个请求名额，返回释放函数；排队超时视为引擎拥堵并顺带熔断
func apiEnter() (func(), error) {
	select {
	case apiSlots <- struct{}{}:
		return func() { <-apiSlots }, nil
	default:
	}
	timer := time.NewTimer(apiQueueWait)
	defer timer.Stop()
	select {
	case apiSlots <- struct{}{}:
		return func() { <-apiSlots }, nil
	case <-timer.C:
		apiTrip(errEngineBusy)
		return nil, errEngineBusy
	}
}

// apiTrip 记录一次传输层失败（超时/连不上），进入静默窗口
func apiTrip(err error) {
	if err == nil {
		return
	}
	apiDownMu.Lock()
	apiDownAt = time.Now()
	apiDownMsg = err.Error()
	apiDownMu.Unlock()
}

// apiHeal 引擎又有响应了，清掉熔断状态
func apiHeal() {
	apiDownMu.Lock()
	apiDownAt = time.Time{}
	apiDownMsg = ""
	apiDownMu.Unlock()
}

// apiDown 当前熔断剩余时间与最后一次失败原因
func apiDown() (time.Duration, string) {
	apiDownMu.Lock()
	defer apiDownMu.Unlock()
	if apiDownAt.IsZero() {
		return 0, ""
	}
	if left := apiCooldown - time.Since(apiDownAt); left > 0 {
		return left, apiDownMsg
	}
	return 0, ""
}

/* ============================ 读缓存 ============================ */

// memoCell 单个 key 的缓存；gen 与全局代数不一致即视为陈旧（写操作后自动作废）
type memoCell struct {
	mu  sync.Mutex
	gen uint64
	at  time.Time
	val any
}

var (
	memoMu    sync.Mutex
	memoCells = map[string]*memoCell{}
	memoGen   atomic.Uint64
)

func memoCellOf(key string) *memoCell {
	memoMu.Lock()
	defer memoMu.Unlock()
	c := memoCells[key]
	if c == nil {
		c = &memoCell{}
		memoCells[key] = c
	}
	return c
}

// apiInvalidate 作废全部读缓存（任何写操作之后调用）
func apiInvalidate() { memoGen.Add(1) }

// memoRead 读接口统一入口，语义：
//   - ttl 内的同一 key 直接复用（并发调用被下面的互斥串行化，天然合并成一次）；
//   - 引擎处于熔断窗口时不再发请求，回落旧值或给可读错误；
//   - 请求失败但手里还有 apiStaleMax 内的旧值，静默返回旧值（面板不刷红字）。
func memoRead[T any](key string, ttl time.Duration, fn func() (T, error)) (T, error) {
	var zero T
	cell := memoCellOf(key)
	gen := memoGen.Load()

	cell.mu.Lock()
	defer cell.mu.Unlock()

	fresh := func() (T, bool) {
		if cell.at.IsZero() || cell.gen != gen {
			return zero, false
		}
		v, ok := cell.val.(T)
		return v, ok
	}
	if v, ok := fresh(); ok && time.Since(cell.at) < ttl {
		return v, nil
	}
	if left, msg := apiDown(); left > 0 {
		if v, ok := fresh(); ok && time.Since(cell.at) < apiStaleMax {
			return v, nil
		}
		return zero, fmt.Errorf("Docker 引擎暂无响应（%.0f 秒后自动重试）：%s", left.Seconds(), msg)
	}
	v, err := fn()
	if err != nil {
		if old, ok := fresh(); ok && time.Since(cell.at) < apiStaleMax {
			return old, nil
		}
		return zero, err
	}
	// 代数沿用进入时的快照：期间若有写操作，这份结果下次自然按陈旧处理
	cell.gen, cell.at, cell.val = gen, time.Now(), v
	return v, nil
}
