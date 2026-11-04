package gitcmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Tools/appdirs"
)

// ---------- 纯解析单测（不需要 git） ----------

func TestParseStatus(t *testing.T) {
	text := strings.Join([]string{
		"# branch.oid 6b4e0a8b89f0c2e0d3e4f5a6b7c8d9e0f1a2b3c4",
		"# branch.head master",
		"# branch.upstream origin/master",
		"# branch.ab +2 -1",
		"1 M. N... 100644 100644 100644 abc def staged.txt",
		"1 .M N... 100644 100644 100644 abc def unstaged.txt",
		"1 A. N... 100644 100644 100644 0000000000000000000000000000000000000000 1111111111111111111111111111111111111111 new 目录/带 空格.txt",
		// 下面这行的分隔方式是跟真机 `git status --porcelain=v2` 对拍过的：score（R100）
		// 与前面各段一样用空格分隔，只有新旧两条路径之间才是 TAB。以前这里写成全 TAB
		// 三段，代码照着它写反而把原路径当成了 Path，真机上重命名全错而单测全绿。
		"2 R. N... 100644 100644 100644 abc def R100 renamed/new path.txt\trenamed/old path.txt",
		"u UU. N... 100644 100644 100644 100644 abc def ghi conflicted.txt",
		"? untracked/文件夹/中文 名.txt",
	}, "\n")
	st := parseStatus(text)
	if st.Branch != "master" || st.Upstream != "origin/master" || st.Ahead != 2 || st.Behind != 1 {
		t.Fatalf("branch header wrong: %+v", st)
	}
	if len(st.Staged) != 3 { // staged.txt, new 目录/带 空格.txt, renamed/new path.txt（X=R 属暂存）
		t.Fatalf("staged=%v", st.Staged)
	}
	if len(st.Unstaged) != 1 || st.Unstaged[0].Path != "unstaged.txt" {
		t.Fatalf("unstaged=%v", st.Unstaged)
	}
	if len(st.Untracked) != 1 || st.Untracked[0].Path != "untracked/文件夹/中文 名.txt" {
		t.Fatalf("untracked=%v", st.Untracked)
	}
	if len(st.Conflicted) != 1 || !st.Conflicted[0].Conflict || st.Conflicted[0].Path != "conflicted.txt" {
		t.Fatalf("conflicted=%v", st.Conflicted)
	}
	var rn FileEntry
	for _, e := range st.Staged {
		if strings.HasPrefix(e.Path, "renamed/new") {
			rn = e
		}
	}
	if rn.OrigPath != "renamed/old path.txt" {
		t.Fatalf("rename orig wrong: %+v", rn)
	}
	// score 与原路径都不许泄进 Path：字段数切错时正是这两样串过来。
	if strings.HasPrefix(rn.Path, "R100") || strings.Contains(rn.Path, "renamed/old") {
		t.Fatalf("rename 把 score 或原路径读进了 Path: %+v", rn)
	}
}

func TestParseNumstat(t *testing.T) {
	text := "3\t1\ta.txt\n-\t-\tbin.dat\n0\t5\tsome dir/删除 行.txt\n"
	list := parseNumstat(text)
	if len(list) != 3 {
		t.Fatalf("len=%d", len(list))
	}
	if list[0].Added != 3 || list[0].Deleted != 1 || list[0].Binary {
		t.Fatalf("first=%+v", list[0])
	}
	if !list[1].Binary || list[1].Path != "bin.dat" {
		t.Fatalf("bin=%+v", list[1])
	}
	if list[2].Path != "some dir/删除 行.txt" || list[2].Deleted != 5 {
		t.Fatalf("cn=%+v", list[2])
	}
}

func TestAssignLanesLinear(t *testing.T) {
	c := mkCommits([]string{"a4", "a3", "a2", "a1"}, [][]string{{"a3"}, {"a2"}, {"a1"}, {}})
	assignLanes(c)
	for i, cc := range c {
		if cc.Lane != 0 {
			t.Fatalf("linear lane[%d]=%d", i, cc.Lane)
		}
	}
}

