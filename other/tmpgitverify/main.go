// Command tmpgitverify 真机验证 Tools/tools/gitcmd 对真实 git 输出的解析。
//
// 不是产品代码，用完即删。包里已有的 4 个 E2E 测试跑在 git init 造的理想小仓库上，
// 而真机风险来自真实仓库的脏数据：中文与带空格的路径、重命名、both-added 冲突、
// staged 与 unstaged 并存。分两部分：
//
//	A. 只读对拍：在一个已存在的真实仓库上（默认当前目录）跑 Discover/Status/Log/Branches，
//	   与 git 原生命令的行数、集合逐项比对，并检查有没有漏网的八进制转义。
//	B. 脏数据构造：在临时仓库里造出中文带空格路径、重命名、冲突等场景，验分类正确。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"Tools/tools/gitcmd"
)

var fails int

func main() {
	repo := flag.String("repo", "", "只读对拍的现有仓库（默认当前目录）")
	flag.Parse()
	ctx := context.Background()

	info := gitcmd.Check(ctx)
	fmt.Printf("git: found=%v ok=%v path=%s version=%s reason=%q\n\n", info.Found, info.OK, info.Path, info.Version, info.Reason)
	if !info.OK {
		fmt.Println("FATAL: git 不可用，无法验证")
		os.Exit(1)
	}

	dir := strings.TrimSpace(*repo)
	if dir == "" {
		d, _ := os.Getwd()
		dir = d
	}
	partA(ctx, dir)
	fmt.Println()
	partB(ctx)

	if fails > 0 {
		fmt.Printf("\n结论：%d 项未通过\n", fails)
		os.Exit(1)
	}
	fmt.Println("\n结论：gitcmd 在真实仓库与脏数据场景下解析与原生命令一致")
}

// ---------- A. 真实仓库只读对拍 ----------

