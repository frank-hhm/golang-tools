//go:build windows

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ============================================================
// 桌面浮动球 —— 纯 Go 原生 Win32 实现（不再用 WebView 小窗）
//
// 形态：一个 WS_EX_LAYERED 分层窗口，用 UpdateLayeredWindow 提交
// 「逐像素 alpha」的圆形位图 —— 圆形底板 + 居中 logo + 柔和投影，
// 背景与阴影均为像素级 alpha，无灰底方框。
// 交互：按住拖动用 SetCapture + SetWindowPos 原生移动（无 IPC 往返，不闪动）；
// 拖动范围以「圆形底板」为准（阴影像素可探出屏幕），鼠标可把球停到屏幕上任意位置；
// 松手时立即把窗口坐标写入配置，下次显示（含重启/重新打开）由 ballInitPos 原样恢复。
// 单击（未拖动）在球上弹出原生系统菜单（TrackPopupMenu），选择项经
// App.handleBallAction → "ball:action" 事件交回主窗口复用前端开窗/截图逻辑。
// 置顶 WS_EX_TOPMOST、不占任务栏 WS_EX_TOOLWINDOW，默认贴屏幕工作区右下角。
//
// 消息循环安全（沿用本仓库既有约定）：
//   - 专用 goroutine + runtime.LockOSThread，退出时不 UnlockOSThread，
//     线程随之销毁，其队列残留一并带走，绝不毒化复用线程；
//   - 全程不 PostQuitMessage，退出靠 WM_DESTROY 置 done 标志；
//   - 关闭由 stopNativeBall 从其它 goroutine PostMessage(WM_CLOSE) 触发。
// ============================================================

// ---------------- Win32 常量 ----------------

const (
	ballWS_POPUP      = 0x80000000
	ballWS_EX_LAYERED = 0x00080000
	ballWS_EX_TOPMOST = 0x00000008
	ballWS_EX_TOOLWIN = 0x00000080

	ballSW_SHOWNOACTIVATE = 4

	ballWM_DESTROY     = 0x0002
	ballWM_CLOSE       = 0x0010
	ballWM_SETCURSOR   = 0x0020
	ballWM_MOUSEMOVE   = 0x0200
	ballWM_LBUTTONDOWN = 0x0201
	ballWM_LBUTTONUP   = 0x0202
	ballWM_RBUTTONUP   = 0x0205
	ballWM_CAPTURECHG  = 0x0215

	ballIDC_ARROW   = 32512
	ballIDC_HAND    = 32649
	ballIDC_SIZEALL = 32646

	ballSWP_NOSIZE     = 0x0001
	ballSWP_NOZORDER   = 0x0004
	ballSWP_NOACTIVATE = 0x0010

	ballULW_ALPHA = 0x00000002
	ballAC_SRC_OVER_ALPHA

	ballMF_STRING       = 0x0000
	ballMF_SEPARATOR    = 0x0800
	ballMF_OWNERDRAW    = 0x0100
	ballTPM_RETURNCMD   = 0x0100
	ballTPM_RIGHTBUTTON = 0x0002

	// 自绘菜单项（owner-draw）：为菜单项加上下留白，需处理这两条消息
	ballWM_DRAWITEM    = 0x002B
	ballWM_MEASUREITEM = 0x002C
	ballODT_MENU       = 1
	ballODS_SELECTED   = 0x0001
	ballODS_GRAYED     = 0x0002
	ballODS_DISABLED   = 0x0004
	ballTRANSPARENT    = 1

	// GetSysColor 索引
	ballCOLOR_MENU          = 3
	ballCOLOR_MENUTEXT      = 7
	ballCOLOR_HIGHLIGHTTEXT = 14
	ballCOLOR_GRAYTEXT      = 17
	ballCOLOR_MENUHILIGHT   = 29

	// GetStockObject / DrawTextW 标志
	ballDEFAULT_GUI_FONT = 17
	ballNULL_PEN         = 8 // GetStockObject：空画笔，圆角高亮条只填色不描边
	ballDT_LEFT          = 0x00000000
	ballDT_VCENTER       = 0x00000004
	ballDT_SINGLELINE    = 0x00000020
	ballDT_NOPREFIX      = 0x00000800

	// 自绘菜单美化：菜单项禁用态、字体、菜单底色
	ballMF_GRAYED          = 0x0001
	ballMF_DISABLED        = 0x0002
	ballFW_NORMAL          = 400
	ballDEFAULT_CHARSET    = 1
	ballCLEARTYPE_QUALITY  = 5
	ballMIIM_BACKGROUND    = 0x00000002 // SetMenuInfo：自定义菜单背景画刷

	// 注册表：auto 主题下读取系统是否使用深色应用模式
	ballHKEY_CURRENT_USER = 0x80000001
	ballKEY_QUERY_VALUE   = 0x0001

	ballSPI_GETWORKAREA = 0x0030

	// GetSystemMetrics：虚拟屏幕（所有显示器并集）范围，用于跨屏钳制
	ballSMXVirtualScreen  = 76
	ballSMYVirtualScreen  = 77
	ballSMCXVirtualScreen = 78
	ballSMCYVirtualScreen = 79
)

// 浮动球视觉参数（单位 DIP，运行时按系统 DPI 放大）
const (
	ballDiameterDIP  = 36.0 // 圆形底板直径（不含阴影留白），比原 48 DIP logo 更小
	ballShadowPadDIP = 14.0 // 四周阴影留白（容纳大半径柔和投影，避免被窗口边缘截断）
	ballLogoRatio    = 0.56 // logo 内容占底板直径的比例
)

// ---------------- Win32 结构体 ----------------

type ballPoint struct{ X, Y int32 }
type ballSize struct{ CX, CY int32 }
type ballRect struct {
	Left, Top, Right, Bottom int32
}

// MEASUREITEMSTRUCT / DRAWITEMSTRUCT（仅菜单 owner-draw 用到的字段布局，amd64 对齐与 C 一致）
type ballMeasureItem struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemWidth  uint32
	ItemHeight uint32
	ItemData   uintptr
}
type ballDrawItem struct {
	CtlType    uint32
	CtlID      uint32
	ItemID     uint32
	ItemAction uint32
	ItemState  uint32
	HwndItem   uintptr
	HDC        uintptr
	RcItem     ballRect
	ItemData   uintptr
}

