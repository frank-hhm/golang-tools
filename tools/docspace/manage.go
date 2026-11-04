package docspace

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"Tools/services/sysx"
)

// ============================================================
// 文档中心 P1：标签、版本快照、回收站
//
// 三者都建立在 store.go 的元数据层之上，文件本体仍「原地为主」：
//   - 标签：Item.Tags 多值，规范化去重；标签词表由条目派生（不单独存表）。
//   - 版本：快照实体落 versions/<id>/<n>.<ext>，manifest.json 记源路径/mtime/size/sha256/备注；
//     还原 = 写回原路径，且写回前先把「当前内容」自动存一版，避免还原把未保存的改动丢掉。
//   - 回收站：仅对 local 源移入实体到 trash/<id>/<原名> 并记 origin 可还原；
//     sftp 远端「移入本机回收站」语义不成立，直接返回 ErrUnsupported（磁盘页交系统回收站）。
//
// 全程零 cgo、跨平台：路径统一用 filepath，落到 appdirs.Data("docspace") 下。
// ============================================================

// snapshotMaxBytes 单文件快照体积上限：超过不吞进内存，挡一下超大文件。
const snapshotMaxBytes = 128 << 20 // 128MB

// Version 一次版本快照的元数据（实体在 versions/<id>/ 下）。
type Version struct {
	N         int    `json:"n"`         // 版本号（从 1 起，单调）
	File      string `json:"file"`      // 相对库根的快照文件路径
	Size      int64  `json:"size"`      // 快照字节数
	SHA256    string `json:"sha256"`    // 快照内容哈希
	Note      string `json:"note"`      // 备注（手动/自动/还原前）
	SrcMTime  int64  `json:"srcMtime"`  // 快照时源文件 mtime（毫秒）
	CreatedAt int64  `json:"createdAt"` // 快照生成时间（毫秒）
}

// ---------- 标签 ----------

// normalizeTags 去空白、去重（保留首次出现顺序）、丢弃空串。
func normalizeTags(tags []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return out
}

