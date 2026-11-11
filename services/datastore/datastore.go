package datastore

// 数据保存位置引擎：本地文件永远是权威源，MySQL/Redis 只是"再写一份持久副本"。
//
// 三条设计线，改这个文件之前先看清：
//
//  1. 零侵入。变更发现靠扫描 mtime+size+sha256，不要求任何存量 store 调我们的写入口。
//     代价是"最后一次改动到落远端"最多滞后一个扫描间隔——副本语义可以接受，
//     所以退出前与「立即同步」按钮都做强制 flush 把这段窗口收掉。
//  2. 不打扰。本地勾选时（默认），远端不可用只记 lastError 与待同步数，绝不弹窗、
//     绝不让任何一次保存失败。只有"取消本地 + 远端不可达"这一种组合才算真出事，
//     那由前端在主页弹窗（见 View.FallbackLocal）。
//  3. 一个位置失败不拖累另一个位置，也不拖累程序本身。每个 sink 独立记错误与重试时钟。

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"Tools/services/applog"
	"Tools/services/config"
	"Tools/tools/mysql"
	"Tools/tools/redis"
)

// sinkPrepareRetry 某个位置 Prepare 失败后的重试间隔。定 30s 是两头折：实例启动
// 本身就要几秒到十几秒，太密就是在反复敲一扇没开的门并把错误刷进日志；太疏则用户
// 点了「启动服务」之后要等好久才看到恢复。提示条上的「启动」按钮会主动把时钟清零。
const sinkPrepareRetry = 30 * time.Second

// maxPending 待同步队列上限：正常配置文档不会超过几十份，撞到这个数说明有位置长期
// 不可用同时又有人在反复改设置。超了丢最旧的一份并计数，不让队列无界增长。
const maxPending = 200

// flushTimeout 退出前最后一次 flush 的总时限。超过就带着未完成的副本退出——
// 程序关闭不该被一个没响的 MySQL 拖住。
const flushTimeout = 5 * time.Second

// startupTimeout 启动阶段"远端优先回写"的总时限，同上理由。
const startupTimeout = 6 * time.Second

// instanceProbeTTL 实例运行状态的缓存时长。主页每 5s 刷新一次系统信息，这里不能
// 每次都去连一遍实例（MySQL 的状态查询要跑 SHOW STATUS）。
const instanceProbeTTL = 10 * time.Second

// snapEntry 上一轮扫描看到的文档样子。
type snapEntry struct {
	Digest  string
	Size    int64
	MtimeMs int64
}

// instanceProbe 绑定实例的存在与运行状态缓存。
type instanceProbe struct {
	Name     string
	Exists   bool
	Running  bool
	Checked  time.Time
	ProbeErr string
}

var (
	mu            sync.Mutex
	cfgSet        config.StorageConfig
	sinks         = map[string]Sink{}
	ready         = map[string]bool{}
	failAt        = map[string]time.Time{}
	lastErr       = map[string]string{}
	lastSync      = map[string]time.Time{}
	remoteMeta    = map[string]map[string]DocMeta{} // kind -> key -> meta
	snap          = map[string]snapEntry{}
	pending       = map[string]map[string]bool{} // key -> 还差哪些 kind
	dropped       int
	emitFn        func(name string, data any)
	stopCh        chan struct{}
	running       bool
	sessionLocal  bool   // 用户在这次会话里选了"本次使用默认数据"
	fallbackLocal bool   // 启动时"没本地且远端不可达"确实发生过
	lastSig       string // 上一次广播的状态签名，用来抑制重复事件

	// probeMu 与 mu 分开：实例运行状态探测要连一次数据库（实例没起来时是几秒的
	// 超时等待），跟主锁共用就会把定时同步与广播一起卡在那儿。
	probeMu sync.Mutex
	probes  = map[string]instanceProbe{} // kind -> 绑定实例状态（只由 probeMu 守护）
)

// Configure 记下设置并重建各外存位置。只碰设置，不碰网络。
func Configure(cfg config.StorageConfig) {
	mu.Lock()
	cfgSet = cfg
	for _, s := range sinks {
		_ = s.Close()
	}
	sinks = buildSinks(cfg)
	// 绑定变了就把就绪状态与错误全清一遍：留着旧实例的错误信息，前端会一边显示
	// "已就绪"一边写着上一个实例连不上的话。
	ready = map[string]bool{}
	failAt = map[string]time.Time{}
	lastErr = map[string]string{}
	remoteMeta = map[string]map[string]DocMeta{}
	mu.Unlock()
	probeMu.Lock()
	probes = map[string]instanceProbe{}
	probeMu.Unlock()
}

