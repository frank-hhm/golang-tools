//go:build !windows

package docker

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"go-tools/services/sysx"
)

// ============================================================
// Docker 管理 - 非 Windows 平台
//
// 引擎通信、容器 / 镜像 / 网络 / 卷 / 日志 / exec 终端全部由公共层实现
// （docker.go、client.go、resource.go、imagectl.go、exec.go、mirror.go），
// Linux 上走 /var/run/docker.sock，行为与 Windows 命名管道一致。
//
// 本文件只处理"引擎进程本身怎么装、怎么起停"这一层，原则是：
//   1. Linux 上确实存在的语义 → 老老实实实现（systemctl + 发行版仓库）；
//   2. 只属于 Docker Desktop（WSL2 / Hyper-V / .exe 安装器）的入口 → 明确
//      说明为什么不存在、Linux 上该做什么，而不是笼统地报"不支持"。
//
// 为什么要区分这两类：面板是同一套前端，用户点按钮时最需要知道的是"下一步做
// 什么"，一句 errUnsupported 会把"概念不存在"和"功能缺失"混为一谈。
// ============================================================

// 事件名与 Windows 侧一致：前端只认 "docker:setup"
const setupEvent = "docker:setup"

func emitSetup(p SetupProgress) { emit(setupEvent, p) }

const (
	// dockerUnit 官方包与各家发行版仓库都注册成 docker.service
	dockerUnit = "docker.service"
	// dockerSocket 按需激活 socket：只 stop 服务不停 socket 会被立刻拉回来
	dockerSocket = "docker.socket"

	// engineReadyWait 起停后等引擎应答的上限。冷启动要 mount overlay、恢复容器，
	// 首次尤其慢，给到 90 秒；超时不回滚，只给排查方向。
	engineReadyWait = 90 * time.Second
	unitOpTimeout   = 3 * time.Minute
)

// setupCanceled 与 Windows 侧的 startCancelled 同义：等待引擎就绪期间用户点了「停止」，
// 轮询循环看到它就收手，不靠 context 传递
var setupCanceled atomic.Bool

/* ============================ 引擎启停（systemd） ============================ */

// requireServiceControl 启停系统服务的前置条件：systemd + root。
//
// 两个都缺时不能"试一下就报错"：systemctl 在非 root 下会走 polkit 交互式认证，
// 面板里没有终端可交互，最后只会吐一句看不懂的 "Interactive authentication required"。
func requireServiceControl(action string) error {
	caps := sysx.Caps()
	if !caps.Systemd() {
		return fmt.Errorf("未检测到 systemd（%s），无法%s docker 服务；请按发行版文档用 service %s 或直接运行 dockerd",
			caps.InitSystem, action, action)
	}
	if !caps.IsRoot {
		return fmt.Errorf("需要 root 才能%s docker 服务：面板当前以非 root 身份运行，请在服务器上执行 sudo systemctl %s docker",
			action, map[string]string{"启动": "start", "停止": "stop", "重启": "restart"}[action])
	}
	return nil
}

// runUnit 跑一次 systemctl，输出逐行走 setup 事件（前端日志框能看见真实进度）
func runUnit(verb, unit string) error {
	err := sysx.RunStreaming(unitOpTimeout, "systemctl", []string{verb, unit}, nil, func(line string) {
		if s := strings.TrimSpace(line); s != "" {
			emitSetup(SetupProgress{Stage: "info", Note: s})
		}
	})
	if err != nil {
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}
	return nil
}

// waitEngineUp 轮询引擎直到可达、被取消或超时
func waitEngineUp(budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		time.Sleep(2 * time.Second)
		if setupCanceled.Load() {
			emitSetup(SetupProgress{Stage: "error", Note: "启动已取消"})
			return fmt.Errorf("启动已取消")
		}
		if eng, _ := GetEngineStatus(); eng.Available {
			emitSetup(SetupProgress{Stage: "done", Percent: 100,
				Note: "Docker 引擎已就绪（" + eng.Version + "），可以开始管理容器"})
			return nil
		}
	}
	note := fmt.Sprintf("docker.service 已启动，但引擎在 %s 内未应答：看 systemctl status docker 与 journalctl -u docker -n 50；"+
		"常见原因是 /etc/docker/daemon.json 写坏、存储驱动不可用、磁盘满", budget)
	emitSetup(SetupProgress{Stage: "error", Note: note})
	return fmt.Errorf("%s", note)
}

// waitEngineDown 停止后确认引擎真的不理人了（systemctl stop 返回 0 不等于进程已退干净）
func waitEngineDown(budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for {
		eng, _ := GetEngineStatus()
		if !eng.Available {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Second)
	}
}