// MENUINFO（SetMenuInfo 用，仅取背景画刷字段；amd64 对齐与 C 一致，size=40）
type ballMenuInfo struct {
	CbSize    uint32
	FMask     uint32
	DwStyle   uint32
	CyMin     uint32
	HbrBack   uintptr
	DwContext uint32
	DwMenuID  uintptr
}
type ballBlend struct {
	BlendOp             byte
	BlendFlags          byte
	SourceConstantAlpha byte
	AlphaFormat         byte
}
type ballMsg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      ballPoint
}
type ballWndClass struct {
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
}
type ballBmih struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}
type ballBitmapInfo struct {
	Header ballBmih
	Colors uintptr
	_      [0]byte
	pad    [0]byte
	// Colors 占位；实际颜色表紧随其后（32bpp BI_RGB 无需调色板）
	_unused [0]uint32
}

// ---------------- Win32 过程 ----------------

var (
	ballUser32   = windows.NewLazySystemDLL("user32.dll")
	ballGdi32    = windows.NewLazySystemDLL("gdi32.dll")
	ballKern32   = windows.NewLazySystemDLL("kernel32.dll")
	ballNewClass = ballUser32.NewProc("RegisterClassW")
	ballCreate   = ballUser32.NewProc("CreateWindowExW")
	ballShow     = ballUser32.NewProc("ShowWindow")
	ballGetMsg   = ballUser32.NewProc("GetMessageW")
	ballTrans    = ballUser32.NewProc("TranslateMessage")
	ballDispatch = ballUser32.NewProc("DispatchMessageW")
	ballDefProc  = ballUser32.NewProc("DefWindowProcW")
	ballDestroy  = ballUser32.NewProc("DestroyWindow")
	ballPostMsg  = ballUser32.NewProc("PostMessageW")
	ballSetCap   = ballUser32.NewProc("SetCapture")
	ballRelCap   = ballUser32.NewProc("ReleaseCapture")
	ballGetCur   = ballUser32.NewProc("GetCursorPos")
	ballSetCur   = ballUser32.NewProc("SetCursor")
	ballGetRect  = ballUser32.NewProc("GetWindowRect")
	ballSetPos   = ballUser32.NewProc("SetWindowPos")
	ballLoadCur  = ballUser32.NewProc("LoadCursorW")
	ballSetFore  = ballUser32.NewProc("SetForegroundWindow")
	ballGetDC    = ballUser32.NewProc("GetDC")
	ballRelDC    = ballUser32.NewProc("ReleaseDC")
	ballSysInfo  = ballUser32.NewProc("SystemParametersInfoW")
	ballMetrics  = ballUser32.NewProc("GetSystemMetrics")
	ballGetDpi   = ballUser32.NewProc("GetDpiForSystem")
	ballCreatePM = ballUser32.NewProc("CreatePopupMenu")
	ballAppendM  = ballUser32.NewProc("AppendMenuW")
	ballTrackPM  = ballUser32.NewProc("TrackPopupMenuEx")
	ballDestroyM = ballUser32.NewProc("DestroyMenu")

	// 自绘菜单用到的 GDI/User32
	ballGetSysColor   = ballUser32.NewProc("GetSysColor")
	ballFillRect      = ballUser32.NewProc("FillRect")
	ballDrawText      = ballUser32.NewProc("DrawTextW")
	ballGetStockObj   = ballGdi32.NewProc("GetStockObject")
	ballCreateBrush   = ballGdi32.NewProc("CreateSolidBrush")
	ballSetBkMode     = ballGdi32.NewProc("SetBkMode")
	ballSetTextColor  = ballGdi32.NewProc("SetTextColor")
	ballGetTextExtent = ballGdi32.NewProc("GetTextExtentPoint32W")
	ballCreateFont    = ballGdi32.NewProc("CreateFontW")
	ballRoundRect     = ballGdi32.NewProc("RoundRect")
	ballSetMenuInfo   = ballUser32.NewProc("SetMenuInfo")
	ballAdv32         = windows.NewLazySystemDLL("advapi32.dll")
	ballRegOpenKey    = ballAdv32.NewProc("RegOpenKeyExW")
	ballRegQueryValue = ballAdv32.NewProc("RegQueryValueExW")
	ballRegCloseKey   = ballAdv32.NewProc("RegCloseKey")
	ballCompatDC      = ballGdi32.NewProc("CreateCompatibleDC")
	ballDeleteDC      = ballGdi32.NewProc("DeleteDC")
	ballDIB           = ballGdi32.NewProc("CreateDIBSection")
	ballSelObj        = ballGdi32.NewProc("SelectObject")
	ballDelObj        = ballGdi32.NewProc("DeleteObject")
	ballULW           = ballUser32.NewProc("UpdateLayeredWindow")
	ballModHdr        = ballKern32.NewProc("GetModuleHandleW")
)

// ---------------- 运行期状态（仅在球线程访问的拖拽标志无需加锁） ----------------

var (
	ballMu        sync.Mutex
	ballRunning   bool
	ballApp       *App
	ballHwnd      uintptr
	ballDone      bool
	ballClassOnce sync.Once
	ballClassName *uint16
	ballClassAtom uint16

	ballSizePx     int // 球窗口边长（物理像素，含阴影留白）
	ballDiameterPx int // 圆球本体直径（物理像素）
	ballShadowPad  int // 四周为阴影预留的边距（物理像素）
	ballRadiusF    float64
	ballLogoImg    image.Image     // 浮动球中心 logo 图像（nil 则回退闪电）
	ballLogoRect   image.Rectangle // logo 非透明内容包围盒（去源图留白）
	ballCx, ballCy float64

	// 拖拽（仅球线程读写）
	ballCaptured bool
	ballDragging bool
	ballDownPt   ballPoint
	ballOrigLeft int32
	ballOrigTop  int32
	ballDragPx   int32 // 判定为拖动的最小位移（物理像素，随 DPI 放大）

	// 光标（仅球线程使用）：默认箭头 / 悬停手型 / 拖动中四向移动
	ballCursorArrow uintptr
	ballCursorHand  uintptr
	ballCursorMove  uintptr

	// 自绘菜单（仅球线程使用；TrackPopupMenu 为模态，同线程重入安全）
	ballScale     float64            // DPI 缩放，用于把留白 DIP 换算成物理像素
	ballMenuLabel map[uintptr]string // 菜单项 ID → 文本，供 MEASURE/DRAW 回查
	ballMenuSep   map[uintptr]bool   // 分隔符项 ID 集合（owner-draw 自绘细线）
	ballMenuFont  uintptr            // 菜单字体句柄（弹菜单前创建，关闭后销毁）
	ballMenuDark  bool               // 当前是否按深色主题绘制（跟随应用浅/暗黑）
)

