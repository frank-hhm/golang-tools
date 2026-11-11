package datastore

// 引擎侧单测：用内存 sink 替掉真 MySQL/Redis，只验我们自己的逻辑
// （脏标记、排除判定、重试冷却、恢复补齐、启动远端优先、弹窗条件）。
//
// 为什么要有 sinkFactory 这个口子：这些分支的正确性跟数据库通不通没关系，
// 但它们必须在"没有实例的机器"上也能跑绿，否则单测就只能覆盖顺利路径。

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"Tools/appdirs"
	"Tools/services/config"
)

/* ============================ 测试脚手架 ============================ */

// fakeSink 内存实现，契约见 sink.go：Prepare/Put/Fetch/Metas/Close。
// 带锁：契约要求实现可并发调用，测试路径里 flush 与广播确实可能并发。
type fakeSink struct {
	kind string

	mu       sync.Mutex
	docs     map[string][]byte
	metas    map[string]DocMeta
	fail     error // 非 nil 就等于"这个位置现在连不上"
	prepares int
	puts     int
}

func newFakeSink(kind string) *fakeSink {
	return &fakeSink{kind: kind, docs: map[string][]byte{}, metas: map[string]DocMeta{}}
}

func (f *fakeSink) Kind() string { return f.kind }

func (f *fakeSink) Prepare(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.prepares++
	return f.fail
}

func (f *fakeSink) Put(_ context.Context, key string, payload []byte, meta DocMeta) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.docs[key] = append([]byte(nil), payload...)
	f.metas[key] = meta
	f.puts++
	return nil
}

func (f *fakeSink) Fetch(_ context.Context, key string) ([]byte, *DocMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.docs[key]
	if !ok {
		return nil, nil, nil // 契约：远端没有这一份是 (nil, nil, nil)，不是错误
	}
	m := f.metas[key]
	return append([]byte(nil), b...), &m, nil
}

func (f *fakeSink) Metas(context.Context) (map[string]DocMeta, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return nil, f.fail
	}
	out := make(map[string]DocMeta, len(f.metas))
	for k, v := range f.metas {
		out[k] = v
	}
	return out, nil
}

func (f *fakeSink) Close() error { return nil }

// seed 直接往远端塞一份内容：演"上次保存下来的就是这份"，绕开先同步再改的写法。
func (f *fakeSink) seed(key, content string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.docs[key] = []byte(content)
	f.metas[key] = DocMeta{Digest: digestBytes([]byte(content)), Size: int64(len(content)), SyncedAt: time.Now().UnixMilli()}
}

func (f *fakeSink) setFail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = err
}

func (f *fakeSink) body(key string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b, ok := f.docs[key]
	return string(b), ok
}

func (f *fakeSink) meta(key string) (DocMeta, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.metas[key]
	return m, ok
}

