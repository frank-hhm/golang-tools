package nginx

// ============================================================
// 本地 Nginx 服务管理（nginx.exe 进程维度）
// 结构复刻 tools/mysql/server.go（redis 同款范式），差异点：
//   - 版本库：nginx.org 官方 Windows zip（nginx-<ver>.zip，解压约 2MB）
//   - bundled    ：<exedir>/plugins/nginx/<ver>/（发布内置，无安装标记）
//   - downloaded ：在线获取/离线 zip 安装，统一落 <exedir>/plugins/nginx/<ver>/ 并写 .Tools-installed 标记
//   - local      ：用户手动注册的本机已有目录（登记在 Tools/nginx-locals.json）
//   - 实例：配置持久化 Tools/nginx-instances.json；每个实例以「实例目录为
//     prefix」运行：Tools/nginx-instances/<id>/{conf/nginx.conf, logs/, html/}。
//     首次启动按模板生成 nginx.conf（端口/站点根取实例配置），之后配置文件由用户掌控。
//   - nginx 不支持改配置热生效以外的优雅关闭入口（无管理端口），因此：
//     停止用 `nginx -s quit`（-p 指向实例目录），兜底按 PID 结束进程树；
//     重载配置用 `nginx -s reload`。
//   - 状态判定：端口在监听 + 进程存在（master + workers 数由进程枚举得到）。
//   - 安装进度经 "nginx:install" 事件推送（InstallProgress，stage 与 redis/mysql 一致）。
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

	"Tools/appdirs"
	"Tools/services/sysx"
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

func emitInstall(p InstallProgress) { srvEmit("nginx:install", p) }

/* ============================ 类型 ============================ */

// ServerVersion 本地可用的一个 Nginx 版本
type ServerVersion struct {
	Version string `json:"version"` // 如 1.27.4
	Path    string `json:"path"`    // nginx.exe 完整路径
	Source  string `json:"source"`  // bundled | downloaded | local
}

// RemoteVersion 线上可安装版本
type RemoteVersion struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	Size    int64  `json:"size"`
	Release string `json:"release"` // 所属产品线，如 1.28
}

// InstanceConfig 本地 Nginx 实例配置
type InstanceConfig struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Version    string `json:"version"`
	Port       int    `json:"port"`       // HTTP 端口（容器模式下是宿主机发布端口）
	SSLPort    int    `json:"sslPort"`    // >0 时生成 https 监听
	Domain     string `json:"domain"`     // 绑定域名（写入 nginx.conf 的 server_name）；空 = 仅 localhost
	DocRoot    string `json:"docRoot"`    // 站点根目录；空 = 实例目录下 html/
	Workers    int    `json:"workers"`    // worker_processes；<=0 用 auto
	StubStatus bool   `json:"stubStatus"` // 是否开放 /stub_status 状态页
	// PhpMode PHP 后端：空/static = 纯静态；fastcgi = php-cgi FastCGI（nginx fastcgi_pass）；proxy = 反向代理到 php -S
	PhpMode string `json:"phpMode"`
	// PhpVersion 关联的 PHP 版本（fastcgi/proxy 时非空）
	PhpVersion string `json:"phpVersion"`
	// PhpPort / PhpInstanceID 由保存流程回填：关联 PHP 实例的监听端口与实例 ID（启停/删除联动用）
	PhpPort       int    `json:"phpPort"`
	PhpInstanceID string `json:"phpInstanceId"`
	// Mode 运行模式：local（默认，空串同义）| docker（容器内跑 nginx，见 docker.go）
	Mode string `json:"mode"`
	// Image Docker 模式使用的镜像，如 nginx:alpine；本地模式为空
	Image string `json:"image"`
	// ContainerName 自定义容器名；留空按实例 ID 生成 Tools-nginx-<id>
	ContainerName string `json:"containerName"`
}

// InstanceStatus 实例运行状态
type InstanceStatus struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Version        string  `json:"version"`
	Port           int     `json:"port"`
	SSLPort        int     `json:"sslPort"`
	Running        bool    `json:"running"`
	PID            int     `json:"pid"` // master 进程 PID（容器模式为 0，进程在容器 namespace 里）
	Workers        int     `json:"workers"`
	UptimeSeconds  int64   `json:"uptimeSeconds"`
	DocRoot        string  `json:"docRoot"`
	Domain         string  `json:"domain"` // 绑定域名（来自实例配置）
	PhpMode        string  `json:"phpMode"`
	PhpVersion     string  `json:"phpVersion"`
	PhpPort        int     `json:"phpPort"`
	ConfigPath     string  `json:"configPath"`
	Prefix         string  `json:"prefix"`
	RequestsPerSec float64 `json:"requestsPerSec"` // 由 access.log 行数估算
	// Mode 运行模式：local | docker
	Mode string `json:"mode"`
	// Image / Container 容器模式下的镜像与容器名（本地模式为空）
	Image     string `json:"image"`
	Container string `json:"container"`
	// ContainerID / ContainerState 实际容器的短 ID 与状态（running/exited/created…）
	ContainerID    string `json:"containerId"`
	ContainerState string `json:"containerState"`
}

