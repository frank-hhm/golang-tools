//go:build windows

package docker

// ============================================================
// Docker Engine HTTP 客户端（Windows 命名管道直连，零第三方依赖）
//
// Docker Desktop 在 \\.\pipe\docker_engine 上提供标准 Engine API。Go 的
// net 包不支持命名管道，这里用 x/sys/windows 自建 net.Conn：
//   CreateFile(FILE_FLAG_OVERLAPPED) → ReadFile/WriteFile + 事件等待
//   → 需要中止时 CancelIoEx（SetDeadline 到期或 Close）
// 端点解析顺序：DOCKER_HOST 环境变量 → 默认管道列表（逐个实探）。
// 端点不是解析一次定终身：每轮重扫候选、拨不通就作废，详见「端点与客户端」一节。
// ============================================================

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go-tools/services/applog"

	"golang.org/x/sys/windows"
)

// defaultPipes 默认探测的命名管道候选（Docker Desktop / 原生 Engine 先后顺序）。
// 候选会每轮重扫：Desktop 重启、切虚拟化后端都可能把引擎挪到另一条管道上，
// 只认第一条会把面板永久钉在「引擎不可用」上。
var defaultPipes = []string{
	`\\.\pipe\docker_engine`,
	`\\.\pipe\dockerDesktopLinuxEngine`,
	`\\.\pipe\dockerDesktopWindowsEngine`,
	`\\.\pipe\dockerBackend`,
}

const errDaemonUnavailable = "无法连接 Docker 引擎（请确认 Docker Desktop 或 Docker Engine 正在运行）"

/* ============================ 命名管道 Conn ============================ */

type pipeConn struct {
	h windows.Handle

	mu      sync.Mutex
	closed  bool
	pending int

	deadline atomic.Value // time.Time；零值表示不限
	readEv   atomic.Pointer[windows.Overlapped]
	writeEv  atomic.Pointer[windows.Overlapped]
}

// dialPipe 打开命名管道（实例全忙时短暂重试）
func dialPipe(path string) (net.Conn, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		h, err := windows.CreateFile(p,
			windows.GENERIC_READ|windows.GENERIC_WRITE,
			0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
		if err == nil {
			c := &pipeConn{h: h}
			c.deadline.Store(time.Time{})
			return c, nil
		}
		lastErr = err
		if err == windows.ERROR_PIPE_BUSY {
			time.Sleep(120 * time.Millisecond)
			continue
		}
		break
	}
	return nil, lastErr
}

