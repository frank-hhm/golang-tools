//go:build windows

package sysx

import (
	"bytes"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ============================================================
// Windows 文件图标
//
// 用 SHGetFileInfoW 而不是 ExtractIconExW：前者认识 shell 的一切——.lnk 会解析到
// 目标程序的图标，docx/pdf 会给扩展名关联图标，找不到目标（快捷方式坏了）时
// 退化给一个通用图标而不是失败。后者只对 exe/dll 有效，且拿不到链接背后那层。
//
// 所有导出函数都先 Find() 探一遍：LazyProc.Call 在函数不存在时是 panic 而非 error，
// 这条路径是 best-effort 的装饰，绝不能把 panic 抛进调用方。
// ============================================================

var (
	modShell32X   = windows.NewLazySystemDLL("shell32.dll")
	modUser32X    = windows.NewLazySystemDLL("user32.dll")
	modGdi32X     = windows.NewLazySystemDLL("gdi32.dll")
	procSHGetFI   = modShell32X.NewProc("SHGetFileInfoW")
	procDrawIconX = modUser32X.NewProc("DrawIconEx")
	procDestroyIC = modUser32X.NewProc("DestroyIcon")
	procSysMetric = modUser32X.NewProc("GetSystemMetrics")
	procGetDCX    = modUser32X.NewProc("GetDC")
	procReleaseDC = modUser32X.NewProc("ReleaseDC")
	procCompDC    = modGdi32X.NewProc("CreateCompatibleDC")
	procDIB       = modGdi32X.NewProc("CreateDIBSection")
	procSelectObj = modGdi32X.NewProc("SelectObject")
	procDelObj    = modGdi32X.NewProc("DeleteObject")
	procDelDC     = modGdi32X.NewProc("DeleteDC")
)

// shgfiIcon = SHGFI_ICON。不给 SHGFI_SMALLICON 即为大图标（SHGFI_LARGEICON 本身就是 0）。
const shgfiIcon = 0x00000100

// shFileInfo 对齐 SHFILEINFOW 布局：只拿 hIcon，其余字段占位。
//
// cbFileInfo 必须与真实结构体等大，否则 SHGetFileInfoW 直接返回 0：
// szDisplayName 是 WCHAR[MAX_PATH=260]，szFileType 是 WCHAR[80]。
type shFileInfo struct {
	Hicon         uintptr
	Icon          int32
	Attributes    uint32
	szName        [260]uint16
	szContentType [80]uint16
}

// bitmapInfo 对齐 BITMAPINFOHEADER。
type bitmapInfo struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

func procReady(ps ...*windows.LazyProc) bool {
	for _, p := range ps {
		if p.Find() != nil {
			return false
		}
	}
	return true
}

// platformIcon 路径 -> 32x32 PNG dataURL；任何一步失败都返回空串。
func platformIcon(path string) string {
	if !procReady(procSHGetFI, procDrawIconX, procDestroyIC, procSysMetric,
		procGetDCX, procReleaseDC, procCompDC, procDIB, procSelectObj, procDelObj, procDelDC) {
		return ""
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}
	var info shFileInfo
	// SHGetFileInfoW(pszPath, dwAttributes, psfi, cbFileInfo, uFlags)
	r, _, _ := procSHGetFI.Call(
		uintptr(unsafe.Pointer(p)), 0,
		uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info),
		shgfiIcon)
	if r == 0 || info.Hicon == 0 {
		return ""
	}
	defer procDestroyIC.Call(info.Hicon)

	sz := int32(32)
	if n, _, _ := procSysMetric.Call(11 /* SM_CXICON */); n > 0 {
		sz = int32(n)
	}
	url := iconToDataURL(info.Hicon, sz)
	if url == "" && sz != 32 {
		url = iconToDataURL(info.Hicon, 32) // 个别系统度量拿到 48 反而画糊，退回 32 再试
	}
	return url
}

// iconToDataURL 把 HICON 画进 32bpp DIB，读出 BGRA 像素转 PNG dataURL。
// DIB 的 biHeight 取负值表示自上而下的行序，这样像素索引和 image.RGBA 一致。
func iconToDataURL(hicon uintptr, sz int32) string {
	screenDC, _, _ := procGetDCX.Call(0)
	if screenDC == 0 {
		return ""
	}
	defer procReleaseDC.Call(screenDC, 0)
	memDC, _, _ := procCompDC.Call(screenDC)
	if memDC == 0 {
		return ""
	}
	defer procDelDC.Call(memDC)

	bmi := bitmapInfo{Size: uint32(unsafe.Sizeof(bitmapInfo{})), Width: sz, Height: -sz, Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	dib, _, _ := procDIB.Call(memDC, uintptr(unsafe.Pointer(&bmi)), 0,
		uintptr(unsafe.Pointer(&bits)), 0, 0)
	if dib == 0 || bits == nil {
		return ""
	}
	defer procDelObj.Call(dib)

	old, _, _ := procSelectObj.Call(memDC, dib)
	// DI_NORMAL = 3：图标本体 + 掩膜一起画，保留透明通道
	procDrawIconX.Call(memDC, 0, 0, hicon, uintptr(sz), uintptr(sz), 0, 0, 3)
	data := unsafe.Slice((*byte)(bits), int(sz)*int(sz)*4)
	out := encodeBGRAPNG(data, int(sz))
	if old != 0 {
		procSelectObj.Call(memDC, old)
	}
	return out
}

// encodeBGRAPNG DIB 像素（BGRA）转 PNG dataURL。
// DrawIconEx 画老式（无 alpha 通道）图标时 alpha 全为 0，此时整幅按不透明处理，
// 否则会得到一张完全透明的图。
func encodeBGRAPNG(px []byte, sz int) string {
	if len(px) < sz*sz*4 {
		return ""
	}
	opaque := true
	for i := 3; i < len(px); i += 4 {
		if px[i] != 0 {
			opaque = false
			break
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, sz, sz))
	for i := 0; i < sz*sz; i++ {
		b, g, r, a := px[i*4], px[i*4+1], px[i*4+2], px[i*4+3]
		if opaque {
			a = 255
		}
		img.SetRGBA(i%sz, i/sz, color.RGBA{R: r, G: g, B: b, A: a})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}
