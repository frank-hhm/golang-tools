//go:build windows

package app

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"Tools/services/applog"
	"Tools/services/config"
	"Tools/services/winui"
	"Tools/tools/screenshot"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// 截图选区浮层（WebView 版）。
//
// 旧实现是一整块 GDI 全屏遮罩（tools/screenshot/selector_windows.go），图标要
// 逐像素手绘、文字标注接不了输入法。现在改成：Go 只负责「冻结哪一帧」和
// 「哪些窗口可整窗截取」，选区/控制点/工具栏/标注全部交给 WebView 页面
// /widget/capture 渲染，图标直接用 Arco 的 SVG 组件。
//
// 浮层用「透明窗口 + 冻结帧背景」：页面把冻结帧作为唯一不透明层铺满，视觉上等价
// 于「屏幕被定住」。之所以要透明而不是实心黑：全屏窗口首次 Show 时 WebView2 往
// 往还没把页面合成出来，会先亮一帧窗口背景色——实心黑底就是那一下「黑屏闪动」。
// 背景透明时这一帧露出的是真实桌面（与冻结帧几乎一致），闪动自然消失。
// 也因此必须先抓屏、后建窗——顺序颠倒会把浮层自己截进冻结帧里。

// captureOverlayName 浮层窗口名，兼作单例判据。
const captureOverlayName = "win:/widget/capture"

// captureReadyTimeout 浮层页面加载超时保护：超时说明前端没起来（路由缺失、
// JS 抛错、绑定未生成），此时宁可关掉窗口也不能留一个看不见的僵尸挡住下次截图。
const captureReadyTimeout = 6 * time.Second

// captureHoldTimeout 浮层调起后一直没人收场（拉起来就去干别的、也不按 ESC）
// 时的兜底回收。capBusy 一旦占住，后续热键会被静默忽略，所以不能无限期等。
// 取 90 秒：正常框选/标注根本用不到这么久，而一个吃点击的全屏窗口压着不放的
// 代价（老机器上就是「屏幕啥都点不了」），比误收一张正在编辑的图高得多。
const captureHoldTimeout = 90 * time.Second

// capturePingTimeout 浮层点亮后页面心跳的最长间隔。页面每 500ms 报一次活，
// 连续这么久一次都没来就说明 JS 线程/合成器已经卡死：窗口仍会吃掉屏幕上所有
// 点击，必须直接收掉（老机器被别的程序抢满 CPU 时就是这个表现）。
//
// 取 6s（等于漏掉 12 次心跳）而不是更短：机器被压满时 JS 定时器本身就会被推迟
// 一两秒，卡得太紧会误收用户正在用的浮层——那是比“慢”更讨厌的体验。
const capturePingTimeout = 6 * time.Second

// capturePaintTimeout 点亮之后首帧上屏的容忍时长。超过它仍未收到「已上屏」回报，
// 说明合成卡住了：用户看不到画面，点击却已经被浮层拦下，同样直接收窗。
// 这个窗口比心跳窗口更危险（画面都没有，用户只会以为“屏幕死了”），所以看紧一点。
const capturePaintTimeout = 5 * time.Second

// captureSlowPaint 「点亮 → 首帧上屏」超过这个时长即记为慢机器：下一轮改用实心
// 背景重建窗口，绕开分层透明窗口在老集显上的软件合成开销。
const captureSlowPaint = 800 * time.Millisecond

// captureEscGrace 兜底 ESC 命中后等页面自己收场的宽限：页面活着时优先让它按
// 自己的语义处理（正在输入文字时 ESC 只退出输入态，不取消整张截图）。
// 1.2s：页面收场要等两帧合成（把浮层擦成透明）+ 一趟绑定往返，机器吃紧时
// 两帧就可能要几百毫秒。
const captureEscGrace = 1200 * time.Millisecond

