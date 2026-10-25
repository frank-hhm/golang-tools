//go:build windows

package screenshot

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"strings"
	"unsafe"

	"github.com/kbinani/screenshot"
	"golang.org/x/sys/windows"
)

type ScreenshotService struct{}

func NewScreenshotService() *ScreenshotService {
	return &ScreenshotService{}
}

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procOpenClipboard           = user32.NewProc("OpenClipboard")
	procCloseClipboard          = user32.NewProc("CloseClipboard")
	procEmptyClipboard          = user32.NewProc("EmptyClipboard")
	procSetClipboardData        = user32.NewProc("SetClipboardData")
	procRegisterClipboardFormat = user32.NewProc("RegisterClipboardFormatW")
	procGlobalAlloc             = kernel32.NewProc("GlobalAlloc")
	procGlobalLock              = kernel32.NewProc("GlobalLock")
	procGlobalUnlock            = kernel32.NewProc("GlobalUnlock")
	procGlobalFree              = kernel32.NewProc("GlobalFree")
	procGlobalSize              = kernel32.NewProc("GlobalSize")
)

const (
	gmemMoveable = 0x0002
	gmemZeroInit = 0x0040
	cfDib        = 8
)

func (s *ScreenshotService) DisplayInfo() ([]map[string]any, error) {
	n := screenshot.NumActiveDisplays()
	out := make([]map[string]any, 0, n)

	for i := 0; i < n; i++ {
		b := screenshot.GetDisplayBounds(i)
		out = append(out, map[string]any{
			"id":     i,
			"x":      b.Min.X,
			"y":      b.Min.Y,
			"width":  b.Dx(),
			"height": b.Dy(),
		})
	}

	return out, nil
}

func (s *ScreenshotService) CaptureFullScreen() (string, error) {
	bounds := screenshot.GetDisplayBounds(0)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return "", fmt.Errorf("capture screen failed: %w", err)
	}

	return encodeImageToBase64PNG(img)
}

func (s *ScreenshotService) CaptureDisplay(id int) (string, error) {
	bounds := screenshot.GetDisplayBounds(id)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return "", fmt.Errorf("capture display %d failed: %w", id, err)
	}

	return encodeImageToBase64PNG(img)
}

func (s *ScreenshotService) CaptureRegion(x, y, width, height int) (string, error) {
	if width <= 0 || height <= 0 {
		return "", fmt.Errorf("invalid region size")
	}

	bounds := screenshot.GetDisplayBounds(0)
	full, err := screenshot.CaptureRect(bounds)
	if err != nil {
		return "", fmt.Errorf("capture screen failed: %w", err)
	}

	src := image.Rect(x, y, x+width, y+height)
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(dst, dst.Bounds(), full, src.Min, draw.Src)

	return encodeImageToBase64PNG(dst)
}

func (s *ScreenshotService) SaveImage(path string, data string) error {
	if path == "" {
		return fmt.Errorf("path is empty")
	}

	raw, err := decodeBase64PNG(data)
	if err != nil {
		return err
	}

	return os.WriteFile(path, raw, 0644)
}

// SaveImageFile 包级版本：把 base64 PNG 写到指定路径。
// 供根包在原生“另存为”对话框确认路径后调用（无需持有 service 实例）。
func SaveImageFile(path string, data string) error {
	if path == "" {
		return fmt.Errorf("path is empty")
	}
	raw, err := decodeBase64PNG(data)
	if err != nil {
		return err
	}
	return os.WriteFile(path, raw, 0644)
}

// DecodeImageBase64 把 dataURL/base64 图片解码为原始字节，供根包写临时文件（如 OCR）。
func DecodeImageBase64(data string) ([]byte, error) {
	return decodeBase64PNG(data)
}

