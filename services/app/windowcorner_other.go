//go:build !windows

package app

import (
	"fmt"

	"Tools/services/sysx"
)

// setWindowCorners 非 Windows 平台没有 DWM 窗口圆角这一环（Linux 桌面圆角由合成器
// 决定、Web 面板根本没有原生窗），方法照常存在供绑定生成，如实返回不支持。
// 记忆档位也不存：没有原生窗可补应用。
func (a *App) setWindowCorners(radius string) error {
	return fmt.Errorf("%w：当前平台没有可设置的窗口圆角属性", sysx.ErrUnsupported)
}

// rememberedCornerRadius 恒为空串：非 Windows 不记忆圆角档。
func (a *App) rememberedCornerRadius() string { return "" }

// applyCornerPref 非 Windows 没有可下发的窗口圆角属性，直接报失败（调用方仅用于新窗
// 补应用的尽力而为路径，不报错）。
func applyCornerPref(hwnd uintptr, radius string) bool { return false }
