//go:build windows

package docker

// ============================================================
// 安装链路单测：只覆盖纯逻辑与网络协议层，不碰真实系统
//   - 直链换算（空/build 号/完整 URL/垃圾输入）
//   - 安装参数拼装（静默安装必须带 --accept-license）
//   - 安装包存放目录（留空回落全局 <插件目录>\docker、自定义目录绝对化、不可用时不静默回落）
//   - 取消下载要删掉半截文件
//   - 安装包探测（404、把错误页当包、CDN 不支持 HEAD 的退化路径）
//   - 离线包挑选、文件名还原、体积口语化、文本压平、WSL 措辞判定
// 提权执行与真实下载留给装了 Docker Desktop 的环境实测，这里一律不调用。
// ============================================================

import (
	"go-tools/appdirs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildInstallerURL(t *testing.T) {
	if got := BuildInstallerURL(""); got != InstallerLatestURL {
		t.Errorf("空输入应回落最新直链，得到 %q", got)
	}
	if got := BuildInstallerURL("   "); got != InstallerLatestURL {
		t.Errorf("空白输入应回落最新直链，得到 %q", got)
	}
	if got := BuildInstallerURL("https://example.com/a.exe"); got != "https://example.com/a.exe" {
		t.Errorf("完整 URL 应原样保留，得到 %q", got)
	}
	if got := BuildInstallerURL("4.34.0/165345"); !strings.HasSuffix(got, "/165345/Docker%20Desktop%20Installer.exe") {
		t.Errorf("版本+build 应取末段数字作 build，得到 %q", got)
	}
	if got := BuildInstallerURL("165345"); !strings.Contains(got, "/win/main/amd64/165345/") {
		t.Errorf("裸 build 号应拼进官方模板，得到 %q", got)
	}
	if got := BuildInstallerURL("乱写的中文"); got != InstallerLatestURL {
		t.Errorf("无数字输入应回落最新直链，得到 %q", got)
	}
}

func TestInstallArgsFlags(t *testing.T) {
	// 协议不再做勾选门禁，但安装器参数必须始终带 --accept-license，否则静默安装会卡在交互提示
	params := InstallArgs(false)
	for _, want := range []string{"install", "--quiet", "--accept-license"} {
		if !strings.Contains(params, want) {
			t.Errorf("参数缺少 %q: %q", want, params)
		}
	}
	if strings.Contains(params, "--web-download") {
		t.Errorf("未勾选 web-download 不应带该参数: %q", params)
	}
	if p2 := InstallArgs(true); !strings.Contains(p2, "--web-download") {
		t.Errorf("勾选 web-download 应带该参数: %q", p2)
	}
}

func TestInstallerNameFromURL(t *testing.T) {
	if got := installerNameFromURL("https://x/win/main/amd64/Docker%20Desktop%20Installer.exe"); got != "Docker Desktop Installer.exe" {
		t.Errorf("应还原带空格文件名，得到 %q", got)
	}
	if got := installerNameFromURL("https://x/y/a.exe"); got != "a.exe" {
		t.Errorf("普通文件名应原样，得到 %q", got)
	}
	if got := installerNameFromURL("https://x/y/"); got != "Docker Desktop Installer.exe" {
		t.Errorf("无文件名时应给默认名，得到 %q", got)
	}
	if got := installerNameFromURL("https://x/y/page.html"); got != "Docker Desktop Installer.exe" {
		t.Errorf("非 exe 应给默认名，得到 %q", got)
	}
}

func TestProbeInstallerRejectsBadLinks(t *testing.T) {
	if _, err := ProbeInstaller("ftp://内网共享/安装包.exe"); err == nil || !strings.Contains(err.Error(), "非法下载地址") {
		t.Errorf("非 http(s) 地址应被拒而不是悄然换成官方直链: %v", err)
	}

	notFound := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer notFound.Close()
	if _, err := ProbeInstaller(notFound.URL); err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Errorf("404 应报地址不可用: %v", err)
	}

	// 错误页：状态 200 但体积只有几百字节 → 不能当安装包下载
	errorPage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>404 Not Found</html>"))
	}))
	defer errorPage.Close()
	if _, err := ProbeInstaller(errorPage.URL + "/Docker.exe"); err == nil || !strings.Contains(err.Error(), "不像安装包") {
		t.Errorf("体积过小应被拦下: %v", err)
	}
}

