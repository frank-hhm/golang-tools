package gitcmd

import (
	"context"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// longOpTimeout 推送/拉取/抓取的上限：大仓弱网下 60s 默认值会砍断，给 10 分钟。
const longOpTimeout = 10 * time.Minute

// ---------- 暂存与提交 ----------

// Stage 暂存指定路径（git add -- <paths>）。
func Stage(ctx context.Context, repoDir string, paths []string) error {
	_, err := Run(ctx, repoDir, append([]string{"add", "--"}, paths...)...)
	return err
}

// StageAll 全部暂存（含删除，git add -A）。
func StageAll(ctx context.Context, repoDir string) error {
	_, err := Run(ctx, repoDir, "add", "-A")
	return err
}

// Unstage 取消暂存指定路径，保留工作区改动（git reset -- <paths>；
// 空仓（无 HEAD）时 reset 会失败，退化为 git rm --cached）。
func Unstage(ctx context.Context, repoDir string, paths []string) error {
	_, err := Run(ctx, repoDir, append([]string{"reset", "-q", "--"}, paths...)...)
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "Failed to resolve 'HEAD'") {
		_, rerr := Run(ctx, repoDir, append([]string{"rm", "--cached", "-q", "--force"}, paths...)...)
		return rerr
	}
	return err
}

// UnstageAll 全部取消暂存（git reset；空仓退化为 rm --cached -r）。
func UnstageAll(ctx context.Context, repoDir string) error {
	_, err := Run(ctx, repoDir, "reset", "-q")
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "Failed to resolve 'HEAD'") {
		_, rerr := Run(ctx, repoDir, "rm", "--cached", "-rq", "--force", "--")
		return rerr
	}
	return err
}

// CommitChanges 提交已暂存改动（函数名避开领域类型 Commit），返回新提交的短哈希。
// message 空由调用方挡掉；amend=false 且无暂存时 git 自己报错，人话错误原样透出。
func CommitChanges(ctx context.Context, repoDir, message string, amend bool) (string, error) {
	args := []string{"commit", "-m", message}
	if amend {
		args = append(args, "--amend", "--no-edit")
	}
	if _, err := Run(ctx, repoDir, args...); err != nil {
		return "", err
	}
	out, err := Run(ctx, repoDir, "rev-parse", "--short", "HEAD")
	if err != nil {
		return "", err
	}
	return out.OutStr(), nil
}

// ---------- 分支 ----------

// branchFmt for-each-ref 的字段模板：短名/对象短哈希/上游/追踪状态/提交秒级时间/标题，TAB 分隔。
const branchFmt = "%(refname:short)%09%(objectname:short)%09%(upstream:short)%09%(upstream:track)%09%(committerdate:unix)%09%(subject)"

var trackRe = regexp.MustCompile(`ahead (\d+)`)

// trackBehindRe 与 ahead 分开抓："[ahead 2, behind 1]" 两个方向未必同时存在。
var trackBehindRe = regexp.MustCompile(`behind (\d+)`)

// Branches 列本地与远程分支。当前分支以 symbolic-ref 判定（游离 HEAD 时全 false）。
func Branches(ctx context.Context, repoDir string) ([]Branch, error) {
	cur := ""
	if out, err := Run(ctx, repoDir, "symbolic-ref", "--short", "-q", "HEAD"); err == nil {
		cur = out.OutStr()
	}
	list := []Branch{}
	for _, ns := range []struct {
		ref    string
		remote bool
	}{{"refs/heads", false}, {"refs/remotes", true}} {
		out, err := Run(ctx, repoDir, "for-each-ref", "--format="+branchFmt, ns.ref)
		if err != nil {
			return nil, err
		}
		for _, line := range strings.Split(out.Stdout, "\n") {
			f := strings.Split(line, "\t")
			if len(f) < 6 || f[0] == "" || strings.HasSuffix(f[0], "/HEAD") {
				continue // 空行与 origin/HEAD 符号别名不参与展示
			}
			b := Branch{Name: f[0], Upstream: f[2], Subject: f[5], Remote: ns.remote}
			if t, e := strconv.ParseInt(f[4], 10, 64); e == nil {
				b.CommitterTime = t
			}
			b.Ahead, _ = atoiFirst(trackRe, f[3])
			b.Behind, _ = atoiFirst(trackBehindRe, f[3])
			b.Current = !ns.remote && f[0] == cur
			list = append(list, b)
		}
	}
	return list, nil
}

