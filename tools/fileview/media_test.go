package fileview

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sample.bin")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestServeMediaOldParamCompat(t *testing.T) {
	p := writeFile(t, "hello-media")

	// 旧调用：只带 ?path=（无 src），必须照常 200 且内容一致
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", MediaPath+"?path="+p, nil)
	serveMedia(w, r)
	if w.Code != 200 {
		t.Fatalf("旧参数应 200，得到 %d body=%q", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "hello-media" {
		t.Fatalf("旧参数内容不对: %q", got)
	}
}

func TestServeMediaNewSrcParam(t *testing.T) {
	p := writeFile(t, "0123456789")

	// 新调用：?src=local&path=
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", MediaPath+"?src=local&path="+p, nil)
	serveMedia(w, r)
	if w.Code != 200 {
		t.Fatalf("src=local 应 200，得到 %d", w.Code)
	}
	if w.Body.String() != "0123456789" {
		t.Fatalf("内容不对: %q", w.Body.String())
	}

	// Range 请求走 206，验证 ServeContent 语义未被 docref 破坏
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", MediaPath+"?src=local&path="+p, nil)
	r2.Header.Set("Range", "bytes=2-4")
	serveMedia(w2, r2)
	if w2.Code != 206 {
		t.Fatalf("Range 应 206，得到 %d", w2.Code)
	}
	if got := w2.Body.String(); got != "234" {
		t.Fatalf("Range 片段不对: %q", got)
	}
}

func TestServeMediaErrors(t *testing.T) {
	// 缺 path → 400
	w := httptest.NewRecorder()
	serveMedia(w, httptest.NewRequest("GET", MediaPath, nil))
	if w.Code != 400 {
		t.Fatalf("缺 path 应 400，得到 %d", w.Code)
	}

	// 文件不存在 → 404
	missing := filepath.Join(t.TempDir(), "nope.txt")
	w = httptest.NewRecorder()
	serveMedia(w, httptest.NewRequest("GET", MediaPath+"?src=local&path="+missing, nil))
	if w.Code != 404 {
		t.Fatalf("不存在应 404，得到 %d", w.Code)
	}

	// 目录 → 400
	w = httptest.NewRecorder()
	serveMedia(w, httptest.NewRequest("GET", MediaPath+"?src=local&path="+filepath.Dir(missing), nil))
	if w.Code != 400 {
		t.Fatalf("目录应 400，得到 %d", w.Code)
	}

	// P4 后 webdav 只读预览可用：非法 URL → 400（Parse 校验拦截）
	w = httptest.NewRecorder()
	serveMedia(w, httptest.NewRequest("GET", MediaPath+"?src=webdav&path=/x", nil))
	if w.Code != 400 {
		t.Fatalf("非法 webdav URL 应 400，得到 %d", w.Code)
	}

	// cloud 已作为受支持源接入（经 clouddrive provider 只读预览）：
	// 缺账号 id 属三元组校验失败 → 400（不再是“源不支持”的 501）。
	w = httptest.NewRecorder()
	serveMedia(w, httptest.NewRequest("GET", MediaPath+"?src=cloud&path=/x", nil))
	if w.Code != 400 {
		t.Fatalf("cloud 缺账号 id 应 400，得到 %d", w.Code)
	}
}

func TestServeMediaDownloadDisposition(t *testing.T) {
	p := writeFile(t, "abc")
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", MediaPath+"?src=local&path="+p+"&dl=1&name=报告%20A.pdf", nil)
	serveMedia(w, r)
	cd := w.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") {
		t.Fatalf("dl=1 应带 attachment 头，得到 %q", cd)
	}
}

func TestBaseName(t *testing.T) {
	cases := map[string]string{
		`C:\a\b\Guizu-Setup.exe`: "Guizu-Setup.exe", // 连字符不能被当分隔符
		"/x/y/note.md":           "note.md",
		"bare.txt":               "bare.txt",
		"":                       "download",
		`C:\dir\`:                "download",
	}
	for in, want := range cases {
		if got := baseName(in); got != want {
			t.Errorf("baseName(%q)=%q, want %q", in, got, want)
		}
	}
}
