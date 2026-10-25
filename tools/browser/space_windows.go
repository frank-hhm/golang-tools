//go:build windows

package browser

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/go-webview2/pkg/edge"
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
	procShowWindow          = user32.NewProc("ShowWindow")
	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procGetWindow           = user32.NewProc("GetWindow")
	procIsWindow            = user32.NewProc("IsWindow")
	procEnumChildWindows    = user32.NewProc("EnumChildWindows")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procGetParent           = user32.NewProc("GetParent")
	procSetFocus            = user32.NewProc("SetFocus")
)

const (
	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsClipChildren = 0x02000000
	wsClipSiblings = 0x04000000

	swHide = 0
	swShow = 5

	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
	swpNoMove     = 0x0002
	swpNoSize     = 0x0001

	hwndTop    = 0 // SetWindowPos 的特殊句柄：放到同级窗口最上面
	gwHwndPrev = 3 // GetWindow 的 GW_HWNDPREV：取压在我上面的那个兄弟窗口

	hostClassName = "GoToolsBrowserHost"
)

// idleSuspendAfter 非活动标签多久之后自动挂起（WebView2 TrySuspend）。
//
// 挂起后页面状态不丢，但渲染进程会被降下来释放大部分内存；用户切回来时
// Resume 一下即可。做成变量是为了测试能把它调小。
var idleSuspendAfter = 5 * time.Minute

// msg 对应 Win32 MSG。
type msg struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      struct{ X, Y int32 }
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

// ============================================================
// 结构
// ============================================================

// tab 一个标签 = 一个 WebView2 controller（共享所在空间的 environment）
// + 一个标签宿主子窗口（挂在空间宿主窗口下，同时只显示活动标签）。
type tab struct {
	id   string
	c    *edge.Chromium
	host uintptr

	mu        sync.Mutex
	url       string
	title     string
	loading   bool
	canBack   bool
	canFwd    bool
	suspended bool
	errMsg    string
	// lastActive 该标签最后一次处于活动状态的时间，用来判定是否该挂起。
	lastActive time.Time
}

// space 一个网页空间 = 一份 WebView2 environment（独立 profile + 独立代理）
// + 若干标签 + 一条独占的 OS 线程（WebView2 的 STA 要求）。
type space struct {
	id string

	hwnd     uintptr // 父窗口（Wails 窗口），用来判活与跟随移动
	host     uintptr // 空间宿主子窗口，真正承载所有标签
	dataPath string  // 该空间的 profile 目录，资源统计靠它归属进程
	home     string  // 新标签的默认首页
	scale    float64 // 父窗口 DPI 缩放（DIP → 物理像素）

	mu      sync.Mutex
	bounds  Rect
	tabs    map[string]*tab
	order   []string
	active  string
	nextSeq int

	// envOwner 只负责建 environment 的那个 Chromium：它的回调被 COM 持有，
	// 必须留一份引用避免被 GC 回收（否则后面的标签建 controller 会拿到野指针）。
	envOwner *edge.Chromium
	env      *edge.ICoreWebView2Environment

	tid        uint32
	opts       Options
	maxTabs    int
	lastRect   [4]int32
	hasLastPos bool
	closed     int32
	// hidden 整块网页区被临时藏起来（打开设置抽屉时）：网页区是原生窗口，
	// 会盖住任何 HTML 覆盖层，只能先隐藏它。
	hidden bool

	pendingMu sync.Mutex
	pending   []func()

	onEvent  func(Event)
	onClosed func()
}

