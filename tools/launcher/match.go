package launcher

import (
	"strings"
	"unicode/utf8"
)

// ============================================================
// 匹配打分
//
// 一次输入要同时喂给应用/工具/窗口/文件四类 provider，打分口径必须统一，
// 否则"排序"就变成了"谁先被遍历到谁赢"。这里定的就是那唯一一套口径。
//
// 不做拼音首字母（"mb" 命中不了"幕布"）：引一张汉字→拼音表进来换那点手感不值，
// 真要做也是单独一层，不影响这里的分数。
// ============================================================

// 分数档位：一眼能看出"为什么这条排在前面"。
const (
	scoreExact    = 1000 // 名字完全相等
	scorePrefix   = 700  // 名字以输入开头
	scoreWordHead = 500  // 名字里某个词（空格/-/_/分隔符）的开头
	scoreInfix    = 300  // 名字里连续出现
	scoreSubseq   = 120  // 名字里跳着出现（"cbd" 命中 "Cloud Backup Drive"）
	scorePathOnly = 90   // 只在目录段里出现，名字里没有
)

// basenameBonus / dirPenalty 命中名字的加权与只命中路径的降权（按分数乘除，不叠加）。
const (
	basenameBonusPct = 130 // ×1.3
	dirPenaltyPct    = 60  // ×0.6
)

// lenPenaltyCap 命中目标越长越不像用户想要的，按长度扣分，但封顶：
// 不封顶的话一个 200 字符的深路径会被扣成负分，等于把"能搜到"变成"搜不到"。
const lenPenaltyCap = 50

// normName 归一化用于比较的名字：小写 + 去掉扩展名。
//
// 去扩展名是为了让"幕布使用"能命中"幕布使用.png"，也让"calc"命中"calc.exe"——
// 用户敲搜索框时几乎不会带上 .exe/.lnk。
func normName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if i := strings.LastIndexByte(s, '.'); i > 0 {
		// 只有像扩展名的尾巴才切：".config" 这种点开头的目录名不能当扩展名处理。
		ext := s[i+1:]
		if ext != "" && len(ext) <= 8 && !strings.ContainsAny(ext, `/\`) {
			s = s[:i]
		}
	}
	return s
}

// normPath 归一化路径：分隔符统一成 '/' 再小写。
// Windows 的反斜杠在比较时纯属噪音，还会让"大小写不敏感"的判断绕弯。
func normPath(s string) string {
	s = strings.ReplaceAll(s, "\\", "/")
	return strings.ToLower(strings.TrimSpace(s))
}

// scoreText 给一条候选打分；0 表示不匹配（调用方据此丢弃）。
// name 是展示名（应用名/文件名），path 是它背后的路径（工具路由、文件全路径）。
func scoreText(q, name, path string) int {
	nq := normName(q)
	if nq == "" {
		return 0
	}
	nn := normName(name)
	base := 0
	switch {
	case nn == nq:
		base = scoreExact
	case strings.HasPrefix(nn, nq):
		base = scorePrefix
	case wordHeadHit(nn, nq):
		base = scoreWordHead
	case strings.Contains(nn, nq):
		base = scoreInfix
	case subseqHit(nq, nn):
		base = scoreSubseq
	}
	if base > 0 {
		return base*basenameBonusPct/100 - lengthPenalty(nn)
	}
	// 名字里没有：退一步看路径。命中的往往是"报告/2026/一季度.xlsx"这类
	// 靠目录名搜文件的用法，给个低位分数，排在名字命中的后面。
	np := normPath(path)
	if np == "" {
		return 0
	}
	if strings.Contains(normNameLastSeg(np), nq) || strings.Contains(np, strings.ToLower(q)) {
		return scorePathOnly*dirPenaltyPct/100 - lengthPenalty(np)
	}
	return 0
}

// lengthPenalty 命中目标越长扣得越多，封顶 lenPenaltyCap。
func lengthPenalty(target string) int {
	n := utf8.RuneCountInString(target)
	if n > lenPenaltyCap {
		n = lenPenaltyCap
	}
	return n
}

// wordHeadHit 输入是否出现在某个词的开头（词由空格/-/_/, 分隔）。
func wordHeadHit(name, q string) bool {
	for i := 0; i+len(q) <= len(name); i++ {
		if name[i] != q[0] {
			continue
		}
		if !strings.HasPrefix(name[i:], q) {
			continue
		}
		if i == 0 || isWordSep(name[i-1]) {
			return true
		}
	}
	return false
}

func isWordSep(c byte) bool {
	switch c {
	case ' ', '-', '_', ',', '/', '(', ')', '[':
		return true
	}
	return false
}

// subseqHit q 是否是 name 的子序列（跳着命中）。
// 只对 ASCII 短输入放开：中文按字节跳着匹配会得出莫名其妙的结果。
func subseqHit(q, name string) bool {
	if len(q) < 2 || len(q) > 12 {
		return false
	}
	for i := 0; i < len(q); i++ {
		if q[i] >= utf8.RuneSelf {
			return false // 含非 ASCII 字节：不做子序列
		}
	}
	qi := 0
	for ni := 0; ni < len(name) && qi < len(q); ni++ {
		if name[ni] == q[qi] {
			qi++
		}
	}
	return qi == len(q)
}

// normNameLastSeg 取路径最后一段并归一化名字（用于"目录名也算名字"的判断）。
func normNameLastSeg(npath string) string {
	if i := strings.LastIndexByte(npath, '/'); i >= 0 {
		npath = npath[i+1:]
	}
	return normName(npath)
}
