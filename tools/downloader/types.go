// Package downloader HTTP/HTTPS 分块下载引擎：多连接 Range 分块 + 断点续传 +
// 队列/限速/重试/分类/计划任务，是「下载管理」工具页与仿站/镜像拉取等复用的内核。
//
// 对标网际快车 FlashGet：一个大文件切成 N 段并发下、各段 WriteAt 落偏移；
// 服务端不支持 Range 时退化单连接流式写 .part 再 rename。进度通过注入的 emitter
// 发 "download:progress"（负载就是 Task），顺带进 services/tasks 任务中心，
// 于是离开页面也看得到进度、终态还能弹通知。
//
// 全跨平台零 cgo：Linux 面板里下载的是服务器本机 SaveDir，语义一致。
// 绑定层在 services/app/download.go，页面在 frontend/src/views/tools/download。
package downloader

// 任务状态机（Task.Status）。
const (
	StatusPending  = "pending"  // 已添加未开始
	StatusQueued   = "queued"   // 排队等并发额度
	StatusRunning  = "running"  // 下载中
	StatusPaused   = "paused"   // 用户暂停，保留已下部分与 .gtdl.json
	StatusDone     = "done"     // 完成
	StatusError    = "error"    // 致命错误（重试耗尽或 4xx）
	StatusCanceled = "canceled" // 用户停止，清理临时文件
)

// 任务事件名（负载 Task）。注册进 main 的 RegisterEvent，并接进 services/tasks。
const EventProgress = "download:progress"

// Part 单个分块的进度。End=-1 表示总长未知（单流退化时只有一个这样的 part）。
type Part struct {
	Index int     `json:"index"`
	Start int64   `json:"start"`
	End   int64   `json:"end"`  // 闭区间末字节；-1=未定长
	Done  int64   `json:"done"` // 该块已下载字节
	Speed float64 `json:"speed"`
}

// Task 一笔下载任务。字段刻意扁平（供 Wails 生成前端模型，嵌套越少越好用）。
//
// Stage 是给 services/tasks 记账用的相位标签（downloading|done|error|canceled|
// stalled|info），与 Status 并行存在：前端下载页看 Status，任务中心看 Stage。
type Task struct {
	ID          string   `json:"id"`
	URL         string   `json:"url"`
	Name        string   `json:"name"`
	SavePath    string   `json:"savePath"`
	Category    string   `json:"category"`
	Size        int64    `json:"size"`
	Downloaded  int64    `json:"downloaded"`
	Status      string   `json:"status"`
	Stage       string   `json:"stage"`
	Connections int      `json:"connections"`
	Speed       float64  `json:"speed"`
	Percent     int      `json:"percent"`
	Proxy       string   `json:"proxy"`
	Mirrors     []string `json:"mirrors"`
	Source      string   `json:"source"` // 当前实际使用的源 URL（多镜像时）
	Parts       []Part   `json:"parts"`
	Err         string   `json:"err"`
	Note        string   `json:"note"`
	StartedAt   int64    `json:"startedAt"`
	UpdatedAt   int64    `json:"updatedAt"`
	EndedAt     int64    `json:"endedAt"`
	ScheduledAt int64    `json:"scheduledAt"` // >0：到点自动开始（unix 毫秒）
	RetryCount  int      `json:"retryCount"`
	AutoStart   bool     `json:"autoStart"`
}

// Options 引擎级默认参数与护栏。零值字段由 New 填默认。
type Options struct {
	MaxTotalParts       int   // 全局并发块数上限（信号量容量）
	MaxRunningTasks     int   // 同时下载的任务数上限，超了排队
	DefaultConnections  int   // 单任务默认分块数
	GlobalSpeedLimitBps int64 // 全局下行限速（字节/秒），0=不限
	MinSegmentedSize    int64 // 小于此大小不分块（单连接即可），默认 4MB
}

// 默认护栏：都是「防跑飞 / 防噎绑定层」的取值，不是功能上限。
const (
	defaultMaxTotalParts    = 32
	defaultMaxRunningTasks  = 3
	defaultConnections      = 4
	defaultMinSegmentedSize = int64(4 << 20) // 4MB
	defaultTimeoutSec       = 30
	defaultMaxRetry         = 5 // 单块重试上限
	defaultUA               = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	maxRedirect             = 10
	probeReadBytes          = 64 << 10 // 展速探测读取的样本字节：64KB
	metaSuffix              = ".gtdl.json"
	partSuffix              = ".part"
)

// 分类表：按扩展名映射 FlashGet 风格的固定分类。写死一张表，够用且零歧义。
var categoryByExt = map[string]string{
	"zip": "压缩", "rar": "压缩", "7z": "压缩", "gz": "压缩", "tar": "压缩", "bz2": "压缩", "xz": "压缩",
	"pdf": "文档", "doc": "文档", "docx": "文档", "xls": "文档", "xlsx": "文档", "ppt": "文档", "pptx": "文档",
	"txt": "文档", "md": "文档", "csv": "文档",
	"mp3": "音乐", "wav": "音乐", "flac": "音乐", "m4a": "音乐", "ogg": "音乐", "aac": "音乐",
	"exe": "程序", "msi": "程序", "dmg": "程序", "deb": "程序", "rpm": "程序", "apk": "程序", "appimage": "程序",
	"mp4": "视频", "mkv": "视频", "avi": "视频", "mov": "视频", "wmv": "视频", "flv": "视频", "webm": "视频",
	"jpg": "图片", "jpeg": "图片", "png": "图片", "gif": "图片", "bmp": "图片", "webp": "图片", "svg": "图片", "ico": "图片",
}

// CategoryOther 未匹配扩展名的兜底分类。
const CategoryOther = "其他"

// categoryOf 按文件名（扩展名）归到固定分类。
func categoryOf(name string) string {
	ext := baseExt(name)
	if c, ok := categoryByExt[ext]; ok {
		return c
	}
	return CategoryOther
}
