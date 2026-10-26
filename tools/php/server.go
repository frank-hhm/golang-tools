package php

// ============================================================
// 本地 PHP 服务管理（php -S 内置 Web 服务器，进程维度）
// 结构复刻 tools/mysql/server.go（redis 同款范式），差异点：
//   - 版本库：windows.php.net 官方 Windows zip（nts / zts 两种构型）
//   - bundled    ：<exedir>/plugins/php/<ver>/（发布内置，无安装标记）
//   - downloaded ：在线获取/离线 zip 安装，统一落 <exedir>/plugins/php/<ver>/ 并写 .go-tools-installed 标记
//   - local      ：用户手动注册的本机已有目录（登记在 go-tools/php-locals.json）
//   - 实例：配置持久化 go-tools/php-instances.json；每个实例工作目录
//     go-tools/php-instances/<id>/（php.ini + access.log + error.log + www/）。
//     站点根目录由用户指定（DocRoot），留空则用实例目录下的 www/。
//   - php.ini：首次使用从版本目录 php.ini-development 复制（没有则生成最小模板），
//     之后完全由用户掌控（GetConfigText/SaveConfigText 逐行编辑保存）。
//   - 扩展管理：扫描 <phpdir>/ext/php_*.dll 得到候选扩展，按 php.ini 中
//     extension=xxx 是否被注释判定启用/禁用；extension_dir 自动校正为绝对路径。
//   - 禁用函数：维护 php.ini 的 disable_functions 列表（读/整体覆盖/增删）。
//   - 停止：Windows 下 Go 不保存子进程句柄，故用任务管理器按 PID/镜像名收尾；
//     状态判定为「端口在监听」+「本程序拉起的进程句柄存活」。
//   - 安装进度经 "php:install" 事件推送（InstallProgress，stage 与 redis/mysql 一致）。
// ============================================================

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-tools/appdirs"
	"go-tools/services/sysx"
)

/* ============================ 事件推送 ============================ */

var srvEmitter func(name string, data ...any)

// SetEmitter 注入事件发射器（绑定 Wails v3 app.Event.Emit）。
func SetEmitter(fn func(name string, data ...any)) { srvEmitter = fn }

func srvEmit(name string, payload any) {
	if srvEmitter != nil {
		srvEmitter(name, payload)
	}
}

func emitInstall(p InstallProgress) { srvEmit("php:install", p) }

/* ============================ 类型 ============================ */

// ServerVersion 本地可用的一个 PHP 版本
type ServerVersion struct {
	Version string `json:"version"` // 如 8.3.12
	Path    string `json:"path"`    // php.exe 完整路径
	Source  string `json:"source"`  // bundled | downloaded | local
}

// RemoteVersion 线上可安装版本
type RemoteVersion struct {
	Version string `json:"version"`
	URL     string `json:"url"`     // NTS 包地址
	NTSURL  string `json:"ntsUrl"`  // 同 URL，字段保留便于前端分别展示
	ZTSURL  string `json:"ztsUrl"`  // ZTS（线程安全）包地址
	Size    int64  `json:"size"`    // NTS 包大小
	Release string `json:"release"` // 所属产品线，如 8.3
}

// InstanceConfig 本地 PHP 实例配置
type InstanceConfig struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	Port       int    `json:"port"`
	NTS        bool   `json:"nts"`        // true=nts 构型，false=zts 构型
	DocRoot    string `json:"docRoot"`    // 站点根目录；空 = 实例目录下 www/
	Router     string `json:"router"`     // 路由脚本（如 index.php），空 = 不使用
	EnableLogs bool   `json:"enableLogs"` // 是否写 access.log（php -S -v）

	// ---- Docker 模式（Mode 为空即本地模式，历史配置无需迁移）----
	Mode          string `json:"mode"`          // local | docker
	Image         string `json:"image"`         // 如 php:8.3-cli
	ContainerName string `json:"containerName"` // 留空自动生成 go-tools-php-<实例ID>
}

// InstanceStatus 实例运行状态
type InstanceStatus struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	Port             int    `json:"port"`
	Running          bool   `json:"running"`
	PID              int    `json:"pid"`
	UptimeSeconds    int64  `json:"uptimeSeconds"`
	DocRoot          string `json:"docRoot"`
	ConfigPath       string `json:"configPath"` // php.ini 路径
	ExtensionsLoaded int    `json:"extensionsLoaded"`
	DisableFunctions string `json:"disableFunctions"`

	// ---- Docker 模式专属（本地模式恒为空）----
	Mode           string `json:"mode"`           // local | docker
	Image          string `json:"image"`          // 镜像
	Container      string `json:"container"`      // 容器名
	ContainerID    string `json:"containerId"`    // 容器 ID
	ContainerState string `json:"containerState"` // running / exited / created
}

