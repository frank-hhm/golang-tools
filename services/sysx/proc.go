package sysx

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-tools/appdirs"
	"go-tools/services/applog"
	"go-tools/tools/logtail"
)

// ============================================================
// direct 实现：面板自己 exec 子进程
//
// 适用：Windows（无 init 系统）、容器内、非 root 的 Linux。
// 与 systemd 实现的差别（调用方需知晓）：
//   - 面板进程退出后，子进程不会自动被托管（Windows 下子进程存活，Linux 下随会话走）
//   - 日志由本包重定向到文件，不做轮转
//   - AutoStart 无效（开机自启只能靠 systemd）
// ============================================================

const (
	defaultProcLogLines = 200
	procStopTimeout     = 8 * time.Second
)

// directProc 直接托管实现。
type directProc struct {
	mu    sync.Mutex
	specs map[string]ProcSpec
	runs  map[string]*directRun
}

// directRun 一次运行期的句柄。
type directRun struct {
	cmd     *exec.Cmd
	pid     int
	started time.Time
	done    chan struct{} // 进程退出后关闭；关闭前 code/err 已写好（happens-before 由 close 保证）
	code    int
	err     error
	logPath string
	logFile *os.File
}

// newDirectProc 创建直接托管实现（Windows 与 Linux 共用）。
func newDirectProc() Proc {
	return &directProc{
		specs: make(map[string]ProcSpec),
		runs:  make(map[string]*directRun),
	}
}

// Ensure 校验规格、补全日志路径并登记。
func (p *directProc) Ensure(spec ProcSpec) error {
	if err := validateSpec(spec); err != nil {
		return err
	}
	if st, err := os.Stat(spec.Bin); err != nil || st.IsDir() {
		return fmt.Errorf("可执行文件不可用: %s", spec.Bin)
	}
	logPath, err := procLogPath(spec)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return fmt.Errorf("创建日志目录失败: %w", err)
	}
	spec.LogFile = logPath

	p.mu.Lock()
	p.specs[spec.ID] = spec
	p.mu.Unlock()
	return nil
}

// Start 启动（已在运行则直接返回）。
func (p *directProc) Start(id string) error {
	p.mu.Lock()
	spec, ok := p.specs[id]
	if !ok {
		p.mu.Unlock()
		return fmt.Errorf("未登记的服务: %s", id)
	}
	if r := p.runs[id]; r != nil && !isDone(r.done) {
		p.mu.Unlock()
		return nil
	}
	logFile, err := os.OpenFile(spec.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		p.mu.Unlock()
		return fmt.Errorf("打开日志文件失败: %w", err)
	}

	cmd := exec.Command(spec.Bin, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = mergeEnv(os.Environ(), spec.Env)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	procAttr(cmd)

	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		p.mu.Unlock()
		return fmt.Errorf("启动 %s 失败: %w", filepath.Base(spec.Bin), err)
	}
	r := &directRun{
		cmd:     cmd,
		pid:     cmd.Process.Pid,
		started: time.Now(),
		done:    make(chan struct{}),
		logPath: spec.LogFile,
		logFile: logFile,
	}
	p.runs[id] = r
	p.mu.Unlock()

	applog.Infof("sysx", "启动服务 %s/%s pid=%d bin=%s", spec.Service, id, r.pid, spec.Bin)
	go func() {
		err := cmd.Wait()
		r.code, r.err = 0, err
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				r.code = ee.ExitCode()
			} else {
				r.code = -1
			}
		}
		_ = logFile.Close()
		close(r.done)
		if r.code != 0 {
			applog.Warnf("sysx", "服务 %s/%s 退出 code=%d err=%v", spec.Service, id, r.code, err)
		}
	}()
	return nil
}

