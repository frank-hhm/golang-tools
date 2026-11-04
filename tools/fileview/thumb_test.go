package fileview

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"Tools/tools/docspace"
)

// writePNG 造一张纯色测试图：left/right 两半分别填色，便于验证盒式平均。
func writePNG(t *testing.T, path string, w, h int, left, right color.Color) {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := left
			if x >= w/2 {
				c = right
			}
			img.Set(x, y, c)
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("建测试图失败: %v", err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatalf("编码测试图失败: %v", err)
	}
}

func TestScaleToRGBAAspectAndAverage(t *testing.T) {
	// 200x100 → 长边限 50，应得 50x25（等比，不形变）
	src := image.NewNRGBA(image.Rect(0, 0, 200, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 200; x++ {
			if x < 100 {
				src.Set(x, y, color.NRGBA{R: 200, G: 100, B: 0, A: 255})
			} else {
				src.Set(x, y, color.NRGBA{R: 100, G: 200, B: 0, A: 255})
			}
		}
	}
	dst := scaleToRGBA(src, 50)
	if b := dst.Bounds(); b.Dx() != 50 || b.Dy() != 25 {
		t.Fatalf("缩放结果 = %dx%d, 期望 50x25", b.Dx(), b.Dy())
	}
	// 左半纯红侧：目标像素应落在源左半色上
	r, g, _, _ := dst.At(2, 12).RGBA()
	if r>>8 < 170 || r>>8 > 215 || g>>8 > 130 {
		t.Errorf("左半色 = (%d,%d), 期望接近 (200,100)", r>>8, g>>8)
	}
	// 右半纯绿侧
	r2, g2, _, _ := dst.At(47, 12).RGBA()
	if r2>>8 > 130 || g2>>8 < 170 {
		t.Errorf("右半色 = (%d,%d), 期望接近 (100,200)", r2>>8, g2>>8)
	}
}

func TestScaleToRGBAUpcalesNotButFlattensAlpha(t *testing.T) {
	// 半透明绿压在白底上：a=128 时约得 (128, 191, 128)，且 A 必须补成不透明
	src := image.NewNRGBA(image.Rect(0, 0, 10, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 10; x++ {
			src.Set(x, y, color.NRGBA{R: 0, G: 255, B: 0, A: 128})
		}
	}
	dst := scaleToRGBA(src, 400) // 比源小都不放大
	if b := dst.Bounds(); b.Dx() != 10 || b.Dy() != 10 {
		t.Fatalf("小图不该被放大，实得 %dx%d", b.Dx(), b.Dy())
	}
	r, g, bl, a := dst.At(5, 5).RGBA()
	if a>>8 != 255 {
		t.Errorf("A = %d, 期望铺平为 255（JPEG 无透明通道）", a>>8)
	}
	if r>>8 > 160 || (g>>8) < 170 || bl>>8 > 160 {
		t.Errorf("半透绿合成色 = (%d,%d,%d), 期望约 (128,191,128)", r>>8, g>>8, bl>>8)
	}
}

func TestScaleToRGABDegenerate(t *testing.T) {
	// 空图（bounds 为 0）不能 panic，也不能给出 0 尺寸目标（后续编码会炸）
	dst := scaleToRGBA(image.NewRGBA(image.Rect(0, 0, 0, 0)), 64)
	if b := dst.Bounds(); b.Dx() < 1 || b.Dy() < 1 {
		t.Errorf("退化图输出尺寸 = %dx%d, 期望至少 1x1", b.Dx(), b.Dy())
	}
}

func TestThumbDimClamp(t *testing.T) {
	if got := thumbDim(""); got != thumbDefaultDim {
		t.Errorf("缺省 = %d, 期望 %d", got, thumbDefaultDim)
	}
	if got := thumbDim("abc"); got != thumbDefaultDim {
		t.Errorf("非法值 = %d, 期望退默认", got)
	}
	if got := thumbDim("0"); got != thumbDefaultDim {
		t.Errorf("0 = %d, 期望退默认", got)
	}
	if got := thumbDim("9999"); got != thumbMaxDim {
		t.Errorf("超大 = %d, 期望夹到 %d", got, thumbMaxDim)
	}
	if got := thumbDim(" 128 "); got != 128 {
		t.Errorf("128 = %d, 期望原样", got)
	}
}

func TestBuildThumbRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.png")
	writePNG(t, p, 120, 60, color.NRGBA{R: 255, G: 0, B: 0, A: 255}, color.NRGBA{R: 0, G: 0, B: 255, A: 255})

	ref, err := docspace.Parse(docspace.SrcLocal, "", p)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	data, err := buildThumb(ref, 60)
	if err != nil {
		t.Fatalf("buildThumb: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("buildThumb 返回空字节")
	}
	img, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("产物解不开: %v", err)
	}
	if format != "jpeg" {
		t.Errorf("产物格式 = %q, 期望 jpeg", format)
	}
	if b := img.Bounds(); b.Dx() > 60 || b.Dy() > 60 {
		t.Errorf("产物尺寸 = %dx%d, 长边应不超过 60", b.Dx(), b.Dy())
	}
}

func TestThumbKeyFollowsEntityAndDim(t *testing.T) {
	ref, err := docspace.Parse(docspace.SrcLocal, "", filepath.Join("d", "a.png"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	base := thumbKey(ref, fakeInfo{size: 100, mod: time.Unix(1000, 0)}, 320)
	if base != thumbKey(ref, fakeInfo{size: 100, mod: time.Unix(1000, 0)}, 320) {
		t.Error("同输入应得同键（否则缓存永不命中）")
	}
	if base == thumbKey(ref, fakeInfo{size: 101, mod: time.Unix(1000, 0)}, 320) {
		t.Error("大小变了必须换键，否则文件改了还回旧图")
	}
	if base == thumbKey(ref, fakeInfo{size: 100, mod: time.Unix(2000, 0)}, 320) {
		t.Error("修改时间变了必须换键")
	}
	if base == thumbKey(ref, fakeInfo{size: 100, mod: time.Unix(1000, 0)}, 640) {
		t.Error("目标尺寸变了必须换键")
	}
}

// fakeInfo 只实现 thumbKey 用到的 Size/ModTime，其余走零值。
type fakeInfo struct {
	size int64
	mod  time.Time
}

func (f fakeInfo) Name() string       { return "a.png" }
func (f fakeInfo) Size() int64        { return f.size }
func (f fakeInfo) Mode() os.FileMode  { return 0o600 }
func (f fakeInfo) ModTime() time.Time { return f.mod }
func (f fakeInfo) IsDir() bool        { return false }
func (f fakeInfo) Sys() any           { return nil }

// serveThumb 的前两道闸门必须在碰到磁盘与缓存目录之前就回绝：
// 非图片类型 415，图片但不存在 404。
func TestServeThumbRejectsEarly(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, ThumbPath+"?src=local&path="+filepath.Join(t.TempDir(), "note.txt"), nil)
	serveThumb(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("非图片 = %d, 期望 %d", rec.Code, http.StatusUnsupportedMediaType)
	}

	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, ThumbPath+"?src=local&path="+filepath.Join(t.TempDir(), "gone.png"), nil)
	serveThumb(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Errorf("图片不存在 = %d, 期望 %d", rec2.Code, http.StatusNotFound)
	}

	rec3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, ThumbPath+"?path=", nil)
	serveThumb(rec3, req3)
	if rec3.Code != http.StatusBadRequest {
		t.Errorf("缺 path = %d, 期望 %d", rec3.Code, http.StatusBadRequest)
	}
}
