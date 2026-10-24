//go:build windows

package npm

import (
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// locateNodeIn 在候选目录内定位 node.exe（根目录或一级子目录，复用 findNode）
func locateNodeIn(dir string) string { return findNode(dir) }

// foldPath Windows 路径不区分大小写
func foldPath(p string) string { return strings.ToLower(p) }

// scanCandidateDirs 汇总 Windows 上 Node.js 可能出现的位置：
// PATH 全量目录 → MSI 注册表 → 各版本管理器 / 包管理器的已知布局。
func scanCandidateDirs() []string {
	var dirs []string
	dirs = append(dirs, pathNodeDirs()...)
	dirs = append(dirs, registryNodeDirs()...)

	pf := os.Getenv("ProgramFiles")
	pf86 := os.Getenv("ProgramFiles(x86)")
	if pf86 == "" {
		pf86 = pf // 32 位系统上没有独立的 (x86) 目录
	}
	local := os.Getenv("LOCALAPPDATA")
	roaming := os.Getenv("APPDATA")
	home, _ := os.UserHomeDir()
	pd := os.Getenv("ProgramData")

	dirs = append(dirs, existing(
		filepath.Join(pf, "nodejs"),
		filepath.Join(pf86, "nodejs"),
		filepath.Join(pd, "chocolatey", "lib", "nodejs", "tools"),
		filepath.Join(home, "scoop", "apps", "nodejs", "current"),
		filepath.Join(home, "scoop", "apps", "nodejs-lts", "current"),
	)...)

	// nvm-windows：版本仓库 <root>\v<ver>\node.exe，另有 <path> 软链指向当前版本
	for _, repo := range nvmHomes() {
		dirs = append(dirs, subDirsWithNode(repo)...)
	}
	dirs = append(dirs, existing(os.Getenv("NVM_SYMLINK"))...)

	// fnm：<root>\node-versions\<ver>\installation\node.exe
	dirs = append(dirs, subDirsWithNode(
		filepath.Join(local, "fnm", "node-versions"),
		filepath.Join(roaming, "fnm", "node-versions"),
	)...)

	// volta：<root>\Volta\tools\image\nodes\<ver>\exe\node.exe
	// 只取真实发行目录，不收 Volta\bin 下的转发壳（那里没有配套的 npm 包目录）
	dirs = append(dirs, subDirsWithNode(
		filepath.Join(local, "Volta", "tools", "image", "nodes"),
	)...)

	return dedupeDirs(dirs)
}

// nvmHomes nvm-windows 的版本仓库根目录：环境变量优先，其次读 settings.txt 的 root
func nvmHomes() []string {
	var out []string
	if v := os.Getenv("NVM_HOME"); v != "" {
		out = append(out, v)
	}
	for _, cfg := range existing(
		filepath.Join(os.Getenv("APPDATA"), "nvm"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "nvm"),
	) {
		out = append(out, cfg)
		if st, err := os.Stat(filepath.Join(cfg, "node.exe")); err == nil && !st.IsDir() {
			continue // 仓库根自己就是 node.exe 所在处的老布局
		}
		if root := readNvmSetting(filepath.Join(cfg, "settings.txt"), "root"); root != "" {
			out = append(out, root)
		}
		if path := readNvmSetting(filepath.Join(cfg, "settings.txt"), "path"); path != "" {
			out = append(out, path) // 当前激活版本的软链目录
		}
	}
	return existing(out...)
}

// readNvmSetting 解析 nvm-windows settings.txt 的「key = value」行
func readNvmSetting(file, key string) string {
	data, err := os.ReadFile(file)
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(data), "\n") {
		l = strings.TrimSpace(strings.TrimSuffix(l, "\r"))
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		i := strings.Index(l, "=")
		if i <= 0 {
			continue
		}
		if strings.TrimSpace(l[:i]) == key {
			return strings.Trim(strings.TrimSpace(l[i+1:]), `"`)
		}
	}
	return ""
}

// registryNodeDirs 读 MSI 安装信息。Node 的 MSI 会把实际安装目录写进
// HKLM\SOFTWARE\Node.js 的 InstallPath（自定义安装路径也准确），
// 这是唯一能覆盖「装了但没进 PATH」的可靠来源。
// 本进程位数未知，三个视图各查一次：默认 / 强制 64 位 / 强制 32 位（x86 发行包）。
func registryNodeDirs() []string {
	var out []string
	for _, view := range []uint32{0, registry.WOW64_64KEY, registry.WOW64_32KEY} {
		k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Node.js`, registry.QUERY_VALUE|view)
		if err != nil {
			continue
		}
		if s, _, err := k.GetStringValue("InstallPath"); err == nil && s != "" {
			out = append(out, strings.TrimRight(s, `"\`))
		}
		k.Close()
	}
	return existing(out...)
}
