package launcher

import (
	"Tools/services/sysx"
)

// ============================================================
// 跨平台执行动作
//
// 打开文件 / 打开链接 / 取图标这三件事 sysx 已经各平台都收口了，这里只做一层
// 语义命名，让 launcher.go 里的分支读起来是"启动应用/打开文件"而不是"调接口"。
// 真正平台专属的（在资源管理器里选中、把某个 HWND 调到前台）在 exec_windows.go /
// winscan_windows.go 里。
// ============================================================

// iconOf 任意路径（exe / 文件 / .lnk）经 shell 解析后的图标 PNG dataURL；取不到返回 ""。
func iconOf(path string) string {
	if path == "" {
		return ""
	}
	return sysx.Icon(path)
}

// launchFile 把文件交给 shell 关联的程序（docx 走 Word、图片走看图）。
//
// 用 Launch 而不是 File：Windows 下 sysx 的 File 是"用记事本编辑"，
// 本仓库拿它开 hosts 与配置文本；这里要的是"打开"语义。
func launchFile(path string) error { return sysx.Open().Launch(path) }

// launchApp 启动一个应用条目。ID 常常是个 .lnk，shell 会解析到它背后的 exe。
func launchApp(it Item) error { return sysx.Open().Launch(it.ID) }

// openURL 用默认浏览器打开链接。
func openURL(u string) error { return sysx.Open().URL(u) }
