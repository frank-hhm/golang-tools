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

	"Tools/appdirs"
	"Tools/tools/logtail"
	"Tools/tools/portcheck"
	"Tools/tools/srvstate"
)

func init() {
	// 端口占用汇总：PHP 实例的端口参与跨工具查重（Nginx/MySQL 等实例也一样）
	portcheck.Register(func() []portcheck.Owner {
		list, err := loadInstances()
		if err != nil {
			return nil
		}
		out := make([]portcheck.Owner, 0, len(list))
		for _, c := range list {
			out = append(out, portcheck.Owner{
				Key:   "php:" + c.ID,
				Label: "PHP 实例「" + c.Name + "」",
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
	useDocker := isDockerMode(&cfg)
	if useDocker {
		cfg.Mode = ModeDocker
		if strings.TrimSpace(cfg.Image) == "" {
			return fmt.Errorf("Docker 模式需指定镜像，如 %s", DefaultDockerImage)
		}
		// 容器实例不引用本机版本与构型：展示版本由镜像 tag 推导，留着旧值会误导切换
		cfg.Version, cfg.NTS = "", true
	} else {
		cfg.Mode = ModeLocal
		if cfg.Version == "" {
			return fmt.Errorf("请选择 PHP 版本")
		}
	}
	if cfg.Port <= 0 {
		cfg.Port = 8080
	}
	if cfg.Port > 65535 {
		return fmt.Errorf("端口非法: %d", cfg.Port)
	}
	// 端口落在保存这一步兜底：别的实例登记过、或系统里已被监听都拒绝
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
		if useDocker {
			// 容器里只认站点目录内的相对路径，存进去就要能换算（不留到启动时才爆）
			root, err := dockerDocRoot(&cfg)
			if err != nil {
				return err
			}
			if _, err := containerRouterPath(&cfg, root); err != nil {
				return err
			}
		}
	}
	list, err := loadInstances()
	if err != nil {
		return err
	}
	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("inst-%d", time.Now().UnixNano())
	}
	if useDocker {
		// 容器名落盘（按最终 ID 生成），前端可直接展示，也保证同名实例不会互相顶掉
		cfg.ContainerName = containerNameOf(&cfg)
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

// RemoveInstance 删除实例配置；运行中先停止（实例目录与站点目录保留）
func RemoveInstance(id string) error {
	if trackedSrv(id) != nil {
		if err := StopInstance(id); err != nil {
			return fmt.Errorf("停止失败，未删除: %v", err)
		}
	}
	// 容器不随配置消失：删掉它，否则下次新建同名实例会撞上残留容器
	if cfg, err := findInstance(id); err == nil && isDockerMode(cfg) {
		if err := dockerRemove(cfg); err != nil {
			return err
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

// defaultDocRoot 默认站点目录：<数据目录>/www/<实例名>，与程序其它数据目录保持一致（基础目录设置页可改）
func defaultDocRoot(id, name string) string {
	base := filepath.Join(appdirs.DataBase(), "www")
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
	// cum 累计写入行数（含被环形挤掉的）；cleared 为「清空」时的 cum，
	// 配合 LinesAfter 实现非破坏式视图截止（只展示清空之后新写入的行）
	cum     int
	cleared int
}

func newRing(cap int) *ring { return &ring{cap: cap, start: time.Now()} }

func (r *ring) Write(p []byte) (int, error) {
	text := strings.TrimRight(string(p), "\r\n")
	if text != "" {
		r.mu.Lock()
		for _, line := range strings.Split(text, "\n") {
			r.buf = append(r.buf, line)
			r.cum++
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

// LinesAfter 返回「最近一次清空之后」新写入行的末尾 n 行（无则 nil）。
func (r *ring) LinesAfter(n int) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	skip := r.cum - r.cleared
	if skip < 0 {
		skip = 0
	}
	if skip > len(r.buf) {
		skip = len(r.buf)
	}
	avail := r.buf[skip:]
	if len(avail) == 0 {
		return nil
	}
	if n <= 0 || n > len(avail) {
		n = len(avail)
	}
	out := make([]string, n)
	copy(out, avail[len(avail)-n:])
	return out
}

// Clear 真实清空环形缓冲：丢弃已缓存行并归零计数（配合截断日志文件实现破坏式清空）。
func (r *ring) Clear() {
	r.mu.Lock()
	r.buf = nil
	r.cum = 0
	r.cleared = 0
	r.mu.Unlock()
}

type srvRunning struct {
	cmd     *exec.Cmd
	done    chan struct{}
	logFile *os.File
	mem     *ring
	// 就绪 / 主动停止标记：退出时据此区分「崩溃」与「用户停止」，见 srvstate
	srvstate.Flag
	// Docker 模式无宿主子进程，改记容器 ID
	containerID string
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

// instanceLogPath 实例访问/错误统一日志：<日志目录>/php/<实例ID>.log（按服务名识别，基础目录设置页可改）
func instanceLogPath(id string) (string, error) {
	dir, err := appdirs.Logs("php")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, filepath.Base(id)+".log"), nil
}

// legacyInstanceLogPath 旧版实例日志（实例工作目录下 php_server.log）：仅用于兼容读取历史日志
func legacyInstanceLogPath(id string) string {
	dir, err := instanceDir(id)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "php_server.log")
}

// StartInstance 启动实例：端口检查 → 按模式分叉（Docker 进容器，本地拉起 php -S）
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
	if isDockerMode(cfg) {
		return dockerStart(cfg)
	}
	cgi := cfg.RunMode == RunModeCgi
	var exe string
	if cgi {
		exe = phpCgiExe(cfg.Version, cfg.NTS)
		if exe == "" {
			exe = phpCgiExe(cfg.Version, !cfg.NTS)
		}
		if exe == "" {
			return fmt.Errorf("未找到 %s：FastCGI 模式需要 php-cgi 后端，Windows 用官方绿色包自带，Linux 请安装 php-cgi 包", cgiFileName)
		}
	} else {
		exe = phpExe(cfg.Version, cfg.NTS)
		if exe == "" {
			exe = phpExe(cfg.Version, !cfg.NTS)
		}
		if exe == "" {
			return fmt.Errorf("版本 %s 不可用，请先安装", cfg.Version)
		}
	}
	phpDir := filepath.Dir(exe)

	instDir, err := instanceDir(id)
	if err != nil {
		return err
	}
	// php.ini 放哪里：Windows 绿色版必须与 php.exe 同级（PHP 默认加载位置）；
	// Linux 的 /usr/bin 是系统目录写不得，实例配置放实例目录再用 -c 指定。
	iniPath := filepath.Join(instDir, "php.ini")
	if perBinaryIni {
		iniPath = filepath.Join(phpDir, "php.ini")
	}
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
	logPath, err := instanceLogPath(id)
	if err != nil {
		return err
	}
	lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}

	args := phpStartArgs(cgi, cfg.Port, docRoot, iniPath, cfg.Router)
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
		// 就绪后非主动停止的退出＝崩溃：发事件让主程序弹系统通知
		if p.Abnormal() {
			code := -1
			if cmd.ProcessState != nil {
				code = cmd.ProcessState.ExitCode()
			}
			srvEmit("php"+srvstate.EventSuffix, srvstate.NewEvent("php", "local", id, cfg.Name, cfg.Port, code))
		}
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
			p.MarkReady()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("启动超时：15s 内端口 %d 未就绪，请查看日志：\n%s", cfg.Port, tailFile(logPath, 12))
}

// StopInstance 停止实例：Docker 走容器；本地 Go 侧不保存子进程句柄，统一按 PID 结束进程树
func StopInstance(id string) error {
	cfg, err := findInstance(id)
	if err != nil {
		return err
	}
	if isDockerMode(cfg) {
		if trackedSrv(id) == nil && dockerContainerID(cfg) == "" && !tcpUp(cfg.Port) {
			return fmt.Errorf("实例未在运行")
		}
		return dockerStop(cfg)
	}
	p := trackedSrv(id)
	if p == nil && !tcpUp(cfg.Port) {
		return fmt.Errorf("实例未在运行")
	}
	// 先标记主动停止：此后进程退出属于预期，不发「异常退出」通知
	if p != nil {
		p.MarkStopping()
	}
	pid := 0
	if p != nil && p.cmd != nil && p.cmd.Process != nil {
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
		// 端口仍被占用：尝试结束本程序之前拉起的同名孤儿进程（按二进制名，会一并影响
		// 其他同名进程，故仅在用户主动点「停止」时执行）。名字用平台常量：Linux 系统包的
		// 进程名是 php，写成 php.exe 在那边永远匹配不到，孤儿进程会一直占着端口。
		killName := binFileName
		if cfg.RunMode == RunModeCgi {
			killName = cgiFileName
		}
		if pids := pidsByName(killName); len(pids) > 0 {
			for _, pid := range pids {
				_ = killTree(pid)
			}
			for i := 0; i < 20 && tcpUp(cfg.Port); i++ {
				time.Sleep(300 * time.Millisecond)
			}
		}
	}
	if tcpUp(cfg.Port) {
		return fmt.Errorf("停止失败：端口 %d 仍在监听（可能是外部启动的 PHP 进程，或端口被其他程序占用）", cfg.Port)
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
	st.RunMode = cfg.RunMode
	if st.RunMode == "" {
		st.RunMode = RunModeServer
	}
	st.Remark = cfg.Remark
	if st.DocRoot == "" {
		st.DocRoot = defaultDocRoot(id, cfg.Name)
	}
	if p, err := iniPathOf(id); err == nil {
		st.ConfigPath = p
	}
	if isDockerMode(cfg) {
		st.Running = tcpUp(cfg.Port)
		fillDockerStatus(cfg, &st) // 容器名/ID/状态与展示版本
		// 覆盖配置提前生成，面板打开即可看到并编辑（容器启动时同样挂这一份）
		if ini, err := ensureDockerIni(cfg); err == nil {
			st.ConfigPath = ini
			if text, err := os.ReadFile(ini); err == nil {
				st.DisableFunctions = iniGet(string(text), "disable_functions")
			}
		}
		return st, nil
	}
	if p := trackedSrv(id); p != nil && p.cmd != nil && p.cmd.Process != nil {
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

// phpCutoff 非破坏式「清空」截止：清空把日志文件当前大小记为字节偏移，
// 之后 Logs 只返回该偏移之后新写入的行，不删磁盘历史，也不会被下一次轮询回填。
var (
	phpCutMu  sync.Mutex
	phpCutoff = map[string]int64{}
)

func phpFileSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}

// Logs 返回实例日志尾部 n 行（Docker 取容器输出；本地优先文件，回退内存缓冲）
func Logs(id string, n int) ([]string, error) {
	if n <= 0 || n > 2000 {
		n = 200
	}
	if cfg, err := findInstance(id); err == nil && isDockerMode(cfg) {
		return dockerLogs(cfg, n)
	}
	logPath, err := instanceLogPath(id)
	if err != nil {
		return nil, err
	}
	phpCutMu.Lock()
	from := phpCutoff[id]
	phpCutMu.Unlock()
	if from > 0 {
		// 已清空：只看截止偏移之后的新内容（文件与内存环形都算）
		if lines, err := logtail.LinesAfter(logPath, from, n); err != nil {
			return nil, err
		} else if len(lines) > 0 {
			return lines, nil
		}
		if p := trackedSrv(id); p != nil {
			return p.mem.LinesAfter(n), nil
		}
		return nil, nil
	}
	if lines := tailLines(logPath, n); len(lines) > 0 {
		return lines, nil
	}
	// 兼容旧版：实例目录下的 php_server.log
	if legacy := legacyInstanceLogPath(id); legacy != "" {
		if lines := tailLines(legacy, n); len(lines) > 0 {
			return lines, nil
		}
	}
	if p := trackedSrv(id); p != nil {
		return p.mem.Lines(n), nil
	}
	return nil, nil
}

// ClearLogs 真实清空指定实例的日志（破坏式：截断日志文件 + 清空内存环形，
// 释放磁盘占用；旧行不再回填）。容器模式日志源在 daemon，本地无法截断，退化为清环形。
func ClearLogs(id string) error {
	if cfg, err := findInstance(id); err == nil && isDockerMode(cfg) {
		if p := trackedSrv(id); p != nil {
			p.mem.Clear()
		}
		return nil
	}
	if logPath, err := instanceLogPath(id); err == nil {
		_ = os.Truncate(logPath, 0)
		phpCutMu.Lock()
		phpCutoff[id] = 0
		phpCutMu.Unlock()
	}
	if p := trackedSrv(id); p != nil {
		// 进程存活时也截断自己持有的日志句柄（O_APPEND 打开，截断安全）
		if p.logFile != nil {
			_ = p.logFile.Truncate(0)
			_, _ = p.logFile.Seek(0, io.SeekStart)
		}
		p.mem.Clear()
	}
	return nil
}

// PortTaken 报告端口是否可用于（新）实例：被其它工具的实例登记过，或已被进程监听。
// excludeID 用于编辑场景排除自身（端口没改时自己正在监听也算可用）。空串表示可用。
func PortTaken(port int, excludeID string) string {
	exclude := ""
	if excludeID != "" {
		exclude = "php:" + excludeID
	}
	return portcheck.Taken(port, exclude)
}

// SuggestPort 探测一个可用端口（从 from 起递增，默认基准 8080）：
// 既跳过其它实例已登记的端口，也跳过系统里已绑不上的端口
func SuggestPort(from int) int {
	if from <= 0 {
		from = 8080
	}
	return portcheck.Suggest(from, 100)
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

// phpStartArgs 组装 php 启动参数：cgi=true 走 php-cgi -b（TCP FastCGI，docroot 由 nginx 传入），
// 否则 php -S 内置 HTTP 服务器（-t 指 docroot，可选路由脚本）。
func phpStartArgs(cgi bool, port int, docRoot, iniPath, router string) []string {
	if cgi {
		return []string{"-b", fmt.Sprintf("127.0.0.1:%d", port), "-c", iniPath}
	}
	args := []string{"-S", fmt.Sprintf("127.0.0.1:%d", port), "-t", docRoot, "-c", iniPath}
	if router != "" {
		if !filepath.IsAbs(router) {
			router = filepath.Join(docRoot, router)
		}
		args = append(args, router)
	}
	return args
}

func tailLines(path string, n int) []string {
	// 只取末尾几行却把整个日志读进内存，是面板轮询里最大的瞬时分配源
	lines, err := logtail.Lines(path, n)
	if err != nil {
		return nil
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
