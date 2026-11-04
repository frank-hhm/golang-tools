//go:build !windows

package gitcmd

import "Tools/services/sysx"

// 非 Windows 没有"系统右键挂载"这回事：HKCU 的 shell 键是资源管理器的机制。
// 这里全部回 sysx.ErrUnsupported，让上层的开关与挂载区按 Capabilities 整块隐藏
// （与压缩 shell_other.go、ShowSystemNotification 的处理一致）。
//
// Git 引擎本身在这个平台上照常能跑（面板只读浏览服务器仓库），被砍掉的只有
// "挂进系统右键"这一段。

// GitShellStatus 挂载状态（非 Windows 恒为不支持）。
func GitShellStatus() (GitShellInfo, error) {
	return GitShellInfo{Supported: false, Slot: SlotAbsent}, sysx.ErrUnsupported
}

// GitShellInstall 挂进系统右键。
func GitShellInstall() error {
	return sysx.ErrUnsupported
}

// GitShellUninstall 撤销系统右键挂载。
func GitShellUninstall() error {
	return sysx.ErrUnsupported
}
