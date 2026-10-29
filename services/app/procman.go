package app

import (
	"Tools/tools/procman"
)

// 进程管理面板的绑定层：把 tools/procman 的能力暴露给前端（入口是首页 CPU 卡片）。
// 无事件推送：列表由前端轮询 ProcSnapshot，操作类调用同步返回。

// ProcSnapshot 一次全量进程快照 + 状态栏汇总。
func (a *App) ProcSnapshot() procman.Snapshot { return procman.SnapshotProcs() }

// ProcKill 结束进程（tree=true 连整棵子树）；Windows 下权限不足自动走一次 UAC 提权。
func (a *App) ProcKill(pid int, tree bool) error { return procman.Kill(pid, tree) }

// ProcSuspend 挂起（on=true）/ 恢复（on=false）进程。
func (a *App) ProcSuspend(pid int, on bool) error { return procman.Suspend(pid, on) }

// ProcRestart 以原路径与命令行重启进程。
func (a *App) ProcRestart(pid int) error { return procman.Restart(pid) }

// ProcSetPriority 调整优先级：high | normal | low。
func (a *App) ProcSetPriority(pid int, class string) error { return procman.SetPriority(pid, class) }

// ProcInfo 进程属性（命令行/用户/位数等重字段）。
func (a *App) ProcInfo(pid int) (procman.ProcInfo, error) { return procman.Info(pid) }

// ProcIcon 进程图标 PNG dataURL；取不到返回空串。
func (a *App) ProcIcon(pid int) string { return procman.Icon(pid) }

// ProcOpenPath 在文件管理器中定位进程 exe。
func (a *App) ProcOpenPath(pid int) error { return procman.OpenPath(pid) }

// ProcOpenVt 用系统浏览器打开该进程 exe 的 VirusTotal 文件页（按 SHA256）。
func (a *App) ProcOpenVt(pid int) error { return procman.OpenVt(pid) }
