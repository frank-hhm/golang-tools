//go:build linux

package sysx

import (
	"os"
	"os/exec"
)

// detectCaps Linux 平台探测。
//
// 两个关键运行时事实：
//   - 是否真的由 systemd 管 init：看 /run/systemd/system（容器里通常没有），
//     再确认 systemctl 在 PATH 里，两者都满足才认为可以用 systemd 托管服务；
//   - 是否 root：写 /etc/systemd/system 与启停系统服务都需要 root，非 root 时回落直接托管。
func detectCaps() Capabilities {
	c := Capabilities{
		OS:         "linux",
		InitSystem: "direct",
		PkgMode:    "hybrid", // 优先用系统包，缺了再回落自带绿色版
	}
	if os.Geteuid() == 0 {
		c.IsRoot = true
	}
	if st, err := os.Stat("/run/systemd/system"); err == nil && st.IsDir() {
		if _, err := exec.LookPath("systemctl"); err == nil {
			c.InitSystem = "systemd"
		}
	}
	if _, err := exec.LookPath("docker"); err == nil {
		c.HasDocker = true
	}
	return c
}

// platformProc Linux 优先 systemd（日志/开机自启/cgroup 免费拿到），
// 没有 systemd 或不是 root 时回落直接托管，保证面板在任何环境都能跑起来。
//
// 调试开关：GO_TOOLS_SYSX=direct 可强制走直接托管（排查"是不是 systemd 的问题"用）。
func platformProc() Proc {
	if Caps().Systemd() && Caps().IsRoot && os.Getenv("GO_TOOLS_SYSX") != "direct" {
		return newSystemdProc()
	}
	return newDirectProc()
}
