package redis

// ============================================================
// Redis Docker 运行模式的单测
//   - 锁定「实例配置 → 容器参数」的换算（镜像/端口/挂载/命令/标签/配置文件）
//   - 锁定 SaveInstance 的模式分叉与历史配置兼容
//   - 锁定规格签名：改端口/改密码这类变更必须触发重建
// 容器真实起停需要 Docker 引擎，属手工验证范畴，此处不触碰。
// 所有用例都把数据目录指向临时目录，绝不写本机 redis-instances.json。
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
	if n := containerNameOf(&InstanceConfig{ID: "inst-7"}); n != "go-tools-redis-inst-7" {
		t.Fatalf("默认容器名错误: %s", n)
	}
	if n := containerNameOf(&InstanceConfig{ID: "inst-7", ContainerName: " my-redis "}); n != "my-redis" {
		t.Fatalf("自定义容器名错误: %s", n)
	}
}

func TestVersionFromImage(t *testing.T) {
	cases := []struct{ image, want string }{
		{"redis:7.4.2-alpine", "7.4.2"},
		{"redis:7-alpine", "7"},
		{"redis:latest", ""},
		{"redis", ""},
		{"my.registry:5000/redis:6.2.14", "6.2.14"},
		{"valkey:8.0.1", "8.0.1"},
	}
	for _, c := range cases {
		if got := versionFromImage(c.image); got != c.want {
			t.Errorf("versionFromImage(%q) = %q, 期望 %q", c.image, got, c.want)
		}
	}
	if v := displayVersionOf(&InstanceConfig{Image: "redis:7.4.2-alpine"}); v != "7.4.2" {
		t.Fatalf("展示版本应取镜像 tag: %q", v)
	}
	// 镜像里没有版本号时退回镜像名，面板至少能看出跑的是哪个镜像
	if v := displayVersionOf(&InstanceConfig{Image: "redis:custom"}); v != "redis:custom" {
		t.Fatalf("镜像无版本号时应回退镜像名: %q", v)
	}
}

func TestBuildDockerConf(t *testing.T) {
	cfg := &InstanceConfig{Port: 46380, AppendOnly: true, MaxMemoryMB: 256, Password: "secret"}
	got := buildDockerConf(cfg)
	// 容器内端口固定 6379：实例端口靠发布映射，不写进 conf
	for _, want := range []string{
		"port 6379\n", "bind 0.0.0.0\n", "daemonize no\n", `logfile ""` + "\n",
		"dir /data\n", "appendonly yes\n", "maxmemory 256m\n", "maxmemory-policy noeviction\n",
		"requirepass secret\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("容器配置缺 %q，实际:\n%s", want, got)
		}
	}
	if strings.Contains(got, "port 46380") {
		t.Errorf("容器配置不该用宿主端口:\n%s", got)
	}

	// 未开启持久化/未限制内存/无密码时，对应指令都不该出现
	min := buildDockerConf(&InstanceConfig{})
	for _, bad := range []string{"appendonly yes", "maxmemory", "requirepass"} {
		if strings.Contains(min, bad) {
			t.Errorf("默认配置不应有 %q:\n%s", bad, min)
		}
	}
	if !strings.Contains(min, "appendonly no\n") {
		t.Errorf("appendonly 应显式关掉:\n%s", min)
	}
}

func TestDockerSpecOf(t *testing.T) {
	cfg := &InstanceConfig{ID: "inst-1", Image: "redis:7-alpine", Port: 46381}
	host := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(host, 0o755); err != nil {
		t.Fatal(err)
	}
	spec := dockerSpecOf(cfg, host)
	if spec.Name != "go-tools-redis-inst-1" || spec.Image != "redis:7-alpine" {
		t.Fatalf("容器名/镜像错误: %+v", spec)
	}
	if spec.ContainerPort != containerPortRedis || spec.HostPort != 46381 || spec.HostIP != "127.0.0.1" {
		t.Fatalf("端口发布错误: %+v", spec)
	}
	// Windows 路径必须转成正斜杠，否则 Docker 认不出挂载源
	wantBind := filepath.ToSlash(host) + ":/data"
	if len(spec.Binds) != 1 || spec.Binds[0] != wantBind {
		t.Fatalf("挂载错误: %#v, 期望 %q", spec.Binds, wantBind)
	}
	if spec.Labels[docker.LabelTool] != "redis" || spec.Labels[docker.LabelInstance] != "inst-1" {
		t.Fatalf("标签错误（重启后要靠它找回容器）: %#v", spec.Labels)
	}
	if strings.Join(spec.Cmd, " ") != "redis-server /data/go-tools-docker.conf" {
		t.Fatalf("容器内命令错误: %#v", spec.Cmd)
	}
}

