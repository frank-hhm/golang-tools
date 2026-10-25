//go:build !windows

package docker

// ============================================================
// Docker 管理 - 非 Windows 平台桩
//
// 本期仅支持 Windows（Docker Desktop 命名管道）。保持与 windows 实现同签名，
// 使 tools/mysql 的双模式代码与 services/app 绑定面在任意 GOOS 下可编译。
// ============================================================

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-tools/appdirs"
)

const (
	LabelTool     = "go-tools"
	LabelInstance = "go-tools.instance"
)

// errUnsupported 直接复用平台无关的 ErrUnsupported：看护协程靠 errors.Is 认出
// 「这台机器压根不支持 Docker」，不会把它当成引擎抖动而无休止地重试
var errUnsupported = ErrUnsupported

func GetEngineStatus() (EngineInfo, error) {
	return EngineInfo{Error: errUnsupported.Error()}, nil
}

func EngineStatusCached(time.Duration) (EngineInfo, error) {
	return EngineInfo{Error: errUnsupported.Error()}, nil
}

func ListContainers(bool) ([]Container, error)         { return nil, errUnsupported }
func InspectContainer(string) (*ContainerState, error) { return nil, errUnsupported }
func ContainerMetricsOf(string) (*ContainerMetrics, error) {
	return nil, errUnsupported
}
func ContainerUptime(string) int64         { return 0 }
func StartContainer(string) error          { return errUnsupported }
func StopContainer(string, int) error      { return errUnsupported }
func RestartContainer(string) error        { return errUnsupported }
func KillContainer(string) error           { return errUnsupported }
func RemoveContainer(string, bool) error   { return errUnsupported }
func RenameContainer(string, string) error { return errUnsupported }

func FindContainerByLabel(string, string) (string, bool, error) { return "", false, errUnsupported }
func CreateContainer(ContainerSpec) (string, error)             { return "", errUnsupported }

func ListImages() ([]Image, error)   { return nil, errUnsupported }
func ImageExists(string) bool        { return false }
func RemoveImage(string, bool) error { return errUnsupported }
func PullImage(string) error         { return errUnsupported }
func EnsureImage(string) error       { return errUnsupported }

func GetContainerLogs(string, int) ([]string, error) { return nil, errUnsupported }
func StreamContainerLogs(string) error               { return errUnsupported }
func StopContainerLogs(string)                       {}
func ContainerLogRing(string, int) []string          { return nil }

/* ============================ 安装与启动 ============================ */

// 与 windows 实现同签名：非 Windows 下统一回 errUnsupported，绑定面不另加分支

func LicenseNotes() []string          { return []string{errUnsupported.Error()} }
func BuildInstallerURL(string) string { return "" }
func InstallArgs(bool) string         { return "" }
func AbortSetup() error               { return errUnsupported }
func CheckEnv(string) (EnvCheck, error) {
	return EnvCheck{
		Backend: Backend{Name: "unknown", Label: "未知", Source: "非 Windows 不支持"},
		Notes:   []string{errUnsupported.Error()},
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

/* ============================ 镜像长任务 ============================ */

func CancelImageOp(string) (string, error)       { return "", errUnsupported }
func CurrentImageOp() string                     { return "" }
func PushImage(string) error                     { return errUnsupported }
func TagImage(string, string) error              { return errUnsupported }
func PushImageTo(string, string) error           { return errUnsupported }
func ExportImage(string, string) (string, error) { return "", errUnsupported }
func ImportImage(string, string) (string, error) { return "", errUnsupported }
func BuildImage(string, string, string) (string, error) {
	return "", errUnsupported
}
func PruneImages(bool) (PruneReport, error) { return PruneReport{}, errUnsupported }
func RunImage(ImageRunSpec) (string, error) { return "", errUnsupported }

/* ============================ 网络 / 存储卷 / 编排 ============================ */

func ListNetworks() ([]Network, error)               { return nil, errUnsupported }
func CreateNetwork(NetworkSpec) (string, error)      { return "", errUnsupported }
func RemoveNetwork(string) error                     { return errUnsupported }
func ListVolumes() ([]Volume, error)                 { return nil, errUnsupported }
func CreateVolume(VolumeSpec) (string, error)        { return "", errUnsupported }
func RemoveVolume(string) error                      { return errUnsupported }
func PruneVolumes() (VolumePruneReport, error)       { return VolumePruneReport{}, errUnsupported }
func ListComposeProjects() ([]ComposeProject, error) { return nil, errUnsupported }
func ComposeAction(string, string) (string, error)   { return "", errUnsupported }

/* ============================ 仓库与加速站 ============================ */

func ListRegistries() []Registry { return nil }
func SaveRegistry(Registry, string, bool) (Registry, error) {
	return Registry{}, errUnsupported
}
func DeleteRegistry(string) error              { return errUnsupported }
func TestRegistry(string) (string, error)      { return "", errUnsupported }
func RegistryRepos(string) ([]HubRepo, error)  { return nil, errUnsupported }
func SearchHub(string, int) ([]HubRepo, error) { return nil, errUnsupported }
func GetDaemonConfig() (DaemonConfig, error)   { return DaemonConfig{}, errUnsupported }
func SetDaemonConfig([]string, []string) (string, error) {
	return "", errUnsupported
}

// 加速站探测：不返回 error，探不通也当结果回（grade=fail）
func ProbeMirror(raw, sample string) MirrorProbe {
	return MirrorProbe{Url: raw, Grade: "fail", Stage: "connect", Label: errUnsupported.Error()}
}
func ProbeMirrors(raws []string, sample string) []MirrorProbe {
	out := make([]MirrorProbe, len(raws))
	for i, u := range raws {
		out[i] = ProbeMirror(u, sample)
	}
	return out
}

// 非 Windows 没有真实探测，也就没有可叫停的任务
func CancelMirrorProbe(string) bool { return false }
func CancelMirrorProbes() int       { return 0 }

/* ============================ 容器内终端 ============================ */

// ExecStream 容器内交互流。Windows 走 Docker Engine 的 exec 升级连接，
// 非 Windows 平台没有实现，这里只保证签名一致，让 tools/term 能跨平台编译。
type ExecStream struct{}

func (s *ExecStream) Resize(rows, cols int) error { return errUnsupported }
func (s *ExecStream) Read(p []byte) (int, error)  { return 0, errUnsupported }
func (s *ExecStream) Write(p []byte) (int, error) { return 0, errUnsupported }
func (s *ExecStream) Close() error                { return nil }

// OpenExec 在容器里起交互 shell，非 Windows 平台不可用。
func OpenExec(containerID string, argv []string, rows, cols int) (*ExecStream, error) {
	return nil, errUnsupported
}

/* ============================ 导出 / 导入 ============================ */

// DefaultExportPath 导出镜像的默认落盘位置：<数据目录>/docker/exports/<repo>-<tag>-<时间>.tar
// 与 windows 实现同规则，仅目录分隔符随平台变化。
func DefaultExportPath(ref string) string {
	dir, err := appdirs.Data("docker")
	if err != nil {
		dir = os.TempDir()
	} else {
		dir = filepath.Join(dir, "exports")
	}
	name := strings.NewReplacer("/", "_", ":", "-", "\\", "_").Replace(ref)
	return filepath.Join(dir, name+"-"+time.Now().Format("20060102-150405")+".tar")
}
