package downloader

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"Tools/appdirs"
)

// ============================================================
// Manager：下载队列的调度与生命周期中枢。
//
// 设计要点：
//   - 单例 Instance() 供绑定层用；New(opts, storeDir, saveDir) 供测试起独立实例。
//   - running 数受 MaxRunningTasks 限，超了置 queued 等额度；
//     全局块并发用容量 MaxTotalParts 的信号量 sem 兜住（downloadFile 内每块取还）。
//   - 每任务一个 goroutine 调 downloadFile（阻塞式，内部自己开 N 个 part worker），
//     取消用 context：pause→保留 .part/.gtdl.json，stop→删临时件。
//   - 进度经 emitter 发 EventProgress（负载 Task），顺带进 services/tasks 任务中心。
//   - 限速两层：全局桶 m.tb + 每任务桶 r.tb（parent=全局），写循环各过一道。
// ============================================================

// Manager 下载引擎单例。
type Manager struct {
	mu      sync.Mutex
	opts    Options
	tb      *tokenBucket  // 全局限速桶
	sem     chan struct{} // 全局块并发闸门
	tasks   map[string]*Task
	order   []string // 任务加入顺序（List/调度按此稳定排序）
	runners map[string]*runner
	limits  map[string]int64 // 每任务限速（bps），记忆重启用

	emitter func(name string, data ...any)
	store   *Store
	saveDir string

	wake    chan struct{} // 唤起调度器
	running int
	clk     func() time.Time // 便于测试注入时间；nil=time.Now

	once     sync.Once
	shutdown chan struct{}
}

// runner 一个在跑任务的控制器。
type runner struct {
	cancel   context.CancelFunc
	kind     string // 取消语义：pause | stop（cancel 前写好）
	tb       *tokenBucket
	prevDl   int64
	prevTime time.Time
}

// AddOpts 新建任务的可选参数（零值走默认）。
type AddOpts struct {
	Name        string // 空则从 URL 推
	SaveDir     string // 空则用 Manager 默认下载目录
	Category    string // 空则按扩展名归类
	Connections int    // 0 则用默认分块数
	Proxy       string
	Mirrors     []string
	ScheduledAt int64 // >0：到点自动开始（unix 毫秒）
	AutoStart   bool
}

// New 造一个引擎实例。storeDir 为空 → 不持久化任务清单（测试常用）；
// saveDir 为空 → 每笔任务新建时实时取配置的全局下载目录（appdirs.Downloads），
// 于是设置页改了下载目录无需重启即对新建任务生效；非空则锁定为该目录（测试常用）。
// 会拉起调度 goroutine；进程内长存，退出可调 Shutdown。
func New(opts Options, storeDir, saveDir string) *Manager {
	if opts.MaxTotalParts <= 0 {
		opts.MaxTotalParts = defaultMaxTotalParts
	}
	if opts.MaxRunningTasks <= 0 {
		opts.MaxRunningTasks = defaultMaxRunningTasks
	}
	if opts.DefaultConnections <= 0 {
		opts.DefaultConnections = defaultConnections
	}
	if opts.MinSegmentedSize <= 0 {
		opts.MinSegmentedSize = defaultMinSegmentedSize
	}
	m := &Manager{
		opts:     opts,
		tb:       newTokenBucket(nil, opts.GlobalSpeedLimitBps),
		sem:      make(chan struct{}, opts.MaxTotalParts),
		tasks:    map[string]*Task{},
		runners:  map[string]*runner{},
		limits:   map[string]int64{},
		wake:     make(chan struct{}, 1),
		shutdown: make(chan struct{}),
		clk:      time.Now,
		saveDir:  saveDir,
	}
	if storeDir != "" {
		m.store = NewStore(storeDir)
		if ts, err := m.store.Load(); err == nil {
			for _, t := range ts {
				m.tasks[t.ID] = t
				m.order = append(m.order, t.ID)
			}
		}
	}
	go m.dispatcher()
	return m
}

/* ============================ 单例 ============================ */

var (
	singletonOnce sync.Once
	singleton     *Manager
)

// Instance 返回进程级单例：默认参数 + 任务清单持久化到 <数据目录>/downloads（引擎自己的账本，
// 与用户下载落点分开），下载落点则随配置的全局下载目录动态解析（saveDir 传空）。
func Instance() *Manager {
	singletonOnce.Do(func() {
		dir := ""
		if d, err := engineStoreDir(); err == nil {
			dir = d
		}
		singleton = New(Options{}, dir, "")
	})
	return singleton
}

