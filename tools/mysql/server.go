package mysql

// ============================================================
// 本地 MySQL 服务管理（服务端进程维度，与连接管理/客户端工具互补）
// 结构复刻 tools/redis/server.go，差异点：
//   - 版本库：官方 dev.mysql.com 免安装 winx64 zip（约 230MB，解压约 1.5GB）
//   - bundled    ：<exedir>/plugins/mysql/<ver>/（发布内置，无安装标记）
//   - downloaded ：在线获取/离线 zip 安装，统一落 <exedir>/plugins/mysql/<ver>/ 并写 .go-tools-installed 标记
//   - local      ：用户手动注册的本机已有目录（登记在 go-tools/mysql-locals.json）
//   - 实例：配置持久化 go-tools/mysql-instances.json；数据/日志在
//     go-tools/mysql-instances/<id>/（my.ini + data/ + mysql.err）。
//     首次启动自动 mysqld --initialize-insecure（root 空密码），
//     配置了 RootPassword 则启动后 ALTER USER 设置。
//   - 停止优先 SHUTDOWN（SQL），兜底 Kill；状态走 SHOW GLOBAL STATUS/VARIABLES；
//     日志为文件型（log-error），Logs() 读尾部行，前端轮询刷新。
//   - 安装进度经 "mysql:install" 事件推送（InstallProgress，stage 与 redis 一致）。
//   - 版本发现：对固定 series 补丁号二分 HEAD 探测官方 URL 模式；失败回退常量清单。
// ============================================================

import (
	"archive/zip"
	"database/sql"
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
	"go-tools/services/sysx"
	"go-tools/tools/logtail"
	"go-tools/tools/portcheck"
)

func init() {
	// 端口占用汇总：MySQL 实例的端口参与跨工具查重
	portcheck.Register(func() []portcheck.Owner {
		list, err := loadInstances()
		if err != nil {
			return nil
		}
		out := make([]portcheck.Owner, 0, len(list))
		for _, c := range list {
			out = append(out, portcheck.Owner{
				Key:   "mysql:" + c.ID,
				Label: "MySQL 实例「" + c.Name + "」",
				Ports: []int{c.Port},
			})
		}
		return out
	})
}

/* ============================ 事件推送 ============================ */

var srvEmitter func(name string, data ...any)

// SetEmitter 注入事件发射器（绑定 Wails v3 app.Event.Emit）。
func SetEmitter(fn func(name string, data ...any)) { srvEmitter = fn }

func srvEmit(name string, payload any) {
	if srvEmitter != nil {
		srvEmitter(name, payload)
	}
}

func emitInstall(p InstallProgress) { srvEmit("mysql:install", p) }

/* ============================ 类型 ============================ */

// ServerVersion 本地可用的一个 MySQL 版本
type ServerVersion struct {
	Version string `json:"version"` // 如 8.4.6
	Path    string `json:"path"`    // mysqld.exe 完整路径
	Source  string `json:"source"`  // bundled | downloaded | local
}

// RemoteVersion 线上可安装版本
type RemoteVersion struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	Size    int64  `json:"size"`
}

// InstanceConfig 本地实例配置
type InstanceConfig struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Version        string `json:"version"`
	Port           int    `json:"port"`
	RootPassword   string `json:"rootPassword"`
	MaxConnections int    `json:"maxConnections"` // <=0 用 mysql 默认 151
	// Docker 运行模式（零值/"local" 为本地拉起 mysqld，向后兼容）
	Mode          string `json:"mode"`          // local | docker
	Image         string `json:"image"`         // docker 模式必填，如 mysql:8.4
	ContainerName string `json:"containerName"` // 留空则 go-tools-mysql-<id>
}

// InstanceStatus 实例运行状态（TCP 探测 + SHOW STATUS 聚合）
type InstanceStatus struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	Port             int    `json:"port"`
	Running          bool   `json:"running"`
	PID              int    `json:"pid"` // 本进程拉起的才有；孤儿进程为 0
	UptimeSeconds    int64  `json:"uptimeSeconds"`
	ThreadsConnected int64  `json:"threadsConnected"`
	ThreadsRunning   int64  `json:"threadsRunning"`
	MaxConnections   int64  `json:"maxConnections"`
	// Docker 模式附加展示（本地模式 Mode="local"，其余字段为空）
	Mode           string `json:"mode"`
	Image          string `json:"image"`
	Container      string `json:"container"`
	ContainerID    string `json:"containerId"`
	ContainerState string `json:"containerState"`
	DataDir        string `json:"dataDir"`
}

