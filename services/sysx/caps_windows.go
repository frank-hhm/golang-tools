//go:build windows

package sysx

import (
	"os/exec"

	"golang.org/x/sys/windows"
)

// detectCaps Windows 平台探测。
//
// Windows 没有 init 系统，服务一律由面板直接托管（direct）；
// 绿色版布局是本项目在 Windows 上的既有约定，因此 PkgMode 为 green。
func detectCaps() Capabilities {
	c := Capabilities{
		OS:             "windows",
		InitSystem:     "direct",
		PkgMode:        "green",
		NativeWindows:  nativeWindows,
		SystemBackdrop: supportsSystemBackdrop(),
	}
	// 粗探测：PATH 里有 docker.exe 就认为 Docker 可用（真正能否连上由 tools/docker 的探测决定）
	if _, err := exec.LookPath("docker.exe"); err == nil {
		c.HasDocker = true
	}
	return c
}

// supportsSystemBackdrop 能否用 Win11 系统级桌面模糊背景（Acrylic/Mica/Tabbed）。
//
// 判据与 Wails w32.SupportsBackdropTypes() 完全一致：Windows 11 build >= 22621。
// 用 RtlGetVersion 取真实版本号（不受 manifest 兼容性垫片影响，比读注册表更可靠）。
func supportsSystemBackdrop() bool {
	info := windows.RtlGetVersion()
	if info == nil {
		return false
	}
	return info.BuildNumber >= 22621
}

// platformProc Windows 全部走直接托管（与本项目原有行为一致）。
func platformProc() Proc { return newDirectProc() }
