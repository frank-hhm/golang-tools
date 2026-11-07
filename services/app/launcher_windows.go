//go:build windows

package app

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	"Tools/services/applog"
)

// lockWorkstation 锁定当前用户会话（命令面板里的"锁屏"）。
//
// x/sys 没导出 LockWorkStation，挂 LazyProc；先 Find() 探一遍——导出函数
// 找不到时是 panic 而不是 error。
//
// 锁屏需要桌面会话，Web 面板（-tags server）根本没有这条命令的入口：
// 上层 launcherMounted 已经把整套服务挡在外面，这里只按返回值如实报错。
func lockWorkstation() error {
	user32 := windows.NewLazySystemDLL("user32.dll")
	proc := user32.NewProc("LockWorkStation")
	if err := proc.Find(); err != nil {
		return err
	}
	// 返回值是 BOOL；0 表示失败（组策略禁锁屏等）。err 由 syscall 附带，
	// 但 Win32 对“成功”也会留一个脏的 LastError，所以判定只看 r，err 只进文案。
	if r, _, err := proc.Call(); r == 0 {
		return fmt.Errorf("锁定工作站失败: %v", err)
	}
	return nil
}

// DWMWA_BORDER_COLOR=34 / DWMWA_COLOR_NONE=0xFFFFFFFE（dwmapi.h，Win11 起可用）：
// Win11 会给无边框窗体自动描一条 1px 边框（默认取强调色/系统色），快捷搜索面板是
// 透明 + Frameless + 关掉装饰的窗，CSS 侧 border/box-shadow 早已归无，用户看到的
// “还有边框”就是这条原生窗框。
const dwmwaBorderColor = 34

// 原生窗口样式位：Wails 以 WS_OVERLAPPEDWINDOW 建窗，DisableResize 只摘掉 WS_THICKFRAME，
// WS_CAPTION(0x00C00000) 样式位仍留着——Frameless 只是用 WM_NCCALCSIZE 把非客户区算成整块
// 客户区，样式位没清。DWM 正是依据 WS_CAPTION 判定“带标题栏的窗”并画那块柔和投影。透明窗走
// WS_EX_NOREDIRECTIONBITMAP（DirectComposition），DWMWA_NCRENDERING_POLICY=DISABLED 对它无效，
// 所以历次“关 NC 渲染”都没去掉 shadow。真正管用的是把 WS_CAPTION 从 GWL_STYLE 里清掉。
const (
	gwlStyle      = ^uintptr(15) // GWL_STYLE = -16（用 ^15 避开“负常量转 uintptr 溢出”编译错误）
	wsCaption     = 0x00C00000   // WS_CAPTION = WS_BORDER | WS_DLGFRAME
	wsThickframe  = 0x00040000   // WS_THICKFRAME（可拖拽边框，DisableResize 本已摘掉，再清一次兜底）
	wsSysmenu     = 0x00080000   // WS_SYSMENU（系统菜单，面板不需要）
	swpNoSize     = 0x0001
	swpNoMove     = 0x0002
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010
	swpFrameChg   = 0x0020 // SWP_FRAMECHANGED：强制重算非客户区，令样式变更即时生效
)

var (
	user32Launcher     = windows.NewLazySystemDLL("user32.dll")
	procGetWindowLongW = user32Launcher.NewProc("GetWindowLongW")
	procSetWindowLongW = user32Launcher.NewProc("SetWindowLongW")
	procSetWindowPosX  = user32Launcher.NewProc("SetWindowPos")
)

// stripLauncherWindowBorder 去掉面板原生窗口的 Win11 自动边框与投影。
// procDwmSetWindowAttr 复用 nativebackdrop_windows.go 里的同一个 DWM 入口。
//
// 两步：① DWMWA_BORDER_COLOR=NONE 灭掉 Win11 那条 1px 自动描边；② 清 GWL_STYLE 的
// WS_CAPTION 并 SWP_FRAMECHANGED，灭掉 DWM 因“带标题栏窗”而画的那块矩形投影（这才是
// 用户反复反馈的 shadow 真身）。低于 Win11 时第①步 DwmSetWindowAttribute 返回失败且无副作用，
// 不影响功能，只进 Debug 日志。必须在窗口可见后（Show 之后）调用，建窗隐藏态调了也会被
// Wails 首次 Show 时的样式下发覆盖。
func stripLauncherWindowBorder(hwnd uintptr) {
	if hwnd == 0 {
		return
	}
	none := uint32(0xFFFFFFFE) // DWMWA_COLOR_NONE
	procDwmSetWindowAttr.Call(
		hwnd,
		uintptr(dwmwaBorderColor),
		uintptr(unsafe.Pointer(&none)),
		uintptr(unsafe.Sizeof(none)),
	)
	// 清到 popup 级：一次去掉 WS_CAPTION|WS_THICKFRAME|WS_SYSMENU（DWM 判定“带标题栏/可拖边框
	// 窗”而画投影的全部依据），保留其余位（WS_VISIBLE/WS_CLIPCHILDREN 等）不变。面板不拖不改
	// 尺寸不弹系统菜单，去掉这些位无副作用。写回后 FRAMECHANGED 触发 WM_NCCALCSIZE 重算。
	const stripMask = uintptr(wsCaption | wsThickframe | wsSysmenu)
	style, _, _ := procGetWindowLongW.Call(hwnd, gwlStyle)
	if newStyle := style & ^stripMask; newStyle != style {
		procSetWindowLongW.Call(hwnd, gwlStyle, newStyle)
		procSetWindowPosX.Call(
			hwnd, 0, 0, 0, 0, 0,
			uintptr(swpNoSize|swpNoMove|swpNoZOrder|swpNoActivate|swpFrameChg),
		)
	}
	applog.Debugf("launcher", "已尝试去除面板窗口原生边框与投影 hwnd=%X", hwnd)
}
