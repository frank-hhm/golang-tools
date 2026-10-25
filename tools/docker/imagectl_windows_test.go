//go:build windows

package docker

import (
	"strings"
	"testing"
	"time"
)

// refHost 决定“这个引用属于哪个仓库”：没域名的（mysql、library/mysql）归 Docker Hub
func TestRefHost(t *testing.T) {
	cases := map[string]string{
		"mysql:8.4":                     "",
		"library/mysql":                 "",
		"mysql":                         "",
		"docker.io/library/mysql:8.4":   "",
		"index.docker.io/library/redis": "",
		"harbor.corp/ns/app:1.0":        "harbor.corp",
		"localhost:5000/app":            "localhost:5000",
		"localhost/app":                 "localhost",
		"127.0.0.1:5000/app":            "127.0.0.1:5000",
		"":                              "",
	}
	for ref, want := range cases {
		if got := refHost(ref); got != want {
			t.Errorf("refHost(%q) = %q, want %q", ref, got, want)
		}
	}
}

// 真实层 ID 是 12 位摘要，字节数量级也要放到 daemon 真会报的程度
const (
	layerA = "0bcc39d79065"
	layerB = "4f4fb700ef54"
	layerC = "6225ade3c743"
)

const mb = int64(1) << 20

func newTracker() *layerTracker {
	return &layerTracker{cur: map[string]int64{}, tot: map[string]int64{}, done: map[string]bool{},
		seen: map[string]struct{}{}}
}

// 字节数可信时，整体进度按字节加权，而不是数“第几层”
func TestLayerTrackerOverall(t *testing.T) {
	tr := newTracker()
	if got := tr.overall(); got != 0 {
		t.Errorf("没有分层时 overall = %d, want 0", got)
	}
	tr.update(layerA, 50*mb, 100*mb, false)
	tr.update(layerB, 0, 100*mb, false)
	if got := tr.overall(); got != 25 {
		t.Errorf("50MB/200MB 应换算成 25%%，实际 %d", got)
	}
	done, total, layers, doneN := tr.snapshot()
	if done != 50*mb || total != 200*mb || layers != 2 || doneN != 0 {
		t.Errorf("snapshot = %d/%d 层 %d/%d", done, total, doneN, layers)
	}
	// 全部完成时直接给 100：有些层不给 total，只按字节算永远差一点
	tr.update(layerA, 100*mb, 100*mb, true)
	tr.update(layerB, 100*mb, 100*mb, true)
	if got := tr.overall(); got != 100 {
		t.Errorf("全部完成后 overall = %d, want 100", got)
	}
	if _, _, _, doneN = tr.snapshot(); doneN != 2 {
		t.Errorf("已完成层数应为 2，实际 %d", doneN)
	}
}

// 完成前不能先到 100%：否则进度条会骗人
func TestLayerTrackerCapsBeforeDone(t *testing.T) {
	tr := newTracker()
	tr.update(layerA, 100*mb, 100*mb, false)
	tr.update(layerB, 100*mb, 100*mb, false)
	if got := tr.overall(); got != 99 {
		t.Errorf("还有层未标记完成时 overall 应为 99，实际 %d", got)
	}
}

// 还有一层在下载、但 daemon 没给 total：层数也要算上，否则总进度提前到 100%，
// 任务就看起来“卡在 100% 的进行中”（前端截图里的那一幕）
func TestLayerTrackerCountsLayersWithoutTotal(t *testing.T) {
	tr := newTracker()
	tr.update(layerA, 5*mb, 5*mb, true)
	tr.update(layerB, 0, 0, false) // 下载中，没有 total
	if got := tr.overall(); got != 99 {
		t.Errorf("有未报大小的层在跑时 overall 应封顶 99，实际 %d", got)
	}
	if _, _, layers, doneN := tr.snapshot(); layers != 2 || doneN != 1 {
		t.Errorf("层数应为 1/2，实际 %d/%d", doneN, layers)
	}
	// “Already exists” 的层也没 total，但它确实是一层
	tr.update(layerC, 0, 0, true)
	if _, _, layers, doneN := tr.snapshot(); layers != 3 || doneN != 2 {
		t.Errorf("层数应为 2/3，实际 %d/%d", doneN, layers)
	}
}

