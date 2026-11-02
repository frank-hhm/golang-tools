package gitcmd

import "sync"

// 推送/拉取/抓取是唯一需要跨页可见的长操作（用户可能直接离开工作台），进度经
// git:progress 事件上报，与 sshgen / lanshare / clouddrive 同构：main 里
// SetEmitter(emit) 注入，负载的 Op/Name/Stage/Percent/Note 正是
// services/tasks.payload 认得的键，任务中心能看到「推送 repo-name」的进度与终态。

// Progress 一次长操作的进度事件负载。
type Progress struct {
	Op      string `json:"op"`      // push / pull / fetch
	Name    string `json:"name"`    // 仓库名（登记表里的展示名）
	Stage   string `json:"stage"`   // running / done / error
	Percent int    `json:"percent"` // 0~100；拿不到百分比时保持上一次值
	Note    string `json:"note"`    // 最近一句人话（git 进度行原文）
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

// emitProgress 发一条 git:progress。emitter 未注入（单测 / 未接线）时静默丢弃。
func emitProgress(p Progress) {
	emitMu.RLock()
	em := emitter
	emitMu.RUnlock()
	if em != nil {
		em("git:progress", p)
	}
}
