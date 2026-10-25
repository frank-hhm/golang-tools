package php

// ============================================================
// PHP 实例的 Docker 运行模式（经 tools/docker 的 Engine API）
//
// 与本地模式的对应关系：
//   - 服务器：容器内跑同一条 php -S（监听 0.0.0.0:8080），端口发布到
//     127.0.0.1:<cfg.Port>，浏览器与 nginx 反代的访问方式与本地模式一致
//     （容器内 0.0.0.0 只是让 Docker 能把流量转进来，宿主机仍只绑 127.0.0.1）
//   - 站点：cfg.DocRoot 绑定挂载到 /var/www/html，改文件即时生效，无需重建容器
//   - 配置：实例目录下 php-docker.ini 挂到 /usr/local/etc/php/conf.d/，
//     以「追加覆盖」生效（Linux 语义，与插件目录里那份 Windows php.ini 互不相干）
//   - 归属：容器打标签 go-tools=php / go-tools.instance=<id>，应用重启后凭标签找回
//   - 日志：php -S 的访问与错误输出都在容器 stdout/stderr，走 docker logs
//
// 与本地模式的差异（如实告知，不做静默兜底）：
//   - 扩展管理不可用：面板的扩展开关操作的是 Windows 插件目录下的 php_*.dll，
//     容器里既没有这些文件，也不做 docker-php-ext-install 编译
//   - phpinfo 不可用：tools/docker 未实现 exec，取不到容器内 php -i
//   - 路由脚本只能是站点目录内的相对路径（宿主绝对路径在容器里无意义）
//   - 站点目录跨文件系统挂载，IO 慢于本地磁盘
//
// 变更生效：创建容器时把规格（镜像/端口/挂载/命令/标签）的签名记在实例目录，
// 下次启动不一致即重建容器（站点与配置都在宿主机目录，重建不丢数据）。
// ============================================================

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go-tools/tools/docker"
)

const (
	// ModeLocal 本地模式（默认，历史配置无 mode 字段即此值）
	ModeLocal = "local"
	// ModeDocker 容器模式
	ModeDocker = "docker"
	// containerPortPHP 容器内 php -S 监听端口
	containerPortPHP = 8080
	// containerDocRootPath 容器内站点根
	containerDocRootPath = "/var/www/html"
	// containerIniPath 容器内追加配置（官方镜像会加载 conf.d/*.ini）
	containerIniPath = "/usr/local/etc/php/conf.d/zz-go-tools.ini"
	// DefaultDockerImage 表单默认镜像：-cli 变体与本地 php -S 语义最接近
	DefaultDockerImage = "php:8.3-cli"
)

// isDockerMode 判定实例运行模式（零值/其他值一律按本地）
func isDockerMode(cfg *InstanceConfig) bool {
	return strings.EqualFold(strings.TrimSpace(cfg.Mode), ModeDocker)
}

// dockerGateError Docker 模式下的能力边界：不可用的功能给出带原因的报错
func dockerGateError(what string) error {
	return fmt.Errorf("%s仅适用于本地实例；Docker 实例的 PHP 与扩展由镜像提供，容器内配置请在「php.ini」页以覆盖项方式修改", what)
}

// containerNameOf 容器名（留空则按实例 ID 生成，保证唯一且可反查）
func containerNameOf(cfg *InstanceConfig) string {
	if n := strings.TrimSpace(cfg.ContainerName); n != "" {
		return n
	}
	return "go-tools-php-" + cfg.ID
}