func partA(ctx context.Context, dir string) {
	fmt.Printf("=== A. 只读对拍 %s ===\n", dir)

	rp, err := gitcmd.Discover(ctx, filepath.Join(dir, "tools"))
	if err != nil {
		fail("Discover(子目录) 失败: %v", err)
		return
	}
	want := norm(dir)
	if norm(rp.Toplevel) != want {
		fail("Discover 解析到的 toplevel 不符: got %q want %q", norm(rp.Toplevel), want)
	} else {
		fmt.Printf("   OK   子目录向上发现仓库根：%s（branch=%q detached=%v）\n", norm(rp.Toplevel), rp.Branch, rp.Detached)
	}

	st, err := gitcmd.StatusOf(ctx, dir)
	if err != nil {
		fail("StatusOf 失败: %v", err)
		return
	}
	raw := mustGit(dir, "status", "--porcelain=v2", "--branch")
	rawLines := splitStatusLines(raw)
	fmt.Printf("   StatusOf: branch=%q upstream=%q ahead=%d behind=%d staged=%d unstaged=%d untracked=%d conflicted=%d\n",
		st.Branch, st.Upstream, st.Ahead, st.Behind, len(st.Staged), len(st.Unstaged), len(st.Untracked), len(st.Conflicted))
	fmt.Printf("   原生 git status --porcelain 行数=%d\n", len(rawLines))

	// 每一行原生输出都应能在解析结果里找到对应路径（重命名行取 -> 之后半段）
	seen := map[string]bool{}
	for _, e := range st.Staged {
		seen[e.Path] = true
	}
	for _, e := range st.Unstaged {
		seen[e.Path] = true
	}
	for _, e := range st.Untracked {
		seen[e.Path] = true
	}
	for _, e := range st.Conflicted {
		seen[e.Path] = true
	}
	var missing []string
	native := map[string]bool{}
	for _, l := range rawLines {
		p := v2Path(l)
		if p == "" {
			continue
		}
		native[p] = true
		if !seen[p] {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		fail("原生输出有 %d 个路径没被解析到：%s", len(missing), strings.Join(missing, " | "))
	} else {
		fmt.Printf("   OK   原生 v2 %d 行全部落进分类\n", len(native))
	}
	// 反向对拍：解析结果里不该冒出原生输出没有的路径（字段数切错时 score 或原路径会串进来）
	var ghost []string
	for p := range seen {
		if !native[p] {
			ghost = append(ghost, p)
		}
	}
	if len(ghost) > 0 {
		fail("解析结果有 %d 个原生输出里不存在的路径（字段切错？）：%s", len(ghost), strings.Join(ghost, " | "))
	} else {
		fmt.Printf("   OK   反向对拍一致：没有多出幽灵路径\n")
	}

	// 八进制转义漏网 = quotepath 没生效
	escaped := 0
	for p := range seen {
		if strings.Contains(p, "\\3") || strings.Contains(p, "\"") {
			escaped++
			fmt.Printf("   可疑转义路径: %q\n", p)
		}
	}
	if escaped > 0 {
		fail("有 %d 个路径带八进制转义或引号，core.quotepath=false 可能没生效", escaped)
	} else {
		fmt.Printf("   OK   无八进制转义漏网（非 ASCII 路径原样）\n")
	}

	commits, err := gitcmd.Log(ctx, dir, "", "", 30)
	if err != nil {
		fail("Log 失败: %v", err)
		return
	}
	n := len(splitLines(mustGit(dir, "log", "--pretty=%h", "-30")))
	if n == 0 {
		fmt.Printf("   SKIP 仓库无提交，Log 对拍跳过\n")
	} else if len(commits) != n {
		fail("Log 条数不符: got %d want %d", len(commits), n)
	} else {
		laneMax := 0
		for _, c := range commits {
			if c.Lane > laneMax {
				laneMax = c.Lane
			}
		}
		fmt.Printf("   OK   Log %d 条一致；lane 最大列=%d；首条 subject=%q\n", len(commits), laneMax, commits[0].Subject)
		for _, c := range commits {
			if strings.ContainsAny(c.Subject, "\ufffd") || strings.HasPrefix(c.Subject, "ï") {
				fail("提交信息编码异常（疑似按 GBK 解码）: %q", c.Subject)
				break
			}
		}
	}

	brs, err := gitcmd.Branches(ctx, dir)
	if err != nil {
		fail("Branches 失败: %v", err)
		return
	}
	nb := len(splitLines(mustGit(dir, "branch", "-a", "--format=%(refname:short)")))
	var local, remote int
	for _, b := range brs {
		if b.Remote {
			remote++
		} else {
			local++
		}
	}
	if local+remote != nb {
		fail("Branches 条数不符: got local=%d remote=%d want %d", local, remote, nb)
	} else {
		cur := ""
		for _, b := range brs {
			if b.Current {
				cur = b.Name
			}
		}
		fmt.Printf("   OK   Branches 本地 %d + 远程 %d = 原生 %d 条，当前分攴=%q\n", local, remote, nb, cur)
	}
}

// ---------- B. 脏数据构造 ----------

func partB(ctx context.Context) {
	fmt.Println("=== B. 临时仓库脏数据 ===")
	base, err := os.MkdirTemp("", "gitverify")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(base)
	repo := filepath.Join(base, "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		fatal(err)
	}

	must(repo, "init", "-q", "-b", "main")
	must(repo, "config", "user.name", "jinbao")
	must(repo, "config", "user.email", "a@b.c")
	must(repo, "config", "core.autocrlf", "false")

	write(filepath.Join(repo, "keep.txt"), "one\ntwo\nthree\n")
	write(filepath.Join(repo, "rename-me.txt"), "x\n")
	must(repo, "add", "-A")
	must(repo, "commit", "-q", "-m", "初始提交：基线")

	// 分支与冲突：两边都改同一行
	must(repo, "checkout", "-q", "-b", "feature")
	write(filepath.Join(repo, "keep.txt"), "one\nfeature\nthree\n")
	must(repo, "add", "-A")
	must(repo, "commit", "-q", "-m", "feature 改了第二行")
	must(repo, "checkout", "-q", "main")
	write(filepath.Join(repo, "keep.txt"), "one\nmainline\nthree\n")
	must(repo, "add", "-A")
	must(repo, "commit", "-q", "-m", "main 改了第二行")
	_, _ = runGit(repo, "merge", "feature") // 预期冲突

	// 中文 + 空格目录与文件（未跟踪）
	odd := filepath.Join(repo, "中文 目录")
	if err := os.MkdirAll(odd, 0o755); err != nil {
		fatal(err)
	}
	write(filepath.Join(odd, "笔记 两行.txt"), "a\nb\n")
	// staged 与 unstaged 并存：另起一个文件。上一版拿冲突那个文件去 add，
	// 而 `git add` 本身就等于「冲突已解决」，等于把要测的场景自己抹掉了。
	write(filepath.Join(repo, "dual.txt"), "a\n")
	must(repo, "add", "dual.txt")
	write(filepath.Join(repo, "dual.txt"), "a\nb\n")
	// 重命名
	must(repo, "mv", "rename-me.txt", "renamed.txt")

	st, err := gitcmd.StatusOf(ctx, repo)
	if err != nil {
		fail("脏数据 StatusOf 失败: %v", err)
		return
	}
	dump := func(label string, es []gitcmd.FileEntry) {
		var ps []string
		for _, e := range es {
			s := e.Path
			if e.OrigPath != "" {
				s = e.OrigPath + "->" + s
			}
			if e.Conflict {
				s += "[C]"
			}
			ps = append(ps, s)
		}
		sort.Strings(ps)
		fmt.Printf("   %-11s (%d) %s\n", label, len(es), strings.Join(ps, " | "))
	}
	dump("staged", st.Staged)
	dump("unstaged", st.Unstaged)
	dump("untracked", st.Untracked)
	dump("conflicted", st.Conflicted)

	// 断言 1：中文带空格路径原样出现且落在 untracked（git 默认只列到目录，不展开文件）
	if !(hasPath(st.Untracked, "中文 目录/笔记 两行.txt") || hasPath(st.Untracked, "中文 目录/")) {
		fail("中文带空格路径没原样进 untracked")
	} else {
		fmt.Printf("   OK   中文+空格路径原样解析进 untracked（git 默认 -u 只列目录，不展开）\n")
	}
	// 断言 2：冲突文件被识别
	if len(st.Conflicted) == 0 {
		fail("merge 冲突未被识别，Conflicted 为空")
	} else {
		fmt.Printf("   OK   冲突文件识别到 %d 个（%s）\n", len(st.Conflicted), st.Conflicted[0].Path)
	}
	// 断言 3：重命名带原路径
	if !hasRename(st.Staged, "rename-me.txt", "renamed.txt") {
		fail("重命名未解析出 OrigPath（staged=%d unstaged=%d）", len(st.Staged), len(st.Unstaged))
	} else {
		fmt.Printf("   OK   重命名解析出 OrigPath：rename-me.txt -> renamed.txt\n")
	}
	// 断言 4：dual.txt 同时出现在 staged 与 unstaged
	if !(hasPath(st.Staged, "dual.txt") && hasPath(st.Unstaged, "dual.txt")) {
		fail("dual.txt 未同时出现在 staged 与 unstaged")
	} else {
		fmt.Printf("   OK   同一文件的暂存改动与工作区改动被分别归类\n")
	}
	// 断言 5：分支与 ahead/behind
	if st.Branch != "main" {
		fail("当前分支解析错: got %q want main", st.Branch)
	} else {
		fmt.Printf("   OK   冲突状态下分支仍解析正确：%s\n", st.Branch)
	}

	// 断言 6：numstat 差异文件集合
	fd, err := gitcmd.Files(ctx, repo, gitcmd.DiffOpts{})
	if err != nil {
		fail("Files(工作区 diff) 失败: %v", err)
		return
	}
	fmt.Printf("   Diff   %d 个文件：%s\n", len(fd), joinDiff(fd))

	// 断言 7：解决冲突后 Conflicted 归零
	write(filepath.Join(repo, "keep.txt"), "one\nresolved\nthree\n")
	must(repo, "add", "keep.txt")
	st2, err := gitcmd.StatusOf(ctx, repo)
	if err != nil {
		fail("解决冲突后 StatusOf 失败: %v", err)
		return
	}
	if len(st2.Conflicted) != 0 {
		fail("冲突已 add 解决但 Conflicted 仍有 %d 项", len(st2.Conflicted))
	} else {
		fmt.Printf("   OK   冲突解决后 Conflicted 归零（staged=%d）\n", len(st2.Staged))
	}
}

// ---------- helpers ----------

func runGit(dir string, args ...string) (string, error) {
	full := append([]string{"-c", "core.quotepath=false", "-C", dir}, args...)
	cmd := exec.Command("git.exe", full...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func must(dir string, args ...string) {
	out, err := runGit(dir, args...)
	if err != nil {
		fatal(fmt.Errorf("git %s 失败: %v｜%s", strings.Join(args, " "), err, strings.TrimSpace(out)))
	}
}

func mustGit(dir string, args ...string) string {
	out, err := runGit(dir, args...)
	if err != nil {
		fatal(fmt.Errorf("git %s 失败: %v", strings.Join(args, " "), err))
	}
	return out
}

func write(path, content string) {
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fatal(err)
	}
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

// splitStatusLines 去掉 # 开头的分支头信息行，只留实体条目。
func splitStatusLines(s string) []string {
	var out []string
	for _, l := range splitLines(s) {
		if !strings.HasPrefix(l, "#") {
			out = append(out, l)
		}
	}
	return out
}

// v2Path 按 git porcelain=v2 的官方字段布局取路径（独立于产品代码重新实现一遗）。
func v2Path(l string) string {
	switch {
	case strings.HasPrefix(l, "? "):
		return l[2:]
	case strings.HasPrefix(l, "1 "):
		f := strings.SplitN(l, " ", 9) // f[8]=path
		if len(f) < 9 {
			return ""
		}
		return f[8]
	case strings.HasPrefix(l, "u "):
		f := strings.SplitN(l, " ", 11) // f[10]=path
		if len(f) < 11 {
			return ""
		}
		return f[10]
	case strings.HasPrefix(l, "2 "):
		f := strings.SplitN(l, " ", 10) // f[8]=R100, f[9]="new<TAB>orig"
		if len(f) < 10 {
			return ""
		}
		return strings.SplitN(f[9], "\t", 2)[0]
	}
	return ""
}

func hasPath(es []gitcmd.FileEntry, p string) bool {
	want := filepath.ToSlash(p)
	for _, e := range es {
		if filepath.ToSlash(e.Path) == want {
			return true
		}
	}
	return false
}

func hasRename(es []gitcmd.FileEntry, from, to string) bool {
	for _, e := range es {
		if filepath.ToSlash(e.Path) == to && filepath.ToSlash(e.OrigPath) == from {
			return true
		}
	}
	return false
}

func joinDiff(fd []gitcmd.FileDiff) string {
	var ps []string
	for _, f := range fd {
		ps = append(ps, fmt.Sprintf("%s(+%d/-%d)", f.Path, f.Added, f.Deleted))
	}
	return strings.Join(ps, " | ")
}

func norm(p string) string {
	p = filepath.ToSlash(p)
	if len(p) > 1 && p[len(p)-1] == '/' {
		p = p[:len(p)-1]
	}
	return strings.ToLower(p)
}

func fail(f string, a ...any) {
	fails++
	fmt.Printf("   FAIL "+f+"\n", a...)
}

func fatal(err error) {
	fmt.Println("FATAL:", err)
	os.Exit(1)
}