func (f *fakeSink) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.docs))
	for k := range f.docs {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (f *fakeSink) counts() (prepares, puts int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.prepares, f.puts
}

// fakeSet 一次给两个位置各一份内存实现：BuildSinks 只会按勾选建，多余的不会被动到。
// 下面的取值方法一律转发到 mysql 那一份 —— 用例里勾的都是 MySQL，redis 只是备着，
// 逐个断言再写一遍 kind 反而更啰嗦。
type fakeSet struct{ mysql, redis *fakeSink }

func (s *fakeSet) keys() []string                  { return s.mysql.keys() }
func (s *fakeSet) body(key string) (string, bool)  { return s.mysql.body(key) }
func (s *fakeSet) meta(key string) (DocMeta, bool) { return s.mysql.meta(key) }
func (s *fakeSet) counts() (int, int)              { return s.mysql.counts() }
func (s *fakeSet) seed(key, content string)        { s.mysql.seed(key, content) }
func (s *fakeSet) setFail(err error)               { s.mysql.setFail(err) }

// startStore 把引擎调到一个可控的干净状态：
//   - data 根与用户配置目录都指到临时目录，受管文档由 files 给（键相对 data 根）；
//   - sinkFactory 换成内存实现，Configure 建出来的位置就是可拿捏的那两份；
//   - 包级状态先清空，用例之间不互相带着上一次的快照与队列跑。
func startStore(t *testing.T, cfg config.StorageConfig, files map[string]string) (dataRoot, cfgFile string, fakes *fakeSet) {
	t.Helper()

	dataRoot = t.TempDir()
	cfgRoot := t.TempDir()
	// 引导文件（app/config.json）的位置由 os.UserConfigDir 决定：Windows 认 APPDATA，
	// Linux 认 XDG_CONFIG_HOME。不指过去的话，清单里会混进跑测试这台机器的真配置，
	// 受管文档的数量与内容就没法断言了。
	t.Setenv("APPDATA", cfgRoot)
	t.Setenv("XDG_CONFIG_HOME", cfgRoot)
	cfgFile = filepath.Join(cfgRoot, "Tools", "config.json")

	if err := appdirs.Set("", dataRoot, ""); err != nil {
		t.Fatalf("重定向数据目录失败: %v", err)
	}
	for rel, content := range files {
		p := filepath.Join(dataRoot, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("建测试目录失败: %v", err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatalf("写测试文档失败: %v", err)
		}
	}

	fakes = &fakeSet{mysql: newFakeSink("mysql"), redis: newFakeSink("redis")}
	prevFactory := sinkFactory
	sinkFactory = func(kind, _, _ string, _ int) Sink {
		if kind == "mysql" {
			return fakes.mysql
		}
		return fakes.redis
	}

	resetEngine()
	Configure(cfg)
	t.Cleanup(func() {
		sinkFactory = prevFactory
		resetEngine()
	})
	return dataRoot, cfgFile, fakes
}

// resetEngine 把包级状态清回复工态。引擎是进程内单例，测试之间必须从零开始。
func resetEngine() {
	mu.Lock()
	cfgSet = config.StorageConfig{}
	sinks = map[string]Sink{}
	ready = map[string]bool{}
	failAt = map[string]time.Time{}
	lastErr = map[string]string{}
	lastSync = map[string]time.Time{}
	remoteMeta = map[string]map[string]DocMeta{}
	snap = map[string]snapEntry{}
	pending = map[string]map[string]bool{}
	dropped = 0
	emitFn = nil
	sessionLocal = false
	fallbackLocal = false
	lastSig = ""
	mu.Unlock()
	probeMu.Lock()
	probes = map[string]instanceProbe{}
	probeMu.Unlock()
}

// mysqlCfg 一份"勾了 MySQL 并绑好实例"的设置，用例都在它上面改个别字段。
func mysqlCfg() config.StorageConfig {
	return config.StorageConfig{
		Local: true, MySQL: true, MySQLInstance: "i1", MySQLDatabase: "tools_data", IntervalSec: 5,
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", path, err)
	}
	return string(b)
}

func pendingKeys() []string {
	mu.Lock()
	defer mu.Unlock()
	var out []string
	for k, kinds := range pending {
		if len(kinds) > 0 {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

/* ============================ 脏标记 ============================ */

func TestScanMarksOnlyChangedDocs(t *testing.T) {
	dataRoot, _, fakes := startStore(t, mysqlCfg(), map[string]string{
		"guard/guard.json": `{"g":1}`,
		// 没有展示名的一条：验证"新服务只要还把缓存落 data/<服务>/xxx.json 就自动纳入"，
		// 不需要加一个服务就回来补一行登记表。
		"downloader/tasks.json": `{"t":1}`,
	})

	scanOnce()
	flush(true)
	got := fakes.keys()
	want := []string{"downloader/tasks.json", "guard/guard.json"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("首轮该把两份配置文档都写到远端，实际写了 %v", got)
	}
	if _, ok := fakes.meta("guard/guard.json"); !ok {
		t.Fatal("远端没记住 guard/guard.json 的元数据")
	}
	if _, ok := fakes.meta("downloader/tasks.json"); !ok {
		t.Fatal("远端没记住 downloader/tasks.json 的元数据")
	}
	// 没在展示名表里的新服务：照旧纳入，标题兜底成文件名——不必"加个服务就回来补一行登记"。
	labels := map[string]string{}
	for _, d := range managedDocs(nil, false) {
		labels[d.Key] = d.Label
	}
	if labels["downloader/tasks.json"] != "tasks.json" {
		t.Errorf("登记表里没有的文档展示名没兜底：%q", labels["downloader/tasks.json"])
	}
	if labels["guard/guard.json"] != "进程守护登记" {
		t.Errorf("已知文档该用登记表里的名字：%q", labels["guard/guard.json"])
	}

	// 内容没变 → 扫描不该标出任何待同步，一轮空 flush 也不该再写远端
	scanOnce()
	if p := pendingKeys(); len(p) != 0 {
		t.Fatalf("内容没变也被标脏: %v", p)
	}
	prepares, puts := fakes.counts()
	flush(false)
	if _, n := fakes.counts(); n != puts {
		t.Fatalf("队列是空的却还在往远端写，puts %d → %d", puts, n)
	}

	// 改一份 → 只有那一份变脏
	if err := os.WriteFile(filepath.Join(dataRoot, "guard", "guard.json"), []byte(`{"g":2}`), 0o600); err != nil {
		t.Fatal(err)
	}
	scanOnce()
	if p := pendingKeys(); len(p) != 1 || p[0] != "guard/guard.json" {
		t.Fatalf("只有改过的那份该变脏，实际 %v", p)
	}
	flush(false)
	if body, _ := fakes.body("guard/guard.json"); body != `{"g":2}` {
		t.Fatalf("远端没收到新内容: %s", body)
	}
	if m, _ := fakes.meta("guard/guard.json"); m.Digest != digestBytes([]byte(`{"g":2}`)) {
		t.Fatal("远端元数据里的摘要没跟着内容更新")
	}
	if prepares == 0 {
		t.Fatal("一轮也没探过位置")
	}
}

/* ============================ 排除与凭据 ============================ */

func TestExclusionAndSensitiveRules(t *testing.T) {
	cfg := mysqlCfg()
	cfg.Exclude = []string{"npm/npmrc"}
	_, _, fakes := startStore(t, cfg, map[string]string{
		"guard/guard.json":           `{"g":1}`,
		"npm/npmrc":                  `prefix=X`,
		"panel/settings.json":        `{"passwordHash":"hash"}`,
		"mysql/mysql-instances.json": `[{"rootPassword":"hunter2"}]`,
		"redis/redis-servers/a.conf": `runtime`,
	})

	scanOnce()
	flush(true)
	if got := fakes.keys(); len(got) != 1 || got[0] != "guard/guard.json" {
		t.Fatalf("只该外存 guard/guard.json，实际 %v", got)
	}

	// 未纳入的原因要说得给人看：这四条各属一种排除路径
	docs := managedDocs(cfg.Exclude, false)
	reasons := map[string]string{}
	for _, d := range docs {
		reasons[d.Key] = d.Reason
		if d.Included && d.Key != "guard/guard.json" {
			t.Fatalf("%s 不该被纳入", d.Key)
		}
	}
	if !strings.Contains(reasons["panel/settings.json"], "永不外存") {
		t.Errorf("panel/settings.json 的原因没说清： %q", reasons["panel/settings.json"])
	}
	if !strings.Contains(reasons["npm/npmrc"], "已在设置页排除") {
		t.Errorf("npm/npmrc 的原因没说清：%q", reasons["npm/npmrc"])
	}
	if !strings.Contains(reasons["mysql/mysql-instances.json"], "含凭据") {
		t.Errorf("实例登记的原因没说清：%q", reasons["mysql/mysql-instances.json"])
	}

	// 勾上"含凭据的文档"：实例登记进来了，但落远端的必须是密文，不是那份明文。
	// 只改设置里这一项，不重跑 Configure——那会把内存 sink 换回真 MySQL。
	mu.Lock()
	cfgSet.IncludeSensitive = true
	mu.Unlock()
	scanOnce()
	flush(true)
	body, ok := fakes.body("mysql/mysql-instances.json")
	if !ok {
		t.Fatal("勾了含凭据仍未外存实例登记")
	}
	if !strings.HasPrefix(body, envelopePrefix) {
		t.Fatalf("外存的凭据文档竟然是明文：%s", body)
	}
	if strings.Contains(body, "hunter2") {
		t.Fatal("密文里居然能直接读到口令")
	}
	m, _ := fakes.meta("mysql/mysql-instances.json")
	if !m.Encrypted || m.Digest != digestBytes([]byte(`[{"rootPassword":"hunter2"}]`)) {
		t.Fatalf("元数据该记明文摘要并标加密，实际 %+v", m)
	}

	// 恢复端读回来还是那份明文：摘要与加密这两件事必须自洽
	plain, err := open([]byte(body))
	if err != nil {
		t.Fatalf("解不开自己写的密文: %v", err)
	}
	if string(plain) != `[{"rootPassword":"hunter2"}]` {
		t.Fatalf("解密结果不对：%s", plain)
	}
}

/* ============================ 远端连不上 ============================ */

func TestUnreachableSinkOnlyMarksPending(t *testing.T) {
	_, _, fakes := startStore(t, mysqlCfg(), map[string]string{"guard/guard.json": `{"g":1}`})
	fakes.setFail(errors.New("实例没起来"))

	flush(true)
	if p := pendingKeys(); len(p) != 1 {
		t.Fatalf("远端连不上时该把这份留在待同步里，实际 %v", p)
	}
	mu.Lock()
	isReady, hasClock := ready["mysql"], !failAt["mysql"].IsZero()
	errMsg := lastErr["mysql"]
	mu.Unlock()
	if isReady {
		t.Fatal("连不上却被标成已就绪")
	}
	if !hasClock {
		t.Fatal("没记重试时钟，下一轮会再撞一次墙")
	}
	if !strings.Contains(errMsg, "实例没起来") {
		t.Errorf("位置错误没留住：%q", errMsg)
	}

	// 重试时钟没到期前，非强制的那几轮不该反复敲门
	before, _ := fakes.counts()
	flush(false)
	flush(false)
	after, _ := fakes.counts()
	if after != before {
		t.Fatalf("冷却期内还在重试：Prepare 次数 %d → %d", before, after)
	}

	// 位置恢复可用 → 队列补完，错误与待同步一起清掉
	fakes.setFail(nil)
	if err := SyncNow(); err != nil {
		t.Fatalf("恢复后同步该成功: %v", err)
	}
	if p := pendingKeys(); len(p) != 0 {
		t.Fatalf("队列没清空: %v", p)
	}
	if body, ok := fakes.body("guard/guard.json"); !ok || body != `{"g":1}` {
		t.Fatalf("补写的内容不对：%q", body)
	}
	v := Snapshot()
	for _, s := range v.Sinks {
		if s.Kind == "mysql" && (s.Pending != 0 || s.LastError != "") {
			t.Fatalf("视图还带着旧状态：%+v", s)
		}
	}
}

/* ============================ 从远端恢复 ============================ */

func TestRestorePullsRemoteCopiesBack(t *testing.T) {
	dataRoot, _, fakes := startStore(t, mysqlCfg(), map[string]string{
		"guard/guard.json": `{"g":"上次保存的"}`,
		"npm/npmrc":        `prefix=本机`,
	})
	// 先把「上次保存下来的样子」立起来：一轮扫描加一次同步，快照与远端就对上了。
	// 省掉这一步的话，Restore 末尾那轮扫描会因为快照是空的，把所有文档都当首次改动排进队列。
	scanOnce()
	flush(false)
	// 远端有、本地清单里没有的键（旧版本留下的）不该被写到磁盘上
	fakes.seed("term/credentials.json", `[{"secret":"旧版残留"}]`)

	// 本地这份写坏了，远端仍留着上次保存的内容
	if err := os.WriteFile(filepath.Join(dataRoot, "guard", "guard.json"), []byte(`{"g":"本地写坏了"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	n, err := Restore()
	if err != nil {
		t.Fatalf("恢复失败: %v", err)
	}
	if n != 1 {
		t.Fatalf("只该覆盖内容真不同的那一份，实际 %d", n)
	}
	if got := readFile(t, filepath.Join(dataRoot, "guard", "guard.json")); got != `{"g":"上次保存的"}` {
		t.Fatalf("本地没被远端那份覆盖：%s", got)
	}
	if _, err := os.Stat(filepath.Join(dataRoot, "term", "credentials.json")); !os.IsNotExist(err) {
		t.Fatal("本地清单里没有的键也被写出来了")
	}
	// 内容本来就一样的那份不该动，也不该因此被重新排队写回远端
	if got := readFile(t, filepath.Join(dataRoot, "npm", "npmrc")); got != `prefix=本机` {
		t.Fatalf("相同内容的文档被改了：%s", got)
	}
	if p := pendingKeys(); len(p) != 0 {
		t.Fatalf("刚从远端回来的内容又被当成本地改动排队: %v", p)
	}
}

/* ============================ 启动时的远端优先 ============================ */

func TestStartupRemoteFirst(t *testing.T) {
	cfg := mysqlCfg()
	cfg.Local = false // 取消本地：远端才是保存位置，本机文件只是缓存
	dataRoot, cfgFile, fakes := startStore(t, cfg, map[string]string{
		"guard/guard.json": `{"g":"本地"}`,
	})
	// 引导文件在用户配置目录里，得让它真的存在，否则清单里根本没这一条
	if err := os.MkdirAll(filepath.Dir(cfgFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgFile, []byte(`{"general":{"appName":"本机旧名"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	fakes.seed("guard/guard.json", `{"g":"远端"}`)
	fakes.seed(configDocKey, `{"general":{"appName":"远端设置"}})`)

	changed, note := Startup(cfg)
	if !changed {
		t.Fatal("config.json 被远端覆盖过，Startup 该回报需要重载配置")
	}
	if !strings.Contains(note, "2 份") {
		t.Errorf("note 里该有恢复份数：%q", note)
	}
	if got := readFile(t, filepath.Join(dataRoot, "guard", "guard.json")); got != `{"g":"远端"}` {
		t.Fatalf("本机文件没被远端盖过：%s", got)
	}
	if got := readFile(t, cfgFile); !strings.Contains(got, "远端设置") {
		t.Fatalf("引导文件没被远端盖过：%s", got)
	}
	v := Snapshot()
	if v.Local {
		t.Error("视图里的本地勾选跟设置不一致")
	}
	if v.NeedsChoice {
		t.Error("远端可用，不该要求弹窗")
	}
}

func TestStartupKeepsLocalWhenSelected(t *testing.T) {
	// 勾着本地（默认）时 Startup 什么都不做：本地文件永远是权威可读源，
	// 远端只是副本，绝不允许启动阶段把远端内容盖到用户正在用的文件上。
	dataRoot, _, fakes := startStore(t, mysqlCfg(), map[string]string{"guard/guard.json": `{"g":"本地"}`})
	fakes.seed("guard/guard.json", `{"g":"远端"}`)
	changed, note := Startup(mysqlCfg())
	if changed || note != "" {
		t.Fatalf("勾着本地却做了远端优先: %v %q", changed, note)
	}
	if got := readFile(t, filepath.Join(dataRoot, "guard", "guard.json")); got != `{"g":"本地"}` {
		t.Fatalf("本地内容被改了：%s", got)
	}
}

/* ============================ 弹窗条件 ============================ */

func TestNeedsChoiceGatesThePopup(t *testing.T) {
	cfg := mysqlCfg()
	cfg.Local = false
	cfg.MySQLDatabase = "tools_data"
	_, _, fakes := startStore(t, cfg, map[string]string{"guard/guard.json": `{"g":1}`})
	fakes.setFail(errors.New("实例没起来"))

	// 没本地 + 启用中的位置一个都没连通 → 前端要弹窗
	if v := buildView(); !v.NeedsChoice {
		t.Fatal("这种局面该弹窗请用户启动服务或改用默认数据")
	}

	// 用户在这次会话里选了"本次使用默认数据" → 不再拦，但不改设置里的勾选
	SetSessionLocal(true)
	if v := buildView(); v.NeedsChoice {
		t.Fatal("已经做了临时选择，还弹窗就是重复打扰")
	}
	if v := buildView(); !v.SessionLocal || v.Local {
		t.Errorf("临时选择该只反映在 sessionLocal 上：%+v", v)
	}
	SetSessionLocal(false)

	// 位置连上了就不再需要弹窗
	fakes.setFail(nil)
	mu.Lock()
	ready["mysql"] = true
	mu.Unlock()
	if v := buildView(); v.NeedsChoice {
		t.Fatal("远端可用却还要弹窗")
	}

	// 勾回本地：无论远端怎样都不该弹
	cfg.Local = true
	Configure(cfg)
	mu.Lock()
	fallbackLocal = true
	mu.Unlock()
	if v := buildView(); v.NeedsChoice {
		t.Fatal("本地是保存位置时没有需要用户做的选择")
	}

	// 一个远端都没勾、又不要本地：只有 Startup 真记过回退事件才算要弹
	cfg.Local = false
	cfg.MySQL = false
	Configure(cfg)
	mu.Lock()
	fallbackLocal = false // 上一段造出来的回退事件清掉，这里验的是「凭空」那半边
	mu.Unlock()
	if v := buildView(); v.NeedsChoice {
		t.Fatal("没发生过回退就不该凭空弹窗")
	}
	mu.Lock()
	fallbackLocal = true
	mu.Unlock()
	if v := buildView(); !v.NeedsChoice {
		t.Fatal("没有任何受管保存位置，该弹窗")
	}
}

/* ============================ 队列上限 ============================ */

// 队列撞上限时的取舍：丢最久没动的那一份，并把"丢过"这件事记下来给前端看。
// 真摆 200 份配置文档不现实，所以直接给 dropOldestLocked 造现场。
func TestPendingQueueDropsOldestWhenFull(t *testing.T) {
	startStore(t, mysqlCfg(), map[string]string{"guard/guard.json": `{"g":1}`})
	mu.Lock()
	pending = map[string]map[string]bool{
		"a/a.json": {"mysql": true},
		"b/b.json": {"mysql": true},
		"c/c.json": {"mysql": true},
	}
	snap = map[string]snapEntry{
		"a/a.json": {MtimeMs: 300},
		"b/b.json": {MtimeMs: 100}, // 最久没动的一份
		"c/c.json": {MtimeMs: 200},
	}
	dropped = 0
	dropOldestLocked()
	left := len(pending)
	d := dropped
	_, hasB := pending["b/b.json"]
	mu.Unlock()
	if hasB {
		t.Fatal("该丢最久没动的那一份")
	}
	if left != 2 || d != 1 {
		t.Fatalf("队列剩 %d 份、丢弃计数 %d，期望 2 与 1", left, d)
	}
}
