//go:build windows

package sysx

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsOpener 用资源管理器/默认浏览器打开目标。
type windowsOpener struct{}

// platformOpen 返回 Windows 打开器。
func platformOpen() Opener { return windowsOpener{} }

var (
	modShell32Open   = windows.NewLazySystemDLL("shell32.dll")
	procShellExecOpe = modShell32Open.NewProc("ShellExecuteW")
)

// swShowNormal 正常窗口启动（不最大化也不最小化）。
const swShowNormal = 1

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

// Launch 走 ShellExecuteW("open")：这是 shell 的唯一入口，.lnk 会解析到目标程序，
// 文档会交给扩展名关联的默认应用。startDetached("notepad.exe") 那条路做不到这一点。
//
// 先 Find() 探一遍：LazyProc 找不到导出函数是 panic，不是 error。
func (windowsOpener) Launch(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if procShellExecOpe.Find() != nil {
		return fmt.Errorf("%w: shell32!ShellExecuteW 不可用", ErrUnsupported)
	}
	verb, err1 := windows.UTF16PtrFromString("open")
	if err1 != nil {
		return err1
	}
	fp, err1 := windows.UTF16PtrFromString(abs)
	if err1 != nil {
		return err1
	}
	r, _, _ := procShellExecOpe.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(fp)),
		0, 0,
		uintptr(swShowNormal))
	// ShellExecuteW 成功返回 >32 的伪 HINSTANCE，失败返回 SE_ERR_* 小整数。
	if int(r) <= 32 {
		return fmt.Errorf("启动 %s 失败（ShellExecuteW=%d）", filepath.Base(abs), int(r))
	}
	return nil
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
