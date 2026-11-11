package docspace

// 智能搜索与在线笔记（P1 顺延项，2026-11-02 补做）。
//
// 搜索不建独立索引、不引全文检索引擎（计划 §一）：条目元数据本就全在内存
// （Store.Load 一次），直接按条件过滤；大小条件仅对 local 源做 os.Stat，
// sftp/webdav 取不到大小就排除——不猜。整盘/内容检索复用 tools/disk 的
// DiskSearch/DiskSearchContent，前端把命中结果一键纳管，闭环同样成立。
//
// 笔记不建独立存储（计划 §五）：笔记就是一个归类到「笔记」分类的文本文件，
// 支持 Markdown(.md) 与富文本(.html) 两种格式，实体落在库内 notes/ 目录
// （属于"被纳管文件"心智下的最自然存放点，可在条目里改名/移动）；
// 编辑入口已拆到独立笔记模块（/tools/note，wangeditor 所见即所得），
// 查看器的裸 md 双栏编辑仍保留供普通 markdown 文件使用。

import (
	"fmt"
	"html"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// noteCategoryName 笔记分类名：NewNote 惰性确保存在（根节点）。
const noteCategoryName = "笔记"

// SearchFilter 智能搜索条件。零值 = 该条件不启用。
type SearchFilter struct {
	Keyword           string   `json:"keyword"`           // 匹配 名称/路径/标签，大小写不敏感
	Category          string   `json:"category"`          // 分类 id；默认含其全部子孙分类
	ExcludeSubCats    bool     `json:"excludeSubCats"`    // true = 只限该分类本身
	Tags              []string `json:"tags"`              // AND：条目须同时带上全部标签
	HasVersions       bool     `json:"hasVersions"`       // true = 只要存过版本快照的
	UpdatedWithinDays int      `json:"updatedWithinDays"` // >0 = 只看最近 N 天更新过的
	MinSizeBytes      int64    `json:"minSizeBytes"`      // >0 = 大小下限；仅 local 可判定
	MaxSizeBytes      int64    `json:"maxSizeBytes"`      // >0 = 大小上限；仅 local 可判定
	Limit             int      `json:"limit"`             // >0 = 截断上限；默认 500
}

// Search 按条件过滤纳管条目，按更新时间降序返回。
// 全平台跨绑定安全：纯内存过滤 + local 源的 os.Stat，Linux 面板同样可用。
func (s *Store) Search(f SearchFilter) ([]Item, error) {
	items, err := s.Items()
	if err != nil {
		return nil, err
	}
	cats, err := s.Categories()
	if err != nil {
		return nil, err
	}

	// 分类集合：选中父分类默认把子孙一起算上（层级即密级/类别级别的心智）
	var catSet map[string]bool
	if f.Category != "" {
		catSet = descendantCategorySet(cats, f.Category, !f.ExcludeSubCats)
	}

	kw := strings.ToLower(strings.TrimSpace(f.Keyword))
	tagNeedle := make([]string, 0, len(f.Tags))
	for _, t := range f.Tags {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			tagNeedle = append(tagNeedle, t)
		}
	}
	var since int64
	if f.UpdatedWithinDays > 0 {
		since = time.Now().UnixMilli() - int64(f.UpdatedWithinDays)*86400_000
	}
	sizeWanted := f.MinSizeBytes > 0 || f.MaxSizeBytes > 0
	limit := f.Limit
	if limit <= 0 {
		limit = 500
	}

	out := make([]Item, 0, 64)
	for _, it := range items {
		if kw != "" && !itemMatchesKeyword(it, kw) {
			continue
		}
		if catSet != nil && !catSet[it.Category] {
			continue
		}
		if len(tagNeedle) > 0 && !itemHasAllTags(it, tagNeedle) {
			continue
		}
		if f.HasVersions && it.Version <= 0 {
			continue
		}
		if since > 0 && it.UpdatedAt < since {
			continue
		}
		if sizeWanted && !itemSizeInRange(it, f.MinSizeBytes, f.MaxSizeBytes) {
			continue
		}
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].UpdatedAt != out[j].UpdatedAt {
			return out[i].UpdatedAt > out[j].UpdatedAt
		}
		return out[i].ID < out[j].ID
	})
	// 先排序再截断：命中很多时留下的是最新的，而不是创建最早的一批
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// itemMatchesKeyword 名称、路径、任一标签命中即可（均已小写化比较）。
func itemMatchesKeyword(it Item, kw string) bool {
	if strings.Contains(strings.ToLower(it.Name), kw) ||
		strings.Contains(strings.ToLower(it.Path), kw) {
		return true
	}
	for _, t := range it.Tags {
		if strings.Contains(strings.ToLower(t), kw) {
			return true
		}
	}
	return false
}

