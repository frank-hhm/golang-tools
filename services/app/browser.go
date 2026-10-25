//go:build windows

package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-tools/appdirs"
	"go-tools/services/applog"
	"go-tools/services/config"
	"go-tools/tools/browser"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// ============================================================
// 自研浏览器：网页区宿主
//
// 分工：
//   - 前端（/tools/browser 页面）画标签栏、地址栏、空间切换，并把自己量出的
//     网页区矩形（CSS 像素，相对视口）交给这里；
//   - Go 侧（tools/browser）在该窗口的 HWND 上挂 WebView2 网页区。
//     一个空间 = 一份 environment（独立 profile + 独立代理），空间内多个标签
//     共享这份 environment（登录态互通）。
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

// BrowserRuntimeInfo 返回运行时版本与浏览器配置摘要（页面顶部状态条用）。
func (a *App) BrowserRuntimeInfo() map[string]any {
	cfg := config.CurrentConfig().Browser
	return map[string]any{
		"runtime":      a.BrowserRuntime(),
		"spaces":       len(cfg.Spaces),
		"maxSpaces":    cfg.MaxSpaces,
		"defaultSpace": cfg.DefaultSpace,
		"searchURL":    cfg.SearchURL,
		"homepage":     cfg.Homepage,
		"proxyMode":    cfg.ProxyMode,
	}
}

// BrowserSpaces 返回浏览器全局配置里的空间列表（含代理等，供页面展示与切换）。
func (a *App) BrowserSpaces() []config.BrowserSpace {
	return config.CurrentConfig().Browser.Spaces
}

// BrowserConfig 返回整份浏览器配置（设置抽屉打开时拉一次）。
func (a *App) BrowserConfig() config.BrowserConfig {
	return config.CurrentConfig().Browser
}

// BrowserSaveConfig 保存浏览器设置（全局默认 + 空间列表 + 空间上限）。
//
// 走 config.Update 的读—改—写，不会覆盖其它设置的并发修改。
// 保存后不自动重建已经开着的空间：代理/UA 这类是 environment 级参数，
// 必须关掉重开才生效，由前端在保存后调用 BrowserRebuild 明确触发。
func (a *App) BrowserSaveConfig(in config.BrowserConfig) error {
	cur := config.CurrentConfig()
	in.Enabled = cur.Browser.Enabled
	if len(in.Spaces) > 0 && in.MaxSpaces > 0 && len(in.Spaces) > in.MaxSpaces {
		return fmt.Errorf("空间数量超过上限（%d）", in.MaxSpaces)
	}
	_, err := config.Update(func(cfg *config.AppConfig) {
		cfg.Browser = in
	})
	if err != nil {
		return err
	}
	applog.Infof("browser", "浏览器设置已保存：%d 个空间，上限 %d", len(config.CurrentConfig().Browser.Spaces), config.CurrentConfig().Browser.MaxSpaces)
	return nil
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

	space, ok := config.ResolveBrowserSpace(cfg, spaceID)
	if !ok {
		return fmt.Errorf("空间不存在: %s", spaceID)
	}
	profile, err := config.BrowserProfileDir(space)
	if err != nil {
		return fmt.Errorf("准备空间目录失败: %w", err)
	}

	m := a.ensureBrowserManager()
	start := time.Now()
	err = m.Open(browser.Options{
		ID:                 space.ID,
		DataPath:           profile,
		ProxyMode:          space.ProxyMode,
		Proxy:              space.Proxy,
		ProxyBypass:        space.ProxyBypass,
		UserAgent:          space.UserAgent,
		DisableWebSecurity: space.DisableWebSecurity,
		HWND:               hwnd,
		Bounds:             browser.Rect{X: x, Y: y, W: w, H: h},
		URL:                strings.TrimSpace(space.Homepage),
	})
	if err != nil {
		applog.Errorf("browser", "打开空间 %s 失败: %v", space.ID, err)
		return err
	}
	applog.Infof("browser", "空间 %s 就绪，耗时 %s（代理=%s %q）",
		space.ID, time.Since(start).Round(time.Millisecond), space.ProxyMode, space.Proxy)
	return nil
}

// BrowserSetBounds 更新网页区矩形（窗口缩放/拖动分隔条时高频调用，走异步投递）。
func (a *App) BrowserSetBounds(spaceID string, x, y, w, h int) error {
	m := a.ensureBrowserManager()
	return m.SetBounds(spaceID, browser.Rect{X: x, Y: y, W: w, H: h})
}

