//go:build windows

package docker

// ============================================================
// Docker 管理 - 对外能力面（Windows 实现）
//
// 全部为包级函数，由 services/app 的 DockerSrv* 方法委托，供前端调用；
// 同时导出给 tools/mysql 复用（容器化实例生命周期）。
//
// 约定：由本工具创建的容器统一打标签
//   go-tools=<工具名>  go-tools.instance=<实例ID>
// 便于在 MySQL 面板中回标归属、并在重启后找回容器。
// ============================================================

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go-tools/services/applog"
)

// 容器归属标签
const (
	LabelTool     = "go-tools"
	LabelInstance = "go-tools.instance"
)

const logRingMax = 500

// streamRequest 流式请求（日志跟随 / 镜像拉取）：自动补 API 版本前缀
func streamRequest(method, path string, query url.Values, longLived bool) (*http.Response, error) {
	return rawRequest(method, prefix()+path, query, nil, longLived)
}

/* ============================ 引擎 ============================ */

// GetEngineStatus 探测 daemon 可达性与基本信息；不可达也返回（Available=false + Error）
func GetEngineStatus() (EngineInfo, error) {
	info := EngineInfo{}
	var v versionProbe
	err := rawDo(http.MethodGet, "/version", nil, nil, &v, false)
	if err != nil && endpointLooksDead(err) {
		// Docker Desktop 可能把引擎换到另一条管道上了（重启过 / 刚被本程序拉起）：
		// 作废端点、重扫候选后原地重试一次，让面板这一轮就跟得上，
		// 而不是每 5 秒撞一次同一个拨不通的端点。
		// 引擎只是忙（管道实例被拉取/日志流占满）不走这里：那种情况下丢端点会让面板乱跳
		invalidateEndpoint("Engine API 无应答: " + err.Error())
		if err2 := rawDo(http.MethodGet, "/version", nil, nil, &v, false); err2 == nil {
			err = nil
		} else {
			err = err2
		}
	}
	// 端点描述放在探测之后取：这一轮若重扫到了新管道，卡片上给的就是实际用上的那个
	info.Endpoint = endpointDesc()
	if err != nil {
		info.Error = err.Error()
		return info, nil // 不返回 error：前端按 Available 分支渲染引导
	}
	info.Available = true
	info.Version = v.Version
	info.APIVersion = v.APIVersion
	info.OS = v.OSType
	info.Arch = v.Arch
	info.Kernel = v.Kernel
	// 内核串顺带判后端：WSL2 后端带 microsoft-standard-WSL2，Hyper-V 后端没有
	info.Backend = backendFromKernel(v.Kernel)
	info.GoVersion = v.GoVersion
	info.Platform = v.Platform.Name
	if strings.Contains(v.Platform.Name, "Docker Desktop") {
		info.IsDesktop = true
	}
	// /info 补充 CPU/内存与 Desktop 判定（失败不影响主流程）
	var sys struct {
		OperatingSystem string   `json:"OperatingSystem"`
		Name            string   `json:"Name"`
		NCPU            int      `json:"NCPU"`
		MemTotal        int64    `json:"MemTotal"`
		RegistryMirrors []string `json:"RegistryMirrors"`
	}
	if err := do(http.MethodGet, "/info", nil, nil, &sys); err == nil {
		info.CPUs, info.MemTotal = sys.NCPU, sys.MemTotal
		info.RegistryMirrors = sys.RegistryMirrors
		if !info.IsDesktop {
			blob := sys.OperatingSystem + " " + sys.Name
			if strings.Contains(blob, "Docker Desktop") || strings.Contains(blob, "docker-desktop") {
				info.IsDesktop = true
			}
		}
		if info.Platform == "" {
			info.Platform = sys.OperatingSystem
		}
	}
	return info, nil
}

/* ============================ 引擎状态缓存 ============================ */

var (
	engCacheMu sync.Mutex
	engCacheAt time.Time
	engCache   EngineInfo
	engProbeMu sync.Mutex // 串行化真探测：并发调用合并成一次拨号
)

// EngineStatusCached 带 TTL 的引擎状态：首页总览轮询用，避免每次都拨一次命名管道。
// 注意：engCacheMu 只能护状态，不能持着它去探测 —— 拨号过程中端点失效会回头
// 清这份缓存（clearEngineStatusCache 同样要这把锁），重入即死锁
func EngineStatusCached(ttl time.Duration) (EngineInfo, error) {
	engCacheMu.Lock()
	if time.Since(engCacheAt) < ttl {
		info := engCache
		engCacheMu.Unlock()
		return info, nil
	}
	engCacheMu.Unlock()

	engProbeMu.Lock()
	defer engProbeMu.Unlock()
	engCacheMu.Lock() // 排队期间可能已有人探好了
	if time.Since(engCacheAt) < ttl {
		info := engCache
		engCacheMu.Unlock()
		return info, nil
	}
	engCacheMu.Unlock()

	info, err := GetEngineStatus()
	engCacheMu.Lock()
	engCache, engCacheAt = info, time.Now()
	engCacheMu.Unlock()
	return info, err
}

// clearEngineStatusCache 作废引擎状态缓存：端点换了/失效了，下一条旧结论都不该再被读到
func clearEngineStatusCache() {
	engCacheMu.Lock()
	engCacheAt = time.Time{}
	engCacheMu.Unlock()
}

/* ============================ 容器 ============================ */

