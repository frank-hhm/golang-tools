//go:build !windows

package npm

import (
	"os"
	"path/filepath"
	"strings"
)

// shimExts POSIX 下命令不带后缀
func shimExts() []string { return []string{""} }

// findShim 定位 dir 下名为 name 的可执行文件（需要可执行位）
func findShim(dir, name string) string {
	p := filepath.Join(dir, name)
	if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
		return p
	}
	return ""
}

// prefixShimNames 覆盖层里 npm 自己的转发壳（相对 prefixShimDir）
var prefixShimNames = []string{"npm", "npx"}

// overrideNpmDir 覆盖层 npm 目录。POSIX 的全局包根在 prefix/lib/node_modules，
// 与 Windows（prefix/node_modules）不同。
func overrideNpmDir(prefix string) string {
	return filepath.Join(prefix, "lib", "node_modules", "npm")
}

// localNpmDir 入口目录自己的 npm 包位置。PATH 里碰到的多是 prefix/bin 这种
// bin 目录，npm 包实在上一级的 lib/node_modules/npm。
func localNpmDir(dir string) string {
	base := dir
	if strings.EqualFold(filepath.Base(dir), "bin") {
		base = filepath.Dir(dir)
	}
	return filepath.Join(base, "lib", "node_modules", "npm")
}

// prefixShimDir 转发壳目录
func prefixShimDir(prefix string) string { return filepath.Join(prefix, "bin") }

// writePrefixShims 建 npm / npx 软链指向包内的 JS 入口（npm 官方在全局安装时就是这么做的）。
// 软链建不了（文件系统不支持等）时退化为复制，保证 cmd 找得到 npm。
func writePrefixShims(prefix, payloadDir string) error {
	bin := prefixShimDir(prefix)
	if err := os.MkdirAll(bin, 0o755); err != nil {
		return err
	}
	for _, name := range prefixShimNames {
		src := filepath.Join(payloadDir, "bin", name+"-cli.js")
		if _, err := os.Stat(src); err != nil {
			continue
		}
		_ = os.Chmod(src, 0o755)
		dst := filepath.Join(bin, name)
		_ = os.Remove(dst)
		rel, err := filepath.Rel(bin, src)
		if err == nil {
			if err := os.Symlink(rel, dst); err == nil {
				continue
			}
		}
		data, rerr := os.ReadFile(src)
		if rerr != nil {
			return rerr
		}
		if werr := os.WriteFile(dst, data, 0o755); werr != nil {
			return werr
		}
	}
	return nil
}

// isNpmOwnShim 只认 npm 自己生成的入口，避免删掉用户放的东西。
// 软链按目标判断，普通文件按 shebang + 内容判断。
func isNpmOwnShim(path string) bool {
	if tgt, err := os.Readlink(path); err == nil {
		return strings.Contains(filepath.ToSlash(tgt), "npm-cli.js") ||
			strings.Contains(filepath.ToSlash(tgt), "npx-cli.js")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	s := string(data)
	return strings.Contains(s, "npm-cli.js") || strings.Contains(s, "npx-cli.js")
}
