//go:build windows

package docker

// ============================================================
// 端点选择与改选的单测：不碰真管道，用可注入的 prober 假探，
// 覆盖「本进程先起来、Desktop 后起来」「引擎挪到另一条管道」
// 「DOCKER_HOST 显式指定不许自动改选」这几类真实故障。
// 这些用例都动包级全局状态，彼此不得并行（不要调 t.Parallel）。
// ============================================================

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// resetEndpointState 把端点状态清回「没探过」
func resetEndpointState() {
	endpMu.Lock()
	endpVal, endpDesc = endpoint{}, ""
	endpPinned, endpFromEnv = false, false
	endpSweptAt = time.Time{}
	endpMu.Unlock()
}

// useProber 注入假实探函数：answer 里显式列为 nil 的端点算「有应答」，
// 没列进去的一律拨不通（要用 comma-ok 区分，不然缺键的零值 error 会被当成应答）
func useProber(t *testing.T, answer map[string]error) *int {
	t.Helper()
	prev, prevTO := endpointProber, endpointProbeTO
	calls := new(int)
	endpointProber = func(ep endpoint) error {
		*calls++
		if err, ok := answer[ep.addr]; ok {
			return err
		}
		return fmt.Errorf("拨不通 %s", shortEndpoint(ep))
	}
	endpointProbeTO = 10 * time.Millisecond
	t.Cleanup(func() {
		endpointProber, endpointProbeTO = prev, prevTO
		resetEndpointState()
	})
	return calls
}

// clearDockerHost 临时摘掉 DOCKER_HOST（假引擎测试会把它设成 tcp 端点）
func clearDockerHost(t *testing.T) {
	t.Helper()
	prev, had := os.LookupEnv("DOCKER_HOST")
	_ = os.Unsetenv("DOCKER_HOST")
	resetEndpointState()
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("DOCKER_HOST", prev)
		}
		resetEndpointState()
	})
}

func TestEndpointSweepPicksFirstAnswering(t *testing.T) {
	clearDockerHost(t)
	calls := useProber(t, map[string]error{
		defaultPipes[1]: nil, // 第一条管道没人应答，引擎实际在第二条上
	})

	ep := resolveEndpoint()
	if ep.addr != defaultPipes[1] {
		t.Fatalf("应选到应答的 %s，实得 %s", defaultPipes[1], ep.addr)
	}
	if *calls != 2 {
		t.Errorf("前两个候选拨完就该命中，不该再试后面的，实得 %d 次", *calls)
	}
	if desc := endpointDesc(); !strings.Contains(desc, "dockerDesktopLinuxEngine") {
		t.Errorf("端点描述该给出真正用上的管道，实得 %q", desc)
	}

	// 钉住之后不再重复实探：面板每 5 秒探一次，不该每次都扫一遍候选
	before := *calls
	if resolveEndpoint().addr != defaultPipes[1] {
		t.Error("钉住的端点不该变")
	}
	if *calls != before {
		t.Errorf("已钉住时不该再探，实得多探 %d 次", *calls-before)
	}
}

func TestEndpointRepickAfterEngineMoves(t *testing.T) {
	clearDockerHost(t)
	calls := useProber(t, map[string]error{defaultPipes[0]: nil})
	if resolveEndpoint().addr != defaultPipes[0] {
		t.Fatal("第一轮该选第一条管道")
	}

	// Desktop 重启后引擎挪走了：只剩第二条应答
	endpointProber = func(ep endpoint) error {
		*calls++
		if ep.addr == defaultPipes[1] {
			return nil
		}
		return windows.ERROR_FILE_NOT_FOUND
	}
	invalidateEndpoint("模拟：拨不通")
	if got := resolveEndpoint(); got.addr != defaultPipes[1] {
		t.Fatalf("端点失效后该重扫到新管道，实得 %s", got.addr)
	}
	if !endpPinned {
		t.Error("重扫命中后应重新钉住")
	}
}

