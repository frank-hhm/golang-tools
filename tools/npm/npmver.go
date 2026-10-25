package npm

// ============================================================
// npm 自身版本切换 + 命令行生效链
//
// 在 cmd 里敲 `npm` 到底执行哪一份 npm，比想象中绕：
//
//   1) PATH × PATHEXT 决定命中哪个转发壳（通常是 Node 目录里的 npm.cmd）；
//   2) 那个 npm.cmd 内部并不写死自带 npm —— 它每次执行都先问
//      `npm prefix -g`，若 <prefix>/node_modules/npm 存在就用它，
//      否则才回退到 <nodeDir>/node_modules/npm。
//
// 于是同一台机器上 npm 分两层：
//   发行层  <nodeDir>\node_modules\npm   跟着 Node 走
//   覆盖层  <prefix>\node_modules\npm    `npm i -g npm` 装的就是这里
// 覆盖层只要存在就一定赢。原实现只读发行层（detectNpmVer），所以用户
// `npm i -g npm@x` 之后面板仍显示旧版本 —— 这里把整条链补出来。
//
// 切换的做法：Go 自己下 npm 的 tarball 解压替换目标层，全程不经过任何 npm CLI。
// 这样才同时避开两个经典麻烦：
//   - 拿旧 npm 覆盖它自己 → Windows 上文件被占用（EBUSY/EPERM），装到一半坏掉；
//   - 以为要改 PATH 才生效 → 机器 PATH 排在用户 PATH 之前，非管理员根本抢不动。
// 换覆盖层连已经开着的 cmd 窗口都会立刻变，因为 npm.cmd 是每次执行时才解析路径。
// ============================================================

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// NpmLayer 生效链上的一层
type NpmLayer struct {
	Kind    string `json:"kind"`    // bundled（Node 自带层）| override（全局 prefix 覆盖层）
	Dir     string `json:"dir"`     // npm 包目录（不存在时也回显目标位置）
	Exists  bool   `json:"exists"`  // 目录里有可用的 npm-cli.js
	HasPrev bool   `json:"hasPrev"` // 这一层有一步可撤销的旧版本留存
	Version string `json:"version"` // package.json 版本
	CliJS   string `json:"cliJs"`
	Engines string `json:"engines"` // package.json engines.node
}

// NpmEntry 一个 npm 入口目录（Node 目录，或 prefix 这类自带转发壳的目录）
type NpmEntry struct {
	Version    string   `json:"version"`    // 目录对应的 Node 版本，读不到为空
	Dir        string   `json:"dir"`        // 入口目录
	Exe        string   `json:"exe"`        // 跑它用的 node
	Registered bool     `json:"registered"` // 是否已在 go-tools 登记
	PathIdx    int      `json:"pathIdx"`    // 在生效 PATH 中的下标，-1 = 不在
	Shim       string   `json:"shim"`       // 该目录下的 npm 转发壳
	HasNode    bool     `json:"hasNode"`    // 目录里是否有 node 本体
	Prefix     string   `json:"prefix"`     // 该入口算出的全局 prefix
	Bundled    NpmLayer `json:"bundled"`    // 本目录的 node_modules/npm
	Override   NpmLayer `json:"override"`   // prefix 指向的 node_modules/npm

	Effective       string `json:"effective"`       // 经该 shim 实际执行的 npm 版本
	EffectiveFrom   string `json:"effectiveFrom"`   // bundled | override
	HasPrev         bool   `json:"hasPrev"`         // 有一步可撤销的旧版本
	LayerDefault    string `json:"layerDefault"`    // 当前所选层的出厂版本
	BundledDefault  string `json:"bundledDefault"`  // 发行层出厂版本（恢复用）
	OverrideDefault string `json:"overrideDefault"` // 覆盖层出厂版本
}

// NpmChain 命令行 npm 的完整生效链
type NpmChain struct {
	Entries       []NpmEntry `json:"entries"`
	ActiveDir     string     `json:"activeDir"`
	ActiveShim    string     `json:"activeShim"`
	ActiveNode    string     `json:"activeNode"`
	ActiveVersion string     `json:"activeVersion"`
	ActiveFrom    string     `json:"activeFrom"`
	ActivePrefix  string     `json:"activePrefix"`
	PathNote      string     `json:"pathNote"`
	Shadowed      []string   `json:"shadowed"` // PATH 里被压住的 npm 入口
	Hints         []string   `json:"hints"`
	Platform      string     `json:"platform"`
}

// NpmRelease 一个可切换的 npm 版本
type NpmRelease struct {
	Version    string `json:"version"`
	Tarball    string `json:"tarball"`
	Engines    string `json:"engines"`
	Tag        string `json:"tag"`        // latest / next 等 dist-tag，无则空
	Compatible bool   `json:"compatible"` // 相对查询指定 Node 是否满足 engines
	Why        string `json:"why"`
}

// NpmReleaseList 可用 npm 版本清单
type NpmReleaseList struct {
	Registry string       `json:"registry"`
	ForNode  string       `json:"forNode"`
	Releases []NpmRelease `json:"releases"`
	Note     string       `json:"note"`
}

