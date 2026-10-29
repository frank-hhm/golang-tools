package docker

import "errors"

// ============================================================
// Docker 管理 - 类型与事件面（平台无关，windows 实现见 docker.go/client.go）
//
// 接入方式（方案 A）：直连 Docker Engine REST API，零第三方 SDK。
//   端点解析顺序：环境变量 DOCKER_HOST（npipe:// / unix:// / tcp://）
//   → Windows 命名管道 \\.\pipe\docker_engine（含 Docker Desktop 备用管道名）
//   → 失败即报"无法连接 Docker 引擎"，由前端展示引导。
//
// 事件：
//   - "docker:pull"      负载 PullProgress（仿 mysql:install，分层进度）
//   - "docker:serverlog" 负载 ContainerLogLine（容器日志跟随，仿 redis:serverlog）
//   - "docker:setup"     负载 SetupProgress（安装链路的下载/安装/启动进度）
//   事件发射器由 main.go 经 SetEmitter 注入，本包不感知 Wails。
// ============================================================

/* ============================ 引擎 ============================ */

// EngineInfo Docker 引擎可达性与基本信息（不可达也返回，字段零值 + Error 说明原因）
type EngineInfo struct {
	Available  bool   `json:"available"`  // 能否与 daemon 对话
	IsDesktop  bool   `json:"isDesktop"`  // 是否 Docker Desktop（影响能否提供安装引导）
	Version    string `json:"version"`    // 服务端版本，如 27.1.1
	APIVersion string `json:"apiVersion"` // 协商到的 API 版本，如 1.47
	OS         string `json:"os"`         // linux / windows
	Arch       string `json:"arch"`
	Kernel     string `json:"kernel"`
	Backend    string `json:"backend"` // 由内核串判出的虚拟化后端：wsl2 | hyperv | ""
	GoVersion  string `json:"goVersion"`
	Platform   string `json:"platform"` // 如 "Docker Desktop 4.34.0 (165345)"
	Endpoint   string `json:"endpoint"` // 实际连上的端点（管道路径或 tcp 地址）
	CPUs       int    `json:"cpus"`
	MemTotal   int64  `json:"memTotal"`
	// 引擎已配的镜像加速器（daemon 的 RegistryMirrors）：拉不到镜像时先看这里
	RegistryMirrors []string `json:"registryMirrors"`
	Error           string   `json:"error"` // 不可达原因，前端直接展示
}

/* ============================ 容器 ============================ */

// PortBinding 端口映射（PrivatePort=容器内，PublicPort=宿主机）
type PortBinding struct {
	IP          string `json:"ip"`
	PrivatePort int    `json:"privatePort"`
	PublicPort  int    `json:"publicPort"`
	Type        string `json:"type"` // tcp | udp
}

// Container 列表项（/containers/json 聚合 /stats 的 CPU、内存）
type Container struct {
	ID        string        `json:"id"`
	Name      string        `json:"name"`
	Image     string        `json:"image"`
	ImageID   string        `json:"imageId"`
	Command   string        `json:"command"`
	Created   int64         `json:"created"`
	State     string        `json:"state"`  // running | exited | paused | created
	Status    string        `json:"status"` // "Up 5 minutes" / "Exited (0) 2 hours ago"
	Ports     []PortBinding `json:"ports"`
	CPUPct    float64       `json:"cpuPct"`
	MemPct    float64       `json:"memPct"`
	MemUsage  string        `json:"memUsage"`  // "12.3 MiB / 15.6 GiB"
	OwnerTool string        `json:"ownerTool"` // 由本工具创建的容器回标所属工具（mysql 等）
	OwnerID   string        `json:"ownerId"`   // 对应工具实例 ID
}

// ContainerState 单容器实时状态（inspect 聚合，供状态轮询与 mysql 生命周期判定）
type ContainerState struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Image      string `json:"image"`
	State      string `json:"state"`
	Status     string `json:"status"`
	Running    bool   `json:"running"`
	Restarting bool   `json:"restarting"`
	ExitCode   int    `json:"exitCode"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`
	HostPort   int    `json:"hostPort"` // 主端口（已发布到宿主机的最小容器端口，判定实例配置是否变更）
	Pid        int    `json:"pid"`      // 容器内主进程在宿主机 namespace 的 pid（Linux 引擎下常为 VM 内 pid，仅展示）
	Error      string `json:"error"`
	// Published 全部已发布端口（按容器端口升序）：双监听的服务（nginx 的 80/443）面板要逐个展示
	Published []PortPublish `json:"published"`
	// UptimeSeconds 已运行秒数（与 Running 同一次 inspect 算出，调用方不必再查一次）
	UptimeSeconds int64 `json:"uptimeSeconds"`
}