func ballUtf16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

// ballLogf 浮动球专用日志辅助（沿用 selector 的 selLogf 约定）：
// 关键生命周期/用户离散操作/异常统一走全局可配置日志（logger.go）。
func ballLogf(format string, args ...any) {
	appLogf(logInfo, "ball", format, args...)
}

// startNativeBall 启动桌面浮动球（幂等）。
func startNativeBall(a *App) error {
	if a == nil {
		return fmt.Errorf("app 为空")
	}
	ballMu.Lock()
	if ballRunning {
		ballMu.Unlock()
		return nil
	}
	ballApp = a
	ballRunning = true
	ballDone = false
	ballMu.Unlock()

	// 加载 logo 图片
	ballLoadLogo()

	go ballThread()
	return nil
}

// stopNativeBall 请求关闭浮动球（从任意 goroutine 调用）。
func stopNativeBall() {
	ballMu.Lock()
	h := ballHwnd
	ballMu.Unlock()
	if h != 0 {
		ballPostMsg.Call(h, ballWM_CLOSE, 0, 0)
	}
}

// ballLoadLogo 从配置加载 logo 图片到 ballLogoImg，并计算非透明内容包围盒。
func ballLoadLogo() {
	ballLogoImg = nil
	ballLogoRect = image.Rectangle{}
	cfg := CurrentConfig()
	var data []byte
	if cfg.General.AppLogo != "" {
		if b, err := os.ReadFile(cfg.General.AppLogo); err == nil {
			data = b
		}
	}
	if len(data) == 0 && ballApp != nil {
		data = ballApp.defaultLogo
	}
	if len(data) == 0 {
		return
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		ballLogf("logo PNG 解码失败: %v", err)
		return
	}
	ballLogoImg = img
	ballLogoRect = ballOpaqueBounds(img)
	if ballLogoRect.Empty() {
		ballLogoRect = img.Bounds()
	}
}

// ballOpaqueBounds 计算图像非透明内容的包围盒（alpha 过低视为空白），
// 避免 logo 源图自带留白导致缩小后视觉过小。
func ballOpaqueBounds(img image.Image) image.Rectangle {
	b := img.Bounds()
	minX, minY := b.Max.X, b.Max.Y
	maxX, maxY := b.Min.X-1, b.Min.Y-1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			_, _, _, a := img.At(x, y).RGBA()
			if a < 0x1000 {
				continue
			}
			if x < minX {
				minX = x
			}
			if y < minY {
				minY = y
			}
			if x > maxX {
				maxX = x
			}
			if y > maxY {
				maxY = y
			}
		}
	}
	if maxX < b.Min.X || maxY < b.Min.Y {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX+1, maxY+1)
}

// ballThread 在专用 OS 线程上建窗并泵消息，直到收到关闭。
func ballThread() {
	runtime.LockOSThread()
	// 故意不 UnlockOSThread：线程随 goroutine 退出而销毁，队列残留一并清除。
	defer func() {
		ballMu.Lock()
		ballRunning = false
		ballHwnd = 0
		ballMu.Unlock()
	}()

	ballClassOnce.Do(ballRegisterClass)
	if ballClassAtom == 0 {
		ballLogf("注册窗口类失败，放弃")
		return
	}

	// 计算尺寸与初始位置（物理像素；按系统 DPI 放大以在高分屏保持视觉大小）
	dpi := uintptr(96)
	if r, _, _ := ballGetDpi.Call(); r > 0 {
		dpi = r
	}
	scale := float64(dpi) / 96.0
	if scale < 1 {
		scale = 1
	}
	ballScale = scale // 自绘菜单留白按同一 DPI 放大
	// 统一形态：圆形底板 + 居中 logo + 柔和投影（阴影留白只用于绘制，不参与点击）
	ballDiameterPx = int(math.Round(ballDiameterDIP * scale))
	if ballDiameterPx < 24 {
		ballDiameterPx = 24
	}
	ballShadowPad = int(math.Round(ballShadowPadDIP * scale))
	if ballShadowPad < 6 {
		ballShadowPad = 6
	}
	ballSizePx = ballDiameterPx + 2*ballShadowPad
	ballCx = float64(ballSizePx-1) / 2
	ballCy = ballCx
	ballRadiusF = float64(ballDiameterPx) / 2
	// 拖动阈值同样随 DPI 放大（高 DPI 下 4 物理像素过小，容易误触拖动）
	ballDragPx = int32(math.Max(3, math.Round(4*scale)))

	left, top := ballInitPos()

	hInst, _, _ := ballModHdr.Call(0)
	hwnd, _, _ := ballCreate.Call(
		uintptr(ballWS_EX_LAYERED|ballWS_EX_TOPMOST|ballWS_EX_TOOLWIN),
		uintptr(unsafe.Pointer(ballClassName)),
		uintptr(unsafe.Pointer(ballUtf16("go-tools 浮动球"))),
		uintptr(ballWS_POPUP),
		uintptr(left), uintptr(top), uintptr(ballSizePx), uintptr(ballSizePx),
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		ballLogf("CreateWindowEx 失败: %v", windows.GetLastError())
		return
	}
	ballMu.Lock()
	ballHwnd = hwnd
	ballMu.Unlock()

	ballPaint(hwnd)
	ballShow.Call(hwnd, ballSW_SHOWNOACTIVATE)

	var msg ballMsg
	for {
		ret, _, _ := ballGetMsg.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) == 0 {
			break // 意外 WM_QUIT：本线程即将销毁，安全退出
		}
		ballTrans.Call(uintptr(unsafe.Pointer(&msg)))
		ballDispatch.Call(uintptr(unsafe.Pointer(&msg)))
		ballMu.Lock()
		done := ballDone
		ballMu.Unlock()
		if done {
			break
		}
	}
}

// ballInitPos 初始位置：优先沿用上次拖动保存的位置（钳制到可见区域），
// 否则停靠主屏工作区右下角。
func ballInitPos() (int32, int32) {
	cfg := CurrentConfig()
	if cfg.Ball.HasPos {
		l, t := ballClampToScreen(int32(cfg.Ball.X), int32(cfg.Ball.Y))
		if l != int32(cfg.Ball.X) || t != int32(cfg.Ball.Y) {
			ballLogf("上次位置 (%d, %d) 不在可见区域，已修正为 (%d, %d)", cfg.Ball.X, cfg.Ball.Y, l, t)
		} else {
			ballLogf("恢复上次位置: (%d, %d)", l, t)
		}
		return l, t
	}
	return ballDefaultPos()
}