// OpenCaptureOverlay 冻结主屏一帧并打开全屏选区编辑浮层。
// 重复触发（热键连按）时直接忽略，避免把已经打开的浮层再刷一遍。
func (a *App) OpenCaptureOverlay() error {
	if a.app == nil {
		return fmt.Errorf("应用尚未初始化")
	}
	a.capMu.Lock()
	if a.capBusy {
		a.capMu.Unlock()
		return nil
	}
	a.capBusy = true
	a.capGen++
	gen := a.capGen
	a.capMu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			applog.Errorf("capture", "打开选区浮层 panic: %v", r)
			// 关窗+复位，避免遗留一个半死的浮层既抓不住又挡住下次截图。
			a.destroyCaptureOverlay()
		}
	}()

	// 立即抓屏。旧实现在此固定 sleep 250ms 等托盘菜单/通知卡片消失，但每次
	// “调起”都要干等这 250ms，老机器上尤其拖慢手感；瞬态提示被定格只是
	// 极少数场景，不值得每次截图都付这段延迟。
	t0 := time.Now()
	frame, err := screenshot.CaptureScreenFrame()
	if err != nil {
		a.resetCapBusy()
		return err
	}
	tFrame := time.Since(t0)

	// 帧先存好再处置窗口：冷建时页面 mount 后会自己来拉（GetCaptureFrame），
	// 预热窗口则靠下面的事件告知“有新帧”。顺序反了会拉到上一轮的旧帧。
	a.capMu.Lock()
	a.plat.capFrame = frame
	a.capReady = false
	// 新的一轮：清掉上一轮的心跳/上屏状态，否则看门狗会拿旧时间戳开始计时
	a.capShown = false
	a.capPainted = false
	a.capPingAt = time.Time{}
	a.capShowAt = time.Time{}
	a.capMu.Unlock()

	win, created := a.ensureCaptureWindow(frame)
	if win == nil {
		a.resetCapBusy()
		return fmt.Errorf("创建选区浮层窗口失败")
	}
	var how string
	if created {
		how = "冷建"
	} else {
		how = "复用"
	}
	applog.Infof("capture", "冻结帧就绪 %dx%d dpr=%.2f 带=%d 抓屏+编码=%s 浮层=%s 可截窗口=%d 个",
		frame.W, frame.H, frame.DPR, len(frame.Bands), tFrame.Round(time.Millisecond),
		how, len(frame.Windows))

	// 复用（含启动时预热）时必须推事件：页面早已 mount，不会再自己取帧。
	// 帧随事件一起送出，省掉「页面回调 GetCaptureFrame」这一整趟往返（帧体 ~650KB）。
	if !created {
		a.app.Event.Emit("capture:newframe", frame)
	}
	// 兜底热键：只要浮层可能吃掉点击，ESC / Ctrl+Alt+Esc 就必须是原生的，
	// 不能只靠页面里的 keydown（页面卡死或压根没获焦时就彻底没退路了）。
	a.armCaptureGuard()
	// 兜底：前端若因故没能回报就绪，超时后销毁窗口，别把后续截图卡死。
	// 两个定时器都只在“自己这一轮还没收场”时才动手（见 capGen）。
	go a.captureReadyWatchdog(gen)
	go a.captureIdleRecycle(gen)
	return nil
}

// resetCapBusy 复位一次截图的进行标志（失败路径用，避免浮层被永久锁住）。
func (a *App) resetCapBusy() {
	a.capMu.Lock()
	a.capBusy = false
	a.capMu.Unlock()
}

// captureIdleRecycle 浮层长时间没人收场时的回收。
// hideCaptureOverlay/destroyCaptureOverlay 都会把 capBusy 置回 false，因此若
// captureHoldTimeout 之后本轮 capBusy 仍为 true，就等价于「这一轮没人正经收场」：
// 只隐窗（窗口对象保留供下次复用）并释放标志，不让后续热键被静默吞掉。
// 靠 gen 认轮：不带轮次判据的话，上一轮的定时器会提前关掉用户正在用的新浮层。
func (a *App) captureIdleRecycle(gen uint64) {
	time.Sleep(captureHoldTimeout)
	a.capMu.Lock()
	held := a.capGen == gen && a.capBusy
	a.capMu.Unlock()
	if !held {
		return
	}
	applog.Warnf("capture", "浮层 %s 无人收场，自动隐藏并释放截图标志", captureHoldTimeout)
	a.abortCaptureOverlay("截图浮层停留过久，已自动关闭")
}

