//go:build windows

package docker

// ============================================================
// Docker Desktop 安装链路（面板内一键安装 / 离线包安装 / 启动等待）
//
// 边界与硬约束（与需求讨论一致，不做妥协）：
//   1. 订阅协议不设勾选门禁，只在安装按钮旁的感叹号提示里如实展示 LicenseNotes；
//      --accept-license 由点击「安装」这个显式动作触发，不默默代同意。
//   2. 改系统状态的动作一律提权且可见（SW_SHOW），用户能在安装器窗口里看到进度、
//      也能自己关掉；超时/取消/非零退出都原样回显，不猜原因。
//   3. 离线包兜底：插件目录放安装器即可跳过下载直接装；下载也支持断点续传与取消。
//   4. 静默安装后 Docker Desktop 不会自启（官方行为），因此装完由本程序以
//      普通用户身份拉起，再轮询命名管道直到引擎可达。
//
// 安装器参数按 Docker Desktop 4.x 公开用法拼装（install --quiet --accept-license
// [--web-download]）；直链默认取官方不带 build 号的最新版，允许覆盖为 build 直链
// 或任意完整 URL，下载前先 HEAD 校验体积，避免把 404 页面当安装包。
// ============================================================

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"Tools/appdirs"
	"Tools/services/winrun"
	"Tools/tools/downloader"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	// InstallerLatestURL 官方最新稳定版直链（内容随官方滚动，故不保证版本可复现）
	InstallerLatestURL = "https://desktop.docker.com/win/main/amd64/Docker%20Desktop%20Installer.exe"
	// installerBuildURLFmt 带 build 号的可复现直链模板
	installerBuildURLFmt = "https://desktop.docker.com/win/main/amd64/%s/Docker%%20Desktop%%20Installer.exe"

	setupEvent = "docker:setup"

	downloadTimeout = 60 * time.Minute
	installTimeout  = 40 * time.Minute // 安装器含 WSL 组件初始化，给足余量
	wslInstallTO    = 20 * time.Minute
	engineWaitTime  = 150 * time.Second

	// browserUA 下载直链走官方 CDN，用通用 UA 避免被拦
	browserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36"
)

/* ============================ 安装引导 ============================ */

// LicenseNotes 安装前需如实告知的要点，前端挂在感叹号悬停提示里逐行展示（不含糊其辞）
func LicenseNotes() []string {
	return []string{
		"Docker Desktop 受 Docker Subscription Service Agreement 约束：个人开发、学习用途、以及小型企业（雇员少于 250 人且年收入不超过 1000 万美元）可免费商用，超出该范围需要付费订阅。",
		"点击「安装」即表示你已阅读并同意上述协议；企业环境请先与授权负责人确认。",
		"安装器会请求管理员权限；WSL2 未就绪时可能需要启用系统组件并重启计算机。",
		"安装包体积约 500–600 MB，请确认网络流量与磁盘空间充足。",
	}
}

func emitSetup(p SetupProgress) { emit(setupEvent, p) }

/* ============================ 直链构造 ============================ */

var digitsRe = regexp.MustCompile(`\d+`)

// BuildInstallerURL 把用户输入换算成最终下载直链：
//
//	空 → 最新稳定版；含 :// → 原样（非 http(s) 交由探测环节拒绝，不默认换成官方地址）；含 build 号（如 4.34.0/165345 或 165345）→ 取末段数字作 build。
func BuildInstallerURL(input string) string {
	v := strings.TrimSpace(input)
	if v == "" {
		return InstallerLatestURL
	}
	if strings.Contains(v, "://") {
		return v
	}
	all := digitsRe.FindAllString(v, -1)
	if len(all) == 0 {
		return InstallerLatestURL
	}
	build := all[len(all)-1] // "4.34.0/165345" → 165345；"165345" → 165345
	return fmt.Sprintf(installerBuildURLFmt, build)
}

func hasHTTPScheme(u string) bool {
	l := strings.ToLower(u)
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}

/* ============================ 取消 ============================ */

var (
	setupMu     sync.Mutex
	setupCancel context.CancelFunc
)

// AbortSetup 取消进行中的下载，并删除已下载的半截文件（安装器已启动后无法安全中断，返回提示）
func AbortSetup() error {
	setupMu.Lock()
	defer setupMu.Unlock()
	if setupCancel == nil {
		return fmt.Errorf("当前没有进行中的下载")
	}
	setupCancel()
	return nil
}

// dropPart 删除未完成的下载文件，返回要展示给用户的说明
func dropPart(part string) string {
	err := os.Remove(part)
	switch {
	case err == nil:
		return "已删除未完成的下载文件: " + part
	case os.IsNotExist(err):
		return "本次尚未写入数据，无需清理"
	default:
		return fmt.Sprintf("未完成的下载文件删除失败: %s（%v）", part, err)
	}
}

func registerCancel(cancel context.CancelFunc) {
	setupMu.Lock()
	setupCancel = cancel
	setupMu.Unlock()
}

func unregisterCancel() {
	setupMu.Lock()
	setupCancel = nil
	setupMu.Unlock()
}

/* ============================ 体检 ============================ */

