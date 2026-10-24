//go:build !windows

// 非 Windows 平台占位：浮动球与全局热键都是 Win32 专属实现（ball_windows.go /
// hotkey_windows.go），这里只保证本包在无构建约束的 `go build ./...` 下依然有
// 同名入口，不至于报"build constraints exclude all Go files"。
// 唯一调用方 app.go 本身即 windows only，因此这些桩在真实链路上不会被走到。
package winui

import "fmt"

// StartBall 非 Windows 平台不支持。
func StartBall(h Host) error { return fmt.Errorf("浮动球仅支持 Windows") }

// StopBall 非 Windows 平台空操作。
func StopBall() {}

// ReloadBallIcon 非 Windows 平台空操作。
func ReloadBallIcon() {}

// StartHotkeys 非 Windows 平台空操作。
func StartHotkeys(h Host) {}

// ReloadHotkeys 非 Windows 平台空操作。
func ReloadHotkeys() {}
