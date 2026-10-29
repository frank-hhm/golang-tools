//go:build !windows

package sysx

// platformIcon 非 Windows 没有"任意路径问 shell 要图标"的统一入口
// （Linux 要按 .desktop 的 Icon 键去 hicolor 主题目录里翻 png，还得处理图标名
// 与扩展名两套查找）。快捷搜索在 Linux 上本来就没有原生窗口，这里给空串，
// 前端按结果类型画字形占位。
func platformIcon(string) string { return "" }
