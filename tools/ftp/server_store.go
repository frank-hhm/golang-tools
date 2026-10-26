package ftp

// ============================================================
// SFTP 服务端配置与虚拟账号落盘
//
// 落盘：<data>/ftp/server.json（0600）。结构分两块：
//   - config：监听地址/端口/服务端根目录/自动启动
//   - accounts：虚拟账号，密码只存 bcrypt 哈希，回给前端的列表里恒为空
//
// 账号根目录限死在服务端根目录之内：只接受相对子路径，拒绝绝对路径与 ..，
// 保存时就建出来，运行时再用 os.Root 兜一层（软链也逃不出去）。
// ============================================================

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-tools/appdirs"

	"golang.org/x/crypto/bcrypt"
)

// accountRec 落盘形态：多一个密码哈希
type accountRec struct {
	Account
	PassHash string `json:"passHash,omitempty"`
}

func (r accountRec) toAccount() Account {
	a := r.Account
	a.Password = ""
	return a
}

// serverFile 服务端落盘文件
type serverFile struct {
	Config   ServerConfig `json:"config"`
	Accounts []accountRec `json:"accounts"`
}

var serverMu sync.Mutex

// serverDir <data>/ftp/server（放 host key 与默认根目录）
func serverDir() (string, error) {
	base, err := appdirs.Data("ftp")
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "server")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建服务端目录失败: %v", err)
	}
	return dir, nil
}

func serverFilePath() (string, error) {
	dir, err := serverDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "server.json"), nil
}

// normalizeServerConfig 补默认值（host/port 空值时用默认）
func normalizeServerConfig(c ServerConfig) ServerConfig {
	c.Host = strings.TrimSpace(c.Host)
	if c.Host == "" {
		c.Host = DefaultListenHost
	}
	if c.Port <= 0 || c.Port > 65535 {
		c.Port = DefaultServerPort
	}
	c.RootDir = strings.TrimSpace(c.RootDir)
	return c
}

func readServerFile() (serverFile, error) {
	f := serverFile{}
	p, err := serverFilePath()
	if err != nil {
		return f, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			f.Config = normalizeServerConfig(ServerConfig{})
			return f, nil
		}
		return f, fmt.Errorf("读取服务端配置失败: %v", err)
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &f); err != nil {
			return f, fmt.Errorf("服务端配置文件损坏: %v", err)
		}
	}
	f.Config = normalizeServerConfig(f.Config)
	return f, nil
}

func writeServerFile(f serverFile) error {
	p, err := serverFilePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// ServerRoot 服务端根目录（空配置时为 <data>/ftp/server/root），不存在则创建
func ServerRoot(cfg ServerConfig) (string, error) {
	if dir := strings.TrimSpace(cfg.RootDir); dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return "", fmt.Errorf("服务端根目录非法: %v", err)
		}
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return "", fmt.Errorf("创建服务端根目录失败: %v", err)
		}
		return abs, nil
	}
	dir, err := serverDir()
	if err != nil {
		return "", err
	}
	root := filepath.Join(dir, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("创建服务端根目录失败: %v", err)
	}
	return root, nil
}

/* ==================== 账号 ==================== */

// normalizeAccountRoot 账号根目录归一：反斜杠转斜杠、去掉前导 ./、
// 空值默认用用户名、"." 表示服务端根目录。
func normalizeAccountRoot(rel, username string) (string, error) {
	rel = strings.ReplaceAll(strings.TrimSpace(rel), "\\", "/")
	rel = strings.TrimPrefix(rel, "./")
	rel = strings.Trim(rel, "/")
	if rel == "" {
		rel = username
	}
	if rel == "" || rel == "." {
		return ".", nil
	}
	clean := path.Clean(rel)
	if clean == "." {
		return ".", nil
	}
	if strings.HasPrefix(clean, "/") || strings.Contains(clean, ":") {
		return "", fmt.Errorf("账号目录只能是服务端根目录下的相对路径")
	}
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("账号目录不能包含 ..")
	}
	return clean, nil
}

// accountAbs 账号根目录的绝对路径，并确认它没跑到服务端根目录之外
func accountAbs(srvRoot, rel string) (string, error) {
	abs := srvRoot
	if rel != "" && rel != "." {
		abs = filepath.Join(srvRoot, filepath.FromSlash(rel))
	}
	rootAbs, err := filepath.Abs(srvRoot)
	if err != nil {
		return "", err
	}
	abs, err = filepath.Abs(abs)
	if err != nil {
		return "", err
	}
	r, err := filepath.Rel(rootAbs, abs)
	if err != nil {
		return "", err
	}
	if r == ".." || strings.HasPrefix(r, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("账号目录必须在服务端根目录之内")
	}
	return abs, nil
}

// ListAccounts 账号列表（哈希与明文都不外传）
func ListAccounts() ([]Account, error) {
	serverMu.Lock()
	defer serverMu.Unlock()
	f, err := readServerFile()
	if err != nil {
		return nil, err
	}
	out := make([]Account, 0, len(f.Accounts))
	for _, r := range f.Accounts {
		out = append(out, r.toAccount())
	}
	return out, nil
}

