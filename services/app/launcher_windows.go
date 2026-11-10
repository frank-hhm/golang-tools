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

// Win11 22H2+ 的窗口投影/圆角属于 frame backdrop 体系：DWMWA_WINDOW_CORNER_PREFERENCE
// 与 DWMWA_SYSTEMBACKDROP_TYPE 两个属性位及其取值已在 windowcorner_windows.go /
// nativebackdrop_windows.go 定义，这里直接复用，显式声明“我不是标准框架窗”，
// 掐掉 DWM 按重叠窗族待遇画的那套圆角+投影。低版本 Windows 上 DwmSetWindowAttribute
// 返回失败且无副作用。

// 原生窗口样式位：Wails 以 WS_OVERLAPPEDWINDOW 建窗，DisableResize 只摘掉 WS_THICKFRAME。
// 上一轮只清 WS_CAPTION|WS_THICKFRAME|WS_SYSMENU 后投影仍在——DWM 判定“重叠窗族”不只
// 看 caption 位（WS_MIN/MAXIMIZEBOX、滚动条位都还留着，窗口基类也是 overlapped 系），
// 且 Win11 的窗影挂在 frame backdrop 上，光清样式位不够。本轮改为整体脱离重叠窗族：
// 清掉 WS_OVERLAPPEDWINDOW 的全部位（0x00CF0000 = WS_BORDER|WS_DLGFRAME|WS_VSCROLL|
// WS_HSCROLL|WS_SYSMENU|WS_THICKFRAME|WS_MINIMIZEBOX|WS_MAXIMIZEBOX）后补 WS_POPUP。
// 裸 WS_POPUP 顶层窗默认没有 DWM NC 投影（无边框自绘窗想要阴影反而必须手动
// DwmExtendFrameIntoClientArea 启用，是社区共识）。透明窗走 WS_EX_NOREDIRECTIONBITMAP
// （DirectComposition），DWMWA_NCRENDERING_POLICY=DISABLED 对它无效，所以不走 NC 策略。
const (
	gwlStyle    = ^uintptr(15) // GWL_STYLE = -16（用 ^15 避开“负常量转 uintptr 溢出”编译错误）
	overlapBits = 0x00CF0000   // WS_OVERLAPPEDWINDOW 全部位
	wsPopup     = 0x80000000   // WS_POPUP：脱离重叠窗族，DWM 不再按框架窗画投影
	swpNoSize   = 0x0001
	swpNoMove   = 0x0002
	swpNoZOrder = 0x0004
	swpNoActive = 0x0010
	swpFrameChg = 0x0020 // SWP_FRAMECHANGED：强制重算非客户区，令样式变更即时生效
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
// 三步（全部是幂等的属性声明/样式改写，失败无副作用）：
// ① DWMWA_BORDER_COLOR=NONE 灭掉 Win11 那条 1px 自动描边；
// ② DWMWA_WINDOW_CORNER_PREFERENCE=DONOTROUND + DWMWA_SYSTEMBACKDROP_TYPE=NONE，
//
//	显式声明窗口没有 frame backdrop——Win11 22H2+ 的圆角与投影都挂在这套体系上；
//
// ③ GWL_STYLE 清掉 WS_OVERLAPPEDWINDOW 全部位并补 WS_POPUP，让窗口彻底脱离
//
//	“重叠窗族”，DWM 对裸 popup 顶层窗不画 NC 投影；写回后 FRAMECHANGED 重算非客户区。
//	面板不拖不改尺寸不弹系统菜单，这些位全清无副作用；Wails 的 isVisible/isMinimised
//	等只看 WS_VISIBLE/WS_MINIMIZE/WS_MAXIMIZE，不在清除范围内。
//
// 必须在窗口可见后（Show 之后）调用，建窗隐藏态调了也会被 Wails 首次 Show 时的样式下发覆盖。
// 样式前后值进 Debug 日志：若投影仍在，先看日志确认位是否真的清掉了（可能被第三方路径写回）。
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
	dontRound := uint32(dwmwcpDoNotRound)
	procDwmSetWindowAttr.Call(
		hwnd,
		uintptr(dwmwaWindowCornerPreference),
		uintptr(unsafe.Pointer(&dontRound)),
		uintptr(unsafe.Sizeof(dontRound)),
	)
	noBackdrop := uint32(dwmsbtNone)
	procDwmSetWindowAttr.Call(
		hwnd,
		uintptr(dwmwaSystemBackdropType),
		uintptr(unsafe.Pointer(&noBackdrop)),
		uintptr(unsafe.Sizeof(noBackdrop)),
	)
	// 转成裸 WS_POPUP：一次清掉重叠窗族的全部样式位并补 popup 标记。
	// 已是目标值时（120ms 补清/复用窗口再点亮）want==style，跳过写回与 FRAMECHANGED，
	// 不产生多余的重算抖动。
	style, _, _ := procGetWindowLongW.Call(hwnd, gwlStyle)
	if want := style & ^uintptr(overlapBits) | uintptr(wsPopup); want != style {
		procSetWindowLongW.Call(hwnd, gwlStyle, want)
		procSetWindowPosX.Call(
			hwnd, 0, 0, 0, 0, 0,
			uintptr(swpNoSize|swpNoMove|swpNoZOrder|swpNoActive|swpFrameChg),
		)
	}
	applog.Debugf("launcher", "已施加原生去投影组合 hwnd=%X style=%X", hwnd, style)
}
