package redis

// ============================================================
// 本地 Redis 服务管理（服务端进程维度，与连接管理/客户端工具互补）
//
// 版本库三渠道（统一 ListVersions 聚合，版本目录名即版本号）：
//   - bundled    ：<exedir>/plugins/redis/<ver>/（发布内置，无安装标记；兼容旧目录 <exedir>/redis-servers/）
//   - downloaded ：在线获取/离线 zip 安装，统一落 <exedir>/plugins/redis/<ver>/ 并写 .go-tools-installed 标记；
//                  旧版缓存 %UserConfigDir%/go-tools/redis-servers/ 首次扫描时自动迁移
//   - local      ：用户手动注册的本机已有目录（登记在 redis-servers/locals.json）
//
// 实例：配置持久化在 go-tools/redis-instances.json；每个实例数据/日志在
// go-tools/redis-instances/<id>/（含生成的 redis.conf 与 server.log）。
// 启动用 exec 隐藏窗口拉起 redis-server，stdout/stderr 逐行进环形缓冲并
// 通过 "redis:serverlog" 事件推给前端；停止优先 RESP "SHUTDOWN NOSAVE" 优雅
// 退出（对孤儿进程同样有效），兜底 Kill。状态用 PING+INFO 实时探测。
//
// 安装进度经 "redis:install" 事件推送（InstallProgress）。
// ============================================================

import (
	"archive/zip"
	"bufio"
	"encoding/json"
	"errors"
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
	"go-tools/tools/srvstate"
)

func init() {
	// 端口占用汇总：Redis 实例的端口参与跨工具查重
	portcheck.Register(func() []portcheck.Owner {
		list, err := loadInstances()
		if err != nil {
			return nil
		}
		out := make([]portcheck.Owner, 0, len(list))
		for _, c := range list {
			out = append(out, portcheck.Owner{
				Key:   "redis:" + c.ID,
				Label: "Redis 实例「" + c.Name + "」",
				Ports: []int{c.Port},
			})
		}
		return out
	})
}

/* ============================ 类型 ============================ */

// ServerVersion 本地可用的一个 redis-server 版本
type ServerVersion struct {
	Version string `json:"version"` // 如 7.4.4
	Path    string `json:"path"`    // redis-server.exe 完整路径
	Source  string `json:"source"`  // bundled | downloaded | local
}

// RemoteVersion 线上（GitHub redis-windows 发行版）可安装版本
type RemoteVersion struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	Size    int64  `json:"size"`
}

// InstanceConfig 一个本地 Redis 实例配置
type InstanceConfig struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Port        int    `json:"port"`
	Password    string `json:"password"`
	AppendOnly  bool   `json:"appendOnly"`
	MaxMemoryMB int    `json:"maxMemoryMB"` // <=0 表示不限制
	DataDir     string `json:"dataDir"`     // 空 = 默认（程序数据目录下按 ID 分配）
	// Mode 运行模式：local（空值等同，兼容历史配置）| docker，详见 docker.go
	Mode string `json:"mode"`
	// Image 容器模式的镜像（如 redis:7-alpine）；本地模式忽略
	Image string `json:"image"`
	// ContainerName 容器名，留空按实例 ID 生成
	ContainerName string `json:"containerName"`
}

// InstanceStatus 实例运行状态（网络探测 + INFO 聚合）
type InstanceStatus struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	Version          string `json:"version"`
	Port             int    `json:"port"`
	Running          bool   `json:"running"`
	PID              int    `json:"pid"` // 本进程拉起的才有；孤儿进程为 0
	UptimeSeconds    int64  `json:"uptimeSeconds"`
	UsedMemoryHuman  string `json:"usedMemoryHuman"`
	ConnectedClients int64  `json:"connectedClients"`
	TotalKeys        int64  `json:"totalKeys"`
	// 以下仅容器模式填写：模式标记、镜像、容器名/ID 与容器状态串（running|exited|...）
	Mode           string `json:"mode"`
	Image          string `json:"image"`
	Container      string `json:"container"`
	ContainerID    string `json:"containerId"`
	ContainerState string `json:"containerState"`
}

// InstallProgress 版本安装进度事件负载
type InstallProgress struct {
	Version string `json:"version"`
	Stage   string `json:"stage"` // downloading|extracting|done|error
	Percent int    `json:"percent"`
	Note    string `json:"note"`
}

// ServerLogLine 实例日志行事件负载
type ServerLogLine struct {
	ID   string `json:"id"`
	Time string `json:"time"`
	Line string `json:"line"`
}

/* ============================ 目录约定 ============================ */

// goToolsDir 数据目录 data/redis：实例登记与实例数据目录（基础目录设置页可改，见 appdirs）
func goToolsDir() (string, error) { return appdirs.Data("redis") }

