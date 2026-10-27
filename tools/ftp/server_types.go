package ftp

// ============================================================
// SFTP 服务端（FileZilla Server 式）公共类型
//
// 服务端完全内嵌在进程里：x/crypto/ssh 做 SSH 层，pkg/sftp 的 RequestServer
// 接 SFTP 子系统，文件系统访问全部经 os.Root 收进账号根目录（防 ../ 与软链逃逸）。
// 虚拟账号对系统用户无依赖，密码只存 bcrypt 哈希。
// ============================================================

const (
	// EventServer 服务端状态快照（启动/停止/账号变更/连接增减都发一次）
	EventServer = "ftp:server"
	// EventServerLog 服务端日志单行
	EventServerLog = "ftp:serverlog"

	// 服务端运行状态
	ServerStopped = "stopped"
	ServerRunning = "running"

	// 日志级别
	LogInfo  = "info"
	LogWarn  = "warn"
	LogError = "error"

	// DefaultServerPort 默认监听端口。避开 22：本机大概率已经有 OpenSSH，
	// 且 1024 以下在 Linux 面板模式下需要 root 才能绑。
	DefaultServerPort = 2222
	// DefaultListenHost 默认监听所有网卡（局域网内可直接连）
	DefaultListenHost = "0.0.0.0"

	// serverLogLimit 服务端内存日志条数
	serverLogLimit = 300
	// serverMaxConns 同时在线的连接上限，防止误暴露后被刷满文件句柄
	serverMaxConns = 32
	// serverMaxHandshaking 认证阶段的并发上限。bcrypt 每次约 50-100ms CPU，
	// 而 ssh 层默认允许 6 次重试，不限并发时任何能连上端口的人都能把进程 CPU 打满
	serverMaxHandshaking = 16
	// serverMaxChannelsPerConn 单条 SSH 连接同时允许的 sftp 通道数
	// （每个通道一个 goroutine + 一个 os.Root 句柄）
	serverMaxChannelsPerConn = 8
)

// ServerConfig 服务端监听配置
type ServerConfig struct {
	Host      string `json:"host"`      // 监听地址，默认 0.0.0.0
	Port      int    `json:"port"`      // 监听端口，默认 2222
	RootDir   string `json:"rootDir"`   // 服务端根目录；空 = <data>/ftp/server/root
	AutoStart bool   `json:"autoStart"` // 应用启动时自动拉起
}

// Account 一个虚拟账号。Password 只在保存时上送，列表里恒为空。
type Account struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Password  string `json:"password,omitempty"` // 明文仅在此刻出现；空 = 沿用原密码
	RootDir   string `json:"rootDir"`            // 服务端根目录下的子目录，"." 表示根
	ReadOnly  bool   `json:"readOnly"`
	Enabled   bool   `json:"enabled"`
	Note      string `json:"note,omitempty"`
	UpdatedAt int64  `json:"updatedAt"`
}

// ServerConn 一条在线连接（SSH 层连接，一个连接内可能有多个 sftp 通道）
type ServerConn struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	ClientIP    string `json:"clientIp"`
	ConnectedAt int64  `json:"connectedAt"` // unix 秒
	ReadOnly    bool   `json:"readOnly"`
}

// ServerStatus 服务端整体状态。事件 ftp:server 与 FtpServerStatus 共用这一种形状。
type ServerStatus struct {
	State     string       `json:"state"` // stopped | running
	Host      string       `json:"host"`
	Port      int          `json:"port"`
	Addr      string       `json:"addr"` // 实际监听地址 running 时才有
	RootDir   string       `json:"rootDir"`
	AutoStart bool         `json:"autoStart"`
	StartedAt int64        `json:"startedAt"`
	Error     string       `json:"error,omitempty"` // 最近一次启动失败原因
	Accounts  int          `json:"accounts"`        // 启用中的账号数
	Conns     []ServerConn `json:"conns"`
	LocalIPs  []string     `json:"localIps"` // 局域网 IPv4，前端拼成 sftp://ip:port
}

// ServerLog 一条服务端日志
type ServerLog struct {
	Seq   int64  `json:"seq"`
	Time  string `json:"time"`  // "2006-01-02 15:04:05"
	Level string `json:"level"` // info | warn | error
	Text  string `json:"text"`
}
