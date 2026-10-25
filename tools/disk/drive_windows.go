//go:build windows

package disk

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

/*
Windows 专属能力：

  - 盘符枚举与容量：GetLogicalDrives / GetDriveTypeW / GetVolumeInformationW /
    GetDiskFreeSpaceExW，比 os.Stat 准确（os.Stat 在 Windows 上拿不到卷容量）。
  - 删除进回收站：SHFileOperationW(FO_DELETE | FOF_ALLOWUNDO)，和资源管理器删文件
    行为一致，误删还能捞回来。
  - 打开资源管理器 / 命令行 / 默认程序：ShellExecuteW + explorer.exe。

这些调用都要正确的 UTF-16 缓冲与结构体布局，写错了不会报错只会拿不到值，
所以每个 proc 都做一次 Call 失败兜底（拿不到就当未知，不让整页崩掉）。
*/

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	ole32    = syscall.NewLazyDLL("ole32.dll")

	procGetLogicalDrives      = kernel32.NewProc("GetLogicalDrives")
	procGetDriveTypeW         = kernel32.NewProc("GetDriveTypeW")
	procGetVolumeInformationW = kernel32.NewProc("GetVolumeInformationW")
	procGetDiskFreeSpaceExW   = kernel32.NewProc("GetDiskFreeSpaceExW")
	procSHFileOperationW      = shell32.NewProc("SHFileOperationW")
	procShellExecuteW         = shell32.NewProc("ShellExecuteW")
	procCoInitializeEx        = ole32.NewProc("CoInitializeEx")
	procCoUninitialize        = ole32.NewProc("CoUninitialize")
)

const (
	driveUnknown   = 0
	driveNoRootDir = 1
	driveRemovable = 2
	driveFixed     = 3
	driveRemote    = 4
	driveCDROM     = 5
	driveRAMDisk   = 6
)

// driveKindText 盘符类型 → 前端展示用的短名。
func driveKindText(t uintptr) string {
	switch t {
	case driveRemovable:
		return "removable"
	case driveFixed:
		return "fixed"
	case driveRemote:
		return "network"
	case driveCDROM:
		return "cdrom"
	case driveRAMDisk:
		return "ram"
	default:
		return "unknown"
	}
}

// queryDrive 取一个盘符的卷标、文件系统与容量。
func queryDrive(root string) Drive {
	d := Drive{Root: root, Kind: "unknown"}
	p, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return d
	}
	if t, _, _ := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(p))); true {
		d.Kind = driveKindText(t)
	}

	// 卷标 + 文件系统：光驱空盘、没插卡这类会返回 0，属于正常情况
	var label [256]uint16
	var fsName [64]uint16
	var serial, maxComp, fsFlags uint32
	r, _, _ := procGetVolumeInformationW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&label[0])), uintptr(len(label)),
		uintptr(unsafe.Pointer(&serial)),
		uintptr(unsafe.Pointer(&maxComp)),
		uintptr(unsafe.Pointer(&fsFlags)),
		uintptr(unsafe.Pointer(&fsName[0])), uintptr(len(fsName)),
	)
	if r != 0 {
		d.Label = syscall.UTF16ToString(label[:])
		d.FSType = syscall.UTF16ToString(fsName[:])
	}

	var freeAvail, total, totalFree uint64
	r, _, _ = procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(p)),
		uintptr(unsafe.Pointer(&freeAvail)),
		uintptr(unsafe.Pointer(&total)),
		uintptr(unsafe.Pointer(&totalFree)),
	)
	if r != 0 {
		d.Total = total
		d.Free = totalFree
		if total >= totalFree {
			d.Used = total - totalFree
		}
		if total > 0 {
			d.UsedPct = float64(d.Used) / float64(total) * 100
		}
	}
	return d
}

// Drives 枚举本机所有盘符（跳过没有根目录的项）。
func Drives() []Drive {
	mask, _, _ := procGetLogicalDrives.Call()
	out := make([]Drive, 0, 8)
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		// 软驱位之类没有介质的盘符直接跳过，免得前端列出一堆 0 字节盘
		p, err := syscall.UTF16PtrFromString(root)
		if err != nil {
			continue
		}
		if t, _, _ := procGetDriveTypeW.Call(uintptr(unsafe.Pointer(p))); t == driveNoRootDir {
			continue
		}
		out = append(out, queryDrive(root))
	}
	return out
}

// DriveOf 返回某个路径所在的盘符信息；取不到容量时 ok=false。
func DriveOf(path string) (Drive, bool) {
	vol := filepath.VolumeName(path)
	if vol == "" {
		return Drive{}, false
	}
	d := queryDrive(vol + string(filepath.Separator))
	if d.Total == 0 {
		return d, false
	}
	return d, true
}

