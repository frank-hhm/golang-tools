//go:build !windows

package app

import (
	"fmt"

	"Tools/services/sysx"
)

// setNativeBackdrop 非 Windows 平台没有系统级桌面材质，返回不支持。
//
// 方法本体（SetWindowNativeBackdrop）在跨平台文件里声明，绑定照常生成；这里只是让
// Linux / server 构建链接通过并如实报错，前端据 caps.nativeWindows/caps.systemBackdrop 隐藏入口。
func (a *App) setNativeBackdrop(_ string) error {
	return fmt.Errorf("%w：原生桌面材质仅 Windows 11 支持", sysx.ErrUnsupported)
}
