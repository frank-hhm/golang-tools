//go:build windows

package php

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// hideWindowAttr 隐藏控制台窗口（php.exe 是控制台程序，不隐藏会闪黑框）
func hideWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}

// killTree 结束进程树（php -S 为单进程，/T 兼容可能派生的子进程）
func killTree(pid int) error {
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}

// findPidsByName 按镜像名查找进程 PID（tasklist 输出解析）
func findPidsByName(name string) []int {
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq "+name, "/FO", "CSV", "/NH")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	var pids []int
	lower := strings.ToLower(name)
	for _, line := range strings.Split(string(out), "\n") {
		cols := strings.Split(strings.TrimSpace(line), `","`)
		if len(cols) < 2 || strings.ToLower(strings.Trim(cols[0], `"`)) != lower {
			continue
		}
		if pid, err := strconv.Atoi(strings.Trim(cols[1], `"`)); err == nil && pid > 0 {
			pids = append(pids, pid)
		}
	}
	return pids
}
