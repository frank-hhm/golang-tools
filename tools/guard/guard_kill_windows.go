//go:build windows

package guard

import (
	"os/exec"
	"strconv"
	"syscall"
)

// applySysAttr Windows：隐藏子进程控制台窗口，避免守护时闪黑框。
func applySysAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// killTree 结束进程树（含其派生的子进程）：taskkill /F /T /PID。
func killTree(pid int) error {
	c := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return c.Run()
}