// TryActivateNativeTool 供 Go 原生入口（全局热键、浮动球菜单）直接触发工具事件：
// 走前端要多付一趟「热键线程 → 主窗口 JS → 绑定调用 → Go」的往返，实测十几毫秒，
// 而截图浮层本来就是 Go 自己开的窗口；顺带也让主窗口已关闭时热键照样能截图。
// 返回 true 表示已在 Go 侧处理完毕，调用方不必再广播给前端。
func (a *App) TryActivateNativeTool(toolID string) bool {
	if a.app == nil || toolID == "" {
		return false
	}
	for _, t := range config.CurrentConfig().Tools {
		if t.Event != "screenshot" || (t.ID != toolID && t.Key != toolID) {
			continue
		}
		go func() {
			if err := a.OpenCaptureOverlay(); err != nil {
				applog.Errorf("capture", "原生入口直接调起浮层失败: %v", err)
			}
		}()
		return true
	}
	return false
}

// ensureCaptureWindow 返回浮层窗口；created 为 true 表示本轮是冷建
// （页面即将自己 mount 取帧），false 表示复用已有窗口（页面在等 newframe 事件）。
// 建窗期间单独持锁，避免抱着 capMu 做耗时操作。
func (a *App) ensureCaptureWindow(frame *screenshot.CaptureFrame) (win application.Window, created bool) {
	a.capWinMu.Lock()
	defer a.capWinMu.Unlock()

	a.capMu.Lock()
	exist := a.capWin
	a.capMu.Unlock()
	if exist != nil {
		return exist, false
	}
	win = a.newCaptureWindow(frame)
	if win == nil {
		return nil, true
	}
	a.capMu.Lock()
	a.capWin = win
	a.capMu.Unlock()
	return win, true
}

// newCaptureWindow 全屏无边框、隐藏创建的选区浮层窗口。尺寸按冻结帧换算成 DIP：
// 必须显式给一整屏尺寸，隐藏窗口拿不到默认尺寸时前端实测的 dpr 会全错。
// 真正点亮由前端 CaptureReady 触发。
//
// 这里绝对不能设 StartState=Fullscreen：Wails(beta.12) 的 fullscreen() 会给窗口
// 强行加上 WS_VISIBLE 并 SetWindowPos(HWND_TOP)，等于把 Hidden 作废——一个透明、
// 未合成内容、置顶且铺满整屏的窗口就此常驻，看不见却吃掉屏幕上所有点击
// （预热时就是这种状态，只有截一次图再收场才会 SW_HIDE 恢复）。预热与冷建都需要
// 窗口保持真隐藏，进全屏的动作已经在 CaptureReady 里按正确顺序显式做了。
func (a *App) newCaptureWindow(frame *screenshot.CaptureFrame) application.Window {
	dpr := frame.DPR
	if dpr <= 0 {
		dpr = 1
	}
	screenW := int(float64(frame.W) / dpr)
	screenH := int(float64(frame.H) / dpr)

	// 背景策略：默认透明（首帧合成前露出真实桌面，没有黑屏闪动）；但若实测到这台
	// 机器点亮后迟迟出不来首帧，说明分层透明窗口（WS_EX_LAYERED，老集显走软件
	// 合成）拖不动整屏重绘，那就换成实心背景——代价是首帧前可能闪一帧底色，
	// 换来画面能真的出来，而不是「窗口挡着点击却什么都不显示」。
	solid := a.pickCaptureBackground()
	bgType := application.BackgroundTypeTransparent
	bgColour := application.NewRGBA(0, 0, 0, 0)
	if solid {
		bgType = application.BackgroundTypeSolid
		bgColour = application.NewRGB(24, 24, 24)
	}
	win := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:          captureOverlayName,
		Title:         "截图",
		X:             0,
		Y:             0,
		Width:         screenW,
		Height:        screenH,
		Frameless:     true,
		AlwaysOnTop:   true,
		DisableResize: true,
		Hidden:        true, // 等前端把冻结帧画出来再显示，避免黑屏一闪（见上方注释：不可叠加 Fullscreen）
		// 透明背景：Show 后、页面首帧合成前的那一帧露出桌面而非黑色（见文件头注释）。
		// 也只有配合 IgnoreMouseEvents 才会点击穿透，这里不设，交互照常。
		BackgroundType:   bgType,
		BackgroundColour: bgColour,
		URL:              "/#/widget/capture",
		Windows: application.WindowsWindow{
			// 全屏浮层不要 Win11 圆角与投影，否则四角会露出真实桌面
			DisableFramelessWindowDecorations: true,
			// 一次性的交互浮层，不该在任务栏占位
			HiddenOnTaskbar: true,
		},
	})
	applog.Infof("capture", "选区浮层窗口已创建（隐藏）%dx%d dpr=%.2f 背景=%s", screenW, screenH, dpr, bgName(solid))
	return win
}

