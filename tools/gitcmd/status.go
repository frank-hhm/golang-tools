package gitcmd

import (
	"context"
	"strconv"
	"strings"
)

// StatusOf 跑 `git status --porcelain=v2 --branch` 并解析成分组结果。
// 不用 -z：v2 的重命名记录里两段路径本就用 TAB 分隔，普通换行模式对
// 「含空格/中文路径」在 core.quotepath=false 下已足够安全，解析也更直白。
func StatusOf(ctx context.Context, repoDir string) (Status, error) {
	out, err := Run(ctx, repoDir, "status", "--porcelain=v2", "--branch")
	if err != nil {
		return Status{}, err
	}
	return parseStatus(out.Stdout), nil
}

func parseStatus(text string) Status {
	st := Status{Staged: []FileEntry{}, Unstaged: []FileEntry{}, Untracked: []FileEntry{}, Conflicted: []FileEntry{}}
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "# branch.oid "):
			st.OID = strings.TrimSpace(line[len("# branch.oid "):])
		case strings.HasPrefix(line, "# branch.head "):
			st.Branch = strings.TrimSpace(line[len("# branch.head "):])
		case strings.HasPrefix(line, "# branch.upstream "):
			st.Upstream = strings.TrimSpace(line[len("# branch.upstream "):])
		case strings.HasPrefix(line, "# branch.ab "):
			st.Ahead, st.Behind = parseAB(line[len("# branch.ab "):])
		case strings.HasPrefix(line, "1 "):
			classifyEntry(st_parse1(line), &st)
		case strings.HasPrefix(line, "2 "):
			classifyEntry(st_parse2(line), &st)
		case strings.HasPrefix(line, "u "):
			e := st_parseU(line)
			st.Conflicted = append(st.Conflicted, e)
		case strings.HasPrefix(line, "? "):
			p := line[2:]
			st.Untracked = append(st.Untracked, FileEntry{Path: p, X: "?", Y: "?"})
		}
		// "! " 忽略项未请求 --ignored，不会出现
	}
	return st
}

// parseAB 解析 "+N -M"（ahead/behind）。
func parseAB(s string) (int, int) {
	f := strings.Fields(s)
	var a, b int
	for _, tk := range f {
		if strings.HasPrefix(tk, "+") {
			a, _ = strconv.Atoi(tk[1:])
		} else if strings.HasPrefix(tk, "-") {
			b, _ = strconv.Atoi(tk[1:])
		}
	}
	return a, b
}

// st_parse1 普通变更条目：type XY sub mH mI mW hH hI path（9 段，path 可含空格）。
func st_parse1(line string) FileEntry {
	f := strings.SplitN(line, " ", 9) // f[0]="1" … f[8]=path
	if len(f) < 9 {
		return FileEntry{}
	}
	xy := f[1]
	return FileEntry{Path: f[8], X: at(xy, 0), Y: at(xy, 1)}
}

// st_parse2 重命名/复制：type XY sub mH mI mW hH hI <score<TAB>new<TAB>orig>（9 段，
// score 与两段路径由 TAB 连成最后一段，路径含空格也不会被拆坏）。
func st_parse2(line string) FileEntry {
	f := strings.SplitN(line, " ", 9) // f[8]="R100<TAB>new<TAB>orig"
	if len(f) < 9 {
		return FileEntry{}
	}
	xy := f[1]
	parts := strings.Split(f[8], "\t") // [score, newpath, origpath]
	e := FileEntry{X: at(xy, 0), Y: at(xy, 1)}
	if len(parts) >= 3 {
		e.Path, e.OrigPath = parts[1], parts[2]
	} else if len(parts) == 2 {
		e.Path = parts[1]
	}
	return e
}

// st_parseU 未合并（冲突）：type XY sub m1 m2 m3 mW h1 h2 h3 path（11 段）。
func st_parseU(line string) FileEntry {
	f := strings.SplitN(line, " ", 11)
	if len(f) < 11 {
		return FileEntry{}
	}
	xy := f[1]
	return FileEntry{Path: f[10], X: at(xy, 0), Y: at(xy, 1), Conflict: true}
}

// classifyEntry 按 XY 状态码把条目分到 Staged / Unstaged。
// X=相对 HEAD（暂存区），Y=相对暂存区（工作区）；'.'/' ' 表示该侧无变化。
func classifyEntry(e FileEntry, st *Status) {
	if e.Path == "" {
		return
	}
	staged := filled(e.X)
	unstaged := filled(e.Y)
	if staged {
		st.Staged = append(st.Staged, e)
	}
	if unstaged {
		st.Unstaged = append(st.Unstaged, e)
	}
}

func filled(c string) bool { return c != "" && c != "." && c != " " }

func at(s string, i int) string {
	if i < len(s) {
		return string(s[i])
	}
	return ""
}
