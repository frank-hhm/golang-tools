package php

// ============================================================
// 实例配置持久化 + 进程管理（php -S 内置服务器）
// ============================================================

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
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
		return fmt.Errorf("请选择 PHP 版本")
	}
	if cfg.Port <= 0 {
		cfg.Port = 8080
	}
	if cfg.Port > 65535 {
		return fmt.Errorf("端口非法: %d", cfg.Port)
	}
	if vers, _ := ListVersions(); true {
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
	if cfg.DocRoot != "" {
		if st, err := os.Stat(cfg.DocRoot); err != nil || !st.IsDir() {
			return fmt.Errorf("站点根目录不存在：%s", cfg.DocRoot)
		}
	}
	if cfg.Router != "" {
		p := cfg.Router
		if !filepath.IsAbs(p) {
			root := cfg.DocRoot
			if root == "" {
				root = defaultDocRoot(cfg.ID, cfg.Name)
			}
			p = filepath.Join(root, p)
		}
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return fmt.Errorf("路由脚本不存在：%s", p)
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

// RemoveInstance 删除实例配置；运行中先停止（实例目录保留）
func RemoveInstance(id string) error {
	if trackedSrv(id) != nil {
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

// defaultDocRoot 默认站点目录：<exedir>/data/www/<实例名>，与程序其它数据目录保持一致
func defaultDocRoot(id, name string) string {
	base := ""
	if exe, err := os.Executable(); err == nil {
		base = filepath.Join(filepath.Dir(exe), "data", "www")
	} else if d, err := goToolsDir(); err == nil {
		base = filepath.Join(d, "www")
	}
	dir := sanitizeDirName(name)
	if dir == "" {
		dir = sanitizeDirName(id)
	}
	if dir == "" {
		dir = "site"
	}
	return filepath.Join(base, dir)
}

var dirBadChars = strings.NewReplacer("<", "_", ">", "_", ":", "_", "\"", "_", "/", "_", "\\", "_", "|", "_", "?", "_", "*", "_")

// sanitizeDirName 将实例名转为可用目录名（保留中文，非法字符替换为下划线）
func sanitizeDirName(s string) string {
	s = strings.Trim(dirBadChars.Replace(strings.TrimSpace(s)), ". ")
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60])
	}
	return s
}

/* ============================ 进程管理 ============================ */

// ring 日志环形缓冲（进程存活期间的输出）
type ring struct {
	mu    sync.Mutex
	buf   []string
	cap   int
	start time.Time
}

func newRing(cap int) *ring { return &ring{cap: cap, start: time.Now()} }

func (r *ring) Write(p []byte) (int, error) {
	text := strings.TrimRight(string(p), "\r\n")
	if text != "" {
		r.mu.Lock()
		for _, line := range strings.Split(text, "\n") {
			r.buf = append(r.buf, line)
		}
		if over := len(r.buf) - r.cap; over > 0 {
			r.buf = append(r.buf[:0], r.buf[over:]...)
		}
		r.mu.Unlock()
	}
	return len(p), nil
}

func (r *ring) Lines(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) == 0 {
		return nil
	}
	if n <= 0 || n > len(r.buf) {
		n = len(r.buf)
	}
	out := make([]string, n)
	copy(out, r.buf[len(r.buf)-n:])
	return out
}

type srvRunning struct {
	cmd     *exec.Cmd
	done    chan struct{}
	logFile *os.File
	mem     *ring
}

var (
	srvRunMu sync.Mutex
	srvProcs = map[string]*srvRunning{}
)

func trackedSrv(id string) *srvRunning {
	srvRunMu.Lock()
	defer srvRunMu.Unlock()
	return srvProcs[id]
}

// tcpUp 端口能建 TCP 连接即视为有服务在监听
func tcpUp(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 600*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// instanceLogPath 实例访问/错误统一日志文件
func instanceLogPath(id string) (string, error) {
	dir, err := instanceDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "php_server.log"), nil
}

