package docspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"Tools/services/sysx"
)

func mkFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSetItemTagsAndAllTags(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	it, err := s.AddItem(Ref{Src: SrcLocal, Path: mkFile(t, dir, "a.txt", "x")}, "")
	if err != nil {
		t.Fatal(err)
	}

	// 规范化：去空白、去重、丢空串
	if err := s.SetItemTags(it.ID, []string{" 重要 ", "重要", "", "发票", "  "}); err != nil {
		t.Fatal(err)
	}
	items, _ := s.Items()
	if len(items) != 1 || strings.Join(items[0].Tags, ",") != "重要,发票" {
		t.Fatalf("标签规范化不对: %+v", items[0].Tags)
	}

	// 词表跨条目聚合且有序
	it2, _ := s.AddItem(Ref{Src: SrcLocal, Path: mkFile(t, dir, "b.txt", "y")}, "")
	_ = s.SetItemTags(it2.ID, []string{"合同"})
	tags, err := s.AllTags()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tags, ",") != "发票,合同,重要" {
		t.Fatalf("AllTags 应为去重且按码位有序的词语表, got %v", tags)
	}
}

func TestSnapshotAndRestoreRoundTrip(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	p := mkFile(t, dir, "note.md", "V1")
	it, err := s.AddItem(Ref{Src: SrcLocal, Path: p}, "")
	if err != nil {
		t.Fatal(err)
	}

	// 存 v1（内容 V1）
	v1, err := s.SnapshotItem(it.ID, "初始", 0)
	if err != nil {
		t.Fatal(err)
	}
	if v1.N != 1 || v1.Size != 2 {
		t.Fatalf("首个快照不对: %+v", v1)
	}

	// 改文件为 V2，再还原到 v1
	if err := os.WriteFile(p, []byte("V2-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreVersion(it.ID, 1); err != nil {
		t.Fatalf("还原失败: %v", err)
	}

	// 磁盘内容应回到 V1
	if b, _ := os.ReadFile(p); string(b) != "V1" {
		t.Fatalf("还原后文件内容应为 V1, got %q", string(b))
	}

	// 还原前会把「当前内容(V2-content)」自动存一版 → 共 2 版
	vers, _ := s.ListVersions(it.ID)
	if len(vers) != 2 {
		t.Fatalf("应有 2 个版本（v1 + 还原前自动存 v2）, got %d", len(vers))
	}
	if !strings.Contains(vers[1].Note, "自动保存") {
		t.Fatalf("v2 应是还原前自动保存: %+v", vers[1])
	}

	// 条目 Version 递增到 2
	items, _ := s.Items()
	if items[0].Version != 2 {
		t.Fatalf("条目版本应升到 2, got %d", items[0].Version)
	}
}

func TestSnapshotRejectsOversize(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	// 造一个超过上限的文件（128MB+1），用稀疏写避免真占磁盘：直接 truncate 到超大
	p := filepath.Join(dir, "big.bin")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(snapshotMaxBytes + 1); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	it, _ := s.AddItem(Ref{Src: SrcLocal, Path: p}, "")
	if _, err := s.SnapshotItem(it.ID, "", 0); err == nil {
		t.Fatal("超大文件快照必须被拒绝")
	}
}

func TestTrashMoveAndRestore(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	p := mkFile(t, dir, "old.txt", "data")
	it, err := s.AddItem(Ref{Src: SrcLocal, Path: p}, "catX")
	if err != nil {
		t.Fatal(err)
	}

	if err := s.MoveToTrash(it.ID); err != nil {
		t.Fatalf("移入回收站失败: %v", err)
	}
	// 原路径消失、回收站有实体与登记、条目从 items 移除
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatal("移入后原路径应不存在")
	}
	entity := filepath.Join(s.root, "trash", it.ID, "old.txt")
	if _, err := os.Stat(entity); err != nil {
		t.Fatalf("回收站实体缺失: %v", err)
	}
	trash, _ := s.ListTrash()
	if len(trash) != 1 || trash[0].Category != "catX" {
		t.Fatalf("回收站登记不对: %+v", trash)
	}
	items, _ := s.Items()
	if len(items) != 0 {
		t.Fatal("移入回收站后条目应消失")
	}

	// 还原
	restored, err := s.RestoreTrash(it.ID)
	if err != nil {
		t.Fatalf("还原失败: %v", err)
	}
	if b, _ := os.ReadFile(p); string(b) != "data" {
		t.Fatalf("还原后内容不对: %q", string(b))
	}
	if restored.Category != "catX" {
		t.Fatalf("还原应沿用原分类, got %q", restored.Category)
	}
	trash, _ = s.ListTrash()
	if len(trash) != 0 {
		t.Fatal("还原后回收站应清空该条目")
	}
	items, _ = s.Items()
	if len(items) != 1 {
		t.Fatal("还原后应重新登记 1 个条目")
	}
}

