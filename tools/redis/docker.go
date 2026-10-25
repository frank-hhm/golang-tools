package redis

// ============================================================
// Redis 实例的 Docker 运行模式（经 tools/docker 的 Engine API）
//
// 与本地模式的对应关系：
//   - 服务器：容器内跑同一条 redis-server，配置文件由本程序生成后挂载进去；
//     容器内固定监听 6379，发布到 127.0.0.1:<cfg.Port>，连接方式与本地模式一致
//     （容器内 bind 0.0.0.0 只是让 Docker 能把流量转进来，宿主机仍只绑 127.0.0.1）
//   - 数据：cfg.DataDir（RDB/AOF/server.log 都在这儿）绑定挂载到 /data，
//     换镜像、重建容器都不丢数据
//   - 配置：实例数据目录下 go-tools-docker.conf 与本地那份 go-tools.conf 同等待遇
//     （每次启动按实例参数覆盖生成），区别只有 port/bind/dir 三项要迁就容器
//   - 归属：容器打标签 go-tools=redis / go-tools.instance=<id>，应用重启后凭标签找回
//   - 日志：容器 stdout 即 redis 日志（conf 里 logfile ""），走 docker logs
//
// 与本地模式的差异（如实告知，不做静默兜底）：
//   - 版本由镜像 tag 决定：本机的 redis-server 版本库、安装/删除版本都管不到容器实例，
//     实例配置里的 Version 在容器模式下清空，展示版本改从镜像 tag 推导
//   - PID 无意义：容器主进程的 pid 在 VM 的 namespace 里，面板改显示容器名与容器状态
//   - 停止/删除走容器接口：本地那条「孤儿进程也按 PING 找得到」的路子对容器同样有效
//     （端口通 = 容器在应答），但兜底是 StopContainer 而不是 Kill
//
// 变更生效：创建容器时把规格（镜像/端口/挂载/命令/标签）的签名记在数据目录，
// 下次启动不一致即重建容器（数据都在宿主机目录，重建不丢 RDB/AOF）。
// ============================================================

import (
	"encoding/json"
	"fmt"
	"net"
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
	// containerPortRedis 容器内 redis-server 监听端口（固定，靠端口发布映射到实例端口）
	containerPortRedis = 6379
	// containerDataPath 容器内数据目录（实例 DataDir 挂到这里）
	containerDataPath = "/data"
	// DefaultDockerImage 表单默认镜像：alpine 体积最小，7.x 与本机常用的 msys2 构建同大版本
	DefaultDockerImage = "redis:7-alpine"
	// dockerConfName 容器专用配置文件名：落在数据目录里，因而随挂载一起出现在 /data 下
	dockerConfName = "go-tools-docker.conf"
	// dockerSpecName 上次创建容器时的规格签名文件名
	dockerSpecName = "redis-docker.container"
)

// isDockerMode 判定实例运行模式（零值/其他值一律按本地）
func isDockerMode(cfg *InstanceConfig) bool {
	return strings.EqualFold(strings.TrimSpace(cfg.Mode), ModeDocker)
}

// containerNameOf 容器名（留空则按实例 ID 生成，保证唯一且可反查）
func containerNameOf(cfg *InstanceConfig) string {
	if n := strings.TrimSpace(cfg.ContainerName); n != "" {
		return n
	}
	return "go-tools-redis-" + cfg.ID
}

// dockerDataDir 实例数据目录（宿主绝对路径，确保存在）：与本地模式同一份目录，
// 因此本地实例切成容器后，已有的 dump.rdb / appendonly 照样读得到
func dockerDataDir(cfg *InstanceConfig) (string, error) {
	dir := strings.TrimSpace(cfg.DataDir)
	if dir == "" {
		d, err := defaultInstanceDataDir(cfg.ID)
		if err != nil {
			return "", err
		}
		dir = d
	} else if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("数据目录不可用: %s（%v）", dir, err)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("数据目录不合法: %s（%v）", dir, err)
	}
	return abs, nil
}