// downloadedRoot 旧版下载缓存根目录（含 locals.json 登记表；新安装统一落插件目录）
func downloadedRoot() (string, error) {
	d, err := goToolsDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(d, "redis-servers")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// offlineRoot 插件目录 plugins/redis：内置版本与离线安装 zip 的默认放置位置（确保存在）
func offlineRoot() (string, error) { return appdirs.Plugins("redis") }

// OfflineDir 导出版：返回插件离线目录（供前端展示放置指引）
func OfflineDir() (string, error) { return offlineRoot() }

// legacyBundledRoot 兼容旧内置目录 <exedir>/redis-servers（不存在则返回空串）
func legacyBundledRoot() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	dir := filepath.Join(filepath.Dir(exe), "redis-servers")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return ""
	}
	return dir
}

// installedMarker 经“在线获取/离线 zip”安装到插件目录的版本标记文件，
// 用于区分安装版（可删除）与发布内置版（无标记，不可删）。
const installedMarker = ".go-tools-installed"

func isInstalledVersion(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, installedMarker))
	return err == nil && !st.IsDir()
}

func markInstalled(dir string) {
	_ = os.WriteFile(filepath.Join(dir, installedMarker), []byte("installed by go-tools\n"), 0o644)
}

// DownloadedRoot 导出版：返回下载缓存根目录（供前端展示离线包放置指引）
func DownloadedRoot() (string, error) { return downloadedRoot() }

func localsRegistryPath() (string, error) {
	root, err := downloadedRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "locals.json"), nil
}

func instancesFilePath() (string, error) {
	d, err := goToolsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "redis-instances.json"), nil
}