// SetEmitter 注入事件发射器（与 tools/* 同形：name, data...）。
func SetEmitter(fn func(name string, data ...any)) { Instance().SetEmitter(fn) }

// SetEmitter 实例级注入。
func (m *Manager) SetEmitter(fn func(name string, data ...any)) {
	m.mu.Lock()
	m.emitter = fn
	m.mu.Unlock()
}

// Shutdown 停调度循环（进程退出时调；进行中的任务由其自身 ctx 收尾）。
func (m *Manager) Shutdown() { m.once.Do(func() { close(m.shutdown) }) }

/* ============================ 生命周期 API ============================ */

// Add 新建一笔下载任务。
func (m *Manager) Add(url string, opts AddOpts) (*Task, error) {
	if url == "" {
		return nil, fmt.Errorf("下载地址不能为空")
	}
	if _, err := http.NewRequest(http.MethodGet, url, nil); err != nil {
		return nil, fmt.Errorf("非法下载地址：%v", err)
	}
	name := opts.Name
	if name == "" {
		name = nameFromURL(url)
	}
	name = sanitizeName(name)
	saveDir := opts.SaveDir
	if saveDir == "" {
		saveDir = m.defaultSaveDir()
	}
	if saveDir == "" {
		return nil, fmt.Errorf("未指定下载目录")
	}
	if err := os.MkdirAll(saveDir, 0o755); err != nil {
		return nil, err
	}
	conns := opts.Connections
	if conns <= 0 {
		conns = m.opts.DefaultConnections
	}
	cat := opts.Category
	if cat == "" {
		cat = categoryOf(name)
	}
	now := m.clk().UnixMilli()
	t := &Task{
		ID:          uniqueID(url),
		URL:         url,
		Name:        name,
		SavePath:    filepath.Join(saveDir, name),
		Category:    cat,
		Status:      StatusPending,
		Stage:       "info",
		Connections: conns,
		Proxy:       opts.Proxy,
		Mirrors:     opts.Mirrors,
		Source:      url,
		ScheduledAt: opts.ScheduledAt,
		AutoStart:   opts.AutoStart || opts.ScheduledAt > 0,
		StartedAt:   now,
		UpdatedAt:   now,
		Parts:       nil,
	}
	// 目标重名：加序号避让，别覆盖已有文件。
	t.SavePath = avoidCollision(t.SavePath)
	t.Name = filepath.Base(t.SavePath)

	m.mu.Lock()
	m.tasks[t.ID] = t
	m.order = append(m.order, t.ID)
	snap := cloneTask(t)
	m.persistLocked()
	m.mu.Unlock()
	m.signal()
	m.emitTask(snap)
	return &snap, nil
}

// Start 立即开始（未开始 / 暂停 / 出错 / 已取消都能拉起）。
func (m *Manager) Start(id string) error { return m.activate(id, false) }

// Resume 续传（等价 Start，语义上强调"从断点继续"）。
func (m *Manager) Resume(id string) error { return m.activate(id, false) }

// Retry 重试（错误 / 取消后重来；保留已下部分，靠续传 meta 少下些）。
func (m *Manager) Retry(id string) error { return m.activate(id, false) }

func (m *Manager) activate(id string, _ bool) error {
	m.mu.Lock()
	t := m.tasks[id]
	if t == nil {
		m.mu.Unlock()
		return fmt.Errorf("任务不存在: %s", id)
	}
	if t.Status == StatusRunning || t.Status == StatusQueued {
		// 已在跑/已排队：这次点击也不能吞——排队中的顶到队首，额度一腾出来先轮到自己。
		if t.Status == StatusQueued {
			m.order = frontID(m.order, id)
			snap := cloneTask(t)
			m.mu.Unlock()
			m.signal()
			m.emitTask(snap)
			return nil
		}
		m.mu.Unlock()
		return nil // 已在跑
	}
	t.Status = StatusQueued
	t.Stage = "downloading"
	t.Err = ""
	t.UpdatedAt = m.clk().UnixMilli()
	// 清掉将来某次误判为"到点才开"的时间：手动开始即无视计划
	t.ScheduledAt = 0
	snap := cloneTask(t)
	m.persistLocked()
	m.mu.Unlock()
	m.signal()
	m.emitTask(snap)
	return nil
}

// Pause 暂停：取消当前下载但保留 .part / .gtdl.json，之后可 Resume。
func (m *Manager) Pause(id string) error { return m.terminate(id, "pause") }

// Stop 停止：取消并清理临时文件，置 canceled。
func (m *Manager) Stop(id string) error { return m.terminate(id, "stop") }

