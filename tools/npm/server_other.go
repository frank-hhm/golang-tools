//go:build !windows

package npm

import (
	"os"
	"os/exec"
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

// openURL 用系统默认浏览器打开地址
func openURL(url string) error {
	return exec.Command("xdg-open", url).Start()
}