// ============================================================
// 空间线程
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
	sp.opts = opts
	sp.bounds = opts.Bounds
	sp.maxTabs = opts.MaxTabs
	if sp.maxTabs <= 0 {
		sp.maxTabs = DefaultMaxTabs
	}
	if strings.TrimSpace(opts.URL) == "" {
		sp.home = "about:blank"
	} else {
		sp.home = opts.URL
	}
	sp.scale = float64(dpiForWindow(opts.HWND)) / 96.0
	if sp.scale <= 0 {
		sp.scale = 1
	}

	host, err := createChildWindowPx(opts.HWND,
		int32(float64(opts.Bounds.X)*sp.scale),
		int32(float64(opts.Bounds.Y)*sp.scale),
		int32(float64(opts.Bounds.W)*sp.scale),
		int32(float64(opts.Bounds.H)*sp.scale),
		true)
	if err != nil {
		ready <- err
		return
	}
	sp.host = host

	// 先把 environment 建出来（一个空间只建一次，后面所有标签共用）。
	owner := edge.NewChromium()
	owner.DataPath = opts.DataPath
	owner.BrowserPath = opts.BrowserPath
	owner.AdditionalBrowserArgs = buildArgs(opts)
	owner.SetErrorCallback(func(e error) {
		sp.emit(Event{Kind: "error", SpaceID: sp.id, Message: e.Error()})
	})
	env, err := owner.CreateEnvironmentOnly()
	if err != nil {
		ready <- err
		return
	}
	sp.envOwner = owner
	sp.env = env

	// 第一个标签
	firstID := sp.allocTabID()
	sp.mu.Lock()
	first := &tab{id: firstID, url: sp.home, lastActive: time.Now()}
	sp.tabs[firstID] = first
	sp.order = append(sp.order, firstID)
	sp.active = firstID
	sp.mu.Unlock()
	sp.createTab(first, sp.home)

	procSetTimer.Call(0, 1, tickIntervalMS, 0)
	ready <- nil

	sp.emit(Event{Kind: "ready", SpaceID: sp.id})
	sp.emitTabs()

	sp.loop()
}

