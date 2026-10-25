// Package browser 把 Chromium（WebView2）网页区嵌进本程序自己的窗口，
// 让"自研浏览器"的地址栏/标签栏由前端画、网页由独立的 WebView2 环境渲染。
//
// 与 Wails 自带 webview 的区别：
//   - Wails v3 beta 的 WebView2 环境是应用级的（所有窗口共享一份 user-data-dir 与代理），
//     这里每个"空间"自己建一份 environment：独立 profile 目录 + 独立代理参数，
//     因此登录态/缓存/出口 IP 天然隔离。
//   - 网页区是同一窗口 HWND 上的额外 controller，矩形由前端量出来交给 SetBounds。
//
// 层级关系（M2 起支持多标签）：
//
//	Wails 窗口 HWND
//	  └─ 空间宿主子窗口（每个空间一个，负责 z-order 与整体矩形）
//	       └─ 标签宿主子窗口（每个标签一个，同时只显示活动标签）
//	            └─ WebView2 controller（共享所在空间的 environment）
//
// 本包只做"宿主"这一件事：开/关空间与标签、导航、改尺寸、开 DevTools、上报状态与资源统计。
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
	// Proxy 代理服务器，如 http://127.0.0.1:7890 / socks5://127.0.0.1:1080。
	Proxy string
	// ProxyMode 代理模式：system（跟着系统，默认）/ direct（显式直连）/ custom（用 Proxy）。
	ProxyMode string
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
	// URL 初始地址（空 = about:blank）；同时也是该空间新建标签的默认首页。
	URL string
	// MaxTabs 该空间允许的标签数上限（0 = 用 DefaultMaxTabs）。
	MaxTabs int
}

// TabInfo 一个标签的当前状态快照（纯数据，可从任意线程读取）。
type TabInfo struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	URL          string `json:"url"`
	Active       bool   `json:"active"`
	Loading      bool   `json:"loading"`
	CanGoBack    bool   `json:"canGoBack"`
	CanGoForward bool   `json:"canGoForward"`
	// Suspended 为 true 表示该标签已被 WebView2 挂起（内存已释放大半）。
	Suspended bool `json:"suspended"`
	// Error 非空表示该标签创建失败，前端可据此展示错误页。
	Error string `json:"error,omitempty"`
}

// SpaceInfo 一个空间的标签快照。
type SpaceInfo struct {
	ID        string    `json:"id"`
	ActiveTab string    `json:"activeTab"`
	Tabs      []TabInfo `json:"tabs"`
	// MaxTabs 该空间的标签上限，前端用来决定「新建」按钮是否禁用。
	MaxTabs int `json:"maxTabs"`
}

// Event 网页区上报的状态变化。
//
// Kind 取值：
//   - "ready"     空间与第一个标签就绪
//   - "tabs"      标签结构变化（新建/关闭/激活/加载完成），Tabs 有效
//   - "state"     单个标签的地址/标题/前进后退状态变化，TabID/URL/Title 有效
//   - "title"     显式的标题回报（页面脚本 postMessage 回来）
//   - "shortcut"  网页区里按下了宿主级快捷键，Message 为动作名（newtab/close/reload/focusurl…）
//   - "newWindow" 页面请求开新窗口，URL 有效（宿主已自动转成新标签）
//   - "error"     Message 有效
//   - "crash"     渲染进程崩溃（Message 为原因）
type Event struct {
	Kind    string `json:"kind"`
	SpaceID string `json:"spaceId"`
	TabID   string `json:"tabId,omitempty"`
	URL     string `json:"url,omitempty"`
	Title   string `json:"title,omitempty"`
	Message string `json:"message,omitempty"`
	// Tabs 仅在 Kind == "tabs" 时有效。
	Tabs      []TabInfo `json:"tabs,omitempty"`
	ActiveTab string    `json:"activeTab,omitempty"`
}

// Stat 资源占用快照（基线测量用）。
type Stat struct {
	// Spaces 当前打开的空间数。
	Spaces int `json:"spaces"`
	// Tabs 当前打开的标签总数。
	Tabs int `json:"tabs"`
	// Processes 属于本程序空间的 WebView2 进程数（浏览器主进程 + 它派生出的
	// GPU/渲染/工具进程），不包含本机其它应用的 WebView2。
	Processes int `json:"processes"`
	// RSSBytes 上述进程常驻内存合计。
	RSSBytes uint64 `json:"rssBytes"`
}

// DefaultMaxTabs 单个空间的标签数默认上限（Options.MaxTabs 为 0 时生效）。
const DefaultMaxTabs = 20

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
