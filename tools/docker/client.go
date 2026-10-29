package docker

// ============================================================
// Docker Engine HTTP 客户端（平台无关层）
//
// 这一层不碰任何操作系统 API：端点快照与重扫、API 版本协商、
// 请求闸门与熔断、响应解码，全部只依赖 net/http。真正"怎么连上去"
// 交给平台文件——client_windows.go 是命名管道，client_other.go 是 unix socket。
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
	"time"

	"Tools/services/applog"
)

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

// sweepErrs 最近一轮全量重扫里每个候选的拨号错误（与候选顺序一一对应）。
// 只用来回答“为什么连不上”：socket 不存在、没权限、无人监听这三种情况
// 在拨号层都是“失败”，但用户要做的下一步完全不同。
var (
	sweepMu   sync.Mutex
	sweepErrs []error
)

func setSweepFailures(errs []error) {
	sweepMu.Lock()
	sweepErrs = errs
	sweepMu.Unlock()
}

// dialFailures 最近一轮候选重扫的失败原因快照（给平台诊断文案用）
func dialFailures() []error {
	sweepMu.Lock()
	defer sweepMu.Unlock()
	return append([]error(nil), sweepErrs...)
}

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
	errs := make([]error, 0, len(cands))
	for i, ep := range cands {
		if err := endpointProber(ep); err == nil {
			setEndpointLocked(ep, true)
			setSweepFailures(nil)
			applog.Infof("docker", "引擎端点定为 %s（第 %d/%d 个候选应答）", ep.desc, i+1, len(cands))
			return ep
		} else {
			// 带上失败原因：权限不足与 socket 不存在在日志里长得一模一样，
			// 不区分就只能给出“请确认服务正在运行”这种无用提示
			tried = append(tried, shortEndpoint(ep)+"（"+classifyDialError(err)+"）")
			errs = append(errs, err)
		}
	}
	// 一个都不应答：仍按首选候选回话，不钉住；引擎一起来下一轮就能跟上
	fb := cands[0]
	setEndpointLocked(fb, false)
	setSweepFailures(errs)
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

// endpointCandidates 本轮要试的候选：DOCKER_HOST 只认它自己，不回落默认候选
func endpointCandidates() []endpoint {
	if h := strings.TrimSpace(os.Getenv("DOCKER_HOST")); h != "" {
		if ep, err := parseDOCKERHOST(h); err == nil {
			ep.fromEnv = true
			return []endpoint{ep}
		}
		applog.Warnf("docker", "DOCKER_HOST=%s 解析不了，改按默认候选探测", h)
	}
	return candidateEndpoints()
}

// probeEndpointLive 实探：拨通且 /version 真给出应答才算它活着。
// 只看「管道存在」不够——Desktop 退出/重启时会留下不应答的管道，
// 这正是面板跟不到状态的原因，所以必须发一次真请求。
func probeEndpointLive(ep endpoint) error {
	ctx, cancel := context.WithTimeout(context.Background(), endpointProbeTO)
	defer cancel()
	c, err := dialEndpointConn(ctx, ep)
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

// shortEndpoint 日志与提示用的短端点名（具体缩写规则由平台决定）
func shortEndpoint(ep endpoint) string { return shortEndpointAddr(ep) }

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
	c, err := dialEndpointConn(ctx, ep)
	if err != nil {
		if dialSaysEndpointWrong(err) {
			// 端点根本不存在：引擎不在这儿（Desktop 重启过 / 换了 socket），作废重扫
			invalidateEndpoint(fmt.Sprintf("拨 %s 失败: %v", ep.addr, err))
			return nil, &endpointDeadError{msg: fmt.Sprintf("%s（端点: %s）", errDaemonUnavailable, endpointDesc())}
		}
		if isEndpointBusy(err) {
			// 连接被占满：引擎在跑且正忙（拉镜像、看日志会这样），不报「没运行」误导人
			return nil, fmt.Errorf("Docker 引擎正忙（连接已占满，端点: %s）", endpointDesc())
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
	case isTransportEndpointErr(err):
		return fmt.Errorf("%s（端点: %s）", errDaemonUnavailable, endpointDesc())
	case errors.As(err, &oe):
		return fmt.Errorf("%s（端点: %s）", errDaemonUnavailable, endpointDesc())
	default:
		return err
	}
}

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

/* ============================ 小工具 ============================ */

// truncate 按 rune 截断，避免把中文切成半个字节序列
func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max]) + "…"
}

// oneLine 压平换行：体检状态这类要显示在单行里的原文
func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// humanSize 体积口语化（进度与提示都用 MB 表达）
func humanSize(n int64) string {
	if n < 0 {
		return "未知大小"
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(n)/1024/1024)
}
