package docker

// ============================================================
// Docker 管理 - 加速站「能不能真下载」探测
//
// 只问 /v2/ 通不通没有意义：加速站常见的毛病是清单给得出来、层数据却一个字节
// 都不发，界面表现为「拉取永远停在 Downloading」。这里按 daemon 真正会走的
// 三步逐段计时：
//   1) GET /v2/                                 → 域名 / TLS / 端口（连通）
//   2) GET /v2/<name>/manifests/<tag>           → 有没有这份清单
//   3) GET /v2/<name>/blobs/<层digest> 的前一段  → 数据是不是真能下、下得多快
// 三步分开给结论，「连不上 / 没清单 / 不给数据 / 能下但慢」一眼可辨。
// 取样默认 library/alpine（层小、每个站都该有），也可以在界面上填成自己
// 正要拉的那个名字，用来复现「就是这一层下不动」。
// ============================================================

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"go-tools/services/applog"
)

const (
	mirrorConnectTO  = 8 * time.Second  // 连通：含 DNS 与 TLS 握手
	mirrorManifestTO = 12 * time.Second // 清单：站点可能要回源，比连通慢
	mirrorBlobTO     = 10 * time.Second // 层数据：只取前一段，够了就收手
	mirrorProbeRange = 512 << 10        // 层数据取样体积
	mirrorEmptyBytes = 4096             // 比这还少 = 根本没发数据
	mirrorSlowMbps   = 1.0              // 低于此速率算「慢」
	mirrorSlowTtfbMs = 3000             // 首字节超过 3 秒也算慢

	mirrorSampleDefault = "library/alpine:latest"
	mirrorProbeWorkers  = 4 // 全部测速时的并发上限，别把出口带宽挤满
)

// manifestAccepts 清单接口可接受的媒体类型：新旧索引/清单格式都带上，
// 让站点自己挑，免得只写一种拿到的是转换过的旧格式
var manifestAccepts = strings.Join([]string{
	"application/vnd.docker.distribution.manifest.list.v2+json",
	"application/vnd.docker.distribution.manifest.v2+json",
	"application/vnd.oci.image.index.v1+json",
	"application/vnd.oci.image.manifest.v1+json",
}, ", ")

/* ============================ 探测主体 ============================ */

// ProbeMirror 探一个加速站能不能真的下载。
// 故意不返回 error：探不通本身就是结果，绑定面按 error 返回会额外打一条 ERR，
// 把「这个站不行」混进程序异常里反而看不清。
func ProbeMirror(raw, sample string) MirrorProbe {
	return probeMirrorTracked(context.Background(), raw, sample)
}

// probeMirrorTracked 把这次探测登记进可取消表：一个不给数据的站要等满 8+12+10 秒，
// 界面上的「测速」按钮在探测中必须能叫停，否则用户只能干看着。
func probeMirrorTracked(parent context.Context, raw, sample string) MirrorProbe {
	if parent.Err() != nil {
		return mirrorCancelled(MirrorProbe{Url: strings.TrimSpace(raw)})
	}
	ctx, cancel := context.WithCancel(parent)
	registerMirrorProbe(raw, cancel)
	defer func() {
		unregisterMirrorProbe(raw)
		cancel()
	}()
	return probeMirror(ctx, raw, sample)
}