// 完成层数不能超过总层数：界面上会出现“分层 3/1”这种怪数字
func TestLayerTrackerDoneNeverExceedsLayers(t *testing.T) {
	// 故意用没有 seen 的旧构造，验证回退分支也不会错位
	tr := &layerTracker{cur: map[string]int64{}, tot: map[string]int64{}, done: map[string]bool{}}
	tr.update(layerA, 10*mb, 10*mb, true)
	tr.update(layerB, 0, 0, true)
	tr.update(layerC, 0, 0, true)
	_, _, layers, doneN := tr.snapshot()
	if layers != 1 || doneN != 1 {
		t.Errorf("层数与完成数错位: %d/%d", doneN, layers)
	}
}

// “Pulling from library/redis” 带的 ID 是 tag（latest），不能当成一层，
// 否则日志里会出现“层 3/8”这种比真实层数多一行的数字
func TestLayerTrackerIgnoresTagAsLayer(t *testing.T) {
	tr := newTracker()
	tr.update("latest", 0, 0, false)
	tr.update(layerA, 0, 0, false)
	if _, _, layers, doneN := tr.snapshot(); layers != 1 || doneN != 0 {
		t.Errorf("tag 不该算一层：完成/总数 %d/%d", doneN, layers)
	}
}

// daemon 只报了几十字节元数据（加速站常见）时，总进度改按已完成层数估算，
// 不能拿 32/32 字节算出“刚开始就 99%”
func TestLayerTrackerOverallByLayersWhenBytesUntrusted(t *testing.T) {
	tr := newTracker()
	tr.update(layerA, 32, 32, true)
	tr.update(layerB, 0, 0, false)
	tr.update(layerC, 0, 0, false)
	if got := tr.overall(); got != 33 {
		t.Errorf("字节不可信时应按 1/3 层估算，实际 %d", got)
	}
}

// 停滞判定看「进度指纹」：daemon 反复重试同一层会重发一样的消息，不能算续命
func TestPullWatcherStall(t *testing.T) {
	w := &pullWatcher{at: time.Now()}
	w.mark("a|100", "0bcc39d79065 Downloading")
	if d, _ := w.idle(); d > time.Second {
		t.Fatalf("刚记一笔时 idle 应接近 0，实际 %v", d)
	}
	time.Sleep(20 * time.Millisecond)
	// 同样的状态与字节数：不续命
	w.mark("a|100", "0bcc39d79065 Downloading")
	if d, _ := w.idle(); d < 15*time.Millisecond {
		t.Errorf("重复消息不应刷新进度时间，实际 idle=%v", d)
	}
	if !w.takeWarn() {
		t.Error("首次超时应提醒")
	}
	if w.takeWarn() {
		t.Error("同一段停滞只提醒一次")
	}
	// 真的在下载：字节数涨了就重新计时，并能再次提醒
	w.mark("a|200", "0bcc39d79065 Downloading")
	if d, _ := w.idle(); d > time.Second {
		t.Errorf("进度变化后应重新计时，实际 idle=%v", d)
	}
	if !w.takeWarn() {
		t.Error("进度变化后应能再次提醒")
	}
}

func TestPctOf(t *testing.T) {
	cases := []struct {
		done, total int64
		want        int
	}{
		{0, 0, 0}, {5, 10, 50}, {11, 10, 100}, {-1, 10, 0}, {1, 3, 33},
	}
	for _, c := range cases {
		if got := pctOf(c.done, c.total); got != c.want {
			t.Errorf("pctOf(%d,%d) = %d, want %d", c.done, c.total, got, c.want)
		}
	}
}

