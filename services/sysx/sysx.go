// Package sysx 是平台适配层：把"Windows 有、Linux 没有"的那几件事收口成接口。
//
// 设计原则（改代码前先看这三条）：
//  1. 差异靠**编译期**解决，不靠运行期 `if os == "windows"`：
//     进程托管、凭据加密、打开目录这类会碰到平台专属 API 的能力，
//     一律用 build tag 各写一份实现，业务层只认接口。这样对方平台的代码
//     不会进二进制，也不会出现"死分支长期没人跑"的腐化。
//  2. 只有**运行时事实**才做判断：有没有 systemd、是不是 root、docker 在不在。
//     这类探测只在 detectCaps() 里做一次，结果缓存进 Caps()，全项目共享。
//  3. Caps 只用来做「能力声明」（日志、面板显示哪些按钮、选哪个驱动），
//     不要拿它去分叉业务逻辑——业务逻辑应当是平台无关的。
//
// 目前涵盖：Proc（服务进程托管）/ Pkg（版本包安装与解包）/ Cred（敏感串落盘加密）/ Open（打开目录与链接）。
package sysx

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
)

// ErrUnsupported 表示当前平台不支持该操作（调用方应自行降级或提示）。
var ErrUnsupported = errors.New("当前平台不支持该操作")

// Capabilities 平台能力快照（进程内唯一、启动时探测一次）。
//
// 序列化字段稳定，可直接作为面板 /api/caps 的返回体，前端按能力渲染，
// 而不是按 os 字符串硬编码分支。
type Capabilities struct {
	OS         string `json:"os"`         // windows / linux / darwin
	Arch       string `json:"arch"`       // amd64 / arm64 ...
	InitSystem string `json:"initSystem"` // systemd / direct（Windows 恒为 direct）
	IsRoot     bool   `json:"isRoot"`     // Linux 上是否 root（决定能否写 systemd unit）
	HasDocker  bool   `json:"hasDocker"`  // 能否连上 Docker Engine
	PkgMode    string `json:"pkgMode"`    // green：自带绿色版；system：系统包；hybrid：优先系统包再回落自带
	// NativeWindows 表示当前构建能真正弹出原生窗口。
	//
	// Windows 桌面版为 true；-tags server 的 Web 运行模式与 Linux 上都是 false——
	// Wails 在这两种构建里照样编译通过、NewWithOptions 也不报错，只是窗口不会真的出现，
	// 前端必须靠这个字段决定"开新窗口"还是"页面内路由"。
	NativeWindows bool `json:"nativeWindows"`
}

// String 便于日志一行打全平台信息。
func (c Capabilities) String() string {
	return fmt.Sprintf("os=%s arch=%s init=%s root=%v docker=%v pkg=%s",
		c.OS, c.Arch, c.InitSystem, c.IsRoot, c.HasDocker, c.PkgMode)
}

// Desktop 表示这是"能弹原生窗口"的桌面平台（决定浮动球/热键/内嵌浏览器等能力是否可用）。
func (c Capabilities) Desktop() bool { return c.OS == "windows" }

// Systemd 表示进程托管走 systemd（需要 root，且真的装了 systemd）。
func (c Capabilities) Systemd() bool { return c.InitSystem == "systemd" }

var (
	capsOnce sync.Once
	capsVal  Capabilities
)

// Caps 返回平台能力快照（首次调用时探测并缓存）。
func Caps() Capabilities {
	capsOnce.Do(func() {
		capsVal = detectCaps()
		if capsVal.OS == "" {
			capsVal.OS = runtime.GOOS
		}
		if capsVal.Arch == "" {
			capsVal.Arch = runtime.GOARCH
		}
	})
	return capsVal
}

// ============================================================
// Proc：服务进程托管
// ============================================================

// ProcSpec 一个受管服务的启动规格。
type ProcSpec struct {
	ID        string            // 实例唯一标识（同一 Service 下唯一即可）
	Service   string            // 归属服务名（nginx/mysql/redis/php/npm…，决定日志目录）
	Bin       string            // 可执行文件绝对路径
	Args      []string          // 启动参数
	Dir       string            // 工作目录
	Env       map[string]string // 追加环境变量
	LogFile   string            // 输出落盘文件；空则用 <logs>/<service>/<id>.log
	AutoStart bool              // systemd 下是否 enable（开机自启）
}

