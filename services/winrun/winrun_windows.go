//go:build windows

package winrun

// ============================================================
// 提权执行公共 helper（UAC）
//
// 由 tools/npm（写系统 PATH）与 tools/docker（安装 Docker Desktop / 启用 WSL2）
// 共用，避免各处重复实现 SHELLEXECUTEINFOW。
//
// x/sys v0.46 未导出 SHELLEXECUTEINFOW，这里按 Win32 内存布局自定义；
// cbSize 必须与结构体大小一致，否则 ShellExecuteExW 直接失败。
// ============================================================

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	seeMaskNoCloseProcess = 0x00000040 // 拿得到进程句柄，才能等待与取退出码
	errUACCancelled       = 1223       // ERROR_CANCELLED：用户在 UAC 弹窗点了拒绝
	defaultWaitTimeout    = 2 * time.Minute
)

// shellexecinfo 对应 Win32 SHELLEXECUTEINFOW（hProc 与 hIcon 是联合体）
type shellexecinfo struct {
	cbSize     uint32
	fMask      uint32
	hwnd       windows.Handle
	verb       *uint16
	file       *uint16
	parameters *uint16
	directory  *uint16
	showCmd    int32
	hInstApp   windows.Handle
	lpIDList   uintptr
	lpClass    *uint16
	hkeyClass  windows.Handle
	dwHotKey   uint32
	hProc      windows.Handle
	hMonitor   windows.Handle
}

var (
	shell32             = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteExW = shell32.NewProc("ShellExecuteExW")
)

// Elevated 一次提权执行的入参
type Elevated struct {
	File      string        // 要提权运行的程序（或文档）
	Params    string        // 命令行参数原样串
	Directory string        // 工作目录，空则默认
	Show      int32         // windows.SW_HIDE / SW_SHOW / SW_SHOWNORMAL
	Timeout   time.Duration // 0 = 2 分钟；安装类长任务需显式放大
}

// RunElevatedWait 以 runas 动词提权启动并等待结束，返回退出码。
// 用户拒绝 UAC 返回的错误可识别（IsUACCancelled），调用方据此区别于真失败。
func RunElevatedWait(e Elevated) (int, error) {
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return -1, err
	}
	fp, err := windows.UTF16PtrFromString(e.File)
	if err != nil {
		return -1, err
	}
	pp, err := windows.UTF16PtrFromString(e.Params)
	if err != nil {
		return -1, err
	}
	var dp *uint16
	if e.Directory != "" {
		p, err := windows.UTF16PtrFromString(e.Directory)
		if err != nil {
			return -1, err
		}
		dp = p
	}
	show := e.Show
	if show == 0 {
		show = windows.SW_HIDE
	}
	timeout := e.Timeout
	if timeout <= 0 {
		timeout = defaultWaitTimeout
	}

	sei := shellexecinfo{}
	sei.cbSize = uint32(unsafe.Sizeof(sei))
	sei.fMask = seeMaskNoCloseProcess
	sei.verb = verb
	sei.file = fp
	sei.parameters = pp
	sei.directory = dp
	sei.showCmd = int32(show)

	r1, _, e1 := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	if r1 == 0 {
		if errno, ok := e1.(syscall.Errno); ok {
			if int(errno) == errUACCancelled {
				return -1, &UACCancelledError{}
			}
			if errno != 0 {
				return -1, fmt.Errorf("提权启动失败: %w", errno)
			}
		}
		return -1, fmt.Errorf("提权启动失败")
	}
	h := sei.hProc
	if h == 0 {
		// 拿不到句柄（如被 Shell 拦截交给别的进程）：视为已启动但无法判定结果
		return 0, nil
	}
	defer windows.CloseHandle(h)

	ms := uint32(timeout / time.Millisecond)
	if ms == 0 {
		ms = 1
	}
	ev, err := windows.WaitForSingleObject(h, ms)
	if err != nil {
		return -1, fmt.Errorf("等待提权进程失败: %w", err)
	}
	if ev != windows.WAIT_OBJECT_0 {
		return -1, fmt.Errorf("提权进程超时未结束（%v）", timeout)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return -1, fmt.Errorf("读取退出码失败: %w", err)
	}
	return int(code), nil
}

// UACCancelledError 用户取消管理员授权
type UACCancelledError struct{}

func (UACCancelledError) Error() string {
	return "已取消管理员授权（UAC），未做任何更改"
}

// IsUACCancelled 判断错误是否源自用户拒绝提权
func IsUACCancelled(err error) bool {
	_, ok := err.(*UACCancelledError)
	return ok
}