// pickCaptureBackground 决定本轮建窗用不用实心背景，并把结果记为当前窗口的实际形态。
// 只在实测过首帧合成过慢（capSlow）时用实心；一旦切过去就不再切回：老机器不会
// 因为截了几张图就变快，来回重建反而每轮都要重付一次 WebView2 冷启动开销。
func (a *App) pickCaptureBackground() bool {
	a.capMu.Lock()
	defer a.capMu.Unlock()
	a.capSolid = a.capSlow
	return a.capSolid
}

func bgName(solid bool) string {
	if solid {
		return "实心（已避开分层合成）"
	}
	return "透明"
}

// PrewarmCaptureOverlay 预建隐藏的浮层窗口（不抓屏），把“新建 WebView2 窗口 +
// 页面 bootstrap”这笔最贵的开销从第一次截图的体验里挪到启动阶段。
// 预热窗口必须真的是隐藏的：既不会被下次 GDI 抓屏抓进去（SW_HIDE 窗口不渲染），
// 也不会被枚举成可截窗口（本进程窗口已过滤），更不会挡住桌面点击。
func (a *App) PrewarmCaptureOverlay() {
	if a.app == nil {
		return
	}
	w, h, dpr := screenshot.ScreenGeometry()
	if w <= 0 || h <= 0 {
		return
	}
	a.capWinMu.Lock()
	defer a.capWinMu.Unlock()
	a.capMu.Lock()
	exist := a.capWin
	a.capMu.Unlock()
	if exist != nil {
		return
	}
	if win := a.newCaptureWindow(&screenshot.CaptureFrame{W: w, H: h, DPR: dpr}); win != nil {
		a.capMu.Lock()
		a.capWin = win
		a.capMu.Unlock()
		applog.Infof("capture", "浮层已预热（隐藏待命，不拦截点击）%dx%d dpr=%.2f", w, h, dpr)
	}
}

// GetCaptureFrame 浮层页面取回冻结帧、屏幕尺寸、光标位置与可截取窗口清单。
// 这里不清空：开发模式 HMR 刷新后页面会重新取一次，拿不到就只剩黑屏；
// 帧的释放在 CloseCaptureOverlay 里统一做。
func (a *App) GetCaptureFrame() *screenshot.CaptureFrame {
	a.capMu.Lock()
	defer a.capMu.Unlock()
	return a.plat.capFrame
}

// CaptureReady 浮层已完成首帧渲染，可以亮出来了。
func (a *App) CaptureReady() {
	now := time.Now()
	a.capMu.Lock()
	w := a.capWin
	gen := a.capGen
	a.capReady = true
	// 点亮之后的存活计时从这里起算：心跳和首帧上屏都在亮出来之后才有意义
	a.capShown = true
	a.capShowAt = now
	a.capPingAt = now
	a.capMu.Unlock()
	if w == nil {
		return
	}
	// 先把几何定死成整屏，再显示：若先 Show 后 Fullscreen，窗口会以创建尺寸
	// 亮出第一帧（此刻 WebView2 尚未合成，露的是黑色 BackgroundColour），
	// Fullscreen 再触发一次重排——用户就看到「黑屏闪一下」。顺序反过来，
	// 首个可见帧就已经是铺满且画好冻结帧的，闪动消失。
	w.SetPosition(0, 0)
	w.Fullscreen()
	w.Show()
	w.Focus()
	// 亮着的时候才盯着它活不活（见 captureAliveWatchdog）
	go a.captureAliveWatchdog(gen)
}

