package php

// ============================================================
// PHP Docker 运行模式的单测
//   - 锁定「实例配置 → 容器参数」的换算（镜像/端口/挂载/命令/标签）
//   - 锁定 SaveInstance 的模式分叉与历史配置兼容
//   - 锁定容器实例的配置读写（覆盖 ini）与扩展/phpinfo 的能力门禁
// 容器真实起停需要 Docker 引擎，属手工验证范畴，此处不触碰。
// 所有用例都把数据目录指向临时目录，绝不写本机 php-instances.json。
// ============================================================

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-tools/appdirs"
)

// useTempData 把数据/日志目录指到临时目录，避免污染本机实例登记（用例结束复原，不影响同包其它测试）
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
	if n := containerNameOf(&InstanceConfig{ID: "inst-7"}); n != "go-tools-php-inst-7" {
		t.Fatalf("默认容器名错误: %s", n)
	}
	if n := containerNameOf(&InstanceConfig{ID: "inst-7", ContainerName: " my-php "}); n != "my-php" {
		t.Fatalf("自定义容器名错误: %s", n)
	}
}

func TestVersionFromImage(t *testing.T) {
	cases := []struct{ image, want string }{
		{"php:8.3.12-cli", "8.3.12"},
		{"php:8.3-cli", "8.3"},
		{"php:8", ""},                       // 单段号不算版本
		{"php:latest", ""},                  // 无版本 tag
		{"php", ""},                         // 连 tag 都没有
		{"my.registry:5000/php:7.4", "7.4"}, // 带仓库端口的镜像名
		{"php:8.3.12-apache", "8.3.12"},
	}
	for _, c := range cases {
		if got := versionFromImage(c.image); got != c.want {
			t.Errorf("versionFromImage(%q) = %q, 期望 %q", c.image, got, c.want)
		}
	}
	if v := displayVersionOf(&InstanceConfig{Image: "php:8.2-cli"}); v != "8.2" {
		t.Fatalf("展示版本应取镜像 tag: %q", v)
	}
	if v := displayVersionOf(&InstanceConfig{Image: "php:custom"}); v != "php:custom" {
		t.Fatalf("镜像无版本号时应回退镜像名: %q", v)
	}
}

func TestContainerRouterPath(t *testing.T) {
	docRoot := t.TempDir()

	if got, err := containerRouterPath(&InstanceConfig{}, docRoot); err != nil || got != "" {
		t.Fatalf("未设路由应为空: %q %v", got, err)
	}
	if got, err := containerRouterPath(&InstanceConfig{Router: "router.php"}, docRoot); err != nil || got != containerDocRootPath+"/router.php" {
		t.Fatalf("相对路由换算错误: %q %v", got, err)
	}
	if got, err := containerRouterPath(&InstanceConfig{Router: "./inc/router.php"}, docRoot); err != nil || got != containerDocRootPath+"/inc/router.php" {
		t.Fatalf("子目录路由换算错误: %q %v", got, err)
	}
	// 站点目录内的绝对路径 → 折成容器内相对路径
	inside := filepath.Join(docRoot, "router.php")
	if got, err := containerRouterPath(&InstanceConfig{Router: inside}, docRoot); err != nil || got != containerDocRootPath+"/router.php" {
		t.Fatalf("站点内绝对路径应可换算: %q %v", got, err)
	}
	// 站点目录外的绝对路径 → 容器里取不到，必须报错而不是静默忽略
	outside := filepath.Join(t.TempDir(), "router.php")
	if _, err := containerRouterPath(&InstanceConfig{Router: outside}, docRoot); err == nil {
		t.Fatal("站点目录外的路由脚本应报错")
	}
}

func TestDockerArgsOf(t *testing.T) {
	args := dockerArgsOf(&InstanceConfig{Port: 8090}, "")
	want := []string{"php", "-S", "0.0.0.0:8080", "-t", containerDocRootPath}
	if strings.Join(args, " ") != strings.Join(want, " ") {
		t.Fatalf("容器内命令错误: %#v", args)
	}
	withRouter := dockerArgsOf(&InstanceConfig{Port: 8090}, containerDocRootPath+"/router.php")
	if withRouter[len(withRouter)-1] != containerDocRootPath+"/router.php" {
		t.Fatalf("路由参数未追加: %#v", withRouter)
	}
}