type rawContainer struct {
	ID      string   `json:"Id"`
	Names   []string `json:"Names"`
	Image   string   `json:"Image"`
	ImageID string   `json:"ImageID"`
	Command string   `json:"Command"`
	Created int64    `json:"Created"`
	Ports   []struct {
		IP          string `json:"IP"`
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
	State  string            `json:"State"`
	Status string            `json:"Status"`
	Labels map[string]string `json:"Labels"`
}

type rawStats struct {
	CPUStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
		OnlineCPUs     uint32 `json:"online_cpus"`
	} `json:"cpu_stats"`
	PrecpuStats struct {
		CPUUsage struct {
			TotalUsage uint64 `json:"total_usage"`
		} `json:"cpu_usage"`
		SystemCPUUsage uint64 `json:"system_cpu_usage"`
	} `json:"precpu_stats"`
	MemoryStats struct {
		Usage uint64 `json:"usage"`
		Limit uint64 `json:"limit"`
		Stats struct {
			Cache        uint64 `json:"cache"`         // cgroup v1
			InactiveFile uint64 `json:"inactive_file"` // cgroup v2（无 cache 字段时近似）
		} `json:"stats"`
	} `json:"memory_stats"`
	BlkioStats struct {
		IoServiceBytesRecursive []struct {
			Op    string `json:"op"`
			Value uint64 `json:"value"`
		} `json:"io_service_bytes_recursive"`
	} `json:"blkio_stats"`
	Networks map[string]struct {
		RxBytes uint64 `json:"rx_bytes"`
		TxBytes uint64 `json:"tx_bytes"`
	} `json:"networks"`
	PidsStats struct {
		Current uint64 `json:"current"`
	} `json:"pids_stats"`
}

// ListContainers 列出容器（all=false 仅运行中）；运行中的容器附带 CPU/内存。
// 带 1 秒缓存：Docker 面板、PHP/MySQL 面板会各自轮询，没必要每次都去打扰 daemon
func ListContainers(all bool) ([]Container, error) {
	return memoRead("containers:"+strconv.FormatBool(all), 900*time.Millisecond, func() ([]Container, error) {
		q := url.Values{}
		q.Set("all", strconv.FormatBool(all))
		q.Set("size", "false")
		var raw []rawContainer
		if err := do(http.MethodGet, "/containers/json", q, nil, &raw); err != nil {
			return nil, err
		}
		out := make([]Container, 0, len(raw))
		for _, c := range raw {
			name := ""
			if len(c.Names) > 0 {
				name = strings.TrimPrefix(c.Names[0], "/")
			}
			item := Container{
				ID:        shortID(c.ID),
				Name:      name,
				Image:     c.Image,
				ImageID:   shortID(c.ImageID),
				Command:   c.Command,
				Created:   c.Created,
				State:     c.State,
				Status:    c.Status,
				OwnerTool: c.Labels[LabelTool],
				OwnerID:   c.Labels[LabelInstance],
			}
			for _, p := range c.Ports {
				item.Ports = append(item.Ports, PortBinding{IP: p.IP, PrivatePort: p.PrivatePort, PublicPort: p.PublicPort, Type: p.Type})
			}
			if c.State == "running" {
				if st, err := statsOf(c.ID); err == nil {
					item.CPUPct, item.MemPct, item.MemUsage = calcUsage(st)
				}
			}
			out = append(out, item)
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].State == out[j].State {
				return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
			}
			return out[i].State == "running"
		})
		return out, nil
	})
}

func statsOf(id string) (*rawStats, error) {
	// one-shot=1：只取一个采样点立刻返回。缺了它 daemon 会等两个采样周期（1~2s），
	// 容器一多、轮询一密，这里就是最贵的一笔开销
	return memoRead("stats:"+id, 2500*time.Millisecond, func() (*rawStats, error) {
		q := url.Values{}
		q.Set("stream", "false")
		q.Set("one-shot", "1")
		var st rawStats
		if err := do(http.MethodGet, "/containers/"+id+"/stats", q, nil, &st); err != nil {
			return nil, err
		}
		return &st, nil
	})
}

// ContainerMetricsOf 单容器实时资源占用（监控弹窗按 1s 轮询）。
// 不走 memoRead：2.5 秒的缓存会把曲线拉成台阶，反而看出假峰值
func ContainerMetricsOf(id string) (*ContainerMetrics, error) {
	q := url.Values{}
	q.Set("stream", "false")
	q.Set("one-shot", "1")
	var st rawStats
	if err := do(http.MethodGet, "/containers/"+id+"/stats", q, nil, &st); err != nil {
		return nil, err
	}
	return toMetrics(&st), nil
}

// toMetrics 把一次 stats 原样换算成监控字段（不做速率：一次采样算不出速率）
func toMetrics(st *rawStats) *ContainerMetrics {
	cpuPct, _, _ := calcUsage(st)
	m := &ContainerMetrics{
		At:         time.Now().UnixMilli(),
		CPUPercent: cpuPct,
		MemUsed:    int64(st.MemoryStats.Usage),
		MemLimit:   int64(st.MemoryStats.Limit),
		Pids:       int64(st.PidsStats.Current),
	}
	cache := st.MemoryStats.Stats.Cache
	if cache == 0 {
		cache = st.MemoryStats.Stats.InactiveFile
	}
	m.MemCache = int64(cache)
	if m.MemUsed > m.MemCache {
		m.MemUsed -= m.MemCache // 工作集：扣掉缓存，否则 Linux 容器的内存看起来总是顶到限额
	} else {
		m.MemUsed = 0
	}
	for _, b := range st.BlkioStats.IoServiceBytesRecursive {
		switch strings.ToLower(b.Op) {
		case "read":
			m.DiskRead += int64(b.Value)
		case "write":
			m.DiskWrite += int64(b.Value)
		}
	}
	for _, n := range st.Networks {
		m.NetRx += int64(n.RxBytes)
		m.NetTx += int64(n.TxBytes)
	}
	return m
}