func (s *ScreenshotService) CopyToClipboard(data string) error {
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

// registerClipboardFormat 注册（或获取）一个剪贴板格式的 UINT 编号，失败返回 0。
func registerClipboardFormat(name string) uint32 {
	p := utf16Ptr(name)
	if p == nil {
		return 0
	}
	ret, _, _ := procRegisterClipboardFormat.Call(uintptr(unsafe.Pointer(p)))
	return uint32(ret)
}

// copyImageToClipboard 把图像写入系统剪贴板，并同时提供两种格式：
//   - CF_DIB（24bpp 位图）：兼容画图、Office、微信/钉钉输入框等传统应用；
//   - "PNG"（私有格式）：兼容浏览器、新版应用。
//
// 这样用户在任意支持粘贴图片的地方 Ctrl+V 都能直接粘出图片。
func copyImageToClipboard(img image.Image) error {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	if w <= 0 || h <= 0 {
		return fmt.Errorf("empty image")
	}

	// --- 构造 CF_DIB：24bpp，biHeight 为正值表示自下而上存储 ---
	rowSize := (w*3 + 3) & ^3 // 每行按 4 字节对齐
	pixelSize := rowSize * h
	const dibHeaderSize = 40 // BITMAPINFOHEADER
	totalSize := dibHeaderSize + pixelSize

	hMem, _, err := procGlobalAlloc.Call(gmemMoveable|gmemZeroInit, uintptr(totalSize))
	if hMem == 0 {
		return fmt.Errorf("GlobalAlloc(dib) failed: %w", err)
	}
	ptr, _, err := procGlobalLock.Call(hMem)
	if ptr == 0 {
		procGlobalFree.Call(hMem)
		return fmt.Errorf("GlobalLock(dib) failed: %w", err)
	}

	buf := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), totalSize)
	binary.LittleEndian.PutUint32(buf[0:4], dibHeaderSize)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(w))
	binary.LittleEndian.PutUint32(buf[8:12], uint32(h)) // 正高度 = 自下而上
	binary.LittleEndian.PutUint16(buf[12:14], 1)        // planes
	binary.LittleEndian.PutUint16(buf[14:16], 24)       // bitcount
	binary.LittleEndian.PutUint32(buf[16:20], 0)        // BI_RGB
	binary.LittleEndian.PutUint32(buf[20:24], uint32(pixelSize))
	binary.LittleEndian.PutUint32(buf[24:28], 2835)
	binary.LittleEndian.PutUint32(buf[28:32], 2835)
	binary.LittleEndian.PutUint32(buf[32:36], 0)
	binary.LittleEndian.PutUint32(buf[36:40], 0)

	pixels := buf[dibHeaderSize:]
	for y := 0; y < h; y++ {
		dstY := h - 1 - y
		dstRow := pixels[dstY*rowSize:]
		for x := 0; x < w; x++ {
			c := img.At(bounds.Min.X+x, bounds.Min.Y+y)
			r, g, b, _ := c.RGBA()
			off := x * 3
			dstRow[off+2] = byte(r >> 8)
			dstRow[off+1] = byte(g >> 8)
			dstRow[off+0] = byte(b >> 8)
		}
	}
	procGlobalUnlock.Call(hMem)

	// --- 构造 PNG 字节（供新式应用粘贴） ---
	cfPNG := registerClipboardFormat("PNG")
	var pngMem uintptr
	if cfPNG != 0 {
		if pngBuf, perr := encodePNGBytes(img); perr == nil && len(pngBuf) > 0 {
			hm, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(len(pngBuf)))
			if hm != 0 {
				p, _, _ := procGlobalLock.Call(hm)
				if p != 0 {
					dst := unsafe.Slice((*byte)(unsafe.Pointer(p)), len(pngBuf))
					copy(dst, pngBuf)
					procGlobalUnlock.Call(hm)
					pngMem = hm
				} else {
					procGlobalFree.Call(hm)
				}
			}
		}
	}

	hwnd := windows.GetForegroundWindow()
	ret, _, err := procOpenClipboard.Call(uintptr(hwnd))
	if ret == 0 {
		if pngMem != 0 {
			procGlobalFree.Call(pngMem)
		}
		procGlobalFree.Call(hMem)
		return fmt.Errorf("OpenClipboard failed: %w", err)
	}
	defer procCloseClipboard.Call()

	procEmptyClipboard.Call()

	dibRet, _, _ := procSetClipboardData.Call(cfDib, hMem)
	okDib := dibRet != 0
	okPNG := false
	if pngMem != 0 {
		pngRet, _, _ := procSetClipboardData.Call(uintptr(cfPNG), pngMem)
		okPNG = pngRet != 0
	}

	// SetClipboardData 成功后内存所有权归系统，不得再释放；仅在失败时回收。
	if !okDib {
		procGlobalFree.Call(hMem)
	}
	if !okPNG && pngMem != 0 {
		procGlobalFree.Call(pngMem)
	}
	if !okDib && !okPNG {
		return fmt.Errorf("SetClipboardData failed (dib=%v png=%v)", okDib, okPNG)
	}
	return nil
}

