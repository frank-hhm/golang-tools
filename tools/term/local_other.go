//go:build !windows

package term

import "fmt"

// startLocalShell 非 Windows 平台不提供本地伪终端（ConPTY 是 Windows 专属）。
// server 模式下 Linux/macOS 打开本地默认项时，如实返回不支持。
func startLocalShell(id, title, exe string, args []string, cwd string, cols, rows int) (*session, error) {
	return nil, fmt.Errorf("本地终端目前仅支持 Windows")
}
