package npm

// ============================================================
// npm 配置与全局包管理
//
// 关键设计：所有 npm 命令统一带 `--userconfig <Tools/npmrc>`，
// 因此本程序里的 registry / 镜像源改动默认只影响 Tools 自身，
// 不会静默改写用户 ~/.npmrc；「应用到用户全局配置」需用户显式点击。
// ============================================================

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// defaultNpmrc 托管配置初始内容
const defaultNpmrc = `; Tools 托管的 npm 配置文件（所有 npm 命令统一 --userconfig 指向本文件）
; 在这里改 registry / 代理只影响 Tools 的「全局包管理」，
; 需要生效到本机所有终端时，点「应用到用户全局 ~/.npmrc」。
registry=https://registry.npmjs.org/
`

var (
	npmMu      sync.Mutex
	npmRunning bool
)

/* ============================ 配置读写 ============================ */

// NpmrcPath 返回本程序托管的 .npmrc 路径（不存在则写入默认内容）
func NpmrcPath() (string, error) {
	p, err := managedNpmrcPath()
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(p); err != nil {
		if err := os.WriteFile(p, []byte(defaultNpmrc), 0o644); err != nil {
			return "", err
		}
	}
	return p, nil
}

// GetConfigText 返回托管 .npmrc 全文
func GetConfigText() (string, error) {
	p, err := NpmrcPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SaveConfigText 写回托管 .npmrc
func SaveConfigText(text string) error {
	p, err := NpmrcPath()
	if err != nil {
		return err
	}
	if !strings.HasSuffix(text, "\n") {
		text += "\n"
	}
	return os.WriteFile(p, []byte(text), 0o644)
}

// ConfigItems 解析托管 .npmrc 为键值对（忽略注释；保留 section 前缀）
func ConfigItems() ([]NpmConfigKV, error) {
	text, err := GetConfigText()
	if err != nil {
		return nil, err
	}
	var out []NpmConfigKV
	section := ""
	for _, l := range strings.Split(text, "\n") {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, ";") || strings.HasPrefix(t, "#") {
			continue
		}
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			section = strings.Trim(t, "[]")
			continue
		}
		i := strings.Index(t, "=")
		if i <= 0 {
			continue
		}
		k := strings.TrimSpace(t[:i])
		if section != "" {
			k = section + ":" + k
		}
		out = append(out, NpmConfigKV{Key: k, Value: strings.TrimSpace(t[i+1:])})
	}
	return out, nil
}

// SetConfigItem 设置/删除（value 为空即删除）一条配置
func SetConfigItem(key, value string) error {
	text, err := GetConfigText()
	if err != nil {
		return err
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines)+1)
	replaced := false
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" && !strings.HasPrefix(t, ";") && !strings.HasPrefix(t, "#") && strings.Contains(t, "=") {
			if strings.TrimSpace(t[:strings.Index(t, "=")]) == key {
				if value == "" {
					continue // 删除
				}
				out = append(out, key+"="+value)
				replaced = true
				continue
			}
		}
		out = append(out, l)
	}
	if !replaced && value != "" {
		out = append(out, key+"="+value)
	}
	return SaveConfigText(strings.Join(out, "\n"))
}

// GetRegistry 返回当前生效的 registry
func GetRegistry() string {
	items, err := ConfigItems()
	if err != nil {
		return ""
	}
	for _, it := range items {
		if it.Key == "registry" {
			return it.Value
		}
	}
	return ""
}

// RegistryOf 指定 Node 版本下的 registry（同托管配置，供状态面板展示）
func RegistryOf(version string) string { return GetRegistry() }

// SetRegistry 切换 registry（快捷设置）
func SetRegistry(url string) error {
	if strings.TrimSpace(url) == "" {
		return fmt.Errorf("registry 不能为空")
	}
	return SetConfigItem("registry", strings.TrimSpace(url))
}

// 常用镜像源，供前端下拉快速切换
func CommonRegistries() []NpmConfigKV {
	return []NpmConfigKV{
		{Key: "npm 官方", Value: "https://registry.npmjs.org/"},
		{Key: "npmmirror(淘宝)", Value: "https://registry.npmmirror.com/"},
		{Key: "腾讯云", Value: "https://mirrors.cloud.tencent.com/npm/"},
		{Key: "华为云", Value: "https://repo.huaweicloud.com/repository/npm/"},
	}
}

// UserNpmrc 返回用户全局 ~/.npmrc 内容与路径（只读展示，便于对照）
func UserNpmrc() (path, text string, exists bool) {
	path = userNpmrcPath()
	if path == "" {
		return "", "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return path, "", false
	}
	return path, string(data), true
}

