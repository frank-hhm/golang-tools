package app

import (
	"fmt"
)

/*
窗口圆角与主题「圆角」档联动（Windows 桌面）。

主题里的圆角投射成 --base-radius-* / --border-radius-*，页面内组件自动跟随；但主窗与
工具窗（各自独立的 WebView2 原生窗口）的四角是 DWM 在窗口合成层裁的，CSS 够不着。
这里补上原生这一环：SetWindowCorner 把圆角档下发到全部已开窗口，OpenToolWindow 建窗后
也按记忆档位补应用一次，新窗不再回到系统默认圆角。

能力边界（UI 与注释都必须如实）：Win11 的 DWMWA_WINDOW_CORNER_PREFERENCE 只有
「跟随系统默认圆角 / 直角」两态，没有连续半径——none/small 出直角，default/large
都是系统默认那档 ~8px 圆角。想要逐像素自定义窗口圆角只能走逐像素透明窗 + CSS
裁切，那套因与无边框/DWM 冲突整体下线过（见 AGENTS.md 与 launcher.go 同段说明），
本实现不重走老路。

方法跨平台存在（供绑定生成），非 Windows / 无原生窗构建返回 sysx.ErrUnsupported，
前端据此静默跳过——与 SetWindowNativeBackdrop 同套约定。
*/

// SetWindowCorner 按主题圆角档设置全部原生窗口（主窗 + 工具窗）的窗口圆角。
// radius ∈ {none, small, default, large}（与 ThemeConfig.Radius 同一套取值）。
func (a *App) SetWindowCorner(radius string) error {
	switch radius {
	case "none", "small", "default", "large":
	default:
		return fmt.Errorf("未知的圆角档: %s", radius)
	}
	return a.setWindowCorners(radius)
}