// NpmSwapRecord 一次切换留下的痕迹（供「撤销」和「恢复出厂」）
type NpmSwapRecord struct {
	Key     string `json:"key"`     // 目标目录的归一化路径
	Dir     string `json:"dir"`     // 被替换的 npm 目录
	Prev    string `json:"prev"`    // 上一版保留目录（.npm-prev）
	From    string `json:"from"`    // 切换前版本
	To      string `json:"to"`      // 切换到的版本
	Default string `json:"default"` // 该层出厂版本（恢复用）
	Layer   string `json:"layer"`   // bundled | override
	Entry   string `json:"entry"`   // 所属入口目录
	At      string `json:"at"`
}

func emitNpmVer(p InstallProgress) { srvEmit("npm:npmver", p) }

/* ============================ 层读取 ============================ */

// bundledNpmDir 入口目录自己的 npm 包位置（Windows 与 POSIX 布局不同）
func bundledNpmDir(entryDir string) string { return localNpmDir(entryDir) }

// readNpmLayer 读一层 npm；目录不存在时仍带着 Dir 返回，好让前端显示目标位置
func readNpmLayer(kind, dir string) NpmLayer {
	l := NpmLayer{Kind: kind, Dir: dir}
	if dir == "" {
		return l
	}
	// 即使当前层是空的，也可能留着上次切换的旧版本（撤销仍然有意义）
	if rec, ok := lookupSwap(dir); ok {
		l.HasPrev = rec.Prev != ""
	}
	cli := filepath.Join(dir, "bin", "npm-cli.js")
	if st, err := os.Stat(cli); err != nil || st.IsDir() {
		return l
	}
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return l
	}
	var p struct {
		Version string `json:"version"`
		Engines struct {
			Node string `json:"node"`
		} `json:"engines"`
	}
	if json.Unmarshal(data, &p) != nil || p.Version == "" {
		return l
	}
	l.CliJS, l.Exists = cli, true
	l.Version, l.Engines = p.Version, strings.TrimSpace(p.Engines.Node)
	return l
}

// derivePrefix 不起进程、纯按配置推导 npm 全局 prefix（问不到时的兜底）
func derivePrefix(nodeExe string) string {
	if v := strings.TrimSpace(os.Getenv("NPM_CONFIG_PREFIX")); v != "" {
		return v // 环境变量压过用户配置，与 npm 的优先级一致
	}
	if v := npmPrefixFromUserNpmrc(); v != "" {
		return v
	}
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		if v := strings.TrimSpace(os.Getenv("PREFIX")); v != "" {
			return v // npm 在 Windows 上认 %PREFIX%（部分企业镜像环境用它）
		}
		if ad := os.Getenv("APPDATA"); ad != "" {
			return filepath.Join(ad, "npm")
		}
		if home != "" {
			return filepath.Join(home, "AppData", "Roaming", "npm")
		}
		return ""
	}
	if nodeExe != "" {
		return filepath.Dir(nodeExe) // POSIX 官方发行包默认 prefix 即 Node 目录
	}
	if home != "" {
		return filepath.Join(home, ".npm-global")
	}
	return ""
}

/* ============================ prefix 与实际生效层 ============================ */

type prefixCacheEntry struct {
	value string
	at    time.Time
}

var (
	prefixMu    sync.Mutex
	prefixCache = map[string]prefixCacheEntry{}
	prefixTTL   = 45 * time.Second
)

func invalidatePrefixCache() {
	prefixMu.Lock()
	prefixCache = map[string]prefixCacheEntry{}
	prefixMu.Unlock()
}

// runNpmRaw 用指定 node + npm 入口跑一条命令，只取 stdout，且不带托管 npmrc。
// npm.cmd 内部问 prefix 用的正是这个姿势，保持一致才测得出真实生效链。
func runNpmRaw(nodeExe, cli string, args []string, timeout time.Duration) (string, error) {
	if nodeExe == "" || cli == "" {
		return "", fmt.Errorf("缺少 node 或 npm 入口")
	}
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, nodeExe, append([]string{cli}, args...)...)
	cmd.SysProcAttr = hideWindowAttr()
	data, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// globalPrefixFor 复刻 npm.cmd 的算法：拿发行层 npm 问 `prefix -g`，
// 问不通就按配置推导。结果短暂缓存，免得刷新一次面板起一堆进程。
func globalPrefixFor(exe string) string {
	if exe == "" {
		return derivePrefix("")
	}
	key := pathKey(exe)
	prefixMu.Lock()
	if e, ok := prefixCache[key]; ok && e.value != "" && time.Since(e.at) < prefixTTL {
		prefixMu.Unlock()
		return e.value
	}
	prefixMu.Unlock()

	v := derivePrefix(exe)
	if cli := readNpmLayer("bundled", localNpmDir(filepath.Dir(exe))).CliJS; cli != "" {
		if out, err := runNpmRaw(exe, cli, []string{"prefix", "-g"}, 10*time.Second); err == nil {
			// 取最后一个非空行：前面可能混着 deprecation 提示之类的噪音
			if line := lastNonEmpty(out); line != "" && filepath.IsAbs(line) {
				v = line
			}
		}
	}
	prefixMu.Lock()
	prefixCache[key] = prefixCacheEntry{value: v, at: time.Now()}
	prefixMu.Unlock()
	return v
}

