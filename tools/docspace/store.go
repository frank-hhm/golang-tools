package docspace

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"Tools/appdirs"
)

// ============================================================
// 文档中心的 JSON 持久化层
//
// 落点全在 appdirs.Data("docspace")（默认 <应用目录>/data/docspace）：
//   - taxonomy.json          分类树（扁平存 parent 指针，层级即级别）
//   - items/<shard>.json     条目元数据，按分类分片以推迟单文件规模上限
//   - trash.json             回收站登记（记原路径可还原）
//   - secure.json            加密登记（只存 salt 与校验哈希，口令不落盘）
//   - shares.json            分享任务（链接由 tools/lanshare 生成，这里只登记）
//   - backups.json           备份计划
// 实体目录 versions/ trash/ secure/ 由后续期填充；本层只管元数据。
//
// 一律 JSON + 原子写（.part 再改名），沿用 services/config 的防抹范式；
// 不引数据库（AGENTS 禁止）。整个 Store 由一把 RWMutex 守护，读写都过它。
// ============================================================

// Category 分类节点：Parent 空表示根节点。层级即密级/类别级别。
type Category struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Parent string `json:"parent,omitempty"`
	Sort   int    `json:"sort"`
}

// Item 被纳管的文件条目。文件本体留在原处（Path），这里只存元数据与归属。
type Item struct {
	ID        string   `json:"id"`
	Src       string   `json:"src"`
	SessionID string   `json:"sid,omitempty"`
	Path      string   `json:"path"`
	Name      string   `json:"name"`
	Category  string   `json:"category"`
	Tags      []string `json:"tags,omitempty"`
	Version   int      `json:"version"`
	CreatedAt int64    `json:"createdAt"`
	UpdatedAt int64    `json:"updatedAt"`
}

// Ref 还原成统一文件源引用，供预览/编辑/Range 分发。
func (it *Item) Ref() Ref {
	return Ref{Src: it.Src, SessionID: it.SessionID, Path: it.Path}
}

// TrashEntry 回收站条目：实体已移入 trash/<id>/<原名>，Origin 记还原目标。
// Src/RelPath/Category/Sid 保留原条目的归属，还原时据此重新登记 Item。
type TrashEntry struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Origin    string `json:"origin"`
	DeletedAt int64  `json:"deletedAt"`
	Src       string `json:"src,omitempty"`
	RelPath   string `json:"relPath,omitempty"`
	Sid       string `json:"sid,omitempty"`
	Category  string `json:"category,omitempty"`
}

// SecureEntry 加密登记：密文落 secure/<id>.gtd，只存 salt 与校验哈希。
type SecureEntry struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	Salt      string `json:"salt"`
	Check     string `json:"check"`
	CreatedAt int64  `json:"createdAt"`
}

// ShareEntry 分享登记（实际监听与链接生成归 tools/lanshare）。
type ShareEntry struct {
	ID        string `json:"id"`
	Ref       Ref    `json:"ref"`
	Link      string `json:"link"`
	ExpiresAt int64  `json:"expiresAt,omitempty"`
	CreatedAt int64  `json:"createdAt"`
}

// BackupPlan 备份计划：Src 目录按 sha256 增量拷到 Dst（本地或异地目录）。
// Enabled && IntervalS>0 且全局 BackupAuto 开时才会被调度器自动跑。
type BackupPlan struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Src        string `json:"src"`
	Dst        string `json:"dst"`
	IntervalS  int    `json:"intervalSec"`
	Enabled    bool   `json:"enabled"`
	LastRunAt  int64  `json:"lastRunAt"`
	LastStatus string `json:"lastStatus"`
	Copied     int    `json:"copied"`
	Skipped    int    `json:"skipped"`
}

// Store 元数据总入口。惰性加载一次，之后所有读写过 mu。
type Store struct {
	mu       sync.RWMutex
	root     string
	loaded   bool
	taxonomy []Category
	items    map[string]*Item
	trash    []TrashEntry
	secure   []SecureEntry
	shares   []ShareEntry
	backups  []BackupPlan
}

