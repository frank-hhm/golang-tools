package gitcmd

import (
	"path/filepath"
	"testing"
)

// TestOpsE2E 覆盖 暂存/取消暂存/提交/分支 与「本地 bare 远端」的 push/fetch 进度链路。
// 远端用 file 协议的本地 bare 仓，不依赖网络；进度事件用注入的假 emitter 断言。
func TestOpsE2E(t *testing.T) {
	ctx := requireGit(t)
	dir := newRepo(t, ctx)

	// 空仓（无 HEAD）：add 后 unstage 走 rm --cached 兜底分支
	write(t, filepath.Join(dir, "a.txt"), "a\n")
	mustRun(t, ctx, dir, "add", "--", "a.txt")
	if err := Unstage(ctx, dir, []string{"a.txt"}); err != nil {
		t.Fatalf("unstage empty repo: %v", err)
	}
	st, err := StatusOf(ctx, dir)
	if err != nil || len(st.Staged) != 0 || len(st.Untracked) != 1 {
		t.Fatalf("after unstage: %+v err=%v", st, err)
	}

	if err := Stage(ctx, dir, []string{"a.txt"}); err != nil {
		t.Fatalf("stage: %v", err)
	}
	write(t, filepath.Join(dir, "b.txt"), "b\n")
	if err := StageAll(ctx, dir); err != nil {
		t.Fatalf("stage all: %v", err)
	}
	hash, err := CommitChanges(ctx, dir, "init: a and b", false)
	if err != nil || hash == "" {
		t.Fatalf("commit: %q %v", hash, err)
	}
	// amend 改写标题
	hash2, err := CommitChanges(ctx, dir, "init: amended", true)
	if err != nil || hash2 == "" || hash2 == hash {
		t.Fatalf("amend: %q vs %q err=%v", hash2, hash, err)
	}

	// 分支：建 + 列 + 切 + 删
	if err := CreateBranch(ctx, dir, "dev", true); err != nil {
		t.Fatalf("create dev: %v", err)
	}
	repo, _ := Discover(ctx, dir)
	if repo.Branch != "dev" {
		t.Fatalf("checkout via create: %+v", repo)
	}
	bs, err := Branches(ctx, dir)
	if err != nil || len(bs) != 2 {
		t.Fatalf("branches: %+v err=%v", bs, err)
	}
	var dev Branch
	for _, b := range bs {
		if b.Name == "dev" {
			dev = b
		}
	}
	if !dev.Current || dev.Subject != "init: amended" {
		t.Fatalf("dev entry: %+v", dev)
	}
	if err := Checkout(ctx, dir, "master"); err != nil {
		t.Fatalf("checkout master: %v", err)
	}
	if err := DeleteBranch(ctx, dir, "dev", false); err != nil {
		t.Fatalf("delete dev: %v", err)
	}

	// 本地 bare 远端：push -u 由两步完成（remote add + push），验证 runStream 与进度事件
	remoteDir := t.TempDir()
	if _, err := Run(ctx, remoteDir, "init", "--bare", "-q"); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	mustRun(t, ctx, dir, "remote", "add", "origin", filepath.Clean(remoteDir))

	var events []Progress
	SetEmitter(func(name string, data ...any) {
		if name == "git:progress" {
			if p, ok := data[0].(Progress); ok {
				events = append(events, p)
			}
		}
	})
	defer SetEmitter(nil)

	if err := Push(ctx, dir); err != nil {
		t.Fatalf("push: %v", err)
	}
	if err := Fetch(ctx, dir); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	// push/fetch 成功都必须以 done(100) 收尾
	if len(events) == 0 || events[len(events)-1].Stage != StageDone {
		t.Fatalf("progress events=%+v", events)
	}
	doneOps := map[string]bool{}
	for _, e := range events {
		if e.Stage == StageDone {
			doneOps[e.Op] = true
		}
	}
	if !doneOps["push"] || !doneOps["fetch"] {
		t.Fatalf("ops=%+v events=%+v", doneOps, events)
	}
	// fetch 后远程分支出现在列表
	bs, err = Branches(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, b := range bs {
		if b.Remote && b.Name == "origin/master" {
			found = true
		}
	}
	if !found {
		t.Fatalf("origin/master missing: %+v", bs)
	}

	// push -u 后 master 已有上游：pull 在 file 协议本地远端上应成功（无新内容）
	if err := Pull(ctx, dir); err != nil {
		t.Fatalf("pull: %v", err)
	}
}

func TestParseProgressLineFilter(t *testing.T) {
	// pctRe 只认带百分比的行；普通 stderr 行不该被当成进度
	if pctRe.MatchString("Enumerating objects: 5, done.") {
		t.Fatal("false positive on non-percent line")
	}
	m := pctRe.FindStringSubmatch("Writing objects: 60% (6/10)")
	if m == nil || m[1] != "60" {
		t.Fatalf("miss: %v", m)
	}
}