func (m *Manager) terminate(id, kind string) error {
	m.mu.Lock()
	t := m.tasks[id]
	r := m.runners[id]
	if t == nil {
		m.mu.Unlock()
		return fmt.Errorf("任务不存在: %s", id)
	}
	if r == nil {
		// 没在跑：直接落终态，不留排队/暂停的歧义
		if kind == "stop" {
			t.Status = StatusCanceled
			t.Stage = "canceled"
		} else {
			t.Status = StatusPaused
			t.Stage = "stalled"
		}
		t.UpdatedAt = m.clk().UnixMilli()
		snap := cloneTask(t)
		m.persistLocked()
		m.mu.Unlock()
		m.signal()
		m.emitTask(snap)
		return nil
	}
	r.kind = kind
	if kind == "stop" {
		m.cleanupFiles(t.SavePath)
	}
	r.cancel()
	m.mu.Unlock()
	return nil
}

// Delete 移除任务并删除其文件与续传 meta。进行中的先停止。
func (m *Manager) Delete(id string) error {
	m.mu.Lock()
	t := m.tasks[id]
	if t == nil {
		m.mu.Unlock()
		return fmt.Errorf("任务不存在: %s", id)
	}
	if r := m.runners[id]; r != nil {
		r.kind = "stop"
		r.cancel()
	}
	m.cleanupFiles(t.SavePath)
	delete(m.tasks, id)
	delete(m.limits, id)
	m.order = removeID(m.order, id)
	m.persistLocked()
	m.mu.Unlock()
	m.signal()
	return nil
}

// SetConnections 改单任务分块数（运行中的下次生效）。
func (m *Manager) SetConnections(id string, n int) error {
	if n < 1 {
		n = 1
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	if t == nil {
		return fmt.Errorf("任务不存在: %s", id)
	}
	t.Connections = n
	t.UpdatedAt = m.clk().UnixMilli()
	m.persistLocked()
	return nil
}

// SetSpeedLimit 设单任务限速（bps，0=仅受全局约束）。
func (m *Manager) SetSpeedLimit(id string, bps int64) error {
	if bps < 0 {
		bps = 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tasks[id] == nil {
		return fmt.Errorf("任务不存在: %s", id)
	}
	m.limits[id] = bps
	if r := m.runners[id]; r != nil {
		r.tb.setRate(bps)
	}
	return nil
}

// SetProxy 设单任务代理（下次下载生效）。
func (m *Manager) SetProxy(id, proxy string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	if t == nil {
		return fmt.Errorf("任务不存在: %s", id)
	}
	t.Proxy = proxy
	t.UpdatedAt = m.clk().UnixMilli()
	m.persistLocked()
	return nil
}

// SelectSource 手动选定实际使用的镜像源（PR4 之前的唯一选源方式）。
func (m *Manager) SelectSource(id, url string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	if t == nil {
		return fmt.Errorf("任务不存在: %s", id)
	}
	if url != "" {
		t.Source = url
	}
	t.UpdatedAt = m.clk().UnixMilli()
	m.persistLocked()
	return nil
}

// SetGlobalLimit 改全局限速（bps，0=解除）。即时作用于所有在跑任务。
func (m *Manager) SetGlobalLimit(bps int64) {
	if bps < 0 {
		bps = 0
	}
	m.mu.Lock()
	m.opts.GlobalSpeedLimitBps = bps
	m.mu.Unlock()
	m.tb.setRate(bps)
}

/* ============================ 查询 ============================ */

// List 全部任务快照（按加入顺序）。
func (m *Manager) List() []Task {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Task, 0, len(m.order))
	for _, id := range m.order {
		if t := m.tasks[id]; t != nil {
			out = append(out, cloneTask(t))
		}
	}
	return out
}

// Snapshot 等价 List（与其它工具的命名对齐，便于绑定层调用）。
func (m *Manager) Snapshot() []Task { return m.List() }

// Stats 全局聚合：合计下行速度、各相位计数、全局限速。
type Stats struct {
	Speed       float64 `json:"speed"`
	Running     int     `json:"running"`
	Queued      int     `json:"queued"`
	Done        int     `json:"done"`
	Total       int     `json:"total"`
	GlobalLimit int64   `json:"globalLimit"`
}

// GlobalStats 供顶栏统计卡。
func (m *Manager) GlobalStats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Stats{GlobalLimit: m.opts.GlobalSpeedLimitBps, Total: len(m.order)}
	for _, id := range m.order {
		t := m.tasks[id]
		if t == nil {
			continue
		}
		switch t.Status {
		case StatusRunning:
			s.Running++
			s.Speed += t.Speed
		case StatusQueued, StatusPending:
			s.Queued++
		case StatusDone:
			s.Done++
		}
	}
	return s
}