// buildDockerConf 容器内用的 redis.conf。
// 与 buildConf 的三项必要差异：端口换成容器端口、bind 放开给 docker-proxy、dir 指向挂载点。
// 其余（appendonly/maxmemory/requirepass）与本地模式同一套参数，保证两种模式行为一致
func buildDockerConf(cfg *InstanceConfig) string {
	var b strings.Builder
	b.WriteString("# go-tools 为 Docker 实例生成，请勿手改（每次启动覆盖）\n")
	fmt.Fprintf(&b, "port %d\n", containerPortRedis)
	// 容器里绑 127.0.0.1 会让发布端口连不上；对外仍由宿主机只映射到 127.0.0.1
	b.WriteString("bind 0.0.0.0\n")
	b.WriteString("daemonize no\n")
	b.WriteString(`logfile ""` + "\n") // 日志走 stdout，docker logs 收
	fmt.Fprintf(&b, "dir %s\n", containerDataPath)
	if cfg.AppendOnly {
		b.WriteString("appendonly yes\n")
	} else {
		b.WriteString("appendonly no\n")
	}
	if cfg.MaxMemoryMB > 0 {
		fmt.Fprintf(&b, "maxmemory %dm\n", cfg.MaxMemoryMB)
		b.WriteString("maxmemory-policy noeviction\n")
	}
	if cfg.Password != "" {
		fmt.Fprintf(&b, "requirepass %s\n", cfg.Password)
	}
	return b.String()
}

// ensureDockerConf 每次启动重写容器配置（与本地模式一致：参数改了就得落到文件里）。
// 同时返回写下的文本：签名要拿它跟容器规格一起算，见 specSignature
func ensureDockerConf(cfg *InstanceConfig, hostDataDir string) (path, text string, err error) {
	text = buildDockerConf(cfg)
	path = filepath.Join(hostDataDir, dockerConfName)
	if err = os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", "", err
	}
	return path, text, nil
}

// dockerArgsOf 容器内 redis-server 的启动参数（entrypoint 是 docker-entrypoint.sh，直接 exec）
func dockerArgsOf() []string {
	// 配置挂在 /data 下（与数据目录同一份挂载），用容器内路径引用
	return []string{"redis-server", containerDataPath + "/" + dockerConfName}
}

// dockerSpecOf 由实例配置生成容器创建参数
func dockerSpecOf(cfg *InstanceConfig, hostDataDir string) docker.ContainerSpec {
	return docker.ContainerSpec{
		Name:          containerNameOf(cfg),
		Image:         strings.TrimSpace(cfg.Image),
		Binds:         []string{filepath.ToSlash(hostDataDir) + ":" + containerDataPath},
		Labels:        map[string]string{docker.LabelTool: "redis", docker.LabelInstance: cfg.ID},
		ContainerPort: containerPortRedis,
		HostPort:      cfg.Port,
		HostIP:        "127.0.0.1",
		Cmd:           dockerArgsOf(),
	}
}

// dockerContainerID 优先取进程表，退回按标签查找（应用重启后仍能操作旧容器）
func dockerContainerID(cfg *InstanceConfig) string {
	if p := trackedProc(cfg.ID); p != nil && p.containerID != "" {
		return p.containerID
	}
	id, found, err := docker.FindContainerByLabel("redis", cfg.ID)
	if err != nil || !found {
		return ""
	}
	return id
}

// specSignature 容器规格签名：用于判断已有容器是否还跟得上当前配置。
// 除了 ContainerSpec，还把生成的 redis.conf 一并计入：镜像/端口/挂载/命令写在
// ContainerSpec 里会被 inspect 映出来，而改密码、开关 AOF、改 maxmemory 只体现在
// 配置文件上，不计入就会漏掉重建（redis 只在启动时读配置，不重建会拿着旧密码跑）
func specSignature(spec docker.ContainerSpec, conf string) string {
	data, err := json.Marshal(struct {
		Spec docker.ContainerSpec `json:"spec"`
		Conf string               `json:"conf"`
	}{spec, conf})
	if err != nil {
		return fmt.Sprintf("%v|%s", spec, conf)
	}
	return string(data)
}

func specSigPath(cfg *InstanceConfig) (string, error) {
	dir, err := dockerDataDir(cfg)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, dockerSpecName), nil
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

