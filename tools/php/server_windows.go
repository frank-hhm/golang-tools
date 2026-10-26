//go:build windows

package php

import (
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// binFileName 可执行文件名（Windows 绿色版带 .exe）。
const binFileName = "php.exe"

// systemPackaged Windows 上走自带的绿色版 zip。
const systemPackaged = false

// systemPkgNames Windows 无系统包候选。
var systemPkgNames []string

// perBinaryIni Windows 绿色版的 php.ini 与 php.exe 同级（PHP 默认加载位置）。
const perBinaryIni = true

// rewriteExtDir Windows 绿色版的扩展放在 <php>/ext，必须由面板指过去。
const rewriteExtDir = true

// iniTemplateCandidates php.ini 模板就在 php.exe 同级目录。
func iniTemplateCandidates(phpDir string) []string {
	return []string{
		filepath.Join(phpDir, "php.ini-development"),
		filepath.Join(phpDir, "php.ini-production"),
		filepath.Join(phpDir, "php.ini"),
	}
}

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
