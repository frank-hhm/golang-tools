package gitcmd

import (
	"context"
	"regexp"
	"strconv"
	"strings"
)

// Blame 用 `git blame --line-porcelain` 取某文件（可选指定版本）的逐行归属，
// 再把连续同一提交的行聚合成段（计划 P2 BlamePane：每段作者/时间/commit）。
//   - path 必填（相对仓库根）。
//   - revision 非空则对该版本 blame（空 = 工作区当前）。
//
// --line-porcelain 对每一行都完整打印头部字段，解析简单、无需二次回查；代价是输出大，
// 但 blame 本就是单文件操作，量可控。
func Blame(ctx context.Context, repoDir, path, revision string) ([]BlameSegment, error) {
	if path == "" {
		return nil, ErrNotRepo
	}
	args := []string{"blame", "--line-porcelain"}
	if revision != "" {
		args = append(args, revision)
	}
	args = append(args, "--", path)
	out, err := Run(ctx, repoDir, args...)
	if err != nil {
		return nil, err
	}
	return parseBlame(out.Stdout), nil
}

// blameHeadRe 匹配每个块首行："<40hex> <origLine> <finalLine>[ <numLines>]"。
var blameHeadRe = regexp.MustCompile(`^([0-9a-f]{40}) (\d+) (\d+)(?: (\d+))?$`)

// parseBlame 解析 --line-porcelain 输出，把连续同 hash 的行聚成段。
// 块边界由下一个首行界定；块内 author / author-time / summary 各取一次。
func parseBlame(text string) []BlameSegment {
	segs := []BlameSegment{}
	var cur *BlameSegment
	var (
		inBlock        bool
		pendingHash    string
		pendingAuthor  string
		pendingSummary string
		pendingTime    int64
		pendingLine    int
	)
	closeBlock := func() {
		if !inBlock || pendingHash == "" {
			return
		}
		if cur != nil && cur.Hash == pendingHash {
			cur.Count++ // 与上一段同提交，续长
			return
		}
		if cur != nil && cur.Count > 0 {
			segs = append(segs, *cur)
		}
		short := pendingHash
		if len(short) > 8 {
			short = short[:8]
		}
		cur = &BlameSegment{
			Hash:      pendingHash,
			Short:     short,
			Author:    pendingAuthor,
			Time:      pendingTime,
			Summary:   pendingSummary,
			StartLine: pendingLine,
			Count:     1,
		}
	}

	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		if m := blameHeadRe.FindStringSubmatch(line); m != nil {
			closeBlock()
			pendingHash = m[1]
			pendingLine, _ = strconv.Atoi(m[3]) // 最终行号
			pendingAuthor, pendingSummary = "", ""
			pendingTime = 0
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		switch {
		case strings.HasPrefix(line, "author "):
			pendingAuthor = strings.TrimSpace(line[len("author "):])
		case strings.HasPrefix(line, "author-time "):
			pendingTime, _ = strconv.ParseInt(strings.TrimSpace(line[len("author-time "):]), 10, 64)
		case strings.HasPrefix(line, "summary "):
			pendingSummary = strings.TrimSpace(line[len("summary "):])
		}
	}
	closeBlock()
	if cur != nil && cur.Count > 0 {
		segs = append(segs, *cur)
	}
	return segs
}