// InstallProgress 版本安装进度事件负载（stage 语义与 redis 一致）
type InstallProgress struct {
	Version string `json:"version"`
	Stage   string `json:"stage"` // info|downloading|extracting|done|error
	Percent int    `json:"percent"`
	Note    string `json:"note"`
}

/* ============================ 目录约定 ============================ */

// goToolsDir 数据目录 data/mysql：实例登记与实例数据目录（基础目录设置页可改，见 appdirs）
func goToolsDir() (string, error) { return appdirs.Data("mysql") }

// pluginsRoot 插件目录 plugins/mysql：内置版本、安装版本与离线 zip 的统一位置（确保存在）
func pluginsRoot() (string, error) { return appdirs.Plugins("mysql") }

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
	return filepath.Join(d, "mysql-locals.json"), nil
}

func instancesFilePath() (string, error) {
	d, err := goToolsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "mysql-instances.json"), nil
}

func defaultInstanceDataDir(id string) (string, error) {
	d, err := goToolsDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(d, "mysql-instances", id)
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

// findMysqld 在版本目录内定位 mysqld：根/bin、根一级子目录/bin、根、根一级子目录
func findMysqld(dir string) string {
	try := []string{
		filepath.Join(dir, "bin", binFileName),
		filepath.Join(dir, binFileName),
	}
	if subs, err := os.ReadDir(dir); err == nil {
		for _, s := range subs {
			if s.IsDir() {
				sub := filepath.Join(dir, s.Name())
				try = append(try, filepath.Join(sub, "bin", binFileName), filepath.Join(sub, binFileName))
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

// normalizeVersionName 目录名不是纯版本号时提取版本号
var verExtractRe = regexp.MustCompile(`(\d+\.\d+(?:\.\d+)*)`)

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

	// 插件目录 <exedir>/plugins/mysql/<ver>/：有安装标记→downloaded；无标记→bundled
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
				// 兼容 zip 未拍平的嵌套目录（mysql-<ver>-winx64/bin/mysqld.exe）
				if p := findMysqld(verDir); p != "" {
					add(src, normalizeVersionName(s.Name()), p)
				}
			}
		}
	}
	// local：mysql-locals.json 登记
	for _, lv := range loadLocals() {
		if st, err := os.Stat(lv.Path); err == nil && !st.IsDir() && strings.EqualFold(st.Name(), binFileName) {
			add("local", lv.Version, lv.Path)
		} else if p := findMysqld(lv.Path); p != "" { // 登记的是目录
			add("local", lv.Version, p)
		}
	}
	// system：系统包管理器装的（Linux 上这是主要来源）
	//
	// 版本号只能问二进制自己：/usr/sbin/mysqld 没有“目录名”可用。
	if bin := sysx.Which(binFileName); bin != "" {
		if ver := sysx.ProbeVersion(bin, "--version"); ver != "" {
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
	Path    string `json:"path"` // 目录或 mysqld 可执行文件路径
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

// AddLocalVersion 注册本机已有的 MySQL 目录（或 mysqld 可执行文件路径）
func AddLocalVersion(dir string) error {
	dir = strings.Trim(dir, `"`)
	if dir == "" {
		return fmt.Errorf("目录不能为空")
	}
	exe := ""
	if st, err := os.Stat(dir); err == nil {
		if st.IsDir() {
			exe = findMysqld(dir)
		} else if strings.EqualFold(st.Name(), binFileName) {
			exe = dir
		}
	}
	if exe == "" {
		return fmt.Errorf("目录中未找到 %s（支持根目录或 bin/、一级子目录）", binFileName)
	}
	// 版本号优先问二进制自己：Linux 系统包的 mysqld 在 /usr/sbin，路径里没有版本信息，
	// 只靠目录名推导会把 /usr/sbin 认成版本号。
	ver := sysx.ProbeVersion(exe, "--version")
	if ver == "" {
		ver = normalizeVersionName(filepath.Base(filepath.Dir(filepath.Dir(exe)))) // <base>/bin/mysqld → <base> 名
		if strings.Count(ver, ".") < 2 {
			ver = normalizeVersionName(filepath.Base(filepath.Dir(exe)))
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
		if root, err := pluginsRoot(); err == nil {
			d := filepath.Join(root, version)
			if isInstalledVersion(d) {
				return os.RemoveAll(d)
			}
			// 兼容嵌套未拍平目录
			if findMysqld(d) != "" {
				return fmt.Errorf("请先在文件管理器中确认该目录为安装版后再删（含安装标记）")
			}
		}
		return fmt.Errorf("未找到版本 %s 的安装目录", version)
	case "local":
		list := loadLocals()
		out := list[:0]
		for _, l := range list {
			if l.Version != version {
				out = append(out, l)
			}
		}
		return saveLocals(out)
	}
	return fmt.Errorf("未知来源: %s", target.Source)
}

/* ============================ 在线版本（cdn.mysql.com 官方 zip） ============================ */

const mysqlZipURL = "https://cdn.mysql.com/Downloads/MySQL-%s/mysql-%s-winx64.zip"

// seriesProbe 各产品线的补丁号探测区间
type seriesProbe struct {
	series string
	lo, hi int
}

var mysqlSeries = []seriesProbe{
	{"5.5", 50, 62},
	{"5.6", 40, 51},
	{"5.7", 30, 44},
	{"8.0", 40, 50},
	{"8.1", 0, 0},
	{"8.2", 0, 0},
	{"8.3", 0, 0},
	{"8.4", 0, 6},
	{"9.0", 0, 8},
	{"9.1", 0, 8},
	{"9.2", 0, 8},
	{"9.3", 0, 8},
	{"9.4", 0, 8},
	{"9.5", 0, 8},
}

// 网络探测全失败时的回退推荐清单（仅版本号，URL 按模式构造，size 未知）
var mysqlFallback = []string{"8.0.45", "8.4.0", "9.3.0", "8.3.0", "8.2.0", "8.1.0", "5.7.44", "5.6.51", "5.5.62"}

func zipURL(ver string) string {
	major := verExtractRe.FindString(ver)
	series := strings.Join(strings.Split(major, ".")[:2], ".")
	return fmt.Sprintf(mysqlZipURL, series, ver)
}

func headExists(url string) (bool, int64) {
	req, err := http.NewRequest(http.MethodHead, url, nil)
	if err != nil {
		return false, 0
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return false, 0
	}
	defer resp.Body.Close()
	// dev.mysql.com 返回 302 重定向到 CDN，CDN 可能不支持 HEAD(405)；
	// 只要不是 404/403/5xx 就认为资源存在。
	ok := resp.StatusCode < 400 || resp.StatusCode == 405
	return ok, resp.ContentLength
}

// probeSeriesLatest 从 hi 向下线性探测某 series 最新可用补丁号（CDN 只保留最新版，不宜二分）
func probeSeriesLatest(p seriesProbe) (string, int64, bool) {
	for patch := p.hi; patch >= p.lo; patch-- {
		ver := fmt.Sprintf("%s.%d", p.series, patch)
		ok, size := headExists(zipURL(ver))
		if ok {
			return ver, size, true
		}
	}
	return "", 0, false
}

// FetchRemoteVersions 拉取可安装版本列表。
//
// Linux 上不是“下载一个 zip”：官方 winx64 zip 到了 Linux 上根本跑不起来，
// 所以这里问的是发行版仓库能装哪个版本，安装时交给包管理器。
func FetchRemoteVersions() ([]RemoteVersion, error) {
	if systemPackaged {
		list := systemRemoteVersions()
		if len(list) == 0 {
			// 空列表在界面上等于“没东西可装”，得把原因和命令一并说清楚
			return nil, fmt.Errorf("%s", sysx.PackageHint(systemPkgNames))
		}
		return list, nil
	}
	type res struct {
		ver  string
		size int64
		ok   bool
	}
	results := make([]res, len(mysqlSeries))
	var wg sync.WaitGroup
	for i, p := range mysqlSeries {
		wg.Add(1)
		go func(i int, p seriesProbe) {
			defer wg.Done()
			ver, size, ok := probeSeriesLatest(p)
			results[i] = res{ver, size, ok}
		}(i, p)
	}
	wg.Wait()
	var out []RemoteVersion
	for _, r := range results {
		if !r.ok {
			continue
		}
		out = append(out, RemoteVersion{Version: r.ver, URL: zipURL(r.ver), Size: r.size})
	}
	if len(out) == 0 {
		for _, v := range mysqlFallback {
			out = append(out, RemoteVersion{Version: v, URL: zipURL(v), Size: 0})
		}
		return out, nil
	}
	sort.Slice(out, func(i, j int) bool { return compareVersion(out[i].Version, out[j].Version) > 0 })
	return out, nil
}

/* ============================ 系统包（Linux） ============================ */

// systemRemoteVersions 把发行版仓库里的可安装版本包成统一的 RemoteVersion。
func systemRemoteVersions() []RemoteVersion {
	cands := sysx.PackageCandidates(systemPkgNames)
	out := make([]RemoteVersion, 0, len(cands))
	for _, c := range cands {
		// 归一：仓库里的 "8.0.36-2ubuntu3" 与 mysqld --version 报告的
		// "8.0.36" 是同一个包，不统一版本库与实例就对不上。
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
// 包管理器会顺手建 mysql 用户、建 /var/lib/mysql 并注册 systemd 服务，因此
// 面板的实例应当指向它装好的 mysqld；启动时由 runAsUserArgs 把 --user 补上。
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
				Note: "本机没有 dnf/yum/apt-get，请手工安装 MySQL 后刷新版本库"})
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

/* ============================ 版本安装（下载+解压拍平） ============================ */

var (
	installMu  sync.Mutex
	installing = map[string]bool{}
)

// InstallVersion 异步下载安装版本；进度与安装日志（stage=info）经 "mysql:install" 事件推送。
// 兜底：插件目录已有 mysql-<ver>-winx64.zip 时直接离线解压安装。
func InstallVersion(version string) error {
	if systemPackaged {
		return installSystemVersion(version)
	}
	if !versionRe.MatchString(version) {
		return fmt.Errorf("非法版本号: %s", version)
	}
	vers, _ := ListVersions()
	for _, v := range vers {
		if v.Version == version {
			return fmt.Errorf("版本 %s 已安装", version)
		}
	}
	installMu.Lock()
	if installing[version] {
		installMu.Unlock()
		return fmt.Errorf("版本 %s 正在安装中", version)
	}
	installing[version] = true
	installMu.Unlock()
	go installAsync(version)
	return nil
}

func installAsync(version string) {
	defer func() {
		installMu.Lock()
		delete(installing, version)
		installMu.Unlock()
	}()
	info := func(note string) {
		emitInstall(InstallProgress{Version: version, Stage: "info", Note: note})
	}
	root, err := pluginsRoot()
	if err != nil {
		emitInstall(InstallProgress{Version: version, Stage: "error", Note: err.Error()})
		return
	}
	destDir := filepath.Join(root, version)

	info(fmt.Sprintf("开始安装 MySQL %s：检查离线包…（解压后约 1.5GB，请耐心等待）", version))

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
	// zip 顶层是 mysql-<ver>-winx64/，拍平到 <ver>/
	inner := findMysqld(tmpDir)
	if inner == "" {
		os.RemoveAll(tmpDir)
		emitInstall(InstallProgress{Version: version, Stage: "error", Note: "安装包中未找到 mysqld.exe"})
		return
	}
	payload := filepath.Dir(filepath.Dir(inner)) // .../bin/mysqld.exe → .../（发行版根）
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
func locateOrDownloadZip(version string) (string, bool, error) {
	root, err := pluginsRoot()
	if err == nil {
		candidates := []string{
			filepath.Join(root, fmt.Sprintf("mysql-%s-winx64.zip", version)),
			filepath.Join(root, version+".zip"),
		}
		if matches, _ := filepath.Glob(filepath.Join(root, fmt.Sprintf("mysql-%s-winx64*.zip", version))); matches != nil {
			candidates = append(candidates, matches...)
		}
		for _, c := range candidates {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c, false, nil
			}
		}
	}
	url := zipURL(version)
	emitInstall(InstallProgress{Version: version, Stage: "info", Note: fmt.Sprintf("开始下载：mysql-%s-winx64.zip", version)})
	destDir := ""
	if root, err := pluginsRoot(); err == nil {
		destDir = filepath.Join(root, version)
	}
	tmp := destDir + ".download.zip"
	if err := downloadFile(url, tmp, version); err != nil {
		os.Remove(tmp)
		return "", false, fmt.Errorf("下载失败: %v；可将 mysql-%s-winx64.zip 放入插件目录后重试", err, version)
	}
	return tmp, true, nil
}

func downloadFile(url, dest, version string) error {
	client := &http.Client{Timeout: 60 * time.Minute} // 230MB 大包，放宽总时限
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36")
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
					emitInstall(InstallProgress{Version: version, Stage: "downloading", Percent: pct, Note: fmt.Sprintf("mysql-%s-winx64.zip", version)})
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

/* ============================ 实例配置持久化 ============================ */

func loadInstances() ([]InstanceConfig, error) {
	path, err := instancesFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var list []InstanceConfig
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func saveInstances(list []InstanceConfig) error {
	path, err := instancesFilePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// ListInstances 返回全部实例配置
func ListInstances() ([]InstanceConfig, error) { return loadInstances() }

// SaveInstance 新增或更新实例配置（ID 为空则生成）
func SaveInstance(cfg InstanceConfig) error {
	if cfg.Name == "" {
		return fmt.Errorf("实例名称不能为空")
	}
	if cfg.Port <= 0 {
		cfg.Port = 3306
	}
	if cfg.Port > 65535 {
		return fmt.Errorf("端口非法: %d", cfg.Port)
	}
	if reason := PortTaken(cfg.Port, cfg.ID); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	if isDockerMode(&cfg) {
		// Docker 模式：不需要本地版本，但要镜像；数据目录与本地模式同路，可互迁
		cfg.Mode = ModeDocker
		if strings.TrimSpace(cfg.Image) == "" {
			return fmt.Errorf("Docker 模式需指定镜像，如 %s", DefaultDockerImage)
		}
	} else {
		cfg.Mode = ModeLocal
		if cfg.Version == "" {
			return fmt.Errorf("请选择 MySQL 版本")
		}
		vers, _ := ListVersions()
		ok := false
		for _, v := range vers {
			if v.Version == cfg.Version {
				ok = true
				break
			}
		}
		if !ok {
			return fmt.Errorf("版本 %s 未安装", cfg.Version)
		}
	}
	list, err := loadInstances()
	if err != nil {
		return err
	}
	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("inst-%d", time.Now().UnixNano())
	}
	if isDockerMode(&cfg) && strings.TrimSpace(cfg.ContainerName) == "" {
		// 容器名默认按最终 ID 生成（保证唯一且可凭名反查实例）
		cfg.ContainerName = "go-tools-mysql-" + cfg.ID
	}
	for i := range list {
		if list[i].ID == cfg.ID {
			list[i] = cfg
			return saveInstances(list)
		}
	}
	list = append(list, cfg)
	return saveInstances(list)
}

// RemoveInstance 删除实例配置；运行中先停止（数据目录与容器一并清理，数据保留）
func RemoveInstance(id string) error {
	if trackedSrv(id) != nil {
		if err := StopInstance(id); err != nil {
			return fmt.Errorf("停止失败，未删除: %v", err)
		}
	}
	// Docker 模式：连带删除容器（未运行也要删干净，数据目录保留）
	if cfg, err := findInstanceConfig(id); err == nil && isDockerMode(cfg) {
		if err := dockerRemove(cfg); err != nil {
			return err
		}
	}
	list, err := loadInstances()
	if err != nil {
		return err
	}
	out := list[:0]
	for _, c := range list {
		if c.ID != id {
			out = append(out, c)
		}
	}
	return saveInstances(out)
}

/* ============================ 实例进程管理 ============================ */

type srvRunning struct {
	cmd         *exec.Cmd
	done        chan struct{}
	containerID string // Docker 模式：本实例对应的容器 ID（cmd 为 nil）
}

var (
	srvRunMu sync.Mutex
	srvProcs = map[string]*srvRunning{}
)

func trackedSrv(id string) *srvRunning {
	srvRunMu.Lock()
	defer srvRunMu.Unlock()
	return srvProcs[id]
}

// tcpUp 端口能建 TCP 连接即视为有服务在监听
func tcpUp(port int) bool {
	c, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 600*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// openRoot 以 root 身份建立短连接（pw 传空串则试空密码）
func openRoot(port int, pw string) (*sql.DB, error) {
	dsn := fmt.Sprintf("root@tcp(127.0.0.1:%d)/?timeout=3s&readTimeout=10s&writeTimeout=10s", port)
	if pw != "" {
		dsn = fmt.Sprintf("root:%s@tcp(127.0.0.1:%d)/?timeout=3s&readTimeout=10s&writeTimeout=10s", pw, port)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// pingRoot 依次尝试配置密码与空密码
func pingRoot(port int, pw string) (*sql.DB, bool) {
	for _, try := range []string{pw, ""} {
		if db, err := openRoot(port, try); err == nil {
			return db, true
		}
	}
	return nil, false
}

// StartInstance 启动实例：本地模式——端口探测 → 生成 my.ini → 首次自动初始化 → 隐藏窗口拉起 → 等待就绪 → 按需设置 root 密码；
// Docker 模式则转交 dockerStart（镜像/容器生命周期）
func StartInstance(id string) error {
	cfgs, err := loadInstances()
	if err != nil {
		return err
	}
	var cfg *InstanceConfig
	for i := range cfgs {
		if cfgs[i].ID == id {
			cfg = &cfgs[i]
			break
		}
	}
	if cfg == nil {
		return fmt.Errorf("实例不存在: %s", id)
	}
	if trackedSrv(id) != nil || tcpUp(cfg.Port) {
		return fmt.Errorf("实例已在运行")
	}
	if isDockerMode(cfg) {
		return dockerStart(cfg) // 容器模式：创建/启动容器并等待就绪
	}
	vers, _ := ListVersions()
	exe := ""
	for _, v := range vers {
		if v.Version == cfg.Version {
			exe = v.Path
			break
		}
	}
	if exe == "" {
		return fmt.Errorf("版本 %s 不可用，请先安装", cfg.Version)
	}
	basedir := filepath.Dir(filepath.Dir(exe)) // <base>/bin/mysqld.exe

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Port))
	if err != nil {
		return fmt.Errorf("端口 %d 已被占用: %v", cfg.Port, err)
	}
	ln.Close()

	instDir, err := defaultInstanceDataDir(id)
	if err != nil {
		return err
	}
	dataDir := filepath.Join(instDir, "data")
	errPath := filepath.Join(instDir, "mysql.err")
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	confPath := filepath.Join(instDir, "my.ini")
	if err := os.WriteFile(confPath, []byte(buildMyIni(cfg, basedir, instDir, dataDir)), 0o644); err != nil {
		return err
	}

	// Linux 上 mysqld 常以 mysql 用户跑：目录归属与 --user 必须先就位，
	// 否则初始化会以 "Please read Security section" 或权限错误告终。
	if err := prepareInstanceDir(instDir); err != nil {
		return err
	}
	userArgs := runAsUserArgs()

	// 首次启动：data 为空则免交互初始化（root 空密码）
	if empty, err := dirIsEmpty(dataDir); err == nil && empty {
		initCmd := exec.Command(exe, append([]string{"--defaults-file=my.ini", "--initialize-insecure"}, userArgs...)...)
		initCmd.Dir = instDir // 与 redis 同理：cwd + 相对文件名，避免路径拼接问题
		initCmd.SysProcAttr = hideWindowAttr()
		out, err := initCmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("初始化数据目录失败: %v\n%s\n%s%s", err, string(out), errTail(errPath, 8), initFailHint(string(out)))
		}
	}

	cmd := exec.Command(exe, append([]string{"--defaults-file=my.ini"}, userArgs...)...)
	cmd.Dir = instDir
	cmd.SysProcAttr = hideWindowAttr()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动失败: %v", err)
	}
	p := &srvRunning{cmd: cmd, done: make(chan struct{})}
	srvRunMu.Lock()
	srvProcs[id] = p
	srvRunMu.Unlock()
	go func() {
		_ = cmd.Wait()
		close(p.done)
		srvRunMu.Lock()
		if srvProcs[id] == p {
			delete(srvProcs, id)
		}
		srvRunMu.Unlock()
	}()

	// 等待就绪（最多 60s；进程提前退出则立即报错并附错误日志尾部）
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-p.done:
			tail := errTail(errPath, 10)
			return fmt.Errorf("进程启动后退出，请查看日志。\n%s%s", tail, initFailHint(tail))
		default:
		}
		if tcpUp(cfg.Port) {
			if db, ok := pingRoot(cfg.Port, cfg.RootPassword); ok {
				db.Close()
				return nil
			}
			// 端口已监听但密码未生效：可能历史密码待改（首次初始化后配置了 RootPassword）
			if cfg.RootPassword != "" {
				if db, ok := applyRootPassword(cfg); ok {
					db.Close()
					return nil
				}
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("启动超时：60s 内未就绪，请查看日志。\n%s", errTail(errPath, 10))
}

// applyRootPassword 用空密码连接并 ALTER USER 设置 root 密码
func applyRootPassword(cfg *InstanceConfig) (*sql.DB, bool) {
	db, err := openRoot(cfg.Port, "")
	if err != nil {
		return nil, false
	}
	if _, err := db.Exec(fmt.Sprintf("ALTER USER 'root'@'localhost' IDENTIFIED BY %s", quoteStr(cfg.RootPassword))); err != nil {
		db.Close()
		return nil, false
	}
	db.Close()
	if db2, err := openRoot(cfg.Port, cfg.RootPassword); err == nil {
		return db2, true
	}
	return nil, false
}

// StopInstance 停止实例：优先 SQL SHUTDOWN（对孤儿进程同样有效），兜底 Kill
func StopInstance(id string) error {
	cfgs, err := loadInstances()
	if err != nil {
		return err
	}
	var cfg *InstanceConfig
	for i := range cfgs {
		if cfgs[i].ID == id {
			cfg = &cfgs[i]
			break
		}
	}
	if cfg == nil {
		return fmt.Errorf("实例不存在: %s", id)
	}
	if isDockerMode(cfg) {
		return dockerStop(cfg) // 容器模式：SHUTDOWN 不适用，走 container stop
	}
	p := trackedSrv(id)
	if !tcpUp(cfg.Port) && p == nil {
		return fmt.Errorf("实例未在运行")
	}
	if db, ok := pingRoot(cfg.Port, cfg.RootPassword); ok {
		_, _ = db.Exec("SHUTDOWN")
		db.Close() // SHUTDOWN 会导致连接断开，忽略错误
	}
	if p != nil {
		select {
		case <-p.done:
		case <-time.After(10 * time.Second):
			if p.cmd.Process != nil {
				_ = p.cmd.Process.Kill()
			}
			select {
			case <-p.done:
			case <-time.After(3 * time.Second):
			}
		}
		srvRunMu.Lock()
		if srvProcs[id] == p {
			delete(srvProcs, id)
		}
		srvRunMu.Unlock()
	}
	if tcpUp(cfg.Port) {
		return fmt.Errorf("停止失败：端口 %d 仍在监听", cfg.Port)
	}
	return nil
}

// GetInstanceStatus 实例状态：进程跟踪信息 + TCP 探测 + SHOW STATUS/VARIABLES
func GetInstanceStatus(id string) (InstanceStatus, error) {
	st := InstanceStatus{ID: id}
	cfgs, err := loadInstances()
	if err != nil {
		return st, err
	}
	var cfg *InstanceConfig
	for i := range cfgs {
		if cfgs[i].ID == id {
			cfg = &cfgs[i]
			break
		}
	}
	if cfg == nil {
		return st, fmt.Errorf("实例不存在: %s", id)
	}
	st.Name, st.Version, st.Port = cfg.Name, cfg.Version, cfg.Port
	st.DataDir = filepath.Join(instDirOf(id), "data")
	if p := trackedSrv(id); p != nil && p.cmd != nil && p.cmd.Process != nil {
		st.PID = p.cmd.Process.Pid
	}
	if isDockerMode(cfg) {
		fillDockerStatus(cfg, &st)
	}
	if !tcpUp(cfg.Port) {
		return st, nil // 未运行不算错误
	}
	db, ok := pingRoot(cfg.Port, cfg.RootPassword)
	if !ok {
		st.Running = true // 端口在但密码不对（被外部改过），仍视为运行
		return st, nil
	}
	defer db.Close()
	st.Running = true
	rows, err := db.Query("SHOW GLOBAL STATUS WHERE Variable_name IN ('Uptime','Threads_connected','Threads_running')")
	if err == nil {
		for rows.Next() {
			var k, v string
			if rows.Scan(&k, &v) == nil {
				n, _ := strconv.ParseInt(v, 10, 64)
				switch k {
				case "Uptime":
					st.UptimeSeconds = n
				case "Threads_connected":
					st.ThreadsConnected = n
				case "Threads_running":
					st.ThreadsRunning = n
				}
			}
		}
		rows.Close()
	}
	vrows, err := db.Query("SHOW VARIABLES WHERE Variable_name IN ('version','max_connections')")
	if err == nil {
		for vrows.Next() {
			var k, v string
			if vrows.Scan(&k, &v) == nil {
				switch k {
				case "version":
					st.Version = v
				case "max_connections":
					st.MaxConnections, _ = strconv.ParseInt(v, 10, 64)
				}
			}
		}
		vrows.Close()
	}
	return st, nil
}

// Logs 返回实例日志尾部 n 行（本地模式读 mysql.err，容器模式读容器输出）
func Logs(id string, n int) ([]string, error) {
	if n <= 0 || n > 2000 {
		n = 200
	}
	if cfg, err := findInstanceConfig(id); err == nil && isDockerMode(cfg) {
		return dockerLogs(cfg, n)
	}
	instDir, err := defaultInstanceDataDir(id)
	if err != nil {
		return nil, err
	}
	// mysql.err 可以长到几百 MB，取尾巴不该整本入堆
	return logtail.Lines(filepath.Join(instDir, "mysql.err"), n)
}

// SuggestPort 探测一个可用端口（从 from 起递增，默认基准 3306）：
// 既跳过其它实例已登记的端口，也跳过系统里已绑不上的端口
func SuggestPort(from int) int {
	if from <= 0 {
		from = 3306
	}
	return portcheck.Suggest(from, 100)
}

// PortTaken 报告端口是否可用于（新）实例（跨工具查重 + 系统监听）；
// excludeID 用于编辑场景排除自身，空串表示可用
func PortTaken(port int, excludeID string) string {
	exclude := ""
	if excludeID != "" {
		exclude = "mysql:" + excludeID
	}
	return portcheck.Taken(port, exclude)
}

/* ============================ 小工具 ============================ */

func dirIsEmpty(dir string) (bool, error) {
	f, err := os.Open(dir)
	if err != nil {
		return false, err
	}
	defer f.Close()
	names, err := f.Readdirnames(1)
	return len(names) == 0, err
}

// errTail 读 mysql.err 尾部 k 行（拼进错误信息，免跳页面排查）
func errTail(errPath string, k int) string {
	t := logtail.Tail(errPath, k)
	if t == "" {
		return ""
	}
	return "—— mysql.err 尾部 ——\n" + t
}

/* ============================ my.ini 生成 ============================ */

func buildMyIni(cfg *InstanceConfig, basedir, instDir, dataDir string) string {
	maxConn := cfg.MaxConnections
	if maxConn <= 0 {
		maxConn = 151
	}
	var b strings.Builder
	b.WriteString("# go-tools 自动生成，请勿手改（每次启动覆盖）\n[mysqld]\n")
	fmt.Fprintf(&b, "basedir=%s\n", filepath.ToSlash(basedir))
	fmt.Fprintf(&b, "datadir=%s\n", filepath.ToSlash(dataDir))
	fmt.Fprintf(&b, "port=%d\n", cfg.Port)
	fmt.Fprintf(&b, "log-error=%s\n", filepath.ToSlash(filepath.Join(instDir, "mysql.err")))
	fmt.Fprintf(&b, "max_connections=%d\n", maxConn)
	b.WriteString(myIniPlatformLines(instDir))
	b.WriteString("skip-name-resolve\n")
	return b.String()
}

// StopAll 停止所有由本程序拉起的 MySQL 实例（应用退出时调用）
func StopAll() {
	srvRunMu.Lock()
	ids := make([]string, 0, len(srvProcs))
	for id := range srvProcs {
		ids = append(ids, id)
	}
	srvRunMu.Unlock()
	for _, id := range ids {
		_ = StopInstance(id)
	}
}
