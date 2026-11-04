package gitcmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ---------- P3：冲突三方与解决、rebase 续跑 ----------
//
// 冲突文件的三个版本以 stage 槽存于 index：1=共同祖先、2=我方(ours)、3=对方(theirs)。
// `git ls-files -u` 逐槽给出 blob 号，取其内容用 `git cat-file blob`（原始字节，不经
// diff 加工）。解决：ours/theirs 走 `git checkout --ours/--theirs` 覆写工作区，手动编辑
// 后与选边一样以 `git add` 标记已解决。

// Conflicts 列出处于冲突（未合并）的文件路径。
func Conflicts(ctx context.Context, repoDir string) ([]string, error) {
	out, err := Run(ctx, repoDir, "diff", "--name-only", "--diff-filter=U")
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for _, line := range strings.Split(out.Stdout, "\n") {
		if p := strings.TrimSpace(line); p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}

// ConflictSidesOf 取某冲突文件的三方内容。缺失的槽（如 add/add 无祖先）回空串。
func ConflictSidesOf(ctx context.Context, repoDir, path string) (ConflictSides, error) {
	if path == "" {
		return ConflictSides{}, ErrNotRepo
	}
	out, err := Run(ctx, repoDir, "ls-files", "-u", "--", path)
	if err != nil {
		return ConflictSides{}, err
	}
	// 每行："<mode> <sha1> <stage>\t<path>"
	shas := map[int]string{}
	for _, line := range strings.Split(out.Stdout, "\n") {
		if line == "" {
			continue
		}
		tab := strings.SplitN(line, "\t", 2)
		meta := strings.Fields(tab[0])
		if len(meta) < 3 {
			continue
		}
		stage, e := strconv.Atoi(meta[2])
		if e != nil {
			continue
		}
		shas[stage] = meta[1]
	}
	sides := ConflictSides{Path: path}
	var cerr error
	if s, ok := shas[1]; ok {
		sides.Base, cerr = catBlob(ctx, repoDir, s)
	}
	if s, ok := shas[2]; ok && cerr == nil {
		sides.Ours, cerr = catBlob(ctx, repoDir, s)
	}
	if s, ok := shas[3]; ok && cerr == nil {
		sides.Theirs, cerr = catBlob(ctx, repoDir, s)
	}
	if cerr != nil {
		return ConflictSides{}, cerr
	}
	return sides, nil
}

// catBlob 取一个 blob 号的原始内容；sha 空回空串。
func catBlob(ctx context.Context, repoDir, sha string) (string, error) {
	if sha == "" {
		return "", nil
	}
	out, err := Run(ctx, repoDir, "cat-file", "blob", sha)
	if err != nil {
		return "", err
	}
	return out.Stdout, nil
}

// ResolveConflict 按 side（ours|theirs）选边解决冲突并标记为已解决。
// 注意：merge 语境下 ours=当前分支、theirs=被并入的分支；rebase 语境下二者语义相反
// （ours=正在被 rebase 到的分支，theirs=正在重放的提交），前端文案需据 OpState 区分。
func ResolveConflict(ctx context.Context, repoDir, path, side string) error {
	var flag string
	switch side {
	case "ours":
		flag = "--ours"
	case "theirs":
		flag = "--theirs"
	default:
		return fmt.Errorf("非法的冲突解决方式: %s", side)
	}
	if _, err := Run(ctx, repoDir, "checkout", flag, "--", path); err != nil {
		return err
	}
	return MarkResolved(ctx, repoDir, path)
}

// MarkResolved 把（已手动编辑或已选边的）文件标记为已解决（git add）。
func MarkResolved(ctx context.Context, repoDir, path string) error {
	_, err := Run(ctx, repoDir, "add", "--", path)
	return err
}

// OpStateOf 探测仓库处于哪种多阶段操作的中断态，靠 .git 下的标记文件判定。
func OpStateOf(ctx context.Context, repoDir string) (OpState, error) {
	out, err := Run(ctx, repoDir, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return OpState{}, err
	}
	gitDir := trimTail(out.Stdout)
	has := func(names ...string) bool {
		for _, n := range names {
			if _, e := os.Stat(filepath.Join(gitDir, n)); e == nil {
				return true
			}
		}
		return false
	}
	st := OpState{
		Merge:      has("MERGE_HEAD"),
		CherryPick: has("CHERRY_PICK_HEAD"),
		Revert:     has("REVERT_HEAD"),
		Bisect:     has("BISECT_LOG"),
		// rebase 目录名因版本/模式而异：应用式 rebase-apply，合并式 rebase-merge
		Rebase: has("rebase-merge", "rebase-apply"),
	}
	return st, nil
}

// RebaseContinue 续跑中断的 rebase。注入 `-c core.editor=true` 让保存即接受的空操作
// 顶掉编辑器，避免在无头子进程里卡在 $EDITOR（与 Revert 的 --no-edit 同一考量）。
func RebaseContinue(ctx context.Context, repoDir string) error {
	_, err := Run(ctx, repoDir, "-c", "core.editor=true", "rebase", "--continue")
	return err
}

// RebaseAbort 放弃 rebase，回到 rebase 前的状态（破坏操作，前端须二次确认）。
func RebaseAbort(ctx context.Context, repoDir string) error {
	_, err := Run(ctx, repoDir, "rebase", "--abort")
	return err
}
