//go:build linux

package sysx

import (
	"fmt"
	"os/exec"
	"path/filepath"
)

// linuxOpener 用 xdg-open 交给桌面环境的默认程序处理。
type linuxOpener struct{}

// platformOpen 返回 Linux 打开器。
func platformOpen() Opener { return linuxOpener{} }

// Dir 在文件管理器中打开目录。
func (linuxOpener) Dir(path string) error { return xdgOpen(path) }

// URL 用默认浏览器打开链接。
func (linuxOpener) URL(u string) error { return xdgOpen(u) }

// xdgOpen 调 xdg-open 并立即返回（不等待目标程序退出）。
//
// 服务器上常常没有桌面环境，此时 xdg-open 会失败——调用方（面板）应把错误
// 转成"当前环境无图形界面"的提示，而不是当成致命错误。
func xdgOpen(target string) error {
	if _, err := exec.LookPath("xdg-open"); err != nil {
		return fmt.Errorf("%w: 未安装 xdg-open（当前环境可能没有图形界面）", ErrUnsupported)
	}
	cmd := exec.Command("xdg-open", target)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 %s 失败: %w", filepath.Base("xdg-open"), err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