// 构建进度靠 "Step 3/12" 推算，解析错了就一直是 0%
func TestParseStep(t *testing.T) {
	cases := []struct {
		line   string
		a, b   int
		expect bool
	}{
		{"Step 3/12 : RUN go build", 3, 12, true},
		{"Step 1/1 : FROM scratch", 1, 1, true},
		{"Step 1/12", 1, 12, true},
		{"Pulling fs layer", 0, 0, false},
		{"Step a/b : X", 0, 0, false},
	}
	for _, c := range cases {
		a, b, ok := parseStep(c.line)
		if ok != c.expect || (ok && (a != c.a || b != c.b)) {
			t.Errorf("parseStep(%q) = %d,%d,%v want %d,%d,%v", c.line, a, b, ok, c.a, c.b, c.expect)
		}
	}
}

// 仓库地址规整：补协议、去 /v2、Docker Hub 多种写法归一
func TestNormalizeRegistryURL(t *testing.T) {
	cases := map[string]string{
		"harbor.corp.local":             "https://harbor.corp.local",
		"harbor.corp.local/":            "https://harbor.corp.local",
		"https://harbor.corp.local/v2":  "https://harbor.corp.local",
		"https://harbor.corp.local/v2/": "https://harbor.corp.local",
		"http://10.0.0.5:5000/":         "http://10.0.0.5:5000",
		"docker.io":                     hubURL,
		"https://index.docker.io/v1/":   hubURL,
		"https://registry-1.docker.io":  hubURL,
		"":                              "",
	}
	for in, want := range cases {
		if got := normalizeRegistryURL(in); got != want {
			t.Errorf("normalizeRegistryURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSameRegistry(t *testing.T) {
	if !sameRegistry("https://harbor.corp.local", "harbor.corp.local") {
		t.Error("同一地址应判为同一仓库")
	}
	if !sameRegistry("https://index.docker.io/v1/", "docker.io") {
		t.Error("Docker Hub 的规范地址与 docker.io 应视为同一仓库")
	}
	if sameRegistry("https://harbor.corp.local", "harbor.corp.local:5000") {
		t.Error("端口不同不能算同一仓库")
	}
	if sameRegistry("https://harbor.corp.local", "") {
		t.Error("空主机名不应匹配任何仓库")
	}
}

func TestIsHub(t *testing.T) {
	if !isHub(Registry{URL: "https://index.docker.io/v1/"}) {
		t.Error("Docker Hub 地址应判为 Hub")
	}
	if isHub(Registry{URL: "https://harbor.corp.local"}) {
		t.Error("私有仓库不该判为 Hub")
	}
}

// Bearer challenge 解析错了就换不到 token，私有仓库列表会一直 401
func TestParseChallenge(t *testing.T) {
	ch := `Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/mysql:pull"`
	realm, service, scope := parseChallenge(ch)
	if realm != "https://auth.docker.io/token" {
		t.Errorf("realm = %q", realm)
	}
	if service != "registry.docker.io" {
		t.Errorf("service = %q", service)
	}
	if scope != "repository:library/mysql:pull" {
		t.Errorf("scope = %q", scope)
	}
	if r, _, _ := parseChallenge(`Basic realm="x"`); r != "" {
		t.Errorf("Basic challenge 不该被当成 Bearer: %q", r)
	}
}

func TestToStrings(t *testing.T) {
	got := toStrings([]any{"a", "b", 3})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("toStrings = %v", got)
	}
	if toStrings("not-array") != nil {
		t.Error("非数组应返回 nil")
	}
}

// 用户点取消必须被翻译成“已取消”，而不是一条像故障的失败
func TestCancelErrWording(t *testing.T) {
	if e := cancelErr(nil, errString("context canceled")); e == nil || !strings.Contains(e.Error(), "已取消") {
		t.Errorf("context canceled 应翻成已取消: %v", e)
	}
	if e := cancelErr(nil, nil); e != nil {
		t.Errorf("nil 应原样返回 nil: %v", e)
	}
}

type errString string

func (e errString) Error() string { return string(e) }