// dockerDocRoot 站点根（宿主机绝对路径，确保存在）
func dockerDocRoot(cfg *InstanceConfig) (string, error) {
	root := strings.TrimSpace(cfg.DocRoot)
	if root == "" {
		root = defaultDocRoot(cfg.ID, cfg.Name)
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("站点目录不合法: %s（%v）", root, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("站点目录不可用: %s（%v）", abs, err)
	}
	return abs, nil
}

// dockerIniPath 容器专用追加配置（宿主机路径，不创建）
func dockerIniPath(cfg *InstanceConfig) (string, error) {
	dir, err := instanceDir(cfg.ID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "php-docker.ini"), nil
}

// ensureDockerIni 追加配置不存在时写一份常用默认（覆盖镜像值用，不必抄整份 php.ini）
func ensureDockerIni(cfg *InstanceConfig) (string, error) {
	p, err := dockerIniPath(cfg)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Size() > 0 {
		return p, nil
	}
	if err := os.WriteFile(p, []byte(dockerIniTemplate()), 0o644); err != nil {
		return "", err
	}
	return p, nil
}

func dockerIniTemplate() string {
	return `; go-tools 为 Docker 实例生成的覆盖配置
; 挂载进容器：` + containerIniPath + `
; 这里只写需要覆盖镜像默认值的指令，未列出的项沿用镜像内 php.ini-production
; 注意：extension= 指向的是容器内的扩展，本面板的「扩展管理」管不到这里
[PHP]
display_errors = On
error_reporting = E_ALL
date.timezone = Asia/Shanghai
memory_limit = 256M
max_execution_time = 120
post_max_size = 64M
upload_max_filesize = 64M
`
}

// containerRouterPath 路由脚本换成容器内路径：只接受站点目录内的相对路径
func containerRouterPath(cfg *InstanceConfig, hostDocRoot string) (string, error) {
	r := strings.TrimSpace(cfg.Router)
	if r == "" {
		return "", nil
	}
	if filepath.IsAbs(r) {
		rel, err := filepath.Rel(hostDocRoot, r)
		if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return "", fmt.Errorf("Docker 模式的路由脚本必须位于站点目录内（当前: %s）", r)
		}
		r = rel
	}
	r = filepath.ToSlash(strings.TrimPrefix(filepath.ToSlash(filepath.Clean(r)), "./"))
	return containerDocRootPath + "/" + r, nil
}

// dockerArgsOf 容器内 php 的完整参数（entrypoint 是 docker-php-entrypoint，直接 exec 参数）
func dockerArgsOf(cfg *InstanceConfig, routerIn string) []string {
	args := []string{"php", "-S", fmt.Sprintf("0.0.0.0:%d", containerPortPHP), "-t", containerDocRootPath}
	if routerIn != "" {
		args = append(args, routerIn)
	}
	return args
}

// dockerSpecOf 由实例配置生成容器创建参数
func dockerSpecOf(cfg *InstanceConfig, hostDocRoot, hostIni string) (docker.ContainerSpec, error) {
	routerIn, err := containerRouterPath(cfg, hostDocRoot)
	if err != nil {
		return docker.ContainerSpec{}, err
	}
	return docker.ContainerSpec{
		Name:  containerNameOf(cfg),
		Image: strings.TrimSpace(cfg.Image),
		Binds: []string{
			filepath.ToSlash(hostDocRoot) + ":" + containerDocRootPath,
			filepath.ToSlash(hostIni) + ":" + containerIniPath + ":ro",
		},
		Labels:        map[string]string{docker.LabelTool: "php", docker.LabelInstance: cfg.ID},
		ContainerPort: containerPortPHP,
		HostPort:      cfg.Port,
		HostIP:        "127.0.0.1",
		Cmd:           dockerArgsOf(cfg, routerIn),
	}, nil
}

// dockerContainerID 优先取进程表，退回按标签查找（应用重启后仍能操作旧容器）
func dockerContainerID(cfg *InstanceConfig) string {
	if p := trackedSrv(cfg.ID); p != nil && p.containerID != "" {
		return p.containerID
	}
	id, found, err := docker.FindContainerByLabel("php", cfg.ID)
	if err != nil || !found {
		return ""
	}
	return id
}

// specSignature 容器规格签名：用于判断已有容器是否还跟得上当前配置
// （inspect 接口不回报挂载与命令，单比镜像/端口会漏掉改站点目录、改路由这两种情况）
func specSignature(spec docker.ContainerSpec) string {
	data, err := json.Marshal(spec)
	if err != nil {
		return fmt.Sprintf("%v", spec)
	}
	return string(data)
}

func specSigPath(cfg *InstanceConfig) (string, error) {
	dir, err := instanceDir(cfg.ID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "php-docker.container"), nil
}