func defaultInstanceDataDir(id string) (string, error) {
	d, err := goToolsDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(d, "redis-instances", id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

/* ============================ 版本工具 ============================ */

var versionRe = regexp.MustCompile(`^\d+(\.\d+){1,3}$`)

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

// findRedisServer 在目录（含子目录）内定位 redis-server 可执行文件
func findRedisServer(dir string) string {
	var found string
	direct := filepath.Join(dir, binFileName)
	if st, err := os.Stat(direct); err == nil && !st.IsDir() {
		return direct
	}
	_ = filepath.Walk(dir, func(p string, st os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !st.IsDir() && strings.EqualFold(st.Name(), binFileName) {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

/* ============================ 版本库 ============================ */

// ListVersions 聚合四渠道版本，按版本号降序；同版本优先 bundled > downloaded > local > system
func ListVersions() ([]ServerVersion, error) {
	migrateOnce.Do(migrateLegacyDownloads)
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

	// 插件目录 <exedir>/plugins/redis/<ver>/：有安装标记→downloaded（可删）；无标记→bundled（发布内置，不可删）
	if root, err := offlineRoot(); err == nil {
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
				add(src, s.Name(), findRedisServer(verDir))
			}
		}
	}
	// 兼容旧内置目录 <exedir>/redis-servers/<ver>/
	if root := legacyBundledRoot(); root != "" {
		if subs, err := os.ReadDir(root); err == nil {
			for _, s := range subs {
				if !s.IsDir() {
					continue
				}
				add("bundled", s.Name(), findRedisServer(filepath.Join(root, s.Name())))
			}
		}
	}
	// 旧版下载缓存（历史兼容；首次列目录时会尽量迁移到插件目录）
	if root, err := downloadedRoot(); err == nil {
		if subs, err := os.ReadDir(root); err == nil {
			for _, s := range subs {
				if !s.IsDir() || !versionRe.MatchString(s.Name()) {
					continue
				}
				add("downloaded", s.Name(), findRedisServer(filepath.Join(root, s.Name())))
			}
		}
	}
	// local：locals.json 登记
	for _, lv := range loadLocals() {
		if st, err := os.Stat(lv.Path); err == nil && !st.IsDir() && strings.EqualFold(st.Name(), binFileName) {
			add("local", lv.Version, lv.Path)
		} else if p := findRedisServer(lv.Path); p != "" { // 登记的是目录
			add("local", lv.Version, p)
		}
	}
	// system：系统包管理器装的（Linux 上这是主要来源，Windows 下也认 PATH 里的）
	//
	// 版本号必须问二进制自己：/usr/bin/redis-server 没有“目录名”可以当版本号。
	// 探测失败就不列，宁可少一条也不要凭空造一个版本号。
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

/* ---------------------- 旧下载缓存 → 插件目录一次性迁移 ---------------------- */

var migrateOnce sync.Once

// migrateLegacyDownloads 把旧用户配置目录缓存里的版本目录迁入 <exedir>/plugins/redis；
// 目标已存在或源被占用（运行中）时跳过，扫描仍会把残留视为 downloaded 兜底。
func migrateLegacyDownloads() {
	srcRoot, err := downloadedRoot()
	if err != nil {
		return
	}
	dstRoot, err := offlineRoot()
	if err != nil {
		return
	}
	subs, err := os.ReadDir(srcRoot)
	if err != nil {
		return
	}
	for _, s := range subs {
		if !s.IsDir() || !versionRe.MatchString(s.Name()) {
			continue
		}
		srcDir := filepath.Join(srcRoot, s.Name())
		if findRedisServer(srcDir) == "" {
			continue
		}
		dstDir := filepath.Join(dstRoot, s.Name())
		if _, err := os.Stat(dstDir); err == nil {
			continue // 已有同名目标，不覆盖
		}
		if err := moveDir(srcDir, dstDir); err == nil {
			markInstalled(dstDir)
		}
	}
}

func moveDir(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	}
	if err := copyDir(src, dst); err != nil {
		os.RemoveAll(dst) // 跨卷清理半成品，下次扫描/迁移再试
		return err
	}
	return os.RemoveAll(src)
}

func copyDir(src, dst string) error {
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
		mode := st.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		return os.WriteFile(target, data, mode)
	})
}

type localEntry struct {
	Version string `json:"version"`
	Path    string `json:"path"` // 目录或 redis-server 可执行文件路径
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

// AddLocalVersion 注册本机已有的 redis-server 目录（或可执行文件路径）
func AddLocalVersion(dir string) error {
	dir = strings.Trim(dir, `"`)
	if dir == "" {
		return fmt.Errorf("目录不能为空")
	}
	st, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("路径不存在: %v", err)
	}
	exe := ""
	if st.IsDir() {
		exe = findRedisServer(dir)
	} else if strings.EqualFold(st.Name(), binFileName) {
		exe = dir
	}
	if exe == "" {
		return fmt.Errorf("目录中未找到 %s", binFileName)
	}
	// 版本号优先问二进制自己：Linux 的 /usr/bin/redis-server 路径里没有版本信息。
	ver := sysx.ProbeVersion(exe, "--version")
	if ver == "" {
		if st.IsDir() {
			ver = normalizeVersionName(st.Name())
		} else {
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

// normalizeVersionName 目录名不是纯版本号时兜底命名
func normalizeVersionName(name string) string {
	if versionRe.MatchString(name) {
		return name
	}
	if m := regexp.MustCompile(`(\d+\.\d+(\.\d+)*)`).FindString(name); m != "" {
		return m
	}
	return name
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
		// 包管理器装的东西不归面板管：手动删 /usr/bin 下的二进制只会把 dpkg/rpm
		// 数据库弄脏。真要卸请走系统命令，这里如实告诉用户。
		return fmt.Errorf("版本 %s 由系统包管理器提供，请用 %s 卸载", version, sysx.PkgManager())
	}
	cfgs, _ := ListInstances()
	for _, c := range cfgs {
		if c.Version == version && isProcessRunning(c.ID, c.Port, c.Password) {
			return fmt.Errorf("实例 %s 正在使用该版本，请先停止", c.Name)
		}
	}
	switch target.Source {
	case "downloaded":
		var lastErr error
		removed := false
		// 插件目录：仅删有安装标记的（避免误删发布内置目录）
		if root, err := offlineRoot(); err == nil {
			d := filepath.Join(root, version)
			if isInstalledVersion(d) {
				if err := os.RemoveAll(d); err != nil {
					lastErr = err
				} else {
					removed = true
				}
			}
		}
		// 旧下载缓存残留
		if root, err := downloadedRoot(); err == nil {
			d := filepath.Join(root, version)
			if st, err := os.Stat(d); err == nil && st.IsDir() {
				if err := os.RemoveAll(d); err != nil {
					lastErr = err
				} else {
					removed = true
				}
			}
		}
		if lastErr != nil {
			return lastErr
		}
		if !removed {
			return fmt.Errorf("未找到版本 %s 的可删除安装目录", version)
		}
		return nil
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

/* ============================ 在线版本（GitHub redis-windows） ============================ */

const ghReleasesAPI = "https://api.github.com/repos/redis-windows/redis-windows/releases?per_page=30"

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

// FetchRemoteVersions 拉取可安装版本列表。
//
// Linux 上不是“下载一个 zip”：Redis 官方只发源码，没有任何可携二进制，
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
	req, err := http.NewRequest(http.MethodGet, ghReleasesAPI, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "go-tools")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("获取版本列表失败（需可访问 api.github.com）: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API 返回 %d", resp.StatusCode)
	}
	var releases []ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return nil, err
	}
	var out []RemoteVersion
	seen := map[string]bool{}
	for _, r := range releases {
		ver := normalizeVersionName(strings.TrimPrefix(r.TagName, "v"))
		if !versionRe.MatchString(ver) || compareVersion(ver, "7.0") < 0 || seen[ver] {
			continue
		}
		// 资产择优：Windows-x64-msys2.zip > Windows-x64-cygwin.zip，排除 with-Service
		var pick *ghAsset
		score := -1
		for i := range r.Assets {
			a := &r.Assets[i]
			n := strings.ToLower(a.Name)
			if !strings.HasSuffix(n, ".zip") || !strings.Contains(n, "windows-x64") || strings.Contains(n, "with-service") {
				continue
			}
			s := 0
			if strings.Contains(n, "msys2") {
				s = 2
			} else if strings.Contains(n, "cygwin") {
				s = 1
			} else {
				continue
			}
			if s > score {
				score, pick = s, a
			}
		}
		if pick == nil {
			continue
		}
		seen[ver] = true
		out = append(out, RemoteVersion{Version: ver, URL: pick.BrowserDownloadURL, Size: pick.Size})
	}
	sort.Slice(out, func(i, j int) bool { return compareVersion(out[i].Version, out[j].Version) > 0 })
	return out, nil
}

/* ============================ 系统包（Linux） ============================ */

// systemRemoteVersions 把发行版仓库里的可安装版本包成统一的 RemoteVersion。
//
// URL 写成 pkg:<包名>：前端仍按版本号展示，不需要为 Linux 多一条分支；
// 安装时认前缀就知道该调包管理器。版本号必须归一：仓库里的
// "5:7.0.15-1ubuntu0.2" 和 redis-server --version 报告的 "7.0.15"
// 是同一个包，不统一两边就对不上。
func systemRemoteVersions() []RemoteVersion {
	cands := sysx.PackageCandidates(systemPkgNames)
	out := make([]RemoteVersion, 0, len(cands))
	for _, c := range cands {
		ver := sysx.TrimVersion(c.Version)
		if ver == "" {
			continue
		}
		out = append(out, RemoteVersion{Version: ver, URL: sysx.PkgScheme + c.Pkg})
	}
	return out
}

// systemPackageVersion 反查“版本号 → 包名”。
//
// 不能拿版本号拼包名：同一版本在不同发行版上带不同后缀，而且仓库随时会推进。
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
// 与下载安装的本质差别：依赖解析、运行用户创建、安装到标准路径全由包管理器
// 负责，面板不再解压一份到 plugins 目录——否则一个机器上会出现两份 Redis。
// 安装输出逐行走 stage=info 进日志框，与下载安装的观感一致。
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
				Note: "本机没有 dnf/yum/apt-get，请手工安装 redis 后刷新版本库"})
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

/* ============================ 版本安装（下载+解压） ============================ */

var (
	installMu  sync.Mutex
	installing = map[string]bool{}
)

func emitInstall(p InstallProgress) { emit("redis:install", p) }

// InstallVersion 异步下载安装版本；进度与安装日志（stage=info）经 "redis:install" 事件推送。
// 兜底：若插件目录（<exedir>/plugins/redis）或下载缓存目录已存在对应 zip，直接离线解压安装。
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
	root, err := offlineRoot()
	if err != nil {
		emitInstall(InstallProgress{Version: version, Stage: "error", Note: err.Error()})
		return
	}
	destDir := filepath.Join(root, version)

	info(fmt.Sprintf("开始安装 Redis %s：检查离线包…", version))

	// 1. 定位 zip：优先离线包（保留用户文件），否则下载到临时文件（用完即删）
	zipPath, isTemp, err := locateOrDownloadZip(version, destDir)
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

	// 2. 解压
	info("正在解压到：" + destDir)
	emitInstall(InstallProgress{Version: version, Stage: "extracting", Percent: 100})
	if err := extractZip(zipPath, destDir); err != nil {
		emitInstall(InstallProgress{Version: version, Stage: "error", Note: fmt.Sprintf("解压失败: %v", err)})
		return
	}
	if findRedisServer(destDir) == "" {
		os.RemoveAll(destDir)
		emitInstall(InstallProgress{Version: version, Stage: "error", Note: "安装包中未找到 redis-server.exe"})
		return
	}
	markInstalled(destDir) // 标记为安装版（区别于发布内置），允许后续删除
	info("安装完成：" + destDir)
	emitInstall(InstallProgress{Version: version, Stage: "done", Percent: 100, Note: destDir})
}