func atoiFirst(re *regexp.Regexp, s string) (int, bool) {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	n, err := strconv.Atoi(m[1])
	return n, err == nil
}

// Checkout 切到指定分支/提交（脏工作区会被 git 自己拒绝，错误人话透出）。
func Checkout(ctx context.Context, repoDir, name string) error {
	_, err := Run(ctx, repoDir, "checkout", name)
	return err
}

// CreateBranch 建分支；checkout=true 同时切过去。
func CreateBranch(ctx context.Context, repoDir, name string, checkout bool) error {
	if _, err := Run(ctx, repoDir, "branch", name); err != nil {
		return err
	}
	if checkout {
		return Checkout(ctx, repoDir, name)
	}
	return nil
}

// DeleteBranch 删分支；force=true 用 -D（未合并也删）。
func DeleteBranch(ctx context.Context, repoDir, name string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	_, err := Run(ctx, repoDir, "branch", flag, name)
	return err
}

// ---------- 长操作（带进度事件） ----------

// pctRe 从进度行里抓百分比（"Receiving objects: 45% (9/20)"）。
var pctRe = regexp.MustCompile(`(\d{1,3})%`)

// withLongTimeout 与 withTimeout 同构，只是兜底上限换成长操作的 10 分钟。
func withLongTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if _, ok := ctx.Deadline(); ok {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, longOpTimeout)
}

// remoteOp 跑一条网络子命令，把 stderr 进度行翻成 git:progress 事件。
// op 进事件负载（push/pull/fetch），仓库目录名作任务名。
func remoteOp(ctx context.Context, repoDir, op string, args ...string) error {
	// ctx 带 deadline 就尊重调用方的，否则兜 longOpTimeout。
	pctx, cancel := withLongTimeout(ctx)
	defer cancel()
	name := filepath.Base(filepath.Clean(repoDir))
	last := -1
	onLine := func(line string) {
		m := pctRe.FindStringSubmatch(line)
		if m == nil {
			return
		}
		p, e := strconv.Atoi(m[1])
		if e != nil || p < 0 || p > 100 || p == last {
			return
		}
		last = p
		emitProgress(Progress{Op: op, Name: name, Stage: StageRunning, Percent: p, Note: line})
	}
	_, err := runStream(pctx, repoDir, onLine, args...)
	if err != nil {
		emitProgress(Progress{Op: op, Name: name, Stage: StageError, Percent: last, Note: err.Error()})
		return err
	}
	emitProgress(Progress{Op: op, Name: name, Stage: StageDone, Percent: 100, Note: "完成"})
	return nil
}

// Push 推送当前分支（凭据走 git 自己的 credential helper，本包不碰）。
// 本地 init 后首次推（无上游）时 plain push 会报错，退化为 push -u origin HEAD。
func Push(ctx context.Context, repoDir string) error {
	err := remoteOp(ctx, repoDir, "push", "push", "--progress")
	if err != nil && strings.Contains(err.Error(), "no upstream branch") {
		err = remoteOp(ctx, repoDir, "push", "push", "--progress", "--set-upstream", "origin", "HEAD")
	}
	return err
}

// Pull 拉取并合并（--no-rebase 固定走 merge，行为可预期；--no-edit 防弹编辑器）。
func Pull(ctx context.Context, repoDir string) error {
	return remoteOp(ctx, repoDir, "pull", "pull", "--no-rebase", "--no-edit", "--progress")
}

// Fetch 抓取远端（顺带 --prune 清掉远端已删的陈旧追踪引用）。
func Fetch(ctx context.Context, repoDir string) error {
	return remoteOp(ctx, repoDir, "fetch", "fetch", "--progress", "--prune")
}