// specMatchesLast 与上次创建容器时的规格是否一致（没记过就算不一致）
func specMatchesLast(cfg *InstanceConfig, sig string) bool {
	p, err := specSigPath(cfg)
	if err != nil {
		return false
	}
	data, err := os.ReadFile(p)
	return err == nil && string(data) == sig
}

func recordSpec(cfg *InstanceConfig, sig string) {
	p, err := specSigPath(cfg)
	if err != nil {
		return
	}
	_ = os.WriteFile(p, []byte(sig), 0o644)
}

// dockerStart 创建（或复用）并启动容器，等待端口就绪
func dockerStart(cfg *InstanceConfig) error {
	if strings.TrimSpace(cfg.Image) == "" {
		return fmt.Errorf("Docker 模式需指定镜像，如 %s", DefaultDockerImage)
	}
	hostDocRoot, err := dockerDocRoot(cfg)
	if err != nil {
		return err
	}
	hostIni, err := ensureDockerIni(cfg)
	if err != nil {
		return err
	}
	spec, err := dockerSpecOf(cfg, hostDocRoot, hostIni)
	if err != nil {
		return err
	}
	sig := specSignature(spec)
	// 镜像缺失则先拉取（首次可能几分钟，进度经 "docker:pull" 事件推送）
	if err := docker.EnsureImage(cfg.Image); err != nil {
		return fmt.Errorf("准备镜像 %s 失败: %v", cfg.Image, err)
	}

	id, found, err := docker.FindContainerByLabel("php", cfg.ID)
	if err != nil {
		return err
	}
	if found {
		st, err := docker.InspectContainer(id)
		if err != nil {
			return err
		}
		if st.Running {
			return fmt.Errorf("容器已在运行")
		}
		if st.Image != cfg.Image || st.HostPort != cfg.Port || !specMatchesLast(cfg, sig) {
			// 镜像/端口/挂载/命令任一变更：重建容器
			if err := docker.RemoveContainer(id, true); err != nil {
				return fmt.Errorf("重建容器失败: %v", err)
			}
			found = false
		}
	}
	if !found {
		if id, err = docker.CreateContainer(spec); err != nil {
			return fmt.Errorf("创建容器失败: %v", err)
		}
		recordSpec(cfg, sig)
	}
	if err := docker.StartContainer(id); err != nil {
		return fmt.Errorf("启动容器失败: %v", err)
	}

	p := &srvRunning{containerID: id, done: make(chan struct{}), mem: newRing(600)}
	srvRunMu.Lock()
	srvProcs[cfg.ID] = p
	srvRunMu.Unlock()
	watchDockerExit(cfg.ID, id, p)

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-p.done:
			return fmt.Errorf("容器已退出，请查看日志:\n%s", strings.Join(dockerLogTail(id, 12), "\n"))
		default:
		}
		if tcpUp(cfg.Port) {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("启动超时：60 秒内 php -S 未在 127.0.0.1:%d 就绪，请查看日志\n%s",
		cfg.Port, strings.Join(dockerLogTail(id, 12), "\n"))
}

// watchDockerExit 跟踪容器退出（与本地模式的 cmd.Wait 语义对齐）
func watchDockerExit(instID, containerID string, p *srvRunning) {
	go func() {
		tick := time.NewTicker(1500 * time.Millisecond)
		defer tick.Stop()
		for {
			<-tick.C
			st, err := docker.InspectContainer(containerID)
			if err != nil || !st.Running {
				forgetSrv(instID, p)
				return
			}
		}
	}()
}

// forgetSrv 从进程表摘除该实例（幂等：只有仍指向同一对象时才删）
func forgetSrv(instID string, p *srvRunning) {
	srvRunMu.Lock()
	if srvProcs[instID] == p {
		delete(srvProcs, instID)
	}
	srvRunMu.Unlock()
	select {
	case <-p.done:
	default:
		close(p.done)
	}
}

