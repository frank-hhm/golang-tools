//go:build windows

package app

import (
	"fmt"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"

	"Tools/services/applog"
	"Tools/services/sysx"
)

// DWMWA_WINDOW_CORNER_PREFERENCE=33（dwmapi.h，Win11 起可用）：只有三态，
// 没有连续半径值。1=DoNotRound 直角，2=Round 跟随系统默认圆角（~8px）。
// 低于 Win11 时 DwmSetWindowAttribute 返回失败且不产生副作用，如实记日志即可。
const (
	dwmwaWindowCornerPreference = 33

	dwmwcpDefault    = 0
	dwmwcpDoNotRound = 1
	dwmwcpRound      = 2
	dwmwcpRoundSmall = 3
)

// setWindowCorners 把圆角档下发到主窗与全部已开的工具窗（含未建好句柄的窗自动跳过）。
// 同时把档位记进 App，OpenToolWindow 新建窗口后据此补应用，新窗不再落回系统默认。
func (a *App) setWindowCorners(radius string) error {
	if !sysx.Caps().NativeWindows {
		return fmt.Errorf("%w：当前运行模式没有原生窗口，无法设置窗口圆角", sysx.ErrUnsupported)
	}
	if a.app == nil {
		return fmt.Errorf("应用尚未初始化")
	}
	a.cornerMu.Lock()
	a.cornerRadius = radius
	a.cornerMu.Unlock()

	wins := make([]application.Window, 0, 8)
	if a.mainWindow != nil {
		wins = append(wins, a.mainWindow)
	}
	for _, w := range a.app.Window.GetAll() {
		if w != nil {
			wins = append(wins, w)
		}
	}
	seen := make(map[uintptr]bool, len(wins))
	done := 0
	for _, w := range wins {
		hwnd := uintptr(w.NativeWindow())
		if hwnd == 0 || seen[hwnd] {
			continue
		}
		seen[hwnd] = true
		if applyCornerPref(hwnd, radius) {
			done++
		}
	}
	applog.Infof("theme", "窗口圆角已按圆角档 %s 下发到 %d 个窗口", radius, done)
	return nil
}

// applyCornerPref 对单个窗口下发圆角偏好，返回是否成功（低于 Win11 时失败不致命）。
// 供 setWindowCorners 批量下发与 OpenToolWindow 建新窗后补应用共用。
func applyCornerPref(hwnd uintptr, radius string) bool {
	pref := uint32(dwmwcpRound)
	if radius == "none" || radius == "small" {
		pref = dwmwcpDoNotRound
	}
	hr, _, _ := procDwmSetWindowAttr.Call(
		hwnd,
		uintptr(dwmwaWindowCornerPreference),
		uintptr(unsafe.Pointer(&pref)),
		uintptr(unsafe.Sizeof(pref)),
	)
	if int32(hr) != 0 {
		applog.Debugf("theme", "DwmSetWindowAttribute(CORNER_PREFERENCE=%d) hwnd=%X hr=0x%08x（可能低于 Win11，忽略）", pref, hwnd, uint32(hr))
		return false
	}
	return true
}

// rememberedCornerRadius 取最近一次下发的圆角档（空串=还没下发过，新窗维持系统默认）。
func (a *App) rememberedCornerRadius() string {
	a.cornerMu.Lock()
	defer a.cornerMu.Unlock()
	return a.cornerRadius
}
