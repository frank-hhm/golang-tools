//go:build !windows

package gitcmd

import "os/exec"

// applyHidden 在非 Windows 平台是无操作：没有控制台窗口闪窗问题。
// 保留同名函数让 run.go 的调用点跨平台一致（AGENTS：差异按平台文件分，业务不分叉）。
func applyHidden(_ *exec.Cmd) {}
