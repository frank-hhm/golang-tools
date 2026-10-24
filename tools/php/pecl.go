package php

// ============================================================
// PECL 常用扩展在线获取（windows.php.net/downloads/pecl）
//   - 目录结构：/releases/<ext>/<ver>/php_<ext>-<ver>-<phpX.Y>-<nts|ts>-<vc|vs>NN>-<x64|x86>.zip
//   - /releases/ 只保留仍在维护的扩展版本；历史版本在 /releases/archives/
//   - 下载包内的 php_<ext>.dll 统一落到 <phpDir>/ext/（与官方发行包同目录），
//     随后可由扩展管理写入 php.ini 的 extension= 行
//   - 目录索引按扩展维度在后台并发拉取（命中缓存立即返回），避免阻塞界面：
//     未就绪时清单先显示「获取中」，前端轮询刷新
//   - 安装进度经 "php:ext-install" 事件推送（复用 InstallProgress，Version 字段填扩展名）
// ============================================================

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	peclReleasesBase = "https://windows.php.net/downloads/pecl/releases/"
	peclArchivesBase = "https://windows.php.net/downloads/pecl/releases/archives/"
	// peclScanLimit 每个扩展最多扫描的发布版本目录数（按版本号倒序）
	peclScanLimit = 8
	peclFetchConn = 4 // 并发抓取版本目录的数量
	peclCacheTTL  = 30 * time.Minute
)

// peclCatalog 扩展管理面板上展示的「常用可下载扩展」清单
var peclCatalog = []struct{ Name, Label, Desc string }{
	{"redis", "Redis", "Redis 客户端，缓存 / Session 存储必备"},
	{"apcu", "APCu", "用户数据缓存（opcode 缓存之外）"},
	{"igbinary", "igbinary", "二进制序列化，redis / memcached 常用搭档"},
	{"xdebug", "Xdebug", "断点调试与性能分析，需配合 IDE"},
	{"memcached", "Memcached", "Memcached 官方驱动，附带依赖 dll"},
	{"memcache", "Memcache", "Memcached 早期客户端（新项目建议 memcached）"},
	{"xlswriter", "xlswriter", "高性能 Excel 读写（导出报表常用）"},
	{"ssh2", "SSH2", "SSH / SFTP 协议支持，附带 libssh2.dll"},
	{"mongodb", "MongoDB", "MongoDB 驱动"},
	{"yaml", "YAML", "YAML 解析与生成"},
	{"geoip", "GeoIP", "IP 归属地查询"},
	{"swoole", "Swoole", "协程网络引擎（Windows 支持有限）"},
}

/* ============================ 类型 ============================ */

// RemoteExtensionInfo 一个可在 PECL 上获取的常用扩展
type RemoteExtensionInfo struct {
	Name        string `json:"name"`        // 扩展名，如 redis
	Label       string `json:"label"`       // 展示名
	Description string `json:"description"` // 用途说明
	Installed   bool   `json:"installed"`   // ext 目录已存在 php_<name>.dll
	Enabled     bool   `json:"enabled"`     // php.ini 已启用
	Downloaded  bool   `json:"downloaded"`  // dll 由 go-tools 下载（可删除）
	DLL         string `json:"dll"`         // 已存在时的文件名
	ExtVersion  string `json:"extVersion"`  // 已存在时记录的扩展版本
	Fetching    bool   `json:"fetching"`    // 线上索引获取中
	Matchable   bool   `json:"matchable"`   // 线上存在匹配当前 PHP 版本的包
	SeriesSeen  bool   `json:"seriesSeen"`  // 线上有该扩展的 Windows 包（未必含当前 PHP 版本）
	Available   string `json:"available"`   // 线上包覆盖的 PHP 产品线，如 8.0, 8.1, 8.2
	ExtVer      string `json:"extVer"`      // 匹配到的扩展版本
	URL         string `json:"url"`         // 匹配到的包地址
	Size        int64  `json:"size"`        // 包大小
	Reason      string `json:"reason"`      // 无法安装的原因
	Busy        bool   `json:"busy"`        // 正在下载 / 安装
}

// peclPackage 适配某个 PHP 产品线 + 线程安全构型的扩展包
type peclPackage struct {
	URL    string
	ExtVer string
	Series string
	Kind   string
	VS     string
}