// pipeExistsOnce 一次 CreateFile 判定管道在不在：不重试、不读写。
// 这里不能靠 os.Stat：命名管道上没有可用实例时它会一直等，体检环节被拖住
// 面板就“跟踪不到状态”了；只取查询属性（SQOS + IDENTIFICATION）不会排队等实例。
// 忙/正在关闭/权限不足都说明有人在监听，只有「找不到这个名字」才算不存在。
func pipeExistsOnce(path string) bool {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, err := windows.CreateFile(p, 0, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
	if err == nil {
		_ = windows.CloseHandle(h)
		return true
	}
	switch err {
	case windows.ERROR_FILE_NOT_FOUND, windows.ERROR_PATH_NOT_FOUND,
		windows.Errno(233), syscall_ERRNO_PIPE_MISSING:
		return false
	}
	return true
}

func (c *pipeConn) curDeadline() time.Time {
	v, _ := c.deadline.Load().(time.Time)
	return v
}

// blockingOp 执行一次重叠 IO 并等待完成；到期或句柄关闭时以 CancelIoEx 中止
func (c *pipeConn) blockingOp(isRead bool, b []byte) (int, error) {
	if isRead && len(b) == 0 {
		return 0, nil
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	c.pending++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.pending--
		needClose := c.closed && c.pending == 0
		c.mu.Unlock()
		if needClose {
			_ = windows.CloseHandle(c.h)
		}
	}()

	ev, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(ev) }()

	// Overlapped 必须活到本次调用结束，故置于堆上并登记到 conn（Close 需 CancelIoEx 它）
	ov := &windows.Overlapped{HEvent: ev}
	if isRead {
		c.readEv.Store(ov)
		defer c.readEv.Store(nil)
	} else {
		c.writeEv.Store(ov)
		defer c.writeEv.Store(nil)
	}

	var done uint32
	if isRead {
		err = windows.ReadFile(c.h, b, &done, ov)
	} else {
		err = windows.WriteFile(c.h, b, &done, ov)
	}
	if err != nil && err != windows.ERROR_IO_PENDING {
		if isRead && err == windows.ERROR_BROKEN_PIPE {
			return 0, io.EOF // 对端关闭：按 EOF 语义交给 http 层
		}
		return 0, err
	}

	timeout := uint32(windows.INFINITE)
	var stopWatch func()
	if dl := c.curDeadline(); !dl.IsZero() {
		d := time.Until(dl)
		if d <= 0 {
			_ = windows.CancelIoEx(c.h, ov)
			_, _ = windows.WaitForSingleObject(ev, windows.INFINITE)
			return 0, os.ErrDeadlineExceeded
		}
		timeout = uint32(d.Milliseconds())
		if timeout < 1 {
			timeout = 1
		}
		stopWatch = startWatchdog(c.h, ov, time.Duration(timeout)*time.Millisecond)
		defer stopWatch()
	}

	wait, err := windows.WaitForSingleObject(ev, timeout)
	if err != nil {
		return 0, err
	}
	if wait == uint32(windows.WAIT_TIMEOUT) {
		_ = windows.CancelIoEx(c.h, ov)
		_, _ = windows.WaitForSingleObject(ev, windows.INFINITE)
		return 0, os.ErrDeadlineExceeded
	}
	done = 0
	if err := windows.GetOverlappedResult(c.h, ov, &done, true); err != nil {
		if isRead && err == windows.ERROR_BROKEN_PIPE {
			return 0, io.EOF
		}
		if err == windows.ERROR_OPERATION_ABORTED {
			return 0, os.ErrDeadlineExceeded
		}
		return 0, err
	}
	return int(done), nil
}

// startWatchdog 超时窗口后再兜底取消一次（防对端静默不应答）
func startWatchdog(h windows.Handle, ov *windows.Overlapped, after time.Duration) func() {
	timer := time.AfterFunc(after+500*time.Millisecond, func() {
		_ = windows.CancelIoEx(h, ov)
	})
	return func() { timer.Stop() }
}

func (c *pipeConn) Read(b []byte) (int, error)  { return c.blockingOp(true, b) }
func (c *pipeConn) Write(b []byte) (int, error) { return c.blockingOp(false, b) }

func (c *pipeConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	hasPending := c.pending > 0
	ovR, ovW := c.readEv.Load(), c.writeEv.Load()
	c.mu.Unlock()

	_ = windows.CancelIoEx(c.h, nil)
	if ovR != nil {
		_ = windows.CancelIoEx(c.h, ovR)
	}
	if ovW != nil {
		_ = windows.CancelIoEx(c.h, ovW)
	}
	if hasPending {
		return nil // 由最后一笔在途 IO 结束时关闭句柄
	}
	return windows.CloseHandle(c.h)
}

func (c *pipeConn) setDeadline(t time.Time) error {
	c.deadline.Store(t)
	c.mu.Lock()
	hasPending := c.pending > 0
	c.mu.Unlock()
	// 只有「设了新期限且确实有在途 IO」才取消让它按新期限重试。
	// 清空期限（time.Time{}，http 层读完响应会这么做）绝不能取消：
	// 那会把同一条 keep-alive 连接上正在等待下一次响应的读打断
	if !t.IsZero() && hasPending {
		_ = windows.CancelIoEx(c.h, nil)
	}
	return nil
}

func (c *pipeConn) SetDeadline(t time.Time) error      { return c.setDeadline(t) }
func (c *pipeConn) SetReadDeadline(t time.Time) error  { return c.setDeadline(t) }
func (c *pipeConn) SetWriteDeadline(t time.Time) error { return c.setDeadline(t) }

