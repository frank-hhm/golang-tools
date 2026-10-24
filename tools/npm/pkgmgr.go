package npm

// ============================================================
// 包管理器探测与全局包扫描
//
// 原实现只有一条路：`npm ls -g`。问题是
//   - 换一台 Node 就得重跑，yarn / pnpm 装的全局包完全看不见；
//   - npm 的 CLI 一坏（prefix 被改、缓存损坏、Node 目录只读）整个面板就空；
//   - 每次都要起进程，慢。
//
// 这里拆成两步：
//   1) ListPackageManagers  探测本机可用的包管理器：随 Node 发行的 npm /
//      corepack、以全局包形式安装的 yarn / pnpm / cnpm、原生安装的 bun；
//      同时解析出各自的「全局安装根」——优先读磁盘上的约定路径，
//      并尊重 ~/.npmrc 的 prefix 与 NPM_CONFIG_PREFIX / PNPM_HOME / BUN_INSTALL。
//   2) ScanGlobalPackages   直接扫这些根目录里的 package.json 汇总，
//      不起任何子进程，npm CLI 坏了也能列出包。
//
// 安装 / 卸载仍走对应管理器的 CLI：各家语法不同（yarn 是 global add、
// pnpm 是 add -g），见 managerArgs；入口统一从包的 package.json bin 字段
// 解析出 JS 文件再用 node 执行，绕开 .cmd / .ps1 转发壳对 shell 的依赖。
// ============================================================

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// PackageManager 探测到的一个包管理器
type PackageManager struct {
	ID      string   `json:"id"`      // 前端选择器用的稳定标识（随 Node 发行的带 @NodeVer 以区分多份）
	Name    string   `json:"name"`    // npm | yarn | pnpm | cnpm | bun | corepack
	Version string   `json:"version"` // 管理器自身版本
	Path    string   `json:"path"`    // 调用入口：JS 脚本，或原生可执行文件
	NodeExe string   `json:"nodeExe"` // 运行该入口用的 node（原生管理器为空）
	Kind    string   `json:"kind"`    // node（随 Node 发行）| global（全局装的包）| native（原生二进制）
	NodeVer string   `json:"nodeVer"` // Kind=node 时所属的 Node 版本
	Roots   []string `json:"roots"`   // 全局安装根（扫描结果来源）
}

// pkgDirInfo 一个包目录里关心的元数据
type pkgDirInfo struct {
	Name        string
	Version     string
	Description string
	Entry       string // bin 字段解析出的 JS 入口（无则空）
	HasBin      bool
}

// 支持探测的管理器；npm / corepack 随 Node 发行，单独处理
var otherManagers = []string{"yarn", "pnpm", "cnpm", "bun"}

/* ============================ 管理器探测 ============================ */

