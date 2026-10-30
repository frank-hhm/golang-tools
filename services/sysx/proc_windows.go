//go:build windows

package sysx

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// createNoWindow = Win32 CREATE_NO_WINDOW：GUI 子系统（-H windowsgui）构建里，
// HideWindow(SW_HIDE) 藏不住 Win11 的 Windows Terminal（它是独立进程、不吃子进程
// 的 STARTUPINFO 显示标志），只有根本不分配控制台才不闪窗。
const createNoWindow = 0x08000000

// procAttr 子进程不弹黑窗：服务类进程（nginx/mysql/redis…）都是无控制台的，
// 一旦漏了这行，面板每起一个服务就会闪一个 cmd 窗口。
func procAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

// envKey Windows 环境变量名不区分大小写（Path 与 PATH 是同一个），统一转大写比对。
func envKey(k string) string { return strings.ToUpper(k) }

// killTree 结束整棵进程树。
//
// nginx/mysql 这类会派生 worker 子进程，只杀根进程会留下孤儿进程继续占端口。
// 优先用 taskkill /T（按进程树杀），失败再退回单进程 Kill。
func killTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	kill := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(p.Pid))
	kill.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if err := kill.Run(); err == nil {
		return nil
	}
	if err := p.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
