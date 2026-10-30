package archive

import "testing"

// 挂载状态的合并逻辑单独钉：它决定设置页上显示「已挂载 / 指向旧路径 /
// 未挂载」的哪一种，判错一次用户就会以为右键菜单坏了（或反之）。
// 注册表读写本身要真机，这里只测不依赖平台的那半。

func TestWorstSlot(t *testing.T) {
	cases := []struct {
		name  string
		slots []int
		want  int
	}{
		{"全好", []int{SlotCurrent, SlotCurrent}, SlotCurrent},
		{"空清单不算挂好", nil, SlotAbsent},
		{"少一个就是没挂好", []int{SlotCurrent, SlotAbsent}, SlotAbsent},
		{"缺了一项就报未挂载（哪怕另一项只是指错）", []int{SlotCurrent, SlotForeign, SlotAbsent}, SlotAbsent},
		{"没缺项时，指错就整体报指错", []int{SlotCurrent, SlotForeign}, SlotForeign},
		{"读不出来压过一切", []int{SlotCurrent, SlotForeign, SlotDamaged}, SlotDamaged},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := worstSlot(c.slots); got != c.want {
				t.Fatalf("worstSlot(%v) = %d，期望 %d", c.slots, got, c.want)
			}
		})
	}
}

func TestShellInfoAggregates(t *testing.T) {
	mounted := ShellInfo{Compress: SlotCurrent, Extract: SlotCurrent, SendTo: SlotCurrent}
	if !AllMounted(mounted) {
		t.Fatal("三项都对却判成未挂载")
	}
	if NeedsRepair(mounted) {
		t.Fatal("路径没变，不该报要修复")
	}
	// SendTo 没挂上就不算"全挂好"：否则设置页会显示已挂载，用户却找不到菜单项
	if AllMounted(ShellInfo{Compress: SlotCurrent, Extract: SlotCurrent, SendTo: SlotAbsent}) {
		t.Fatal("SendTo 缺失时不该判成全挂载")
	}
	// 修复只认 Stale：被别的程序占了位置时擅自覆写不是自修复，是抢地盘
	if NeedsRepair(ShellInfo{Compress: SlotDamaged}) {
		t.Fatal("读取失败不该走静默重写")
	}
	if !NeedsRepair(ShellInfo{Compress: SlotForeign, Stale: true}) {
		t.Fatal("换了路径要报需修复")
	}
}

func TestSlotTextCoversEveryState(t *testing.T) {
	// 漏一个态就会掉进 default 显示「未挂载」，那是最坏的一种误导
	for _, s := range []int{SlotAbsent, SlotCurrent, SlotForeign, SlotDamaged} {
		if SlotText(s) == "" {
			t.Fatalf("槽位 %d 没有文案", s)
		}
	}
	if SlotText(99) != "未挂载" {
		t.Fatalf("未知态应回落到未挂载，实得 %q", SlotText(99))
	}
}