// dockerStop 停止容器：优先进程表，退回按标签查找
func dockerStop(cfg *InstanceConfig) error {
	id := dockerContainerID(cfg)
	if id == "" {
		return fmt.Errorf("未找到该实例对应的容器（可能已被删除）")
	}
	if err := docker.StopContainer(id, 10); err != nil {
		return fmt.Errorf("停止容器失败: %v", err)
	}
	if p := trackedSrv(cfg.ID); p != nil {
		forgetSrv(cfg.ID, p)
	}
	if tcpUp(cfg.Port) {
		return fmt.Errorf("停止失败：端口 %d 仍在监听", cfg.Port)
	}
	return nil
}

// dockerRemove 删除容器（站点目录与配置文件保留，与本地模式语义一致）
func dockerRemove(cfg *InstanceConfig) error {
	if p, err := specSigPath(cfg); err == nil {
		_ = os.Remove(p) // 规格签名随容器一起作废
	}
	id := dockerContainerID(cfg)
	if id == "" {
		return nil // 从未起过容器
	}
	if err := docker.RemoveContainer(id, true); err != nil {
		return fmt.Errorf("删除容器失败: %v", err)
	}
	return nil
}

func dockerLogTail(id string, n int) []string {
	lines, err := docker.GetContainerLogs(id, n)
	if err != nil {
		return []string{err.Error()}
	}
	return lines
}

// dockerLogs 实例日志（容器输出，即 php -S 的访问与错误日志）
func dockerLogs(cfg *InstanceConfig, n int) ([]string, error) {
	id := dockerContainerID(cfg)
	if id == "" {
		return nil, nil // 尚无容器
	}
	return docker.GetContainerLogs(id, n)
}

// fillDockerStatus 补充 Docker 模式的状态字段
func fillDockerStatus(cfg *InstanceConfig, st *InstanceStatus) {
	st.Mode = ModeDocker
	st.Image = cfg.Image
	st.Container = containerNameOf(cfg)
	st.Version = displayVersionOf(cfg)
	if ini, err := dockerIniPath(cfg); err == nil {
		st.ConfigPath = ini
	}
	id := dockerContainerID(cfg)
	if id == "" {
		return
	}
	st.ContainerID = id
	if cs, err := docker.InspectContainer(id); err == nil {
		st.ContainerState = cs.State
		if cs.Running {
			// 运行时长与 inspect 同源，不再为了一个 uptime 多查一次 daemon
			st.UptimeSeconds = cs.UptimeSeconds
		}
	}
}

// displayVersionOf Docker 实例展示用的版本号：从镜像 tag 里取，取不到退回镜像名
func displayVersionOf(cfg *InstanceConfig) string {
	if v := versionFromImage(cfg.Image); v != "" {
		return v
	}
	if v := strings.TrimSpace(cfg.Version); v != "" {
		return v
	}
	return strings.TrimSpace(cfg.Image)
}

// versionFromImage 从镜像名里抽版本号（php:8.3.12-cli → 8.3.12；php:8.3-cli → 8.3；取不到返回空）
func versionFromImage(image string) string {
	tag := image
	if i := strings.LastIndex(image, ":"); i >= 0 && !strings.Contains(image[i+1:], "/") {
		tag = image[i+1:]
	} else {
		return ""
	}
	var out []byte
	for _, ch := range tag {
		if (ch >= '0' && ch <= '9') || ch == '.' {
			out = append(out, byte(ch))
			continue
		}
		break
	}
	v := strings.Trim(string(out), ".")
	if v == "" || !strings.Contains(v, ".") {
		return ""
	}
	return v
}

// DockerAvailable 引擎是否可用（前端据此提示，不阻断保存）
func DockerAvailable() (bool, string) {
	info, err := docker.GetEngineStatus()
	if err != nil {
		return false, err.Error()
	}
	if !info.Available {
		return false, info.Error
	}
	return true, ""
}

// DockerImages 本地 php 系镜像 tag（表单下拉候选）
func DockerImages() ([]string, error) {
	images, err := docker.ListImages()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(images))
	for _, im := range images {
		if im.Tag == "" || im.Tag == "<none>" {
			continue
		}
		if strings.Contains(strings.ToLower(im.Repo), "php") {
			out = append(out, im.Repo+":"+im.Tag)
		}
	}
	return out, nil
}
