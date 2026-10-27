//go:build windows

package sysx

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
)

// windowsOpener 用资源管理器/默认浏览器打开目标。
type windowsOpener struct{}

// platformOpen 返回 Windows 打开器。
func platformOpen() Opener { return windowsOpener{} }

// Dir 在资源管理器中打开目录。
func (windowsOpener) Dir(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	return startDetached("explorer.exe", abs)
}

// File 用记事本打开文件（系统 hosts 等受保护文件保存仍需提权，由调用方处理）。
func (windowsOpener) File(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	return startDetached("notepad.exe", abs)
}

// URL 用默认浏览器打开链接。
func (windowsOpener) URL(u string) error {
	return startDetached("rundll32.exe", "url.dll,FileProtocolHandler", u)
}

// startDetached 起一个不等待、不弹黑窗的子进程。
func startDetached(bin string, args ...string) error {
	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 %s 失败: %w", filepath.Base(bin), err)
	}
	// 不等结果，但必须回收，避免僵尸句柄堆积
	go func() { _ = cmd.Wait() }()
	return nil
}