// TestProbeInstallerFallbackToRange 模拟不支持 HEAD 的 CDN：
// 405 后退化为 Range GET，用 Content-Range 还原真实体积。
func TestProbeInstallerFallbackToRange(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method)
		switch r.Method {
		case http.MethodHead:
			w.WriteHeader(http.StatusMethodNotAllowed)
		case http.MethodGet:
			if r.Header.Get("Range") != "bytes=0-0" {
				t.Errorf("退化探测应带 Range: %q", r.Header.Get("Range"))
			}
			w.Header().Set("Content-Range", "bytes 0-0/629145600")
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte{0})
		}
	}))
	defer srv.Close()

	info, err := ProbeInstaller(srv.URL + "/Docker%20Desktop%20Installer.exe")
	if err != nil {
		t.Fatalf("Range 退化探测应成功: %v（seen=%v）", err, seen)
	}
	if info.Size != 629145600 {
		t.Errorf("体积应由 Content-Range 还原，得到 %d", info.Size)
	}
	if info.Name != "Docker Desktop Installer.exe" {
		t.Errorf("文件名解析异常: %q", info.Name)
	}
	if len(seen) != 2 || seen[0] != http.MethodHead || seen[1] != http.MethodGet {
		t.Errorf("应先 HEAD 后 GET，实际 %v", seen)
	}
}

func TestPickNewestInstaller(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "Docker Desktop Installer.exe")
	nw := filepath.Join(dir, "docker-4.34-installer.exe")
	stale := filepath.Join(dir, "not-an-exe.txt")
	for _, p := range []string{old, nw, stale} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 目录不该被选中
	if err := os.Mkdir(filepath.Join(dir, "dir-installer.exe"), 0o755); err != nil {
		t.Fatal(err)
	}
	newer := time.Now().Add(-time.Hour)
	older := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, older, older); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(nw, newer, newer); err != nil {
		t.Fatal(err)
	}
	// 非 exe 候选由调用方的 glob 过滤，这里归入也要能跑：把它改成最旧的一个
	if err := os.Chtimes(stale, older, older); err != nil {
		t.Fatal(err)
	}
	matches := []string{old, nw, stale, filepath.Join(dir, "dir-installer.exe"), filepath.Join(dir, "gone.exe")}
	// 调用方按 *nstaller*.exe 过滤后传入，这里直接验证挑选逻辑
	got, size := pickNewestInstaller(matches)
	if filepath.Base(got) != filepath.Base(nw) {
		t.Errorf("应选最新修改时间的安装器，得到 %q", got)
	}
	if size != 1 {
		t.Errorf("体积应取文件真实大小，得到 %d", size)
	}
	if p, s := pickNewestInstaller(nil); p != "" || s != 0 {
		t.Errorf("空候选应返回空: %q %d", p, s)
	}
}

func TestInstallerDirFollowsGlobalLayout(t *testing.T) {
	// 未指定目录时必须回到全局约定的 <插件目录>\docker，不能丢到临时目录或系统下载里
	got, err := installerDir("")
	if err != nil {
		t.Fatalf("默认目录不应报错: %v", err)
	}
	want, _ := appdirs.Plugins("docker")
	if got != want {
		t.Errorf("默认应为 %q，得到 %q", want, got)
	}
	if !strings.HasSuffix(got, filepath.Join("plugins", "docker")) {
		t.Errorf("默认目录应落在 plugins/docker 下: %q", got)
	}
}

func TestInstallerDirCustom(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "docker 包")
	got, err := installerDir(target)
	if err != nil {
		t.Fatalf("自定义目录应可用: %v", err)
	}
	if got != target {
		t.Errorf("应原样使用自定义目录，得到 %q", got)
	}
	if st, err := os.Stat(got); err != nil || !st.IsDir() {
		t.Errorf("目录应自动创建: %v", err)
	}
	// 相对路径必须绝对化，否则包会写到进程当前的工作目录去
	t.Chdir(base)
	rel, err := installerDir("rel-pkg")
	if err != nil {
		t.Fatalf("相对路径不应报错: %v", err)
	}
	if !filepath.IsAbs(rel) || filepath.Dir(rel) != base {
		t.Errorf("相对路径应基于当前目录绝对化，得到 %q", rel)
	}
}