// ballDefaultPos 工作区右下角（物理像素），让球面（不含阴影）距屏幕边缘 14 DIP。
func ballDefaultPos() (int32, int32) {
	var rc ballRect
	ballSysInfo.Call(ballSPI_GETWORKAREA, 0, uintptr(unsafe.Pointer(&rc)), 0)
	margin := int32(14)
	right := rc.Right - int32(ballDiameterPx) - margin - int32(ballShadowPad)
	bottom := rc.Bottom - int32(ballDiameterPx) - margin - int32(ballShadowPad)
	if right < rc.Left {
		right = rc.Left
	}
	if bottom < rc.Top {
		bottom = rc.Top
	}
	return right, bottom
}

func ballRegisterClass() {
	ballClassName = ballUtf16("GoToolsFloatBallCls")
	ballCursorArrow = ballLoadCursor(ballIDC_ARROW)
	ballCursorHand = ballLoadCursor(ballIDC_HAND)
	ballCursorMove = ballLoadCursor(ballIDC_SIZEALL)
	hInst, _, _ := ballModHdr.Call(0)
	wc := ballWndClass{
		LpfnWndProc:   syscall.NewCallback(ballWndProc),
		HInstance:     hInst,
		HCursor:       ballCursorArrow,
		LpszClassName: ballClassName,
	}
	atom, _, err := ballNewClass.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		ballLogf("RegisterClassW 失败: %v", err)
		return
	}
	ballClassAtom = uint16(atom)
}

// ballLoadCursor 加载系统光标（id 为 IDC_* 常量）。
func ballLoadCursor(id uintptr) uintptr {
	r, _, _ := ballLoadCur.Call(0, id)
	return r
}

// ballWndProc 窗口过程（仅在球线程被调用）。
func ballWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case ballWM_LBUTTONDOWN:
		x := int32(int16(uint16(lParam & 0xFFFF)))
		y := int32(int16(uint16((lParam >> 16) & 0xFFFF)))
		if !ballInsideCircle(float64(x), float64(y)) {
			break // 圆外透明区不响应
		}
		ballCaptured = true
		ballDragging = false
		ballGetCur.Call(uintptr(unsafe.Pointer(&ballDownPt)))
		var rc ballRect
		ballGetRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
		ballOrigLeft, ballOrigTop = rc.Left, rc.Top
		ballSetCap.Call(hwnd)
		return 0

	case ballWM_MOUSEMOVE:
		if !ballCaptured {
			break
		}
		var p ballPoint
		ballGetCur.Call(uintptr(unsafe.Pointer(&p)))
		dx := p.X - ballDownPt.X
		dy := p.Y - ballDownPt.Y
		if !ballDragging && (abs32(dx) >= ballDragPx || abs32(dy) >= ballDragPx) {
			ballDragging = true
			ballSetCur.Call(ballCursorMove) // 捕获期间不会收到 WM_SETCURSOR，这里主动切光标
		}
		if ballDragging {
			nl, nt := ballClampToScreen(ballOrigLeft+dx, ballOrigTop+dy)
			// SetWindowPos(hwnd, hWndInsertAfter, X, Y, cx, cy, flags)：
			// 第 2 参必须传 0（配合 SWP_NOZORDER 被忽略）；若误把 X 传在第 2 参，
			// 会被当成插入句柄而导致移动静默失败（表现为拖不动）。
			ballSetPos.Call(hwnd, 0, uintptr(nl), uintptr(nt), 0, 0,
				ballSWP_NOSIZE|ballSWP_NOZORDER|ballSWP_NOACTIVATE)
		}
		return 0

	case ballWM_LBUTTONUP:
		if ballCaptured {
			wasDragging := ballDragging
			if wasDragging {
				// 兜底：按松手时的光标做最后一次定位，避免快速松手漏掉最后一次 MOUSEMOVE
				var p ballPoint
				ballGetCur.Call(uintptr(unsafe.Pointer(&p)))
				nl, nt := ballClampToScreen(
					ballOrigLeft+(p.X-ballDownPt.X),
					ballOrigTop+(p.Y-ballDownPt.Y),
				)
				ballSetPos.Call(hwnd, 0, uintptr(nl), uintptr(nt), 0, 0,
					ballSWP_NOSIZE|ballSWP_NOZORDER|ballSWP_NOACTIVATE)
			}
			// 先复位标志再释放捕获：ReleaseCapture 会同步派发 WM_CAPTURECHANGED，
			// 若不先复位会被它清成“未拖动”，导致刚拖完的位置不落盘。
			ballCaptured = false
			ballDragging = false
			ballRelCap.Call()
			if wasDragging {
				ballSavePos(hwnd) // 拖动结束：同步记忆位置，立即落盘
				ballApplyCursor(hwnd)
			} else {
				ballShowMenu(hwnd)
			}
		}
		return 0

	case ballWM_CAPTURECHG:
		// 捕获被系统/其他窗口抢走时复位拖拽状态，避免卡在“按住”状态
		ballCaptured = false
		ballDragging = false
		return 0

	case ballWM_SETCURSOR:
		ballApplyCursor(hwnd)
		return 1

	case ballWM_RBUTTONUP:
		ballShowMenu(hwnd)
		return 0

	case ballWM_CLOSE:
		ballDestroy.Call(hwnd)
		return 0

	case ballWM_DESTROY:
		ballMu.Lock()
		ballDone = true
		ballMu.Unlock()
		return 0

	case ballWM_MEASUREITEM:
		mis := (*ballMeasureItem)(unsafe.Pointer(lParam))
		if mis.CtlType == ballODT_MENU {
			ballMeasureMenuItem(mis)
			return 1
		}

	case ballWM_DRAWITEM:
		dis := (*ballDrawItem)(unsafe.Pointer(lParam))
		if dis.CtlType == ballODT_MENU {
			ballDrawMenuItem(dis)
			return 1
		}
	}
	r, _, _ := ballDefProc.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// ballApplyCursor 悬停在圆球上显示手型、拖动中显示四向移动光标，提示“可按住拖动”。
func ballApplyCursor(hwnd uintptr) {
	cur := ballCursorArrow
	switch {
	case ballDragging || ballCaptured:
		cur = ballCursorMove
	default:
		var p ballPoint
		ballGetCur.Call(uintptr(unsafe.Pointer(&p)))
		var rc ballRect
		ballGetRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
		if ballInsideCircle(float64(p.X-rc.Left), float64(p.Y-rc.Top)) {
			cur = ballCursorHand
		}
	}
	if cur != 0 {
		ballSetCur.Call(cur)
	}
}