// ApplyChanged 只在设置真的变了时才重建位置：SaveConfig 是"整份配置保存"的入口，
// 任何一栏设置保存都会带一份 storage 过来，无条件重连会把刚建好的连接与就绪状态
// 白白抖掉（前端于是看到提示条一闪一闪地重连）。设置没变就是空转。
func ApplyChanged(cfg config.StorageConfig) {
	if storageEqual(cfg) {
		return
	}
	Configure(cfg)
}

func storageEqual(cfg config.StorageConfig) bool {
	b, _ := json.Marshal(cfg)
	c, _ := json.Marshal(settings())
	return string(b) == string(c)
}

// settings 取当前设置副本。Configure 是唯一写入方，读取都走这里，
// 免得各函数拿着一个正在被换掉的 struct 读数。
func settings() config.StorageConfig {
	mu.Lock()
	defer mu.Unlock()
	return cfgSet
}

// sinkFactory 位置类型 → 具体实现。生产路径就是真 MySQL / 真 Redis；单测把它整体换成
// 内存实现，才能在没有任何实例的机器上验脏标记、重试冷却、恢复补齐这些引擎自己的逻辑。
var sinkFactory = defaultSinkFactory

func defaultSinkFactory(kind, instID, database string, redisDB int) Sink {
	switch kind {
	case "mysql":
		return newMySQLSink(instID, database)
	case "redis":
		return newRedisSink(instID, redisDB)
	default:
		return nil
	}
}

// buildSinks 按设置造启用中的位置。没勾的就不建，也就永远不会去敲那扇门。
func buildSinks(cfg config.StorageConfig) map[string]Sink {
	out := map[string]Sink{}
	if cfg.MySQL && strings.TrimSpace(cfg.MySQLInstance) != "" {
		if s := sinkFactory("mysql", cfg.MySQLInstance, cfg.MySQLDatabase, 0); s != nil {
			out["mysql"] = s
		}
	}
	if cfg.Redis && strings.TrimSpace(cfg.RedisInstance) != "" {
		if s := sinkFactory("redis", cfg.RedisInstance, "", cfg.RedisDB); s != nil {
			out["redis"] = s
		}
	}
	return out
}

// Startup 在程序启动早期（各服务读自己的配置之前）跑一次"远端优先"。
//
// 只有取消本地勾选才有意义：那种模式下远端才是保存位置，本机文件只是缓存，
// 所以远端版本必须盖过本地，之后各服务读到的才是"上次真正保存下来的那份"。
// 返回 changedConfig 表示 config.json 被远端版本覆盖过，主程序要重新加载一次配置。
func Startup(cfg config.StorageConfig) (changedConfig bool, note string) {
	Configure(cfg)
	if cfg.Local {
		return false, ""
	}
	// 一个远端都没勾，又不要本地：没有任何保存位置，只能退回本地并记下这件事，
	// 前端据此弹窗（而不是静默地把用户的数据写到一个他明确取消勾选的位置上）。
	if len(activeSinks()) == 0 {
		markFallback("没有勾选任何保存位置")
		return false, "没有可用的保存位置，本次使用本地数据"
	}
	ctx, cancel := context.WithTimeout(context.Background(), startupTimeout)
	defer cancel()
	var restored, errs []string
	for kind, s := range activeSinks() {
		if err := s.Prepare(ctx); err != nil {
			errs = append(errs, kind+": "+err.Error())
			continue
		}
		metas, err := s.Metas(ctx)
		if err != nil {
			errs = append(errs, kind+": "+err.Error())
			continue
		}
		// 拿本地清单里"应该被外存"的那批文档去和远端比摘要
		for _, d := range managedDocs(cfg.Exclude, cfg.IncludeSensitive) {
			if !d.Included {
				continue
			}
			m, ok := metas[d.Key]
			if !ok || m.Digest == d.Digest {
				continue
			}
			payload, meta, err := s.Fetch(ctx, d.Key)
			if err != nil || payload == nil {
				continue
			}
			if meta != nil && meta.Digest == d.Digest {
				continue // 内容一样，只是元数据落后
			}
			plain, err := open(payload)
			if err != nil {
				errs = append(errs, d.Key+": 远端密文解不开（主密钥换过了？）")
				continue
			}
			if err := writeFileAtomic(d.Path, plain); err != nil {
				errs = append(errs, d.Key+": 回写本地失败: "+err.Error())
				continue
			}
			restored = append(restored, d.Key)
			if d.Key == configDocKey {
				changedConfig = true
			}
		}
		mu.Lock()
		remoteMeta[kind] = metas
		ready[kind] = true
		lastErr[kind] = ""
		mu.Unlock()
	}
	if len(restored) > 0 {
		note = fmt.Sprintf("已从远端恢复 %d 份文档：%s", len(restored), strings.Join(restored, "、"))
		applog.Infof("datastore", "启动远端优先：恢复 %d 份文档", len(restored))
	}
	if len(errs) > 0 {
		mu.Lock()
		for _, e := range errs {
			kind, msg, _ := strings.Cut(e, ":")
			if kind == "mysql" || kind == "redis" {
				lastErr[kind] = msg
				failAt[kind] = time.Now().Add(sinkPrepareRetry)
			}
		}
		mu.Unlock()
	}
	// 一个位置都没准备好 → 这次启动没有任何受管保存位置 → 需要弹窗
	if !anyReady() {
		markFallback(strings.Join(errs, "；"))
		if note == "" {
			note = "绑定的保存位置都不可用，本次使用本地数据"
		}
	}
	// 首轮扫描：把当前本地样子记成快照，之后只把变化过的文档入队，
	// 不会每次启动都把所有文档重写一遍远端。
	scanOnce()
	return changedConfig, note
}

