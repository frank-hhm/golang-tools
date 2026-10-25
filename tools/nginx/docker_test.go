package nginx

// ============================================================
// Nginx Docker 运行模式的单测
//   - 锁定「实例配置 → 容器参数」的换算：镜像/端口发布（含 HTTPS 第二端口）/挂载/命令/标签
//   - 锁定生成的 nginx.conf：listen 必须是容器端口、路径必须指挂载点、不能出现宿主机路径
//   - 锁定规格签名：改端口/改镜像/换站点根必须重建，仅改实例名不重建
//   - 锁定 SaveInstance 的模式分叉与历史配置兼容
// 容器真实起停需要 Docker 引擎，属手工验证范畴，此处不触碰。
// 所有用例都把数据目录指向临时目录，绝不写本机 nginx-instances.json。
// ============================================================

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-tools/appdirs"
	"go-tools/tools/docker"
)

// useTempData 把数据/日志目录指到临时目录，避免污染本机实例登记（用例结束复原）
func useTempData(t *testing.T) {
	t.Helper()
	prevP, prevD, prevL := appdirs.PluginsBase(), appdirs.DataBase(), appdirs.LogsBase()
	t.Cleanup(func() { _ = appdirs.Set(prevP, prevD, prevL) })
	tmp := t.TempDir()
	if err := appdirs.Set("", filepath.Join(tmp, "data"), filepath.Join(tmp, "logs")); err != nil {
		t.Fatal(err)
	}
}

// saveDocker 保存一个容器实例并回其落盘后的 ID（SaveInstance 只在内部生成 ID，不回写调用方副本）
func saveDocker(t *testing.T, name string, port int) string {
	t.Helper()
	if err := SaveInstance(InstanceConfig{Name: name, Mode: ModeDocker, Image: "nginx:alpine", Port: port}); err != nil {
		t.Fatal(err)
	}
	list, err := ListInstances()
	if err != nil || len(list) == 0 {
		t.Fatalf("实例未落盘: %v", err)
	}
	return list[len(list)-1].ID
}

func TestIsDockerMode(t *testing.T) {
	cases := []struct {
		mode string
		want bool
	}{
		{"", false},          // 历史配置无 mode 字段
		{"local", false},     // 显式本地
		{"docker", true},     // 容器
		{"Docker", true},     // 容错大小写
		{" docker ", true},   // 容错空白
		{"container", false}, // 未知值按本地
	}
	for _, c := range cases {
		if got := isDockerMode(&InstanceConfig{Mode: c.mode}); got != c.want {
			t.Errorf("Mode=%q → %v, 期望 %v", c.mode, got, c.want)
		}
	}
}

func TestContainerNameOf(t *testing.T) {
	if n := containerNameOf(&InstanceConfig{ID: "inst-7"}); n != "go-tools-nginx-inst-7" {
		t.Fatalf("默认容器名错误: %s", n)
	}
	if n := containerNameOf(&InstanceConfig{ID: "inst-7", ContainerName: " my-site "}); n != "my-site" {
		t.Fatalf("自定义容器名错误: %s", n)
	}
}

func TestVersionFromImage(t *testing.T) {
	cases := []struct{ image, want string }{
		{"nginx:1.27.4-alpine", "1.27.4"},
		{"nginx:1.27", "1.27"},
		{"nginx:alpine", ""},
		{"nginx:latest", ""},
		{"nginx", ""},
		{"my.registry:5000/nginx:1.25.5", "1.25.5"},
	}
	for _, c := range cases {
		if got := versionFromImage(c.image); got != c.want {
			t.Errorf("versionFromImage(%q) = %q, 期望 %q", c.image, got, c.want)
		}
	}
	if v := displayVersionOf(&InstanceConfig{Image: "nginx:1.27.4-alpine"}); v != "1.27.4" {
		t.Fatalf("展示版本应取镜像 tag: %q", v)
	}
	// tag 里没有版本号时退回镜像名，面板至少能看出跑的是哪个镜像
	if v := displayVersionOf(&InstanceConfig{Image: "nginx:custom"}); v != "nginx:custom" {
		t.Fatalf("镜像无版本号时应回退镜像名: %q", v)
	}
}

