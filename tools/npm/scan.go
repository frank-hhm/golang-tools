package npm

// ============================================================
// 本机 Node.js 扫描登记
//
// 「本机」渠道原本只能靠用户手填 node.exe 所在目录登记，而多数人
// 并不知道自己那份 Node 装在哪（MSI 自定义目录、nvm/fnm/volta 的
// 版本仓库、绿色包），结果版本管理里只看得到 Tools 自己下载的版本。
//
// 这里按 PATH 全量目录 → 注册表 → 各家版本管理器的已知布局穷举候选，
// 归一化真实路径去重后批量登记进 npm-locals.json，等价于「点了很多次
// 添加」，不改动 Tools 自管（bundled / downloaded）的那份目录。
// ============================================================

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ScanLocalNodes 扫描本机已安装的 Node.js 并登记为「本机」来源。
// 幂等：按 node 可执行文件的真实路径去重，已登记过的不再重复写入；
// 返回本次新增登记的版本列表（无新增时返回空）。
func ScanLocalNodes() ([]ServerVersion, error) {
	locals := loadLocals()

	// 已登记的本机项 + 已在版本列表中的项（含 Tools 自管版本）先入表，避免重复
	known := make(map[string]bool, len(locals)*2)
	for _, l := range locals {
		known[pathKey(l.Path)] = true
	}
	if vers, err := ListVersions(); err == nil {
		for _, v := range vers {
			known[pathKey(v.Path)] = true
		}
	}

	// Tools 插件目录下的版本走 bundled / downloaded 渠道，不当本机重复登记
	root, _ := pluginsRoot()

	var added []ServerVersion
	for _, dir := range scanCandidateDirs() {
		exe := locateNodeIn(dir)
		if exe == "" {
			continue
		}
		key := pathKey(exe)
		if known[key] {
			continue
		}
		if root != "" && withinDir(exe, root) {
			continue
		}
		ver := resolveNodeVersion(exe, dir)
		if ver == "" {
			continue // 目录名不像版本号且 node -p 失败（损坏 / 不兼容），跳过
		}
		known[key] = true
		added = append(added, ServerVersion{Version: ver, Path: exe, Source: "local", NpmVer: detectNpmVer(exe)})
	}
	if len(added) == 0 {
		return nil, nil
	}
	list := locals
	for _, a := range added {
		list = append(list, localEntry{Version: a.Version, Path: a.Path})
	}
	if err := saveLocals(list); err != nil {
		return nil, err
	}
	sort.Slice(added, func(i, j int) bool { return compareVersion(added[i].Version, added[j].Version) > 0 })
	return added, nil
}

// resolveNodeVersion 取版本号：目录名本身就是版本号（nvm / fnm 的 v22.14.0）时直接采用，
// 省掉一次进程启动；否则执行 node -p process.versions.node 探测。
func resolveNodeVersion(exe, dir string) string {
	for _, name := range []string{filepath.Base(dir), filepath.Base(filepath.Dir(dir))} {
		if v := normalizeVersionName(name); versionRe.MatchString(v) {
			return v
		}
	}
	if v, err := probeVersion(exe); err == nil {
		return v
	}
	return ""
}

/* ============================ 候选收集（平台无关部分） ============================ */

// pathNodeDirs 遍历 PATH 中每个目录，保留含 node 可执行文件的。
// 不用 exec.LookPath：它只返回命中的第一个，而一台机器常有多份 Node 分列 PATH 前后。
func pathNodeDirs() []string {
	var out []string
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		d = strings.Trim(d, `"`)
		if d == "" {
			continue
		}
		if locateNodeIn(d) != "" {
			out = append(out, d)
		}
	}
	return out
}

// subDirsWithNode 列出各版本仓库根目录下含 node 的一级子目录（nvm / fnm / volta 的布局）
func subDirsWithNode(roots ...string) []string {
	var out []string
	for _, dir := range roots {
		subs, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, s := range subs {
			if !s.IsDir() {
				continue
			}
			sub := filepath.Join(dir, s.Name())
			if locateNodeIn(sub) != "" {
				out = append(out, sub)
			}
		}
	}
	return out
}

// existing 过滤出真实存在的目录
func existing(dirs ...string) []string {
	var out []string
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			out = append(out, d)
		}
	}
	return out
}

// dedupeDirs 按归一化路径去重，保持发现顺序（PATH 命中的排前面）
func dedupeDirs(dirs []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		k := pathKey(d)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, d)
	}
	return out
}

/* ============================ 路径归一化 ============================ */

// pathKey 归一化路径用作去重键：绝对化 + 解析符号链接 + 平台相关的大小写折叠。
// nvm-windows 的 NVM_SYMLINK 是指向版本目录的软链，不解析就会把一份 Node 记成两条。
func pathKey(p string) string {
	p = strings.Trim(p, `"`)
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	if r, err := filepath.EvalSymlinks(p); err == nil {
		p = r
	}
	return foldPath(filepath.Clean(p))
}

// withinDir p 是否位于 dir 之内
func withinDir(p, dir string) bool {
	pp, dd := pathKey(p), pathKey(dir)
	return pp == dd || strings.HasPrefix(pp, dd+string(os.PathSeparator))
}
