package app

import (
	"Tools/tools/portman"
	"Tools/tools/procman"
)

// 端口管理面板的绑定层：入口是首页「网络」卡片，不注册为工具项。
// 列表由前端轮询 PortSnapshot；结束进程直接复用 ProcKill（procman 里已含非管理员
// 的一次 UAC 提权兜底），图标/定位/在线搜索走下面三个「按 exe」的方法——
// 端口面板手里只有路径，没有 procman 的进程快照，用不了 pid 版那几个。

// PortSnapshot 一次本机端口表（TCP/UDP 监听 + 已连接，带归属进程）。
func (a *App) PortSnapshot() (portman.Snapshot, error) { return portman.SnapshotPorts() }

// PortIcon 按 exe 路径取图标 PNG dataURL；取不到返回空串。
func (a *App) PortIcon(exe string) string { return procman.IconOf(exe) }

// PortOpenPath 在系统文件管理器中定位该 exe。
func (a *App) PortOpenPath(exe string) error { return procman.OpenExe(exe) }

// PortOpenVt 按 exe 的 SHA256 用系统浏览器打开 VirusTotal 文件页。
func (a *App) PortOpenVt(exe string) error { return procman.OpenVtOf(exe) }
