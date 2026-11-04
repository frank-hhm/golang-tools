package app

import (
	"context"

	"Tools/tools/gitcmd"
)

/*
Git 可视化（gitcmd）绑定层——核心工作台 P1。

内核在 tools/gitcmd：调本机 git.exe/git CLI 并解析机器可读输出，跨平台零 cgo。
不加 build tag：Linux 面板同样存在（AGENTS「绑定方法不能藏在 build tag 后面」）；
git 命令本身三平台一致，面板语义 = 只读浏览/操作服务器上的仓库。

凭据一律交给 git 自己的 credential helper，本层不碰任何密码/token。
推送/拉取/抓取的进度经 git:progress 事件上报（main 里 SetEmitter 注入）。
*/

// GitCheck 探测本机 git（是否存在、版本达标）。永不返回 error，结果在 Info 里，
// 前端据此引导安装。
func (a *App) GitCheck() gitcmd.Info {
	return gitcmd.Check(context.Background())
}

// GitDiscover 从目录向上定位仓库（登记前的校验与基本信息）。
func (a *App) GitDiscover(path string) (gitcmd.Repo, error) {
	return gitcmd.Discover(context.Background(), path)
}

// GitRepos 读仓库登记表。
func (a *App) GitRepos() ([]gitcmd.RepoRef, error) {
	return gitcmd.LoadRepos()
}

// GitRepoAdd 登记仓库（校验是 git 仓库后按路径去重），返回最新列表。
func (a *App) GitRepoAdd(path string) ([]gitcmd.RepoRef, error) {
	return gitcmd.AddRepo(context.Background(), path)
}

// GitRepoRemove 从登记表移除，返回最新列表。
func (a *App) GitRepoRemove(path string) ([]gitcmd.RepoRef, error) {
	return gitcmd.RemoveRepo(path)
}

// GitStatus 一次工作区状态快照（手动刷新 / 进仓库时拉一次，不做轮询）。
func (a *App) GitStatus(repo string) (gitcmd.Status, error) {
	return gitcmd.StatusOf(context.Background(), repo)
}

// GitStage 暂存指定路径。
func (a *App) GitStage(repo string, paths []string) error {
	return gitcmd.Stage(context.Background(), repo, paths)
}

// GitUnstage 取消暂存（保留工作区改动）。
func (a *App) GitUnstage(repo string, paths []string) error {
	return gitcmd.Unstage(context.Background(), repo, paths)
}

// GitStageAll 全部暂存。
func (a *App) GitStageAll(repo string) error {
	return gitcmd.StageAll(context.Background(), repo)
}

// GitUnstageAll 全部取消暂存。
func (a *App) GitUnstageAll(repo string) error {
	return gitcmd.UnstageAll(context.Background(), repo)
}

// GitCommit 提交已暂存改动（amend 改写上一笔），返回新提交短哈希。
func (a *App) GitCommit(repo, message string, amend bool) (string, error) {
	return gitcmd.CommitChanges(context.Background(), repo, message, amend)
}

// GitPull 拉取并合并（进度经 git:progress 事件）。
func (a *App) GitPull(repo string) error {
	return gitcmd.Pull(context.Background(), repo)
}

// GitFetch 抓取远端并清理陈旧引用。
func (a *App) GitFetch(repo string) error {
	return gitcmd.Fetch(context.Background(), repo)
}

// GitPush 推送当前分支（首次推送自动建立上游）。
func (a *App) GitPush(repo string) error {
	return gitcmd.Push(context.Background(), repo)
}

// GitBranchList 本地 + 远程分支（含领先落后与当前标记）。
func (a *App) GitBranchList(repo string) ([]gitcmd.Branch, error) {
	return gitcmd.Branches(context.Background(), repo)
}

// GitCheckout 切换分支/提交。
func (a *App) GitCheckout(repo, name string) error {
	return gitcmd.Checkout(context.Background(), repo, name)
}

