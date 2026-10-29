//go:build windows

package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/shirou/gopsutil/v3/process"
	"github.com/wailsapp/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

// ============================================================
// Manager：空间与标签的对外接口
//
// 线程约定：这里的方法都能从任意 goroutine 调用（前端绑定跑在 HTTP handler
// goroutine 上）。涉及 WebView2 对象的操作一律排到空间线程执行，返回值只用
// 纯内存状态拼出来。
// ============================================================

// Open 打开（或复用）一个网页空间。返回时 environment 与第一个标签已就绪。
func (m *Manager) Open(opts Options) error {
	if opts.HWND == 0 {
		return fmt.Errorf("父窗口句柄为空")
	}
	if strings.TrimSpace(opts.ID) == "" {
		return fmt.Errorf("空间 ID 不能为空")
	}
	if strings.TrimSpace(opts.DataPath) == "" {
		// 没给 profile 目录就按 ID 分一个临时目录：绝不能落回 edge 包默认的
		// "AppData\<exe名>"，否则所有空间会共用同一个 profile（登录态/代理全串）。
		opts.DataPath = filepath.Join(os.TempDir(), "Tools-browser", opts.ID)
	}
	if err := os.MkdirAll(opts.DataPath, 0o755); err != nil {
		return fmt.Errorf("创建 profile 目录失败: %w", err)
	}
	if v := webviewRuntimeVersion(); v == "" {
		return fmt.Errorf("未找到 WebView2 运行时（系统未安装且未配置固定版目录）")
	}

	m.mu.RLock()
	existing := m.spaces[opts.ID]
	m.mu.RUnlock()
	if existing != nil {
		// 已存在：只更新矩形与地址，避免同一个 profile 起两份 environment
		existing.setBounds(opts.Bounds)
		if opts.URL != "" {
			existing.navigateTab("", opts.URL)
		}
		return nil
	}

	sp := &space{
		id:      opts.ID,
		bounds:  opts.Bounds,
		tabs:    make(map[string]*tab),
		onEvent: m.emit,
	}
	sp.onClosed = func() {
		m.mu.Lock()
		if m.spaces[opts.ID] == sp {
			delete(m.spaces, opts.ID)
		}
		m.mu.Unlock()
	}

	ready := make(chan error, 1)
	go sp.run(opts, ready)
	select {
	case err := <-ready:
		if err != nil {
			return err
		}
	case <-time.After(30 * time.Second):
		return fmt.Errorf("创建网页区超时：WebView2 环境未就绪")
	}

	m.mu.Lock()
	m.spaces[opts.ID] = sp
	m.mu.Unlock()
	return nil
}

// Close 关闭一个空间：销毁全部标签，线程退出，profile 目录锁释放。
func (m *Manager) Close(id string) {
	m.mu.Lock()
	sp := m.spaces[id]
	delete(m.spaces, id)
	m.mu.Unlock()
	if sp != nil {
		sp.Close()
	}
}

// CloseAll 关闭全部空间（程序退出/关窗时调用）。
func (m *Manager) CloseAll() {
	m.mu.Lock()
	list := make([]*space, 0, len(m.spaces))
	for _, sp := range m.spaces {
		list = append(list, sp)
	}
	m.spaces = make(map[string]*space)
	m.mu.Unlock()
	for _, sp := range list {
		sp.Close()
	}
}

// ============================================================
// 标签操作
// ============================================================

// NewTab 在指定空间里新建标签，返回标签 ID。
// activate 为 true 时新标签立即成为活动标签；url 为空时用该空间的首页。
func (m *Manager) NewTab(spaceID, url string, activate bool) (string, error) {
	sp := m.get(spaceID)
	if sp == nil {
		return "", fmt.Errorf("空间不存在: %s", spaceID)
	}
	t, err := sp.reserveTab(url, activate)
	if err != nil {
		return "", err
	}
	sp.spawnTab(t, t.url, activate)
	return t.id, nil
}

