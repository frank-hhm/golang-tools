// Package panel 提供 Linux / Web 部署时"面板自身"的访问控制与运行参数：
// 账号密码登录、会话 Cookie、安全入口路径、监听地址与端口。
//
// 三个设计约束：
//  1. 配置独立存放在 <用户配置目录>/go-tools/panel.json，不并入 AppConfig——
//     密码哈希不能随 GetConfig 这类绑定方法下发给前端。
//  2. 认证必须做在 HTTP 层而不是前端路由：前端资源与 /wails/runtime 上的
//     四百多个绑定方法（能装包、能起进程、能开 shell）是同一批入口，
//     只挡页面等于没挡。
//  3. 端口/入口/账号密码改动要能即时生效。面板服务是自己起的 net/http，
//     改端口就是重新 Listen，不需要重启整个进程（服务里跑着 MySQL/Redis 实例）。
package panel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"go-tools/services/config"

	"golang.org/x/crypto/bcrypt"
)

// 默认值：监听所有网卡（面板通常装在服务器上远程访问），端口沿用 Wails 惯例 8080。
const (
	defaultHost         = "0.0.0.0"
	defaultPort         = 8080
	defaultUsername     = "admin"
	defaultSessionHours = 12
	minSessionHours     = 1
	maxSessionHours     = 24 * 30
)

// entryPattern 安全入口路径格式：/ + 4~32 位字母数字下划线横线（不含斜杠，便于整段匹配）
var entryPattern = regexp.MustCompile(`^/[A-Za-z0-9_-]{4,32}$`)

// Settings 面板访问控制配置。字段全部有 JSON 标签，直接落 panel.json。
type Settings struct {
	Enabled      bool   `json:"enabled"`      // 是否开启登录；关闭后等同无鉴权（只建议内网调试用）
	Host         string `json:"host"`         // 监听地址，0.0.0.0 = 所有网卡，127.0.0.1 = 只允许本机
	Port         int    `json:"port"`         // 对外监听端口
	EntryPath    string `json:"entryPath"`    // 安全入口路径，如 "/a1b2c3"；空 = 不启用
	Username     string `json:"username"`     // 登录账号
	PasswordHash string `json:"passwordHash"` // bcrypt 哈希，永不明文
	SessionHours int    `json:"sessionHours"` // 登录有效期（小时）
	Epoch        int    `json:"epoch"`        // 会话代数：改密码/改账号时 +1，旧 Cookie 全部失效
	Secret       string `json:"secret"`       // Cookie 签名密钥（随机 32 字节 hex）
	UpdatedAt    string `json:"updatedAt"`    // 最近一次修改时间（展示用）
}

var (
	settingsMu sync.RWMutex
	cached     Settings
	cachedSet  bool
)

// Path 返回 panel.json 的绝对路径（目录不存在则创建）。
func Path() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "panel.json"), nil
}

// Default 返回一份可直接使用的默认配置（不含密码哈希，调用方需再 SetPassword）。
func Default() Settings {
	return Settings{
		Enabled:      true,
		Host:         defaultHost,
		Port:         defaultPort,
		Username:     defaultUsername,
		SessionHours: defaultSessionHours,
		Epoch:        1,
	}
}

// Load 读取面板配置。
//
// 文件不存在时落一份默认配置，并生成随机初始密码——返回值 generated 非空表示
// "这是首次生成的管理员密码"，主程序应打印出来（与宝塔面板首次安装一致）。
// 已有配置文件时 generated 恒为空串。
func Load() (s Settings, generated string, err error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return loadLocked()
}

func loadLocked() (s Settings, generated string, err error) {
	p, err := Path()
	if err != nil {
		return Settings{}, "", err
	}
	data, rerr := os.ReadFile(p)
	if rerr != nil {
		if !os.IsNotExist(rerr) {
			return Settings{}, "", rerr
		}
		s = Default()
		generated = randomPassword()
		if err := s.setPasswordLocked(generated); err != nil {
			return Settings{}, "", err
		}
		if err := ensureSecret(&s); err != nil {
			return Settings{}, "", err
		}
		s.UpdatedAt = now()
		if err := writeLocked(s); err != nil {
			return Settings{}, "", err
		}
		copyToCache(s)
		return s, generated, nil
	}

	if err := json.Unmarshal(data, &s); err != nil {
		return Settings{}, "", fmt.Errorf("解析 %s 失败: %w", p, err)
	}
	normalize(&s)
	// 老配置可能没有密钥（比如手工删了字段）：补一个并回写
	if s.Secret == "" {
		if err := ensureSecret(&s); err != nil {
			return Settings{}, "", err
		}
		if err := writeLocked(s); err != nil {
			return Settings{}, "", err
		}
	}
	copyToCache(s)
	return s, "", nil
}

// Current 返回当前生效的配置副本（未加载过则读盘，保证 CLI 与运行期一致）。
func Current() Settings {
	settingsMu.RLock()
	if cachedSet {
		s := cached
		settingsMu.RUnlock()
		return s
	}
	settingsMu.RUnlock()
	s, _, err := Load()
	if err != nil {
		return Default()
	}
	return s
}

