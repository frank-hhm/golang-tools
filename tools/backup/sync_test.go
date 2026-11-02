package backup

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSyncCopiesNewAndSkipsIdentical(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "hello")
	writeFile(t, filepath.Join(src, "sub", "b.txt"), "world")

	st, err := Sync(src, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Scanned != 2 || st.Copied != 2 || st.Skipped != 0 {
		t.Fatalf("首轮应拷贝 2 个: %+v", st)
	}
	// 内容落到目标
	if b, _ := os.ReadFile(filepath.Join(dst, "sub", "b.txt")); string(b) != "world" {
		t.Fatalf("子目录文件未正确拷贝: %q", string(b))
	}

	// 再同步一次：内容一致 → 全跳过
	st2, err := Sync(src, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st2.Copied != 0 || st2.Skipped != 2 {
		t.Fatalf("二轮应全跳过: %+v", st2)
	}
}

func TestSyncCopiesChangedSameSize(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, "x.txt"), "aaaa") // 4 字节
	if _, err := Sync(src, dst, nil); err != nil {
		t.Fatal(err)
	}
	// 改成同大小不同内容：仅按大小判断会漏，必须靠 sha256 命中
	writeFile(t, filepath.Join(src, "x.txt"), "bbbb")
	st, err := Sync(src, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Copied != 1 {
		t.Fatalf("同大小不同内容应被判定变化: %+v", st)
	}
	if b, _ := os.ReadFile(filepath.Join(dst, "x.txt")); string(b) != "bbbb" {
		t.Fatalf("变化文件未更新: %q", string(b))
	}
}

func TestSyncNeverDeletesTarget(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, "keep.txt"), "v1")
	writeFile(t, filepath.Join(dst, "extra.txt"), "only-in-dst") // 目标独有文件
	if _, err := Sync(src, dst, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dst, "extra.txt")); err != nil {
		t.Fatalf("纯增量不应删除目标独有文件: %v", err)
	}
}

func TestSyncRejectsBadLayout(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	writeFile(t, filepath.Join(src, "a.txt"), "x")

	if _, err := Sync(src, src, nil); err == nil {
		t.Fatal("src==dst 应被拒绝")
	}
	// dst 位于 src 之内 → 拒绝（自我递归）
	nested := filepath.Join(src, "out")
	if _, err := Sync(src, nested, nil); err == nil {
		t.Fatal("目标位于源之内应被拒绝")
	}
	// 源不存在
	if _, err := Sync(filepath.Join(dir, "nope"), filepath.Join(dir, "d"), nil); err == nil {
		t.Fatal("源不存在应报错")
	}
}

func TestSyncProgressCallback(t *testing.T) {
	src := t.TempDir()
	dst := t.TempDir()
	writeFile(t, filepath.Join(src, "a.txt"), "1")
	writeFile(t, filepath.Join(src, "b.txt"), "2")

	var mu sync.Mutex
	var lastDone, lastTotal int
	_, err := Sync(src, dst, func(done, total int, rel string) {
		mu.Lock()
		lastDone, lastTotal = done, total
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	if lastDone != 2 || lastTotal != 2 {
		t.Fatalf("进度回调末次应为 2/2, got %d/%d", lastDone, lastTotal)
	}
}

// 空目录：源存在但无文件 → 0 扫描，目标被创建。
func TestSyncEmptySource(t *testing.T) {
	src := t.TempDir()
	dst := filepath.Join(t.TempDir(), "newdir")
	st, err := Sync(src, dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Scanned != 0 {
		t.Fatalf("空源应扫描 0: %+v", st)
	}
	if fi, err := os.Stat(dst); err != nil || !fi.IsDir() {
		t.Fatal("目标目录应被创建")
	}
}