// peclExtIndex 某扩展的目录扫描结果
type peclExtIndex struct {
	mu       sync.Mutex
	Packages map[string]peclPackage // "<series>|<nts|ts>" → 最新包
	Series   map[string]bool        // 扫描范围内出现过的产品线
	Kinds    map[string]bool        // 扫描范围内出现过的构型
	Err      string                 // 目录整体拉取失败的原因
	Ready    bool                   // 扫描已完成
	Deep     bool                   // 已深度扫描过全部历史版本
	at       time.Time
	forceAt  time.Time // 上次手动刷新的时间，避免 20s 内重复刷
	inflight bool
}

/* ============================ 目录扫描 ============================ */

var (
	extZipRe = regexp.MustCompile(`href="(php_([a-z0-9_]+)-(\d+\.\d+(?:\.\d+)?)-(\d+\.\d+)-(nts|ts)-(vc\d+|vs\d+)-(x64|x86)\.zip)"`)
	extDirRe = regexp.MustCompile(`href="(\d+\.\d+(?:\.\d+)?)/"`)

	peclIdxMu sync.Mutex
	peclIdx   = map[string]*peclExtIndex{}
	peclBusy  sync.Map // ext → struct{}，正在安装中的扩展
)

// phpVSFor PHP 产品线要求的编译器版本（PECL 包名中的 vs/vc 号）
func phpVSFor(series string) string {
	switch series {
	case "7.4":
		return "vc15"
	case "8.0", "8.1", "8.2":
		return "vs16"
	default: // 8.3+
		return "vs17"
	}
}

func peclIdxOf(ext string) *peclExtIndex {
	peclIdxMu.Lock()
	defer peclIdxMu.Unlock()
	it, ok := peclIdx[ext]
	if !ok {
		it = &peclExtIndex{Packages: map[string]peclPackage{}, Series: map[string]bool{}, Kinds: map[string]bool{}}
		peclIdx[ext] = it
	}
	return it
}

// peclIndex 取扩展索引；未就绪则后台拉取并立即返回（调用方据 Ready 显示「获取中」）
func peclIndex(ext string, force bool) *peclExtIndex {
	return startPeclScan(ext, force, peclScanLimit)
}

// startPeclScan 后台启动索引扫描；limitVers<=0 表示深度查找（扫描全部历史版本）
func startPeclScan(ext string, force bool, limitVers int) *peclExtIndex {
	it := peclIdxOf(ext)
	it.mu.Lock()
	need := it.inflight == false && (!it.Ready || time.Since(it.at) > peclCacheTTL)
	if limitVers <= 0 && !need && !it.inflight {
		need = true // 深度扫描不受缓存时限限制
	}
	if force && !need && time.Since(it.forceAt) > 20*time.Second {
		need = true
	}
	if need {
		it.inflight = true
		if force {
			it.forceAt = time.Now()
		}
	}
	it.mu.Unlock()
	if need {
		go refreshPeclIndex(ext, it, limitVers)
	}
	return it
}

// waitPeclIndex 阻塞等待扩展索引就绪（安装前需要确切拿到包地址）
func waitPeclIndex(ext string, force bool, timeout time.Duration) *peclExtIndex {
	it := peclIndex(ext, force)
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		it.mu.Lock()
		ready := !it.inflight && it.Ready
		it.mu.Unlock()
		if ready {
			return it
		}
		time.Sleep(200 * time.Millisecond)
	}
	return it
}

// lookupPeclPackage 安装前的包定位：快扫 → 深度扫描（覆盖仅存在于历史目录的旧版包）
func lookupPeclPackage(ext, series, kind string) (*peclPackage, error) {
	it := waitPeclIndex(ext, false, 60*time.Second)
	it.mu.Lock()
	p, ok := it.Packages[series+"|"+kind]
	errText := it.Err
	it.mu.Unlock()
	if ok {
		return &p, nil
	}
	if errText != "" {
		return nil, fmt.Errorf("获取 %s 索引失败：%s", ext, errText)
	}
	// 深度查找：扫描 releases + archives 的全部版本目录（较慢，仅在安装/手动触发时执行）
	startPeclScan(ext, true, 0)
	it = waitPeclIndex(ext, false, 5*time.Minute)
	it.mu.Lock()
	p, ok = it.Packages[series+"|"+kind]
	errText = it.Err
	it.mu.Unlock()
	if !ok {
		if errText != "" {
			return nil, fmt.Errorf("获取 %s 索引失败：%s", ext, errText)
		}
		return nil, fmt.Errorf("未找到 %s 适配 PHP %s（%s）的 Windows 包", ext, series, strings.ToUpper(kind))
	}
	return &p, nil
}