// encodePNGBytes 将图像编码为 PNG 字节（不含 data 前缀）。
func encodePNGBytes(img image.Image) ([]byte, error) {
	buf := new(bytes.Buffer)
	if err := png.Encode(buf, img); err != nil {
		return nil, fmt.Errorf("encode png failed: %w", err)
	}
	return buf.Bytes(), nil
}

// decodeBase64Data 去除任意 data:image/...;base64, 前缀后解码。
func decodeBase64Data(data string) ([]byte, error) {
	if i := strings.Index(data, "base64,"); i >= 0 {
		data = data[i+len("base64,"):]
	}
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("decode base64 failed: %w", err)
	}
	return decoded, nil
}

func (s *ScreenshotService) ComposeAnnotations(
	base64Image string,
	canvasWidth int,
	canvasHeight int,
	annotations []map[string]any,
) (string, error) {
	raw, err := decodeBase64PNG(base64Image)
	if err != nil {
		return "", err
	}

	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("decode image failed: %w", err)
	}

	dst := image.NewRGBA(image.Rect(0, 0, canvasWidth, canvasHeight))
	draw.Draw(dst, dst.Bounds(), img, image.Point{}, draw.Src)

	for _, a := range annotations {
		typ, _ := a["type"].(string)
		colorHex, _ := a["color"].(string)
		lineWidthF, _ := a["lineWidth"].(float64)

		r, g, b := hexToRGB(colorHex)
		lw := int(math.Max(1, lineWidthF))

		switch typ {
		case "rect":
			x1 := int(a["x1"].(float64))
			y1 := int(a["y1"].(float64))
			x2 := int(a["x2"].(float64))
			y2 := int(a["y2"].(float64))

			drawRect(dst, x1, y1, x2, y2, r, g, b, lw)

		case "arrow":
			x1 := int(a["x1"].(float64))
			y1 := int(a["y1"].(float64))
			x2 := int(a["x2"].(float64))
			y2 := int(a["y2"].(float64))

			drawArrow(dst, x1, y1, x2, y2, r, g, b, lw)

		case "mosaic":
			x1 := int(a["x1"].(float64))
			y1 := int(a["y1"].(float64))
			x2 := int(a["x2"].(float64))
			y2 := int(a["y2"].(float64))
			sizeF, _ := a["size"].(float64)
			size := int(math.Max(4, sizeF))

			drawMosaic(dst, x1, y1, x2, y2, size)

		case "text":
			continue
		}
	}

	return encodeImageToBase64PNG(dst)
}

func encodeImageToBase64PNG(img image.Image) (string, error) {
	buf := new(bytes.Buffer)
	if err := png.Encode(buf, img); err != nil {
		return "", fmt.Errorf("encode png failed: %w", err)
	}

	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

func decodeBase64PNG(data string) ([]byte, error) {
	if strings.HasPrefix(data, "data:image/png;base64,") {
		data = strings.TrimPrefix(data, "data:image/png;base64,")
	}

	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return nil, fmt.Errorf("decode base64 failed: %w", err)
	}

	return decoded, nil
}

func hexToRGB(hex string) (uint8, uint8, uint8) {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) != 6 {
		return 255, 0, 0
	}

	var r, g, b uint8
	fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b)
	return r, g, b
}

func drawRect(dst *image.RGBA, x1, y1, x2, y2 int, r, g, b uint8, lw int) {
	minX := min(x1, x2)
	maxX := max(x1, x2)
	minY := min(y1, y2)
	maxY := max(y1, y2)

	for i := 0; i < lw; i++ {
		drawHLine(dst, minX-i, maxX+i, minY-i, r, g, b)
		drawHLine(dst, minX-i, maxX+i, maxY+i, r, g, b)
		drawVLine(dst, minX-i, minY, maxY, r, g, b)
		drawVLine(dst, maxX+i, minY, maxY, r, g, b)
	}
}

