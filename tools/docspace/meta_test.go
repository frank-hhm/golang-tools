package docspace

import (
	"os"
	"path/filepath"
	"testing"

	"Tools/tools/filekind"
)

// writeFile 在 temp 下造一个指定扩展名与字数的实体文件，返回绝对路径。
func writeFile(t *testing.T, dir, name string, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("写测试文件失败 %s: %v", p, err)
	}
	return p
}

// addItem 纳管一个本机条目，失败即 fatal。
func addItem(t *testing.T, s *Store, path string) Item {
	t.Helper()
	it, err := s.AddItem(Ref{Src: SrcLocal, Path: path}, "")
	if err != nil {
		t.Fatalf("AddItem(%s): %v", path, err)
	}
	return it
}

func TestMetasKindSizeAndThumbable(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	src := writeFile(t, dir, "a.txt", "") // 内容长度在下面按需覆盖
	src2 := writeFile(t, dir, "b.png", "")
	src3 := writeFile(t, dir, "c.unknownext", "")
	_ = os.WriteFile(src, []byte("12345"), 0o600)
	_ = os.WriteFile(src2, []byte("123"), 0o600)
	_ = os.WriteFile(src3, []byte("x"), 0o600)

	addItem(t, s, src)
	addItem(t, s, src2)
	addItem(t, s, src3)

	metas, err := s.Metas()
	if err != nil {
		t.Fatalf("Metas: %v", err)
	}
	if len(metas) != 3 {
		t.Fatalf("Metas 数量 = %d, 期望 3", len(metas))
	}
	// Metas 顺序与 Items 一致（创建时间升序），这里按扩展名对齐断言更直观
	byExt := map[string]ItemMeta{}
	for _, m := range metas {
		byExt[m.Ext] = m
	}
	cases := []struct {
		ext       string
		kind      string
		size      int64
		thumbable bool
	}{
		{"txt", filekind.KindText, 5, false},
		{"png", filekind.KindImage, 3, true},
		{"unknownext", filekind.KindBinary, 1, false},
	}
	for _, c := range cases {
		m, ok := byExt[c.ext]
		if !ok {
			t.Fatalf("缺少扩展名 %q 的元数据", c.ext)
		}
		if m.Kind != c.kind {
			t.Errorf("%s kind = %q, 期望 %q", c.ext, m.Kind, c.kind)
		}
		if m.Size != c.size {
			t.Errorf("%s size = %d, 期望 %d", c.ext, m.Size, c.size)
		}
		if m.Thumbable != c.thumbable {
			t.Errorf("%s thumbable = %v, 期望 %v", c.ext, m.Thumbable, c.thumbable)
		}
		if m.Unreachable {
			t.Errorf("%s 被判不可达，实体明明存在", c.ext)
		}
		if m.ModTime == 0 {
			t.Errorf("%s modTime = 0, 期望取到实体修改时间", c.ext)
		}
	}
}

func TestMetasMarksMissingEntityUnreachable(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	gone := filepath.Join(dir, "deleted.md")
	addItem(t, s, gone) // 从未创建：纳管只登记不校验存在性

	metas, err := s.Metas()
	if err != nil {
		t.Fatalf("Metas: %v", err)
	}
	if len(metas) != 1 || !metas[0].Unreachable {
		t.Fatalf("实体不存在的条目应标 unreachable，实得 %+v", metas)
	}
	if metas[0].Size != 0 || metas[0].Kind != filekind.KindText {
		t.Errorf("不可达条目仍应有类型但无大小，实得 kind=%q size=%d", metas[0].Kind, metas[0].Size)
	}

	st, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Total != 1 || st.Missing != 1 {
		t.Errorf("Stats total/missing = %d/%d, 期望 1/1", st.Total, st.Missing)
	}
	if st.TotalSize != 0 {
		t.Errorf("不可达条目不该计入总体积，实得 %d", st.TotalSize)
	}
}

// 缓存必须能被作废：改完文件后整表失效，下一轮要看到新大小；不作废时保持 TTL 内旧值。
func TestMetaCacheInvalidation(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	p := writeFile(t, dir, "note.md", "short")
	addItem(t, s, p)

	first, err := s.Metas()
	if err != nil || len(first) != 1 || first[0].Size != int64(len("short")) {
		t.Fatalf("首轮元数据不对: %+v err=%v", first, err)
	}

	if err := os.WriteFile(p, []byte("a much longer body"), 0o600); err != nil {
		t.Fatalf("改写文件失败: %v", err)
	}
	cached, err := s.Metas()
	if err != nil {
		t.Fatalf("Metas(缓存): %v", err)
	}
	if cached[0].Size != int64(len("short")) {
		t.Errorf("TTL 内应命中缓存（旧大小），实得 %d —— 缓存没生效", cached[0].Size)
	}

	s.InvalidateAllMeta()
	fresh, err := s.Metas()
	if err != nil {
		t.Fatalf("Metas(重取): %v", err)
	}
	if fresh[0].Size != int64(len("a much longer body")) {
		t.Errorf("作废后应重取到新大小 %d，实得 %d", len("a much longer body"), fresh[0].Size)
	}
}

// 条目增删要顺带作废缓存（saveItems 单点挂钩），否则新纳管的条目会命中空/旧表。
func TestSaveItemsInvalidatesMeta(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	a := writeFile(t, dir, "a.txt", "1")
	b := writeFile(t, dir, "b.txt", "22")
	addItem(t, s, a)
	if _, err := s.Metas(); err != nil {
		t.Fatalf("Metas: %v", err)
	}
	addItem(t, s, b) // 走 saveItems
	metas, err := s.Metas()
	if err != nil {
		t.Fatalf("Metas: %v", err)
	}
	if len(metas) != 2 {
		t.Fatalf("新条目应出现在元数据里，实得 %d 条", len(metas))
	}
}

func TestStatsAndKindBreakdownOrder(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	for _, n := range []string{"d1.md", "d2.md", "s.xlsx", "p.pdf", "i.png", "z.unknownext"} {
		addItem(t, s, writeFile(t, dir, n, "xxxx"))
	}
	st, err := s.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if st.Total != 6 {
		t.Errorf("total = %d, 期望 6", st.Total)
	}
	if st.ByKind[filekind.KindText] != 2 {
		t.Errorf("text 计数 = %d, 期望 2", st.ByKind[filekind.KindText])
	}
	if st.TotalSize != 6*4 {
		t.Errorf("totalSize = %d, 期望 24", st.TotalSize)
	}
	if st.Updated7d != 6 {
		t.Errorf("updated7d = %d, 期望 6（刚写入的文件都在一周内）", st.Updated7d)
	}

	list := st.KindBreakdown()
	if len(list) == 0 {
		t.Fatal("KindBreakdown 为空")
	}
	// 必须按 filekind.Order 排，且不含数量为 0 的类型
	seen := map[string]int{}
	lastIdx := -1
	for _, kc := range list {
		if kc.Num <= 0 {
			t.Errorf("breakdown 含零数量项 %q", kc.Kind)
		}
		seen[kc.Kind] = kc.Num
		idx := -1
		for i, k := range filekind.Order {
			if k == kc.Kind {
				idx = i
			}
		}
		if idx < lastIdx {
			t.Errorf("breakdown 顺序错乱：%q 出现在序 %d 之后", kc.Kind, lastIdx)
		}
		lastIdx = idx
	}
	for _, k := range []string{filekind.KindText, filekind.KindSheet, filekind.KindPDF, filekind.KindImage, filekind.KindBinary} {
		if seen[k] == 0 {
			t.Errorf("breakdown 缺少类型 %q", k)
		}
	}
}
