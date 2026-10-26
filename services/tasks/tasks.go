package tasks

// ============================================================
// 安装/下载类长任务的统一登记处
//
// 为什么需要它：进度事件是"瞬时的"——Wails 事件只发给当时还在的页面。
// 用户在装 MySQL 时点去设置页、关掉工具窗口，页面里那份 installProgress
// 就没了；任务本身仍在后端 goroutine 里跑，但没有任何一方记得它跑到哪。
//
// 所以这里把事件里的事实落成一份带终态的清单：
//   - 页面随时可以拉快照（不关心谁发起的，只关心"还在装什么"）；
//   - 增量通过 "task:update" 统一事件推送，前端只认一种形状；
//   - done / error 时由注入的 notifier 弹系统通知（桌面版走通知卡，
//     面板模式交给浏览器 Notification）。
//
// 这里只做"记账"，不控制任务生命周期：取消与重试仍由各工具包负责。
// ============================================================

import (
	"encoding/json"
	"sort"
	"sync"
	"time"
)

/* ============================ 类型 ============================ */

// 任务状态：running 之外的三个都是终态
const (
	StatusRunning  = "running"
	StatusDone     = "done"
	StatusError    = "error"
	StatusCanceled = "canceled"
)

// UpdateEvent 增量事件名：负载就是 Task 本身
const UpdateEvent = "task:update"

// Task 一笔长任务的当前快照
//
// 字段刻意保持扁平：它要被 Wails 生成成前端模型，嵌套越少越好用。
type Task struct {
	ID        string   `json:"id"`        // 稳定主键：<tool>|<kind>|<name>
	Tool      string   `json:"tool"`      // docker / mysql / php / redis / nginx / npm
	Kind      string   `json:"kind"`      // install / ext / setup / pull / op
	Name      string   `json:"name"`      // 版本号 / 镜像 / 扩展名
	Title     string   `json:"title"`     // 展示用："MySQL 8.4.6"、"Docker 引擎"
	Stage     string   `json:"stage"`     // info / downloading / extracting / …
	Status    string   `json:"status"`    // running / done / error / canceled
	Percent   int      `json:"percent"`   // 0~100；不确定进度的阶段保持原值
	Note      string   `json:"note"`      // 最近一条说明
	StartedAt int64    `json:"startedAt"` // unix 毫秒
	UpdatedAt int64    `json:"updatedAt"`
	EndedAt   int64    `json:"endedAt"` // 0 表示还在跑
	Logs      []string `json:"logs"`    // 最近的进度说明（有上限）
}

// Finished 任务是否已结束
func (t Task) Finished() bool { return t.Status != StatusRunning }

/* ============================ 事件映射 ============================ */

// eventMeta 一次事件属于哪个工具、哪类任务
type eventMeta struct {
	tool string
	kind string
}

// events 需要登记的事件名
//
// 只收"用户会离开页面还想看进度"的长任务。终端输出、监控命令这类高频
// 流不在此列——它们没有终态，登记进来只会把任务列表刷满。
var events = map[string]eventMeta{
	"mysql:install":   {"mysql", "install"},
	"php:install":     {"php", "install"},
	"php:ext-install": {"php", "ext"},
	"redis:install":   {"redis", "install"},
	"nginx:install":   {"nginx", "install"},
	"npm:install":     {"npm", "install"},
	"npm:npmver":      {"npm", "npmver"},
	"docker:setup":    {"docker", "setup"},
	"docker:pull":     {"docker", "pull"},
	"docker:imageop":  {"docker", "op"},
}

// toolNames 工具展示名
var toolNames = map[string]string{
	"docker": "Docker",
	"mysql":  "MySQL",
	"php":    "PHP",
	"redis":  "Redis",
	"nginx":  "Nginx",
	"npm":    "Node/npm",
}

// opVerbs 镜像操作动词（docker:imageop 的 op 字段）
var opVerbs = map[string]string{
	"push":   "推送",
	"build":  "构建",
	"import": "导入",
	"export": "导出",
	"prune":  "清理",
}

// payload 各工具进度负载的并集
//
// 不直接依赖 tools/* 的类型：那样会让 services 层反向依赖工具包，而五个
// 包的 InstallProgress 本来就同形。json 往返一次即可拿到需要的字段。
type payload struct {
	Version string `json:"version"`
	Image   string `json:"image"`
	Op      string `json:"op"`
	Stage   string `json:"stage"`
	Percent int    `json:"percent"`
	Overall int    `json:"overall"` // docker 拉取只有总体百分比
	Note    string `json:"note"`
}