func probeMirror(parent context.Context, raw, sample string) MirrorProbe {
	base, err := mirrorBaseURL(raw)
	if err != nil {
		return MirrorProbe{
			Url: strings.TrimSpace(raw), Grade: "fail", Stage: "url",
			Label: "地址不合法", Hint: err.Error(), ProbedAt: time.Now().Format("15:04:05"),
		}
	}
	p := MirrorProbe{Url: base, Host: hostOnly(base), Grade: "fail", ProbedAt: time.Now().Format("15:04:05")}
	smp := strings.TrimSpace(sample)
	if smp == "" {
		smp = mirrorSampleDefault
	}
	p.Sample = smp
	repo, tag := mirrorRepoTag(smp)

	cli := registryClient(registryInsecure(p.Host))
	cli.Timeout = 0 // 三段各有自己的截止时间，别让整条请求共用一个 15 秒

	// ---------- 1. 连通 ----------
	cctx, ccancel := context.WithTimeout(parent, mirrorConnectTO)
	defer ccancel()
	t0 := time.Now()
	resp, err := mirrorGet(cctx, cli, base+"/v2/", nil)
	if err != nil {
		if mirrorStopped(parent) {
			return mirrorCancelled(p)
		}
		p.Stage, p.Label, p.Hint = "connect", "连不上 "+p.Host, mirrorNetHint(err)
		applog.Warnf("docker", "加速站探测 %s 连通失败: %v", base, err)
		return p
	}
	drainMirrorResp(resp)
	p.ConnectMs = time.Since(t0).Milliseconds()
	if resp.StatusCode >= 400 {
		p.Stage, p.Label = "connect", fmt.Sprintf("/v2/ 返回 HTTP %d", resp.StatusCode)
		p.Hint = mirrorStatusHint(resp.StatusCode, p.Host)
		applog.Warnf("docker", "加速站探测 %s /v2/ = %d", base, resp.StatusCode)
		return p
	}

	// ---------- 2. 清单 ----------
	man, err := mirrorManifest(parent, cli, base, repo, tag)
	if err != nil {
		if mirrorStopped(parent) {
			return mirrorCancelled(p)
		}
		p.Stage, p.Label, p.Hint = "manifest", "清单取不到", mirrorErrorHint(err)
		applog.Warnf("docker", "加速站探测 %s 清单失败(%s:%s): %v", base, repo, tag, err)
		return p
	}
	if len(man.Layers) == 0 {
		p.Stage, p.Label = "manifest", "清单里没有层数据"
		p.Hint = "返回的清单是空的，或多架构索引解析不来：换个取样镜像试试"
		return p
	}
	p.ManifestMs = time.Since(t0).Milliseconds()

	// 取最大的一层：最能暴露「慢」和「卡」，也比小层更接近真实拉取
	layers := man.Layers
	sort.SliceStable(layers, func(i, j int) bool { return layers[i].Size > layers[j].Size })
	blob := layers[0]
	p.Layer, p.BlobSize = shortDigest(blob.Digest), blob.Size

	// ---------- 3. 层数据 ----------
	bctx, bcancel := context.WithTimeout(parent, mirrorBlobTO)
	defer bcancel()
	tb := time.Now()
	bresp, err := mirrorGet(bctx, cli, fmt.Sprintf("%s/v2/%s/blobs/%s", base, repo, blob.Digest),
		map[string]string{"Range": fmt.Sprintf("bytes=0-%d", mirrorProbeRange-1)})
	if err != nil {
		if mirrorStopped(parent) {
			return mirrorCancelled(p)
		}
		p.Stage, p.Label, p.Hint = "blob", "层数据请求发不出去", mirrorErrorHint(err)
		applog.Warnf("docker", "加速站探测 %s 层数据失败(层 %s): %v", base, p.Layer, err)
		return p
	}
	defer bresp.Body.Close()
	if bresp.StatusCode >= 400 {
		p.Stage, p.Label = "blob", fmt.Sprintf("层数据返回 HTTP %d", bresp.StatusCode)
		p.Hint = mirrorStatusHint(bresp.StatusCode, p.Host)
		applog.Warnf("docker", "加速站探测 %s 层数据 = %d", base, bresp.StatusCode)
		return p
	}
	got, ttfb, rerr := readMirrorBody(bctx, bresp.Body)
	p.Bytes, p.TtfbMs = got, ttfb.Milliseconds()
	secs := time.Since(tb).Seconds()
	if secs < 0.05 {
		secs = 0.05
	}
	p.Mbps = float64(got) * 8 / 1e6 / secs
	p.Stage = "blob"
	if rerr != nil && got < mirrorEmptyBytes {
		// 用户点了「停止」时读循环也会报中断，不能当成站点不给数据
		if mirrorStopped(parent) {
			return mirrorCancelled(p)
		}
		p.Stage, p.Label, p.Hint = "blob", "层数据读取中断", mirrorErrorHint(rerr)
		applog.Warnf("docker", "加速站探测 %s 层数据中断(层 %s): %v", base, p.Layer, rerr)
		return p
	}
	if mirrorStopped(parent) {
		return mirrorCancelled(p)
	}
	mirrorVerdict(&p)
	applog.Infof("docker", "加速站探测 %s 结论 %s：%s（连通 %dms，清单 %dms，首字节 %dms，%.1f Mbps，取样 %s:%s 层 %s）",
		base, p.Grade, p.Label, p.ConnectMs, p.ManifestMs, p.TtfbMs, p.Mbps, repo, tag, p.Layer)
	return p
}

