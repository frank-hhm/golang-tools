//go:build !windows

package npm

import (
	"os"
	"path/filepath"
)

// locateNodeIn 在候选目录内定位 node 可执行文件：根目录，以及发行包常见的
// 下一级 bin/ 、fnm 的 installation/ 布局
func locateNodeIn(dir string) string {
	var try []string
	if dir == "" {
		return ""
	}
	try = append(try, filepath.Join(dir, "node"))
	if subs, err := os.ReadDir(dir); err == nil {
		for _, s := range subs {
			if !s.IsDir() {
				continue
			}
			sub := filepath.Join(dir, s.Name())
			try = append(try, filepath.Join(sub, "node"), filepath.Join(sub, "bin", "node"), filepath.Join(sub, "installation", "bin", "node"))
		}
	}
	for _, p := range try {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// foldPath POSIX 路径区分大小写
func foldPath(p string) string { return p }

// scanCandidateDirs 汇总 POSIX 上 Node.js 可能出现的位置
func scanCandidateDirs() []string {
	home, _ := os.UserHomeDir()
	var dirs []string
	dirs = append(dirs, pathNodeDirs()...)
	dirs = append(dirs, existing(
		"/usr/local/bin",
		"/usr/bin",
		"/opt/homebrew/bin", // Apple Silicon 上的 Homebrew
		"/opt/bin",
		filepath.Join(home, ".linuxbrew", "bin"),
	)...)

	// 版本管理器：仓库根下的 <ver> 目录里再按 node / bin/node / installation/bin/node 定位
	dirs = append(dirs, subDirsWithNode(
		filepath.Join(home, ".nvm", "versions", "node"),
		filepath.Join(home, ".fnm", "node-versions"),
		filepath.Join(home, ".local", "share", "fnm", "node-versions"),
		filepath.Join(home, ".volta", "tools", "image", "node"),
		filepath.Join(home, ".asdf", "installs", "nodejs"),
		filepath.Join(home, ".local", "share", "mise", "installs", "node"),
	)...)

	return dedupeDirs(dirs)
}
