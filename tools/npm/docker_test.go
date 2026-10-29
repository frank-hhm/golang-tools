package npm

// ============================================================
// 静态站点 Docker 运行模式的单测
//   - 锁定「实例配置 → 容器参数」的换算：镜像/端口发布/挂载/命令/标签
//   - 锁定生成的 nginx.conf：listen 必须是容器端口、路径必须指挂载点、SPA 与列目录的指令映射
//   - 锁定 access.log 统计（容器模式没有内置服务器的原子计数器，请求数与流量靠日志还原）
//   - 锁定规格签名与 SaveInstance 的模式分叉
// 容器真实起停需要 Docker 引擎，属手工验证范畴，此处不触碰。
// 所有用例都把数据目录指向临时目录，绝不写本机 npm-instances.json。
// ============================================================

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Tools/appdirs"
	"Tools/tools/docker"
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
	cfg := InstanceConfig{Name: name, Mode: ModeDocker, Image: "nginx:alpine", Port: port}
	if err := SaveInstance(cfg); err != nil {
		t.Fatal(err)
	}
	list, err := ListInstances()
	if err != nil || len(list) == 0 {
		t.Fatalf("实例未落盘: %v", err)
	}
	return list[len(list)-1].ID
}

func TestNpmIsDockerMode(t *testing.T) {
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

func TestNpmContainerNameOf(t *testing.T) {
	if n := containerNameOf(&InstanceConfig{ID: "inst-7"}); n != "Tools-npm-inst-7" {
		t.Fatalf("默认容器名错误: %s", n)
	}
	if n := containerNameOf(&InstanceConfig{ID: "inst-7", ContainerName: " docs-site "}); n != "docs-site" {
		t.Fatalf("自定义容器名未生效: %s", n)
	}
}

func TestNpmVersionFromImage(t *testing.T) {
	cases := []struct{ image, want string }{
		{"nginx:1.27.4-alpine", "1.27.4"},
		{"nginx:1.27", "1.27"},
		{"nginx:alpine", ""},                // tag 不是版本号
		{"nginx:latest", ""},                // 同上
		{"", ""},                            // 空镜像
		{"a.com:5000/nginx:1.2.3", "1.2.3"}, // 带仓库端口的镜像
	}
	for _, c := range cases {
		if got := versionFromImage(c.image); got != c.want {
			t.Errorf("versionFromImage(%q) = %q, 期望 %q", c.image, got, c.want)
		}
	}
	// 取不到版本号时退回镜像名展示，避免出现空白的版本 tag
	if v := displayVersionOf(&InstanceConfig{Image: "nginx:alpine"}); v != "nginx:alpine" {
		t.Errorf("displayVersionOf 回退错误: %q", v)
	}
}

func TestNpmDockerBindsAndArgs(t *testing.T) {
	prefix := filepath.FromSlash("C:/data/npm/npm-instances/inst-1")
	docRoot := filepath.FromSlash("D:/www/dist")
	binds := dockerBinds(prefix, docRoot)
	want := []string{
		"C:/data/npm/npm-instances/inst-1/conf:/Tools/conf",
		"C:/data/npm/npm-instances/inst-1/logs:/var/log/nginx",
		"D:/www/dist:/usr/share/nginx/html",
	}
	if len(binds) != len(want) {
		t.Fatalf("挂载条数错误: %v", binds)
	}
	for i, w := range want {
		if binds[i] != w {
			t.Errorf("挂载[%d] = %q, 期望 %q", i, binds[i], w)
		}
	}
	for _, b := range binds {
		if strings.Contains(b, `\`) {
			t.Errorf("挂载串里不能留反斜杠（Docker 会把 \\ 当转义）: %s", b)
		}
	}
	// 挂的是 conf 目录本身：否则宿主机 nginx.conf 会落到 /Tools/conf/conf/nginx.conf，nginx 找不到配置
	args := dockerArgsOf()
	if strings.Join(args, " ") != `nginx -g daemon off; -c /Tools/conf/nginx.conf` {
		t.Fatalf("启动参数错误: %v", args)
	}
}

func TestNpmDockerSpecOf(t *testing.T) {
	cfg := &InstanceConfig{ID: "inst-1", Port: 3000, Image: " nginx:alpine ", Mode: ModeDocker}
	spec := dockerSpecOf(cfg, filepath.FromSlash("C:/p"), filepath.FromSlash("C:/site"))
	if spec.Name != "Tools-npm-inst-1" || spec.Image != "nginx:alpine" {
		t.Fatalf("容器名/镜像不对: %+v", spec)
	}
	if spec.ContainerPort != 80 || spec.HostPort != 3000 || spec.HostIP != "127.0.0.1" {
		t.Fatalf("端口发布不对: %+v", spec)
	}
	// 静态站点只有一个 HTTP 端口，不该带出第二端口
	if len(spec.ExtraPorts) != 0 {
		t.Fatalf("ExtraPorts 应为空: %+v", spec.ExtraPorts)
	}
	if spec.Labels[docker.LabelTool] != "npm" || spec.Labels[docker.LabelInstance] != "inst-1" {
		t.Fatalf("标签不对: %+v", spec.Labels)
	}
}

func TestNpmDockerConfText(t *testing.T) {
	cfg := &InstanceConfig{Port: 3000, SPA: true, Directory: true}
	text := dockerConfText(cfg)
	for _, want := range []string{
		"listen       80;", // 容器端口，不是宿主端口
		`root         "/usr/share/nginx/html";`,
		"autoindex on;",
		"try_files $uri $uri/ /index.html;",
		"location ~* \\.\\w+$ {",
		"try_files $uri =404;",
		"log_format gonfo",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("配置缺少 %q\n%s", want, text)
		}
	}
	// log_format 必须排在引用它的 access_log 之前，否则 nginx 解析期就报错
	if strings.Index(text, "log_format gonfo") > strings.Index(text, "access_log") {
		t.Errorf("log_format 必须写在 access_log 之前:\n%s", text)
	}
	// 宿主端口与宿主路径都不该出现在配置里
	if strings.Contains(text, "3000") {
		t.Errorf("配置里不该出现宿主端口:\n%s", text)
	}
	if strings.Contains(text, `C:\`) || strings.Contains(text, "c:/") {
		t.Errorf("配置里不该出现宿主机路径:\n%s", text)
	}
	// 探针是内置 Go 服务器的实现，容器里没有这一条路由
	if strings.Contains(text, "location = /__gonfo__") {
		t.Errorf("容器配置不该声明 /__gonfo__ 路由:\n%s", text)
	}

	// 关掉两个开关：既无 autoindex 也不回退 index.html
	plain := dockerConfText(&InstanceConfig{Port: 3000})
	if strings.Contains(plain, "autoindex") || strings.Contains(plain, "/index.html") {
		t.Errorf("默认配置不该带列目录与 SPA 回退:\n%s", plain)
	}
	if !strings.Contains(plain, "try_files $uri $uri/ =404;") {
		t.Errorf("默认配置应直接 404:\n%s", plain)
	}
	// 只开 SPA：回退但不列目录
	spaOnly := dockerConfText(&InstanceConfig{Port: 3000, SPA: true})
	if !strings.Contains(spaOnly, "try_files $uri $uri/ /index.html;") || strings.Contains(spaOnly, "autoindex") {
		t.Errorf("SPA 配置不对:\n%s", spaOnly)
	}
}

func TestNpmDockerConfPathCreatesDir(t *testing.T) {
	useTempData(t)
	id := saveDocker(t, "site-a", 3010)
	prefix, err := instanceDir(id)
	if err != nil {
		t.Fatal(err)
	}
	confPath, err := dockerConfPath(prefix)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(confPath) != "nginx.conf" || filepath.Base(filepath.Dir(confPath)) != "conf" {
		t.Fatalf("配置路径不对: %s", confPath)
	}
	if st, err := os.Stat(filepath.Dir(confPath)); err != nil || !st.IsDir() {
		t.Fatalf("conf 目录未创建: %v", err)
	}
}

func TestNpmEnsureDockerConfKeepsUserEdits(t *testing.T) {
	useTempData(t)
	cfg := &InstanceConfig{ID: "inst-1", Port: 3000, SPA: true}
	confPath := filepath.Join(t.TempDir(), "nginx.conf")
	if err := ensureDockerConf(cfg, confPath); err != nil {
		t.Fatal(err)
	}
	// 用户接管：手改一段
	if err := os.WriteFile(confPath, []byte("user wrote this"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.Directory = true
	if err := ensureDockerConf(cfg, confPath); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(confPath)
	if string(data) != "user wrote this" {
		t.Fatalf("用户改过的配置被覆盖了:\n%s", data)
	}
}

func TestNpmEnsureDockerConfRefreshesGenerated(t *testing.T) {
	useTempData(t)
	confPath := filepath.Join(t.TempDir(), "nginx.conf")
	cfg := &InstanceConfig{ID: "inst-1", Port: 3000, SPA: true, Directory: true}
	if err := ensureDockerConf(cfg, confPath); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(confPath)
	if !strings.Contains(string(first), "autoindex on;") {
		t.Fatalf("首次生成缺少列目录:\n%s", first)
	}
	// 关掉两个开关：仍是工具生成的那一份时要跟随刷新（否则面板拨了开关但容器行为是老的）
	cfg.SPA, cfg.Directory = false, false
	if err := ensureDockerConf(cfg, confPath); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(confPath)
	if strings.Contains(string(second), "autoindex") || strings.Contains(string(second), "/index.html") {
		t.Fatalf("开关关闭后配置未刷新:\n%s", second)
	}
	// 刷新后仍要与新内容对齐，否则下一次改参数会被误判成「用户接管」
	if !confIsToolGenerated(confPath, string(second)) {
		t.Fatal("刷新后生成标记未同步")
	}
}

func TestNpmDockerAccessStat(t *testing.T) {
	useTempData(t)
	prefix, err := instanceDir("inst-stat")
	if err != nil {
		t.Fatal(err)
	}
	// 生成的 log_format：'$remote_addr - [$time_local] "$request" $status $body_bytes_sent $request_time'
	// $request 里有空格，所以统计只能按行尾取数
	content := strings.Join([]string{
		`127.0.0.1 - [24/Oct/2026:10:00:00 +0800] "GET /index.html HTTP/1.1" 200 4949 0.001`,
		`127.0.0.1 - [24/Oct/2026:10:00:01 +0800] "GET /a b/c HTTP/1.1" 200 100 0.002`,
		`127.0.0.1 - [24/Oct/2026:10:00:02 +0800] "GET /miss HTTP/1.1" 304 - 0.000`,
		``,
	}, "\n")
	if err := os.WriteFile(filepath.Join(prefix, "logs", "access.log"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	req, bytes := dockerAccessStat(prefix)
	if req != 3 {
		t.Errorf("请求数 = %d, 期望 3", req)
	}
	if bytes != 5049 {
		t.Errorf("流量 = %d, 期望 5049（304 的 - 不计）", bytes)
	}
	// 日志还没落下来时不能报错，返回 0 让面板显示为空
	req, bytes = dockerAccessStat(filepath.Join(t.TempDir(), "none"))
	if req != 0 || bytes != 0 {
		t.Errorf("无日志时应为 0/0, 得到 %d/%d", req, bytes)
	}
}

func TestNpmSpecSignatureChanges(t *testing.T) {
	cfg := &InstanceConfig{ID: "inst-1", Name: "站点一", Port: 3000, Image: "nginx:alpine", SPA: true}
	prefix := filepath.FromSlash("C:/p/inst-1")
	docRoot := filepath.FromSlash("C:/site")
	base := specSignature(dockerSpecOf(cfg, prefix, docRoot))
	if specSignature(dockerSpecOf(cfg, prefix, docRoot)) != base {
		t.Fatal("同一配置签名不稳定")
	}
	// 只改实例名：容器参数没变，不必重建
	same := *cfg
	same.Name = "改了个名字"
	if specSignature(dockerSpecOf(&same, prefix, docRoot)) != base {
		t.Error("改实例名不该让容器重建")
	}
	// SPA / 列目录只改写挂载出去的配置，不进容器规格：改开关不需要重建，重启容器即可
	if specSignature(dockerSpecOf(&InstanceConfig{ID: "inst-1", Port: 3000, Image: "nginx:alpine"}, prefix, docRoot)) != base {
		t.Error("开关变化不该触发容器重建（配置靠挂载带进去，重启即生效）")
	}
	changes := []struct {
		desc string
		mut  func(c *InstanceConfig, p, d string) docker.ContainerSpec
	}{
		{"改端口", func(c *InstanceConfig, p, d string) docker.ContainerSpec {
			c.Port = 3001
			return dockerSpecOf(c, p, d)
		}},
		{"换镜像", func(c *InstanceConfig, p, d string) docker.ContainerSpec {
			c.Image = "nginx:1.27.4-alpine"
			return dockerSpecOf(c, p, d)
		}},
		{"换容器名", func(c *InstanceConfig, p, d string) docker.ContainerSpec {
			c.ContainerName = "manual-name"
			return dockerSpecOf(c, p, d)
		}},
		{"换站点根", func(c *InstanceConfig, p, d string) docker.ContainerSpec {
			return dockerSpecOf(c, p, filepath.FromSlash("C:/other-site"))
		}},
		{"换实例目录", func(c *InstanceConfig, p, d string) docker.ContainerSpec {
			return dockerSpecOf(c, filepath.FromSlash("C:/p/inst-2"), d)
		}},
	}
	for _, ch := range changes {
		got := specSignature(ch.mut(cfg, prefix, docRoot))
		if got == base {
			t.Errorf("%s 后签名未变，容器不会被重建", ch.desc)
		}
		*cfg = InstanceConfig{ID: "inst-1", Name: "站点一", Port: 3000, Image: "nginx:alpine", SPA: true}
	}
}

func TestNpmSpecSigRecordAndMatch(t *testing.T) {
	useTempData(t)
	prefix := t.TempDir()
	if specMatchesLast(prefix, "abc") {
		t.Fatal("没记过签名不该算一致")
	}
	recordSpec(prefix, "abc")
	if !specMatchesLast(prefix, "abc") {
		t.Fatal("记录后应判为一致")
	}
	if specMatchesLast(prefix, "abd") {
		t.Fatal("签名不同不该算一致")
	}
}

func TestNpmSaveInstanceModeFork(t *testing.T) {
	useTempData(t)
	// Docker 模式：镜像必填，Node 版本被清空（本机版本库不参与容器实例）
	if err := SaveInstance(InstanceConfig{Name: "no-image", Mode: ModeDocker, Port: 3100}); err == nil {
		t.Fatal("Docker 模式缺镜像应当被拒")
	}
	if err := SaveInstance(InstanceConfig{Name: "docker-1", Mode: ModeDocker, Image: "nginx:alpine", Version: "22.14.0", Port: 3101}); err != nil {
		t.Fatal(err)
	}
	list, _ := ListInstances()
	got := list[len(list)-1]
	if got.Mode != ModeDocker || got.Image != "nginx:alpine" || got.Version != "" {
		t.Fatalf("容器实例落盘字段不对: %+v", got)
	}
	// 本地模式：不选版本就拒绝；带进来的镜像/容器名要清掉，避免两份模式字段混在一起
	if err := SaveInstance(InstanceConfig{Name: "local-1", Port: 3102}); err == nil {
		t.Fatal("本地模式未选 Node 版本应当被拒")
	}
	if err := SaveInstance(InstanceConfig{Name: "local-2", Port: 3103, Mode: "local", Image: "nginx:alpine", ContainerName: "left-over"}); err == nil {
		t.Fatal("本地模式要求版本，未选应当被拒")
	}
	// 站点根目录不存在：两种模式都拒
	if err := SaveInstance(InstanceConfig{Name: "bad-root", Mode: ModeDocker, Image: "nginx:alpine", Port: 3104,
		DocRoot: filepath.Join(t.TempDir(), "not-exist")}); err == nil {
		t.Fatal("站点根目录不存在应当被拒")
	}
}

func TestNpmRemoveInstanceDockerWithoutContainer(t *testing.T) {
	useTempData(t)
	// 引擎在不在都不该拦住删除：容器查不到就是没起过，直接删配置
	id := saveDocker(t, "docker-del", 3110)
	if err := RemoveInstance(id); err != nil {
		t.Fatalf("删除容器实例失败: %v", err)
	}
	list, _ := ListInstances()
	for _, c := range list {
		if c.ID == id {
			t.Fatal("实例未被删除")
		}
	}
}

func TestNpmStatusDockerFieldsWithoutEngine(t *testing.T) {
	useTempData(t)
	id := saveDocker(t, "docker-st", 3120)
	st, err := GetInstanceStatus(id)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode != ModeDocker || st.Image != "nginx:alpine" {
		t.Fatalf("状态里的模式/镜像不对: %+v", st)
	}
	if st.Container != "Tools-npm-"+id {
		t.Fatalf("容器名不对: %s", st.Container)
	}
	// 展示版本从镜像 tag 推导；alpine 不是版本号，回退镜像名
	if st.Version != "nginx:alpine" {
		t.Fatalf("展示版本不对: %s", st.Version)
	}
	if st.NodePath != "" || st.NpmVer != "" || st.Registry != "" {
		t.Fatalf("容器模式不该报 Node 相关字段: %+v", st)
	}
	if st.PID != 0 {
		t.Fatalf("容器模式不该报本机 PID: %d", st.PID)
	}
	if st.Running {
		t.Fatal("没起容器不该报运行中")
	}
	if st.DocRoot == "" {
		t.Fatal("默认站点根目录应指到实例目录下的 www")
	}
}

func TestNpmLogsDockerReadsHostFiles(t *testing.T) {
	useTempData(t)
	id := saveDocker(t, "docker-log", 3130)
	prefix, err := instanceDir(id)
	if err != nil {
		t.Fatal(err)
	}
	line := `127.0.0.1 - [24/Oct/2026:10:00:00 +0800] "GET / HTTP/1.1" 200 49 0.001`
	if err := os.WriteFile(filepath.Join(prefix, "logs", "access.log"), []byte(line+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Logs(id, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != line {
		t.Fatalf("日志应按文件读: %v", got)
	}
}

func TestNpmDockerDocRootDefaultsToWww(t *testing.T) {
	useTempData(t)
	id := saveDocker(t, "docker-root", 3140)
	cfg, err := findInstance(id)
	if err != nil {
		t.Fatal(err)
	}
	dir, err := dockerDocRoot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(dir) != "www" {
		t.Fatalf("默认站点根应为实例目录下 www，得到 %s", dir)
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		t.Fatalf("站点根未创建: %v", err)
	}
	// 显式填了就用填的
	site := t.TempDir()
	cfg.DocRoot = site
	if got, err := dockerDocRoot(cfg); err != nil || got != filepath.Clean(site) {
		t.Fatalf("自定义站点根未生效: %s (%v)", got, err)
	}
}
