package gitcmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// read 读临时文件内容；出错直接 fatal。工作区文件在 Windows 上会被 git 转成 CRLF，
// 而测试写入/断言用的是逻辑 LF，故把 CRLF 归一化回 LF 后再比较（blob 侧的
// cat-file 本就存 LF，不受影响）。
func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.ReplaceAll(string(b), "\r\n", "\n")
}

// ---------- P3：stash / tag / remote / 冲突三方 / rebase ----------

func TestStashE2E(t *testing.T) {
	ctx := requireGit(t)
	dir := newRepo(t, ctx)
	write(t, filepath.Join(dir, "a.txt"), "base\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "c1")

	// 空栈
	if list, err := StashList(ctx, dir); err != nil || len(list) != 0 {
		t.Fatalf("empty stack: %+v err=%v", list, err)
	}

	// 有改动 + 一个未跟踪文件；--include-untracked 应把两者都收走
	write(t, filepath.Join(dir, "a.txt"), "base modified\n")
	write(t, filepath.Join(dir, "u.txt"), "untracked\n")
	if err := StashPush(ctx, dir, "wip msg", true); err != nil {
		t.Fatalf("stash push: %v", err)
	}
	if exists(filepath.Join(dir, "u.txt")) {
		t.Fatal("--include-untracked 未收走未跟踪文件")
	}
	if body := read(t, filepath.Join(dir, "a.txt")); body != "base\n" {
		t.Fatalf("已跟踪文件未回退: %q", body)
	}
	list, err := StashList(ctx, dir)
	if err != nil || len(list) != 1 {
		t.Fatalf("after push: %+v err=%v", list, err)
	}
	if list[0].Index != 0 || !strings.Contains(list[0].Message, "wip msg") {
		t.Fatalf("entry: %+v", list[0])
	}

	// apply 保留栈，pop 出栈
	if err := StashApply(ctx, dir, "0"); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if body := read(t, filepath.Join(dir, "a.txt")); body != "base modified\n" {
		t.Fatalf("apply 未恢复: %q", body)
	}
	if l, _ := StashList(ctx, dir); len(l) != 1 {
		t.Fatalf("apply 应保留栈: %+v", l)
	}
	// apply 只出内容不出栈，工作区已脏；pop 同一栈前先把工作区清干净，否则 git 拒合并
	mustRun(t, ctx, dir, "reset", "--hard", "HEAD")
	mustRun(t, ctx, dir, "clean", "-fdq")
	if err := StashPop(ctx, dir, ""); err != nil { // 空=栈顶
		t.Fatalf("pop: %v", err)
	}
	if l, _ := StashList(ctx, dir); len(l) != 0 {
		t.Fatalf("pop 后应空栈: %+v", l)
	}

	// 无改动 → ErrNothingToStash（pop 已恢复工作区改动，先清空再验空暂存）
	mustRun(t, ctx, dir, "reset", "--hard", "HEAD")
	mustRun(t, ctx, dir, "clean", "-fdq")
	if err := StashPush(ctx, dir, "空", false); err != ErrNothingToStash {
		t.Fatalf("期望 ErrNothingToStash，实得 %v", err)
	}

	// drop：暂存一条再丢弃
	write(t, filepath.Join(dir, "a.txt"), "again\n")
	if err := StashPush(ctx, dir, "d1", false); err != nil {
		t.Fatalf("push d1: %v", err)
	}
	if err := StashDrop(ctx, dir, "0"); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if l, _ := StashList(ctx, dir); len(l) != 0 {
		t.Fatalf("drop 后应空: %+v", l)
	}
}

func TestTagE2E(t *testing.T) {
	ctx := requireGit(t)
	dir := newRepo(t, ctx)
	write(t, filepath.Join(dir, "a.txt"), "x\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "c1")

	// 轻量标签
	if err := TagCreate(ctx, dir, "v-light", "", ""); err != nil {
		t.Fatalf("lightweight tag: %v", err)
	}
	// 附注标签（本工具强制不签名）
	if err := TagCreate(ctx, dir, "v-annot", "release note", "HEAD"); err != nil {
		t.Fatalf("annotated tag: %v", err)
	}
	list, err := TagList(ctx, dir)
	if err != nil || len(list) != 2 {
		t.Fatalf("tag list: %+v err=%v", list, err)
	}
	var light, annot Tag
	for _, tg := range list {
		if tg.Name == "v-light" {
			light = tg
		}
		if tg.Name == "v-annot" {
			annot = tg
		}
	}
	if light.Annotated || light.Commit == "" {
		t.Fatalf("light: %+v", light)
	}
	if !annot.Annotated || annot.Subject != "release note" || annot.Commit == "" {
		t.Fatalf("annotated: %+v", annot)
	}

	if err := TagDelete(ctx, dir, "v-light"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	list, _ = TagList(ctx, dir)
	if len(list) != 1 || list[0].Name != "v-annot" {
		t.Fatalf("after delete: %+v", list)
	}
}

func TestRemoteE2E(t *testing.T) {
	ctx := requireGit(t)
	dir := newRepo(t, ctx)

	if list, err := RemoteList(ctx, dir); err != nil || len(list) != 0 {
		t.Fatalf("empty remotes: %+v err=%v", list, err)
	}
	if err := RemoteAdd(ctx, dir, "origin", "https://example.com/a.git"); err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := RemoteAdd(ctx, dir, "up", "https://example.com/b.git"); err != nil {
		t.Fatalf("add up: %v", err)
	}
	list, err := RemoteList(ctx, dir)
	if err != nil || len(list) != 2 {
		t.Fatalf("list: %+v err=%v", list, err)
	}
	var origin Remote
	for _, r := range list {
		if r.Name == "origin" {
			origin = r
		}
	}
	if origin.FetchURL != "https://example.com/a.git" || origin.PushURL != "https://example.com/a.git" {
		t.Fatalf("origin urls: %+v", origin)
	}

	// 改抓取地址
	if err := RemoteSetURL(ctx, dir, "origin", "https://example.com/c.git", false); err != nil {
		t.Fatalf("set-url: %v", err)
	}
	// 单独设推送地址
	if err := RemoteSetURL(ctx, dir, "origin", "https://example.com/push.git", true); err != nil {
		t.Fatalf("set-url push: %v", err)
	}
	list, _ = RemoteList(ctx, dir)
	for _, r := range list {
		if r.Name == "origin" {
			origin = r
		}
	}
	if origin.FetchURL != "https://example.com/c.git" || origin.PushURL != "https://example.com/push.git" {
		t.Fatalf("after set-url: %+v", origin)
	}

	if err := RemoteRemove(ctx, dir, "up"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	list, _ = RemoteList(ctx, dir)
	if len(list) != 1 || list[0].Name != "origin" {
		t.Fatalf("after remove: %+v", list)
	}
}

func TestConflictResolveE2E(t *testing.T) {
	ctx := requireGit(t)
	dir := newRepo(t, ctx)
	write(t, filepath.Join(dir, "a.txt"), "line\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "base")

	// feature 分支改 a.txt
	mustRun(t, ctx, dir, "checkout", "-q", "-b", "feature")
	write(t, filepath.Join(dir, "a.txt"), "feature\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "feat")

	// master 改 a.txt（分叉）
	mustRun(t, ctx, dir, "checkout", "-q", "master")
	write(t, filepath.Join(dir, "a.txt"), "master\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "mast")

	// merge feature → 冲突（Run 返回非零，错误人话，工作区进中断态）
	if _, err := Run(ctx, dir, "merge", "feature"); err == nil {
		t.Fatal("期望 merge 冲突，却成功")
	}
	st, err := OpStateOf(ctx, dir)
	if err != nil || !st.Merge {
		t.Fatalf("op state merge: %+v err=%v", st, err)
	}
	paths, err := Conflicts(ctx, dir)
	if err != nil || len(paths) != 1 || paths[0] != "a.txt" {
		t.Fatalf("conflicts: %+v err=%v", paths, err)
	}
	sides, err := ConflictSidesOf(ctx, dir, "a.txt")
	if err != nil {
		t.Fatalf("sides: %v", err)
	}
	if sides.Base != "line\n" || sides.Ours != "master\n" || sides.Theirs != "feature\n" {
		t.Fatalf("三方内容: %+v", sides)
	}

	// 选 ours（=HEAD/master）解决
	if err := ResolveConflict(ctx, dir, "a.txt", "ours"); err != nil {
		t.Fatalf("resolve ours: %v", err)
	}
	if body := read(t, filepath.Join(dir, "a.txt")); body != "master\n" {
		t.Fatalf("选 ours 后内容: %q", body)
	}
	if paths, _ := Conflicts(ctx, dir); len(paths) != 0 {
		t.Fatalf("解决后仍有冲突: %+v", paths)
	}
	// 非法 side 被白名单挡掉
	if err := ResolveConflict(ctx, dir, "a.txt", "--help"); err == nil {
		t.Fatal("非法 side 应报错")
	}
}

func TestRebaseE2E(t *testing.T) {
	ctx := requireGit(t)
	dir := newRepo(t, ctx)
	write(t, filepath.Join(dir, "a.txt"), "line\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "base")

	mustRun(t, ctx, dir, "checkout", "-q", "-b", "feature")
	write(t, filepath.Join(dir, "a.txt"), "feature\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "feat")

	mustRun(t, ctx, dir, "checkout", "-q", "master")
	write(t, filepath.Join(dir, "a.txt"), "master\n")
	mustRun(t, ctx, dir, "add", ".")
	mustRun(t, ctx, dir, "commit", "-qm", "mast")

	// feature rebase 到 master → 冲突
	mustRun(t, ctx, dir, "checkout", "-q", "feature")
	if _, err := Run(ctx, dir, "rebase", "master"); err == nil {
		t.Fatal("期望 rebase 冲突，却成功")
	}
	st, err := OpStateOf(ctx, dir)
	if err != nil || !st.Rebase {
		t.Fatalf("rebase 态未探测到: %+v err=%v", st, err)
	}

	// 先验证 abort：回到 rebase 前
	if err := RebaseAbort(ctx, dir); err != nil {
		t.Fatalf("abort: %v", err)
	}
	if st, _ := OpStateOf(ctx, dir); st.Rebase {
		t.Fatal("abort 后仍在 rebase 态")
	}

	// 再 rebase → 解决 → continue（core.editor=true 顶掉编辑器）
	if _, err := Run(ctx, dir, "rebase", "master"); err == nil {
		t.Fatal("期望第二次 rebase 冲突")
	}
	// rebase 语境 theirs=正在重放的提交（feature），取它
	if err := ResolveConflict(ctx, dir, "a.txt", "theirs"); err != nil {
		t.Fatalf("resolve theirs: %v", err)
	}
	if err := RebaseContinue(ctx, dir); err != nil {
		t.Fatalf("continue: %v", err)
	}
	if st, _ := OpStateOf(ctx, dir); st.Rebase {
		t.Fatal("continue 后仍在 rebase 态")
	}
	if body := read(t, filepath.Join(dir, "a.txt")); body != "feature\n" {
		t.Fatalf("continue 后内容: %q", body)
	}
}
