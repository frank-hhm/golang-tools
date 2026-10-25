package sysx

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// ============================================================
// Caps
// ============================================================

func TestCapsFillsRuntimeFields(t *testing.T) {
	c := Caps()
	if c.OS != runtime.GOOS {
		t.Fatalf("OS = %q，期望 %q", c.OS, runtime.GOOS)
	}
	if c.Arch != runtime.GOARCH {
		t.Fatalf("Arch = %q，期望 %q", c.Arch, runtime.GOARCH)
	}
	if c.InitSystem == "" || c.PkgMode == "" {
		t.Fatalf("探测结果不完整: %s", c)
	}
	if c.Desktop() != (runtime.GOOS == "windows") {
		t.Fatalf("Desktop() 与平台不符: %s", c)
	}
	// 同一进程内必须稳定（探测只做一次）
	if again := Caps(); again != c {
		t.Fatalf("Caps 不幂等: %+v vs %+v", c, again)
	}
}

// ============================================================
// safeJoin（zip-slip 防护）
// ============================================================

func TestSafeJoin(t *testing.T) {
	dst := t.TempDir()
	cases := []struct {
		name string
		in   string
		bad  bool
	}{
		{name: "普通相对路径", in: "bin/nginx"},
		{name: "带点号", in: "./conf/nginx.conf"},
		{name: "向上穿越", in: "../evil", bad: true},
		{name: "深层穿越", in: "a/../../evil", bad: true},
		{name: "绝对路径", in: "/etc/passwd", bad: true},
		{name: "反斜杠穿越", in: `..\evil`, bad: true},
		{name: "反斜杠深层穿越", in: `a\..\..\evil`, bad: true},
		// 反斜杠包在 Linux 上是合法文件名字符，但在 Windows 上是分隔符；
		// 两端都得接受（不越界），且落盘位置必须在 dst 内。
		{name: "含反斜杠的普通路径", in: `docs\readme.txt`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := safeJoin(dst, c.in)
			if c.bad {
				if err == nil {
					t.Fatalf("%q 应被拒绝，实际得到 %q", c.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("%q 不应被拒绝: %v", c.in, err)
			}
			rel, err := filepath.Rel(dst, got)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				t.Fatalf("%q 逃出了目标目录: %q", c.in, got)
			}
		})
	}
}

// ============================================================
// flattenSingleRoot
// ============================================================

func TestFlattenSingleRoot(t *testing.T) {
	t.Run("单层顶层目录提上来", func(t *testing.T) {
		dir := t.TempDir()
		inner := filepath.Join(dir, "nginx-1.27.0")
		mustMkdir(t, filepath.Join(inner, "conf"))
		mustWrite(t, filepath.Join(inner, "conf", "nginx.conf"), "worker_processes 1;")
		mustWrite(t, filepath.Join(inner, "nginx.exe"), "bin")

		if err := flattenSingleRoot(dir); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "nginx.exe")); err != nil {
			t.Fatalf("文件没提上来: %v", err)
		}
		if _, err := os.Stat(filepath.Join(dir, "conf", "nginx.conf")); err != nil {
			t.Fatalf("子目录没提上来: %v", err)
		}
	})

	t.Run("顶层多个条目保持原样", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "redis-server.exe"), "bin")
		mustWrite(t, filepath.Join(dir, "redis.conf"), "conf")
		if err := flattenSingleRoot(dir); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(dir, "redis-server.exe")); err != nil {
			t.Fatalf("不该动顶层结构: %v", err)
		}
	})

	t.Run("同名冲突时不产生半迁移", func(t *testing.T) {
		dir := t.TempDir()
		mustWrite(t, filepath.Join(dir, "keep.txt"), "outer")
		inner := filepath.Join(dir, "pkg")
		mustMkdir(t, inner)
		mustWrite(t, filepath.Join(inner, "keep.txt"), "inner")
		mustWrite(t, filepath.Join(inner, "other.txt"), "inner")

		if err := flattenSingleRoot(dir); err != nil {
			t.Fatal(err)
		}
		// 一个都没动：不能出现 other.txt 已提上来、keep.txt 还在子目录的残局
		if _, err := os.Stat(filepath.Join(dir, "other.txt")); err == nil {
			t.Fatal("冲突时不应移动任何条目")
		}
		if _, err := os.Stat(filepath.Join(inner, "other.txt")); err != nil {
			t.Fatalf("子目录内容丢失: %v", err)
		}
	})
}

