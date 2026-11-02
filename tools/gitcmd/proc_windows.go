//go:build windows

package gitcmd

import (
	"os/exec"
	"syscall"
)

// createNoWindow = Win32 CREATE_NO_WINDOW：GUI 子系统构建里 HideWindow(SW_HIDE)
// 藏不住 Win11 的 Windows Terminal，只有根本不分配控制台才不闪窗。git 状态刷新
// 会频繁起子进程，漏了这行就每次刷一下黑框。
const createNoWindow = 0x08000000

// applyHidden 给 git 子进程设不弹控制台的系统属性。
func applyHidden(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
