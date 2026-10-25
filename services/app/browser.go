//go:build windows

package app

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"go-tools/services/applog"
	"go-tools/services/config"
	"go-tools/tools/browser"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ============================================================
// 自研浏览器：网页区宿主
//
// 分工：
//   - 前端（/tools/browser 页面）画地址栏、标签栏、空间切换，并把自己量出的
//     网页区矩形（CSS 像素，相对视口）交给这里；
//   - Go 侧（tools/browser）在该窗口的 HWND 上挂一个 WebView2 controller，
//     每个“空间”独占一份 WebView2 environment（独立 profile + 独立代理）。
//
// 窗口定位：前端把自己的窗口名（runtime Window.Name()）传进来，这里用
// GetByName 拿到 application.Window，再取 NativeWindow() 的 HWND。
// 不依赖窗口标题，也不依赖“最后激活的窗口”这种竞态来源。
// ============================================================

// browserEventName 网页区状态推送事件名（前端 Events.On 订阅）
const browserEventName = "browser:event"

// browserMu 串行化浏览器管理器的惰性初始化（设置页改代理后需要重建）。
var (
	browserMu    sync.Mutex
	browserMgr   *browser.Manager
	browserReady bool // 运行时版本已探测且可用
	browserVer   string
)

// BrowserRuntime 返回 WebView2 运行时版本；返回空串表示本机不可用（前端据此提示）。
func (a *App) BrowserRuntime() string {
	browserMu.Lock()
	defer browserMu.Unlock()
	if !browserReady {
		browserVer = browser.DetectRuntimeVersion()
		browserReady = true
	}
	return browserVer
}

// BrowserRuntimeInfo 返回运行时版本与浏览器配置里当前空间数量（页面顶部状态条用）。
func (a *App) BrowserRuntimeInfo() map[string]any {
	cfg := config.CurrentConfig().Browser
	return map[string]any{
		"runtime":      a.BrowserRuntime(),
		"spaces":       len(cfg.Spaces),
		"defaultSpace": cfg.DefaultSpace,
	}
}

// BrowserSpaces 返回浏览器全局配置里的空间列表（含代理等，供页面切换空间时展示）。
func (a *App) BrowserSpaces() []config.BrowserSpace {
	return config.CurrentConfig().Browser.Spaces
}

// BrowserAttach 让指定空间挂到调用方的窗口上：首次调用会创建 WebView2 环境
// （冷启动约 200~400ms），之后同一空间只做矩形更新与导航。
//
// spaceID 为空时用配置里的默认空间；windowName 为空时退回单例窗口名。
// x/y/w/h 为网页区矩形（CSS 像素，相对窗口客户区）。
func (a *App) BrowserAttach(spaceID, windowName string, x, y, w, h int) error {
	cfg := config.CurrentConfig()
	if !cfg.Browser.Enabled {
		return fmt.Errorf("浏览器已在设置中关闭")
	}
	win, err := a.browserWindow(windowName)
	if err != nil {
		return err
	}
	hwnd := uintptr(win.NativeWindow())
	if hwnd == 0 {
		return fmt.Errorf("窗口句柄不可用")
	}

	space, ok := config.BrowserSpaceByID(cfg, spaceID)
	if !ok {
		return fmt.Errorf("空间不存在: %s", spaceID)
	}
	profile, err := config.BrowserProfileDir(space)
	if err != nil {
		return fmt.Errorf("准备空间目录失败: %w", err)
	}

	home := strings.TrimSpace(space.Homepage)
	if home == "" {
		home = cfg.Browser.Homepage
	}

	m := a.ensureBrowserManager()
	start := time.Now()
	err = m.Open(browser.Options{
		ID:                 space.ID,
		DataPath:           profile,
		Proxy:              space.Proxy,
		ProxyBypass:        space.ProxyBypass,
		UserAgent:          space.UserAgent,
		DisableWebSecurity: space.DisableWebSecurity,
		HWND:               hwnd,
		Bounds:             browser.Rect{X: x, Y: y, W: w, H: h},
		URL:                home,
	})
	if err != nil {
		applog.Errorf("browser", "打开空间 %s 失败: %v", space.ID, err)
		return err
	}
	applog.Infof("browser", "空间 %s 就绪，耗时 %s（代理=%q）", space.ID, time.Since(start).Round(time.Millisecond), space.Proxy)
	return nil
}

