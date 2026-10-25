//go:build linux

package sysx

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"

	"go-tools/services/applog"
	"go-tools/tools/logtail"
)

// ============================================================
// Linux 侧两套实现
//
//   - direct（proc.go）：面板自己 exec 子进程，非 root / 无 systemd 时使用
//   - systemd（本文件）：写 unit 交给 systemd 托管，root + systemd 环境首选
//     —— 日志、开机自启、cgroup 资源限制、崩溃可见性都由 systemd 提供，
//     面板重启也不会把服务带走。
// ============================================================

const systemdUnitDir = "/etc/systemd/system"

// procAttr 子进程独立进程组：面板退出或按 Ctrl-C 时不连带影响服务进程。
func procAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// envKey Linux 环境变量区分大小写，原样比对。
func envKey(k string) string { return k }

// killTree 结束整棵进程树（负 PID 作用到进程组）。
func killTree(p *os.Process) error {
	if p == nil {
		return nil
	}
	if err := syscall.Kill(-p.Pid, syscall.SIGKILL); err == nil {
		return nil
	}
	if err := p.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}

// systemdProc systemd 托管实现。
type systemdProc struct {
	mu    sync.Mutex
	specs map[string]ProcSpec
}

// newSystemdProc 创建 systemd 实现。
func newSystemdProc() Proc { return &systemdProc{specs: make(map[string]ProcSpec)} }

// Ensure 写 unit 文件并 daemon-reload。
func (p *systemdProc) Ensure(spec ProcSpec) error {
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

	unit := unitName(spec)
	if err := writeFileAtomic(filepath.Join(systemdUnitDir, unit), []byte(renderUnit(spec)), 0o644); err != nil {
		return fmt.Errorf("写 systemd unit 失败（需要 root 权限）: %w", err)
	}
	if _, err := systemctl("daemon-reload"); err != nil {
		return err
	}
	if spec.AutoStart {
		if _, err := systemctl("enable", unit); err != nil {
			// 自启失败不阻断启动，只记录
			applog.Warnf("sysx", "systemd enable %s 失败: %v", unit, err)
		}
	}

	p.mu.Lock()
	p.specs[spec.ID] = spec
	p.mu.Unlock()
	applog.Infof("sysx", "登记 systemd 服务 %s/%s unit=%s", spec.Service, spec.ID, unit)
	return nil
}

// Start 启动 unit。
func (p *systemdProc) Start(id string) error {
	spec, err := p.spec(id)
	if err != nil {
		return err
	}
	_, err = systemctl("start", unitName(spec))
	return err
}

// Stop 停止 unit。
func (p *systemdProc) Stop(id string) error {
	spec, err := p.spec(id)
	if err != nil {
		return err
	}
	unit := unitName(spec)
	// 已经不活跃时 systemctl stop 也是成功的，不必特判
	_, err = systemctl("stop", unit)
	return err
}

// Reload 平滑重载；unit 不支持 reload 时 systemd 会自动退化为 restart。
func (p *systemdProc) Reload(id string) error {
	spec, err := p.spec(id)
	if err != nil {
		return err
	}
	_, err = systemctl("reload-or-restart", unitName(spec))
	return err
}

// Status 查询状态（is-active + MainPID）。
func (p *systemdProc) Status(id string) (ProcStatus, error) {
	spec, err := p.spec(id)
	if err != nil {
		return ProcStatus{ID: id, Backend: "systemd", Message: "未登记"}, err
	}
	unit := unitName(spec)
	st := ProcStatus{ID: id, Backend: "systemd"}
	state, _ := systemctl("is-active", unit) // 非 active 时退出码非 0，属正常情况
	st.Running = state == "active"
	if st.Running {
		if pidText, err := systemctl("show", "-p", "MainPID", "--value", unit); err == nil {
			var pid int
			_, _ = fmt.Sscanf(pidText, "%d", &pid)
			st.PID = pid
		}
	} else if state != "" {
		st.Message = state // inactive / failed / activating
	}
	return st, nil
}

