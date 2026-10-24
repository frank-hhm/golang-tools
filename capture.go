//go:build windows

package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"go-tools/tools/screenshot"

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
	a.capMu.Unlock()

	defer func() {
		if r := recover(); r != nil {
			a.capMu.Lock()
			a.capBusy = false
			a.capWin = nil
			a.capMu.Unlock()
			errLogf("capture", "打开选区浮层 panic: %v", r)
		}
	}()

	// 等一下：热键往往来自托盘菜单/通知卡片，让它们先从屏幕上消失再抓帧，
	// 否则这些转瞬即逝的东西会被定格进冻结帧里。
	time.Sleep(250 * time.Millisecond)
	frame, err := screenshot.CaptureScreenFrame()
	if err != nil {
		a.capMu.Lock()
		a.capBusy = false
		a.capMu.Unlock()
		return err
	}

	a.capMu.Lock()
	a.capFrame = frame
	a.capReady = false
	a.capMu.Unlock()

	dpr := frame.DPR
	if dpr <= 0 {
		dpr = 1
	}
	// 显式给一个整屏尺寸（DIP）：StartState 万一不被实现，窗口也已经铺满，
	// 前端据 innerWidth 实测的 dpr 才不会算错（隐藏窗口拿不到默认尺寸就全糊了）。
	screenW := int(float64(frame.W) / dpr)
	screenH := int(float64(frame.H) / dpr)
	w := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:          captureOverlayName,
		Title:         "截图",
		X:             0,
		Y:             0,
		Width:         screenW,
		Height:        screenH,
		Frameless:     true,
		AlwaysOnTop:   true,
		DisableResize: true,
		Hidden:        true, // 等前端把冻结帧画出来再显示，避免黑屏一闪
		StartState:    application.WindowStateFullscreen,
		// 透明背景：Show 后、页面首帧合成前的那一帧露出桌面而非黑色（见文件头注释）。
		// 只有配合 IgnoreMouseEvents 才会点击穿透，这里不设，交互照常。
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/#/widget/capture",
		Windows: application.WindowsWindow{
			// 全屏浮层不要 Win11 圆角与投影，否则四角会露出真实桌面
			DisableFramelessWindowDecorations: true,
			// 一次性的交互浮层，不该在任务栏占位
			HiddenOnTaskbar: true,
		},
	})
	a.capMu.Lock()
	a.capWin = w
	a.capMu.Unlock()

	// 兜底：前端若因故没能回报就绪，超时后直接收场，别把后续截图卡死。
	go func() {
		time.Sleep(captureReadyTimeout)
		a.capMu.Lock()
		stale := a.capWin != nil && !a.capReady
		a.capMu.Unlock()
		if stale {
			warnLogf("capture", "选区浮层 %s 内未就绪，自动关闭", captureReadyTimeout)
			a.CloseCaptureOverlay()
		}
	}()

	opLogf("capture", "选区浮层已打开 %dx%d dpr=%.2f 窗口=%d 个",
		frame.W, frame.H, frame.DPR, len(frame.Windows))
	return nil
}

// GetCaptureFrame 浮层页面取回冻结帧、屏幕尺寸、光标位置与可截取窗口清单。
// 这里不清空：开发模式 HMR 刷新后页面会重新取一次，拿不到就只剩黑屏；
// 帧的释放在 CloseCaptureOverlay 里统一做。
func (a *App) GetCaptureFrame() *screenshot.CaptureFrame {
	a.capMu.Lock()
	defer a.capMu.Unlock()
	return a.capFrame
}

// CaptureReady 浮层已完成首帧渲染，可以亮出来了。
func (a *App) CaptureReady() {
	a.capMu.Lock()
	w := a.capWin
	a.capReady = true
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
	opLogf("capture", "截图完成 copy=%v preview=%v %dKB", data != "", openPreview, len(data)/1024)
	// 关窗放在本次调用返回之后：浮层就是这次调用的发起方，同步销毁它会让
	// 响应永远回不去，极端情况下还会撞上 WebView2 正在序列化的回调。
	a.closeOverlayLater(150 * time.Millisecond)
	return nil
}

// CaptureCancel 放弃本次截图（ESC / 右键 / ✕）。
func (a *App) CaptureCancel() {
	opLogf("capture", "截图已取消")
	a.closeOverlayLater(0)
}

// closeOverlayLater 延迟关闭浮层，给当前这次绑定调用留出返回的时机。
func (a *App) closeOverlayLater(d time.Duration) {
	time.AfterFunc(d, a.CloseCaptureOverlay)
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
	opLogf("capture", "浮层: %s", note)
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
	opLogf("capture", "截图已保存: %s", path)
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
	f := a.capFrame
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
	opLogf("capture", "图钉已置顶 #%d %dx%d @%d,%d", id, dw, dh, dx, dy)
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

// CloseCaptureOverlay 关闭浮层窗口并复位会话状态。
func (a *App) CloseCaptureOverlay() {
	a.capMu.Lock()
	w := a.capWin
	a.capWin = nil
	a.capFrame = nil
	a.capReady = false
	a.capBusy = false
	a.capMu.Unlock()
	if w != nil {
		w.Close()
	}
}