// loop 该空间线程的消息循环：只分发 Win32 消息，外加我们自己的命令队列与定时器。
func (sp *space) loop() {
	sp.drain() // 进循环前先清一次，把 run 期间排进来的命令执行掉
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
	if sp.tid != 0 {
		procPostThreadMessageW.Call(uintptr(sp.tid), wmAppCall, 0, 0)
	}
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

// tick 250ms 一次：判活、抢 z-order、通知父窗口位置变化、判定该不该挂起。
func (sp *space) tick() {
	if isWindow(sp.hwnd) == 0 {
		// 窗口没了（用户直接关了浏览器窗口）：自己收摊，避免进程常驻
		sp.Close()
		return
	}
	// 兄弟窗口（尤其是 Wails 自己的 WebView2）会抢 z-order，抢回来。
	// 藏起来的时候不能抢：否则会把设置抽屉盖掉。
	sp.mu.Lock()
	hidden := sp.hidden
	sp.mu.Unlock()
	if !hidden {
		sp.raiseHost()
	}

	var r struct{ L, T, R, B int32 }
	procGetWindowRect.Call(sp.hwnd, uintptr(unsafe.Pointer(&r)))
	cur := [4]int32{r.L, r.T, r.R, r.B}
	if !sp.hasLastPos || cur != sp.lastRect {
		sp.lastRect = cur
		sp.hasLastPos = true
		sp.notifyParentMoved()
	}

	sp.suspendIdleTabs()
}

// notifyParentMoved 父窗口动过之后告诉 WebView2 重新摆放弹层。
func (sp *space) notifyParentMoved() {
	sp.mu.Lock()
	tabs := sp.tabListLocked()
	sp.mu.Unlock()
	for _, t := range tabs {
		if t.c == nil {
			continue
		}
		if ctl := t.c.GetController(); ctl != nil {
			_ = ctl.NotifyParentWindowPositionChanged()
		}
	}
}

// suspendIdleTabs 把长时间没人看的标签挂起，省内存。
func (sp *space) suspendIdleTabs() {
	now := time.Now()
	sp.mu.Lock()
	active := sp.active
	tabs := sp.tabListLocked()
	sp.mu.Unlock()
	for _, t := range tabs {
		if t.id == active || t.c == nil {
			continue
		}
		t.mu.Lock()
		idle := !t.suspended && now.Sub(t.lastActive) >= idleSuspendAfter
		if idle {
			t.suspended = true // 先置位，避免下一轮重复发起
		}
		t.mu.Unlock()
		if !idle {
			continue
		}
		sp.trySuspend(t)
	}
}

func (sp *space) trySuspend(t *tab) {
	wv := t.c.GetICoreWebView2_3()
	if wv == nil {
		t.mu.Lock()
		t.suspended = false
		t.mu.Unlock()
		return
	}
	err := wv.Suspend(func(ok bool) {
		if !ok {
			t.mu.Lock()
			t.suspended = false
			t.mu.Unlock()
		}
		sp.emitTabs()
	})
	if err != nil {
		t.mu.Lock()
		t.suspended = false
		t.mu.Unlock()
	}
}

func (sp *space) resume(t *tab) {
	t.mu.Lock()
	suspended := t.suspended
	t.lastActive = time.Now()
	t.mu.Unlock()
	if !suspended || t.c == nil {
		return
	}
	if wv := t.c.GetICoreWebView2_3(); wv != nil {
		if err := wv.Resume(); err == nil {
			t.mu.Lock()
			t.suspended = false
			t.mu.Unlock()
		}
	}
}

// ============================================================
// 标签
// ============================================================

// allocTabID 生成标签 ID（纯计数，调用方负责加锁或独占线程）。
func (sp *space) allocTabID() string {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	sp.nextSeq++
	return fmt.Sprintf("%s-t%d", sp.id, sp.nextSeq)
}

// tabListLocked 按显示顺序返回标签切片（调用方需持有 sp.mu）。
func (sp *space) tabListLocked() []*tab {
	out := make([]*tab, 0, len(sp.order))
	for _, id := range sp.order {
		if t := sp.tabs[id]; t != nil {
			out = append(out, t)
		}
	}
	return out
}

func (sp *space) tabByID(id string) *tab {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.tabs[id]
}

func (sp *space) activeTab() *tab {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.tabs[sp.active]
}

// createTab 在空间线程上为 t 建宿主子窗口 + controller，然后导航。
func (sp *space) createTab(t *tab, url string) {
	w := int32(float64(sp.boundsW()) * sp.scale)
	h := int32(float64(sp.boundsH()) * sp.scale)
	host, err := createChildWindowPx(sp.host, 0, 0, w, h, false)
	if err != nil {
		t.mu.Lock()
		t.errMsg = err.Error()
		t.mu.Unlock()
		sp.emit(Event{Kind: "error", SpaceID: sp.id, TabID: t.id, Message: err.Error()})
		sp.emitTabs()
		return
	}
	t.host = host

	c := edge.NewChromium()
	c.DataPath = sp.opts.DataPath
	c.BrowserPath = sp.opts.BrowserPath
	c.SetErrorCallback(func(e error) {
		sp.emit(Event{Kind: "error", SpaceID: sp.id, TabID: t.id, Message: e.Error()})
	})
	c.MessageCallback = func(text string, _ *edge.ICoreWebView2, _ *edge.ICoreWebView2WebMessageReceivedEventArgs) {
		sp.onPageMessage(t, text)
	}
	c.NavigationCompletedCallback = func(sender *edge.ICoreWebView2, _ *edge.ICoreWebView2NavigationCompletedEventArgs) {
		t.mu.Lock()
		t.loading = false
		t.mu.Unlock()
		sp.syncTabState(t, sender)
		// 再走一脚脚本上报：部分页面在 NavigationCompleted 这一刻
		// document.title 还没落定，靠页面自己回报能补上
		if t.c != nil {
			t.c.Eval(`window.chrome.webview.postMessage("go-tools:title:" + (document.title || ""))`)
		}
		sp.emitTabs()
	}
	c.SourceChangedCallback = func(sender *edge.ICoreWebView2, _ *edge.ICoreWebView2SourceChangedEventArgs) {
		sp.syncTabState(t, sender)
	}
	c.ProcessFailedCallback = func(_ *edge.ICoreWebView2, args *edge.ICoreWebView2ProcessFailedEventArgs) {
		reason := "renderer failed"
		if args != nil {
			if kind, err := args.GetProcessFailedKind(); err == nil {
				reason = fmt.Sprintf("process failed (kind=%d)", kind)
			}
		}
		sp.emit(Event{Kind: "crash", SpaceID: sp.id, TabID: t.id, Message: reason})
	}
	c.NewWindowRequestedCallback = func(_ *edge.ICoreWebView2, uri string) bool {
		// 不能在这个回调里直接建新 controller：那会在事件派发过程中再起一层
		// 消息泵。排进命令队列，等这轮派发结束再建。
		target := uri
		sp.call(func() { sp.openTabFromPage(target) })
		return true
	}
	t.c = c

	if !c.EmbedWithEnvironment(host, sp.env) {
		t.mu.Lock()
		t.errMsg = "WebView2 控制器创建失败"
		t.mu.Unlock()
		sp.emit(Event{Kind: "error", SpaceID: sp.id, TabID: t.id, Message: "WebView2 控制器创建失败"})
		sp.emitTabs()
		return
	}
	c.Init(shortcutScript)

	t.mu.Lock()
	t.url = url
	t.loading = true
	t.lastActive = time.Now()
	t.mu.Unlock()

	sp.resizeTab(t)
	sp.applyActive()

	if url != "" && url != "about:blank" {
		c.Navigate(url)
	}
	sp.emitTabs()
}

// reserveTab 登记一个新标签并返回它（此时 controller 还没建）。
//
// 登记是纯内存操作，可从任意线程调用；真正建 controller 必须排到空间线程。
func (sp *space) reserveTab(url string, activate bool) (*tab, error) {
	if strings.TrimSpace(url) == "" {
		url = sp.home
	}
	sp.mu.Lock()
	defer sp.mu.Unlock()
	if len(sp.tabs) >= sp.maxTabs {
		return nil, fmt.Errorf("标签数已达上限 %d", sp.maxTabs)
	}
	sp.nextSeq++
	id := fmt.Sprintf("%s-t%d", sp.id, sp.nextSeq)
	t := &tab{id: id, url: url, lastActive: time.Now()}
	sp.tabs[id] = t
	sp.order = append(sp.order, id)
	if activate {
		sp.active = id
	}
	return t, nil
}

// spawnTab 把「建 controller + 切到该标签」排到空间线程（跨线程调用用）。
func (sp *space) spawnTab(t *tab, url string, activate bool) {
	sp.call(func() {
		sp.createTab(t, url)
		if activate {
			sp.applyActive()
		}
		sp.emitTabs()
	})
}

// removeTab 从登记表里删掉一个标签，并选好新的活动标签。
// 返回 removed 与「删完之后是否已经不剩标签」。
func (sp *space) removeTab(id string) (*tab, bool) {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	t := sp.tabs[id]
	if t == nil {
		return nil, false
	}
	delete(sp.tabs, id)
	for i, v := range sp.order {
		if v == id {
			sp.order = append(sp.order[:i], sp.order[i+1:]...)
			break
		}
	}
	if sp.active == id {
		if len(sp.order) > 0 {
			sp.active = sp.order[0]
		} else {
			sp.active = ""
		}
	}
	return t, len(sp.tabs) == 0
}

// setActive 切换活动标签（纯内存 + 排一个刷新命令）。
func (sp *space) setActive(id string) bool {
	sp.mu.Lock()
	t := sp.tabs[id]
	if t == nil {
		sp.mu.Unlock()
		return false
	}
	sp.active = id
	sp.mu.Unlock()
	sp.call(func() {
		sp.resume(t)
		sp.applyActive()
		sp.emitTabs()
	})
	return true
}

// activeTabID 当前活动标签 ID。
func (sp *space) activeTabID() string {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.active
}

// navigateTab 让指定标签（空 = 活动标签）跳转。
func (sp *space) navigateTab(tabID, url string) {
	if strings.TrimSpace(url) == "" {
		return
	}
	sp.call(func() {
		var t *tab
		if tabID == "" {
			t = sp.activeTab()
		} else {
			t = sp.tabByID(tabID)
		}
		if t == nil || t.c == nil {
			return
		}
		t.mu.Lock()
		t.url = url
		t.loading = true
		t.lastActive = time.Now()
		t.mu.Unlock()
		t.c.Navigate(url)
		sp.emitTabs()
	})
}

// restoreTabs 用一组地址重建该空间的标签（会话恢复用）。
//
// 不能用「逐个 CloseTab 再 NewTab」凑：关掉最后一个标签会自动开一个首页标签，
// 恢复完就会多出一个。这里直接清空登记表重建。
func (sp *space) restoreTabs(urls []string, active int) {
	sp.mu.Lock()
	old := sp.tabListLocked()
	sp.tabs = make(map[string]*tab)
	sp.order = nil
	sp.active = ""
	sp.mu.Unlock()
	for _, t := range old {
		sp.destroyTab(t)
	}

	created := 0
	for i, u := range urls {
		t, err := sp.reserveTab(u, i == active)
		if err != nil {
			sp.emit(Event{Kind: "error", SpaceID: sp.id, Message: err.Error()})
			break
		}
		sp.createTab(t, t.url)
		created++
	}
	if created == 0 {
		// 一个都没建出来（比如地址全失效）：至少留一个首页标签，别留下空壳
		if t, err := sp.reserveTab(sp.home, true); err == nil {
			sp.createTab(t, t.url)
		}
	}
	sp.applyActive()
	sp.emitTabs()
}

// openTabFromPage 页面里 target=_blank / window.open 时由宿主接过来开新标签。
func (sp *space) openTabFromPage(uri string) {
	t, err := sp.reserveTab(uri, true)
	if err != nil {
		sp.emit(Event{Kind: "error", SpaceID: sp.id, Message: err.Error()})
		return
	}
	sp.createTab(t, t.url)
	sp.applyActive()
	sp.emitTabs()
	sp.emit(Event{Kind: "newWindow", SpaceID: sp.id, TabID: t.id, URL: t.url})
}

// destroyTab 在空间线程上销毁一个标签的 controller 与宿主窗口。
func (sp *space) destroyTab(t *tab) {
	if t == nil {
		return
	}
	if t.c != nil {
		t.c.ShuttingDown()
		if ctl := t.c.GetController(); ctl != nil {
			_ = ctl.Close()
		}
		t.c = nil
	}
	if t.host != 0 {
		procDestroyWindow.Call(t.host)
		t.host = 0
	}
}

// resizeTab 把标签宿主窗口铺满空间宿主窗口，并让 controller 跟着改尺寸。
func (sp *space) resizeTab(t *tab) {
	if t.host == 0 {
		return
	}
	w := int32(float64(sp.boundsW()) * sp.scale)
	h := int32(float64(sp.boundsH()) * sp.scale)
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	procSetWindowPos.Call(t.host, 0, 0, 0, uintptr(w), uintptr(h), swpNoZOrder|swpNoActivate)
	if t.c != nil {
		r := edge.Rect{Left: 0, Top: 0, Right: w, Bottom: h}
		t.c.ResizeWithBounds(&r)
		if ctl := t.c.GetController(); ctl != nil {
			_ = ctl.NotifyParentWindowPositionChanged()
		}
	}
}

// applyActive 按当前活动标签显示/隐藏各标签窗口，并把活动标签提到最上层。
func (sp *space) applyActive() {
	sp.mu.Lock()
	active := sp.active
	tabs := sp.tabListLocked()
	sp.mu.Unlock()

	for _, t := range tabs {
		if t.host == 0 {
			continue
		}
		if t.id == active {
			procShowWindow.Call(t.host, swShow)
			if t.c != nil {
				_ = t.c.Show()
			}
			sp.resizeTab(t)
			procSetWindowPos.Call(t.host, hwndTop, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoActivate)
		} else {
			if t.c != nil {
				_ = t.c.Hide()
			}
			procShowWindow.Call(t.host, swHide)
		}
	}
}

// setVisible 显示/隐藏整块网页区（必须在空间线程上调用）。
//
// 打开设置抽屉时必须先隐藏：网页区是真正的原生子窗口，永远画在 HTML 之上，
// 抽屉、弹层这些前端组件都会被它压住看不见。
func (sp *space) setVisible(v bool) {
	if sp.host == 0 {
		return
	}
	sp.mu.Lock()
	sp.hidden = !v
	sp.mu.Unlock()
	if !v {
		procShowWindow.Call(sp.host, swHide)
		return
	}
	// 恢复时重新摆一遍：隐藏期间窗口可能被拖动/缩放过
	sp.applyBounds()
	sp.applyActive()
	procShowWindow.Call(sp.host, swShow)
	sp.raiseHost()
}

// focusShell 把键盘焦点交回前端页面。
//
// 网页区是父窗口下的另一个子窗口，只要它持有焦点，前端输入框就算拿到了 DOM
// 焦点也收不到按键（按键全被 WebView2 吃掉了）。所以 Ctrl+L 这类“去焦点到
// 地址栏”的操作必须显式把 Win32 焦点换回 Wails 自己的 webview 子窗口。
func (sp *space) focusShell() {
	var target uintptr
	cb := syscall.NewCallback(func(child uintptr, _ uintptr) uintptr {
		if child == sp.host {
			return 1
		}
		// 只要直接子窗口：标签宿主是空间宿主的子窗口，属于要跳过的子树
		p, _, _ := procGetParent.Call(child)
		if p != sp.hwnd {
			return 1
		}
		if v, _, _ := procIsWindowVisible.Call(child); v == 0 {
			return 1
		}
		if target == 0 {
			target = child
		}
		return 1
	})
	procEnumChildWindows.Call(sp.hwnd, cb, 0)
	if target == 0 {
		target = sp.hwnd
	}
	procSetFocus.Call(target)
}

// syncTabState 从页面拉一次地址/标题/前进后退状态，有变化就上报。
func (sp *space) syncTabState(t *tab, wv *edge.ICoreWebView2) {
	if wv == nil {
		return
	}
	url, _ := wv.GetSource()
	title, _ := wv.GetDocumentTitle()
	back, _ := wv.GetCanGoBack()
	fwd, _ := wv.GetCanGoForward()

	t.mu.Lock()
	changed := false
	titleChanged := false
	if url != "" && url != t.url {
		t.url = url
		changed = true
	}
	if title != "" && title != t.title {
		t.title = title
		titleChanged = true
		changed = true
	}
	if back != t.canBack || fwd != t.canFwd {
		t.canBack, t.canFwd = back, fwd
		changed = true
	}
	curURL, curTitle := t.url, t.title
	t.mu.Unlock()

	if changed {
		sp.emit(Event{Kind: "state", SpaceID: sp.id, TabID: t.id, URL: curURL, Title: curTitle})
	}
	if titleChanged {
		sp.emit(Event{Kind: "title", SpaceID: sp.id, TabID: t.id, Title: curTitle})
	}
}

// onPageMessage 处理页面 postMessage 回来的两类消息：快捷键与标题回报。
func (sp *space) onPageMessage(t *tab, text string) {
	switch {
	case strings.HasPrefix(text, "go-tools:key:"):
		sp.emit(Event{Kind: "shortcut", SpaceID: sp.id, TabID: t.id, Message: text[len("go-tools:key:"):]})
	case strings.HasPrefix(text, "go-tools:title:"):
		title := text[len("go-tools:title:"):]
		t.mu.Lock()
		t.title = title
		t.mu.Unlock()
		sp.emit(Event{Kind: "title", SpaceID: sp.id, TabID: t.id, Title: title})
	}
}

// ============================================================
// 尺寸与 z-order
// ============================================================

func (sp *space) boundsW() int {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.bounds.W
}

func (sp *space) boundsH() int {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	return sp.bounds.H
}

func (sp *space) setBounds(r Rect) {
	sp.mu.Lock()
	sp.bounds = r
	sp.mu.Unlock()
	sp.call(sp.applyBounds)
}

// applyBounds 把 DIP 矩形换算成物理像素：空间宿主窗口摆到目标位置，标签铺满它。
//
// edge 包创建 controller 时启用了 COREWEBVIEW2_BOUNDS_MODE_USE_RAW_PIXELS 并关掉了
// 显示器缩放自动跟随，所以这里必须自己乘 dpi/96。
func (sp *space) applyBounds() {
	if sp.host == 0 {
		return
	}
	sp.mu.Lock()
	b := sp.bounds
	tabs := sp.tabListLocked()
	sp.mu.Unlock()

	scale := sp.scale
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
	for _, t := range tabs {
		sp.resizeTab(t)
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

// ============================================================
// 生命周期
// ============================================================

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
		for _, t := range sp.tabListLocked() {
			sp.destroyTab(t)
		}
		if sp.host != 0 {
			procDestroyWindow.Call(sp.host)
			sp.host = 0
		}
		procPostQuitMessage.Call(0)
	})
	sp.pendingMu.Unlock()
	if sp.tid != 0 {
		procPostThreadMessageW.Call(uintptr(sp.tid), wmAppCall, 0, 0)
	}

	if sp.onClosed != nil {
		sp.onClosed()
	}
}

