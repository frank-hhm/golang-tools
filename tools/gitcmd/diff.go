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

// ShowBlob 取某版本下某文件的完整内容（git show <rev>:<path>），供并排 diff 用左右两栏。
//   - rev 为 git 修订词："HEAD"、"<hash>"、"<hash>^"（父）等；rev=="" 表示暂存区（index，即 ":path"）。
//   - 该版本不含此文件（新增文件的父版本、删除文件的当前版本、首提交的父）返回空串而非报错，
//     前端据此把一侧渲染成空、另一侧全增/全删。
func ShowBlob(ctx context.Context, repoDir, rev, path string) (string, error) {
	if path == "" {
		return "", ErrNotRepo
	}
	spec := rev + ":" + path
	out, err := Run(ctx, repoDir, "show", spec)
	if err != nil {
		if missingBlob(err.Error()) {
			return "", nil
		}
		return "", err
	}
	return out.Stdout, nil
}

// missingBlob 判断 git show 的失败是否只是「该版本没有这个文件」，而非仓库损坏等真错误。
func missingBlob(msg string) bool {
	for _, s := range []string{
		"does not exist", // fatal: path 'x' does not exist in 'rev'
		"exists on disk, but not in",
		"unknown revision",    // <rootHash>^
		"invalid object name", // HEAD^ 于首提交：fatal: invalid object name 'HEAD^'
		"bad revision",
		"does not have a", // rev 不是 tree
	} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// parseNumstat 解析 `diff --numstat` / `show --numstat` 输出。
// 行格式："<added>\t<deleted>\t<path>"，二进制是 "-\t-\t<path>"。
// 重命名路径形如 "old => new" 或 "dir/{old => new}.ext"，P0 原样保留展示。
// 冲突（unmerged）路径 git 会按 stage 各出一行（同一路径出现多次），这里按 path 合并成
// 一行——否则前端以 path 为 v-for key 会撞车（引擎层真机对拍 §十四 记的缺陷）。
func parseNumstat(text string) []FileDiff {
	list := []FileDiff{}
	idx := map[string]int{} // path -> list 下标
	for _, line := range strings.Split(text, "\n") {
		if line == "" {
			continue
		}
		f := strings.SplitN(line, "\t", 3)
		if len(f) < 3 {
			continue
		}
		path := strings.TrimSpace(f[2])
		bin := f[0] == "-" || f[1] == "-"
		var add, del int
		if !bin {
			add, _ = strconv.Atoi(strings.TrimSpace(f[0]))
			del, _ = strconv.Atoi(strings.TrimSpace(f[1]))
		}
		if i, ok := idx[path]; ok {
			// 同一路径多行（按 stage）：取各阶段最大增删，任一为二进制即整体标二进制。
			d := &list[i]
			if bin {
				d.Binary = true
			}
			if add > d.Added {
				d.Added = add
			}
			if del > d.Deleted {
				d.Deleted = del
			}
			continue
		}
		idx[path] = len(list)
		list = append(list, FileDiff{Path: path, Added: add, Deleted: del, Binary: bin})
	}
	return list
}
