package docspace

import (
	"path/filepath"
	"strings"
	"testing"

	"Tools/tools/filekind"
)

// ---------- 批量改分类 ----------

func TestMoveItemsCategoryBatch(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	a := addItem(t, s, writeFile(t, dir, "a.txt", "1"))
	b := addItem(t, s, writeFile(t, dir, "b.txt", "2"))
	c := addItem(t, s, writeFile(t, dir, "c.txt", "3"))

	cat, err := s.AddCategory("合同", "")
	if err != nil {
		t.Fatalf("AddCategory: %v", err)
	}

	// 空 id 列表不能触发写盘，也不能报错
	if n, err := s.MoveItemsCategory(nil, cat.ID); err != nil || n != 0 {
		t.Errorf("空批量 = %d/%v, 期望 0/nil", n, err)
	}
	// 重复 id 只算一条
	if n, err := s.MoveItemsCategory([]string{a.ID, a.ID, b.ID}, cat.ID); err != nil || n != 2 {
		t.Fatalf("移动两条 = %d/%v, 期望 2/nil", n, err)
	}

	items, err := s.Items()
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	inCat := 0
	for _, it := range items {
		if it.Category == cat.ID {
			inCat++
		}
	}
	if inCat != 2 {
		t.Errorf("分类内条目 = %d, 期望 2", inCat)
	}

	// 已在目标分类里的不再计入"改动"，也不该被刷更新时间
	before, _ := s.Items()
	var stamp int64
	for _, it := range before {
		if it.ID == a.ID {
			stamp = it.UpdatedAt
		}
	}
	if n, err := s.MoveItemsCategory([]string{a.ID}, cat.ID); err != nil || n != 0 {
		t.Errorf("重复移动 = %d/%v, 期望 0/nil", n, err)
	}
	after, _ := s.Items()
	for _, it := range after {
		if it.ID == a.ID && it.UpdatedAt != stamp {
			t.Error("无变化的批量不该刷新 UpdatedAt（否则列表会跳到最新）")
		}
	}

	// 不存在的分类必须整批拒绝：分片按分类命名，写进去条目就从树上消失了
	if _, err := s.MoveItemsCategory([]string{c.ID}, "c_not_exist"); err == nil {
		t.Error("移动到不存在的分类应报错")
	} else if !strings.Contains(err.Error(), "分类不存在") {
		t.Errorf("报错文案不对: %v", err)
	}
	// 从分类移回未分类：合法（c 本来就在未分类，所以拿 a 来试）
	if n, err := s.MoveItemsCategory([]string{a.ID}, ""); err != nil || n != 1 {
		t.Errorf("移到未分类 = %d/%v, 期望 1/nil", n, err)
	}
	// 本来就在未分类的条目再移一次不算改动
	if n, err := s.MoveItemsCategory([]string{c.ID}, ""); err != nil || n != 0 {
		t.Errorf("重复移到未分类 = %d/%v, 期望 0/nil", n, err)
	}
}

func TestSetItemCategorySingle(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	it := addItem(t, s, writeFile(t, dir, "a.txt", "1"))
	cat, err := s.AddCategory("报销", "")
	if err != nil {
		t.Fatalf("AddCategory: %v", err)
	}
	if err := s.SetItemCategory(it.ID, cat.ID); err != nil {
		t.Fatalf("SetItemCategory: %v", err)
	}
	items, _ := s.Items()
	if items[0].Category != cat.ID {
		t.Errorf("分类 = %q, 期望 %q", items[0].Category, cat.ID)
	}
	// 条目不存在必须报错，不能静默成功
	if err := s.SetItemCategory("i_missing", cat.ID); err == nil {
		t.Error("不存在的条目应报错")
	}
}

// ---------- 批量标签 ----------

