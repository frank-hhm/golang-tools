//go:build windows

package screenshot

import (
	"bytes"
	"image"
	"strings"
	"testing"

	_ "image/png"
)

// 这些用例会真的抓一帧当前屏幕，属于对真实 Win32 调用的冒烟验证：
// 只断言结构与自洽性（尺寸/解码/清单合理性），不断言具体像素，避免换台机器就红。

func TestCaptureScreenFrameSelfConsistent(t *testing.T) {
	f, err := CaptureScreenFrame()
	if err != nil {
		t.Fatalf("CaptureScreenFrame 失败: %v", err)
	}
	if !strings.HasPrefix(f.Image, "data:image/png;base64,") {
		t.Fatalf("Image 不是 png data URL，前缀=%q", f.Image[:min(40, len(f.Image))])
	}
	raw, err := decodeBase64Data(f.Image)
	if err != nil {
		t.Fatalf("base64 解码失败: %v", err)
	}
	img, format, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("PNG 解码失败: %v", err)
	}
	if format != "png" {
		t.Fatalf("编码格式应为 png，实际 %q", format)
	}
	b := img.Bounds()
	if b.Dx() != f.W || b.Dy() != f.H {
		t.Fatalf("声明尺寸与图片实际尺寸不符: 声明 %dx%d 实际 %dx%d", f.W, f.H, b.Dx(), b.Dy())
	}
	if f.W <= 0 || f.H <= 0 {
		t.Fatalf("屏幕尺寸非法: %dx%d", f.W, f.H)
	}
	if f.DPR < 1 || f.DPR > 8 {
		t.Fatalf("DPR 异常: %v", f.DPR)
	}
}

func TestListVisibleWindowsSanity(t *testing.T) {
	ws := listVisibleWindows()
	if len(ws) == 0 {
		t.Fatal("一个可见窗口都没枚举到，EnumWindows 回调大概率没跑通")
	}
	for i, w := range ws {
		if w.W <= 0 || w.H <= 0 {
			t.Fatalf("第 %d 个窗口尺寸非法: %+v", i, w)
		}
		if strings.TrimSpace(w.Title) == "" {
			t.Fatalf("第 %d 个窗口标题为空，应当已被过滤: %+v", i, w)
		}
	}
}

// 枚举靠回调 return 1 继续遍历；若误 return 0 会在第一个窗口就中断，
// 上面的用例在正常桌面上会因「尺寸非法/标题为空」而失败。

func TestScreenDPRPositive(t *testing.T) {
	if d := screenDPR(); d <= 0 {
		t.Fatalf("screenDPR 返回 %v，应为正数", d)
	}
}

func TestCursorPosWithinScreen(t *testing.T) {
	f, err := CaptureScreenFrame()
	if err != nil {
		t.Fatalf("CaptureScreenFrame 失败: %v", err)
	}
	// 光标必然落在主屏内（多屏时可能为负或越界，此时只要求不是完全离谱的值）
	if f.CursorX < -20000 || f.CursorY < -20000 {
		t.Fatalf("光标坐标离谱: (%d,%d)", f.CursorX, f.CursorY)
	}
}