// GitCreateBranch 建分支，checkout=true 同时切过去。
func (a *App) GitCreateBranch(repo, name string, checkout bool) error {
	return gitcmd.CreateBranch(context.Background(), repo, name, checkout)
}

// GitDeleteBranch 删分支（force=true 未合并也删，前端需二次确认）。
func (a *App) GitDeleteBranch(repo, name string, force bool) error {
	return gitcmd.DeleteBranch(context.Background(), repo, name, force)
}

// GitLog 取提交历史，带父信息与 lane 布局列（前端 canvas 只按 lane/行号画连线）。
//   - revision 非空：从该 ref/区间起算（空 = HEAD）。
//   - path 非空：只看这一个文件的历史，引擎附 --follow 跨重命名回溯。
//   - limit<=0 取引擎默认 200。
func (a *App) GitLog(repo, revision, path string, limit int) ([]gitcmd.Commit, error) {
	return gitcmd.Log(context.Background(), repo, revision, path, limit)
}

// GitBlame 取某文件（可选版本）的逐行归属，连续同提交聚合成段（BlamePane）。
//   - path 必填（相对仓库根）；revision 空 = 工作区当前。
func (a *App) GitBlame(repo, path, revision string) ([]gitcmd.BlameSegment, error) {
	return gitcmd.Blame(context.Background(), repo, path, revision)
}

// GitCreateBranchAt 在指定起点（提交哈希/分支/ref）建分支，checkout=true 同时切过去。
func (a *App) GitCreateBranchAt(repo, name, startRef string, checkout bool) error {
	return gitcmd.CreateBranchAt(context.Background(), repo, name, startRef, checkout)
}

// GitCherryPick 把某笔提交应用到当前分支（冲突时中断，本期只发起）。
func (a *App) GitCherryPick(repo, hash string) error {
	return gitcmd.CherryPick(context.Background(), repo, hash)
}

// GitRevert 生成一笔反向提交。
func (a *App) GitRevert(repo, hash string) error {
	return gitcmd.Revert(context.Background(), repo, hash)
}

// GitReset 按 mode（soft|mixed|hard）回退到 target（空=HEAD）；hard 丢工作区改动，前端必须二次确认。
func (a *App) GitReset(repo, mode, target string) error {
	return gitcmd.Reset(context.Background(), repo, mode, target)
}

// GitDiffFile 变更文件清单（增删行数；opts 决定看 工作区/暂存区/某提交）。
func (a *App) GitDiffFile(repo string, opts gitcmd.DiffOpts) ([]gitcmd.FileDiff, error) {
	return gitcmd.Files(context.Background(), repo, opts)
}

// GitDiffText 一个文件或一段差异的 unified diff 文本。
func (a *App) GitDiffText(repo string, opts gitcmd.DiffOpts) (string, error) {
	return gitcmd.Text(context.Background(), repo, opts)
}

// GitShowBlob 取某版本下某文件的完整内容（供并排 diff 左右两栏）。
// rev："HEAD"/"<hash>"/"<hash>^" 等；rev=="" 表示暂存区（index）。该版本无此文件时回空串。
func (a *App) GitShowBlob(repo, rev, path string) (string, error) {
	return gitcmd.ShowBlob(context.Background(), repo, rev, path)
}

// ---------- P3：暂存栈 / 标签 / 远端 / 冲突三方 / rebase ----------

// GitStashList 列出暂存栈（新→旧）。
func (a *App) GitStashList(repo string) ([]gitcmd.StashEntry, error) {
	return gitcmd.StashList(context.Background(), repo)
}

// GitStashPush 暂存当前改动；includeUntracked=true 会连未跟踪文件一并收走（UI 需明确提示）。
// 无改动可暂存时返回可读错误。
func (a *App) GitStashPush(repo, msg string, includeUntracked bool) error {
	return gitcmd.StashPush(context.Background(), repo, msg, includeUntracked)
}

