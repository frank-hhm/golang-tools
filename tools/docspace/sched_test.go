package docspace

import (
	"path/filepath"
	"testing"
	"time"
)

func TestDueAt(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)

	cases := []struct {
		name string
		p    BackupPlan
		want bool
	}{
		{"仅手动(IntervalS=0)", BackupPlan{Enabled: true, IntervalS: 0}, false},
		{"负间隔视为手动", BackupPlan{Enabled: true, IntervalS: -5}, false},
		{"停用不跑", BackupPlan{Enabled: false, IntervalS: 60, LastRunAt: now.UnixMilli() - 999999}, false},
		{"到点", BackupPlan{Enabled: true, IntervalS: 60, LastRunAt: now.UnixMilli() - 61_000}, true},
		{"恰好压线", BackupPlan{Enabled: true, IntervalS: 60, LastRunAt: now.UnixMilli() - 60_000}, true},
		{"未到点", BackupPlan{Enabled: true, IntervalS: 60, LastRunAt: now.UnixMilli() - 30_000}, false},
		{"从未跑过(零值)立即补", BackupPlan{Enabled: true, IntervalS: 60, LastRunAt: 0}, true},
	}
	for _, c := range cases {
		if got := dueAt(c.p, now); got != c.want {
			t.Errorf("%s: dueAt=%v, 期望 %v", c.name, got, c.want)
		}
	}
}

// TestSetBackupInterval 验证间隔可改、负值归零、不存在报错。
func TestSetBackupInterval(t *testing.T) {
	s := newTestStore(t)
	src := t.TempDir()
	p, err := s.AddBackup("计划", src, filepath.Join(t.TempDir(), "dst"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetBackupInterval(p.ID, 3600); err != nil {
		t.Fatal(err)
	}
	if err := s.SetBackupInterval("b-nope", 60); err == nil {
		t.Fatal("不存在的计划应报错")
	}
	plans, err := s.ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if plans[0].IntervalS != 3600 {
		t.Fatalf("间隔未生效: %d", plans[0].IntervalS)
	}
	// 负值归零（= 仅手动）
	if err := s.SetBackupInterval(p.ID, -1); err != nil {
		t.Fatal(err)
	}
	plans, _ = s.ListBackups()
	if plans[0].IntervalS != 0 {
		t.Fatalf("负值应归零: %d", plans[0].IntervalS)
	}
	// 落盘往返：新建 Store 指向同目录应读到持久化值
	s2 := &Store{root: s.root, items: map[string]*Item{}}
	if err := s2.Load(); err != nil {
		t.Fatal(err)
	}
	plans, err = s2.ListBackups()
	if err != nil {
		t.Fatal(err)
	}
	if plans[0].IntervalS != 0 {
		t.Fatalf("持久化往返失败: %d", plans[0].IntervalS)
	}
}