// ListPackageManagers 枚举本机可用的包管理器，按名称排序
func ListPackageManagers() ([]PackageManager, error) {
	var out []PackageManager
	seen := map[string]bool{}
	add := func(pm PackageManager) {
		if pm.Name == "" || pm.Path == "" {
			return
		}
		k := pm.Name + "|" + pathKey(pm.Path)
		if seen[k] {
			return
		}
		seen[k] = true
		// 同名的多份 npm（每个 Node 一份）靠 @NodeVer 区分，其余管理器直接用名字
		if pm.Kind == "node" && pm.NodeVer != "" {
			pm.ID = pm.Name + "@" + pm.NodeVer
		} else {
			pm.ID = pm.Name
		}
		out = append(out, pm)
	}

	// 1) 随各 Node 版本发行的 npm / corepack
	vers, _ := ListVersions()
	node := defaultNodeExe()
	for _, v := range vers {
		dir := filepath.Dir(v.Path)
		for _, name := range []string{"npm", "corepack"} {
			var info pkgDirInfo
			var ok bool
			if name == "npm" {
				// 和 cmd 保持同一份：prefix 覆盖层存在时报那一个，否则面板与 npm -v 又对不上
				info, ok = npmEntryFor(v.Path)
			} else {
				info, ok = readPkgDir(filepath.Join(dir, "node_modules", name))
			}
			if !ok || info.Entry == "" {
				continue
			}
			add(PackageManager{
				Name: name, Version: info.Version, Path: info.Entry,
				NodeExe: v.Path, Kind: "node", NodeVer: v.Version,
				Roots: globalRootsFor(name, dir),
			})
		}
	}

	// 2) 全局 bin 目录里以包形式安装的管理器（yarn / pnpm / cnpm）与原生 bun
	for _, dir := range globalBinDirs() {
		for _, name := range otherManagers {
			if info, ok := readPkgDir(filepath.Join(dir, "node_modules", name)); ok && info.Entry != "" {
				add(PackageManager{
					Name: name, Version: info.Version, Path: info.Entry,
					NodeExe: node, Kind: "global",
					Roots: globalRootsFor(name, dir),
				})
				continue
			}
			if exe := nativeShim(dir, name); exe != "" {
				add(PackageManager{
					Name: name, Version: probeManagerVersion(exe), Path: exe,
					Kind: "native", Roots: globalRootsFor(name, dir),
				})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return compareVersion(out[i].Version, out[j].Version) > 0
	})
	return out, nil
}

// defaultNodeExe 执行管理器入口时用的 node：PATH 优先，其次本机最高版本
func defaultNodeExe() string {
	if p, err := exec.LookPath("node"); err == nil {
		return p
	}
	if vers, err := ListVersions(); err == nil && len(vers) > 0 {
		return vers[0].Path // ListVersions 已按版本降序
	}
	return ""
}

// globalBinDirs 可能存放全局 CLI 转发壳的目录：PATH 全量 + 各家管理器自己的 bin
func globalBinDirs() []string {
	dirs := filepath.SplitList(os.Getenv("PATH"))
	appData := os.Getenv("APPDATA")
	local := os.Getenv("LOCALAPPDATA")
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		dirs = append(dirs, appData+"\\npm", local+"\\Yarn\\bin", local+"\\pnpm")
	} else {
		dirs = append(dirs,
			filepath.Join(appData, "npm"),
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, ".bun", "bin"),
			filepath.Join(home, ".local", "share", "pnpm"),
			filepath.Join(local, "pnpm"),
		)
	}
	if home != "" {
		dirs = append(dirs, filepath.Join(home, ".bun", "bin"))
	}
	return dedupeDirs(dirs)
}

// nativeShim 目录下的原生可执行入口（bun 这类不走 node 的管理器）
func nativeShim(dir, name string) string {
	if runtime.GOOS == "windows" {
		p := filepath.Join(dir, name+".exe")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
		return ""
	}
	p := filepath.Join(dir, name)
	if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
		return p
	}
	return ""
}

// probeManagerVersion 执行 <mgr> --version 取版本号，失败返回空
func probeManagerVersion(exe string) string {
	cmd := exec.Command(exe, "--version")
	cmd.SysProcAttr = hideWindowAttr()
	data, err := cmd.Output()
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(data))
	s = strings.TrimPrefix(strings.SplitN(s, "\n", 2)[0], "v")
	if versionRe.MatchString(s) {
		return s
	}
	if m := verExtractRe.FindString(s); m != "" {
		return normalizeVersionName(m)
	}
	return ""
}

// readPkgDir 读取一个包目录的 package.json
func readPkgDir(dir string) (pkgDirInfo, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return pkgDirInfo{}, false
	}
	var p struct {
		Name        string          `json:"name"`
		Version     string          `json:"version"`
		Description string          `json:"description"`
		Bin         json.RawMessage `json:"bin"`
	}
	if json.Unmarshal(data, &p) != nil || p.Version == "" {
		return pkgDirInfo{}, false
	}
	name := p.Name
	if name == "" {
		name = filepath.Base(dir)
	}
	entry, hasBin := resolveBinEntry(p.Bin, name, dir)
	return pkgDirInfo{Name: name, Version: p.Version, Description: p.Description, Entry: entry, HasBin: hasBin}, true
}