func refreshPeclIndex(ext string, it *peclExtIndex, limitVers int) {
	deep := limitVers <= 0
	defer func() {
		it.mu.Lock()
		it.inflight = false
		it.Ready = true
		if deep {
			it.Deep = true
		}
		it.at = time.Now()
		it.mu.Unlock()
	}()
	pkgs := map[string]peclPackage{}
	series := map[string]bool{}
	kinds := map[string]bool{}
	var errs []string
	for _, base := range []string{peclReleasesBase, peclArchivesBase} {
		body, err := httpGet(base + ext + "/")
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		scanPeclVers(base, ext, body, pkgs, series, kinds, limitVers)
	}
	it.mu.Lock()
	for k, v := range pkgs {
		it.Packages[k] = v
	}
	for k := range series {
		it.Series[k] = true
	}
	for k := range kinds {
		it.Kinds[k] = true
	}
	if len(errs) == 2 && len(it.Packages) == 0 {
		it.Err = firstLine(errs[0])
	} else if len(it.Packages) > 0 {
		it.Err = ""
	}
	it.mu.Unlock()
}

// scanPeclVers 扫描扩展目录下的版本（并发拉取），归并每个 "<series>|<kind>" 的最新包。
// limitVers<=0 表示不限量（深度查找）。
func scanPeclVers(base, ext, body string, pkgs map[string]peclPackage, series, kinds map[string]bool, limitVers int) int {
	seen := map[string]bool{}
	vers := []string{}
	for _, m := range extDirRe.FindAllStringSubmatch(body, -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			vers = append(vers, m[1])
		}
	}
	if len(vers) == 0 {
		return 0
	}
	// 目录页按日期排序不可靠，按版本号倒序取最近若干个
	sort.Slice(vers, func(i, j int) bool { return compareVersion(vers[i], vers[j]) > 0 })
	if limitVers > 0 && len(vers) > limitVers {
		vers = vers[:limitVers]
	}
	type vres struct {
		ver  string
		rows [][]string
	}
	ch := make(chan vres, len(vers))
	sem := make(chan struct{}, peclFetchConn)
	var wg sync.WaitGroup
	for _, v := range vers {
		wg.Add(1)
		go func(v string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			sub, err := httpGet(base + ext + "/" + v + "/")
			if err != nil {
				return
			}
			ch <- vres{ver: v, rows: extZipRe.FindAllStringSubmatch(sub, -1)}
		}(v)
	}
	wg.Wait()
	close(ch)

	// 每个 (ext, series, kind) 只留版本号最高的包
	best := map[string]peclPackage{}
	for r := range ch {
		for _, m := range r.rows {
			if m[2] != ext || m[7] != "x64" { // 扩展名一致，且本项目统一使用 64 位 PHP
				continue
			}
			series[m[4]] = true
			kinds[m[5]] = true
			cand := peclPackage{URL: base + ext + "/" + r.ver + "/" + m[1], ExtVer: m[3], Series: m[4], Kind: m[5], VS: m[6]}
			key := m[4] + "|" + m[5]
			if cur, ok := best[key]; !ok || betterExtVer(cur.ExtVer, cand.ExtVer) {
				best[key] = cand
			}
		}
	}
	for k, v := range best {
		pkgs[k] = v
	}
	return len(best)
}

// betterExtVer a 是否比 cur 版本更高
func betterExtVer(cur, a string) bool {
	if a == "" {
		return false
	}
	if cur == "" {
		return true
	}
	return compareVersion(a, cur) > 0
}

/* ============================ 对外：清单 ============================ */

// RefreshExtCatalog 丢弃索引缓存，强制重新拉取线上 PECL 目录
func RefreshExtCatalog() {
	peclIdxMu.Lock()
	peclIdx = map[string]*peclExtIndex{}
	peclIdxMu.Unlock()
}

