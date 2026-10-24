//go:build windows

package screenshot

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ============================================================
// Windows 全屏取色覆盖层（“从屏幕取色”）
//
// 交互：
//   1. 冻结一帧屏幕（prepareRender 在建窗前完成，避免自捕获）
//   2. 深色遮罩 + 跟随光标的放大镜（含坐标与 HEX/RGB 标签）
//   3. 单击任意位置取该点像素颜色并退出
//   4. 右键 / ESC 取消
//
// 与选区截图不同：不依赖 WebView 的 EyeDropper，在任意窗口（含隐藏/
// 最小化状态的工具窗口）中调用均可用。“取色时隐藏本窗口”已移除：
// 跨窗口隐藏与 Wails 状态机存在多种冲突，收益小、风险大。
// ============================================================

var (
	// 窗口类只需注册一次，进程生命周期内常驻，避免每次取色重复 Register/Unregister。
	pickerClassOnce sync.Once
	pickerClassAtom uintptr
)

const pickerClassName = "ScreenPickerCls"

// 退出机制：取色/选区消息循环均不用 PostQuitMessage/WM_QUIT 退出。
// 坑：WM_QUIT 是线程级消息，一旦投递而未被 GetMessage 消费，残留会毒化被
// Go 复用的 LockOSThread 线程，下一次取色 GetMessage 立刻返回 0（“秒进秒退”
// 直接返回 #000000）；而“窗口过程内 DestroyWindow + 排空 WM_QUIT”实测会卡死。
// 方案：退出只靠窗口过程置位 done 标志；循环内 GetMessage 无消息时会阻塞，
// 用一个长间隔定时器（WM_TIMER）唤醒来做兜底超时——WM_TIMER 会被 GetMessage
// 正常取出并唤醒阻塞，不需任何外部线程/事件，也不产生 WM_QUIT。
var (
	procSetTimer  = modUser32.NewProc("SetTimer")
	procKillTimer = modUser32.NewProc("KillTimer")
)

const (
	WM_TIMER      = 0x0111
	pickerTimerID = uintptr(0xC0DE) // 兜底定时器 ID（任意非 0 常量）
)

// pickerState 取色覆盖层状态（线程安全）。
type pickerState struct {
	mu      sync.Mutex
	curX    int32 // 当前光标位置
	curY    int32
	sawDown bool // 覆盖层自身收到过按下消息（防止启动按钮的残留抬起被误判为取色）
	done    bool // 已取色或已取消
	cancel  bool // 用户取消
	r, g, b byte // 命中的颜色
}

// gPick 窗口过程回调无法传参，使用全局状态。
var gPick *pickerState

// pickerWindowProc 取色覆盖层消息处理：移动刷新放大镜，单击取色，右键/ESC 取消。
func pickerWindowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	// lParam：低/高 16 位为客户端坐标；窗口铺满主屏(0,0)，客户端坐标==屏幕坐标。
	x := int32(int16(uint16(lParam & 0xFFFF)))
	y := int32(int16(uint16((lParam >> 16) & 0xFFFF)))

	switch msg {
	case WM_MOUSEMOVE:
		gPick.mu.Lock()
		gPick.curX, gPick.curY = x, y
		gPick.mu.Unlock()
		pickerRedraw(hwnd, gPick)

	case WM_LBUTTONDOWN:
		// 记录“覆盖层自身收到过按下”，并同步光标位置。
		gPick.mu.Lock()
		gPick.sawDown = true
		gPick.curX, gPick.curY = x, y
		gPick.mu.Unlock()
		pickerRedraw(hwnd, gPick)

	case WM_LBUTTONUP:
		// 关键：若覆盖层从未收到过按下，说明这是点击“从屏幕取色”按钮时
		// 残留的抬起消息（覆盖层弹出瞬间鼠标正悬停在其上），直接忽略，
		// 否则会出现刚弹出就立即取色返回、窗口被秒恢复的错觉。
		gPick.mu.Lock()
		if !gPick.sawDown {
			gPick.mu.Unlock()
			selLogf("picker 忽略残留的 up (%d,%d)：未经过本层按下", x, y)
			break
		}
		if gRender != nil {
			gPick.r, gPick.g, gPick.b = rawPixelColor(gRender.rawBits, gRender.w, gRender.h, int(x), int(y))
		}
		gPick.done = true
		gPick.mu.Unlock()
		selLogf("picker click (%d,%d) -> 取色", x, y)

	case WM_RBUTTONUP:
		gPick.mu.Lock()
		gPick.cancel = true
		gPick.done = true
		gPick.mu.Unlock()
		selLogf("picker rightup -> 取消取色")

	case WM_KEYDOWN:
		if wParam == uintptr(VK_ESCAPE) {
			gPick.mu.Lock()
			gPick.cancel = true
			gPick.done = true
			gPick.mu.Unlock()
			selLogf("picker ESC -> 取消取色")
		}

	case WM_PAINT:
		var ps PAINTSTRUCT
		procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		pickerRedraw(hwnd, gPick)
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	case WM_ERASEBKGND:
		return 1

	case WM_TIMER:
		// 兜底超时定时器触发：视为取消，退出循环（同时防止覆盖层卡死屏幕）。
		if wParam == pickerTimerID {
			procKillTimer.Call(hwnd, pickerTimerID)
			gPick.mu.Lock()
			gPick.cancel = true
			gPick.done = true
			gPick.mu.Unlock()
			selLogf("picker 兜底超时（WM_TIMER），退出取色")
		}

	case WM_DESTROY:
		// 故意不 PostQuitMessage：循环靠 done 退出，WM_QUIT 一旦投递
		// 就可能残留在线程队列毒化下一次取色/选区（“秒进秒退”#000000）。
	}

	return defWindowProc(hwnd, uintptr(msg), wParam, lParam)
}