// StartInstance 启动 php -S 内置服务器：
// 端口检查 → 准备 php.ini → 拉起进程（隐藏窗口，输出落文件）→ 等待端口就绪
func StartInstance(id string) error {
	cfg, err := findInstance(id)
	if err != nil {
		return err
	}
	if trackedSrv(id) != nil {
		return fmt.Errorf("实例已在运行")
	}
	if tcpUp(cfg.Port) {
		return fmt.Errorf("端口 %d 已被其他进程占用，请先停止或改端口", cfg.Port)
	}
	exe := phpExe(cfg.Version, cfg.NTS)
	if exe == "" {
		exe = phpExe(cfg.Version, !cfg.NTS)
	}
	if exe == "" {
		return fmt.Errorf("版本 %s 不可用，请先安装", cfg.Version)
	}
	phpDir := filepath.Dir(exe)

	instDir, err := instanceDir(id)
	if err != nil {
		return err
	}
	iniPath := filepath.Join(phpDir, "php.ini")
	if err := ensureIni(cfg, phpDir, iniPath); err != nil {
		return fmt.Errorf("准备 php.ini 失败: %v", err)
	}
	docRoot := cfg.DocRoot
	if docRoot == "" {
		docRoot = defaultDocRoot(id, cfg.Name)
		if err := os.MkdirAll(docRoot, 0o755); err != nil {
			return err
		}
	}
	logPath := filepath.Join(instDir, "php_server.log")
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	args := []string{"-S", fmt.Sprintf("127.0.0.1:%d", cfg.Port), "-t", docRoot, "-c", iniPath}
	if cfg.Router != "" {
		router := cfg.Router
		if !filepath.IsAbs(router) {
			router = filepath.Join(docRoot, router)
		}
		args = append(args, router)
	}
	cmd := exec.Command(exe, args...)
	cmd.Dir = instDir
	cmd.SysProcAttr = hideWindowAttr()
	w := newRing(600)
	cmd.Stdout = io.MultiWriter(lf, w)
	cmd.Stderr = io.MultiWriter(lf, w)

	if err := cmd.Start(); err != nil {
		_ = lf.Close()
		return fmt.Errorf("启动失败: %v", err)
	}
	p := &srvRunning{cmd: cmd, done: make(chan struct{}), logFile: lf, mem: w}
	srvRunMu.Lock()
	srvProcs[id] = p
	srvRunMu.Unlock()

	go func() {
		_ = cmd.Wait()
		close(p.done)
		srvRunMu.Lock()
		if srvProcs[id] == p {
			delete(srvProcs, id)
		}
		srvRunMu.Unlock()
		_ = lf.Close()
	}()

	// 等待就绪（最多 15s；进程提前退出则立即报错并附日志尾部）
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-p.done:
			return fmt.Errorf("进程启动后退出，请查看日志：\n%s", tailFile(logPath, 12))
		default:
		}
		if tcpUp(cfg.Port) {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("启动超时：15s 内端口 %d 未就绪，请查看日志：\n%s", cfg.Port, tailFile(logPath, 12))
}

// StopInstance 停止实例：Go 侧不保存 Windows 子进程句柄，统一按 PID 结束进程树
func StopInstance(id string) error {
	cfg, err := findInstance(id)
	if err != nil {
		return err
	}
	p := trackedSrv(id)
	if p == nil && !tcpUp(cfg.Port) {
		return fmt.Errorf("实例未在运行")
	}
	pid := 0
	if p != nil && p.cmd.Process != nil {
		pid = p.cmd.Process.Pid
	}
	if pid > 0 {
		_ = killTree(pid)
	}
	if p != nil {
		select {
		case <-p.done:
		case <-time.After(6 * time.Second):
		}
		srvRunMu.Lock()
		if srvProcs[id] == p {
			delete(srvProcs, id)
		}
		srvRunMu.Unlock()
	}
	if tcpUp(cfg.Port) {
		// 端口仍被占用：尝试结束本程序之前拉起的同名孤儿进程（按镜像名，会一并影响
		// 其他同名进程，故仅在用户主动点「停止」时执行）
		if pids := pidsByName("php.exe"); len(pids) > 0 {
			for _, pid := range pids {
				_ = killTree(pid)
			}
			for i := 0; i < 20 && tcpUp(cfg.Port); i++ {
				time.Sleep(300 * time.Millisecond)
			}
		}
	}
	if tcpUp(cfg.Port) {
		return fmt.Errorf("停止失败：端口 %d 仍在监听（可能是外部启动的 php.exe 或服务被其他程序占用）", cfg.Port)
	}
	return nil
}

