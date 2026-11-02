package archive

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeZip 在 dir 下建一个 zip：含一个嵌套文件 a/b.txt、一个根文件 root.txt、一个空目录 d/。
func makeZip(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "pack.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)

	// 空目录条目
	if _, err := zw.Create("d/"); err != nil {
		t.Fatal(err)
	}
	w, err := zw.Create("root.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("ROOT")); err != nil {
		t.Fatal(err)
	}
	w, err = zw.Create("a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("NESTED-CONTENT")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

// makeTarGz 建一个 tar.gz：含 a/b.txt 与 root.txt。
func makeTarGz(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(dir, "pack.tar.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gw := gzip.NewWriter(f)
	tw := tar.NewWriter(gw)

	files := []struct{ name, body string }{
		{"root.txt", "ROOT"},
		{"a/b.txt", "NESTED-CONTENT"},
	}
	for _, bf := range files {
		hdr := &tar.Header{Name: bf.name, Mode: 0o644, Size: int64(len(bf.body)), Typeflag: tar.TypeReg}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(bf.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gw.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestListZip(t *testing.T) {
	zipPath := makeZip(t, t.TempDir())
	entries, err := List(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	// 目录在前，其后按展示路径升序：d/ , a/b.txt , root.txt
	var names []string
	for _, e := range entries {
		names = append(names, e.DispPath)
	}
	got := strings.Join(names, ",")
	want := "d,a/b.txt,root.txt"
	if got != want {
		t.Fatalf("List(zip) 排序不对: got %q want %q", got, want)
	}
	// 尺寸：a/b.txt 应为 NESTED-CONTENT 的 14 字节
	for _, e := range entries {
		if e.DispPath == "a/b.txt" {
			if e.IsDir || e.Size != 14 {
				t.Fatalf("a/b.txt 条目元数据不对: %+v", e)
			}
		}
	}
}

func TestExtractOneZip(t *testing.T) {
	zipPath := makeZip(t, t.TempDir())
	dest := filepath.Join(t.TempDir(), "out")
	got, err := ExtractOne(zipPath, "a/b.txt", dest)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(got) != "b.txt" {
		t.Fatalf("提取落盘名不对: %s", got)
	}
	b, err := os.ReadFile(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "NESTED-CONTENT" {
		t.Fatalf("提取内容不对: %q", string(b))
	}
}

func TestExtractOneRejectsZipSlip(t *testing.T) {
	zipPath := makeZip(t, t.TempDir())
	dest := t.TempDir()
	if _, err := ExtractOne(zipPath, "../evil.txt", dest); err == nil {
		t.Fatal("越界条目名必须被拒绝")
	}
	if _, err := ExtractOne(zipPath, "/abs/evil.txt", dest); err == nil {
		t.Fatal("绝对路径条目名必须被拒绝")
	}
}

func TestExtractOneMissingEntry(t *testing.T) {
	zipPath := makeZip(t, t.TempDir())
	if _, err := ExtractOne(zipPath, "nope.txt", t.TempDir()); err == nil {
		t.Fatal("不存在的条目应报错")
	}
}

func TestListAndExtractTarGz(t *testing.T) {
	p := makeTarGz(t, t.TempDir())
	entries, err := List(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("tar.gz 应有 2 条目, got %d", len(entries))
	}
	dest := t.TempDir()
	got, err := ExtractOne(p, "a/b.txt", dest)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(got); string(b) != "NESTED-CONTENT" {
		t.Fatalf("tar.gz 提取内容不对: %q", string(b))
	}
}

func TestListUnsupportedFormat(t *testing.T) {
	// 一个假 .rar：不在支持格式内，List 应明确报错
	p := filepath.Join(t.TempDir(), "x.rar")
	if err := os.WriteFile(p, []byte("not-real"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := List(p); err == nil {
		t.Fatal("rar 应被判为不支持列举")
	}
}
