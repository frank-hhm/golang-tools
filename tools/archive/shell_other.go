//go:build !windows

package archive

import (
	"Tools/services/sysx"
)

// 非 Windows 没有"系统右键挂载"这回事：HKCU 的 shell 键与 SendTo 目录
// 都是资源管理器的机制。这里全部回 sysx.ErrUnsupported，让上层的开关
// 与挂载区按 Capabilities 整块隐藏（与 ShowSystemNotification 的处理一致）。
//
// 压缩/解压本身在这个平台上照常能跑，被砍掉的只有"挂进右键"这一段。

// ShellStatus 挂载状态（非 Windows 恒为不支持）。
func ShellStatus() (ShellInfo, error) {
	return ShellInfo{Supported: false, Compress: SlotAbsent, Extract: SlotAbsent, SendTo: SlotAbsent},
		sysx.ErrUnsupported
}

// ShellInstall 挂进系统右键。
func ShellInstall(sendTo bool) error {
	return sysx.ErrUnsupported
}

// ShellUninstall 撤销系统右键挂载。
func ShellUninstall() error {
	return sysx.ErrUnsupported
}
