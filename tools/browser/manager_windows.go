//go:build windows

package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/shirou/gopsutil/v3/process"
	"github.com/wailsapp/go-webview2/pkg/edge"
	"github.com/wailsapp/go-webview2/webviewloader"
	"golang.org/x/sys/windows"
)

// ============================================================
// Win32 常量与入口
// ============================================================

const (
	wmAppCall = 0x8000 + 1 // 投递到空间线程执行的命令
	wmTimer   = 0x0113
	wmQuit    = 0x0012

	// 空间线程上的定时器：每 250ms 检查父窗口是否被移动/销毁。
	// WebView2 需要 NotifyParentWindowPositionChanged 才能在父窗口移动后
	// 正确摆放弹层（下拉、右键菜单、DevTools 停靠）。
	tickIntervalMS = 250
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostThreadMessageW  = user32.NewProc("PostThreadMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procSetTimer            = user32.NewProc("SetTimer")
	procKillTimer           = user32.NewProc("KillTimer")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procGetDpiForWindow     = user32.NewProc("GetDpiForWindow")
	procEnumWindows         = user32.NewProc("EnumWindows")
	procGetWindowTextW      = user32.NewProc("GetWindowTextW")
	procGetWindowTextLength = user32.NewProc("GetWindowTextLengthW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procGetWindow           = user32.NewProc("GetWindow")
)

const (
	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsClipChildren = 0x02000000
	wsClipSiblings = 0x04000000

	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
	swpNoMove     = 0x0002
	swpNoSize     = 0x0001

	hwndTop    = 0 // SetWindowPos 的特殊句柄：放到同级窗口最上面
	gwHwndPrev = 3 // GetWindow 的 GW_HWNDPREV：取压在我上面的那个兄弟窗口

	hostClassName = "GoToolsBrowserHost"
)

// msg 对应 Win32 MSG。
type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ X, Y int32 }
}

// space 一个网页空间 = 一个 WebView2 environment（独立 profile + 独立代理）
// + 一个 controller（网页区）+ 一条独占的 OS 线程（WebView2 的 STA 要求）。
type space struct {
	id string

	hwnd       uintptr // 父窗口（Wails 窗口），用来判活与跟随移动
	host       uintptr // 我们自己建的子窗口，真正承载 WebView2 的父窗口
	dataPath   string  // 该空间的 profile 目录，资源统计靠它归属进程
	mu         sync.Mutex
	bounds     Rect
	c          *edge.Chromium
	tid        uint32
	lastRect   [4]int32
	hasLastPos bool
	closed     int32

	pendingMu sync.Mutex
	pending   []func()

	onEvent  func(Event)
	onClosed func()

	m *Manager
}

// ============================================================
// Manager
// ============================================================

// Open 打开（或复用）一个网页空间。返回时控制器已就绪，可以立即导航。
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
		opts.DataPath = filepath.Join(os.TempDir(), "go-tools-browser", opts.ID)
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
			existing.navigate(opts.URL)
		}
		return nil
	}

	sp := &space{id: opts.ID, bounds: opts.Bounds, onEvent: m.emit, m: m}
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

