package nginx

// ============================================================
// 实例配置持久化 + 进程管理（nginx.exe，master + worker 模型）
// ============================================================

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-tools/appdirs"
	"go-tools/tools/portcheck"

	"github.com/shirou/gopsutil/v3/process"
)

func init() {
	// 端口占用汇总：Nginx 实例的 HTTP/HTTPS 端口都参与跨工具查重
	portcheck.Register(func() []portcheck.Owner {
		list, err := loadInstances()
		if err != nil {
			return nil
		}
		out := make([]portcheck.Owner, 0, len(list))
		for _, c := range list {
			ports := []int{c.Port}
			if c.SSLPort > 0 {
				ports = append(ports, c.SSLPort)
			}
			out = append(out, portcheck.Owner{
				Key:   "nginx:" + c.ID,
				Label: "Nginx 实例「" + c.Name + "」",
				Ports: ports,
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
		// 容器模式由镜像决定 nginx 本体，本机版本库不参与；Version 留空避免前端按本机版本展示
		cfg.Mode = ModeDocker
		cfg.Image = strings.TrimSpace(cfg.Image)
		if cfg.Image == "" {
			return fmt.Errorf("Docker 模式请选择镜像，如 %s", DefaultDockerImage)
		}
		cfg.Version = ""
	} else {
		cfg.Mode = ModeLocal
		cfg.Image = ""
		if cfg.Version == "" {
			return fmt.Errorf("请选择 Nginx 版本")
		}
	}
	if cfg.Port <= 0 {
		cfg.Port = 80
	}
	if cfg.Port > 65535 {
		return fmt.Errorf("端口非法: %d", cfg.Port)
	}
	if reason := PortTaken(cfg.Port, cfg.ID); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	if cfg.SSLPort > 0 {
		if cfg.SSLPort > 65535 {
			return fmt.Errorf("端口非法: %d", cfg.SSLPort)
		}
		if cfg.SSLPort == cfg.Port {
			return fmt.Errorf("HTTPS 端口不能与 HTTP 端口相同: %d", cfg.SSLPort)
		}
		if reason := PortTaken(cfg.SSLPort, cfg.ID); reason != "" {
			return fmt.Errorf("HTTPS %s", reason)
		}
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
	if running(id) {
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
		if c.ID == id {
			if isDockerMode(&c) {
				_ = dockerRemove(&c) // 容器随实例删除；站点目录与日志留在宿主机
			}
			continue
		}
		out = append(out, c)
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

/* ============================ 进程跟踪 ============================ */

type srvRunning struct {
	cmd         *exec.Cmd
	done        chan struct{}
	started     time.Time
	containerID string // 容器模式没有 cmd，改记容器 ID
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
	if port <= 0 {
		return false
	}
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 600*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}

// running 实例是否在跑：本程序拉起的进程存活，或端口被占用
func running(id string) bool {
	if trackedSrv(id) != nil {
		return true
	}
	if cfg, err := findInstance(id); err == nil {
		return tcpUp(cfg.Port)
	}
	return false
}

// instanceConfPath 实例 nginx.conf 路径
func instanceConfPath(id string) (string, error) {
	dir, err := instanceDir(id)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "conf", "nginx.conf"), nil
}

/* ============================ 默认站点 / 工具辅帮 ============================ */

// dirBadChars 文件名中禁止的字符（同 PHP 包保持一致）
var dirBadChars = strings.NewReplacer(
	"<", "_", ">", "_", ":", "_", "\"", "_", "/", "_", "\\", "_", "|", "_", "?", "_", "*", "_",
)

// sanitizeDirName 将实例名转为安全目录名（保留中文，非法字符替换为下划线）
func sanitizeDirName(s string) string {
	s = strings.Trim(dirBadChars.Replace(strings.TrimSpace(s)), ". ")
	if r := []rune(s); len(r) > 60 {
		s = string(r[:60])
	}
	return s
}

// defaultDocRoot 默认站点目录：<数据目录>/www/<实例名>，与 PHP 包保持一致（基础目录设置页可改）
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

// nginxConfSrcDir 返回 Nginx 安装目录下的 conf 子目录绝对路径（mime.types 等内置文件所在地）
func nginxConfSrcDir(version string) string {
	exe := nginxExe(version)
	if exe == "" {
		return ""
	}
	return filepath.Dir(exe) + "/conf" // 用正斜杠，写入 nginx.conf 后跨平台一致
}

/* ============================ 启动 / 停止 / 重载 ============================ */

// StartInstance 启动实例：端口检查 → 准备 nginx.conf → 隐藏窗口拉起 → 等待端口就绪
func StartInstance(id string) error {
	cfg, err := findInstance(id)
	if err != nil {
		return err
	}
	if trackedSrv(id) != nil {
		return fmt.Errorf("实例已在运行")
	}
	if isDockerMode(cfg) {
		return dockerStart(cfg)
	}
	if tcpUp(cfg.Port) {
		return fmt.Errorf("端口 %d 已被其他进程占用（若为外部 nginx，请先停止或改端口）", cfg.Port)
	}
	exe := nginxExe(cfg.Version)
	if exe == "" {
		return fmt.Errorf("版本 %s 不可用，请先安装", cfg.Version)
	}
	prefix, err := instanceDir(id)
	if err != nil {
		return err
	}
	confPath, err := instanceConfPath(id)
	if err != nil {
		return err
	}
	if err := ensureConf(cfg, prefix, confPath); err != nil {
		return fmt.Errorf("准备 nginx.conf 失败: %v", err)
	}
	docRoot := cfg.DocRoot
	if docRoot == "" {
		docRoot = defaultDocRoot(cfg.ID, cfg.Name)
		if err := os.MkdirAll(docRoot, 0o755); err != nil {
			return err
		}
	}

	cmd := exec.Command(exe, "-p", prefix, "-c", confPath)
	cmd.Dir = filepath.Dir(exe)
	cmd.SysProcAttr = hideWindowAttr()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动失败: %v", err)
	}
	p := &srvRunning{cmd: cmd, done: make(chan struct{}), started: time.Now()}
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
	}()

	// nginx 启动后 master 会立即返回（worker 由 master 派生），等待端口就绪
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-p.done:
			return fmt.Errorf("进程启动后退出，请查看日志：\n%s", errTail(prefix, 12))
		default:
		}
		if tcpUp(cfg.Port) {
			return nil
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("启动超时：15s 内端口 %d 未监听，请查看日志：\n%s", cfg.Port, errTail(prefix, 12))
}

// StopInstance 停止实例：nginx -s quit（优雅）兆底仅杀本实例进程树；容器模式停容器
func StopInstance(id string) error {
	cfg, err := findInstance(id)
	if err != nil {
		return err
	}
	if isDockerMode(cfg) {
		p := trackedSrv(id)
		if p == nil && dockerContainerID(cfg) == "" && !tcpUp(cfg.Port) {
			return fmt.Errorf("实例未在运行")
		}
		return dockerStop(cfg)
	}
	p := trackedSrv(id)
	if p == nil && !tcpUp(cfg.Port) {
		return fmt.Errorf("实例未在运行")
	}
	prefix, _ := instanceDir(id)
	confPath, _ := instanceConfPath(id)
	// 尝试优雅停止
	if exe := nginxExe(cfg.Version); exe != "" {
		cmd := exec.Command(exe, "-p", prefix, "-c", confPath, "-s", "quit")
		cmd.Dir = filepath.Dir(exe)
		cmd.SysProcAttr = hideWindowAttr()
		_ = cmd.Run()
	}
	if p != nil {
		select {
		case <-p.done:
		case <-time.After(8 * time.Second):
			// 超时：仅杀本实例 master 及其子进窋（不波及同机其他实例）
			if p.cmd.Process != nil {
				_ = killTree(p.cmd.Process.Pid)
			}
			select {
			case <-p.done:
			case <-time.After(3 * time.Second):
			}
		}
		srvRunMu.Lock()
		if srvProcs[id] == p {
			delete(srvProcs, id)
		}
		srvRunMu.Unlock()
	}
	// 端口仍占用且未跟踪到进窋（外部 nginx 启动的实例）：仅杀匹配本实例 prefix 的进窋
	if tcpUp(cfg.Port) && prefix != "" {
		if pids := nginxPidsForPrefix(prefix); len(pids) > 0 {
			for _, pid := range pids {
				_ = killTree(pid)
			}
			for i := 0; i < 20 && tcpUp(cfg.Port); i++ {
				time.Sleep(300 * time.Millisecond)
			}
		}
	}
	if tcpUp(cfg.Port) {
		return fmt.Errorf("停止失败：端口 %d 仍在监听", cfg.Port)
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

// ReloadInstance 重载配置（nginx -s reload）；未运行时等同启动。
// 容器模式进不了容器执行 nginx -s reload，因此「重载」就是重启容器（配置已挂载在宿主机，
// 重启后新配置一定生效，代价是短暂中断）
func ReloadInstance(id string) error {
	cfg, err := findInstance(id)
	if err != nil {
		return err
	}
	if isDockerMode(cfg) {
		return dockerReload(cfg)
	}
	if !tcpUp(cfg.Port) && trackedSrv(id) == nil {
		return StartInstance(id)
	}
	exe := nginxExe(cfg.Version)
	if exe == "" {
		return fmt.Errorf("版本 %s 不可用", cfg.Version)
	}
	prefix, _ := instanceDir(id)
	confPath, _ := instanceConfPath(id)
	cmd := exec.Command(exe, "-p", prefix, "-c", confPath, "-s", "reload")
	cmd.Dir = filepath.Dir(exe)
	cmd.SysProcAttr = hideWindowAttr()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("重载失败: %v\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// CheckConfig 校验 nginx.conf（nginx -t），返回原始输出供前端展示
func CheckConfig(id string) (string, error) {
	cfg, err := findInstance(id)
	if err != nil {
		return "", err
	}
	if isDockerMode(cfg) {
		// 把不能做与怎么做写成同一段文字，同时回到输出与 error：
		// 绑定层出错时 Promise 会 reject，前端 catch 里只能拿到 error，只返 text 的话说明会被丢掉
		msg := "Docker 模式无法在容器外执行 nginx -t。\n" +
			"要验证配置：保存后点「重载」（容器模式即重启容器），起不来就把 error.log 与容器输出摊在日志里。\n" +
			"配置报错时容器会立即退出，日志能看到 nginx 的原始报错行。"
		return msg, fmt.Errorf("%s", msg)
	}
	exe := nginxExe(cfg.Version)
	if exe == "" {
		return "", fmt.Errorf("版本 %s 不可用", cfg.Version)
	}
	prefix, _ := instanceDir(id)
	confPath, _ := instanceConfPath(id)
	cmd := exec.Command(exe, "-p", prefix, "-c", confPath, "-t")
	cmd.Dir = filepath.Dir(exe)
	cmd.SysProcAttr = hideWindowAttr()
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			text = err.Error()
		}
		return text, fmt.Errorf("配置校验未通过")
	}
	return text, nil
}

/* ============================ 状态 / 日志 ============================ */

// GetInstanceStatus 实例状态：端口 + 进程数 + 运行时长 + access.log 估算请求量
func GetInstanceStatus(id string) (InstanceStatus, error) {
	st := InstanceStatus{ID: id}
	cfg, err := findInstance(id)
	if err != nil {
		return st, err
	}
	st.Name, st.Version, st.Port, st.SSLPort = cfg.Name, cfg.Version, cfg.Port, cfg.SSLPort
	st.Mode = ModeLocal
	prefix, _ := instanceDir(id)
	st.Prefix = prefix
	st.ConfigPath = filepath.Join(prefix, "conf", "nginx.conf")
	st.DocRoot = cfg.DocRoot
	if st.DocRoot == "" {
		st.DocRoot = defaultDocRoot(cfg.ID, cfg.Name)
	}
	st.Running = tcpUp(cfg.Port)
	if isDockerMode(cfg) {
		// 容器里的 nginx 进程不在本机 namespace：不按本机 nginx.exe 误计 PID/worker，
		// 也不跑 nginxPids()（会把同机其他本地实例的进程报成本实例的）
		fillDockerStatus(cfg, &st)
		if p := trackedSrv(id); p != nil && st.UptimeSeconds == 0 {
			st.UptimeSeconds = int64(time.Since(p.started).Seconds())
		}
		if st.Running {
			st.RequestsPerSec = estimateRps(prefix, st.UptimeSeconds)
		}
		return st, nil
	}
	pids := nginxPids()
	st.Workers = len(pids)
	if p := trackedSrv(id); p != nil {
		if p.cmd != nil && p.cmd.Process != nil {
			st.PID = p.cmd.Process.Pid
		}
		st.UptimeSeconds = int64(time.Since(p.started).Seconds())
	} else if len(pids) > 0 {
		st.PID = pids[0]
		if pr, err := process.NewProcess(int32(pids[0])); err == nil {
			if ct, err := pr.CreateTime(); err == nil {
				sec := time.Since(time.UnixMilli(ct)).Seconds()
				if sec > 0 {
					st.UptimeSeconds = int64(sec)
				}
			}
		}
	}
	if st.Running {
		st.RequestsPerSec = estimateRps(prefix, st.UptimeSeconds)
	}
	return st, nil
}

// Logs 返回实例日志尾部 n 行（error.log 优先，其后接 access.log）
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
	return logFilesOf(prefix, n)
}

// logFilesOf 拼接实例 logs 目录下的 error.log 与 access.log 尾部（本地与容器模式共用）
func logFilesOf(prefix string, n int) ([]string, error) {
	errLines := tailLines(filepath.Join(prefix, "logs", "error.log"), n)
	accLines := tailLines(filepath.Join(prefix, "logs", "access.log"), n)
	var out []string
	if len(errLines) > 0 {
		out = append(out, "—— error.log ——")
		out = append(out, errLines...)
	}
	if len(accLines) > 0 {
		out = append(out, "—— access.log ——")
		out = append(out, accLines...)
	}
	return out, nil
}

// PortTaken 报告端口是否可用于（新）实例：其它实例的 HTTP/HTTPS 端口已登记，或已被进程监听。
// excludeID 用于编辑场景排除自身（端口没改时自己正在监听也算可用）。空串表示可用。
func PortTaken(port int, excludeID string) string {
	exclude := ""
	if excludeID != "" {
		exclude = "nginx:" + excludeID
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

// StopAll 停止所有由本程序拉起的 Nginx 实例（应用退出时调用）
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

/* ============================ nginx.conf 生成 ============================ */

const confMarker = "# go-tools 生成的 nginx.conf（改端口/站点根请编辑本文件后「重启」实例）"

// ensureConf 配置文件不存在时按实例参数生成；存在则保留用户内容
func ensureConf(cfg *InstanceConfig, prefix, confPath string) error {
	if data, err := os.ReadFile(confPath); err == nil && len(data) > 0 {
		return nil
	}
	docRoot := cfg.DocRoot
	if docRoot == "" {
		docRoot = defaultDocRoot(cfg.ID, cfg.Name)
		_ = os.MkdirAll(docRoot, 0o755)
	}
	workers := cfg.Workers
	if workers <= 0 {
		workers = 1
	}
	// 计估计对路径到 Nginx 安装目录的 conf 子目录，使 include 指令不依赖 prefix
	confSrc := nginxConfSrcDir(cfg.Version)
	mimePath := "mime.types"
	fastcgiPath := "fastcgi_params"
	if confSrc != "" {
		mimePath = filepath.ToSlash(filepath.Join(confSrc, "mime.types"))
		fastcgiPath = filepath.ToSlash(filepath.Join(confSrc, "fastcgi_params"))
	}
	var b strings.Builder
	b.WriteString(confMarker + "\n")
	fmt.Fprintf(&b, "worker_processes  %d;\n", workers)
	fmt.Fprintf(&b, "error_log  \"%s\";\n", filepath.ToSlash(filepath.Join(prefix, "logs", "error.log")))
	fmt.Fprintf(&b, "pid        \"%s\";\n", filepath.ToSlash(filepath.Join(prefix, "logs", "nginx.pid")))
	b.WriteString("events {\n    worker_connections  1024;\n}\n")
	b.WriteString("http {\n")
	fmt.Fprintf(&b, "    access_log      \"%s\";\n", filepath.ToSlash(filepath.Join(prefix, "logs", "access.log")))
	fmt.Fprintf(&b, "    include       \"%s\";\n", mimePath)
	b.WriteString("    default_type  application/octet-stream;\n")
	b.WriteString("    sendfile        on;\n")
	b.WriteString("    keepalive_timeout  65;\n")
	b.WriteString("    server {\n")
	fmt.Fprintf(&b, "        listen       %d;\n", cfg.Port)
	b.WriteString("        server_name  localhost;\n")
	fmt.Fprintf(&b, "        root         \"%s\";\n", filepath.ToSlash(docRoot))
	b.WriteString("        index  index.html index.php index.htm;\n")
	b.WriteString("        location / {\n            try_files $uri $uri/ /index.php?$query_string;\n        }\n")
	b.WriteString("        location ~ \\.php$ {\n")
	b.WriteString("            fastcgi_pass   127.0.0.1:9000;\n")
	b.WriteString("            fastcgi_index  index.php;\n")
	b.WriteString("            fastcgi_param  SCRIPT_FILENAME $document_root$fastcgi_script_name;\n")
	fmt.Fprintf(&b, "            include        \"%s\";\n        }\n", fastcgiPath)
	if cfg.StubStatus {
		b.WriteString("        location = /stub_status {\n            stub_status;\n            access_log off;\n        }\n")
	}
	b.WriteString("        error_page   500 502 503 504  /50x.html;\n")
	fmt.Fprintf(&b, "        location = /50x.html {\n            root   \"%s\";\n        }\n", filepath.ToSlash(filepath.Join(prefix, "html")))
	b.WriteString("    }\n")
	if cfg.SSLPort > 0 {
		b.WriteString("    server {\n")
		fmt.Fprintf(&b, "        listen       %d ssl;\n", cfg.SSLPort)
		b.WriteString("        server_name  localhost;\n")
		b.WriteString("        ssl_certificate      server.crt;\n")
		b.WriteString("        ssl_certificate_key  server.key;\n")
		fmt.Fprintf(&b, "        root         \"%s\";\n", filepath.ToSlash(docRoot))
		b.WriteString("        location / {\n            try_files $uri $uri/ =404;\n        }\n")
		b.WriteString("    }\n")
	}
	b.WriteString("}\n")
	return os.WriteFile(confPath, []byte(b.String()), 0o644)
}

// GetConfigText 返回实例 nginx.conf 全文（不存在则先生成）
func GetConfigText(id string) (string, error) {
	confPath, err := instanceConfPath(id)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(confPath); err != nil {
		cfg, err := findInstance(id)
		if err != nil {
			return "", err
		}
		prefix, _ := instanceDir(id)
		if isDockerMode(cfg) {
			// 容器实例的 listen 必须是容器端口，不能拿本地模板生成
			if err := ensureDockerConf(cfg, confPath); err != nil {
				return "", err
			}
		} else if err := ensureConf(cfg, prefix, confPath); err != nil {
			return "", err
		}
	}
	data, err := os.ReadFile(confPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SaveConfigText 写回实例 nginx.conf：先备份、写入、运行 nginx -t 校验，失败则回滚并返回错误。
func SaveConfigText(id, text string) error {
	confPath, err := instanceConfPath(id)
	if err != nil {
		return err
	}
	if _, err := instanceDir(id); err != nil {
		return err
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	// 备份原文件
	var backup []byte
	hasBackup := false
	if old, err := os.ReadFile(confPath); err == nil && len(old) > 0 {
		backup = old
		hasBackup = true
		_ = os.WriteFile(confPath+".bak", old, 0o644)
	}
	// 写入新内容
	if err := os.WriteFile(confPath, []byte(text), 0o644); err != nil {
		return err
	}
	// 容器模式跑不了 nginx -t：保存就直接生效（不回滚），用户靠重启容器 + 日志验证
	if cfg, err := findInstance(id); err == nil && isDockerMode(cfg) {
		return nil
	}
	// 运行 nginx -t 校验
	checkOut, checkErr := CheckConfig(id)
	if checkErr != nil {
		// 校验失败：回滚
		if hasBackup {
			_ = os.WriteFile(confPath, backup, 0o644)
		} else {
			_ = os.Remove(confPath)
		}
		return fmt.Errorf("配置校验失败（已回滚）：\n%s", checkOut)
	}
	return nil
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

func errTail(prefix string, k int) string {
	lines := tailLines(filepath.Join(prefix, "logs", "error.log"), k)
	if len(lines) == 0 {
		return ""
	}
	return "—— logs/error.log 尾部 ——\n" + strings.Join(lines, "\n")
}

// estimateRps 用 access.log 行数 / 运行秒数 粗估 QPS
func estimateRps(prefix string, uptime int64) float64 {
	if uptime <= 0 {
		return 0
	}
	data, err := os.ReadFile(filepath.Join(prefix, "logs", "access.log"))
	if err != nil || len(data) == 0 {
		return 0
	}
	n := int64(strings.Count(string(data), "\n"))
	if n <= 0 {
		return 0
	}
	return float64(n) / float64(uptime)
}