// dockerStart 创建（或复用）并启动容器，等待 PING 就绪
func dockerStart(cfg *InstanceConfig) error {
	image := strings.TrimSpace(cfg.Image)
	if image == "" {
		return fmt.Errorf("Docker 模式需指定镜像，如 %s", DefaultDockerImage)
	}
	hostDataDir, err := dockerDataDir(cfg)
	if err != nil {
		return err
	}
	_, confText, err := ensureDockerConf(cfg, hostDataDir)
	if err != nil {
		return fmt.Errorf("准备容器配置失败: %v", err)
	}
	spec := dockerSpecOf(cfg, hostDataDir)
	sig := specSignature(spec, confText)

	// 发布端口先占一下：宿主机已经被占的话，容器要等到 start 才报错，不如这里直接说清楚
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Port))
	if err != nil {
		return fmt.Errorf("端口 %d 已被占用: %v", cfg.Port, err)
	}
	_ = ln.Close()

	// 镜像缺失则先拉取（首次可能几分钟，进度经 "docker:pull" 事件推送）
	if err := docker.EnsureImage(image); err != nil {
		return fmt.Errorf("准备镜像 %s 失败: %v", image, err)
	}

	id, found, err := docker.FindContainerByLabel("redis", cfg.ID)
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
		if st.Image != image || st.HostPort != cfg.Port || !specMatchesLast(cfg, sig) {
			// 镜像/端口/挂载/命令任一变更：重建容器（数据在宿主机目录，不受影响）
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

	p := &runningProc{containerID: id, done: make(chan struct{})}
	runMu.Lock()
	runProcs[cfg.ID] = p
	runMu.Unlock()
	watchDockerExit(cfg.ID, id, p)

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-p.done:
			return fmt.Errorf("容器已退出，请查看日志:\n%s", strings.Join(dockerLogTail(id, 12), "\n"))
		default:
		}
		if isProcessRunning(cfg.ID, cfg.Port, cfg.Password) {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("启动超时：60 秒内 redis 未在 127.0.0.1:%d 应答 PING，请查看日志\n%s",
		cfg.Port, strings.Join(dockerLogTail(id, 12), "\n"))
}

// watchDockerExit 跟踪容器退出（与本地模式的 cmd.Wait 语义对齐：退出即从进程表摘除）
func watchDockerExit(instID, containerID string, p *runningProc) {
	go func() {
		tick := time.NewTicker(1500 * time.Millisecond)
		defer tick.Stop()
		for {
			<-tick.C
			st, err := docker.InspectContainer(containerID)
			if err != nil || !st.Running {
				forgetProc(instID, p)
				return
			}
		}
	}()
}

// forgetProc 从进程表摘除该实例并关 done 通道（幂等：只有仍指向同一对象时才删）
func forgetProc(instID string, p *runningProc) {
	runMu.Lock()
	if runProcs[instID] == p {
		delete(runProcs, instID)
	}
	runMu.Unlock()
	select {
	case <-p.done:
	default:
		close(p.done)
	}
}

// dockerStop 停止容器（保留容器本体，下次启动复用）：优先进程表，退回按标签查找
func dockerStop(cfg *InstanceConfig) error {
	id := dockerContainerID(cfg)
	if id == "" {
		return fmt.Errorf("未找到该实例对应的容器（可能已被删除）")
	}
	if err := docker.StopContainer(id, 10); err != nil {
		return fmt.Errorf("停止容器失败: %v", err)
	}
	if p := trackedProc(cfg.ID); p != nil {
		forgetProc(cfg.ID, p)
	}
	if isProcessRunning(cfg.ID, cfg.Port, cfg.Password) {
		return fmt.Errorf("停止失败：端口 %d 仍在响应 PING", cfg.Port)
	}
	return nil
}

// dockerRemove 删除容器（数据目录与容器配置保留，与本地模式删除实例的语义一致）
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

// dockerLogs 实例日志（容器 stdout，即 redis 自己的日志）
func dockerLogs(cfg *InstanceConfig, n int) ([]string, error) {
	id := dockerContainerID(cfg)
	if id == "" {
		return nil, nil // 尚无容器
	}
	return docker.GetContainerLogs(id, n)
}

// fillDockerStatus 补充 Docker 模式的状态字段（PID 不填：容器 pid 在 VM namespace 里，给人看只会误导）
func fillDockerStatus(cfg *InstanceConfig, st *InstanceStatus) {
	st.Mode = ModeDocker
	st.Image = strings.TrimSpace(cfg.Image)
	st.Container = containerNameOf(cfg)
	st.Version = displayVersionOf(cfg)
	id := dockerContainerID(cfg)
	if id == "" {
		return
	}
	st.ContainerID = id
	if cs, err := docker.InspectContainer(id); err == nil {
		st.ContainerState = cs.State
		if cs.Running {
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

// versionFromImage 从镜像名里抽版本号（redis:7.4.2-alpine → 7.4.2；redis:7-alpine → 7；取不到返回空）
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
	return strings.Trim(string(out), ".")
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

// DockerImages 本地 redis 系镜像 tag（表单下拉候选）
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
		low := strings.ToLower(im.Repo)
		if strings.Contains(low, "redis") || strings.Contains(low, "valkey") {
			out = append(out, im.Repo+":"+im.Tag)
		}
	}
	return out, nil
}