func TestRestoreTrashRejectsOccupiedOrigin(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	p := mkFile(t, dir, "dup.txt", "data")
	it, _ := s.AddItem(Ref{Src: SrcLocal, Path: p}, "")
	if err := s.MoveToTrash(it.ID); err != nil {
		t.Fatal(err)
	}
	// 原路径被重新占用 → 还原必须失败，不覆盖
	mkFile(t, dir, "dup.txt", "occupied")
	if _, err := s.RestoreTrash(it.ID); err == nil {
		t.Fatal("原路径被占用时还原应失败")
	}
}

func TestTrashRejectsSFTP(t *testing.T) {
	s := newTestStore(t)
	it, err := s.AddItem(Ref{Src: SrcSFTP, SessionID: "sess", Path: "/remote/x.txt"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MoveToTrash(it.ID); err != sysx.ErrUnsupported {
		t.Fatalf("sftp 源移入本机回收站应返回 ErrUnsupported, got %v", err)
	}
}

func TestSnapshotPrunesToKeep(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	p := mkFile(t, dir, "f.txt", "c1")
	it, err := s.AddItem(Ref{Src: SrcLocal, Path: p}, "")
	if err != nil {
		t.Fatal(err)
	}
	// 连续存 5 版，保留上限 3：最终只应剩最新 3 版（v3..v5）
	for i := 1; i <= 5; i++ {
		if err := os.WriteFile(p, []byte(strings.Repeat("x", i)), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SnapshotItem(it.ID, "", 3); err != nil {
			t.Fatal(err)
		}
	}
	vers, _ := s.ListVersions(it.ID)
	if len(vers) != 3 {
		t.Fatalf("应按 keep 裁到 3 版, got %d", len(vers))
	}
	if vers[0].N != 3 || vers[2].N != 5 {
		t.Fatalf("应保留最新 3 版 v3..v5, got %+v", vers)
	}
	// 被裁掉的 v1/v2 实体文件应已从磁盘删除
	if _, err := os.Stat(filepath.Join(s.root, "versions", it.ID, "1.txt")); !os.IsNotExist(err) {
		t.Fatal("被裁剪的旧快照实体应已删除")
	}
}

func TestPurgeTrashByAge(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	it, _ := s.AddItem(Ref{Src: SrcLocal, Path: mkFile(t, dir, "a.txt", "1")}, "")
	if err := s.MoveToTrash(it.ID); err != nil {
		t.Fatal(err)
	}
	// 把删除时间伪造成 100 天前，再按保留 30 天清理 → 应清掉 1 条
	for i := range s.trash {
		s.trash[i].DeletedAt = time.Now().Add(-100 * 24 * time.Hour).UnixMilli()
	}
	_ = s.saveTrash()
	removed, err := s.PurgeTrash(30)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("应清理 1 条超期实体, got %d", removed)
	}
	trash, _ := s.ListTrash()
	if len(trash) != 0 {
		t.Fatalf("超期条目应被清除, got %d", len(trash))
	}
}

func TestPurgeTrashKeepsRecent(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	it, _ := s.AddItem(Ref{Src: SrcLocal, Path: mkFile(t, dir, "a.txt", "1")}, "")
	_ = s.MoveToTrash(it.ID)
	removed, err := s.PurgeTrash(30) // 刚移入，未超期
	if err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatalf("未超期不应清理, got %d", removed)
	}
	trash, _ := s.ListTrash()
	if len(trash) != 1 {
		t.Fatalf("未超期条目应保留, got %d", len(trash))
	}
}

func TestDeleteAndEmptyTrash(t *testing.T) {
	s := newTestStore(t)
	dir := t.TempDir()
	it1, _ := s.AddItem(Ref{Src: SrcLocal, Path: mkFile(t, dir, "a.txt", "1")}, "")
	it2, _ := s.AddItem(Ref{Src: SrcLocal, Path: mkFile(t, dir, "b.txt", "2")}, "")
	_ = s.MoveToTrash(it1.ID)
	_ = s.MoveToTrash(it2.ID)

	// 彻底删除一个：实体目录应消失
	if err := s.DeleteTrashEntry(it1.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.root, "trash", it1.ID)); !os.IsNotExist(err) {
		t.Fatal("彻底删除后回收站实体目录应消失")
	}
	// 清空
	if err := s.EmptyTrash(); err != nil {
		t.Fatal(err)
	}
	trash, _ := s.ListTrash()
	if len(trash) != 0 {
		t.Fatalf("清空后回收站应为空, got %d", len(trash))
	}
}
