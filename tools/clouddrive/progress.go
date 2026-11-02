package clouddrive

// 传输进度事件（计划-云盘管理工具 PR3/PR4）。
//
// emitter 与 downloader / lanshare / archive 同形：main 里 SetEmitter(emit) 注入，
// 负载的 Op/Name/Stage/Percent/Note 正好是 services/tasks.payload 认得的键，
// 于是「用户离开云盘页」也能在任务中心看到转存/下载/上传进度，终态还能弹通知。
//
// 与 lanshare 的区别只在事件名：本包用 cloud:progress，一条事件按 Op 分三类
// （transfer 云→云转存 / download 云→本机 / upload 本机→云）。

import (
	"sync"
)

// Progress 一次云传输的进度事件负载。
type Progress struct {
	Op      string `json:"op"`      // transfer / download / upload
	ID      string `json:"id"`      // 传输 id（transfer 可用于取消；download/upload 与任务中心同名）
	Name    string `json:"name"`    // 条目名（任务中心据此并行为一行）
	Stage   string `json:"stage"`   // running / done / error / canceled
	Percent int    `json:"percent"` // 0~100；总大小未知时为 0（前端画不定进度）
	Note    string `json:"note"`
}

// 传输类型。
const (
	OpTransfer = "transfer" // 云→云跨盘转存
	OpDownload = "download" // 云→本机
	OpUpload   = "upload"   // 本机→云
)

// 传输相位（与任务中心的 Stage 口径一致）。
const (
	StageRunning  = "running"
	StageDone     = "done"
	StageError    = "error"
	StageCanceled = "canceled"
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

// emitProgress 发一条 cloud:progress。emitter 未注入（单测 / 未接线）时静默丢弃。
func emitProgress(p Progress) {
	emitMu.RLock()
	em := emitter
	emitMu.RUnlock()
	if em != nil {
		em("cloud:progress", p)
	}
}
