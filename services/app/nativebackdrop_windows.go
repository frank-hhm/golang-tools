//go:build windows

package app

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"

	"Tools/services/applog"
	"Tools/services/sysx"
)

// DWM 窗口属性与取值（dwmapi.h）。
//
// DWMWA_SYSTEMBACKDROP_TYPE=38 自 Windows 11 22621 起可用；低于该版本时
// DwmSetWindowAttribute 返回失败且不产生副作用。前端已用 caps.systemBackdrop 隐藏
// 入口，这里不重复判版本，仅在下发失败时如实记录。
const (
	dwmwaSystemBackdropType = 38

	dwmsbtAuto    = 0
	dwmsbtNone    = 1
	dwmsbtMica    = 2
	dwmsbtAcrylic = 3
	dwmsbtTabbed  = 4
)

var (
	dwmapi               = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmSetWindowAttr = dwmapi.NewProc("DwmSetWindowAttribute")
)

// setNativeBackdrop 对主窗口运行期设置系统级 backdrop 材质（Win11 22621+）。
func (a *App) setNativeBackdrop(mode string) error {
	if !sysx.Caps().NativeWindows {
		return fmt.Errorf("%w：当前运行模式没有原生窗口，无法设置原生材质", sysx.ErrUnsupported)
	}
	if a.mainWindow == nil {
		return fmt.Errorf("主窗口尚未就绪")
	}
	var bt uint32
	switch mode {
	case "mica":
		bt = dwmsbtMica
	case "acrylic":
		bt = dwmsbtAcrylic
	case "tabbed":
		bt = dwmsbtTabbed
	case "none":
		bt = dwmsbtNone
	default:
		bt = dwmsbtAuto
	}
	hwnd := a.mainWindow.NativeWindow()
	if hwnd == nil {
		return fmt.Errorf("无法获取主窗口原生句柄")
	}
	hr, _, _ := procDwmSetWindowAttr.Call(
		uintptr(hwnd),
		uintptr(dwmwaSystemBackdropType),
		uintptr(unsafe.Pointer(&bt)),
		uintptr(unsafe.Sizeof(bt)),
	)
	// HRESULT：0 = S_OK；非 0 常见于低于 Win11 22621（属性不被支持）
	if int32(hr) != 0 {
		applog.Infof("theme", "DwmSetWindowAttribute(SYSTEMBACKDROP_TYPE=%d) hr=0x%08x（可能低于 Win11 22621，忽略）", bt, uint32(hr))
		return fmt.Errorf("设置原生材质失败：需 Windows 11 22621 及以上")
	}
	applog.Infof("theme", "主窗口原生材质已设为 %s", mode)
	return nil
}