func ballInsideCircle(x, y float64) bool {
	dx, dy := x-ballCx, y-ballCy
	r := ballRadiusF + 2 // 多留 2px 容差，按住边缘也能拖动
	return dx*dx+dy*dy <= r*r
}

// ballClampToScreen 把窗口左上角钳制到允许放置的范围：
// 以「圆形底板」为准（阴影像素允许探出屏幕边缘），所以球体能贴住任意一边/角落，
// 屏幕上任意位置都能停；显示器拔插后恢复旧位置也会被拉回可见区域。
func ballClampToScreen(l, t int32) (int32, int32) {
	vxRaw, _, _ := ballMetrics.Call(ballSMXVirtualScreen)
	vyRaw, _, _ := ballMetrics.Call(ballSMYVirtualScreen)
	vwRaw, _, _ := ballMetrics.Call(ballSMCXVirtualScreen)
	vhRaw, _, _ := ballMetrics.Call(ballSMCYVirtualScreen)
	vx, vy := int32(vxRaw), int32(vyRaw)
	vw, vh := int32(vwRaw), int32(vhRaw)
	if vw <= 0 || vh <= 0 {
		return l, t
	}
	pad := int32(ballShadowPad)
	dia := int32(ballDiameterPx)
	// 窗口左上角的合法区间：圆球在左/上边贴齐屏幕时为屏幕坐标减阴影留白
	minL, maxL := vx-pad, vx+vw-pad-dia
	minT, maxT := vy-pad, vy+vh-pad-dia
	if maxL < minL {
		maxL = minL
	}
	if maxT < minT {
		maxT = minT
	}
	if l < minL {
		l = minL
	} else if l > maxL {
		l = maxL
	}
	if t < minT {
		t = minT
	} else if t > maxT {
		t = maxT
	}
	return l, t
}

// ballSavePos 拖动结束后把窗口坐标立即写回配置（同步写，避免退出时异步落盘未完成），
// 下次启动/重开浮动球时由 ballInitPos 原样恢复。
func ballSavePos(hwnd uintptr) {
	l, t := ballCurPos(hwnd)
	if err := SaveBallPos(int(l), int(t)); err != nil {
		ballLogf("保存位置失败: (%d, %d) %v", l, t, err)
		return
	}
	ballLogf("已记住位置: (%d, %d)", l, t)
}

// ballQuickItems 从当前配置读取标记为快捷（Quick=true）的工具，作为原生菜单项（ID → 显示名）。
func ballQuickItems() []struct{ ID, Label string } {
	var out []struct{ ID, Label string }
	for _, t := range CurrentConfig().Tools {
		if !t.Quick {
			continue
		}
		id := t.ID
		if id == "" {
			id = t.Key
		}
		label := t.Name
		if label == "" {
			label = t.Key
		}
		out = append(out, struct{ ID, Label string }{id, label})
	}
	return out
}

// ballShowMenu 在光标处弹出原生快捷菜单（菜单项动态来自配置的 quick 工具）。
// 全部项 owner-draw，按应用当前浅/暗黑主题自绘：深色底 + 圆角高亮条 + 现代字体。
func ballShowMenu(hwnd uintptr) {
	items := ballQuickItems()
	ballMenuDark = ballEffectiveDark()
	ballMenuSep = map[uintptr]bool{}
	ballMenuLabel = map[uintptr]string{}

	// 菜单字体（比 DEFAULT_GUI_FONT 更现代）。WM_MEASUREITEM 在 AppendMenu 时即触发，
	// 故必须在追加任何项之前把字体建好。失败则回退系统 GUI 字体。
	face := ballUtf16("Microsoft YaHei UI")
	fontH := -int32(math.Round(13 * ballScale))
	if fontH > -13 {
		fontH = -13
	}
	ballMenuFont, _, _ = ballCreateFont.Call(
		uintptr(fontH), 0, 0, 0, ballFW_NORMAL, 0, 0, 0,
		ballDEFAULT_CHARSET, 0, 0, ballCLEARTYPE_QUALITY, 0,
		uintptr(unsafe.Pointer(face)))
	if ballMenuFont != 0 {
		defer func() { ballDelObj.Call(ballMenuFont); ballMenuFont = 0 }()
	}

	menu, _, _ := ballCreatePM.Call()
	if menu == 0 {
		return
	}
	defer ballDestroyM.Call(menu)

	// 菜单底色画刷：填充项与项之间的缝隙/边距，避免系统灰底在深色主题下露出
	th := ballThemeColors(ballMenuDark)
	bgBrush, _, _ := ballCreateBrush.Call(th.bg)
	if bgBrush != 0 {
		defer ballDelObj.Call(bgBrush)
		var mi ballMenuInfo
		mi.CbSize = uint32(unsafe.Sizeof(mi))
		mi.FMask = ballMIIM_BACKGROUND
		mi.HbrBack = bgBrush
		ballSetMenuInfo.Call(menu, uintptr(unsafe.Pointer(&mi)))
	}

	// 逐项分配自增 ID：quick 项 1..n，分隔符 n+1，关闭 n+2（分隔符置灰禁用，不可选中）
	cmd := uintptr(0)
	next := func() uintptr { cmd++; return cmd }
	for _, it := range items {
		id := next()
		ballMenuLabel[id] = it.Label
		ballAppendM.Call(menu, ballMF_OWNERDRAW, id, id)
	}
	sepID := next()
	ballMenuSep[sepID] = true
	ballAppendM.Call(menu, ballMF_OWNERDRAW|ballMF_GRAYED|ballMF_DISABLED, sepID, sepID)
	closeID := next()
	ballMenuLabel[closeID] = "关闭浮动球"
	ballAppendM.Call(menu, ballMF_OWNERDRAW, closeID, closeID)

	var p ballPoint
	ballGetCur.Call(uintptr(unsafe.Pointer(&p)))
	ballSetFore.Call(hwnd) // 让菜单在点击别处时正确消失
	ret, _, _ := ballTrackPM.Call(menu, ballTPM_RIGHTBUTTON|ballTPM_RETURNCMD,
		uintptr(p.X), uintptr(p.Y), hwnd, 0)
	ballPostMsg.Call(hwnd, 0, 0, 0) // WM_NULL：解除菜单与前台窗口的已知死锁

	if ret == 0 {
		return
	}
	app := ballApp
	if app == nil {
		return
	}
	if ret == closeID {
		go app.handleBallAction("close")
		return
	}
	if int(ret-1) < len(items) {
		go app.handleBallAction(items[ret-1].ID)
	}
}