func TestTagItemsAddAndRemove(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	a := addItem(t, s, writeFile(t, dir, "a.txt", "1"))
	b := addItem(t, s, writeFile(t, dir, "b.txt", "2"))
	if err := s.SetItemTags(a.ID, []string{"旧标签", "共用"}); err != nil {
		t.Fatalf("SetItemTags: %v", err)
	}

	// 加：与已有标签取并集且去重
	if n, err := s.TagItems([]string{a.ID, b.ID}, []string{"重要", " 重要 ", ""}, nil); err != nil || n != 2 {
		t.Fatalf("加标签 = %d/%v, 期望 2/nil", n, err)
	}
	items, _ := s.Items()
	for _, it := range items {
		if len(it.Tags) == 0 {
			t.Fatalf("条目 %s 没打上标签", it.ID)
		}
		seen := map[string]int{}
		for _, tag := range it.Tags {
			seen[tag]++
		}
		if seen["重要"] != 1 {
			t.Errorf("%s 标签里「重要」出现 %d 次, 期望 1（必须去重）", it.ID, seen["重要"])
		}
	}

	// 减：只去掉指定标签，其余保留
	if n, err := s.TagItems([]string{a.ID}, nil, []string{"旧标签"}); err != nil || n != 1 {
		t.Fatalf("减标签 = %d/%v, 期望 1/nil", n, err)
	}
	items, _ = s.Items()
	for _, it := range items {
		if it.ID != a.ID {
			continue
		}
		if strings.Join(it.Tags, ",") != "共用,重要" {
			t.Errorf("减后标签 = %v, 期望 [共用 重要]", it.Tags)
		}
	}

	// add/remove 都空 → 不写盘不报错
	if n, err := s.TagItems([]string{a.ID}, []string{"  "}, nil); err != nil || n != 0 {
		t.Errorf("空操作 = %d/%v, 期望 0/nil", n, err)
	}
	// 加减互相抵消 → 无变化
	if n, err := s.TagItems([]string{a.ID}, []string{"临时"}, []string{"临时"}); err != nil || n != 0 {
		t.Errorf("自抵消 = %d/%v, 期望 0/nil", n, err)
	}
}

// ---------- 批量取消纳管 ----------

func TestRemoveItemsBatch(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	a := addItem(t, s, writeFile(t, dir, "a.txt", "1"))
	b := addItem(t, s, writeFile(t, dir, "b.txt", "2"))
	addItem(t, s, writeFile(t, dir, "c.txt", "3"))

	n, err := s.RemoveItems([]string{a.ID, b.ID, "i_gone"})
	if err != nil {
		t.Fatalf("RemoveItems: %v", err)
	}
	if n != 2 {
		t.Errorf("删除条数 = %d, 期望 2（不存在的 id 不计入）", n)
	}
	items, err := s.Items()
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(items) != 1 || items[0].ID == a.ID || items[0].ID == b.ID {
		t.Errorf("剩余条目不对: %+v", items)
	}
	// 取消纳管后派生元数据必须重取（saveItems 单点作废）
	metas, err := s.Metas()
	if err != nil {
		t.Fatalf("Metas: %v", err)
	}
	if len(metas) != 1 {
		t.Errorf("Metas = %d 条, 期望 1", len(metas))
	}
}

// ---------- 批量回收站 / 快照 ----------

func TestTrashItemsSkipsNonLocal(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	ok := addItem(t, s, writeFile(t, dir, "keep.txt", "hello"))
	remote, err := s.AddItem(Ref{Src: SrcSFTP, SessionID: "s1", Path: "/srv/x.txt"}, "")
	if err != nil {
		t.Fatalf("纳管远端条目: %v", err)
	}
	missing := addItem(t, s, filepath.Join(dir, "never-existed.txt"))

	res, err := s.TrashItems([]string{ok.ID, remote.ID, missing.ID})
	if err != nil {
		t.Fatalf("TrashItems: %v", err)
	}
	if res.Done != 1 {
		t.Errorf("成功数 = %d, 期望 1", res.Done)
	}
	if len(res.Failed) != 2 {
		t.Errorf("失败明细 = %v, 期望 2 条（远端 + 实体不存在）", res.Failed)
	}
	// 失败条目必须还在列表里，不能被"半成功"吞掉
	items, _ := s.Items()
	left := map[string]bool{}
	for _, it := range items {
		left[it.ID] = true
	}
	if !left[remote.ID] || !left[missing.ID] {
		t.Errorf("失败的条目应原样保留, 实得 %+v", items)
	}
	if left[ok.ID] {
		t.Error("成功的条目应已从列表移除")
	}
	if res.Reason == "" {
		t.Error("有失败时必须给出原因线索")
	}
	if res.Done == 0 {
		t.Error("本机条目应能成功移入回收站")
	}
}

func TestSnapshotItemsBatch(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	a := addItem(t, s, writeFile(t, dir, "a.md", "one"))
	b := addItem(t, s, writeFile(t, dir, "b.md", "two"))
	ghost := addItem(t, s, filepath.Join(dir, "gone.md"))

	res, err := s.SnapshotItems([]string{a.ID, b.ID, ghost.ID}, "批量存档", 5)
	if err != nil {
		t.Fatalf("SnapshotItems: %v", err)
	}
	if res.Done != 2 {
		t.Errorf("成功数 = %d, 期望 2", res.Done)
	}
	if len(res.Failed) != 1 || res.Failed[0] != ghost.ID {
		t.Errorf("失败明细 = %v, 期望 [%s]", res.Failed, ghost.ID)
	}
	vers, err := s.ListVersions(a.ID)
	if err != nil {
		t.Fatalf("ListVersions: %v", err)
	}
	if len(vers) != 1 || vers[0].Note != "批量存档" {
		t.Errorf("快照结果不对: %+v", vers)
	}
	// 快照改变了实体状态（版本号），派生元数据必须作废重取
	items, _ := s.Items()
	for _, it := range items {
		if it.ID == a.ID && it.Version != 1 {
			t.Errorf("条目版本号 = %d, 期望 1", it.Version)
		}
	}
}