func TestDockerBindsAndArgs(t *testing.T) {
	prefix := filepath.FromSlash("C:/data/nginx/nginx-instances/inst-1")
	docRoot := filepath.FromSlash("C:/www/site")
	binds := dockerBinds(prefix, docRoot)
	want := []string{
		"C:/data/nginx/nginx-instances/inst-1/conf:/go-tools/conf",
		"C:/data/nginx/nginx-instances/inst-1/logs:/var/log/nginx",
		"C:/www/site:/usr/share/nginx/html",
		"C:/data/nginx/nginx-instances/inst-1/html:/go-tools/html",
	}
	if strings.Join(binds, "|") != strings.Join(want, "|") {
		t.Fatalf("挂载表不符:\n got %v\nwant %v", binds, want)
	}
	// 挂的必须是 <实例目录>/conf 本身：否则宿主机 nginx.conf 会落在 /go-tools/conf/conf/nginx.conf，
	// 容器里 -c 指过去找不到文件（真机上出现过）
	if binds[0] != filepath.ToSlash(filepath.Join(prefix, "conf"))+":"+containerConfDir {
		t.Fatalf("配置目录挂载层级错误: %s", binds[0])
	}
	for _, b := range binds {
		if strings.Contains(b, "\\") {
			t.Fatalf("挂载路径必须用正斜杠（容器内不认反斜杠）: %s", b)
		}
	}
	args := dockerArgsOf()
	if strings.Join(args, " ") != "nginx -g daemon off; -c /go-tools/conf/nginx.conf" {
		t.Fatalf("容器内启动命令错误: %v", args)
	}
	// daemon off; 不能少：覆盖官方镜像的 CMD 后 nginx 默认会 daemonize，容器立刻退出
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "daemon off;") {
		t.Fatalf("缺少 daemon off; 会让容器启动即退出: %s", joined)
	}
}

func TestDockerSpecOf(t *testing.T) {
	cfg := &InstanceConfig{ID: "inst-1", Name: "站点", Mode: ModeDocker, Image: "nginx:alpine", Port: 8080}
	spec := dockerSpecOf(cfg, "/prefix", "/www")
	if spec.Image != "nginx:alpine" || spec.Name != "go-tools-nginx-inst-1" {
		t.Fatalf("镜像/容器名错误: %+v", spec)
	}
	if spec.ContainerPort != containerPortHTTP || spec.HostPort != 8080 {
		t.Fatalf("主端口应为容器 80 发布到宿主 8080: %+v", spec)
	}
	if spec.HostIP != "127.0.0.1" {
		t.Fatalf("默认只发布到本机回环: %q", spec.HostIP)
	}
	if len(spec.ExtraPorts) != 0 {
		t.Fatalf("未开 HTTPS 时不该有额外端口: %v", spec.ExtraPorts)
	}
	if spec.Labels[docker.LabelTool] != "nginx" || spec.Labels[docker.LabelInstance] != "inst-1" {
		t.Fatalf("归属标签错误（应用重启后靠它找回容器）: %v", spec.Labels)
	}
	// 开了 HTTPS：443 作为第二端口发布，容器内监听仍是 443
	cfg.SSLPort = 8443
	spec = dockerSpecOf(cfg, "/prefix", "/www")
	if len(spec.ExtraPorts) != 1 || spec.ExtraPorts[0].Container != containerPortHTTPS || spec.ExtraPorts[0].Host != 8443 {
		t.Fatalf("HTTPS 端口发布错误: %+v", spec.ExtraPorts)
	}
}

