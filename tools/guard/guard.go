package guard

// 进程守护引擎：登记本地程序（名称/路径/参数/工作目录），负责拉起、监控、
// 意外退出后按「退避 + 连续快速失败上限」自动重启；go-tools 退出时一并结束子进程。
//
// 只管理「本工具启动的子进程」（能拿到退出事件才能重启），不接管外部已运行进程。
// 列表与守护参数持久化在 <数据目录>/guard/guard.json（仿 ftp 服务端存储）。
// 运行态经 guard:status 事件推给前端；平台差异（隐藏控制台 / 进程树终止）在
// guard_kill_windows.go 与 guard_kill_other.go 里按构建标签处理。

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-tools/appdirs"
	"go-tools/services/applog"
	"go-tools/services/config"
)

// minUptimeSec 进程稳定运行阈值：跑够此时长再退出视为「正常结束」，清零连续快速失败计数。
const minUptimeSec = 5

// Entry 一条守护登记项（进绑定模型）。Args 是一行命令行参数，启动时按引号感知切分。
type Entry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Args    string `json:"args"`
	WorkDir string `json:"workDir"`
	Enabled bool   `json:"enabled"` // 是否启用守护：随应用启动自动拉起 + 意外退出自动重启
}

// Row 表格展示行：登记项 + 实时运行态。State: running | stopped | paused | disabled。
type Row struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Args      string `json:"args"`
	WorkDir   string `json:"workDir"`
	Enabled   bool   `json:"enabled"`
	Running   bool   `json:"running"`
	Pid       int    `json:"pid"`
	Paused    bool   `json:"paused"`
	Restarts  int    `json:"restarts"`
	LastError string `json:"lastError"`
	State     string `json:"state"`
}

// Snapshot 一次全量状态（guard:status 事件负载 / GuardGetState 返回）。
type Snapshot struct {
	Rows            []Row `json:"rows"`
	AutoStart       bool  `json:"autoStart"`
	RestartDelaySec int   `json:"restartDelaySec"`
	MaxFastFail     int   `json:"maxFastFail"`
}

// managed 单个登记项的运行态。st 保护其字段；mu（包级）保护 entries/procs。
type managed struct {
	st          sync.Mutex
	cmd         *exec.Cmd
	running     bool
	pid         int
	intentional bool // 主动停止/禁用：Wait 返回后不重启
	fastFail    int
	restarts    int
	paused      bool
	lastErr     string
	stop        chan struct{} // 关闭通知 runLoop 彻底退出（等待重启期间也能立即退出）
	stopClosed  bool
	loopActive  bool
}

var (
	mu       sync.Mutex
	entries  []Entry
	procs    = map[string]*managed{}
	emitter  func(name string, data ...any)
	settings = guardSettings{RestartDelaySec: 3, MaxFastFail: 5}
	loaded   bool
)

// guardSettings 守护重启策略参数（持久化）。
type guardSettings struct {
	RestartDelaySec int `json:"restartDelaySec"`
	MaxFastFail     int `json:"maxFastFail"`
}

// SetEmitter 注入事件发射器（main.go 里与其它工具共用同一个 emit）。
func SetEmitter(fn func(name string, data ...any)) { emitter = fn }

// ensureLoaded 惰性载入持久化列表与参数（StartAutoGuard 会先行触发，页面直接查询也安全）。
func ensureLoaded() {
	mu.Lock()
	defer mu.Unlock()
	if !loaded {
		loadLocked()
		loaded = true
	}
}

// ---------- 查询 ----------

// GetState 汇总登记项 + 运行态 + 开机自启 + 守护参数，供前端渲染表格。
func GetState() Snapshot {
	ensureLoaded()
	mu.Lock()
	rows := make([]Row, 0, len(entries))
	for _, e := range entries {
		row := Row{
			ID: e.ID, Name: e.Name, Path: e.Path, Args: e.Args,
			WorkDir: e.WorkDir, Enabled: e.Enabled,
		}
		if m := procs[e.ID]; m != nil {
			m.st.Lock()
			row.Running = m.running
			row.Pid = m.pid
			row.Paused = m.paused
			row.Restarts = m.restarts
			row.LastError = m.lastErr
			m.st.Unlock()
		}
		switch {
		case row.Running:
			row.State = "running"
		case row.Paused:
			row.State = "paused"
		case !row.Enabled:
			row.State = "disabled"
		default:
			row.State = "stopped"
		}
		rows = append(rows, row)
	}
	snap := Snapshot{
		Rows:            rows,
		AutoStart:       config.CurrentConfig().General.AutoStart,
		RestartDelaySec: settings.RestartDelaySec,
		MaxFastFail:     settings.MaxFastFail,
	}
	mu.Unlock()
	return snap
}

