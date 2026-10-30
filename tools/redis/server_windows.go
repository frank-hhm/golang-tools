//go:build windows

package redis

import "syscall"

// binFileName 可执行文件名（Windows 绿色版带 .exe）。
const binFileName = "redis-server.exe"

// systemPackaged Windows 上没有"系统包"这回事，版本走自带的 msys2 构建 zip。
const systemPackaged = false

// systemPkgNames Windows 无系统包候选。
var systemPkgNames []string

// createNoWindow = Win32 CREATE_NO_WINDOW：GUI 子系统构建里 HideWindow(SW_HIDE)
// 藏不住 Win11 的 Windows Terminal，只有不分配控制台才不闪窗。
const createNoWindow = 0x08000000

// hideWindowAttr 以隐藏窗口方式拉起 redis-server 子进程（后台服务无黑框）
func hideWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