// ballMenuPad 自绘菜单项的留白（物理像素，随 DPI 放大）：
// 上下留白撑出行高，左留白给文字缩进，右留白收边（含高亮条内缩余量）。
func ballMenuPad() (vPad, leftPad, rightPad int32) {
	vPad = int32(ballScale * 8)
	if vPad < 6 {
		vPad = 6
	}
	leftPad = int32(ballScale * 18)
	rightPad = int32(ballScale * 22)
	return
}

// ballMeasureMenuItem 处理 WM_MEASUREITEM：按文本实际宽度 + 留白定下每项尺寸。
func ballMeasureMenuItem(mis *ballMeasureItem) {
	id := uintptr(mis.ItemID)
	if ballMenuSep[id] {
		mis.ItemHeight = uint32(ballScale * 10)
		mis.ItemWidth = uint32(ballScale * 40)
		return
	}
	dc, _, _ := ballGetDC.Call(0)
	if dc == 0 {
		return
	}
	old := ballMenuSelectFont(dc)
	u, _ := syscall.UTF16FromString(ballMenuLabel[id])
	var sz ballSize
	if len(u) > 1 {
		ballGetTextExtent.Call(dc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&sz)))
	}
	if old != 0 {
		ballSelObj.Call(dc, old)
	}
	ballRelDC.Call(0, dc)

	vPad, leftPad, rightPad := ballMenuPad()
	mis.ItemHeight = uint32(int32(sz.CY) + 2*vPad)
	mis.ItemWidth = uint32(int32(sz.CX) + leftPad + rightPad)
}

// ballDrawMenuItem 处理 WM_DRAWITEM：自绘主题底色、圆角选中高亮、分隔细线与文本。
func ballDrawMenuItem(dis *ballDrawItem) {
	hdc := dis.HDC
	rc := dis.RcItem
	id := uintptr(dis.ItemID)
	th := ballThemeColors(ballMenuDark)

	// 背景铺满（与 SetMenuInfo 底色一致，双保险）
	if bg, _, _ := ballCreateBrush.Call(th.bg); bg != 0 {
		ballFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), bg)
		ballDelObj.Call(bg)
	}

	// 分隔符：画一条两端内缩的细线后返回
	if ballMenuSep[id] {
		lineH := int32(ballScale)
		if lineH < 1 {
			lineH = 1
		}
		cy := (rc.Top + rc.Bottom) / 2
		inx := int32(ballScale * 12)
		band := ballRect{Left: rc.Left + inx, Top: cy, Right: rc.Right - inx, Bottom: cy + lineH}
		if sep, _, _ := ballCreateBrush.Call(th.sep); sep != 0 {
			ballFillRect.Call(hdc, uintptr(unsafe.Pointer(&band)), sep)
			ballDelObj.Call(sep)
		}
		return
	}

	selected := dis.ItemState&ballODS_SELECTED != 0
	disabled := dis.ItemState&(ballODS_GRAYED|ballODS_DISABLED) != 0

	// 选中：内缩留边的圆角高亮条（只填色不描边）
	if selected {
		inx := int32(ballScale * 5)
		iny := int32(ballScale * 3)
		rad := int32(ballScale * 6)
		if rad < 2 {
			rad = 2
		}
		hl, _, _ := ballCreateBrush.Call(th.selBg)
		nullPen, _, _ := ballGetStockObj.Call(ballNULL_PEN)
		oldBr, _, _ := ballSelObj.Call(hdc, hl)
		oldPen, _, _ := ballSelObj.Call(hdc, nullPen)
		ballRoundRect.Call(hdc, uintptr(rc.Left+inx), uintptr(rc.Top+iny),
			uintptr(rc.Right-inx), uintptr(rc.Bottom-iny), uintptr(2*rad), uintptr(2*rad))
		ballSelObj.Call(hdc, oldBr)
		ballSelObj.Call(hdc, oldPen)
		if hl != 0 {
			ballDelObj.Call(hl)
		}
	}

	// 文本色：禁用灰 / 选中白 / 常态主题文字
	tx := th.text
	switch {
	case disabled:
		tx = th.disabled
	case selected:
		tx = th.selText
	}
	ballSetBkMode.Call(hdc, ballTRANSPARENT)
	ballSetTextColor.Call(hdc, tx)

	old := ballMenuSelectFont(hdc)
	_, leftPad, rightPad := ballMenuPad()
	tr := ballRect{Left: rc.Left + leftPad, Top: rc.Top, Right: rc.Right - rightPad, Bottom: rc.Bottom}
	u, _ := syscall.UTF16FromString(ballMenuLabel[id])
	if len(u) > 1 {
		flags := uintptr(ballDT_LEFT | ballDT_VCENTER | ballDT_SINGLELINE | ballDT_NOPREFIX)
		ballDrawText.Call(hdc, uintptr(unsafe.Pointer(&u[0])), uintptr(len(u)-1), uintptr(unsafe.Pointer(&tr)), flags)
	}
	if old != 0 {
		ballSelObj.Call(hdc, old)
	}
}

// ---------------- 自绘菜单的主题配色与字体 ----------------

type ballMenuColors struct {
	bg, text, selBg, selText, sep, disabled uintptr // COLORREF（0x00BBGGRR）
}

// ballRGB 组装 GDI COLORREF（字节序为 0x00BBGGRR）。
func ballRGB(r, g, b int) uintptr { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }

// ballThemeColors 返回浅/暗黑两套菜单配色，贴近应用 Arco 语义色与图2观感。
func ballThemeColors(dark bool) ballMenuColors {
	accent := ballRGB(0x16, 0x5D, 0xFF) // 主色蓝，与前端 --primary-6 对齐
	if dark {
		return ballMenuColors{
			bg:       ballRGB(0x2B, 0x2B, 0x2B),
			text:     ballRGB(0xE8, 0xE8, 0xE8),
			selBg:    accent,
			selText:  ballRGB(0xFF, 0xFF, 0xFF),
			sep:      ballRGB(0x3D, 0x3D, 0x3D),
			disabled: ballRGB(0x70, 0x70, 0x70),
		}
	}
	return ballMenuColors{
		bg:       ballRGB(0xFF, 0xFF, 0xFF),
		text:     ballRGB(0x1D, 0x21, 0x29),
		selBg:    accent,
		selText:  ballRGB(0xFF, 0xFF, 0xFF),
		sep:      ballRGB(0xE5, 0xE6, 0xEB),
		disabled: ballRGB(0xC9, 0xCD, 0xD4),
	}
}