// itemHasAllTags 条目标签须覆盖全部过滤标签（AND），大小写不敏感。
func itemHasAllTags(it Item, needles []string) bool {
	have := make(map[string]bool, len(it.Tags))
	for _, t := range it.Tags {
		have[strings.ToLower(strings.TrimSpace(t))] = true
	}
	for _, n := range needles {
		if !have[n] {
			return false
		}
	}
	return true
}

// itemSizeInRange 判定大小区间。只有 local 源能可靠取到大小：
// 其余源（sftp/webdav/cloud）或 stat 失败一律判不命中——宁可少报，不猜。
func itemSizeInRange(it Item, minB, maxB int64) bool {
	if it.Src != "" && it.Src != SrcLocal {
		return false
	}
	st, err := os.Stat(it.Path)
	if err != nil || st.IsDir() {
		return false
	}
	size := st.Size()
	if minB > 0 && size < minB {
		return false
	}
	if maxB > 0 && size > maxB {
		return false
	}
	return true
}

// descendantCategorySet 从 root 出发收集分类 id 集合；withSub 时含全部子孙。
func descendantCategorySet(cats []Category, root string, withSub bool) map[string]bool {
	set := map[string]bool{root: true}
	if !withSub {
		return set
	}
	// 逐层展开：taxonomy 规模上限内（万级分类不太可能）重复扫一遍即可
	for changed := true; changed; {
		changed = false
		for _, c := range cats {
			if c.Parent != "" && set[c.Parent] && !set[c.ID] {
				set[c.ID] = true
				changed = true
			}
		}
	}
	return set
}

// ---------- 在线笔记 ----------

// NewNote 在库内 notes/ 建一个 .md 或 .html 文件并纳管到「笔记」分类（惰性创建分类）。
// format 取 "md"（默认，空值也按 md）或 "html"；name 可缺省（自动生成带时间戳名）；
// 已存在同名笔记时报错，绝不覆盖。md 写标题骨架，html 写最小富文本骨架（标题做 HTML 转义）。
func (s *Store) NewNote(name string, format string) (Item, error) {
	ext := ".md"
	if strings.EqualFold(strings.TrimSpace(format), "html") {
		ext = ".html"
	}
	title := sanitizeNoteName(name)
	fileName := title + ext
	dir := s.pathOf("notes")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Item{}, err
	}
	p := filepath.Join(dir, fileName)
	if _, err := os.Stat(p); err == nil {
		return Item{}, fmt.Errorf("笔记已存在: %s", fileName)
	}

	// 惰性确保「笔记」根分类存在
	catID, err := s.ensureNoteCategory()
	if err != nil {
		return Item{}, err
	}

	var content string
	if ext == ".html" {
		content = "<h1>" + html.EscapeString(title) + "</h1><p><br></p>"
	} else {
		content = "# " + title + "\n\n"
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return Item{}, err
	}
	ref, err := Parse(SrcLocal, "", p)
	if err != nil {
		_ = os.Remove(p)
		return Item{}, err
	}
	it, err := s.AddItem(ref, catID)
	if err != nil {
		_ = os.Remove(p) // 登记失败就删掉刚建的空文件，不留孤儿实体
		return Item{}, err
	}
	return it, nil
}

// ensureNoteCategory 找名为「笔记」的根分类；不存在则创建。返回分类 id。
func (s *Store) ensureNoteCategory() (string, error) {
	cats, err := s.Categories()
	if err != nil {
		return "", err
	}
	for _, c := range cats {
		if c.Name == noteCategoryName && c.Parent == "" {
			return c.ID, nil
		}
	}
	c, err := s.AddCategory(noteCategoryName, "")
	if err != nil {
		return "", err
	}
	return c.ID, nil
}

// sanitizeNoteName 去非法文件名字符与控制字符，限长；空则用时间戳兜底。
// 不做多语言花活：Windows 与 POSIX 的交集里能安全落盘即可。
func sanitizeNoteName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "note-" + time.Now().Format("20060102-150405")
	}
	replacer := strings.NewReplacer(
		`\`, "-", `/`, "-", ":", "-", "*", "-", "?", "-", `"`, "'", "<", "-", ">", "-", "|", "-",
	)
	name = replacer.Replace(name)
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 { // 控制字符
			continue
		}
		b.WriteRune(r)
	}
	name = strings.TrimSpace(b.String())
	// 去掉误带的笔记后缀（.md/.html/.htm）再按所选格式统一补，避免 a.md.md / a.html.md；大小写原样保留
	for _, suf := range []string{".md", ".html", ".htm"} {
		if len(name) > len(suf) && strings.EqualFold(name[len(name)-len(suf):], suf) {
			name = name[:len(name)-len(suf)]
			break
		}
	}
	name = strings.TrimSpace(name)
	if len([]rune(name)) > 80 {
		name = string([]rune(name)[:80])
	}
	if name == "" {
		return "note-" + time.Now().Format("20060102-150405")
	}
	return name
}