// ProbeMirrors 一次探完多个站（界面上的「全部测速」）：并发有限，结果按传入顺序回。
// 整批登记成一个可取消任务：停止后还在排队的站点直接回「已停止」，不会继续跑完。
func ProbeMirrors(raws []string, sample string) []MirrorProbe {
	ctx, cancel := context.WithCancel(context.Background())
	registerMirrorProbe(mirrorBatchKey, cancel)
	defer func() {
		unregisterMirrorProbe(mirrorBatchKey)
		cancel()
	}()

	out := make([]MirrorProbe, len(raws))
	sem := make(chan struct{}, mirrorProbeWorkers)
	var wg sync.WaitGroup
	for i, u := range raws {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, u string) {
			defer func() { <-sem }()
			defer wg.Done()
			out[i] = probeMirrorTracked(ctx, u, sample)
		}(i, u)
	}
	wg.Wait()
	return out
}

/* ============================ 叫停注册表 ============================ */

var (
	mirrorProbeMu   sync.Mutex
	mirrorProbeStop = map[string]context.CancelFunc{}
)

// mirrorBatchKey 是「全部测速」整批的键；单个站用地址原文，两者不会撞
const mirrorBatchKey = "*"

func mirrorProbeKey(raw string) string { return strings.TrimSpace(raw) }

func registerMirrorProbe(raw string, cancel context.CancelFunc) {
	mirrorProbeMu.Lock()
	mirrorProbeStop[mirrorProbeKey(raw)] = cancel
	mirrorProbeMu.Unlock()
}

func unregisterMirrorProbe(raw string) {
	mirrorProbeMu.Lock()
	delete(mirrorProbeStop, mirrorProbeKey(raw))
	mirrorProbeMu.Unlock()
}

// CancelMirrorProbe 停掉某个站正在跑的探测；没在跑返回 false（界面据此提示「已经测完了」）
func CancelMirrorProbe(raw string) bool {
	mirrorProbeMu.Lock()
	cancel, ok := mirrorProbeStop[mirrorProbeKey(raw)]
	mirrorProbeMu.Unlock()
	if ok && cancel != nil {
		cancel()
	}
	return ok
}

// CancelMirrorProbes 停掉所有在跑的探测（含「全部测速」整批），返回叫停的个数
func CancelMirrorProbes() int {
	mirrorProbeMu.Lock()
	pending := make([]context.CancelFunc, 0, len(mirrorProbeStop))
	for _, c := range mirrorProbeStop {
		pending = append(pending, c)
	}
	mirrorProbeMu.Unlock()
	for _, c := range pending {
		c()
	}
	return len(pending)
}

// mirrorStopped 这次探测是不是被用户叫停的：叫停时请求会以 context 错误返回，
// 不能当成「连不上 / 读中断 / 不给数据」报到界面上
func mirrorStopped(parent context.Context) bool {
	return parent != nil && parent.Err() != nil
}

func mirrorCancelled(p MirrorProbe) MirrorProbe {
	p.Grade, p.Stage, p.Label = "cancel", "cancel", "已停止测速"
	p.Hint = "手动中止，这一段没有结论；需要的话再点一次测速"
	return p
}

/* ============================ 结论分级 ============================ */

// mirrorVerdict 只按数字给结论，不碰网络，便于单测
func mirrorVerdict(p *MirrorProbe) {
	switch {
	case p.Bytes < mirrorEmptyBytes:
		p.Grade = "empty"
		p.Label = "层数据一个字节都没发过来"
		p.Hint = fmt.Sprintf(
			"连通和清单都正常，但要 %s 这一层时站点不再吐数据（已等 %d 毫秒）——"+
				"拉取卡在 Downloading 就是它。换个加速站再拉同一个镜像。",
			humanSize(p.BlobSize), p.TtfbMs)
	case p.Mbps < mirrorSlowMbps || p.TtfbMs > mirrorSlowTtfbMs:
		p.Grade = "slow"
		p.Label = fmt.Sprintf("能下，但很慢：%.1f Mbps，首字节 %d 毫秒", p.Mbps, p.TtfbMs)
		p.Hint = "批量拉取前把并发压到 1~2，或与其它站对比后再定默认来源"
	default:
		p.Grade = "ok"
		p.Label = fmt.Sprintf("可下载：%.1f Mbps，首字节 %d 毫秒", p.Mbps, p.TtfbMs)
		p.Hint = ""
	}
}

// mirrorBaseURL 规整加速站地址：补协议、去尾斜杠与写顺手的 /v2
func mirrorBaseURL(raw string) (string, error) {
	u := strings.TrimRight(strings.TrimSpace(raw), "/")
	if u == "" {
		return "", fmt.Errorf("加速站地址为空")
	}
	u = strings.TrimRight(strings.TrimSuffix(u, "/v2"), "/")
	if u == "" {
		return "", fmt.Errorf("加速站地址为空")
	}
	low := strings.ToLower(u)
	if !strings.HasPrefix(low, "http://") && !strings.HasPrefix(low, "https://") {
		u = "https://" + u
		low = strings.ToLower(u)
	}
	if strings.Contains(strings.TrimPrefix(strings.TrimPrefix(low, "https://"), "http://"), "/") {
		return "", fmt.Errorf("只填站点根地址（如 https://docker.m.daocloud.io），不要带路径")
	}
	return u, nil
}

