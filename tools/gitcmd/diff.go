package gitcmd

import (
	"context"
	"strconv"
	"strings"
)

// DiffOpts 描述要看哪一段差异。
//   - Commit 非空：看该提交自身引入的改动（git show）。
//   - 否则 Staged=true：看已暂存改动（HEAD..index，git diff --cached）。
//   - 否则：看工作区未暂存改动（index..worktree，git diff）。
//   - Path 非空：只看这一个文件。
type DiffOpts struct {
	Staged bool   `json:"staged"`
	Commit string `json:"commit"`
	Path   string `json:"path"`
}

// diffArgs 按 DiffOpts 拼子命令参数；withText=true 时出 unified diff 文本，否则出 numstat。
func (d DiffOpts) diffArgs(withText bool) []string {
	args := []string{"diff"}
	if withText {
		args = append(args, "--no-ext-diff", "--unified=3")
	} else {
		args = append(args, "--numstat")
	}
	if d.Staged {
		args = append(args, "--cached")
	}
	if d.Path != "" {
		args = append(args, "--", d.Path)
	}
	return args
}

// showArgs Commit 非空时改用 show。注意 commit 必须排在 `--` 之前（否则被当成路径）。
func (d DiffOpts) showArgs(withText bool) []string {
	args := []string{"show", "--format="}
	if withText {
		args = append(args, "--no-color", "--unified=3")
	} else {
		args = append(args, "--numstat")
	}
	args = append(args, d.Commit)
	if d.Path != "" {
		args = append(args, "--", d.Path)
	}
	return args
}

// Files 返回变更文件清单（增删行数）。二进制文件 Added/Deleted 为 0、Binary=true。
func Files(ctx context.Context, repoDir string, opts DiffOpts) ([]FileDiff, error) {
	var out Out
	var err error
	if opts.Commit != "" {
		out, err = Run(ctx, repoDir, opts.showArgs(false)...)
	} else {
		out, err = Run(ctx, repoDir, opts.diffArgs(false)...)
	}
	if err != nil {
		return nil, err
	}
	return parseNumstat(out.Stdout), nil
}

// Text 返回 unified diff 文本（供前端高亮展示）。
func Text(ctx context.Context, repoDir string, opts DiffOpts) (string, error) {
	var out Out
	var err error
	if opts.Commit != "" {
		out, err = Run(ctx, repoDir, opts.showArgs(true)...)
	} else {
		out, err = Run(ctx, repoDir, opts.diffArgs(true)...)
	}
	if err != nil {
		return "", err
	}
	return out.Stdout, nil
}

// parseNumstat 解析 `diff --numstat` / `show --numstat` 输出。
// 行格式："<added>\t<deleted>\t<path>"，二进制是 "-\t-\t<path>"。
// 重命名路径形如 "old => new" 或 "dir/{old => new}.ext"，P0 原样保留展示。
func parseNumstat(text string) []FileDiff {
	list := []FileDiff{}
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "\t", 3)
		if len(f) < 3 {
			continue
		}
		d := FileDiff{Path: strings.TrimSpace(f[2])}
		if f[0] == "-" || f[1] == "-" {
			d.Binary = true
		} else {
			d.Added, _ = strconv.Atoi(strings.TrimSpace(f[0]))
			d.Deleted, _ = strconv.Atoi(strings.TrimSpace(f[1]))
		}
		list = append(list, d)
	}
	return list
}
