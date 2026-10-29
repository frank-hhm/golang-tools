// Package siteclone 仿站/扒站：给定 URL，抓「主页」或「整站」的 HTML/CSS/JS/图片等资源，
// 存成本地目录树，并把页面里的链接改写成本地相对路径，使离线副本可直接双击 index.html 打开。
//
// 只做静态抓取：不执行 JS、不跑 SSR，纯按 HTML/CSS 源码里出现的 URL 逐个下载。
// 整站模式仅在同 host 内跟 <a> 链接（深度/页数有上限防跑飞），但 CSS/JS/图片等资源允许来自 CDN 等外域。
// 绑定层在 services/app/siteclone.go，本包不碰 UI 与 Wails。
package siteclone

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Options 一次仿站任务的参数（字段全导出，供 Wails 生成前端模型）。
type Options struct {
	URL         string `json:"url"`
	Mode        string `json:"mode"`        // page=仅主页 | site=整站
	MaxDepth    int    `json:"maxDepth"`    // site 模式最大深度（入口页记为 0）；<=0 走默认
	MaxPages    int    `json:"maxPages"`    // site 模式最大页数；<=0 走默认
	Concurrency int    `json:"concurrency"` // 资源并发下载数；<=0 走默认
	TimeoutSec  int    `json:"timeoutSec"`  // 单请求超时秒；<=0 走默认
	UserAgent   string `json:"userAgent"`   // 空走默认 UA
	OutDir      string `json:"outDir"`      // 输出根目录（绝对路径）
	Rewrite     bool   `json:"rewrite"`     // 是否把链接改写成本地相对路径（离线可打开）
}

// Progress 进度事件负载。
type Progress struct {
	JobID      string `json:"jobId"`
	Stage      string `json:"stage"` // start|fetch|parse|asset|rewrite|done|error|canceled
	URL        string `json:"url"`
	PagesDone  int    `json:"pagesDone"`
	PagesTotal int    `json:"pagesTotal"`
	Files      int    `json:"files"`
	Bytes      int64  `json:"bytes"`
	Status     string `json:"status"` // ok|skip|fail
	Msg        string `json:"msg"`
	Root       string `json:"root"` // 输出根目录（done/canceled 事件带上，供前端「打开目录」）
}

// Result 任务结束汇总。
type Result struct {
	Root      string `json:"root"`      // 站点本地根目录（OutDir/<host>）
	Pages     int    `json:"pages"`     // 抓取的 HTML 页数
	Files     int    `json:"files"`     // 落盘文件总数（含 HTML 与资源）
	Bytes     int64  `json:"bytes"`     // 落盘总字节
	IndexFile string `json:"indexFile"` // 入口页本地路径
}

// 各类上限与默认值：都是「防跑飞」的护栏，不是功能约束。
const (
	defaultUA       = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"
	defaultDepth    = 3
	defaultMaxPages = 200
	defaultConc     = 6
	defaultTimeout  = 20
	maxRedirect     = 10
	maxFileBytes    = int64(25 << 20) // 单文件 25MB 封顶，超了跳过（保留原链接）
	maxAssets       = int64(5000)     // 资源总数封顶
)

var (
	urlRE     = regexp.MustCompile(`(?i)url\(\s*['"]?([^'")]+?)['"]?\s*\)`)
	importRE  = regexp.MustCompile(`(?i)@import\s+(?:"([^"]*)"|'([^']*)')`)
	attrIsURL = map[string]bool{"href": true, "src": true, "poster": true, "data": true, "xlink:href": true}
	attrIsSrc = map[string]bool{"srcset": true, "imagesrcset": true}
)

// clone 一次任务的运行态。
type clone struct {
	ctx    context.Context
	opts   Options
	client *http.Client
	emit   func(Progress)
	jobID  string

	sem chan struct{}  // 并发闸门，容量=Concurrency
	wg  sync.WaitGroup // 等资源 worker 全部收尾

	mu        sync.Mutex
	local     map[string]string // absKey -> 本地路径（页面与资源共用一张表，改写时统一查）
	pending   map[string]bool   // 已入队的资源 absKey，防并发重复下载
	entryHost string            // 入口站 host（同域判定用），首个页面确定
	siteRoot  string            // OutDir/<entryHost>，index 与「打开目录」落点

	// 改写阶段要处理的文件（页面循环单线程写，资源 worker 写 cssFiles 需加锁）
	htmlFiles []fileRef
	cssFiles  []fileRef

	pagesDone  int
	fileCount  int64
	byteCount  int64
	assetCount int64
}