// BrowserSetVisible 显示/隐藏网页区。
//
// 网页区是原生窗口，会盖住前端的一切覆盖层：打开设置抽屉、弹窗类 UI 时
// 前端先把它藏起来，关掉再显示。空间未创建时静默成功（没有可藏的东西）。
func (a *App) BrowserSetVisible(spaceID string, visible bool) error {
	m := a.ensureBrowserManager()
	if err := m.SetVisible(spaceID, visible); err != nil {
		if !visible || strings.Contains(err.Error(), "不存在") {
			return nil
		}
		return err
	}
	return nil
}

// BrowserNavigate 导航当前活动标签（支持 http/https/file/about:blank）。
func (a *App) BrowserNavigate(spaceID, url string) error {
	m := a.ensureBrowserManager()
	return m.Navigate(spaceID, url)
}

// BrowserNewTab 新建标签，返回标签 ID。url 为空时用空间首页。
func (a *App) BrowserNewTab(spaceID, url string, activate bool) (string, error) {
	m := a.ensureBrowserManager()
	return m.NewTab(spaceID, url, activate)
}

// BrowserCloseTab 关闭标签；关掉最后一个会自动开一个首页标签。
func (a *App) BrowserCloseTab(spaceID, tabID string) error {
	m := a.ensureBrowserManager()
	return m.CloseTab(spaceID, tabID)
}

// BrowserActivateTab 切换标签。
func (a *App) BrowserActivateTab(spaceID, tabID string) error {
	m := a.ensureBrowserManager()
	return m.ActivateTab(spaceID, tabID)
}

// BrowserBack 后退。
func (a *App) BrowserBack(spaceID string) error {
	return a.ensureBrowserManager().GoBack(spaceID)
}

// BrowserForward 前进。
func (a *App) BrowserForward(spaceID string) error {
	return a.ensureBrowserManager().GoForward(spaceID)
}

// BrowserReload 刷新。
func (a *App) BrowserReload(spaceID string) error {
	return a.ensureBrowserManager().Reload(spaceID)
}

// BrowserStop 停止加载。
func (a *App) BrowserStop(spaceID string) error {
	return a.ensureBrowserManager().Stop(spaceID)
}

// BrowserSpaceState 返回一个空间的标签快照。
func (a *App) BrowserSpaceState(spaceID string) (browser.SpaceInfo, error) {
	return a.ensureBrowserManager().SpaceState(spaceID)
}

// BrowserSpaceStates 返回全部空间的标签快照。
func (a *App) BrowserSpaceStates() []browser.SpaceInfo {
	return a.ensureBrowserManager().SpaceStates()
}

// BrowserFocusShell 把键盘焦点交回前端页面（网页区抢焦点时用，Ctrl+L 等场景）。
func (a *App) BrowserFocusShell(spaceID string) error {
	return a.ensureBrowserManager().FocusShell(spaceID)
}

// BrowserDevTools 打开活动标签的开发者工具（独立窗口）。
func (a *App) BrowserDevTools(spaceID string) error {
	m := a.ensureBrowserManager()
	return m.DevTools(spaceID)
}

// BrowserEval 在活动标签里执行一段脚本。
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

// BrowserRebuild 关掉再重开一个空间：代理/UA 等 environment 级设置改完后生效。
func (a *App) BrowserRebuild(spaceID, windowName string, x, y, w, h int) error {
	a.BrowserClose(spaceID)
	// 给 WebView2 的浏览器进程一点退出时间，否则同一个 profile 目录会被锁住
	time.Sleep(300 * time.Millisecond)
	return a.BrowserAttach(spaceID, windowName, x, y, w, h)
}

// BrowserStats 返回资源占用快照：空间数 + 标签数 + WebView2 进程数与常驻内存。
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

