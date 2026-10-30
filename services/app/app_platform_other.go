//go:build !windows

package app

import (
	"fmt"
	"runtime"

	"Tools/services/sysx"
)

// platformState 汇总 App 里只在 Windows 上才有意义的字段。
//
// 非 Windows（Linux 的 server 无头模式）没有任何原生窗口/截图浮层，
// 这里保持空结构体，让 App 的定义能跨平台复用同一份。
type platformState struct{}

// workAreaDIP 返回主显示器工作区坐标。
//
// server 模式下窗口是 HTTP 侧的桩实现，位置参数不参与任何渲染，
// 这里给一个 1080p 的保守值只是为了不让调用方的坐标计算出负数。
func workAreaDIP() (int, int, int, int) {
	return 0, 0, 1920, 1080
}

// ShowSystemNotification 非 Windows 平台没有 WinRT 通知中心。
// 调用方（前端）在 Linux 面板里应当按 Capabilities 隐藏该入口。
func (a *App) ShowSystemNotification(title string, content string) error {
	return fmt.Errorf("%w: %s 上没有系统通知中心，请改用前端提示", sysx.ErrUnsupported, runtime.GOOS)
}

// notifySync 同步版通知：非 Windows 没有通知中心，一律回 unsupported，
// 调用方（压缩的无界面分支）只把它当一句日志看。
func (a *App) notifySync(title string, content string) error {
	return a.ShowSystemNotification(title, content)
}

// notifyWithTarget 非 Windows 没有通知中心，带不带跳转目标一律回 unsupported。
// 方法本身（ShowSystemNotificationWithTarget）在 navigate.go 里跨平台存在，
// 不会被 build tag 挡掉而变成 unknown bound method id。
func (a *App) notifyWithTarget(title string, content string, target string) error {
	return a.ShowSystemNotification(title, content)
}

// ensureToastRegistered 非 Windows 没有 WinRT 通知中心，也不需要 AUMID/快捷方式
// 登记：空实现。与 toast_windows.go 同名函数成对，让 ServiceStartup 能跨平台调用。
func ensureToastRegistered() {}

// toastStatusText 非 Windows 无系统通知中心，诊断文本直接回不支持。
func toastStatusText() string {
	return "本平台无 Windows 系统通知中心（原生 Toast 仅桌面版 Windows 提供）"
}