// mirrorRepoTag 把取样名拆成 registry API 要的 路径/标签。
// 加速站只代理 docker.io，所以名字前习惯带的仓库主机名要摘掉；
// 单段名字补 library/——与 daemon 的行为一致，否则清单路径对不上。
func mirrorRepoTag(sample string) (string, string) {
	repo, tag := splitRepoTag(strings.TrimSpace(sample))
	segs := strings.Split(repo, "/")
	if len(segs) >= 2 && looksLikeRegistryHost(segs[0]) {
		segs = segs[1:]
	}
	if len(segs) == 0 || strings.TrimSpace(segs[0]) == "" {
		segs = []string{"library", "alpine"}
	}
	if len(segs) == 1 {
		segs = []string{"library", strings.TrimSpace(segs[0])}
	}
	return strings.Join(segs, "/"), tag
}

func looksLikeRegistryHost(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || s == "library" {
		return false
	}
	return strings.Contains(s, ".") || strings.Contains(s, ":")
}

// mirrorNetHint 把传输层错误翻译成该做什么，而不是把 Go 的原文甩给用户
func mirrorNetHint(err error) string {
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "no such host"):
		return "域名解析不了：检查地址拼写，或本机 DNS 是否被改过"
	case strings.Contains(s, "connection refused"):
		return "对方拒绝连接：这个地址上没有 registry-mirror 在监听"
	case strings.Contains(s, "x509"), strings.Contains(s, "tls"), strings.Contains(s, "certificate"):
		return "证书握手失败：自签或过期证书，请先把该地址登记到「私有仓库地址」里放行"
	case strings.Contains(s, "timeout"), strings.Contains(s, "deadline exceeded"):
		return "连接超时：本机到该站的路不通（或被中间设备拦了），换站或稍后再试"
	case strings.Contains(s, "reset by peer"), strings.Contains(s, "broken pipe"):
		return "连接被中途掐断：链路或该站不稳定，重试一次；反复如此就换站"
	case strings.Contains(s, "proxy"):
		return "走了代理但代理不通：系统代理里若有该域名的规则，先直连试一次"
	default:
		return err.Error()
	}
}

// mirrorErrorHint 清单/层数据阶段的错误：先认状态码与协议错误，再回落到网络归因
func mirrorErrorHint(err error) string {
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "http 404"), strings.Contains(s, "not found"), strings.Contains(s, "manifest unknown"):
		return "该站取不到这个名字的清单：名字不存在，或它只代理 docker.io 而你填了别处的镜像"
	case strings.Contains(s, "http 401"), strings.Contains(s, "http 403"), strings.Contains(s, "too many requests"):
		return "站点拒绝匿名取清单：稍后再试，或换站"
	}
	return mirrorNetHint(err)
}

func mirrorStatusHint(code int, host string) string {
	switch code {
	case http.StatusUnauthorized:
		return fmt.Sprintf("%s 要求登录：加速站一般不该要账号，确认是否把私有仓库地址写到了这里", host)
	case http.StatusForbidden, http.StatusTooManyRequests:
		return "站点拒绝或限流：稍后再试，或换一个加速站"
	case http.StatusNotFound:
		return "地址根下没有 registry v2 接口：这多半不是加速站，而是某个仓库或网页站点"
	default:
		return "服务异常：换站试试，或稍后重试"
	}
}

/* ============================ 请求细节 ============================ */

// mirrorGet 发一次仓库请求：遇 Bearer challenge 自动换匿名 token 重试一次
func mirrorGet(ctx context.Context, cli *http.Client, rawURL string, hdr map[string]string) (*http.Response, error) {
	resp, err := doMirrorGet(ctx, cli, rawURL, hdr, "")
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}
	ch := resp.Header.Get("WWW-Authenticate")
	if !strings.HasPrefix(strings.ToLower(ch), "bearer ") {
		return resp, nil
	}
	_ = resp.Body.Close()
	tok, terr := fetchRegistryToken(cli, ch, "", "")
	if terr != nil || tok == "" {
		return nil, fmt.Errorf("该站要求认证但令牌拿不到: %v", terr)
	}
	return doMirrorGet(ctx, cli, rawURL, hdr, tok)
}