// locateDesktopExe 找到 Docker Desktop.exe：ProgramFiles 系列 → 常见自定义路径 → 注册表 InstallLocation / DisplayIcon
func locateDesktopExe() (string, bool) {
	var dirs []string
	for _, env := range []string{"ProgramFiles", "ProgramW6432", "ProgramFiles(x86)"} {
		if v := os.Getenv(env); v != "" {
			dirs = append(dirs, filepath.Join(v, "Docker", "Docker"))
		}
	}
	if pf := os.Getenv("LOCALAPPDATA"); pf != "" {
		dirs = append(dirs, filepath.Join(pf, "Programs", "Docker", "Docker"))
	}
	for _, d := range dirs {
		p := filepath.Join(d, "Docker Desktop.exe")
		if fileExists(p) {
			return p, true
		}
	}
	if info, ok := findUninstallEntry(); ok {
		var cands []string
		if info.installLocation != "" {
			cands = append(cands, filepath.Join(info.installLocation, "Docker Desktop.exe"))
		}
		if p := iconExePath(info.displayIcon); p != "" {
			cands = append(cands, p)
		}
		for _, p := range cands {
			if fileExists(p) {
				return p, true
			}
		}
	}
	return "", false
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// uninstallInfo 卸载表里的 Docker Desktop 登记信息
type uninstallInfo struct {
	installLocation string
	displayVersion  string
	displayIcon     string // 通常是 exe 全路径（可能带 ",0" 图标索引后缀）
}

// findUninstallEntry 在卸载表（HKLM 64/32 位 + HKCU）里找 Docker Desktop 登记
func findUninstallEntry() (uninstallInfo, bool) {
	for _, src := range []struct {
		hive registry.Key
		root string
	}{
		{registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
		{registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
	} {
		if info, ok := scanUninstall(src.hive, src.root); ok {
			return info, true
		}
	}
	return uninstallInfo{}, false
}

// scanUninstall 遍历指定卸载表找 Docker Desktop
func scanUninstall(hive registry.Key, root string) (uninstallInfo, bool) {
	var info uninstallInfo
	k, err := registry.OpenKey(hive, root, registry.READ)
	if err != nil {
		return info, false
	}
	defer k.Close()
	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return info, false
	}
	for _, n := range names {
		sk, err := registry.OpenKey(k, n, registry.READ)
		if err != nil {
			continue
		}
		display, _, _ := sk.GetStringValue("DisplayName")
		if !strings.EqualFold(strings.TrimSpace(display), "Docker Desktop") {
			sk.Close()
			continue
		}
		info.installLocation, _, _ = sk.GetStringValue("InstallLocation")
		info.displayVersion, _, _ = sk.GetStringValue("DisplayVersion")
		info.displayIcon, _, _ = sk.GetStringValue("DisplayIcon")
		info.installLocation = strings.TrimSpace(info.installLocation)
		info.displayVersion = strings.TrimSpace(info.displayVersion)
		info.displayIcon = strings.TrimSpace(info.displayIcon)
		sk.Close()
		return info, true
	}
	return info, false
}

// iconExePath 从 DisplayIcon 里剥出 exe 路径：兼容带引号与尾部 ",<索引>" 两种写法
func iconExePath(icon string) string {
	if icon == "" {
		return ""
	}
	if strings.HasPrefix(icon, `"`) {
		if end := strings.Index(icon[1:], `"`); end >= 0 {
			return icon[1 : 1+end]
		}
		return strings.TrimPrefix(icon, `"`)
	}
	if i := strings.LastIndex(icon, ","); i > 1 {
		if _, err := strconv.Atoi(strings.TrimSpace(icon[i+1:])); err == nil {
			return icon[:i]
		}
	}
	return icon
}

// pendingReboot 系统是否有挂起的重启操作（MSI 安装器常见尾声）：
// Session Manager 有待重命名/替换的文件，或 CBS 挂了 RebootPending 标志。
func pendingReboot() bool {
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager`, registry.READ); err == nil {
		names, nerr := k.ReadValueNames(-1)
		k.Close()
		if nerr == nil {
			for _, n := range names {
				if strings.EqualFold(n, "PendingFileRenameOperations") {
					return true
				}
			}
		}
	}
	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows\CurrentVersion\Component Based Servicing\RebootPending`, registry.READ); err == nil {
		k.Close()
		return true
	}
	return false
}

// pipeListening 有没有人在监听引擎管道（不阻塞）：
// 它是区分「Desktop 没启动」与「正在启动中、只是还没应答」的关键证据
func pipeListening() bool {
	for _, p := range defaultPipes {
		if pipeExistsOnce(p) {
			return true
		}
	}
	return false
}

// CheckEnv 安装前只读体检：不安装、不提权、不改任何系统状态。
// dir 为安装包存放/扫描目录，空 = 默认 <插件目录>\docker。
func CheckEnv(dir string) (EnvCheck, error) {
	c := EnvCheck{DefaultURL: InstallerLatestURL}
	if p, ok := locateDesktopExe(); ok {
		c.DockerExePath, c.DockerInstalled = p, true
		_, c.DockerVersion = registryVersionOnly()
	}
	c.PipeExists = pipeListening()
	// WSL：wsl.exe 存在且 --status 正常返回才算组件就绪；
	// 同时把发行版清单带回去，能在“为什么没切成功”里充当独立证据
	w := probeWsl()
	c.WslInstalled, c.WslStatus, c.WslDockerDistro = w.Ready, w.Note, w.Docker
	c.WslDistros = w.distroLabels()
	var dirErr error
	if d, err := installerDir(dir); err == nil {
		c.InstallerDir = d
		if f, size := findLocalInstallerIn(d); f != "" {
			c.InstallerFile, c.InstallerSize = f, size
		}
	} else {
		c.InstallerDir, dirErr = strings.TrimSpace(dir), err
	}
	// 后端识别（含引擎探活）：失败也不影响其余体检，最差是 Backend=未知
	fillBackend(&c)
	c.Notes = append(envNotes(c), backendNotes(c)...)
	if dirErr != nil {
		// 目录坏了不能静默忽略：否则用户以为包会下到他选的目录
		c.Notes = append(c.Notes, "安装包目录不可用: "+dirErr.Error())
	}
	return c, nil
}

func registryVersionOnly() (string, string) {
	if info, ok := findUninstallEntry(); ok {
		return info.installLocation, info.displayVersion
	}
	return "", ""
}

// wslBroken WSL 组件本身缺失/未启用的措辞：这种状态下 Docker Desktop 用不了 WSL2 后端，
// 切过去只会静默沿用 Hyper-V。本机实测：wsl --status 只回“默认版本: 2”，
// 要靠 wsl -l -v 那句“未安装适用于 Linux 的 Windows 子系统”才露馅。
func wslBroken(s string) bool {
	l := strings.ToLower(s)
	for _, kw := range []string{"未安装", "not installed", "未启用", "无法", "could not", "命令未找到", "not found"} {
		if strings.Contains(l, kw) {
			return true
		}
	}
	return false
}

// vmPlatformDisabledWords 命中「虚拟机平台 (Virtual Machine Platform) 未启用」的中英文措辞。
// 这是 WSL2 后端特有的硬卡点（区别于一般的“WSL 没装”）：启用它必须重启计算机才生效，
// Docker Desktop 自己不会替用户跑这条命令，所以值得单独识别、给出方向性指引。
func vmPlatformDisabledWords(s string) bool {
	l := strings.ToLower(s)
	hasVM := strings.Contains(l, "虚拟机平台") || strings.Contains(l, "virtual machine platform")
	hasOff := strings.Contains(l, "未启用") || strings.Contains(l, "not enabled") ||
		strings.Contains(l, "无法启动") || strings.Contains(l, "could not") ||
		strings.Contains(l, "未安装") || strings.Contains(l, "not installed")
	return hasVM && hasOff
}

// vmPlatformBlocked 只读探一次 wsl，判「虚拟机平台未启用」这一具体卡点。
// 命中时返回 (true, 证据原文)：调用方据此尽早中止无谓的引擎等待，直接提示启用+重启。
// 注意用词稳定（是系统功能状态，不是 WSL 初始化中的瞬时报错），不会把首次建发行版误判进来。
func vmPlatformBlocked() (bool, string) {
	out, err := runWsl(10*time.Second, "--status")
	probe := out
	if err != nil {
		probe += " " + err.Error()
	}
	list, errL := runWsl(12*time.Second, "-l", "-v")
	if errL != nil && strings.TrimSpace(list) == "" {
		list = errL.Error()
	}
	probe += " " + list
	if vmPlatformDisabledWords(probe) {
		return true, oneLine(truncate(probe, 240))
	}
	return false, ""
}

// wslNoDistro “组件在但一个发行版都没注册”的措辞：Docker Desktop 会自建 docker-desktop，
// 这只算状态说明，不能当成用不了 WSL2。
// 本机实测（WSL_UTF8=1）的原文是“适用于 Linux 的 Windows 子系统没有已安装的分发。”
func wslNoDistro(s string) bool {
	l := strings.ToLower(s)
	for _, kw := range []string{
		"no installed distribution", "no distributions", "there is no distribution",
		"没有已安装的分发", "未找到任何分发", "未找到任何发行版", "没有已安装的发行版",
		"尚未安装发行版", "尚未安装分发", "没有注册",
	} {
		if strings.Contains(l, kw) {
			return true
		}
	}
	return false
}

// wslNegative WSL 不可用的总结判据（组件缺失或没发行版），供体检兜底文案使用
func wslNegative(s string) bool { return wslBroken(s) || wslNoDistro(s) }

// wslDistro wsl -l -v 里的一个发行版
type wslDistro struct {
	Name  string
	State string // running | stopped | installing
}

// parseWslDistros 解析 wsl -l -v：表头与报错文案跳过，状态词中英都认。
// 原文形如（* 与名字之间是一个空格，不能直接按首字段取名字）：
//
//	  NAME            STATE           VERSION
//	* docker-desktop  Running         2
func parseWslDistros(out string) []wslDistro {
	var list []wslDistro
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		i := 0
		if f[0] == "*" {
			i = 1
		}
		if len(f) < i+2 {
			continue
		}
		name := strings.TrimSpace(strings.TrimPrefix(f[i], "*"))
		if name == "" || strings.EqualFold(name, "name") || name == "名称" {
			continue
		}
		l := strings.ToLower(line)
		state := ""
		switch {
		case strings.Contains(l, "running") || strings.Contains(line, "正在运行"):
			state = "running"
		case strings.Contains(l, "stopped") || strings.Contains(line, "已停止"):
			state = "stopped"
		case strings.Contains(l, "installing") || strings.Contains(line, "正在安装"):
			state = "installing"
		}
		if state == "" {
			continue // 表头、报错文案、提示行都不带状态词
		}
		list = append(list, wslDistro{Name: name, State: state})
	}
	return list
}