// locateOrDownloadZip 返回可用 zip 路径与是否临时下载文件（离线包优先且不删除；否则下载到临时文件）
// 离线包搜索顺序：<exedir>/plugins/redis → 下载缓存目录
func locateOrDownloadZip(version, destDir string) (string, bool, error) {
	var dirs []string
	if od, err := offlineRoot(); err == nil {
		dirs = append(dirs, od)
	}
	if rd, err := downloadedRoot(); err == nil {
		dirs = append(dirs, rd)
	}
	for _, d := range dirs {
		candidates := []string{
			filepath.Join(d, "Redis-"+version+"-Windows-x64-msys2.zip"),
			filepath.Join(d, version+".zip"),
		}
		if matches, _ := filepath.Glob(filepath.Join(d, "Redis-"+version+"-Windows-x64-*.zip")); matches != nil {
			candidates = append(candidates, matches...)
		}
		for _, c := range candidates {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c, false, nil
			}
		}
	}
	// 在线下载
	remotes, err := FetchRemoteVersions()
	if err != nil {
		return "", false, fmt.Errorf("获取版本列表失败: %v", err)
	}
	var url string
	var size int64
	for _, r := range remotes {
		if r.Version == version {
			url = r.URL
			size = r.Size
			break
		}
	}
	if url == "" {
		return "", false, fmt.Errorf("线上未找到版本 %s；可将对应 zip 放入插件目录 plugins/redis 后重试", version)
	}
	name := urlVersionHint(url)
	if size > 0 {
		emitInstall(InstallProgress{Version: version, Stage: "info", Note: fmt.Sprintf("开始下载：%s（%.1f MB）", name, float64(size)/1024/1024)})
	} else {
		emitInstall(InstallProgress{Version: version, Stage: "info", Note: "开始下载：" + name})
	}
	tmp := destDir + ".download.zip"
	if err := downloadFile(url, tmp, version); err != nil {
		os.Remove(tmp)
		return "", false, fmt.Errorf("下载失败: %v", err)
	}
	return tmp, true, nil
}

