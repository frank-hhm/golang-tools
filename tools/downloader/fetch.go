package downloader

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ============================================================
// 下载内核：探测 → 分块/单流 → 落盘 → 续传 → 重试 → 限速。
// downloadFile 是纯函数式的（只吃参数与回调），Manager 与 go test 都直接调它，
// 便于用 httptest 起本地服务器逐字节校验。
// ============================================================

// errFallbackSingle 分块途中发现服务端其实不守 Range（回了 200 全量）时，
// 用它通知上层改走单流，而不是把同一份内容重复写进各段。
var errFallbackSingle = errors.New("server ignored Range, fallback to single stream")

// dlParams downloadFile 的入参。
type dlParams struct {
	ctx      context.Context
	client   *http.Client
	url      string
	savePath string
	conns    int
	minSeg   int64
	maxRetry int
	tb       *tokenBucket
	// gate 全局块并发闸门（Manager 注入；测试可传 nil 表示不限）。
	gate func() (release func())
	// onProgress 周期性回调：size 可能为 0（未知长度），downloaded 为已落盘字节。
	onProgress func(size, downloaded int64, parts []Part)
}

// downloadFile 下载单个文件到 savePath，阻塞至完成 / ctx 取消 / 致命错误。
func downloadFile(p dlParams) error {
	if p.maxRetry <= 0 {
		p.maxRetry = defaultMaxRetry
	}
	size, acceptRanges, err := probe(p.ctx, p.client, p.url)
	if err != nil {
		return err
	}

	chunked := size > 0 && acceptRanges && size > p.minSeg && p.conns > 1
	if chunked {
		err = downloadChunked(p, size)
		if errors.Is(err, errFallbackSingle) {
			chunked = false
			err = nil
		}
	}
	if !chunked {
		err = downloadSingle(p, size)
	}
	return err
}