func lastNonEmpty(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if t := strings.TrimSpace(lines[i]); t != "" {
			return t
		}
	}
	return ""
}

// buildEntry 算出一个入口目录的两层 npm，以及 cmd 经它会实际执行的版本
func buildEntry(dir, exe string, registered bool) NpmEntry {
	e := NpmEntry{Dir: dir, Exe: exe, Registered: registered, PathIdx: -1, HasNode: exe != ""}
	e.Shim = findShim(dir, "npm")
	e.Bundled = readNpmLayer("bundled", localNpmDir(dir))
	if e.Exe == "" {
		// prefix 这类目录里没有 node 本体，用 PATH 上的 node 去问 prefix
		e.Exe = defaultNodeExe()
	}
	e.Prefix = globalPrefixFor(e.Exe)
	if e.Prefix != "" {
		e.Override = readNpmLayer("override", overrideNpmDir(e.Prefix))
	}
	// prefix 恰好就是这个目录时（POSIX 官方包常见），两层是同一个地方，别当覆盖层
	if e.Override.Dir != "" && pathKey(e.Override.Dir) == pathKey(e.Bundled.Dir) {
		e.Override = NpmLayer{Kind: "override"}
	}
	switch {
	case e.Override.Exists:
		e.Effective, e.EffectiveFrom = e.Override.Version, "override"
	case e.Bundled.Exists:
		e.Effective, e.EffectiveFrom = e.Bundled.Version, "bundled"
	}
	if rec, ok := lookupSwap(e.Bundled.Dir); ok {
		e.BundledDefault = rec.Default
	} else {
		e.BundledDefault = e.Bundled.Version
	}
	e.HasPrev = e.Bundled.HasPrev || e.Override.HasPrev
	if e.Override.Dir != "" {
		if rec, ok := lookupSwap(e.Override.Dir); ok {
			e.OverrideDefault = rec.Default
		} else if e.Override.Exists {
			e.OverrideDefault = e.Override.Version
		}
	}
	e.LayerDefault = e.BundledDefault
	if e.EffectiveFrom == "override" {
		e.LayerDefault = e.OverrideDefault
	}
	if vr := nodeVersionOfEntry(exe, dir); vr != "" {
		e.Version = vr
	}
	return e
}

func nodeVersionOfEntry(exe, dir string) string {
	if exe == "" {
		return ""
	}
	if v := resolveNodeVersion(exe, dir); v != "" {
		return v
	}
	if v, err := probeVersion(exe); err == nil {
		return v
	}
	return ""
}

// effectiveNpmCLI 与 npm.cmd 同逻辑取「该 Node 实际会用的 npm 入口」，
// 让工具内部跑 npm 的结果和用户在 cmd 里看到的保持一致。
func effectiveNpmCLI(exe string) string {
	dir := filepath.Dir(exe)
	bundled := readNpmLayer("bundled", localNpmDir(dir))
	if prefix := globalPrefixFor(exe); prefix != "" {
		ov := readNpmLayer("override", overrideNpmDir(prefix))
		if ov.Exists {
			return ov.CliJS
		}
	}
	return bundled.CliJS
}

// npmEntryFor 一个 Node 实际该用的那份 npm 的元数据（可能不是它目录里自带的那一份）
func npmEntryFor(exe string) (pkgDirInfo, bool) {
	cli := effectiveNpmCLI(exe)
	if cli == "" {
		return pkgDirInfo{}, false
	}
	info, ok := readPkgDir(filepath.Dir(filepath.Dir(cli)))
	if !ok {
		return pkgDirInfo{}, false
	}
	info.Entry = cli
	return info, true
}

/* ============================ 生效链体检 ============================ */

