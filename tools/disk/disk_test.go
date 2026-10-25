package disk

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeFile 在 dir/name 写一段内容，方便造测试数据。
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// TestListOrderAndCounts 验证目录在前、文件在后，以及当前层计数/大小合计。
func TestListOrderAndCounts(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "zdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "adir"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, root, "b.txt", "hello")
	writeFile(t, root, "a.log", "world!")

	got, err := List(root)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got.Dirs != 2 || got.Files != 2 {
		t.Fatalf("计数不对: dirs=%d files=%d", got.Dirs, got.Files)
	}
	if got.Size != int64(len("hello")+len("world!")) {
		t.Fatalf("大小合计不对: %d", got.Size)
	}
	want := []string{"adir", "zdir", "a.log", "b.txt"}
	for i, name := range want {
		if got.Entries[i].Name != name {
			t.Fatalf("第 %d 项应为 %s，实际 %s", i, name, got.Entries[i].Name)
		}
	}
	if got.Entries[0].Mode == "" || got.Entries[0].ModTime == "" {
		t.Fatalf("权限/修改时间没填上: %+v", got.Entries[0])
	}
	if got.Entries[2].Ext != "log" {
		t.Fatalf("扩展名解析不对: %q", got.Entries[2].Ext)
	}
	if len(got.Parts) == 0 || got.Parts[len(got.Parts)-1].Path != root {
		t.Fatalf("面包屑最后一段应为当前路径: %+v", got.Parts)
	}
}

// TestNameValidation 非法名称必须在落盘前被拦下。
func TestNameValidation(t *testing.T) {
	root := t.TempDir()
	for _, bad := range []string{"", "  ", ".", "..", "a/b", `a\b`, "a:b", "a?b", "a*b", `a"b`, "a<b", "a>b", "a|b", "trail "} {
		if _, err := Mkdir(root, bad); err == nil {
			t.Fatalf("名称 %q 应该被拒绝", bad)
		}
	}
	if _, err := Mkdir(root, "ok-name"); err != nil {
		t.Fatalf("合法名称被拒: %v", err)
	}
}

// TestMkdirNewFileRename 基本增删改链路。
func TestMkdirNewFileRename(t *testing.T) {
	root := t.TempDir()
	dir, err := Mkdir(root, "sub")
	if err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	if _, err := Mkdir(root, "sub"); err == nil {
		t.Fatal("同名目录应报错")
	}
	f, err := NewFile(root, "note.txt")
	if err != nil {
		t.Fatalf("NewFile: %v", err)
	}
	if _, err := NewFile(root, "note.txt"); err == nil {
		t.Fatal("同名文件应报错")
	}
	moved, err := Rename(f, "note2.txt")
	if err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if filepath.Base(moved) != "note2.txt" {
		t.Fatalf("改名后路径不对: %s", moved)
	}
	if _, err := os.Stat(filepath.Join(dir, "x")); err == nil {
		t.Fatal("不该凭空出现文件")
	}
}

// TestCopyMoveDeleteAndSize 复制 / 移动 / 永久删除 / 目录大小。
func TestCopyMoveDeleteAndSize(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "src")
	writeFile(t, src, "deep/a.txt", strings.Repeat("x", 100))
	writeFile(t, src, "b.txt", strings.Repeat("y", 50))
	if size, err := DirSize(src); err != nil || size != 150 {
		t.Fatalf("DirSize = %d, err=%v", size, err)
	}

	dst := filepath.Join(root, "dst")
	if err := os.Mkdir(dst, 0o755); err != nil {
		t.Fatal(err)
	}
	n, err := Copy([]string{src}, dst)
	if err != nil || n != 1 {
		t.Fatalf("Copy n=%d err=%v", n, err)
	}
	if size, err := DirSize(filepath.Join(dst, "src")); err != nil || size != 150 {
		t.Fatalf("复制后大小不对: %d err=%v", size, err)
	}

	// 目标已有同名项时必须报错，而不是悄悄覆盖
	if _, err := Copy([]string{src}, dst); err == nil {
		t.Fatal("重复复制到同一目录应报错")
	}

	// 复制到自己的子目录要拦住（否则会无限递归）
	if _, err := Copy([]string{src}, filepath.Join(src, "deep")); err == nil {
		t.Fatal("复制到自己子目录应报错")
	}

	if n, err := Move([]string{filepath.Join(dst, "src")}, src); err != nil || n != 1 {
		t.Fatalf("Move n=%d err=%v", n, err)
	}

	if n, err := PermanentDelete([]string{filepath.Join(src, "src")}); err != nil || n != 1 {
		t.Fatalf("PermanentDelete n=%d err=%v", n, err)
	}
	if _, err := os.Stat(filepath.Join(src, "src")); !os.IsNotExist(err) {
		t.Fatalf("删除后仍存在: %v", err)
	}
}

// TestSearchByNameAndContent 名称搜索与内容搜索。
func TestSearchByNameAndContent(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "app/main.go", "package main\n// TODO: 修一下\n")
	writeFile(t, root, "app/readme.md", "nothing here\n")
	writeFile(t, root, "other/todo.txt", "todo later\n")

	hits, err := Search(root, "tod", true, 10)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(hits) != 1 || filepath.Base(hits[0].Path) != "todo.txt" {
		t.Fatalf("按名搜索命中不对: %+v", hits)
	}

	// 不包含子目录时，当前层没有匹配项
	hits, err = Search(root, "tod", false, 10)
	if err != nil || len(hits) != 0 {
		t.Fatalf("当前层搜索应为空: %+v err=%v", hits, err)
	}

	matches, err := SearchContent(root, "todo", true)
	if err != nil {
		t.Fatalf("SearchContent: %v", err)
	}
	// main.go 的注释与 todo.txt 都含 todo（不区分大小写）
	if len(matches) != 2 {
		t.Fatalf("内容搜索命中数应为 2，实际 %d: %+v", len(matches), matches)
	}
	for _, m := range matches {
		if m.Line <= 0 || m.Text == "" {
			t.Fatalf("命中行信息不全: %+v", m)
		}
	}
}

// TestListErrors 不存在的目录要报错而不是返回空列表。
func TestListErrors(t *testing.T) {
	if _, err := List(filepath.Join(t.TempDir(), "not-exist")); err == nil {
		t.Fatal("不存在的目录应报错")
	}
	f := writeFile(t, t.TempDir(), "a.txt", "x")
	if _, err := List(f); err == nil {
		t.Fatal("传文件当目录应报错")
	}
}

// TestModTimeFormat 修改时间用固定格式，前端才能直接显示。
func TestModTimeFormat(t *testing.T) {
	root := t.TempDir()
	f := writeFile(t, root, "a.txt", "x")
	ts := time.Date(2024, 5, 6, 7, 8, 9, 0, time.Local)
	if err := os.Chtimes(f, ts, ts); err != nil {
		t.Fatal(err)
	}
	got, err := List(root)
	if err != nil {
		t.Fatal(err)
	}
	if got.Entries[0].ModTime != "2024-05-06 07:08:09" {
		t.Fatalf("时间格式不对: %q", got.Entries[0].ModTime)
	}
}