func TestEndpointSweepRateLimitedWhenNothingAnswers(t *testing.T) {
	clearDockerHost(t)
	calls := useProber(t, nil) // 谁都不应答

	ep := resolveEndpoint()
	if ep.addr != defaultPipes[0] {
		t.Errorf("全都不应答时按首选候选回话，实得 %s", ep.addr)
	}
	if endpPinned {
		t.Error("没探到真引擎不该钉住端点")
	}
	first := *calls
	if got := endpointDesc(); !strings.Contains(got, "都没应答") {
		t.Errorf("失败描述要如实说明扫过候选，实得 %q", got)
	}
	// 刚扫过：限流窗口内不重复扫（多个面板同时轮询）
	resolveEndpoint()
	if *calls != first {
		t.Errorf("限流窗口内不该重扫，实得多探 %d 次", *calls-first)
	}
	// 窗口过了：再来一轮就能接上新起的引擎（本进程先起、Desktop 后起的那类故障）
	endpMu.Lock()
	endpSweptAt = time.Time{}
	endpMu.Unlock()
	endpointProber = func(e endpoint) error {
		*calls++
		if e.addr == defaultPipes[2] {
			return nil
		}
		return fmt.Errorf("没应答")
	}
	if got := resolveEndpoint(); got.addr != defaultPipes[2] {
		t.Fatalf("引擎起来后下一轮该跟上，实得 %s", got.addr)
	}
}

func TestDockerHostIsStrictNeverFallsBack(t *testing.T) {
	prev, had := os.LookupEnv("DOCKER_HOST")
	_ = os.Setenv("DOCKER_HOST", "tcp://127.0.0.1:32375")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("DOCKER_HOST", prev)
		} else {
			_ = os.Unsetenv("DOCKER_HOST")
		}
		resetEndpointState()
	})
	calls := useProber(t, nil) // 连 prober 都说没应答，也不能偷偷改用管道

	ep := resolveEndpoint()
	if ep.network != "tcp" || !ep.fromEnv {
		t.Fatalf("DOCKER_HOST 已指定，端点必须是它：%+v", ep)
	}
	if *calls != 1 {
		t.Errorf("显式端点只探它自己，不该扫管道，实得 %d 次", *calls)
	}
	invalidateEndpoint("模拟：拨不通")
	if got := resolveEndpoint(); got.network != "tcp" || !got.fromEnv {
		t.Errorf("显式端点不许自动改选到别的端点，实得 %+v", got)
	}
}

func TestDialEndpointClassifiesMissingPipeAsDead(t *testing.T) {
	clearDockerHost(t)
	useProber(t, map[string]error{defaultPipes[0]: nil})
	if resolveEndpoint().addr != defaultPipes[0] {
		t.Fatal("先把一个端点钉住，再看拨别的管道会不会被判端点错")
	}

	c, err := dialEndpoint(context.Background(), endpoint{network: "pipe", addr: `\\.\pipe\Tools-no-such-pipe`, desc: "npipe://gone"})
	if err == nil {
		c.Close()
		t.Skip("本机意外存在这个管道")
	}
	if !endpointLooksDead(err) {
		t.Errorf("管道不存在应判端点错（才能触发重扫）：%v", err)
	}
	if !strings.Contains(err.Error(), "无法连接 Docker 引擎") {
		t.Errorf("文案仍要给可操作提示，实得 %q", err.Error())
	}
	// 重扫重试不该把「引擎在忙」误归为端点错：忙错误不是 endpointDeadError
	if endpointLooksDead(&endpointDeadError{msg: "假的"}) != true {
		t.Error("endpointDeadError 应该被认出来")
	}
	if endpointLooksDead(windows.ERROR_PIPE_BUSY) {
		t.Error("管道忙不算端点死")
	}
}