// PortPublish 一对端口映射：容器内端口 → 宿主机发布端口（Host 为 0 表示未发布）
type PortPublish struct {
	Container int `json:"container"`
	Host      int `json:"host"`
}

// ContainerMetrics 单次 /stats 采样的实时资源占用（容器监控图表用）。
// 除 CPU/内存外均为累计计数器：速率由前端对相邻两次采样取差除以间隔得到
// （容器重启后计数器归零，前端遇到负增量按 0 处理）
type ContainerMetrics struct {
	At         int64   `json:"at"`         // 采样时刻（unix 毫秒，X 轴用）
	CPUPercent float64 `json:"cpuPercent"` // 0~100：已按 online_cpus 归一，100 = 吃满全部分到的核
	MemUsed    int64   `json:"memUsed"`    // 工作集字节（usage 减缓存，排除 page cache 虚高）
	MemCache   int64   `json:"memCache"`   // 缓存字节（cgroup v1 取 cache，v2 用 inactive_file 近似）
	MemLimit   int64   `json:"memLimit"`   // 内存限额，0 = 未限制
	DiskRead   int64   `json:"diskRead"`   // blkio 累计读字节
	DiskWrite  int64   `json:"diskWrite"`  // blkio 累计写字节
	NetRx      int64   `json:"netRx"`      // 各网卡累计接收字节
	NetTx      int64   `json:"netTx"`      // 各网卡累计发送字节
	Pids       int64   `json:"pids"`       // 当前进程数
}

// ContainerSpec 创建容器所需参数（覆盖 mysql/php 这类"单端口 + 数据卷"与 nginx 这类双监听场景）
type ContainerSpec struct {
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	Env           []string          `json:"env"`
	Binds         []string          `json:"binds"` // "C:/host/path:/container/path"
	Labels        map[string]string `json:"labels"`
	ContainerPort int               `json:"containerPort"` // 主端口，如 3306
	HostPort      int               `json:"hostPort"`      // 宿主机发布端口
	HostIP        string            `json:"hostIp"`        // 空则 127.0.0.1
	Cmd           []string          `json:"cmd"`
	// ExtraPorts 主端口之外还要发布的端口（nginx 的 HTTPS 这类双监听）。
	// 主端口继续走 ContainerPort/HostPort：既有 php/mysql 调用与断言一律不受影响
	ExtraPorts []PortPublish `json:"extraPorts,omitempty"`
}

/* ============================ 镜像 ============================ */

// ErrCanceled 用户主动取消长任务，不是故障。
// 绑定层据此把「取消」当正常返回，避免 Wails 把用户点取消记成「绑定调用失败」（ERR）。
var ErrCanceled = errors.New("已取消")

// IsCanceled 判断错误是否源自用户主动取消
func IsCanceled(err error) bool { return err != nil && errors.Is(err, ErrCanceled) }

// Image 本地镜像列表项（同一镜像多个 tag 会拆成多行，便于按 tag 展示）
type Image struct {
	ID      string `json:"id"`      // 短 ID
	Digest  string `json:"digest"`  // sha256 全量
	Repo    string `json:"repo"`    // mysql
	Tag     string `json:"tag"`     // 8.4
	Created int64  `json:"created"` // Unix 秒
	Size    int64  `json:"size"`
}

// PullProgress 镜像拉取进度事件负载（stage 语义与 mysql:install 一致）
type PullProgress struct {
	Image   string `json:"image"`
	Stage   string `json:"stage"`   // info|downloading|extracting|done|error|canceled
	Layer   string `json:"layer"`   // 分层 id
	Status  string `json:"status"`  // Pulling fs layer | Download complete | ...
	Percent int    `json:"percent"` // 该层百分比（由 progressDetail 换算，无明细则 0）
	Note    string `json:"note"`
	// 整体进度：按已见分层的 total 字节加权，前端据此画一根可信的总进度条
	Overall int   `json:"overall"`
	Bytes   int64 `json:"bytes"`      // 已完成字节
	Total   int64 `json:"total"`      // 已知总字节（有层未给 total 时为已见部分的合计）
	Layers  int   `json:"layers"`     // 已见分层数
	Done    int   `json:"layersDone"` // 已完成分层数
}

