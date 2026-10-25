//go:build !windows

package app

// 这些方法在 Windows 上依赖 Win32 / WebView2（截图选区浮层、内嵌浏览器窗口、
// 原生工具窗激活、录制与截图的后台窗口计数）。Linux 的 server 模式没有对应能力，
// 这里保留同名方法，只为让无标签的 app.go 里那些调用点能跨平台编译。
//
// 行为上退化为"什么也不做"或"没有"：这些入口在 Linux 面板里应当由前端按
// Capabilities 隐藏，不应依赖这些桩返回成功。

// PrewarmCaptureOverlay 非 Windows 没有截图浮层窗口，无需预热。
func (a *App) PrewarmCaptureOverlay() {}

// destroyCaptureOverlay 非 Windows 没有浮层可销毁。
func (a *App) destroyCaptureOverlay() {}

// BrowserCloseAll 非 Windows 没有内嵌浏览器窗口。
func (a *App) BrowserCloseAll() {}

// TryActivateNativeTool 非 Windows 没有原生工具窗口可激活，恒为 false。
func (a *App) TryActivateNativeTool(toolID string) bool { return false }
