//go:build windows

package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// createNoWindow 是 Win32 CREATE_NO_WINDOW：GUI 子系统（-H windowsgui）的发布包本就没有
// 控制台，但 dev 构建是控制台程序，光靠 HideWindow(SW_HIDE) 在 Win11 上藏不住 Windows
// Terminal（它是独立进程，不吃子进程的 STARTUPINFO 显示标志），只有根本不分配控制台才不闪窗。
const createNoWindow = 0x08000000

// restartSelf 重新拉起程序自身一次（不等结果、不闪控制台），供托盘「重启应用」在退出收尾时调用。
//
// 不带任何命令行参数：重启等于一次干净的双击启动。上一次启动若是由压缩右键、SendTo 或
// 通知点击（--archive / --archive-panel / tools:xxx）拉起来的，那些转交语义不该在重启后
// 被再执行一遍。
// 工作目录定在 exe 所在目录，与资源管理器直接双击启动一致（内置插件、data 等相对路径按此约定）。
func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("取不到程序自身路径: %w", err)
	}
	cmd := exec.Command(exe)
	if dir := filepath.Dir(exe); dir != "" {
		cmd.Dir = dir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("拉起新进程失败: %w", err)
	}
	// 不等结果，但要回收，避免僵尸句柄（本进程随即退出，这一句主要图个语义清楚）
	go func() { _ = cmd.Wait() }()
	return nil
}
