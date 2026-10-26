//go:build windows

package winui

import (
	"runtime"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"go-tools/services/applog"
)

// ============================================================
// 全局热键 —— 纯 Go 原生 Win32 实现
//
// 在专用 OS 线程上创建一个「消息-only 窗口」(HWND_MESSAGE)，用 RegisterHotKey
// 注册来自宿主（Host.Hotkeys，形如 "Ctrl+Alt+S"）的系统级全局热键；命中 WM_HOTKEY 后：
// 统一交回 Host.DispatchTool，由主程序决定 Go 侧直接调进还是以 "tool:activate" 事件回主窗口。
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
	hkHost      Host
	hkHwnd      uintptr
	hkStarted   bool
	hkClassOnce sync.Once
	hkClassName *uint16

	// hkIds 仅在热键线程读写：下标 i 对应热键 id=i+1，值为工具 ID
	hkIds []string

	// 兜底热键：SetGuardHotkeys 从任意 goroutine 写入 guardList，热键线程在
	// 重载时把它注册成 id=hkGuardBase+i，并拷一份到只在热键线程读写的 guardIDs
	guardMu   sync.Mutex
	guardList []GuardHotkey
	guardIDs  []GuardHotkey
)

// hkGuardBase 兜底热键的 id 起点：与工具热键（从 1 开始递增）隔开足够远，
// 两边各自增删都不会撞号。
const hkGuardBase = 100

// StartHotkeys 启动全局热键线程（幂等）；已启动则请求按当前配置重载。
func StartHotkeys(h Host) {
	if h == nil {
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
	hkHost = h
	hkStarted = true
	hkMu.Unlock()
	go hkThread()
}

// SetGuardHotkeys 用给定清单替换「临时兜底热键」（从任意 goroutine 调用，异步生效）。
// 传 nil 即全部注销。截图浮层点亮前登记、收场时注销：这类热键必须随浮层进出，
// 常驻注册会把 ESC 从其它应用手里抢走。
//
// 清单没变就不触发重载：重载要把配置里的工具热键全部注销再注册，每截一图来一遍
// 既没必要，又给了「重新注册瞬时失败」丢热键的机会。
func SetGuardHotkeys(list []GuardHotkey) {
	guardMu.Lock()
	same := len(list) == len(guardList)
	for i := 0; same && i < len(list); i++ {
		if list[i].ID != guardList[i].ID || list[i].Shortcut != guardList[i].Shortcut {
			same = false
		}
	}
	guardList = list
	guardMu.Unlock()
	if same {
		return
	}
	requestHotkeyReload()
}

// ClearGuardHotkeys 注销全部兜底热键。
func ClearGuardHotkeys() { SetGuardHotkeys(nil) }

// requestHotkeyReload 请求热键线程按当前配置 + 兜底清单重载（任一如仍未启动则忽略）。
func requestHotkeyReload() {
	hkMu.Lock()
	h := hkHwnd
	hkMu.Unlock()
	if h != 0 {
		ballPostMsg.Call(h, hkWM_APP_RELOAD, 0, 0)
	}
}

// ReloadHotkeys 请求热键线程按当前配置重新注册（从任意 goroutine 调用）。
func ReloadHotkeys() {
	requestHotkeyReload()
}

// hkThread 在专用 OS 线程上建消息窗口并泵消息。
func hkThread() {
	runtime.LockOSThread()
	// 故意不 UnlockOSThread：线程随 goroutine 退出而销毁。

	hkClassOnce.Do(hkRegisterClass)
	if hkClassName == nil {
		applog.Errorf("hotkey", "窗口类注册失败，热键不可用")
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
		applog.Errorf("hotkey", "创建消息窗口失败")
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
		applog.Errorf("hotkey", "RegisterClassW 失败: %v", err)
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
		// 兜底热键：回调一刀切进独立 goroutine，热键线程立刻回去泵消息
		if id >= hkGuardBase && id-hkGuardBase < len(guardIDs) {
			g := guardIDs[id-hkGuardBase]
			if g.Fn != nil {
				applog.Infof("hotkey", "兜底热键触发 %s -> %s", g.Shortcut, g.ID)
				go g.Fn()
			}
			return 0
		}
		var toolID string
		if id-1 >= 0 && id-1 < len(hkIds) {
			toolID = hkIds[id-1]
		}
		if toolID != "" && hkHost != nil {
			applog.Infof("hotkey", "全局热键触发 -> %s", toolID)
			// 截图等 Go 侧能直接处理的工具由主程序就地调起，其余发事件回主窗口
			hkHost.DispatchTool(toolID)
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

// hkApply 注销全部旧热键并按当前配置 + 兜底清单重新注册（仅在热键线程调用）。
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
	for i := range guardIDs {
		hkUnregProc.Call(hwnd, uintptr(hkGuardBase+i))
	}
	guardIDs = nil

	if hkHost == nil {
		return
	}
	for _, b := range hkHost.Hotkeys() {
		sc := strings.TrimSpace(b.Shortcut)
		if sc == "" {
			continue
		}
		mods, vk, ok := parseShortcut(sc)
		if !ok {
			applog.Warnf("hotkey", "无法解析热键 %q（工具 %s）", sc, b.ID)
			continue
		}
		id := uintptr(len(hkIds) + 1)
		r, _, _ := hkRegProc.Call(hwnd, id, uintptr(mods|hkMOD_NOREPEAT), uintptr(vk))
		if r == 0 {
			applog.Warnf("hotkey", "注册热键失败 %q（工具 %s），可能与其他应用冲突", sc, b.ID)
			continue
		}
		hkIds = append(hkIds, b.ID)
	}
	applog.Infof("hotkey", "已注册 %d 个全局热键", len(hkIds))
	hkApplyGuard(hwnd)
}

// hkApplyGuard 注册兜底热键（仅在热键线程、工具热键之后调用）。
// 注册失败不是致命问题：浮层页面自身仍有 ESC，只是少了页面卡死时的退路。
func hkApplyGuard(hwnd uintptr) {
	guardMu.Lock()
	list := append([]GuardHotkey(nil), guardList...)
	guardMu.Unlock()
	for _, g := range list {
		sc := strings.TrimSpace(g.Shortcut)
		if sc == "" {
			continue
		}
		mods, vk, ok := parseShortcut(sc)
		if !ok {
			applog.Warnf("hotkey", "无法解析兜底热键 %q（%s）", sc, g.ID)
			continue
		}
		id := len(guardIDs)
		r, _, _ := hkRegProc.Call(hwnd, uintptr(hkGuardBase+id), uintptr(mods|hkMOD_NOREPEAT), uintptr(vk))
		if r == 0 {
			applog.Warnf("hotkey", "注册兜底热键失败 %q（%s），可能与其他应用冲突", sc, g.ID)
			continue
		}
		guardIDs = append(guardIDs, g)
	}
	if len(guardIDs) > 0 {
		applog.Infof("hotkey", "已注册 %d 个兜底热键", len(guardIDs))
	}
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