var (
	globalOnce  sync.Once
	globalStore *Store
	globalErr   error
)

// Global 返回进程内单例 Store（首次调用时解析目录并加载）。
func Global() (*Store, error) {
	globalOnce.Do(func() {
		root, err := appdirs.Data("docspace")
		if err != nil {
			globalErr = err
			return
		}
		globalStore = &Store{root: root, items: map[string]*Item{}}
		globalErr = globalStore.ensureDirs()
	})
	if globalErr != nil {
		return nil, globalErr
	}
	return globalStore, nil
}

// ensureDirs 预建实体子目录（幂等）。
func (s *Store) ensureDirs() error {
	for _, d := range []string{"items", "versions", "trash", "secure"} {
		if err := os.MkdirAll(filepath.Join(s.root, d), 0o755); err != nil {
			return err
		}
	}
	// 首启清扫加密临时明文：上次进程崩溃可能把解出的明文留在 secure/tmp 下，开机即删。
	_ = os.RemoveAll(filepath.Join(s.root, "secure", "tmp"))
	return nil
}

// Root 库根目录（设置页展示与实体落点用）。
func (s *Store) Root() string { return s.root }

// pathOf 库内相对路径的绝对化。
func (s *Store) pathOf(rel ...string) string {
	return filepath.Join(append([]string{s.root}, rel...)...)
}

// Load 从磁盘载入全部元数据（幂等，只真正读一次）。
func (s *Store) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}
	if err := loadJSON(s.pathOf("taxonomy.json"), &s.taxonomy); err != nil {
		return err
	}
	if err := s.loadItems(); err != nil {
		return err
	}
	if err := loadJSON(s.pathOf("trash.json"), &s.trash); err != nil {
		return err
	}
	if err := loadJSON(s.pathOf("secure.json"), &s.secure); err != nil {
		return err
	}
	if err := loadJSON(s.pathOf("shares.json"), &s.shares); err != nil {
		return err
	}
	if err := loadJSON(s.pathOf("backups.json"), &s.backups); err != nil {
		return err
	}
	s.loaded = true
	return nil
}

// loadItems 读 items/ 下所有分片，合并进内存表。
func (s *Store) loadItems() error {
	if s.items == nil {
		s.items = map[string]*Item{}
	}
	entries, err := os.ReadDir(s.pathOf("items"))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		var shard []*Item
		if err := loadJSON(filepath.Join(s.pathOf("items"), e.Name()), &shard); err != nil {
			return err
		}
		for _, it := range shard {
			if it != nil && it.ID != "" {
				s.items[it.ID] = it
			}
		}
	}
	return nil
}

// saveItems 按分类重写所有分片（先清旧分片再写，保证删除项不残留）。
// 调用方须持写锁。
func (s *Store) saveItems() error {
	dir := s.pathOf("items")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if old, err := filepath.Glob(filepath.Join(dir, "*.json")); err == nil {
		for _, f := range old {
			_ = os.Remove(f)
		}
	}
	groups := map[string][]*Item{}
	for _, it := range s.items {
		key := it.Category
		if key == "" {
			key = "_uncat"
		}
		groups[key] = append(groups[key], it)
	}
	for key, list := range groups {
		sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
		if err := writeJSONAtomic(filepath.Join(dir, shardName(key)+".json"), list); err != nil {
			return err
		}
	}
	return nil
}

// shardName 把分类 id 收敛成安全文件名（只留字母数字下划线连字符）。
func shardName(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "_empty"
	}
	return b.String()
}

// ---------- 查询 ----------

// Categories 返回分类（按 Sort 稳定排序）。
func (s *Store) Categories() ([]Category, error) {
	if err := s.Load(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Category, len(s.taxonomy))
	copy(out, s.taxonomy)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Sort < out[j].Sort })
	return out, nil
}