// ensureBrowserManager 惰性创建空间管理器，并把空间事件转发给前端 + 记会话。
func (a *App) ensureBrowserManager() *browser.Manager {
	browserMu.Lock()
	defer browserMu.Unlock()
	if browserMgr == nil {
		m := browser.NewManager()
		m.OnEvent = func(ev browser.Event) {
			if ev.Kind == "tabs" || ev.Kind == "ready" {
				scheduleSessionSave(m)
			}
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

// ============================================================
// 会话记录（为「点击恢复」服务）
//
// 只记录「哪个空间开着哪些标签」，不自动恢复：用户点按钮才执行。
// 落在数据目录（browser/session.json），不进 config.json——登录态/标签属于
// 运行时数据，不该跟着配置上传到云端。
// ============================================================

// browserSessionSpace 一个空间在会话里的形态。
type browserSessionSpace struct {
	ID     string   `json:"id"`
	URLs   []string `json:"urls"`
	Active int      `json:"active"`
}

// browserSession 一次会话快照。
type browserSession struct {
	SavedAt time.Time             `json:"savedAt"`
	Spaces  []browserSessionSpace `json:"spaces"`
}

var (
	// sessionSaveMu 保护下面的去抖状态（前端切标签会频繁触发事件）。
	sessionSaveMu    sync.Mutex
	sessionSaveTimer *time.Timer
)

// sessionFilePath 会话文件路径（<数据目录>/browser/session.json）。
func sessionFilePath() (string, error) {
	base, err := appdirs.Data("browser")
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "session.json"), nil
}

// scheduleSessionSave 去抖 2 秒后落盘一次会话快照。
//
// 标签一多，切标签/导航会连着抛事件，每次都写盘纯属浪费。
func scheduleSessionSave(m *browser.Manager) {
	sessionSaveMu.Lock()
	defer sessionSaveMu.Unlock()
	if sessionSaveTimer != nil {
		sessionSaveTimer.Stop()
	}
	sessionSaveTimer = time.AfterFunc(2*time.Second, func() { saveSession(m) })
}

func saveSession(m *browser.Manager) {
	states := m.SpaceStates()
	if len(states) == 0 {
		return
	}
	sess := browserSession{SavedAt: time.Now()}
	for _, st := range states {
		item := browserSessionSpace{ID: st.ID}
		for _, t := range st.Tabs {
			url := strings.TrimSpace(t.URL)
			if url == "" || url == "about:blank" {
				continue
			}
			if t.ID == st.ActiveTab {
				item.Active = len(item.URLs)
			}
			item.URLs = append(item.URLs, url)
		}
		if len(item.URLs) > 0 {
			sess.Spaces = append(sess.Spaces, item)
		}
	}
	if len(sess.Spaces) == 0 {
		return
	}
	path, err := sessionFilePath()
	if err != nil {
		return
	}
	data, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		applog.Errorf("browser", "写会话文件失败: %v", err)
	}
}

// BrowserSessionInfo 返回上次会话摘要，供前端显示「恢复上次标签页」入口。
func (a *App) BrowserSessionInfo() map[string]any {
	out := map[string]any{"available": false}
	path, err := sessionFilePath()
	if err != nil {
		return out
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return out
	}
	var sess browserSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return out
	}
	total := 0
	spaces := make([]map[string]any, 0, len(sess.Spaces))
	for _, s := range sess.Spaces {
		total += len(s.URLs)
		spaces = append(spaces, map[string]any{
			"id":     s.ID,
			"tabs":   len(s.URLs),
			"active": s.Active,
			"urls":   s.URLs,
		})
	}
	if total == 0 {
		return out
	}
	return map[string]any{
		"available": true,
		"savedAt":   sess.SavedAt.Format("2006-01-02 15:04"),
		"tabs":      total,
		"spaces":    spaces,
	}
}

// BrowserRestoreSession 把上次会话里该空间的标签重新开出来（用户点击才执行）。
//
// 返回恢复出来的标签数；空间还没 attach 时返回错误，前端应先 attach。
func (a *App) BrowserRestoreSession(spaceID string) (int, error) {
	path, err := sessionFilePath()
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, fmt.Errorf("没有可恢复的会话")
	}
	var sess browserSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return 0, fmt.Errorf("会话文件损坏")
	}
	var target *browserSessionSpace
	for i := range sess.Spaces {
		if sess.Spaces[i].ID == spaceID {
			target = &sess.Spaces[i]
			break
		}
	}
	if target == nil || len(target.URLs) == 0 {
		return 0, fmt.Errorf("上次会话里没有这个空间的标签")
	}

	m := a.ensureBrowserManager()
	if err := m.RestoreTabs(spaceID, target.URLs, target.Active); err != nil {
		return 0, err
	}
	applog.Infof("browser", "已恢复空间 %s 的 %d 个标签", spaceID, len(target.URLs))
	return len(target.URLs), nil
}
