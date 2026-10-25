package npm

// ============================================================
// 本地 Node.js / npm 管理
//
// 结构复刻 tools/mysql/server.go（redis 同款范式），能力：
//   - 版本库：nodejs.org 官方 dist（index.json + node-v<ver>-win-x64.zip）
//   - bundled    ：<exedir>/plugins/npm/<ver>/（发布内置，无安装标记）
//   - downloaded ：在线获取/离线 zip 安装，统一落 <exedir>/plugins/npm/<ver>/ 并写 .go-tools-installed 标记
//   - local      ：用户手动注册的本机已有目录（登记在 go-tools/npm-locals.json）
//   - 实例：go-tools/npm-instances.json；每个实例是一个「本地静态站点服务」，
//     由本程序内置的 Go HTTP 服务器承载（不依赖 http-server 等外部包，零网络风险），
//     记录的 Node 版本用于展示与「用该 Node 跑 npm 命令」。
//     目录：go-tools/npm-instances/<id>/{logs/, www/}
//   - npm 配置：go-tools/npmrc（本程序托管的 .npmrc），所有 npm 命令统一带
//     --userconfig 走这份文件，避免误改用户 ~/.npmrc；
//     同时提供「应用到用户全局 ~/.npmrc」「打开目录」等主动操作。
//   - 全局包：npm ls -g --depth=0 --json / npm install -g <name>（进度经 "npm:install" 事件）
//   - 安装进度经 "npm:install" 事件推送（InstallProgress，stage 与 redis/mysql 一致）。
// ============================================================

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net"
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

func emitInstall(p InstallProgress) { srvEmit("npm:install", p) }

/* ============================ 类型 ============================ */

// ServerVersion 本地可用的一个 Node.js 版本
type ServerVersion struct {
	Version string `json:"version"` // 如 22.14.0
	Path    string `json:"path"`    // node.exe 完整路径
	Source  string `json:"source"`  // bundled | downloaded | local
	NpmVer  string `json:"npmVer"`  // 随包发行的 npm 版本（读不到为空）
}

// RemoteVersion 线上可安装版本
type RemoteVersion struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	Size    int64  `json:"size"`
	LTS     string `json:"lts"` // LTS 代号，非 LTS 为空
	Files   string `json:"files"`
}

// InstanceConfig 本地实例配置（静态站点服务）
type InstanceConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"` // 关联的 Node 版本（展示 + 跑 npm 命令）；容器模式不用（置空）
	Port        int    `json:"port"`
	DocRoot     string `json:"docRoot"`   // 站点根目录；空 = 实例目录下 www/
	SPA         bool   `json:"spa"`       // 找不到文件时回退 index.html（前端单页应用）
	Directory   bool   `json:"directory"` // 无 index.html 时允许列目录
	OpenBrowser bool   `json:"openBrowser"`
	// Mode 运行模式：local = 本程序内置的 Go 静态服务器，docker = nginx 容器承载站点目录；
	// 历史配置没这个字段，零值按本地处理
	Mode string `json:"mode"`
	// Image 容器模式的镜像（如 nginx:alpine）；站点目录挂到 /usr/share/nginx/html
	Image string `json:"image"`
	// ContainerName 容器名，留空按实例 ID 自动生成（go-tools-npm-<id>）
	ContainerName string `json:"containerName"`
}

// InstanceStatus 实例运行状态
type InstanceStatus struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Version        string  `json:"version"`
	Port           int     `json:"port"`
	Running        bool    `json:"running"`
	PID            int     `json:"pid"`
	UptimeSeconds  int64   `json:"uptimeSeconds"`
	DocRoot        string  `json:"docRoot"`
	Files          int     `json:"files"`
	Bytes          int64   `json:"bytes"`
	Requests       int64   `json:"requests"`
	RequestsPerSec float64 `json:"requestsPerSec"`
	NodePath       string  `json:"nodePath"`
	NpmVer         string  `json:"npmVer"`
	Registry       string  `json:"registry"`
	Mode           string  `json:"mode"`
	Image          string  `json:"image"`
	Container      string  `json:"container"`      // 容器名
	ContainerID    string  `json:"containerId"`    // 容器短 ID（tooltip 里给全长）
	ContainerState string  `json:"containerState"` // running | exited | created…
}