// CapturePing 浮层页面心跳：可见期间每 ~500ms 报一次活。
//
// 为什么需要它：浮层是全屏、置顶、默认吃点击的窗口。页面一旦因机器吃紧（别的程序
// 抢满 CPU/内存）而冻住，用户看到的就是「画面停在那，整屏什么都点不了」——除了
// 任务管理器没有任何退路。有了心跳，Go 侧就能在一两秒内自己把窗口收掉。
// painted=true 表示这是「首帧已上屏」后的第一次心跳，用来量点亮到真正出画面的耗时。
func (a *App) CapturePing(painted bool) {
	now := time.Now()
	a.capMu.Lock()
	if !a.capBusy || !a.capShown {
		// 已经收过场（或还没点亮）：旧页面的定时器迟到的包一律忽略
		a.capMu.Unlock()
		return
	}
	a.capPingAt = now
	slow := false
	if painted && !a.capPainted {
		a.capPainted = true
		d := now.Sub(a.capShowAt)
		if d > captureSlowPaint {
			a.capSlowHits++
		} else {
			a.capSlowHits = 0
		}
		// 连续两次都慢才改背景：冷建第一张图时合成器首次为该窗口落地，
		// 偶尔也会超过阈值，单次就切会把快机器也误判成慢机器。
		if a.capSlowHits >= 2 && !a.capSlow {
			a.capSlow = true
			slow = true
		}
	}
	a.capMu.Unlock()
	if slow {
		applog.Warnf("capture", "连续两次点亮→首帧上屏超过 %s，下次改用实心背景重建浮层", captureSlowPaint)
	}
}

// captureAliveWatchdog 点亮后的存活看门狗：页面心跳断了或首帧迟迟不上屏，
// 就直接把浮层收掉——宁可丢一次截图，也不能留一个看不见（或停住）却吃掉全部
// 点击的全屏窗口压着桌面。
//
// 与 captureReadyWatchdog 分工：那个管「还没点亮就跑死」，这个管「点亮之后跑死」。
// 同样靠 gen 认轮，避免上一轮的定时器关掉用户正在用的新浮层。
func (a *App) captureAliveWatchdog(gen uint64) {
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for range tick.C {
		now := time.Now()
		a.capMu.Lock()
		if a.capGen != gen || !a.capBusy || !a.capShown {
			a.capMu.Unlock()
			return
		}
		dead := now.Sub(a.capPingAt) > capturePingTimeout
		blind := !a.capPainted && now.Sub(a.capShowAt) > capturePaintTimeout
		silent := now.Sub(a.capPingAt)
		litFor := now.Sub(a.capShowAt)
		a.capMu.Unlock()
		switch {
		case dead:
			applog.Warnf("capture", "浮层心跳中断 %s（>%s），自动关闭", silent.Round(time.Millisecond), capturePingTimeout)
			a.abortCaptureOverlay("截图浮层无响应，已自动关闭")
			return
		case blind:
			applog.Warnf("capture", "浮层点亮 %s 仍未出首帧（>%s），自动关闭", litFor.Round(time.Millisecond), capturePaintTimeout)
			a.abortCaptureOverlay("截图画面未能显示，已自动关闭")
			return
		}
	}
}

// abortCaptureOverlay 异常收场：收起浮层 + 提示用户（否则用户只会发现「截图没出来」，
// 完全不知道刚才那一下是怎么回事）。提示用原生小窗，不依赖浮层自己的页面。
func (a *App) abortCaptureOverlay(text string) {
	a.hideCaptureOverlay()
	a.ShowMessage("warning", text)
}

