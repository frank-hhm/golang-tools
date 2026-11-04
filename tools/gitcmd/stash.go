package gitcmd

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// ---------- P3：暂存栈（git stash） ----------
//
// stash 的每条记录是 refs/stash 的 reflog 项，故列表用 `git stash list`（走 log 的
// pretty 格式），增删用子命令。ref 选择器统一经 stashRef 规整成 stash@{N}：前端既可
// 传列表里的序号，也可传原样字符串；数字之外的内容因走 argv（非 shell）不构成注入面。

// stashSelRe 从 %gd（形如 "stash@{0}"）里取序号。
var stashSelRe = regexp.MustCompile(`stash@\{(\d+)\}`)

// ErrNothingToStash 工作区无改动可暂存：git stash push 此时退出码为 0、只在 stdout
// 提示，直接返回 nil 会让前端「点了没反应」，故翻成可提示的人话错误。
var ErrNothingToStash = errors.New("没有本地改动可暂存")

// StashList 列出暂存栈（新→旧）。空栈返回空切片（非 nil），前端渲染方便。
func StashList(ctx context.Context, repoDir string) ([]StashEntry, error) {
	out, err := Run(ctx, repoDir, "stash", "list", "--format=%gd%x09%gs%x09%ci")
	if err != nil {
		return nil, err
	}
	list := []StashEntry{}
	for i, line := range strings.Split(out.Stdout, "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "\t", 3)
		if len(f) < 3 {
			continue
		}
		idx := i
		if m := stashSelRe.FindStringSubmatch(f[0]); m != nil {
			if n, e := strconv.Atoi(m[1]); e == nil {
				idx = n
			}
		}
		list = append(list, StashEntry{Index: idx, Message: f[1], Date: f[2]})
	}
	return list, nil
}

// StashPush 暂存当前改动。includeUntracked=true 带 --include-untracked，会把未跟踪文件
// 一并收走（前端须明确提示这点，见计划 §七）。msg 空则用 git 默认的 "WIP on <branch>"。
func StashPush(ctx context.Context, repoDir, msg string, includeUntracked bool) error {
	args := []string{"stash", "push"}
	if includeUntracked {
		args = append(args, "--include-untracked")
	}
	if msg != "" {
		args = append(args, "-m", msg)
	}
	out, err := Run(ctx, repoDir, args...)
	if err != nil {
		return err
	}
	if strings.Contains(out.Stdout, "No local changes to save") {
		return ErrNothingToStash
	}
	return nil
}

// stashRef 把前端传来的选择器规整成 git 认的 stash@{N}：纯数字补成大括号形式，
// 空串默认栈顶，其余原样（已是 stash@{..} 或别的合法 ref）。
func stashRef(ref string) string {
	if ref == "" {
		return "stash@{0}"
	}
	if n, err := strconv.Atoi(ref); err == nil {
		return fmt.Sprintf("stash@{%d}", n)
	}
	return ref
}

// StashApply 应用一条暂存但不出栈（保留记录）。
func StashApply(ctx context.Context, repoDir, ref string) error {
	_, err := Run(ctx, repoDir, "stash", "apply", stashRef(ref))
	return err
}

// StashPop 应用一条暂存并出栈。
func StashPop(ctx context.Context, repoDir, ref string) error {
	_, err := Run(ctx, repoDir, "stash", "pop", stashRef(ref))
	return err
}

// StashDrop 丢弃一条暂存（破坏操作，前端须二次确认）。
func StashDrop(ctx context.Context, repoDir, ref string) error {
	_, err := Run(ctx, repoDir, "stash", "drop", stashRef(ref))
	return err
}
