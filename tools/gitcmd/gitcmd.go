// Package gitcmd 是 Git 可视化的引擎层：发现本机 git.exe、以统一方式运行 git
// 子命令、把 --porcelain=v2 等机器可读输出解析成领域模型。**不含任何 UI 语义**。
//
// 设计取向（对齐计划决策）：
//   - 调本机 git.exe（TortoiseGit 同路子），不引 go-git；未装 git 时功能整体不可用，
//     由前端引导安装。
//   - 跨平台零 cgo：git 子命令本身三平台一致，差异只在「不弹控制台窗」的系统调用上，
//     按 *_windows.go / *_other.go 分开。
//   - 凭据一律交给 git 自己的 credential helper，本包不碰密码/token。
//   - 不做轮询：状态由调用方在「进入仓库 / 手动刷新」时拉一次。
package gitcmd

import (
	"errors"
	"strings"
)

// ErrGitNotFound 本机找不到 git 可执行文件（或未达最低版本）。前端据此引导安装。
var ErrGitNotFound = errors.New("未检测到可用的 git，请安装 Git 后重试")

// ErrNotRepo 目标目录不在任何 git 工作树内。
var ErrNotRepo = errors.New("当前目录不是 Git 仓库")

// minMajor/minMinor porcelain v2 需要 git ≥ 2.11；这里取更稳的 2.18 作门槛
// （2.18 起 --porcelain=v2 的 branch.ab、rename 记录等更规整）。
const (
	minMajor = 2
	minMinor = 18
)

// Info git 二进制的探测结果。
type Info struct {
	Found   bool   `json:"found"`   // 是否找到 git
	OK      bool   `json:"ok"`      // 且版本达标（可安全使用本包）
	Path    string `json:"path"`    // 解析到的可执行路径
	Version string `json:"version"` // "git version 2.55.0..." 里提取的版本号
	Reason  string `json:"reason"`  // OK=false 时给人类的说明
}

// FileEntry 工作区里一个有变化的文件。
type FileEntry struct {
	Path     string `json:"path"`     // 相对仓库根的路径
	OrigPath string `json:"origPath"` // 重命名/复制时的原路径（否则空）
	X        string `json:"x"`        // 暂存区状态码（相对 HEAD）
	Y        string `json:"y"`        // 工作区状态码（相对暂存区）
	Conflict bool   `json:"conflict"` // 是否处于冲突（未合并）
}

// Status 一次 git status 的解析结果。
type Status struct {
	Branch     string      `json:"branch"`     // 当前分支名；游离 HEAD 时为 "(头分离 ..."
	OID        string      `json:"oid"`        // HEAD 指向的 commit
	Upstream   string      `json:"upstream"`   // 上游分支（无则空）
	Ahead      int         `json:"ahead"`      // 领先上游的提交数
	Behind     int         `json:"behind"`     // 落后上游的提交数
	Staged     []FileEntry `json:"staged"`     // 已暂存（相对 HEAD 有改动）
	Unstaged   []FileEntry `json:"unstaged"`   // 工作区改动未暂存
	Untracked  []FileEntry `json:"untracked"`  // 未跟踪
	Conflicted []FileEntry `json:"conflicted"` // 冲突未解决
}

// Repo 一个仓库的定位信息。
type Repo struct {
	Path           string `json:"path"`           // 传入的查询路径（已规整）
	Toplevel       string `json:"toplevel"`       // 工作树根（--show-toplevel）
	GitDir         string `json:"gitDir"`         // .git 目录（--git-dir）
	Bare           bool   `json:"bare"`           // 裸仓
	InsideWorkTree bool   `json:"insideWorkTree"` // 是否在 worktree 内
	Detached       bool   `json:"detached"`       // 是否游离 HEAD
	Branch         string `json:"branch"`         // 当前分支（游离时为空）
}

// Commit 历史里一个提交节点。lane/parents 供前端 canvas 画 DAG。
type Commit struct {
	Hash        string   `json:"hash"`
	Short       string   `json:"short"`
	Subject     string   `json:"subject"`
	Author      string   `json:"author"`
	AuthorEmail string   `json:"authorEmail"`
	Time        int64    `json:"time"`    // Unix 秒
	Refs        string   `json:"refs"`    // decorate 出来的分支/标签（可空）
	Parents     []string `json:"parents"` // 父提交短哈希
	Lane        int      `json:"lane"`    // 布局列（本包算好，前端只画）
}

// FileDiff diff --numstat 的一行。
type FileDiff struct {
	Path    string `json:"path"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Binary  bool   `json:"binary"`
}

// Branch 一条本地/远程分支。
type Branch struct {
	Name          string `json:"name"`
	Current       bool   `json:"current"`
	Upstream      string `json:"upstream"`
	Ahead         int    `json:"ahead"`
	Behind        int    `json:"behind"`
	Subject       string `json:"subject"`
	Remote        bool   `json:"remote"`
	CommitterTime int64  `json:"committerTime"`
}

// trimGitErr 从 git 的 stderr 里取一句人话错误：优先 fatal:/error: 首行。
func trimGitErr(stderr string) string {
	for _, ln := range strings.Split(stderr, "\n") {
		s := strings.TrimSpace(ln)
		if s == "" {
			continue
		}
		if strings.HasPrefix(s, "fatal:") || strings.HasPrefix(s, "error:") {
			return s
		}
	}
	// 没有 fatal/error 就取第一非空行
	for _, ln := range strings.Split(stderr, "\n") {
		if s := strings.TrimSpace(ln); s != "" {
			return s
		}
	}
	return strings.TrimSpace(stderr)
}