func downloadFile(url, dest, version string) error {
	client := &http.Client{Timeout: 10 * time.Minute}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "go-tools")
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
					emitInstall(InstallProgress{Version: version, Stage: "downloading", Percent: pct, Note: urlVersionHint(url)})
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

func urlVersionHint(url string) string {
	if i := strings.LastIndex(url, "/"); i >= 0 {
		return url[i+1:]
	}
	return ""
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
	useDocker := isDockerMode(&cfg)
	if useDocker {
		cfg.Mode = ModeDocker
		if strings.TrimSpace(cfg.Image) == "" {
			return fmt.Errorf("Docker 模式需指定镜像，如 %s", DefaultDockerImage)
		}
		// 容器实例不引用本机版本库：展示版本由镜像 tag 推导，留着旧值会误导模式切换
		cfg.Version = ""
	} else {
		cfg.Mode = ModeLocal
		if cfg.Version == "" {
			return fmt.Errorf("请选择 Redis 版本")
		}
	}
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return fmt.Errorf("端口非法: %d", cfg.Port)
	}
	if reason := PortTaken(cfg.Port, cfg.ID); reason != "" {
		return fmt.Errorf("%s", reason)
	}
	if !useDocker {
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
	for i := range list {
		if list[i].ID == cfg.ID {
			list[i] = cfg
			return saveInstances(list)
		}
	}
	list = append(list, cfg)
	return saveInstances(list)
}

