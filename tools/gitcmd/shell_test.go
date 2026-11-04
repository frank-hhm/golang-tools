package gitcmd

import "testing"

// TestParseShellArgs 只测命令行分流的纯逻辑（真 git 无关，三平台都能跑）：
// 认自家 --git-* 精确标记、且与压缩的 --archive/--extract/--archive-panel 互不误认。
func TestParseShellArgs(t *testing.T) {
	cases := []struct {
		name string
		args []string
		op   string
		repo string
		ok   bool
	}{
		{"命中状态带路径", []string{"exe", "--git-status", `C:\repo`}, "status", `C:\repo`, true},
		{"命中提交带路径", []string{"exe", "--git-commit", "/home/u/proj"}, "commit", "/home/u/proj", true},
		{"空白处无 token（靠 cwd）", []string{"exe", "--git-branch"}, "branch", "", true},
		{"后面还跟别的标记不误吞", []string{"exe", "--git-push", "D:\\x", "--extra"}, "push", "D:\\x", true},
		{"压缩标记不误认", []string{"exe", "--archive", "a.zip"}, "", "", false},
		{"面板标记不误认", []string{"exe", "--archive-panel", "a.txt"}, "", "", false},
		{"普通启动", []string{"exe"}, "", "", false},
		{"路径为空串丢弃", []string{"exe", "--git-pull", "  "}, "pull", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			op, repo, ok := ParseShellArgs(c.args)
			if ok != c.ok || op != c.op || repo != c.repo {
				t.Fatalf("ParseShellArgs(%q) = (%q,%q,%v)，想要 (%q,%q,%v)", c.args, op, repo, ok, c.op, c.repo, c.ok)
			}
		})
	}
}

// TestOpForArg 标记 → 操作名映射齐整，且不认识压缩标记。
func TestOpForArg(t *testing.T) {
	want := map[string]string{
		"--git-status": "status",
		"--git-commit": "commit",
		"--git-push":   "push",
		"--git-pull":   "pull",
		"--git-branch": "branch",
	}
	for arg, op := range want {
		got, ok := OpForArg(arg)
		if !ok || got != op {
			t.Errorf("OpForArg(%q) = (%q,%v)，想要 (%q,true)", arg, got, ok, op)
		}
	}
	for _, foreign := range []string{"--archive", "--extract", "--archive-panel", "--git", ""} {
		if _, ok := OpForArg(foreign); ok {
			t.Errorf("OpForArg(%q) 不该命中 Git 标记", foreign)
		}
	}
}

// TestWorstSlot 聚合取最差：缺一个就不算挂好，读不出优先于指错。
func TestWorstSlot(t *testing.T) {
	if got := worstSlot([]int{SlotCurrent, SlotCurrent}); got != SlotCurrent {
		t.Errorf("全当前应为 current，得 %d", got)
	}
	if got := worstSlot([]int{SlotCurrent, SlotForeign}); got != SlotForeign {
		t.Errorf("含指回应为 foreign，得 %d", got)
	}
	if got := worstSlot([]int{SlotForeign, SlotDamaged}); got != SlotDamaged {
		t.Errorf("含读不出应为 damaged（优先于指错），得 %d", got)
	}
	if got := worstSlot([]int{SlotCurrent, SlotAbsent}); got != SlotAbsent {
		t.Errorf("含未挂应为 absent，得 %d", got)
	}
	if got := worstSlot(nil); got != SlotAbsent {
		t.Errorf("空集应为 absent，得 %d", got)
	}
}
