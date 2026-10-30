package launcher

import "testing"

func TestScoreTextTiers(t *testing.T) {
	const target = "Cloud Book"
	cases := []struct {
		name string
		q    string
		hits bool
		min  int // 期望的分数下限（0 表示不匹配）
	}{
		{"完全相等", "Cloud Book", true, scoreExact},
		{"大小写无关", "cloud book", true, scoreExact},
		{"前缀", "Cloud", true, scorePrefix},
		{"词首", "book", true, scoreWordHead}, // 不在开头，但在某个词的开头
		{"连续包含", "lou", true, scoreInfix},
		{"子序列", "cbk", true, scoreSubseq}, // c..b..k 跳着落在 "cloud book" 上
		{"不匹配", "zzz", false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := scoreText(c.q, target, "")
			if !c.hits && s != 0 {
				t.Fatalf("期望不匹配，实得 %d", s)
			}
			if c.hits && s < c.min {
				t.Fatalf("期望 >= %d，实得 %d", c.min, s)
			}
		})
	}
}

func TestScoreTextExtAgnostic(t *testing.T) {
	// 用户敲搜索词不会带扩展名：calc 要能命中 calc.exe。
	if s := scoreText("calc", "calc.exe", `C:\Windows\System32\calc.exe`); s < scoreExact {
		t.Fatalf("去扩展名后应算完全相等，实得 %d", s)
	}
}

func TestScoreTextPathFallback(t *testing.T) {
	// 名字里没有、只在目录段里出现：给低位分数，且必须排在名字命中之后。
	byName := scoreText("report", "月度报告.docx", `C:\Users\a\Documents\月度报告.docx`)
	byPath := scoreText("report", "一季度.xlsx", `C:\Users\a\report\一季度.xlsx`)
	if byPath <= 0 {
		t.Fatal("路径命中不该得 0 分")
	}
	if byName > 0 && byPath >= byName {
		t.Fatalf("只命中路径不该压过命中名字：%d vs %d", byPath, byName)
	}
}

func TestScoreOrdering(t *testing.T) {
	want := map[string]int{}
	for _, n := range []string{"Calculator", "Calc", "MyCalcTool"} {
		want[n] = scoreText("calc", n, "")
	}
	if want["Calc"] <= want["Calculator"] {
		t.Fatalf("完全相等应压过前缀：%d vs %d", want["Calc"], want["Calculator"])
	}
	if want["Calculator"] <= want["MyCalcTool"] {
		t.Fatalf("前缀应压过中间出现：%d vs %d", want["Calculator"], want["MyCalcTool"])
	}
	// 子序列单独验：换一个真能跳着命中的输入（"calc" 在 "Call of Duty" 里连 c 都只有一枚）。
	if s := scoreText("cod", "Call of Duty", ""); s < scoreSubseq {
		t.Fatalf("\"cod\" 应子序列命中 \"Call of Duty\"，实得 %d", s)
	}
}

func TestNormNameKeepsDotPrefixNames(t *testing.T) {
	// ".gitignore" 的要点不是扩展名，切了会把文件名切成空。
	if got := normName(".gitignore"); got != ".gitignore" {
		t.Fatalf(".gitignore 被切坏了：%q", got)
	}
	if got := normName("README.MD"); got != "readme" {
		t.Fatalf("正常扩展名没切掉：%q", got)
	}
}

func TestSubseqRejectsNonASCII(t *testing.T) {
	// 中文按字节跳着匹配会得出莫名其妙的结果，必须直接拒绝。
	if subseqHit("中文", "中文文档") {
		t.Fatal("含非 ASCII 的输入不该走子序列")
	}
}
