//go:build windows

package redis

import "syscall"

// hideWindowAttr 以隐藏窗口方式拉起 redis-server 子进程（后台服务无黑窗）
func hideWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}