// RemoveInstance 删除实例配置；运行中先停止
func RemoveInstance(id string) error {
	if isTracked(id) {
		if err := StopInstance(id); err != nil {
			return fmt.Errorf("停止失败，未删除: %v", err)
		}
	}
	// 容器不随配置消失：删掉它，否则下次新建同名实例会撞上残留容器
	if cfg, err := findInstance(id); err == nil && isDockerMode(cfg) {
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

type runningProc struct {
	cmd     *exec.Cmd
	logFile *os.File
	mu      sync.Mutex
	logs    []string // 环形缓冲（本进程生命周期内）
	done    chan struct{}
	// 就绪 / 主动停止标记：退出时据此区分「崩溃」与「用户停止」，见 srvstate
	srvstate.Flag
	// containerID 容器模式的实例没有 cmd，改用容器 ID 标识（停止/日志/状态都走 docker API）
	containerID string
	// cleared 记录「清空」时已产出的行数（=当时的 cum），Logs 据此跳过旧行；
	// cum 是进程生命周期内累计产出行数（含被环形缓冲挤掉的），两者配合实现非破坏式视图截止
	cleared int
	cum     int
}

// findInstance 按 ID 取实例配置
func findInstance(id string) (*InstanceConfig, error) {
	cfgs, err := loadInstances()
	if err != nil {
		return nil, err
	}
	for i := range cfgs {
		if cfgs[i].ID == id {
			return &cfgs[i], nil
		}
	}
	return nil, fmt.Errorf("实例不存在: %s", id)
}

var (
	runMu    sync.Mutex
	runProcs = map[string]*runningProc{}
)

const logRingMax = 500

// 非破坏式「清空」截止：清空只记下截止点（文件=字节偏移），
// 之后 Logs 只返回截止点之后新写入的日志，不删磁盘历史，也不会被下一次轮询回填。
var (
	fileCutMu  sync.Mutex
	fileCutoff = map[string]int64{} // 孤儿实例 server.log：已截止到的字节偏移
)

// fileSize 取文件当前大小（不存在/出错返回 0）
func fileSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}

func isTracked(id string) bool {
	runMu.Lock()
	defer runMu.Unlock()
	_, ok := runProcs[id]
	return ok
}

func trackedProc(id string) *runningProc {
	runMu.Lock()
	defer runMu.Unlock()
	return runProcs[id]
}

// isProcessRunning 网络探测：能 PING 通即视为运行（含非本进程拉起的孤儿实例）
func isProcessRunning(id string, port int, password string) bool {
	c, err := dial(fmt.Sprintf("127.0.0.1:%d", port), password, 0, 600*time.Millisecond)
	if err != nil {
		return false
	}
	defer c.Close()
	// 探活不能等：端口上接了但不回话时，2 秒内判否，不把状态轮询卡死
	c.SetCmdTimeout(2 * time.Second)
	_, err = c.Command("PING")
	return err == nil
}

func appendProcLog(p *runningProc, id, line string) {
	line = strings.TrimRight(line, "\r")
	if line == "" {
		return
	}
	ts := time.Now().Format("15:04:05.000")
	p.mu.Lock()
	p.cum++
	p.logs = append(p.logs, ts+"  "+line)
	if len(p.logs) > logRingMax {
		p.logs = p.logs[len(p.logs)-logRingMax:]
	}
	if p.logFile != nil {
		fmt.Fprintf(p.logFile, "%s  %s\n", ts, line)
	}
	p.mu.Unlock()
	emit("redis:serverlog", ServerLogLine{ID: id, Time: ts, Line: line})
}

// StartInstance 启动实例：端口探测 → 生成 redis.conf → 隐藏窗口拉起进程 → 等待 PING 就绪
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
	if isTracked(id) || isProcessRunning(id, cfg.Port, cfg.Password) {
		return fmt.Errorf("实例已在运行")
	}
	if isDockerMode(cfg) {
		return dockerStart(cfg)
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
	// 端口占用探测（区别于本实例：本实例已确认未运行）
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", cfg.Port))
	if err != nil {
		return fmt.Errorf("端口 %d 已被占用: %v", cfg.Port, err)
	}
	ln.Close()

	dataDir := cfg.DataDir
	if dataDir == "" {
		if dataDir, err = defaultInstanceDataDir(id); err != nil {
			return err
		}
	} else if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	confPath := filepath.Join(dataDir, "go-tools.conf")
	if err := os.WriteFile(confPath, []byte(buildConf(cfg, dataDir)), 0o644); err != nil {
		return err
	}

	logF, _ := os.OpenFile(filepath.Join(dataDir, "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)

	// 注意：msys2 构建会把命令行里的绝对 Windows 路径与 cwd 拼接成无效路径（/cygdrive/.../C:\...），
	// 因此固定以 dataDir 为工作目录、传相对文件名。
	cmd := exec.Command(exe, filepath.Base(confPath))
	cmd.Dir = dataDir
	cmd.SysProcAttr = hideWindowAttr()
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动失败: %v", err)
	}
	p := &runningProc{cmd: cmd, logFile: logF, done: make(chan struct{})}
	runMu.Lock()
	runProcs[id] = p
	runMu.Unlock()

	go scanPipe(id, p, stdout)
	go scanPipe(id, p, stderr)
	go func() {
		err := cmd.Wait()
		msg := "进程已退出"
		if err != nil {
			msg = fmt.Sprintf("进程退出: %v", err)
		}
		appendProcLog(p, id, msg)
		if p.logFile != nil {
			p.logFile.Close()
		}
		close(p.done)
		runMu.Lock()
		if runProcs[id] == p {
			delete(runProcs, id)
		}
		runMu.Unlock()
		// 就绪后非主动停止的退出＝崩溃：发事件让主程序弹系统通知
		if p.Abnormal() {
			code := -1
			if cmd.ProcessState != nil {
				code = cmd.ProcessState.ExitCode()
			}
			emit("redis"+srvstate.EventSuffix, srvstate.NewEvent("redis", "local", id, cfg.Name, cfg.Port, code))
		}
	}()

	// 等待就绪（最多 8s）
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if isProcessRunning(id, cfg.Port, cfg.Password) {
			appendProcLog(p, id, fmt.Sprintf("[go-tools] 实例 %s 启动成功 (port=%d ver=%s pid=%d)", cfg.Name, cfg.Port, cfg.Version, cmd.Process.Pid))
			p.MarkReady()
			return nil
		}
		select {
		case <-p.done:
			return fmt.Errorf("进程启动后退出，请查看日志")
		case <-time.After(200 * time.Millisecond):
		}
	}
	return fmt.Errorf("启动超时：8s 内未响应 PING，请查看日志")
}

func scanPipe(id string, p *runningProc, r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		appendProcLog(p, id, scanner.Text())
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
		appendProcLog(p, id, "读取进程输出结束: "+err.Error())
	}
}