func (sp *space) emit(ev Event) {
	if sp.onEvent != nil {
		sp.onEvent(ev)
	}
}

// snapshot 生成标签快照（纯数据，可从任意线程调用）。
func (sp *space) snapshot() SpaceInfo {
	sp.mu.Lock()
	defer sp.mu.Unlock()
	info := SpaceInfo{ID: sp.id, ActiveTab: sp.active, MaxTabs: sp.maxTabs}
	for _, t := range sp.tabListLocked() {
		t.mu.Lock()
		info.Tabs = append(info.Tabs, TabInfo{
			ID:           t.id,
			Title:        t.title,
			URL:          t.url,
			Active:       t.id == sp.active,
			Loading:      t.loading,
			CanGoBack:    t.canBack,
			CanGoForward: t.canFwd,
			Suspended:    t.suspended,
			Error:        t.errMsg,
		})
		t.mu.Unlock()
	}
	return info
}

// emitTabs 上报标签结构变化。
func (sp *space) emitTabs() {
	info := sp.snapshot()
	sp.emit(Event{Kind: "tabs", SpaceID: sp.id, Tabs: info.Tabs, ActiveTab: info.ActiveTab})
}

// ============================================================
// 宿主窗口
// ============================================================

// hostClassOnce 注册一次宿主子窗口类（进程级，幂等）。
var hostClassOnce sync.Once

