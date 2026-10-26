//go:build !windows

package ftp

// 非 Windows 平台没有可代劳的本地防火墙机制：容器/云主机通常在宿主机或
// 安全组层面放行，程序里改不了，这里只把该说的话说清楚。

import "fmt"

// AllowFirewall 提示用户自行放行端口
func AllowFirewall(port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("端口非法: %d", port)
	}
	srv.logf(LogWarn, "请在系统防火墙/云安全组放行 TCP %d", port)
	return fmt.Errorf("当前平台无法自动放行端口，请在系统防火墙或云安全组放行 TCP %d", port)
}

// RemoveFirewallRule 非 Windows 无对应动作
func RemoveFirewallRule(port int) error {
	return fmt.Errorf("当前平台无需删除程序防火墙规则（TCP %d）", port)
}