type fileRef struct {
	lp   string
	base *url.URL
}

// Run 执行一次仿站，返回汇总。ctx 取消会中断抓取并返回已完成的部分。
// 只有「参数非法」才返回 error；抓取过程中的单页/单资源失败只记进度，不中断整体。
func Run(ctx context.Context, opts Options, jobID string, emit func(Progress)) (Result, error) {
	if err := normalize(&opts); err != nil {
		return Result{}, err
	}
	entry, err := url.Parse(opts.URL)
	if err != nil {
		return Result{}, fmt.Errorf("URL 无效: %w", err)
	}
	c := &clone{
		ctx:     ctx,
		opts:    opts,
		emit:    emit,
		jobID:   jobID,
		sem:     make(chan struct{}, opts.Concurrency),
		local:   map[string]string{},
		pending: map[string]bool{},
		client:  newClient(opts.UserAgent, time.Duration(opts.TimeoutSec)*time.Second),
	}
	c.emitProg(Progress{Stage: "start", URL: opts.URL, Msg: "开始抓取"})
	c.bfs(entry.String())
	c.wg.Wait()
	if opts.Rewrite {
		c.emitProg(Progress{Stage: "rewrite", Msg: "改写本地链接"})
		c.rewriteAll()
	}
	res := Result{
		Root:      c.siteRoot,
		Pages:     c.pagesDone,
		Files:     int(atomic.LoadInt64(&c.fileCount)),
		Bytes:     atomic.LoadInt64(&c.byteCount),
		IndexFile: c.local[absKey(entry)],
	}
	if ctx.Err() != nil {
		c.emitProg(Progress{Stage: "canceled", Root: c.siteRoot, Msg: "已取消"})
	} else {
		c.emitProg(Progress{Stage: "done", Root: c.siteRoot, Msg: "完成"})
	}
	return res, nil
}

// normalize 校验并补齐默认值。
func normalize(o *Options) error {
	o.URL = strings.TrimSpace(o.URL)
	if o.URL == "" {
		return fmt.Errorf("请先填写要抓取的 URL")
	}
	if !strings.Contains(o.URL, "://") {
		o.URL = "http://" + o.URL
	}
	if strings.TrimSpace(o.OutDir) == "" {
		return fmt.Errorf("请先选择输出目录")
	}
	if o.Mode != "site" {
		o.Mode = "page"
	}
	if o.MaxDepth <= 0 {
		o.MaxDepth = defaultDepth
	}
	if o.MaxPages <= 0 {
		o.MaxPages = defaultMaxPages
	}
	if o.Concurrency <= 0 {
		o.Concurrency = defaultConc
	}
	if o.Concurrency > 16 {
		o.Concurrency = 16
	}
	if o.TimeoutSec <= 0 {
		o.TimeoutSec = defaultTimeout
	}
	if strings.TrimSpace(o.UserAgent) == "" {
		o.UserAgent = defaultUA
	}
	return nil
}

// newClient 带 UA、超时与跳数上限的 HTTP 客户端。
func newClient(ua string, timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirect {
				return http.ErrUseLastResponse
			}
			req.Header.Set("User-Agent", ua)
			return nil
		},
	}
}