// ProcStatus 受管服务的运行状态。
type ProcStatus struct {
	ID       string `json:"id"`
	Running  bool   `json:"running"`
	PID      int    `json:"pid"`
	ExitCode int    `json:"exitCode"` // 仅直接托管且已退出时有意义
	Backend  string `json:"backend"`  // direct / systemd，便于面板展示与排障
	Message  string `json:"message"`  // 附加说明（错误原因等）
}

// Proc 服务进程托管。
//
// 两套实现共用同一份接口：
//   - direct：面板自己 exec 子进程（Windows 默认；Linux 无 systemd 或非 root 时回落）
//   - systemd：生成 unit 交给 systemd（Linux + root 时首选，日志/开机自启/cgroup 免费拿到）
type Proc interface {
	// Ensure 登记规格并做好准备（systemd 下写 unit 文件 + daemon-reload；direct 下建日志目录）。
	Ensure(spec ProcSpec) error
	Start(id string) error
	Stop(id string) error
	// Reload 平滑重载；底层不支持时退化为重启，调用方不必区分。
	Reload(id string) error
	Status(id string) (ProcStatus, error)
	// Logs 返回最后 n 行输出（n <= 0 时给默认行数）。
	Logs(id string, lines int) ([]string, error)
	// Remove 停止并删除登记（日志保留）。
	Remove(id string) error
}

var (
	procOnce sync.Once
	procVal  Proc
)

// NewProc 返回进程托管实现（进程内单例，内部保有序号/规格状态）。
func NewProc() Proc {
	procOnce.Do(func() { procVal = platformProc() })
	return procVal
}

// ============================================================
// Pkg：版本包安装（自带绿色版这一套）
// ============================================================

// Version 插件目录下的一个版本。
type Version struct {
	Name      string `json:"name"`      // 目录名即版本号
	Dir       string `json:"dir"`       // 绝对路径
	Installed bool   `json:"installed"` // 有 .go-tools-installed 标记 = 在线/离线安装版（可删）；否则为发布内置版
}

// Pkg 负责 "<plugins>/<service>/<version>" 这套目录约定：
// 解包、写安装标记、列版本、删除。各工具只管下载与业务，不再各自复制一遍这套逻辑。
type Pkg interface {
	// PluginsDir 返回某服务的插件根目录（不存在则创建）。
	PluginsDir(service string) (string, error)
	// Versions 列出某服务已就位的版本（按版本号排序）。
	Versions(service string) ([]Version, error)
	// Install 把归档解包到 <plugins>/<service>/<version> 并写安装标记，返回目标目录。
	// 归档支持 .zip / .tar.gz / .tgz；失败会清理半截目录（不留脏数据）。
	Install(service, version, archive string) (string, error)
	// Remove 删除一个安装版（内置版必须由调用方先行拦截）。
	Remove(service, version string) error
}

// NewPkg 返回基于全局 appdirs 布局的包管理器。
func NewPkg() Pkg { return newPkg() }

// ============================================================
// Cred：敏感串落盘加密
// ============================================================

// Cred 敏感短串（仓库密码、令牌）的落盘加密。
//
//   - Windows：DPAPI（CryptProtectData），密文只能被同一用户在同一台机器解开
//   - 其它：文件权限 0600（Linux 下 chmod 后仅属主可读）；若将来接 libsecret 直接替换实现即可
type Cred interface {
	Save(path string, secret []byte) error
	Load(path string) ([]byte, error)
	// Delete 不存在时不算错误。
	Delete(path string) error
}

// NewCred 返回平台对应的加密实现。
func NewCred() Cred { return platformCred() }

// ============================================================
// Opener：打开目录 / URL
// ============================================================

// Opener 用系统默认程序打开目录或链接（替代直接调 explorer / xdg-open）。
type Opener interface {
	Dir(path string) error
	URL(url string) error
}

// Open 返回平台对应的打开器。
func Open() Opener { return platformOpen() }