// armCaptureGuard 登记截图期间的原生兜底热键。
//
// 页面里的 keydown 只在「页面活着且窗口有焦点」时生效，而这正是出问题时的两个前提。
// 原生热键由系统投递，不经过 WebView：页面卡死、画面停住都能靠它收场。
func (a *App) armCaptureGuard() {
	winui.SetGuardHotkeys([]winui.GuardHotkey{
		{ID: "capture-esc", Shortcut: "Esc", Fn: a.captureEscFromGuard},
		{ID: "capture-force-close", Shortcut: "Ctrl+Alt+Esc", Fn: func() {
			applog.Warnf("capture", "兜底热键 Ctrl+Alt+Esc：直接关闭浮层")
			a.hideCaptureOverlay()
		}},
	})
}

// disarmCaptureGuard 注销兜底热键（收场时必须调，否则 ESC 一直被本进程抢着）。
func (a *App) disarmCaptureGuard() {
	winui.ClearGuardHotkeys()
}

// captureEscFromGuard 兜底 ESC：页面活着时先让它按自己的语义处理——正在输入文字时
// ESC 只退出输入态，不该取消整张截图；页面没响应就直接收窗。
func (a *App) captureEscFromGuard() {
	a.capMu.Lock()
	alive := a.capBusy && a.capShown && !a.capPingAt.IsZero() &&
		time.Since(a.capPingAt) < capturePingTimeout
	gen := a.capGen
	a.capMu.Unlock()
	if alive && a.app != nil {
		a.app.Event.Emit("capture:esc")
		time.Sleep(captureEscGrace)
		a.capMu.Lock()
		still := a.capGen == gen && a.capBusy && a.capShown
		a.capMu.Unlock()
		if !still {
			return
		}
	}
	applog.Warnf("capture", "兜底热键 Esc：页面未响应，直接关闭浮层")
	a.hideCaptureOverlay()
}

// CaptureFinish 结束一次截图：成品图写剪贴板，按需打开预览窗口，最后收掉浮层。
// openPreview 为 true 时额外打开（或刷新）截图预览窗口；
// 勾号/回车/双击走 false——只复制、直接收场，不打扰当前工作。
func (a *App) CaptureFinish(data string, openPreview bool) error {
	if data != "" {
		if err := screenshot.CopyImageBase64(data); err != nil {
			// 浮层不能关，否则前端拿不到错误也就无从提示
			a.CloseCaptureOverlay()
			return fmt.Errorf("写入剪贴板失败: %w", err)
		}
		if openPreview {
			if err := a.OpenToolWindow("/win/tools/screenshot?fs=0", "截图预览", true, false, 0, 0); err != nil {
				a.CloseCaptureOverlay()
				return err
			}
			// 预览窗已存在（single 复用）时它不会重新 mount，靠事件驱动重读 localStorage
			a.app.Event.Emit("screenshot:updated")
		}
	}
	applog.Infof("capture", "截图完成 copy=%v preview=%v %dKB", data != "", openPreview, len(data)/1024)
	// 关窗放在本次调用返回之后：浮层就是这次调用的发起方，同步销毁它会让
	// 响应永远回不去，极端情况下还会撞上 WebView2 正在序列化的回调。
	a.closeOverlayLater(150 * time.Millisecond)
	return nil
}

// CaptureCancel 放弃本次截图（ESC / 右键 / ✕）。
func (a *App) CaptureCancel() {
	applog.Infof("capture", "截图已取消")
	a.closeOverlayLater(0)
}

// closeOverlayLater 延迟收场浮层，给当前这次绑定调用留出返回的时机。
// 正常结束时隐藏而非关闭，以便下次截图复用同一个窗口。
func (a *App) closeOverlayLater(d time.Duration) {
	time.AfterFunc(d, a.hideCaptureOverlay)
}

// CaptureLog 浮层回写一条诊断日志。
//
// 浮层是隐藏创建、由前端自己点亮的全屏窗口，一旦 JS 在 CaptureReady 之前抛错，
// 用户只会看到黑屏（或什么都没有），而 Go 日志里一片干净。留一个回写口子，
// 让前端能把实测视口、dpr 与异常带回来，否则这类问题根本无从定位。
func (a *App) CaptureLog(note string) {
	note = strings.TrimSpace(note)
	if note == "" {
		return
	}
	applog.Infof("capture", "浮层: %s", note)
}

