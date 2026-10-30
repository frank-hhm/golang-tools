package downloader

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// ============================================================
// 下载引擎测试：httptest 起 Range 服务器，逐字节校验多分块 / 单流退化 /
// 续传 / 限速 / 重试 / probe / store。downloadFile 是纯函数式入口，
// 直接喂 dlParams 即可，无需起整个 Manager。
// ============================================================

// genData 生成确定性内容（按序号取模），便于逐字节比对。
func genData(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

// rangeRecorder 支持 Range 的假服务器，并记录收到的每个区间请求。
type rangeRecorder struct {
	data         []byte
	acceptRanges bool // false：忽略 Range 一律回 200 全量
	force200     bool // true：即便带 Range 也回 200（模拟不守 Range）

	mu     sync.Mutex
	ranges []string // 收到的 Range 头原文（GET）
	gets   int
	failN  int // 前 N 次 GET 回 500（测重试）
}

func (s *rangeRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodHead {
		if s.acceptRanges {
			w.Header().Set("Accept-Ranges", "bytes")
		}
		w.Header().Set("Content-Length", strconv.Itoa(len(s.data)))
		return
	}
	s.mu.Lock()
	s.gets++
	n := s.gets
	s.mu.Unlock()
	if s.failN > 0 && n <= s.failN {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	rng := r.Header.Get("Range")
	s.mu.Lock()
	if rng != "" {
		s.ranges = append(s.ranges, rng)
	}
	s.mu.Unlock()

	if rng == "" || s.force200 || !s.acceptRanges {
		w.Header().Set("Content-Length", strconv.Itoa(len(s.data)))
		if s.acceptRanges {
			w.Header().Set("Accept-Ranges", "bytes")
		} else {
			w.Header().Set("Accept-Ranges", "none")
		}
		_, _ = w.Write(s.data)
		return
	}
	start, end, ok := parseRange(rng, int64(len(s.data)))
	if !ok {
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, len(s.data)))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Content-Length", strconv.FormatInt(end-start+1, 10))
	w.WriteHeader(http.StatusPartialContent)
	_, _ = w.Write(s.data[start : end+1])
}

func parseRange(rng string, size int64) (int64, int64, bool) {
	if !strings.HasPrefix(rng, "bytes=") {
		return 0, 0, false
	}
	spec := strings.SplitN(rng[len("bytes="):], "-", 2)
	if len(spec) != 2 {
		return 0, 0, false
	}
	start, _ := strconv.ParseInt(strings.TrimSpace(spec[0]), 10, 64)
	var end int64
	if strings.TrimSpace(spec[1]) == "" {
		end = size - 1
	} else {
		end, _ = strconv.ParseInt(strings.TrimSpace(spec[1]), 10, 64)
	}
	if end > size-1 {
		end = size - 1 // 真实服务器把超界区间截到文件尾，而非报 416
	}
	if start < 0 || start >= size || start > end {
		return 0, 0, false
	}
	return start, end, true
}

func newRangeServer(t *testing.T, data []byte, acceptRanges bool) *httptest.Server {
	srv := httptest.NewServer(&rangeRecorder{data: data, acceptRanges: acceptRanges})
	t.Cleanup(srv.Close)
	return srv
}

func baseParams(srv *httptest.Server, savePath string) dlParams {
	return dlParams{
		ctx:      context.Background(),
		client:   srv.Client(),
		url:      srv.URL,
		savePath: savePath,
		conns:    1,
		minSeg:   defaultMinSegmentedSize,
		maxRetry: 1,
	}
}

/* ---------------- 多分块逐字节一致 ---------------- */

func TestDownloadChunked_ByteExact(t *testing.T) {
	data := genData(100 * 1024) // 100KB
	srv := newRangeServer(t, data, true)
	save := filepath.Join(t.TempDir(), "big.bin")

	var mu sync.Mutex
	var got []Part
	p := baseParams(srv, save)
	p.conns = 4
	p.minSeg = 1024 // 远小于 size → 触发分块
	p.onProgress = func(size, dl int64, parts []Part) {
		mu.Lock()
		got = parts
		mu.Unlock()
	}
	if err := downloadFile(p); err != nil {
		t.Fatalf("分块下载失败: %v", err)
	}
	gotBytes, err := os.ReadFile(save)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(gotBytes, data) {
		t.Fatalf("逐字节不一致：got %d want %d", len(gotBytes), len(data))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 4 {
		t.Fatalf("分块数应为 4，实得 %d", len(got))
	}
	for _, pt := range got {
		if pt.Done != pt.End-pt.Start+1 {
			t.Fatalf("块 %d 未下满: done=%d 区间=%d", pt.Index, pt.Done, pt.End-pt.Start+1)
		}
	}
}

/* ---------------- 服务端不守 Range → 退化单流 ---------------- */

func TestDownload_FallbackSingle(t *testing.T) {
	data := genData(64 * 1024)
	// acceptRanges=true 但 force200=true：要了区间却回 200，逼出退化路径。
	srv := httptest.NewServer(&rangeRecorder{data: data, acceptRanges: true, force200: true})
	t.Cleanup(srv.Close)
	save := filepath.Join(t.TempDir(), "f.bin")

	p := baseParams(srv, save)
	p.conns = 4
	p.minSeg = 1024
	if err := downloadFile(p); err != nil {
		t.Fatalf("退化单流应成功: %v", err)
	}
	got, _ := os.ReadFile(save)
	if !bytes.Equal(got, data) {
		t.Fatal("退化单流内容不一致")
	}
}

/* ---------------- Accept-Ranges: none → .part 单流路径 ---------------- */

func TestDownload_AcceptRangesNone(t *testing.T) {
	data := genData(48 * 1024)
	srv := newRangeServer(t, data, false) // 回 Accept-Ranges: none
	save := filepath.Join(t.TempDir(), "n.bin")

	p := baseParams(srv, save)
	p.conns = 4
	p.minSeg = 1024
	if err := downloadFile(p); err != nil {
		t.Fatalf("单流下载失败: %v", err)
	}
	got, _ := os.ReadFile(save)
	if !bytes.Equal(got, data) {
		t.Fatal("内容不一致")
	}
	// .part 与 meta 应已清理
	if _, err := os.Stat(save + partSuffix); !os.IsNotExist(err) {
		t.Fatal(".part 未清理")
	}
	if _, err := os.Stat(save + metaSuffix); !os.IsNotExist(err) {
		t.Fatal("meta 未清理")
	}
}

/* ---------------- 续传：只补未完成区间 ---------------- */

func TestDownload_ResumeSkipsCompleted(t *testing.T) {
	data := genData(100 * 1024)
	rec := &rangeRecorder{data: data, acceptRanges: true}
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	save := filepath.Join(dir, "r.bin")

	const conns = 4
	parts := planParts(int64(len(data)), conns)
	// 预置：前两块完整下完，第三块半截，第四块没开始。
	f, err := os.OpenFile(save, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Truncate(int64(len(data)))
	_, _ = f.WriteAt(data[:parts[0].length()], parts[0].start)
	_, _ = f.WriteAt(data[parts[1].start:parts[1].start+parts[1].length()], parts[1].start)
	half := parts[2].length() / 2
	_, _ = f.WriteAt(data[parts[2].start:parts[2].start+half], parts[2].start)
	f.Close()

	m := &meta{URL: srv.URL, Size: int64(len(data)), Conns: conns, Chunked: true,
		Parts: map[int]int64{0: parts[0].length(), 1: parts[1].length(), 2: half, 3: 0}}
	saveMeta(save, m)

	p := baseParams(srv, save)
	p.conns = conns
	p.minSeg = 1024
	if err := downloadFile(p); err != nil {
		t.Fatalf("续传失败: %v", err)
	}
	got, _ := os.ReadFile(save)
	if !bytes.Equal(got, data) {
		t.Fatal("续传后内容不一致")
	}
	// 已完成的块（index 0/1）不该再被请求：其整块区间不应出现在收到的 Range 里。
	rec.mu.Lock()
	defer rec.mu.Unlock()
	full0 := fmt.Sprintf("bytes=%d-%d", parts[0].start, parts[0].end)
	full1 := fmt.Sprintf("bytes=%d-%d", parts[1].start, parts[1].end)
	for _, r := range rec.ranges {
		if r == full0 || r == full1 {
			t.Fatalf("已完成块被重复下载: %s", r)
		}
	}
	// 块 3 从未下过，必然被请求（起点=parts[3].start）。
	want3 := fmt.Sprintf("bytes=%d-%d", parts[3].start, parts[3].end)
	found := false
	for _, r := range rec.ranges {
		if r == want3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("未下载的块 3 未被请求，收到的区间: %v", rec.ranges)
	}
}

/* ---------------- 限速：吞吐不超上限（放宽时间窗） ---------------- */

func TestDownload_SpeedLimit(t *testing.T) {
	data := genData(300 * 1024)
	srv := newRangeServer(t, data, true)
	save := filepath.Join(t.TempDir(), "s.bin")

	tb := newTokenBucket(context.Background(), 128*1024) // 128KB/s
	p := baseParams(srv, save)
	p.conns = 1
	p.tb = tb
	start := time.Now()
	if err := downloadFile(p); err != nil {
		t.Fatalf("限速下载失败: %v", err)
	}
	elapsed := time.Since(start)
	// 首 128KB 由初始令牌垫付，余 ~172KB 需按 128KB/s 补 → 至少 ~1.2s；断言留足容差。
	if min := 900 * time.Millisecond; elapsed < min {
		t.Fatalf("限速未生效，耗时 %v < %v", elapsed, min)
	}
	got, _ := os.ReadFile(save)
	if !bytes.Equal(got, data) {
		t.Fatal("限速下内容不一致")
	}
}

/* ---------------- 重试：一次 500 后成功 ---------------- */

func TestDownload_RetryAfter500(t *testing.T) {
	data := genData(16 * 1024)
	rec := &rangeRecorder{data: data, acceptRanges: true, failN: 1} // 第一次 GET 回 500
	srv := httptest.NewServer(rec)
	t.Cleanup(srv.Close)
	save := filepath.Join(t.TempDir(), "retry.bin")

	p := baseParams(srv, save)
	p.conns = 1
	p.maxRetry = 3
	if err := downloadFile(p); err != nil {
		t.Fatalf("应重试成功: %v", err)
	}
	got, _ := os.ReadFile(save)
	if !bytes.Equal(got, data) {
		t.Fatal("内容不一致")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if rec.gets < 2 {
		t.Fatalf("应至少发生一次重试，gets=%d", rec.gets)
	}
}

/* ---------------- 致命 404：不重试 ---------------- */

func TestDownload_Fatal404NoRetry(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	save := filepath.Join(t.TempDir(), "404.bin")

	p := baseParams(srv, save)
	p.conns = 1
	p.maxRetry = 5
	err := downloadFile(p)
	if err == nil {
		t.Fatal("404 应返回错误")
	}
	if !isFatal(err) {
		t.Fatalf("404 应判致命不重试，err=%v", err)
	}
}

/* ---------------- probe / ProbeSources ---------------- */

func TestProbeSources(t *testing.T) {
	data := genData(32 * 1024)
	srv := newRangeServer(t, data, true)
	stats := ProbeSources([]string{srv.URL, srv.URL + "/missing"})
	if len(stats) != 2 {
		t.Fatalf("应返回 2 条，得 %d", len(stats))
	}
	if !stats[0].OK {
		t.Fatalf("首个源应 OK，err=%q", stats[0].Err)
	}
	if stats[0].Size != int64(len(data)) {
		t.Fatalf("长度应为 %d，得 %d", len(data), stats[0].Size)
	}
}

func TestProbeProxies_BadProxy(t *testing.T) {
	// 指向一个必定连不上的代理：结果应 OK=false 且带错误说明，不 panic。
	stats := ProbeProxies([]string{"http://127.0.0.1:1"}, "http://example.invalid/")
	if len(stats) != 1 || stats[0].OK {
		t.Fatalf("坏代理应判不可用: %+v", stats)
	}
	if stats[0].Err == "" {
		t.Fatal("坏代理应有错误说明")
	}
}

/* ---------------- store：running→paused + 分类 ---------------- */

func TestStore_NormalizeRunning(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir)
	in := []*Task{
		{ID: "a", Name: "a.zip", Status: StatusRunning, Stage: "downloading"},
		{ID: "b", Name: "b.mp3", Status: StatusQueued},
		{ID: "c", Name: "c.txt", Status: StatusDone},
	}
	if err := s.Save(in); err != nil {
		t.Fatal(err)
	}
	out, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Status != StatusPaused || out[0].Stage != "stalled" {
		t.Fatalf("running 应归一为 paused/stalled，得 %s/%s", out[0].Status, out[0].Stage)
	}
	if out[1].Status != StatusPaused {
		t.Fatalf("queued 应归一为 paused，得 %s", out[1].Status)
	}
	if out[2].Status != StatusDone {
		t.Fatalf("done 不应被改动，得 %s", out[2].Status)
	}
}

func TestCategoryOf(t *testing.T) {
	cases := map[string]string{
		"setup.exe": "程序", "song.mp3": "音乐", "photo.PNG": "图片",
		"report.pdf": "文档", "movie.mkv": "视频", "a.zip": "压缩", "unknown.xyz": CategoryOther,
	}
	for name, want := range cases {
		if got := categoryOf(name); got != want {
			t.Fatalf("categoryOf(%q)=%q want %q", name, got, want)
		}
	}
}

/* ---------------- 端到端：Manager 调度 + 落盘 + 事件 + 持久化 ---------------- */

func TestManager_EndToEnd(t *testing.T) {
	data := genData(64 * 1024)
	srv := newRangeServer(t, data, true)
	dir := t.TempDir()

	m := New(Options{MaxRunningTasks: 2, DefaultConnections: 1}, dir, dir)
	defer m.Shutdown()

	var mu sync.Mutex
	var last Task
	m.SetEmitter(func(name string, d ...any) {
		if name == EventProgress && len(d) == 1 {
			if ts, ok := d[0].(Task); ok {
				mu.Lock()
				last = ts
				mu.Unlock()
			}
		}
	})

	task, err := m.Add(srv.URL+"/movie.mkv", AddOpts{Name: "movie.mkv", Connections: 1, AutoStart: true})
	if err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(15 * time.Second)
	var status string
	for time.Now().Before(deadline) {
		mu.Lock()
		status = last.Status
		mu.Unlock()
		if status == "" {
			for _, x := range m.List() {
				if x.ID == task.ID {
					status = x.Status
				}
			}
		}
		if status == StatusDone || status == StatusError {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if status != StatusDone {
		t.Fatalf("任务应完成，实际 %s", status)
	}

	savePath := filepath.Join(dir, "movie.mkv")
	got, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("读下载结果: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("端到端内容不一致")
	}
	if _, err := os.Stat(filepath.Join(dir, "tasks.json")); err != nil {
		t.Fatalf("任务清单未持久化: %v", err)
	}
	// category 应归到「视频」
	for _, x := range m.List() {
		if x.ID == task.ID && x.Category != "视频" {
			t.Fatalf("分类应为视频，得 %q", x.Category)
		}
	}
	// 全局统计：至少完成 1
	if s := m.GlobalStats(); s.Done < 1 {
		t.Fatalf("GlobalStats.done 应>=1，得 %+v", s)
	}
	_ = io.Discard
}

/* ---------------- Fetch：阻塞式下载（工具侧安装链路复用） ---------------- */

func TestFetch_DownloadAndWait(t *testing.T) {
	data := genData(64 * 1024)
	srv := newRangeServer(t, data, true)
	dir := t.TempDir()

	m := New(Options{DefaultConnections: 1}, dir, dir)
	defer m.Shutdown()

	var mu sync.Mutex
	var pcts []int
	savePath, id, err := m.Fetch(context.Background(), srv.URL+"/mysql-8.0.46.zip", FetchOpts{
		Name: "mysql-8.0.46.zip", SaveDir: dir, Category: "安装", Connections: 1,
	}, func(pct int, _, _ int64, _ float64) {
		mu.Lock()
		pcts = append(pcts, pct)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("Fetch 应成功: %v", err)
	}
	if id == "" {
		t.Fatal("Fetch 应回传任务 id")
	}
	got, err := os.ReadFile(savePath)
	if err != nil {
		t.Fatalf("读下载结果: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("Fetch 内容不一致")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(pcts) == 0 || pcts[len(pcts)-1] != 100 {
		t.Fatalf("onProgress 末次应为 100，得 %v", pcts)
	}
	// 归类到「安装」，且任务确实进了引擎清单（下载页可见）
	for _, x := range m.List() {
		if x.ID == id && x.Category != "安装" {
			t.Fatalf("分类应为安装，得 %q", x.Category)
		}
	}
}

func TestFetch_CtxCancel(t *testing.T) {
	data := genData(64 * 1024)
	srv := newRangeServer(t, data, true)
	dir := t.TempDir()

	m := New(Options{DefaultConnections: 1}, dir, dir)
	defer m.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消：Fetch 应停掉任务并回错
	_, id, err := m.Fetch(ctx, srv.URL+"/x.zip", FetchOpts{Name: "x.zip", SaveDir: dir, Connections: 1}, nil)
	if err == nil {
		t.Fatal("取消后应返回 error")
	}
	// 取消→runner 退出→finish 落 canceled 是异步的，轮询等终态
	if got := waitForStatus(m, id, StatusCanceled, 5*time.Second); got != StatusCanceled {
		t.Fatalf("取消后任务应 canceled，实际 %s", got)
	}
}

/* ---------------- 队列额度让位：暂停/停止后排队任务应被拉起 ---------------- */

// waitForStatus 轮询等任务进入期望状态（或超时回退当前状态）。
func waitForStatus(m *Manager, id, want string, timeout time.Duration) string {
	deadline := time.Now().Add(timeout)
	cur := ""
	for time.Now().Before(deadline) {
		if t, ok := m.Get(id); ok {
			cur = t.Status
			if cur == want {
				return cur
			}
		}
		time.Sleep(30 * time.Millisecond)
	}
	return cur
}

func TestManager_QueuedPromotion(t *testing.T) {
	data := genData(128 * 1024)
	srv := newRangeServer(t, data, true)
	dir := t.TempDir()

	// 只给 1 个并发额度：A 占着时 B 应排队；A 暂停后 B 应自动拉起跑完。
	m := New(Options{MaxRunningTasks: 1, DefaultConnections: 1}, dir, dir)
	defer m.Shutdown()

	a, err := m.Add(srv.URL+"/a.zip", AddOpts{Name: "a.zip", Connections: 1})
	if err != nil {
		t.Fatal(err)
	}
	// 先钉住 A 再开跑：128KB @ 8KB/s ≈ 16 秒，足够 B 排队/暂停联动；
	// 额度满时 AutoStart 的 Add 只会排到 running 让位为止，不依赖这里。
	_ = m.SetSpeedLimit(a.ID, 8*1024)
	if err := m.Start(a.ID); err != nil {
		t.Fatal(err)
	}
	if got := waitForStatus(m, a.ID, StatusRunning, 3*time.Second); got != StatusRunning {
		t.Fatalf("A 应先跑起来，实际 %s", got)
	}

	b, err := m.Add(srv.URL+"/b.zip", AddOpts{Name: "b.zip", Connections: 1, AutoStart: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := waitForStatus(m, b.ID, StatusQueued, 3*time.Second); got != StatusQueued {
		t.Fatalf("B 应排队，实际 %s", got)
	}

	// 用户对着「排队中」的 B 点开始：当前已排队，activate 不能吞掉这次点击（应顶到队首并给反馈）
	if err := m.Start(b.ID); err != nil {
		t.Fatalf("对排队任务 Start 不应报错: %v", err)
	}

	if err := m.Pause(a.ID); err != nil {
		t.Fatal(err)
	}
	if got := waitForStatus(m, a.ID, StatusPaused, 5*time.Second); got != StatusPaused {
		t.Fatalf("A 应暂停，实际 %s", got)
	}
	if got := waitForStatus(m, b.ID, StatusDone, 15*time.Second); got != StatusDone {
		t.Fatalf("A 让位后 B 应被调度拉起并跑完，实际 %s", got)
	}
}