// resolveBinEntry 从 package.json 的 bin 字段解析出 JS 入口。
// bin 可能是字符串（单入口）或对象（多入口，取与包名同名者，否则取第一个 JS）。
// hasBin 表示该包提供了命令行入口（即使入口不是 .js，如 bun 的 .exe）。
func resolveBinEntry(raw json.RawMessage, name, dir string) (entry string, hasBin bool) {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == `""` {
		return "", false
	}
	binName := name
	if i := strings.LastIndex(binName, "/"); i >= 0 {
		binName = binName[i+1:] // @scope/cli -> cli
	}
	var targets []string
	var one string
	if json.Unmarshal(raw, &one) == nil && one != "" {
		targets = []string{one}
	} else {
		var m map[string]string
		if json.Unmarshal(raw, &m) != nil {
			return "", false
		}
		if v, ok := m[binName]; ok {
			targets = append(targets, v)
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			targets = append(targets, m[k])
		}
		hasBin = len(m) > 0
	}
	for _, t := range targets {
		if t == "" {
			continue
		}
		p := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(t, "./")))
		if !isJS(p) {
			continue
		}
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, true
		}
	}
	return "", hasBin || len(targets) > 0
}

func isJS(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".js", ".cjs", ".mjs":
		return true
	}
	return false
}

/* ============================ 全局安装根 ============================ */

// globalRootsFor 某管理器的全局安装根候选（只返回真实存在的目录）
func globalRootsFor(name, contextDir string) []string {
	var roots []string
	home, _ := os.UserHomeDir()
	appData := os.Getenv("APPDATA")
	local := os.Getenv("LOCALAPPDATA")
	win := runtime.GOOS == "windows"

	switch name {
	case "npm", "cnpm":
		// npm 的 prefix 可被 ~/.npmrc 或环境变量改写，优先级高于默认位置
		for _, prefix := range []string{npmPrefixFromUserNpmrc(), os.Getenv("NPM_CONFIG_PREFIX")} {
			if prefix != "" {
				roots = append(roots, filepath.Join(prefix, "node_modules"))
			}
		}
		if win {
			roots = append(roots, filepath.Join(appData, "npm", "node_modules"))
			roots = append(roots, filepath.Join(contextDir, "node_modules"))
		} else {
			roots = append(roots, filepath.Join(contextDir, "lib", "node_modules"))
			if home != "" {
				roots = append(roots, filepath.Join(home, ".npm-global", "lib", "node_modules"))
			}
		}
	case "yarn":
		if win {
			roots = append(roots, filepath.Join(local, "Yarn", ".config", "yarn", "config", "global", "node_modules"))
		} else if home != "" {
			roots = append(roots, filepath.Join(home, ".config", "yarn", "global", "node_modules"))
		}
	case "pnpm":
		if v := os.Getenv("PNPM_HOME"); v != "" {
			roots = append(roots, filepath.Join(v, "node_modules"), filepath.Join(v, "global", "v5", "node_modules"))
		}
		if win {
			roots = append(roots,
				filepath.Join(local, "pnpm", "node_modules"),
				filepath.Join(local, "pnpm", "global", "v5", "node_modules"),
			)
		} else if home != "" {
			roots = append(roots,
				filepath.Join(home, ".local", "share", "pnpm", "global", "v5", "node_modules"),
				filepath.Join(home, ".local", "share", "pnpm", "global", "5", "node_modules"),
				filepath.Join(home, ".pnpm", "global", "v5", "node_modules"),
			)
		}
	case "bun":
		base := os.Getenv("BUN_INSTALL")
		if base == "" && home != "" {
			base = filepath.Join(home, ".bun")
		}
		if base != "" {
			roots = append(roots, filepath.Join(base, "install", "global", "node_modules"))
		}
	}
	return existing(dedupeDirs(roots)...)
}

