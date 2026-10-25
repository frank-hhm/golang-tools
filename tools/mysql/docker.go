package mysql

// ============================================================
// MySQL 实例的 Docker 运行模式（经 tools/docker 的 Engine API）
//
// 与本地模式的对应关系：
//   - 数据目录一致：<data>/mysql-instances/<id>/data，直接绑定挂载进容器
//     /var/lib/mysql，因此本地实例与容器实例可互迁（换 Mode 而不动数据）
//   - 端口：容器内 3306 发布到 127.0.0.1:<cfg.Port>，状态探测沿用 tcpUp + SQL
//     采集，前端与本地模式无差异
//   - root 密码：官方镜像环境变量注入（留空则 MYSQL_ALLOW_EMPTY_PASSWORD=yes，
//     与本地模式 --initialize-insecure 语义一致）
//   - 归属：容器打标签 go-tools=mysql / go-tools.instance=<id>，应用重启后凭标签
//     找回容器（不依赖进程表）
//   - 生命周期入口仍是 StartInstance/StopInstance/Logs，按 cfg.Mode 分叉
//
// 变更生效：镜像或端口改动会重建容器（数据在宿主机目录，不丢）；其余参数
// （max_connections 等）以启动命令传入，同样在重建时生效。
// ============================================================

import (
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
	// containerPortMySQL 官方镜像内 mysqld 监听端口
	containerPortMySQL = 3306
	// containerDataPath 官方镜像内数据目录
	containerDataPath = "/var/lib/mysql"
	// DefaultDockerImage 表单默认镜像
	DefaultDockerImage = "mysql:8.4"
)

// findInstanceConfig 按 ID 取实例配置（生命周期分叉时判定模式）
func findInstanceConfig(id string) (*InstanceConfig, error) {
	list, err := loadInstances()
	if err != nil {
		return nil, err
	}
	for i := range list {
		if list[i].ID == id {
			return &list[i], nil
		}
	}
	return nil, fmt.Errorf("实例不存在: %s", id)
}

// isDockerMode 判定实例运行模式（零值/其他值一律按本地）
func isDockerMode(cfg *InstanceConfig) bool {
	return strings.EqualFold(strings.TrimSpace(cfg.Mode), ModeDocker)
}

// containerNameOf 容器名（留空则按实例 ID 生成，保证唯一且可反查）
func containerNameOf(cfg *InstanceConfig) string {
	if n := strings.TrimSpace(cfg.ContainerName); n != "" {
		return n
	}
	return "go-tools-mysql-" + cfg.ID
}

// instDirOf 实例目录（只拼路径不创建，供状态展示）
func instDirOf(id string) string {
	d, err := goToolsDir()
	if err != nil {
		return ""
	}
	return filepath.Join(d, "mysql-instances", id)
}

// dockerDataDir 宿主机数据目录（确保存在）+ 容器内挂载路径
func dockerDataDir(cfg *InstanceConfig) (host string, err error) {
	instDir, err := defaultInstanceDataDir(cfg.ID)
	if err != nil {
		return "", err
	}
	host = filepath.Join(instDir, "data")
	if err := os.MkdirAll(host, 0o755); err != nil {
		return "", err
	}
	return host, nil
}

// dockerSpecOf 由实例配置生成容器创建参数
func dockerSpecOf(cfg *InstanceConfig, hostDataDir string) docker.ContainerSpec {
	env := []string{}
	if pw := cfg.RootPassword; pw != "" {
		env = append(env, "MYSQL_ROOT_PASSWORD="+pw)
	} else {
		env = append(env, "MYSQL_ALLOW_EMPTY_PASSWORD=yes")
	}
	spec := docker.ContainerSpec{
		Name:          containerNameOf(cfg),
		Image:         strings.TrimSpace(cfg.Image),
		Env:           env,
		Binds:         []string{filepath.ToSlash(hostDataDir) + ":" + containerDataPath},
		Labels:        map[string]string{docker.LabelTool: "mysql", docker.LabelInstance: cfg.ID},
		ContainerPort: containerPortMySQL,
		HostPort:      cfg.Port,
		HostIP:        "127.0.0.1",
	}
	if cfg.MaxConnections > 0 {
		// 官方镜像把额外命令参数直接传给 mysqld
		spec.Cmd = []string{fmt.Sprintf("--max-connections=%d", cfg.MaxConnections)}
	}
	return spec
}

// findDockerContainer 凭标签找回本实例的容器 ID（不存在返回空串）
func findDockerContainer(cfg *InstanceConfig) (string, bool, error) {
	return docker.FindContainerByLabel("mysql", cfg.ID)
}

// dockerContainerID 优先取进程表，退回按标签查找（应用重启后仍能操作旧容器）
func dockerContainerID(cfg *InstanceConfig) string {
	if p := trackedSrv(cfg.ID); p != nil && p.containerID != "" {
		return p.containerID
	}
	id, found, err := findDockerContainer(cfg)
	if err != nil || !found {
		return ""
	}
	return id
}

