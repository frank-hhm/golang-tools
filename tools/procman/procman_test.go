package procman

import (
	"os"
	"testing"
)

func TestCpuPct(t *testing.T) {
	// 4 核机器上 2 秒墙钟内烧掉 8 秒 CPU 时间 = 100%
	if got := cpuPct(0, 8, 2, 4); got != 100 {
		t.Fatalf("期望 100，得到 %v", got)
	}
	// 半载
	if got := cpuPct(10, 12, 2, 4); got != 25 {
		t.Fatalf("期望 25，得到 %v", got)
	}
	// 冷启动（无墙钟）/ 时钟回退都归 0
	if got := cpuPct(5, 3, 1, 4); got != 0 {
		t.Fatalf("负差应归 0，得到 %v", got)
	}
	if got := cpuPct(5, 9, 0, 4); got != 0 {
		t.Fatalf("零墙钟应归 0，得到 %v", got)
	}
}

func TestDescendants(t *testing.T) {
	children := map[int][]int{
		4:   {10, 20},
		10:  {11, 12},
		12:  {13},
		999: {10}, // 另一个父（树以 pid 为根不受影响）
	}
	got := descendants(children, 10)
	want := map[int]bool{10: true, 11: true, 12: true, 13: true}
	if len(got) != len(want) {
		t.Fatalf("期望 %v，得到 %v", want, got)
	}
	for _, p := range got {
		if !want[p] {
			t.Fatalf("多了 %d（%v）", p, got)
		}
	}
	// 根自身不在表里：只回自己
	only := descendants(children, 77)
	if len(only) != 1 || only[0] != 77 {
		t.Fatalf("孤立 pid 应只回自身，得到 %v", only)
	}
	// System(4) 自身不进可杀集合，但它的子树照常展开：{10,20,11,12,13}
	if got := descendants(children, 4); len(got) != 5 {
		t.Fatalf("从 4 展开应得 5 个（排除 4 自身），得到 %d: %v", len(got), got)
	}
	// 环保护：a->b->a 也只各访问一次
	cyc := map[int][]int{5: {6}, 6: {5}}
	if got := descendants(cyc, 5); len(got) != 2 {
		t.Fatalf("环应只访问两次，得到 %v", got)
	}
	// 自身进程必须被剔除
	self := os.Getpid()
	withSelf := map[int][]int{1: {2, self}}
	for _, p := range descendants(withSelf, 1) {
		if p == self {
			t.Fatal("不应把自己列进可杀集合")
		}
	}
}

// TestSnapshotLoads 验证包初始化与快照入口在测试机上可跑通（跨平台冒烟）。
func TestSnapshotLoads(t *testing.T) {
	snap := SnapshotProcs()
	if len(snap.Procs) == 0 {
		t.Fatal("快照拿不到任何进程")
	}
	if snap.Sum.ProcCount != len(snap.Procs) {
		t.Fatalf("汇总进程数 %d 与列表 %d 不一致", snap.Sum.ProcCount, len(snap.Procs))
	}
	// 二次快照走 CPU 差分路径，不应 panic
	if s2 := SnapshotProcs(); len(s2.Procs) == 0 {
		t.Fatal("二次快照为空")
	}
}