// CloseTab 关闭一个标签。关掉最后一个标签时会自动开一个首页标签，
// 避免留下"有窗口没网页区"的中间态。
func (m *Manager) CloseTab(spaceID, tabID string) error {
	sp := m.get(spaceID)
	if sp == nil {
		return fmt.Errorf("空间不存在: %s", spaceID)
	}
	t, last := sp.removeTab(tabID)
	if t == nil {
		return fmt.Errorf("标签不存在: %s", tabID)
	}
	sp.call(func() {
		if last {
			if nt, err := sp.reserveTab(sp.home, true); err == nil {
				sp.destroyTab(t)
				sp.createTab(nt, nt.url)
				sp.applyActive()
				sp.emitTabs()
				return
			}
		}
		sp.destroyTab(t)
		sp.applyActive()
		sp.emitTabs()
	})
	return nil
}

// ActivateTab 切换到指定标签（会顺带把挂起的标签唤醒）。
func (m *Manager) ActivateTab(spaceID, tabID string) error {
	sp := m.get(spaceID)
	if sp == nil {
		return fmt.Errorf("空间不存在: %s", spaceID)
	}
	if !sp.setActive(tabID) {
		return fmt.Errorf("标签不存在: %s", tabID)
	}
	return nil
}

// SetVisible 显示/隐藏整块网页区。
//
// 网页区是原生窗口，永远画在前端 HTML 之上；打开设置抽屉这类覆盖层时
// 必须先把它藏起来，否则抽屉会被盖住。
func (m *Manager) SetVisible(spaceID string, visible bool) error {
	sp := m.get(spaceID)
	if sp == nil {
		return fmt.Errorf("空间不存在: %s", spaceID)
	}
	sp.call(func() { sp.setVisible(visible) })
	return nil
}

// RestoreTabs 用一组地址重建该空间的标签（会话恢复用，用户点击才调用）。
func (m *Manager) RestoreTabs(spaceID string, urls []string, active int) error {
	sp := m.get(spaceID)
	if sp == nil {
		return fmt.Errorf("空间不存在: %s", spaceID)
	}
	if len(urls) == 0 {
		return fmt.Errorf("没有可恢复的标签")
	}
	sp.call(func() { sp.restoreTabs(urls, active) })
	return nil
}

// Navigate 让空间的当前活动标签跳转到 url。
func (m *Manager) Navigate(spaceID, url string) error {
	return m.NavigateTab(spaceID, "", url)
}

// NavigateTab 让指定标签（空 = 当前活动标签）跳转到 url。
func (m *Manager) NavigateTab(spaceID, tabID, url string) error {
	sp := m.get(spaceID)
	if sp == nil {
		return fmt.Errorf("空间不存在: %s", spaceID)
	}
	sp.navigateTab(tabID, url)
	return nil
}

// GoBack / GoForward / Reload / Stop 是导航的四个常规动作，作用于活动标签。
func (m *Manager) GoBack(spaceID string) error {
	return m.tabCommand(spaceID, "back")
}

// GoForward 前进一步。
func (m *Manager) GoForward(spaceID string) error {
	return m.tabCommand(spaceID, "forward")
}

// Reload 重新加载当前页面。
func (m *Manager) Reload(spaceID string) error {
	return m.tabCommand(spaceID, "reload")
}

// Stop 停止加载。
func (m *Manager) Stop(spaceID string) error {
	return m.tabCommand(spaceID, "stop")
}

// tabCommand 把导航动作排到空间线程，作用于活动标签。
func (m *Manager) tabCommand(spaceID, action string) error {
	sp := m.get(spaceID)
	if sp == nil {
		return fmt.Errorf("空间不存在: %s", spaceID)
	}
	sp.call(func() {
		t := sp.activeTab()
		if t == nil || t.c == nil {
			return
		}
		switch action {
		case "back":
			_ = t.c.Back()
		case "forward":
			_ = t.c.Forward()
		case "reload":
			t.mu.Lock()
			t.loading = true
			t.mu.Unlock()
			_ = t.c.Reload()
		case "stop":
			_ = t.c.Stop()
		}
		sp.emitTabs()
	})
	return nil
}