// downloadChunked 定长 + 支持 Range：切 N 段并发，各段 WriteAt 落偏移。
func downloadChunked(p dlParams, size int64) error {
	f, err := os.OpenFile(p.savePath, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Truncate(size); err != nil { // 稀疏预分配，续传只补未完成区间
		return err
	}

	parts := planParts(size, p.conns)
	// 续传：meta 与本次布局（url/size/conns）一致才复用偏移，否则从头再来。
	if m := loadMeta(p.savePath); m != nil && m.matchChunked(p.url, size, p.conns) {
		for i := range parts {
			if d, ok := m.Parts[parts[i].index]; ok && d > 0 && d <= parts[i].length() {
				parts[i].done.Store(d)
			}
		}
	}

	var wg sync.WaitGroup
	errCh := make(chan error, len(parts))
	stop := make(chan struct{}) // 任一致命错误 → 通知其余块尽快收手
	var stopOnce sync.Once

	for i := range parts {
		pt := &parts[i]
		if pt.done.Load() >= pt.length() {
			continue // 已下完的块（续传）直接跳过，不重复请求
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if p.gate != nil {
				release := p.gate()
				defer release()
			}
			if err := fetchRange(p, f, pt, stop); err != nil {
				if errors.Is(err, errFallbackSingle) || isFatal(err) || errors.Is(err, context.Canceled) {
					stopOnce.Do(func() { close(stop) })
				}
				errCh <- err
			} else {
				// 每完成一块就落一次续传 meta：进程被杀也不丢已完成段。
				saveMeta(p.savePath, metaForParts(p.url, size, p.conns, true, parts))
			}
		}()
	}
	wg.Wait()
	close(errCh)

	// 汇总第一个错误（fallback 优先，其次 ctx，最后其它）。
	var firstErr error
	for e := range errCh {
		if e == nil {
			continue
		}
		if errors.Is(e, errFallbackSingle) {
			firstErr = e
			break
		}
		if firstErr == nil || (errors.Is(firstErr, context.Canceled) && !errors.Is(e, context.Canceled)) {
			firstErr = e
		}
	}
	if firstErr != nil {
		saveMeta(p.savePath, metaForParts(p.url, size, p.conns, true, parts))
		return firstErr
	}

	// 全部完成：清 meta，进度补满。
	removeMeta(p.savePath)
	emitProgress(p, size, parts)
	return nil
}

// fetchRange 下载一段 [start,end]，带重试与限速；写偏移 = start+已下。
func fetchRange(p dlParams, f *os.File, pt *partState, stop <-chan struct{}) error {
	var lastErr error
	for attempt := 0; attempt <= p.maxRetry; attempt++ {
		if p.ctx.Err() != nil {
			return p.ctx.Err()
		}
		select {
		case <-stop:
			return context.Canceled
		default:
		}
		if attempt > 0 {
			if !sleepBackoff(p.ctx, attempt) {
				return p.ctx.Err()
			}
		}
		err := getRange(p.ctx, p.client, p.url, pt, f, p.tb)
		if err == nil {
			return nil
		}
		if errors.Is(err, errFallbackSingle) || isFatal(err) || errors.Is(err, context.Canceled) {
			return err
		}
		lastErr = err // 可重试：网络抖动 / 5xx
	}
	if lastErr == nil {
		lastErr = errors.New("下载失败")
	}
	return lastErr
}

// getRange 发一次区间请求并落盘；返回 errFallbackSingle 表示服务端没按 Range 走。
func getRange(ctx context.Context, client *http.Client, u string, pt *partState, f *os.File, tb *tokenBucket) error {
	off := pt.start + pt.done.Load()
	if off > pt.end {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Range", "bytes="+strconv.FormatInt(off, 10)+"-"+strconv.FormatInt(pt.end, 10))
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := statusErr(resp); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusPartialContent {
		return errFallbackSingle // 要了区间却回 200：不守 Range
	}
	w := &offsetWriter{f: f, off: off, tb: tb, ctx: ctx}
	if _, err := io.Copy(w, io.LimitReader(resp.Body, pt.end-off+1)); err != nil {
		return err
	}
	pt.done.Store(pt.end - pt.start + 1)
	return nil
}

// downloadSingle 单连接流式写 .part，完成 rename；支持从 .part 长度续传。
func downloadSingle(p dlParams, size int64) error {
	tmp := p.savePath + partSuffix
	// 续传起点：.part 现有长度；服务端不支持 Range 或 meta 不匹配则从 0 重来。
	var start int64
	if st, err := os.Stat(tmp); err == nil {
		start = st.Size()
	}
	if m := loadMeta(p.savePath); m == nil || m.Chunked || !strings.EqualFold(m.URL, p.url) || (size > 0 && m.Size != size) {
		start = 0
	}

	flags := os.O_CREATE | os.O_WRONLY
	if start == 0 {
		flags |= os.O_TRUNC
	} else {
		flags |= os.O_APPEND
	}
	f, err := os.OpenFile(tmp, flags, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	pt := &partState{index: 0, start: 0, end: -1}
	pt.done.Store(start)

	if err := streamSingle(p, f, pt, start, size); err != nil {
		saveMeta(p.savePath, &meta{URL: p.url, Size: size, Chunked: false})
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, p.savePath); err != nil {
		// Windows rename 不覆盖已存在目标：先删再rename
		_ = os.Remove(p.savePath)
		if err2 := os.Rename(tmp, p.savePath); err2 != nil {
			return err2
		}
	}
	removeMeta(p.savePath)
	final := pt.done.Load()
	emitProgressSingle(p, size, final)
	return nil
}

func streamSingle(p dlParams, f *os.File, pt *partState, start, size int64) error {
	var lastErr error
	for attempt := 0; attempt <= p.maxRetry; attempt++ {
		if p.ctx.Err() != nil {
			return p.ctx.Err()
		}
		if attempt > 0 {
			if !sleepBackoff(p.ctx, attempt) {
				return p.ctx.Err()
			}
		}
		err := streamOnce(p, f, pt, start, size)
		if err == nil {
			return nil
		}
		if isFatal(err) || errors.Is(err, context.Canceled) {
			return err
		}
		lastErr = err
		// 重试前把起点同步成当前已落盘长度（可能已推进）
		start = pt.done.Load()
	}
	if lastErr == nil {
		lastErr = errors.New("下载失败")
	}
	return lastErr
}

func streamOnce(p dlParams, f *os.File, pt *partState, start, size int64) error {
	req, err := http.NewRequestWithContext(p.ctx, http.MethodGet, p.url, nil)
	if err != nil {
		return err
	}
	if start > 0 {
		req.Header.Set("Range", "bytes="+strconv.FormatInt(start, 10)+"-")
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if err := statusErr(resp); err != nil {
		return err
	}
	// 要求续传却回 200：从头覆盖写（start 归零并截断）。
	if start > 0 && resp.StatusCode == http.StatusOK {
		if err := f.Truncate(0); err != nil {
			return err
		}
		if _, err := f.Seek(0, io.SeekStart); err != nil {
			return err
		}
		pt.done.Store(0)
		start = 0
	}
	w := &offsetWriter{f: f, off: start, tb: p.tb, ctx: p.ctx}
	w.onWrite = func(n int64) {
		pt.done.Add(n)
		emitProgressSingle(p, size, pt.done.Load())
	}
	if size > 0 {
		_, err = io.Copy(w, io.LimitReader(resp.Body, size-start))
	} else {
		_, err = io.Copy(w, resp.Body)
	}
	return err
}

/* ============================ 探测 ============================ */

// probe 取内容长度与是否支持 Range：HEAD 优先，退化 GET+Range:bytes=0-0。
func probe(ctx context.Context, client *http.Client, u string) (size int64, acceptRanges bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u, nil)
	if err != nil {
		return 0, false, err
	}
	resp, err := client.Do(req)
	if err == nil {
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 400 {
			size = parseSize(resp.Header.Get("Content-Length"))
			acceptRanges = rangeOK(resp.Header.Get("Accept-Ranges"))
			if size > 0 {
				return size, acceptRanges, nil
			}
		}
	}
	// HEAD 不给长度（或不被允许）：用一次极小的区间 GET 兜底探长度。
	return probeByRange(ctx, client, u)
}

func probeByRange(ctx context.Context, client *http.Client, u string) (int64, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, false, err
	}
	req.Header.Set("Range", "bytes=0-0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()
	if err := statusErr(resp); err != nil {
		return 0, false, err
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1))
	switch resp.StatusCode {
	case http.StatusPartialContent:
		return parseTotal(resp.Header.Get("Content-Range")), true, nil
	case http.StatusOK:
		return parseSize(resp.Header.Get("Content-Length")), rangeOK(resp.Header.Get("Accept-Ranges")), nil
	}
	return 0, false, nil
}

/* ============================ 令牌桶限速 ============================ */

// tokenBucket 极简令牌桶：rate=0 表示不限速。按字节计，写循环前 wait(n)。
// parent 指向全局桶（每任务桶挂全局桶之下）：wait 先过全局层再过本层，
// 于是「全局 + 每任务」两层限速天然叠加，取更严的一道生效。
type tokenBucket struct {
	mu     sync.Mutex
	rate   float64 // 字节/秒
	cap    float64
	tokens float64
	last   time.Time
	parent *tokenBucket
}

// newTokenBucket 造桶；ctx 为在跑任务的取消上下文（仅任务层桶传，全局桶传 nil）：
// 攒令牌睡觉期间也要能被暂停/停止叫醒。
func newTokenBucket(ctx context.Context, bps int64) *tokenBucket {
	if bps <= 0 {
		return &tokenBucket{}
	}
	return &tokenBucket{rate: float64(bps), cap: float64(bps), tokens: float64(bps), last: time.Now()}
}

// setRate 运行期改限速（0=解除）；重置容量以尽快反映新速率。
func (tb *tokenBucket) setRate(bps int64) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	if bps <= 0 {
		tb.rate, tb.cap, tb.tokens = 0, 0, 0
		return
	}
	tb.rate, tb.cap = float64(bps), float64(bps)
	if tb.tokens > tb.cap {
		tb.tokens = tb.cap
	}
	tb.last = time.Now()
}

// wait 阻塞到攒够 n 个令牌（先过全局父桶，再过本桶）。
// ctx 贯穿等待：暂停/停止不能卡在攒令牌的觉里——之前这一睡会把 runner 拖住，
// 并发额度迟迟不释放，排队任务就一直「点了开始没反应」。
func (tb *tokenBucket) wait(ctx context.Context, n int64) {
	if tb == nil {
		return
	}
	if tb.parent != nil {
		tb.parent.wait(ctx, n)
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return
		default:
		}
	}
	tb.mu.Lock()
	if tb.rate <= 0 {
		tb.mu.Unlock()
		return
	}
	for {
		now := time.Now()
		tb.tokens += now.Sub(tb.last).Seconds() * tb.rate
		if tb.tokens > tb.cap {
			tb.tokens = tb.cap
		}
		tb.last = now
		if tb.tokens >= float64(n) {
			tb.tokens -= float64(n)
			tb.mu.Unlock()
			return
		}
		need := (float64(n) - tb.tokens) / tb.rate
		tb.mu.Unlock()
		if !sleepCtx(ctx, time.Duration(need*float64(time.Second))+time.Millisecond) {
			return
		}
		tb.mu.Lock()
	}
}