func TestAssignLanesDiamond(t *testing.T) {
	// M 父 [A B]；A 父 [R]；B 父 [R]；R 根。log 顺序新→旧：M A B R
	c := mkCommits([]string{"M", "A", "B", "R"}, [][]string{{"A", "B"}, {"R"}, {"R"}, {}})
	assignLanes(c)
	if c[0].Lane != 0 || c[1].Lane != 0 {
		t.Fatalf("merge/first-parent should share lane0: %d %d", c[0].Lane, c[1].Lane)
	}
	if c[2].Lane != 1 {
		t.Fatalf("side branch lane=%d want 1", c[2].Lane)
	}
}

func mkCommits(hashes []string, parents [][]string) []Commit {
	list := make([]Commit, 0, len(hashes))
	for i, h := range hashes {
		p := parents[i]
		if p == nil {
			p = []string{}
		}
		list = append(list, Commit{Hash: h, Parents: p})
	}
	return list
}

func TestCleanRefs(t *testing.T) {
	if got := cleanRefs("(HEAD -> main, tag: v1)"); got != "HEAD -> main, tag: v1" {
		t.Fatalf("got %q", got)
	}
	if got := cleanRefs(""); got != "" {
		t.Fatalf("got %q", got)
	}
}

// ---------- 真 git 端到端（无 git 则跳过） ----------

func requireGit(t *testing.T) context.Context {
	t.Helper()
	ctx := context.Background()
	if info := Check(ctx); !info.OK {
		t.Skipf("本机无可用 git：%s", info.Reason)
	}
	return ctx
}

// newRepo 建一个配好身份的临时仓库目录。
func newRepo(t *testing.T, ctx context.Context) string {
	t.Helper()
	dir := t.TempDir()
	if _, err := Run(ctx, dir, "init", "-q"); err != nil {
		t.Fatalf("git init: %v", err)
	}
	mustRun(t, ctx, dir, "config", "user.name", "tester")
	mustRun(t, ctx, dir, "config", "user.email", "t@example.com")
	mustRun(t, ctx, dir, "config", "commit.gpgsign", "false")
	return dir
}

func mustRun(t *testing.T, ctx context.Context, dir string, args ...string) string {
	t.Helper()
	out, err := Run(ctx, dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, out.Stderr)
	}
	return trimTail(out.Stdout)
}

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverE2E(t *testing.T) {
	ctx := requireGit(t)
	dir := newRepo(t, ctx)
	write(t, filepath.Join(dir, "a.txt"), "1\n")
	mustRun(t, ctx, dir, "add", "a.txt")
	mustRun(t, ctx, dir, "commit", "-qm", "first")

	repo, err := Discover(ctx, dir)
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	if repo.Detached || repo.Branch == "" {
		t.Fatalf("want branch, got %+v", repo)
	}
	// 游离 HEAD
	h := mustRun(t, ctx, dir, "rev-parse", "HEAD")
	mustRun(t, ctx, dir, "checkout", "-q", h)
	repo2, err := Discover(ctx, dir)
	if err != nil || !repo2.Detached || repo2.Branch != "" {
		t.Fatalf("detached: %+v err=%v", repo2, err)
	}
	// 非仓库目录
	if _, err := Discover(ctx, t.TempDir()); err != ErrNotRepo {
		t.Fatalf("want ErrNotRepo, got %v", err)
	}
}

