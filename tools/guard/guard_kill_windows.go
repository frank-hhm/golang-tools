//go:build windows

package guard

import (
	"os/exec"
	"strconv"
	"syscall"
)

// createNoWindow = Win32 CREATE_NO_WINDOW：GUI 子系统构建里 HideWindow(SW_HIDE)
// 藏不住 Win11 的 Windows Terminal，只有不分配控制台才不闪窗。
const createNoWindow = 0x08000000

// applySysAttr Windows：隐藏子进程控制台窗口，避免守护时闪黑框。
func applySysAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// killTree 结束进程树（含其派生的子进程）：taskkill /F /T /PID。
func killTree(pid int) error {
	c := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	c.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	return c.Run()
}