// InstallProgress 安装进度事件负载（stage 语义与 redis/mysql 一致）
type InstallProgress struct {
	Version string `json:"version"`
	Stage   string `json:"stage"` // info|downloading|extracting|done|error
	Percent int    `json:"percent"`
	Note    string `json:"note"`
}

// NpmPackage 一个已安装的包
type NpmPackage struct {
	Name         string `json:"name"`
	Version      string `json:"version"`
	Description  string `json:"description"`
	Path         string `json:"path"`
	Manager      string `json:"manager"` // 归属的包管理器（npm / yarn / pnpm …）
	HasBin       bool   `json:"hasBin"`  // 是否提供命令行入口
	Builtin      bool   `json:"builtin"` // 随 Node 发行（npm 本体），不可卸载
	DependentsOn string `json:"-"`
}

// NpmConfigKV 一条 npm 配置
type NpmConfigKV struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// NpmPathBind 用户 PATH 绑定状态（强制切换本地 npm 命令）
type NpmPathBind struct {
	Bound         bool     `json:"bound"`         // 用户 PATH 最前是否为某 Node 目录
	BoundDir      string   `json:"boundDir"`      // 当前绑定（最前）的 Node 目录
	BoundVersion  string   `json:"boundVersion"`  // 该目录 Node 版本
	BoundNpm      string   `json:"boundNpm"`      // 该目录生效 npm 版本
	BoundScope    string   `json:"boundScope"`    // 生效绑定所在作用域：system | user
	OtherNodeDirs []string `json:"otherNodeDirs"` // 用户 PATH 里其它 Node 目录（绑定时移除）
	SystemHasNode bool     `json:"systemHasNode"` // 系统 PATH 里存在 Node 目录（会盖过用户 PATH，仅告警不改）
	CanUndo       bool     `json:"canUndo"`       // 是否存在原始 PATH 备份
	UndoNote      string   `json:"undoNote"`      // 备份时间说明
	Platform      string   `json:"platform"`
	Note          string   `json:"note"`
}

/* ============================ 目录约定 ============================ */

// goToolsDir 数据目录 data/npm：实例登记、托管 npmrc、版本切换记录（基础目录设置页可改，见 appdirs）
func goToolsDir() (string, error) { return appdirs.Data("npm") }

// pluginsRoot 插件目录 plugins/npm（确保存在）
func pluginsRoot() (string, error) { return appdirs.Plugins("npm") }

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
	return filepath.Join(d, "npm-locals.json"), nil
}

func instancesFilePath() (string, error) {
	d, err := goToolsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "npm-instances.json"), nil
}

// managedNpmrcPath 本程序托管的 .npmrc（所有 npm 命令统一 --userconfig 指向它）
func managedNpmrcPath() (string, error) {
	d, err := goToolsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "npmrc"), nil
}

// userNpmrcPath 用户全局 ~/.npmrc
func userNpmrcPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".npmrc")
}

// instanceDir 实例目录（logs/ + 默认 www/）
func instanceDir(id string) (string, error) {
	d, err := goToolsDir()
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("实例 ID 不能为空")
	}
	dir := filepath.Join(d, "npm-instances", filepath.Base(id))
	for _, sub := range []string{"", "logs", "www"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return "", err
		}
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

// normalizeVersionName node-v22.14.0-win-x64 → 22.14.0
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