func emitStatus() {
	if emitter != nil {
		emitter("guard:status", GetState())
	}
}

// PushStatus 主动推送一次当前状态：供外部改了引擎不感知的字段（如 AutoStart）后刷新前端。
func PushStatus() { emitStatus() }

// ---------- 增删改 ----------

// Upsert 新增或更新一条登记项（ID 为空则生成）；改了可执行文件相关字段且正在跑则重启。
func Upsert(e Entry) error {
	ensureLoaded()
	e.Name = strings.TrimSpace(e.Name)
	e.Path = strings.TrimSpace(e.Path)
	if e.Path == "" {
		return fmt.Errorf("程序路径不能为空")
	}
	if e.Name == "" {
		e.Name = filepath.Base(e.Path)
	}

	mu.Lock()
	if strings.TrimSpace(e.ID) == "" {
		e.ID = "g" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	idx := -1
	for i := range entries {
		if entries[i].ID == e.ID {
			idx = i
			break
		}
	}
	restart := false
	if idx >= 0 {
		old := entries[idx]
		restart = (old.Path != e.Path || old.Args != e.Args || old.WorkDir != e.WorkDir) &&
			procs[e.ID] != nil && procs[e.ID].isRunning()
		entries[idx] = e
	} else {
		entries = append(entries, e)
	}
	saveLocked()
	mu.Unlock()

	if restart {
		_ = StopOne(e.ID)
		_ = StartOne(e.ID)
	}
	emitStatus()
	return nil
}

// Remove 删除一条登记项（先停止其进程）。
func Remove(id string) error {
	_ = StopOne(id)
	mu.Lock()
	out := entries[:0]
	for _, e := range entries {
		if e.ID != id {
			out = append(out, e)
		}
	}
	entries = out
	delete(procs, id)
	saveLocked()
	mu.Unlock()
	emitStatus()
	return nil
}

// Clear 清空全部登记项（先停止全部进程）。
func Clear() error {
	StopAll()
	mu.Lock()
	entries = nil
	saveLocked()
	mu.Unlock()
	emitStatus()
	return nil
}

// ---------- 控制 ----------

// SetEnabled 切换某项的「启用守护」：开启且未跑则拉起；关闭则主动停止。
func SetEnabled(id string, enabled bool) error {
	ensureLoaded()
	mu.Lock()
	found := false
	for i := range entries {
		if entries[i].ID == id {
			entries[i].Enabled = enabled
			found = true
			break
		}
	}
	saveLocked()
	mu.Unlock()
	if !found {
		return fmt.Errorf("未找到该程序")
	}
	if enabled {
		_ = StartOne(id)
	} else {
		_ = StopOne(id)
	}
	emitStatus()
	return nil
}

// StartOne 启动（或重新纳入守护）一个登记项：清暂停/主动停止标记，未在守护则起 runLoop。
func StartOne(id string) error {
	ensureLoaded()
	mu.Lock()
	var e *Entry
	for i := range entries {
		if entries[i].ID == id {
			cp := entries[i]
			e = &cp
			break
		}
	}
	m := procs[id]
	if m == nil {
		m = &managed{}
		procs[id] = m
	}
	mu.Unlock()

	if e == nil {
		return fmt.Errorf("未找到该程序")
	}

	m.st.Lock()
	if m.loopActive {
		m.st.Unlock()
		return nil // 已在守护循环中
	}
	m.intentional = false
	m.paused = false
	m.fastFail = 0
	m.lastErr = ""
	m.stop = make(chan struct{})
	m.stopClosed = false
	m.loopActive = true
	m.st.Unlock()

	go runLoop(id)
	emitStatus()
	return nil
}

// StopOne 主动停止一个登记项：置 intentional（Wait 后不重启）、通知循环退出并结束进程树。
func StopOne(id string) error {
	mu.Lock()
	m := procs[id]
	mu.Unlock()
	if m == nil {
		return nil
	}
	m.st.Lock()
	m.intentional = true
	m.paused = false
	var pid int
	if m.running {
		pid = m.pid
	}
	if !m.stopClosed && m.stop != nil {
		close(m.stop)
		m.stopClosed = true
	}
	m.st.Unlock()

	if pid > 0 {
		_ = killTree(pid)
	}
	return nil
}

// StartAll 启用所有程序：对每个 Enabled 项拉起。
func StartAll() error {
	ensureLoaded()
	mu.Lock()
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Enabled {
			ids = append(ids, e.ID)
		}
	}
	mu.Unlock()
	for _, id := range ids {
		_ = StartOne(id)
	}
	return nil
}