// ============================================================
// Pkg
// ============================================================

func TestPkgInstallZip(t *testing.T) {
	root := t.TempDir()
	p := pkg{root: func(string) (string, error) { return root, nil }}

	archive := filepath.Join(t.TempDir(), "nginx-1.27.0.zip")
	writeZip(t, archive, map[string]string{
		"nginx-1.27.0/nginx.exe":       "bin",
		"nginx-1.27.0/conf/nginx.conf": "worker_processes 1;",
	})

	dir, err := p.Install("nginx", "1.27.0", archive)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "1.27.0" {
		t.Fatalf("安装目录不对: %s", dir)
	}
	// 单层顶层目录应被提上来
	if _, err := os.Stat(filepath.Join(dir, "nginx.exe")); err != nil {
		t.Fatalf("内容没提到版本根目录: %v", err)
	}
	// 安装标记必须存在，否则会被当成"发布内置版"删不掉
	if !isInstalledDir(dir) {
		t.Fatal("缺少安装标记")
	}

	vers, err := p.Versions("nginx")
	if err != nil {
		t.Fatal(err)
	}
	if len(vers) != 1 || vers[0].Name != "1.27.0" || !vers[0].Installed {
		t.Fatalf("版本列表不对: %+v", vers)
	}

	// 同名版本重装应覆盖而不是叠加
	if _, err := p.Install("nginx", "1.27.0", archive); err != nil {
		t.Fatalf("重复安装失败: %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	// 只剩版本目录，暂存目录必须清掉
	if len(entries) != 1 || entries[0].Name() != "1.27.0" {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("插件目录有残留: %v", names)
	}

	if err := p.Remove("nginx", "1.27.0"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("版本目录没删掉: %v", err)
	}
}

func TestPkgRejectsBadInput(t *testing.T) {
	root := t.TempDir()
	p := pkg{root: func(string) (string, error) { return root, nil }}

	if _, err := p.Install("nginx", "../evil", "x.zip"); err == nil {
		t.Fatal("版本号穿越应被拒绝")
	}
	if _, err := p.Install("nginx", "1.0.0", filepath.Join(root, "missing.zip")); err == nil {
		t.Fatal("不存在的归档应报错")
	}
	// 发布内置版（无安装标记）不允许删除
	builtin := filepath.Join(root, "9.9.9")
	mustMkdir(t, builtin)
	if err := p.Remove("nginx", "9.9.9"); err == nil {
		t.Fatal("内置版不应允许删除")
	}
}

func TestPkgInstallTarGz(t *testing.T) {
	root := t.TempDir()
	p := pkg{root: func(string) (string, error) { return root, nil }}

	archive := filepath.Join(t.TempDir(), "redis-7.4.0.tar.gz")
	writeTarGz(t, archive, map[string]string{
		"redis-7.4.0/src/redis-server": "bin",
		"redis-7.4.0/redis.conf":       "port 6379",
	})

	dir, err := p.Install("redis", "7.4.0", archive)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "src", "redis-server")); err != nil {
		t.Fatalf("tar.gz 解包结果不对: %v", err)
	}
}

// ============================================================
// Cred
// ============================================================

func TestCredRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "sub", "repo.pass")
	c := NewCred()

	// 没存过时返回 (nil, nil)，调用方不必区分"没存过"与"空值"
	if got, err := c.Load(path); err != nil || got != nil {
		t.Fatalf("未存过时应为 (nil, nil)，实际 (%q, %v)", got, err)
	}

	secret := []byte(`p@ss"word\with 空格`)
	if err := c.Save(path, secret); err != nil {
		t.Fatal(err)
	}
	got, err := c.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(secret) {
		t.Fatalf("往返不一致: %q vs %q", got, secret)
	}

	// 非 Windows 平台落盘必须是 0600（属主可读写）
	if runtime.GOOS != "windows" {
		st, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := st.Mode().Perm(); perm != 0o600 {
			t.Fatalf("权限应为 0600，实际 %o", perm)
		}
	}

	if err := c.Delete(path); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(path); err != nil {
		t.Fatalf("重复删除不应报错: %v", err)
	}
	if got, err := c.Load(path); err != nil || got != nil {
		t.Fatalf("删除后应为 (nil, nil)，实际 (%q, %v)", got, err)
	}
}

