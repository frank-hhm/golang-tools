//go:build !windows

package docker

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

// CheckEnv 非 Windows 没有 Desktop 后端需要体检
func CheckEnv(string) (EnvCheck, error) {
	return EnvCheck{
		Backend: Backend{Name: "unknown", Label: "未知", Source: "非 Windows 平台，由系统包管理器维护"},
		Notes:   []string{"非 Windows 平台请用 systemctl 管理 Docker 服务"},
	}, errUnsupported
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