// StopInstance 停止实例：优先优雅 SHUTDOWN NOSAVE（对孤儿进程有效），兜底 Kill
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
		if trackedProc(id) == nil && dockerContainerID(cfg) == "" && !isProcessRunning(id, cfg.Port, cfg.Password) {
			return fmt.Errorf("实例未在运行")
		}
		if err := dockerStop(cfg); err != nil {
			return err
		}
		CloseAddr(fmt.Sprintf("127.0.0.1:%d", cfg.Port))
		return nil
	}
	p := trackedProc(id)
	if !isProcessRunning(id, cfg.Port, cfg.Password) && p == nil {
		return fmt.Errorf("实例未在运行")
	}
	// 先标记主动停止：此后进程退出属于预期，不发「异常退出」通知
	if p != nil {
		p.MarkStopping()
	}
	// 优雅关闭
	if c, derr := dial(fmt.Sprintf("127.0.0.1:%d", cfg.Port), cfg.Password, 0, time.Second); derr == nil {
		if _, serr := c.Command("SHUTDOWN", "NOSAVE"); serr == nil || strings.Contains(serr.Error(), "EOF") || strings.Contains(serr.Error(), "use of closed") {
			c.Close()
		} else {
			c.Close()
		}
	}
	if p != nil {
		select {
		case <-p.done:
		case <-time.After(3 * time.Second):
			if p.cmd.Process != nil {
				_ = p.cmd.Process.Kill()
			}
			select {
			case <-p.done:
			case <-time.After(2 * time.Second):
			}
		}
		runMu.Lock()
		if runProcs[id] == p {
			delete(runProcs, id)
		}
		runMu.Unlock()
	}
	if isProcessRunning(id, cfg.Port, cfg.Password) {
		return fmt.Errorf("停止失败：实例仍在响应 PING")
	}
	// 实例已不再监听，把指向它的已登记连接一律作废：
	// 留着只会让前端下次操作撞上一次死 socket 的系统错误
	CloseAddr(fmt.Sprintf("127.0.0.1:%d", cfg.Port))
	return nil
}

// GetInstanceStatus 实例状态：进程跟踪信息 + PING/INFO 实时探测
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
	st.Name, st.Port = cfg.Name, cfg.Port
	// 容器模式：版本从镜像 tag 推导，PID 无意义；先填容器字段，后面的 INFO 会用真实服务端版本覆盖
	st.Mode = ModeLocal
	if isDockerMode(cfg) {
		fillDockerStatus(cfg, &st)
	} else {
		st.Version = cfg.Version
		if p := trackedProc(id); p != nil && p.cmd != nil && p.cmd.Process != nil {
			st.PID = p.cmd.Process.Pid
		}
	}
	c, err := dial(fmt.Sprintf("127.0.0.1:%d", cfg.Port), cfg.Password, 0, time.Second)
	if err != nil {
		return st, nil // 未运行不算错误
	}
	defer c.Close()
	c.SetCmdTimeout(5 * time.Second) // INFO 在负载高的实例上不快，但也不能无限期卡住面板
	st.Running = true
	info, err := c.Command("INFO")
	if err == nil {
		kv := parseInfo(asString(info))
		st.UptimeSeconds = parseInt(kv["uptime_in_seconds"])
		st.UsedMemoryHuman = kv["used_memory_human"]
		st.ConnectedClients = parseInt(kv["connected_clients"])
		st.TotalKeys = countAllKeys(kv)
		if v := kv["redis_version"]; v != "" {
			st.Version = v
		}
	}
	return st, nil
}

