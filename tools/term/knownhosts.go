package term

// ============================================================
// SSH 主机密钥校验（known_hosts / TOFU）
//
// 落盘：<data>/term/known_hosts（OpenSSH 兼容格式，可直接被系统 ssh 复用），
// 策略：<data>/term/hostkey.json {"policy":"tofu"}。
//
// 三档策略：
//   - strict：主机密钥不在 known_hosts 里即拒绝（不自动记录），必须先导入/确认；
//   - tofu （默认，Trust On First Use）：首次连接静默记录指纹并放行，
//     之后指纹变化则硬阻断（可能被中间人）；
//   - off  ：维持历史行为 InsecureIgnoreHostKey，不校验也不记录（仅兼容老用法）。
//
// 与 P0 的凭据表互不相干：这里只管"服务端是谁"，凭据管"我以谁登录"。
// ============================================================

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"Tools/appdirs"
)

const (
	HostKeyStrict = "strict"
	HostKeyTOFU   = "tofu"
	HostKeyOff    = "off"
)

var hostKeyMu sync.Mutex

// KnownHostEntry known_hosts 的一行（回给前端展示指纹）
type KnownHostEntry struct {
	Address     string `json:"address"`     // host 或 [host]:port
	KeyType     string `json:"keyType"`     // ssh-ed25519 / rsa-sha2-256 ...
	Fingerprint string `json:"fingerprint"` // SHA256:xxxx（与 OpenSSH 同口径）
}

// knownHostsPath <data>/term/known_hosts
func knownHostsPath() (string, error) {
	dir, err := appdirs.Data("term")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "known_hosts"), nil
}

// hostKeyPolicyPath <data>/term/hostkey.json
func hostKeyPolicyPath() (string, error) {
	dir, err := appdirs.Data("term")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "hostkey.json"), nil
}

// GetHostKeyPolicy 读校验策略；无配置文件时默认 tofu。
func GetHostKeyPolicy() string {
	hostKeyMu.Lock()
	defer hostKeyMu.Unlock()
	return readHostKeyPolicyLocked()
}

func readHostKeyPolicyLocked() string {
	path, err := hostKeyPolicyPath()
	if err != nil {
		return HostKeyTOFU
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return HostKeyTOFU
	}
	var v struct {
		Policy string `json:"policy"`
	}
	if err := json.Unmarshal(data, &v); err != nil {
		return HostKeyTOFU
	}
	switch v.Policy {
	case HostKeyStrict, HostKeyOff:
		return v.Policy
	default:
		return HostKeyTOFU
	}
}

// SetHostKeyPolicy 写校验策略（仅接受三档值），返回生效后的策略。
func SetHostKeyPolicy(policy string) (string, error) {
	switch policy {
	case HostKeyStrict, HostKeyTOFU, HostKeyOff:
	default:
		return "", fmt.Errorf("未知的主机密钥策略: %s", policy)
	}
	hostKeyMu.Lock()
	defer hostKeyMu.Unlock()
	path, err := hostKeyPolicyPath()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	data, _ := json.Marshal(map[string]string{"policy": policy})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return policy, nil
}

// ensureKnownHostsFile 保证 known_hosts 存在（knownhosts.New 打不开缺失文件会报错）
func ensureKnownHostsFile(path string) error {
	if _, err := os.Stat(path); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte{}, 0o600)
}

// HostKeyCallback 是 hostKeyCallback 的导出封装，供其它包（如 sshgen 部署公钥）
// 复用同一套 known_hosts 校验策略，避免另写一份不校验主机密钥的不安全实现。
func HostKeyCallback(host string, port int) ssh.HostKeyCallback {
	return hostKeyCallback(host, port)
}

// hostKeyCallback 返回接入 ssh.ClientConfig 的 HostKeyCallback。
// host/port 用于策略为 off 之外时构造校验闭包；实际匹配以拨号传入的 hostname 为准。
func hostKeyCallback(host string, port int) ssh.HostKeyCallback {
	policy := GetHostKeyPolicy()
	if policy == HostKeyOff {
		return ssh.InsecureIgnoreHostKey()
	}
	return func(hostname string, remote net.Addr, key ssh.PublicKey) error {
		path, err := knownHostsPath()
		if err != nil {
			return err
		}
		if err := ensureKnownHostsFile(path); err != nil {
			return err
		}
		dbCb, err := knownhosts.New(path)
		if err != nil {
			return fmt.Errorf("known_hosts 解析失败: %w", err)
		}
		err = dbCb(hostname, remote, key)
		if err == nil {
			return nil // 已记录且匹配
		}
		var ke *knownhosts.KeyError
		if errors.As(err, &ke) && len(ke.Want) == 0 {
			// 主机未知
			if policy == HostKeyTOFU {
				hostKeyMu.Lock()
				defer hostKeyMu.Unlock()
				if rerr := recordHostKeyLocked(path, hostname, key); rerr != nil {
					return rerr
				}
				return nil // 首次静默记录并放行
			}
			return fmt.Errorf("主机 %s 的密钥不在 known_hosts 中（strict 模式已拒绝）；请先导入或确认其指纹", hostname)
		}
		// 已记录但不匹配：硬阻断（tofu / strict 同）
		return fmt.Errorf("主机 %s 的密钥指纹与 known_hosts 记录不一致，可能被中间人攻击；确认后再在主机密钥管理中替换旧指纹", hostname)
	}
}

