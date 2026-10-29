//go:build !windows

package guard

import (
	"os/exec"
	"syscall"
)

// applySysAttr 非 Windows：让子进程进入独立进程组，便于按组整体终止。
func applySysAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// killTree 结束整个进程组（负 PID = 组）；失败回落杀单进程。
func killTree(pid int) error {
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		return syscall.Kill(pid, syscall.SIGKILL)
	}
	return nil
}