func doMirrorGet(ctx context.Context, cli *http.Client, rawURL string, hdr map[string]string, bearer string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return cli.Do(req)
}

// manifestDoc 清单接口的形状：多架构索引与单平台清单共用一个结构，各取所需字段
type manifestDoc struct {
	SchemaVersion int    `json:"schemaVersion"`
	MediaType     string `json:"mediaType"`
	Layers        []struct {
		Digest string `json:"digest"`
		Size   int64  `json:"size"`
	} `json:"layers"`
	Manifests []struct {
		Digest   string `json:"digest"`
		Size     int64  `json:"size"`
		Platform struct {
			OS           string `json:"os"`
			Architecture string `json:"architecture"`
		} `json:"platform"`
	} `json:"manifests"`
}

// mirrorManifest 取清单：多架构索引先落到本机这一份（linux/amd64），再取它的层列表
func mirrorManifest(parent context.Context, cli *http.Client, base, repo, tag string) (manifestDoc, error) {
	var man manifestDoc
	hdr := map[string]string{"Accept": manifestAccepts}
	u := fmt.Sprintf("%s/v2/%s/manifests/%s", base, repo, tag)
	resp, err := mirrorGet(mirrorCtx(parent, mirrorManifestTO), cli, u, hdr)
	if err != nil {
		return man, err
	}
	if resp.StatusCode >= 400 {
		body := readMirrorHead(resp)
		_ = resp.Body.Close()
		return man, fmt.Errorf("清单 HTTP %d %s", resp.StatusCode, body)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&man); err != nil {
		return man, fmt.Errorf("清单格式读不了: %v", err)
	}
	if len(man.Layers) > 0 || len(man.Manifests) == 0 {
		return man, nil
	}
	// 多架构索引：本机是 amd64，就按 amd64 那份算真实下载量
	sub := man.Manifests[0].Digest
	for _, m := range man.Manifests {
		if strings.EqualFold(m.Platform.OS, "linux") &&
			(strings.EqualFold(m.Platform.Architecture, "amd64") || strings.EqualFold(m.Platform.Architecture, "x86_64")) {
			sub = m.Digest
			break
		}
	}
	u2 := fmt.Sprintf("%s/v2/%s/manifests/%s", base, repo, sub)
	resp2, err := mirrorGet(mirrorCtx(parent, mirrorManifestTO), cli, u2, hdr)
	if err != nil {
		return man, err
	}
	if resp2.StatusCode >= 400 {
		body := readMirrorHead(resp2)
		_ = resp2.Body.Close()
		return man, fmt.Errorf("子清单 HTTP %d %s", resp2.StatusCode, body)
	}
	defer resp2.Body.Close()
	var inner manifestDoc
	if err := json.NewDecoder(io.LimitReader(resp2.Body, 8<<20)).Decode(&inner); err != nil {
		return man, fmt.Errorf("子清单格式读不了: %v", err)
	}
	return inner, nil
}

// mirrorCtx 给一段独立的截止时间（继承外层的取消信号）。cancel 不立即 defer 是因为
// 响应体还要继续读，由发起探测的那一层统一收尾。
func mirrorCtx(parent context.Context, d time.Duration) context.Context {
	ctx, cancel := context.WithTimeout(parent, d)
	_ = cancel // 到期自动回收；不保留 cancel 是因为请求完成后由上层统一释放
	return ctx
}

// readMirrorBody 边读边计首字节耗时：到量、到时或断流就回来，不追求读完一层
func readMirrorBody(ctx context.Context, body io.Reader) (int64, time.Duration, error) {
	var got int64
	var ttfb time.Duration
	start := time.Now()
	buf := make([]byte, 64<<10)
	for got < mirrorProbeRange {
		n, rerr := body.Read(buf)
		if n > 0 {
			if got == 0 {
				ttfb = time.Since(start)
			}
			got += int64(n)
		}
		if rerr != nil {
			if ctx.Err() != nil { // 取样时间到了：算正常收工
				return got, ttfb, nil
			}
			if rerr == io.EOF {
				break
			}
			return got, ttfb, rerr
		}
	}
	return got, ttfb, nil
}

func readMirrorHead(resp *http.Response) string {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	return oneLine(strings.TrimSpace(string(data)))
}

func drainMirrorResp(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
	_ = resp.Body.Close()
}

func shortDigest(d string) string {
	d = strings.TrimPrefix(strings.ToLower(d), "sha256:")
	if len(d) > 12 {
		return d[:12]
	}
	return d
}
