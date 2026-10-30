//go:build windows

package launcher

import (
	"fmt"
	"strconv"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ============================================================
// Windows 窗口枚举与激活
//
// 只列"用户看得见、也切得过去"的顶层窗口：不可见的、被 DWM 披了 cloak 的
// （UWP 挂起态、其他虚拟桌面）、WS_EX_TOOLWINDOW 的小工具条、零尺寸的，
// 统统不收——否则搜 "x" 能捞出几十个空气窗口。
//
// x/sys 没导出 GetWindowTextW / SetForegroundWindow 这几个，挂 LazyProc；
// 每个都先 Find() 探一遍（找不到导出函数是 panic，不是 error）。
// ============================================================

var (
	modUser32Win  = windows.NewLazySystemDLL("user32.dll")
	procTextLen   = modUser32Win.NewProc("GetWindowTextLengthW")
	procTextGet   = modUser32Win.NewProc("GetWindowTextW")
	procSetFore   = modUser32Win.NewProc("SetForegroundWindow")
	procShowWin   = modUser32Win.NewProc("ShowWindow")
	procIsIconic  = modUser32Win.NewProc("IsIconic")
	procBringTop  = modUser32Win.NewProc("BringWindowToTop")
	procAttachIn  = modUser32Win.NewProc("AttachThreadInput")
	procWinLong   = modUser32Win.NewProc("GetWindowLongW")
	procWinRect   = modUser32Win.NewProc("GetWindowRect")
	procIsWinProc = modUser32Win.NewProc("IsWindow")
)

const (
	// gwlExStyle = GWL_EXSTYLE(-20)。传进 Call 时要当 uintptr 用，
	// 负数常量不能直接转 uintptr，用同位宽的补码形式表达（ 32/64 位都成立）。
	gwlExStyle       = ^uintptr(0) - 19
	wsExToolwindow   = 0x00000080
	wsExNoActivate   = 0x08000000
	dwmwaCloaked     = 14
	swRestore        = 9
	processQueryLimm = 0x1000 // PROCESS_QUERY_LIMITED_INFORMATION
	winMaxScan       = 400    // 枚举上限：再多说明桌面异常，及时收手
)

// enumerateWindows 过一遍顶层窗口，把候选交给 emit。
//
// winMaxScan 是唯一的中断条件：正常桌面远远达不到，只有枚举到异常数量
// （驱动/远控软件塞了几千个隐形顶层窗）才及早收手，不为一两条结果去跟
// EnumWindows 的返回值较劲。
func enumerateWindows(emit func(windowCand)) {
	if procTextLen.Find() != nil || procTextGet.Find() != nil || procWinLong.Find() != nil ||
		procIsWinProc.Find() != nil || procWinRect.Find() != nil {
		return
	}
	count := 0
	cb := func(hwnd, _ uintptr) uintptr {
		count++
		if count > winMaxScan {
			return 0 // 停：EnumWindows 返回 FALSE 结束枚举
		}
		if excludedHWND != 0 && hwnd == excludedHWND {
			return 1
		}
		title := windowTitle(hwnd)
		if title == "" || !showableWindow(hwnd) {
			return 1
		}
		emit(windowCand{HWND: hwnd, ID: strconv.FormatUint(uint64(hwnd), 10), Title: title})
		return 1
	}
	_ = windows.EnumWindows(windows.NewCallback(cb), nil)
}

// windowTitle 取窗口标题；空标题（大部分后台消息窗）由调用方丢弃。
func windowTitle(hwnd uintptr) string {
	n, _, _ := procTextLen.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	r, _, _ := procTextGet.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r == 0 {
		return ""
	}
	return windows.UTF16ToString(buf[:r])
}

// showableWindow 这个窗口值不值得进结果列表。
func showableWindow(hwnd uintptr) bool {
	if !windows.IsWindowVisible(windows.HWND(hwnd)) {
		return false
	}
	// 被 DWM 披纱的窗口（UWP 挂起、不在这个虚拟桌面）看着存在其实切不过去。
	// DwmGetWindowAttribute 在 x/sys 里是导出的，但拿不到错误时按未披纱处理。
	var cloaked uint32
	if err := windows.DwmGetWindowAttribute(windows.HWND(hwnd), dwmwaCloaked, unsafe.Pointer(&cloaked), 4); err == nil && cloaked != 0 {
		return false
	}
	ex, _, _ := procWinLong.Call(hwnd, gwlExStyle)
	if ex&wsExToolwindow != 0 || ex&wsExNoActivate != 0 {
		return false
	}
	var rc rect
	if drawn, _, _ := procWinRect.Call(hwnd, uintptr(unsafe.Pointer(&rc))); drawn == 0 {
		return false
	}
	if rc.Right-rc.Left <= 1 || rc.Bottom-rc.Top <= 1 {
		return false // 零尺寸/贴边隐藏的工具窗
	}
	return true
}

type rect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// windowExePath 窗口所属进程的可执行文件路径；取不到返回空串（图标与"打开位置"
// 随后一并退化，不影响这条结果本身）。
func windowExePath(hwnd uintptr) string {
	pid, err := windows.GetWindowThreadProcessId(windows.HWND(hwnd), nil)
	if err != nil || pid == 0 {
		return ""
	}
	h, err := windows.OpenProcess(processQueryLimm, false, pid)
	if err != nil {
		return ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	size := uint32(len(buf))
	if err := windows.QueryFullProcessImageName(h, 0, &buf[0], &size); err != nil {
		return ""
	}
	return windows.UTF16ToString(buf[:size])
}

// activateWindow 把一个已有窗口调到前台。
//
// 不用 Wails 的 win.Focus()：它底下是 chromium 的 MoveFocus，失败时会
// os.Exit(1) 直接把整个程序带走（本仓库踩过）。这里只走 Win32。
//
// 系统对"谁能抢前台"有限制：非前台进程调 SetForegroundWindow 常被允许"闪一下
// 任务栏"而已。所以失败时用 AttachThreadInput 把自己临时并进前台线程再试一次——
// 这是这类跳转的标准做法。
//
// 两条路都切不动时不报错：前台授权由系统把关，用户已经点了这一条，再弹个
// 失败提示只会更迷惑。
func activateWindow(id string) error {
	h, err := strconv.ParseUint(id, 10, 64)
	if err != nil || h == 0 {
		return fmt.Errorf("非法的窗口句柄 %q", id)
	}
	if procIsWinProc.Find() != nil || procSetFore.Find() != nil {
		return fmt.Errorf("当前系统不支持窗口跳转")
	}
	hwnd := uintptr(h)
	if r, _, _ := procIsWinProc.Call(hwnd); r == 0 {
		return fmt.Errorf("该窗口已经关闭")
	}
	if procIsIconic.Find() == nil {
		if r, _, _ := procIsIconic.Call(hwnd); r != 0 {
			procShowWin.Call(hwnd, uintptr(swRestore)) // 最小化先还原，否则切过去也看不见
		}
	}
	if procBringTop.Find() == nil {
		procBringTop.Call(hwnd)
	}
	r, _, _ := procSetFore.Call(hwnd)
	if r != 0 {
		return nil
	}
	if procAttachIn.Find() != nil {
		return nil // 尽力而为：切不动就留给用户自己点任务栏
	}
	// 把输入队列并进当前前台线程，短暂借用它的"可以设前台"权限。
	tidFg, _ := windows.GetWindowThreadProcessId(windows.GetForegroundWindow(), nil)
	tidSelf := windows.GetCurrentThreadId()
	procAttachIn.Call(uintptr(tidSelf), uintptr(tidFg), 1)
	if procBringTop.Find() == nil {
		procBringTop.Call(hwnd)
		procSetFore.Call(hwnd)
	}
	procAttachIn.Call(uintptr(tidSelf), uintptr(tidFg), 0)
	return nil
}
