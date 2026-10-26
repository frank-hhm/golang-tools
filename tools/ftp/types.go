package ftp

// ============================================================
// 文件传输（Xftp 式）公共类型
//
// M1 只落地客户端 SFTP：连接记录 → 双栏浏览 → 上传/下载/删除/重命名/新建目录/预览。
// 服务端（虚拟账号 + 端口对外）在 M2 追加，类型里预留协议与服务端字段，
// 避免后续改落盘结构。
// ============================================================

const (
	// ProtoSFTP SFTP（SSH 子系统）；FTP/FTPS 在 M3 接入
	ProtoSFTP = "sftp"

	// AuthPassword 密码登录；AuthKey 私钥登录（带口令的私钥暂不支持）
	AuthPassword = "password"
	AuthKey      = "key"

	// 传输方向
	DirUp   = "up"   // 本地上传到远端
	DirDown = "down" // 远端下载到本地

	// 传输状态
	StateQueued   = "queued"
	StateRunning  = "running"
	StateDone     = "done"
	StateFailed   = "failed"
	StateCanceled = "canceled"

	// EventTransfer 传输任务快照（前端按 id upsert）
	EventTransfer = "ftp:transfer"
	// EventSession 会话关闭通知（远端断开 / 主动关闭）
	EventSession = "ftp:session"
)

// Connection 一条远程连接记录（密码明文只在上送时出现，落盘前即加密）
type Connection struct {
	ID        string `json:"id"`
	Name      string `json:"name"`                // 列表标题；为空时用 备注/主机
	Proto     string `json:"proto"`               // sftp | ftp | ftps
	Host      string `json:"host"`                // IP 或域名
	Port      int    `json:"port"`                // SFTP 默认 22
	User      string `json:"user"`                // 登录账号
	Auth      string `json:"auth"`                // password | key
	Password  string `json:"password,omitempty"`  // 明文只在此刻出现
	KeyPath   string `json:"keyPath,omitempty"`   // 私钥文件路径
	Charset   string `json:"charset,omitempty"`   // UTF-8 / GBK（FTP 阶段用）
	Passive   bool   `json:"passive,omitempty"`   // 被动模式（FTP 阶段用）
	RemoteDir string `json:"remoteDir,omitempty"` // 打开后默认进入的远端目录
	Note      string `json:"note,omitempty"`      // 备注
	UpdatedAt int64  `json:"updatedAt"`
}

// SessionInfo 打开文件管理后回给前端的句柄
type SessionInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Host  string `json:"host"`
	Port  int    `json:"port"`
	User  string `json:"user"`
	Home  string `json:"home"` // 登录后的起始目录（远端 pwd）
}

// SessionEvent 会话关闭；Reason 直接给前端当提示语
type SessionEvent struct {
	SessionID string `json:"sessionId"`
	Reason    string `json:"reason"`
}

// Entry 远端目录项。字段刻意与 tools/disk.Entry 对齐，
// 前端双栏可以共用同一套表格列与排序逻辑。
type Entry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`    // 目录固定 0
	ModTime string `json:"modTime"` // "2006-01-02 15:04:05"
	Mode    string `json:"mode"`    // 权限位，"755"/"644"
	Hidden  bool   `json:"hidden"`
	Link    bool   `json:"link"`
	Ext     string `json:"ext"` // 小写扩展名（不含点）
}

// Part 远端路径面包屑的一段
type Part struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Listing 一次远端目录浏览结果（与 disk.Listing 同构，去掉盘符信息）
type Listing struct {
	Path    string  `json:"path"`
	Parent  string  `json:"parent"` // 已在根时为 ""
	Parts   []Part  `json:"parts"`
	Entries []Entry `json:"entries"`
	Dirs    int     `json:"dirs"`
	Files   int     `json:"files"`
	Size    int64   `json:"size"` // 当前层文件大小合计
}

// Transfer 一个传输任务（同时也是 ftp:transfer 事件的负载）
type Transfer struct {
	ID      string `json:"id"`
	Dir     string `json:"dir"`     // up | down
	Name    string `json:"name"`    // 展示名（文件名或目录名）
	Local   string `json:"local"`   // 本地绝对路径
	Remote  string `json:"remote"`  // 远端绝对路径
	Total   int64  `json:"total"`   // 源大小（目录或未知时为 0）
	Done    int64  `json:"done"`    // 已传字节
	Speed   int64  `json:"speed"`   // 字节/秒
	State   string `json:"state"`   // queued/running/done/failed/canceled
	Err     string `json:"err"`     // 失败原因
	Started int64  `json:"started"` // Unix 秒
	Ended   int64  `json:"ended"`   // Unix 秒，未结束为 0
}

// Preview 远端文件预览结果
type Preview struct {
	Kind      string `json:"kind"`      // text | image | binary | tooLarge
	Data      string `json:"data"`      // text 正文，或图片 data URL
	Size      int64  `json:"size"`      // 文件字节数
	Truncated bool   `json:"truncated"` // text 是否因超限被截断
}
