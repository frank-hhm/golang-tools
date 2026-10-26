//go:build windows

package mysql

import "syscall"

// binFileName 可执行文件名（Windows 绿色版带 .exe）。
const binFileName = "mysqld.exe"

// systemPackaged Windows 上走自带的绿色版 zip，不用系统包管理器。
const systemPackaged = false

// systemPkgNames Windows 无系统包候选。
var systemPkgNames []string

// runAsUserArgs Windows 没有"以某个账户运行"的等价参数。
func runAsUserArgs() []string { return nil }

// prepareInstanceDir Windows 不需要转交目录归属。
func prepareInstanceDir(dir string) error { return nil }

// initFailHint Windows 没有 AppArmor 这一层，失败原因已在 mysqld 原文里。
func initFailHint(string) string { return "" }

// myIniPlatformLines Windows 的 MySQL 不靠 unix socket 通信（面板走 TCP），
// 实例之间不会互相抢全局 socket 路径，不需要额外写这两行。
func myIniPlatformLines(string) string { return "" }

// hideWindowAttr 以隐藏窗口方式拉起 mysqld 子进程（后台服务无黑框）
func hideWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}
