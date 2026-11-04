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

// st_parse2 重命名/复制：v2 一共 10 个空格字段 ——
//
//	2 <XY> <sub> <mH> <mI> <mW> <hH> <hI> <X><score> <newPath><TAB><origPath>
//
// score（如 R100/C100）自己占第 9 段，第 10 段才是 TAB 连着的新旧两条路径。按 9 段
// 切会把 score 和新路径粘进同一格，进而把原路径当成 Path 读出来，界面上就变成「旧文件
// 还在、新名字丢了」。路径含空格或中文不会被拆坏——这也是选 v2 而非 v1 的主要原因
// （v1 会给非 ASCII 路径外加引号，还得再解一次转义）。
func st_parse2(line string) FileEntry {
	f := strings.SplitN(line, " ", 10) // f[8]="R100", f[9]="newpath<TAB>origpath"
	if len(f) < 10 {
		return FileEntry{}
	}
	xy := f[1]
	pair := strings.SplitN(f[9], "\t", 2)
	e := FileEntry{Path: pair[0], X: at(xy, 0), Y: at(xy, 1)}
	if len(pair) == 2 {
		e.OrigPath = pair[1]
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
