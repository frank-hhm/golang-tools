//go:build windows

package sysx

import "os/exec"

// detectCaps Windows 平台探测。
//
// Windows 没有 init 系统，服务一律由面板直接托管（direct）；
// 绿色版布局是本项目在 Windows 上的既有约定，因此 PkgMode 为 green。
func detectCaps() Capabilities {
	c := Capabilities{
		OS:            "windows",
		InitSystem:    "direct",
		PkgMode:       "green",
		NativeWindows: nativeWindows,
	}
	// 粗探测：PATH 里有 docker.exe 就认为 Docker 可用（真正能否连上由 tools/docker 的探测决定）
	if _, err := exec.LookPath("docker.exe"); err == nil {
		c.HasDocker = true
	}
	return c
}

// platformProc Windows 全部走直接托管（与本项目原有行为一致）。
func platformProc() Proc { return newDirectProc() }
