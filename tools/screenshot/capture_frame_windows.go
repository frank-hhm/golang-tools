//go:build windows

// 截图浮层的数据供给：整屏冻结帧、可见窗口清单、DPI 缩放比。
//
// 选区编辑已迁到 WebView 浮层（/widget/capture），但 WebView 无法抓取其他
// 进程的窗口内容，所以「冻结哪一帧」「哪些窗口可被整窗截取」必须由这里
// 在原生侧一次性算好，交给前端渲染。浮层窗口是在冻结之后才创建的，
// 因此冻结帧里不会包含浮层自身。
package screenshot

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/png"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// CaptureWindowInfo 一个可被整窗截取的可见窗口（坐标为物理像素，左上原点）。
type CaptureWindowInfo struct {
	X     int    `json:"x"`
	Y     int    `json:"y"`
	W     int    `json:"w"`
	H     int    `json:"h"`
	Title string `json:"title"`
}

// CaptureFrame 交给选区浮层的完整上下文。
type CaptureFrame struct {
	// Image 是整屏冻结帧的 data URL（data:image/png;base64,...）。
	Image string `json:"image"`
	// W/H 是冻结帧的物理像素尺寸，前端据此换算 CSS 像素与图片像素的比例。
	W int `json:"w"`
	H int `json:"h"`
	// DPR 是主屏缩放比（1 / 1.25 / 1.5 / 2 ...）。
	DPR float64 `json:"dpr"`
	// CursorX/CursorY 是触发截图时光标的物理像素坐标，浮层据此决定初始选区。
	CursorX int `json:"cursorX"`
	CursorY int `json:"cursorY"`
	// Windows 按 Z 序自顶向下排列，前端取第一个包住光标的作为悬停高亮目标。
	Windows []CaptureWindowInfo `json:"windows"`
}

// CaptureScreenFrame 冻结当前主屏一帧并打包成浮层可直接消费的数据。
//
// PNG 用 BestSpeed：整屏 3440x1440 用默认压缩要 1s 上下，浮层会明显卡顿；
// BestSpeed 体积只大 20% 左右但编码快得多，而 PNG 本身无损，不影响成品质量。
func CaptureScreenFrame() (*CaptureFrame, error) {
	img := captureFrozenScreen()
	if img == nil {
		return nil, fmt.Errorf("抓屏失败：屏幕或显卡驱动不可访问")
	}

	// 枚举窗口是跨进程 IPC（每个候选窗 1~2 次 DwmGetWindowAttribute），与
	// CPU 密集的整屏 PNG 编码彼此独立：并行跑，把枚举耗时藏进编码里。窗口开
	// 得越多、机器越老，这段重叠省下的绝对时间越明显。
	var wins []CaptureWindowInfo
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		wins = listVisibleWindows()
	}()

	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := enc.Encode(&buf, img); err != nil {
		wg.Wait()
		return nil, fmt.Errorf("PNG 编码失败: %w", err)
	}
	bounds := img.Bounds()
	frame := &CaptureFrame{
		Image: "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()),
		W:     bounds.Dx(),
		H:     bounds.Dy(),
		DPR:   screenDPR(),
	}
	cx, cy := getCursorPos()
	frame.CursorX, frame.CursorY = int(cx), int(cy)

	wg.Wait()
	frame.Windows = wins

	selLogf("冻结帧 %dx%d dpr=%.2f png=%dKB 可见窗口=%d",
		frame.W, frame.H, frame.DPR, buf.Len()/1024, len(frame.Windows))
	return frame, nil
}

// CopyImageBase64 把 data URL 或裸 base64 的 PNG/JPEG 图片写入系统剪贴板。
// 供选区浮层导出成品图后调用（WebView 无法直接写图片剪贴板）。
func CopyImageBase64(data string) error {
	raw, err := decodeBase64Data(data)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("decode image failed: %w", err)
	}
	return copyImageToClipboard(img)
}

// screenDPR 主屏缩放比。GetDpiForSystem 在 Win10+ 可用，取不到时退回 1。
func screenDPR() float64 {
	// 优先按进程 DPI 感知上下文取，避免多屏不同缩放时拿到错误值
	if dpi, _, _ := procGetDpiForSystem.Call(); dpi > 0 {
		return float64(dpi) / 96.0
	}
	hdc, _, _ := procGetDC.Call(0)
	if hdc == 0 {
		return 1
	}
	defer procReleaseDC.Call(hdc, 0)
	logPixels, _, _ := procGetDeviceCaps.Call(hdc, LOGPIXELSY)
	if logPixels == 0 {
		return 1
	}
	return float64(logPixels) / 96.0
}

const LOGPIXELSY = 90