// Navigate 让指定空间跳转到 url。
func (m *Manager) Navigate(id, url string) error {
	sp := m.get(id)
	if sp == nil {
		return fmt.Errorf("空间不存在: %s", id)
	}
	sp.navigate(url)
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

// DevTools 在独立窗口里打开该空间的开发者工具。
func (m *Manager) DevTools(id string) error {
	sp := m.get(id)
	if sp == nil {
		return fmt.Errorf("空间不存在: %s", id)
	}
	sp.call(func() {
		if sp.c != nil {
			sp.c.OpenDevToolsWindow()
		}
	})
	return nil
}

// Eval 在页面里执行一段脚本（无返回值）。
func (m *Manager) Eval(id, script string) error {
	sp := m.get(id)
	if sp == nil {
		return fmt.Errorf("空间不存在: %s", id)
	}
	sp.call(func() {
		if sp.c != nil {
			sp.c.Eval(script)
		}
	})
	return nil
}

// Close 关闭一个空间：销毁 controller，线程退出，profile 目录锁释放。
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

func (m *Manager) get(id string) *space {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.spaces[id]
}

// ============================================================
// space 生命周期
// ============================================================

// run 在一条锁定到 OS 线程的 goroutine 上创建 environment 并进入消息循环。
// WebView2 的 controller 是 STA 的：创建与后续 PutBounds/MoveFocus 都必须在同一条线程上。
func (sp *space) run(opts Options, ready chan<- error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err != nil {
		ready <- fmt.Errorf("COM 初始化失败: %w", err)
		return
	}
	defer windows.CoUninitialize()

	sp.tid = windows.GetCurrentThreadId()
	sp.hwnd = opts.HWND
	sp.dataPath = opts.DataPath

	host, err := createHostWindow(opts.HWND, opts.Bounds)
	if err != nil {
		ready <- err
		return
	}
	sp.host = host

	c := edge.NewChromium()
	c.DataPath = opts.DataPath
	c.BrowserPath = opts.BrowserPath
	c.AdditionalBrowserArgs = buildArgs(opts)
	c.SetErrorCallback(func(err error) {
		sp.emit(Event{Kind: "error", SpaceID: sp.id, Message: err.Error()})
	})
	c.MessageCallback = func(text string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		// 只认我们自己注入的 title 回报，页面自己的 postMessage 忽略
		if strings.HasPrefix(text, "go-tools:title:") {
			sp.emit(Event{Kind: "title", SpaceID: sp.id, Title: strings.TrimPrefix(text, "go-tools:title:")})
		}
	}
	c.NavigationCompletedCallback = func(_ *edge.ICoreWebView2, _ *edge.ICoreWebView2NavigationCompletedEventArgs) {
		c.Eval(`window.chrome.webview.postMessage("go-tools:title:" + (document.title || ""))`)
	}
	c.ProcessFailedCallback = func(_ *edge.ICoreWebView2, args *edge.ICoreWebView2ProcessFailedEventArgs) {
		reason := "renderer failed"
		if args != nil {
			if kind, err := args.GetProcessFailedKind(); err == nil {
				reason = fmt.Sprintf("process failed (kind=%d)", kind)
			}
		}
		sp.emit(Event{Kind: "crash", SpaceID: sp.id, Message: reason})
	}
	sp.c = c

	if !c.Embed(sp.host) {
		ready <- fmt.Errorf("WebView2 控制器创建失败")
		return
	}
	sp.applyBounds()
	procSetTimer.Call(0, 1, tickIntervalMS, 0)
	ready <- nil

	sp.emit(Event{Kind: "ready", SpaceID: sp.id})
	if opts.URL != "" {
		c.Navigate(opts.URL)
	}
	sp.loop()
}

// loop 该空间线程的消息循环：只分发 Win32 消息，外加我们自己的命令队列与定时器。
func (sp *space) loop() {
	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) == -1 || r == 0 {
			return
		}
		switch m.message {
		case wmAppCall:
			sp.drain()
		case wmTimer:
			sp.tick()
		case wmQuit:
			return
		default:
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
		}
	}
}

// call 把一段操作排到空间线程执行（controller 只能在它的 STA 线程上碰）。
func (sp *space) call(fn func()) {
	if atomic.LoadInt32(&sp.closed) != 0 {
		return
	}
	sp.pendingMu.Lock()
	sp.pending = append(sp.pending, fn)
	sp.pendingMu.Unlock()
	procPostThreadMessageW.Call(uintptr(sp.tid), wmAppCall, 0, 0)
}

