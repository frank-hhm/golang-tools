package app

import (
	"fmt"
)

/*
原生 DWM 材质（Windows 11 22621+ 的系统级桌面模糊 Acrylic/Mica/Tabbed）作为全局主题背景档
（ThemeConfig.Background.Type=="native"）的运行期切换入口。

方法跨平台存在（供绑定生成），非 Windows / 无原生窗构建返回 sysx.ErrUnsupported，前端据此隐藏
入口——与 ImportThemeBackground / OpenGlassDebugWindow 同套约定，不靠"调用失败再回退"。

运行期下发的是 DWMWA_SYSTEMBACKDROP_TYPE 这一独立 DWM 属性，对已存在的主窗即时生效。
但 WebView2 客户区是否随之透出桌面，取决于建窗时是否为 translucent：主窗默认不透明，
所以"逐像素透明/桌面模糊贯穿内容"仍以重启后由 main.go 启动路径按 native 档建 translucent
窗为准（设置页选 native 会如实提示需重启）。此处运行期即时切换的是窗口系统材质本身。
*/

// SetWindowNativeBackdrop 对主窗口即时下发生成原生 DWM 材质档。
// mode ∈ {acrylic, mica, tabbed, none}（none 关闭系统材质）。
func (a *App) SetWindowNativeBackdrop(mode string) error {
	switch mode {
	case "acrylic", "mica", "tabbed", "none":
	default:
		return fmt.Errorf("未知的原生材质档: %s", mode)
	}
	return a.setNativeBackdrop(mode)
}
