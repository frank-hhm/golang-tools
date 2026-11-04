package docspace

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// newTestStore 造一个指向临时目录、脱离全局单例的 Store，保证用例互不干扰。
func newTestStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	s := &Store{root: dir, items: map[string]*Item{}}
	if err := s.ensureDirs(); err != nil {
		t.Fatalf("ensureDirs: %v", err)
	}
	return s
}

func TestParseDefaultsAndValidation(t *testing.T) {
	// src 空 → 默认 local（旧 ?path= 兼容的关键）
	r, err := Parse("", "", "C:/a/b.txt")
	if err != nil {
		t.Fatalf("空 src 应默认为 local: %v", err)
	}
	if r.Src != SrcLocal || r.Path != "C:/a/b.txt" {
		t.Fatalf("默认 local 结果不对: %+v", r)
	}

	// local 缺 path → 报错
	if _, err := Parse("local", "", "  "); err == nil {
		t.Fatal("local 缺 path 必须报错")
	}

	// sftp 缺会话 id → 报错
	if _, err := Parse("sftp", "", "/x/y"); err == nil {
		t.Fatal("sftp 缺 sid 必须报错")
	}
	// sftp 缺 path → 报错
	if _, err := Parse("sftp", "sid1", ""); err == nil {
		t.Fatal("sftp 缺 path 必须报错")
	}

	// webdav：合法 http(s) 通过；非法 URL 报错（两者都不静默当 local）
	if _, err := Parse("webdav", "", "http://h/a"); err != nil {
		t.Fatalf("合法 webdav URL 应通过: %v", err)
	}
	if _, err := Parse("webdav", "", "/x"); err == nil {
		t.Fatal("非法 webdav URL 必须报错")
	}
	// cloud：账号 id 与文件 id 缺一不可（Parse 只校验三元组，不建连、不联网）
	if _, err := Parse("cloud", "", "/x"); err == nil {
		t.Fatal("cloud 缺账号 id 必须报错")
	}
	if _, err := Parse("cloud", "acct-1", ""); err == nil {
		t.Fatal("cloud 缺文件 id 必须报错")
	}
	if rc, err := Parse("cloud", "acct-1", "file-9"); err != nil || rc.Src != SrcCloud || rc.SessionID != "acct-1" {
		t.Fatalf("合法 cloud 引用应通过校验: %+v err=%v", rc, err)
	}

	// 未知源类型
	if _, err := Parse("nfs", "", "/x"); err == nil {
		t.Fatal("未知源类型必须报错")
	}

	// src 大小写与空白被规范化
	r2, err := Parse("  SFTP ", "id", "/p")
	if err != nil || r2.Src != SrcSFTP {
		t.Fatalf("src 应规范化为小写 sftp: %+v err=%v", r2, err)
	}
}

func TestRefQueryRoundTrip(t *testing.T) {
	orig, err := Parse(SrcSFTP, "sess-9", "/data/note.md")
	if err != nil {
		t.Fatal(err)
	}
	back, err := ParseQuery(orig.Query())
	if err != nil {
		t.Fatalf("ParseQuery: %v", err)
	}
	if back != orig {
		t.Fatalf("Query 往返不一致: %+v != %+v", back, orig)
	}

	// local 也显式带 src，且 String 形式稳定
	lr, _ := Parse(SrcLocal, "", filepath.Join("tmp", "f.bin"))
	if got := lr.Query().Get("src"); got != SrcLocal {
		t.Fatalf("local 的 Query 应显式写 src=local, got %q", got)
	}
	if s := (Ref{Src: SrcSFTP, SessionID: "ab", Path: "/x/y"}).String(); s != "sftp#ab:/x/y" {
		t.Fatalf("sftp String 不对: %q", s)
	}
}

func TestWriteJSONAtomicAndLoadMissing(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "nested", "x.json")

	var out []Category
	// 读不存在的文件不算错，保持零值
	if err := loadJSON(p, &out); err != nil {
		t.Fatalf("loadJSON 不存在应为 nil err: %v", err)
	}
	if len(out) != 0 {
		t.Fatal("不存在文件应解出零值")
	}

	// 原子写会自建父目录，且不留 .part
	in := []Category{{ID: "c1", Name: "根", Sort: 0}}
	if err := writeJSONAtomic(p, in); err != nil {
		t.Fatalf("writeJSONAtomic: %v", err)
	}
	if _, err := os.Stat(p + ".part"); !os.IsNotExist(err) {
		t.Fatal("原子写后不应残留 .part 文件")
	}
	if err := loadJSON(p, &out); err != nil {
		t.Fatalf("loadJSON 写后读: %v", err)
	}
	if len(out) != 1 || out[0].ID != "c1" {
		t.Fatalf("写后读不一致: %+v", out)
	}
}