// BrowserSetBounds 更新网页区矩形（窗口缩放/拖动分隔条时高频调用，走异步投递）。
func (a *App) BrowserSetBounds(spaceID string, x, y, w, h int) error {
	m := a.ensureBrowserManager()
	return m.SetBounds(spaceID, browser.Rect{X: x, Y: y, W: w, H: h})
}

// BrowserNavigate 导航到 url（http/https/file 或 about:blank）。
func (a *App) BrowserNavigate(spaceID, url string) error {
	m := a.ensureBrowserManager()
	return m.Navigate(spaceID, url)
}

// BrowserDevTools 打开该空间的开发者工具（独立窗口）。
func (a *App) BrowserDevTools(spaceID string) error {
	m := a.ensureBrowserManager()
	return m.DevTools(spaceID)
}

// BrowserEval 在页面里执行一段脚本（地址栏取标题、读取选中文本等轻量操作用）。
func (a *App) BrowserEval(spaceID, script string) error {
	m := a.ensureBrowserManager()
	return m.Eval(spaceID, script)
}

// BrowserClose 关闭一个空间（释放 profile 锁与内存）；页面切空间/退出时调用。
func (a *App) BrowserClose(spaceID string) {
	browserMu.Lock()
	m := browserMgr
	browserMu.Unlock()
	if m != nil {
		m.Close(spaceID)
	}
}

// BrowserCloseAll 关闭全部空间。
func (a *App) BrowserCloseAll() {
	browserMu.Lock()
	m := browserMgr
	browserMu.Unlock()
	if m != nil {
		m.CloseAll()
	}
}

// BrowserStats 返回资源占用快照：空间数 + 本程序空间下的 WebView2 进程数与常驻内存。
func (a *App) BrowserStats() browser.Stat {
	m := a.ensureBrowserManager()
	return m.Stats()
}

// BrowserOpenWindow 打开浏览器工具窗口（首页卡片/托盘菜单统一入口，
// 前端也可直接调，省得各处拼参数）。
func (a *App) BrowserOpenWindow() error {
	cfg := config.CurrentConfig()
	name := "浏览器"
	for _, t := range cfg.Tools {
		if t.Key == "browser" {
			name = t.Name
			break
		}
	}
	return a.OpenToolWindow("/tools/browser", name, true, true, 0, 0)
}

// ensureBrowserManager 惰性创建空间管理器，并把空间事件转发给前端。
func (a *App) ensureBrowserManager() *browser.Manager {
	browserMu.Lock()
	defer browserMu.Unlock()
	if browserMgr == nil {
		m := browser.NewManager()
		m.OnEvent = func(ev browser.Event) {
			if a.app == nil {
				return
			}
			a.app.Event.Emit(browserEventName, ev)
		}
		browserMgr = m
	}
	return browserMgr
}

// browserWindow 按窗口名取窗口：优先前端传入的当前窗口名，
// 退回单例窗口名（OpenToolWindow 用 "win:"+路由 命名）。
func (a *App) browserWindow(name string) (application.Window, error) {
	if a.app == nil {
		return nil, fmt.Errorf("应用尚未初始化")
	}
	if n := strings.TrimSpace(name); n != "" {
		if w, ok := a.app.Window.GetByName(n); ok {
			return w, nil
		}
	}
	if w, ok := a.app.Window.GetByName("win:/tools/browser"); ok {
		return w, nil
	}
	return nil, fmt.Errorf("未找到浏览器窗口（请从工具菜单打开）")
}
