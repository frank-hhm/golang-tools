package app

import "go-tools/services/tasks"

// ============================================================
// 任务清单的查询与清理（对前端暴露）
//
// 安装任务跑在后端 goroutine 里，页面只是"查看器"：切到设置页、关掉工具
// 窗口、甚至在另一个窗口打开，都靠这几个方法把那边的账本取回来。
// 方法必须在两个平台都存在（见 AGENTS.md「绑定方法不能藏在 build tag 后面」）。
// ============================================================

// TaskList 返回安装/下载任务的快照：进行中的在最前，其次是最近结束的。
func (a *App) TaskList() []tasks.Task {
	return tasks.Snapshot()
}

// TaskClear 清掉一条已结束的任务（进行中的不允许删，还在写）。
func (a *App) TaskClear(id string) {
	tasks.Clear(id)
}

// TaskClearFinished 一次清掉所有已结束的任务。
func (a *App) TaskClearFinished() {
	tasks.ClearFinished()
}