func (sp *space) drain() {
	sp.pendingMu.Lock()
	fns := sp.pending
	sp.pending = nil
	sp.pendingMu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

// tick 父窗口被移动/销毁时的补偿动作。
func (sp *space) tick() {
	if isWindow(sp.hwnd) == 0 {
		// 窗口没了（用户直接关了浏览器窗口）：自己收摊，避免进程常驻
		sp.Close()
		return
	}
	// 兄弟窗口（尤其是 Wails 自己的 WebView2）会抢 z-order，抢回来
	sp.raiseHost()
	var r struct{ L, T, R, B int32 }
	procGetWindowRect.Call(sp.hwnd, uintptr(unsafe.Pointer(&r)))
	cur := [4]int32{r.L, r.T, r.R, r.B}
	if !sp.hasLastPos || cur != sp.lastRect {
		sp.lastRect = cur
		sp.hasLastPos = true
		if sp.c != nil {
			if ctl := sp.c.GetController(); ctl != nil {
				_ = ctl.NotifyParentWindowPositionChanged()
			}
		}
	}
}

func (sp *space) navigate(url string) {
	if strings.TrimSpace(url) == "" {
		return
	}
	sp.call(func() {
		if sp.c != nil {
			sp.c.Navigate(url)
		}
	})
}

func (sp *space) setBounds(r Rect) {
	sp.mu.Lock()
	sp.bounds = r
	sp.mu.Unlock()
	sp.call(sp.applyBounds)
}

// applyBounds 把 DIP 矩形换算成物理像素：宿主子窗口摆到目标位置，controller 铺满它。
//
// edge 包创建 controller 时启用了 COREWEBVIEW2_BOUNDS_MODE_USE_RAW_PIXELS 并关掉了
// 显示器缩放自动跟随，所以这里必须自己乘 dpi/96。
func (sp *space) applyBounds() {
	if sp.c == nil || sp.host == 0 {
		return
	}
	sp.mu.Lock()
	b := sp.bounds
	sp.mu.Unlock()

	scale := float64(dpiForWindow(sp.hwnd)) / 96.0
	if scale <= 0 {
		scale = 1
	}
	x := int32(float64(b.X) * scale)
	y := int32(float64(b.Y) * scale)
	w := int32(float64(b.W) * scale)
	h := int32(float64(b.H) * scale)
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}

	procSetWindowPos.Call(sp.host, hwndTop, uintptr(x), uintptr(y), uintptr(w), uintptr(h), swpNoActivate)
	r := edge.Rect{Left: 0, Top: 0, Right: w, Bottom: h}
	sp.c.ResizeWithBounds(&r)
	if ctl := sp.c.GetController(); ctl != nil {
		_ = ctl.NotifyParentWindowPositionChanged()
	}
}

