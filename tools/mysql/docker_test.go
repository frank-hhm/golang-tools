package mysql

// ============================================================
// Docker 运行模式的配置映射与校验单测
// （容器实际起停需真引擎，归属手工隔离验证；此处锁定"配置→容器参数"的换算）
// ============================================================

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-tools/appdirs"
)

// useTempData 把数据目录指到临时目录，避免污染本机实例登记
func useTempData(t *testing.T) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "data")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := appdirs.Set("", dir, filepath.Join(t.TempDir(), "logs")); err != nil {
		t.Fatal(err)
	}
}

func TestIsDockerMode(t *testing.T) {
	cases := []struct {
		mode string
		want bool
	}{
		{"", false}, // 历史配置无 mode 字段
		{"local", false},
		{"docker", true},
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
	if n := containerNameOf(&InstanceConfig{ID: "inst-7"}); n != "go-tools-mysql-inst-7" {
		t.Fatalf("默认容器名错误: %s", n)
	}
	if n := containerNameOf(&InstanceConfig{ID: "inst-7", ContainerName: "my-db"}); n != "my-db" {
		t.Fatalf("自定义容器名被覆盖: %s", n)
	}
}

func TestDockerSpecOf(t *testing.T) {
	cfg := &InstanceConfig{
		ID: "inst-1", Name: "本地库", Port: 3307, Image: "mysql:8.4",
		RootPassword: "secret", MaxConnections: 200,
	}
	// 数据目录用当前平台的绝对路径构造：Windows 上才需要把反斜杠转成 docker 认的正斜杠，
	// 硬编码 C:\... 会让同一个测试在 Linux 上因 filepath.ToSlash 不处理反斜杠而假失败。
	dataDir := filepath.Join(t.TempDir(), "mysql", "inst-1", "data")
	spec := dockerSpecOf(cfg, dataDir)

	if spec.Name != "go-tools-mysql-inst-1" || spec.Image != "mysql:8.4" {
		t.Fatalf("基础字段错误: %+v", spec)
	}
	if spec.ContainerPort != 3306 || spec.HostPort != 3307 || spec.HostIP != "127.0.0.1" {
		t.Fatalf("端口映射错误: %+v", spec)
	}
	wantBind := filepath.ToSlash(dataDir) + ":/var/lib/mysql"
	if len(spec.Binds) != 1 || spec.Binds[0] != wantBind {
		t.Fatalf("数据目录挂载应为容器路径正斜杠, 实际: %#v", spec.Binds)
	}
	if spec.Env[0] != "MYSQL_ROOT_PASSWORD=secret" {
		t.Fatalf("root 密码注入错误: %#v", spec.Env)
	}
	if spec.Labels["go-tools"] != "mysql" || spec.Labels["go-tools.instance"] != "inst-1" {
		t.Fatalf("归属标签错误: %#v", spec.Labels)
	}
	if len(spec.Cmd) != 1 || spec.Cmd[0] != "--max-connections=200" {
		t.Fatalf("max_connections 未传入容器: %#v", spec.Cmd)
	}

	// 空密码：与本地模式 --initialize-insecure 对齐，走官方镜像的允许空密码开关
	nopw := dockerSpecOf(&InstanceConfig{ID: "inst-2", Port: 3308, Image: "mysql:8.4"}, filepath.Join(t.TempDir(), "d"))
	if len(nopw.Env) != 1 || nopw.Env[0] != "MYSQL_ALLOW_EMPTY_PASSWORD=yes" {
		t.Fatalf("空密码应注入 MYSQL_ALLOW_EMPTY_PASSWORD, 实际: %#v", nopw.Env)
	}
	if len(nopw.Cmd) != 0 {
		t.Fatalf("未设 max_connections 时不应附加启动参数: %#v", nopw.Cmd)
	}
}

func TestSaveInstanceModeFork(t *testing.T) {
	useTempData(t)

	// Docker 模式：不校验本地版本，但镜像必填
	if err := SaveInstance(InstanceConfig{Name: "d1", Mode: "docker", Port: 3391}); err == nil {
		t.Fatal("Docker 模式缺镜像应报错")
	}
	if err := SaveInstance(InstanceConfig{Name: "d1", Mode: "docker", Port: 3391, Image: "mysql:8.4"}); err != nil {
		t.Fatalf("Docker 模式合法配置保存失败: %v", err)
	}
	list, err := ListInstances()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("实例数错误: %+v", list)
	}
	saved := list[0]
	if saved.Mode != ModeDocker {
		t.Fatalf("Mode 未归一: %q", saved.Mode)
	}
	if saved.ContainerName != "go-tools-mysql-"+saved.ID {
		t.Fatalf("容器名默认值错误: %q", saved.ContainerName)
	}

	// 本地模式：版本仍必须已安装
	if err := SaveInstance(InstanceConfig{Name: "l1", Port: 3392, Version: "9.9.9"}); err == nil {
		t.Fatal("本地模式未安装版本应报错")
	}
	if err := SaveInstance(InstanceConfig{Name: "l1", Port: 3392}); err == nil {
		t.Fatal("本地模式缺版本应报错")
	}
	// 本地模式端口非法
	if err := SaveInstance(InstanceConfig{Name: "l2", Mode: "local", Port: 70000, Version: "8.4.6"}); err == nil {
		t.Fatal("端口非法应报错")
	}
}

func TestSaveInstanceKeepsLegacyLocalMode(t *testing.T) {
	useTempData(t)
	// 模拟历史配置（无 mode 字段）落盘后再读出：应视为本地模式且不报错
	raw := `[{"id":"inst-old","name":"旧实例","version":"8.4.6","port":3306,"rootPassword":""}]`
	path, err := instancesFilePath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	list, err := ListInstances()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || isDockerMode(&list[0]) {
		t.Fatalf("历史配置应判为本地模式: %+v", list)
	}
}

func TestInstDirOfAndDataDir(t *testing.T) {
	useTempData(t)
	d := instDirOf("inst-9")
	if !strings.HasSuffix(d, filepath.Join("mysql-instances", "inst-9")) {
		t.Fatalf("实例目录错误: %s", d)
	}
	if !strings.HasSuffix(filepath.Join(d, "data"), filepath.Join("mysql-instances", "inst-9", "data")) {
		t.Fatalf("数据目录拼接错误: %s", d)
	}
}

func TestFindInstanceConfig(t *testing.T) {
	useTempData(t)
	if err := SaveInstance(InstanceConfig{Name: "d2", Mode: "docker", Port: 3393, Image: "mariadb:11"}); err != nil {
		t.Fatal(err)
	}
	list, _ := ListInstances()
	cfg, err := findInstanceConfig(list[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !isDockerMode(cfg) || cfg.Image != "mariadb:11" {
		t.Fatalf("按 ID 取配置错误: %+v", cfg)
	}
	if _, err := findInstanceConfig("nope"); err == nil {
		t.Fatal("不存在的实例应报错")
	}
}