// bfs 按深度优先遍历页面：入口页 depth=0，site 模式在同 host 内跟 <a>。
func (c *clone) bfs(entryAbs string) {
	type item struct {
		abs   string
		depth int
	}
	visited := map[string]bool{entryAbs: true}
	queue := []item{{entryAbs, 0}}

	for len(queue) > 0 {
		if c.ctx.Err() != nil {
			return
		}
		it := queue[0]
		queue = queue[1:]

		u, err := url.Parse(it.abs)
		if err != nil {
			continue
		}
		c.emitProg(Progress{Stage: "fetch", URL: it.abs, PagesTotal: c.pagesDone + len(queue), Msg: "下载页面"})
		body, finalURL, ct, err := c.fetchPage(it.abs)
		if err != nil {
			c.emitProg(Progress{Stage: "error", URL: it.abs, Status: "fail", Msg: err.Error()})
			continue
		}
		// 确定站点根目录与同域基准（用入口页最终 host，兼容跳转）
		if c.entryHost == "" {
			c.entryHost = finalURL.Hostname()
			c.siteRoot = filepath.Join(c.opts.OutDir, sanitizeHost(finalURL.Host))
			_ = os.MkdirAll(c.siteRoot, 0o755)
		}
		// 非 HTML（如 .pdf/.zip 直链）：当作资源落盘，不解析
		if !isHTML(ct, finalURL) {
			lp := c.saveBody(finalURL, ct, body)
			c.setLocal(absKey(finalURL), lp)
			c.bumpFile(int64(len(body)))
			c.emitProg(Progress{Stage: "asset", URL: finalURL.String(), Status: "ok", Msg: "非页面文件，直接下载"})
			continue
		}

		lp := c.saveBody(finalURL, "text/html", body)
		c.setLocal(absKey(u), lp)
		c.setLocal(absKey(finalURL), lp)
		c.bumpFile(int64(len(body)))
		c.pagesDone++
		c.htmlFiles = append(c.htmlFiles, fileRef{lp, finalURL})
		c.emitProg(Progress{Stage: "parse", URL: finalURL.String(), PagesDone: c.pagesDone, PagesTotal: c.pagesDone + len(queue), Status: "ok", Msg: "解析页面"})

		pageLinks, assets := parseLinks(body)
		for _, a := range assets {
			if au, ok := resolveAbs(finalURL, a); ok {
				c.startAsset(absKey(au), finalURL.String())
			}
		}
		if c.opts.Mode == "site" && it.depth < c.opts.MaxDepth {
			for _, l := range pageLinks {
				au, ok := resolveAbs(finalURL, l)
				if !ok || !strings.EqualFold(au.Hostname(), c.entryHost) {
					continue
				}
				key := absKey(au)
				if visited[key] {
					continue
				}
				if c.pagesDone+len(queue) >= c.opts.MaxPages {
					continue
				}
				visited[key] = true
				queue = append(queue, item{key, it.depth + 1})
			}
		}
	}
}

// fetchPage 取一个页面的原始字节与最终 URL（跟随重定向）。
func (c *clone) fetchPage(abs string) ([]byte, *url.URL, string, error) {
	resp, err := c.get(abs, "")
	if err != nil {
		return nil, nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFileBytes+1))
	if err != nil {
		return nil, nil, "", err
	}
	if int64(len(body)) > maxFileBytes {
		return nil, nil, "", fmt.Errorf("页面过大，已跳过")
	}
	ct := resp.Header.Get("Content-Type")
	return body, resp.Request.URL, ct, nil
}

// startAsset 把一个资源 URL 排进并发下载队列；同一 URL 只下一次（占位去重）。
func (c *clone) startAsset(absKeyStr, referer string) {
	c.mu.Lock()
	_, seen := c.local[absKeyStr]
	dup := seen || c.pending[absKeyStr]
	if !dup {
		c.pending[absKeyStr] = true
	}
	c.mu.Unlock()
	if dup {
		return
	}
	if atomic.LoadInt64(&c.assetCount) >= maxAssets {
		return
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.sem <- struct{}{}
		defer func() { <-c.sem }()
		c.fetchAsset(absKeyStr, referer)
	}()
}

// fetchAsset 下载单个资源；CSS 会再解析其中 url()/@import 递归抓一层。
func (c *clone) fetchAsset(absURL, referer string) {
	if c.ctx.Err() != nil {
		return
	}
	resp, err := c.get(absURL, referer)
	if err != nil {
		c.emitProg(Progress{Stage: "asset", URL: absURL, Status: "fail", Msg: err.Error()})
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		c.emitProg(Progress{Stage: "asset", URL: absURL, Status: "fail", Msg: fmt.Sprintf("HTTP %d", resp.StatusCode)})
		return
	}
	ct := resp.Header.Get("Content-Type")
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFileBytes+1))
	if err != nil {
		c.emitProg(Progress{Stage: "asset", URL: absURL, Status: "fail", Msg: err.Error()})
		return
	}
	if int64(len(body)) > maxFileBytes {
		c.emitProg(Progress{Stage: "asset", URL: absURL, Status: "skip", Msg: "文件过大，保留原链接"})
		return
	}
	fu := resp.Request.URL
	lp := c.saveBody(fu, ct, body)
	c.setLocal(absURL, lp)
	c.setLocal(absKey(fu), lp)
	atomic.AddInt64(&c.assetCount, 1)
	c.bumpFile(int64(len(body)))
	c.emitProg(Progress{Stage: "asset", URL: absURL, Status: "ok", Msg: "下载资源"})

	if isCSS(ct) {
		c.mu.Lock()
		c.cssFiles = append(c.cssFiles, fileRef{lp, fu})
		c.mu.Unlock()
		for _, ref := range extractCSSURLs(string(body)) {
			if au, ok := resolveAbs(fu, ref); ok {
				c.startAsset(absKey(au), fu.String())
			}
		}
	}
}