// Stop 结束进程（含子进程树），等待退出。
func (p *directProc) Stop(id string) error {
	p.mu.Lock()
	r := p.runs[id]
	p.mu.Unlock()
	if r == nil || isDone(r.done) {
		return nil
	}
	_ = killTree(r.cmd.Process)
	select {
	case <-r.done:
		return nil
	case <-time.After(procStopTimeout):
		return fmt.Errorf("等待进程 %s 退出超时", id)
	}
}

// Reload 直接托管没有"平滑重载"的通用手段，退化为重启。
func (p *directProc) Reload(id string) error {
	if err := p.Stop(id); err != nil {
		return err
	}
	return p.Start(id)
}

// Status 查询运行状态。
func (p *directProc) Status(id string) (ProcStatus, error) {
	p.mu.Lock()
	_, ok := p.specs[id]
	r := p.runs[id]
	p.mu.Unlock()

	st := ProcStatus{ID: id, Backend: "direct"}
	if !ok {
		st.Message = "未登记"
		return st, fmt.Errorf("未登记的服务: %s", id)
	}
	if r == nil {
		return st, nil
	}
	if isDone(r.done) {
		st.ExitCode = r.code
		if r.err != nil {
			st.Message = r.err.Error()
		}
		return st, nil
	}
	st.Running = true
	st.PID = r.pid
	return st, nil
}

// Logs 读取日志末 n 行。
func (p *directProc) Logs(id string, lines int) ([]string, error) {
	p.mu.Lock()
	r := p.runs[id]
	spec, ok := p.specs[id]
	p.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("未登记的服务: %s", id)
	}
	path := spec.LogFile
	if r != nil && r.logPath != "" {
		path = r.logPath
	}
	if lines <= 0 {
		lines = defaultProcLogLines
	}
	return logtail.Lines(path, lines)
}

// Remove 停止并注销（日志保留，便于事后排查）。
func (p *directProc) Remove(id string) error {
	if err := p.Stop(id); err != nil {
		return err
	}
	p.mu.Lock()
	delete(p.specs, id)
	delete(p.runs, id)
	p.mu.Unlock()
	return nil
}

// ============================================================
// 公共辅助
// ============================================================

// validateSpec 校验 ID/Bin，并挡住目录穿越（ID 会被拼进日志文件名）。
func validateSpec(spec ProcSpec) error {
	id := strings.TrimSpace(spec.ID)
	if id == "" {
		return fmt.Errorf("ProcSpec.ID 不能为空")
	}
	if strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
		return fmt.Errorf("ProcSpec.ID 非法（不能含路径分隔符）: %s", id)
	}
	if strings.TrimSpace(spec.Bin) == "" {
		return fmt.Errorf("ProcSpec.Bin 不能为空")
	}
	return nil
}

// procLogPath 计算日志落盘路径（未显式指定时落 <logs>/<service>/<id>.log）。
func procLogPath(spec ProcSpec) (string, error) {
	if v := strings.TrimSpace(spec.LogFile); v != "" {
		return v, nil
	}
	base := appdirs.LogsBase()
	if svc := strings.TrimSpace(spec.Service); svc != "" {
		dir, err := appdirs.Logs(svc)
		if err != nil {
			return "", err
		}
		base = dir
	}
	if base == "" {
		return "", fmt.Errorf("日志目录尚未初始化")
	}
	return filepath.Join(base, spec.ID+".log"), nil
}

// mergeEnv 在基础环境上追加/覆盖变量。
func mergeEnv(base []string, extra map[string]string) []string {
	if len(extra) == 0 {
		return base
	}
	out := make([]string, 0, len(base)+len(extra))
	seen := make(map[string]string, len(extra))
	for k, v := range extra {
		seen[envKey(k)] = v
	}
	for _, kv := range base {
		if i := strings.IndexByte(kv, '='); i > 0 {
			if _, ok := seen[envKey(kv[:i])]; ok {
				continue
			}
		}
		out = append(out, kv)
	}
	for k, v := range extra {
		out = append(out, k+"="+v)
	}
	return out
}

// isDone 非阻塞判断 channel 是否已关闭。
func isDone(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