// ---------- 总览 ----------

func TestOverviewCountsAndRollup(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()

	root1, err := s.AddCategory("项目", "")
	if err != nil {
		t.Fatalf("AddCategory: %v", err)
	}
	child, err := s.AddCategory("子项目", root1.ID)
	if err != nil {
		t.Fatalf("AddCategory: %v", err)
	}
	if _, err := s.AddCategory("空分类", ""); err != nil {
		t.Fatalf("AddCategory: %v", err)
	}

	p1 := addItem(t, s, writeFile(t, dir, "p1.md", "aaa")) // → 项目
	if err := s.SetItemCategory(p1.ID, root1.ID); err != nil {
		t.Fatalf("SetItemCategory: %v", err)
	}
	p2 := addItem(t, s, writeFile(t, dir, "p2.xlsx", "bbbb")) // → 子项目
	if err := s.SetItemCategory(p2.ID, child.ID); err != nil {
		t.Fatalf("SetItemCategory: %v", err)
	}
	addItem(t, s, writeFile(t, dir, "u1.png", "cc")) // 未分类

	ov, err := s.Overview()
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if ov.Stats.Total != 3 {
		t.Errorf("total = %d, 期望 3", ov.Stats.Total)
	}
	if ov.Stats.TotalSize != 3+4+2 {
		t.Errorf("totalSize = %d, 期望 9", ov.Stats.TotalSize)
	}

	byID := map[string]CategoryCount{}
	for _, c := range ov.Categories {
		byID[c.ID] = c
	}
	if c := byID[root1.ID]; c.Num != 1 || c.Inclusive != 2 {
		t.Errorf("父分类 num/inclusive = %d/%d, 期望 1/2（含子级）", c.Num, c.Inclusive)
	}
	if c := byID[child.ID]; c.Num != 1 || c.Inclusive != 1 {
		t.Errorf("子分类 num/inclusive = %d/%d, 期望 1/1", c.Num, c.Inclusive)
	}
	if c := byID[""]; c.Num != 1 || c.Name != "未分类" || c.ID != "" {
		t.Errorf("未分类 = %+v, 期望 id 空串、数量 1", c)
	}
	// 零数量的分类也要出现在表里，否则左树徽标对不上节点
	var empty *CategoryCount
	for i, c := range ov.Categories {
		if c.Name == "空分类" {
			empty = &ov.Categories[i]
		}
	}
	if empty == nil {
		t.Error("零数量分类被丢掉了，树徽标会缺节点")
	} else if empty.Num != 0 || empty.Inclusive != 0 {
		t.Errorf("空分类计数 = %d/%d, 期望 0/0", empty.Num, empty.Inclusive)
	}

	if len(ov.Sources) != 1 || ov.Sources[0].Src != SrcLocal || ov.Sources[0].Num != 3 {
		t.Errorf("源分布 = %+v, 期望 local 3", ov.Sources)
	}

	// 类型 chips：png 是 image、xlsx 是 sheet、md 是 text；带中文标签
	labels := map[string]string{}
	for _, k := range ov.Kinds {
		labels[k.Kind] = k.Label
	}
	for _, want := range []string{filekind.KindImage, filekind.KindSheet, filekind.KindText} {
		if labels[want] == "" {
			t.Errorf("类型 %q 缺少标签（KindBreakdown 应带 Label）", want)
		}
	}
	if labels[filekind.KindImage] != "图片" {
		t.Errorf("图片标签 = %q, 期望 图片", labels[filekind.KindImage])
	}

	// 热门格式按数量降序：三个扩展名各 1 条，平局按字典序
	if len(ov.TopExts) != 3 {
		t.Fatalf("TopExts = %+v, 期望 3 项", ov.TopExts)
	}
	if ov.TopExts[0].Ext != "md" {
		t.Errorf("平局时首个 = %q, 期望 md（字典序）", ov.TopExts[0].Ext)
	}
	for _, e := range ov.TopExts {
		if e.Num != 1 {
			t.Errorf("扩展名 %q 计数 = %d, 期望 1", e.Ext, e.Num)
		}
	}
}