func (c *pipeConn) LocalAddr() net.Addr  { return pipeAddr("pipe") }
func (c *pipeConn) RemoteAddr() net.Addr { return pipeAddr("docker_engine") }
func (c *pipeConn) String() string       { return "pipeconn" }

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }

/* ============================ 端点与客户端 ============================ */

type endpoint struct {
	network string // "pipe" | "tcp"
	addr    string // 管道路径 或 host:port
	desc    string // 展示用
	fromEnv bool   // 来自 DOCKER_HOST：用户显式指定，永不自动改选到别的端点
}

// 端点选错是「引擎明明在跑、面板一直说不可用」的头号来源：
//   - 本进程比 Docker Desktop 先起（开机自启、或刚从面板拉起它），第一探测全部拨空；
//   - Desktop 重启/切后端后，引擎换到 dockerDesktopLinuxEngine，而旧管道残留但不再应答；
//   - daemon 退出时会留下「存在但没人接」的管道，只看「管道在不在」分不出来。
//
// 所以端点只作为一个可作废的快照：拨不通就作废，下一轮（或当轮）重扫候选。
var (
	endpMu      sync.Mutex
	endpVal     endpoint  // 当前生效端点（拨号与错误提示都以它为准）
	endpDesc    string    // 展示用描述，与 endpVal.desc 同步（失败时附带扫过哪些候选）
	endpPinned  bool      // true：已实探确认（或由 DOCKER_HOST 指定），不再重扫
	endpFromEnv bool      // true：当前端点来自 DOCKER_HOST
	endpSweptAt time.Time // 上次全量重扫时间（限流：多页面轮询不要各扫一遍）
)

// endpointProbeTO 单个候选的实探上限：拨通了不应答也算它不行
// 用 var 而不是 const，单测可以把它改短
var endpointProbeTO = 2500 * time.Millisecond

// endpointSweepGap 全量重扫的最小间隔：面板每 5 秒探一次，背对背重扫会众扣拨号开销
const endpointSweepGap = 2 * time.Second

// endpointProber 实探一个候选能不能用（单测换成假引擎）
var endpointProber = probeEndpointLive

// resolveEndpoint 取当前生效端点；未钉住时按限流做一次全量重扫
func resolveEndpoint() endpoint {
	endpMu.Lock()
	defer endpMu.Unlock()
	if endpPinned {
		return endpVal
	}
	if time.Since(endpSweptAt) < endpointSweepGap {
		return endpVal // 刚扫过且没扫到：先按首选候选回话，下一轮自动再扫
	}
	return sweepLocked()
}

// sweepLocked 逐个候选实探，命中即钉住；必须在持锁时调用
func sweepLocked() endpoint {
	endpSweptAt = time.Now()
	cands := endpointCandidates()
	tried := make([]string, 0, len(cands))
	for i, ep := range cands {
		if err := endpointProber(ep); err == nil {
			setEndpointLocked(ep, true)
			applog.Infof("docker", "引擎端点定为 %s（第 %d/%d 个候选应答）", ep.desc, i+1, len(cands))
			return ep
		}
		tried = append(tried, shortEndpoint(ep))
	}
	// 一个都不应答：仍按首选候选回话，不钉住；引擎一起来下一轮就能跟上
	fb := cands[0]
	setEndpointLocked(fb, false)
	endpDesc = fb.desc + "（试过的候选都没应答：" + strings.Join(tried, "、") + "）"
	return fb
}

// setEndpointLocked 落定当前端点；换端点时上一个引擎的协商结果与读缓存都不能再用
func setEndpointLocked(ep endpoint, pinned bool) {
	if pinned && (endpVal.addr != ep.addr || endpVal.network != ep.network) {
		resetEngineCaches()
	}
	endpVal, endpPinned, endpFromEnv = ep, pinned, ep.fromEnv
	endpDesc = ep.desc
}