// ImageTaskProgress 其余镜像长任务（push / build / import / export）的进度负载，
// 与拉取分开一个事件（docker:imageop），前端按 op 复用同一套进度界面
type ImageTaskProgress struct {
	Op      string `json:"op"`    // push | build | import | export
	Image   string `json:"image"` // 镜像引用或产物文件名
	Stage   string `json:"stage"` // info|running|done|error|canceled
	Status  string `json:"status"`
	Percent int    `json:"percent"`
	Bytes   int64  `json:"bytes"`
	Total   int64  `json:"total"`
	Note    string `json:"note"`
}

/* ============================ 仓库与加速器 ============================ */

// Registry 面板里维护的私有仓库（密码用 DPAPI 加密落盘，接口只回传“是否已保存”）
type Registry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`      // 显示名，如 公司内网 Harbor
	URL       string `json:"url"`       // 仓库根地址，如 https://harbor.corp.local（不带 /v2）
	User      string `json:"user"`      // 登录用户名，空表示匿名
	HasPass   bool   `json:"hasPass"`   // 是否已保存密码（明文永不出后端）
	Namespace string `json:"namespace"` // 默认命名空间/项目，推送时拼成 url/ns/name:tag
	Note      string `json:"note"`
}

// DaemonConfig 引擎侧 daemon.json 的两个常用键（加速器与不安全仓库）
type DaemonConfig struct {
	File     string   `json:"file"`     // 实际读写的文件路径
	Mirrors  []string `json:"mirrors"`  // registry-mirrors：拉取加速站
	Insecure []string `json:"insecure"` // insecure-registries：内网 http/自签证书仓库
	OtherKey int      `json:"otherKey"` // 其余键数量（提示“只改这两项，其它保留”）
	EngineUp bool     `json:"engineUp"` // 引擎当前是否在跑（改完要重启才生效）
}

// MirrorProbe 加速站探测结果：连通 / 清单 / 层数据三段各自计时，结论分四级
// grade：ok 可下载 | slow 能下但慢 | empty 不给层数据 | fail 连不上或没清单
type MirrorProbe struct {
	Url        string  `json:"url"`        // 实际探测的根地址（已补协议）
	Host       string  `json:"host"`       // 主机[:端口]
	Sample     string  `json:"sample"`     // 取样镜像，默认 library/alpine:latest
	Grade      string  `json:"grade"`      // ok / slow / empty / fail
	Label      string  `json:"label"`      // 一句话结论
	Hint       string  `json:"hint"`       // 该怎么办（失败时才给）
	Stage      string  `json:"stage"`      // 走到/卡在哪一步：url / connect / manifest / blob
	ConnectMs  int64   `json:"connectMs"`  // /v2/ 往返（含 TLS 握手）
	ManifestMs int64   `json:"manifestMs"` // 取清单的累计耗时
	TtfbMs     int64   `json:"ttfbMs"`     // 层数据首字节耗时：卡住的站这个值最大
	Bytes      int64   `json:"bytes"`      // 层数据实收字节
	BlobSize   int64   `json:"blobSize"`   // 取样那一层的总大小
	Mbps       float64 `json:"mbps"`       // 这段取样的速率
	Layer      string  `json:"layer"`      // 取的是哪一层（短 digest）
	ProbedAt   string  `json:"probedAt"`   // 探测时刻 HH:MM:SS
}

// HubRepo 线上镜像条目（Docker Hub 搜索 或 私有仓库 catalog 归一化后的形状）
type HubRepo struct {
	Name        string `json:"name"`        // 完整引用，如 library/mysql 或 harbor.corp/ns/app
	Description string `json:"description"` // 简介
	Source      string `json:"source"`      // Docker Hub / 某私有仓库名
	Stars       int    `json:"stars"`       // Star 数（私有仓库没有则为 0）
	Pulls       int64  `json:"pulls"`       // 拉取次数（私有仓库没有则为 0）
	Official    bool   `json:"official"`    // 官方镜像
	Tags        string `json:"tags"`        // 已知 tag（逗号分隔，可能为空）
}

// PruneReport 镜像清理结果
type PruneReport struct {
	Deleted []string `json:"deleted"` // 被清掉的镜像 ID/tag
	Space   int64    `json:"space"`   // 回收字节
}

// ImageRunSpec 由镜像直接创建容器（面板“创建容器”），端口/环境变量够用即可
type ImageRunSpec struct {
	Image         string `json:"image"`
	Name          string `json:"name"`
	ContainerPort int    `json:"containerPort"` // 0 表示不发布端口
	HostPort      int    `json:"hostPort"`      // 0 表示随机分配
	Env           string `json:"env"`           // 每行一个 KEY=VALUE
	AutoStart     bool   `json:"autoStart"`     // 创建后立刻启动
}

/* ============================ 网络 ============================ */

// Network Docker 网络（/networks 列表项）
type Network struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Driver     string            `json:"driver"` // bridge | host | overlay | null
	Scope      string            `json:"scope"`  // local | swarm
	Internal   bool              `json:"internal"`
	Attachable bool              `json:"attachable"`
	Ingress    bool              `json:"ingress"`
	Created    string            `json:"created"` // RFC3339，daemon 原样给
	Subnet     string            `json:"subnet"`  // 第一个 IPAM 子网（多子网只取头一个用于列表展示）
	Gateway    string            `json:"gateway"`
	Containers int               `json:"containers"` // 已接入的容器数（daemon 只统计运行中的）
	Builtin    bool              `json:"builtin"`    // bridge/host/none：daemon 不让删，前端据此禁按钮
	Labels     map[string]string `json:"labels"`
	OwnerTool  string            `json:"ownerTool"` // 由本工具创建时回标（标签 Tools）
}

// NetworkSpec 新建网络：只有名称必填，其余留空走 daemon 默认
type NetworkSpec struct {
	Name     string `json:"name"`
	Driver   string `json:"driver"`   // 空=bridge
	Internal bool   `json:"internal"` // 无外网出口
	Subnet   string `json:"subnet"`   // 空=IPAM 自动分配，如 172.30.0.0/16
}

/* ============================ 存储卷 ============================ */

// Volume Docker 存储卷（/volumes 列表项）
type Volume struct {
	Name       string            `json:"name"`
	Driver     string            `json:"driver"`
	Mountpoint string            `json:"mountpoint"`
	Created    string            `json:"created"` // RFC3339
	Scope      string            `json:"scope"`
	InUse      int               `json:"inUse"` // 引用它的容器数（daemon 不给，按容器 Mounts 现算）
	Labels     map[string]string `json:"labels"`
	OwnerTool  string            `json:"ownerTool"`
}

// VolumeSpec 新建存储卷
type VolumeSpec struct {
	Name   string `json:"name"`
	Driver string `json:"driver"` // 空=local
}

// VolumePruneReport 存储卷清理结果
type VolumePruneReport struct {
	Deleted []string `json:"deleted"`
	Space   int64    `json:"space"` // 回收字节
}

/* ============================ 编排 ============================ */

// ComposeService 编排项目里的一个服务（compose 里一个服务通常就是一个容器）
type ComposeService struct {
	Name      string `json:"name"`      // 服务名（com.docker.compose.service）
	Container string `json:"container"` // 容器名
	ID        string `json:"id"`        // 短 ID，批量操作按它下发
	Image     string `json:"image"`
	State     string `json:"state"`
	Status    string `json:"status"`
}

// ComposeProject 按 com.docker.compose.* 标签聚合出的编排项目。
// Engine API 没有 compose 端点（那是 docker compose CLI 的活），
// 所以这里只做归组展示与批量启停，不解析 yml、不依赖 CLI
type ComposeProject struct {
	Name        string           `json:"name"`
	WorkingDir  string           `json:"workingDir"`  // 项目工作目录
	ConfigFiles []string         `json:"configFiles"` // compose 文件（可能多个 -f）
	Created     int64            `json:"created"`     // 项目内最早的容器创建时间（unix 秒）
	Services    []ComposeService `json:"services"`
	Running     int              `json:"running"`
	Total       int              `json:"total"`
	Status      string           `json:"status"` // running | partial | stopped
}

/* ============================ 日志 ============================ */

// ContainerLogLine 容器日志行事件负载（仿 redis ServerLogLine）
type ContainerLogLine struct {
	ID   string `json:"id"`
	Time string `json:"time"`
	Line string `json:"line"`
}

/* ============================ 安装与启动 ============================ */

// Backend Docker Desktop 的虚拟化后端判定（WSL2 与 Hyper-V 对外是同一套 Engine API）
type Backend struct {
	Name   string `json:"name"`   // wsl2 | hyperv | unknown
	Label  string `json:"label"`  // 前端直接展示：WSL2 / Hyper-V / 未知
	Source string `json:"source"` // 判定依据：引擎内核串 | 配置文件 | 缺省
	// 真正控制后端的配置键：新版 Docker Desktop 是 EngineTypeWindows（wsl2/hyper-v/docker-vmm），
	// 旧版是 wslEngineEnabled（true/false）。4.96.3 起旧键会被当作“未知设置”丢弃。
	SettingKey string `json:"settingKey"`
	// 该键在文件里的原值；空表示文件里根本没写这个键
	SettingVal string `json:"settingVal"`
	// 由 SettingVal 归一化出的意图：wsl2 | hyperv | ""（认不出的值不当意图）
	SettingWant string `json:"settingWant"`
	// 文件里遗留的旧键 wslEngineEnabled 原值：新版 Docker Desktop 不读它，
	// 保留只为解释“以前切过一次但没生效”
	LegacyVal    string   `json:"legacyVal"`
	SettingsFile string   `json:"settingsFile"` // 实际读到的配置文件，写回也只改这一个
	Kernel       string   `json:"kernel"`       // 参与判定的内核串
	Verdict      string   `json:"verdict"`      // 一句话结论：配置意图与引擎实际不一致时才填
	Fix          []string `json:"fix"`          // 结论对应的可执行下一步
}

// EnvCheck 安装前只读体检结果（不装也不改任何东西，全部为探测事实 + 待办提示）
type EnvCheck struct {
	DockerInstalled bool     `json:"dockerInstalled"` // 程序文件存在（比注册表可靠）
	DockerExePath   string   `json:"dockerExePath"`   // Docker Desktop.exe 实际路径
	DockerVersion   string   `json:"dockerVersion"`   // 注册表卸载项 DisplayVersion，取不到为空
	WslInstalled    bool     `json:"wslInstalled"`    // wsl.exe 可用且已注册
	WslStatus       string   `json:"wslStatus"`       // WSL 现状说明（wsl --status / -l -v 原文，已转 UTF-8 并截断）
	WslDistros      []string `json:"wslDistros"`      // 已注册发行版："docker-desktop(running)"
	WslDockerDistro string   `json:"wslDockerDistro"` // docker-desktop 发行版状态 running|stopped|空=没有
	PipeExists      bool     `json:"pipeExists"`      // 命名管道已在监听（引擎进过）
	EngineAvailable bool     `json:"engineAvailable"` // 能跟 daemon 说上话
	InstallerFile   string   `json:"installerFile"`   // 存放目录里的离线安装包
	InstallerSize   int64    `json:"installerSize"`
	InstallerDir    string   `json:"installerDir"`    // 实际生效的安装包存放目录（空=默认 <插件目录>/docker）
	DefaultURL      string   `json:"defaultUrl"`      // 在线下载默认直链
	Notes           []string `json:"notes"`           // 体检给出的待办，前端逐行展示
	Backend         Backend  `json:"backend"`         // 当前虚拟化后端（含诊断结论）
	HypervInstalled bool     `json:"hypervInstalled"` // Hyper-V 角色已安装（vmms.exe 存在）
	HypervRunning   bool     `json:"hypervRunning"`   // 管理程序已在跑（WMI 查不到则为 false）
	HypervSupported bool     `json:"hypervSupported"` // 系统版本具备 Hyper-V 能力（Pro/Ent/Edu）
	EditionID       string   `json:"editionId"`       // Professional / Enterprise / Core …
}

// SetupProgress 安装链路进度事件负载（"docker:setup"，语义对齐 mysql:install）
type SetupProgress struct {
	Stage    string `json:"stage"` // info|downloading|installing|starting|done|error
	Percent  int    `json:"percent"`
	Note     string `json:"note"`
	Received int64  `json:"received"`
	Total    int64  `json:"total"`
}

// ProbeInfo 安装包探测结果：绑定面一次返回多个字段，免得前端拆多元组
type ProbeInfo struct {
	Url  string `json:"url"`  // 最终生效的直链
	Name string `json:"name"` // 落盘文件名
	Size int64  `json:"size"` // 总体积，-1 表示服务端未给
}

/* ============================ 事件注入 ============================ */

var emitter func(name string, data ...any)

// SetEmitter 注入事件发射器（绑定 Wails v3 app.Event.Emit）。
func SetEmitter(fn func(name string, data ...any)) { emitter = fn }

func emit(name string, payload any) {
	if emitter != nil {
		emitter(name, payload)
	}
}
