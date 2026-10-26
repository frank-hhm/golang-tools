//go:build windows

package panel

import "errors"

// restartPanel 在 Windows 上不适用：面板模式只在 Linux / 容器里部署，
// 桌面版没有"面板服务"可重启。这里给出明确说明而不是空实现。
func restartPanel() error {
	return errors.New("Windows 下没有面板服务进程（桌面版直接关掉窗口重开即可）")
}
