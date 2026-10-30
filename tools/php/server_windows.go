//go:build windows

package php

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// binFileName 可执行文件名（Windows 绿色版带 .exe）。
const binFileName = "php.exe"

// cgiFileName FastCGI 后端可执行文件名（官方 Windows 包与 php.exe 同级）。
const cgiFileName = "php-cgi.exe"

// systemPackaged Windows 上走自带的绿色版 zip。
const systemPackaged = false

// systemPkgNames Windows 无系统包候选。
var systemPkgNames []string

// perBinaryIni Windows 绿色版的 php.ini 与 php.exe 同级（PHP 默认加载位置）。
const perBinaryIni = true

// rewriteExtDir Windows 绿色版的扩展放在 <php>/ext，必须由面板指过去。
const rewriteExtDir = true

// extIniValue 决定写进 php.ini 的扩展加载值，并回答“这枚扩展在本机是否存在”。
//
// Windows 绿色版的扩展是 <php>/ext 下的 php_<name>.dll，写全文件名最稳；
// 找不到 dll 时返回 ok=false，由调用方决定是拒绝启用还是照常写名字。
func extIniValue(name, phpDir string) (string, bool) {
	dll := filepath.Join(phpDir, "ext", "php_"+name+".dll")
	if _, err := os.Stat(dll); err == nil {
		return "php_" + name + ".dll", true // 与官方 php.ini 注释行的写法保持一致
	}
	return name, false
}

// iniTemplateCandidates php.ini 模板就在 php.exe 同级目录。
func iniTemplateCandidates(phpDir string) []string {
	return []string{
		filepath.Join(phpDir, "php.ini-development"),
		filepath.Join(phpDir, "php.ini-production"),
		filepath.Join(phpDir, "php.ini"),
	}
}

// createNoWindow 是 Win32 CREATE_NO_WINDOW：GUI 子系统（-H windowsgui）构建里，
// 光靠 HideWindow(SW_HIDE) 藏不住子控制台——Win11 默认终端是 Windows Terminal，
// 它是独立进程、不吃子进程的 STARTUPINFO 显示标志，于是每次 spawn php.exe 都闪一个终端窗。
// CREATE_NO_WINDOW 让系统根本不分配控制台，从根上不产生那个窗口。
const createNoWindow = 0x08000000

// hideWindowAttr 隐藏控制台窗口（php.exe 是控制台程序，不隐藏会闪黑框）
func hideWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// killTree 结束进程树（php -S 为单进程，/T 兼容可能派生的子进程）
func killTree(pid int) error {
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = hideWindowAttr()
	return cmd.Run()
}

// findPidsByName 按镜像名查找进程 PID（tasklist 输出解析）
func findPidsByName(name string) []int {
	cmd := exec.Command("tasklist", "/FI", "IMAGENAME eq "+name, "/FO", "CSV", "/NH")
	cmd.SysProcAttr = hideWindowAttr()
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