func TestDockerSpecOf(t *testing.T) {
	docRoot, ini := t.TempDir(), filepath.Join(t.TempDir(), "php-docker.ini")
	cfg := &InstanceConfig{
		ID: "inst-1", Name: "容器站", Mode: ModeDocker, Image: "php:8.3-cli",
		Port: 8091, Router: "router.php",
	}
	spec, err := dockerSpecOf(cfg, docRoot, ini)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Name != "go-tools-php-inst-1" || spec.Image != "php:8.3-cli" {
		t.Fatalf("基础字段错误: %+v", spec)
	}
	// 容器内固定 8080，宿主只发布到 127.0.0.1（与本地模式一样不对外暴露）
	if spec.ContainerPort != containerPortPHP || spec.HostPort != 8091 || spec.HostIP != "127.0.0.1" {
		t.Fatalf("端口映射错误: %+v", spec)
	}
	wantDoc := filepath.ToSlash(docRoot) + ":" + containerDocRootPath
	wantIni := filepath.ToSlash(ini) + ":" + containerIniPath + ":ro"
	if len(spec.Binds) != 2 || spec.Binds[0] != wantDoc || spec.Binds[1] != wantIni {
		t.Fatalf("挂载应为站点目录 + 只读覆盖 ini, 实际: %#v", spec.Binds)
	}
	if spec.Labels["go-tools"] != "php" || spec.Labels["go-tools.instance"] != "inst-1" {
		t.Fatalf("归属标签错误: %#v", spec.Labels)
	}
	if len(spec.Cmd) != 6 || spec.Cmd[5] != containerDocRootPath+"/router.php" {
		t.Fatalf("容器内命令错误: %#v", spec.Cmd)
	}

	// 站点外的路由脚本在建容器前就该失败（SaveInstance 已拦，这里双保险）
	outside := &InstanceConfig{ID: "inst-2", Image: "php:8.3-cli", Port: 8092, Router: filepath.Join(t.TempDir(), "r.php")}
	if _, err := dockerSpecOf(outside, docRoot, ini); err == nil {
		t.Fatal("路由在站点目录外应报错")
	}
}

func TestSaveInstanceModeFork(t *testing.T) {
	useTempData(t)

	// Docker：镜像必填，不校验本机版本
	if err := SaveInstance(InstanceConfig{Name: "d0", Mode: "docker", Port: 18091}); err == nil {
		t.Fatal("Docker 模式缺镜像应报错")
	}
	if err := SaveInstance(InstanceConfig{Name: "d1", Mode: "docker", Port: 18091, Image: "php:8.3-cli", Version: "8.2.20"}); err != nil {
		t.Fatalf("Docker 模式合法配置保存失败: %v", err)
	}
	list, err := ListInstances()
	if err != nil {
		t.Fatal(err)
	}
	var saved *InstanceConfig
	for i := range list {
		if list[i].Name == "d1" {
			saved = &list[i]
		}
	}
	if saved == nil {
		t.Fatalf("实例未落盘: %+v", list)
	}
	if saved.Mode != ModeDocker {
		t.Fatalf("Mode 未归一: %q", saved.Mode)
	}
	if saved.Version != "" {
		t.Fatalf("Docker 实例不应保留本机版本: %q", saved.Version)
	}
	if saved.ContainerName != "go-tools-php-"+saved.ID {
		t.Fatalf("容器名默认值错误: %q", saved.ContainerName)
	}

	// 本地：仍要求选版本且已安装
	if err := SaveInstance(InstanceConfig{Name: "l1", Port: 18092}); err == nil {
		t.Fatal("本地模式缺版本应报错")
	}
	if err := SaveInstance(InstanceConfig{Name: "l1", Mode: "local", Port: 18092, Version: "9.9.9"}); err == nil {
		t.Fatal("本地模式未安装版本应报错")
	}
	if err := SaveInstance(InstanceConfig{Name: "l2", Port: 70000, Version: "8.2.20"}); err == nil {
		t.Fatal("端口非法应报错")
	}
}