// get 发一个 GET 请求，带 UA / Referer。
func (c *clone) get(abs, referer string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(c.ctx, http.MethodGet, abs, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.opts.UserAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	return c.client.Do(req)
}

// saveBody 按 URL 推导本地路径并落盘（自动建目录）。
func (c *clone) saveBody(u *url.URL, ct string, body []byte) string {
	lp := c.localPathFor(u, ct)
	_ = os.MkdirAll(filepath.Dir(lp), 0o755)
	_ = os.WriteFile(lp, body, 0o644)
	return lp
}

// localPathFor 把远程 URL 映射成本地路径：OutDir/<host>/<path>；目录/根 -> index.html；
// 带 query 的把 query 做短哈希拼进文件名；无扩展名按 Content-Type 兜底。
func (c *clone) localPathFor(u *url.URL, ct string) string {
	host := sanitizeHost(u.Host)
	p := strings.TrimPrefix(u.EscapedPath(), "/")
	segs := strings.Split(p, "/")
	for i := range segs {
		segs[i] = sanitizeSeg(segs[i])
	}
	// 目录引用（path 以 / 结尾或空）-> index.html
	if p == "" || strings.HasSuffix(u.Path, "/") || segs[len(segs)-1] == "" {
		segs = append(segs, "index.html")
	}
	name := segs[len(segs)-1]
	if q := u.RawQuery; q != "" {
		ext := filepath.Ext(name)
		base := strings.TrimSuffix(name, ext)
		name = fmt.Sprintf("%s.%s%s", base, md5hex(q)[:8], ext)
		segs[len(segs)-1] = name
	}
	if filepath.Ext(name) == "" {
		if e := extForCT(ct); e != "" {
			name += e
			segs[len(segs)-1] = name
		}
	}
	return filepath.Join(c.opts.OutDir, host, filepath.Join(segs...))
}

// ---------- 改写 ----------

// rewriteAll 把已落盘的 HTML/CSS 里的链接改写成本地相对路径。
func (c *clone) rewriteAll() {
	for _, hf := range c.htmlFiles {
		c.rewriteHTML(hf.lp, hf.base)
	}
	c.mu.Lock()
	css := append([]fileRef(nil), c.cssFiles...)
	c.mu.Unlock()
	for _, cf := range css {
		c.rewriteCSS(cf.lp, cf.base)
	}
}

func (c *clone) rewriteHTML(lp string, base *url.URL) {
	b, err := os.ReadFile(lp)
	if err != nil {
		return
	}
	doc, err := html.Parse(bytes.NewReader(b))
	if err != nil {
		return
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			for i := range n.Attr {
				k := strings.ToLower(n.Attr[i].Key)
				v := n.Attr[i].Val
				switch {
				case attrIsURL[k]:
					if nv, ok := c.rewriteURL(base, lp, v); ok {
						n.Attr[i].Val = nv
					}
				case attrIsSrc[k]:
					n.Attr[i].Val = c.rewriteSrcset(base, lp, v)
				case k == "style":
					n.Attr[i].Val = c.rewriteCSSRefs(base, lp, v)
				}
			}
			if n.DataAtom == atom.Style {
				for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
					if ch.Type == html.TextNode {
						ch.Data = c.rewriteCSSRefs(base, lp, ch.Data)
					}
				}
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(doc)
	var buf bytes.Buffer
	if err := html.Render(&buf, doc); err != nil {
		return
	}
	_ = os.WriteFile(lp, buf.Bytes(), 0o644)
}

func (c *clone) rewriteCSS(lp string, base *url.URL) {
	b, err := os.ReadFile(lp)
	if err != nil {
		return
	}
	s := c.rewriteCSSRefs(base, lp, string(b))
	s = importRE.ReplaceAllStringFunc(s, func(m string) string {
		g := importRE.FindStringSubmatch(m)
		if len(g) < 3 {
			return m
		}
		// 双引号命中在 g[1]，单引号命中在 g[2]（RE2 不支持 \1 反向引用，只能用两个分组）
		inner, quote := g[1], `"`
		if inner == "" {
			inner, quote = g[2], "'"
		}
		if inner == "" {
			return m
		}
		if nv, ok := c.rewriteURL(base, lp, inner); ok {
			return "@import " + quote + nv + quote
		}
		return m
	})
	_ = os.WriteFile(lp, []byte(s), 0o644)
}

// rewriteURL 把单个引用 URL 换成本地相对路径；未抓到（不在 local 表）则返回 false 保持原样。
func (c *clone) rewriteURL(base *url.URL, fromFile, ref string) (string, bool) {
	au, ok := resolveAbs(base, ref)
	if !ok {
		return "", false
	}
	if target, found := c.lookup(absKey(au)); found {
		return relURL(fromFile, target), true
	}
	return "", false
}

func (c *clone) rewriteSrcset(base *url.URL, fromFile, v string) string {
	parts := strings.Split(v, ",")
	for i, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if nv, ok := c.rewriteURL(base, fromFile, fields[0]); ok {
			fields[0] = nv
			parts[i] = strings.Join(fields, " ")
		}
	}
	return strings.Join(parts, ", ")
}

func (c *clone) rewriteCSSRefs(base *url.URL, fromFile, text string) string {
	return urlRE.ReplaceAllStringFunc(text, func(m string) string {
		g := urlRE.FindStringSubmatch(m)
		if len(g) < 2 {
			return m
		}
		if nv, ok := c.rewriteURL(base, fromFile, g[1]); ok {
			return "url(" + nv + ")"
		}
		return m
	})
}

// ---------- 解析 ----------

// parseLinks 扫一遍 HTML，返回页面链接（<a href>）与资源 URL（其余标签属性、style、<style>）。
func parseLinks(body []byte) (pageLinks, assets []string) {
	z := html.NewTokenizer(bytes.NewReader(body))
	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			return
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		t := z.Token()
		for _, a := range t.Attr {
			k := strings.ToLower(a.Key)
			v := a.Val
			switch t.DataAtom {
			case atom.A:
				if k == "href" {
					pageLinks = append(pageLinks, v)
				}
			case atom.Link:
				if k == "href" {
					assets = append(assets, v)
				}
			case atom.Script:
				if k == "src" {
					assets = append(assets, v)
				}
			case atom.Img:
				if k == "src" {
					assets = append(assets, v)
				} else if k == "srcset" {
					assets = append(assets, parseSrcset(v)...)
				}
			case atom.Source:
				if k == "src" {
					assets = append(assets, v)
				} else if k == "srcset" {
					assets = append(assets, parseSrcset(v)...)
				}
			case atom.Video, atom.Audio:
				if k == "src" || k == "poster" {
					assets = append(assets, v)
				}
			case atom.Embed:
				if k == "src" {
					assets = append(assets, v)
				}
			case atom.Object:
				if k == "data" {
					assets = append(assets, v)
				}
			}
			if k == "style" {
				assets = append(assets, extractCSSURLs(v)...)
			}
		}
		if t.DataAtom == atom.Style && z.Next() == html.TextToken {
			assets = append(assets, extractCSSURLs(z.Token().Data)...)
		}
	}
}