// StartEngine 已安装但引擎没跑时，拉起 docker.service 并等待就绪
func StartEngine() error {
	setupCanceled.Store(false)
	if eng, _ := GetEngineStatus(); eng.Available {
		emitSetup(SetupProgress{Stage: "done", Percent: 100, Note: "Docker 引擎已在运行（" + eng.Version + "）"})
		return nil
	}
	if err := requireServiceControl("启动"); err != nil {
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}
	emitSetup(SetupProgress{Stage: "starting", Note: "正在启动 docker.service…"})
	if err := runUnit("start", dockerUnit); err != nil {
		return err
	}
	return waitEngineUp(engineReadyWait)
}

// StopEngine 停止引擎：先叫停可能还在跑的等待，再停服务与 socket，最后确认引擎真的退了
func StopEngine() error {
	setupCanceled.Store(true)
	if err := requireServiceControl("停止"); err != nil {
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}
	emitSetup(SetupProgress{Stage: "info", Note: "正在停止 docker.service…"})
	if err := runUnit("stop", dockerUnit); err != nil {
		return err
	}
	// socket 不一起停的话，任何一次 docker 客户端连接都会把它按需拉起来
	if err := runUnit("stop", dockerSocket); err != nil {
		emitSetup(SetupProgress{Stage: "info", Note: "docker.socket 未在运行（可忽略）：" + err.Error()})
	}
	if !waitEngineDown(15 * time.Second) {
		eng, _ := GetEngineStatus()
		note := "已发出停止命令，但引擎仍在应答（" + eng.Version + "）：可能有 rootless dockerd、容器里的引擎，或别的 dockerd 在跑"
		emitSetup(SetupProgress{Stage: "error", Note: note})
		return fmt.Errorf("%s", note)
	}
	emitSetup(SetupProgress{Stage: "done", Note: "Docker 引擎已停止（开机自启未改动；要关掉请 systemctl disable docker）"})
	return nil
}

// RestartEngine 重启引擎（改完 daemon.json 后用它让配置生效）
func RestartEngine() error {
	setupCanceled.Store(false)
	if err := requireServiceControl("重启"); err != nil {
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}
	emitSetup(SetupProgress{Stage: "starting", Note: "正在重启 docker.service（daemon.json 的改动此时生效）…"})
	if err := runUnit("restart", dockerUnit); err != nil {
		return err
	}
	return waitEngineUp(engineReadyWait)
}

// CancelStart 中止正在进行的启动等待（面板点「停止」时先叫停等待，再去停服务）
func CancelStart() { setupCanceled.Store(true) }

/* ============================ 安装（包管理器） ============================ */

// LicenseNotes 非 Windows 不用装 Desktop：Docker Engine 是 Apache-2.0，
// 没有 Desktop 那套订阅协议限制，这里如实说明并给一条能照做的命令。
func LicenseNotes() []string {
	notes := []string{"Linux 上装的是 Docker Engine（Apache-2.0），没有 Docker Desktop 的订阅协议限制，商用无需付费订阅。"}
	if mgr := sysx.PkgManager(); mgr != "" {
		notes = append(notes, fmt.Sprintf("安装走系统包管理器：%s install -y %s（需 root，依赖解析与运行用户由它负责）。", mgr, pickDockerPackage()))
	} else {
		notes = append(notes, "本机没有 dnf/yum/apt-get：请按官方文档手工安装 docker-ce 后回到本页。")
	}
	return notes
}

// 各包管理器里 Docker Engine 的包名：Debian/Ubuntu 官方仓库是 docker.io，
// Docker 官方仓库是 docker-ce，RHEL/CentOS 是 docker（extras / AppStream）。
var dockerPackages = []string{"docker-ce", "docker.io", "docker"}

// pickDockerPackage 选一个仓库里真有候选版本的包名；都查不到时按包管理器猜一个，
// 让安装错误里带出仓库原文，比"我不知道装哪个包"有用。
func pickDockerPackage() string {
	for _, p := range dockerPackages {
		if sysx.PackageCandidate(p) != "" {
			return p
		}
	}
	if strings.HasSuffix(sysx.PkgManager(), "apt-get") {
		return "docker.io"
	}
	return "docker"
}

// BuildInstallerURL / InstallArgs 是 Docker Desktop 安装器的概念，
// Linux 没有安装器可下载，因此恒为空。保留符号是为了绑定面两个平台一致。
func BuildInstallerURL(string) string { return "" }
func InstallArgs(bool) string         { return "" }