func markFallback(why string) {
	mu.Lock()
	fallbackLocal = true
	mu.Unlock()
	if why != "" {
		applog.Warnf("datastore", "本地未勾选且远端不可用，本次以本地数据运行：%s", why)
	}
}

func anyReady() bool {
	mu.Lock()
	defer mu.Unlock()
	for kind := range sinks {
		if ready[kind] {
			return true
		}
	}
	return false
}

func activeSinks() map[string]Sink {
	mu.Lock()
	defer mu.Unlock()
	out := make(map[string]Sink, len(sinks))
	for k, v := range sinks {
		out[k] = v
	}
	return out
}

// Start 起定时扫描。emit 为事件广播函数（nil 也能跑，只是不通知前端）。
func Start(emit func(name string, data any)) {
	mu.Lock()
	if running {
		mu.Unlock()
		return
	}
	emitFn = emit
	stopCh = make(chan struct{})
	running = true
	interval := cfgSet.IntervalSec
	mu.Unlock()
	if interval <= 0 {
		interval = config.DefaultStorageIntervalSec
	}
	go loop(time.Duration(interval)*time.Second, stopCh)
}

func loop(tick time.Duration, stop <-chan struct{}) {
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-t.C:
			scanOnce()
			for _, kind := range dueRetries() {
				clearFailClock(kind)
			}
			flush(false)
			broadcast()
		}
	}
}

// dueRetries 列出重试时钟到期的位置，让到点的那一轮重新探一次。
func dueRetries() []string {
	mu.Lock()
	defer mu.Unlock()
	var out []string
	now := time.Now()
	for kind := range sinks {
		if !ready[kind] && !failAt[kind].IsZero() && now.After(failAt[kind]) {
			out = append(out, kind)
		}
	}
	return out
}

func clearFailClock(kind string) {
	mu.Lock()
	failAt[kind] = time.Time{}
	mu.Unlock()
}

// Stop 停定时器并做最后一次 flush（限时），然后关掉手上的连接。
func Stop() {
	mu.Lock()
	ch := stopCh
	stopCh = nil
	running = false
	mu.Unlock()
	if ch != nil {
		close(ch)
	}
	flush(true)
	for _, s := range activeSinks() {
		_ = s.Close()
	}
}

// scanOnce 扫一遍受管文档，把新增/内容变过的键记进待同步队列。
func scanOnce() {
	cfg := settings()
	docs := managedDocs(cfg.Exclude, cfg.IncludeSensitive)
	mu.Lock()
	seen := make(map[string]bool, len(docs))
	for _, d := range docs {
		seen[d.Key] = true
		if !d.Included {
			continue
		}
		old, had := snap[d.Key]
		if !had || old.Digest != d.Digest {
			if len(pending) >= maxPending && !had {
				dropOldestLocked()
			}
			if pending[d.Key] == nil {
				pending[d.Key] = map[string]bool{}
			}
			// 内容变了就要重写给每一个启用中的位置
			for kind := range sinks {
				pending[d.Key][kind] = true
			}
		}
		snap[d.Key] = snapEntry{Digest: d.Digest, Size: d.Size, MtimeMs: d.MtimeMs}
	}
	// 文件被删（重置目录、卸载服务）就不再管它：远端那份留着，宁可多留一份历史，
	// 也不做"本地一删就把远端也删掉"的联动——那种联动会把误删放大成两处一起丢。
	for k := range snap {
		if !seen[k] && !strings.HasPrefix(k, "lanshare/") {
			delete(snap, k)
			delete(pending, k)
		}
	}
	mu.Unlock()
}

// dropOldestLocked 队列满时丢一份最久没动的（调用方持锁）。
func dropOldestLocked() {
	var oldest string
	var oldestMs int64 = -1
	for k, kinds := range pending {
		if len(kinds) == 0 {
			continue
		}
		ms := snap[k].MtimeMs
		if oldestMs < 0 || ms < oldestMs {
			oldest, oldestMs = k, ms
		}
	}
	if oldest != "" {
		delete(pending, oldest)
		dropped++
		applog.Warnf("datastore", "待同步队列已满，丢弃最旧的一份（%s），累计 %d 份", oldest, dropped)
	}
}