// GitStashApply 应用一条暂存但不出栈（ref：序号字符串或 stash@{N}，空=栈顶）。
func (a *App) GitStashApply(repo, ref string) error {
	return gitcmd.StashApply(context.Background(), repo, ref)
}

// GitStashPop 应用一条暂存并出栈。
func (a *App) GitStashPop(repo, ref string) error {
	return gitcmd.StashPop(context.Background(), repo, ref)
}

// GitStashDrop 丢弃一条暂存（破坏操作，前端需二次确认）。
func (a *App) GitStashDrop(repo, ref string) error {
	return gitcmd.StashDrop(context.Background(), repo, ref)
}

// GitTagList 列出标签。
func (a *App) GitTagList(repo string) ([]gitcmd.Tag, error) {
	return gitcmd.TagList(context.Background(), repo)
}

// GitTagCreate 建标签：message 非空为附注标签（本工具强制不签名），空为轻量；target 空=HEAD。
func (a *App) GitTagCreate(repo, name, message, target string) error {
	return gitcmd.TagCreate(context.Background(), repo, name, message, target)
}

// GitTagDelete 删本地标签（破坏操作，前端需二次确认）。
func (a *App) GitTagDelete(repo, name string) error {
	return gitcmd.TagDelete(context.Background(), repo, name)
}

// GitRemoteList 列出远端及抓取/推送地址。
func (a *App) GitRemoteList(repo string) ([]gitcmd.Remote, error) {
	return gitcmd.RemoteList(context.Background(), repo)
}

// GitRemoteAdd 新增远端。
func (a *App) GitRemoteAdd(repo, name, url string) error {
	return gitcmd.RemoteAdd(context.Background(), repo, name, url)
}

// GitRemoteRemove 删除远端（破坏操作，前端需二次确认）。
func (a *App) GitRemoteRemove(repo, name string) error {
	return gitcmd.RemoteRemove(context.Background(), repo, name)
}

// GitRemoteSetURL 改远端地址；push=true 只改推送地址。
func (a *App) GitRemoteSetURL(repo, name, url string, push bool) error {
	return gitcmd.RemoteSetURL(context.Background(), repo, name, url, push)
}

// GitOpState 仓库是否处于 merge/rebase/cherry-pick/revert 等中断态（前端据此显示冲突/继续条）。
func (a *App) GitOpState(repo string) (gitcmd.OpState, error) {
	return gitcmd.OpStateOf(context.Background(), repo)
}

// GitConflicts 列出冲突（未合并）文件路径。
func (a *App) GitConflicts(repo string) ([]string, error) {
	return gitcmd.Conflicts(context.Background(), repo)
}

// GitConflictSides 取某冲突文件的三方内容（基线/我方/对方）。
func (a *App) GitConflictSides(repo, path string) (gitcmd.ConflictSides, error) {
	return gitcmd.ConflictSidesOf(context.Background(), repo, path)
}

// GitResolveConflict 按 side（ours|theirs）选边解决并标记已解决（内部走 git add）。
func (a *App) GitResolveConflict(repo, path, side string) error {
	return gitcmd.ResolveConflict(context.Background(), repo, path, side)
}

// GitMarkResolved 把（已手动编辑的）冲突文件标记为已解决（git add）。
func (a *App) GitMarkResolved(repo, path string) error {
	return gitcmd.MarkResolved(context.Background(), repo, path)
}

// GitRebaseContinue 续跑中断的 rebase（已顶掉编辑器，不会卡在 $EDITOR）。
func (a *App) GitRebaseContinue(repo string) error {
	return gitcmd.RebaseContinue(context.Background(), repo)
}

// GitRebaseAbort 放弃 rebase，回到前状态（破坏操作，前端需二次确认）。
func (a *App) GitRebaseAbort(repo string) error {
	return gitcmd.RebaseAbort(context.Background(), repo)
}
