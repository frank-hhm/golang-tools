package docspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mkLocalFile 在临时目录落一个指定大小的文件，返回路径。
func mkLocalFile(t *testing.T, dir, name string, size int) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatalf("写测试文件失败: %v", err)
	}
	return p
}

func TestSearchFilters(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()

	// 分类：root「资料」→ child「发票」
	root, err := s.AddCategory("资料", "")
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.AddCategory("发票", root.ID)
	if err != nil {
		t.Fatal(err)
	}

	big := mkLocalFile(t, dir, "Report-2026.pdf", 4096)
	small := mkLocalFile(t, dir, "todo.md", 10)
	remote := "/srv/invoice.pdf"

	itBig, err := s.AddItem(Ref{Src: SrcLocal, Path: big}, root.ID)
	if err != nil {
		t.Fatal(err)
	}
	itSmall, err := s.AddItem(Ref{Src: SrcLocal, Path: small}, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	itRemote, err := s.AddItem(Ref{Src: SrcSFTP, SessionID: "sid", Path: remote}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetItemTags(itSmall.ID, []string{"重要", "个人"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetItemTags(itRemote.ID, []string{"重要"}); err != nil {
		t.Fatal(err)
	}
	// 直接改内存元数据模拟「存过版本」与「很久没更新」（同包测试特权）
	s.mu.Lock()
	s.items[itBig.ID].Version = 2
	s.items[itSmall.ID].UpdatedAt = time.Now().AddDate(0, 0, -40).UnixMilli()
	s.mu.Unlock()

	// keyword：大小写不敏感，命中名称
	got, err := s.Search(SearchFilter{Keyword: "report-2026"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != itBig.ID {
		t.Fatalf("keyword 命中不对: %+v", got)
	}
	// keyword 也能命中标签
	got, _ = s.Search(SearchFilter{Keyword: "个人"})
	if len(got) != 1 || got[0].ID != itSmall.ID {
		t.Fatalf("keyword 命中标签不对: %+v", got)
	}

	// 分类含子孙：root 下应看到 child 的条目
	got, _ = s.Search(SearchFilter{Category: root.ID})
	if len(got) != 2 {
		t.Fatalf("分类含子级应 2 条: %+v", got)
	}
	// 只限本级
	got, _ = s.Search(SearchFilter{Category: root.ID, ExcludeSubCats: true})
	if len(got) != 1 || got[0].ID != itBig.ID {
		t.Fatalf("只限本级应 1 条: %+v", got)
	}

	// 标签 AND
	got, _ = s.Search(SearchFilter{Tags: []string{"重要"}})
	if len(got) != 2 {
		t.Fatalf("单标签应 2 条: %+v", got)
	}
	got, _ = s.Search(SearchFilter{Tags: []string{"重要", "个人"}})
	if len(got) != 1 || got[0].ID != itSmall.ID {
		t.Fatalf("双标签 AND 应 1 条: %+v", got)
	}

	// 有无版本
	got, _ = s.Search(SearchFilter{HasVersions: true})
	if len(got) != 1 || got[0].ID != itBig.ID {
		t.Fatalf("hasVersions 应 1 条: %+v", got)
	}

	// 最近 N 天：40 天前更新的 itSmall 被 30 天窗口排除
	got, _ = s.Search(SearchFilter{UpdatedWithinDays: 30})
	for _, it := range got {
		if it.ID == itSmall.ID {
			t.Fatal("40 天前的条目不该进 30 天窗口")
		}
	}

	// 大小条件：仅 local 参与；4KB 文件落在 [2048, +inf)
	got, _ = s.Search(SearchFilter{MinSizeBytes: 2048})
	if len(got) != 1 || got[0].ID != itBig.ID {
		t.Fatalf("minSize 应只剩 big（sftp 不参与）: %+v", got)
	}
	got, _ = s.Search(SearchFilter{MaxSizeBytes: 100})
	if len(got) != 1 || got[0].ID != itSmall.ID {
		t.Fatalf("maxSize 应只剩 small: %+v", got)
	}

	// 空条件全量 + 按更新时间降序（itSmall 被改到 40 天前 → 垫底）
	got, err = s.Search(SearchFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("空条件应 3 条: %+v", got)
	}
	if got[len(got)-1].ID != itSmall.ID {
		t.Fatalf("更新时间降序不对: %+v", got)
	}
	// limit 在排序后截断
	got, _ = s.Search(SearchFilter{Limit: 1})
	if len(got) != 1 || got[0].ID == itSmall.ID {
		t.Fatalf("limit=1 应留最新的而非最旧: %+v", got)
	}
}

func TestNewNote(t *testing.T) {
	s := newTestStore(t)

	it, err := s.NewNote("周记：2026/10 复盘")
	if err != nil {
		t.Fatalf("NewNote: %v", err)
	}
	if !strings.HasSuffix(it.Name, ".md") {
		t.Fatalf("应自动补 .md 后缀: %s", it.Name)
	}
	if strings.ContainsAny(it.Name, `/:*?"<>|`) {
		t.Fatalf("非法字符未被清洗: %s", it.Name)
	}
	if !strings.HasPrefix(it.Path, s.pathOf("notes")) {
		t.Fatalf("笔记实体应落库内 notes/: %s", it.Path)
	}
	b, err := os.ReadFile(it.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "# ") {
		t.Fatalf("初始内容应是标题行: %q", string(b))
	}
	// 自动归入「笔记」根分类
	cats, _ := s.Categories()
	var noteCat *Category
	for i := range cats {
		if cats[i].Name == noteCategoryName && cats[i].Parent == "" {
			noteCat = &cats[i]
		}
	}
	if noteCat == nil || it.Category != noteCat.ID {
		t.Fatalf("应挂到「笔记」分类: cat=%+v item.cat=%s", noteCat, it.Category)
	}

	// 同名再建必须报错且不覆盖
	if _, err := s.NewNote("周记：2026/10 复盘"); err == nil {
		t.Fatal("同名笔记应报错")
	}

	// 误带 .md 不产生 a.md.md
	it2, err := s.NewNote("购物清单.md")
	if err != nil {
		t.Fatal(err)
	}
	if it2.Name != "购物清单.md" {
		t.Fatalf("后缀归一失败: %s", it2.Name)
	}

	// 空名 → 时间戳兜底；且「笔记」分类不会重复建
	if _, err := s.NewNote("  "); err != nil {
		t.Fatal(err)
	}
	cats, _ = s.Categories()
	n := 0
	for _, c := range cats {
		if c.Name == noteCategoryName {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("笔记分类应只建一次，实际 %d", n)
	}
}