// SaveCaptureFile 弹出原生「另存为」对话框（默认落在配置的截图目录 data/screenshots），
// 用户确认后把成品 PNG 写到所选路径。返回最终路径；用户取消则返回空串、不报错。
func (a *App) SaveCaptureFile(data string) (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	if data == "" {
		return "", fmt.Errorf("没有可保存的图片数据")
	}
	dlg := a.app.Dialog.SaveFile().
		SetFilename(fmt.Sprintf("screenshot-%d.png", time.Now().UnixMilli())).
		SetMessage("保存截图").
		AddFilter("PNG 图片 (*.png)", "*.png").
		AddFilter("所有文件 (*.*)", "*.*")
	// 默认目录用已配置的截图目录（<exedir>/data/screenshots），创建失败则退回系统默认
	if dir, derr := a.AppDataDir("screenshots"); derr == nil {
		dlg = dlg.SetDirectory(dir)
	}
	a.capMu.Lock()
	parent := a.capWin
	a.capMu.Unlock()
	if parent != nil {
		dlg = dlg.AttachToWindow(parent)
	}
	path, err := dlg.PromptForSingleSelection()
	if err != nil {
		// 用户取消对话框不算错误（Wails 内部返回 "cancelled by user"）
		if strings.Contains(strings.ToLower(err.Error()), "cancel") {
			return "", nil
		}
		return "", err
	}
	if path == "" {
		return "", nil
	}
	if !strings.EqualFold(filepath.Ext(path), ".png") {
		path += ".png"
	}
	if err := screenshot.SaveImageFile(path, data); err != nil {
		return "", fmt.Errorf("保存失败: %w", err)
	}
	applog.Infof("capture", "截图已保存: %s", path)
	return path, nil
}

// OpenPinnedImage 把一张截图钉成无边框置顶小窗，出现在原选区的屏幕位置。
// x/y/w/h 为选区的物理像素坐标（前端传过来），按 dpr 换算成 DIP 作为窗口几何。
// 返回图钉 id，页面据 id 从 GetPinnedImage 拉图、从 ClosePinnedImage 关窗。
func (a *App) OpenPinnedImage(data string, x, y, w, h int) (int, error) {
	if a.app == nil {
		return 0, fmt.Errorf("应用尚未初始化")
	}
	if data == "" {
		return 0, fmt.Errorf("没有可钉住的图片")
	}
	if w <= 0 || h <= 0 {
		return 0, fmt.Errorf("选区尺寸非法")
	}
	a.capMu.Lock()
	f := a.plat.capFrame
	a.capMu.Unlock()
	dpr := 1.0
	if f != nil && f.DPR > 0 {
		dpr = f.DPR
	}
	dx, dy := int(float64(x)/dpr), int(float64(y)/dpr)
	dw, dh := int(float64(w)/dpr), int(float64(h)/dpr)
	if dw < 24 {
		dw = 24
	}
	if dh < 24 {
		dh = 24
	}
	// 顶部预留公共标题栏高度（与工具窗 BlankLayout 一致 40px），图片在其下方按原选区尺寸显示
	const pinBarH = 40

	a.pinMu.Lock()
	a.pinSeq++
	id := a.pinSeq
	if a.pinData == nil {
		a.pinData = map[int]string{}
	}
	if a.pinWins == nil {
		a.pinWins = map[int]application.Window{}
	}
	a.pinData[id] = data
	a.pinMu.Unlock()

	win := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:          fmt.Sprintf("win:/widget/pin#%d", id),
		Title:         "图钉",
		X:             dx,
		Y:             dy,
		Width:         dw,
		Height:        dh + pinBarH,
		Frameless:     true,
		AlwaysOnTop:   true,
		DisableResize: true,
		URL:           fmt.Sprintf("/#/widget/pin?id=%d", id),
		Windows: application.WindowsWindow{
			// 不关闭 frameless 装饰：保留与工具窗一致的圆角/投影边框
			HiddenOnTaskbar:        true,
			NonClientRegionSupport: true, // 开启非客户区支持，前端顶部标题栏的 app-region: drag 才能拖动无边框窗
		},
	})
	a.pinMu.Lock()
	a.pinWins[id] = win
	a.pinMu.Unlock()
	win.Show()
	win.Focus()
	applog.Infof("capture", "图钉已置顶 #%d %dx%d @%d,%d", id, dw, dh, dx, dy)
	return id, nil
}