// StopAll 关闭所有程序：主动停止全部在跑/在守护的项。
func StopAll() {
	mu.Lock()
	ids := make([]string, 0, len(procs))
	for id := range procs {
		ids = append(ids, id)
	}
	mu.Unlock()
	for _, id := range ids {
		_ = StopOne(id)
	}
}

// Shutdown 应用退出兜底：结束所有由本工具拉起的子进程（一并结束）。
func Shutdown() {
	StopAll()
}

// StartAutoGuard 应用启动时按配置自动拉起所有「启用守护」项（失败只记日志，不阻断启动）。
func StartAutoGuard() {
	ensureLoaded()
	mu.Lock()
	n := 0
	for _, e := range entries {
		if e.Enabled {
			n++
		}
	}
	mu.Unlock()
	if n == 0 {
		return
	}
	applog.Infof("guard", "启动自动拉起守护程序：%d 项", n)
	_ = StartAll()
}

// SetSettings 更新重启策略参数（delaySec 重启间隔、maxFastFail 连续快速失败上限）。
func SetSettings(delaySec, maxFastFail int) error {
	ensureLoaded()
	if delaySec < 0 {
		delaySec = 0
	}
	if delaySec > 3600 {
		delaySec = 3600
	}
	if maxFastFail < 1 {
		maxFastFail = 1
	}
	if maxFastFail > 1000 {
		maxFastFail = 1000
	}
	mu.Lock()
	settings.RestartDelaySec = delaySec
	settings.MaxFastFail = maxFastFail
	saveLocked()
	mu.Unlock()
	emitStatus()
	return nil
}

// ---------- 守护循环 ----------

// runLoop 常驻守护某个登记项：拉起进程 → 等待退出 → 判定是否重启，直到主动停止或项被删除/禁用。
func runLoop(id string) {
	defer func() {
		mu.Lock()
		m := procs[id]
		mu.Unlock()
		if m != nil {
			m.st.Lock()
			m.loopActive = false
			m.running = false
			m.pid = 0
			m.cmd = nil
			m.st.Unlock()
		}
		emitStatus()
	}()

	for {
		select {
		case <-stopCh(id):
			return
		default:
		}

		e, ok := entryByID(id)
		if !ok {
			return // 项已被删除
		}

		cmd, logF, err := startProcess(e)
		if err != nil {
			m := procOf(id)
			m.st.Lock()
			m.lastErr = err.Error()
			m.fastFail++
			ff := m.fastFail
			m.st.Unlock()
			applog.Warnf("guard", "启动失败：%s（%v）", e.Name, err)
			emitStatus()
			if ff >= maxFastFail() {
				m.st.Lock()
				m.paused = true
				m.st.Unlock()
				return
			}
			if !sleepDelayOrStop(id) {
				return
			}
			continue
		}

		startAt := time.Now()
		m := procOf(id)
		m.st.Lock()
		m.cmd = cmd
		m.running = true
		m.pid = cmd.Process.Pid
		m.lastErr = ""
		m.st.Unlock()
		applog.Infof("guard", "已拉起：%s pid=%d", e.Name, cmd.Process.Pid)
		emitStatus()

		waitErr := cmd.Wait()
		if logF != nil {
			_ = logF.Close()
		}
		uptime := time.Since(startAt)

		m.st.Lock()
		m.running = false
		m.pid = 0
		m.cmd = nil
		intentional := m.intentional
		m.st.Unlock()

		select {
		case <-stopCh(id):
			return
		default:
		}
		if intentional {
			return
		}

		e2, ok := entryByID(id)
		if !ok || !e2.Enabled {
			return // 已删除或已关闭守护
		}

		if uptime >= time.Duration(minUptimeSec)*time.Second {
			m.st.Lock()
			m.fastFail = 0
			m.st.Unlock()
		}
		m.st.Lock()
		m.fastFail++
		ff := m.fastFail
		m.restarts++
		if waitErr != nil {
			m.lastErr = waitErr.Error()
		} else {
			m.lastErr = "进程已退出"
		}
		m.st.Unlock()
		applog.Warnf("guard", "进程退出：%s（uptime=%s err=%v）连续快速失败=%d", e2.Name, uptime.Round(time.Second), waitErr, ff)
		emitStatus()

		if ff >= maxFastFail() {
			m.st.Lock()
			m.paused = true
			m.st.Unlock()
			applog.Warnf("guard", "守护暂停：%s 连续快速失败达上限 %d 次", e2.Name, ff)
			return
		}
		if !sleepDelayOrStop(id) {
			return
		}
	}
}