func parseSrcset(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		f := strings.Fields(strings.TrimSpace(p))
		if len(f) > 0 {
			out = append(out, f[0])
		}
	}
	return out
}

func extractCSSURLs(text string) []string {
	var out []string
	for _, m := range urlRE.FindAllStringSubmatch(text, -1) {
		if len(m) >= 2 {
			out = append(out, m[1])
		}
	}
	return out
}

// ---------- 小工具 ----------

// resolveAbs 把引用 ref 相对 base 解析成绝对 http(s) URL；data/js/mailto/blob 等一律拒绝。
func resolveAbs(base *url.URL, ref string) (*url.URL, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, false
	}
	low := strings.ToLower(ref)
	if strings.HasPrefix(low, "data:") || strings.HasPrefix(low, "javascript:") ||
		strings.HasPrefix(low, "mailto:") || strings.HasPrefix(low, "tel:") ||
		strings.HasPrefix(low, "blob:") || strings.HasPrefix(low, "#") {
		return nil, false
	}
	ru, err := url.Parse(ref)
	if err != nil {
		return nil, false
	}
	abs := base.ResolveReference(ru)
	if abs.Scheme != "http" && abs.Scheme != "https" {
		return nil, false
	}
	abs.Fragment = ""
	return abs, true
}

// absKey 归一化去重键：去 fragment、host 小写、目录补尾斜杠歧义交给 path 原样。
func absKey(u *url.URL) string {
	h := strings.ToLower(u.Host)
	p := u.Path
	if p == "" {
		p = "/"
	}
	q := ""
	if u.RawQuery != "" {
		q = "?" + u.RawQuery
	}
	return u.Scheme + "://" + h + p + q
}