// createChildWindowPx 在调用线程上建一个子窗口（坐标/尺寸为物理像素）。
//
// 为什么不直接把 Wails 窗口的 HWND 交给 controller：controller 必须与父窗口同线程
// （WebView2 会向父窗口所在线程同步发消息），而 Wails 的窗口在 Wails 自己的线程上创建。
// 跨线程父窗口会让 CreateCoreWebView2Controller 永不返回（实测：environment 回调已
// 同步返回 res=0，紧接着就卡死在 controller 创建）。
// 解法：在空间线程上建 WS_CHILD 子窗口挂到 Wails 窗口下——父子窗口允许跨线程，
// 位置随父窗口移动，输入/焦点走子窗口自己的线程。
func createChildWindowPx(parent uintptr, x, y, w, h int32, visible bool) (uintptr, error) {
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
	style := uintptr(wsChild | wsClipChildren | wsClipSiblings)
	if visible {
		style |= wsVisible
	}
	if w < 0 {
		w = 0
	}
	if h < 0 {
		h = 0
	}
	hwnd, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(cls)),
		0,
		style,
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, 0, 0, 0,
	)
	if hwnd == 0 {
		return 0, fmt.Errorf("创建网页区宿主子窗口失败: %v", callErr)
	}
	return hwnd, nil
}

