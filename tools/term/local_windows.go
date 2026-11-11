//go:build windows

package term

// ============================================================
// Windows 本地 shell：ConPTY（伪控制台）
//
// 手写 kernel32 伪控制台胶水（无第三方依赖，离线可用）：
//   CreatePseudoConsole -> 带 PSEUDOCONPTY 属性列表的 CreateProcessW 起子进程。
// 输入走 hInput 管道写端，输出从 hOutput 管道读端读出——ConPTY 在管道侧统一
// 按 UTF-8 编码，故 xterm 直接渲染中文不乱码，无需 chcp。
// 会话结构复用 ssh.go 的 session / emitWriter / closeSession / term:data 通道。
// ============================================================

import (
	"encoding/base64"
	"fmt"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procCreatePseudoConsole = kernel32.NewProc("CreatePseudoConsole")
	procResizePseudoConsole = kernel32.NewProc("ResizePseudoConsole")
	procClosePseudoConsole  = kernel32.NewProc("ClosePseudoConsole")
	procInitAttrList        = kernel32.NewProc("InitializeProcThreadAttributeList")
	procUpdateAttrList      = kernel32.NewProc("UpdateProcThreadAttribute")
	procDeleteAttrList      = kernel32.NewProc("DeleteProcThreadAttributeList")
	procCreateProcessW      = kernel32.NewProc("CreateProcessW")
)

const (
	extStartupInfoPresent = 0x00080000
	attrPseudoConsole     = 0x00020016
	startfUseStdHandles   = 0x00000100
)

// 自带结构体，避免依赖 x/sys/windows 对 STARTUPINFOEX 的具体类型命名。
type startupInfoW struct {
	cb              uint32
	lpReserved      *uint16
	lpDesktop       *uint16
	lpTitle         *uint16
	dwX             uint32
	dwY             uint32
	dwXSize         uint32
	dwYSize         uint32
	dwXCountChars   uint32
	dwYCountChars   uint32
	dwFillAttribute uint32
	dwFlags         uint32
	wShowWindow     uint16
	cbReserved2     uint16
	lpReserved2     *byte
	hStdInput       windows.Handle
	hStdOutput      windows.Handle
	hStdError       windows.Handle
}

type startupInfoExW struct {
	si            startupInfoW
	attributeList uintptr
}

type processInformation struct {
	hProcess    windows.Handle
	hThread     windows.Handle
	dwProcessId uint32
	dwThreadId  uint32
}

// ptySize 把 cols/rows 打包成 COORD（低位 X=列，高位 Y=行）。
func ptySize(cols, rows int) uintptr {
	return uintptr(uint32(cols&0xffff) | uint32(rows&0xffff)<<16)
}

