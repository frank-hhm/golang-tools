//go:build windows

package mysql

import (
	"strings"
	"syscall"
)

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

// initFailHint 把 Windows 上本地模式的常见失败翻译成下一步动作。
//
// mysqld 的原文（尤其是一串 MY-xxxxx 错误码）对用户没有可操作性，这里给出
// 「现在该看哪里/点哪里」；返回空串表示没什么可补充的（原文已够清楚）。
func initFailHint(output string) string {
	low := strings.ToLower(output)
	switch {
	case strings.Contains(low, "failed to find valid data directory"),
		strings.Contains(low, "data dictionary initialization failed"):
		return "\n提示：数据目录缺少系统表（上一次初始化被打断，或目录被清理过），" +
			"启动时会自动把它备份为 data.broken-* 并重建；若反复出现，先看看该目录是否被杀软/同步盘占用。"
	case strings.Contains(low, "has files in it"):
		return "\n提示：数据目录里已有文件。启动时会自动备份旧目录再重建；若仍失败，先确认没有其它 mysqld 正在使用这个目录。"
	case strings.Contains(low, "access is denied"), strings.Contains(low, "can't create/write to file"),
		strings.Contains(low, "permission denied"):
		return "\n提示：数据目录不可写。别把实例目录放在受保护位置（Program Files、系统盘根目录），" +
			"或以管理员身份重新运行。"
	case strings.Contains(low, "10048"), strings.Contains(low, "bind on tcp"),
		strings.Contains(low, "do you already have another mysqld"):
		// 不用裸的 "port"：正常启动日志里也有 "port: 3306"，那会让每条失败都误报端口冲突
		return "\n提示：端口被别的进程占用，换个端口或先停掉占用方。"
	}
	return ""
}

// myIniPlatformLines Windows 的 MySQL 不靠 unix socket 通信（面板走 TCP），
// 实例之间不会互相抢全局 socket 路径，不需要额外写这两行。
func myIniPlatformLines(string) string { return "" }

// createNoWindow = Win32 CREATE_NO_WINDOW：GUI 子系统构建里 HideWindow(SW_HIDE)
// 藏不住 Win11 的 Windows Terminal，只有不分配控制台才不闪窗。
const createNoWindow = 0x08000000

// hideWindowAttr 以隐藏窗口方式拉起 mysqld 子进程（后台服务无黑框）
func hideWindowAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}