// InspectNpmChain 计算「cmd 里敲 npm 到底执行哪一份 npm」
func InspectNpmChain() (*NpmChain, error) {
	pathDirs := filepath.SplitList(os.Getenv("PATH"))
	idx := map[string]int{}
	for i, d := range pathDirs {
		d = strings.Trim(d, `"`)
		if d == "" {
			continue
		}
		if _, ok := idx[pathKey(d)]; !ok {
			idx[pathKey(d)] = i
		}
	}

	byDir := map[string]NpmEntry{}
	add := func(e NpmEntry) {
		k := pathKey(e.Dir)
		if old, ok := byDir[k]; ok && old.Registered && !e.Registered {
			return
		}
		if j, ok := idx[k]; ok {
			e.PathIdx = j
		}
		byDir[k] = e
	}

	// 1) go-tools 已登记的 Node（含工具自己下的，这类通常不在 PATH 里）
	vers, _ := ListVersions()
	for _, v := range vers {
		dir := filepath.Dir(v.Path)
		e := buildEntry(dir, v.Path, true)
		e.Version = v.Version
		add(e)
	}

	// 2) PATH 里有 npm 转发壳、但没登记的目录：装机后没碰过工具的机器很常见，
	//    prefix 目录装着覆盖层时也会以这种身份出现在 PATH 里
	for _, d := range pathDirs {
		d = strings.Trim(d, `"`)
		if d == "" {
			continue
		}
		k := pathKey(d)
		if findShim(d, "npm") == "" {
			continue
		}
		if _, seen := byDir[k]; seen {
			continue
		}
		exe := findShim(d, "node")
		if exe == "" {
			if p := findNode(d); p != "" {
				exe = p
			}
		}
		add(buildEntry(d, exe, false))
	}

	entries := make([]NpmEntry, 0, len(byDir))
	for _, e := range byDir {
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i].PathIdx, entries[j].PathIdx
		if a < 0 {
			a = 1 << 30
		}
		if b < 0 {
			b = 1 << 30
		}
		if a != b {
			return a < b
		}
		return compareVersion(entries[i].Version, entries[j].Version) > 0
	})

	c := &NpmChain{Entries: entries, Platform: runtime.GOOS}
	// cmd 是「逐目录 × 按 PATHEXT」取第一个命中，所以 PathIdx 最小且有转发壳的即生效
	for _, e := range entries {
		if e.PathIdx >= 0 && e.Shim != "" {
			c.ActiveDir, c.ActiveNode, c.ActiveShim = e.Dir, e.Version, e.Shim
			c.ActiveVersion, c.ActiveFrom, c.ActivePrefix = e.Effective, e.EffectiveFrom, e.Prefix
			break
		}
	}
	for _, e := range entries {
		if e.Shim == "" || e.PathIdx < 0 {
			continue
		}
		if pathKey(e.Dir) == pathKey(c.ActiveDir) {
			continue
		}
		c.Shadowed = append(c.Shadowed, fmt.Sprintf("PATH[%d] %s → npm %s", e.PathIdx, e.Shim, orNone(e.Effective)))
	}
	c.PathNote = "PATH 顺序取自 go-tools 进程启动时的环境；之后改过系统/用户 PATH 的，请新开一个终端再对照。"
	c.Hints = chainHints(c)
	return c, nil
}

func layerName(from string) string {
	if from == "override" {
		return "全局覆盖层(prefix)"
	}
	return "Node 自带层(发行层)"
}

func chainHints(c *NpmChain) []string {
	var h []string
	if c.ActiveShim == "" {
		h = append(h, "当前 PATH 里找不到 npm 转发壳 —— cmd 敲 npm 会报「不是内部或外部命令」。下面选一个 Node 把 npm 装好，再把它的目录加入 PATH。")
		return h
	}
	h = append(h, fmt.Sprintf("cmd 里 `npm -v` = %s，来自%s；转发壳 %s", orNone(c.ActiveVersion), layerName(c.ActiveFrom), c.ActiveShim))

	for _, e := range c.Entries {
		if e.PathIdx < 0 || pathKey(e.Dir) != pathKey(c.ActiveDir) {
			continue
		}
		if e.EffectiveFrom == "override" && e.Bundled.Exists && e.Bundled.Version != e.Override.Version {
			h = append(h, fmt.Sprintf("覆盖层装着 npm %s，压住了这个 Node 自带的 %s；想回到自带版本点「移除覆盖层」。",
				e.Override.Version, e.Bundled.Version))
		}
		if e.EffectiveFrom == "bundled" && !e.Override.Exists {
			h = append(h, fmt.Sprintf("覆盖层目录不存在（%s），所以用的是自带 npm。往这里装一个版本就能覆盖自带 —— 不需要管理员，也不用改 PATH；npm.cmd 是每次执行才解析路径，已经开着的 cmd 下一条命令就会用上新版本。",
				orNone(overrideNpmDir(e.Prefix))))
		}
	}
	for _, e := range c.Entries {
		if e.PathIdx < 0 || pathKey(e.Dir) == pathKey(c.ActiveDir) {
			continue
		}
		if e.Effective != "" {
			h = append(h, fmt.Sprintf("PATH[%d] 的 %s（npm %s）被靠前的 %s 压住，永远轮不到。",
				e.PathIdx, filepath.Base(e.Dir), e.Effective, filepath.Base(c.ActiveDir)))
		}
	}
	if len(c.Shadowed) > 0 {
		h = append(h, fmt.Sprintf("PATH 里另有 %d 个 npm 入口被压制：%s", len(c.Shadowed), strings.Join(head(c.Shadowed, 3), "；")))
	}
	return h
}

func orNone(s string) string {
	if s == "" {
		return "（未知）"
	}
	return s
}