// InstallProgress 版本安装进度事件负载（stage 语义与 redis/mysql 一致）
type InstallProgress struct {
	Version string `json:"version"`
	Stage   string `json:"stage"` // info|downloading|extracting|done|error
	Percent int    `json:"percent"`
	Note    string `json:"note"`
}

// PHPInfo php -i 解析出的关键配置概览
type PHPInfo struct {
	Version           string   `json:"version"`
	SAPI              string   `json:"sapi"`
	PHPIni            string   `json:"phpIni"`
	ExtensionDir      string   `json:"extensionDir"`
	MemoryLimit       string   `json:"memoryLimit"`
	UploadMaxFilesize string   `json:"uploadMaxFilesize"`
	PostMaxSize       string   `json:"postMaxSize"`
	MaxExecTime       string   `json:"maxExecutionTime"`
	DisplayErrors     string   `json:"displayErrors"`
	Timezone          string   `json:"timezone"`
	DisableFunctions  string   `json:"disableFunctions"`
	Loaded            []string `json:"loaded"`
}

// ExtensionInfo 一个候选扩展
type ExtensionInfo struct {
	Name      string `json:"name"`      // 如 curl
	DLL       string `json:"dll"`       // 如 php_curl.dll
	Available bool   `json:"available"` // ext 目录下存在 dll
	Enabled   bool   `json:"enabled"`   // php.ini 中未注释的 extension= 行
	Loaded    bool   `json:"loaded"`    // php -m 实测已加载
	System    bool   `json:"system"`    // 内置于核心（无法开关，如 core/date/libxml）
}

/* ============================ 目录约定 ============================ */

// goToolsDir 数据目录 data/php：实例登记/配置缓存（基础目录设置页可改，见 appdirs）
func goToolsDir() (string, error) { return appdirs.Data("php") }

// pluginsRoot 插件目录 plugins/php：内置版本、安装版本与离线 zip 的统一位置（确保存在）
func pluginsRoot() (string, error) { return appdirs.Plugins("php") }

// PluginsDir 导出版：返回插件目录（供前端展示离线包放置指引）
func PluginsDir() (string, error) { return pluginsRoot() }

// installedMarker 安装版标记（区别于发布内置版，允许删除）
const installedMarker = ".go-tools-installed"

func isInstalledVersion(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, installedMarker))
	return err == nil && !st.IsDir()
}

func markInstalled(dir string) {
	_ = os.WriteFile(filepath.Join(dir, installedMarker), []byte("installed by go-tools\n"), 0o644)
}

func localsRegistryPath() (string, error) {
	d, err := goToolsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "php-locals.json"), nil
}

func instancesFilePath() (string, error) {
	d, err := goToolsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "php-instances.json"), nil
}

// instanceDir 实例工作目录（php.ini / 日志 / 默认站点根）
func instanceDir(id string) (string, error) {
	d, err := goToolsDir()
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("实例 ID 不能为空")
	}
	dir := filepath.Join(d, "php-instances", filepath.Base(id))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

/* ============================ 版本工具 ============================ */

var versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// compareVersion 按数字段比较；a<b 返回 -1，相等 0，a>b 返回 1
func compareVersion(a, b string) int {
	as, bs := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(as) || i < len(bs); i++ {
		var av, bv int
		if i < len(as) {
			av, _ = strconv.Atoi(as[i])
		}
		if i < len(bs) {
			bv, _ = strconv.Atoi(bs[i])
		}
		if av != bv {
			if av < bv {
				return -1
			}
			return 1
		}
	}
	return 0
}

var verExtractRe = regexp.MustCompile(`(\d+\.\d+(?:\.\d+)*)`)

// normalizeVersionName 目录名不是纯版本号时提取版本号（php-8.3.12-Win32-vs16-x64 → 8.3.12）
func normalizeVersionName(name string) string {
	if versionRe.MatchString(name) {
		return name
	}
	if m := verExtractRe.FindString(name); m != "" {
		if strings.Count(m, ".") == 1 {
			m += ".0"
		}
		return m
	}
	return name
}

