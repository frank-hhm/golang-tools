package sshgen

import "sync"

// 部署公钥是唯一的长操作（要连远端 + 读写授权文件），进度经 sshgen:progress 事件
// 上报，与 downloader / lanshare / clouddrive 同构：main 里 SetEmitter(emit) 注入，
// 负载的 Op/Name/Stage/Percent/Note 正是 services/tasks.payload 认得的键，
// 于是用户离开表单弹窗也能在任务中心看到「部署公钥 user@host」的进度与终态。

// Progress 一次部署的进度事件负载。
type Progress struct {
	Op      string `json:"op"`      // deploy
	Name    string `json:"name"`    // user@host
	Stage   string `json:"stage"`   // running / done / error
	Percent int    `json:"percent"` // 0~100
	Note    string `json:"note"`
}

// 相位。
const (
	StageRunning = "running"
	StageDone    = "done"
	StageError   = "error"
)

var (
	emitMu  sync.RWMutex
	emitter func(name string, data ...any)
)

// SetEmitter 注入事件发射器（与其余工具包一致，main 里统一接 tasks.Observe + 前端）。
func SetEmitter(fn func(name string, data ...any)) {
	emitMu.Lock()
	emitter = fn
	emitMu.Unlock()
}

// emitProgress 发一条 sshgen:progress。emitter 未注入（单测 / 未接线）时静默丢弃。
func emitProgress(p Progress) {
	emitMu.RLock()
	em := emitter
	emitMu.RUnlock()
	if em != nil {
		em("sshgen:progress", p)
	}
}