// Items 返回全部条目（按创建时间升序）。
func (s *Store) Items() ([]Item, error) {
	if err := s.Load(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Item, 0, len(s.items))
	for _, it := range s.items {
		out = append(out, *it)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CreatedAt != out[j].CreatedAt {
			return out[i].CreatedAt < out[j].CreatedAt
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Counts 汇总各类条目数（前端概览与开关判断用）。
type Counts struct {
	Categories int `json:"categories"`
	Items      int `json:"items"`
	Trash      int `json:"trash"`
	Secure     int `json:"secure"`
	Shares     int `json:"shares"`
	Backups    int `json:"backups"`
}

func (s *Store) Counts() (Counts, error) {
	if err := s.Load(); err != nil {
		return Counts{}, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return Counts{
		Categories: len(s.taxonomy),
		Items:      len(s.items),
		Trash:      len(s.trash),
		Secure:     len(s.secure),
		Shares:     len(s.shares),
		Backups:    len(s.backups),
	}, nil
}

// ---------- 分类 CRUD ----------

// AddCategory 新增分类并落盘，返回带 id 的节点。
func (s *Store) AddCategory(name, parent string) (Category, error) {
	if err := s.Load(); err != nil {
		return Category{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := Category{ID: newID("c"), Name: strings.TrimSpace(name), Parent: parent, Sort: len(s.taxonomy)}
	s.taxonomy = append(s.taxonomy, c)
	if err := writeJSONAtomic(s.pathOf("taxonomy.json"), s.taxonomy); err != nil {
		s.taxonomy = s.taxonomy[:len(s.taxonomy)-1]
		return Category{}, err
	}
	return c, nil
}

// RemoveCategory 删除分类（其下条目退为未分类），落盘。
func (s *Store) RemoveCategory(id string) error {
	if err := s.Load(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.taxonomy[:0]
	for _, c := range s.taxonomy {
		if c.ID != id {
			kept = append(kept, c)
		}
	}
	s.taxonomy = kept
	for _, it := range s.items {
		if it.Category == id {
			it.Category = ""
		}
	}
	if err := writeJSONAtomic(s.pathOf("taxonomy.json"), s.taxonomy); err != nil {
		return err
	}
	return s.saveItems()
}

// ---------- 条目 CRUD ----------

// AddItem 纳管一个文件（原地为主，不拷贝）：登记元数据并落分片。
func (s *Store) AddItem(ref Ref, category string) (Item, error) {
	if err := s.Load(); err != nil {
		return Item{}, err
	}
	if ref.Src == "" {
		ref.Src = SrcLocal
	}
	now := time.Now().UnixMilli()
	it := &Item{
		ID:        newID("i"),
		Src:       ref.Src,
		SessionID: ref.SessionID,
		Path:      ref.Path,
		Name:      filepath.Base(filepath.FromSlash(ref.Path)),
		Category:  category,
		Version:   0,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[it.ID] = it
	if err := s.saveItems(); err != nil {
		delete(s.items, it.ID)
		return Item{}, err
	}
	cp := *it
	return cp, nil
}

// RemoveItem 取消纳管（不动磁盘上的原文件），落分片。
func (s *Store) RemoveItem(id string) error {
	if err := s.Load(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	old, ok := s.items[id]
	if !ok {
		return nil
	}
	delete(s.items, id)
	if err := s.saveItems(); err != nil {
		s.items[id] = old
		return err
	}
	return nil
}

// ---------- 原子 JSON 读写 ----------

// loadJSON 读一个 JSON 文件到 v；文件不存在视为空（v 保持零值），不算错。
func loadJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return nil
	}
	return json.Unmarshal(b, v)
}

// writeJSONAtomic 缩进序列化后先写 .part 再改名替换，避免半写毁掉原文件。
func writeJSONAtomic(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	part := path + ".part"
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		_ = os.Remove(part)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(part)
		return err
	}
	if err := os.Rename(part, path); err != nil {
		// Windows 改名不覆盖已存在目标：先删再换
		if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
			_ = os.Remove(part)
			return rmErr
		}
		return os.Rename(part, path)
	}
	return nil
}

// newID 生成带前缀的稳定 ID：前缀 + 纳秒时间 + 4 字节随机（十六进制）。
func newID(prefix string) string {
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%s%d", prefix, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s%d%s", prefix, time.Now().UnixNano(), hex.EncodeToString(b[:]))
}