// startLocalShell 用 ConPTY 起子进程并注册为一条终端会话（尚未放进 sessions 表）。
func startLocalShell(id, title, exe string, args []string, cwd string, cols, rows int) (*session, error) {
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("创建输入管道失败: %w", err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		return nil, fmt.Errorf("创建输出管道失败: %w", err)
	}

	var hPty uintptr
	if hr, _, _ := procCreatePseudoConsole.Call(ptySize(cols, rows), uintptr(inRead), uintptr(outWrite), 0, uintptr(unsafe.Pointer(&hPty))); hr != 0 {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		windows.CloseHandle(outWrite)
		return nil, fmt.Errorf("CreatePseudoConsole 失败(hr=0x%x，可能缺少 UTF-8/伪控制台支持)", hr)
	}
	// 伪控制台已接管 inRead/outWrite，关掉本进程这端避免句柄泄漏
	windows.CloseHandle(inRead)
	windows.CloseHandle(outWrite)

	// 属性列表：先探大小，再初始化并挂 PSEUDOCONPTY 属性
	var listSize uintptr
	procInitAttrList.Call(0, 1, 0, uintptr(unsafe.Pointer(&listSize)))
	if listSize == 0 {
		listSize = 64
	}
	attrList := make([]byte, listSize)
	cleanupFail := func() {
		procDeleteAttrList.Call(uintptr(unsafe.Pointer(&attrList[0])))
		procClosePseudoConsole.Call(hPty)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
	}
	if ok, _, _ := procInitAttrList.Call(uintptr(unsafe.Pointer(&attrList[0])), 1, 0, uintptr(unsafe.Pointer(&listSize))); ok == 0 {
		procClosePseudoConsole.Call(hPty)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return nil, fmt.Errorf("初始化线程属性列表失败")
	}
	if ok, _, _ := procUpdateAttrList.Call(uintptr(unsafe.Pointer(&attrList[0])), 0, uintptr(attrPseudoConsole), hPty, unsafe.Sizeof(hPty), 0, 0); ok == 0 {
		procDeleteAttrList.Call(uintptr(unsafe.Pointer(&attrList[0])))
		procClosePseudoConsole.Call(hPty)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return nil, fmt.Errorf("绑定伪控制台属性失败")
	}

	// 命令行：exe 路径加引号，参数原样追加
	cmdLine := `"` + exe + `"`
	for _, a := range args {
		cmdLine += " " + a
	}
	cmdLineZ, err := windows.UTF16PtrFromString(cmdLine)
	if err != nil {
		cleanupFail()
		return nil, fmt.Errorf("命令行编码失败: %w", err)
	}
	var cwdZ *uint16
	if cwd != "" {
		cwdZ, err = windows.UTF16PtrFromString(cwd)
		if err != nil {
			cleanupFail()
			return nil, fmt.Errorf("工作目录编码失败: %w", err)
		}
	}

	siEx := startupInfoExW{}
	siEx.si.cb = uint32(unsafe.Sizeof(siEx))
	siEx.attributeList = uintptr(unsafe.Pointer(&attrList[0]))
	// std 句柄必须显式声明（值为 NULL），否则子进程会沿用本进程那三个：
	// 本进程一旦被带重定向 stdin 的方式拉起（IDE / wails dev / 脚本传管道），
	// cmd 与 PowerShell 就会从那根句柄读命令，ConPTY 写进来的按键全被丢弃，
	// 现象即「banner 与提示符正常、敲任何键都没反应」。留 NULL 时 shell 会退回
	// 读自己的控制台（就是这根伪控制台），输入输出都走 ConPTY。
	siEx.si.dwFlags = startfUseStdHandles
	var pi processInformation
	r1, _, e1 := procCreateProcessW.Call(
		0,
		uintptr(unsafe.Pointer(cmdLineZ)),
		0, 0,
		0, // bInheritHandles = FALSE
		uintptr(extStartupInfoPresent),
		0,
		uintptr(unsafe.Pointer(cwdZ)),
		uintptr(unsafe.Pointer(&siEx)),
		uintptr(unsafe.Pointer(&pi)),
	)
	// 属性列表在 CreateProcess 后即可释放
	procDeleteAttrList.Call(uintptr(unsafe.Pointer(&attrList[0])))
	if r1 == 0 {
		procClosePseudoConsole.Call(hPty)
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return nil, fmt.Errorf("启动 %s 失败: %v", exe, e1)
	}

	// 输出侧用 os.File 包住管道读端做阻塞读（Read 在同步句柄上工作正常，
	// banner 即由此读出）；输入侧不经 os.File，直接对写端调同步 WriteFile，
	// 少一层运行时句柄模式假设，写入语义一眼可查。
	outFile := os.NewFile(uintptr(outRead), "conout")

	s := &session{
		info:   SessionInfo{ID: id, Title: title, Kind: "local"},
		write:  conptyInput{h: inWrite},
		resize: func(c, r int) error { return resizePty(hPty, c, r) },
	}
	s.shut = func() {
		procClosePseudoConsole.Call(hPty)
		if pi.hProcess != 0 {
			_ = windows.TerminateProcess(pi.hProcess, 0)
			_ = windows.CloseHandle(pi.hThread)
			_ = windows.CloseHandle(pi.hProcess)
		}
		_ = windows.CloseHandle(inWrite)
		_ = outFile.Close()
	}

	// 输出泵：ConPTY 读端出块即转 term:data；EOF/出错（进程退出）时收会话
	go func() {
		buf := make([]byte, 8192)
		for {
			n, rerr := outFile.Read(buf)
			if n > 0 {
				emit("term:data", DataEvent{SessionID: id, Data: base64.StdEncoding.EncodeToString(buf[:n])})
			}
			if rerr != nil {
				closeSession(id, "本地终端已退出")
				return
			}
		}
	}()

	return s, nil
}

func resizePty(hPty uintptr, cols, rows int) error {
	cols, rows = clampSize(cols, rows)
	if hr, _, _ := procResizePseudoConsole.Call(hPty, ptySize(cols, rows)); hr != 0 {
		return fmt.Errorf("ResizePseudoConsole 失败(hr=0x%x)", hr)
	}
	return nil
}

// conptyInput 把前端按键用同步 WriteFile 写进 ConPTY 输入管道写端。
type conptyInput struct{ h windows.Handle }

func (w conptyInput) Write(p []byte) (int, error) {
	var written uint32
	if err := windows.WriteFile(w.h, p, &written, nil); err != nil {
		return int(written), err
	}
	return int(written), nil
}