/* ============================ 调度 ============================ */

func (m *Manager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) dispatcher() {
	tick := time.NewTicker(time.Second) // 轮询到点的计划任务
	defer tick.Stop()
	for {
		select {
		case <-m.shutdown:
			return
		case <-m.wake:
			m.schedule()
		case <-tick.C:
			m.schedule()
		}
	}
}

// schedule 把到期 / 被激活的排队任务塞进空余 running 额度。
func (m *Manager) schedule() {
	now := m.clk().UnixMilli()
	m.mu.Lock()
	var changed bool
	// 1) 到点的计划任务：pending（带未来 ScheduledAt）现在到期 → queued
	for _, id := range m.order {
		t := m.tasks[id]
		if t == nil || t.Status != StatusPending || !t.AutoStart {
			continue
		}
		if t.ScheduledAt > 0 && t.ScheduledAt > now {
			continue // 还没到点
		}
		t.Status = StatusQueued
		t.Stage = "downloading"
		changed = true
	}
	// 2) 填额度：按加入顺序取 queued
	for _, id := range m.order {
		if m.running >= m.opts.MaxRunningTasks {
			break
		}
		t := m.tasks[id]
		if t == nil || t.Status != StatusQueued {
			continue
		}
		m.launchLocked(t)
		changed = true
	}
	if changed {
		m.persistLocked()
	}
	m.mu.Unlock()
}

// launchLocked 起一个任务 goroutine（调用方持锁）。
func (m *Manager) launchLocked(t *Task) {
	ctx, cancel := context.WithCancel(context.Background())
	r := &runner{
		cancel:   cancel,
		kind:     "pause",
		tb:       newTokenBucket(ctx, m.limits[t.ID]),
		prevTime: m.clk(),
	}
	r.tb.parent = m.tb // 全局层
	m.runners[t.ID] = r
	t.Status = StatusRunning
	t.Stage = "downloading"
	t.Err = ""
	t.StartedAt = m.clk().UnixMilli()
	t.UpdatedAt = t.StartedAt
	m.running++
	go m.runTask(cloneTask(t), r, ctx)
}

// runTask 单任务编排：造 client → 调 downloadFile（内部并发分块）→ 收尾。
// t 是入队时的快照（值拷贝）：字段只读，实时进度经 id 回查 m.tasks，不碰这份。
func (m *Manager) runTask(t Task, r *runner, ctx context.Context) {
	client := newClient(t.Proxy, defaultTimeoutSec*time.Second)
	conns := t.Connections
	if conns <= 0 {
		conns = m.opts.DefaultConnections
	}
	src := t.Source
	if src == "" {
		src = t.URL
	}
	p := dlParams{
		ctx:      ctx,
		client:   client,
		url:      src,
		savePath: t.SavePath,
		conns:    conns,
		minSeg:   m.opts.MinSegmentedSize,
		maxRetry: defaultMaxRetry,
		tb:       r.tb,
		gate:     m.gate,
		onProgress: func(size, dl int64, parts []Part) {
			m.updateProgress(t.ID, r, size, dl, parts)
		},
	}
	err := downloadFile(p)
	m.finish(t.ID, r, err)
}

// gate 取一个全局块并发额度；返回的 release 归还它。
func (m *Manager) gate() func() {
	m.sem <- struct{}{}
	return func() { <-m.sem }
}

// updateProgress 把 downloadFile 回调的进度并进 Task 并广播。
func (m *Manager) updateProgress(id string, r *runner, size, dl int64, parts []Part) {
	m.mu.Lock()
	t := m.tasks[id]
	if t == nil || t.Status != StatusRunning {
		m.mu.Unlock()
		return
	}
	now := m.clk()
	if size > 0 {
		t.Size = size
	}
	t.Downloaded = dl
	if t.Size > 0 {
		pct := int(dl * 100 / t.Size)
		if pct > 100 {
			pct = 100
		}
		t.Percent = pct
	}
	t.Parts = parts
	t.UpdatedAt = now.UnixMilli()
	if dt := now.Sub(r.prevTime).Seconds(); dt >= 0.4 {
		if d := dl - r.prevDl; d >= 0 {
			t.Speed = float64(d) / dt
		}
		r.prevDl, r.prevTime = dl, now
	}
	snap := cloneTask(t)
	m.mu.Unlock()
	m.emitTask(snap)
}