func head(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

/* ============================ 可用 npm 版本 ============================ */

var (
	relMu       sync.Mutex
	relCache    *NpmReleaseList
	relCacheReg string
	relCacheAt  time.Time
)

const npmReleasesTTL = 10 * time.Minute

// FetchNpmVersions 从当前 registry 取 npm 版本清单。
// 用 install-v1 精简元数据（约 2.4MB）而不是完整元数据（约 26MB）。
func FetchNpmVersions(nodeVersion string) (*NpmReleaseList, error) {
	reg := strings.TrimRight(GetRegistry(), "/")
	if reg == "" {
		reg = "https://registry.npmjs.org"
	}
	relMu.Lock()
	if relCacheReg == reg && relCache != nil && time.Since(relCacheAt) < npmReleasesTTL {
		list := *relCache
		relMu.Unlock()
		fillCompat(&list, nodeVersion)
		return &list, nil
	}
	relMu.Unlock()

	req, err := http.NewRequest(http.MethodGet, reg+"/npm", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.npm.install-v1+json")
	req.Header.Set("User-Agent", userAgent())
	client := &http.Client{Timeout: 2 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取 npm 版本列表失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取 npm 版本列表失败: HTTP %d（registry %s）", resp.StatusCode, reg)
	}
	var payload struct {
		DistTags map[string]string `json:"dist-tags"`
		Versions map[string]struct {
			Engines struct {
				Node string `json:"node"`
			} `json:"engines"`
			Dist struct {
				Tarball string `json:"tarball"`
			} `json:"dist"`
		} `json:"versions"`
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 64<<20))
	if err := dec.Decode(&payload); err != nil {
		return nil, fmt.Errorf("解析 npm 版本列表失败: %v", err)
	}
	tagFor := map[string]string{}
	for tag, ver := range payload.DistTags {
		if _, busy := tagFor[ver]; busy && tag != "latest" {
			continue
		}
		tagFor[ver] = tag
	}
	list := &NpmReleaseList{Registry: reg, ForNode: nodeVersion}
	for ver, item := range payload.Versions {
		if !versionRe.MatchString(ver) {
			continue // 预发布版（-rc / -next）不进列表，避免装上半成品
		}
		url := item.Dist.Tarball
		if url == "" {
			url = fmt.Sprintf("%s/npm/-/npm-%s.tgz", reg, ver)
		}
		list.Releases = append(list.Releases, NpmRelease{
			Version: ver, Tarball: url, Engines: item.Engines.Node, Tag: tagFor[ver],
		})
	}
	if len(list.Releases) == 0 {
		return nil, fmt.Errorf("registry %s 未返回任何 npm 版本", reg)
	}
	sort.Slice(list.Releases, func(i, j int) bool {
		return compareVersion(list.Releases[i].Version, list.Releases[j].Version) > 0
	})
	anyEngines := false
	for _, r := range list.Releases {
		if r.Engines != "" {
			anyEngines = true
			break
		}
	}
	if !anyEngines {
		list.Note = "该 registry 的精简元数据没给 engines，无法预判与 Node 的兼容性；切换后工具会实测 npm -v，跑不通自动回滚。"
	}
	relMu.Lock()
	relCache, relCacheReg, relCacheAt = list, reg, time.Now()
	relMu.Unlock()

	out := *list
	fillCompat(&out, nodeVersion)
	return &out, nil
}

// RefreshNpmVersions 清掉缓存重新拉（切换失败或换了镜像源后用）
func RefreshNpmVersions(nodeVersion string) (*NpmReleaseList, error) {
	relMu.Lock()
	relCache, relCacheReg, relCacheAt = nil, "", time.Time{}
	relMu.Unlock()
	return FetchNpmVersions(nodeVersion)
}

func fillCompat(list *NpmReleaseList, nodeVersion string) {
	for i := range list.Releases {
		r := &list.Releases[i]
		if nodeVersion == "" || r.Engines == "" {
			r.Compatible, r.Why = true, ""
			continue
		}
		r.Compatible = enginesOK(nodeVersion, r.Engines)
		if !r.Compatible {
			r.Why = fmt.Sprintf("npm %s 要求 Node %s，当前 Node %s 不满足", r.Version, r.Engines, nodeVersion)
		}
	}
}

var hyphenRangeRe = regexp.MustCompile(`^(\d+\.\d+\.\d+)\s*-\s*(\d+\.\d+\.\d+)$`)

// enginesOK 判断 nodeVer 是否满足 engines.node。
// 只实现 npm 官方真会用的写法（^x.y.z、>=x.y.z、x.y.z - a.b.c 及 || 组合）；
// 解析不了的放行 —— 宁可漏报也不拦用户切换，真跑不通有实测回滚兜底。
func enginesOK(nodeVer, rng string) bool {
	rng = strings.TrimSpace(rng)
	if rng == "" || nodeVer == "" {
		return true
	}
	for _, part := range strings.Split(rng, "||") {
		if rangePartOK(nodeVer, strings.TrimSpace(part)) {
			return true
		}
	}
	return false
}

func rangePartOK(nodeVer, part string) bool {
	if part == "" || part == "*" {
		return true
	}
	if m := hyphenRangeRe.FindStringSubmatch(part); m != nil {
		return compareVersion(nodeVer, m[1]) >= 0 && compareVersion(nodeVer, m[2]) <= 0
	}
	fields := strings.Fields(part)
	if len(fields) == 0 {
		return true
	}
	for _, f := range fields {
		if !clauseOK(nodeVer, f) {
			return false
		}
	}
	return true
}

func clauseOK(nodeVer, c string) bool {
	tail := func(n int) string {
		s := c[n:]
		if m := verExtractRe.FindString(s); m != "" {
			return m
		}
		return s
	}
	switch {
	case strings.HasPrefix(c, ">="):
		return compareVersion(nodeVer, tail(2)) >= 0
	case strings.HasPrefix(c, "<="):
		return compareVersion(nodeVer, tail(2)) <= 0
	case strings.HasPrefix(c, ">"):
		return compareVersion(nodeVer, tail(1)) > 0
	case strings.HasPrefix(c, "<"):
		return compareVersion(nodeVer, tail(1)) < 0
	case strings.HasPrefix(c, "^"), strings.HasPrefix(c, "~"):
		b := tail(1)
		if compareVersion(nodeVer, b) < 0 {
			return false
		}
		nv, nb := strings.Split(nodeVer, "."), strings.Split(b, ".")
		if len(nb) == 0 {
			return true
		}
		// ^major.* 与 ~major.minor.* 都按「主版本号一致」粗判：足够卡住跨大版本误装
		return nv[0] == nb[0]
	default:
		return compareVersion(nodeVer, tail(0)) == 0
	}
}

/* ============================ 切换记录 ============================ */

func npmSwapsPath() (string, error) {
	d, err := goToolsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "npm-swaps.json"), nil
}