// Update 在写锁内读—改—写配置并落盘，返回写入后的配置。
func Update(mutate func(*Settings) error) (Settings, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()

	base := cached
	if !cachedSet {
		var err error
		base, _, err = loadLocked()
		if err != nil {
			return Settings{}, err
		}
	}
	s := base
	if err := mutate(&s); err != nil {
		return Settings{}, err
	}
	normalize(&s)
	if err := writeLocked(s); err != nil {
		return Settings{}, err
	}
	copyToCache(s)
	return s, nil
}

// Save 校验并整体保存（HTTP 接口收到新配置时用）。
func Save(s Settings) (Settings, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()

	normalize(&s)
	if err := validate(s); err != nil {
		return Settings{}, err
	}
	if err := ensureSecret(&s); err != nil {
		return Settings{}, err
	}
	s.UpdatedAt = now()
	if err := writeLocked(s); err != nil {
		return Settings{}, err
	}
	copyToCache(s)
	return s, nil
}

// CheckPassword 校验明文密码。
func (s Settings) CheckPassword(plain string) bool {
	if s.PasswordHash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(s.PasswordHash), []byte(plain)) == nil
}

// SetPassword 更新密码哈希，并推进会话代数（所有已登录设备需重新登录）。
func SetPassword(plain string) (Settings, error) {
	if len(strings.TrimSpace(plain)) < 6 {
		return Settings{}, fmt.Errorf("密码至少 6 位")
	}
	return Update(func(s *Settings) error {
		if err := s.setPasswordLocked(plain); err != nil {
			return err
		}
		s.Epoch++
		s.UpdatedAt = now()
		return nil
	})
}

func (s *Settings) setPasswordLocked(plain string) error {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	s.PasswordHash = string(h)
	return nil
}

// ListenAddr 返回 net.Listen 用的地址串。
func (s Settings) ListenAddr() string {
	return net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
}

// AccessURL 返回给用户看的访问地址（0.0.0.0 换成"服务器IP"占位）。
func (s Settings) AccessURL() string {
	host := s.Host
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "<服务器IP>"
	}
	// 不带尾部斜杠：安全入口是"精确匹配一个路径"，写成 /manage-xxx/ 会落到 404
	path := s.EntryPath
	if path == "" {
		path = "/"
	}
	return fmt.Sprintf("http://%s:%d%s", host, s.Port, path)
}

func normalize(s *Settings) {
	s.Host = strings.TrimSpace(s.Host)
	if s.Host == "" {
		s.Host = defaultHost
	}
	if s.Port == 0 {
		s.Port = defaultPort
	}
	s.Username = strings.TrimSpace(s.Username)
	if s.Username == "" {
		s.Username = defaultUsername
	}
	if s.SessionHours == 0 {
		s.SessionHours = defaultSessionHours
	}
	if s.SessionHours < minSessionHours {
		s.SessionHours = minSessionHours
	}
	if s.SessionHours > maxSessionHours {
		s.SessionHours = maxSessionHours
	}
	s.EntryPath = normalizeEntry(s.EntryPath)
	if s.Epoch <= 0 {
		s.Epoch = 1
	}
}

// normalizeEntry 统一安全入口写法：补前导斜杠、去尾部斜杠，"off" 视为关闭。
func normalizeEntry(v string) string {
	v = strings.TrimSpace(v)
	switch strings.ToLower(v) {
	case "", "off", "none", "disable", "0", "-":
		return ""
	}
	if !strings.HasPrefix(v, "/") {
		v = "/" + v
	}
	return strings.TrimRight(v, "/")
}

// ValidateEntry 供接口层校验用户输入的安全入口（空串表示关闭）。
func ValidateEntry(v string) error {
	v = normalizeEntry(v)
	if v == "" {
		return nil
	}
	if !entryPattern.MatchString(v) {
		return fmt.Errorf("安全入口需以 / 开头，后接 4~32 位字母、数字、下划线或横线，例如 /mypanel2026")
	}
	return nil
}

func validate(s Settings) error {
	if strings.TrimSpace(s.Username) == "" {
		return fmt.Errorf("账号不能为空")
	}
	if len(s.Username) > 64 {
		return fmt.Errorf("账号过长（最多 64 字符）")
	}
	if s.Port < 1 || s.Port > 65535 {
		return fmt.Errorf("端口需在 1~65535 之间")
	}
	if strings.TrimSpace(s.Host) == "" {
		return fmt.Errorf("监听地址不能为空（0.0.0.0 表示所有网卡）")
	}
	if s.PasswordHash == "" {
		return fmt.Errorf("密码未设置")
	}
	return ValidateEntry(s.EntryPath)
}

func writeLocked(s Settings) error {
	p, err := Path()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	// 密码哈希与签名密钥：仅本用户可读
	return os.WriteFile(p, data, 0o600)
}

func copyToCache(s Settings) {
	cached = s
	cachedSet = true
}

func ensureSecret(s *Settings) error {
	if s.Secret != "" {
		return nil
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return err
	}
	s.Secret = hex.EncodeToString(buf)
	return nil
}

// randomPassword 生成 16 位随机初始密码（去掉易混淆的 0/O/1/l/I）。
func randomPassword() string {
	const alphabet = "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		// 极端的熵源故障：退化成时间戳，至少不会给出固定密码
		return fmt.Sprintf("gt-%d", time.Now().UnixNano())
	}
	out := make([]byte, len(buf))
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out)
}

// GeneratePassword 给 CLI 的 reset 命令用。
func GeneratePassword() string { return randomPassword() }

func now() string { return time.Now().Format("2006-01-02 15:04:05") }
