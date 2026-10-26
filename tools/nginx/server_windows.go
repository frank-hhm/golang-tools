//go:build windows

package nginx

import (
	"os/exec"
	"strconv"
	"syscall"
)

// 平台事实：Windows 只认官方绿色版 zip（nginx.exe + conf/），
// 没有系统包管理器这条路。
const binFileName = "nginx.exe"
const systemPackaged = false

var systemPkgNames []string

// systemConfDir Windows 无系统配置目录，conf 始终在 nginx.exe 同级
const systemConfDir = ""

// hideWindowAttr 隐藏控制台窗口（nginx.exe 启动时会开控制台，不隐藏会闪黑框）
func hideWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}

// preparePrefixDirs Windows 无降权 worker（nginx.exe 以当前用户身份跑），临时目录由它自己建。
func preparePrefixDirs(string) error { return nil }

// killTree 结束进程树（master + worker）
func killTree(pid int) error {
	cmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Run()
}
