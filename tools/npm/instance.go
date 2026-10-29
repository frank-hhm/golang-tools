package npm

// ============================================================
// 实例配置持久化 + 静态服务器生命周期管理
// ============================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"Tools/tools/logtail"
	"Tools/tools/portcheck"
)

func init() {
	// 端口占用汇总：静态站点的端口参与跨工具查重
	portcheck.Register(func() []portcheck.Owner {
		list, err := loadInstances()
		if err != nil {
			return nil
		}
		out := make([]portcheck.Owner, 0, len(list))
		for _, c := range list {
			out = append(out, portcheck.Owner{
				Key:   "npm:" + c.ID,
				Label: "站点「" + c.Name + "」",
				Ports: []int{c.Port},
			})
		}
		return out
	})
}

/* ============================ 配置持久化 ============================ */

func loadInstances() ([]InstanceConfig, error) {
	path, err := instancesFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var list []InstanceConfig
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func saveInstances(list []InstanceConfig) error {
	path, err := instancesFilePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ListInstances 返回全部实例配置
func ListInstances() ([]InstanceConfig, error) { return loadInstances() }

// SaveInstance 新增或更新实例配置（ID 为空则生成）
func SaveInstance(cfg InstanceConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("实例名称不能为空")
	}
	// 模式分叉：容器模式的站点由 nginx 容器承载，跟本机 Node 版本库没关系了
	useDocker := isDockerMode(&cfg)
	if useDocker {
		cfg.Mode = ModeDocker
		cfg.Image = strings.TrimSpace(cfg.Image)
		if cfg.Image == "" {
			return fmt.Errorf("Docker 模式请选择镜像，如 %s", DefaultDockerImage)
		}
		// 置空 Version：留着旧值会让面板显示一个并未参与运行的 Node 版本号
		cfg.Version = ""
	} else {
		cfg.Mode = ModeLocal
		cfg.Image = ""
		cfg.ContainerName = ""
		if cfg.Version == "" {
			return fmt.Errorf("请选择 Node.js 版本")
		}
	}
	if cfg.Port <= 0 {
		cfg.Port = 3000
	}
	if cfg.Port > 65535 {
		return fmt.Errorf("端口非法: %d", cfg.Port)
	}
	if reason := PortTaken(cfg.Port, cfg.ID); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	if !useDocker {
		vers, _ := ListVersions()
		ok := false
		for _, v := range vers {
			if v.Version == cfg.Version {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("版本 %s 未安装", cfg.Version)
		}
	}
	// 站点根目录与模式无关：两种模式都读同一个路径（留空则用实例目录下自动创建的 www/）
	if cfg.DocRoot != "" {
		if st, err := os.Stat(cfg.DocRoot); err != nil || !st.IsDir() {
			return fmt.Errorf("站点根目录不存在：%s", cfg.DocRoot)
		}
	}
	list, err := loadInstances()
	if err != nil {
		return err
	}
	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("inst-%d", time.Now().UnixNano())
	}
	for i := range list {
		if list[i].ID == cfg.ID {
			list[i] = cfg
			return saveInstances(list)
		}
	}
	list = append(list, cfg)
	return saveInstances(list)
}

// RemoveInstance 删除实例配置；运行中先停止
func RemoveInstance(id string) error {
	if c, err := findInstance(id); err == nil && isDockerMode(c) {
		// 容器实例：先停再删容器（实例目录与站点目录保留，与本地模式语义一致）
		if trackedDocker(id) != nil || dockerContainerID(c) != "" {
			_ = dockerStop(c)
		}
		defer func() { _ = dockerRemove(c) }()
	}
	if tracked(id) != nil {
		if err := StopInstance(id); err != nil {
			return fmt.Errorf("停止失败，未删除: %v", err)
		}
	}
	list, err := loadInstances()
	if err != nil {
		return err
	}
	out := list[:0]
	for _, c := range list {
		if c.ID != id {
			out = append(out, c)
		}
	}
	return saveInstances(out)
}

func findInstance(id string) (*InstanceConfig, error) {
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

// nodeOfInstance 实例关联的 Node 版本（展示用；找不到返回空串）
func nodeOfInstance(id string) string {
	cfg, err := findInstance(id)
	if err != nil {
		return ""
	}
	return cfg.Version
}

/* ============================ 进程/服务器跟踪 ============================ */

var (
	runMu  sync.Mutex
	runSrv = map[string]*StaticServer{}
)

func tracked(id string) *StaticServer {
	runMu.Lock()
	defer runMu.Unlock()
	return runSrv[id]
}

// StartInstance 启动静态站点服务（本地：内置 Go HTTP 服务器；Docker：nginx 容器）
func StartInstance(id string) error {
	cfg, err := findInstance(id)
	if err != nil {
		return err
	}
	if tracked(id) != nil {
		return fmt.Errorf("实例已在运行")
	}
	if isDockerMode(cfg) {
		return dockerStart(cfg)
	}
	if tcpUp(cfg.Port) {
		return fmt.Errorf("端口 %d 已被其他进程占用，请先停止或改端口", cfg.Port)
	}
	if nodeExe(cfg.Version) == "" {
		return fmt.Errorf("版本 %s 不可用，请先安装", cfg.Version)
	}
	prefix, err := instanceDir(id)
	if err != nil {
		return err
	}
	docRoot := cfg.DocRoot
	if docRoot == "" {
		docRoot = filepath.Join(prefix, "www")
		if err := os.MkdirAll(docRoot, 0o755); err != nil {
			return err
		}
	}
	lf, err := os.OpenFile(filepath.Join(prefix, "logs", "access.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	s := &StaticServer{
		id:      id,
		port:    cfg.Port,
		docRoot: docRoot,
		opts:    StaticOptions{SPA: cfg.SPA, Directory: cfg.Directory},
		logFile: lf,
		started: time.Now(),
	}
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Port))
	if err != nil {
		_ = lf.Close()
		return fmt.Errorf("监听端口 %d 失败: %v", cfg.Port, err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.serveHTTP)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}

	runMu.Lock()
	runSrv[id] = s
	runMu.Unlock()

	go func() {
		_ = s.srv.Serve(ln)
	}()

	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if tcpUp(cfg.Port) {
			if cfg.OpenBrowser {
				openURL(fmt.Sprintf("http://127.0.0.1:%d/", cfg.Port))
			}
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	_ = StopInstance(id)
	return fmt.Errorf("启动超时：端口 %d 未就绪", cfg.Port)
}

// StopInstance 优雅停止（http.Server.Shutdown），兜底直接关闭监听；容器模式停容器
func StopInstance(id string) error {
	cfg, err := findInstance(id)
	if err != nil {
		return err
	}
	if isDockerMode(cfg) {
		if trackedDocker(id) == nil && dockerContainerID(cfg) == "" && !tcpUp(cfg.Port) {
			return fmt.Errorf("实例未在运行")
		}
		return dockerStop(cfg)
	}
	s := tracked(id)
	if s == nil {
		if tcpUp(cfg.Port) {
			return fmt.Errorf("端口 %d 被外部进程占用（非本程序启动，无法停止）", cfg.Port)
		}
		return fmt.Errorf("实例未在运行")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = s.srv.Shutdown(ctx)
	_ = s.srv.Close()
	if s.logFile != nil {
		_ = s.logFile.Close()
	}
	runMu.Lock()
	if runSrv[id] == s {
		delete(runSrv, id)
	}
	runMu.Unlock()
	return nil
}

// RestartInstance 重启实例（容器模式：先停容器再起，镜像/端口/挂载变了会重建）
func RestartInstance(id string) error {
	if cfg, err := findInstance(id); err == nil && isDockerMode(cfg) {
		return dockerReload(cfg)
	}
	if tracked(id) != nil {
		if err := StopInstance(id); err != nil {
			return err
		}
		time.Sleep(200 * time.Millisecond)
	}
	return StartInstance(id)
}

// GetInstanceStatus 实例状态：端口连通 + 计数 + 站点文件统计
func GetInstanceStatus(id string) (InstanceStatus, error) {
	st := InstanceStatus{ID: id}
	cfg, err := findInstance(id)
	if err != nil {
		return st, err
	}
	st.Name, st.Version, st.Port = cfg.Name, cfg.Version, cfg.Port
	st.Mode = ModeLocal
	prefix, _ := instanceDir(id)
	st.DocRoot = cfg.DocRoot
	if st.DocRoot == "" {
		st.DocRoot = filepath.Join(prefix, "www")
	}
	if isDockerMode(cfg) {
		// 容器模式：站点文件统计照旧（挂的就是这个目录），Node 相关字段与 PID 没有意义；
		// Bytes 已在 fillDockerStatus 里按 access.log 统计成流量，不能再被目录总字节顶掉
		fillDockerStatus(cfg, &st, prefix)
		st.Running = tcpUp(cfg.Port)
		st.Files, _ = statTree(st.DocRoot)
		return st, nil
	}
	st.NodePath = nodeExe(cfg.Version)
	st.NpmVer = detectNpmVer(st.NodePath)
	st.Registry = RegistryOf(cfg.Version)

	s := tracked(id)
	if s != nil {
		st.Running = tcpUp(cfg.Port)
		st.PID = os.Getpid()
		st.UptimeSeconds = int64(time.Since(s.started).Seconds())
		st.Requests = readInt64(&s.requests)
		st.Bytes = readInt64(&s.bytesSent)
		if st.UptimeSeconds > 0 {
			st.RequestsPerSec = float64(st.Requests) / float64(st.UptimeSeconds)
		}
	} else {
		st.Running = tcpUp(cfg.Port)
	}
	if st.DocRoot != "" {
		st.Files, st.Bytes = statTree(st.DocRoot)
	}
	return st, nil
}

// Logs 返回实例 access.log 尾部 n 行（容器模式的 access.log 由容器写到挂载的同一份文件）
func Logs(id string, n int) ([]string, error) {
	if n <= 0 || n > 2000 {
		n = 200
	}
	prefix, err := instanceDir(id)
	if err != nil {
		return nil, err
	}
	if cfg, err := findInstance(id); err == nil && isDockerMode(cfg) {
		return dockerLogsBlend(cfg, prefix, n)
	}
	return tailFile(filepath.Join(prefix, "logs", "access.log"), n)
}

// tailFile 文件尾部 n 行；文件还不存在返回空（面板当成“暂无日志”）
func tailFile(path string, n int) ([]string, error) {
	// 以前为了拿尾巴 n 行把整个 access.log 读进内存再拆分：站点跑几天就是几百 MB，
	// 日志面板一刷新就是一次几百 MB 瞬时分配。改成只读末尾一段。
	return logtail.Lines(path, n)
}

// PortTaken 报告端口是否不可用于（新）站点：被其它已登记实例的端口占用，或已被进程监听。
// excludeID 用于编辑场景排除自身（含自身正在运行的监听）。返回非空字符串即占用原因，空串表示可用。
func PortTaken(port int, excludeID string) string {
	exclude := ""
	if excludeID != "" {
		exclude = "npm:" + excludeID
	}
	return portcheck.Taken(port, exclude)
}

// SuggestPort 探测一个可用端口（从 from 起递增，默认基准 3000）：
// 既跳过被其它已登记实例占用的端口，也跳过系统里已无法绑定的端口。
func SuggestPort(from int) int {
	if from <= 0 {
		from = 3000
	}
	return portcheck.Suggest(from, 300)
}

// StopAll 停止全部由本程序启动的静态服务（应用退出时调用）
func StopAll() {
	runMu.Lock()
	ids := make([]string, 0, len(runSrv))
	for id := range runSrv {
		ids = append(ids, id)
	}
	runMu.Unlock()
	for _, id := range ids {
		_ = StopInstance(id)
	}
	// 容器实例不在 runSrv 里：同样逐个停（只停容器，不删，下次启动复用）
	for _, id := range dockerRunIDs() {
		if cfg, err := findInstance(id); err == nil {
			_ = dockerStop(cfg)
		}
	}
}

/* ============================ 小工具 ============================ */

// statTree 统计站点目录的文件数与总字节数
func statTree(dir string) (files int, bytes int64) {
	_ = filepath.Walk(dir, func(p string, st os.FileInfo, err error) error {
		if err != nil || st == nil || st.IsDir() {
			return nil
		}
		files++
		bytes += st.Size()
		return nil
	})
	return files, bytes
}

// readInt64 原子读取计数器
func readInt64(p *int64) int64 { return atomic.LoadInt64(p) }