func TestStoreCategoryAndItemLifecycle(t *testing.T) {
	s := newTestStore(t)

	root, err := s.AddCategory("工作", "")
	if err != nil {
		t.Fatal(err)
	}
	child, err := s.AddCategory("合同", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	cats, err := s.Categories()
	if err != nil || len(cats) != 2 {
		t.Fatalf("分类数应为 2，得到 %d err=%v", len(cats), err)
	}

	it, err := s.AddItem(Ref{Src: SrcLocal, Path: filepath.Join("tmp", "a.txt")}, child.ID)
	if err != nil {
		t.Fatal(err)
	}
	if it.Name != "a.txt" {
		t.Fatalf("条目名应取末段 a.txt, got %q", it.Name)
	}
	if it.Ref().Path != filepath.Join("tmp", "a.txt") {
		t.Fatalf("Item.Ref 回填路径不对: %+v", it.Ref())
	}

	// 分片应按分类落在 items/ 下
	if _, err := os.Stat(filepath.Join(s.root, "items", shardName(child.ID)+".json")); err != nil {
		t.Fatalf("条目应写入对应分类分片: %v", err)
	}

	// 删除分类：其下条目退为未分类
	if err := s.RemoveCategory(child.ID); err != nil {
		t.Fatal(err)
	}
	after, err := s.Items()
	if err != nil || len(after) != 1 {
		t.Fatalf("删分类不应删条目本体登记，条目数应仍为 1，得到 %d err=%v", len(after), err)
	}
	if after[0].Category != "" {
		t.Fatalf("删除分类后条目应退为未分类, got %q", after[0].Category)
	}

	// 重命名只改 Name、id 不动：条目靠 id 引用分类，改 id 会把归属冲掉
	if err := s.RenameCategory(root.ID, "客户资料"); err != nil {
		t.Fatal(err)
	}
	cats, err = s.Categories()
	if err != nil || len(cats) != 1 || cats[0].Name != "客户资料" || cats[0].ID != root.ID {
		t.Fatalf("重命名后应 id 不变、名为「客户资料」，得到 %+v err=%v", cats, err)
	}
	if err := s.RenameCategory(root.ID, "   "); err == nil {
		t.Fatal("空分类名应报错")
	}

	// 删父分类时子分类要上提一级：旧实现只把条目退成未分类、不管子节点，
	// 子分类的 Parent 会指向已删 id，前端建树时整棵子树变成取不到的孤儿
	grand, err := s.AddCategory("2026年", root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveCategory(root.ID); err != nil {
		t.Fatal(err)
	}
	cats, err = s.Categories()
	if err != nil || len(cats) != 1 {
		t.Fatalf("删父分类后应只剩子分类，得到 %d err=%v", len(cats), err)
	}
	if cats[0].ID != grand.ID || cats[0].Parent != "" {
		t.Fatalf("子分类应上提为顶级（Parent 为空），得到 %+v", cats[0])
	}

	// 取消纳管：条目消失，但磁盘原文件（这里不存在也无妨）不受影响
	if err := s.RemoveItem(it.ID); err != nil {
		t.Fatal(err)
	}
	c, err := s.Counts()
	if err != nil || c.Items != 0 || c.Categories != 1 {
		t.Fatalf("计数不对: %+v err=%v", c, err)
	}
}

func TestStoreConcurrentAddIsRaceFree(t *testing.T) {
	s := newTestStore(t)
	var wg sync.WaitGroup
	const n = 32
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.AddItem(Ref{Src: SrcLocal, Path: filepath.Join("d", string(rune('a'+i%26))+".txt")}, "")
			if err != nil {
				t.Errorf("并发 AddItem 失败: %v", err)
			}
		}(i)
	}
	wg.Wait()
	got, err := s.Items()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != n {
		t.Fatalf("并发写入后条目数应为 %d，得到 %d", n, len(got))
	}
}