// recordHostKeyLocked 追加一行到 known_hosts（调用方持 hostKeyMu）；已存在同地址同密钥则跳过。
func recordHostKeyLocked(path, hostname string, key ssh.PublicKey) error {
	finger := fingerprint(key)
	if knownHas(path, hostname, finger) {
		return nil
	}
	line := knownhosts.Line([]string{knownhosts.Normalize(hostname)}, key)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(line + "\n")
	return err
}

// fingerprint 计算 SHA256 指纹（与 OpenSSH `ssh-keygen -lf` 同口径）
func fingerprint(key ssh.PublicKey) string {
	sum := sha256.Sum256(key.Marshal())
	return "SHA256:" + base64.StdEncoding.EncodeToString(sum[:])
}

// knownHas 判断 known_hosts 里是否已有该地址 + 该指纹的组合（宽松按地址串匹配）
func knownHas(path, hostname, finger string) bool {
	entries, err := parseKnownHosts(path)
	if err != nil {
		return false
	}
	want := knownhosts.Normalize(hostname)
	for _, e := range entries {
		if e.Address == want && e.Fingerprint == finger {
			return true
		}
	}
	return false
}

// parseKnownHosts 解析 known_hosts 为可展示条目（跳过注释/空行/hash 主机名）。
// 对哈希过的地址（|1|...）原样保留在 Address 字段，指纹照常计算。
func parseKnownHosts(path string) ([]KnownHostEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []KnownHostEntry
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		addr := fields[0]
		keyType := fields[1]
		blob, err := base64.StdEncoding.DecodeString(fields[2])
		if err != nil {
			continue
		}
		sum := sha256.Sum256(blob)
		out = append(out, KnownHostEntry{
			Address:     addr,
			KeyType:     keyType,
			Fingerprint: "SHA256:" + base64.StdEncoding.EncodeToString(sum[:]),
		})
	}
	return out, nil
}

// ListKnownHosts 列出本机已记录的主机密钥
func ListKnownHosts() ([]KnownHostEntry, error) {
	hostKeyMu.Lock()
	defer hostKeyMu.Unlock()
	path, err := knownHostsPath()
	if err != nil {
		return nil, err
	}
	return parseKnownHosts(path)
}

// DeleteKnownHost 删除某地址的整行（指纹变了要重来时用）
func DeleteKnownHost(address string) ([]KnownHostEntry, error) {
	hostKeyMu.Lock()
	defer hostKeyMu.Unlock()
	path, err := knownHostsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []KnownHostEntry{}, nil
		}
		return nil, err
	}
	var kept []string
	for _, raw := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) > 0 && fields[0] == address {
			continue
		}
		kept = append(kept, trimmed)
	}
	out := []byte(strings.Join(kept, "\n"))
	if len(out) > 0 {
		out = append(out, '\n')
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return nil, err
	}
	return parseKnownHosts(path)
}

// systemKnownHostsPath 系统 OpenSSH 的 known_hosts（~/.ssh/known_hosts）
func systemKnownHostsPath() (string, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false
	}
	p := filepath.Join(home, ".ssh", "known_hosts")
	if _, err := os.Stat(p); err != nil {
		return "", false
	}
	return p, true
}

// ImportSystemKnownHosts 导入系统 known_hosts：去重合并，不覆盖本机已记录项。
// 返回新增行数；系统文件不存在返回 (0, nil)。
func ImportSystemKnownHosts() (int, error) {
	src, ok := systemKnownHostsPath()
	if !ok {
		return 0, nil
	}
	hostKeyMu.Lock()
	defer hostKeyMu.Unlock()
	dst, err := knownHostsPath()
	if err != nil {
		return 0, err
	}
	if err := ensureKnownHostsFile(dst); err != nil {
		return 0, err
	}
	dstEntries, err := parseKnownHosts(dst)
	if err != nil {
		return 0, err
	}
	have := make(map[string]bool, len(dstEntries))
	for _, e := range dstEntries {
		have[e.Address+"|"+e.Fingerprint] = true
	}
	srcData, err := os.ReadFile(src)
	if err != nil {
		return 0, err
	}
	var add []string
	for _, raw := range strings.Split(string(srcData), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "@") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// 逐地址去重（一行可能有逗号分隔的多个主机）
		addr := fields[0]
		for _, a := range strings.Split(addr, ",") {
			if !have[a+"|"+fields[2]] {
				// 只要有一个新地址就整行导入一次即可
				add = append(add, line)
				break
			}
		}
	}
	if len(add) == 0 {
		return 0, nil
	}
	f, err := os.OpenFile(dst, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	written := 0
	for _, line := range add {
		if _, err := f.WriteString(line + "\n"); err != nil {
			return written, err
		}
		written++
	}
	return written, nil
}