// dockerStart 创建（或复用）并启动容器，等待就绪
func dockerStart(cfg *InstanceConfig) error {
	if strings.TrimSpace(cfg.Image) == "" {
		return fmt.Errorf("Docker 模式需指定镜像，如 %s", DefaultDockerImage)
	}
	hostDataDir, err := dockerDataDir(cfg)
	if err != nil {
		return err
	}
	// 镜像缺失则先拉取（首次使用会耗时较久，进度经 docker:pull 事件推送）
	if err := docker.EnsureImage(cfg.Image); err != nil {
		return fmt.Errorf("准备镜像 %s 失败: %v", cfg.Image, err)
	}

	id, found, err := findDockerContainer(cfg)
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
		if st.Image != cfg.Image || st.HostPort != cfg.Port {
			// 镜像/端口变更：重建容器（数据在宿主机目录，不受影响）
			if err := docker.RemoveContainer(id, true); err != nil {
				return fmt.Errorf("重建容器失败: %v", err)
			}
			found = false
		}
	}
	if !found {
		id, err = docker.CreateContainer(dockerSpecOf(cfg, hostDataDir))
		if err != nil {
			return fmt.Errorf("创建容器失败: %v", err)
		}
	}
	if err := docker.StartContainer(id); err != nil {
		return fmt.Errorf("启动容器失败: %v", err)
	}

	p := &srvRunning{containerID: id, done: make(chan struct{})}
	srvRunMu.Lock()
	srvProcs[cfg.ID] = p
	srvRunMu.Unlock()
	watchDockerExit(cfg.ID, id, p)

	// 等待就绪（首次初始化数据目录较慢，给 3 分钟；其余 60s）
	budget := 60 * time.Second
	if empty, err := dirIsEmpty(hostDataDir); err == nil && empty {
		budget = 180 * time.Second
	}
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		select {
		case <-p.done:
			return fmt.Errorf("容器已退出，请查看日志:\n%s", strings.Join(dockerLogTail(id, 10), "\n"))
		default:
		}
		if tcpUp(cfg.Port) {
			if db, ok := pingRoot(cfg.Port, cfg.RootPassword); ok {
				db.Close()
				return nil
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("启动超时：%d 秒内容器未在 127.0.0.1:%d 就绪，请查看日志\n%s",
		int(budget.Seconds()), cfg.Port, strings.Join(dockerLogTail(id, 10), "\n"))
}

// watchDockerExit 跟踪容器退出（与本地模式的 cmd.Wait 语义对齐）。
// 改由 docker 包的共享看护负责：引擎一时问不到不等于容器没了，退避重试，
// 避免 Desktop 抖一下就把还在跑的 MySQL 从进程表里抹掉。
func watchDockerExit(instID, containerID string, p *srvRunning) {
	docker.WatchContainerExit(containerID, func() { forgetSrv(instID, p) })
}

// forgetSrv 从进程表摘除该实例并关 done 通道（幂等：只有仍指向同一对象时才删）
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
		srvRunMu.Lock()
		if srvProcs[cfg.ID] == p {
			delete(srvProcs, cfg.ID)
		}
		srvRunMu.Unlock()
		select {
		case <-p.done:
		case <-time.After(8 * time.Second):
			close(p.done)
		}
	}
	if tcpUp(cfg.Port) {
		return fmt.Errorf("停止失败：端口 %d 仍在监听", cfg.Port)
	}
	return nil
}

// dockerRemove 删除容器（数据目录保留，与本地模式语义一致）
func dockerRemove(cfg *InstanceConfig) error {
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

// dockerLogs 实例日志（容器输出，含 mysqld 启动横幅与错误）
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
	st.DataDir = filepath.Join(instDirOf(cfg.ID), "data")
	if st.Version == "" {
		st.Version = cfg.Image // 未运行时版本位展示镜像
	}
	id := dockerContainerID(cfg)
	if id == "" {
		return
	}
	st.ContainerID = id
	if cs, err := docker.InspectContainer(id); err == nil {
		st.ContainerState = cs.State
		if cs.Running {
			st.UptimeSeconds = secondsSinceRFC(cs.StartedAt)
		} else {
			st.UptimeSeconds = 0
		}
	}
}

// secondsSinceRFC 与 docker 包同口径的运行时长换算
func secondsSinceRFC(rfc string) int64 {
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, rfc); err == nil {
			d := int64(time.Since(t).Seconds())
			if d < 0 {
				d = 0
			}
			return d
		}
	}
	return 0
}

// DockerAvailable 引擎是否可用（前端据此放开/置灰 Docker 选项）
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

// DockerImages 本地 mysql 系镜像 tag（表单下拉候选）
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
		if strings.Contains(im.Repo, "mysql") || strings.Contains(im.Repo, "mariadb") || strings.Contains(im.Repo, "percona") {
			out = append(out, im.Repo+":"+im.Tag)
		}
	}
	return out, nil
}