/* ============================ 存储 ============================ */

const (
	// keepLogs 每个任务保留的进度说明行数；页面日志框最多也就这么多
	keepLogs = 80
	// maxTasks 清单上限：超出按"最先结束的"淘汰
	maxTasks = 80
	// finishedTTL 已结束任务的保留时长（跨页回看足够，又不至于越攒越多）
	finishedTTL = 2 * time.Hour
)

var (
	mu       sync.RWMutex
	items    = map[string]*Task{}
	emitter  func(name string, data ...any)
	notifier func(Task)
)

// SetEmitter 注入增量事件发射器（绑 Wails v3 的 app.Event.Emit）。
//
// 传进来的必须是"裸"的 Emit，而不是外面包了 Observe 的闭包，
// 否则 task:update 会被再次 Observe 造成回环。
func SetEmitter(fn func(name string, data ...any)) {
	mu.Lock()
	emitter = fn
	mu.Unlock()
}

// SetNotifier 注入终态通知（done / error 各一次）。
//
// 回调放在独立 goroutine 里跑：通知卡片要建原生窗口，别把安装流程卡住。
func SetNotifier(fn func(Task)) {
	mu.Lock()
	notifier = fn
	mu.Unlock()
}

/* ============================ 写入 ============================ */

// Observe 处理一条工具进度事件：登记/更新对应任务，必要时推送增量与通知。
//
// 参数与工具的 emitter 同形（name, data...）：这样在 main 里可以无脑代理，
// 不需要每个包各写一份拆包代码。事件名不在 events 表里时直接忽略。
func Observe(event string, data ...any) {
	meta, ok := events[event]
	if !ok || len(data) == 0 || data[0] == nil {
		return
	}
	buf, err := json.Marshal(data[0])
	if err != nil {
		return
	}
	var p payload
	if err := json.Unmarshal(buf, &p); err != nil || p.Stage == "" {
		return
	}

	t, changed, terminal := apply(meta, p)
	if !changed {
		return
	}

	mu.RLock()
	em := emitter
	nf := notifier
	mu.RUnlock()
	if em != nil {
		em(UpdateEvent, t)
	}
	if terminal && nf != nil {
		go nf(t)
	}
}

// apply 把一条负载并进清单，返回更新后的快照、是否真的变了、是否刚进入终态。
func apply(meta eventMeta, p payload) (Task, bool, bool) {
	id, name, title := identify(meta, p)
	now := time.Now().UnixMilli()

	mu.Lock()
	defer mu.Unlock()

	t := items[id]
	if t == nil {
		t = &Task{ID: id, Tool: meta.tool, Kind: meta.kind, Name: name, Title: title, StartedAt: now}
		items[id] = t
	}
	before := t.Status
	beforeStage := t.Stage
	t.UpdatedAt = now
	t.Title = title
	t.Name = name

	terminalStage := p.Stage == "done" || p.Stage == "error" || p.Stage == "canceled"
	// 上一次已结束、这次又从 info/downloading 开始：视为重试，进度与日志重来
	if t.Finished() && !terminalStage {
		t.StartedAt = now
		t.EndedAt = 0
		t.Percent = 0
		t.Logs = nil
	}

	stageChanged := beforeStage != p.Stage
	if p.Stage != "stalled" { // stalled 只是"当前阶段卡久了"的提示，不改阶段
		t.Stage = p.Stage
	}

	pct := p.Percent
	if pct == 0 && p.Overall > 0 {
		pct = p.Overall
	}
	if !terminalStage && pct > t.Percent {
		t.Percent = pct // 只涨不跌：daemon 重发同一层时会把百分比打回去
	}

	switch p.Stage {
	case "done":
		t.Status = StatusDone
		t.Percent = 100
		if t.EndedAt == 0 {
			t.EndedAt = now
		}
	case "error":
		t.Status = StatusError
		if t.EndedAt == 0 {
			t.EndedAt = now
		}
	case "canceled":
		t.Status = StatusCanceled
		if t.EndedAt == 0 {
			t.EndedAt = now
		}
	case "stalled":
		// 仍在跑，只更新提示
	default:
		t.Status = StatusRunning
	}

	if note := trimNote(p.Note); note != "" {
		t.Note = note
		if stageChanged || p.Stage == "info" || terminalStage {
			appendLog(t, note)
		}
	}

	pruneLocked(now)

	snap := clone(t)
	// 同一终态重复上报只通知一次
	terminal := terminalStage && before != t.Status
	changed := before != t.Status || stageChanged || trimNote(p.Note) != "" || pct > 0
	return snap, changed, terminal
}

