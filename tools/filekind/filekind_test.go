package filekind

import "testing"

func TestClassifyKind(t *testing.T) {
	cases := []struct {
		path string
		kind string
		ext  string
	}{
		{"a/b/c.PNG", KindImage, "png"},
		{"x.jpeg", KindImage, "jpeg"},
		{"照片.jpg", KindImage, "jpg"},
		{`C:\docs\报告.docx`, KindDoc, "docx"},
		{"sheet.xlsx", KindSheet, "xlsx"},
		{"manual.pdf", KindPDF, "pdf"},
		{"readme.md", KindText, "md"},
		{"main.go", KindCode, "go"},
		{"song.mp3", KindAudio, "mp3"},
		{"clip.mp4", KindVideo, "mp4"},
		{"pack.zip", KindArchive, "zip"},
		// .tar.gz 的 filepath.Ext 只会拿到 .gz，必须按整段后缀识别
		{"backup.tar.gz", KindArchive, "tar.gz"},
		{"backup.tgz", KindArchive, "tgz"},
		{"unknown.xyzdata", KindBinary, "xyzdata"},
		{"noext", KindBinary, ""},
	}
	for _, c := range cases {
		kind, _, ext := Classify(c.path)
		if kind != c.kind {
			t.Errorf("Classify(%q) kind = %q, 期望 %q", c.path, kind, c.kind)
		}
		if ext != c.ext {
			t.Errorf("Classify(%q) ext = %q, 期望 %q", c.path, ext, c.ext)
		}
	}
}

func TestClassifyMimeExplicit(t *testing.T) {
	// 归档与图片走显式 MIME，不依赖系统注册表 / /etc/mime.types
	if _, m, _ := Classify("a.zip"); m != "application/zip" {
		t.Errorf("zip mime = %q, 期望 application/zip", m)
	}
	if _, m, _ := Classify("a.tar.gz"); m != "application/gzip" {
		t.Errorf("tar.gz mime = %q, 期望 application/gzip", m)
	}
	if _, m, _ := Classify("a.png"); m != "image/png" {
		t.Errorf("png mime = %q, 期望 image/png", m)
	}
	if _, m, _ := Classify("a.SVG"); m != "image/svg+xml" {
		t.Errorf("svg mime = %q, 期望 image/svg+xml", m)
	}
}

func TestThumbable(t *testing.T) {
	yes := []string{"a.png", "b.jpg", "c.JPEG", "d.gif"}
	no := []string{"e.svg", "f.webp", "g.bmp", "h.pdf", "i.txt", `C:\x\y.mp4`}
	for _, p := range yes {
		if !Thumbable(p) {
			t.Errorf("Thumbable(%q) = false, 期望 true（标准库能解码）", p)
		}
	}
	for _, p := range no {
		if Thumbable(p) {
			t.Errorf("Thumbable(%q) = true, 期望 false", p)
		}
	}
}

// Order 必须覆盖 Classify 可能返回的每一种类型，否则前端按 Order 分组会漏条目。
func TestOrderCoversAllKinds(t *testing.T) {
	inOrder := map[string]bool{}
	for _, k := range Order {
		inOrder[k] = true
	}
	for _, k := range []string{
		KindImage, KindPDF, KindSheet, KindDoc, KindText,
		KindCode, KindAudio, KindVideo, KindArchive, KindBinary,
	} {
		if !inOrder[k] {
			t.Errorf("Order 缺少类型 %q，前端分组会漏掉这类条目", k)
		}
	}
}