// Logs 优先读 unit 的 append 日志文件，没配日志文件时退回 journalctl。
func (p *systemdProc) Logs(id string, lines int) ([]string, error) {
	spec, err := p.spec(id)
	if err != nil {
		return nil, err
	}
	if lines <= 0 {
		lines = defaultProcLogLines
	}
	if spec.LogFile != "" {
		if out, err := logtail.Lines(spec.LogFile, lines); err == nil {
			return out, nil
		}
	}
	out, err := systemctl("--no-pager", "-n", fmt.Sprint(lines), "-o", "cat", "-u", unitName(spec))
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// Remove 停止、取消自启并删除 unit。
func (p *systemdProc) Remove(id string) error {
	spec, err := p.spec(id)
	if err != nil {
		return err
	}
	unit := unitName(spec)
	if _, err := systemctl("stop", unit); err != nil {
		applog.Warnf("sysx", "停止 %s 失败（继续删除 unit）: %v", unit, err)
	}
	if _, err := systemctl("disable", unit); err != nil {
		applog.Warnf("sysx", "disable %s 失败（继续删除 unit）: %v", unit, err)
	}
	if err := os.Remove(filepath.Join(systemdUnitDir, unit)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除 unit 失败: %w", err)
	}
	if _, err := systemctl("daemon-reload"); err != nil {
		return err
	}
	p.mu.Lock()
	delete(p.specs, id)
	p.mu.Unlock()
	return nil
}

// spec 取登记过的规格。
func (p *systemdProc) spec(id string) (ProcSpec, error) {
	p.mu.Lock()
	spec, ok := p.specs[id]
	p.mu.Unlock()
	if !ok {
		return ProcSpec{}, fmt.Errorf("未登记的服务: %s", id)
	}
	return spec, nil
}

// unitName 生成 unit 文件名：go-tools-<service>-<id>.service（只保留安全字符）。
func unitName(spec ProcSpec) string {
	return "go-tools-" + unitSlug(spec.Service) + "-" + unitSlug(spec.ID) + ".service"
}

// unitSlug 把任意标识压成 systemd 文件名/单元名允许的字符集。
func unitSlug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "x"
	}
	return out
}

// renderUnit 生成 unit 文本（Env 排序后再写，保证同样配置产出同样文件）。
func renderUnit(spec ProcSpec) string {
	var b strings.Builder
	b.WriteString("# 由 go-tools 生成，请勿手改；改配置请回面板操作后重新保存\n")
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=go-tools %s/%s\n", spec.Service, spec.ID)
	b.WriteString("After=network.target\n\n")

	b.WriteString("[Service]\n")
	b.WriteString("Type=simple\n")
	fmt.Fprintf(&b, "ExecStart=%s\n", systemdCommand(spec.Bin, spec.Args))
	if spec.Dir != "" {
		fmt.Fprintf(&b, "WorkingDirectory=%s\n", spec.Dir)
	}
	if len(spec.Env) > 0 {
		keys := make([]string, 0, len(spec.Env))
		for k := range spec.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "Environment=%s\n", systemdQuote(k+"="+spec.Env[k]))
		}
	}
	fmt.Fprintf(&b, "StandardOutput=append:%s\n", spec.LogFile)
	fmt.Fprintf(&b, "StandardError=append:%s\n", spec.LogFile)
	b.WriteString("Restart=no\n\n")

	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")
	return b.String()
}

// systemdCommand 拼完整的 ExecStart 命令行。
func systemdCommand(bin string, args []string) string {
	parts := make([]string, 0, len(args)+1)
	parts = append(parts, systemdQuote(bin))
	for _, a := range args {
		parts = append(parts, systemdQuote(a))
	}
	return strings.Join(parts, " ")
}

// systemdQuote 按 systemd 规则加引号（含空白/引号/反斜杠/@ 时）。
func systemdQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'\\@;") {
		return s
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

// systemctl 调 systemctl 并返回输出；失败带命令与输出便于排障。
func systemctl(args ...string) (string, error) {
	cmd := exec.Command("systemctl", args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return text, fmt.Errorf("systemctl %s 失败: %v（%s）", strings.Join(args, " "), err, text)
	}
	return text, nil
}
