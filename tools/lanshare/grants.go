package lanshare

// 常驻互传门户的「授权下载」grants（计划 §四 portal 下载侧）。
//
// 与 link.go 的临时链接互补：链接是「一次一事、带 token、有次数上限」；
// grants 是「显式授权某些本机路径，局域网门户里长期可下」。二者共同点：
// 路径绝不进 URL，未授权一律 404（不泄露路径是否存在）。
//
// grants.json 落 data/lanshare/，原子写，重启后继续有效。目录授权：
// Recursive=false 时只能整包流式 zip 下载，Recursive=true 时可逐项浏览下载。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Grant 一条持久授权。Path 是绝对路径（本机），绝不进 URL。
type Grant struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Name      string `json:"name"`
	Recursive bool   `json:"recursive"` // 目录是否可逐项浏览下载
	ExpiryAt  int64  `json:"expiryAt"`  // UnixMilli，0 = 长期
	CreatedAt int64  `json:"createdAt"`
	Downloads int    `json:"downloads"`
	LastAt    int64  `json:"lastAt,omitempty"`
	Dir       bool   `json:"dir"` // 登记时是否目录（仅展示；实体类型以 stat 为准）
}

// alive 授权是否仍可用：存在、未过期、实体仍在。
func (g *Grant) alive(now time.Time) bool {
	if g == nil {
		return false
	}
	if g.ExpiryAt > 0 && now.UnixMilli() > g.ExpiryAt {
		return false
	}
	if _, err := os.Stat(g.Path); err != nil {
		return false
	}
	return true
}

var grantsMu sync.Mutex

func grantsPath() (string, error) {
	base, err := baseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "grants.json"), nil
}

func loadGrants() ([]Grant, error) {
	p, err := grantsPath()
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
	var out []Grant
	if err := json.Unmarshal(bs, &out); err != nil {
		return nil, fmt.Errorf("grants.json 解析失败: %w", err)
	}
	return out, nil
}

func saveGrants(grants []Grant) error {
	p, err := grantsPath()
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	bs, err := json.MarshalIndent(grants, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(tmp, bs, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// AddGrant 授权一个本机路径。目录可指定 recursive（是否允许逐项浏览下载）。
// hours<=0 表示长期有效。
func AddGrant(path string, recursive bool, hours int) (Grant, error) {
	grantsMu.Lock()
	defer grantsMu.Unlock()

	abs, err := filepath.Abs(path)
	if err != nil {
		return Grant{}, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return Grant{}, fmt.Errorf("路径不可用: %w", err)
	}
	id, err := newToken() // 16B url-safe base64，同 token 生成器
	if err != nil {
		return Grant{}, err
	}
	grants, err := loadGrants()
	if err != nil {
		return Grant{}, err
	}
	g := Grant{
		ID:        id,
		Path:      abs,
		Name:      filepath.Base(abs),
		Recursive: recursive,
		CreatedAt: time.Now().UnixMilli(),
		Dir:       st.IsDir(),
	}
	if hours > 0 {
		g.ExpiryAt = time.Now().Add(time.Duration(hours) * time.Hour).UnixMilli()
	}
	grants = append(grants, g)
	if err := saveGrants(grants); err != nil {
		return Grant{}, err
	}
	return g, nil
}

// ListGrants 全部授权（含已失效项，前端标灰用），按创建时间倒序。
func ListGrants() ([]Grant, error) {
	grantsMu.Lock()
	defer grantsMu.Unlock()
	grants, err := loadGrants()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(grants, func(i, j int) bool { return grants[i].CreatedAt > grants[j].CreatedAt })
	return grants, nil
}

// RevokeGrant 撤销一条授权（只删登记，不动实体）。
func RevokeGrant(id string) error {
	grantsMu.Lock()
	defer grantsMu.Unlock()
	grants, err := loadGrants()
	if err != nil {
		return err
	}
	idx := -1
	for i := range grants {
		if grants[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("授权不存在: %s", id)
	}
	grants = append(grants[:idx], grants[idx+1:]...)
	return saveGrants(grants)
}

// touchGrant 命中一条可用授权并计一次下载（全程持锁，判可用再自增）。
func touchGrant(id string, now time.Time) (*Grant, bool) {
	grantsMu.Lock()
	defer grantsMu.Unlock()
	grants, err := loadGrants()
	if err != nil {
		return nil, false
	}
	for i := range grants {
		if grants[i].ID != id {
			continue
		}
		if !grants[i].alive(now) {
			return nil, false
		}
		grants[i].Downloads++
		grants[i].LastAt = now.UnixMilli()
		g := grants[i]
		_ = saveGrants(grants) // 记账失败不阻断已放行的下载
		return &g, true
	}
	return nil, false
}

// joinGrant 在授权根内解析 rel（防目录穿越，等价 safeJoin）。
// rel 为空返回授权根；穿越或绝对路径返回 error。
func joinGrant(root, rel string) (string, error) {
	rel = strings.TrimLeft(filepath.ToSlash(rel), "/")
	if rel == "" {
		return root, nil
	}
	if filepath.IsAbs(rel) || strings.Contains(rel, "..") {
		return "", fmt.Errorf("非法路径")
	}
	full := filepath.Join(root, filepath.FromSlash(rel))
	cleanRoot := filepath.Clean(root) + string(os.PathSeparator)
	if !strings.HasPrefix(filepath.Clean(full)+string(os.PathSeparator), cleanRoot) {
		return "", fmt.Errorf("路径越界")
	}
	return full, nil
}