func TestInstallerDirUnusable(t *testing.T) {
	// 同名普通文件占位 → 必须报错而不是静默回落到默认目录（否则“另存目录”会被悄悄忽略）
	base := t.TempDir()
	blocker := filepath.Join(base, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := installerDir(blocker); err == nil {
		t.Fatal("目录不可用时应报错")
	}
	if p, s := findLocalInstaller(blocker); p != "" || s != 0 {
		t.Errorf("目录不可用时离线包扫描应返回空: %q %d", p, s)
	}
}

func TestFindLocalInstallerInDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Docker Desktop Installer.exe"), []byte("MZ"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, size := findLocalInstallerIn(dir)
	if filepath.Base(got) != "Docker Desktop Installer.exe" || size != 2 {
		t.Errorf("应识别出安装器，得到 %q %d", got, size)
	}
	if p, s := findLocalInstallerIn(filepath.Join(dir, "none")); p != "" || s != 0 {
		t.Errorf("目录不存在应返回空: %q %d", p, s)
	}
}

func TestDropPartCleansCancel(t *testing.T) {
	part := filepath.Join(t.TempDir(), "Docker Desktop Installer.exe.part")
	if err := os.WriteFile(part, []byte("half"), 0o644); err != nil {
		t.Fatal(err)
	}
	note := dropPart(part)
	if !strings.Contains(note, "已删除") {
		t.Errorf("取消应删除半截文件并说明: %q", note)
	}
	if _, err := os.Stat(part); err == nil {
		t.Error("半截文件必须真的被删掉")
	}
	if note := dropPart(part); !strings.Contains(note, "无需清理") {
		t.Errorf("文件本就不存在时应说明无需清理: %q", note)
	}
}

func TestSetupTextHelpers(t *testing.T) {
	if got := humanSize(-1); got != "未知大小" {
		t.Errorf("负体积应有兜底文案: %q", got)
	}
	if got := humanSize(2048); got != "2.0 KB" {
		t.Errorf("KB 档换算异常: %q", got)
	}
	if got := humanSize(600 * 1024 * 1024); got != "600.0 MB" {
		t.Errorf("MB 档换算异常: %q", got)
	}
	// 中文按 rune 截断，不能出现半个字
	if got := truncate("未安装 WSL2，需要重启", 4); got != "未安装 …" {
		t.Errorf("截断应按 rune 并加省略号: %q", got)
	}
	if got := oneLine("a\r\n  b \t c"); got != "a b c" {
		t.Errorf("应压平空白为单行: %q", got)
	}
	for _, s := range []string{"The Windows Subsystem for Linux is not installed.", "未安装适用于 Linux 的 Windows 子系统", "No distributions are installed"} {
		if !wslNegative(s) {
			t.Errorf("应识别为 WSL 未就绪: %q", s)
		}
	}
	if wslNegative("默认发行版: Ubuntu-24.04") {
		t.Error("正常状态不应判为未就绪")
	}
}

func TestAbortSetupWithoutRunningTask(t *testing.T) {
	// 没有进行中的下载时，取消应给出明确提示而不是 panic
	unregisterCancel()
	if err := AbortSetup(); err == nil || !strings.Contains(err.Error(), "没有进行中的下载") {
		t.Errorf("空闲时取消应报无进行中任务: %v", err)
	}
}

func TestEnvNotesWording(t *testing.T) {
	// 四种环境各给一句可执行的下一步，不能只说"失败了"
	cases := []struct {
		in   EnvCheck
		want string
	}{
		{EnvCheck{EngineAvailable: true}, "无需安装"},
		{EnvCheck{DockerInstalled: true, PipeExists: true}, "引擎未应答"},
		{EnvCheck{DockerInstalled: true}, "点「启动引擎」"},
		{EnvCheck{}, "未检测到 Docker Desktop"},
	}
	for _, c := range cases {
		got := strings.Join(envNotes(c.in), "\n")
		if !strings.Contains(got, c.want) {
			t.Errorf("体检文案应含 %q，得到:\n%s", c.want, got)
		}
	}
	// 未装 Docker 且 WSL 未就绪时，必须提示 WSL2 待办
	if n := strings.Join(envNotes(EnvCheck{InstallerFile: `C:\x\Docker Desktop Installer.exe`}), "\n"); !strings.Contains(n, "WSL2") || !strings.Contains(n, "离线安装包") {
		t.Errorf("应同时提示 WSL2 与离线包:\n%s", n)
	}
}

func TestLicenseNotesMentionsGate(t *testing.T) {
	notes := strings.Join(LicenseNotes(), "\n")
	for _, kw := range []string{"Subscription", "管理员权限", "重启"} {
		if !strings.Contains(notes, kw) {
			t.Errorf("协议要点应提及 %q", kw)
		}
	}
}