var (
	procGetDpiForSystem          = modUser32.NewProc("GetDpiForSystem")
	procGetDeviceCaps            = modGdi32.NewProc("GetDeviceCaps")
	procEnumWindows              = modUser32.NewProc("EnumWindows")
	procIsWindowVisible          = modUser32.NewProc("IsWindowVisible")
	procGetWindowTextW           = modUser32.NewProc("GetWindowTextW")
	procGetWindowTextLW          = modUser32.NewProc("GetWindowTextLengthW")
	procGetWindowRect            = modUser32.NewProc("GetWindowRect")
	procGetWindowThreadProcessId = modUser32.NewProc("GetWindowThreadProcessId")

	modDwmapi            = windows.NewLazySystemDLL("dwmapi.dll")
	procDwmGetWindowAttr = modDwmapi.NewProc("DwmGetWindowAttribute")
)

const (
	dwmwaExtendedFrameBounds = 9
	dwmwaCloaked             = 13
)

type winRect struct {
	left, top, right, bottom int32
}

// enumWindowsProc 是 EnumWindows 的回调。
// 必须是包级函数：syscall.NewCallback 不接受捕获了变量的闭包，
// 所以累加目标经 lParam 传成一个 *[]CaptureWindowInfo 指针。
// 取回时读的是 lParam 这个局部变量自己的地址（而非把 uintptr 转成指针），
// 既符合 syscall 惯例也不会触发 vet 的 unsafe.Pointer 告警。
//
// 过滤按“越来越贵”排序：先做几次纯 user32 本地调用（可见性 / 标题长度 /
// GetWindowRect 尺寸初筛 / 进程归属），把明显无关的窗口挡在外面，最后才对
// 幸存窗口发 DWM 跨进程调用。窗口开得越多，省下的 DWM 往返越可观。
func enumWindowsProc(hwnd, lParam uintptr) uintptr {
	if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
		return 1
	}
	// 无标题的窗口多是 shell 的宿主窗（Program Manager / WorkerW / 托盘幽灵窗），
	// 它们铺满整屏，若不排除会把真正的应用全部盖住。长度顺手留给后面取标题用。
	n, _, _ := procGetWindowTextLW.Call(hwnd)
	if n == 0 {
		return 1
	}
	// 便宜的 GetWindowRect 先做尺寸初筛（含 Win10/11 那圈约 7px 透明边框也无妨，
	// 只是超集），明显过小的幽灵窗就此出局，不必再动 DWM。
	var r winRect
	if ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&r))); ret == 0 {
		return 1
	}
	if r.right-r.left < 8 || r.bottom-r.top < 8 {
		return 1
	}
	// 本进程自己的窗口全部剔除：浮层里再列出「截图预览」「主窗口」没有意义
	var pid uint32
	procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == selfProcessID {
		return 1
	}
	// 被 DWM 遮蔽（UWP 挂起、最小化的现代应用）的窗口画不出内容，跳过
	var cloaked int32
	procDwmGetWindowAttr.Call(hwnd, dwmwaCloaked, uintptr(unsafe.Pointer(&cloaked)), 4)
	if cloaked != 0 {
		return 1
	}
	// 真实可见边界优先用 DWM 的 EXTENDED_FRAME_BOUNDS（去掉那圈透明边框，否则截出
	// 来会带黑边）；调用失败时 DWM 不写缓冲，r 仍是上面 GetWindowRect 的结果。
	procDwmGetWindowAttr.Call(hwnd, dwmwaExtendedFrameBounds,
		uintptr(unsafe.Pointer(&r)), uintptr(unsafe.Sizeof(r)))
	title := windowTitleN(hwnd, int(n))
	if title == "" {
		return 1
	}
	out := *(**[]CaptureWindowInfo)(unsafe.Pointer(&lParam))
	*out = append(*out, CaptureWindowInfo{
		X: int(r.left), Y: int(r.top),
		W: int(r.right - r.left), H: int(r.bottom - r.top),
		Title: title,
	})
	return 1
}

var selfProcessID = uint32(windows.GetCurrentProcessId())

// windowTitleN 按已知标题长度（前面 GetWindowTextLengthW 已取过）读取窗口标题，
// 避免对同一窗口重复调用 GetWindowTextLengthW。
func windowTitleN(hwnd uintptr, n int) string {
	if n <= 0 {
		return ""
	}
	buf := make([]uint16, n+2)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return windows.UTF16ToString(buf)
}

// listVisibleWindows 按 Z 序自顶向下返回可截取的窗口。
func listVisibleWindows() []CaptureWindowInfo {
	var out []CaptureWindowInfo
	procEnumWindows.Call(syscall.NewCallback(enumWindowsProc), uintptr(unsafe.Pointer(&out)))
	return out
}
