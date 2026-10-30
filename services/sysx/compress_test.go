package sysx

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// ============================================================
// 压缩原语
//
// 主要口径是"往返"：压完用现成的 extractArchive 解回来逐字节比对。
// 两侧同在一个包里，这个测试同时钉住了 compress.go 与 extract.go，
// 任何一边改动条目名/路径写法都会在这里先炸，而不是在用户机器上。
// ============================================================

// tree 写一棵测试用的目录树，返回根路径。
//
// 根目录名刻意写死成 src：t.TempDir() 最后一段是 001/002 这种递增名，
// 拿它当包内顶层名会让断言里的期望值没法静态写出来。
func tree(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "src")
	files := map[string]string{
		"a.txt":            "A",
		"目录/乙.txt":         "B-deep",
		"目录/内层/c.txt":      "C",
		"with space/d.txt": "D",
	}
	for rel, content := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "空目录", "再下一层"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// walkNames 收集目录下所有条目的相对路径：文件记 "路径|内容"，目录记 "路径/"。
func walkNames(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(dir, p)
		if rerr != nil {
			return rerr
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if fi.IsDir() {
			out = append(out, rel+"/")
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		out = append(out, rel+"|"+string(data))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

func TestCompressExtractRoundTrip(t *testing.T) {
	src := tree(t)

	for _, format := range []string{ArchiveZip, ArchiveTarGz} {
		t.Run(format, func(t *testing.T) {
			dir := t.TempDir()
			var ext string
			if format == ArchiveZip {
				ext = ".zip"
			} else {
				ext = ".tar.gz"
			}
			dst := filepath.Join(dir, "包"+ext)

			st, err := CompressArchive(context.Background(), dst, []string{src}, format, LevelNormal, nil)
			if err != nil {
				t.Fatalf("压缩失败: %v", err)
			}
			if st.Files != 4 {
				t.Errorf("文件数 = %d，期望 4（%+v）", st.Files, st)
			}
			if st.Bytes != 9 {
				t.Errorf("原始字节 = %d，期望 9（A+B-deep+C+D）", st.Bytes)
			}
			// 根 + with space + 目录 + 目录/内层 + 空目录 + 空目录/再下一层
			if st.Dirs != 6 {
				t.Errorf("目录数 = %d，期望 6（%+v）", st.Dirs, st)
			}
			if len(st.Skipped) != 0 {
				t.Errorf("不该有跳过项: %v", st.Skipped)
			}

			out := filepath.Join(dir, "out")
			if err := ExtractArchive(context.Background(), dst, out, nil); err != nil {
				t.Fatalf("解包失败: %v", err)
			}
			got := walkNames(t, out)
			want := []string{
				"src/", // 根条目以自己的 basename 落在包内第一层
				"src/a.txt|A",
				"src/with space/",
				"src/with space/d.txt|D",
				"src/空目录/",
				"src/空目录/再下一层/",
				"src/目录/",
				"src/目录/内层/",
				"src/目录/内层/c.txt|C",
				"src/目录/乙.txt|B-deep",
			}
			sort.Strings(want)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Fatalf("往返后内容不一致\n实际: %v\n期望: %v", got, want)
			}
		})
	}
}

// 压缩整目录时输出文件就在目录里：不排除自己会边写边读，甚至把半成品包进包。
func TestCompressExcludesOwnOutput(t *testing.T) {
	src := tree(t)
	dst := filepath.Join(src, "自我包含.zip")

	st, err := CompressArchive(context.Background(), dst, []string{src}, ArchiveZip, 0, nil)
	if err != nil {
		t.Fatalf("压缩失败: %v", err)
	}
	for _, s := range st.Skipped {
		if strings.Contains(s, "自我包含") {
			t.Fatalf("输出文件被当成输入: %v", st.Skipped)
		}
	}

	out := filepath.Join(t.TempDir(), "out")
	if err := ExtractArchive(context.Background(), dst, out, nil); err != nil {
		t.Fatalf("解包失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(out, filepath.Base(src), "自我包含.zip")); !os.IsNotExist(err) {
		t.Fatalf("压缩包被写进了自己里面（stat err=%v）", err)
	}
	if _, err := os.Stat(filepath.Join(out, filepath.Base(src), "a.txt")); err != nil {
		t.Fatalf("正常条目丢了: %v", err)
	}
}

// 取消必须立刻停下，并且不留 .part 半成品（用户看到目录里有个 .part 会以为压坏了）。
func TestCompressCancelLeavesNoPartial(t *testing.T) {
	src := tree(t)
	dst := filepath.Join(t.TempDir(), "包.zip")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompressArchive(ctx, dst, []string{src}, ArchiveZip, LevelFast, nil); err == nil {
		t.Fatal("已取消的 ctx 应该让压缩失败")
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Fatalf("取消了却留下了成品: %v", err)
	}
	if _, err := os.Stat(dst + ".part"); !os.IsNotExist(err) {
		t.Fatalf("取消了却留下了半成品: %v", err)
	}
}

// 进度回调要单调递增，末值等于原始总字节——前端的百分比全靠它。
func TestCompressProgressMonotonic(t *testing.T) {
	src := tree(t)
	dst := filepath.Join(t.TempDir(), "包.zip")

	var marks []int64
	st, err := CompressArchive(context.Background(), dst, []string{src}, ArchiveZip, LevelNormal, func(written int64) {
		marks = append(marks, written)
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) == 0 {
		t.Fatal("一次进度都没回调")
	}
	for i := 1; i < len(marks); i++ {
		if marks[i] < marks[i-1] {
			t.Fatalf("进度倒退了: %v", marks)
		}
	}
	if marks[len(marks)-1] != st.Bytes {
		t.Fatalf("末值 %d 与统计字节 %d 不符", marks[len(marks)-1], st.Bytes)
	}
}

// 多选两个同名目录：包内不能撞名，后者带序号且保留扩展名语义。
func TestCompressDuplicateRootNames(t *testing.T) {
	base := t.TempDir()
	p1 := filepath.Join(base, "one", "data")
	p2 := filepath.Join(base, "two", "data")
	for _, p := range []string{p1, p2} {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p, "f.txt"), []byte(p), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dst := filepath.Join(base, "包.zip")
	if _, err := CompressArchive(context.Background(), dst, []string{p1, p2}, ArchiveZip, 0, nil); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(base, "out")
	if err := ExtractArchive(context.Background(), dst, out, nil); err != nil {
		t.Fatal(err)
	}
	got := walkNames(t, out)
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, "data/f.txt|"+p1) {
		t.Fatalf("第一个 data 没进来: %v", got)
	}
	if !strings.Contains(joined, "data (2)/f.txt|"+p2) {
		t.Fatalf("第二个 data 没有按撞名规则改名: %v", got)
	}
}

// 单文件、空目录、不存在的根、非法格式——都是编排层会真实递过来的输入。
func TestCompressInputEdges(t *testing.T) {
	base := t.TempDir()
	one := filepath.Join(base, "only.txt")
	if err := os.WriteFile(one, []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("单文件", func(t *testing.T) {
		dst := filepath.Join(base, "单文件.zip")
		st, err := CompressArchive(context.Background(), dst, []string{one}, ArchiveZip, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		if st.Files != 1 {
			t.Fatalf("文件数 = %d", st.Files)
		}
		out := filepath.Join(base, "out1")
		if err := ExtractArchive(context.Background(), dst, out, nil); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(out, "only.txt"))
		if err != nil || string(data) != "X" {
			t.Fatalf("解回来的内容不对: %q err=%v", data, err)
		}
	})

	t.Run("无输入报错", func(t *testing.T) {
		if _, err := CompressArchive(context.Background(), filepath.Join(base, "e.zip"), nil, ArchiveZip, 0, nil); err == nil {
			t.Fatal("空 roots 应该报错")
		}
	})

	t.Run("根不存在", func(t *testing.T) {
		dst := filepath.Join(base, "e2.zip")
		if _, err := CompressArchive(context.Background(), dst, []string{filepath.Join(base, "没有这个东西")}, ArchiveZip, 0, nil); err == nil {
			t.Fatal("根不存在应该报错")
		}
		if _, err := os.Stat(dst); !os.IsNotExist(err) {
			t.Fatal("失败时不该有成品")
		}
	})

	t.Run("非法格式", func(t *testing.T) {
		if _, err := CompressArchive(context.Background(), filepath.Join(base, "e3.zip"), []string{one}, "7z", 0, nil); err == nil {
			t.Fatal("不支持的格式应该报错")
		}
	})

	t.Run("解包目标等于归档", func(t *testing.T) {
		dst := filepath.Join(base, "self.zip")
		if _, err := CompressArchive(context.Background(), dst, []string{one}, ArchiveZip, 0, nil); err != nil {
			t.Fatal(err)
		}
		if err := ExtractArchive(context.Background(), dst, dst, nil); err == nil {
			t.Fatal("把归档解到自己头上应该被挡")
		}
	})
}

// 解包侧新加的进度回调：条目数与字节数都得往上涨，否则任务中心看不出进展。
func TestExtractProgressReports(t *testing.T) {
	src := tree(t)
	dst := filepath.Join(t.TempDir(), "包.zip")
	if _, err := CompressArchive(context.Background(), dst, []string{src}, ArchiveZip, 0, nil); err != nil {
		t.Fatal(err)
	}

	var entries []int
	var bytesTotal int64
	out := filepath.Join(t.TempDir(), "out")
	if err := ExtractArchive(context.Background(), dst, out, func(n int, b int64) {
		entries = append(entries, n)
		bytesTotal = b
	}); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 10 {
		t.Fatalf("回调次数 = %d，期望 10（6 目录 + 4 文件），序列 %v", len(entries), entries)
	}
	for i := 1; i < len(entries); i++ {
		if entries[i] <= entries[i-1] {
			t.Fatalf("条目数没有递增: %v", entries)
		}
	}
	if bytesTotal != 9 {
		t.Fatalf("累计字节 = %d，期望 9", bytesTotal)
	}
}

func TestEntryNameAndUnique(t *testing.T) {
	// 卷根与盘符只在 Windows 上有意义：同样串在 Linux 上“C:\”不是路径分隔，
	// filepath.Base 会把整串当一个普通名字，断言会变成假失败。
	if runtime.GOOS == "windows" {
		if got, err := entryName(`C:\Users\me\Desktop`); err != nil || got != "Desktop" {
			t.Fatalf("普通目录 = %q, err=%v", got, err)
		}
		if got, err := entryName(`C:\`); err != nil || got != "c" {
			t.Fatalf("卷根 = %q, err=%v（既不能带冒号也不能带分隔符）", got, err)
		}
	}
	if got, err := entryName(filepath.Join("some", "dir")); err != nil || got != "dir" {
		t.Fatalf("相对目录 = %q, err=%v", got, err)
	}
	if got := uniqueEntryName("data", 2); got != "data (2)" {
		t.Fatalf("无扩展名条目 = %q", got)
	}
	if got := uniqueEntryName("a.tar.gz", 3); got != "a.tar (3).gz" {
		t.Fatalf("撞名改名要保留最后一段扩展名，实得 %q", got)
	}
	if got := NormalizeLevel(0); got != LevelNormal {
		t.Fatalf("0 应回落到默认级别，实得 %d", got)
	}
	if got := NormalizeLevel(99); got != LevelMax {
		t.Fatalf("超界级别应夹到 9，实得 %d", got)
	}
}
