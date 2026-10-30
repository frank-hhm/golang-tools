package fileview

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

// write 在临时目录里放一个文件并返回路径。
func write(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("写测试文件失败: %v", err)
	}
	return p
}

func TestClassify(t *testing.T) {
	cases := []struct {
		path string
		kind string
	}{
		{"a/b/c.PNG", "image"},
		{"x.jpeg", "image"},
		{"doc.pdf", "pdf"},
		{"表格.xlsx", "sheet"},
		{"data.csv", "sheet"},
		{"song.mp3", "audio"},
		{"movie.mp4", "video"},
		{"main.go", "code"},
		{"page.vue", "code"},
		{"config.yaml", "code"},
		{"readme.md", "text"},
		{"app.log", "text"},
		{"mystery.dat", "binary"},
		{"noext", "binary"},
	}
	for _, c := range cases {
		if kind, _ := Classify(c.path); kind != c.kind {
			t.Errorf("Classify(%q) kind = %q, 期望 %q", c.path, kind, c.kind)
		}
	}
	// 图片 MIME 走显式表，不受系统注册表影响
	if _, m := Classify("a/b.png"); m != "image/png" {
		t.Errorf("png mime = %q, 期望 image/png", m)
	}
	if _, m := Classify("a.ico"); m != "image/x-icon" {
		t.Errorf("ico mime = %q, 期望 image/x-icon", m)
	}
	// 未知扩展名兜底 octet-stream
	if k, m := Classify("x.unknownext"); k != "binary" || m != "application/octet-stream" {
		t.Errorf("未知扩展名 got (%q,%q), 期望 (binary, octet-stream)", k, m)
	}
}

func TestReadTextRoundtrip(t *testing.T) {
	dir := t.TempDir()
	content := "你好 world\nsecond line"
	p := write(t, dir, "a.txt", []byte(content))
	got, err := ReadText(p)
	if err != nil {
		t.Fatalf("ReadText: %v", err)
	}
	if got.Content != content || got.Binary || got.Truncated {
		t.Errorf("ReadText = %+v, 期望原文且不截断非二进制", got)
	}
	if got.Size != int64(len([]byte(content))) {
		t.Errorf("Size = %d, 期望 %d", got.Size, len([]byte(content)))
	}
}

func TestReadTextBinary(t *testing.T) {
	dir := t.TempDir()
	// 含 NUL
	p := write(t, dir, "nul.bin", append(bytes.Repeat([]byte("ab"), 10), 0))
	got, err := ReadText(p)
	if err != nil {
		t.Fatalf("ReadText: %v", err)
	}
	if !got.Binary {
		t.Errorf("含 NUL 应判二进制, got %+v", got)
	}
	// 非法 UTF-8
	p = write(t, dir, "bad.txt", []byte{0xff, 0xfe, 'a', 'b'})
	got, err = ReadText(p)
	if err != nil {
		t.Fatalf("ReadText: %v", err)
	}
	if !got.Binary {
		t.Errorf("非法 UTF-8 应判二进制, got %+v", got)
	}
}

func TestReadTextTruncatedKeepsTextKind(t *testing.T) {
	dir := t.TempDir()
	// 恰好超限 + 结尾一个完整三字节汉字：截掉尾部残段后仍须判为文本
	body := strings.Repeat("a", MaxTextInline-1) + "你"
	p := write(t, dir, "big.txt", []byte(body))
	got, err := ReadText(p)
	if err != nil {
		t.Fatalf("ReadText: %v", err)
	}
	if !got.Truncated || got.Binary {
		t.Errorf("Truncated=%v Binary=%v, 期望截断但仍是文本", got.Truncated, got.Binary)
	}
	// 截断点落在"你"中间：残段被整段丢弃，长度应接近上限但不超过，且仍是合法 UTF-8
	if len(got.Content) > MaxTextInline || len(got.Content) < MaxTextInline-3 {
		t.Errorf("Content 长度 = %d, 期望落在 (%d-3, %d]", len(got.Content), MaxTextInline-3, MaxTextInline)
	}
	if !utf8.ValidString(got.Content) {
		t.Error("截断后的 Content 不是合法 UTF-8")
	}
	if !strings.HasSuffix(got.Content, "a") {
		t.Errorf("截断收尾应回到完整 rune（末尾应是 'a'）: %q", got.Content[len(got.Content)-6:])
	}
}

func TestReadTextRejectsDirAndMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadText(dir); err == nil {
		t.Error("目录应报错")
	}
	if _, err := ReadText(filepath.Join(dir, "nope.txt")); err == nil {
		t.Error("不存在应报错")
	}
}

func TestWriteTextOnlyOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "w.txt", []byte("old"))
	if err := WriteText(p, "新内容"); err != nil {
		t.Fatalf("WriteText: %v", err)
	}
	got, err := ReadText(p)
	if err != nil || got.Content != "新内容" {
		t.Errorf("回读 = %+v, err=%v", got, err)
	}
	// 不存在的文件不许无中生有
	missing := filepath.Join(dir, "ghost.txt")
	if err := WriteText(missing, "x"); err == nil {
		t.Error("WriteText 应只覆盖已存在文件")
	}
	if _, err := os.Stat(missing); err == nil {
		t.Error("失败路径不应留下文件")
	}
}

func TestWriteTextOverLimit(t *testing.T) {
	dir := t.TempDir()
	p := write(t, dir, "lim.txt", []byte("old"))
	err := WriteText(p, strings.Repeat("a", MaxTextInline+1))
	if err == nil {
		t.Fatal("超限内容应被拒绝")
	}
	// 原文件必须完好，且没有 .part 残留
	if data, _ := os.ReadFile(p); string(data) != "old" {
		t.Errorf("原文件被破坏: %q", data)
	}
	assertNoPart(t, dir)
}

func TestWriteTextFailLeavesNoPart(t *testing.T) {
	dir := t.TempDir()
	// 目标是目录：Stat 通过但写回必失败，验证 .part 清理
	if err := WriteText(dir, "x"); err == nil {
		t.Error("目录目标应报错")
	}
	assertNoPart(t, dir)
	sub := filepath.Join(dir, "d")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteText(sub, "y"); err == nil {
		t.Error("目录目标应报错")
	}
	assertNoPart(t, sub)
}

func TestReadBinaryDataURL(t *testing.T) {
	dir := t.TempDir()
	raw := bytes.Repeat([]byte{0x89, 'P', 'N', 'G'}, 64)
	p := write(t, dir, "i.png", raw)
	got, err := ReadBinaryDataURL(p, "")
	if err != nil {
		t.Fatalf("ReadBinaryDataURL: %v", err)
	}
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
	if got.Kind != "inline" || got.DataURL != want || got.Size != int64(len(raw)) {
		t.Errorf("got %+v", got)
	}
	// 超限：造一个比上限大的稀疏文件，不必写真字节
	big := filepath.Join(dir, "big.bin")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(int64(MaxBinInline) + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()
	got, err = ReadBinaryDataURL(big, "application/octet-stream")
	if err != nil {
		t.Fatalf("ReadBinaryDataURL(tooLarge): %v", err)
	}
	if got.Kind != "tooLarge" || got.DataURL != "" || got.Size != int64(MaxBinInline)+1 {
		t.Errorf("tooLarge 结果不符: %+v", got)
	}
}

func TestWriteBinaryRoundtrip(t *testing.T) {
	dir := t.TempDir()
	raw := bytes.Repeat([]byte{0, 1, 2, 255}, 1000)
	b64 := base64.StdEncoding.EncodeToString(raw)
	// 可新建
	created := filepath.Join(dir, "new.dat")
	if err := WriteBinary(created, b64); err != nil {
		t.Fatalf("WriteBinary 新建: %v", err)
	}
	if data, err := os.ReadFile(created); err != nil || !bytes.Equal(data, raw) {
		t.Errorf("新建回读不符: err=%v data=%v", err, data)
	}
	// 可覆盖
	old := write(t, dir, "old.dat", []byte("stale"))
	if err := WriteBinary(old, b64); err != nil {
		t.Fatalf("WriteBinary 覆盖: %v", err)
	}
	if data, err := os.ReadFile(old); err != nil || !bytes.Equal(data, raw) {
		t.Errorf("覆盖回读不符: err=%v data=%v", err, data)
	}
	assertNoPart(t, dir)
}

func TestWriteBinaryRejects(t *testing.T) {
	dir := t.TempDir()
	if err := WriteBinary(filepath.Join(dir, "x"), "not-base64!!"); err == nil {
		t.Error("非法 base64 应报错")
	}
	// 超限：构造刚过上限的有效 base64（全零字节压缩率高，长度可控）
	oversize := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0}, MaxWriteBinary+1))
	p := filepath.Join(dir, "huge.bin")
	if err := WriteBinary(p, oversize); err == nil {
		t.Error("超过 MaxWriteBinary 应拒绝")
	}
	if _, err := os.Stat(p); err == nil {
		t.Error("拒绝后不应留下文件")
	}
	// 目录目标
	if err := WriteBinary(dir, base64.StdEncoding.EncodeToString([]byte("x"))); err == nil {
		t.Error("目录目标应报错")
	}
	assertNoPart(t, dir)
}