// offsetWriter 把响应流按偏移写进文件，写前过令牌桶（限速）、写后回调进度。
// ctx 取消时直接回错，让 io.Copy 尽早退出（暂停响应时机）。
type offsetWriter struct {
	f       *os.File
	off     int64
	tb      *tokenBucket
	ctx     context.Context
	onWrite func(n int64)
}

func (w *offsetWriter) Write(p []byte) (int, error) {
	if w.ctx != nil {
		select {
		case <-w.ctx.Done():
			return 0, w.ctx.Err()
		default:
		}
	}
	w.tb.wait(w.ctx, int64(len(p)))
	n, err := w.f.WriteAt(p, w.off)
	w.off += int64(n)
	if n > 0 && w.onWrite != nil {
		w.onWrite(int64(n))
	}
	return n, err
}

/* ============================ HTTP client ============================ */

// newClient 造带代理与超时的 client。proxy 为空=直连；解析失败也退回直连（不因代理配错而全下不动）。
func newClient(proxy string, timeout time.Duration) *http.Client {
	tr := &http.Transport{
		MaxIdleConns:        32,
		MaxIdleConnsPerHost: 16,
		IdleConnTimeout:     60 * time.Second,
	}
	if proxy != "" {
		if pu, err := url.Parse(proxy); err == nil && pu.Scheme != "" {
			tr.Proxy = http.ProxyURL(pu)
		}
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirect {
				return http.ErrUseLastResponse
			}
			req.Header.Set("User-Agent", defaultUA)
			return nil
		},
	}
}

