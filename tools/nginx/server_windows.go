//go:build windows

package nginx

import (
	"os/exec"
	"strconv"
	"syscall"
)

// hideWindowAttr 隐藏控制台窗口（nginx.exe 启动时会开控制台，不隐藏会闪黑框）
func hideWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}

// killTree 结束进程树（master + worker）
func killTree(pid int) error {
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}