// shFileOpStruct 对应 Win32 SHFILEOPSTRUCTW（x64 布局，字段间按自然对齐补位）。
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	_pad0                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	_pad1                 uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

const (
	foDelete           = 0x0003
	fofSilent          = 0x0004
	fofNoConfirmation  = 0x0010
	fofAllowUndo       = 0x0040
	fofNoErrorUI       = 0x0400
	coinitApartmentThr = 0x2
)

// doubleNull 把路径列表拼成 SHFileOperationW 要的双 \0 结尾格式。
func doubleNull(paths []string) (*uint16, error) {
	var b strings.Builder
	for _, p := range paths {
		b.WriteString(p)
		b.WriteByte(0)
	}
	b.WriteByte(0)
	return syscall.UTF16PtrFromString(b.String())
}

// RecycleBin 把一批路径删到 Windows 回收站，返回处理条数。
//
// SHFileOperationW 要求调用线程初始化 COM，这里临时锁一条线程做 STA，
// 用完就解绑，避免把整个 goroutine 钉在 STA 上。
func RecycleBin(paths []string) (int, error) {
	clean := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return 0, err
		}
		if _, err := os.Stat(abs); err != nil {
			return 0, err
		}
		clean = append(clean, abs)
	}
	if len(clean) == 0 {
		return 0, nil
	}

	from, err := doubleNull(clean)
	if err != nil {
		return 0, err
	}

	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// S_FALSE(1) 表示这条线程已经初始化过 COM，同样要配对 CoUninitialize
	hr, _, _ := procCoInitializeEx.Call(0, uintptr(coinitApartmentThr))
	if int32(hr) >= 0 {
		defer procCoUninitialize.Call()
	}

	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  from,
		fFlags: fofAllowUndo | fofNoConfirmation | fofNoErrorUI | fofSilent,
	}
	r, _, callErr := procSHFileOperationW.Call(uintptr(unsafe.Pointer(&op)))
	if r != 0 {
		return 0, fmt.Errorf("删除到回收站失败（代码 %d）：%v", r, callErr)
	}
	if op.fAnyOperationsAborted != 0 {
		return 0, fmt.Errorf("操作被取消")
	}
	return len(clean), nil
}

// shellExecute 走 ShellExecuteW，verb 传 "open"。
func shellExecute(target, params string) error {
	verb, err := syscall.UTF16PtrFromString("open")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(target)
	if err != nil {
		return err
	}
	var paramPtr *uint16
	if params != "" {
		if paramPtr, err = syscall.UTF16PtrFromString(params); err != nil {
			return err
		}
	}
	r, _, _ := procShellExecuteW.Call(0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		uintptr(unsafe.Pointer(paramPtr)),
		0, 1, // SW_SHOWNORMAL
	)
	if r <= 32 {
		return fmt.Errorf("ShellExecute 失败（代码 %d）", r)
	}
	return nil
}

// RevealInExplorer 在资源管理器里打开目录；传文件时打开所在目录并选中它。
func RevealInExplorer(path string) error {
	p := strings.TrimSpace(path)
	if p == "" {
		return fmt.Errorf("路径为空")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return err
	}
	if st, err := os.Stat(abs); err == nil && st.IsDir() {
		return shellExecute("explorer.exe", `"`+abs+`"`)
	}
	return shellExecute("explorer.exe", `/select,"`+abs+`"`)
}

// OpenShell 在指定目录打开命令行：优先 Windows Terminal，没有就退回 cmd。
func OpenShell(path string) error {
	dir := strings.TrimSpace(path)
	if dir == "" {
		return fmt.Errorf("路径为空")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	if st, serr := os.Stat(abs); serr == nil && !st.IsDir() {
		abs = filepath.Dir(abs)
	}
	if _, err := exec.LookPath("wt.exe"); err == nil {
		cmd := exec.Command("wt.exe", "-d", abs)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
		if err := cmd.Start(); err == nil {
			return nil
		}
	}
	cmd := exec.Command("cmd.exe", "/k", "cd /d "+abs)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd.Start()
}

// OpenTrash 打开系统回收站。
func OpenTrash() error {
	return shellExecute("explorer.exe", "shell:RecycleBinFolder")
}

// OpenFile 用系统默认程序打开文件；目录交给资源管理器。
func OpenFile(path string) error {
	p := strings.TrimSpace(path)
	if p == "" {
		return fmt.Errorf("路径为空")
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return err
	}
	if st, serr := os.Stat(abs); serr == nil && st.IsDir() {
		return shellExecute("explorer.exe", `"`+abs+`"`)
	}
	return shellExecute(abs, "")
}

// Home 默认浏览起点：用户主目录（拿不到就用系统盘根）。
func Home() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	if d, err := os.UserConfigDir(); err == nil && d != "" {
		return d
	}
	return `C:\`
}