func TestDockerConfText(t *testing.T) {
	cfg := &InstanceConfig{ID: "inst-1", Name: "站点", Mode: ModeDocker, Image: "nginx:alpine", Port: 8080, DocRoot: `C:\www\site`}
	text := dockerConfText(cfg)
	for _, want := range []string{
		"listen       80;",                        // 容器端口，不是宿主端口
		"root         \"/usr/share/nginx/html\";", // 站点根走挂载点
		"include       /etc/nginx/mime.types;",
		"error_log  /var/log/nginx/error.log warn;",
		"pid        /var/run/nginx.pid;",
		"/go-tools/html", // 50x 兜底页挂载点
	} {
		if !strings.Contains(text, want) {
			t.Errorf("配置缺少 %q:\n%s", want, text)
		}
	}
	for _, ban := range []string{"C:\\", "C:/www", "8080", "listen       8080;"} {
		if strings.Contains(text, ban) {
			t.Errorf("容器配置里不该出现 %q:\n%s", ban, text)
		}
	}
	// 改宿主端口不应动配置文件（端口靠发布映射），因此签名之外的变更只有镜像/挂载/HTTPS
	cfg.SSLPort = 8443
	text = dockerConfText(cfg)
	if !strings.Contains(text, "listen       443 ssl;") || !strings.Contains(text, "/go-tools/conf/server.crt;") {
		t.Fatalf("HTTPS server 段生成错误:\n%s", text)
	}
	if strings.Contains(text, "8443") {
		t.Fatalf("HTTPS 宿主端口不该写进配置:\n%s", text)
	}
	// worker_processes：>0 用数字，<=0 用 auto
	if !strings.Contains(dockerConfText(&InstanceConfig{Workers: 0}), "worker_processes  auto;") {
		t.Fatal("Workers<=0 应生成 auto")
	}
	if !strings.Contains(dockerConfText(&InstanceConfig{Workers: 2}), "worker_processes  2;") {
		t.Fatal("Workers=2 应生成 2")
	}
	if strings.Contains(dockerConfText(&InstanceConfig{StubStatus: false}), "stub_status;") {
		t.Fatal("未勾选时不该生成 stub_status")
	}
	if !strings.Contains(dockerConfText(&InstanceConfig{StubStatus: true}), "stub_status;") {
		t.Fatal("勾选后应生成 stub_status")
	}
}