func TestSaveInstanceKeepsLegacyLocalMode(t *testing.T) {
	useTempData(t)
	// 历史配置（无 mode 字段）落盘后读出：应视为本地模式，且不会被容器逻辑接管
	path, err := instancesFilePath()
	if err != nil {
		t.Fatal(err)
	}
	raw := `[{"id":"inst-old","name":"旧实例","version":"8.2.20","nts":true,"port":18097,"docRoot":""}]`
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

func TestSaveInstanceDockerRouterPrecheck(t *testing.T) {
	useTempData(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "router.php"), []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// 站点内路由：保存成功
	if err := SaveInstance(InstanceConfig{Name: "ok", Mode: ModeDocker, Image: "php:8.3-cli", Port: 18094, DocRoot: root, Router: "router.php"}); err != nil {
		t.Fatalf("站点内路由应可保存: %v", err)
	}
	// 站点外绝对路径路由：不留到启动时才爆
	outside := filepath.Join(t.TempDir(), "router.php")
	if err := os.WriteFile(outside, []byte("<?php\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveInstance(InstanceConfig{Name: "bad", Mode: ModeDocker, Image: "php:8.3-cli", Port: 18095, DocRoot: root, Router: outside}); err == nil {
		t.Fatal("站点目录外的路由脚本保存时应报错")
	}
}

func TestDockerConfigReadWrite(t *testing.T) {
	useTempData(t)
	if err := SaveInstance(InstanceConfig{Name: "d2", Mode: ModeDocker, Image: "php:8.3-cli", Port: 18096}); err != nil {
		t.Fatal(err)
	}
	list, _ := ListInstances()
	id := list[0].ID

	// ini 路径落实例目录，而不是去找本机 php.exe（后者会被判"版本不可用"）
	p, err := iniPathOf(id)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "php-docker.ini" {
		t.Fatalf("Docker 实例配置名错误: %s", p)
	}

	text, err := GetConfigText(id)
	if err != nil {
		t.Fatalf("生成覆盖配置失败: %v", err)
	}
	if !strings.Contains(text, "display_errors") || !strings.Contains(text, containerIniPath) {
		t.Fatalf("覆盖配置应含默认项与挂载说明:\n%s", text)
	}
	// 禁用函数在容器模式下同样可编辑（就是改那份挂载进 conf.d 的文件）
	if err := SetDisableFunctions(id, []string{"exec", "passthru"}); err != nil {
		t.Fatalf("Docker 实例设置 disable_functions 失败: %v", err)
	}
	got, err := DisableFunctions(id)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "exec,passthru" {
		t.Fatalf("disable_functions 回读错误: %#v", got)
	}
}

func TestDockerCapabilityGates(t *testing.T) {
	useTempData(t)
	if err := SaveInstance(InstanceConfig{Name: "d3", Mode: ModeDocker, Image: "php:8.3-cli", Port: 18098}); err != nil {
		t.Fatal(err)
	}
	list, _ := ListInstances()
	id := list[0].ID

	// 容器实例没有宿主 php.exe，也没有 Windows dll：这些入口要给出带原因的门禁错误
	if _, err := Extensions(id); err == nil || !strings.Contains(err.Error(), "仅适用于本地实例") {
		t.Fatalf("扩展列表应被门禁: %v", err)
	}
	if _, err := SetExtension(id, "curl", true); err == nil || !strings.Contains(err.Error(), "仅适用于本地实例") {
		t.Fatalf("扩展开关应被门禁: %v", err)
	}
	if _, err := LoadedExtensions(id); err == nil || !strings.Contains(err.Error(), "仅适用于本地实例") {
		t.Fatalf("php -m 应被门禁: %v", err)
	}
	if _, err := Info(id); err == nil || !strings.Contains(err.Error(), "仅适用于本地实例") {
		t.Fatalf("phpinfo 应被门禁: %v", err)
	}
	if _, err := ExtCatalog(id, false); err == nil || !strings.Contains(err.Error(), "仅适用于本地实例") {
		t.Fatalf("扩展目录应被门禁: %v", err)
	}
	if err := InstallExtension(id, "curl", true); err == nil || !strings.Contains(err.Error(), "仅适用于本地实例") {
		t.Fatalf("扩展安装应被门禁: %v", err)
	}
	if err := RemoveExtension(id, "curl"); err == nil || !strings.Contains(err.Error(), "仅适用于本地实例") {
		t.Fatalf("扩展卸载应被门禁: %v", err)
	}
	if _, err := SearchExtension(id, "curl"); err == nil || !strings.Contains(err.Error(), "仅适用于本地实例") {
		t.Fatalf("扩展搜索应被门禁: %v", err)
	}

	// 门禁不该波及本地实例（这里用不存在的版本，错误应是"未安装"而非门禁）
	if err := SaveInstance(InstanceConfig{Name: "l3", Port: 18099, Version: "8.2.20"}); err == nil {
		t.Fatal("未安装的版本不应保存成功")
	}
}

func TestDockerSpecSignature(t *testing.T) {
	useTempData(t)
	cfg := &InstanceConfig{ID: "inst-s", Mode: ModeDocker, Image: "php:8.3-cli", Port: 18100}
	rootA, iniA := t.TempDir(), filepath.Join(t.TempDir(), "php-docker.ini")

	specA, err := dockerSpecOf(cfg, rootA, iniA)
	if err != nil {
		t.Fatal(err)
	}
	sigA := specSignature(specA)
	if specMatchesLast(cfg, sigA) {
		t.Fatal("未记录过规格时应判为需重建")
	}
	recordSpec(cfg, sigA)
	if !specMatchesLast(cfg, sigA) {
		t.Fatal("记录后同样规格应判为一致")
	}

	// 端口变更（命令与创建参数里都带宿主端口）
	moved := *cfg
	moved.Port = 18101
	specMoved, err := dockerSpecOf(&moved, rootA, iniA)
	if err != nil {
		t.Fatal(err)
	}
	if specMatchesLast(&moved, specSignature(specMoved)) {
		t.Fatal("端口变更应判为需重建")
	}

	// 换站点目录：挂载变了，仅比镜像/端口是看不出来的
	rootB := t.TempDir()
	specRoot, err := dockerSpecOf(cfg, rootB, iniA)
	if err != nil {
		t.Fatal(err)
	}
	if specMatchesLast(cfg, specSignature(specRoot)) {
		t.Fatal("站点目录变更应判为需重建")
	}

	// 换路由：容器内命令变
	routed := *cfg
	routed.Router = "another.php"
	specRouter, err := dockerSpecOf(&routed, rootA, iniA)
	if err != nil {
		t.Fatal(err)
	}
	if specMatchesLast(&routed, specSignature(specRouter)) {
		t.Fatal("路由变更应判为需重建")
	}

	// 删容器同时作废规格签名（下次肯定重建）
	if err := dockerRemove(cfg); err != nil {
		t.Fatalf("无容器时删除应静默通过: %v", err)
	}
	if specMatchesLast(cfg, sigA) {
		t.Fatal("删除容器后规格签名应被清掉")
	}
}

func TestDockerDocRootDefault(t *testing.T) {
	useTempData(t)
	cfg := &InstanceConfig{ID: "inst-x", Name: "站点 名字", Mode: ModeDocker, Image: "php:8.3-cli"}
	root, err := dockerDocRoot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(root, filepath.Join(appdirs.DataBase(), "www", "站点 名字")) {
		t.Fatalf("默认站点根应落在数据目录 www 下: %s", root)
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		t.Fatalf("站点根应被创建: %v", err)
	}
	// 指定 DocRoot 时按其绝对化使用
	abs := filepath.Join(t.TempDir(), "web")
	cfg2 := &InstanceConfig{ID: "inst-y", Mode: ModeDocker, DocRoot: abs}
	got, err := dockerDocRoot(cfg2)
	if err != nil || got != abs {
		t.Fatalf("指定站点根被改写: %q vs %q (%v)", got, abs, err)
	}
}
