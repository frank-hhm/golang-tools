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
)

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
	if cfg.Version == "" {
		return fmt.Errorf("请选择 Node.js 版本")
	}
	if cfg.Port <= 0 {
		cfg.Port = 3000
	}
	if cfg.Port > 65535 {
		return fmt.Errorf("端口非法: %d", cfg.Port)
	}
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

// StartInstance 启动静态站点服务（内置 Go HTTP 服务器）
func StartInstance(id string) error {
	cfg, err := findInstance(id)
	if err != nil {
		return err
	}
	if tracked(id) != nil {
		return fmt.Errorf("实例已在运行")
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

// StopInstance 优雅停止（http.Server.Shutdown），兜底直接关闭监听
func StopInstance(id string) error {
	s := tracked(id)
	if s == nil {
		cfg, err := findInstance(id)
		if err != nil {
			return err
		}
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

// RestartInstance 重启实例
func RestartInstance(id string) error {
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
	prefix, _ := instanceDir(id)
	st.DocRoot = cfg.DocRoot
	if st.DocRoot == "" {
		st.DocRoot = filepath.Join(prefix, "www")
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

// Logs 返回实例 access.log 尾部 n 行
func Logs(id string, n int) ([]string, error) {
	if n <= 0 || n > 2000 {
		n = 200
	}
	prefix, err := instanceDir(id)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(prefix, "logs", "access.log"))
	if err != nil {
		return nil, nil // 尚无日志
	}
	lines := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// SuggestPort 探测一个可用端口（从 from 起递增，默认基准 3000）
func SuggestPort(from int) int {
	if from <= 0 {
		from = 3000
	}
	for p := from; p < from+100; p++ {
		if freePort(p) == nil {
			return p
		}
	}
	return from
}

// StopAll 停止所有由本程序启动的静态服务（应用退出时调用）
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
