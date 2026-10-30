//go:build !windows

package launcher

import (
	"fmt"

	"Tools/services/sysx"
)

// 非 Windows 没有统一的"列出顶层窗口并把它调到前台"入口：X11 要走 EWMH
// （_NET_CLIENT_LIST + _NET_ACTIVE_WINDOW，还得判断自己跑不在图形会话里），
// Wayland 干脆不允许第三方程序抢焦点。快捷搜索的正身是原生置顶小窗，这个平台
// 上本来就没有，所以这里给空实现，保证引擎与绑定照样能编、能跑单测。

// enumerateWindows 非 Windows 不枚举窗口。
func enumerateWindows(func(windowCand)) {}

// windowExePath 非 Windows 拿不到窗口所属程序。
func windowExePath(uintptr) string { return "" }

// activateWindow 非 Windows 不支持窗口跳转。
func activateWindow(string) error {
	return fmt.Errorf("%w: 当前平台不支持窗口跳转", sysx.ErrUnsupported)
}