// ---------- MediaHandler ----------

func newMediaServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	mux := MediaHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "NEXT:"+r.URL.Path)
	}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, dir
}

func TestMediaFullAndRange(t *testing.T) {
	srv, dir := newMediaServer(t)
	body := bytes.Repeat([]byte("0123456789abcdef"), 4096) // 64KB
	p := write(t, dir, "clip.mp4", body)

	// 全量 200（路径用 url.Values 编码，避免 Windows 的 \ 与 : 破坏 query）
	q := url.Values{"path": {p}}
	resp, err := http.Get(srv.URL + MediaPath + "?" + q.Encode())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !bytes.Equal(data, body) {
		t.Fatalf("全量: code=%d len=%d", resp.StatusCode, len(data))
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "video/mp4") {
		t.Errorf("Content-Type = %q, 期望 video/mp4", ct)
	}
	if resp.Header.Get("Accept-Ranges") != "bytes" {
		t.Errorf("缺 Accept-Ranges: %v", resp.Header)
	}

	// Range 206
	req, _ := http.NewRequest(http.MethodGet, srv.URL+MediaPath+"?"+q.Encode(), nil)
	req.Header.Set("Range", "bytes=16-31")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, _ = io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent || !bytes.Equal(data, body[16:32]) {
		t.Fatalf("Range: code=%d len=%d", resp.StatusCode, len(data))
	}
	if cr := resp.Header.Get("Content-Range"); cr != fmt.Sprintf("bytes 16-31/%d", len(body)) {
		t.Errorf("Content-Range = %q", cr)
	}
}

func TestMediaDownloadDisposition(t *testing.T) {
	srv, dir := newMediaServer(t)
	p := write(t, dir, "报告.pdf", []byte("%PDF-1.4"))

	resp, err := http.Head(srv.URL + MediaPath + "?" + url.Values{"path": {p}, "dl": {"1"}, "name": {"报告.pdf"}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	cd := resp.Header.Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") || !strings.Contains(cd, "filename*=UTF-8''") {
		t.Errorf("Content-Disposition = %q", cd)
	}

	// 不带 dl 时不该有下载头
	resp, err = http.Get(srv.URL + MediaPath + "?" + url.Values{"path": {p}}.Encode())
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Header.Get("Content-Disposition") != "" {
		t.Error("非 dl 请求不应带 Content-Disposition")
	}
}

func TestMediaRejects(t *testing.T) {
	srv, dir := newMediaServer(t)
	sub := filepath.Join(dir, "d")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	codes := []struct {
		vals url.Values
		want int
	}{
		{url.Values{}, http.StatusBadRequest},                                   // 空 path
		{url.Values{"path": {sub}}, http.StatusBadRequest},                      // 目录
		{url.Values{"path": {filepath.Join(dir, "nope")}}, http.StatusNotFound}, // 不存在
	}
	for _, c := range codes {
		resp, err := http.Get(srv.URL + MediaPath + "?" + c.vals.Encode())
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != c.want {
			t.Errorf("%v code=%d, 期望 %d", c.vals, resp.StatusCode, c.want)
		}
	}
	// POST 被拒
	resp, err := http.Post(srv.URL+MediaPath+"?"+url.Values{"path": {sub}}.Encode(), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("POST code=%d, 期望 405", resp.StatusCode)
	}
}

func TestMediaPassThrough(t *testing.T) {
	srv, _ := newMediaServer(t)
	resp, err := http.Get(srv.URL + "/assets/x.js")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(data) != "NEXT:/assets/x.js" {
		t.Errorf("非 media 路径未透传: code=%d body=%q", resp.StatusCode, data)
	}
}

// assertNoPart 校验目录里没有 .part 残留（原子写的失败清理是否生效）。
func assertNoPart(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".part") {
			t.Errorf("残留临时文件: %s", e.Name())
		}
	}
}
