package term

// ============================================================
// ~/.ssh/config 解析（只导草稿，不接管运行时）
//
// 目的：把用户已有的 OpenSSH 客户端配置读出来，作为"可编辑的主机草稿列表"
// 呈现，勾选后才由前端逐项调 TermSaveHost 落库。我们**不实现** ProxyJump、
// ForwardAgent 等多跳/转发语义——本期 x/crypto/ssh 走的是单跳直连，导入时把
// 这些指令如实标注为"未生效字段"，避免给出连不上的假承诺。
// ============================================================

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// HostDraft 一条从 ~/.ssh/config 解析出的主机草稿（不落库，供前端预览勾选）
type HostDraft struct {
	Alias        string   `json:"alias"`        // Host 后的模式名（作主机名）
	HostName     string   `json:"hostName"`     // 实际地址
	User         string   `json:"user"`         // 账号
	Port         int      `json:"port"`         // 端口（默认 22）
	IdentityFile string   `json:"identityFile"` // 私钥（取第一条）
	Unsupported  []string `json:"unsupported"`  // 被忽略、本期不生效的指令名
	Wildcard     bool     `json:"wildcard"`     // 模式含 * 或 ?，不适合直接落库
}

// systemSSHConfigPath ~/.ssh/config（不存在返回 ok=false）
func systemSSHConfigPath() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	p := filepath.Join(home, ".ssh", "config")
	if _, err := os.Stat(p); err != nil {
		return "", false
	}
	return p, true
}

// expandHome 把路径里的前导 ~ 展开为主目录（IdentityFile 常用 ~/）
func expandHome(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

// ParseSSHConfig 解析 ~/.ssh/config 为草稿列表；文件不存在返回空列表（非错误）。
func ParseSSHConfig() ([]HostDraft, error) {
	path, ok := systemSSHConfigPath()
	if !ok {
		return []HostDraft{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取 SSH 配置失败: %w", err)
	}
	return parseSSHConfigBytes(data), nil
}

// parseSSHConfigBytes 纯函数解析（便于单测，不碰文件系统）
func parseSSHConfigBytes(data []byte) []HostDraft {
	var drafts []HostDraft
	var cur *HostDraft
	inMatch := false // Match 块本期不支持，整块跳过

	flush := func() {
		if cur != nil {
			drafts = append(drafts, *cur)
			cur = nil
		}
	}

	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Host / Match 是块起点，其余 key value 归属当前块
		key, val, hasVal := splitConfigLine(line)
		lk := strings.ToLower(key)

		if lk == "match" {
			// 遇到 Match：关掉当前块并忽略后续直到下一个 Host
			flush()
			inMatch = true
			continue
		}
		if lk == "host" {
			inMatch = false
			flush()
			if !hasVal {
				continue
			}
			patterns := strings.Fields(val)
			alias := ""
			if len(patterns) > 0 {
				alias = patterns[0]
			}
			wild := false
			for _, pt := range patterns {
				if strings.ContainsAny(pt, "*?") {
					wild = true
					break
				}
			}
			cur = &HostDraft{Alias: alias, Port: 22, Wildcard: wild}
			continue
		}
		if inMatch {
			continue // Match 块内所有指令忽略
		}
		if cur == nil || !hasVal {
			continue
		}

		switch lk {
		case "hostname":
			cur.HostName = val
		case "user":
			cur.User = val
		case "port":
			if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && n > 0 && n <= 65535 {
				cur.Port = n
			}
		case "identityfile":
			// 多行 IdentityFile 取第一条已存在的（OpenSSH 允许回退列表）
			p := expandHome(strings.TrimSpace(val))
			if cur.IdentityFile == "" {
				cur.IdentityFile = p
			}
		case "proxyjump", "forwardagent", "localforward", "remoteforward", "dynamicforward", "proxycommand":
			name := unsupportedKeyNames[lk]
			found := false
			for _, u := range cur.Unsupported {
				if u == name {
					found = true
					break
				}
			}
			if !found {
				cur.Unsupported = append(cur.Unsupported, name)
			}
		}
	}
	flush()
	return drafts
}

// unsupportedKeyNames 本期不生效、仅如实标注的指令（小写 key → 展示名）
var unsupportedKeyNames = map[string]string{
	"proxyjump":      "ProxyJump",
	"forwardagent":   "ForwardAgent",
	"localforward":   "LocalForward",
	"remoteforward":  "RemoteForward",
	"dynamicforward": "DynamicForward",
	"proxycommand":   "ProxyCommand",
}

// splitConfigLine 拆 "Key Value" / "Key=Value"：返回 key、value、是否存在值。
// SSH config 大小写不敏感，key 原样返回（用于展示），value 去首尾空白。
func splitConfigLine(line string) (key, value string, hasVal bool) {
	if i := strings.Index(line, "="); i >= 0 && !strings.Contains(line[:i], " ") {
		return strings.TrimSpace(line[:i]), strings.TrimSpace(line[i+1:]), strings.TrimSpace(line[i+1:]) != ""
	}
	parts := strings.SplitN(line, " ", 2)
	if len(parts) == 1 {
		return strings.TrimSpace(parts[0]), "", false
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]), strings.TrimSpace(parts[1]) != ""
}