/* ============================ 通用小工具 ============================ */

// statusErr 把非 2xx/3xx 归一成 error，并区分致命（4xx）与可重试（5xx/408/429）。
func statusErr(resp *http.Response) error {
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode >= 300 && resp.StatusCode < 400:
		return nil // client 会自动跟随；跟到上限则停在最后一跳
	case resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests:
		return &httpError{code: resp.StatusCode, retry: true}
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		return &httpError{code: resp.StatusCode, retry: false} // 4xx 致命，不重试
	default:
		return &httpError{code: resp.StatusCode, retry: true} // 5xx 可重试
	}
}

// httpError 带「是否可重试」标记的 HTTP 状态错误。
type httpError struct {
	code  int
	retry bool
}

func (e *httpError) Error() string { return "HTTP " + strconv.Itoa(e.code) }

// isFatal 判断错误是否不该重试（4xx）。
func isFatal(err error) bool {
	var he *httpError
	return errors.As(err, &he) && !he.retry
}

// sleepBackoff 指数退避；ctx 取消返回 false。
func sleepBackoff(ctx context.Context, attempt int) bool {
	d := time.Duration(1<<uint(attempt-1)) * 500 * time.Millisecond
	if d > 15*time.Second {
		d = 15 * time.Second
	}
	return sleepCtx(ctx, d)
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	if ctx == nil {
		time.Sleep(d)
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func parseSize(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if n < 0 {
		return 0
	}
	return n
}

// parseTotal 从 "bytes 0-0/12345" 取总长。
func parseTotal(contentRange string) int64 {
	i := strings.LastIndexByte(contentRange, '/')
	if i < 0 {
		return 0
	}
	return parseSize(contentRange[i+1:])
}

func rangeOK(v string) bool {
	return strings.EqualFold(strings.TrimSpace(v), "bytes")
}

// baseExt 取小写扩展名（不含点）。
func baseExt(name string) string {
	return strings.ToLower(strings.TrimPrefix(path.Ext(name), "."))
}

// nameFromURL 从 URL 推文件名（去 query/fragment），取不到用时间戳兜底。
func nameFromURL(u string) string {
	if x, err := url.Parse(u); err == nil {
		if base := path.Base(x.Path); base != "" && base != "/" && base != "." {
			return sanitizeName(base)
		}
	}
	return "download-" + strconv.FormatInt(time.Now().Unix(), 10)
}

// sanitizeName 去掉路径分隔与控制字符，防目录穿越与非法文件名。
func sanitizeName(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = filepath.Base(name)
	name = strings.Map(func(r rune) rune {
		switch r {
		case ':', '*', '?', '"', '<', '>', '|', 0:
			return '_'
		}
		return r
	}, name)
	if strings.TrimSpace(name) == "" || name == "." || name == ".." {
		return "download"
	}
	return name
}

// uniqueID 由 url+时间 生成稳定又不冲突的任务 id。
func uniqueID(u string) string {
	sum := md5.Sum([]byte(u + strconv.FormatInt(time.Now().UnixNano(), 10)))
	return hex.EncodeToString(sum[:])[:12]
}