// flush 把待同步队列里的文档写给各位置。force=true 时忽略重试时钟，硬探一次。
func flush(force bool) {
	// 先重扫一次，保证"这一轮里刚被别的模块写完"的文件也进得来
	scanOnce()
	cfg := settings()
	docs := managedDocs(cfg.Exclude, cfg.IncludeSensitive)
	byKey := make(map[string]doc, len(docs))
	for _, d := range docs {
		byKey[d.Key] = d
	}
	mu.Lock()
	if len(pending) == 0 || len(sinks) == 0 {
		mu.Unlock()
		return
	}
	type job struct {
		key   string
		kinds []string
	}
	jobs := make([]job, 0, len(pending))
	for k, kinds := range pending {
		var ks []string
		for kind := range kinds {
			ks = append(ks, kind)
		}
		sort.Strings(ks)
		jobs = append(jobs, job{key: k, kinds: ks})
	}
	mu.Unlock()
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].key < jobs[j].key })

	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	for _, j := range jobs {
		d, ok := byKey[j.key]
		if !ok || !d.Included {
			mu.Lock()
			delete(pending, j.key)
			mu.Unlock()
			continue
		}
		raw, err := os.ReadFile(d.Path)
		if err != nil {
			mu.Lock()
			delete(pending, j.key)
			mu.Unlock()
			continue
		}
		if digestBytes(raw) != d.Digest {
			continue // 读到的已经比快照更新，下一轮再写，不追这一刻的变化
		}
		payload := raw
		encrypted := false
		if shouldSeal(d) {
			sealed, err := seal(raw)
			if err != nil {
				recordErr("", d.Key+" 加密失败: "+err.Error())
				continue
			}
			payload, encrypted = sealed, true
		}
		for _, kind := range j.kinds {
			s := sinkOf(kind)
			if s == nil {
				markPendingDone(j.key, kind)
				continue
			}
			if !force && !usable(kind) {
				continue // 还在重试冷却里，别每条文档都撞一次墙
			}
			if err := s.Prepare(ctx); err != nil {
				markUnusable(kind, err.Error())
				continue
			}
			meta := DocMeta{Digest: d.Digest, Size: d.Size, Encrypted: encrypted, SyncedAt: time.Now().UnixMilli()}
			if err := s.Put(ctx, d.Key, payload, meta); err != nil {
				markUnusable(kind, err.Error())
				continue
			}
			markPendingDone(j.key, kind)
			mu.Lock()
			if remoteMeta[kind] == nil {
				remoteMeta[kind] = map[string]DocMeta{}
			}
			remoteMeta[kind][d.Key] = meta
			ready[kind] = true
			lastErr[kind] = ""
			lastSync[kind] = time.Now()
			mu.Unlock()
		}
	}
}

func sinkOf(kind string) Sink {
	mu.Lock()
	defer mu.Unlock()
	return sinks[kind]
}

// usable 这个位置现在能不能试：就绪，或已过重试间隔。
func usable(kind string) bool {
	mu.Lock()
	defer mu.Unlock()
	if ready[kind] {
		return true
	}
	return failAt[kind].IsZero() || time.Now().After(failAt[kind])
}

func markUnusable(kind, msg string) {
	mu.Lock()
	ready[kind] = false
	failAt[kind] = time.Now().Add(sinkPrepareRetry)
	lastErr[kind] = msg
	mu.Unlock()
	applog.Warnf("datastore", "%s 外存位置不可用：%s", kind, msg)
}

func recordErr(kind, msg string) {
	mu.Lock()
	if kind != "" {
		lastErr[kind] = msg
	}
	mu.Unlock()
	applog.Warnf("datastore", "%s", msg)
}

func markPendingDone(key, kind string) {
	mu.Lock()
	if kinds, ok := pending[key]; ok {
		delete(kinds, kind)
		if len(kinds) == 0 {
			delete(pending, key)
		}
	}
	mu.Unlock()
}

/* ============================ 视图与广播 ============================ */

// DocView 一份文档在前端的样子。
type DocView struct {
	Key       string   `json:"key"`
	Label     string   `json:"label"`
	Path      string   `json:"path"`
	Size      int64    `json:"size"`
	MtimeMs   int64    `json:"mtimeMs"`
	Sensitive bool     `json:"sensitive"`
	Included  bool     `json:"included"`
	Reason    string   `json:"reason"`
	AtRemote  []string `json:"atRemote"` // 哪些位置已经有这份且摘要一致
}