// calcUsage 按 Docker 官方口径换算 CPU% 与内存占用
func calcUsage(st *rawStats) (cpuPct, memPct float64, memUsage string) {
	if cpuDelta := int64(st.CPUStats.CPUUsage.TotalUsage) - int64(st.PrecpuStats.CPUUsage.TotalUsage); cpuDelta >= 0 {
		sysDelta := int64(st.CPUStats.SystemCPUUsage) - int64(st.PrecpuStats.SystemCPUUsage)
		if sysDelta > 0 {
			cpus := float64(st.CPUStats.OnlineCPUs)
			if cpus == 0 {
				cpus = 1
			}
			cpuPct = float64(cpuDelta) / float64(sysDelta) * cpus * 100
		}
	}
	if st.MemoryStats.Limit > 0 {
		memPct = float64(st.MemoryStats.Usage) / float64(st.MemoryStats.Limit) * 100
	}
	memUsage = humanBytes(int64(st.MemoryStats.Usage)) + " / " + humanBytes(int64(st.MemoryStats.Limit))
	return
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// portBindingJSON daemon 返回的单条端口绑定
type portBindingJSON struct {
	HostIp   string `json:"HostIp"`
	HostPort string `json:"HostPort"`
}

// sortedPublishes 把 daemon 的 PortBindings（键形如 "80/tcp"）整理成按容器端口升序的列表；
// 未发布到宿主机的键（HostPort 为空）归为 0，由调用方自己判读
func sortedPublishes(bindings map[string][]portBindingJSON) []PortPublish {
	out := make([]PortPublish, 0, len(bindings))
	for key, list := range bindings {
		if len(list) == 0 {
			continue
		}
		num, _, _ := strings.Cut(key, "/")
		cp, err := strconv.Atoi(num)
		if err != nil || cp <= 0 {
			continue // UDP 以外的非数字键（理论上不会出现）：不拿去参与端口比较
		}
		hp, _ := strconv.Atoi(list[0].HostPort)
		out = append(out, PortPublish{Container: cp, Host: hp})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Container < out[j].Container })
	return out
}

// InspectContainer 单容器实时状态（mysql 生命周期与面板轮询用）；带 0.8 秒缓存
func InspectContainer(id string) (*ContainerState, error) {
	return memoRead("inspect:"+id, 800*time.Millisecond, func() (*ContainerState, error) {
		var raw struct {
			ID     string `json:"Id"`
			Name   string `json:"Name"`
			Config struct {
				Image string `json:"Image"`
			} `json:"Config"`
			HostConfig struct {
				PortBindings map[string][]portBindingJSON `json:"PortBindings"`
			} `json:"HostConfig"`
			State struct {
				Status     string `json:"Status"`
				Running    bool   `json:"Running"`
				Restarting bool   `json:"Restarting"`
				ExitCode   int    `json:"ExitCode"`
				Error      string `json:"Error"`
				StartedAt  string `json:"StartedAt"`
				FinishedAt string `json:"FinishedAt"`
				Pid        int    `json:"Pid"`
			} `json:"State"`
		}
		if err := do(http.MethodGet, "/containers/"+id+"/json", nil, nil, &raw); err != nil {
			return nil, err
		}
		st := &ContainerState{
			ID:         shortID(raw.ID),
			Name:       strings.TrimPrefix(raw.Name, "/"),
			Image:      raw.Config.Image,
			State:      raw.State.Status,
			Running:    raw.State.Running,
			Restarting: raw.State.Restarting,
			ExitCode:   raw.State.ExitCode,
			StartedAt:  raw.State.StartedAt,
			FinishedAt: raw.State.FinishedAt,
			Pid:        raw.State.Pid,
			Error:      raw.State.Error,
		}
		st.Status = raw.State.Status
		// 已发布端口按容器端口升序，主端口取最小那个（nginx 的 80/443 里实例端口是 80）。
		// 不能靠 map 遍历顺序：同一容器两次 inspect 给出不同 HostPort，
		// 上层会误判「端口改了」而反复重建容器
		st.Published = sortedPublishes(raw.HostConfig.PortBindings)
		if len(st.Published) > 0 {
			st.HostPort = st.Published[0].Host
		}
		if st.Running {
			// 已运行秒数顺手算掉：调用方别再为了一个 uptime 再查一次 inspect
			st.UptimeSeconds = secondsSince(raw.State.StartedAt)
			if up := st.UptimeSeconds; up > 0 {
				st.Status = fmt.Sprintf("Up %s", shortDur(up))
			}
		}
		return st, nil
	})
}

// ContainerUptime 容器已运行秒数（未运行返回 0）
func ContainerUptime(id string) int64 {
	st, err := InspectContainer(id)
	if err != nil || !st.Running {
		return 0
	}
	return secondsSince(st.StartedAt)
}

func secondsSince(rfc string) int64 {
	if rfc == "" || strings.HasPrefix(rfc, "0001-01-01") {
		return 0
	}
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

func shortDur(sec int64) string {
	d, h, m := sec/86400, (sec%86400)/3600, (sec%3600)/60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd %dh", d, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	case m > 0:
		return fmt.Sprintf("%dm", m)
	default:
		return fmt.Sprintf("%ds", sec)
	}
}

// StartContainer 启动容器（已运行视为成功）
func StartContainer(id string) error {
	return containerAction(id, "start", true)
}

// StopContainer 停止容器，timeoutSec 为优雅退出等待秒（<=0 用 10）
func StopContainer(id string, timeoutSec int) error {
	if timeoutSec <= 0 {
		timeoutSec = 10
	}
	q := url.Values{}
	q.Set("t", strconv.Itoa(timeoutSec))
	err := do(http.MethodPost, "/containers/"+id+"/stop", q, nil, nil)
	if err != nil && strings.Contains(err.Error(), "304") {
		return nil
	}
	return err
}

// RestartContainer 重启容器
func RestartContainer(id string) error {
	return containerAction(id, "restart", true)
}

// KillContainer 强杀容器（SIGKILL）
func KillContainer(id string) error {
	return do(http.MethodPost, "/containers/"+id+"/kill", nil, nil, nil)
}