// wslStateLines 数 wsl -l -v 里的发行版行（“名字 状态”），便于旧调用点与测试复用
func wslStateLines(s string) []string {
	var out []string
	for _, d := range parseWslDistros(s) {
		out = append(out, d.Name+" "+d.State)
	}
	return out
}

// wslDistroRunning 指定发行版是否在跑：WSL2 后端下 docker-desktop 一定是 Running，
// 它是“引擎确实在用 WSL2”的正面证据（内核串万一认不出时用来兜底）
func wslDistroRunning(out, name string) bool {
	for _, d := range parseWslDistros(out) {
		if strings.EqualFold(d.Name, name) {
			return d.State == "running"
		}
	}
	return false
}

// envNotes 把体检结果翻译成待办清单，前端逐行展示
func envNotes(c EnvCheck) []string {
	var n []string
	switch {
	case c.EngineAvailable:
		n = append(n, "Docker 引擎已在运行，无需安装，可直接使用容器与 MySQL Docker 模式。")
	case c.DockerInstalled && c.PipeExists:
		n = append(n, "已安装 Docker Desktop 且管道存在，但引擎未应答：通常是正在启动或后端异常，先看引擎错误详情。"+
			"面板每 5 秒自动重探，引擎一应答就自己接上，不用反复点「启动引擎」。")
	case c.DockerInstalled:
		n = append(n, "已安装 Docker Desktop，但引擎未运行：点「启动引擎」拉起，或在开始菜单打开 Docker Desktop。")
	default:
		n = append(n, "未检测到 Docker Desktop，需要安装后才能使用容器管理。")
	}
	if !c.WslInstalled {
		if c.DockerInstalled {
			n = append(n, "WSL2 未就绪（下方 wsl 原文是证据）：Docker Desktop 只能跑在 Hyper-V 后端，切 WSL2 会被静默沿用旧后端；"+
				"先点「启用 WSL2」或「更新 WSL2」，必要时重启计算机。")
		} else {
			n = append(n, "WSL2 未就绪：Linux 容器（官方 mysql 镜像）依赖它，可勾选后一键提权启用，安装器也会自动补齐组件。")
		}
	}
	if c.InstallerFile != "" {
		n = append(n, "安装目录已发现离线安装包（"+filepath.Base(c.InstallerFile)+"，位于 "+filepath.Dir(c.InstallerFile)+"），可直接用它安装，跳过下载。")
	}
	return n
}