// raiseHost 把网页区提回同级窗口的最上面。
//
// 宿主子窗口和 Wails 自己的 WebView2 是同一个父窗口下的兄弟窗口，谁在上面谁可见。
// Chromium 会在获得焦点/重新绘制时把自己的窗口提到最前，于是网页区就被盖住
// （实测现象：网页区位置显示的是前端页面的背景色，看不到任何网页）。
// 这里只在自己的上面真压着兄弟窗口时才提一次，避免白白刷 z-order。
func (sp *space) raiseHost() {
	if sp.host == 0 {
		return
	}
	prev, _, _ := procGetWindow.Call(sp.host, gwHwndPrev)
	if prev == 0 {
		return
	}
	procSetWindowPos.Call(sp.host, hwndTop, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
}

// Close 关闭该空间。幂等，可从任意线程调用。
func (sp *space) Close() {
	if !atomic.CompareAndSwapInt32(&sp.closed, 0, 1) {
		return
	}
	// 注意：不能走 sp.call（它会因为 closed 已置位而丢弃命令），
	// 否则清理与退出消息永远送不到空间线程。
	sp.pendingMu.Lock()
	sp.pending = append(sp.pending, func() {
		procKillTimer.Call(0, 1)
		if sp.c != nil {
			sp.c.ShuttingDown()
			if ctl := sp.c.GetController(); ctl != nil {
				_ = ctl.Close()
			}
		}
		if sp.host != 0 {
			procDestroyWindow.Call(sp.host)
			sp.host = 0
		}
		procPostQuitMessage.Call(0)
	})
	sp.pendingMu.Unlock()
	procPostThreadMessageW.Call(uintptr(sp.tid), wmAppCall, 0, 0)

	if sp.onClosed != nil {
		sp.onClosed()
	}
}

func (sp *space) emit(ev Event) {
	if sp.onEvent != nil {
		sp.onEvent(ev)
	}
}

// hostClassOnce 注册一次宿主子窗口类（进程级，幂等）。
var hostClassOnce sync.Once

// createHostWindow 在空间线程上建一个子窗口作为 WebView2 的父窗口。
//
// 为什么不直接把 Wails 窗口的 HWND 交给 controller：controller 必须与父窗口同线程
// （WebView2 会向父窗口所在线程同步发消息），而 Wails 的窗口在 Wails 自己的线程上创建。
// 跨线程父窗口会让 CreateCoreWebView2Controller 永不返回（实测：environment 回调已
// 同步返回 res=0，紧接着就卡死在 controller 创建）。
// 解法：在空间线程上建一个 WS_CHILD 子窗口挂到 Wails 窗口下——父子窗口允许跨线程，
// 位置随父窗口移动，输入/焦点走子窗口自己的线程。
func createHostWindow(parent uintptr, r Rect) (uintptr, error) {
	var regErr error
	hostClassOnce.Do(func() {
		cls, err := syscall.UTF16PtrFromString(hostClassName)
		if err != nil {
			regErr = err
			return
		}
		wc := wndClassEx{
			cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
			style:         0,
			lpfnWndProc:   procDefWindowProcW.Addr(),
			hInstance:     0,
			hbrBackground: 0,
			lpszClassName: cls,
		}
		r, _, callErr := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		if r == 0 {
			regErr = fmt.Errorf("注册窗口类失败: %v", callErr)
		}
	})
	if regErr != nil {
		return 0, regErr
	}

	cls, err := syscall.UTF16PtrFromString(hostClassName)
	if err != nil {
		return 0, err
	}
	scale := float64(dpiForWindow(parent)) / 96.0
	if scale <= 0 {
		scale = 1
	}
	h, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(cls)),
		0,
		wsChild|wsVisible|wsClipChildren|wsClipSiblings,
		uintptr(int32(float64(r.X)*scale)),
		uintptr(int32(float64(r.Y)*scale)),
		uintptr(int32(float64(r.W)*scale)),
		uintptr(int32(float64(r.H)*scale)),
		parent, 0, 0, 0,
	)
	if h == 0 {
		return 0, fmt.Errorf("创建网页区宿主子窗口失败: %v", callErr)
	}
	return h, nil
}

// wndClassEx 对应 Win32 WNDCLASSEXW。
type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

// buildArgs 组装 Chromium 启动参数。
//
// 刻意不加 --disable-background-timer-throttling / --disable-backgrounding-occluded-windows：
// 那是给录屏浮层用的，网页区带上会让隐藏标签继续满速跑动画，CPU 白烧。
func buildArgs(opts Options) []string {
	args := []string{
		// 关掉 Edge 首次运行的引导与边栏，减少无谓请求
		"--no-first-run",
		"--disable-features=msEdgeSidebarV2,msEdgeShoppingAssistant",
	}
	if p := strings.TrimSpace(opts.Proxy); p != "" {
		args = append(args, "--proxy-server="+p)
		if b := strings.TrimSpace(opts.ProxyBypass); b != "" {
			args = append(args, "--proxy-bypass-list="+b)
		}
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

func isWindow(hwnd uintptr) uintptr {
	r, _, _ := user32.NewProc("IsWindow").Call(hwnd)
	return r
}

func dpiForWindow(hwnd uintptr) uint32 {
	r, _, _ := procGetDpiForWindow.Call(hwnd)
	if int32(r) <= 0 {
		return 96
	}
	return uint32(r)
}
