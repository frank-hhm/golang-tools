//go:build windows

package screenshot

import (
	"bytes"
	"fmt"
	"image"
	"strings"
	"testing"

	_ "image/png"
)

// 这些用例会真的抓一帧当前屏幕，属于对真实 Win32 调用的冒烟验证：
// 只断言结构与自洽性（尺寸/解码/清单合理性），不断言具体像素，避免换台机器就红。

// 冻结帧要么是单张 Image（降级路径），要么是等高的 Bands（分带并行编码），
// 二者恰有其一；每条带解出来宽度必须等于屏宽，高度合计必须等于屏高，
// 否则浮层拼接会错位/留缝。
func frameDataURLs(f *CaptureFrame) ([]string, error) {
	if len(f.Bands) > 0 && f.Image != "" {
		return nil, fmt.Errorf("Image 与 Bands 不应同时给出")
	}
	urls := f.Bands
	if len(urls) == 0 {
		urls = []string{f.Image}
	} else if f.BandH*len(urls) != f.H {
		return nil, fmt.Errorf("分带高度不自洽: bandH=%d bands=%d h=%d", f.BandH, len(urls), f.H)
	}
	return urls, nil
}

func TestCaptureScreenFrameSelfConsistent(t *testing.T) {
	f, err := CaptureScreenFrame()
	if err != nil {
		t.Fatalf("CaptureScreenFrame 失败: %v", err)
	}
	urls, err := frameDataURLs(f)
	if err != nil {
		t.Fatal(err)
	}
	stacked := 0
	for i, u := range urls {
		if !strings.HasPrefix(u, "data:image/png;base64,") {
			t.Fatalf("第 %d 条不是 png data URL，前缀=%q", i, min2(u, 40))
		}
		raw, err := decodeBase64Data(u)
		if err != nil {
			t.Fatalf("第 %d 条 base64 解码失败: %v", i, err)
		}
		img, format, err := image.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatalf("第 %d 条 PNG 解码失败: %v", i, err)
		}
		if format != "png" {
			t.Fatalf("编码格式应为 png，实际 %q", format)
		}
		b := img.Bounds()
		if b.Dx() != f.W {
			t.Fatalf("第 %d 条宽度与声明不符: 声明 %d 实际 %d", i, f.W, b.Dx())
		}
		stacked += b.Dy()
	}
	if stacked != f.H {
		t.Fatalf("各带高度合计与屏高不符: 声明 %d 实际 %d", f.H, stacked)
	}
	if f.W <= 0 || f.H <= 0 {
		t.Fatalf("屏幕尺寸非法: %dx%d", f.W, f.H)
	}
	if f.DPR < 1 || f.DPR > 8 {
		t.Fatalf("DPR 异常: %v", f.DPR)
	}
}

func min2(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
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
