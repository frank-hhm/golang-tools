//go:build !windows

package launcher

import (
	"os"
	"path/filepath"
	"strings"
)

// ============================================================
// 非 Windows 的目录清单
//
// 快捷搜索的正身是 Alt+Space 原生置顶小窗，Linux/server 构建没有这个窗口形态，
// 所以这里只求"引擎照样能跑、设置页照样能看到目录"，不追求与 Windows 等价：
//   - 应用清单为空（.desktop 要解析 Exec 的 %f/%U 字段码、还要处理本地化 Name[xx]，
//     那是另一件事，不该混进一个 Windows 定位的功能里顺手做）；
//   - 没有"最近使用"目录概念。
// ============================================================

// defaultRoots 主目录下常见的那几个用户目录 + XDG 配置里指到的位置。
func defaultRoots() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	out := []string{
		xdgUserDir("XDG_DOCUMENTS_DIR", filepath.Join(home, "Documents")),
		xdgUserDir("XDG_DOWNLOAD_DIR", filepath.Join(home, "Downloads")),
		xdgUserDir("XDG_DESKTOP_DIR", filepath.Join(home, "Desktop")),
		xdgUserDir("XDG_PICTURES_DIR", filepath.Join(home, "Pictures")),
	}
	return compactDirs(out...)
}

// xdgUserDir 读 user-dirs.dirs 里的定义（$HOME 展开成真实主目录）；
// 环境变量优先，都没有时退回传入的默认值。
func xdgUserDir(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	cfg := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "user-dirs.dirs")
	if os.Getenv("XDG_CONFIG_HOME") == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cfg = filepath.Join(home, ".config", "user-dirs.dirs")
		}
	}
	data, err := os.ReadFile(cfg)
	if err != nil {
		return fallback
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, key+"=") {
			continue
		}
		v := strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, key+"=")), `"`)
		v = strings.ReplaceAll(v, "$HOME", os.Getenv("HOME"))
		if v == "" {
			return fallback
		}
		return v
	}
	return fallback
}

// startMenuRoots 非 Windows 不扫应用清单，见文件头。
func startMenuRoots() []string { return nil }

// recentRoot 非 Windows 没有"最近使用"目录。
func recentRoot() string { return "" }