func TestUnavailableErrUnwrapsURLShell(t *testing.T) {
	clearDockerHost(t)
	useProber(t, map[string]error{defaultPipes[0]: nil})
	resolveEndpoint()

	dead := &endpointDeadError{msg: errDaemonUnavailable + "（端点: npipe://x）"}
	// http.Client 实发失败时长这样：Get "<url>": <真正的错>。剥壳后不能把类型弄丢，
	// 否则 GetEngineStatus 认不出「端点死了」，面板就再也扫不回新管道
	wrapped := &url.Error{Op: "Get", URL: "http://localhost/version", Err: dead}
	got := unavailableErr(wrapped)
	if !endpointLooksDead(got) {
		t.Errorf("剥掉 url.Error 后仍要认出端点死：%v", got)
	}
	if strings.HasPrefix(got.Error(), "Get ") {
		t.Errorf("面板上的错误不该带 url 外壳：%q", got.Error())
	}
}

func TestProbeStatusAndShortEndpoint(t *testing.T) {
	if got := probeStatus("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\n"); got != 200 {
		t.Errorf("状态码解析错，实得 %d", got)
	}
	if got := probeStatus("HTTP/1.1 500 Internal"); got != 500 {
		t.Errorf("5xx 解析错，实得 %d", got)
	}
	if got := probeStatus("garbage"); got != 0 {
		t.Errorf("读不出状态行应给 0，实得 %d", got)
	}
	if got := shortEndpoint(endpoint{network: "pipe", addr: `\\.\pipe\docker_engine`}); got != "docker_engine" {
		t.Errorf("shortEndpoint = %q", got)
	}
	if got := shortEndpoint(endpoint{network: "tcp", addr: "1.2.3.4:2375"}); got != "1.2.3.4:2375" {
		t.Errorf("tcp 端点应原样给出，实得 %q", got)
	}
}

func TestDialSaysEndpointWrongOnlyForDeadEndpoint(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{windows.ERROR_FILE_NOT_FOUND, true}, // 管道不存在
		{windows.Errno(3), true},             // ERROR_PATH_NOT_FOUND
		{windows.Errno(233), true},           // ERROR_PIPE_NOT_AVAILABLE
		{windows.Errno(234), true},           // ERROR_NO_DATA：Desktop 正在退出
		{syscall_ERRNO_PIPE_MISSING, true},   // 兼容旧常量
		{windows.Errno(231), false},          // ERROR_PIPE_BUSY：引擎在忙，不是端点错
		{windows.ERROR_BROKEN_PIPE, false},   // 读到一半断了：引擎还在，别丢端点
		{os.ErrDeadlineExceeded, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := dialSaysEndpointWrong(c.err); got != c.want {
			t.Errorf("dialSaysEndpointWrong(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestProbeEndpointLiveRejectsSilentEndpoint(t *testing.T) {
	prevTO := endpointProbeTO
	endpointProbeTO = 300 * time.Millisecond
	t.Cleanup(func() { endpointProbeTO = prevTO })

	// 应答正常的 tcp 端点
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/version" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, `{"ApiVersion":"1.47"}`)
	}))
	defer ok.Close()
	epOK := endpoint{network: "tcp", addr: strings.TrimPrefix(ok.URL, "http://"), desc: ok.URL}
	if err := probeEndpointLive(epOK); err != nil {
		t.Fatalf("正常端点不该判失败: %v", err)
	}

	// 接了连接但一个字都不发：必须算它不可用（这正是「管道在但没人给数据」的形态）。
	// 这里不能用 httptest：它的 Close 会等在建之请求，那个处理函数永不返回
	silent, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("本地监听起来不来了：%v", err)
	}
	defer silent.Close()
	go func() {
		for {
			c, err := silent.Accept()
			if err != nil {
				return
			}
			_ = c // 只接不回：跟残留管道的行为一致
		}
	}()
	start := time.Now()
	if perr := probeEndpointLive(endpoint{network: "tcp", addr: silent.Addr().String()}); perr == nil {
		t.Fatal("不应答的端点必须判失败")
	}
	if time.Since(start) > 2*time.Second {
		t.Errorf("实探该在超时上限内收住，实得 %v", time.Since(start))
	}
}