// ExtCatalog 返回常用扩展的可下载 / 已安装状态。force=true 重新拉取线上索引。
// 索引拉取在后台进行：首次调用通常返回 Fetching=true，前端轮询即可。
func ExtCatalog(id string, force bool) ([]RemoteExtensionInfo, error) {
	cfg, err := findInstance(id)
	if err != nil {
		return nil, err
	}
	exe := phpExe(cfg.Version, cfg.NTS)
	if exe == "" {
		exe = phpExe(cfg.Version, !cfg.NTS)
	}
	if exe == "" {
		return nil, fmt.Errorf("版本 %s 未安装", cfg.Version)
	}
	phpDir := filepath.Dir(exe)
	extDir := filepath.Join(phpDir, "ext")
	series := majorSeries(cfg.Version)
	kind := "ts"
	if cfg.NTS {
		kind = "nts"
	}
	text, _ := GetConfigText(id)

	out := make([]RemoteExtensionInfo, 0, len(peclCatalog))
	for _, c := range peclCatalog {
		item := RemoteExtensionInfo{Name: c.Name, Label: c.Label, Description: c.Desc}
		dll := filepath.Join(extDir, "php_"+c.Name+".dll")
		if st, err := os.Stat(dll); err == nil && !st.IsDir() {
			item.Installed = true
			item.DLL = "php_" + c.Name + ".dll"
			item.Downloaded = isDownloadedExt(dll)
			item.ExtVersion = downloadedExtVersion(dll)
			item.Enabled = iniHasActiveExt(text, c.Name)
			out = append(out, item)
			continue
		}
		_, busy := peclBusy.Load(c.Name)
		item.Busy = busy
		it := peclIndex(c.Name, force)
		it.mu.Lock()
		p, ok := it.Packages[series+"|"+kind]
		ready := it.Ready && !it.inflight
		errText := it.Err
		hasAny := len(it.Packages) > 0
		seriesSeen := it.Series[series]
		avail := sortedSeries(it.Series)
		it.mu.Unlock()
		item.Available = strings.Join(avail, ", ")
		item.SeriesSeen = seriesSeen
		if !ready {
			item.Fetching = true
			item.Reason = "正在获取线上索引…"
			out = append(out, item)
			continue
		}
		if errText != "" && !hasAny {
			item.Reason = errText
			out = append(out, item)
			continue
		}
		if !ok {
			item.Reason = peclMissReason(c.Name, series, kind, seriesSeen, avail, it)
			out = append(out, item)
			continue
		}
		if want := phpVSFor(series); !strings.EqualFold(p.VS, want) {
			item.Reason = fmt.Sprintf("包编译器 %s 与 PHP %s 要求的 %s 不一致，可能无法加载", p.VS, series, want)
		}
		item.Matchable = true
		item.ExtVer = p.ExtVer
		item.URL = p.URL
		if ok, size := headExists(p.URL); ok {
			item.Size = size
		}
		out = append(out, item)
	}
	return out, nil
}

// peclMissReason 无匹配包时给出可操作的解释
func peclMissReason(ext, series, kind string, seriesSeen bool, avail []string, it *peclExtIndex) string {
	prettyKind := map[bool]string{true: "NTS", false: "ZTS"}[kind == "nts"]
	other := "ts"
	if kind == "ts" {
		other = "nts"
	}
	it.mu.Lock()
	_, otherOK := it.Packages[series+"|"+other]
	it.mu.Unlock()
	if otherOK {
		return fmt.Sprintf("PHP %s 仅提供 %s 构型的包，请改用 %s 构型的 PHP 实例", series, strings.ToUpper(other), prettyKind)
	}
	if seriesSeen {
		// 近期发布未含当前版本：安装时会自动深度查找历史版本
		return fmt.Sprintf("近期发布包未提供 PHP %s 版本（现有：%s），安装时会自动查找历史版本", series, strings.Join(avail, ", "))
	}
	return fmt.Sprintf("暂无适配 PHP %s（%s）的 Windows 包（现有：%s）", series, prettyKind, strings.Join(avail, ", "))
}

// sortedSeries 产品线升序排列，便于展示「线上提供哪些 PHP 版本的包」
func sortedSeries(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return compareVersion(out[i], out[j]) < 0 })
	return out
}

/* ============================ 对外：下载安装 ============================ */

// InstallExtension 异步下载并安装扩展 dll 到 <phpDir>/ext/；enable=true 时顺带写入 php.ini。
// 进度经 "php:ext-install" 事件推送。
func InstallExtension(id, ext string, enable bool) error {
	cfg, err := findInstance(id)
	if err != nil {
		return err
	}
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" || strings.ContainsAny(ext, `/\ .`) {
		return fmt.Errorf("非法扩展名: %s", ext)
	}
	exe := phpExe(cfg.Version, cfg.NTS)
	if exe == "" {
		exe = phpExe(cfg.Version, !cfg.NTS)
	}
	if exe == "" {
		return fmt.Errorf("版本 %s 未安装", cfg.Version)
	}
	series := majorSeries(cfg.Version)
	kind := "ts"
	if cfg.NTS {
		kind = "nts"
	}
	p, err := lookupPeclPackage(ext, series, kind)
	if err != nil {
		return err
	}
	if _, busy := peclBusy.Load(ext); busy {
		return fmt.Errorf("扩展 %s 正在安装中", ext)
	}
	peclBusy.Store(ext, struct{}{})
	go installExtAsync(id, ext, p.URL, filepath.Dir(exe), enable)
	return nil
}