// RemoveContainer 删除容器；force=true 时先杀再删
func RemoveContainer(id string, force bool) error {
	q := url.Values{}
	q.Set("force", strconv.FormatBool(force))
	q.Set("v", "false")
	return do(http.MethodDelete, "/containers/"+id, q, nil, nil)
}

// RenameContainer 重命名容器
func RenameContainer(id, name string) error {
	q := url.Values{}
	q.Set("name", name)
	return do(http.MethodPost, "/containers/"+id+"/rename", q, nil, nil)
}

// containerAction 通用 POST 动作；already started(304) 归一为成功
func containerAction(id, action string, okIfState bool) error {
	err := do(http.MethodPost, "/containers/"+id+"/"+action, nil, nil, nil)
	if err != nil && okIfState {
		msg := err.Error()
		if strings.Contains(msg, "(304)") || strings.Contains(msg, "already") {
			return nil
		}
	}
	return err
}

// labelHit 按标签查容器的缓存形状（memoRead 只吃一个返回值）
type labelHit struct {
	ID    string
	Found bool
}

// FindContainerByLabel 按标签组合查容器（含已停止），返回完整 ID
func FindContainerByLabel(tool, instance string) (string, bool, error) {
	hit, err := memoRead("find:"+tool+":"+instance, 900*time.Millisecond, func() (labelHit, error) {
		labels := []string{LabelTool + "=" + tool}
		if instance != "" {
			labels = append(labels, LabelInstance+"="+instance)
		}
		f, _ := json.Marshal(map[string]any{"label": labels})
		q := url.Values{}
		q.Set("all", "true")
		q.Set("filters", string(f))
		var raw []rawContainer
		if err := do(http.MethodGet, "/containers/json", q, nil, &raw); err != nil {
			return labelHit{}, err
		}
		if len(raw) == 0 {
			return labelHit{}, nil
		}
		return labelHit{ID: raw[0].ID, Found: true}, nil
	})
	if err != nil {
		return "", false, err
	}
	return hit.ID, hit.Found, nil
}

// CreateContainer 按 spec 创建容器，返回完整 ID。
// 镜像缺失时自动拉取一次再重试（首次使用 mysql:8.4 这类镜像的常见路径）。
func CreateContainer(spec ContainerSpec) (string, error) {
	id, err := createOnce(spec)
	var ne *notFoundError
	if err != nil && errors.As(err, &ne) {
		if perr := PullImage(spec.Image); perr != nil {
			return "", fmt.Errorf("镜像 %s 拉取失败: %v", spec.Image, perr)
		}
		return createOnce(spec)
	}
	return id, err
}

// notFoundError 标记"404 且提示 no such image"，用于触发自动拉取
type notFoundError struct{ error }

func createOnce(spec ContainerSpec) (string, error) {
	if spec.Image == "" {
		return "", fmt.Errorf("镜像不能为空")
	}
	hostIP := spec.HostIP
	if hostIP == "" {
		hostIP = "127.0.0.1"
	}
	body := map[string]any{
		"Image":  spec.Image,
		"Labels": spec.Labels,
		"HostConfig": map[string]any{
			"Binds":         spec.Binds,
			"AutoRemove":    false,
			"RestartPolicy": map[string]any{"Name": "no", "MaximumRetryCount": 0},
		},
	}
	if len(spec.Env) > 0 {
		body["Env"] = spec.Env
	}
	if len(spec.Cmd) > 0 {
		body["Cmd"] = spec.Cmd
	}
	if spec.ContainerPort > 0 || len(spec.ExtraPorts) > 0 {
		// 主端口 + 额外端口（nginx 的 HTTPS）一次算完：daemon 的 ExposedPorts/PortBindings
		// 本来就是按 "<port>/<proto>" 建表，多一个键不影响既有单端口调用
		exposed := map[string]any{}
		bindings := map[string]any{}
		add := func(container, host int) {
			if container <= 0 {
				return
			}
			key := strconv.Itoa(container) + "/tcp"
			if _, dup := exposed[key]; dup {
				return // 额外端口与主端口重合：以主端口为准
			}
			hostPort := ""
			if host > 0 {
				hostPort = strconv.Itoa(host)
			}
			exposed[key] = map[string]any{}
			bindings[key] = []map[string]any{{"HostIp": hostIP, "HostPort": hostPort}}
		}
		add(spec.ContainerPort, spec.HostPort)
		for _, p := range spec.ExtraPorts {
			add(p.Container, p.Host)
		}
		body["ExposedPorts"] = exposed
		body["HostConfig"].(map[string]any)["PortBindings"] = bindings
	}
	q := url.Values{}
	if spec.Name != "" {
		q.Set("name", spec.Name)
	}
	var out struct {
		ID string `json:"Id"`
	}
	err := do(http.MethodPost, "/containers/create", q, body, &out)
	if err != nil {
		if strings.Contains(err.Error(), "No such image") || strings.Contains(err.Error(), "not found") {
			return "", &notFoundError{err}
		}
		return "", err
	}
	return out.ID, nil
}

/* ============================ 镜像 ============================ */