// SinkView 一个外存位置的状态。前端据此决定提示条与弹窗该说什么。
type SinkView struct {
	Kind            string `json:"kind"`
	Enabled         bool   `json:"enabled"`
	Bound           bool   `json:"bound"`
	InstanceID      string `json:"instanceId"`
	InstanceName    string `json:"instanceName"`
	InstanceMissing bool   `json:"instanceMissing"` // 绑的实例已从登记表消失 → 提示去新增
	Running         bool   `json:"running"`
	ProbeError      string `json:"probeError"`
	Coords          string `json:"coords"`
	Ready           bool   `json:"ready"`
	Docs            int    `json:"docs"`
	Pending         int    `json:"pending"`
	LastSyncAt      string `json:"lastSyncAt"`
	LastError       string `json:"lastError"`
}

// View 数据保存位置的全貌（DataStoreStatus 的返回类型）。
type View struct {
	Local            bool       `json:"local"`
	IncludeSensitive bool       `json:"includeSensitive"`
	IntervalSec      int        `json:"intervalSec"`
	Running          bool       `json:"running"`
	Docs             []DocView  `json:"docs"`
	Sinks            []SinkView `json:"sinks"`
	Pending          int        `json:"pending"`
	TotalBytes       int64      `json:"totalBytes"`
	Dropped          int        `json:"dropped"`
	FallbackLocal    bool       `json:"fallbackLocal"`
	SessionLocal     bool       `json:"sessionLocal"`
	NeedsChoice      bool       `json:"needsChoice"` // 前端据此弹窗：没本地 + 没有可用位置
}

// Snapshot 取当前全貌。会顺带刷一次扫描（不上网络，只读本地）。
// 叫 Snapshot 不叫 View：给前端的结构体已经占了 View 这个名字，函数同名就是重复声明。
func Snapshot() View {
	scanOnce()
	return buildView()
}