// findPHP 在版本目录内定位 php 可执行文件：根、一级子目录
func findPHP(dir string) string {
	try := []string{filepath.Join(dir, binFileName)}
	if subs, err := os.ReadDir(dir); err == nil {
		for _, s := range subs {
			if s.IsDir() {
				sub := filepath.Join(dir, s.Name())
				try = append(try, filepath.Join(sub, binFileName))
			}
		}
	}
	for _, p := range try {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// isNTSPath 路径是否指向 nts 构型（目录名含 -nts-）
func isNTSPath(p string) bool {
	return strings.Contains(strings.ToLower(p), "-nts-")
}

/* ============================ 版本库 ============================ */

// ListVersions 聚合四渠道版本，按版本号降序；同版本优先 bundled > downloaded > local > system
func ListVersions() ([]ServerVersion, error) {
	out := map[string]ServerVersion{}
	priority := map[string]int{"bundled": 0, "downloaded": 1, "local": 2, "system": 3}

	add := func(source, ver, path string) {
		if ver == "" || path == "" {
			return
		}
		if old, ok := out[ver]; ok && priority[old.Source] <= priority[source] {
			return
		}
		out[ver] = ServerVersion{Version: ver, Path: path, Source: source}
	}

	// 插件目录 <exedir>/plugins/php/<ver>/：有安装标记→downloaded；无标记→bundled
	if root, err := pluginsRoot(); err == nil {
		if subs, err := os.ReadDir(root); err == nil {
			for _, s := range subs {
				if !s.IsDir() {
					continue
				}
				verDir := filepath.Join(root, s.Name())
				src := "bundled"
				if isInstalledVersion(verDir) {
					src = "downloaded"
				}
				if p := findPHP(verDir); p != "" {
					add(src, normalizeVersionName(s.Name()), p)
				}
			}
		}
	}
	// local：php-locals.json 登记
	for _, lv := range loadLocals() {
		if st, err := os.Stat(lv.Path); err == nil && !st.IsDir() && strings.EqualFold(st.Name(), binFileName) {
			add("local", lv.Version, lv.Path)
		} else if st != nil && st.IsDir() {
			if p := findPHP(lv.Path); p != "" { // 登记的是目录
				add("local", lv.Version, p)
			}
		}
	}
	// system：系统包管理器装的（Linux 上这是唯一可行的来源）
	if bin := sysx.Which(binFileName); bin != "" {
		if ver := sysx.ProbeVersion(bin, "-v"); ver != "" {
			add("system", ver, bin)
		}
	}

	list := make([]ServerVersion, 0, len(out))
	for _, v := range out {
		if v.Path != "" {
			list = append(list, v)
		}
	}
	sort.Slice(list, func(i, j int) bool { return compareVersion(list[i].Version, list[j].Version) > 0 })
	return list, nil
}

type localEntry struct {
	Version string `json:"version"`
	Path    string `json:"path"` // 目录或 php 可执行文件路径
}

func loadLocals() []localEntry {
	path, err := localsRegistryPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var list []localEntry
	if json.Unmarshal(data, &list) != nil {
		return nil
	}
	return list
}

func saveLocals(list []localEntry) error {
	path, err := localsRegistryPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// AddLocalVersion 注册本机已有的 PHP 目录（或 php 可执行文件路径）
func AddLocalVersion(dir string) error {
	dir = strings.Trim(dir, `"`)
	if dir == "" {
		return fmt.Errorf("目录不能为空")
	}
	exe := ""
	if st, err := os.Stat(dir); err == nil {
		if st.IsDir() {
			exe = findPHP(dir)
		} else if strings.EqualFold(st.Name(), binFileName) {
			exe = dir
		}
	}
	if exe == "" {
		return fmt.Errorf("目录中未找到 %s（支持根目录或一级子目录）", binFileName)
	}
	// 版本号优先问二进制自己：Linux 的 /usr/bin/php 路径里没有版本信息。
	ver := ""
	if v, err := probeVersion(exe); err == nil {
		ver = v
	}
	if strings.Count(ver, ".") < 2 {
		ver = normalizeVersionName(filepath.Base(filepath.Dir(exe)))
	}
	if strings.Count(ver, ".") < 2 {
		ver = normalizeVersionName(filepath.Base(dir))
	}
	list := loadLocals()
	for i := range list {
		if strings.EqualFold(list[i].Path, exe) {
			return nil // 幂等
		}
	}
	list = append(list, localEntry{Version: ver, Path: exe})
	return saveLocals(list)
}

// RemoveVersion 删除版本：内置不可删；运行中实例引用的版本不可删
func RemoveVersion(version string) error {
	vers, err := ListVersions()
	if err != nil {
		return err
	}
	var target *ServerVersion
	for i := range vers {
		if vers[i].Version == version {
			target = &vers[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("版本 %s 未安装", version)
	}
	if target.Source == "bundled" {
		return fmt.Errorf("内置版本不可删除")
	}
	if target.Source == "system" {
		return fmt.Errorf("版本 %s 由系统包管理器提供，请用 %s 卸载", version, sysx.PkgManager())
	}
	cfgs, _ := ListInstances()
	for _, c := range cfgs {
		if c.Version == version && tcpUp(c.Port) {
			return fmt.Errorf("实例 %s 正在使用该版本，请先停止", c.Name)
		}
	}
	switch target.Source {
	case "downloaded":
		root, err := pluginsRoot()
		if err != nil {
			return err
		}
		// 同版本可能存在 nts / zts 两个目录，一并删除
		var lastErr error
		removed := false
		if subs, err := os.ReadDir(root); err == nil {
			for _, s := range subs {
				if !s.IsDir() || normalizeVersionName(s.Name()) != version {
					continue
				}
				d := filepath.Join(root, s.Name())
				if !isInstalledVersion(d) {
					return fmt.Errorf("目录 %s 非 go-tools 安装版（内置或手工放置），不可删除", s.Name())
				}
				if err := os.RemoveAll(d); err != nil {
					lastErr = err
				} else {
					removed = true
				}
			}
		}
		if !removed && lastErr == nil {
			return fmt.Errorf("未找到版本 %s 的安装目录", version)
		}
		return lastErr
	case "local":
		list := loadLocals()
		out := list[:0]
		removed := false
		for _, l := range list {
			if l.Version == version {
				removed = true
				continue
			}
			out = append(out, l)
		}
		if !removed {
			return fmt.Errorf("未找到本机登记的版本 %s", version)
		}
		return saveLocals(out)
	}
	return fmt.Errorf("未知来源: %s", target.Source)
}

// probeVersion 执行 php -r "echo PHP_VERSION;" 获取真实版本号
func probeVersion(exe string) (string, error) {
	cmd := exec.Command(exe, "-r", "echo PHP_VERSION;")
	cmd.SysProcAttr = hideWindowAttr()
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	s := strings.TrimSpace(string(out))
	if versionRe.MatchString(s) {
		return s, nil
	}
	return "", fmt.Errorf("无法解析版本号: %s", s)
}

/* ============================ 在线版本（windows.php.net） ============================ */

const (
	phpReleasesIndex = "https://windows.php.net/downloads/releases/"
	phpArchivesIndex = "https://windows.php.net/downloads/releases/archives/"
)

// phpZipHrefRe 匹配官方目录页中的发行包文件名（如 php-8.3.36-nts-Win32-vs16-x64.zip），
// 自动忽略 debug-pack / devel-pack / src 等非安装包。
var phpZipHrefRe = regexp.MustCompile(`href="(php-(\d+\.\d+\.\d+)(-nts)?-Win32-(?:vs|vc)\d+-x64\.zip)"`)

var (
	zipIdxMu sync.Mutex
	zipIdx   map[string]string // releases 目录：key "<ver>|nts" / "<ver>|zts" → 完整 URL
	zipIdxAt time.Time
	arcIdx   map[string]string // archives 目录：旧版本发行包
	arcIdxAt time.Time
)

// parseZipIndex 从官方目录页 HTML 抽取发行包：key "<ver>|nts|zts" → 完整 URL
func parseZipIndex(base, body string) map[string]string {
	idx := map[string]string{}
	for _, m := range phpZipHrefRe.FindAllStringSubmatch(body, -1) {
		name, ver, nts := m[1], m[2], m[3] != ""
		kind := "zts"
		if nts {
			kind = "nts"
		}
		key := ver + "|" + kind
		if _, ok := idx[key]; !ok {
			idx[key] = base + name
		}
	}
	return idx
}

// phpReleaseIndex 解析官方 releases 目录索引（带 10 分钟缓存）。force=true 强制刷新。
// 失败时返回上次缓存（可能为 nil），由调用方回退到探测逻辑。
func phpReleaseIndex(force bool) map[string]string {
	zipIdxMu.Lock()
	defer zipIdxMu.Unlock()
	if !force && len(zipIdx) > 0 && time.Since(zipIdxAt) < 10*time.Minute {
		return zipIdx
	}
	body, err := httpGet(phpReleasesIndex)
	if err != nil {
		return zipIdx
	}
	if idx := parseZipIndex(phpReleasesIndex, body); len(idx) > 0 {
		zipIdx, zipIdxAt = idx, time.Now()
	}
	return zipIdx
}

// phpArchiveIndex 解析官方 archives 目录（历史版本，如 7.4.x / 8.0.x），同样 10 分钟缓存。
func phpArchiveIndex(force bool) map[string]string {
	zipIdxMu.Lock()
	defer zipIdxMu.Unlock()
	if !force && len(arcIdx) > 0 && time.Since(arcIdxAt) < 10*time.Minute {
		return arcIdx
	}
	body, err := httpGet(phpArchivesIndex)
	if err != nil {
		return arcIdx
	}
	if idx := parseZipIndex(phpArchivesIndex, body); len(idx) > 0 {
		arcIdx, arcIdxAt = idx, time.Now()
	}
	return arcIdx
}

// FetchRemoteVersions 解析官方 releases 目录索引得到可安装版本（含真实文件名，避免拼接错误 404）。
// 网络失败时回退内置常量清单；Linux 则问发行版仓库（官方包都是 Windows 构建）。
func FetchRemoteVersions() ([]RemoteVersion, error) {
	if systemPackaged {
		list := systemRemoteVersions()
		if len(list) == 0 {
			// 空列表在界面上等于“没东西可装”，得把原因和命令一并说清楚
			return nil, fmt.Errorf("%s", sysx.PackageHint(systemPkgNames))
		}
		return list, nil
	}
	idx := phpReleaseIndex(true)
	if len(idx) == 0 {
		return phpFallbackList(), nil
	}
	merged := make(map[string]string, len(idx))
	for k, v := range idx {
		merged[k] = v // releases 目录优先
	}
	// 旧版本在 archives 目录：每个 x.y 系列只保留最新补丁，避免列表膨胀
	arc := phpArchiveIndex(false)
	best := map[string]string{} // "x.y|kind" → 最高小版本号
	for key := range arc {
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 || compareVersion(parts[0], "7.4.0") < 0 {
			continue
		}
		dot := strings.LastIndex(parts[0], ".")
		if dot <= 0 {
			continue
		}
		sk := parts[0][:dot] + "|" + parts[1]
		if cur, ok := best[sk]; !ok || compareVersion(parts[0], cur) > 0 {
			best[sk] = parts[0]
		}
	}
	for sk, ver := range best {
		key := ver + "|" + sk[strings.LastIndex(sk, "|")+1:]
		if _, exists := merged[key]; exists {
			continue
		}
		if u := arc[key]; u != "" {
			merged[key] = u
		}
	}
	type agg struct{ nts, zts string }
	vers := map[string]*agg{}
	for key, url := range merged {
		parts := strings.SplitN(key, "|", 2)
		if len(parts) != 2 {
			continue
		}
		a, ok := vers[parts[0]]
		if !ok {
			a = &agg{}
			vers[parts[0]] = a
		}
		if parts[1] == "nts" {
			a.nts = url
		} else {
			a.zts = url
		}
	}
	out := make([]RemoteVersion, 0, len(vers))
	for ver, a := range vers {
		rv := RemoteVersion{Version: ver, Release: strings.Join(strings.Split(ver, ".")[:2], "."), NTSURL: a.nts, ZTSURL: a.zts}
		rv.URL = a.nts
		if rv.URL == "" {
			rv.URL = a.zts
		}
		out = append(out, rv)
	}
	// 并发获取 NTS 包大小（仅用于展示）
	var wg sync.WaitGroup
	for i := range out {
		if out[i].URL == "" {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if ok, size := headExists(out[i].URL); ok && size > 0 {
				out[i].Size = size
			}
		}(i)
	}
	wg.Wait()
	sort.Slice(out, func(i, j int) bool { return compareVersion(out[i].Version, out[j].Version) > 0 })
	return out, nil
}

// via compiler 代号命名后缀（旧版用 vc15/vc14，新版用 vs16/vs17）
func phpVSCandidates(ver string) []string {
	switch {
	case strings.HasPrefix(ver, "8.4"), strings.HasPrefix(ver, "8.5"):
		return []string{"vs17", "vs16"}
	case strings.HasPrefix(ver, "7."):
		return []string{"vc15", "vc14", "vs16"}
	default:
		return []string{"vs16", "vs17", "vc15"}
	}
}

// resolvePHPZipURL 解析安装包直链：优先官方索引；否则按命名规则逐候选 HEAD 探测（releases → archives）。
func resolvePHPZipURL(ver string, nts bool) (string, bool) {
	kind, mid := "zts", ""
	if nts {
		kind, mid = "nts", "-nts"
	}
	if idx := phpReleaseIndex(false); len(idx) > 0 {
		if u := idx[ver+"|"+kind]; u != "" {
			return u, true
		}
	}
	if idx := phpArchiveIndex(false); len(idx) > 0 {
		if u := idx[ver+"|"+kind]; u != "" {
			return u, true
		}
	}
	for _, dir := range []string{phpReleasesIndex, phpArchivesIndex} {
		for _, vs := range phpVSCandidates(ver) {
			u := fmt.Sprintf("%sphp-%s%s-Win32-%s-x64.zip", dir, ver, mid, vs)
			if ok, _ := headExists(u); ok {
				return u, true
			}
		}
	}
	return "", false
}

// phpZipName 按命名规则拼接包名（仅用于回退清单展示）
func phpZipName(ver string, nts bool) string {
	vs := phpVSCandidates(ver)[0]
	mid := ""
	if nts {
		mid = "-nts"
	}
	return fmt.Sprintf("php-%s%s-Win32-%s-x64.zip", ver, mid, vs)
}

// 网络探测全失败时的回退推荐清单（仅版本号，URL 按模式构造，size 未知）
var phpFallback = []string{"8.5.12", "8.4.27", "8.3.36", "8.2.35", "8.1.34", "8.0.30", "7.4.33"}

func phpFallbackList() []RemoteVersion {
	var out []RemoteVersion
	for _, v := range phpFallback {
		nts := phpReleasesIndex + phpZipName(v, true)
		zts := phpReleasesIndex + phpZipName(v, false)
		out = append(out, RemoteVersion{
			Version: v, Release: strings.Join(strings.Split(v, ".")[:2], "."),
			URL: nts, NTSURL: nts, ZTSURL: zts,
		})
	}
	return out
}

func httpGet(url string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", phpUserAgent())
	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func phpUserAgent() string {
	return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
}

func headExists(url string) (bool, int64) {
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return false, 0
	}
	req.Header.Set("User-Agent", phpUserAgent())
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, 0
	}
	defer resp.Body.Close()
	ok := resp.StatusCode < 400 || resp.StatusCode == 405
	return ok, resp.ContentLength
}

/* ============================ 版本安装（下载+解压拍平） ============================ */

var (
	installMu  sync.Mutex
	installing = map[string]bool{}
)

// InstallVersion 异步安装版本；nts=true 装非线程安全版。进度经 "php:install" 事件推送。
func InstallVersion(version string, nts bool) error {
	if systemPackaged {
		return installSystemVersion(version)
	}
	if !versionRe.MatchString(version) {
		return fmt.Errorf("非法版本号: %s", version)
	}
	installMu.Lock()
	if installing[version] {
		installMu.Unlock()
		return fmt.Errorf("版本 %s 正在安装中", version)
	}
	installMu.Unlock()

	vers, _ := ListVersions()
	for _, v := range vers {
		if v.Version == version {
			return fmt.Errorf("版本 %s 已安装", version)
		}
	}
	go installAsync(version, nts)
	return nil
}

/* ============================ 系统包（Linux） ============================ */

// systemRemoteVersions 把发行版仓库里的可安装版本包成统一的 RemoteVersion。
func systemRemoteVersions() []RemoteVersion {
	cands := sysx.PackageCandidates(systemPkgNames)
	out := make([]RemoteVersion, 0, len(cands))
	for _, c := range cands {
		// 归一：仓库里的 "8.1.2-1ubuntu2.14" 与 php -v 报告的 "8.1.2"
		// 是同一个包，不统一版本库与实例就对不上。
		ver := sysx.TrimVersion(c.Version)
		if ver == "" {
			continue
		}
		out = append(out, RemoteVersion{Version: ver, URL: sysx.PkgScheme + c.Pkg})
	}
	return out
}

// systemPackageVersion 反查“版本号 → 包名”。
func systemPackageVersion(version string) string {
	for _, c := range sysx.PackageCandidates(systemPkgNames) {
		if sysx.TrimVersion(c.Version) == version {
			return c.Pkg
		}
	}
	return ""
}

// installSystemVersion 交给系统包管理器安装。
//
// nts 参数在这里没有意义：线程安全与否是 Windows VC 构建的选择，Linux 包只有一个。
func installSystemVersion(version string) error {
	pkg := systemPackageVersion(version)
	if pkg == "" {
		return fmt.Errorf("发行版仓库里没有版本 %s（镜像可能已更新，请刷新在线版本列表）", version)
	}
	installMu.Lock()
	if installing[version] {
		installMu.Unlock()
		return fmt.Errorf("版本 %s 正在安装中", version)
	}
	installing[version] = true
	installMu.Unlock()

	go func() {
		defer func() {
			installMu.Lock()
			delete(installing, version)
			installMu.Unlock()
		}()
		info := func(note string) {
			emitInstall(InstallProgress{Version: version, Stage: "info", Note: note})
		}
		if sysx.PkgManager() == "" {
			emitInstall(InstallProgress{Version: version, Stage: "error",
				Note: "本机没有 dnf/yum/apt-get，请手工安装 php-cli 后刷新版本库"})
			return
		}
		info(fmt.Sprintf("使用系统包管理器安装 %s %s（需 root）…", pkg, version))
		err := sysx.InstallPackages([]string{pkg}, func(line string) {
			if strings.TrimSpace(line) != "" {
				info(line)
			}
		})
		if err != nil {
			emitInstall(InstallProgress{Version: version, Stage: "error", Note: err.Error()})
			return
		}
		info("安装完成；版本库刷新后会出现 system 来源的版本")
		emitInstall(InstallProgress{Version: version, Stage: "done", Percent: 100})
	}()
	return nil
}

func installAsync(version string, nts bool) {
	defer func() {
		installMu.Lock()
		delete(installing, version)
		installMu.Unlock()
	}()
	info := func(note string) { emitInstall(InstallProgress{Version: version, Stage: "info", Note: note}) }
	root, err := pluginsRoot()
	if err != nil {
		emitInstall(InstallProgress{Version: version, Stage: "error", Note: err.Error()})
		return
	}
	suffix := "-nts-Win32"
	if !nts {
		suffix = "-Win32"
	}
	destDir := filepath.Join(root, fmt.Sprintf("php-%s%s-x64", version, suffix))

	info(fmt.Sprintf("开始安装 PHP %s（%s）：检查离线包…", version, map[bool]string{true: "nts", false: "zts"}[nts]))

	zipPath, isTemp, err := locateOrDownloadZip(version, nts)
	if err != nil {
		emitInstall(InstallProgress{Version: version, Stage: "error", Note: err.Error()})
		return
	}
	if isTemp {
		defer os.Remove(zipPath)
		info("下载完成，准备解压")
	} else {
		info("发现离线包：" + zipPath)
	}

	tmpDir := destDir + ".extract"
	_ = os.RemoveAll(tmpDir)
	info("正在解压到：" + destDir)
	emitInstall(InstallProgress{Version: version, Stage: "extracting", Percent: 100})
	if err := extractZip(zipPath, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		emitInstall(InstallProgress{Version: version, Stage: "error", Note: fmt.Sprintf("解压失败: %v", err)})
		return
	}
	inner := findPHP(tmpDir)
	if inner == "" {
		os.RemoveAll(tmpDir)
		emitInstall(InstallProgress{Version: version, Stage: "error", Note: "安装包中未找到 php.exe"})
		return
	}
	payload := filepath.Dir(inner) // php-x.y.z-Win32-vsNN-x64/php.exe → 发行版根
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		os.RemoveAll(tmpDir)
		emitInstall(InstallProgress{Version: version, Stage: "error", Note: err.Error()})
		return
	}
	if err := moveTree(payload, destDir); err != nil {
		os.RemoveAll(tmpDir)
		os.RemoveAll(destDir)
		emitInstall(InstallProgress{Version: version, Stage: "error", Note: fmt.Sprintf("整理目录失败: %v", err)})
		return
	}
	os.RemoveAll(tmpDir)
	markInstalled(destDir)
	info("安装完成：" + destDir)
	emitInstall(InstallProgress{Version: version, Stage: "done", Percent: 100, Note: destDir})
}

// locateOrDownloadZip 优先插件目录离线 zip；否则按官方 URL 模式直接下载（临时文件）
func locateOrDownloadZip(version string, nts bool) (string, bool, error) {
	pattern := fmt.Sprintf("php-%s-Win32*.zip", version)
	if nts {
		pattern = fmt.Sprintf("php-%s-nts-Win32*.zip", version)
	}
	if root, err := pluginsRoot(); err == nil {
		if matches, _ := filepath.Glob(filepath.Join(root, pattern)); len(matches) > 0 {
			return matches[0], false, nil
		}
		if matches, _ := filepath.Glob(filepath.Join(root, fmt.Sprintf("php-%s*.zip", version))); len(matches) > 0 {
			return matches[0], false, nil
		}
	}
	url := ""
	if u, ok := resolvePHPZipURL(version, nts); ok {
		url = u
	}
	if url == "" {
		return "", false, fmt.Errorf("未找到 PHP %s 的官方安装包（可能已下线），可将 zip 放入插件目录后重试", version)
	}
	info := InstallProgress{Version: version, Stage: "info", Note: "开始下载：" + filepath.Base(url)}
	emitInstall(info)
	root, err := pluginsRoot()
	if err != nil {
		return "", false, err
	}
	tmp := filepath.Join(root, version+".download.zip")
	if err := downloadFile(url, tmp, version); err != nil {
		os.Remove(tmp)
		return "", false, fmt.Errorf("下载失败 %s：%v", filepath.Base(url), err)
	}
	return tmp, true, nil
}

func downloadFile(url, dest, version string) error {
	client := &http.Client{Timeout: 30 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", phpUserAgent())
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
				if pct/5 != lastPct/5 { // 5% 步进推送
					lastPct = pct
					emitInstall(InstallProgress{Version: version, Stage: "downloading", Percent: pct, Note: filepath.Base(url)})
				}
			}
		}
		if rerr == io.EOF {
			return nil
		}
		if rerr != nil {
			return rerr
		}
	}
}

// extractZip 安全解压（拒绝路径穿越）
func extractZip(zipPath, destDir string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	for _, f := range r.File {
		name := filepath.Clean(strings.ReplaceAll(f.Name, "\\", "/"))
		target := filepath.Join(destDir, name)
		if !strings.HasPrefix(target, filepath.Clean(destDir)+string(os.PathSeparator)) {
			continue
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if f.FileInfo().IsDir() || strings.HasSuffix(strings.ToLower(name), ".exe") || strings.HasSuffix(strings.ToLower(name), ".dll") {
			mode = 0o755
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// moveTree 把 src 目录内容整体搬到 dst（同卷 rename 优先，跨卷复制+删除兜底）
func moveTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		from := filepath.Join(src, e.Name())
		to := filepath.Join(dst, e.Name())
		if err := os.Rename(from, to); err != nil {
			if e.IsDir() {
				if err := copyTree(from, to); err != nil {
					return err
				}
				if err := os.RemoveAll(from); err != nil {
					return err
				}
				continue
			}
			data, rerr := os.ReadFile(from)
			if rerr != nil {
				return rerr
			}
			if werr := os.WriteFile(to, data, 0o755); werr != nil {
				return werr
			}
			if rerr := os.Remove(from); rerr != nil {
				return rerr
			}
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, st os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, p)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if st.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			return rerr
		}
		return os.WriteFile(target, data, 0o755)
	})
}

/* ============================ 版本定位（内部） ============================ */

// phpExe 按版本 + 构型定位 php.exe
func phpExe(version string, nts bool) string {
	vers, _ := ListVersions()
	for _, v := range vers {
		if v.Version == version {
			return v.Path
		}
	}
	// 同版本多构型：优先匹配构型
	if root, err := pluginsRoot(); err == nil {
		want := "-Win32"
		if nts {
			want = "-nts-Win32"
		}
		if subs, err := os.ReadDir(root); err == nil {
			for _, s := range subs {
				name := strings.ToLower(s.Name())
				if s.IsDir() && normalizeVersionName(s.Name()) == version && strings.Contains(name, want) {
					if p := findPHP(filepath.Join(root, s.Name())); p != "" {
						return p
					}
				}
			}
		}
	}
	return ""
}