// invalidateEndpoint 作废当前端点，下一次探测重扫候选。
// 只在「这个端点确实拨不通 / 引擎不接话」时调；管道忙、读到一半断属于引擎在忙，
// 交给闸门与熔断处理，不要把好好连着的端点丢掉。
func invalidateEndpoint(why string) {
	endpMu.Lock()
	if !endpPinned || endpFromEnv {
		endpMu.Unlock()
		return
	}
	dead := endpVal.addr
	endpPinned = false
	endpSweptAt = time.Time{} // 允许立刻重扫（并发重扫由 endpointSweepGap 在 sweepLocked 里卡住）
	endpMu.Unlock()
	resetEngineCaches()
	applog.Warnf("docker", "端点 %s 失效：%s；下一次探测重扫候选管道", dead, oneLine(truncate(why, 200)))
}

// endpointCandidates 本轮要试的候选：DOCKER_HOST 只认它自己，不回落管道
func endpointCandidates() []endpoint {
	if h := strings.TrimSpace(os.Getenv("DOCKER_HOST")); h != "" {
		if ep, err := parseDOCKERHOST(h); err == nil {
			ep.fromEnv = true
			return []endpoint{ep}
		}
		applog.Warnf("docker", "DOCKER_HOST=%s 解析不了，改按默认管道候选探测", h)
	}
	out := make([]endpoint, 0, len(defaultPipes))
	for _, p := range defaultPipes {
		out = append(out, endpoint{network: "pipe", addr: p, desc: "npipe://" + p})
	}
	return out
}

// probeEndpointLive 实探：拨通且 /version 真给出应答才算它活着。
// 只看「管道存在」不够——Desktop 退出/重启时会留下不应答的管道，
// 这正是面板跟不到状态的原因，所以必须发一次真请求。
func probeEndpointLive(ep endpoint) error {
	var (
		c   net.Conn
		err error
	)
	if ep.network == "tcp" {
		d := net.Dialer{Timeout: endpointProbeTO}
		c, err = d.Dial("tcp", ep.addr)
	} else {
		c, err = dialPipe(ep.addr)
	}
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(endpointProbeTO))
	// 不带版本前缀：/version 是所有 Engine API 都收的免协商入口，避开 prefix() 里自回调
	req := "GET /version HTTP/1.1\r\nHost: localhost\r\nAccept: application/json\r\nConnection: close\r\n\r\n"
	if _, err := c.Write([]byte(req)); err != nil {
		return err
	}
	buf := make([]byte, 128)
	n, err := io.ReadAtLeast(c, buf, 12) // 至少读到 "HTTP/1.1 200"
	if err != nil {
		return err
	}
	head := string(buf[:n])
	if !strings.HasPrefix(head, "HTTP/1.") {
		return fmt.Errorf("这个端点给的不是 Engine API 响应：%s", oneLine(truncate(head, 60)))
	}
	if code := probeStatus(head); code >= 500 {
		return fmt.Errorf("端点应答 HTTP %d", code)
	}
	return nil
}

// probeStatus 从状态行取状态码（"HTTP/1.1 200 OK" → 200）
func probeStatus(head string) int {
	f := strings.Fields(head)
	if len(f) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(f[1])
	return n
}