func TestEnsureDockerConfKeepsUserEdits(t *testing.T) {
	tmp := t.TempDir()
	confPath := filepath.Join(tmp, "nginx.conf")
	cfg := &InstanceConfig{ID: "inst-1", Name: "站点", Mode: ModeDocker, Image: "nginx:alpine", Port: 8080}
	if err := ensureDockerConf(cfg, confPath); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(confPath)
	if !strings.Contains(string(first), "listen       80;") {
		t.Fatalf("首次应生成容器版配置:\n%s", first)
	}
	// 用户接管后不能被覆盖（与本地模式 ensureConf 同等待遇）
	if err := os.WriteFile(confPath, []byte("# 我自己写的\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureDockerConf(cfg, confPath); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(confPath)
	if string(got) != "# 我自己写的\n" {
		t.Fatalf("已有配置被覆盖:\n%s", got)
	}
	// 空文件视为未生成（历史遗留 0 字节文件不能挡住模板）
	if err := os.WriteFile(confPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureDockerConf(cfg, confPath); err != nil {
		t.Fatal(err)
	}
	if got, _ = os.ReadFile(confPath); !strings.Contains(string(got), "listen       80;") {
		t.Fatalf("空文件应被重新生成:\n%s", got)
	}
}

func TestEnsureDockerConfRefreshesGenerated(t *testing.T) {
	tmp := t.TempDir()
	confPath := filepath.Join(tmp, "nginx.conf")
	cfg := &InstanceConfig{ID: "inst-1", Name: "站点", Mode: ModeDocker, Image: "nginx:alpine", Port: 8080}
	if err := ensureDockerConf(cfg, confPath); err != nil {
		t.Fatal(err)
	}
	// 开启 HTTPS：未被接管的生成配置必须跟着补上 443 ssl 段，否则发布的端口没人服务
	cfg.SSLPort = 8443
	if err := ensureDockerConf(cfg, confPath); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(confPath)
	if !strings.Contains(string(got), "listen       443 ssl;") {
		t.Fatalf("开 HTTPS 后配置应刷新出 ssl 段:\n%s", got)
	}
	if !confIsToolGenerated(confPath, string(got)) {
		t.Fatal("刷新后应仍记录为工具生成")
	}
	// 关闭 HTTPS： ssl 段要消失，不然 nginx 因缺证书直接起不来（真机上出现过）
	cfg.SSLPort = 0
	if err := ensureDockerConf(cfg, confPath); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(confPath)
	if strings.Contains(string(got), "443 ssl") {
		t.Fatalf("关 HTTPS 后应去掉 ssl 段:\n%s", got)
	}
	// 用户改过（面板保存或手写）后不再覆盖
	edited := string(got) + "\n# 用户自己的 location\n"
	if err := os.WriteFile(confPath, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.SSLPort = 8443
	if err := ensureDockerConf(cfg, confPath); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(confPath)
	if string(got) != edited {
		t.Fatalf("用户接管的配置被覆盖了:\n%s", got)
	}
	if confIsToolGenerated(confPath, string(got)) {
		t.Fatal("修改后不应仍判为工具生成")
	}
}

func TestDockerPrecheckCerts(t *testing.T) {
	tmp := t.TempDir()
	confDir := filepath.Join(tmp, "conf")
	if err := os.MkdirAll(confDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &InstanceConfig{ID: "inst-1", Mode: ModeDocker, Image: "nginx:alpine", Port: 8080}
	// 没开 HTTPS：不查证书
	if err := dockerPrecheck(cfg, tmp); err != nil {
		t.Fatalf("未开 HTTPS 不该报错: %v", err)
	}
	cfg.SSLPort = 8443
	err := dockerPrecheck(cfg, tmp)
	if err == nil {
		t.Fatal("缺证书应报错，而不是让容器起来后 502 才发现")
	}
	if !strings.Contains(err.Error(), "server.crt") || !strings.Contains(err.Error(), "server.key") {
		t.Fatalf("报错应说清缺哪两个文件: %v", err)
	}
	for _, f := range []string{"server.crt", "server.key"} {
		if err := os.WriteFile(filepath.Join(confDir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// 证书齐了但配置里没有 443 ssl 段（用户接管后自己删了）：得告诉用户端口不会有人服务
	sslCfg := *cfg
	sslCfg.SSLPort = 0
	if err := ensureDockerConf(&sslCfg, filepath.Join(confDir, "nginx.conf")); err != nil {
		t.Fatal(err)
	}
	err = dockerPrecheck(cfg, tmp)
	if err == nil || !strings.Contains(err.Error(), "ssl server") {
		t.Fatalf("缺 ssl 段应报错并给出补法: %v", err)
	}
	// 配置里有 ssl 段（用工具带 HTTPS 重生成的那份）：预检通过
	if err := ensureDockerConf(cfg, filepath.Join(confDir, "nginx.conf")); err != nil {
		t.Fatal(err)
	}
	if err := dockerPrecheck(cfg, tmp); err != nil {
		t.Fatalf("证书齐且配置有 ssl 段不该报错: %v", err)
	}
}

func TestSpecSignatureChanges(t *testing.T) {
	prefix := filepath.Join(string(filepath.Separator), "prefix")
	docRoot := filepath.Join(string(filepath.Separator), "www", "site")
	sig := func(cfg *InstanceConfig) string {
		return specSignature(dockerSpecOf(cfg, prefix, docRoot))
	}
	base := &InstanceConfig{ID: "inst-1", Name: "站点甲", Mode: ModeDocker, Image: "nginx:alpine", Port: 8080, SSLPort: 8443}
	first := sig(base)
	cases := []struct {
		name string
		mut  func(c *InstanceConfig)
		want bool // true = 签名应一致（不重建）
	}{
		{"原样", func(c *InstanceConfig) {}, true},
		{"仅改实例名", func(c *InstanceConfig) { c.Name = "站点乙" }, true},
		{"改宿主 HTTP 端口", func(c *InstanceConfig) { c.Port = 8081 }, false},
		{"改宿主 HTTPS 端口", func(c *InstanceConfig) { c.SSLPort = 9443 }, false},
		{"关掉 HTTPS", func(c *InstanceConfig) { c.SSLPort = 0 }, false},
		{"换镜像", func(c *InstanceConfig) { c.Image = "nginx:1.25.5" }, false},
		{"自定义容器名", func(c *InstanceConfig) { c.ContainerName = "mine" }, false},
	}
	for _, c := range cases {
		mut := c.mut
		cfg := *base
		mut(&cfg)
		got := sig(&cfg) == first
		if got != c.want {
			t.Errorf("%s: 签名一致=%v, 期望 %v", c.name, got, c.want)
		}
	}
	// 站点根换了必须重建（挂载点变了）
	if specSignature(dockerSpecOf(base, prefix, `/www/other`)) == first {
		t.Error("换站点根后签名应失效")
	}
	// 实例目录换了（例如整体迁移数据目录）也要重建
	if specSignature(dockerSpecOf(base, filepath.Join(string(filepath.Separator), "prefix2"), docRoot)) == first {
		t.Error("换实例目录后签名应失效")
	}
}

func TestSpecSigRecordAndMatch(t *testing.T) {
	tmp := t.TempDir()
	if specMatchesLast(tmp, "sig-1") {
		t.Fatal("没记过就该判不一致（走重建）")
	}
	recordSpec(tmp, "sig-1")
	if !specMatchesLast(tmp, "sig-1") {
		t.Fatal("记录后应判一致")
	}
	if specMatchesLast(tmp, "sig-2") {
		t.Fatal("规格变了必须判不一致")
	}
	if specSigPath(tmp) != filepath.Join(tmp, "nginx-docker.container") {
		t.Fatalf("签名文件名不符: %s", specSigPath(tmp))
	}
}

func TestSaveInstanceModeFork(t *testing.T) {
	useTempData(t)
	// Docker 模式：不校验本机版本，镜像必填，Version 清空
	if err := SaveInstance(InstanceConfig{Name: "容器站", Mode: ModeDocker, Port: 18080}); err == nil {
		t.Fatal("Docker 模式缺镜像应报错")
	} else if !strings.Contains(err.Error(), "镜像") {
		t.Fatalf("应提示选择镜像: %v", err)
	}
	if err := SaveInstance(InstanceConfig{Name: "容器站", Mode: ModeDocker, Image: "nginx:alpine", Version: "1.27.4", Port: 18080}); err != nil {
		t.Fatalf("Docker 模式不该校验本机版本: %v", err)
	}
	list, err := ListInstances()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("实例数错误: %d", len(list))
	}
	cfg := list[0]
	if cfg.Mode != ModeDocker || cfg.Image != "nginx:alpine" || cfg.Version != "" {
		t.Fatalf("模式分叉结果错误: %+v", cfg)
	}
	if _, err := instanceDir(cfg.ID); err != nil {
		t.Fatal(err)
	}
	// 切回本地：必须已安装版本，且镜像字段被清掉
	cfg.Mode = ModeLocal
	cfg.Version = "9.99.99"
	if err := SaveInstance(cfg); err == nil || !strings.Contains(err.Error(), "未安装") {
		t.Fatalf("本地模式应校验版本已安装: %v", err)
	}
	cfg.ID = ""
	cfg.Mode = ""
	cfg.Image = "nginx:alpine"
	cfg.Version = ""
	if err := SaveInstance(cfg); err == nil || !strings.Contains(err.Error(), "版本") {
		t.Fatalf("空 Mode（历史配置）应按本地处理并要求版本: %v", err)
	}
}

func TestSaveInstancePortChecksApplyToDocker(t *testing.T) {
	useTempData(t)
	if err := SaveInstance(InstanceConfig{Name: "甲", Mode: ModeDocker, Image: "nginx:alpine", Port: 18099, SSLPort: 18099}); err == nil {
		t.Fatal("HTTPS 与 HTTP 同端口应拒绝")
	}
	if err := SaveInstance(InstanceConfig{Name: "甲", Mode: ModeDocker, Image: "nginx:alpine", Port: 18099, SSLPort: 70000}); err == nil {
		t.Fatal("端口越界应拒绝")
	}
	if err := SaveInstance(InstanceConfig{Name: "甲", Mode: ModeDocker, Image: "nginx:alpine", Port: 18099, DocRoot: filepath.Join("D:", "不存在的目录")}); err == nil {
		t.Fatal("站点根不存在应拒绝（容器模式挂不上同样起不来）")
	}
}

func TestRemoveInstanceDockerWithoutContainer(t *testing.T) {
	useTempData(t)
	id := saveDocker(t, "容器站", 18123)
	// 没有容器（引擎里查不到）时删除实例不能被卡住
	if err := RemoveInstance(id); err != nil {
		t.Fatalf("删除未起过容器的实例不该报错: %v", err)
	}
	list, err := ListInstances()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("实例未删除干净: %+v", list)
	}
}

func TestGetStatusDockerFieldsWithoutEngine(t *testing.T) {
	useTempData(t)
	id := saveDocker(t, "容器站", 18145)
	if err := SaveInstance(InstanceConfig{ID: id, Name: "容器站", Mode: ModeDocker, Image: "nginx:1.27.4-alpine", Port: 18145}); err != nil {
		t.Fatal(err)
	}
	st, err := GetInstanceStatus(id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode != ModeDocker {
		t.Fatalf("状态应标出 docker 模式: %+v", st)
	}
	if st.Image != "nginx:1.27.4-alpine" || st.Version != "1.27.4" {
		t.Fatalf("展示版本应来自镜像 tag: %+v", st)
	}
	if st.Container != "go-tools-nginx-"+id {
		t.Fatalf("容器名错误: %q", st.Container)
	}
	prefix, err := instanceDir(id)
	if err != nil {
		t.Fatal(err)
	}
	// 容器模式不按本机进程报 PID/worker，否则会把别的 nginx.exe 算进来
	if st.PID != 0 || st.Workers != 0 {
		t.Fatalf("容器模式不该有本机 PID/worker 数: %+v", st)
	}
	// 没起过容器时不该编造容器 ID；配置路径仍指向实例目录（面板里的「打开配置」靠它）
	if st.ContainerID != "" || st.ContainerState != "" {
		t.Fatalf("无容器时不该有容器 ID/状态: %+v", st)
	}
	if st.ConfigPath != filepath.Join(prefix, "conf", "nginx.conf") || st.Prefix != prefix {
		t.Fatalf("配置路径错误: %+v", st)
	}
}

func TestLogsDockerReadsHostFiles(t *testing.T) {
	useTempData(t)
	id := saveDocker(t, "容器站", 18167)
	prefix, err := instanceDir(id)
	if err != nil {
		t.Fatal(err)
	}
	// 日志目录挂载后由容器写，面板按宿主机文件读，与本地模式同一份数据
	if err := os.WriteFile(filepath.Join(prefix, "logs", "error.log"), []byte("err line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := Logs(id, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0] != "—— error.log ——" || out[1] != "err line" {
		t.Fatalf("日志拼接错误: %v", out)
	}
}

func TestDockerDocRootDefaultsToSharedWww(t *testing.T) {
	useTempData(t)
	cfg := &InstanceConfig{ID: "inst-9", Name: "站点丙", Mode: ModeDocker, Image: "nginx:alpine"}
	dir, err := dockerDocRoot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(appdirs.DataBase(), "www", "站点丙"); dir != want {
		t.Fatalf("默认站点根应与本地模式一致:\n got %s\nwant %s", dir, want)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("默认站点根应确保存在: %v", err)
	}
	// 显式配置优先，并归一为绝对路径
	abs := filepath.Join(t.TempDir(), "mine")
	cfg.DocRoot = abs + string(filepath.Separator)
	if got, err := dockerDocRoot(cfg); err != nil || got != abs {
		t.Fatalf("DocRoot 归一错误: got=%q err=%v", got, err)
	}
}

func TestCheckConfigDockerTellsLimitation(t *testing.T) {
	useTempData(t)
	id := saveDocker(t, "容器站", 18189)
	out, err := CheckConfig(id)
	if err == nil {
		t.Fatal("容器模式 nginx -t 必须明确失败，不能假装校验通过")
	}
	if !strings.Contains(out, "nginx -t") || !strings.Contains(out, "重载") {
		t.Fatalf("输出应说明能力边界与替代做法: %q", out)
	}
	if !strings.Contains(err.Error(), "nginx -t") {
		t.Fatalf("报错应说清不支持什么: %v", err)
	}
	// SaveConfigText 在无引擎时也不该因 nginx -t 失败而回滚用户内容
	if err := SaveConfigText(id, "user text\n"); err != nil {
		t.Fatalf("容器模式保存配置应成功（不做 -t 校验）: %v", err)
	}
	got, err := GetConfigText(id)
	if err != nil || got != "user text\n" {
		t.Fatalf("配置内容被改动: got=%q err=%v", got, err)
	}
}
