//go:build windows

package npm

import (
	"os/exec"
	"strconv"
	"syscall"
)

// createNoWindow = Win32 CREATE_NO_WINDOW：GUI 子系统构建里 HideWindow(SW_HIDE)
// 藏不住 Win11 的 Windows Terminal（独立进程不吃子进程显示标志），只有不分配控制台才不闪窗。
const createNoWindow = 0x08000000

// hideWindowAttr 隐藏控制台窗口（node.exe 是控制台程序）
func hideWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// killTree 结束进程树
func killTree(pid int) error {
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = hideWindowAttr()
	return cmd.Run()
}

// openURL 用系统默认浏览器打开地址
func openURL(url string) error {
	cmd := exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	cmd.SysProcAttr = hideWindowAttr()
	return cmd.Start()
}
