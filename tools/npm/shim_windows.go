//go:build windows

package npm

import (
	"os"
	"path/filepath"
	"strings"
)

// shimExts 返回 cmd / PowerShell 解析 `npm` 时尝试的后缀顺序。
// 必须照抄 PATHEXT 而不能硬编码 .cmd：同一个目录里 .COM / .EXE 会盖过 .CMD，
// 判断「cmd 敲 npm 到底执行谁」只有按这个顺序才准确。
func shimExts() []string {
	ext := strings.ToUpper(os.Getenv("PATHEXT"))
	if ext == "" {
		ext = ".COM;.EXE;.BAT;.CMD;.VBS;.JS;.WS;.MSC"
	}
	var out []string
	for _, e := range strings.Split(ext, ";") {
		e = strings.TrimSpace(strings.ToUpper(e))
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		out = append(out, e)
	}
	return out
}

// findShim 按 PATHEXT 顺序在 dir 内找名为 name 的可执行入口。
// 无后缀的同名文件（Node 目录里也有个 npm）cmd 看不见，所以不作兜底。
func findShim(dir, name string) string {
	for _, e := range shimExts() {
		p := filepath.Join(dir, name+e)
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// prefixShimNames 覆盖层（全局 prefix）里 npm 自己的转发壳，相对 prefixShimDir。
// cmd 只看得见 .cmd，PowerShell 还会用 .ps1，所以一并列出供「移除覆盖层」清理。
var prefixShimNames = []string{"npm.cmd", "npx.cmd", "npm", "npx", "npm.ps1", "npx.ps1"}

// overrideNpmDir 覆盖层 npm 目录。Windows 上全局包直接摊在 prefix/node_modules，
// 没有 lib 这一层（与 POSIX 不同）。
func overrideNpmDir(prefix string) string {
	return filepath.Join(prefix, "node_modules", "npm")
}

// localNpmDir 入口目录自己的 npm 包位置
func localNpmDir(dir string) string {
	return filepath.Join(dir, "node_modules", "npm")
}

// prefixShimDir 转发壳目录，Windows 上就是 prefix 根目录
func prefixShimDir(prefix string) string { return prefix }

// writePrefixShims 在 prefix 目录生成 npm / npx 转发壳，内容取自 tarball 的 bin/*。
// 严格说不是必需：Node 目录的 npm.cmd 每次执行都会自己跳到 prefix 里的 npm-cli.js。
// 补上是为了和 `npm i -g npm` 的结果一致 —— 有些机器的 prefix 排在 PATH 前面，
// 那时靠的就是这些壳；缺了会退化成「切了版本但 cmd 找不到 npm」。
func writePrefixShims(prefix, payloadDir string) error {
	bin := prefixShimDir(prefix)
	for _, name := range []string{"npm.cmd", "npx.cmd"} {
		data, err := os.ReadFile(filepath.Join(payloadDir, "bin", name))
		if err != nil {
			continue // 老版本 tarball 里没有这个壳，跳过不影响生效链
		}
		if err := os.WriteFile(filepath.Join(bin, name), data, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// isNpmOwnShim 判断一个转发壳是不是 npm 自己生成的（只删这种，不误删用户文件）
func isNpmOwnShim(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	s := string(data)
	return strings.Contains(s, `node_modules\npm\bin\npm-cli.js`) ||
		strings.Contains(s, "node_modules/npm/bin/npm-cli.js") ||
		strings.Contains(s, `node_modules\npm\bin\npm-prefix.js`) ||
		strings.Contains(s, "node_modules/npm/bin/npm-prefix.js")
}