// shortEndpoint 日志与提示用的短端点名：\""\\.\pipe\dockerDesktopLinuxEngine" → dockerDesktopLinuxEngine"
func shortEndpoint(ep endpoint) string {
	if ep.network != "pipe" {
		return ep.addr
	}
	return strings.TrimPrefix(ep.addr, `\\.\pipe\`)
}

// dialSaysEndpointWrong 只有「这个端点根本打不开 / 对面正在退出」才判端点选错；
// 管道全忙(ERROR_PIPE_BUSY=231) 是引擎在忙不是端点错（dialPipe 内部已重试过），
// 把它也算进去会导致好好连着的端点被反复丢弃重扫
func dialSaysEndpointWrong(err error) bool {
	return errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || // 2：管道不存在
		errors.Is(err, windows.ERROR_PATH_NOT_FOUND) || // 3
		errors.Is(err, windows.Errno(233)) || // ERROR_PIPE_NOT_AVAILABLE：实例都被关闭了
		errors.Is(err, windows.Errno(234)) || // ERROR_NO_DATA：管道正在关闭（Desktop 在退出）
		errors.Is(err, syscall_ERRNO_PIPE_MISSING)
}

// parseDOCKERHOST 支持 npipe:////./pipe/xxx、tcp://host:port、unix://（Windows 拒绝）
func parseDOCKERHOST(h string) (endpoint, error) {
	switch {
	case strings.HasPrefix(h, "npipe://"):
		p := strings.TrimPrefix(h, "npipe://")
		p = strings.ReplaceAll(p, "/", `\`)
		for strings.HasPrefix(p, `\`) {
			p = p[1:]
		}
		if !strings.HasPrefix(p, `.\pipe\`) {
			p = `.\pipe\` + p
		}
		full := `\\` + p
		return endpoint{network: "pipe", addr: full, desc: h}, nil
	case strings.HasPrefix(h, "tcp://"), strings.HasPrefix(h, "http://"):
		u, err := url.Parse(h)
		if err != nil {
			return endpoint{}, err
		}
		host := u.Host
		if u.Port() == "" {
			host += ":2375"
		}
		return endpoint{network: "tcp", addr: host, desc: h}, nil
	case strings.HasPrefix(h, "unix://"):
		return endpoint{}, fmt.Errorf("Windows 下不支持 unix:// 端点: %s", h)
	default:
		return endpoint{}, fmt.Errorf("未知协议: %s", h)
	}
}

// endpointDeadError 端点根本拨不通（管道不存在 / 对面正在退出）。
// 必须与「引擎在忙」（管道实例全被拉取、日志流占着）分开：前者该重扫候选，
// 后者只需等下一帧，误当端点错会导致拉取过程中面板乱跳
type endpointDeadError struct{ msg string }

func (e *endpointDeadError) Error() string { return e.msg }

// endpointLooksDead 这次失败是不是「端点拨不通」（而不是引擎在忙）
func endpointLooksDead(err error) bool {
	var dead *endpointDeadError
	return errors.As(err, &dead)
}

// dialEndpoint 按当前端点拨一条连接：拨不通就作废端点，下一轮重扫候选
func dialEndpoint(ctx context.Context, ep endpoint) (net.Conn, error) {
	if ep.network == "tcp" {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", ep.addr)
	}
	c, err := dialPipe(ep.addr)
	if err != nil {
		if dialSaysEndpointWrong(err) {
			// 这条管道根本不存在：引擎不在这儿（Desktop 重启过/换管道了），作废重扫
			invalidateEndpoint(fmt.Sprintf("拨 %s 失败: %v", ep.addr, err))
			return nil, &endpointDeadError{msg: fmt.Sprintf("%s（端点: %s）", errDaemonUnavailable, endpointDesc())}
		}
		if err == windows.ERROR_PIPE_BUSY {
			// 实例全被占着：引擎在跑且正忙（拉镜像、看日志会这样），不报「没运行」误导人
			return nil, fmt.Errorf("Docker 引擎正忙（管道实例已占满，端点: %s）", endpointDesc())
		}
		return nil, fmt.Errorf("%s（端点: %s）", errDaemonUnavailable, endpointDesc())
	}
	return c, nil
}

// resetEngineCaches 端点变了或失效了：把「上一个引擎」的协商结果与读缓存一起作废。
// 不作废的话，新 daemon 会沿用旧 API 版本前缀拼出不存在的 /v1.xx/ 路径，
// 面板还会拿死引擎留下的旧列表盖住新状态
func resetEngineCaches() {
	apiMu.Lock()
	apiVersion = ""
	versionProbeTTL = time.Time{}
	apiMu.Unlock()
	apiInvalidate()
	apiHeal()
	clearEngineStatusCache()
}

/* ============================ HTTP 客户端 ============================ */

var (
	clientOnce sync.Mutex
	httpClient *http.Client // 常规调用：整体 30s
	streamCli  *http.Client // 流式（日志跟随/镜像拉取）：不设整体超时
)

func initClients() {
	clientOnce.Lock()
	defer clientOnce.Unlock()
	if httpClient != nil {
		return
	}
	dialer := func(ctx context.Context, _, _ string) (net.Conn, error) {
		return dialEndpoint(ctx, resolveEndpoint())
	}
	mk := func(timeout, headerTimeout time.Duration) *http.Client {
		return &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext:           dialer,
				MaxIdleConns:          16,
				MaxIdleConnsPerHost:   16,
				IdleConnTimeout:       30 * time.Second,
				ResponseHeaderTimeout: headerTimeout,
				DisableCompression:    true, // 逐行解析 JSON 流，压缩会干扰
			},
		}
	}
	httpClient = mk(30*time.Second, 15*time.Second)
	streamCli = mk(0, 20*time.Second)
}

/* ============================ 请求封装 ============================ */

var (
	apiMu           sync.Mutex
	apiVersion      string // 协商到的 Engine API 版本（/version 返回）
	versionProbeTTL time.Time
)

type versionProbe struct {
	Version    string `json:"Version"`
	APIVersion string `json:"ApiVersion"`
	OSType     string `json:"Os"`
	Arch       string `json:"Arch"`
	Kernel     string `json:"KernelVersion"`
	GoVersion  string `json:"GoVersion"`
	Platform   struct {
		Name string `json:"Name"`
	} `json:"Platform"`
}

// getAPIVersion 取（并短期缓存）API 版本，用于拼 /v{ver}/ 前缀。
// 这里必须走免排队通道：前缀是每个请求的前置条件，若再占一个闸门名额，
// 闸门打满时会出现「所有请求都在等 /version，而 /version 又在等名额」的自锁。
// 拿 /version 时不能持 apiMu：拨号里可能因端点失效回头清缓存（同样要 apiMu），
// 同一个 goroutine 重入就是死锁
func getAPIVersion() string {
	apiMu.Lock()
	v, ttl := apiVersion, versionProbeTTL
	apiMu.Unlock()
	if v != "" && time.Now().Before(ttl) {
		return v
	}
	var p versionProbe
	if err := rawDoDirect(http.MethodGet, "/version", nil, nil, &p); err != nil {
		return "" // 引擎不可达：交给上层报统一错误
	}
	apiMu.Lock()
	apiVersion = p.APIVersion
	versionProbeTTL = time.Now().Add(10 * time.Minute)
	apiMu.Unlock()
	return p.APIVersion
}

func prefix() string {
	if v := getAPIVersion(); v != "" {
		return "/v" + v
	}
	return ""
}

// do 发起一次 Engine API 调用；out 非 nil 时按 JSON 解码响应体
func do(method, path string, query url.Values, body any, out any) error {
	return rawDo(method, prefix()+path, query, body, out, false)
}

// rawDo 实现细节：path 已含版本前缀；longLived 表示流式请求（不设整体超时）
func rawDo(method, path string, query url.Values, body any, out any, longLived bool) error {
	resp, err := rawRequest(method, path, query, body, longLived)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResponse(resp, out)
}

// rawDoDirect 不走闸门的请求：仅供取 API 版本这类前置探测使用
func rawDoDirect(method, path string, query url.Values, body any, out any) error {
	resp, err := rawRequestNoGate(method, path, query, body, false)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return decodeResponse(resp, out)
}

// rawRequest 发起请求并把响应原样交回（调用方负责 Close）。
// 闸门只覆盖到「拿到响应头」：轮询雪崩就发生在这一步，读完 body 的那点时间不值得再占名额
func rawRequest(method, path string, query url.Values, body any, longLived bool) (*http.Response, error) {
	release, err := apiEnter()
	if err != nil {
		return nil, err
	}
	resp, err := rawRequestNoGate(method, path, query, body, longLived)
	release()
	if err != nil {
		return nil, err
	}
	// 写操作成功后立刻作废读缓存，避免面板拿旧列表盖回新状态
	if method != http.MethodGet {
		apiInvalidate()
	}
	return resp, nil
}

// rawRequestNoGate 真正发请求的地方：成功即解除熔断，传输层失败即进入静默窗口
func rawRequestNoGate(method, path string, query url.Values, body any, longLived bool) (*http.Response, error) {
	initClients()
	// Engine API 不校验 Host（daemon 仅要求 tcp 绑定时的 IP/localhost），统一用 localhost
	u := url.URL{Scheme: "http", Host: "localhost", Path: path}
	if query != nil {
		u.RawQuery = query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, u.String(), rdr)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	cli := httpClient
	if longLived {
		cli = streamCli
	}
	resp, err := cli.Do(req)
	if err != nil {
		e := unavailableErr(err)
		apiTrip(e)
		return nil, e
	}
	apiHeal()
	return resp, nil
}

// decodeResponse 按状态码处理响应，2xx 且 out 非 nil 时解码 JSON
func decodeResponse(resp *http.Response, out any) error {
	data, over, err := readCapped(resp.Body, apiResponseMax)
	if err != nil {
		e := unavailableErr(err)
		apiTrip(e) // 拿到响应头但读 body 也超时：同样是引擎卡住，要进静默窗口
		return e
	}
	if resp.StatusCode >= 400 {
		return apiError(resp.StatusCode, data)
	}
	// 正常 JSON 不会到这个量级；撞上限说明对方不是给我们的应答（或被劫持），
	// 继续读只会把内存吃掉一大块，直接报错收场
	if over {
		return fmt.Errorf("Docker 响应超过 %d MB，本次已放弃读取（请缩小查询范围）", apiResponseMax>>20)
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("解析 Docker 响应失败: %v", err)
	}
	return nil
}

const (
	// apiResponseMax 一次普通 API 应答的字节上限（防“一个响应吃掉几 GB”）
	apiResponseMax = 32 << 20
	// apiErrBodyMax 错误体只用来提取 message，超过这个长度就没必要读了
	apiErrBodyMax = 1 << 20
)

// readCapped 读满上限就停，并告知是否超长。宁可少一截也不能让一次无上限
// 的 ReadAll 把进程顶到 OOM（多读 1 字节用来判别是否真的到了头）。
func readCapped(r io.Reader, limit int64) ([]byte, bool, error) {
	data, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return data, false, err
	}
	if int64(len(data)) <= limit {
		return data, false, nil
	}
	return data[:limit], true, nil
}

// apiError 把 Docker 的错误体（{"message":"..."}）翻成可读信息
func apiError(status int, data []byte) error {
	var e struct {
		Message string `json:"message"`
	}
	msg := ""
	if json.Unmarshal(data, &e) == nil && e.Message != "" {
		msg = e.Message
	} else {
		msg = strings.TrimSpace(string(data))
	}
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	// 带上状态码返回：看护容器退出的调用方需要分清「404 对象没了」与「引擎一时答不上」
	return apiStatusErr(status, msg)
}

// unavailableErr 把底层拨号/IO 错误统一成引擎不可达提示（保留原始信息便于排查）
func unavailableErr(err error) error {
	if err == nil {
		return nil
	}
	// http.Client 会把传输层错误裹成 url.Error（前面挂一串 Get "http://localhost/version": ），
	// 那层壳对面板没有信息量，先剥掉再归类；剥完仍是 endpointDeadError，
	// 上层 endpointLooksDead 照样认得出「端点死了」
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		err = ue.Err
	}
	var oe *net.OpError
	switch {
	case errors.Is(err, windows.ERROR_FILE_NOT_FOUND),
		errors.Is(err, windows.ERROR_PIPE_BUSY),
		errors.Is(err, windows.ERROR_BROKEN_PIPE),
		errors.Is(err, syscall_ERRNO_PIPE_MISSING):
		return fmt.Errorf("%s（端点: %s）", errDaemonUnavailable, endpointDesc())
	case errors.As(err, &oe):
		return fmt.Errorf("%s（端点: %s）", errDaemonUnavailable, endpointDesc())
	default:
		return err
	}
}

// syscall_ERRNO_PIPE_MISSING 部分 x/sys 版本未导出该常量，这里按 Win32 码值补齐
var syscall_ERRNO_PIPE_MISSING = windows.Errno(230) // ERROR_PIPE_NOT_AVAILABLE

// endpointDesc 当前端点描述（未连上也 informative，供错误提示与引擎卡片展示）。
// 这里故意不触发重扫：拼错误信息不该顺带拨一轮候选管道
func endpointDesc() string {
	endpMu.Lock()
	defer endpMu.Unlock()
	if endpVal.desc != "" {
		return endpDesc
	}
	return endpointCandidates()[0].desc
}
