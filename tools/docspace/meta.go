package docspace

import (
	"os"
	"sort"
	"sync"
	"time"

	"Tools/tools/filekind"
)

// ItemMeta 条目的展示元数据：类型、大小、修改时间。
//
// 为什么不落盘：这些是实体文件的派生事实，写进 items/*.json 就会与磁盘脱节
// （用户在资源管理器里改名/删除/改大小，本程序不知道）。所以每次列表时经
// docref 现取，再用 TTL 缓存挡住重复 IO——与「运行时事实交给各模块缓存与重扫」
// 的仓库口径一致。
type ItemMeta struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`    // image|pdf|sheet|doc|text|code|audio|video|archive|binary
	Mime    string `json:"mime"`    // 取不到时 application/octet-stream
	Ext     string `json:"ext"`     // 小写不带点；tar.gz 整段保留
	Size    int64  `json:"size"`    // 字节；Unreachable 时为 0
	ModTime int64  `json:"modTime"` // 毫秒时间戳；Unreachable 时为 0
	// Unreachable 表示取不到实体元信息：本机文件已删除/移动，或远端会话已关闭。
	// 前端据此把条目标灰并给排查线索，不猜大小、不冒充 0 字节。
	Unreachable bool `json:"unreachable"`
	// Thumbable 表示该条目能出真缩略图（类型是图片且扩展名在标准库解码器覆盖内）。
	// 后端算好交出去，省得前端再维护一份扩展名表与后端对不上。
	Thumbable bool `json:"thumbable"`
	// Thumb 是缩略图端点的完整 URL（?src=&sid=&path=&w=），Thumbable 为假时为空。
	// 由绑定层补齐：Store 不 import fileview（fileview 已经 import docspace，
	// 反向就成环），所以这里留空。前端一次 Metas 就拿齐 URL，不必逐条发调用。
	Thumb string `json:"thumb,omitempty"`
}

// metaTTL 是元数据缓存的有效期。列表页 5s 轮询别的接口，条目列表只在进页与
// 变更后刷新，45s 足够让一次进页只 Stat 一轮；用户在外部改文件最迟 45s 后看到。
const metaTTL = 45 * time.Second

// metaCacheEntry 一条缓存：连同写入时刻，用于 TTL 判定。
// 远端源（sftp/webdav）的 Stat 可能慢，所以整表只存结果，不在缓存里做等待。
type metaCacheEntry struct {
	meta ItemMeta
	at   time.Time
}

// kindOf 纯派生：只看路径后缀，不做任何 IO。
func kindOf(path string) (kind, mime, ext string) {
	return filekind.Classify(path)
}

// statItem 取实体大小与修改时间。local 直接 os.Stat（微秒级）；其余源经 docref
// 统一分发，会话没开或网络不通都会失败——失败不报错，只标记不可达。
func statItem(it Item) (size, modMs int64, ok bool) {
	if it.Src == "" || it.Src == SrcLocal {
		st, err := os.Stat(it.Path)
		if err != nil || st.IsDir() {
			return 0, 0, false
		}
		return st.Size(), st.ModTime().UnixMilli(), true
	}
	ref, err := Parse(it.Src, it.SessionID, it.Path)
	if err != nil {
		return 0, 0, false
	}
	st, err := ref.Stat()
	if err != nil || st.IsDir() {
		return 0, 0, false
	}
	return st.Size(), st.ModTime().UnixMilli(), true
}

// metaOne 取单条目元数据，命中未过期缓存就不再 Stat。调用方需已持有条目值。
func (s *Store) metaOne(it Item) ItemMeta {
	s.metaMu.Lock()
	if e, ok := s.metas[it.ID]; ok && time.Since(e.at) < metaTTL {
		s.metaMu.Unlock()
		return e.meta
	}
	s.metaMu.Unlock()

	kind, mime, ext := kindOf(it.Path)
	size, modMs, ok := statItem(it)
	m := ItemMeta{
		ID:          it.ID,
		Kind:        kind,
		Mime:        mime,
		Ext:         ext,
		Size:        size,
		ModTime:     modMs,
		Unreachable: !ok,
		Thumbable:   kind == filekind.KindImage && filekind.StdlibDecodable[ext],
	}
	s.metaMu.Lock()
	if s.metas == nil {
		s.metas = map[string]metaCacheEntry{}
	}
	s.metas[it.ID] = metaCacheEntry{meta: m, at: time.Now()}
	s.metaMu.Unlock()
	return m
}

// Metas 返回全部条目的元数据，顺序与 Items() 一致（创建时间升序）。
//
// 并发取：一次进页可能上百条，串行 Stat 时远端源一条就能拖几百毫秒。这里开
// 固定 8 个 worker 分摊，既压住延迟也不至于把远端连接打爆。
func (s *Store) Metas() ([]ItemMeta, error) {
	items, err := s.Items()
	if err != nil {
		return nil, err
	}
	out := make([]ItemMeta, len(items))
	if len(items) == 0 {
		return out, nil
	}
	var (
		idx  int
		idxM sync.Mutex
		wg   sync.WaitGroup
	)
	const workers = 8
	for i := 0; i < workers && i < len(items); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				idxM.Lock()
				n := idx
				idx++
				idxM.Unlock()
				if n >= len(items) {
					return
				}
				out[n] = s.metaOne(items[n])
			}
		}()
	}
	wg.Wait()
	return out, nil
}

// InvalidateMeta 作废单个条目的缓存：改名/换分类之外的实体动作（快照、还原、
// 移入回收站、纳管、取消纳管）都会改变大小或时间，必须让下一轮重取。
func (s *Store) InvalidateMeta(itemID string) {
	s.metaMu.Lock()
	delete(s.metas, itemID)
	s.metaMu.Unlock()
}

// InvalidateAllMeta 作废整表（批量操作与外部改动后调用）。
func (s *Store) InvalidateAllMeta() {
	s.metaMu.Lock()
	s.metas = map[string]metaCacheEntry{}
	s.metaMu.Unlock()
}

// Stats 是文档中心的总览数字：总量、体积、按类型分布、近况。
// 与 Metas 同源，所以口径一致（不可达的条目计入 Missing，不计入体积）。
type Stats struct {
	Total     int              `json:"total"`
	TotalSize int64            `json:"totalSize"`
	ByKind    map[string]int   `json:"byKind"`
	KindSize  map[string]int64 `json:"kindSize"`
	Missing   int              `json:"missing"`
	Updated7d int              `json:"updated7d"` // 实体或登记信息 7 天内动过
	WithTags  int              `json:"withTags"`
	WithVers  int              `json:"withVersions"`
}

// Stats 汇总一次全量元数据。前端统计卡与类型 chips 的计数都来自这里，
// 避免"筛选后计数"与"全量计数"两套数字互相打脸（分面计数另说）。
func (s *Store) Stats() (Stats, error) {
	items, err := s.Items()
	if err != nil {
		return Stats{}, err
	}
	metas, err := s.Metas()
	if err != nil {
		return Stats{}, err
	}
	st := Stats{ByKind: map[string]int{}, KindSize: map[string]int64{}}
	weekAgo := time.Now().Add(-7 * 24 * time.Hour).UnixMilli()
	for i := range items {
		st.Total++
		m := metas[i]
		st.ByKind[m.Kind]++
		if m.Unreachable {
			st.Missing++
			continue
		}
		st.TotalSize += m.Size
		st.KindSize[m.Kind] += m.Size
		if m.ModTime >= weekAgo || items[i].UpdatedAt >= weekAgo {
			st.Updated7d++
		}
		if len(items[i].Tags) > 0 {
			st.WithTags++
		}
		if items[i].Version > 0 {
			st.WithVers++
		}
	}
	return st, nil
}

// KindCount 是「类型 + 数量」的一项，供前端按 Order 渲染 chips。
// Label 一并给出：中文标签的口径留在 Go 侧，前端只负责颜色，新增类型时
// 不会出现前后端两份名称表对不上的情况。
type KindCount struct {
	Kind  string `json:"kind"`
	Label string `json:"label"`
	Num   int    `json:"num"`
	Size  int64  `json:"size"`
}

// KindBreakdown 把 Stats.ByKind 摊成按 Order 排序的切片。
// 只列真出现的类型（数量为 0 的不占位），未知类型一律并入 binary 兜底。
func (st Stats) KindBreakdown() []KindCount {
	out := make([]KindCount, 0, len(st.ByKind))
	for _, k := range filekind.Order {
		if n := st.ByKind[k]; n > 0 {
			out = append(out, KindCount{Kind: k, Label: KindLabel(k), Num: n, Size: st.KindSize[k]})
		}
	}
	// 表里若有 Order 未覆盖的类型（将来扩展 kind 时），补在末尾不丢数
	known := make(map[string]bool, len(filekind.Order))
	for _, k := range filekind.Order {
		known[k] = true
	}
	var extra []KindCount
	for k, n := range st.ByKind {
		if !known[k] && n > 0 {
			extra = append(extra, KindCount{Kind: k, Label: KindLabel(k), Num: n, Size: st.KindSize[k]})
		}
	}
	sort.Slice(extra, func(i, j int) bool { return extra[i].Kind < extra[j].Kind })
	return append(out, extra...)
}