// createNoWindow = Win32 CREATE_NO_WINDOW：GUI 子系统构建里 HideWindow(SW_HIDE)
// 藏不住 Win11 的 Windows Terminal，只有不分配控制台才不闪窗。
const createNoWindow = 0x08000000

// StartDetached 以后台方式启动一个程序（不提权、不等结束、不抢焦点窗口）。
// 静默安装 Docker Desktop 后需要以普通用户身份拉起它，提权进程派生的话
// 会变成管理员权限运行，容器与卷的归属都不对，故这里显式走非提权路径。
func StartDetached(exe string, args ...string) error {
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 %s 失败: %w", exe, err)
	}
	// 不等结束：交付给系统回收，避免僵尸进程
	go func() { _ = cmd.Wait() }()
	return nil
}

// OpenElevated 以管理员权限（runas）启动一个程序并立即返回，不等待其结束。
// 用于“提权打开记事本编辑 hosts”这类交给用户、不能阻塞调用方的场景。
// 与 RunElevatedWait 的区别：不带 SEE_MASK_NOCLOSEPROCESS，ShellExecuteExW 在启动
// 后立即返回；用户在 UAC 弹窗点「否」仍会同步拿到 ERROR_CANCELLED，因此能区分“已打开”与“被拒”。
func OpenElevated(file, params string) error {
	verb, err := windows.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	fp, err := windows.UTF16PtrFromString(file)
	if err != nil {
		return err
	}
	pp, err := windows.UTF16PtrFromString(params)
	if err != nil {
		return err
	}
	sei := shellexecinfo{}
	sei.cbSize = uint32(unsafe.Sizeof(sei))
	sei.verb = verb
	sei.file = fp
	sei.parameters = pp
	sei.showCmd = windows.SW_SHOWNORMAL
	r1, _, e1 := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&sei)))
	if r1 == 0 {
		if errno, ok := e1.(syscall.Errno); ok {
			if int(errno) == errUACCancelled {
				return &UACCancelledError{}
			}
			if errno != 0 {
				return fmt.Errorf("提权启动 %s 失败: %w", filepath.Base(file), errno)
			}
		}
		return fmt.Errorf("提权启动 %s 失败", filepath.Base(file))
	}
	return nil
}

// RunHidden 以普通权限运行命令并取回输出（用于只读体检，如 wsl --status）。
// 超时返回错误；命令非零退出也返回错误，但输出照样交给调用方（状态判定要看原文）。
func RunHidden(timeout time.Duration, name string, args ...string) (string, error) {
	return RunHiddenEnv(timeout, nil, name, args...)
}

// RunHiddenEnv 同 RunHidden，但可额外注入环境变量（nil = 只继承当前进程）。
// 加这个入口主要为了 wsl.exe：中文系统下它按 UTF-16 输出中文，而 UTF-16 的汉字
// 字节里没 NUL，下面的密度启发式根本看不出来，判词表就会空转；
// 设 WSL_UTF8=1 才能稳定拿到 UTF-8。
func RunHiddenEnv(timeout time.Duration, extraEnv []string, name string, args ...string) (string, error) {
	if timeout <= 0 {
		timeout = defaultWaitTimeout
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	out, err := cmd.Output()
	text := decodeMaybeUTF16(string(out))
	if ctx.Err() == context.DeadlineExceeded {
		return text, fmt.Errorf("命令超时未返回: %s %v", name, args)
	}
	return text, err
}

// decodeMaybeUTF16 把可能是 UTF-16LE 的控制台输出转成 UTF-8。
// wsl.exe 在部分 Windows 版本用 UTF-16 输出（含大量 NUL），直接当字符串比较会全花。
// 只按 NUL 密度判：汉字在 UTF-16 里没 NUL，所以中文占主的输出认不出来，
// 调用方应该靠 WSL_UTF8=1；这里另外接一下 BOM 的情况。
func decodeMaybeUTF16(s string) string {
	if len(s) < 4 {
		return s
	}
	if s[0] == 0xff && s[1] == 0xfe { // UTF-16LE BOM
		return utf16LEToString(s[2:])
	}
	nuls := 0
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			nuls++
		}
	}
	if nuls*2 < len(s)/2 { // NUL 占比不明显即视为 UTF-8
		return s
	}
	return utf16LEToString(s)
}

// utf16LEToString 按小端两字节一组转 UTF-8
func utf16LEToString(s string) string {
	u := make([]uint16, 0, len(s)/2)
	for i := 0; i+1 < len(s); i += 2 {
		u = append(u, uint16(s[i])|uint16(s[i+1])<<8)
	}
	return windows.UTF16ToString(u)
}