// SpaceState 返回某个空间的标签快照。
func (m *Manager) SpaceState(spaceID string) (SpaceInfo, error) {
	sp := m.get(spaceID)
	if sp == nil {
		return SpaceInfo{}, fmt.Errorf("空间不存在: %s", spaceID)
	}
	return sp.snapshot(), nil
}

// SpaceStates 返回全部空间的标签快照（页面初始化时一次性拉齐）。
func (m *Manager) SpaceStates() []SpaceInfo {
	m.mu.RLock()
	list := make([]*space, 0, len(m.spaces))
	for _, sp := range m.spaces {
		list = append(list, sp)
	}
	m.mu.RUnlock()

	out := make([]SpaceInfo, 0, len(list))
	for _, sp := range list {
		out = append(out, sp.snapshot())
	}
	return out
}

// FocusShell 把键盘焦点交回前端页面（网页区抢着焦点时用）。
func (m *Manager) FocusShell(spaceID string) error {
	sp := m.get(spaceID)
	if sp == nil {
		return fmt.Errorf("空间不存在: %s", spaceID)
	}
	sp.call(sp.focusShell)
	return nil
}

// SetBounds 更新指定空间的网页区矩形（DIP，相对父窗口客户区）。
func (m *Manager) SetBounds(id string, r Rect) error {
	sp := m.get(id)
	if sp == nil {
		return fmt.Errorf("空间不存在: %s", id)
	}
	sp.setBounds(r)
	return nil
}

// DevTools 在独立窗口里打开活动标签的开发者工具。
func (m *Manager) DevTools(id string) error {
	sp := m.get(id)
	if sp == nil {
		return fmt.Errorf("空间不存在: %s", id)
	}
	sp.call(func() {
		if t := sp.activeTab(); t != nil && t.c != nil {
			t.c.OpenDevToolsWindow()
		}
	})
	return nil
}

// Eval 在活动标签的页面里执行一段脚本（无返回值）。
func (m *Manager) Eval(id, script string) error {
	sp := m.get(id)
	if sp == nil {
		return fmt.Errorf("空间不存在: %s", id)
	}
	sp.call(func() {
		if t := sp.activeTab(); t != nil && t.c != nil {
			t.c.Eval(script)
		}
	})
	return nil
}

func (m *Manager) get(id string) *space {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.spaces[id]
}

// ============================================================
// 资源统计
// ============================================================

// Stats 采集资源基线。
//
// 只统计“属于本程序空间”的 msedgewebview2.exe 进程：以命令行里带该空间 profile
// 目录的浏览器主进程为根，再把它的子进程（GPU/渲染/工具进程）一并算进来。
// 直接统计全机同名进程会把 Wails 自己的壳、Edge、其它 WebView2 应用都算进来（实测能差出好几倍）。
func (m *Manager) Stats() Stat {
	m.mu.RLock()
	st := Stat{Spaces: len(m.spaces)}
	paths := make([]string, 0, len(m.spaces))
	for _, sp := range m.spaces {
		st.Tabs += len(sp.tabs)
		if sp.dataPath != "" {
			paths = append(paths, strings.ToLower(filepath.Clean(sp.dataPath)))
		}
	}
	m.mu.RUnlock()
	if len(paths) == 0 {
		return st
	}

	procs, err := process.Processes()
	if err != nil {
		return st
	}
	byPID := make(map[int32]*process.Process, len(procs))
	for _, p := range procs {
		byPID[p.Pid] = p
	}

	// 1) 找到各空间的主浏览器进程
	seen := make(map[int32]bool, len(procs))
	for _, p := range procs {
		name, err := p.Name()
		if err != nil || !strings.EqualFold(name, "msedgewebview2.exe") {
			continue
		}
		cmd, err := p.Cmdline()
		if err != nil || cmd == "" {
			continue
		}
		lower := strings.ToLower(cmd)
		for _, path := range paths {
			if strings.Contains(lower, path) {
				collectTree(p, byPID, seen, &st)
				break
			}
		}
	}
	return st
}