// ballMenuSelectFont 在 dc 上选入菜单字体（缺省回退系统 GUI 字体），返回原对象便于复原。
func ballMenuSelectFont(dc uintptr) uintptr {
	f := ballMenuFont
	if f == 0 {
		f, _, _ = ballGetStockObj.Call(ballDEFAULT_GUI_FONT)
	}
	old, _, _ := ballSelObj.Call(dc, f)
	return old
}

// ballEffectiveDark 解析当前应采用的深浅模式：跟随配置 light/dark，auto 时读系统偏好。
func ballEffectiveDark() bool {
	switch CurrentConfig().General.Theme {
	case "dark":
		return true
	case "light":
		return false
	default:
		return ballSystemUsesDarkTheme()
	}
}

// ballSystemUsesDarkTheme 读注册表 AppsUseLightTheme（0 表示深色应用模式）。
func ballSystemUsesDarkTheme() bool {
	sub := ballUtf16(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`)
	var hk uintptr
	if r, _, _ := ballRegOpenKey.Call(ballHKEY_CURRENT_USER, uintptr(unsafe.Pointer(sub)), 0, ballKEY_QUERY_VALUE, uintptr(unsafe.Pointer(&hk))); r != 0 {
		return false
	}
	defer ballRegCloseKey.Call(hk)
	name := ballUtf16("AppsUseLightTheme")
	var typ, val, cb uint32 = 0, 1, 4
	if r, _, _ := ballRegQueryValue.Call(hk, uintptr(unsafe.Pointer(name)), 0, uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&val)), uintptr(unsafe.Pointer(&cb))); r != 0 {
		return false
	}
	return val == 0
}

// ============================================================
// 位图渲染：Go 生成逐像素 alpha 的圆形球（预乘 BGRA，自上而下）
// ============================================================

// ballRender 返回 size*size 的预乘 BGRA 缓冲（top-down）：
// 双层柔和投影 + 白色圆形底板 + 居中 logo。
func ballRender(size int) []byte {
	buf := make([]byte, size*size*4)
	c := float64(size-1) / 2
	rad := ballRadiusF - 0.5
	if rad < 1 {
		rad = 1
	}

	// 投影：双层柔和投影（近距离主阴影 + 大半径环境阴影），低透明度、大模糊，
	// 与设计稿中「白色圆钮 + 轻阴影」的观感一致。
	// blur 过大会被窗口边缘截断，上限由 ballShadowPadDIP 留白约束。
	pad := float64(ballShadowPad)
	keyOffY := math.Max(1.2, pad*0.16) // 主阴影轻微下移
	keyBlur := math.Max(4.0, pad*0.45) // 主阴影模糊半径
	const keyAlpha = 0.10              // 主阴影最大不透明度
	ambOffY := math.Max(0.6, pad*0.06) // 环境阴影几乎不下移
	ambBlur := math.Max(6.0, pad*0.85) // 环境阴影更大更散
	const ambAlpha = 0.05              // 环境阴影最大不透明度

	// 底板：纯白轻微渐变 + 极细内描边，保证在白墙纸上也有轮廓
	rimW := math.Max(1, float64(ballDiameterPx)*0.028)

	hasLogo := ballLogoImg != nil && !ballLogoRect.Empty()

	// 无 logo 时回退绘制闪电
	bolt := [][2]float64{
		{0.10, -0.48}, {-0.28, 0.06}, {-0.02, 0.06},
		{-0.12, 0.48}, {0.28, -0.10}, {0.02, -0.10},
	}
	s := rad * 1.15
	boltPoly := make([][2]float64, len(bolt))
	for i, pt := range bolt {
		boltPoly[i] = [2]float64{c + pt[0]*s, c + pt[1]*s}
	}

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dx := float64(x) - c
			dyBall := float64(y) - c
			distBall := math.Sqrt(dx*dx + dyBall*dyBall)

			var cov float64
			switch {
			case distBall <= rad-0.5:
				cov = 1
			case distBall < rad+0.5:
				cov = rad + 0.5 - distBall
			}

			dySh := float64(y) - (c + keyOffY)
			keyA := ballShadowAlpha(dx, dySh, rad, keyBlur, keyAlpha)
			ambA := ballShadowAlpha(dx, float64(y)-(c+ambOffY), rad, ambBlur, ambAlpha)
			// 双层叠加：1-(1-a)(1-b)，交叠处不会叠加发黑
			shA := 1 - (1-keyA)*(1-ambA)

			lr, lg, lb, la := 0.0, 0.0, 0.0, 0.0
			if hasLogo {
				lr, lg, lb, la = ballLogoSample(x, y, c)
			}

			if cov <= 0 && shA <= 0.004 && la <= 0 {
				continue
			}

			var sr, sg, sb float64
			if cov > 0 {
				t := (float64(y) - (c - rad)) / (2 * rad)
				if t < 0 {
					t = 0
				} else if t > 1 {
					t = 1
				}
				sr = lerp(255, 251, t)
				sg = lerp(255, 252, t)
				sb = lerp(255, 254, t)
				// 边缘内描边：极轻压暗，白底上也能看清圆形边界
				if distBall > rad-rimW {
					k := (distBall - (rad - rimW)) / rimW
					if k > 1 {
						k = 1
					}
					k *= 0.08
					sr *= 1 - k
					sg *= 1 - k
					sb *= 1 - k
				}
				if !hasLogo && pointInPoly(float64(x), float64(y), boltPoly) {
					sr, sg, sb = 48, 50, 58
				}
			}

			// 预乘 alpha 合成：底板盖投影，logo 盖底板
			a := cov + (1-cov)*shA
			r := sr * cov
			g := sg * cov
			b := sb * cov
			if la > 0 {
				r = lr*la + r*(1-la)
				g = lg*la + g*(1-la)
				b = lb*la + b*(1-la)
				a = la + a*(1-la)
			}
			if a <= 0.003 {
				continue
			}
			o := (y*size + x) * 4
			buf[o] = byte(clamp255(b))
			buf[o+1] = byte(clamp255(g))
			buf[o+2] = byte(clamp255(r))
			buf[o+3] = byte(clamp255(a * 255))
		}
	}
	return buf
}

// ballShadowAlpha 计算单层圆形柔和阴影在相对圆心 (dx, dy) 处的不透明度：
// 半径 rad 内为满值，向外 blur 像素用 smoothstep 过渡到 0，边缘无硬边。
func ballShadowAlpha(dx, dy, rad, blur, maxAlpha float64) float64 {
	d := math.Sqrt(dx*dx + dy*dy)
	if d >= rad+blur {
		return 0
	}
	f := (rad + blur - d) / blur
	if f > 1 {
		f = 1
	}
	return maxAlpha * f * f * (3 - 2*f)
}

// ballLogoSample 按底板比例将 logo 非透明内容居中缩放，返回预乘 RGB（0..255）与 alpha（0..1）。
func ballLogoSample(x, y int, c float64) (r, g, b, a float64) {
	rect := ballLogoRect
	target := float64(ballDiameterPx) * ballLogoRatio
	scale := math.Min(target/float64(rect.Dx()), target/float64(rect.Dy()))
	w := float64(rect.Dx()) * scale
	h := float64(rect.Dy()) * scale
	left := c - w/2
	top := c - h/2
	px := float64(x) + 0.5
	py := float64(y) + 0.5
	if px < left || px >= left+w || py < top || py >= top+h {
		return
	}
	// 目标像素中心 → 源内容坐标（像素中心在整数坐标）
	sx := float64(rect.Min.X) + (px-left)/w*float64(rect.Dx()) - 0.5
	sy := float64(rect.Min.Y) + (py-top)/h*float64(rect.Dy()) - 0.5
	return ballSampleBilinear(ballLogoImg, sx, sy)
}

// ballSampleBilinear 对图像做双线性采样（在预乘空间插值，避免透明边缘发黑）。
func ballSampleBilinear(img image.Image, sx, sy float64) (r, g, b, a float64) {
	bounds := img.Bounds()
	x0 := int(math.Floor(sx))
	y0 := int(math.Floor(sy))
	fx := sx - float64(x0)
	fy := sy - float64(y0)
	x1, y1 := x0+1, y0+1

	x0 = ballClampInt(x0, bounds.Min.X, bounds.Max.X-1)
	x1 = ballClampInt(x1, bounds.Min.X, bounds.Max.X-1)
	y0 = ballClampInt(y0, bounds.Min.Y, bounds.Max.Y-1)
	y1 = ballClampInt(y1, bounds.Min.Y, bounds.Max.Y-1)

	r00, g00, b00, a00 := img.At(x0, y0).RGBA()
	r10, g10, b10, a10 := img.At(x1, y0).RGBA()
	r01, g01, b01, a01 := img.At(x0, y1).RGBA()
	r11, g11, b11, a11 := img.At(x1, y1).RGBA()

	topR := lerp(float64(r00), float64(r10), fx)
	topG := lerp(float64(g00), float64(g10), fx)
	topB := lerp(float64(b00), float64(b10), fx)
	topA := lerp(float64(a00), float64(a10), fx)
	botR := lerp(float64(r01), float64(r11), fx)
	botG := lerp(float64(g01), float64(g11), fx)
	botB := lerp(float64(b01), float64(b11), fx)
	botA := lerp(float64(a01), float64(a11), fx)

	return lerp(topR, botR, fy) / 257,
		lerp(topG, botG, fy) / 257,
		lerp(topB, botB, fy) / 257,
		lerp(topA, botA, fy) / 65535
}

func ballClampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func lerp(a, b, t float64) float64 { return a + (b-a)*t }
func clamp255(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return v
}

func pointInPoly(px, py float64, poly [][2]float64) bool {
	inside := false
	n := len(poly)
	for i, j := 0, n-1; i < n; j, i = i, i+1 {
		xi, yi := poly[i][0], poly[i][1]
		xj, yj := poly[j][0], poly[j][1]
		if ((yi > py) != (yj > py)) &&
			(px < (xj-xi)*(py-yi)/(yj-yi)+xi) {
			inside = !inside
		}
	}
	return inside
}

// ballPaint 用 UpdateLayeredWindow 提交逐像素 alpha 的球位图。
func ballPaint(hwnd uintptr) {
	size := ballSizePx
	premul := ballRender(size)

	screenDC, _, _ := ballGetDC.Call(0)
	if screenDC == 0 {
		return
	}
	defer ballRelDC.Call(0, screenDC)

	memDC, _, _ := ballCompatDC.Call(screenDC)
	if memDC == 0 {
		return
	}
	defer ballDeleteDC.Call(memDC)

	var bmi ballBitmapInfo
	bmi.Header.BiSize = uint32(unsafe.Sizeof(bmi.Header))
	bmi.Header.BiWidth = int32(size)
	bmi.Header.BiHeight = -int32(size) // top-down
	bmi.Header.BiPlanes = 1
	bmi.Header.BiBitCount = 32
	bmi.Header.BiCompression = 0 // BI_RGB

	var bits *byte
	// CreateDIBSection(hdc, pbmi, iUsage, ppvBits, hSection, offset)
	// iUsage=DIB_RGB_COLORS(0)；ppvBits=&bits（第 3、4 参不可写反，否则返回 ERROR_INVALID_PARAMETER）
	bmp, _, _ := ballDIB.Call(memDC, uintptr(unsafe.Pointer(&bmi)),
		0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bmp == 0 || bits == nil {
		return
	}
	defer ballDelObj.Call(bmp)

	n := size * size * 4
	copy(unsafe.Slice(bits, n), premul)

	old, _, _ := ballSelObj.Call(memDC, bmp)

	var ptDst ballPoint
	l, t := ballCurPos(hwnd)
	ptDst.X, ptDst.Y = l, t
	sz := ballSize{CX: int32(size), CY: int32(size)}
	ptSrc := ballPoint{0, 0}
	blend := ballBlend{BlendOp: 0, BlendFlags: 0, SourceConstantAlpha: 255, AlphaFormat: 1} // AC_SRC_ALPHA

	ballULW.Call(hwnd, screenDC, uintptr(unsafe.Pointer(&ptDst)), uintptr(unsafe.Pointer(&sz)),
		memDC, uintptr(unsafe.Pointer(&ptSrc)), 0, uintptr(unsafe.Pointer(&blend)), ballULW_ALPHA)

	if old != 0 {
		ballSelObj.Call(memDC, old)
	}
}

// ballCurPos 读取当前窗口左上角（物理像素）。
func ballCurPos(hwnd uintptr) (int32, int32) {
	var rc ballRect
	ballGetRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	return rc.Left, rc.Top
}

// 确保 image 包被引用（ballRender 以字节缓冲实现，未直接使用 image 类型时保留占位）
var _ = image.Point{}