func isWindow(hwnd uintptr) uintptr {
	r, _, _ := procIsWindow.Call(hwnd)
	return r
}

func dpiForWindow(hwnd uintptr) uint32 {
	r, _, _ := procGetDpiForWindow.Call(hwnd)
	if int32(r) <= 0 {
		return 96
	}
	return uint32(r)
}

// ============================================================
// 页面注入脚本
// ============================================================

// shortcutScript 注入到每个页面的快捷键捕获脚本。
//
// 键盘事件发生在网页区（原生窗口）里，前端 Vue 收不到，所以在这里拦下来
// 通过 postMessage 交回宿主，再由宿主转成事件让前端处理。
// 只拦浏览器的常规快捷键，其余一律放行给页面。
const shortcutScript = `(function(){
  if (window.__goToolsKeys) { return; }
  window.__goToolsKeys = 1;
  function post(a) {
    try { window.chrome.webview.postMessage('go-tools:key:' + a); } catch (e) {}
  }
  window.addEventListener('keydown', function(e){
    var k = e.key;
    if (e.ctrlKey && !e.altKey && !e.metaKey && (k === 't' || k === 'T')) { e.preventDefault(); post('newtab'); return; }
    if (e.ctrlKey && !e.altKey && !e.metaKey && (k === 'w' || k === 'W')) { e.preventDefault(); post('closetab'); return; }
    if (e.ctrlKey && !e.altKey && (k === 'l' || k === 'L')) { e.preventDefault(); post('focusurl'); return; }
    if (e.ctrlKey && !e.altKey && (k === 'r' || k === 'R')) { e.preventDefault(); post('reload'); return; }
    if (k === 'F5') { e.preventDefault(); post('reload'); return; }
    if (e.ctrlKey && k === 'Tab') { e.preventDefault(); post(e.shiftKey ? 'prevtab' : 'nexttab'); return; }
    if (e.altKey && k === 'ArrowLeft') { e.preventDefault(); post('back'); return; }
    if (e.altKey && k === 'ArrowRight') { e.preventDefault(); post('forward'); return; }
  }, true);
})();`