func loadSwaps() []NpmSwapRecord {
	p, err := npmSwapsPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var list []NpmSwapRecord
	if json.Unmarshal(data, &list) != nil {
		return nil
	}
	return list
}

func saveSwaps(list []NpmSwapRecord) error {
	p, err := npmSwapsPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o644)
}

func lookupSwap(dir string) (NpmSwapRecord, bool) {
	if dir == "" {
		return NpmSwapRecord{}, false
	}
	for _, r := range loadSwaps() {
		if r.Key == pathKey(dir) {
			return r, true
		}
	}
	return NpmSwapRecord{}, false
}

func putSwap(rec NpmSwapRecord) error {
	list := loadSwaps()
	idx := -1
	for i := range list {
		if list[i].Key == rec.Key {
			idx = i
		}
	}
	if idx >= 0 {
		list[idx] = rec
	} else {
		list = append(list, rec)
	}
	return saveSwaps(list)
}

func dropSwap(key string) {
	list := loadSwaps()
	out := list[:0]
	for _, r := range list {
		if r.Key != key {
			out = append(out, r)
		}
	}
	_ = saveSwaps(out)
}

/* ============================ 版本切换 ============================ */

var (
	switchMu   sync.Mutex
	switchBusy = map[string]bool{}
)

// npmTarget 解析目标层要写哪个目录
func npmTarget(layer, entryDir string) (target, exe, prefix string, err error) {
	entryDir = strings.Trim(entryDir, `"`)
	if entryDir == "" {
		return "", "", "", fmt.Errorf("未指定 Node 目录")
	}
	if st, serr := os.Stat(entryDir); serr != nil || !st.IsDir() {
		return "", "", "", fmt.Errorf("目录不存在：%s", entryDir)
	}
	exe = findShim(entryDir, "node")
	if exe == "" {
		exe = findNode(entryDir)
	}
	if exe == "" {
		exe = defaultNodeExe() // 入口目录里没有 node 本体（如 Windows 的 prefix），用 PATH 上的
	}
	prefix = globalPrefixFor(exe)
	switch strings.ToLower(layer) {
	case "bundled":
		return localNpmDir(entryDir), exe, prefix, nil
	case "override":
		if prefix == "" {
			return "", "", "", fmt.Errorf("无法确定该 Node 的全局 prefix")
		}
		t := overrideNpmDir(prefix)
		if pathKey(t) == pathKey(localNpmDir(entryDir)) {
			// POSIX 官方发行包的 prefix 就是 Node 目录，两层重叠
			return "", "", "", fmt.Errorf("该 Node 的 prefix 就是它自己的目录，覆盖层与发行层是同一个位置，请直接切「Node 自带层」")
		}
		return t, exe, prefix, nil
	}
	return "", "", "", fmt.Errorf("未知目标层: %s", layer)
}

// SwitchNpm 异步把指定层的 npm 换成指定版本；进度经 "npm:npmver" 事件推送
func SwitchNpm(layer, entryDir, npmVersion string) error {
	if !versionRe.MatchString(npmVersion) {
		return fmt.Errorf("非法 npm 版本号: %s", npmVersion)
	}
	target, _, _, err := npmTarget(layer, entryDir)
	if err != nil {
		return err
	}
	key := pathKey(target)
	switchMu.Lock()
	if switchBusy[key] {
		switchMu.Unlock()
		return fmt.Errorf("该位置正在切换中，请等当前任务结束")
	}
	switchBusy[key] = true
	switchMu.Unlock()
	go switchNpmAsync(layer, entryDir, npmVersion, target, key)
	return nil
}

