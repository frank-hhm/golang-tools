//go:build windows

package ftp

// Windows 防火墙放行：由面板显式点「放行端口」触发，走 UAC 提权跑 netsh。
// 规则名带端口号，方便用户之后在防火墙面板里找到并删除同一条。

import (
	"fmt"

	"go-tools/services/winrun"
)

// firewallRuleName 规则显示名（删除时按同一个名字找）
func firewallRuleName(port int) string { return fmt.Sprintf("go-tools SFTP %d", port) }

// AllowFirewall 入站放行 TCP 端口
func AllowFirewall(port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("端口非法: %d", port)
	}
	params := fmt.Sprintf(
		`advfirewall firewall add rule name="%s" dir=in action=allow protocol=TCP localport=%d profile=any`,
		firewallRuleName(port), port)
	code, err := winrun.RunElevatedWait(winrun.Elevated{File: "netsh.exe", Params: params})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("netsh 返回退出码 %d", code)
	}
	srv.logf(LogInfo, "已在 Windows 防火墙放行 TCP %d（规则「%s」）", port, firewallRuleName(port))
	return nil
}

// RemoveFirewallRule 删除同名入站规则
func RemoveFirewallRule(port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("端口非法: %d", port)
	}
	params := fmt.Sprintf(`advfirewall firewall delete rule name="%s"`, firewallRuleName(port))
	code, err := winrun.RunElevatedWait(winrun.Elevated{File: "netsh.exe", Params: params})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("netsh 返回退出码 %d（规则可能本来就不存在）", code)
	}
	srv.logf(LogInfo, "已删除 Windows 防火墙规则「%s」", firewallRuleName(port))
	return nil
}