// ListImages 本地镜像；同镜像多 tag 拆成多行，方便按 tag 展示（带 2 秒缓存）
func ListImages() ([]Image, error) {
	return memoRead("images", 2*time.Second, func() ([]Image, error) {
		var raw []struct {
			ID       string            `json:"Id"`
			RepoTags []string          `json:"RepoTags"`
			Created  int64             `json:"Created"`
			Size     int64             `json:"Size"`
			Labels   map[string]string `json:"Labels"`
		}
		if err := do(http.MethodGet, "/images/json", url.Values{"all": {"false"}}, nil, &raw); err != nil {
			return nil, err
		}
		out := make([]Image, 0, len(raw))
		for _, r := range raw {
			base := Image{Created: r.Created, Size: r.Size, Digest: r.ID}
			if len(r.RepoTags) == 0 {
				i := base
				i.ID = shortID(r.ID)
				i.Repo, i.Tag = "<none>", "<none>"
				out = append(out, i)
				continue
			}
			for _, t := range r.RepoTags {
				i := base
				i.ID = shortID(r.ID)
				i.Repo, i.Tag = splitRepoTag(t)
				out = append(out, i)
			}
		}
		sort.Slice(out, func(i, j int) bool {
			if out[i].Repo == out[j].Repo {
				return out[i].Tag < out[j].Tag
			}
			return out[i].Repo < out[j].Repo
		})
		return out, nil
	})
}

func splitRepoTag(t string) (string, string) {
	if i := strings.LastIndex(t, ":"); i > 0 {
		return t[:i], t[i+1:]
	}
	return t, "latest"
}

// ImageExists 本地是否已有该 tag
func ImageExists(image string) bool {
	repo, tag := splitRepoTag(image)
	images, err := ListImages()
	if err != nil {
		return false
	}
	for _, im := range images {
		if im.Repo == repo && im.Tag == tag {
			return true
		}
	}
	return false
}

// RemoveImage 删除本地镜像（按 tag 或 ID）。force=true 时连带删除依赖它的子镜像；
// 仍被容器引用时 daemon 会返回 409 冲突，前端原样提示即可
func RemoveImage(image string, force bool) error {
	q := url.Values{}
	if force {
		q.Set("force", "1")
	}
	var out []map[string]string
	return do(http.MethodDelete, "/images/"+image, q, nil, &out)
}

// pullHint 把 daemon 回传的拉取失败翻成可执行的下一步：
// “Docker API 错误(500): context deadline exceeded” 这种原文对用户没有信息量，
// 真正原因绝大多数是引擎所在的虚拟机访不到 registry（DNS/代理/防火墙/没配加速器）。
func pullHint(err error) error {
	if err == nil {
		return nil
	}
	l := strings.ToLower(err.Error())
	switch {
	case strings.Contains(l, "context deadline exceeded"), strings.Contains(l, "client.timeout"),
		strings.Contains(l, "tls handshake timeout"), strings.Contains(l, "i/o timeout"),
		strings.Contains(l, "no such host"), strings.Contains(l, "dial tcp"):
		return fmt.Errorf("%w；引擎连不上镜像仓库（不是本工具的问题，是 Docker Desktop 所在虚拟机出不去）："+
			"若刚切换过后端或 WSL2 本身异常，先重启 Docker Desktop（必要时 wsl --update 后重启计算机）再拉；"+
			"否则先在 Docker Desktop → Settings → Resources → Proxies 确认代理，"+
			"或打开面板右上角「仓库 → 加速站」加一个镜像加速地址（保存并重启引擎后生效）；"+
			"也可改用一个内网可达的仓库前缀", err)
	case strings.Contains(l, "pull access denied"), strings.Contains(l, "repository name not known"),
		strings.Contains(l, "manifest unknown"):
		return fmt.Errorf("%w；仓库里找不到这个镜像或 tag：确认名称（library/mysql 可直接写 mysql）与 tag 是否存在", err)
	case strings.Contains(l, "toomanyrequests"), strings.Contains(l, "rate limit"):
		return fmt.Errorf("%w；仓库限流（匿名拉取额度用尽）：稍后再试，或登录 Docker Hub、配镜像加速器", err)
	}
	return err
}

// 拉取流的空闲阈值：daemon 会重复发同样的“重试中”消息，所以要看进度有没有变化，
// 不能只看“有没有消息”；没进度先给一次可重试的提醒，再不动就彻底断流。
const (
	pullStallWarn = 45 * time.Second // 无进度这么久：提前提醒，界面上可直接重试
	pullStallStop = 90 * time.Second // 再无进度：断开流，按失败收尾
)

// pullWatcher 记录「最后一次进度变化」的时刻与最后一条状态。
// 进度指纹由层 ID / 状态 / 字节数组成，daemon 反复重试同一层时指纹不变，
// 因此能被识别为停滞；真正在下载时 Current 会增长，指纹就变了。
type pullWatcher struct {
	mu     sync.Mutex
	sig    string
	at     time.Time
	status string
	warned bool
}

func (w *pullWatcher) mark(sig, status string) {
	w.mu.Lock()
	if sig != w.sig {
		w.sig = sig
		w.at = time.Now()
		w.warned = false
	}
	w.status = status
	w.mu.Unlock()
}

// idle 返回距上次进度变化多久、最后一条状态
func (w *pullWatcher) idle() (time.Duration, string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return time.Since(w.at), w.status
}

// takeWarn 同一段停滞只提醒一次（进度一动就重置）
func (w *pullWatcher) takeWarn() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.warned {
		return false
	}
	w.warned = true
	return true
}