// SaveAccount 新建或更新账号；Password 留空表示沿用原密码。
// 返回更新后的完整列表与这次保存的账号 ID。
func SaveAccount(in Account) ([]Account, string, error) {
	in.Username = strings.TrimSpace(in.Username)
	in.Note = strings.TrimSpace(in.Note)
	if in.Username == "" {
		return nil, "", fmt.Errorf("请填写用户名")
	}
	if len([]rune(in.Username)) > 32 {
		return nil, "", fmt.Errorf("用户名最长 32 个字符")
	}
	if strings.ContainsAny(in.Username, " \t/\\:@") {
		return nil, "", fmt.Errorf("用户名不能包含空格、斜杠、冒号或 @")
	}
	rel, err := normalizeAccountRoot(in.RootDir, in.Username)
	if err != nil {
		return nil, "", err
	}
	in.RootDir = rel

	serverMu.Lock()
	defer serverMu.Unlock()
	f, err := readServerFile()
	if err != nil {
		return nil, "", err
	}

	idx := -1
	for i := range f.Accounts {
		if f.Accounts[i].ID == in.ID {
			idx = i
			break
		}
	}
	for i := range f.Accounts {
		if i != idx && strings.EqualFold(f.Accounts[i].Username, in.Username) {
			return nil, "", fmt.Errorf("用户名 %s 已存在", in.Username)
		}
	}

	rec := accountRec{Account: in}
	if idx >= 0 {
		rec.ID = f.Accounts[idx].ID
		rec.PassHash = f.Accounts[idx].PassHash
	} else {
		if rec.ID == "" {
			rec.ID = fmt.Sprintf("fa%d", time.Now().UnixNano())
		}
		// 新账号默认启用：新建完就能连，勾选反直觉
		rec.Enabled = in.Enabled
		if in.Password == "" {
			return nil, "", fmt.Errorf("请设置登录密码")
		}
	}
	if in.Password != "" {
		if len(in.Password) < 6 {
			return nil, "", fmt.Errorf("密码至少 6 位")
		}
		hash, herr := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
		if herr != nil {
			return nil, "", fmt.Errorf("密码加密失败: %v", herr)
		}
		rec.PassHash = string(hash)
	}
	if rec.PassHash == "" && rec.Enabled {
		return nil, "", fmt.Errorf("请设置登录密码")
	}

	// 账号目录当场建出来：连上去先看到一个空目录，比登录成功却列不出目录好
	srvRoot, err := ServerRoot(f.Config)
	if err != nil {
		return nil, "", err
	}
	abs, err := accountAbs(srvRoot, rec.RootDir)
	if err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, "", fmt.Errorf("创建账号目录失败: %v", err)
	}

	rec.UpdatedAt = time.Now().Unix()
	rec.Password = ""
	if idx >= 0 {
		f.Accounts[idx] = rec
	} else {
		f.Accounts = append(f.Accounts, rec)
	}
	if err := writeServerFile(f); err != nil {
		return nil, "", fmt.Errorf("保存账号失败: %v", err)
	}
	out := make([]Account, 0, len(f.Accounts))
	for _, r := range f.Accounts {
		out = append(out, r.toAccount())
	}
	return out, rec.ID, nil
}

// deleteAccountRec 删除账号记录，返回更新后的列表与被删账号名
func deleteAccountRec(id string) ([]Account, string, error) {
	serverMu.Lock()
	defer serverMu.Unlock()
	f, err := readServerFile()
	if err != nil {
		return nil, "", err
	}
	kept := make([]accountRec, 0, len(f.Accounts))
	name := ""
	for _, r := range f.Accounts {
		if r.ID == id {
			name = r.Username
			continue
		}
		kept = append(kept, r)
	}
	if name == "" {
		return nil, "", fmt.Errorf("账号不存在，可能已被删除")
	}
	if err := writeServerFile(serverFile{Config: f.Config, Accounts: kept}); err != nil {
		return nil, "", fmt.Errorf("保存账号失败: %v", err)
	}
	out := make([]Account, 0, len(kept))
	for _, r := range kept {
		out = append(out, r.toAccount())
	}
	return out, name, nil
}

// findAccount 登录时按用户名取账号与哈希（含禁用状态，由调用方给不同错误文案）
func findAccount(username string) (accountRec, bool) {
	serverMu.Lock()
	defer serverMu.Unlock()
	f, err := readServerFile()
	if err != nil {
		return accountRec{}, false
	}
	for _, r := range f.Accounts {
		if strings.EqualFold(r.Username, username) {
			return r, true
		}
	}
	return accountRec{}, false
}

// accountByUsername 连接建立后取账号当前配置（改权限/改目录后重连即生效）
func accountByUsername(username string) (Account, bool) {
	r, ok := findAccount(username)
	if !ok {
		return Account{}, false
	}
	return r.toAccount(), true
}

// LoadServerConfig 读取服务端配置
func LoadServerConfig() (ServerConfig, error) {
	serverMu.Lock()
	defer serverMu.Unlock()
	f, err := readServerFile()
	if err != nil {
		return ServerConfig{}, err
	}
	return f.Config, nil
}

// SaveServerConfig 保存监听配置（不动账号）
func SaveServerConfig(cfg ServerConfig) (ServerConfig, error) {
	cfg = normalizeServerConfig(cfg)
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return ServerConfig{}, fmt.Errorf("端口需在 1-65535 之间")
	}
	if cfg.RootDir != "" {
		abs, err := filepath.Abs(cfg.RootDir)
		if err != nil {
			return ServerConfig{}, fmt.Errorf("服务端根目录非法: %v", err)
		}
		cfg.RootDir = abs
	}
	serverMu.Lock()
	defer serverMu.Unlock()
	f, err := readServerFile()
	if err != nil {
		return ServerConfig{}, err
	}
	f.Config = cfg
	if err := writeServerFile(f); err != nil {
		return ServerConfig{}, fmt.Errorf("保存服务端配置失败: %v", err)
	}
	return cfg, nil
}

// enabledAccountCount 启用中的账号数（状态栏展示）
func enabledAccountCount() int {
	serverMu.Lock()
	defer serverMu.Unlock()
	f, err := readServerFile()
	if err != nil {
		return 0
	}
	n := 0
	for _, r := range f.Accounts {
		if r.Enabled {
			n++
		}
	}
	return n
}