// finish 收尾：落终态、清 runner、让额度、持久化、广播。
func (m *Manager) finish(id string, r *runner, err error) {
	m.mu.Lock()
	t := m.tasks[id]
	delete(m.runners, id)
	if m.running > 0 {
		m.running--
	}
	if t == nil {
		m.mu.Unlock()
		m.signal()
		return
	}
	now := m.clk().UnixMilli()
	t.UpdatedAt = now
	canceled := errors.Is(err, context.Canceled)
	switch {
	case err == nil:
		t.Status = StatusDone
		t.Stage = "done"
		t.Percent = 100
		t.Speed = 0
		t.EndedAt = now
		if fi, e := os.Stat(t.SavePath); e == nil {
			t.Size = fi.Size()
			t.Downloaded = fi.Size()
		}
	case r.kind == "stop":
		t.Status = StatusCanceled
		t.Stage = "canceled"
		t.Speed = 0
		t.EndedAt = now
		m.cleanupFiles(t.SavePath) // stop 语义：临时件一并清掉
	case r.kind == "pause" && canceled:
		t.Status = StatusPaused
		t.Stage = "stalled" // 暂停：任务中心仍视为在跑，不弹通知
		t.Speed = 0
	default: // 真错误（含默认识 kind=pause 但并非取消的情况）
		t.Status = StatusError
		t.Stage = "error"
		t.Speed = 0
		t.EndedAt = now
		t.Err = errText(err)
	}
	snap := cloneTask(t)
	m.persistLocked()
	m.mu.Unlock()
	m.signal() // 腾出的额度给排队任务
	m.emitTask(snap)
}

/* ============================ 内部小工具 ============================ */

// emitTask 广播一条进度（快照已在锁外拷贝，避免回调里持锁）。
func (m *Manager) emitTask(t Task) {
	m.mu.Lock()
	em := m.emitter
	m.mu.Unlock()
	if em != nil {
		em(EventProgress, t)
	}
}

// persistLocked 全量落清单（调用方持锁）；量小，全量重写最省心。
func (m *Manager) persistLocked() {
	if m.store == nil {
		return
	}
	list := make([]*Task, 0, len(m.order))
	for _, id := range m.order {
		if t := m.tasks[id]; t != nil {
			cp := *t
			list = append(list, &cp)
		}
	}
	_ = m.store.Save(list)
}

func (m *Manager) defaultSaveDir() string {
	m.mu.Lock()
	d := m.saveDir
	m.mu.Unlock()
	if d != "" {
		return d
	}
	// 未锁定（单例）：实时取配置的全局下载目录，设置页改后立即对新建任务生效
	if dir, err := appdirs.Downloads(); err == nil {
		return dir
	}
	return ""
}

// DefaultSaveDir 当前生效的全局默认下载目录（供前端展示「留空则存到…」）。
func DefaultSaveDir() string {
	if dir, err := appdirs.Downloads(); err == nil {
		return dir
	}
	return ""
}

// cleanupFiles 删除目标文件、.part 与续传 meta。
func (m *Manager) cleanupFiles(savePath string) {
	_ = os.Remove(savePath)
	_ = os.Remove(savePath + partSuffix)
	_ = os.Remove(savePath + metaSuffix)
}

// avoidCollision 目标存在时插入序号避让（name(1).ext）。
func avoidCollision(p string) string {
	if _, err := os.Stat(p); err != nil {
		return p
	}
	dir := filepath.Dir(p)
	base := filepath.Base(p)
	ext := filepath.Ext(base)
	stem := base[:len(base)-len(ext)]
	for i := 1; i < 10000; i++ {
		cand := filepath.Join(dir, fmt.Sprintf("%s(%d)%s", stem, i, ext))
		if _, err := os.Stat(cand); err != nil {
			return cand
		}
	}
	return p
}

func removeID(ids []string, id string) []string {
	out := ids[:0]
	for _, x := range ids {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

// frontID 把 id 顶到队列最前（不在队里则插入队首）；供「排队中点开始＝插队」用。
func frontID(ids []string, id string) []string {
	rest := removeID(ids, id)
	return append([]string{id}, rest...)
}

func cloneTask(t *Task) Task {
	cp := *t
	if t.Parts != nil {
		cp.Parts = append([]Part(nil), t.Parts...)
	}
	if t.Mirrors != nil {
		cp.Mirrors = append([]string(nil), t.Mirrors...)
	}
	return cp
}

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// engineStoreDir 引擎任务清单（tasks.json）的持久化目录：<数据目录>/downloads。
// 与用户下载落点（appdirs.Downloads，可在设置页改）分开，换下载目录不丢任务历史。
func engineStoreDir() (string, error) {
	return appdirs.Data("downloads")
}