// findNode 在版本目录内定位 node.exe：根、一级子目录
func findNode(dir string) string {
	try := []string{filepath.Join(dir, "node.exe")}
	if subs, err := os.ReadDir(dir); err == nil {
		for _, s := range subs {
			if s.IsDir() {
				try = append(try, filepath.Join(dir, s.Name(), "node.exe"))
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

// detectNpmVer 读取随 Node 发行的 npm 版本
func detectNpmVer(nodeExe string) string {
	pkg := filepath.Join(filepath.Dir(nodeExe), "node_modules", "npm", "package.json")
	data, err := os.ReadFile(pkg)
	if err != nil {
		return ""
	}
	var m struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(data, &m) != nil {
		return ""
	}
	return m.Version
}

/* ============================ 版本库 ============================ */

// ListVersions 聚合三渠道版本，按版本号降序；同版本优先 bundled > downloaded > local
func ListVersions() ([]ServerVersion, error) {
	out := map[string]ServerVersion{}
	priority := map[string]int{"bundled": 0, "downloaded": 1, "local": 2}

	add := func(source, ver, path string) {
		if ver == "" || path == "" {
			return
		}
		if old, ok := out[ver]; ok && priority[old.Source] <= priority[source] {
			return
		}
		out[ver] = ServerVersion{Version: ver, Path: path, Source: source, NpmVer: detectNpmVer(path)}
	}

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
				if p := findNode(verDir); p != "" {
					add(src, normalizeVersionName(s.Name()), p)
				}
			}
		}
	}
	for _, lv := range loadLocals() {
		if st, err := os.Stat(lv.Path); err == nil {
			if !st.IsDir() && strings.EqualFold(st.Name(), "node.exe") {
				add("local", lv.Version, lv.Path)
			} else if st.IsDir() {
				if p := findNode(lv.Path); p != "" {
					add("local", lv.Version, p)
				}
			}
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
	Path    string `json:"path"` // 目录或 node.exe 路径
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

// AddLocalVersion 注册本机已有的 Node.js 目录（或 node.exe 路径）
func AddLocalVersion(dir string) error {
	dir = strings.Trim(dir, `"`)
	if dir == "" {
		return fmt.Errorf("目录不能为空")
	}
	exe := ""
	if st, err := os.Stat(dir); err == nil {
		if st.IsDir() {
			exe = findNode(dir)
		} else if strings.EqualFold(st.Name(), "node.exe") {
			exe = dir
		}
	}
	if exe == "" {
		return fmt.Errorf("目录中未找到 node.exe（支持根目录或一级子目录）")
	}
	ver := normalizeVersionName(filepath.Base(filepath.Dir(exe)))
	if strings.Count(ver, ".") < 2 {
		ver = normalizeVersionName(filepath.Base(dir))
	}
	if strings.Count(ver, ".") < 2 {
		if v, err := probeVersion(exe); err == nil {
			ver = v
		}
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
		d := filepath.Join(root, version)
		if !isInstalledVersion(d) {
			if findNode(d) == "" {
				return fmt.Errorf("未找到版本 %s 的安装目录", version)
			}
			return fmt.Errorf("目录 %s 非 go-tools 安装版（内置或手工放置），不可删除", version)
		}
		return os.RemoveAll(d)
	case "local":
		list := loadLocals()
		// 同一版本可能有多份本机登记（PATH 一份、nvm 仓库一份），
		// 按 node 可执行文件的真实路径精确移除，避免连带删掉另一份。
		idx := -1
		for i, l := range list {
			if l.Version == version && pathKey(l.Path) == pathKey(target.Path) {
				idx = i
				break
			}
		}
		if idx < 0 { // 兜底：登记项路径与当前解析结果不一致时按版本命中第一条
			for i, l := range list {
				if l.Version == version {
					idx = i
					break
				}
			}
		}
		if idx < 0 {
			return fmt.Errorf("未找到本机登记的版本 %s", version)
		}
		return saveLocals(append(list[:idx], list[idx+1:]...))
	}
	return fmt.Errorf("未知来源: %s", target.Source)
}

// nodeExe 定位版本目录中的 node.exe
func nodeExe(version string) string {
	vers, _ := ListVersions()
	for _, v := range vers {
		if v.Version == version {
			return v.Path
		}
	}
	return ""
}

// probeVersion 执行 node -p process.versions.node 获取版本号
func probeVersion(exe string) (string, error) {
	cmd := exec.Command(exe, "-p", "process.versions.node")
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

/* ============================ 在线版本（nodejs.org dist） ============================ */

const nodeDistIndex = "https://nodejs.org/dist/index.json"

// FetchRemoteVersions 拉取官方 dist/index.json，返回 win-x64 可安装版本（默认最多 60 条）
func FetchRemoteVersions() ([]RemoteVersion, error) {
	req, err := http.NewRequest(http.MethodGet, nodeDistIndex, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent())
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取 Node 版本列表失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("获取 Node 版本列表失败: HTTP %d", resp.StatusCode)
	}
	var items []struct {
		Version string   `json:"version"`
		Date    string   `json:"date"`
		LTS     any      `json:"lts"`
		Files   []string `json:"files"`
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 16<<20))
	if err := dec.Decode(&items); err != nil {
		return nil, fmt.Errorf("解析 Node 版本列表失败: %v", err)
	}
	var out []RemoteVersion
	for _, it := range items {
		ver := strings.TrimPrefix(it.Version, "v")
		if !versionRe.MatchString(ver) {
			continue // 跳过 nightly / rc 等
		}
		hasWin := false
		for _, f := range it.Files {
			if f == "win-x64-zip" || f == "win-x64" {
				hasWin = true
				break
			}
		}
		if !hasWin && len(it.Files) == 0 {
			hasWin = true
		}
		if !hasWin {
			continue
		}
		lts := ""
		switch v := it.LTS.(type) {
		case string:
			lts = v
		case bool:
		}
		out = append(out, RemoteVersion{
			Version: ver,
			URL:     fmt.Sprintf("https://nodejs.org/dist/v%s/node-v%s-win-x64.zip", ver, ver),
			LTS:     lts,
			Files:   it.Date,
		})
		if len(out) >= 60 {
			break
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("未获得可用版本")
	}
	return out, nil
}

func userAgent() string {
	return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
}

/* ============================ 版本安装（下载+解压拍平） ============================ */

var (
	installMu  sync.Mutex
	installing = map[string]bool{}
)

// InstallVersion 异步安装 Node 版本；进度经 "npm:install" 事件推送
func InstallVersion(version string) error {
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
	go installAsync(version)
	return nil
}

func installAsync(version string) {
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
	destDir := filepath.Join(root, version)

	info(fmt.Sprintf("开始安装 Node.js %s：检查离线包…（解压约 80MB）", version))
	zipPath, isTemp, err := locateOrDownloadZip(version)
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
	inner := findNode(tmpDir)
	if inner == "" {
		os.RemoveAll(tmpDir)
		emitInstall(InstallProgress{Version: version, Stage: "error", Note: "安装包中未找到 node.exe"})
		return
	}
	payload := filepath.Dir(inner)
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
	info(fmt.Sprintf("安装完成：%s（npm %s）", destDir, detectNpmVer(filepath.Join(destDir, "node.exe"))))
	emitInstall(InstallProgress{Version: version, Stage: "done", Percent: 100, Note: destDir})
}

// locateOrDownloadZip 优先插件目录离线 zip；否则按官方 URL 下载（临时文件）
func locateOrDownloadZip(version string) (string, bool, error) {
	root, err := pluginsRoot()
	if err != nil {
		return "", false, err
	}
	candidates := []string{
		filepath.Join(root, fmt.Sprintf("node-v%s-win-x64.zip", version)),
		filepath.Join(root, version+".zip"),
	}
	if matches, _ := filepath.Glob(filepath.Join(root, fmt.Sprintf("node-v%s-win-x64*.zip", version))); matches != nil {
		candidates = append(candidates, matches...)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, false, nil
		}
	}
	url := fmt.Sprintf("https://nodejs.org/dist/v%s/node-v%s-win-x64.zip", version, version)
	emitInstall(InstallProgress{Version: version, Stage: "info", Note: "开始下载：" + filepath.Base(url)})
	tmp := filepath.Join(root, version+".download.zip")
	if err := downloadFile(url, tmp, version); err != nil {
		os.Remove(tmp)
		return "", false, fmt.Errorf("下载失败: %v；可将 node-v%s-win-x64.zip 放入插件目录后重试（离线安装）", err, version)
	}
	return tmp, true, nil
}

func downloadFile(url, dest, version string) error {
	return downloadFileTo(url, dest, func(p InstallProgress) {
		p.Version = version
		emitInstall(p)
	})
}

// downloadFileTo 下载并把进度交给 emit 派发。
// Node 安装包与 npm tarball 共用这段循环，只是回报到不同事件上。
func downloadFileTo(url, dest string, emit func(InstallProgress)) error {
	client := &http.Client{Timeout: 40 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", userAgent())
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
				if pct/5 != lastPct/5 {
					lastPct = pct
					emit(InstallProgress{Stage: "downloading", Percent: pct, Note: filepath.Base(url)})
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
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
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

// tcpUp 端口能建 TCP 连接即视为有服务在监听
func tcpUp(port int) bool {
	if port <= 0 {
		return false
	}
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 600*time.Millisecond)
	if err != nil {
		return false
	}
	_ = c.Close()
	return true
}