// PullImage 拉取镜像，分层与整体进度经 "docker:pull" 事件推送（同步阻塞至完成）。
// 带仓库凭据（能匹配上「仓库管理」里的地址时），可随时被 CancelImageOp 取消。
func PullImage(image string) error {
	repo, tag := splitRepoTag(image)
	if repo == "" {
		return fmt.Errorf("镜像名不能为空")
	}
	q := url.Values{}
	q.Set("fromImage", repo)
	q.Set("tag", tag)
	hdr := map[string]string{}
	if a := registryAuthHeader(repo + ":" + tag); a != "" {
		hdr["X-Registry-Auth"] = a
	}
	emit("docker:pull", PullProgress{Image: image, Stage: "info", Note: "开始拉取 " + image})
	applog.Infof("docker", "拉取开始 %s", image)
	started := time.Now()
	ctx, cancel := beginOp("pull " + image)
	defer endOp(cancel)
	resp, err := openStream(ctx, http.MethodPost, "/images/create", q, hdr, nil, "", true)
	if err != nil {
		e := pullHint(cancelErr(ctx, err))
		// 用户取消不算失败：用 canceled 阶段，前端显示「已取消」而非红字报错
		stage := "error"
		if IsCanceled(e) {
			stage = "canceled"
			applog.Infof("docker", "拉取已取消 %s", image)
		} else {
			applog.Errorf("docker", "拉取失败 %s（连不上 daemon）: %v", image, e)
		}
		emit("docker:pull", PullProgress{Image: image, Stage: stage, Note: e.Error()})
		return e
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, apiErrBodyMax))
		e := pullHint(apiError(resp.StatusCode, data))
		applog.Errorf("docker", "拉取失败 %s（HTTP %d）: %v", image, resp.StatusCode, e)
		emit("docker:pull", PullProgress{Image: image, Stage: "error", Note: e.Error()})
		return e
	}
	dec := json.NewDecoder(resp.Body)
	tr := &layerTracker{cur: map[string]int64{}, tot: map[string]int64{}, done: map[string]bool{},
		seen: map[string]struct{}{}}

	// 流式拉取的兜底：某层在 daemon / 加速站那卡住时 Decode 会无限阻塞（或 daemon
	// 反复重试同一层只重发同样的消息），任务就永远「进行中」。这里看进度指纹：
	// 先到 pullStallWarn 发一次可重试的提醒，再到 pullStallStop 就断流收尾。
	watch := &pullWatcher{at: time.Now()}
	var stalled atomic.Bool
	// 所有层都已完成的标志：daemon 有时拉完了却不关流，不能把这种成功当成卡死
	var allDone atomic.Bool
	var streamDone atomic.Bool
	watchDone := make(chan struct{})
	defer close(watchDone)
	go func() {
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-watchDone:
				return
			case <-tick.C:
			}
			idleFor, last := watch.idle()
			if allDone.Load() {
				// 层全部完成、只是流没关：等足一分钟仍无新消息就静默收尾（成功）。
				// 时间给得比 stall 提醒长，避开“前几层已存在、剩下的还没开始”的早期误判。
				if idleFor >= time.Minute {
					streamDone.Store(true)
					applog.Infof("docker", "拉取 %s 所有层已完成，%s 无新消息，主动收尾", image, idleFor.Round(time.Second))
					_ = resp.Body.Close()
					return
				}
				continue
			}
			warnAt, stopAt := pullStallWarn, pullStallStop
			if strings.Contains(last, "Extracting") {
				// 解压大层时 daemon 不常报进度，放宽到分钟级
				warnAt, stopAt = 3*time.Minute, 5*time.Minute
			}
			if idleFor >= stopAt {
				stalled.Store(true)
				applog.Warnf("docker", "拉取 %s 停滞 %s 后主动断开（最后状态: %q）", image, idleFor.Round(time.Second), last)
				_ = resp.Body.Close() // 阻塞中的 Read 会因此立即返回
				return
			}
			if idleFor >= warnAt && watch.takeWarn() {
				note := fmt.Sprintf("已 %d 秒没有新的进度（最后状态：%s）。这一层的数据没传过来：可以先点「重试」；若反复卡在同一层，请在面板右上角「仓库 → 加速站」换一个来源。完整过程已写进日志（设置 → 日志目录 → app/）",
					int(idleFor.Seconds()), txtOr(last, "等待 daemon 响应"))
				applog.Warnf("docker", "拉取 %s 疑似卡住: %s", image, note)
				emit("docker:pull", PullProgress{Image: image, Stage: "stalled", Status: last, Overall: tr.overall(), Note: note})
			}
		}
	}()
	// 日志用：同一层的状态变化才记一行（Downloading 的状态串不变、字节数在变，不能逐条刷）
	lastLayerStatus := map[string]string{}
	lastDecile := -1
	for {
		var m struct {
			Status         string `json:"status"`
			ID             string `json:"id"`
			Progress       string `json:"progress"`
			Error          string `json:"error"`
			ProgressDetail struct {
				Current int64 `json:"current"`
				Total   int64 `json:"total"`
			} `json:"progressDetail"`
		}
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			if ctx.Err() != nil {
				applog.Infof("docker", "拉取已取消 %s", image)
				emit("docker:pull", PullProgress{Image: image, Stage: "canceled", Note: "拉取已取消"})
				return ErrCanceled
			}
			if streamDone.Load() {
				break // 流是我们自己断的：层都完了，按成功收尾
			}
			if stalled.Load() {
				_, last := watch.idle()
				e := fmt.Errorf("拉取卡住了：%d 秒没有新进度（最后状态: %s），已断开。重试若还卡在同一层，请在面板右上角「仓库 → 加速站」换个来源；详细过程见日志",
					int(pullStallStop.Seconds()), txtOr(last, "无响应"))
				applog.Errorf("docker", "拉取停滞结束 %s: %v", image, e)
				emit("docker:pull", PullProgress{Image: image, Stage: "error", Note: e.Error()})
				return e
			}
			// 流被截断：视为失败
			applog.Errorf("docker", "拉取流中断 %s: %v", image, err)
			emit("docker:pull", PullProgress{Image: image, Stage: "error", Note: "拉取流中断: " + err.Error()})
			return fmt.Errorf("拉取流中断: %v", err)
		}
		overall := tr.overall()
		lid := txtOr(m.ID, "main")
		lidShort := lid
		if len(lidShort) > 12 {
			lidShort = lidShort[:12]
		}
		// daemon 的收尾标志：走加速站/代理时流可能不主动关闭，见到它就收尾
		if strings.HasPrefix(m.Status, "Downloaded newer image") || strings.HasPrefix(m.Status, "Image is up to date") {
			break
		}
		if m.Error != "" {
			e := pullHint(fmt.Errorf("拉取失败: %s", m.Error))
			applog.Errorf("docker", "拉取失败 %s: %v", image, e)
			emit("docker:pull", PullProgress{Image: image, Stage: "error", Note: e.Error()})
			return e
		}
		if m.Status == "" && m.ID == "" {
			continue
		}
		stage := "downloading"
		switch {
		case strings.Contains(m.Status, "Extracting"), strings.Contains(m.Status, "Download extract"):
			stage = "extracting"
		case strings.Contains(m.Status, "Verifying Checksum"), strings.Contains(m.Status, "Download complete"),
			strings.Contains(m.Status, "Pull complete"), strings.Contains(m.Status, "Already exists"):
			stage = "extracting"
		}
		complete := strings.Contains(m.Status, "Pull complete") || strings.Contains(m.Status, "Already exists")
		tr.update(m.ID, m.ProgressDetail.Current, m.ProgressDetail.Total, complete)
		doneB, totalB, layers, doneN := tr.snapshot()
		overall = tr.overall()
		allDone.Store(layers > 0 && doneN >= layers)
		// 指纹只看“实质进度”：Downloading / Retrying 之间来回切不算进展，
		// 否则 daemon 无限重试时永远看上去“一直在动”，任务就会永远停不下去
		watch.mark(fmt.Sprintf("%d|%d|%d|%d|%d", doneB, totalB, layers, doneN, overall),
			strings.TrimSpace(lidShort+" "+m.Status))
		// 日志：哪一层走到哪一步（卡住时看最后几行就知道堵在哪）
		if prev := lastLayerStatus[lid]; prev != m.Status {
			lastLayerStatus[lid] = m.Status
			applog.Infof("docker", "拉取 %s: 层 %s → %s %s", image, lidShort, m.Status, m.Progress)
		}
		if overall/10 != lastDecile {
			lastDecile = overall / 10
			if totalB >= untrustedBytes {
				applog.Infof("docker", "拉取 %s 进度 %d%%（层 %d/%d，%d/%d 字节）", image, overall, doneN, layers, doneB, totalB)
			} else {
				applog.Infof("docker", "拉取 %s 进度 %d%%（层 %d/%d，daemon 未上报字节进度，按层数估算）", image, overall, doneN, layers)
			}
		}
		emit("docker:pull", PullProgress{
			Image:   image,
			Stage:   stage,
			Layer:   m.ID,
			Status:  m.Status,
			Percent: pctOf(m.ProgressDetail.Current, m.ProgressDetail.Total),
			Note:    m.Progress,
			Overall: overall,
			Bytes:   doneB,
			Total:   totalB,
			Layers:  layers,
			Done:    doneN,
		})
	}
	// 取消可能刚好落在流结束的一瞬：别把“用户刚点取消”当成拉取成功
	if ctx.Err() != nil {
		applog.Infof("docker", "拉取已取消 %s", image)
		emit("docker:pull", PullProgress{Image: image, Stage: "canceled", Note: "拉取已取消"})
		return ErrCanceled
	}
	applog.Infof("docker", "拉取完成 %s，用时 %s", image, time.Since(started).Round(time.Second))
	emit("docker:pull", PullProgress{Image: image, Stage: "done", Note: "镜像 " + image + " 就绪", Percent: 100, Overall: 100})
	return nil
}

