package docspace

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"Tools/tools/filekind"
)

// ============================================================
// 文档中心：批量操作与总览
//
// 为什么要批量：列表页选中几十条后逐条发绑定调用，前端要等几十次往返，
// 中途关页面还会留下"改了一半"的状态。这里把整批收进一次调用，落盘也只
// 写一次分片（saveItems 是整表重写，逐条调它就是 N 次全量写）。
//
// 锁的用法分两类，不能混：
//   - 只动元数据的（改分类、打标签、取消纳管）自己持写锁改内存，最后
//     saveItems 一次；
//   - 要碰实体文件的（移入回收站、存快照）必须逐条委托给既有单条方法，
//     因为那些方法内部就要抢同一把锁，在这里握着再调会自死锁。代价是
//     N 次落盘，换的是"文件语义只有一份实现"。
//
// 部分失败不整批回滚：回收站/快照本来就是逐条独立的动作，硬凑原子性只会
// 让实现变假。失败的 id 一律如实回传给前端点名，不藏。
// ============================================================

// batchIDs 规整批量入参：去空白、去重、保序。空结果直接放弃，避免误触发全量写盘。
func batchIDs(ids []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out
}

// tagsEqual 判断两组标签等价（长度与逐项一致）。调用方都已规范化，这里不再排序。
func tagsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// ---------- 批量改分类 ----------

// SetItemCategory 设置单个条目的分类（category 空表示移入未分类）。
// 与批量同一份实现，避免出现"单条不校验分类存在性"的口径分叉。
func (s *Store) SetItemCategory(itemID, category string) error {
	n, err := s.MoveItemsCategory([]string{itemID}, category)
	if err != nil {
		return err
	}
	if n == 0 {
		// 0 有两种：条目不存在，或它本来就在目标分类里。前者才是错误，得区分出来。
		if err := s.Load(); err != nil {
			return err
		}
		s.mu.RLock()
		_, ok := s.items[itemID]
		s.mu.RUnlock()
		if !ok {
			return fmt.Errorf("条目不存在: %s", itemID)
		}
	}
	return nil
}