// Logs 返回实例最近 n 行日志（优先内存环形缓冲；孤儿进程读磁盘 server.log）
func Logs(id string, n int) ([]string, error) {
	if n <= 0 || n > logRingMax {
		n = 200
	}
	if cfg, err := findInstance(id); err == nil && isDockerMode(cfg) {
		return dockerLogs(cfg, n)
	}
	if p := trackedProc(id); p != nil {
		p.mu.Lock()
		logs := append([]string(nil), p.logs...)
		skip := p.cum - p.cleared // 清空之后再产出的行才展示：跳过前 skip 行（含被环形挤掉的）
		p.mu.Unlock()
		if skip < 0 {
			skip = 0
		}
		if skip > len(logs) {
			skip = len(logs)
		}
		logs = logs[skip:]
		if len(logs) > n {
			logs = logs[len(logs)-n:]
		}
		return logs, nil
	}
	cfgs, _ := loadInstances()
	for i := range cfgs {
		if cfgs[i].ID == id {
			dir := cfgs[i].DataDir
			if dir == "" {
				if d, err := defaultInstanceDataDir(id); err == nil {
					dir = d
				}
			}
			path := filepath.Join(dir, "server.log")
			fileCutMu.Lock()
			from := fileCutoff[id]
			fileCutMu.Unlock()
			// server.log 只在没跟踪到进程时做兼容读取，取尾巴不该整本入堆
			if from > 0 {
				return logtail.LinesAfter(path, from, n)
			}
			return logtail.Lines(path, n)
		}
	}
	return nil, fmt.Errorf("实例不存在: %s", id)
}

// ClearLogs 真实清空指定实例的日志（破坏式：截断 server.log、清空环形缓冲，
// 释放磁盘占用；旧行不再回填，刷新/重开弹窗都只剩清空之后的新日志）。
//   - 跟踪中进程：清空内存环形 + 截断自己持有的 server.log 句柄（O_APPEND 打开，截断安全）；
//   - 容器模式：日志源在 Docker daemon，本地无法截断，退化为清环形（刷新后仍可能带回旧行）；
//   - 孤儿/停止实例：直接截断 server.log 并归零截止偏移。
func ClearLogs(id string) error {
	if cfg, err := findInstance(id); err == nil && isDockerMode(cfg) {
		return nil
	}
	if p := trackedProc(id); p != nil {
		p.mu.Lock()
		p.logs = nil
		p.cum = 0
		p.cleared = 0
		if p.logFile != nil {
			_ = p.logFile.Truncate(0)
			_, _ = p.logFile.Seek(0, io.SeekStart)
		}
		p.mu.Unlock()
		return nil
	}
	cfgs, _ := loadInstances()
	for i := range cfgs {
		if cfgs[i].ID == id {
			dir := cfgs[i].DataDir
			if dir == "" {
				if d, err := defaultInstanceDataDir(id); err == nil {
					dir = d
				}
			}
			path := filepath.Join(dir, "server.log")
			_ = os.Truncate(path, 0)
			fileCutMu.Lock()
			fileCutoff[id] = 0
			fileCutMu.Unlock()
			return nil
		}
	}
	return fmt.Errorf("实例不存在: %s", id)
}

// SuggestPort 探测一个可用端口（从 from 起递增）：
// 既跳过其它实例已登记的端口，也跳过系统里已绑不上的端口
func SuggestPort(from int) int {
	if from <= 0 {
		from = 6379
	}
	return portcheck.Suggest(from, 100)
}

// PortTaken 报告端口是否可用于（新）实例（跨工具查重 + 系统监听）；
// excludeID 用于编辑场景排除自身，空串表示可用
func PortTaken(port int, excludeID string) string {
	exclude := ""
	if excludeID != "" {
		exclude = "redis:" + excludeID
	}
	return portcheck.Taken(port, exclude)
}

/* ============================ redis.conf 生成 ============================ */

func buildConf(cfg *InstanceConfig, dataDir string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# go-tools 自动生成，请勿手改（每次启动覆盖）\n")
	fmt.Fprintf(&b, "port %d\n", cfg.Port)
	fmt.Fprintf(&b, "bind 127.0.0.1\n")
	fmt.Fprintf(&b, "daemonize no\n")
	fmt.Fprintf(&b, "logfile \"\"\n") // 日志走 stdout，由本程序捕获
	fmt.Fprintf(&b, "dir %s\n", filepath.ToSlash(dataDir))
	if cfg.AppendOnly {
		b.WriteString("appendonly yes\n")
	} else {
		b.WriteString("appendonly no\n")
	}
	if cfg.MaxMemoryMB > 0 {
		fmt.Fprintf(&b, "maxmemory %dm\n", cfg.MaxMemoryMB)
		b.WriteString("maxmemory-policy noeviction\n")
	}
	if cfg.Password != "" {
		fmt.Fprintf(&b, "requirepass %s\n", cfg.Password)
	}
	return b.String()
}

// StopAll 停止所有由本程序拉起的 Redis 实例（应用退出时调用）
func StopAll() {
	runMu.Lock()
	ids := make([]string, 0, len(runProcs))
	for id := range runProcs {
		ids = append(ids, id)
	}
	runMu.Unlock()
	for _, id := range ids {
		_ = StopInstance(id)
	}
}
