//go:build windows

package mysql

import "syscall"

// hideWindowAttr 以隐藏窗口方式拉起 mysqld 子进程（后台服务无黑窗）
func hideWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}
