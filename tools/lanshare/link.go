package lanshare

// 临时分享链接：token（crypto/rand）+ 有效期 + 下载次数上限。
// 链接指向的实体不拷贝、不搬家，只登记一条授权；撤销只删登记。
// 落盘 links.json（原子写），进程重启后未过期的链接继续有效。

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// Link 一条分享授权。Path 是绝对路径（本机文件），绝不进 URL。
type Link struct {
	Token     string `json:"token"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	MaxDowns  int    `json:"maxDownloads"` // 0 = 不限次数
	ExpiryAt  int64  `json:"expiryAt"`     // UnixMilli，0 = 不过期
	CreatedAt int64  `json:"createdAt"`
	Downloads int    `json:"downloads"`
	LastAt    int64  `json:"lastAt,omitempty"`
	Bytes     int64  `json:"bytes"`
}

// alive 链接是否仍可用：存在、未过期、次数未用尽、实体仍在。
func (l *Link) alive(now time.Time) bool {
	if l == nil {
		return false
	}
	if l.ExpiryAt > 0 && now.UnixMilli() > l.ExpiryAt {
		return false
	}
	if l.MaxDowns > 0 && l.Downloads >= l.MaxDowns {
		return false
	}
	st, err := os.Stat(l.Path)
	if err != nil || st.IsDir() {
		return false
	}
	return true
}

var linksMu sync.Mutex

func linksPath() (string, error) {
	base, err := baseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "links.json"), nil
}

func loadLinks() ([]Link, error) {
	p, err := linksPath()
	if err != nil {
		return nil, err
	}
	bs, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Link
	if err := json.Unmarshal(bs, &out); err != nil {
		return nil, fmt.Errorf("links.json 解析失败: %w", err)
	}
	return out, nil
}

func saveLinks(links []Link) error {
	p, err := linksPath()
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	bs, err := json.MarshalIndent(links, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, bs, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// newToken 16 字节随机数 → URL 安全 base64（22 字符，无填充）。
func newToken() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// CreateLink 为本地文件登记一条分享链接。
// hours<=0 表示不过期；maxDownloads<=0 表示不限次数。
func CreateLink(path string, hours, maxDownloads int) (Link, error) {
	linksMu.Lock()
	defer linksMu.Unlock()

	abs, err := filepath.Abs(path)
	if err != nil {
		return Link{}, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return Link{}, fmt.Errorf("文件不可用: %w", err)
	}
	if st.IsDir() {
		return Link{}, fmt.Errorf("暂不支持分享目录（需要打包，走 tools/archive 后续接入）")
	}
	token, err := newToken()
	if err != nil {
		return Link{}, err
	}
	links, err := loadLinks()
	if err != nil {
		return Link{}, err
	}
	l := Link{
		Token:     token,
		Path:      abs,
		Name:      filepath.Base(abs),
		MaxDowns:  maxDownloads,
		CreatedAt: time.Now().UnixMilli(),
		Bytes:     st.Size(),
	}
	if hours > 0 {
		l.ExpiryAt = time.Now().Add(time.Duration(hours) * time.Hour).UnixMilli()
	}
	links = append(links, l)
	if err := saveLinks(links); err != nil {
		return Link{}, err
	}
	return l, nil
}

// ListLinks 全部链接（含已失效项，前端标灰用），按创建时间倒序。
func ListLinks() ([]Link, error) {
	linksMu.Lock()
	defer linksMu.Unlock()
	links, err := loadLinks()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(links, func(i, j int) bool { return links[i].CreatedAt > links[j].CreatedAt })
	return links, nil
}

// RevokeLink 撤销一条链接（只删登记，不动实体文件）。
func RevokeLink(token string) error {
	linksMu.Lock()
	defer linksMu.Unlock()
	links, err := loadLinks()
	if err != nil {
		return err
	}
	idx := -1
	for i := range links {
		if links[i].Token == token {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("链接不存在: %s", token)
	}
	links = append(links[:idx], links[idx+1:]...)
	return saveLinks(links)
}

// touchLink 记一次下载并落盘；返回本次是否被允许（并发下以这份实现为准：
// 先判可用再自增，全程持锁，宁可慢也不超发）。
func touchLink(token string, now time.Time) (*Link, bool) {
	linksMu.Lock()
	defer linksMu.Unlock()
	links, err := loadLinks()
	if err != nil {
		return nil, false
	}
	for i := range links {
		if links[i].Token != token {
			continue
		}
		if !links[i].alive(now) {
			return nil, false
		}
		links[i].Downloads++
		links[i].LastAt = now.UnixMilli()
		l := links[i]
		_ = saveLinks(links) // 记账失败不阻断已放行的下载
		return &l, true
	}
	return nil, false
}