// drawArrow 实心三角头 + 粗杆，比例与预览路径 dibArrow / 前端 drawArrow 一致：
// 头长约 4.5 个线宽、底半宽 0.44 头长，杆画进头里 0.8 头长，圆端不会戳出尖端。
func drawArrow(dst *image.RGBA, x1, y1, x2, y2 int, r, g, b uint8, lw int) {
	dx, dy := float64(x2-x1), float64(y2-y1)
	length := math.Hypot(dx, dy)
	if length < 1 {
		return
	}
	ux, uy := dx/length, dy/length
	head := math.Max(6, math.Min(math.Max(9, float64(lw)*4.5), length*0.58))
	half := head * 0.44

	drawLine(dst, x1, y1, x2-int(ux*head*0.8), y2-int(uy*head*0.8), r, g, b, lw)

	tipX, tipY := float64(x2), float64(y2)
	baseX, baseY := tipX-ux*head, tipY-uy*head
	fillTriangle(dst,
		tipX, tipY,
		baseX-uy*half, baseY+ux*half,
		baseX+uy*half, baseY-ux*half,
		r, g, b)
}

// fillTriangle 逐像素判重心填充实心三角形（箭头头部），坐标允许小数
func fillTriangle(dst *image.RGBA, x1, y1, x2, y2, x3, y3 float64, r, g, b uint8) {
	bounds := dst.Bounds()
	minX := max(bounds.Min.X, int(math.Floor(math.Min(x1, math.Min(x2, x3)))))
	maxX := min(bounds.Max.X-1, int(math.Ceil(math.Max(x1, math.Max(x2, x3)))))
	minY := max(bounds.Min.Y, int(math.Floor(math.Min(y1, math.Min(y2, y3)))))
	maxY := min(bounds.Max.Y-1, int(math.Ceil(math.Max(y1, math.Max(y2, y3)))))

	for py := minY; py <= maxY; py++ {
		fy := float64(py) + 0.5
		for px := minX; px <= maxX; px++ {
			fx := float64(px) + 0.5
			e1 := triCross(x1, y1, x2, y2, fx, fy)
			e2 := triCross(x2, y2, x3, y3, fx, fy)
			e3 := triCross(x3, y3, x1, y1, fx, fy)
			if (e1 >= 0 && e2 >= 0 && e3 >= 0) || (e1 <= 0 && e2 <= 0 && e3 <= 0) {
				dst.SetRGBA(px, py, color.RGBA{R: r, G: g, B: b, A: 255})
			}
		}
	}
}

func triCross(ax, ay, bx, by, px, py float64) float64 {
	return (bx-ax)*(py-ay) - (by-ay)*(px-ax)
}

func drawMosaic(dst *image.RGBA, x1, y1, x2, y2, size int) {
	minX := min(x1, x2)
	maxX := max(x1, x2)
	minY := min(y1, y2)
	maxY := max(y1, y2)

	bounds := dst.Bounds()

	for y := minY; y < maxY; y += size {
		for x := minX; x < maxX; x += size {
			var rr, gg, bb, count int
			for sy := y; sy < y+size && sy < maxY; sy++ {
				for sx := x; sx < x+size && sx < maxX; sx++ {
					if !image.Pt(sx, sy).In(bounds) {
						continue
					}
					c := dst.RGBAAt(sx, sy)
					rr += int(c.R)
					gg += int(c.G)
					bb += int(c.B)
					count++
				}
			}
			if count == 0 {
				continue
			}
			avgR := uint8(rr / count)
			avgG := uint8(gg / count)
			avgB := uint8(bb / count)

			for sy := y; sy < y+size && sy < maxY; sy++ {
				for sx := x; sx < x+size && sx < maxX; sx++ {
					if !image.Pt(sx, sy).In(bounds) {
						continue
					}
					dst.SetRGBA(sx, sy, color.RGBA{R: avgR, G: avgG, B: avgB, A: 255})
				}
			}
		}
	}
}

func drawLine(dst *image.RGBA, x1, y1, x2, y2 int, r, g, b uint8, lw int) {
	dx := x2 - x1
	dy := y2 - y1
	length := math.Sqrt(float64(dx*dx + dy*dy))
	if length < 1 {
		return
	}

	ux := float64(dx) / length
	uy := float64(dy) / length

	for t := 0.0; t <= length; t += 0.5 {
		px := x1 + int(ux*t)
		py := y1 + int(uy*t)
		drawDot(dst, px, py, r, g, b, lw)
	}
}

func drawDot(dst *image.RGBA, x, y int, r, g, b uint8, lw int) {
	half := lw / 2
	for dy := -half; dy <= half; dy++ {
		for dx := -half; dx <= half; dx++ {
			px := x + dx
			py := y + dy
			if image.Pt(px, py).In(dst.Bounds()) {
				dst.SetRGBA(px, py, color.RGBA{R: r, G: g, B: b, A: 255})
			}
		}
	}
}