func buildView() View {
	cfg := settings()
	docs := managedDocs(cfg.Exclude, cfg.IncludeSensitive)
	// 实例运行状态先在线外探好，再带着结果进锁：探一个没起来的 MySQL 可能要几秒，
	// 拿着主锁干这件事会把定时同步与广播一起堵死。
	probed := map[string]instanceProbe{}
	for _, kind := range []string{"mysql", "redis"} {
		if id := boundInstance(kind, cfg); id != "" {
			probed[kind] = probeInstance(kind, id)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	out := View{
		Local:            cfg.Local,
		IncludeSensitive: cfg.IncludeSensitive,
		IntervalSec:      cfg.IntervalSec,
		Running:          running,
		SessionLocal:     sessionLocal,
		FallbackLocal:    fallbackLocal,
		Dropped:          dropped,
		Docs:             make([]DocView, 0, len(docs)),
	}
	for _, d := range docs {
		v := DocView{
			Key: d.Key, Label: d.Label, Path: d.Path, Size: d.Size, MtimeMs: d.MtimeMs,
			Sensitive: d.Sensitive, Included: d.Included, Reason: d.Reason,
			AtRemote: []string{},
		}
		for kind, metas := range remoteMeta {
			if m, ok := metas[d.Key]; ok && m.Digest == d.Digest {
				v.AtRemote = append(v.AtRemote, kind)
			}
		}
		sort.Strings(v.AtRemote)
		if d.Included {
			out.TotalBytes += d.Size
		}
		out.Docs = append(out.Docs, v)
	}
	var anyEnabled, anyUsable bool
	for _, kind := range []string{"mysql", "redis"} {
		sv := sinkView(kind, docs, cfg, probed[kind])
		if sv.Enabled {
			anyEnabled = true
			if sv.Ready {
				anyUsable = true
			}
		}
		out.Sinks = append(out.Sinks, sv)
	}
	for _, kinds := range pending {
		if len(kinds) > 0 {
			out.Pending++
		}
	}
	// 需要弹窗的那一种局面：本地不算保存位置，且启用中的位置一个都没连通。
	// 用户在这次会话里挑了「本次使用默认数据」（sessionLocal）就不再拦他：那是一次
	// 明确的临时选择，不是没选；下次启动这标记清零，照旧会被问一次。
	out.NeedsChoice = !cfg.Local && !sessionLocal && (anyEnabled && !anyUsable || (!anyEnabled && fallbackLocal))
	return out
}

// boundInstance 这个位置当前绑定的实例 id（没勾或没选就是空）。
func boundInstance(kind string, cfg config.StorageConfig) string {
	if kind == "mysql" {
		return strings.TrimSpace(cfg.MySQLInstance)
	}
	return strings.TrimSpace(cfg.RedisInstance)
}

// sinkView 组装一个位置的状态；调用方持 mu（读 ready/lastErr/remoteMeta/pending），
// 实例探测结果由锁外算好传进来。
func sinkView(kind string, docs []doc, cfg config.StorageConfig, p instanceProbe) SinkView {
	sv := SinkView{Kind: kind}
	var instID string
	if kind == "mysql" {
		sv.Enabled = cfg.MySQL
		instID = strings.TrimSpace(cfg.MySQLInstance)
		sv.Coords = cfg.MySQLDatabase + " · " + mysqlTable
	} else {
		sv.Enabled = cfg.Redis
		instID = strings.TrimSpace(cfg.RedisInstance)
		sv.Coords = fmt.Sprintf("db %d · %s*", cfg.RedisDB, redisPrefix)
	}
	if !sv.Enabled || instID == "" {
		return sv
	}
	sv.Bound = true
	sv.InstanceID = instID
	sv.InstanceName, sv.InstanceMissing, sv.Running, sv.ProbeError = p.Name, !p.Exists, p.Running, p.ProbeErr
	sv.Ready = ready[kind]
	sv.LastError = lastErr[kind]
	if t, ok := lastSync[kind]; ok && !t.IsZero() {
		sv.LastSyncAt = t.Format("2006-01-02 15:04:05")
	}
	if metas, ok := remoteMeta[kind]; ok {
		sv.Docs = len(metas)
	}
	for _, d := range docs {
		if d.Included && pending[d.Key][kind] {
			sv.Pending++
		}
	}
	return sv
}

// probeInstance 查绑定实例在不在登记表里、跑起来没有。结果带 TTL 缓存，
// 免得主页每 5 秒刷新一次就真去连一次数据库。
func probeInstance(kind, id string) instanceProbe {
	probeMu.Lock()
	defer probeMu.Unlock()
	if p, ok := probes[kind]; ok && p.Exists && time.Since(p.Checked) < instanceProbeTTL {
		return p
	}
	p := instanceProbe{Checked: time.Now()}
	switch kind {
	case "mysql":
		list, err := mysql.ListInstances()
		if err != nil {
			p.ProbeErr = err.Error()
		}
		for i := range list {
			if list[i].ID == id {
				p.Exists, p.Name = true, list[i].Name
			}
		}
		if p.Exists {
			st, err := mysql.GetInstanceStatus(id)
			if err != nil {
				p.ProbeErr = err.Error()
			} else {
				p.Running = st.Running
			}
		}
	case "redis":
		list, err := redis.ListInstances()
		if err != nil {
			p.ProbeErr = err.Error()
		}
		for i := range list {
			if list[i].ID == id {
				p.Exists, p.Name = true, list[i].Name
			}
		}
		if p.Exists {
			st, err := redis.GetInstanceStatus(id)
			if err != nil {
				p.ProbeErr = err.Error()
			} else {
				p.Running = st.Running
			}
		}
	}
	probes[kind] = p
	return p
}

// broadcast 位置状态或队列有变化时通知前端；完全没变就不发，免得主页白刷新。
func broadcast() {
	v := buildView()
	sig := viewSignature(v)
	mu.Lock()
	changed := sig != lastSig
	lastSig = sig
	emit := emitFn
	mu.Unlock()
	if changed && emit != nil {
		emit("datastore:status", v)
	}
}

func viewSignature(v View) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%v|%v|%d|%d|", v.Local, v.IncludeSensitive, v.Pending, len(v.Sinks))
	for _, s := range v.Sinks {
		fmt.Fprintf(&b, "%s:%v/%v/%v/%v/%d/%d/%s/%s;", s.Kind, s.Enabled, s.InstanceMissing, s.Running, s.Ready, s.Docs, s.Pending, s.LastSyncAt, s.LastError)
	}
	return b.String()
}

/* ============================ 对外动作 ============================ */

// SyncNow 立即把当前所有应外存的文档强制写一遍远端（忽略快照，也忽略重试冷却）。
// 供设置页「立即同步」按钮与实例启动成功后的回调使用。
func SyncNow() error {
	enabled := activeSinks()
	if len(enabled) == 0 {
		return fmt.Errorf("还没有勾选任何远端保存位置")
	}
	mu.Lock()
	snap = map[string]snapEntry{} // 清快照，让 scanOnce 把所有文档都当新的看一遍
	mu.Unlock()
	scanOnce()
	flush(true)
	broadcast()
	if !anyReady() {
		return fmt.Errorf("绑定的保存位置都连不上，详见状态里的错误")
	}
	return nil
}

// Restore 把远端的文档拉回本地（内容不同才写）。返回实际覆盖的份数。
// 与启动时那次不同：这里是用户在设置页主动点的，所以不看 Local 勾选。
func Restore() (int, error) {
	enabled := activeSinks()
	if len(enabled) == 0 {
		return 0, fmt.Errorf("还没有勾选任何远端保存位置")
	}
	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()
	cfg := settings()
	docs := managedDocs(cfg.Exclude, cfg.IncludeSensitive)
	byKey := make(map[string]doc, len(docs))
	for _, d := range docs {
		byKey[d.Key] = d
	}
	var errs []string
	n := 0
	for kind, s := range enabled {
		if err := s.Prepare(ctx); err != nil {
			errs = append(errs, kind+": "+err.Error())
			continue
		}
		metas, err := s.Metas(ctx)
		if err != nil {
			errs = append(errs, kind+": "+err.Error())
			continue
		}
		for key, m := range metas {
			d, ok := byKey[key]
			if !ok {
				continue // 远端有、本地清单里没有的键不动（可能是旧版本留下的）
			}
			if d.Digest == m.Digest && d.Size == m.Size {
				continue
			}
			payload, _, err := s.Fetch(ctx, key)
			if err != nil || payload == nil {
				continue
			}
			plain, err := open(payload)
			if err != nil {
				errs = append(errs, key+": 远端密文解不开（主密钥换过了？）")
				continue
			}
			if err := writeFileAtomic(d.Path, plain); err != nil {
				errs = append(errs, key+": 写本地失败: "+err.Error())
				continue
			}
			n++
			// 快照跟着改成刚写下来的这一份，而不是清空整张：清空会让下一轮扫描把
			// "刚从远端回来的内容"当成本地新改动，又原样写回远端，白跑一趟；
			// 其余没被动过的文档照旧拿着旧快照比就行。
			var mtimeMs int64
			if st, err := os.Stat(d.Path); err == nil {
				mtimeMs = st.ModTime().UnixMilli()
			}
			mu.Lock()
			snap[key] = snapEntry{Digest: digestBytes(plain), Size: int64(len(plain)), MtimeMs: mtimeMs}
			mu.Unlock()
		}
		mu.Lock()
		remoteMeta[kind] = metas
		ready[kind] = true
		mu.Unlock()
	}
	scanOnce()
	broadcast()
	if n == 0 && len(errs) > 0 {
		return 0, fmt.Errorf("没有恢复任何文档：%s", strings.Join(errs, "；"))
	}
	return n, nil
}

// SetSessionLocal 记下"本次使用默认数据"这个选择；只在当前进程有效，
// 不改设置里的勾选——用户下次启动还想被提醒一次，而不是被自己的临时选择永久改掉配置。
func SetSessionLocal(v bool) {
	mu.Lock()
	sessionLocal = v
	if v {
		fallbackLocal = false
	}
	mu.Unlock()
	broadcast()
}

// StartBound 启动这个位置绑定的实例。主页提示条上的「启动」按钮走这里；
// 实例已经在跑就是幂等。
func StartBound(kind string) error {
	cfg := settings()
	switch kind {
	case "mysql":
		id := strings.TrimSpace(cfg.MySQLInstance)
		if id == "" {
			return fmt.Errorf("还没有绑定 MySQL 实例")
		}
		if err := mysql.StartInstance(id); err != nil {
			return err
		}
	case "redis":
		id := strings.TrimSpace(cfg.RedisInstance)
		if id == "" {
			return fmt.Errorf("还没有绑定 Redis 实例")
		}
		if err := redis.StartInstance(id); err != nil {
			return err
		}
	default:
		return fmt.Errorf("未知保存位置类型: %s", kind)
	}
	InstanceStarted(kind)
	return nil
}

// InstanceStarted 某个服务实例刚被拉起来：清掉这个位置的重试时钟与探测缓存，
// 稍后补一轮同步。
//
// 各服务自己的启动入口也会调这里（用户从 MySQL/Redis 页点的启动同样是启动），
// 所以不能假设调用方就是本页：kind 跟当前绑定不相干就直接返回，不做任何重连。
// 实例真正能接受连接还要几秒，这里不阻塞等待，交给下一轮扫描与重试时钟去接。
func InstanceStarted(kind string) {
	cfg := settings()
	if boundInstance(kind, cfg) == "" {
		return
	}
	probeMu.Lock()
	delete(probes, kind)
	probeMu.Unlock()
	mu.Lock()
	failAt[kind] = time.Time{}
	mu.Unlock()
	go func() {
		time.Sleep(2 * time.Second)
		flush(true)
		broadcast()
	}()
}

// firstMySQLInstance 取登记表里的第一个实例 id，供"还没选实例"时先列一次库用。
func firstMySQLInstance() string {
	list, err := mysql.ListInstances()
	if err != nil || len(list) == 0 {
		return ""
	}
	return list[0].ID
}

/* ============================ 设置页要看的周边信息 ============================ */

// InstanceItem 一个可选的服务实例（设置页的实例下拉）。
//
// 这里刻意不直接返回 mysql.InstanceConfig / redis.InstanceConfig：那两个结构体里带着
// RootPassword、Password 这些明文凭据，绑定方法一旦返回就原样发到前端。下拉只需要
// 名字、模式、端口和「跑没跑」这几样，别的一概不给。
type InstanceItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Mode    string `json:"mode"`
	Port    int    `json:"port"`
	Version string `json:"version"`
	Running bool   `json:"running"`
}

