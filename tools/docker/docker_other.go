//go:build !windows

package docker

import (
	"fmt"

	"go-tools/services/sysx"
)

// ============================================================
// Docker 管理 - 非 Windows 平台未实现的能力
//
// 引擎通信、容器 / 镜像 / 网络 / 卷 / 日志 / exec 终端现在全部由公共层实现
// （docker.go、client.go、resource.go、imagectl.go、exec.go、mirror.go），
// Linux 上走 /var/run/docker.sock，行为与 Windows 命名管道一致。
//
// 这里只剩真正依赖 Windows 桌面环境的两类能力：
//   1. Docker Desktop 的安装、WSL2 / Hyper-V 虚拟化后端切换；
//   2. 镜像仓库凭据：Windows 用 DPAPI 加密存储，非 Windows 尚未接入替代方案。
//
// 前端应当按 Capabilities 把这些入口隐藏或置灰，不要让用户以为能用。
// ============================================================

// errUnsupported 直接复用平台无关的 ErrUnsupported：看护协程靠 errors.Is 认出
// 「这台机器压根不支持这个能力」，不会把它当成引擎抖动而无休止地重试
var errUnsupported = ErrUnsupported

/* ============================ 安装与启动（Docker Desktop 专属） ============================ */

// LicenseNotes 非 Windows 不用装 Desktop，直接给一句能照做的引导
func LicenseNotes() []string {
	return []string{"非 Windows 平台请用系统包管理器安装 Docker，例如：apt install docker.io 或 dnf install docker-ce"}
}

func BuildInstallerURL(string) string { return "" }
func InstallArgs(bool) string         { return "" }
func AbortSetup() error               { return errUnsupported }

// CheckEnv 非 Windows 平台的体检：不碰安装包，只如实报告引擎能不能连、连的哪个端点，
// 以及连不上时最该查的三件事。这里恰恰是排查“为什么面板连不到 docker”最有用的地方，
// 所以不返回 errUnsupported，前端也不必把它当失败展示。
func CheckEnv(installDir string) (EnvCheck, error) {
	caps := sysx.Caps()
	env := EnvCheck{
		InstallerDir: installDir,
		Backend: Backend{
			Name:   "native",
			Label:  "原生",
			Source: "Linux 上容器直接跑在宿主内核上，没有 WSL2 / Hyper-V 这类虚拟化后端",
		},
	}

	info, err := GetEngineStatus()
	if err != nil || !info.Available {
		reason := info.Error
		if err != nil {
			reason = err.Error()
		}
		if reason == "" {
			reason = "未连上 daemon"
		}
		env.Notes = []string{
			fmt.Sprintf("引擎不可达：%s", reason),
			"排查顺序：1) 服务在跑吗 → systemctl status docker；2) 有权限读 socket 吗 → ls -l /var/run/docker.sock（非 root 需在 docker 组）；3) 装了吗 → docker version",
			"本面板不管引擎的安装与启停，yum/apt 装好、systemctl enable --now docker 之后这里会自动接上",
		}
		return env, nil
	}

	env.DockerInstalled = true
	env.EngineAvailable = true
	env.PipeExists = true
	env.DockerVersion = info.Version
	env.Notes = []string{
		fmt.Sprintf("引擎已连接：Docker %s（API %s）", info.Version, info.APIVersion),
		fmt.Sprintf("端点：%s", info.Endpoint),
		fmt.Sprintf("运行环境：%s/%s，init=%s，root=%v", caps.OS, caps.Arch, caps.InitSystem, caps.IsRoot),
	}
	if !caps.IsRoot {
		env.Notes = append(env.Notes, "当前非 root：靠 docker 组成员身份访问 socket；容器挂载出来的文件属主仍可能是 root")
	}
	if caps.InitSystem != "systemd" {
		env.Notes = append(env.Notes, "未检测到 systemd：引擎可能是被手工拉起的，开机自启需自行配置")
	}
	return env, nil
}

func ProbeInstaller(string) (ProbeInfo, error)         { return ProbeInfo{}, errUnsupported }
func DownloadInstaller(string, string) (string, error) { return "", errUnsupported }
func InstallDocker(string, string, string, bool) error { return errUnsupported }
func EnableWsl2() error                                { return errUnsupported }
func UpdateWsl() error                                 { return errUnsupported }
func StartEngine() error                               { return errUnsupported }

func SwitchGateError(bool, bool) error { return errUnsupported }
func SwitchBackend(bool, bool) error   { return errUnsupported }
func EnableHyperV(bool) error          { return errUnsupported }

/* ============================ 引擎停止 ============================ */

func StopEngine() error    { return errUnsupported }
func RestartEngine() error { return errUnsupported }
func CancelStart()         {}

// 仓库与加速站、daemon 配置现在也由公共层实现（registry.go / mirror.go），
// 非 Windows 的仓库密码退化为 base64 编码 + 配置文件权限保护（见 dpapi_other.go）。
