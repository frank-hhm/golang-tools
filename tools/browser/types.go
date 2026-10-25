// Package browser 把 Chromium（WebView2）网页区嵌进本程序自己的窗口，
// 让"自研浏览器"的地址栏/标签栏由前端画、网页由独立的 WebView2 环境渲染。
//
// 与 Wails 自带 webview 的区别：
//   - Wails v3 beta 的 WebView2 环境是应用级的（所有窗口共享一份 user-data-dir 与代理），
//     这里每个"空间"自己建一份 environment：独立 profile 目录 + 独立代理参数，
//     因此登录态/缓存/出口 IP 天然隔离。
//   - 网页区是同一窗口 HWND 上的第二个 controller，矩形由前端量出来交给 SetBounds。
//
// 本包只做"宿主"这一件事：开/关空间、导航、改尺寸、开 DevTools、上报状态与资源统计。
package browser

import (
	"errors"
	"sync"
)

// Rect 网页区矩形，单位是 DIP（= 前端 CSS 像素，按 100% 缩放换算），
// 原点为父窗口客户区左上角。
type Rect struct {
	X int
	Y int
	W int
	H int
}

// Options 打开一个网页空间所需的一切。
type Options struct {
	// ID 空间标识，同时用作 profile 目录名（由调用方保证合法文件名）。
	ID string
	// DataPath profile 目录（空 = 与 ID 同名的临时目录，由调用方给出更好）。
	DataPath string
	// BrowserPath 固定版 WebView2 运行时目录（空 = 用系统安装的运行时）。
	BrowserPath string
	// Proxy 代理服务器，如 http://127.0.0.1:7890 / socks5://127.0.0.1:1080（空 = 直连）。
	Proxy string
	// ProxyBypass 代理绕过列表（Chrome 语法，如 "localhost,127.0.0.1"）。
	ProxyBypass string
	// UserAgent 自定义 UA（空 = 默认）。
	UserAgent string
	// DisableWebSecurity 关掉同源策略（仅调试空间建议开启，需重启空间生效）。
	DisableWebSecurity bool

	// HWND 承载网页区的父窗口句柄。
	HWND uintptr
	// Bounds 初始矩形。
	Bounds Rect
	// URL 初始地址（空 = about:blank）。
	URL string
}

// Event 网页区上报的状态变化。
//
// Kind 取值：
//   - "ready"    控制器就绪（可以开始导航/改尺寸）
//   - "title"    Title 有效（导航完成后从页面里取回的 document.title）
//   - "error"    Message 有效
//   - "crash"    渲染进程崩溃（Message 为原因）
type Event struct {
	Kind    string
	SpaceID string
	URL     string
	Title   string
	Message string
}

// Stat 资源占用快照（基线测量用）。
type Stat struct {
	// Spaces 当前打开的空间数。
	Spaces int `json:"spaces"`
	// Processes 属于本程序空间的 WebView2 进程数（浏览器主进程 + 它派生出的
	// GPU/渲染/工具进程），不包含本机其它应用的 WebView2。
	Processes int `json:"processes"`
	// RSSBytes 上述进程常驻内存合计。
	RSSBytes uint64 `json:"rssBytes"`
}

// ErrUnsupported 平台不支持（非 Windows）。
var ErrUnsupported = errors.New("当前平台不支持嵌入式浏览器")

// Manager 管理多个网页空间。零值不可用，用 NewManager 构造。
type Manager struct {
	mu      sync.RWMutex
	spaces  map[string]*space
	OnEvent func(Event)
}

// NewManager 创建空间管理器。
func NewManager() *Manager {
	return &Manager{spaces: make(map[string]*space)}
}

func (m *Manager) emit(ev Event) {
	if m.OnEvent != nil {
		m.OnEvent(ev)
	}
}

func (m *Manager) count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.spaces)
}