func drawHLine(dst *image.RGBA, x1, x2, y int, r, g, b uint8) {
	for x := x1; x <= x2; x++ {
		if image.Pt(x, y).In(dst.Bounds()) {
			dst.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
}

func drawVLine(dst *image.RGBA, x, y1, y2 int, r, g, b uint8) {
	for y := y1; y <= y2; y++ {
		if image.Pt(x, y).In(dst.Bounds()) {
			dst.SetRGBA(x, y, color.RGBA{R: r, G: g, B: b, A: 255})
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// CaptureSelectedRegion 弹出区域选区，确认后：
//   - 从选区开始时“冻结”的那帧画面裁剪（所见即所得：选区时能看见的
//     tooltip/气泡/提示也会包含在成品图里，而不是事后重抓实时屏——那时提示已消失）；
//   - 将截图以图像形式写入系统剪贴板（可直接 Ctrl+V 粘贴）；
//   - 按 format 返回 base64 数据（"png" 或 "jpg"/"jpeg"，空值默认 png）。
//
// 用户取消（ESC/右键/无有效选区）时返回空字符串。
func (s *ScreenshotService) CaptureSelectedRegion(format string) (string, error) {
	rect, ok, err := SelectRegion()
	if err != nil {
		return "", fmt.Errorf("select region failed: %w", err)
	}
	if !ok {
		// 用户取消（ESC / 右键 / 未形成有效选区）
		return "", nil
	}

	// 优先从冻结帧裁剪；冻结帧缺失或与选区不匹配时回退到实时抓屏兜底。
	var img image.Image
	if frozen := FrozenFrame.Img; frozen != nil {
		cropped := cropRGBA(frozen, rect)
		if cropped != nil {
			img = cropped
		}
	}
	if img == nil {
		live, cerr := screenshot.CaptureRect(rect)
		if cerr != nil {
			return "", fmt.Errorf("capture failed: %w", cerr)
		}
		img = live
	}
	// 用完释放，避免整屏位图长期占用内存
	FrozenFrame.Img = nil

	// 复制到剪贴板（微信式行为）；失败不影响 base64 返回
	if err := copyImageToClipboard(img); err != nil {
		fmt.Printf("copy to clipboard failed: %v\n", err)
	}

	return encodeImageToBase64Format(img, format)
}

// cropRGBA 从整屏 RGBA 中按屏幕坐标矩形裁剪出子图（自动限制在图像范围内），
// 无有效交集时返回 nil。
func cropRGBA(full *image.RGBA, rect image.Rectangle) *image.RGBA {
	r := rect.Intersect(full.Bounds())
	if r.Dx() <= 0 || r.Dy() <= 0 {
		return nil
	}
	out := image.NewRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	// dst 必须用 out 自身坐标系 (0,0,dx,dy)，源点用整屏坐标下的 r.Min；
	// 若误把整屏坐标的 r 当 dst 传入，draw.Draw 会将其裁剪到 out 范围外→一像素不画→空白图。
	draw.Draw(out, out.Bounds(), full, r.Min, draw.Src)
	return out
}

// PickColorFromScreen 弹出全屏取色覆盖层（冻结屏幕 + 放大镜），单击取色，
// 返回 "#RRGGBB"；用户取消（右键/ESC）时返回空字符串。
// 不依赖 WebView 窗口可见性；“取色时隐藏本窗口”已移除（与 Wails 窗口
// 状态机冲突导致隐藏后被复原/卡死），冻结帧中可能残留本窗口画面。
func (s *ScreenshotService) PickColorFromScreen() (string, error) {
	hex, ok, err := pickColorWindows()
	if err != nil {
		return "", fmt.Errorf("pick color failed: %w", err)
	}
	if !ok {
		return "", nil
	}
	return hex, nil
}

// encodeImageToBase64Format 按指定格式将图像编码为带 data 前缀的 base64。
// 支持 "png" 与 "jpg"/"jpeg"；未知/空值回退到 png。
func encodeImageToBase64Format(img image.Image, format string) (string, error) {
	switch strings.ToLower(format) {
	case "jpg", "jpeg":
		buf := new(bytes.Buffer)
		if err := jpeg.Encode(buf, img, &jpeg.Options{Quality: 92}); err != nil {
			return "", fmt.Errorf("encode jpeg failed: %w", err)
		}
		return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
	default:
		return encodeImageToBase64PNG(img)
	}
}