// RestartInstance 重启实例
func RestartInstance(id string) error {
	if err := StopInstance(id); err != nil && !strings.Contains(err.Error(), "未在运行") {
		return err
	}
	return StartInstance(id)
}

// GetInstanceStatus 实例状态：端口探测 + php.ini 概览
func GetInstanceStatus(id string) (InstanceStatus, error) {
	st := InstanceStatus{ID: id}
	cfg, err := findInstance(id)
	if err != nil {
		return st, err
	}
	st.Name, st.Version, st.Port = cfg.Name, cfg.Version, cfg.Port
	st.DocRoot = cfg.DocRoot
	if st.DocRoot == "" {
		st.DocRoot = defaultDocRoot(id, cfg.Name)
	}
	if p, err := iniPathOf(id); err == nil {
		st.ConfigPath = p
	}
	if p := trackedSrv(id); p != nil && p.cmd.Process != nil {
		st.PID = p.cmd.Process.Pid
		st.UptimeSeconds = int64(time.Since(p.mem.start).Seconds())
	}
	st.Running = tcpUp(cfg.Port)
	if text, err := os.ReadFile(st.ConfigPath); err == nil {
		st.DisableFunctions = iniGet(string(text), "disable_functions")
	}
	// 已加载扩展数：优先实测（php -m），失败退回 ini 统计
	exe := phpExe(cfg.Version, cfg.NTS)
	if exe == "" {
		exe = phpExe(cfg.Version, !cfg.NTS)
	}
	if exe != "" {
		if info, err := runPHPInfo(exe, st.ConfigPath); err == nil {
			st.ExtensionsLoaded = len(info.Loaded)
			if info.Version != "" {
				st.Version = cfg.Version
			}
		}
	}
	return st, nil
}

// Logs 返回实例日志尾部 n 行（优先文件，回退内存缓冲）
func Logs(id string, n int) ([]string, error) {
	if n <= 0 || n > 2000 {
		n = 200
	}
	logPath, err := instanceLogPath(id)
	if err != nil {
		return nil, err
	}
	if lines := tailLines(logPath, n); len(lines) > 0 {
		return lines, nil
	}
	if p := trackedSrv(id); p != nil {
		return p.mem.Lines(n), nil
	}
	return nil, nil
}

// SuggestPort 探测一个可用端口（从 from 起递增，默认基准 8080）
func SuggestPort(from int) int {
	if from <= 0 {
		from = 8080
	}
	for p := from; p < from+100; p++ {
		ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", p))
		if err == nil {
			_ = ln.Close()
			return p
		}
	}
	return from
}

// StopAll 停止所有由本程序拉起的 PHP 实例（应用退出时调用）
func StopAll() {
	srvRunMu.Lock()
	ids := make([]string, 0, len(srvProcs))
	for id := range srvProcs {
		ids = append(ids, id)
	}
	srvRunMu.Unlock()
	for _, id := range ids {
		_ = StopInstance(id)
	}
}

// pidsByName 由本程序管理的同名进程 PID（Windows 下 Go 不保存子进程句柄时的兜底）
func pidsByName(name string) []int {
	return findPidsByName(name)
}

/* ============================ 小工具 ============================ */

func tailLines(path string, n int) []string {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return nil
	}
	lines := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines
}

func tailFile(path string, k int) string {
	lines := tailLines(path, k)
	if len(lines) == 0 {
		return ""
	}
	return "—— php_server.log 尾部 ——\n" + strings.Join(lines, "\n")
}