// pickerRedraw 合成一帧：压暗底图 + 光标处放大镜 + 顶部操作提示，BitBlt 到窗口。
func pickerRedraw(hwnd uintptr, st *pickerState) {
	rc := gRender
	if rc == nil {
		return
	}
	st.mu.Lock()
	cx, cy := int(st.curX), int(st.curY)
	st.mu.Unlock()

	work := rc.workBits
	copy(work, rc.dimBits)

	drawMagnifier(work, rc.rawBits, rc.workDC, rc.w, rc.h, cx, cy)

	hint := "单击取色 · 右键 / ESC 取消"
	boxW := len([]rune(hint))*8 + 16
	boxH := 24
	bx := (rc.w - boxW) / 2
	by := 40
	dibFill(work, rc.w, rc.h, bx, by, bx+boxW, by+boxH, 0, 0, 0)
	drawTextOn(rc.workDC, bx+8, by+4, hint, textColor)

	winDC := getDC(hwnd)
	if winDC == 0 {
		return
	}
	defer releaseDC(hwnd, winDC)
	bitBlt(winDC, 0, 0, int32(rc.w), int32(rc.h), rc.workDC, 0, 0, SRCCOPY)
}

// pickColorWindows 是取色覆盖层主流程：建窗 -> 消息循环 -> 返回颜色。
// 返回 ("#RRGGBB", true, nil) 表示取色成功；取消时 ok=false。
func pickColorWindows() (string, bool, error) {
	// Win32 消息循环要求“建窗线程 == 泵消息线程”，钉死当前协程到 OS 线程。
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	selLogf("=== 启动屏幕取色工具 ===")

	st := &pickerState{}
	gPick = st
	st.curX, st.curY = getCursorPos()

	screenW := getSystemMetrics(SM_CXSCREEN)
	screenH := getSystemMetrics(SM_CYSCREEN)

	// 步骤 1：在遮罩窗口显示之前先“冻结”一帧屏幕，避免自捕获。
	gRender = prepareRender()
	if gRender == nil {
		return "", false, fmt.Errorf("prepareRender failed")
	}
	defer func() {
		releaseRender(gRender)
		gRender = nil
	}()

	ensureFont()

	// 步骤 2：注册窗口类（仅首次）。
	hInstance, _, _ := procGetModuleHandleW.Call(0)
	className := utf16Ptr(pickerClassName)
	pickerClassOnce.Do(func() {
		wc := WNDCLASSW{
			Style:         CS_OWNDC,
			LpfnWndProc:   syscall.NewCallback(pickerWindowProc),
			HInstance:     hInstance,
			HCursor:       loadCursor(IDC_CROSS),
			LpszClassName: className,
		}
		pickerClassAtom, _, _ = procRegisterClassW.Call(uintptr(unsafe.Pointer(&wc)))
	})
	if pickerClassAtom == 0 {
		return "", false, fmt.Errorf("register picker class failed")
	}

	// 步骤 3：创建全屏置顶弹窗（尚未显示，不会被冻结帧捕获）。
	hwnd := createWindowEx(
		WS_EX_TOPMOST,
		className,
		utf16Ptr("picker"),
		WS_POPUP,
		0, 0, screenW, screenH,
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return "", false, windows.GetLastError()
	}

	showWindow(hwnd, SW_SHOW)
	updateWindow(hwnd)
	setForegroundWindow(hwnd)
	setFocus(hwnd)
	pickerRedraw(hwnd, st)

	// 兜底超时：启动 5 分钟定时器。无用户操作时 GetMessage 会阻塞，
	// 到点 WM_TIMER 会投递到本窗口并唤醒 GetMessage，在窗口过程里置位取消。
	// 不需外部线程/事件，也不产生 WM_QUIT。
	procSetTimer.Call(hwnd, pickerTimerID, uintptr(5*60*1000), 0)
	defer procKillTimer.Call(hwnd, pickerTimerID)

	var msg MSG
	for {
		ret := getMessage(&msg, 0, 0, 0)
		if ret == 0 {
			// 意外 WM_QUIT（如历史残留）：不当作取色结果，丢弃后继续，
			// 避免“秒进秒退返回 #000000”（WM_QUIT 一次性，不会无限循环）。
			selLogf("picker 收到意外 WM_QUIT，丢弃并继续等待")
			continue
		}
		translateMessage(&msg)
		dispatchMessage(&msg)

		st.mu.Lock()
		done := st.done
		st.mu.Unlock()
		if done {
			break
		}
	}
	destroyWindow(hwnd)

	st.mu.Lock()
	defer st.mu.Unlock()
	if st.cancel {
		selLogf("=== 取色结束：取消 ===")
		return "", false, nil
	}
	hex := fmt.Sprintf("#%02X%02X%02X", st.r, st.g, st.b)
	selLogf("=== 取色结束：%s ===", hex)
	return hex, true, nil
}