func TestDockerDataDirDefaults(t *testing.T) {
	useTempData(t)
	cfg := &InstanceConfig{ID: "inst-dd"}
	dir, err := dockerDataDir(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(dir, filepath.Join("redis", "redis-instances", "inst-dd")) {
		t.Fatalf("默认数据目录位置错误: %s", dir)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("数据目录应已创建: %v", err)
	}
	// 指定了 DataDir 就用指定的（本地实例切成容器后，已有的 dump.rdb 要跟着走）
	own := filepath.Join(t.TempDir(), "my-redis")
	if got, err := dockerDataDir(&InstanceConfig{ID: "inst-dd", DataDir: own}); err != nil || got != own {
		t.Fatalf("自定义数据目录处理错误: %q %v", got, err)
	}
}

func TestSpecSignatureRebuildTrigger(t *testing.T) {
	useTempData(t)
	cfg := &InstanceConfig{ID: "inst-sig", Name: "签名", Mode: ModeDocker, Image: "redis:7-alpine", Port: 46382}
	dir, err := dockerDataDir(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// 签名 = 容器规格 + 生成的 redis.conf：两类变更都要能掀掉旧容器
	sigOf := func() string { return specSignature(dockerSpecOf(cfg, dir), buildDockerConf(cfg)) }
	sig := sigOf()
	if specMatchesLast(cfg, sig) {
		t.Fatal("还没创建过容器，不应算作一致")
	}
	recordSpec(cfg, sig)
	if !specMatchesLast(cfg, sig) {
		t.Fatal("记录后同样的规格应算一致")
	}
	// 换端口 / 换镜像：写进 ContainerSpec 的变更
	cfg.Port = 46383
	if specMatchesLast(cfg, sigOf()) {
		t.Fatal("改端口后规格签名应失效")
	}
	cfg.Port = 46382
	cfg.Image = "redis:8-alpine"
	if specMatchesLast(cfg, sigOf()) {
		t.Fatal("改镜像后规格签名应失效")
	}
	cfg.Image = "redis:7-alpine"
	// 改密码 / 开关 AOF / 改内存上限：只体现在生成的 redis.conf 上
	cfg.Password = "rotated"
	if specMatchesLast(cfg, sigOf()) {
		t.Fatal("改密码后规格签名应失效（容器里跑的还是旧 requirepass）")
	}
	cfg.Password = ""
	cfg.AppendOnly = true
	if specMatchesLast(cfg, sigOf()) {
		t.Fatal("开关 AOF 后规格签名应失效")
	}
	cfg.AppendOnly = false
	cfg.MaxMemoryMB = 128
	if specMatchesLast(cfg, sigOf()) {
		t.Fatal("改 maxmemory 后规格签名应失效")
	}
	// 只有名字这种不进规格也不进配置文件的变更，才允许复用容器
	cfg.MaxMemoryMB = 0
	cfg.Name = "改名了"
	if !specMatchesLast(cfg, sigOf()) {
		t.Fatal("实例改名不该触发容器重建")
	}
}

func TestSaveInstanceModeFork(t *testing.T) {
	useTempData(t)

	// Docker 模式缺镜像必须报错，而不是悄悄按本地起
	err := SaveInstance(InstanceConfig{Name: "无镜像", Mode: ModeDocker, Port: 46384})
	if err == nil || !strings.Contains(err.Error(), "镜像") {
		t.Fatalf("Docker 模式未给镜像应报错，实际: %v", err)
	}

	// 容器实例不查本机版本库：给了一个本机没装的 version 也应该能保存，并被清空
	if err := SaveInstance(InstanceConfig{
		ID: "inst-d", Name: "容器实例", Mode: "docker", Image: " redis:7-alpine ",
		Port: 46385, Version: "8.0.1",
	}); err != nil {
		t.Fatal(err)
	}
	list, err := ListInstances()
	if err != nil {
		t.Fatal(err)
	}
	var got *InstanceConfig
	for i := range list {
		if list[i].ID == "inst-d" {
			got = &list[i]
		}
	}
	if got == nil {
		t.Fatal("容器实例未落盘")
	}
	if got.Mode != ModeDocker || got.Version != "" {
		t.Fatalf("容器实例应归一化 mode 并清空本机版本: %+v", got)
	}

	// 历史配置（无 mode）保存后归一化为 local，且仍要求本机装了该版本
	if err := SaveInstance(InstanceConfig{Name: "本地实例", Port: 46386, Version: "9.9.9"}); err == nil {
		t.Fatal("本地模式选了未安装的版本应报错")
	}
}

func TestRemoveInstanceDockerWithoutContainer(t *testing.T) {
	useTempData(t)
	cfg := InstanceConfig{ID: "inst-rm", Name: "待删", Mode: ModeDocker, Image: "redis:7-alpine", Port: 46387}
	if err := SaveInstance(cfg); err != nil {
		t.Fatal(err)
	}
	dir, err := dockerDataDir(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	recordSpec(&cfg, "stub")
	if err := RemoveInstance(cfg.ID); err != nil {
		t.Fatalf("没有对应容器时删除实例不该失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, dockerSpecName)); !os.IsNotExist(err) {
		t.Fatal("规格签名文件应随实例删除一起作废")
	}
	list, err := ListInstances()
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range list {
		if c.ID == cfg.ID {
			t.Fatal("实例未被删除")
		}
	}
}

func TestGetStatusDockerFieldsWithoutEngine(t *testing.T) {
	useTempData(t)
	cfg := InstanceConfig{ID: "inst-st", Name: "容器", Mode: ModeDocker, Image: "redis:7.4.2-alpine", Port: 46388}
	if err := SaveInstance(cfg); err != nil {
		t.Fatal(err)
	}
	st, err := GetInstanceStatus(cfg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode != ModeDocker {
		t.Fatalf("状态应标出容器模式: %+v", st)
	}
	if st.Container != "go-tools-redis-inst-st" {
		t.Fatalf("容器名缺失: %+v", st)
	}
	if st.Version != "7.4.2" {
		t.Fatalf("展示版本应取自镜像 tag: %q", st.Version)
	}
	// 端口没人应答：运行中为 false，且不能编造 PID
	if st.Running || st.PID != 0 {
		t.Fatalf("未启动的容器实例不该报告在运行: %+v", st)
	}
}

func TestEnsureDockerConfRewritten(t *testing.T) {
	useTempData(t)
	cfg := &InstanceConfig{ID: "inst-conf", Password: "old"}
	dir, err := dockerDataDir(cfg)
	if err != nil {
		t.Fatal(err)
	}
	p, text, err := ensureDockerConf(cfg, dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != dockerConfName {
		t.Fatalf("配置文件名错误: %s", p)
	}
	if !strings.Contains(text, "requirepass old") {
		t.Fatalf("返回值应与落盘内容一致: %q", text)
	}
	cfg.Password = "new"
	if _, _, err := ensureDockerConf(cfg, dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "requirepass new") || strings.Contains(string(data), "requirepass old") {
		t.Fatalf("配置应每次启动按当前参数重写:\n%s", data)
	}
	// 落在数据目录里，因而会随挂载出现在容器的 /data 下
	if !strings.HasPrefix(p, dir) {
		t.Fatalf("配置应落在数据目录: %s vs %s", p, dir)
	}
}