func switchNpmAsync(layer, entryDir, npmVersion, target, key string) {
	defer func() {
		switchMu.Lock()
		delete(switchBusy, key)
		switchMu.Unlock()
	}()
	tag := "npm@" + npmVersion
	info := func(note string) { emitNpmVer(InstallProgress{Version: tag, Stage: "info", Note: note}) }
	fail := func(note string) { emitNpmVer(InstallProgress{Version: tag, Stage: "error", Note: note}) }

	exe, prefix, err := func() (string, string, error) {
		_, e, p, err := npmTarget(layer, entryDir)
		return e, p, err
	}()
	if err != nil {
		fail(err.Error())
		return
	}
	from := readNpmLayer(layer, target)
	label := layerName(layer)
	info(fmt.Sprintf("开始切换%s的 npm：%s → %s（目标 %s）", label, orNone(from.Version), npmVersion, target))

	tgz, isTemp, err := locateOrDownloadNpm(npmVersion, tag)
	if err != nil {
		fail(err.Error())
		return
	}
	if isTemp {
		defer os.Remove(tgz)
	}

	parent := filepath.Dir(target)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		fail(fmt.Sprintf("无法创建 %s：%v（这个位置可能需要管理员权限）", parent, err))
		return
	}
	staging := filepath.Join(parent, ".npm-staging")
	prev := filepath.Join(parent, ".npm-prev")
	_ = os.RemoveAll(staging)

	info("正在解压 npm " + npmVersion)
	emitNpmVer(InstallProgress{Version: tag, Stage: "extracting", Note: target})
	if err := extractTarGz(tgz, staging); err != nil {
		os.RemoveAll(staging)
		fail(fmt.Sprintf("解压失败: %v", err))
		return
	}
	if _, err := os.Stat(filepath.Join(staging, "bin", "npm-cli.js")); err != nil {
		os.RemoveAll(staging)
		fail("tarball 里没有 bin/npm-cli.js，不像 npm 官方包")
		return
	}

	// 旧的 .npm-prev 丢掉：撤销只保留一步，免得越攒越占盘
	_ = os.RemoveAll(prev)
	// 只有真把旧版挪走了才算“可撤销”；新建层没有上一份，别留个假路径
	keptPrev := ""
	if from.Exists {
		if err := os.Rename(target, prev); err != nil {
			os.RemoveAll(staging)
			fail(fmt.Sprintf("原目录改不了名（多半是被正在运行的 npm/node 占用）：%v", err))
			return
		}
		keptPrev = prev
	}
	if err := os.Rename(staging, target); err != nil {
		if from.Exists {
			_ = os.Rename(prev, target) // 新的放不进去就还原现场，别留个空位
		}
		fail(fmt.Sprintf("新目录就位失败：%v", err))
		return
	}
	if layer == "override" && prefix != "" {
		if err := writePrefixShims(prefix, target); err != nil {
			info("npm 已就位，但转发壳没写进去（不影响 prefix 覆盖生效）：" + err.Error())
		}
	}

	invalidatePrefixCache()
	got, verr := verifyNpm(exe, filepath.Join(target, "bin", "npm-cli.js"))
	if verr != nil || (got != "" && got != npmVersion) {
		_ = os.RemoveAll(target)
		if from.Exists {
			_ = os.Rename(prev, target)
		}
		invalidatePrefixCache()
		if verr != nil {
			fail(fmt.Sprintf("npm %s 在这个 Node 上跑不起来，已自动还原为 %s：%v", npmVersion, orNone(from.Version), verr))
		} else {
			fail(fmt.Sprintf("装完自检是 %s（期望 %s），已自动还原为 %s", got, npmVersion, orNone(from.Version)))
		}
		return
	}

	rec := NpmSwapRecord{
		Key: key, Dir: target, Prev: keptPrev, From: from.Version, To: npmVersion,
		Default: layerDefault(layer, from), Layer: layer, Entry: entryDir,
		At: time.Now().Format(time.RFC3339),
	}
	if err := putSwap(rec); err != nil {
		info("切换成功，但记录写入失败（撤销功能可能不可用）：" + err.Error())
	} else if keptPrev != "" {
		info("旧版本留在 " + keptPrev + "（可一键撤销；下次切换会覆盖它）")
	}
	emitNpmVer(InstallProgress{Version: tag, Stage: "done", Percent: 100, Note: target})
}

// layerDefault 该层「出厂」版本：已有记录就原样沿用（只认第一次，包括「本来没有这一层」
// 记下的空值），没记录才用当前读到的
func layerDefault(layer string, from NpmLayer) string {
	if rec, ok := lookupSwap(from.Dir); ok {
		return rec.Default
	}
	return from.Version
}

// verifyNpm 实测一次 npm -v，确认装上去的东西真能跑
func verifyNpm(exe, cli string) (string, error) {
	out, err := runNpmRaw(exe, cli, []string{"-v"}, 60*time.Second)
	if err != nil {
		return "", err
	}
	s := lastNonEmpty(out)
	if !versionRe.MatchString(s) {
		return "", fmt.Errorf("无法解析 npm 版本输出: %s", trimForErr(out))
	}
	return s, nil
}

// locateOrDownloadNpm 优先用插件目录里的离线 tgz，否则下载
func locateOrDownloadNpm(version, tag string) (string, bool, error) {
	root, err := pluginsRoot()
	if err != nil {
		return "", false, err
	}
	for _, c := range []string{
		filepath.Join(root, fmt.Sprintf("npm-%s.tgz", version)),
		filepath.Join(root, fmt.Sprintf("npm-%s.download.tgz", version)),
	} {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			emitNpmVer(InstallProgress{Version: tag, Stage: "info", Note: "发现离线包：" + c})
			return c, false, nil
		}
	}
	url := npmTarballURL(version)
	tmp := filepath.Join(root, fmt.Sprintf("npm-%s.download.tgz", version))
	emitNpmVer(InstallProgress{Version: tag, Stage: "info", Note: "开始下载：" + filepath.Base(url)})
	err = downloadFileTo(url, tmp, func(p InstallProgress) {
		p.Version = tag
		emitNpmVer(p)
	})
	if err != nil {
		os.Remove(tmp)
		return "", false, fmt.Errorf("下载失败: %v；也可把 npm-%s.tgz 放进插件目录后离线切换", err, version)
	}
	return tmp, true, nil
}