// ApplyToUserNpmrc 把托管配置追加写入用户全局 ~/.npmrc（覆盖同名键，先备份）
func ApplyToUserNpmrc() (string, error) {
	path := userNpmrcPath()
	if path == "" {
		return "", fmt.Errorf("无法定位用户主目录")
	}
	items, err := ConfigItems()
	if err != nil {
		return "", err
	}
	var old string
	if data, err := os.ReadFile(path); err == nil {
		old = string(data)
		_ = os.WriteFile(path+".bak", data, 0o644)
	}
	lines := strings.Split(old, "\n")
	var out []string
	used := map[string]bool{}
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" && !strings.HasPrefix(t, ";") && !strings.HasPrefix(t, "#") && strings.Contains(t, "=") {
			k := strings.TrimSpace(t[:strings.Index(t, "=")])
			for _, it := range items {
				if it.Key == k {
					used[k] = true
					out = append(out, it.Key+"="+it.Value)
					break
				}
			}
			if !used[k] {
				out = append(out, l)
			}
			continue
		}
		out = append(out, l)
	}
	var added []string
	for _, it := range items {
		if !used[it.Key] && !strings.Contains(it.Key, ":") {
			added = append(added, it.Key+"="+it.Value)
		}
	}
	if len(added) > 0 {
		out = append(out, "; ---- added by Tools ----")
		out = append(out, added...)
	}
	content := strings.Join(out, "\n")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

/* ============================ npm 命令执行 ============================ */

// npmCmdPath 定位一个 Node 实际会用到的 npm 入口（绕过 .cmd 包装，跨平台一致）。
// 与 npm.cmd 同逻辑：全局 prefix 里有 npm 就用它，否则才用 Node 自带的 ——
// 不这么对齐的话，面板里的 npm 版本和用户在 cmd 里 `npm -v` 看到的就是两个数。
func npmCmdPath(nodeExe string) string {
	if cli := effectiveNpmCLI(nodeExe); cli != "" {
		return cli
	}
	return filepath.Join(filepath.Dir(nodeExe), "node_modules", "npm", "bin", "npm-cli.js")
}

// runNpm 用指定 Node 版本执行 npm 参数；env 注入该版本的 PATH 与 --userconfig
func runNpm(version string, args []string, timeout time.Duration) (string, error) {
	exe := nodeExe(version)
	if exe == "" {
		return "", fmt.Errorf("Node %s 未安装", version)
	}
	cli := npmCmdPath(exe)
	if _, err := os.Stat(cli); err != nil {
		return "", fmt.Errorf("未找到 npm 入口（%s），该 Node 目录可能不含 npm", cli)
	}
	npmrc, err := NpmrcPath()
	if err != nil {
		return "", err
	}
	full := append([]string{cli}, args...)
	full = append(full, "--userconfig", npmrc)
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, exe, full...)
	cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(exe)+string(os.PathListSeparator)+os.Getenv("PATH"))
	cmd.SysProcAttr = hideWindowAttr()
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		out := strings.TrimSpace(buf.String())
		if out == "" {
			return "", err
		}
		return out, fmt.Errorf("npm %v: %v", args, err)
	}
	return strings.TrimSpace(buf.String()), nil
}

// GlobalPackages 列出指定 Node 版本下的全局包
func GlobalPackages(version string) ([]NpmPackage, error) {
	out, err := runNpm(version, []string{"ls", "-g", "--depth", "0", "--json"}, 2*time.Minute)
	if err != nil {
		return nil, err
	}
	// npm 偶尔在 JSON 前打印告警行，从第一个 '{' 起截取
	if i := strings.IndexByte(out, '{'); i > 0 {
		out = out[i:]
	} else if i < 0 {
		return nil, nil
	}
	var payload struct {
		Dependencies map[string]struct {
			Version     string `json:"version"`
			Description string `json:"description"`
			Path        string `json:"path"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		return nil, fmt.Errorf("解析 npm 输出失败: %v\n%s", err, trimForErr(out))
	}
	var list []NpmPackage
	for name, d := range payload.Dependencies {
		list = append(list, NpmPackage{
			Name: name, Version: d.Version, Description: d.Description, Path: d.Path,
			Manager: "npm",
		})
	}
	return list, nil
}

// InstallGlobal 异步全局安装（按 Node 版本定位其自带的 npm）
func InstallGlobal(version, pkg string) error { return runPkgOnNode(version, "install", pkg) }

// UninstallGlobal 异步全局卸载
func UninstallGlobal(version, pkg string) error { return runPkgOnNode(version, "uninstall", pkg) }

func runPkgOnNode(version, action, pkg string) error {
	if err := checkPkgName(pkg); err != nil {
		return err
	}
	m, err := npmManagerForVersion(version)
	if err != nil {
		return err
	}
	return runPkgAction(m, action, strings.TrimSpace(pkg))
}

// NpmVersion 返回指定 Node 版本自带 npm 的版本
func NpmVersion(version string) string {
	if v := nodeExe(version); v != "" {
		if got := detectNpmVer(v); got != "" {
			return got
		}
	}
	return ""
}

// NodeVersion 返回指定版本目录中 node 的真实版本（读文件失败时执行 node -p）
func NodeVersion(version string) string {
	exe := nodeExe(version)
	if exe == "" {
		return ""
	}
	if v, err := probeVersion(exe); err == nil {
		return v
	}
	return version
}

// NpmDir 返回该 Node 版本目录（供前端展示 / 打开目录）
func NpmDir(version string) string {
	exe := nodeExe(version)
	if exe == "" {
		return ""
	}
	return filepath.Dir(exe)
}

func trimForErr(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 1200 {
		return s[:1200] + "…"
	}
	return s
}