// ============================================================
// Proc（direct 实现，两平台都会走这段）
// ============================================================

func TestDirectProcLifecycle(t *testing.T) {
	bin, args := helperCommand(t)
	dir := t.TempDir()
	p := newDirectProc()

	spec := ProcSpec{
		ID:      "inst1",
		Service: "test",
		Bin:     bin,
		Args:    args,
		Dir:     dir,
		Env:     map[string]string{helperEnv: helperEnvVal},
		LogFile: filepath.Join(dir, "inst1.log"),
	}
	if err := p.Ensure(spec); err != nil {
		t.Fatal(err)
	}
	if err := p.Start("inst1"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Remove("inst1") })

	// 等进程真的跑起来并产出输出
	waitFor(t, 10*time.Second, func() bool {
		st, err := p.Status("inst1")
		if err != nil || !st.Running {
			return false
		}
		lines, err := p.Logs("inst1", 50)
		if err != nil {
			return false
		}
		return strings.Contains(strings.Join(lines, "\n"), marker)
	}, "进程未进入运行状态或没有输出")

	// 重复 Start 应当幂等
	if err := p.Start("inst1"); err != nil {
		t.Fatalf("重复启动应幂等: %v", err)
	}

	st, err := p.Status("inst1")
	if err != nil {
		t.Fatal(err)
	}
	if !st.Running || st.PID == 0 || st.Backend != "direct" {
		t.Fatalf("状态不对: %+v", st)
	}

	if err := p.Stop("inst1"); err != nil {
		t.Fatal(err)
	}
	st, err = p.Status("inst1")
	if err != nil {
		t.Fatal(err)
	}
	if st.Running {
		t.Fatalf("停止后仍为运行中: %+v", st)
	}
	// 重复 Stop 应当幂等
	if err := p.Stop("inst1"); err != nil {
		t.Fatalf("重复停止应幂等: %v", err)
	}

	if err := p.Remove("inst1"); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Status("inst1"); err == nil {
		t.Fatal("注销后查询应报错")
	}
}

func TestProcEnsureValidation(t *testing.T) {
	bin, _ := helperCommand(t)
	p := newDirectProc()

	for _, bad := range []ProcSpec{
		{ID: "", Bin: bin},
		{ID: "../evil", Bin: bin},
		{ID: "a/b", Bin: bin},
		{ID: "ok", Bin: ""},
		{ID: "ok", Bin: filepath.Join(t.TempDir(), "不存在.exe")},
	} {
		if err := p.Ensure(bad); err == nil {
			t.Fatalf("非法规格应被拒绝: %+v", bad)
		}
	}
}

// ============================================================
// 测试辅助
// ============================================================

const (
	marker    = "sysx-ready"
	helperEnv = "SYSX_TEST_HELPER"
	// helperEnvVal 用不可猜的串：环境里恰好存在同名变量的概率几乎为零，
	// 避免手动跑 go test 时因残留环境变量误触发 helper（会莫名多等 30 秒）。
	helperEnvVal = "proc-lifecycle-9f3c"
)

// TestProcHelperProcess 不是给别人跑的：它只在被 Proc 拉起时（helperEnv 匹配）
// 打印 marker 然后挂住，充当一个可预测的"服务进程"。
func TestProcHelperProcess(t *testing.T) {
	if os.Getenv(helperEnv) != helperEnvVal {
		return
	}
	fmt.Println(marker)
	time.Sleep(30 * time.Second)
}

// helperCommand 用测试二进制自身当服务进程：
// 跨平台、不依赖 cmd.exe 或 /bin/sh，也不会受机器上装没装某个命令影响。
func helperCommand(t *testing.T) (string, []string) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("拿不到测试二进制路径: %v", err)
	}
	return exe, []string{"-test.run=^TestProcHelperProcess$"}
}

// waitFor 轮询直到 cond 返回 true 或超时。
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal(msg)
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeZip 用"相对路径 → 内容"生成一个 zip。
func writeZip(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// writeTarGz 用"相对路径 → 内容"生成一个 tar.gz（带可执行位）。
func writeTarGz(t *testing.T, path string, files map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o755,
			Size:     int64(len(body)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}