// collectTree 把某个进程及其全部子孙进程计入统计（广度优先，带 visited 防环）。
func collectTree(root *process.Process, byPID map[int32]*process.Process, seen map[int32]bool, st *Stat) {
	queue := []*process.Process{root}
	for len(queue) > 0 {
		p := queue[0]
		queue = queue[1:]
		if p == nil || seen[p.Pid] {
			continue
		}
		seen[p.Pid] = true
		st.Processes++
		if mi, err := p.MemoryInfo(); err == nil && mi != nil {
			st.RSSBytes += mi.RSS
		}
		kids, err := p.Children()
		if err != nil {
			continue
		}
		for _, kid := range kids {
			if kid == nil {
				continue
			}
			if _, ok := byPID[kid.Pid]; ok {
				queue = append(queue, kid)
			}
		}
	}
}

// ============================================================
// 杂项
// ============================================================

// FindWindowByTitle 按窗口标题精确查找顶层窗口（本进程自己创建的窗口）。
// 用于把"某个前端页面所在的窗口"与它的 HWND 对应起来：Wails 不暴露句柄，
// 而浏览器窗口的标题由我们自己设定且唯一。
func FindWindowByTitle(title string) (uintptr, error) {
	desired := strings.TrimSpace(title)
	if desired == "" {
		return 0, fmt.Errorf("标题为空")
	}
	var found uintptr
	cb := syscall.NewCallback(func(hwnd uintptr, _ uintptr) uintptr {
		n, _, _ := procGetWindowTextLength.Call(hwnd)
		if n == 0 {
			return 1
		}
		buf := make([]uint16, int(n)+1)
		procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		if strings.TrimSpace(windows.UTF16ToString(buf)) == desired {
			found = hwnd
			return 0 // 停止枚举
		}
		return 1
	})
	procEnumWindows.Call(cb, 0)
	if found == 0 {
		return 0, fmt.Errorf("未找到标题为 %q 的窗口", desired)
	}
	return found, nil
}

// buildArgs 组装 Chromium 启动参数（作用于该空间的 environment）。
//
// 刻意不加 --disable-background-timer-throttling / --disable-backgrounding-occluded-windows：
// 那是给录屏浮层用的，网页区带上会让隐藏标签继续满速跑动画，CPU 白烧。
func buildArgs(opts Options) []string {
	args := []string{
		// 关掉 Edge 首次运行的引导与边栏，减少无谓请求
		"--no-first-run",
		"--disable-features=msEdgeSidebarV2,msEdgeShoppingAssistant",
		// 新窗口请求由我们自己接管（转成内嵌标签）。浏览器的弹窗拦截器
		// 会在叫到宿主之前就把 window.open 干掉，得显式关掉。
		"--disable-popup-blocking",
	}
	switch strings.ToLower(strings.TrimSpace(opts.ProxyMode)) {
	case "direct":
		// 显式直连：即使系统配了代理也不走
		args = append(args, "--no-proxy-server")
	case "custom":
		if p := strings.TrimSpace(opts.Proxy); p != "" {
			args = append(args, "--proxy-server="+p)
			if b := strings.TrimSpace(opts.ProxyBypass); b != "" {
				args = append(args, "--proxy-bypass-list="+b)
			}
		}
	default:
		// system（或不指定）：跟随系统代理，不传任何参数
	}
	if ua := strings.TrimSpace(opts.UserAgent); ua != "" {
		args = append(args, "--user-agent="+ua)
	}
	if opts.DisableWebSecurity {
		args = append(args, "--disable-web-security")
	}
	return args
}

func webviewRuntimeVersion() string {
	v, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
	if err != nil {
		return ""
	}
	return v
}

// DetectRuntimeVersion 对外暴露 WebView2 运行时版本（空串 = 本机不可用）。
// 前端据此决定是否显示“未安装 WebView2 运行时”提示。
func DetectRuntimeVersion() string { return webviewRuntimeVersion() }