// npmTarballURL 优先用清单里的地址（镜像源可能改过 host），取不到就按 registry 拼
func npmTarballURL(version string) string {
	reg := strings.TrimRight(GetRegistry(), "/")
	if reg == "" {
		reg = "https://registry.npmjs.org"
	}
	relMu.Lock()
	cached := relCache
	relMu.Unlock()
	if cached != nil {
		for _, r := range cached.Releases {
			if r.Version == version && r.Tarball != "" {
				return r.Tarball
			}
		}
	}
	return fmt.Sprintf("%s/npm/-/npm-%s.tgz", reg, version)
}

// extractTarGz 解压 npm tarball。npm 的包统一以 package/ 为根，剥掉它让内容直接落到 destDir。
// npm 把依赖打成真实文件发布（实测包内无软/硬链接），所以只处理目录和普通文件。
func extractTarGz(tgz, destDir string) error {
	f, err := os.Open(tgz)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("不是合法的 gzip: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	base := filepath.Clean(destDir)
	count := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.ReplaceAll(hdr.Name, "\\", "/")
		name = strings.TrimPrefix(strings.TrimPrefix(name, "./"), "package/")
		if name == "package" || name == "" || name == "." || strings.HasPrefix(name, "/") {
			continue
		}
		target := filepath.Join(base, filepath.FromSlash(name))
		if target != base && !strings.HasPrefix(target, base+string(os.PathSeparator)) {
			continue // 路径穿越
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			mode := os.FileMode(hdr.Mode & 0o777)
			if mode == 0 {
				mode = 0o644
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
			count++
		default:
			continue // 其它类型（链接等）npm 官方包不会有，保险起见跳过
		}
	}
	if count == 0 {
		return fmt.Errorf("解压后没有任何文件，tarball 可能不完整")
	}
	return nil
}

/* ============================ 撤销 / 移除覆盖层 ============================ */

// RollbackNpm 撤销一次切换，回到 .npm-prev 里留存的旧版本
func RollbackNpm(layer, entryDir string) error {
	target, _, prefix, err := npmTarget(layer, entryDir)
	if err != nil {
		return err
	}
	rec, ok := lookupSwap(target)
	if !ok || rec.Prev == "" {
		return fmt.Errorf("没有可撤销的记录（%s）", target)
	}
	if _, err := os.Stat(rec.Prev); err != nil {
		return fmt.Errorf("旧版本目录已不存在：%s", rec.Prev)
	}
	switchMu.Lock()
	if switchBusy[rec.Key] {
		switchMu.Unlock()
		return fmt.Errorf("该位置正在切换中，请等当前任务结束")
	}
	switchBusy[rec.Key] = true
	switchMu.Unlock()
	defer func() {
		switchMu.Lock()
		delete(switchBusy, rec.Key)
		switchMu.Unlock()
	}()

	cur := filepath.Join(filepath.Dir(target), ".npm-rejected")
	_ = os.RemoveAll(cur)
	if err := os.Rename(target, cur); err != nil {
		return fmt.Errorf("当前 npm 被占用，无法撤销：%v", err)
	}
	if err := os.Rename(rec.Prev, target); err != nil {
		_ = os.Rename(cur, target)
		return fmt.Errorf("还原失败：%v", err)
	}
	_ = os.RemoveAll(cur)
	if layer == "override" && prefix != "" {
		if err := writePrefixShims(prefix, target); err != nil {
			return err
		}
	}
	invalidatePrefixCache()
	// 只抹掉 Prev，保留 Default：撤销不等于失忆，出厂版本还得接着认
	rec.Prev = ""
	_ = putSwap(rec)
	emitNpmVer(InstallProgress{Version: "npm@" + rec.From, Stage: "done", Percent: 100, Note: target})
	return nil
}

// RemoveNpmOverride 删掉 prefix 覆盖层的 npm，让 cmd 回到该 Node 自带的版本。
// 只删 npm 自己的东西：包目录，外加内容里认得出的那些转发壳。
func RemoveNpmOverride(entryDir string) error {
	target, _, prefix, err := npmTarget("override", entryDir)
	if err != nil {
		return err
	}
	if target == "" {
		return fmt.Errorf("该 Node 没有独立的覆盖层可移除")
	}
	if _, err := os.Stat(target); err != nil {
		return fmt.Errorf("覆盖层不存在（%s），cmd 本来就在用 Node 自带的 npm", target)
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("删除失败，可能被占用或需要写权限：%v", err)
	}
	removed := 0
	for _, name := range prefixShimNames {
		p := filepath.Join(prefixShimDir(prefix), name)
		if isNpmOwnShim(p) {
			if err := os.Remove(p); err == nil {
				removed++
			}
		}
	}
	invalidatePrefixCache()
	dropSwap(pathKey(target))
	emitNpmVer(InstallProgress{
		Version: "npm@override", Stage: "done", Percent: 100,
		Note: fmt.Sprintf("已移除覆盖层 %s（连带清掉 %d 个转发壳），cmd 的 npm 回到 Node 自带版本", target, removed),
	})
	return nil
}
