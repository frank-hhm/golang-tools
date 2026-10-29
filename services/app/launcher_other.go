//go:build !windows

package app

import (
	"Tools/services/sysx"
)

// lockWorkstation 非 Windows 没有"锁定工作站"这一层：锁屏归桌面环境
// （gnome-screensaver / loginctl lock-session 之类），而本程序在 Linux 上跑的是
// 无头面板，根本没有可锁的本机会话。
//
// 命令面板在 server 构建里整体不挂载（见 launcherMounted），正常路径走不到这里；
// 保留这个方法只为让无标签的 launcher.go 跨平台编过，并按 AGENTS 的约定返回
// ErrUnsupported 而不是让绑定方法消失。
func lockWorkstation() error {
	return sysx.ErrUnsupported
}