// SetItemTags 覆盖设置某条目的标签，落分片。
func (s *Store) SetItemTags(itemID string, tags []string) error {
	if err := s.Load(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[itemID]
	if !ok {
		return fmt.Errorf("条目不存在: %s", itemID)
	}
	it.Tags = normalizeTags(tags)
	it.UpdatedAt = time.Now().UnixMilli()
	return s.saveItems()
}

// AllTags 返回全部条目派生出的标签词表（按字母序稳定）。
func (s *Store) AllTags() ([]string, error) {
	if err := s.Load(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	set := map[string]bool{}
	for _, it := range s.items {
		for _, t := range it.Tags {
			set[t] = true
		}
	}
	out := make([]string, 0, len(set))
	for t := range set {
		out = append(out, t)
	}
	sort.Strings(out)
	return out, nil
}

// ---------- 版本 ----------

func (s *Store) versionDir(itemID string) string {
	return filepath.Join(s.root, "versions", itemID)
}

func (s *Store) manifestPath(itemID string) string {
	return filepath.Join(s.versionDir(itemID), "manifest.json")
}

// readVersions 直接读磁盘 manifest（调用方持锁，不再走 Load）。
func (s *Store) readVersions(itemID string) []Version {
	var v []Version
	_ = loadJSON(s.manifestPath(itemID), &v)
	return v
}

// ListVersions 返回某条目的全部版本快照（按版本号升序）。
func (s *Store) ListVersions(itemID string) ([]Version, error) {
	if err := s.Load(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	vers := s.readVersions(itemID)
	sort.Slice(vers, func(i, j int) bool { return vers[i].N < vers[j].N })
	out := make([]Version, len(vers))
	copy(out, vers)
	return out, nil
}

// SnapshotItem 读取条目当前内容并存为一版快照，返回新版本。
// 体积超上限、条目不存在、源不可读都会失败（不静默）。
// keep>0 时存完后把该条目最旧的超额快照裁掉（连带删实体文件），<=0 不裁剪。
func (s *Store) SnapshotItem(itemID, note string, keep int) (Version, error) {
	if err := s.Load(); err != nil {
		return Version{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[itemID]
	if !ok {
		return Version{}, fmt.Errorf("条目不存在: %s", itemID)
	}
	ref := it.Ref()
	data, err := ref.ReadAll(snapshotMaxBytes)
	if err != nil {
		return Version{}, err
	}
	var mtime int64
	if st, err := ref.Stat(); err == nil {
		mtime = st.ModTime().UnixMilli()
	}
	n := it.Version + 1
	ext := strings.ToLower(filepath.Ext(it.Name))
	fname := fmt.Sprintf("%d%s", n, ext)
	fpath := filepath.Join(s.versionDir(itemID), fname)
	if err := os.MkdirAll(s.versionDir(itemID), 0o755); err != nil {
		return Version{}, err
	}
	if err := os.WriteFile(fpath, data, 0o644); err != nil {
		return Version{}, err
	}
	sum := sha256.Sum256(data)
	rel := filepath.ToSlash(filepath.Join("versions", itemID, fname))
	ver := Version{
		N:         n,
		File:      rel,
		Size:      int64(len(data)),
		SHA256:    hex.EncodeToString(sum[:]),
		Note:      strings.TrimSpace(note),
		SrcMTime:  mtime,
		CreatedAt: time.Now().UnixMilli(),
	}
	vers := upsertVersion(s.readVersions(itemID), ver)
	vers, pruned := pruneVersions(vers, keep)
	for _, p := range pruned {
		_ = os.Remove(filepath.Join(s.root, filepath.FromSlash(p.File)))
	}
	if err := writeJSONAtomic(s.manifestPath(itemID), vers); err != nil {
		_ = os.Remove(fpath)
		return Version{}, err
	}
	it.Version = n
	it.UpdatedAt = ver.CreatedAt
	if err := s.saveItems(); err != nil {
		return Version{}, err
	}
	return ver, nil
}

// RestoreVersion 把某版本快照写回原路径；写回前先自动存一版当前内容（防止丢改动）。
func (s *Store) RestoreVersion(itemID string, n int) error {
	if err := s.Load(); err != nil {
		return err
	}
	// 1) 锁内定位条目与目标快照，取出待还原字节
	s.mu.Lock()
	it, ok := s.items[itemID]
	if !ok {
		s.mu.Unlock()
		return fmt.Errorf("条目不存在: %s", itemID)
	}
	var target *Version
	vers := s.readVersions(itemID)
	for i := range vers {
		if vers[i].N == n {
			target = &vers[i]
			break
		}
	}
	if target == nil {
		s.mu.Unlock()
		return fmt.Errorf("版本不存在: %s v%d", itemID, n)
	}
	absSnap := filepath.Join(s.root, filepath.FromSlash(target.File))
	ref := it.Ref()
	s.mu.Unlock()

	data, err := os.ReadFile(absSnap)
	if err != nil {
		return fmt.Errorf("读取快照失败: %w", err)
	}

	// 2) 还原前先把「当前内容」自动存一版
	if _, err := s.SnapshotItem(itemID, fmt.Sprintf("还原 v%d 前自动保存", n), 0); err != nil {
		return err
	}

	// 3) 写回原路径
	if err := ref.WriteAll(data); err != nil {
		return fmt.Errorf("写回原路径失败: %w", err)
	}
	s.mu.Lock()
	if cur, ok := s.items[itemID]; ok {
		cur.UpdatedAt = time.Now().UnixMilli()
		_ = s.saveItems()
	}
	s.mu.Unlock()
	return nil
}

// upsertVersion 覆盖同版本号或追加，返回按版本号升序的切片。
func upsertVersion(vers []Version, v Version) []Version {
	out := make([]Version, 0, len(vers)+1)
	replaced := false
	for _, x := range vers {
		if x.N == v.N {
			out = append(out, v)
			replaced = true
			continue
		}
		out = append(out, x)
	}
	if !replaced {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].N < out[j].N })
	return out
}

// pruneVersions 只保留最新 keep 个版本（按版本号），返回保留列表与被裁掉的版本。
// keep<=0 表示不裁剪，原样返回、被裁列表为空。
func pruneVersions(vers []Version, keep int) ([]Version, []Version) {
	if keep <= 0 || len(vers) <= keep {
		return vers, nil
	}
	sort.Slice(vers, func(i, j int) bool { return vers[i].N > vers[j].N }) // 新→旧
	keepList := vers[:keep]
	dropped := vers[keep:]
	sort.Slice(keepList, func(i, j int) bool { return keepList[i].N < keepList[j].N }) // 还原为升序
	out := make([]Version, len(keepList))
	copy(out, keepList)
	drop := make([]Version, len(dropped))
	copy(drop, dropped)
	return out, drop
}

// ---------- 回收站 ----------

// MoveToTrash 把 local 条目的实体移入库内回收站并记 origin，条目从 items 移除。
// sftp 源不支持（远端文件不能移进本机回收站），返回 sysx.ErrUnsupported。
//
// 安全契约（借鉴 DeskBox 的文件安全口径）：是移动不是删除——实体只换位置，
// 内容不动、不写副本；除用户显式彻底删除/超龄清理外永不删源；
// 还原时原路径被占用则拒绝覆盖，实体保留在回收站里等用户处置。
func (s *Store) MoveToTrash(itemID string) error {
	if err := s.Load(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.items[itemID]
	if !ok {
		return fmt.Errorf("条目不存在: %s", itemID)
	}
	if it.Src != SrcLocal {
		return sysx.ErrUnsupported
	}
	origin, err := filepath.Abs(it.Path)
	if err != nil {
		return err
	}
	if st, err := os.Stat(origin); err != nil {
		return err
	} else if st.IsDir() {
		return fmt.Errorf("暂不支持把目录移入文档回收站: %s", origin)
	}
	destDir := filepath.Join(s.root, "trash", itemID)
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(destDir, it.Name)
	if err := os.Rename(origin, dest); err != nil {
		return fmt.Errorf("移入回收站失败: %w", err)
	}
	entry := TrashEntry{
		ID:        itemID,
		Name:      it.Name,
		Origin:    origin,
		DeletedAt: time.Now().UnixMilli(),
		Src:       it.Src,
		RelPath:   it.Path,
		Sid:       it.SessionID,
		Category:  it.Category,
	}
	s.trash = upsertTrash(s.trash, entry)
	delete(s.items, itemID)
	if err := s.saveTrash(); err != nil {
		return err
	}
	return s.saveItems()
}

// ListTrash 返回回收站条目（按删除时间倒序，最近的在前）。
func (s *Store) ListTrash() ([]TrashEntry, error) {
	if err := s.Load(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]TrashEntry, len(s.trash))
	copy(out, s.trash)
	sort.SliceStable(out, func(i, j int) bool { return out[i].DeletedAt > out[j].DeletedAt })
	return out, nil
}

// RestoreTrash 把回收站实体移回原路径，并重新登记为条目（沿用原分类）。
// 原路径已被占用时失败，不覆盖既有文件。
func (s *Store) RestoreTrash(entryID string) (Item, error) {
	if err := s.Load(); err != nil {
		return Item{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, t := range s.trash {
		if t.ID == entryID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Item{}, fmt.Errorf("回收站条目不存在: %s", entryID)
	}
	te := s.trash[idx]
	dest := filepath.Join(s.root, "trash", te.ID, te.Name)
	if _, err := os.Stat(dest); err != nil {
		return Item{}, fmt.Errorf("回收站实体缺失: %w", err)
	}
	if _, err := os.Stat(te.Origin); err == nil {
		return Item{}, fmt.Errorf("原路径已存在同名文件，未覆盖: %s", te.Origin)
	}
	if err := os.MkdirAll(filepath.Dir(te.Origin), 0o755); err != nil {
		return Item{}, err
	}
	if err := os.Rename(dest, te.Origin); err != nil {
		return Item{}, fmt.Errorf("还原失败: %w", err)
	}
	// 实体目录移空后清掉
	_ = os.Remove(filepath.Join(s.root, "trash", te.ID))

	s.trash = append(s.trash[:idx], s.trash[idx+1:]...)
	if err := s.saveTrash(); err != nil {
		return Item{}, err
	}

	// 重新登记条目（新 id；分类沿用）
	now := time.Now().UnixMilli()
	src := te.Src
	if src == "" {
		src = SrcLocal
	}
	path := te.RelPath
	if path == "" {
		path = te.Origin
	}
	it := &Item{
		ID:        newID("i"),
		Src:       src,
		SessionID: te.Sid,
		Path:      path,
		Name:      te.Name,
		Category:  te.Category,
		Version:   0,
		CreatedAt: now,
		UpdatedAt: now,
	}
	s.items[it.ID] = it
	if err := s.saveItems(); err != nil {
		delete(s.items, it.ID)
		return Item{}, err
	}
	cp := *it
	return cp, nil
}

// DeleteTrashEntry 彻底删除某回收站条目（连带删其实体），不可恢复。
func (s *Store) DeleteTrashEntry(entryID string) error {
	if err := s.Load(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i, t := range s.trash {
		if t.ID == entryID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	_ = os.RemoveAll(filepath.Join(s.root, "trash", s.trash[idx].ID))
	s.trash = append(s.trash[:idx], s.trash[idx+1:]...)
	return s.saveTrash()
}

// EmptyTrash 清空回收站（删除全部实体与登记）。
func (s *Store) EmptyTrash() error {
	if err := s.Load(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = os.RemoveAll(filepath.Join(s.root, "trash"))
	if err := os.MkdirAll(filepath.Join(s.root, "trash"), 0o755); err != nil {
		return err
	}
	s.trash = nil
	return s.saveTrash()
}

// PurgeTrash 彻底删除移入已超过 days 天的回收站实体（days<=0 不动）；返回被清理条数。
// 由绑定层按配置的保留天数在访问回收站时调用。
func (s *Store) PurgeTrash(days int) (int, error) {
	if days <= 0 {
		return 0, nil
	}
	if err := s.Load(); err != nil {
		return 0, err
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
	s.mu.Lock()
	defer s.mu.Unlock()
	var keep []TrashEntry
	removed := 0
	for _, t := range s.trash {
		if t.DeletedAt != 0 && t.DeletedAt < cutoff {
			_ = os.RemoveAll(filepath.Join(s.root, "trash", t.ID))
			removed++
			continue
		}
		keep = append(keep, t)
	}
	if removed == 0 {
		return 0, nil
	}
	s.trash = keep
	if err := s.saveTrash(); err != nil {
		return removed, err
	}
	return removed, nil
}

func (s *Store) saveTrash() error {
	return writeJSONAtomic(s.pathOf("trash.json"), s.trash)
}

// upsertTrash 覆盖同 id 或追加。
func upsertTrash(list []TrashEntry, e TrashEntry) []TrashEntry {
	for i := range list {
		if list[i].ID == e.ID {
			list[i] = e
			return list
		}
	}
	return append(list, e)
}