// installerDir 解析安装包存放目录：dir 为空回落到全局约定的 <插件目录>\docker；
// 显式指定时只做绝对化与可建校验，绝不静默回落——避免“另存目录”失效后把包丢到别处。
func installerDir(dir string) (string, error) {
	d := strings.TrimSpace(dir)
	if d == "" {
		return appdirs.Plugins("docker")
	}
	abs, err := filepath.Abs(d)
	if err != nil {
		return "", fmt.Errorf("安装包目录不合法: %s（%v）", d, err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", fmt.Errorf("安装包目录不可用: %s（%v）", abs, err)
	}
	return abs, nil
}

// findLocalInstaller 按 dir 解析出存放目录后找安装器（dir 空=插件目录/docker）
func findLocalInstaller(dir string) (string, int64) {
	d, err := installerDir(dir)
	if err != nil {
		return "", 0
	}
	return findLocalInstallerIn(d)
}

// findLocalInstallerIn 指定目录下最新的安装器 exe（离线兜底）
func findLocalInstallerIn(dir string) (string, int64) {
	matches, _ := filepath.Glob(filepath.Join(dir, "*nstaller*.exe"))
	if len(matches) == 0 {
		matches, _ = filepath.Glob(filepath.Join(dir, "*Docker*.exe"))
	}
	return pickNewestInstaller(matches)
}

// pickNewestInstaller 在候选里挑修改时间最新的（目录与无效项跳过），便于单测
func pickNewestInstaller(matches []string) (string, int64) {
	type ent struct {
		path string
		mod  time.Time
		size int64
	}
	var list []ent
	for _, m := range matches {
		st, err := os.Stat(m)
		if err != nil || st.IsDir() {
			continue
		}
		list = append(list, ent{m, st.ModTime(), st.Size()})
	}
	if len(list) == 0 {
		return "", 0
	}
	sort.Slice(list, func(i, j int) bool { return list[i].mod.After(list[j].mod) })
	return list[0].path, list[0].size
}

/* ============================ 下载 ============================ */

// ProbeInstaller 下载前先探一次：确认直链有效并拿到真实体积与文件名。
// 404/403 或体积过小（把 HTML 错误页当安装包）都在这里拦下。
func ProbeInstaller(input string) (ProbeInfo, error) {
	u := BuildInstallerURL(input)
	if !hasHTTPScheme(u) {
		return ProbeInfo{}, fmt.Errorf("非法下载地址: %s", u)
	}
	info := ProbeInfo{Url: u, Name: installerNameFromURL(u), Size: -1}
	client := &http.Client{Timeout: 2 * time.Minute}
	req, err := http.NewRequest(http.MethodHead, u, nil)
	if err != nil {
		return ProbeInfo{}, err
	}
	req.Header.Set("User-Agent", browserUA)
	resp, err := client.Do(req)
	if err != nil {
		return ProbeInfo{}, fmt.Errorf("探测安装包失败: %v", err)
	}
	defer resp.Body.Close()
	size := resp.ContentLength
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotImplemented {
		// 有些 CDN 不支持 HEAD：退化为 Range GET 探总长度
		size, err = probeByRange(client, u)
		if err != nil {
			return ProbeInfo{}, err
		}
	} else if resp.StatusCode != http.StatusOK {
		return ProbeInfo{}, fmt.Errorf("安装包地址不可用（HTTP %d），请改填 build 号或完整直链", resp.StatusCode)
	}
	if size >= 0 && size < 50*1024*1024 {
		return ProbeInfo{}, fmt.Errorf("返回体积仅 %s，不像安装包（应为数百 MB）；请检查地址或改用 build 号直链", humanSize(size))
	}
	info.Size = size
	return info, nil
}

func probeByRange(client *http.Client, u string) (int64, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", browserUA)
	req.Header.Set("Range", "bytes=0-0")
	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("探测安装包失败: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent && resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("安装包地址不可用（HTTP %d）", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusPartialContent {
		if i := strings.LastIndex(resp.Header.Get("Content-Range"), "/"); i >= 0 {
			var total int64
			if _, err := fmt.Sscanf(strings.TrimSpace(resp.Header.Get("Content-Range")[i+1:]), "%d", &total); err == nil {
				return total, nil
			}
		}
	}
	return resp.ContentLength, nil
}

func installerNameFromURL(u string) string {
	base := u
	if i := strings.LastIndex(u, "/"); i >= 0 {
		base = u[i+1:]
	}
	base = strings.ReplaceAll(base, "%20", " ")
	if base == "" || !strings.HasSuffix(strings.ToLower(base), ".exe") {
		return "Docker Desktop Installer.exe"
	}
	return base
}

// DownloadInstaller 下载安装包：已有完整文件直接复用，半截文件走 Range 续传，
// 用户主动取消则删掉半截文件不留垃圾。dir 空 = 默认 <插件目录>\docker。
func DownloadInstaller(input, dir string) (string, error) {
	info, err := ProbeInstaller(input)
	if err != nil {
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return "", err
	}
	total, name := info.Size, info.Name
	destDir, err := installerDir(dir)
	if err != nil {
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return "", err
	}
	dest := filepath.Join(destDir, name)
	if total > 0 {
		if st, err := os.Stat(dest); err == nil && st.Size() == total {
			emitSetup(SetupProgress{Stage: "info", Percent: 100, Note: "安装包已存在，跳过下载: " + dest})
			return dest, nil
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); unregisterCancel() }()
	registerCancel(cancel)

	emitSetup(SetupProgress{Stage: "info", Note: fmt.Sprintf("开始下载 %s（约 %s）到 %s", name, humanSize(total), destDir)})
	// 下载交给 tools/downloader 引擎：天然进「下载管理」页，可暂停 / 续传 / 限速 / 排队；
	// 进度经 onProgress 桥接回 docker:setup 事件，面板零改动照常显示百分比。
	// ctx 取消 = 停止该引擎任务（AbortSetup 走的就是这个 cancel）。
	savePath, _, ferr := downloader.Fetch(ctx, info.Url, downloader.FetchOpts{
		Name:     name,
		SaveDir:  destDir,
		Category: "安装",
	}, func(pct int, downloaded, size int64, _ float64) {
		emitSetup(SetupProgress{Stage: "downloading", Percent: pct, Received: downloaded, Total: size,
			Note: fmt.Sprintf("%s / %s", humanSize(downloaded), humanSize(size))})
	})
	if ferr != nil {
		if ctx.Err() == context.Canceled {
			emitSetup(SetupProgress{Stage: "error", Note: "下载已取消"})
			return "", fmt.Errorf("下载已取消")
		}
		emitSetup(SetupProgress{Stage: "error", Note: "下载失败: " + ferr.Error()})
		return "", ferr
	}
	emitSetup(SetupProgress{Stage: "info", Percent: 100, Note: "安装包就绪: " + savePath})
	return savePath, nil
}

/* ============================ 安装 ============================ */

// InstallArgs 拼装安装器参数。
func InstallArgs(webDownload bool) string {
	args := []string{"install", "--quiet", "--accept-license"}
	if webDownload {
		args = append(args, "--web-download")
	}
	return strings.Join(args, " ")
}

// InstallDocker 提权静默安装 → 以普通用户身份启动 Docker Desktop → 等引擎可达。
// installerPath 为空时自动按 dir 目录（空=默认插件目录/docker）找离线包，没有则先按 input 下载。
func InstallDocker(installerPath, input, dir string, webDownload bool) error {
	params := InstallArgs(webDownload)
	path := strings.TrimSpace(installerPath)
	if path == "" {
		if local, _ := findLocalInstaller(dir); local != "" {
			emitSetup(SetupProgress{Stage: "info", Note: "使用安装目录中的离线安装包: " + local})
			path = local
		} else {
			p, err := DownloadInstaller(input, dir)
			if err != nil {
				return err
			}
			path = p
		}
	}
	if !fileExists(path) {
		err := fmt.Errorf("安装包不存在: %s", path)
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}

	emitSetup(SetupProgress{Stage: "installing", Note: "安装器已请求管理员权限，请在 UAC 与安装器窗口中确认…"})
	code, err := winrun.RunElevatedWait(winrun.Elevated{
		File:    path,
		Params:  params,
		Show:    windows.SW_SHOW,
		Timeout: installTimeout,
	})
	if err != nil {
		if winrun.IsUACCancelled(err) {
			emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
			return err
		}
		note := fmt.Sprintf("安装未完成: %v（退出码 %d）", err, code)
		emitSetup(SetupProgress{Stage: "error", Note: note})
		return fmt.Errorf("%s", note)
	}
	// 1641 / 3010 是 MSI「成功但需重启」退出码：文件落盘推迟到重启之后。
	// 不算失败，但也无法立刻拉起引擎，给出明确的重启指引即可。
	if code == 1641 || code == 3010 {
		note := fmt.Sprintf("Docker Desktop 安装成功（退出码 %d）：系统需要重启才能完成文件落盘。请重启计算机后重新进入本页面自动识别，无需重装。", code)
		emitSetup(SetupProgress{Stage: "error", Note: note})
		return fmt.Errorf("%s", note)
	}
	if code != 0 {
		note := fmt.Sprintf("安装器退出码 %d，未确认安装成功；请查看安装器窗口或官方日志 %%LOCALAPPDATA%%\\Docker\\log\\installer.log", code)
		emitSetup(SetupProgress{Stage: "error", Note: note})
		return fmt.Errorf("%s", note)
	}
	// 安装器返回 0 不等于文件已就位：MSI 在进程退出后可能仍在写盘/挂起重命名
	// （偶尔推迟到重启后完成）。轮询等待 exe 出现，最多 90 秒。
	emitSetup(SetupProgress{Stage: "installing", Note: "安装器报告完成，正在等待程序文件落盘…"})
	exe, ok := waitDesktopExe(90 * time.Second)
	if !ok {
		note := desktopMissingNote()
		emitSetup(SetupProgress{Stage: "error", Note: note})
		return fmt.Errorf("%s", note)
	}

	emitSetup(SetupProgress{Stage: "starting", Note: "安装完成，正在启动 Docker Desktop 并等待引擎就绪…"})
	if err := startDesktop(exe); err != nil {
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}
	return waitEngineReady()
}

// waitDesktopExe 轮询 locateDesktopExe 直到出现或超时：覆盖「安装器已退出但文件仍在写入」的窗口期
func waitDesktopExe(timeout time.Duration) (string, bool) {
	deadline := time.Now().Add(timeout)
	for {
		if p, ok := locateDesktopExe(); ok {
			return p, true
		}
		if !time.Now().Before(deadline) {
			return "", false
		}
		time.Sleep(3 * time.Second)
	}
}

// desktopMissingNote 安装器报成功但找不到 exe 时，按「注册表登记 + 挂起重启」两项证据
// 组装方向性指引：已登记且待重启时提示重启后重检（无需重装），只有全无痕迹才是真失败。
func desktopMissingNote() string {
	const instLog = `%LOCALAPPDATA%\Docker\log\installer.log`
	info, registered := findUninstallEntry()
	reboot := pendingReboot()
	ver := info.displayVersion
	if ver == "" {
		ver = "版本未知"
	}
	switch {
	case registered && reboot:
		return "Docker Desktop（" + ver + "）已登记安装，但程序文件尚未落盘，且系统有待重启操作——这是 MSI 安装器常见的尾声。请重启计算机后重新进入本页面自动识别，无需重装。"
	case registered:
		return "Docker Desktop（" + ver + "）已在系统登记，但安装目录与注册表路径都找不到程序文件。若刚完成安装，可先重启后重检；否则请查看安装器日志 " + instLog + " 后重装。"
	case reboot:
		return "安装器正常退出，但系统尚未登记 Docker Desktop，且存在挂起的重启操作：请先重启计算机后重新检测；若仍未找到，再重新发起安装。"
	default:
		return "安装器报告完成，但常见安装目录与注册表均未发现 Docker Desktop 痕迹，安装可能被静默中止——请查看安装器日志 " + instLog + " 后重新安装。"
	}
}

// waitEngineReady 轮询引擎直至可达、被取消或超时；超时不报错回滚，只给排查方向
func waitEngineReady() error {
	startCancelled.Store(false)
	deadline := time.Now().Add(engineWaitTime)
	for i := 0; time.Now().Before(deadline); i++ {
		time.Sleep(3 * time.Second)
		if startCancelled.Load() {
			emitSetup(SetupProgress{Stage: "error", Note: "启动已取消"})
			return fmt.Errorf("启动已取消")
		}
		eng, _ := GetEngineStatus()
		if eng.Available {
			emitSetup(SetupProgress{Stage: "done", Percent: 100,
				Note: "Docker 引擎已就绪（" + eng.Version + "），可以开始管理容器"})
			return nil
		}
		if i%5 == 4 {
			emitSetup(SetupProgress{Stage: "starting",
				Note: "引擎仍未应答，首次启动需初始化 WSL 后端，请稍候…（可点「停止」中止）"})
		}
		// 等了几轮仍无应答，且探到「虚拟机平台未启用」：这不是“再等等就好”，而是必须
		// 启用系统功能并重启才生效的硬卡点——尽早中止，别把整段 engineWaitTime 白等掉。
		if i >= 3 {
			if blocked, ev := vmPlatformBlocked(); blocked {
				note := "检测到「虚拟机平台 (Virtual Machine Platform)」未启用：Docker Desktop 的 WSL2 后端依赖它，不启用引擎起不来。\n" +
					"请点本面板的「启用 WSL2」（自动提权启用），或在管理员终端执行：\n" +
					"dism /online /enable-feature /featurename:VirtualMachinePlatform /all /norestart\n" +
					"启用后必须【重启计算机】才会生效（只重启 Docker Desktop 不够）；重启后重新打开 Docker Desktop 即可。证据：" + ev
				emitSetup(SetupProgress{Stage: "error", Note: note})
				return fmt.Errorf("%s", note)
			}
		}
	}
	note := "引擎在 " + engineWaitTime.String() + " 内未就绪：若刚启用 WSL2 请先重启计算机，再手动打开 Docker Desktop 等待右下角鲸鱼变绿"
	emitSetup(SetupProgress{Stage: "error", Note: note})
	return fmt.Errorf("%s", note)
}

// startCancelled 标记“启动等待已被用户叫停”：每轮轮询都看它，不靠 context 传递
var startCancelled atomic.Bool

// CancelStart 中止正在进行的启动等待（面板点「停止」时先叫停等待，再去停进程）
func CancelStart() { startCancelled.Store(true) }

/* ============================ 启动 ============================ */

// startDesktop 以普通用户身份拉起 Docker Desktop。
// 提权安装结束后若由管理员进程派生，容器/卷归属会错位，故显式走非提权路径。
func startDesktop(exe string) error {
	if err := winrun.StartDetached(exe); err != nil {
		return fmt.Errorf("已安装但自动启动失败: %v，请手动从开始菜单打开 Docker Desktop", err)
	}
	return nil
}

// StartEngine 已安装但引擎没跑时，拉起 Docker Desktop 并等待就绪
func StartEngine() error {
	exe, ok := locateDesktopExe()
	if !ok {
		err := fmt.Errorf("未找到 Docker Desktop.exe，请先完成安装")
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}
	emitSetup(SetupProgress{Stage: "starting", Note: "正在启动 Docker Desktop…"})
	if err := startDesktop(exe); err != nil {
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}
	return waitEngineReady()
}

/* ============================ WSL2 ============================ */

// runWsl 统一跑 wsl.exe：带 WSL_UTF8=1。中文系统默认按 UTF-16 输出，而 UTF-16 的
// 汉字里没有 NUL，winrun 的密度启发式认不出来，判词表会全部空转
// （本机实测：WSL 根本没装的机器上 wsl --status 只回“默认版本: 2”，
// 而 wsl -l -v 那句“未安装…”不转 UTF-8 就匹不中）。
func runWsl(timeout time.Duration, args ...string) (string, error) {
	return winrun.RunHiddenEnv(timeout, []string{"WSL_UTF8=1"}, "wsl.exe", args...)
}

// wslState WSL2 只读探测的全部事实：组件能不能用、有哪些发行版、docker-desktop 在不在跑
type wslState struct {
	Ready   bool
	Distros []wslDistro
	Docker  string // docker-desktop 的状态：running | stopped | ""（没这个发行版）
	Note    string
}

// distroLabels 给人看的发行版清单："docker-desktop(running)、Ubuntu(stopped)"
func (s wslState) distroLabels() []string {
	out := make([]string, 0, len(s.Distros))
	for _, d := range s.Distros {
		out = append(out, d.Name+"("+d.State+")")
	}
	return out
}

// probeWsl 只读判 WSL2 到底能不能用，两个来源缺一不可：
//   - wsl --status：组件与默认版本（WSL 未真装时它也能正常返回，看不出问题）
//   - wsl -l -v：有没有已注册的发行版（组件缺失时它直说“未安装适用于 Linux 的 Windows 子系统”）
//
// 注意：没有发行版不代表 WSL2 不能用——切到 WSL2 后由 Docker Desktop 自建 docker-desktop。
// 真正用不了时 Ready=false，此时 Docker Desktop 不报错，只是沿用 Hyper-V。
func probeWsl() wslState {
	status, err := runWsl(10*time.Second, "--status")
	st := oneLine(truncate(status, 200))
	if err != nil {
		return wslState{Note: oneLine(truncate(status+" "+err.Error(), 300))}
	}
	if wslBroken(status) {
		return wslState{Note: st}
	}
	list, errL := runWsl(15*time.Second, "-l", "-v")
	ls := oneLine(truncate(list, 240))
	switch {
	case wslBroken(list):
		return wslState{Note: st + "；但 wsl -l -v 说：" + ls + "（WSL 组件不可用，Docker Desktop 会沿用 Hyper-V）"}
	case wslNoDistro(list), errL != nil && strings.TrimSpace(list) == "":
		return wslState{Ready: true,
			Note: st + "；暂无已注册发行版（" + ls + "）：这不影响 WSL2 后端，切过去时由 Docker Desktop 自建 docker-desktop"}
	}
	distros := parseWslDistros(list)
	s := wslState{Ready: true, Distros: distros}
	for _, d := range distros {
		if strings.EqualFold(d.Name, "docker-desktop") {
			s.Docker = d.State
		}
	}
	if len(distros) == 0 {
		s.Note = st + "；发行版清单认不出（原文：" + ls + "）"
		return s
	}
	s.Note = fmt.Sprintf("%s；发行版 %d 个：%s", st, len(distros), strings.Join(s.distroLabels(), "、"))
	return s
}

// wslProbe 兼容旧调用点：只关心“能不能用”与一句说明
func wslProbe() (bool, string) {
	s := probeWsl()
	return s.Ready, s.Note
}

// wslDockerDistroRunning docker-desktop 发行版是否在跑（多一次 wsl.exe 调用，只在切换核验时用）
func wslDockerDistroRunning() bool {
	out, _ := runWsl(15*time.Second, "-l", "-v")
	return wslDistroRunning(out, "docker-desktop")
}

// UpdateWsl 提权执行 wsl --update：WSL 组件在但内核过旧/损坏时，
// Docker Desktop 切 WSL2 会静默回落 Hyper-V，这里给面板一个修复入口。
func UpdateWsl() error {
	ready, st := wslProbe()
	emitSetup(SetupProgress{Stage: "info",
		Note: "即将提权更新 WSL2（wsl --update），当前状态：" + orUnknown(st)})
	code, err := winrun.RunElevatedWait(winrun.Elevated{
		File:    "wsl.exe",
		Params:  "--update",
		Show:    windows.SW_SHOW,
		Timeout: wslInstallTO,
	})
	if err != nil {
		if winrun.IsUACCancelled(err) {
			emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
			return err
		}
		note := fmt.Sprintf("更新 WSL2 失败: %v（退出码 %d）。也可在管理员 PowerShell 里手动执行 wsl --update", err, code)
		emitSetup(SetupProgress{Stage: "error", Note: note})
		return fmt.Errorf("%s", note)
	}
	if code != 0 {
		note := fmt.Sprintf("wsl --update 退出码 %d，未确认更新成功；若提示未找到更新，说明 WSL 可能由系统组件提供，请改用「启用 WSL2」或重启后重试", code)
		emitSetup(SetupProgress{Stage: "error", Note: note})
		return fmt.Errorf("%s", note)
	}
	_, after := wslProbe()
	note := "WSL2 更新已提交，更新后状态：" + orUnknown(after)
	if ready {
		emitSetup(SetupProgress{Stage: "done", Percent: 100, Note: note + "；回面板点「启动引擎」或重新切换后端即可生效"})
	} else {
		emitSetup(SetupProgress{Stage: "done", Percent: 100, Note: note + "；若仍未就绪，请重启计算机后再切 WSL2 后端"})
	}
	return nil
}

// EnableWsl2 提权执行 wsl --install --no-distribution（仅装 WSL 组件，不装发行版）。
// 该动作可能要求重启才生效，返回值里会把这一提示带回去，由前端明示用户。
func EnableWsl2() error {
	emitSetup(SetupProgress{Stage: "info", Note: "即将提权启用 WSL2（wsl --install --no-distribution），可能需要重启…"})
	code, err := winrun.RunElevatedWait(winrun.Elevated{
		File:    "wsl.exe",
		Params:  "--install --no-distribution",
		Show:    windows.SW_SHOW,
		Timeout: wslInstallTO,
	})
	if err != nil {
		if winrun.IsUACCancelled(err) {
			emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
			return err
		}
		note := fmt.Sprintf("启用 WSL2 失败: %v（退出码 %d）。老版本 Windows 可手动执行：\ndism /online /enable-feature /featurename:Microsoft-Windows-Subsystem-Linux /all /norestart\ndism /online /enable-feature /featurename:VirtualMachinePlatform /all /norestart", err, code)
		emitSetup(SetupProgress{Stage: "error", Note: note})
		return fmt.Errorf("%s", note)
	}
	if code != 0 {
		note := fmt.Sprintf("WSL 安装命令退出码 %d；请重启后在管理员终端执行 wsl --update 再检查状态", code)
		emitSetup(SetupProgress{Stage: "error", Note: note})
		return fmt.Errorf("%s", note)
	}
	c, _ := CheckEnv("") // 只看系统组件状态，与包存放目录无关
	note := "WSL2 组件已提交安装："
	if c.WslInstalled {
		note += "当前状态已就绪，可继续安装 Docker Desktop"
	} else {
		note += "通常需要重启计算机才生效，重启后再回到本面板继续"
	}
	emitSetup(SetupProgress{Stage: "done", Percent: 100, Note: note})
	return nil
}