// txtOr 取非空串，空时给个兼容值（日志/提示里不想看到空括号）
func txtOr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

// EnsureImage 确保本地有该镜像，缺失则拉取（mysql 启动前置）
func EnsureImage(image string) error {
	if ImageExists(image) {
		return nil
	}
	return PullImage(image)
}

/* ============================ 日志 ============================ */

type logStream struct {
	mu      sync.Mutex
	lines   []string
	stop    chan struct{}
	done    chan struct{}
	body    io.Closer
	closeOn sync.Once
}

// kill 主动结束跟随：关流使阻塞中的读立即返回
func (s *logStream) kill() {
	s.closeOn.Do(func() {
		if s.body != nil {
			_ = s.body.Close()
		}
	})
}

var (
	logMu    sync.Mutex
	logProcs = map[string]*logStream{}
)

// GetContainerLogs 读容器日志尾部 n 行（历史，含已停止容器）。
// 带 1.2 秒缓存：PHP/MySQL 面板与 Docker 日志弹窗都在 2 秒级轮询，同一份尾巴复用即可
func GetContainerLogs(id string, n int) ([]string, error) {
	if n <= 0 || n > 2000 {
		n = 200
	}
	return memoRead(fmt.Sprintf("logs:%s:%d", id, n), 1200*time.Millisecond, func() ([]string, error) {
		q := url.Values{}
		q.Set("stdout", "1")
		q.Set("stderr", "1")
		q.Set("tail", strconv.Itoa(n))
		q.Set("timestamps", "0")
		resp, err := streamRequest(http.MethodGet, "/containers/"+id+"/logs", q, false)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			data, _ := io.ReadAll(io.LimitReader(resp.Body, apiErrBodyMax))
			return nil, apiError(resp.StatusCode, data)
		}
		// tail=N 只限制行数、不限行宽：容器把一行拼到 100MB 时，不封顶就是一次
		// 100MB 的 ReadAll，而且面板还在秒级轮询同一个尾巴。读到上限就按截断处理。
		data, over, err := readCapped(resp.Body, logTailMaxBytes)
		if err != nil {
			return nil, unavailableErr(err)
		}
		tty, _ := isTTY(id)
		text := data
		if !tty {
			text, err = demuxLog(data)
			if err != nil {
				text = data // 头解析失败退回原样，避免日志完全不可见
			}
		}
		lines := splitLines(string(text))
		if len(lines) > n {
			lines = lines[len(lines)-n:]
		}
		if over {
			lines = append(lines, fmt.Sprintf("（日志过长，已在 %d MB 处截断）", logTailMaxBytes>>20))
		}
		return lines, nil
	})
}