// ListInstances 列出某个位置类型下可选的本机服务实例。没装 Docker 时容器模式的
// 实例一样列进来：连接走的仍是 127.0.0.1:发布端口，与本地模式同一条路。
func ListInstances(kind string) ([]InstanceItem, error) {
	switch kind {
	case "mysql":
		list, err := mysql.ListInstances()
		if err != nil {
			return nil, err
		}
		out := make([]InstanceItem, 0, len(list))
		for _, inst := range list {
			item := InstanceItem{ID: inst.ID, Name: inst.Name, Mode: inst.Mode, Port: inst.Port, Version: inst.Version}
			if st, err := mysql.GetInstanceStatus(inst.ID); err == nil {
				item.Running = st.Running
			}
			out = append(out, item)
		}
		return out, nil
	case "redis":
		list, err := redis.ListInstances()
		if err != nil {
			return nil, err
		}
		out := make([]InstanceItem, 0, len(list))
		for _, inst := range list {
			item := InstanceItem{ID: inst.ID, Name: inst.Name, Mode: inst.Mode, Port: inst.Port, Version: inst.Version}
			if st, err := redis.GetInstanceStatus(inst.ID); err == nil {
				item.Running = st.Running
			}
			out = append(out, item)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("未知保存位置类型: %s", kind)
	}
}

// DBOption 库选择器的一个候选项。
type DBOption struct {
	Value string `json:"value"` // MySQL：库名；Redis：库序号的十进制字符串
	Label string `json:"label"`
}

// ListDBOptions 列出这个位置可选的库。MySQL 是实例里已有的库（实例没起来时列不出，
// 返回错误由前端提示「先启动实例」，同时仍允许手填默认库名）；Redis 是固定 0~15 序号。
func ListDBOptions(ctx context.Context, kind, instID string) ([]DBOption, error) {
	switch kind {
	case "mysql":
		if strings.TrimSpace(instID) == "" {
			instID = firstMySQLInstance()
		}
		if instID == "" {
			return nil, fmt.Errorf("这台机器上还没有 MySQL 实例，请先在 MySQL 服务里新增一个")
		}
		names, err := newMySQLSink(instID, config.DefaultStorageDatabase).ListDatabases(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]DBOption, 0, len(names))
		for _, n := range names {
			out = append(out, DBOption{Value: n, Label: n})
		}
		return out, nil
	case "redis":
		// Redis 的库是序号：可建、无需实例配合，所以不会失败。上限取默认配置 16。
		out := make([]DBOption, 0, redisDefaultDatabases)
		for i := 0; i < redisDefaultDatabases; i++ {
			v := fmt.Sprint(i)
			out = append(out, DBOption{Value: v, Label: "db " + v})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("未知保存位置类型: %s", kind)
	}
}

// redisDefaultDatabases redis.conf 里 databases 的默认值。设置页给的就是这一串序号；
// 用户自己改小了也不怕——真正写入时 SELECT 失败会照实报错。
const redisDefaultDatabases = 16

// SinkInfo 主动试连一个位置并回报看到的事实，供设置页「测试连接」按钮显示。
// 不走缓存、不动引擎状态：用户刚改完绑定就想立刻看一眼，这时候拿旧快照最没用。
//
// 建的是临时 sink，不碰引擎里那一份：试连接不该顺手把定时同步在用的连接与重试时钟改掉。
func SinkInfo(ctx context.Context, kind, instID, database string, redisDB int) (string, error) {
	switch kind {
	case "mysql":
		s := newMySQLSink(instID, database)
		if err := s.Prepare(ctx); err != nil {
			return "", err
		}
		defer s.Close()
		names, err := s.ListDatabases(ctx)
		if err != nil {
			return "", err
		}
		rows, err := s.CountDocs(ctx)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("库 %s 已就绪（实例上共 %d 个库），外存表现有 %d 份文档", database, len(names), rows), nil
	case "redis":
		s := newRedisSink(instID, redisDB)
		if err := s.Prepare(ctx); err != nil {
			return "", err
		}
		n, err := s.ListDBSize(ctx)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("db %d 可写，当前库里共 %d 个键", redisDB, n), nil
	default:
		return "", fmt.Errorf("未知保存位置类型: %s", kind)
	}
}

/* ============================ 小工具 ============================ */

// writeFileAtomic 先写同目录临时文件再 rename，跟各服务自己的写法保持一致：
// 直接覆盖正在被读的 json 文件，会有一小段窗口让别人读到半截。
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".datastore.tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		// Windows 上目标文件正被占用时 rename 会失败：退化成直接覆盖，
		// 至少不让一次恢复整单失败（占用窗口极短，重试代价更高）。
		return os.WriteFile(path, data, 0o600)
	}
	return nil
}
