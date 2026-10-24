//go:build !windows

package nginx

import (
	"os"
	"syscall"
)

// hideWindowAttr 非 Windows 无控制台窗口问题
func hideWindowAttr() *syscall.SysProcAttr { return nil }

// killTree 结束进程（POSIX 下直接 Kill）
func killTree(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