// InstallProgress 版本安装进度事件负载（stage 语义与 redis/mysql 一致）
type InstallProgress struct {
	Version string `json:"version"`
	Stage   string `json:"stage"` // info|downloading|extracting|done|error
	Percent int    `json:"percent"`
	Note    string `json:"note"`
}

/* ============================ 目录约定 ============================ */

// goToolsDir 数据目录 data/nginx：实例登记与实例工作目录（基础目录设置页可改，见 appdirs）
func goToolsDir() (string, error) { return appdirs.Data("nginx") }

// pluginsRoot 插件目录 plugins/nginx（确保存在）
func pluginsRoot() (string, error) { return appdirs.Plugins("nginx") }

// PluginsDir 导出版：返回插件目录（供前端展示离线包放置指引）
func PluginsDir() (string, error) { return pluginsRoot() }

// installedMarker 安装版标记（区别于发布内置版，允许删除）
const installedMarker = ".Tools-installed"

func isInstalledVersion(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, installedMarker))
	return err == nil && !st.IsDir()
}

func markInstalled(dir string) {
	_ = os.WriteFile(filepath.Join(dir, installedMarker), []byte("installed by Tools\n"), 0o644)
}

func localsRegistryPath() (string, error) {
	d, err := goToolsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "nginx-locals.json"), nil
}

func instancesFilePath() (string, error) {
	d, err := goToolsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "nginx-instances.json"), nil
}

// instanceDir 实例 prefix 目录（conf/ + logs/ + html/ + temp/ 各子目录）
// Windows 版 nginx 启动时必须预建 temp 子目录，否则报 CreateDirectory() failed
func instanceDir(id string) (string, error) {
	d, err := goToolsDir()
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", fmt.Errorf("实例 ID 不能为空")
	}
	dir := filepath.Join(d, "nginx-instances", filepath.Base(id))
	for _, sub := range []string{
		"", "conf", "logs", "html",
		"temp",
		"temp/client_body_temp",
		"temp/proxy_temp",
		"temp/fastcgi_temp",
		"temp/uwsgi_temp",
		"temp/scgi_temp",
	} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			return "", err
		}
	}
	return dir, nil
}

/* ============================ 版本工具 ============================ */

var versionRe = regexp.MustCompile(`^\d+\.\d+(\.\d+)?$`)

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

