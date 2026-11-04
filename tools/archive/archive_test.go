package archive

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Tools/services/sysx"
)

// mk 建测试用的文件树：rel 用 / 分隔，值是要写的文本。
func mk(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for rel, body := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestFormatAndExtMapping(t *testing.T) {
	if got := ExtFor(sysx.ArchiveZip); got != ".zip" {
		t.Fatalf("zip 扩展名 = %q", got)
	}
	if got := ExtFor(sysx.ArchiveTarGz); got != ".tar.gz" {
		t.Fatalf("targz 扩展名 = %q", got)
	}
	// .tar.gz 必须比 .gz 先认，否则会被误判成不存在的 gz 格式
	if got := FormatFor(`x\log.tar.gz`); got != sysx.ArchiveTarGz {
		t.Fatalf("log.tar.gz 识别 = %q", got)
	}
	if got := FormatFor("x/LOG.TGZ"); got != sysx.ArchiveTarGz {
		t.Fatalf("大写 .TGZ 识别 = %q", got)
	}
	if got := FormatFor("a.zip"); got != sysx.ArchiveZip {
		t.Fatalf("a.zip 识别 = %q", got)
	}
	if got := FormatFor("a.7z"); got != "" {
		t.Fatalf("不支持的类型应返回空串，实得 %q", got)
	}
}

func TestStemStripsArchiveSuffixWhole(t *testing.T) {
	cases := map[string]string{
		"log.tar.gz": "log",
		"a.TGZ":      "a",
		"photo.jpg":  "photo",
		"noext":      "noext",
		"a.b.c":      "a.b",
	}
	for in, want := range cases {
		if got := stem(in); got != want {
			t.Errorf("stem(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestNormalize(t *testing.T) {
	base := t.TempDir()
	one := filepath.Join(base, "a.txt")
	if err := os.WriteFile(one, []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}

	t.Run("默认值补齐", func(t *testing.T) {
		o := Options{Paths: []string{" " + one + " ", ""}}
		if err := Normalize(&o); err != nil {
			t.Fatal(err)
		}
		if o.Op != OpCompress || o.Format != sysx.ArchiveZip {
			t.Fatalf("默认值不对: %+v", o)
		}
		if o.Level != sysx.LevelNormal {
			t.Fatalf("级别 = %d，期望默认 %d", o.Level, sysx.LevelNormal)
		}
		if len(o.Paths) != 1 || !filepath.IsAbs(o.Paths[0]) {
			t.Fatalf("路径清洗不对: %v", o.Paths)
		}
	})

	t.Run("空输入", func(t *testing.T) {
		o := Options{Paths: []string{"", "  "}}
		if err := Normalize(&o); err == nil || !strings.Contains(err.Error(), "没有选择") {
			t.Fatalf("应报没选东西，实得 %v", err)
		}
	})

	t.Run("非法格式与操作", func(t *testing.T) {
		o := Options{Paths: []string{one}, Format: "7z"}
		if err := Normalize(&o); err == nil {
			t.Fatal("非法格式应报错")
		}
		o = Options{Paths: []string{one}, Op: "encrypt"}
		if err := Normalize(&o); err == nil {
			t.Fatal("非法操作应报错")
		}
	})

	t.Run("路径不存在", func(t *testing.T) {
		o := Options{Paths: []string{filepath.Join(base, "没有这个")}}
		if err := Normalize(&o); err == nil {
			t.Fatal("不存在的输入应报错")
		}
	})

	t.Run("解压只取一个归档", func(t *testing.T) {
		z := filepath.Join(base, "a.zip")
		if err := os.WriteFile(z, []byte("PK"), 0o644); err != nil { // Normalize 只验存不存在与扩展名
			t.Fatal(err)
		}
		o := Options{Op: OpExtract, Paths: []string{z, one}}
		if err := Normalize(&o); err != nil {
			t.Fatalf("先按只裁到一个归档处理: %v", err)
		}
		if len(o.Paths) != 1 || o.Paths[0] != z {
			t.Fatalf("paths = %v", o.Paths)
		}
		o = Options{Op: OpExtract, Paths: []string{one}}
		if err := Normalize(&o); err == nil {
			t.Fatal("非归档扩展名应报错")
		}
	})
}

func TestSuggestOutNamingRules(t *testing.T) {
	base := t.TempDir()
	mk(t, base, map[string]string{
		"proj/a.txt":     "A",
		"proj/b.txt":     "B",
		"proj/sub/c.txt": "C",
		"solo.txt":       "S",
	})
	proj := filepath.Join(base, "proj")
	solo := filepath.Join(base, "solo.txt")
	aTxt := filepath.Join(proj, "a.txt")
	bTxt := filepath.Join(proj, "b.txt")
	cTxt := filepath.Join(proj, "sub", "c.txt")

	t.Run("单文件去扩展名就地", func(t *testing.T) {
		got, err := SuggestOut(Options{Paths: []string{solo}, Format: sysx.ArchiveZip})
		if err != nil {
			t.Fatal(err)
		}
		want := filepath.Join(base, "solo.zip")
		if got != want {
			t.Fatalf("= %q，期望 %q", got, want)
		}
	})

	t.Run("单目录保留目录名", func(t *testing.T) {
		got, err := SuggestOut(Options{Paths: []string{proj}, Format: sysx.ArchiveTarGz})
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Join(base, "proj.tar.gz") {
			t.Fatalf("= %q", got)
		}
	})

	t.Run("同目录多选：首项名加项数", func(t *testing.T) {
		got, err := SuggestOut(Options{Paths: []string{aTxt, bTxt}, Format: sysx.ArchiveZip})
		if err != nil {
			t.Fatal(err)
		}
		// 拿父目录名会变成 proj/proj.zip（自己套自己），所以用“a 等2项”
		if got != filepath.Join(proj, "a 等2项.zip") {
			t.Fatalf("= %q，期望 proj/a 等2项.zip", got)
		}
	})

	t.Run("跳子目录多选：公共父目录名且放到它的上一级", func(t *testing.T) {
		got, err := SuggestOut(Options{Paths: []string{aTxt, cTxt}, Format: sysx.ArchiveZip})
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Join(base, "proj.zip") {
			t.Fatalf("= %q，期望以公共父目录 proj 命名并放在 proj 旁边", got)
		}
	})

	t.Run("公共父目录是桌面这类就不配当包名", func(t *testing.T) {
		desk := filepath.Join(base, "Desktop")
		x := filepath.Join(desk, "x", "x.txt")
		y := filepath.Join(desk, "y", "y.txt")
		mk(t, base, map[string]string{"Desktop/x/x.txt": "X", "Desktop/y/y.txt": "Y"})
		got, err := SuggestOut(Options{Paths: []string{x, y}, Format: sysx.ArchiveZip})
		if err != nil {
			t.Fatal(err)
		}
		// 兵底名不伴随机：用户此时正在 Desktop 里，成品就该落在那
		if got != filepath.Join(desk, DefaultFallbackName+".zip") {
			t.Fatalf("= %q，期望 Desktop/%s.zip", got, DefaultFallbackName)
		}
	})

	t.Run("自定义名字与输出目录", func(t *testing.T) {
		out := filepath.Join(base, "out")
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := SuggestOut(Options{Paths: []string{solo}, Format: sysx.ArchiveZip, OutDir: out, Name: "我的包"})
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Join(out, "我的包.zip") {
			t.Fatalf("= %q", got)
		}
	})

	t.Run("重名递增避让", func(t *testing.T) {
		if err := os.WriteFile(filepath.Join(base, "solo.zip"), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := SuggestOut(Options{Paths: []string{solo}, Format: sysx.ArchiveZip})
		if err != nil {
			t.Fatal(err)
		}
		if got != filepath.Join(base, "solo (1).zip") {
			t.Fatalf("= %q，期望 solo (1).zip", got)
		}
	})

	t.Run("输出目录不可用", func(t *testing.T) {
		if _, err := SuggestOut(Options{Paths: []string{solo}, Format: sysx.ArchiveZip, OutDir: solo}); err == nil {
			t.Fatal("输出目录指到文件上应报错")
		}
		if _, err := SuggestOut(Options{Paths: []string{solo}, Format: sysx.ArchiveZip, OutDir: filepath.Join(base, "无此目录")}); err == nil {
			t.Fatal("输出目录不存在应报错")
		}
	})
}

// uniquePath 的 .tar.gz 后缀要整段处理：只剥 .gz 会生成 "x.tar (1).gz" 这种名字。
func TestUniquePathKeepsArchiveSuffix(t *testing.T) {
	base := t.TempDir()
	p := filepath.Join(base, "x.tar.gz")
	if err := os.WriteFile(p, []byte("X"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := uniquePath(p); got != filepath.Join(base, "x (1).tar.gz") {
		t.Fatalf("= %q，期望 x (1).tar.gz", got)
	}
	p2 := filepath.Join(base, "y.zip")
	if err := os.WriteFile(p2, []byte("Y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := uniquePath(p2); got != filepath.Join(base, "y (1).zip") {
		t.Fatalf("= %q", got)
	}
}

func TestScanCountsMatchCompress(t *testing.T) {
	base := t.TempDir()
	mk(t, base, map[string]string{
		"src/a.txt":     "AAAA",
		"src/sub/b.txt": "BB",
	})
	src := filepath.Join(base, "src")
	files, bytes := Scan([]string{src})
	if files != 2 || bytes != 6 {
		t.Fatalf("Scan = (%d, %d)，期望 (2, 6)", files, bytes)
	} // 分母必须与实际写进包的字节一致，否则进度条永远走不满
	st, err := sysx.CompressArchive(context.Background(), filepath.Join(base, "s.zip"), []string{src}, sysx.ArchiveZip, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Bytes != bytes || st.Files != files {
		t.Fatalf("Scan(%d,%d) 与压缩统计(%d,%d) 不一致", files, bytes, st.Files, st.Bytes)
	}
}

func TestRunCompressEmitsStages(t *testing.T) {
	base := t.TempDir()
	mk(t, base, map[string]string{"src/a.txt": "AAAA", "src/b.txt": "BBBB"})
	src := filepath.Join(base, "src")

	var stages []string
	var lastPercent int
	res, err := Run(context.Background(), Options{Paths: []string{src}, Format: sysx.ArchiveZip}, "j1", func(p Progress) {
		if p.JobID != "j1" || p.Op != OpCompress {
			t.Errorf("事件缺 jobId/op: %+v", p)
		}
		stages = append(stages, p.Stage)
		lastPercent = p.Percent
	})
	if err != nil {
		t.Fatalf("Run 失败: %v", err)
	}
	joined := strings.Join(stages, ",")
	for _, want := range []string{StageScan, StageWriting, StageDone} {
		if !strings.Contains(joined, want) {
			t.Fatalf("事件序列缺 %s：%v", want, stages)
		}
	}
	if lastPercent != 100 {
		t.Fatalf("终态百分比 = %d，期望 100", lastPercent)
	}
	if filepath.Base(res.Out) != "src.zip" || res.Files != 2 {
		t.Fatalf("结果不对: %+v", res)
	}
	if _, err := os.Stat(res.Out); err != nil {
		t.Fatalf("成品不存在: %v", err)
	}
	// 半成品不能留
	if _, err := os.Stat(res.Out + ".part"); !os.IsNotExist(err) {
		t.Fatal("留下了 .part")
	}
}

func TestRunExtractToDefaultDest(t *testing.T) {
	base := t.TempDir()
	mk(t, base, map[string]string{"src/a.txt": "AAAA"})
	if _, err := Run(context.Background(), Options{Paths: []string{filepath.Join(base, "src")}, Format: sysx.ArchiveZip}, "j", nil); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(base, "src.zip")

	var stages []string
	res, err := Run(context.Background(), Options{Op: OpExtract, Paths: []string{zipPath}}, "j2", func(p Progress) {
		stages = append(stages, p.Stage)
	})
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if !strings.Contains(strings.Join(stages, ","), StageExtracting) {
		t.Fatalf("缺 extracting 事件: %v", stages)
	}
	// 默认解到归档同级的同名文件夹：src.zip → src/；但 src/ 正是压缩时那个源目录，
	// 已存在就必须让开——不然会把包内的 src/ 直接合并进用户的原目录里。
	want := filepath.Join(base, "src (1)", "src", "a.txt")
	if filepath.Clean(res.Out) != filepath.Clean(filepath.Join(base, "src (1)")) {
		t.Fatalf("目标目录 = %q，期望 %s", res.Out, filepath.Join(base, "src (1)"))
	}
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("没解出 %s: %v", want, err)
	}
	// 原目录一个字都不能多
	if _, err := os.Stat(filepath.Join(base, "src", "src")); !os.IsNotExist(err) {
		t.Fatal("往已存在的 src/ 里合并出了套娃目录")
	}
	// 上面 want 里的 src (1)/src 那一层是包内自带顶层目录名导致的，属于已知代价：
	// 主流归档工具（7-Zip / WinRAR 的“解压到 <包名>”）同样如此。要不要在“包内只有一个
	// 顶层目录”时不另套一层，是产品语义选择，改之前先记在这里。
	if _, err := os.Stat(filepath.Join(base, "src", "a.txt")); err != nil {
		t.Fatalf("避让不该动到原目录: %v", err)
	}
}

// 真机右键链路（--archive 紧跟 --extract）复现出来的那一幕。
func TestRunExtractAvoidsExistingDirWithCJKName(t *testing.T) {
	base := t.TempDir()
	// 真机右键链路踩到的就是这一种：中文带空格的目录名，压缩后紧挨着解压。
	// 名字里的空格以前会暴露出另一类问题（参数切分、引号），所以不能只用 src 过测。
	mk(t, base, map[string]string{"源 目录/b.txt": "B", "源 目录/中文 文件.txt": "CN"})
	if _, err := Run(context.Background(), Options{Paths: []string{filepath.Join(base, "源 目录")}, Format: sysx.ArchiveZip}, "jc", nil); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), Options{Op: OpExtract, Paths: []string{filepath.Join(base, "源 目录.zip")}}, "jd", nil)
	if err != nil {
		t.Fatalf("解压失败: %v", err)
	}
	if filepath.Base(res.Out) != "源 目录 (1)" {
		t.Fatalf("目标目录 = %q，期望让开已存在的同名目录", res.Out)
	}
	if _, err := os.Stat(filepath.Join(base, "源 目录", "源 目录")); !os.IsNotExist(err) {
		t.Fatal("往用户的原目录里塞了套娃副本")
	}
	if _, err := os.Stat(filepath.Join(res.Out, "源 目录", "中文 文件.txt")); err != nil {
		t.Fatalf("中文带空格的名字没解出来: %v", err)
	}
}

// 用户自己指定的目标目录不做重名避让：挑一个已有文件夹正是那次选择的意思。
func TestRunExtractHonoursExplicitDest(t *testing.T) {
	base := t.TempDir()
	mk(t, base, map[string]string{"src/a.txt": "AAAA"})
	if _, err := Run(context.Background(), Options{Paths: []string{filepath.Join(base, "src")}, Format: sysx.ArchiveZip}, "j", nil); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(base, "src.zip")
	dest := filepath.Join(base, "in here")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), Options{Op: OpExtract, Paths: []string{zipPath}, Dest: dest}, "jx", nil)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Clean(res.Out) != filepath.Clean(dest) {
		t.Fatalf("目标目录被改了: %q", res.Out)
	}
	if _, err := os.Stat(filepath.Join(dest, "src", "a.txt")); err != nil {
		t.Fatalf("没解到指定目录: %v", err)
	}
}

// 归档与目标同名会互相踩：src.zip 默认解到 src/ 是安全的，但 "a" 这种无扩展名
// 归档去扩展名后等于自己所在目录，必须让开。
func TestRunExtractAvoidsSelfDirectory(t *testing.T) {
	base := t.TempDir()
	mk(t, base, map[string]string{"work/x.txt": "X"})
	archive := filepath.Join(base, "work", "data.zip")
	if _, err := sysx.CompressArchive(context.Background(), archive, []string{filepath.Join(base, "work", "x.txt")}, sysx.ArchiveZip, 0, nil); err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), Options{Op: OpExtract, Paths: []string{archive}}, "j3", nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Out == filepath.Clean(filepath.Join(base, "work")) {
		t.Fatalf("解压目标撞回归档所在目录: %q", res.Out)
	}
	if _, err := os.Stat(filepath.Join(res.Out, "x.txt")); err != nil {
		t.Fatalf("内容没解出来: %v", err)
	}
}

func TestRunCancelAndBadInput(t *testing.T) {
	base := t.TempDir()
	mk(t, base, map[string]string{"src/a.txt": strings.Repeat("A", 4096)})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stages []string
	if _, err := Run(ctx, Options{Paths: []string{filepath.Join(base, "src")}, Format: sysx.ArchiveZip}, "j4", func(p Progress) {
		stages = append(stages, p.Stage)
	}); err == nil {
		t.Fatal("取消后应返回错误")
	}
	if !strings.Contains(strings.Join(stages, ","), StageCanceled) {
		t.Fatalf("应发 canceled 终态，实得 %v", stages)
	}
	if _, err := os.Stat(filepath.Join(base, "src.zip")); !os.IsNotExist(err) {
		t.Fatal("取消了还留下成品")
	}

	if _, err := Run(context.Background(), Options{Paths: nil}, "j5", nil); err == nil {
		t.Fatal("空输入应直接报错")
	}
}