// InstallDocker 用发行版仓库安装 Docker Engine，装完启动并等引擎就绪。
//
// 三个 Windows 参数（installerPath / input / dir）与 webDownload 在这里没有对应物：
// Linux 的引擎由仓库提供，不需要 .exe 安装包，也没有"web 下载版"这种选择。
func InstallDocker(installerPath, input, dir string, webDownload bool) error {
	_, _, _, _ = installerPath, input, dir, webDownload

	if eng, _ := GetEngineStatus(); eng.Available {
		emitSetup(SetupProgress{Stage: "done", Percent: 100, Note: "Docker 引擎已在运行（" + eng.Version + "），无需安装"})
		return nil
	}
	mgr := sysx.PkgManager()
	if mgr == "" {
		err := fmt.Errorf("未找到 dnf/yum/apt-get，无法自动安装：请按官方文档装好 docker-ce 后回到本页")
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}
	if !sysx.Caps().IsRoot {
		err := fmt.Errorf("安装 Docker 需要 root：面板当前以非 root 身份运行，请在服务器上执行 %s install -y %s", mgr, pickDockerPackage())
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}

	pkg := pickDockerPackage()
	emitSetup(SetupProgress{Stage: "installing", Note: fmt.Sprintf("使用 %s 安装 %s（依赖解析与用户组创建由它负责，可能需要几分钟）…", mgr, pkg)})
	if err := sysx.InstallPackages([]string{pkg}, func(line string) {
		if s := strings.TrimSpace(line); s != "" {
			emitSetup(SetupProgress{Stage: "installing", Note: s})
		}
	}); err != nil {
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}
	// 装好不等于在跑：有的仓库只 enable 不 start，交给 StartEngine 统一处理
	emitSetup(SetupProgress{Stage: "starting", Note: "安装完成，正在启动 docker.service 并等待引擎就绪…"})
	return StartEngine()
}

// AbortSetup 非 Windows 没有"下载中的安装包"可取消：安装是一条同步跑完的
// 包管理器命令，真正需要中断的是"等待引擎就绪"，那由 CancelStart 负责。
func AbortSetup() error {
	setupCanceled.Store(true)
	emitSetup(SetupProgress{Stage: "info", Note: "Linux 上没有下载中的安装包；已请求后续等待引擎的动作尽早结束"})
	return nil
}

// ProbeInstaller / DownloadInstaller 是 Docker Desktop 安装包才有的两步（HEAD 探体积、
// 断点续传下载）。Linux 上不该走到这里，报错时直接给替代动作。
func ProbeInstaller(string) (ProbeInfo, error) {
	return ProbeInfo{}, fmt.Errorf("在线安装包只对 Windows 的 Docker Desktop 有意义；Linux 请直接点「安装 Docker Engine」，由 dnf/yum/apt 完成")
}

func DownloadInstaller(string, string) (string, error) {
	return "", fmt.Errorf("Linux 不下载安装器：引擎由发行版仓库提供，点「安装 Docker Engine」即可")
}

/* ============================ Windows 桌面专有概念 ============================ */

// windowsOnly 构造"这个能力在 Linux 上不存在"的错误，并给出该改用什么。
func windowsOnly(feature, instead string) error {
	return fmt.Errorf("%s 只存在于 Windows 的 Docker Desktop；%s", feature, instead)
}

// EnableWsl2 / UpdateWsl：WSL2 是 Windows 的轻量虚拟化层，Linux 本身就是宿主内核
func EnableWsl2() error {
	return windowsOnly("WSL2", "Linux 上容器直接跑在宿主内核，没有虚拟化后端可启用")
}

func UpdateWsl() error {
	return windowsOnly("更新 WSL2", "Linux 的对应动作是升级内核/发行版包，与 Docker 引擎无关")
}

// EnableHyperV：Hyper-V 角色是 Windows 功能组件
func EnableHyperV(bool) error {
	return windowsOnly("Hyper-V 角色", "Linux 没有这个角色；需要虚拟化请另装 KVM/libvirt")
}

// SwitchBackend / SwitchGateError：Desktop 才有 WSL2 与 Hyper-V 两种后端
func SwitchBackend(bool, bool) error {
	return windowsOnly("虚拟化后端切换（WSL2 / Hyper-V）", "Linux 的引擎只有一个后端（原生），无需切换")
}

func SwitchGateError(bool, bool) error {
	return windowsOnly("虚拟化后端切换（WSL2 / Hyper-V）", "Linux 的引擎只有一个后端（原生），无需切换")
}

/* ============================ 体检 ============================ */

// CheckEnv 非 Windows 的体检：不碰安装包，只如实报告引擎能不能连、连的哪个端点，
// 以及连不上时最该查的三件事。这里恰恰是排查"为什么面板连不到 docker"最有用的地方，
// 所以不返回错误，前端也不必把它当失败展示。
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
			"本页可以直接装/起/停：装用发行版仓库（需 root），起停走 systemctl（需 root 与 systemd）",
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
	if !caps.Systemd() {
		env.Notes = append(env.Notes, "未检测到 systemd：引擎可能是被手工拉起的，面板的启停按钮会拒绝执行，需自行管理进程")
	}
	return env, nil
}

// 仓库与加速站、daemon 配置现在也由公共层实现（registry.go / mirror.go），
// 非 Windows 的仓库密码退化为 base64 编码 + 配置文件权限保护（见 dpapi_other.go）。