// GetPinnedImage 图钉页面按 id 取回要显示的图片 dataURL。
func (a *App) GetPinnedImage(id int) string {
	a.pinMu.Lock()
	defer a.pinMu.Unlock()
	return a.pinData[id]
}

// ClosePinnedImage 关闭一个图钉窗口并清理其数据。
func (a *App) ClosePinnedImage(id int) {
	a.pinMu.Lock()
	w := a.pinWins[id]
	delete(a.pinWins, id)
	delete(a.pinData, id)
	a.pinMu.Unlock()
	if w != nil {
		w.Close()
	}
}

// CloseCaptureOverlay 兼容旧调用名：彻底关闭并丢弃浮层窗口（异常收场时用），
// 下次截图会冷建一个新窗口。
func (a *App) CloseCaptureOverlay() {
	a.destroyCaptureOverlay()
}

// captureReadyWatchdog 就绪看门狗：一次截图在 captureReadyTimeout 内仍未收到
// 前端 CaptureReady 回报，就判定浮层页面异常（JS 抛错/绑定缺失），销毁窗口，
// 下次截图会重新冷建——不复用一个可能已坏死的面板。
//
// 必须拿轮次 gen 做判据：capReady 在正常收场（hideCaptureOverlay）时也会被清回
// false，只看 !capReady 的话，每次秒完成的截图都会在 6s 后被看门狗当成僵尸干掉，
// 刚预热的窗口白白销毁，下次只能冷建（实测日志里每张图后都跟一条“未就绪，销毁窗口”）。
func (a *App) captureReadyWatchdog(gen uint64) {
	time.Sleep(captureReadyTimeout)
	a.capMu.Lock()
	stale := a.capWin != nil && a.capGen == gen && a.capBusy && !a.capReady
	a.capMu.Unlock()
	if stale {
		applog.Warnf("capture", "选区浮层 %s 内未就绪，销毁窗口", captureReadyTimeout)
		a.destroyCaptureOverlay()
	}
}

// hideCaptureOverlay 结束一次截图：隐藏浮层窗口并复位状态，但保留窗口对象，
// 供下次截图直接复用（省去重建 WebView2 的开销）。窗口隐藏后不会被下次
// 冻结帧抓进去（GDI 抓屏不含 SW_HIDE 窗口）。
func (a *App) hideCaptureOverlay() {
	a.capMu.Lock()
	w := a.capWin
	// 实测首帧合成过慢、而当前窗口又是透明背景建的：这一轮不再复用，直接销毁，
	// 下一轮按实心背景重建（背景类型只能在建窗时定）。只切一次，之后 capSolid
	// 与 capSlow 一致，回到普通的隐藏复用。
	rebuild := w != nil && a.capSlow && !a.capSolid
	if rebuild {
		a.capWin = nil
	}
	a.plat.capFrame = nil
	a.capReady = false
	a.capBusy = false
	a.capShown = false
	a.capPainted = false
	a.capPingAt = time.Time{}
	a.capShowAt = time.Time{}
	a.capMu.Unlock()
	// ESC 兜底热键随浮层进出：留着就是跟其它应用抢 ESC
	a.disarmCaptureGuard()
	if w == nil {
		return
	}
	if rebuild {
		applog.Infof("capture", "本机透明合成过慢，浮层改为实心背景重建")
		w.Close()
		return
	}
	w.Hide()
}

// destroyCaptureOverlay 彻底关闭并丢弃浮层窗口对象（看门狗超时/异常时用）。
func (a *App) destroyCaptureOverlay() {
	a.capMu.Lock()
	w := a.capWin
	a.capWin = nil
	a.plat.capFrame = nil
	a.capReady = false
	a.capBusy = false
	a.capShown = false
	a.capPainted = false
	a.capPingAt = time.Time{}
	a.capShowAt = time.Time{}
	a.capMu.Unlock()
	a.disarmCaptureGuard()
	if w != nil {
		w.Close()
	}
}
