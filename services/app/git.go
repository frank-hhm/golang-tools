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

// GitDiffFile 变更文件清单（增删行数；opts 决定看 工作区/暂存区/某提交）。
func (a *App) GitDiffFile(repo string, opts gitcmd.DiffOpts) ([]gitcmd.FileDiff, error) {
	return gitcmd.Files(context.Background(), repo, opts)
}

// GitDiffText 一个文件或一段差异的 unified diff 文本。
func (a *App) GitDiffText(repo string, opts gitcmd.DiffOpts) (string, error) {
	return gitcmd.Text(context.Background(), repo, opts)
}