// logTailMaxBytes 一次取容器日志尾巴的字节上限（tail 只数行，不数行宽）
const logTailMaxBytes = 16 << 20

// StreamContainerLogs 开启日志跟随（幂等）：行进环形缓冲并推 "docker:serverlog" 事件
func StreamContainerLogs(id string) error {
	logMu.Lock()
	if _, ok := logProcs[id]; ok {
		logMu.Unlock()
		return nil
	}
	s := &logStream{stop: make(chan struct{}), done: make(chan struct{})}
	logProcs[id] = s
	logMu.Unlock()

	q := url.Values{}
	q.Set("stdout", "1")
	q.Set("stderr", "1")
	q.Set("follow", "1")
	q.Set("tail", "200")
	resp, err := streamRequest(http.MethodGet, "/containers/"+id+"/logs", q, true)
	if err != nil {
		logMu.Lock()
		if logProcs[id] == s {
			delete(logProcs, id)
		}
		logMu.Unlock()
		return err
	}
	if resp.StatusCode >= 400 {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, apiErrBodyMax))
		resp.Body.Close()
		e := apiError(resp.StatusCode, data)
		logMu.Lock()
		if logProcs[id] == s {
			delete(logProcs, id)
		}
		logMu.Unlock()
		return e
	}
	tty, _ := isTTY(id)
	select {
	case <-s.stop: // 请求期间已被 Stop：不留下无人管的跟随协程
		resp.Body.Close()
		return errors.New("日志跟随已取消")
	default:
	}
	s.body = resp.Body
	// follow=1&tail=200：历史与新增同流，无需额外补齐
	go pumpLogs(id, s, resp, tty)
	return nil
}

// StopContainerLogs 停止跟随（弹窗关闭时调用；请求随之结束）
func StopContainerLogs(id string) {
	logMu.Lock()
	s := logProcs[id]
	if s != nil {
		close(s.stop)
		s.kill() // 不等下一帧，立即断开跟随
		delete(logProcs, id)
	}
	logMu.Unlock()
	if s != nil {
		<-s.done
	}
}

// ContainerLogRing 读当前环形缓冲（面板初次打开时取历史）
func ContainerLogRing(id string, n int) []string {
	logMu.Lock()
	s := logProcs[id]
	logMu.Unlock()
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 || n > len(s.lines) {
		n = len(s.lines)
	}
	out := make([]string, n)
	copy(out, s.lines[len(s.lines)-n:])
	return out
}

func pumpLogs(id string, s *logStream, resp *http.Response, tty bool) {
	defer func() {
		s.kill()
		close(s.done)
	}()
	br := bufio.NewReaderSize(resp.Body, 64*1024)
	if tty {
		// 原始字节流：按行读
		for {
			line, err := readRawLine(br)
			if line != "" {
				appendLog(id, s, line)
			}
			if err != nil {
				if err != io.EOF {
					appendLog(id, s, "日志跟随结束: "+err.Error())
				}
				return
			}
			if closed(s) {
				return
			}
		}
	}
	// 多路复用帧：8 字节头（类型 + 大端长度）
	var hdr [8]byte
	for {
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return
		}
		size := int(binary.BigEndian.Uint32(hdr[4:8]))
		if size <= 0 || size > 1<<22 {
			continue
		}
		buf := make([]byte, size)
		if _, err := io.ReadFull(br, buf); err != nil {
			return
		}
		for _, line := range strings.Split(strings.TrimRight(string(buf), "\r\n"), "\n") {
			appendLog(id, s, line)
		}
		if closed(s) {
			return
		}
	}
}

func readRawLine(br *bufio.Reader) (string, error) {
	line, err := br.ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

func closed(s *logStream) bool {
	select {
	case <-s.stop:
		return true
	default:
		return false
	}
}

func appendLog(id string, s *logStream, line string) {
	line = strings.TrimRight(line, "\r")
	if line == "" {
		return
	}
	ts := time.Now().Format("15:04:05.000")
	s.mu.Lock()
	s.lines = append(s.lines, ts+"  "+line)
	if len(s.lines) > logRingMax {
		s.lines = s.lines[len(s.lines)-logRingMax:]
	}
	s.mu.Unlock()
	emit("docker:serverlog", ContainerLogLine{ID: id, Time: ts, Line: line})
}

// isTTY 容器是否为单流（TTY）日志；查询失败按非 TTY 处理。
// 容器的 Tty 一旦创建就不会变，按容器缓存 5 分钟，别为每轮日志轮询再查一次 inspect
func isTTY(id string) (bool, error) {
	return memoRead("tty:"+id, 5*time.Minute, func() (bool, error) {
		var raw struct {
			Config struct {
				Tty bool `json:"Tty"`
			} `json:"Config"`
		}
		if err := do(http.MethodGet, "/containers/"+id+"/json", nil, nil, &raw); err != nil {
			return false, err
		}
		return raw.Config.Tty, nil
	})
}

// demuxLog 解析 Docker 多路复用日志帧
func demuxLog(data []byte) ([]byte, error) {
	var out []byte
	i := 0
	for i+8 <= len(data) {
		size := int(binary.BigEndian.Uint32(data[i+4 : i+8]))
		if data[i] > 2 || size < 0 || i+8+size > len(data) {
			return nil, fmt.Errorf("非法日志帧")
		}
		out = append(out, data[i+8:i+8+size]...)
		i += 8 + size
	}
	if i < len(data) {
		return nil, fmt.Errorf("日志帧尾部不完整")
	}
	return out, nil
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\r\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func shortID(id string) string {
	if len(id) >= 12 {
		return id[:12]
	}
	return id
}
