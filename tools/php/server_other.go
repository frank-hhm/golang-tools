//go:build !windows

package php

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

// findPidsByName 非 Windows 不做镜像名兜底
func findPidsByName(name string) []int { return nil }
