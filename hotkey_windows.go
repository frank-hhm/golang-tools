//go:build windows

package main

import (
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// ============================================================
// 全局热键 —— 纯 Go 原生 Win32 实现
//
// 在专用 OS 线程上创建一个「消息-only 窗口」(HWND_MESSAGE)，用 RegisterHotKey
// 注册来自 config（工具项 Shortcut，如 "Ctrl+Alt+S"）的系统级全局热键；
// 命中 WM_HOTKEY 后经 App 以 "tool:activate" 事件交回主窗口，复用开窗/截图逻辑。
//
// 线程/消息安全（沿用本仓库约定）：
//   - 专用 goroutine + runtime.LockOSThread，退出时不 Unlock（线程随 goroutine 销毁）；
//   - 全程不 PostQuitMessage；
//   - 注册/注销只在热键线程内进行（hkApply 由该线程处理 WM_APP_RELOAD 时调用），
//     其它 goroutine 仅通过 PostMessage 请求重载。
//
// 复用 ball_windows.go 中的 Win32 过程与结构（同包）：ballNewClass/ballCreate/
// ballGetMsg/ballTrans/ballDispatch/ballDefProc/ballDestroy/ballPostMsg/ballModHdr、
// ballWndClass/ballMsg/ballUtf16。
// ============================================================

const (
	hkWM_HOTKEY     = 0x0312
	hkWM_CLOSE      = 0x0010
	hkWM_DESTROY    = 0x0002
	hkWM_APP_RELOAD = 0x8006 // WM_APP(0x8000) + 6

	hkMOD_ALT      = 0x0001
	hkMOD_CONTROL  = 0x0002
	hkMOD_SHIFT    = 0x0004
	hkMOD_WIN      = 0x0008
	hkMOD_NOREPEAT = 0x4000
)

var (
	hkRegProc   = ballUser32.NewProc("RegisterHotKey")
	hkUnregProc = ballUser32.NewProc("UnregisterHotKey")
)

var (
	hkMu        sync.Mutex
	hkApp       *App
	hkHwnd      uintptr
	hkStarted   bool
	hkClassOnce sync.Once
	hkClassName *uint16

	// hkIds 仅在热键线程读写：下标 i 对应热键 id=i+1，值为工具 ID
	hkIds []string
)

// startHotKeys 启动全局热键线程（幂等）；已启动则请求按最新配置重载。
func startHotKeys(a *App) {
	if a == nil {
		return
	}
	hkMu.Lock()
	if hkStarted {
		h := hkHwnd
		hkMu.Unlock()
		if h != 0 {
			ballPostMsg.Call(h, hkWM_APP_RELOAD, 0, 0)
		}
		return
	}
	hkApp = a
	hkStarted = true
	hkMu.Unlock()
	go hkThread()
}

// reloadHotKeys 请求热键线程按当前配置重新注册（从任意 goroutine 调用）。
func reloadHotKeys() {
	hkMu.Lock()
	h := hkHwnd
	hkMu.Unlock()
	if h != 0 {
		ballPostMsg.Call(h, hkWM_APP_RELOAD, 0, 0)
	}
}

// hkThread 在专用 OS 线程上建消息窗口并泵消息。
func hkThread() {
	runtime.LockOSThread()
	// 故意不 UnlockOSThread：线程随 goroutine 退出而销毁。

	hkClassOnce.Do(hkRegisterClass)
	if hkClassName == nil {
		errLogf("hotkey", "窗口类注册失败，热键不可用")
		return
	}

	hInst, _, _ := ballModHdr.Call(0)
	hwnd, _, _ := ballCreate.Call(
		0,
		uintptr(unsafe.Pointer(hkClassName)),
		0,
		0,
		0, 0, 0, 0,
		uintptr(^uintptr(2)), // HWND_MESSAGE = (HWND)-3：消息-only 窗口
		0, hInst, 0,
	)
	if hwnd == 0 {
		errLogf("hotkey", "创建消息窗口失败")
		return
	}
	hkMu.Lock()
	hkHwnd = hwnd
	hkMu.Unlock()

	hkApply() // 初次注册

	var msg ballMsg
	for {
		ret, _, _ := ballGetMsg.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(ret) == 0 {
			break
		}
		ballTrans.Call(uintptr(unsafe.Pointer(&msg)))
		ballDispatch.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

func hkRegisterClass() {
	hkClassName = ballUtf16("GoToolsHotkeyMsgCls")
	hInst, _, _ := ballModHdr.Call(0)
	wc := ballWndClass{
		LpfnWndProc:   syscall.NewCallback(hkWndProc),
		HInstance:     hInst,
		LpszClassName: hkClassName,
	}
	atom, _, err := ballNewClass.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		errLogf("hotkey", "RegisterClassW 失败: %v", err)
		hkClassName = nil
	}
}

// hkWndProc 仅在热键线程被调用。
func hkWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case hkWM_APP_RELOAD:
		hkApply()
		return 0

	case hkWM_HOTKEY:
		id := int(wParam)
		var toolID string
		if id-1 >= 0 && id-1 < len(hkIds) {
			toolID = hkIds[id-1]
		}
		hkMu.Lock()
		app := hkApp
		hkMu.Unlock()
		if toolID != "" && app != nil && app.app != nil {
			opLogf("hotkey", "全局热键触发 -> %s", toolID)
			app.app.Event.Emit("tool:activate", toolID)
		}
		return 0

	case hkWM_CLOSE:
		ballDestroy.Call(hwnd)
		return 0

	case hkWM_DESTROY:
		return 0
	}
	r, _, _ := ballDefProc.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

// hkApply 注销全部旧热键并按当前配置重新注册（仅在热键线程调用）。
func hkApply() {
	hkMu.Lock()
	hwnd := hkHwnd
	hkMu.Unlock()
	if hwnd == 0 {
		return
	}
	for i := range hkIds {
		hkUnregProc.Call(hwnd, uintptr(i+1))
	}
	hkIds = nil

	for _, t := range CurrentConfig().Tools {
		sc := strings.TrimSpace(t.Shortcut)
		if sc == "" {
			continue
		}
		mods, vk, ok := parseShortcut(sc)
		if !ok {
			warnLogf("hotkey", "无法解析热键 %q（工具 %s）", sc, t.ID)
			continue
		}
		id := uintptr(len(hkIds) + 1)
		r, _, _ := hkRegProc.Call(hwnd, id, uintptr(mods|hkMOD_NOREPEAT), uintptr(vk))
		if r == 0 {
			warnLogf("hotkey", "注册热键失败 %q（工具 %s），可能与其他应用冲突", sc, t.ID)
			continue
		}
		hkIds = append(hkIds, t.ID)
	}
	opLogf("hotkey", "已注册 %d 个全局热键", len(hkIds))
}

// parseShortcut 解析 "Ctrl+Alt+S" 形式为 (修饰键, 虚拟键码, 是否有效)。
func parseShortcut(s string) (mods uint32, vk uint32, ok bool) {
	parts := strings.Split(s, "+")
	for _, p := range parts {
		k := strings.ToLower(strings.TrimSpace(p))
		if k == "" {
			continue
		}
		switch k {
		case "ctrl", "control":
			mods |= hkMOD_CONTROL
		case "alt", "option":
			mods |= hkMOD_ALT
		case "shift":
			mods |= hkMOD_SHIFT
		case "win", "cmd", "meta", "super":
			mods |= hkMOD_WIN
		default:
			v := nameToVK(k)
			if v == 0 {
				return 0, 0, false
			}
			if vk != 0 { // 只允许一个主键
				return 0, 0, false
			}
			vk = v
		}
	}
	if vk == 0 {
		return 0, 0, false
	}
	return mods, vk, true
}

// nameToVK 将键名映射为虚拟键码（不支持返回 0）。
func nameToVK(k string) uint32 {
	if len(k) == 1 {
		c := k[0]
		if c >= 'a' && c <= 'z' {
			return uint32(c-'a') + 0x41 // VK_A..VK_Z
		}
		if c >= '0' && c <= '9' {
			return uint32(c) // VK_0..VK_9 与 ASCII 相同
		}
	}
	switch k {
	case "f1":
		return 0x70
	case "f2":
		return 0x71
	case "f3":
		return 0x72
	case "f4":
		return 0x73
	case "f5":
		return 0x74
	case "f6":
		return 0x75
	case "f7":
		return 0x76
	case "f8":
		return 0x77
	case "f9":
		return 0x78
	case "f10":
		return 0x79
	case "f11":
		return 0x7A
	case "f12":
		return 0x7B
	case "space":
		return 0x20
	case "enter", "return":
		return 0x0D
	case "tab":
		return 0x09
	case "esc", "escape":
		return 0x1B
	case "left":
		return 0x25
	case "up":
		return 0x26
	case "right":
		return 0x27
	case "down":
		return 0x28
	case "home":
		return 0x24
	case "end":
		return 0x23
	case "pageup", "prior":
		return 0x21
	case "pagedown", "next":
		return 0x22
	case "insert":
		return 0x2D
	case "delete", "del":
		return 0x2E
	}
	return 0
}