// identify 由负载推出任务主键、库内名称与展示标题。
//
// Docker 的安装链路没有版本号（它装的是引擎本身），单独给一个人能懂的标题。
func identify(meta eventMeta, p payload) (id, name, title string) {
	name = p.Version
	if name == "" {
		name = p.Image
	}
	if name == "" {
		name = "engine"
	}
	id = meta.tool + "|" + meta.kind + "|" + name

	switch meta.kind {
	case "setup":
		return id, name, "Docker 引擎"
	case "ext":
		return id, name, "PHP 扩展 " + name
	case "pull":
		return id, name, "Docker 镜像 " + name
	case "op":
		verb := opVerbs[p.Op]
		if verb == "" {
			verb = "镜像操作"
		}
		return id, name, verb + " " + name
	}
	tool := toolNames[meta.tool]
	if tool == "" {
		tool = meta.tool
	}
	return id, name, tool + " " + name
}

// trimNote 说明文本压成一行：有的包会把多行输出塞进 note
func trimNote(s string) string {
	out := make([]rune, 0, len(s))
	lastSpace := false
	for _, r := range s {
		if r == '\n' || r == '\r' || r == '\t' {
			r = ' '
		}
		if r == ' ' {
			if lastSpace {
				continue
			}
			lastSpace = true
		} else {
			lastSpace = false
		}
		out = append(out, r)
	}
	if len(out) > 300 {
		out = append(out[:300], '…')
	}
	return string(out)
}

// appendLog 追加一行日志（相邻重复丢弃，超上限从头截断）
func appendLog(t *Task, note string) {
	if n := len(t.Logs); n > 0 && t.Logs[n-1] == note {
		return
	}
	t.Logs = append(t.Logs, note)
	if len(t.Logs) > keepLogs {
		t.Logs = append([]string(nil), t.Logs[len(t.Logs)-keepLogs:]...)
	}
}

// pruneLocked 淘汰过期/超量的已结束任务（调用方已持锁）
func pruneLocked(now int64) {
	expire := now - finishedTTL.Milliseconds()
	for id, t := range items {
		if t.Finished() && t.EndedAt > 0 && t.EndedAt < expire {
			delete(items, id)
		}
	}
	if len(items) <= maxTasks {
		return
	}
	// 超量：先淘汰结束得最早的，进行中的永远保留
	finished := make([]*Task, 0, len(items))
	for _, t := range items {
		if t.Finished() {
			finished = append(finished, t)
		}
	}
	sort.Slice(finished, func(i, j int) bool { return finished[i].EndedAt < finished[j].EndedAt })
	for _, t := range finished {
		if len(items) <= maxTasks {
			return
		}
		delete(items, t.ID)
	}
}

/* ============================ 读取 ============================ */

// Snapshot 返回全部任务的快照：进行中的在前，其次按开始时间倒序。
//
// 页面挂载时先拉这个再订阅 task:update，"离开期间"的进度就补回来了。
func Snapshot() []Task {
	mu.RLock()
	out := make([]Task, 0, len(items))
	for _, t := range items {
		out = append(out, clone(t))
	}
	mu.RUnlock()

	sort.Slice(out, func(i, j int) bool {
		if out[i].Finished() != out[j].Finished() {
			return !out[i].Finished()
		}
		return out[i].StartedAt > out[j].StartedAt
	})
	return out
}

// Clear 删除一条已结束的任务；进行中的不允许删（还在写，删了会重建）。
func Clear(id string) {
	mu.Lock()
	defer mu.Unlock()
	if t := items[id]; t != nil && t.Finished() {
		delete(items, id)
	}
}

// ClearFinished 清掉所有已结束的任务
func ClearFinished() {
	mu.Lock()
	defer mu.Unlock()
	for id, t := range items {
		if t.Finished() {
			delete(items, id)
		}
	}
}

// clone 深拷贝（Logs 是切片，不能把内部状态暴露给调用方）
func clone(t *Task) Task {
	cp := *t
	cp.Logs = append([]string(nil), t.Logs...)
	return cp
}