func TestStatusE2E(t *testing.T) {
	ctx := requireGit(t)
	dir := newRepo(t, ctx)
	write(t, filepath.Join(dir, "a.txt"), "a\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "init")

	write(t, filepath.Join(dir, "a.txt"), "a2\n")     // unstaged
	write(t, filepath.Join(dir, "b.txt"), "b\n")      // new
	write(t, filepath.Join(dir, "c d 中文.txt"), "c\n") // untracked 含空格中文
	mustRun(t, ctx, dir, "add", "b.txt")              // staged
	mustRun(t, ctx, dir, "add", "a.txt")
	write(t, filepath.Join(dir, "a.txt"), "a3\n") // staged + 再改 → 两侧都出现

	st, err := StatusOf(ctx, dir)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if st.Branch == "" || st.OID == "" {
		t.Fatalf("header missing: %+v", st)
	}
	has := func(list []FileEntry, p string) bool {
		for _, e := range list {
			if e.Path == p {
				return true
			}
		}
		return false
	}
	if !has(st.Staged, "b.txt") || !has(st.Staged, "a.txt") {
		t.Fatalf("staged: %+v", st.Staged)
	}
	if !has(st.Unstaged, "a.txt") {
		t.Fatalf("unstaged: %+v", st.Unstaged)
	}
	if !has(st.Untracked, "c d 中文.txt") {
		t.Fatalf("untracked: %+v", st.Untracked)
	}
}

func TestLogDiffE2E(t *testing.T) {
	ctx := requireGit(t)
	dir := newRepo(t, ctx)
	write(t, filepath.Join(dir, "a.txt"), "1\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "root")

	mustRun(t, ctx, dir, "checkout", "-qb", "feature")
	write(t, filepath.Join(dir, "f.txt"), "f\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "feat")

	mustRun(t, ctx, dir, "checkout", "-q", "master")
	write(t, filepath.Join(dir, "a.txt"), "2\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "main")
	mustRun(t, ctx, dir, "merge", "-q", "--no-ff", "-m", "merge", "feature")

	list, err := Log(ctx, dir, "", "", 0)
	if err != nil {
		t.Fatalf("log: %v", err)
	}
	if len(list) != 4 || list[0].Subject != "merge" {
		t.Fatalf("log=%+v", list)
	}
	if len(list[0].Parents) != 2 {
		t.Fatalf("merge parents=%v", list[0].Parents)
	}
	if list[0].Lane != 0 {
		t.Fatalf("merge lane=%d", list[0].Lane)
	}
	lanes := map[string]int{}
	for _, c := range list {
		lanes[c.Subject] = c.Lane
	}
	if lanes["feat"] == lanes["main"] {
		t.Fatalf("side branch should get own lane: %+v", lanes)
	}

	// 工作区改动 numstat + 文本
	write(t, filepath.Join(dir, "a.txt"), "3\n3\n")
	fd, err := Files(ctx, dir, DiffOpts{})
	if err != nil || len(fd) != 1 || fd[0].Path != "a.txt" {
		t.Fatalf("files: %+v err=%v", fd, err)
	}
	txt, err := Text(ctx, dir, DiffOpts{})
	if err != nil || !strings.Contains(txt, "+++ b/a.txt") {
		t.Fatalf("text: %q err=%v", txt, err)
	}
	// 提交差异（show）：取 feat 提交自身；merge 提交默认 combined diff，show 不列文件，不能拿它断言。
	featHash := ""
	for _, c := range list {
		if c.Subject == "feat" {
			featHash = c.Hash
		}
	}
	sd, err := Files(ctx, dir, DiffOpts{Commit: featHash})
	if err != nil || len(sd) != 1 || sd[0].Path != "f.txt" {
		t.Fatalf("show files: %+v err=%v", sd, err)
	}
}

func TestRepoRegistry(t *testing.T) {
	ctx := requireGit(t)
	origBase := appdirs.DataBase()
	defer appdirs.Set("", origBase, "")
	tmp := t.TempDir()
	if err := appdirs.Set("", tmp, ""); err != nil {
		t.Fatal(err)
	}
	dir := newRepo(t, ctx)
	write(t, filepath.Join(dir, "a.txt"), "1\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "c")

	list, err := AddRepo(ctx, dir)
	if err != nil || len(list) != 1 {
		t.Fatalf("add: %+v err=%v", list, err)
	}
	again, err := AddRepo(ctx, dir) // 去重
	if err != nil || len(again) != 1 {
		t.Fatalf("dup: %+v err=%v", again, err)
	}
	got, err := LoadRepos()
	if err != nil || len(got) != 1 || got[0].Name != filepath.Base(filepath.Clean(dir)) {
		t.Fatalf("load: %+v err=%v", got, err)
	}
	out, err := RemoveRepo(dir)
	if err != nil || len(out) != 0 {
		t.Fatalf("remove: %+v err=%v", out, err)
	}
}

// ---------- P2：blame 解析 / 冲突去重 / 单文件历史 / 变更操作 ----------

// TestParseBlame 校验 --line-porcelain 聚合：连续同提交行合成一段，跨提交切段。
func TestParseBlame(t *testing.T) {
	text := strings.Join([]string{
		"aa11111111111111111111111111111111111111 1 1 2",
		"author Alice",
		"author-mail <a@example.com>",
		"author-time 1700000000",
		"summary first commit",
		"\tline one",
		"aa11111111111111111111111111111111111111 2 2",
		"author Alice",
		"author-time 1700000000",
		"summary first commit",
		"\tline two",
		"bb22222222222222222222222222222222222222 3 3",
		"author Bob",
		"author-time 1700000100",
		"summary second commit",
		"\tline three",
		"",
	}, "\n")
	segs := parseBlame(text)
	if len(segs) != 2 {
		t.Fatalf("want 2 segs, got %+v", segs)
	}
	if segs[0].Short != "aa111111" || segs[0].Author != "Alice" || segs[0].Count != 2 || segs[0].StartLine != 1 || segs[0].Summary != "first commit" {
		t.Fatalf("seg0=%+v", segs[0])
	}
	if segs[1].Short != "bb222222" || segs[1].Author != "Bob" || segs[1].Count != 1 || segs[1].StartLine != 3 || segs[1].Time != 1700000100 {
		t.Fatalf("seg1=%+v", segs[1])
	}
}

// TestParseNumstatConflict 校验同路径多行（按 stage）合并成一条，避免前端 v-for key 撞车。
func TestParseNumstatConflict(t *testing.T) {
	text := "5\t2\tconflict.txt\n1\t0\tconflict.txt\n3\t1\tone.txt\n"
	list := parseNumstat(text)
	if len(list) != 2 {
		t.Fatalf("want 2, got %+v", list)
	}
	var c FileDiff
	for _, d := range list {
		if d.Path == "conflict.txt" {
			c = d
		}
	}
	if c.Added != 5 || c.Deleted != 2 { // 各 stage 取最大增删
		t.Fatalf("conflict=%+v", c)
	}
}

// TestLogFollowE2E 校验单文件历史 --follow 跨重命名回溯到改名前的提交。
func TestLogFollowE2E(t *testing.T) {
	ctx := requireGit(t)
	dir := newRepo(t, ctx)
	write(t, filepath.Join(dir, "a.txt"), "1\n")
	write(t, filepath.Join(dir, "b.txt"), "b\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "root")
	// 纯重命名（不改内容）→ 相似度 100%，git 必判为 rename，--follow 才能跨名回溯
	mustRun(t, ctx, dir, "mv", "a.txt", "c.txt")
	mustRun(t, ctx, dir, "commit", "-qm", "rename")

	list, err := Log(ctx, dir, "", "c.txt", 0)
	if err != nil {
		t.Fatalf("log follow: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("--follow 应回溯改名前提交: %+v", list)
	}
	if list[0].Subject != "rename" || list[1].Subject != "root" {
		t.Fatalf("order=%+v", list)
	}
}

// TestHistoryOpsE2E 覆盖 P2 历史节点变更操作：建分支/cherry-pick/revert/reset/游离检出/blame。
func TestHistoryOpsE2E(t *testing.T) {
	ctx := requireGit(t)
	dir := newRepo(t, ctx)
	write(t, filepath.Join(dir, "a.txt"), "1\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "c1")

	// CreateBranchAt 在 c1 建 at-c1（不检出）
	if err := CreateBranchAt(ctx, dir, "at-c1", "HEAD", false); err != nil {
		t.Fatalf("CreateBranchAt: %v", err)
	}
	bl, err := Branches(ctx, dir)
	if err != nil {
		t.Fatalf("branches: %v", err)
	}
	found := false
	for _, b := range bl {
		if b.Name == "at-c1" && !b.Remote {
			found = true
		}
	}
	if !found {
		t.Fatalf("at-c1 未建出: %+v", bl)
	}

	// feature 分支上提交 feat，回 master 后 cherry-pick 该提交
	mustRun(t, ctx, dir, "checkout", "-qb", "feature")
	write(t, filepath.Join(dir, "f.txt"), "f\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "feat")
	featHash := mustRun(t, ctx, dir, "rev-parse", "HEAD")
	mustRun(t, ctx, dir, "checkout", "-q", "master")
	if err := CherryPick(ctx, dir, featHash); err != nil {
		t.Fatalf("CherryPick: %v", err)
	}
	if !exists(filepath.Join(dir, "f.txt")) {
		t.Fatalf("cherry-pick 后 f.txt 未落地")
	}

	// Revert 最新（cherry-pick 出来的）提交，产生一笔反向提交
	if err := Revert(ctx, dir, "HEAD"); err != nil {
		t.Fatalf("Revert: %v", err)
	}
	if exists(filepath.Join(dir, "f.txt")) {
		t.Fatalf("revert 后 f.txt 应被删除")
	}

	// Reset soft：回到 c1，工作区文件保留
	if err := Reset(ctx, dir, "soft", "HEAD~2"); err != nil {
		t.Fatalf("Reset soft: %v", err)
	}
	if !exists(filepath.Join(dir, "a.txt")) {
		t.Fatalf("reset soft 后 a.txt 应仍在工作区")
	}
	// 非法 mode 被白名单挡下
	if err := Reset(ctx, dir, "nope", "HEAD"); err == nil {
		t.Fatalf("非法 reset mode 应报错")
	}

	// Reset hard：清掉 soft 遗留的暂存/工作区改动，回到干净的 c1
	if err := Reset(ctx, dir, "hard", "HEAD"); err != nil {
		t.Fatalf("Reset hard: %v", err)
	}
	logAll, _ := Log(ctx, dir, "", "", 0)
	if len(logAll) != 1 || logAll[0].Subject != "c1" {
		t.Fatalf("reset hard 后应只剩 c1: %+v", logAll)
	}

	// Checkout 到具体提交（游离 HEAD）
	if err := Checkout(ctx, dir, "at-c1"); err != nil {
		t.Fatalf("Checkout commit: %v", err)
	}

	// Blame 单文件逐行归属
	segs, err := Blame(ctx, dir, "a.txt", "")
	if err != nil {
		t.Fatalf("Blame: %v", err)
	}
	if len(segs) == 0 || segs[0].Author != "tester" {
		t.Fatalf("blame segs=%+v", segs)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// TestShowBlobE2E 校验并排 diff 取两栏内容：HEAD/index/工作区/首提交的父（不存在→空）。
func TestShowBlobE2E(t *testing.T) {
	ctx := requireGit(t)
	dir := newRepo(t, ctx)
	write(t, filepath.Join(dir, "a.txt"), "old\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "c1")

	got, err := ShowBlob(ctx, dir, "HEAD", "a.txt")
	if err != nil || got != "old\n" {
		t.Fatalf("HEAD blob=%q err=%v", got, err)
	}
	// 首提交的父不存在 → 空串不报错
	parent, err := ShowBlob(ctx, dir, "HEAD^", "a.txt")
	if err != nil || parent != "" {
		t.Fatalf("root parent blob=%q err=%v", parent, err)
	}
	// 改工作区 + 暂存：index 与 HEAD 不致
	write(t, filepath.Join(dir, "a.txt"), "new\n")
	mustRun(t, ctx, dir, "add", "a.txt")
	idx, err := ShowBlob(ctx, dir, "", "a.txt") // rev 空 = index
	if err != nil || idx != "new\n" {
		t.Fatalf("index blob=%q err=%v", idx, err)
	}
	// 该版本无此文件（未提交的新文件）→ 空
	miss, err := ShowBlob(ctx, dir, "HEAD", "zz.txt")
	if err != nil || miss != "" {
		t.Fatalf("missing blob=%q err=%v", miss, err)
	}
}
