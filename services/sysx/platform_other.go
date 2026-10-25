//go:build !windows && !linux

package sysx

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
)

// 兜底平台（darwin 等）：只保证能编译、能力按最小集处理。
// 这些平台不在当前交付范围内，等真要用时按 linux 的两套实现补。

// detectCaps 兜底探测：一律直接托管、绿色版布局。
func detectCaps() Capabilities {
	return Capabilities{InitSystem: "direct", PkgMode: "green"}
}

// platformProc 兜底平台走直接托管。
func platformProc() Proc { return newDirectProc() }

// procAttr 子进程独立进程组。
func procAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// envKey 兜底平台按区分大小写比对。
func envKey(k string) string { return k }

// killTree 结束整棵进程树（负 PID 作用到进程组）。
func killTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	if err := p.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

// otherOpener 用系统自带的 open 命令打开目录/链接。
type otherOpener struct{}

// platformOpen 返回兜底平台打开器。
func platformOpen() Opener { return otherOpener{} }

// Dir 打开目录。
func (otherOpener) Dir(path string) error { return openTarget(path) }

// URL 打开链接。
func (otherOpener) URL(u string) error { return openTarget(u) }

// openTarget 调 open 并立即返回。
func openTarget(target string) error {
	if _, err := exec.LookPath("open"); err != nil {
		return fmt.Errorf("%w: 找不到 open 命令", ErrUnsupported)
	}
	cmd := exec.Command("open", target)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 open 失败: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