// npmPrefixFromUserNpmrc 从用户 ~/.npmrc 读 prefix（只读，不改写用户配置）
func npmPrefixFromUserNpmrc() string {
	path := userNpmrcPath()
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, ";") || strings.HasPrefix(t, "#") {
			continue
		}
		i := strings.Index(t, "=")
		if i <= 0 || strings.TrimSpace(t[:i]) != "prefix" {
			continue
		}
		v := strings.Trim(strings.TrimSpace(t[i+1:]), `"'`)
		if v == "" {
			return ""
		}
		// npm 允许 ~ 表示主目录
		if strings.HasPrefix(v, "~/") || strings.HasPrefix(v, "~\\") {
			if home, err := os.UserHomeDir(); err == nil {
				v = filepath.Join(home, v[1:])
			}
		}
		return v
	}
	return ""
}

/* ============================ 全局包扫描 ============================ */

// ScanGlobalPackages 扫描各管理器全局安装根下的包，不起子进程。
// 结果按 管理器 -> 包名 排序；同名的 npm 发行版会带 Builtin 标记，前端据此禁用卸载。
func ScanGlobalPackages() ([]NpmPackage, error) {
	mgrs, err := ListPackageManagers()
	if err != nil {
		return nil, err
	}
	// npm 的全局根也是 cnpm / corepack 的根，按地盘优先级扫描才能正确归属（先到先得）
	priority := map[string]int{"npm": 0, "cnpm": 1, "yarn": 2, "pnpm": 3, "bun": 4}
	sort.SliceStable(mgrs, func(i, j int) bool {
		return priority[strings.ToLower(mgrs[i].Name)] < priority[strings.ToLower(mgrs[j].Name)]
	})
	seen := map[string]bool{}
	var out []NpmPackage
	for _, m := range mgrs {
		for _, root := range m.Roots {
			for _, p := range scanRoot(root, m.Name) {
				k := p.Name + "|" + pathKey(p.Path)
				if seen[k] {
					continue
				}
				seen[k] = true
				out = append(out, p)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Manager != out[j].Manager {
			return out[i].Manager < out[j].Manager
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}

// scanRoot 读一个全局安装根：普通包一层目录，@scope 包再下一层
func scanRoot(root, manager string) []NpmPackage {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []NpmPackage
	collect := func(dir string) {
		info, ok := readPkgDir(dir)
		if !ok {
			return
		}
		out = append(out, NpmPackage{
			Name: info.Name, Version: info.Version, Description: info.Description,
			Path: dir, Manager: manager, HasBin: info.HasBin,
			// npm / corepack 是 Node 发行件，跟着 Node 走，不允许从这里卸载
			Builtin: info.Name == "npm" || info.Name == "corepack",
		})
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if strings.HasPrefix(e.Name(), "@") {
			subs, err := os.ReadDir(filepath.Join(root, e.Name()))
			if err != nil {
				continue
			}
			for _, s := range subs {
				if s.IsDir() {
					collect(filepath.Join(root, e.Name(), s.Name()))
				}
			}
			continue
		}
		collect(filepath.Join(root, e.Name()))
	}
	return out
}

/* ============================ 管理器调用 ============================ */

// resolveManager 按 ID（npm / npm@22.14.0 / yarn …）定位包管理器；
// 同名多份且未指定 Node 版本时取版本最高的
func resolveManager(id string) (PackageManager, error) {
	if id == "" {
		id = "npm"
	}
	name, nodeVer, _ := strings.Cut(id, "@")
	mgrs, err := ListPackageManagers()
	if err != nil {
		return PackageManager{}, err
	}
	for _, m := range mgrs {
		if !strings.EqualFold(m.Name, name) {
			continue
		}
		if nodeVer != "" && m.NodeVer != nodeVer {
			continue
		}
		return m, nil // ListPackageManagers 已按名称分组、版本降序
	}
	return PackageManager{}, fmt.Errorf("未找到包管理器 %s，可在「Node.js 版本管理」里先扫描本机 Node", id)
}

// resolveManagerForPkg 按包的实际安装目录反查装它的管理器（哪个 Node 自带的 npm）；
// 反查不到（包已被手删、或目录不在已知安装根下）时回退到按 ID 解析。
func resolveManagerForPkg(id, pkgDir string) (PackageManager, error) {
	if pkgDir != "" {
		if mgrs, err := ListPackageManagers(); err == nil {
			for _, m := range mgrs {
				for _, r := range m.Roots {
					if withinDir(pkgDir, r) {
						return m, nil
					}
				}
			}
		}
	}
	return resolveManager(id)
}

// npmManagerForVersion 取指定 Node 版本实际会用的 npm（兼容旧的全局安装接口）
func npmManagerForVersion(version string) (PackageManager, error) {
	exe := nodeExe(version)
	if exe == "" {
		return PackageManager{}, fmt.Errorf("Node %s 未安装", version)
	}
	dir := filepath.Dir(exe)
	info, ok := npmEntryFor(exe)
	if !ok || info.Entry == "" {
		return PackageManager{}, fmt.Errorf("未找到可用的 npm 入口（%s），该 Node 目录可能不含 npm", filepath.Join(dir, "node_modules", "npm"))
	}
	return PackageManager{
		Name: "npm", Version: info.Version, Path: info.Entry, NodeExe: exe,
		Kind: "node", NodeVer: version, Roots: globalRootsFor("npm", dir),
	}, nil
}

// managerArgs 组装安装 / 卸载参数。各家语法不同：yarn 是 global add，pnpm / bun 是 add -g。
func managerArgs(m PackageManager, action, pkg string) []string {
	switch strings.ToLower(m.Name) {
	case "yarn":
		if action == "install" {
			return []string{"global", "add", pkg}
		}
		return []string{"global", "remove", pkg}
	case "pnpm", "cnpm", "bun":
		if action == "install" {
			return []string{"add", "-g", pkg}
		}
		return []string{"remove", "-g", pkg}
	default: // npm
		if action == "install" {
			return []string{"install", "-g", pkg}
		}
		return []string{"uninstall", "-g", pkg}
	}
}

// managerExtraArgs 仅 npm 支持 --userconfig；其余管理器靠环境变量传递 registry
func managerExtraArgs(m PackageManager) []string {
	if !strings.EqualFold(m.Name, "npm") {
		return nil
	}
	if p, err := managedNpmrcPath(); err == nil {
		return []string{"--userconfig", p}
	}
	return nil
}

// checkPkgName 包名基本校验：参数虽不经 shell，但拦住以 - 开头的输入，
// 避免 `--prefix=c:\...` 这类被当成 CLI 选项改写安装位置。
func checkPkgName(s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Errorf("包名不能为空")
	}
	if strings.HasPrefix(s, "-") {
		return fmt.Errorf("包名不能以 - 开头")
	}
	if strings.ContainsAny(s, " \t\r\n\"'&|;<>^") {
		return fmt.Errorf("包名含空白或 shell 元字符")
	}
	return nil
}

// runManager 用指定管理器执行一次命令，返回合并的 stdout/stderr
func runManager(m PackageManager, args []string, timeout time.Duration) (string, error) {
	exe, full := m.Path, append(args, managerExtraArgs(m)...)
	var pathDirs []string
	if isJS(m.Path) {
		node := m.NodeExe
		if node == "" {
			node = defaultNodeExe()
		}
		if node == "" {
			return "", fmt.Errorf("未找到可用于运行 %s 的 Node.js，请先在「Node.js 版本管理」里扫描本机 Node", m.Name)
		}
		exe = node
		full = append([]string{m.Path}, full...)
		pathDirs = append(pathDirs, filepath.Dir(node))
	}
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, exe, full...)
	pathDirs = append(pathDirs, filepath.Dir(m.Path))
	if d := globalBinOf(m); d != "" {
		pathDirs = append(pathDirs, d)
	}
	env := os.Environ()
	sep := string(os.PathListSeparator)
	env = append(env, "PATH="+strings.Join(pathDirs, sep)+sep+os.Getenv("PATH"))
	if reg := GetRegistry(); reg != "" {
		env = append(env, "npm_config_registry="+reg)
	}
	cmd.Env = env
	cmd.SysProcAttr = hideWindowAttr()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		out := strings.TrimSpace(buf.String())
		if out == "" {
			return "", fmt.Errorf("%s %v: %v", m.Name, args, err)
		}
		return out, fmt.Errorf("%s %v: %v", m.Name, args, err)
	}
	return strings.TrimSpace(buf.String()), nil
}

// globalBinOf 管理器写可执行文件转发壳的目录（安装后提示用）
func globalBinOf(m PackageManager) string {
	for _, r := range m.Roots {
		if filepath.Base(r) == "node_modules" {
			return filepath.Dir(r)
		}
	}
	return ""
}

// RunManagerVersion 取管理器版本（探测不到版本号时由前端主动调用一次）
func RunManagerVersion(id string) (string, error) {
	m, err := resolveManager(id)
	if err != nil {
		return "", err
	}
	out, err := runManager(m, []string{"--version"}, time.Minute)
	if err != nil {
		return "", err
	}
	return normalizeVersionName(verExtractRe.FindString(out)), nil
}

// InstallGlobalWith 按管理器异步全局安装，进度经 "npm:install" 事件推送
func InstallGlobalWith(id, pkg string) error { return runPkgByManager(id, "", "install", pkg) }

// UninstallGlobalWith 按管理器异步全局卸载；installedAt 为包的实际安装目录，
// 用于反查当初是哪个（哪份 Node 的）管理器装的它，避开多份 npm 互相卸错。
func UninstallGlobalWith(id, pkg, installedAt string) error {
	return runPkgByManager(id, installedAt, "uninstall", pkg)
}

func runPkgByManager(id, installedAt, action, pkg string) error {
	if err := checkPkgName(pkg); err != nil {
		return err
	}
	pkg = strings.TrimSpace(pkg)
	var (
		m   PackageManager
		err error
	)
	if action == "uninstall" {
		m, err = resolveManagerForPkg(id, installedAt)
	} else {
		m, err = resolveManager(id)
	}
	if err != nil {
		return err
	}
	if strings.EqualFold(m.Name, "corepack") {
		return fmt.Errorf("corepack 只用于启用 / 锁定 yarn、pnpm，不支持按包安装")
	}
	return runPkgAction(m, action, pkg)
}

// runPkgAction 共用的异步执行 + 事件推送（同管理器串行，避免并发改全局 node_modules）
func runPkgAction(m PackageManager, action, pkg string) error {
	npmMu.Lock()
	if npmRunning {
		npmMu.Unlock()
		return fmt.Errorf("已有包任务在执行，请稍后再试")
	}
	npmRunning = true
	npmMu.Unlock()

	label := strings.Join(managerArgs(m, action, pkg), " ")
	go func() {
		defer func() {
			npmMu.Lock()
			npmRunning = false
			npmMu.Unlock()
		}()
		emitInstall(InstallProgress{Version: pkg, Stage: "info", Note: fmt.Sprintf("%s %s（%s）", m.Name, label, pkg)})
		out, err := runManager(m, managerArgs(m, action, pkg), 15*time.Minute)
		if out != "" {
			emitInstall(InstallProgress{Version: pkg, Stage: "info", Note: trimForErr(out)})
		}
		if err != nil {
			emitInstall(InstallProgress{Version: pkg, Stage: "error", Note: err.Error()})
			return
		}
		if bin := globalBinOf(m); action == "install" && bin != "" && !pathListed(bin) {
			emitInstall(InstallProgress{Version: pkg, Stage: "info", Note: "提示：全局 CLI 在 " + bin + "，不在 PATH 中则命令行调不到"})
		}
		verb := "安装"
		if action != "install" {
			verb = "卸载"
		}
		emitInstall(InstallProgress{Version: pkg, Stage: "done", Percent: 100, Note: pkg + " 已" + verb + "完成"})
	}()
	return nil
}

// pathListed 目录是否已在 PATH 中
func pathListed(dir string) bool {
	if dir == "" {
		return false
	}
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		if pathKey(d) == pathKey(dir) {
			return true
		}
	}
	return false
}