// sleepDelayOrStop 等待重启间隔；期间收到停止信号返回 false。
func sleepDelayOrStop(id string) bool {
	m := procOf(id)
	m.st.Lock()
	stop := m.stop
	m.st.Unlock()
	d := time.Duration(restartDelaySec()) * time.Second
	t := time.NewTimer(d)
	defer t.Stop()
	if stop == nil {
		<-time.After(d)
		return true
	}
	select {
	case <-stop:
		return false
	case <-t.C:
		return true
	}
}

// ---------- 进程与工具 ----------

// startProcess 组装并启动子进程，返回 cmd 与其日志文件（stdout/stderr 追加到 <数据目录>/guard/<id>.log）。
func startProcess(e Entry) (*exec.Cmd, *os.File, error) {
	if strings.TrimSpace(e.Path) == "" {
		return nil, nil, fmt.Errorf("程序路径为空")
	}
	if st, err := os.Stat(e.Path); err != nil || st.IsDir() {
		return nil, nil, fmt.Errorf("程序不存在或不是文件：%s", e.Path)
	}
	cmd := exec.Command(e.Path, splitArgs(e.Args)...)
	dir := strings.TrimSpace(e.WorkDir)
	if dir == "" {
		dir = filepath.Dir(e.Path)
	}
	cmd.Dir = dir
	applySysAttr(cmd)

	var logF *os.File
	if p, err := logPath(e.ID); err == nil {
		if f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			logF = f
			cmd.Stdout = f
			cmd.Stderr = f
		}
	}
	if err := cmd.Start(); err != nil {
		if logF != nil {
			_ = logF.Close()
		}
		return nil, nil, err
	}
	return cmd, logF, nil
}

func logPath(id string) (string, error) {
	base, err := appdirs.Data("guard")
	if err != nil {
		return "", err
	}
	return filepath.Join(base, id+".log"), nil
}

// splitArgs 引号感知切分一行参数为 argv（双/单引号内的空格不切分）。
func splitArgs(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var out []string
	var cur strings.Builder
	inQuote := false
	var quote byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\'':
			if inQuote && c == quote {
				inQuote = false
				quote = 0
			} else if !inQuote {
				inQuote = true
				quote = c
			} else {
				cur.WriteByte(c)
			}
		case (c == ' ' || c == '\t') && !inQuote:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// ---------- 内部：在包锁保护下访问 entries/procs ----------

func (m *managed) isRunning() bool {
	m.st.Lock()
	defer m.st.Unlock()
	return m.running
}

func procOf(id string) *managed {
	mu.Lock()
	defer mu.Unlock()
	m := procs[id]
	if m == nil {
		m = &managed{}
		procs[id] = m
	}
	return m
}

func stopCh(id string) chan struct{} {
	m := procOf(id)
	m.st.Lock()
	defer m.st.Unlock()
	return m.stop
}

func entryByID(id string) (Entry, bool) {
	mu.Lock()
	defer mu.Unlock()
	for _, e := range entries {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

func restartDelaySec() int {
	mu.Lock()
	defer mu.Unlock()
	if settings.RestartDelaySec <= 0 {
		return 3
	}
	return settings.RestartDelaySec
}

func maxFastFail() int {
	mu.Lock()
	defer mu.Unlock()
	if settings.MaxFastFail <= 0 {
		return 5
	}
	return settings.MaxFastFail
}