// normalizeVersionName 目录名不是纯版本号时提取版本号（nginx-1.27.4 → 1.27.4）
func normalizeVersionName(name string) string {
	if versionRe.MatchString(name) {
		if strings.Count(name, ".") == 1 {
			return name + ".0"
		}
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

// findNginx 在版本目录内定位 nginx 二进制：根、一级子目录
func findNginx(dir string) string {
	try := []string{filepath.Join(dir, binFileName)}
	if subs, err := os.ReadDir(dir); err == nil {
		for _, s := range subs {
			if s.IsDir() {
				try = append(try, filepath.Join(dir, s.Name(), binFileName))
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

/* ============================ 版本库 ============================ */

// ListVersions 聚合各渠道版本，按版本号降序；同版本优先 bundled > downloaded > local > system
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
				if p := findNginx(verDir); p != "" {
					add(src, normalizeVersionName(s.Name()), p)
				}
			}
		}
	}
	for _, lv := range loadLocals() {
		if st, err := os.Stat(lv.Path); err == nil {
			if !st.IsDir() && strings.EqualFold(st.Name(), binFileName) {
				add("local", lv.Version, lv.Path)
			} else if st.IsDir() {
				if p := findNginx(lv.Path); p != "" {
					add("local", lv.Version, p)
				}
			}
		}
	}
	// system：系统包管理器装的（Linux 上这是主要来源，Windows 下也认 PATH 里的）
	// 版本号必须问二进制自己：/usr/sbin/nginx 没有“目录名”可当版本号。
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
	Path    string `json:"path"` // 目录或 nginx.exe 路径
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

// AddLocalVersion 注册本机已有的 Nginx 目录（或 nginx.exe 路径）
func AddLocalVersion(dir string) error {
	dir = strings.Trim(dir, `"`)
	if dir == "" {
		return fmt.Errorf("目录不能为空")
	}
	exe := ""
	if st, err := os.Stat(dir); err == nil {
		if st.IsDir() {
			exe = findNginx(dir)
		} else if strings.EqualFold(st.Name(), binFileName) {
			exe = dir
		}
	}
	if exe == "" {
		return fmt.Errorf("目录中未找到 %s（支持根目录或一级子目录）", binFileName)
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
	if target.Source == "system" {
		// 包管理器装的东西不归面板管：手动删 /usr/sbin 下的二进制只会把 dpkg/rpm
		// 数据库弄脏。真要卸请走系统命令，这里如实告诉用户。
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
		d := filepath.Join(root, version)
		if !isInstalledVersion(d) {
			if findNginx(d) == "" {
				return fmt.Errorf("未找到版本 %s 的安装目录", version)
			}
			return fmt.Errorf("目录 %s 非 Tools 安装版（内置或手工放置），不可删除", version)
		}
		return os.RemoveAll(d)
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

// nginxExe 定位版本目录中的 nginx 二进制
func nginxExe(version string) string {
	vers, _ := ListVersions()
	for _, v := range vers {
		if v.Version == version {
			return v.Path
		}
	}
	return ""
}

// probeVersion 执行 nginx -v 获取版本号
func probeVersion(exe string) (string, error) {
	cmd := exec.Command(exe, "-v")
	cmd.SysProcAttr = hideWindowAttr()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", err
	}
	if m := verExtractRe.FindString(string(out)); m != "" {
		return m, nil
	}
	return "", fmt.Errorf("无法解析版本号")
}

/* ============================ 系统包（Linux） ============================ */

// systemRemoteVersions 把发行版仓库里的可安装版本包成统一的 RemoteVersion。
//
// URL 写成 pkg:<包名>：前端仍按版本号展示，不需要为 Linux 多一条分支；
// 安装时认前缀就知道该调包管理器。版本号用 TrimVersion 归一——仓库里的
// "1.24.0-1ubuntu2" 装完 `nginx -v` 只会报告 "1.24.0"，两边必须长得一样，
// 否则实例里选的版本号回头找不到对应二进制。
func systemRemoteVersions() []RemoteVersion {
	cands := sysx.PackageCandidates(systemPkgNames)
	out := make([]RemoteVersion, 0, len(cands))
	for _, c := range cands {
		ver := sysx.TrimVersion(c.Version)
		if ver == "" {
			continue
		}
		out = append(out, RemoteVersion{Version: ver, URL: sysx.PkgScheme + c.Pkg, Release: seriesOf(ver)})
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
// 负责，面板不再解压一份到 plugins 目录——否则一台机器上会出现两份二进制。
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
				Note: "本机没有 dnf/yum/apt-get，请手工安装 nginx 后刷新版本库"})
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

/* ============================ 下载与解压 ============================ */

var (
	installMu  sync.Mutex
	installing = map[string]bool{}
)

// InstallVersion 异步安装版本；进度经 "nginx:install" 事件推送
func InstallVersion(version string) error {
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

	info(fmt.Sprintf("开始安装 Nginx %s：检查离线包…", version))
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
	inner := findNginx(tmpDir)
	if inner == "" {
		os.RemoveAll(tmpDir)
		emitInstall(InstallProgress{Version: version, Stage: "error", Note: "安装包中未找到 nginx.exe"})
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
	info("安装完成：" + destDir)
	emitInstall(InstallProgress{Version: version, Stage: "done", Percent: 100, Note: destDir})
}

// locateOrDownloadZip 优先插件目录离线 zip；否则按官方 URL 下载（临时文件）
func locateOrDownloadZip(version string) (string, bool, error) {
	root, err := pluginsRoot()
	if err != nil {
		return "", false, err
	}
	candidates := []string{
		filepath.Join(root, fmt.Sprintf("nginx-%s.zip", version)),
		filepath.Join(root, version+".zip"),
	}
	if matches, _ := filepath.Glob(filepath.Join(root, fmt.Sprintf("nginx-%s*.zip", version))); matches != nil {
		candidates = append(candidates, matches...)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, false, nil
		}
	}
	url := zipURL(version)
	emitInstall(InstallProgress{Version: version, Stage: "info", Note: "开始下载：" + filepath.Base(url)})
	tmp := filepath.Join(root, version+".download.zip")
	if err := downloadFile(url, tmp, version); err != nil {
		os.Remove(tmp)
		return "", false, fmt.Errorf("下载失败: %v；可将 nginx-%s.zip 放入插件目录后重试（离线安装）", err, version)
	}
	return tmp, true, nil
}

func downloadFile(url, dest, version string) error {
	client := &http.Client{Timeout: 20 * time.Minute}
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

func userAgent() string {
	return "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
}