// ExtensionInstalling 扩展当前是否正在下载安装（供前端轮询）
func ExtensionInstalling(ext string) bool {
	_, busy := peclBusy.Load(strings.ToLower(strings.TrimSpace(ext)))
	return busy
}

// ExtSearchResult 深度查找结果
type ExtSearchResult struct {
	Found  bool   `json:"found"`
	ExtVer string `json:"extVer"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	Reason string `json:"reason"`
}

// SearchExtension 深度扫描该扩展的全部历史版本（releases + archives），
// 用于「近期发布包未覆盖当前 PHP 版本」时找回旧版安装包。耗时较长（十秒级）。
func SearchExtension(id, ext string) (*ExtSearchResult, error) {
	cfg, err := findInstance(id)
	if err != nil {
		return nil, err
	}
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return nil, fmt.Errorf("扩展名不能为空")
	}
	series := majorSeries(cfg.Version)
	kind := "ts"
	if cfg.NTS {
		kind = "nts"
	}
	out := &ExtSearchResult{}
	p, err := searchPeclOnce(ext, series, kind)
	if err != nil {
		return nil, err
	}
	if p == nil {
		out.Reason = fmt.Sprintf("已扫描全部历史版本，未找到适配 PHP %s（%s）的 %s 包", series, strings.ToUpper(kind), ext)
		return out, nil
	}
	out.Found = true
	out.ExtVer = p.ExtVer
	out.URL = p.URL
	if ok, size := headExists(p.URL); ok {
		out.Size = size
	}
	return out, nil
}

// searchPeclOnce 同步执行一次深度扫描并取回匹配包
func searchPeclOnce(ext, series, kind string) (*peclPackage, error) {
	it := peclIdxOf(ext)
	it.mu.Lock()
	deep := it.Deep
	it.mu.Unlock()
	if !deep {
		startPeclScan(ext, true, 0)
		it = waitPeclIndex(ext, false, 5*time.Minute)
	}
	it.mu.Lock()
	defer it.mu.Unlock()
	p, ok := it.Packages[series+"|"+kind]
	if !ok {
		if it.Err != "" && len(it.Packages) == 0 {
			return nil, fmt.Errorf("获取 %s 索引失败：%s", ext, it.Err)
		}
		return nil, nil
	}
	return &p, nil
}

// RemoveExtension 删除由 go-tools 下载的扩展 dll（内置 dll 拒绝删除），并禁用 php.ini 中的启用行
func RemoveExtension(id, ext string) error {
	cfg, err := findInstance(id)
	if err != nil {
		return err
	}
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" || strings.ContainsAny(ext, `/\ .`) {
		return fmt.Errorf("非法扩展名: %s", ext)
	}
	exe := phpExe(cfg.Version, cfg.NTS)
	if exe == "" {
		exe = phpExe(cfg.Version, !cfg.NTS)
	}
	if exe == "" {
		return fmt.Errorf("版本 %s 未安装", cfg.Version)
	}
	dll := filepath.Join(filepath.Dir(exe), "ext", "php_"+ext+".dll")
	if _, err := os.Stat(dll); err != nil {
		return fmt.Errorf("扩展 %s 不存在", ext)
	}
	if !isDownloadedExt(dll) {
		return fmt.Errorf("扩展 %s 随 PHP 发行包内置，无法删除（可直接禁用）", ext)
	}
	if _, err := SetExtension(id, ext, false); err != nil {
		return err
	}
	if err := os.Remove(dll); err != nil {
		return fmt.Errorf("%s（若 PHP 正在运行，请先停止实例后重试）", err)
	}
	_ = os.Remove(dll + dlMarkerSuffix)
	// 依赖 dll 可能仍在被其它扩展使用，保留不删
	return nil
}

// dlMarkerSuffix 记录 dll 由 go-tools 下载的标记文件后缀（内容含来源 URL）
const dlMarkerSuffix = ".go-tools-dl"

func isDownloadedExt(dll string) bool {
	_, err := os.Stat(dll + dlMarkerSuffix)
	return err == nil
}

func downloadedExtVersion(dll string) string {
	data, err := os.ReadFile(dll + dlMarkerSuffix)
	if err != nil {
		return ""
	}
	m := extZipRe.FindStringSubmatch(strings.TrimSpace(string(data)))
	if m == nil {
		return ""
	}
	return m[3]
}

func extEmit(p InstallProgress) { srvEmit("php:ext-install", p) }

func installExtAsync(id, ext, url, phpDir string, enable bool) {
	defer peclBusy.Delete(ext)
	info := func(note string) { extEmit(InstallProgress{Version: ext, Stage: "info", Note: note}) }
	fail := func(note string) { extEmit(InstallProgress{Version: ext, Stage: "error", Note: note}) }

	extDir := filepath.Join(phpDir, "ext")
	if err := os.MkdirAll(extDir, 0o755); err != nil {
		fail(err.Error())
		return
	}
	work := filepath.Join(phpDir, ".pecl-tmp")
	_ = os.RemoveAll(work)
	if err := os.MkdirAll(work, 0o755); err != nil {
		fail(err.Error())
		return
	}
	defer os.RemoveAll(work)
	zipPath := filepath.Join(work, filepath.Base(url))

	info(fmt.Sprintf("开始下载 %s：%s", ext, filepath.Base(url)))
	if err := downloadExtZip(url, zipPath, ext); err != nil {
		fail(fmt.Sprintf("下载失败：%v", err))
		return
	}
	info("下载完成，正在解压")
	extEmit(InstallProgress{Version: ext, Stage: "extracting", Percent: 100})
	payload := filepath.Join(work, "x")
	if err := extractZip(zipPath, payload); err != nil {
		fail(fmt.Sprintf("解压失败: %v", err))
		return
	}
	want := "php_" + ext + ".dll"
	found := ""
	_ = filepath.Walk(payload, func(p string, st os.FileInfo, err error) error {
		if err != nil || st.IsDir() {
			return nil
		}
		if strings.EqualFold(st.Name(), want) {
			found = p
		}
		return nil
	})
	if found == "" {
		fail(fmt.Sprintf("安装包中未找到 %s", want))
		return
	}
	if err := replaceFile(found, filepath.Join(extDir, want)); err != nil {
		fail(fmt.Sprintf("写入 dll 失败: %v", err))
		return
	}
	if err := os.WriteFile(filepath.Join(extDir, want)+dlMarkerSuffix, []byte(url+"\n"), 0o644); err != nil {
		fail(err.Error())
		return
	}
	// 附带的依赖 dll（如 ssh2 需要的 libssh2.dll）一并落到 php.exe 同级目录
	_ = filepath.Walk(payload, func(p string, st os.FileInfo, err error) error {
		if err != nil || st.IsDir() {
			return nil
		}
		name := strings.ToLower(st.Name())
		if !strings.HasSuffix(name, ".dll") || strings.EqualFold(name, want) {
			return nil
		}
		_ = replaceFile(p, filepath.Join(phpDir, name))
		return nil
	})
	info("已写入：" + filepath.Join(extDir, want))

	if enable {
		warn, err := SetExtension(id, ext, true)
		if err != nil {
			fail(fmt.Sprintf("扩展已下载，但写入 php.ini 失败: %v", err))
			return
		}
		if warn != "" {
			info("php.ini 校验提示：" + firstLine(warn))
		}
	}
	extEmit(InstallProgress{Version: ext, Stage: "done", Percent: 100, Note: want})
}

// downloadExtZip 下载扩展包，进度经 "php:ext-install" 推送
func downloadExtZip(url, dest, ext string) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", phpUserAgent())
	client := &http.Client{Timeout: 20 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	total := resp.ContentLength
	var done int64
	lastPct := -1
	buf := make([]byte, 256*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return werr
			}
			done += int64(n)
			if total > 0 {
				pct := int(done * 100 / total)
				if pct/10 != lastPct/10 { // 10% 步进推送
					lastPct = pct
					extEmit(InstallProgress{Version: ext, Stage: "downloading", Percent: pct, Note: filepath.Base(url)})
				}
			}
		}
		if rerr != nil {
			if rerr == io.EOF {
				return nil
			}
			return rerr
		}
	}
}

// replaceFile 覆盖目标文件（目标被占用时给出可读错误）
func replaceFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		return fmt.Errorf("%s（若 PHP 正在运行，请先停止实例后重试）", err)
	}
	return nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}

// majorSeries "7.4.33" → "7.4"
func majorSeries(ver string) string {
	parts := strings.Split(ver, ".")
	if len(parts) >= 2 {
		return parts[0] + "." + parts[1]
	}
	return ver
}