// MoveItemsCategory 把若干条目移到同一分类，返回真正被改动的条数。
// category 非空时必须已在分类树里，否则整批拒绝——写进一个不存在的分类，
// 条目会从树上消失（分片按分类命名），那比报错严重得多。
func (s *Store) MoveItemsCategory(ids []string, category string) (int, error) {
	list := batchIDs(ids)
	if len(list) == 0 {
		return 0, nil
	}
	if err := s.Load(); err != nil {
		return 0, err
	}
	category = strings.TrimSpace(category)
	if category != "" {
		// ""（未分类）是合法归属，不需要存在性校验。
		s.mu.RLock()
		found := false
		for _, c := range s.taxonomy {
			if c.ID == category {
				found = true
				break
			}
		}
		s.mu.RUnlock()
		if !found {
			return 0, fmt.Errorf("分类不存在: %s", category)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	changed := 0
	for _, id := range list {
		it, ok := s.items[id]
		if !ok || it.Category == category {
			continue
		}
		it.Category = category
		it.UpdatedAt = now
		changed++
	}
	if changed == 0 {
		return 0, nil
	}
	if err := s.saveItems(); err != nil {
		return 0, err
	}
	return changed, nil
}

// ---------- 批量标签 ----------

// TagItems 给若干条目统一加/减标签（add 取并集、remove 取差集），
// 返回实际发生变化的条数。没变化的条目不刷 UpdatedAt，避免"点了个空操作
// 结果全列表跳到最新"。
func (s *Store) TagItems(ids, add, remove []string) (int, error) {
	list := batchIDs(ids)
	if len(list) == 0 {
		return 0, nil
	}
	adds, rems := normalizeTags(add), normalizeTags(remove)
	if len(adds) == 0 && len(rems) == 0 {
		return 0, nil
	}
	if err := s.Load(); err != nil {
		return 0, err
	}

	drop := make(map[string]bool, len(rems))
	for _, r := range rems {
		drop[r] = true
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UnixMilli()
	changed := 0
	for _, id := range list {
		it, ok := s.items[id]
		if !ok {
			continue
		}
		merged := make([]string, 0, len(it.Tags)+len(adds))
		merged = append(merged, it.Tags...)
		merged = append(merged, adds...)
		next := normalizeTags(merged)
		if len(drop) > 0 {
			kept := make([]string, 0, len(next))
			for _, t := range next {
				if !drop[t] {
					kept = append(kept, t)
				}
			}
			next = kept
		}
		if tagsEqual(it.Tags, next) {
			continue
		}
		it.Tags = next
		it.UpdatedAt = now
		changed++
	}
	if changed == 0 {
		return 0, nil
	}
	if err := s.saveItems(); err != nil {
		return 0, err
	}
	return changed, nil
}

// ---------- 批量取消纳管 ----------

// RemoveItems 批量取消纳管（不动磁盘原文件），返回删除条数。
// 落盘失败时把整批原样放回内存，保持内存与磁盘一致（与单条 RemoveItem 同口径）。
func (s *Store) RemoveItems(ids []string) (int, error) {
	list := batchIDs(ids)
	if len(list) == 0 {
		return 0, nil
	}
	if err := s.Load(); err != nil {
		return 0, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	removed := make([]*Item, 0, len(list))
	for _, id := range list {
		if it, ok := s.items[id]; ok {
			removed = append(removed, it)
			delete(s.items, id)
		}
	}
	if len(removed) == 0 {
		return 0, nil
	}
	if err := s.saveItems(); err != nil {
		for _, it := range removed {
			s.items[it.ID] = it
		}
		return 0, err
	}
	return len(removed), nil
}

// ---------- 批量回收站 / 快照（委托单条实现） ----------

// BatchResult 批量动作的统一回执：成功数 + 失败明细。
// 前端据此决定提示级别（全成功 success / 部分失败 warning），不把失败藏起来。
type BatchResult struct {
	Done   int      `json:"done"`
	Failed []string `json:"failed"` // 失败条目的 id
	// Reason 只给首条失败的原因（一整批可能各有原因，全塞进一句反而读不清）。
	// 无论全失败还是部分失败都给出，好让用户知道"为什么少了几条"。
	Reason string `json:"reason,omitempty"`
}

// collectBatchErr 记住第一条失败原因，供回执展示。
func collectBatchErr(reason string, err error) string {
	if reason != "" {
		return reason
	}
	return err.Error()
}

// TrashItems 批量移入回收站。非 local 源（sftp/webdav/cloud）语义不成立，
// 逐条跳过并计入 Failed，不让一条远端把整批带崩。
func (s *Store) TrashItems(ids []string) (BatchResult, error) {
	list := batchIDs(ids)
	res := BatchResult{Failed: []string{}}
	for _, id := range list {
		if err := s.MoveToTrash(id); err != nil {
			res.Failed = append(res.Failed, id)
			res.Reason = collectBatchErr(res.Reason, err)
			continue
		}
		res.Done++
	}
	return res, nil
}

// SnapshotItems 批量存快照，keep 取调用方传入的配置值。
// 单条体积上限、读取失败都由 SnapshotItem 把关，这里只收集失败 id。
func (s *Store) SnapshotItems(ids []string, note string, keep int) (BatchResult, error) {
	list := batchIDs(ids)
	res := BatchResult{Failed: []string{}}
	for _, id := range list {
		if _, err := s.SnapshotItem(id, note, keep); err != nil {
			res.Failed = append(res.Failed, id)
			res.Reason = collectBatchErr(res.Reason, err)
			continue
		}
		res.Done++
	}
	return res, nil
}

// ---------- 总览 ----------

// CategoryCount 分类节点计数：Num 是挂在该节点自身的条目数，
// Inclusive 额外把子节点滚上来（给左树徽标用，父节点显示"含子级"更直观）。
type CategoryCount struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Num       int    `json:"num"`
	Inclusive int    `json:"inclusive"`
	Size      int64  `json:"size"`
}

// SourceCount 按文件源汇总的条目数，前端用来解释"为什么这条没大小"。
type SourceCount struct {
	Src  string `json:"src"`
	Num  int    `json:"num"`
	Size int64  `json:"size"`
}

// KindCount 按扩展名汇总的一项，供「热门格式」展示。
type ExtCount struct {
	Ext string `json:"ext"`
	Num int    `json:"num"`
}

// Overview 一次给齐统计卡、类型 chips、分类徽标与源分布。
// 合并成一个方法而不是四个：统计页进页就要全量数据，四次往返在远端源下
// 肉眼可见地慢；且都源自同一份 Metas 缓存，拆开只会重复算。
type Overview struct {
	Stats      Stats           `json:"stats"`
	Kinds      []KindCount     `json:"kinds"`
	Categories []CategoryCount `json:"categories"`
	Sources    []SourceCount   `json:"sources"`
	TopExts    []ExtCount      `json:"topExts"`
}

// overviewExtTop 热门格式取前几名。再多就是噪声，卡片放不下。
const overviewExtTop = 6

// Overview 汇总当前库的总览数据。分类含"未分类"一项（id 空串），
// 即使数量为 0 也保留，好让前端徽标与树节点一一对上。
func (s *Store) Overview() (Overview, error) {
	items, err := s.Items()
	if err != nil {
		return Overview{}, err
	}
	metas, err := s.Metas()
	if err != nil {
		return Overview{}, err
	}
	cats, err := s.Categories()
	if err != nil {
		return Overview{}, err
	}
	st, err := s.Stats()
	if err != nil {
		return Overview{}, err
	}
	ov := Overview{Stats: st, Kinds: st.KindBreakdown()}

	// 自身计数：metas 与 items 同序（两者都来自 Items() 的同一顺序），按下标对齐。
	self := map[string]int{}
	selfSize := map[string]int64{}
	srcNum := map[string]int{}
	srcSize := map[string]int64{}
	extNum := map[string]int{}
	for i := range items {
		self[items[i].Category]++
		srcNum[items[i].Src]++
		if i < len(metas) {
			selfSize[items[i].Category] += metas[i].Size
			srcSize[items[i].Src] += metas[i].Size
			if metas[i].Ext != "" {
				extNum[metas[i].Ext]++
			}
		}
	}

	// 子树滚加：按 parent 建链后自底向上累加。带 visited 兜住脏数据里的环，
	// 免得分类树被手改坏时这里无限递归。
	children := map[string][]string{}
	for _, c := range cats {
		children[c.Parent] = append(children[c.Parent], c.ID)
	}
	var rollup func(id string, depth int) int
	rollup = func(id string, depth int) int {
		if depth > 32 {
			return self[id]
		}
		total := self[id]
		for _, kid := range children[id] {
			total += rollup(kid, depth+1)
		}
		return total
	}
	seenCat := map[string]bool{}
	for _, c := range cats { // cats 已按 Sort 稳定排序，直接沿用这个顺序
		if seenCat[c.ID] {
			continue
		}
		seenCat[c.ID] = true
		ov.Categories = append(ov.Categories, CategoryCount{
			ID: c.ID, Name: c.Name, Num: self[c.ID], Inclusive: rollup(c.ID, 0), Size: selfSize[c.ID],
		})
	}
	if !seenCat[""] {
		ov.Categories = append(ov.Categories, CategoryCount{ID: "", Name: "未分类", Num: self[""], Inclusive: self[""], Size: selfSize[""]})
	}

	for _, src := range []string{SrcLocal, SrcSFTP, SrcWebDAV, SrcCloud} {
		if srcNum[src] > 0 {
			ov.Sources = append(ov.Sources, SourceCount{Src: src, Num: srcNum[src], Size: srcSize[src]})
		}
	}
	// 未知源（历史数据里的空串等）不丢，归到 local：空 Src 的语义就是本机。
	known := 0
	for _, x := range ov.Sources {
		known += x.Num
	}
	if rest := len(items) - known; rest > 0 {
		ov.Sources = append(ov.Sources, SourceCount{Src: SrcLocal, Num: rest, Size: srcSize[""]})
	}

	exts := make([]ExtCount, 0, len(extNum))
	for e, n := range extNum {
		exts = append(exts, ExtCount{Ext: e, Num: n})
	}
	sort.SliceStable(exts, func(i, j int) bool {
		if exts[i].Num != exts[j].Num {
			return exts[i].Num > exts[j].Num
		}
		return exts[i].Ext < exts[j].Ext
	})
	if len(exts) > overviewExtTop {
		exts = exts[:overviewExtTop]
	}
	ov.TopExts = exts
	return ov, nil
}

// KindLabel 把内部 kind 值换成前端展示用的短标签。
// 放在 Go 侧而不是前端：新增类型时只改一处，不会前后端两份表对不上。
func KindLabel(kind string) string {
	switch kind {
	case filekind.KindImage:
		return "图片"
	case filekind.KindPDF:
		return "PDF"
	case filekind.KindSheet:
		return "表格"
	case filekind.KindDoc:
		return "文档"
	case filekind.KindText:
		return "文本"
	case filekind.KindCode:
		return "代码"
	case filekind.KindAudio:
		return "音频"
	case filekind.KindVideo:
		return "视频"
	case filekind.KindArchive:
		return "压缩包"
	case filekind.KindBinary:
		return "其他"
	default:
		return "其他"
	}
}