func (c *clone) setLocal(key, lp string) {
	c.mu.Lock()
	c.local[key] = lp
	c.mu.Unlock()
}

func (c *clone) lookup(key string) (string, bool) {
	c.mu.Lock()
	v, ok := c.local[key]
	c.mu.Unlock()
	return v, ok
}

func (c *clone) bumpFile(n int64) {
	atomic.AddInt64(&c.fileCount, 1)
	atomic.AddInt64(&c.byteCount, n)
}

func (c *clone) emitProg(p Progress) {
	p.JobID = c.jobID
	p.PagesDone = c.pagesDone
	if p.Files == 0 {
		p.Files = int(atomic.LoadInt64(&c.fileCount))
	}
	if p.Bytes == 0 {
		p.Bytes = atomic.LoadInt64(&c.byteCount)
	}
	if c.emit != nil {
		c.emit(p)
	}
}

// relURL 计算从 fromFile 所在目录到 toFile 的相对路径（正斜杠，供 HTML/CSS 用）。
func relURL(fromFile, toFile string) string {
	rel, err := filepath.Rel(filepath.Dir(fromFile), toFile)
	if err != nil {
		return filepath.ToSlash(toFile)
	}
	return filepath.ToSlash(rel)
}

func isHTML(ct string, u *url.URL) bool {
	if strings.Contains(strings.ToLower(ct), "text/html") {
		return true
	}
	if ct == "" {
		p := strings.ToLower(u.Path)
		return strings.HasSuffix(p, ".html") || strings.HasSuffix(p, ".htm") || strings.HasSuffix(p, "/") || u.Path == ""
	}
	return false
}

func isCSS(ct string) bool {
	return strings.Contains(strings.ToLower(ct), "text/css")
}

// sanitizeHost host:port -> 合法目录名（: 换 _，去非法字符）。
func sanitizeHost(h string) string {
	h = strings.ToLower(strings.TrimSpace(h))
	h = strings.ReplaceAll(h, ":", "_")
	return sanitizeSeg(h)
}

// sanitizeSeg 清洗单个路径段：去 Windows 非法字符与控制符，去尾部点/空格，防穿越。
func sanitizeSeg(s string) string {
	s = strings.TrimSpace(s)
	if s == ".." || s == "." {
		return "_"
	}
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\', ':', '*', '?', '"', '<', '>', '|':
			b.WriteByte('_')
		default:
			if r < 0x20 {
				b.WriteByte('_')
				continue
			}
			b.WriteRune(r)
		}
	}
	out := strings.TrimRight(b.String(), ". ")
	if out == "" {
		return "_"
	}
	return out
}

func extForCT(ct string) string {
	ct = strings.ToLower(ct)
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = ct[:i]
	}
	ct = strings.TrimSpace(ct)
	switch ct {
	case "text/css":
		return ".css"
	case "application/javascript", "text/javascript", "application/x-javascript", "ecmascript":
		return ".js"
	case "text/html":
		return ".html"
	case "application/json":
		return ".json"
	case "image/png":
		return ".png"
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/svg+xml":
		return ".svg"
	case "image/x-icon", "image/vnd.microsoft.icon":
		return ".ico"
	case "font/woff":
		return ".woff"
	case "font/woff2":
		return ".woff2"
	case "font/ttf":
		return ".ttf"
	case "font/otf":
		return ".otf"
	case "application/pdf":
		return ".pdf"
	}
	return ""
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}
