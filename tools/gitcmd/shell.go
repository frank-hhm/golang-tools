package gitcmd

// ============================================================
// Git 右键挂载的公共定义（P4）
//
// 挂载本身只在 Windows 上有实现（shell_windows.go）：HKCU 静态 verb。
// 非 Windows 一律回 sysx.ErrUnsupported，前端按 Capabilities 把挂载区整块藏起来
// ——与文件压缩那套（tools/archive/shell*.go）同构，但命名空间完全独立：
// verb 键名 ToolsGit*、命令行标记 --git-*，绝不与压缩的 GoToolsArchive/GoTools*
// 或 --archive/--extract 撞车，卸载各删各的。
//
// 这层单独成文件是因为状态结构、命令行标记、op 名必须两头共用：
// main.go 的分流要认得标记，设置页要认得状态，而它们都不能依赖只有
// windows 才编译进来的那个文件。
// ============================================================

import (
	"context"
	"path/filepath"
	"strings"
)

// verb 命名前缀：带程序标识，卸载时只删 ToolsGit 开头的这几把键。
const VerbGitPrefix = "ToolsGit"

// 每个操作一个 verb（PascalCase 后缀），对应一个菜单项与一个命令行标记。
// 菜单文案都带 (Tools)，好和 TortoiseGit 等别家的右键 Git 菜单区分开。
type gitVerb struct {
	suffix string // verb 键后缀：ToolsGitStatus
	op     string // 交给前端的操作名：status
	arg    string // 命令行标记：--git-status
	label  string // 菜单文案
}

// gitVerbs 挂载的全部操作项。顺序即卸载/状态检查顺序。
var gitVerbs = []gitVerb{
	{"Status", "status", "--git-status", "Git (Tools) · 仓库状态"},
	{"Commit", "commit", "--git-commit", "Git (Tools) · 提交"},
	{"Push", "push", "--git-push", "Git (Tools) · 推送"},
	{"Pull", "pull", "--git-pull", "Git (Tools) · 拉取"},
	{"Branch", "branch", "--git-branch", "Git (Tools) · 分支"},
}

// GitArgMarkers 全部命令行标记（供分流与测试比对；值以 --git- 打头，
// 与压缩的 --archive/--extract/--archive-panel 天然不冲突）。
func GitArgMarkers() []string {
	out := make([]string, 0, len(gitVerbs))
	for _, v := range gitVerbs {
		out = append(out, v.arg)
	}
	return out
}

// OpForArg 命令行标记 → 操作名；不是本工具的标记返回 ok=false。
func OpForArg(arg string) (string, bool) {
	for _, v := range gitVerbs {
		if v.arg == arg {
			return v.op, true
		}
	}
	return "", false
}

// verbName 该操作占的 verb 键名。
func (v gitVerb) verbName() string { return VerbGitPrefix + v.suffix }

// shellLocations verb 挂载的三类宿主位置。
//
//	Directory : 右键一个文件夹            → token %1（选中的目录）
//	*         : 右键任意文件              → token %1（文件所在仓库由 rev-parse 上溯）
//	Background: 在文件夹空白处右键        → 无 token（资源管理器把 cwd 设为该目录）
//
// 不挂 Folder（桌面/库根），挂 Directory：后者才对真正的文件夹右键生效。
type shellLocation struct {
	prefix string // 键路径前缀（到 ...\shell\ 为止）
	token  string // %1 / %V / ""（空=命令行不带路径，靠进程 cwd）
	short  string // UI 上缩写这个位置
}

var gitShellLocations = []shellLocation{
	{`Software\Classes\Directory\shell\`, "%1", "文件夹"},
	{`Software\Classes\*\shell\`, "%1", "文件"},
	{`Software\Classes\Directory\Background\shell\`, "", "空白处"},
}

// GitShellInfo 挂载状态。扁平结构，直接给前端用。
//
// 与压缩不同，Git 只有一个总开关管着全部 verb，故不逐项分槽，聚合成一个
// Slot（所有 verb 键里最差的那个）供 UI 显示，明细列在 MountedKeys/ProblemKeys。
type GitShellInfo struct {
	Supported   bool     `json:"supported"`
	Exe         string   `json:"exe"`         // 当前程序路径
	Slot        int      `json:"slot"`        // 聚合槽位态（取所有 verb 键最差）
	Stale       bool     `json:"stale"`       // 有键指向的不是当前 exe（程序挪过位置）
	Detail      string   `json:"detail"`      // 一句话人话说明
	MountedKeys []string `json:"mountedKeys"` // 已确认挂在当前 exe 上的项
	ProblemKeys []string `json:"problemKeys"` // 缺失或失配的项
}

// 挂载槽位的态。逐个 verb 键读回再聚合。
const (
	SlotAbsent  = 0 // 压根没挂
	SlotCurrent = 1 // 挂了，且指向当前 exe
	SlotForeign = 2 // 挂了，但记的是别的路径（要修）
	SlotDamaged = 3 // 键在但读不出来（权限不够，或那个位置被别的程序占了）
)

// SlotText 槽位态的中文说明（前端直接显示，不再拼判断）。
func SlotText(slot int) string {
	switch slot {
	case SlotCurrent:
		return "已挂载"
	case SlotForeign:
		return "指向旧路径"
	case SlotDamaged:
		return "读取失败"
	default:
		return "未挂载"
	}
}

// worstSlot 取一组槽位里最差的那个：只要有一处没挂好，整体就不算挂好。
// 读不出来排在指错之前：前者意味着连改都改不了（权限/被占），报成"指向旧
// 路径"反而会让用户以为点一下修复就行。
func worstSlot(slots []int) int {
	if len(slots) == 0 {
		return SlotAbsent
	}
	worst := SlotCurrent
	for _, s := range slots {
		switch {
		case s == SlotDamaged:
			return SlotDamaged
		case s == SlotForeign && worst != SlotAbsent:
			worst = SlotForeign
		case s == SlotAbsent:
			worst = SlotAbsent
		}
	}
	return worst
}

// GitNeedsRepair 挂载开着但键里记的路径已经不是当前 exe —— 启动时静默重写一次。
// 只认 Stale：SlotDamaged 那一档可能是位置被别的程序占了，擅自覆写别人的键
// 不是"自修复"，是抢地盘。
func GitNeedsRepair(info GitShellInfo) bool { return info.Stale }

// GitAllMounted 聚合槽位是否为"已挂载"（用于「已挂载」这个整体状态显示）。
func GitAllMounted(info GitShellInfo) bool { return info.Slot == SlotCurrent }

// gitDetail 把聚合槽位拼成一句给人看的话（三平台共用，非 Windows 用不上）。
func gitDetail(info GitShellInfo) string {
	switch {
	case GitAllMounted(info):
		return "Git 右键菜单已挂在当前程序上（文件夹 / 文件 / 空白处三类位置）"
	case info.Stale:
		return "程序换过位置，菜单里记的还是旧路径，重新挂载一次即修好"
	case info.Slot == SlotDamaged:
		return "有挂载项读不出来：可能是键被别的程序占了，或当前账户没写权限"
	default:
		return "尚未挂载到系统右键菜单"
	}
}

// ResolveRepo 判断 dir（或其上任一父目录）是否在某 git 工作树内，返回仓库根与是否命中。
//
// 右键挂载的上下文判断就用它：verb 触发后拿到路径，命中则登记/打开，
// 没命中就在拉起的那个进程里 toast 说明——不改菜单可见性（原生 shell 条件
// 注册要写 COM，与「不做图标角标」同因不做）。dir 空时以进程当前目录为准
// （Background 空白处那条就是无参数、cwd 落在该目录）。
func ResolveRepo(ctx context.Context, dir string) (string, bool) {
	repo, err := Discover(ctx, dir)
	if err != nil {
		return "", false
	}
	root := repo.Toplevel
	if root == "" {
		root = repo.GitDir // 裸仓没有 toplevel
	}
	if root == "" || (!repo.InsideWorkTree && !repo.Bare) {
		return "", false
	}
	return filepath.Clean(root), true
}

// firstGitArg 从命令行参数里找第一个 Git 标记及其后紧跟的路径。
// 供 services/app 的分流复用：这里只认精确标记，别的一律不碰，
// 所以普通双击启动、压缩的 --archive 拉起的进程都零影响。
func firstGitArg(args []string) (op, raw string, ok bool) {
	for i, a := range args {
		o, hit := OpForArg(a)
		if !hit {
			continue
		}
		if i+1 < len(args) {
			raw = args[i+1]
		}
		return o, raw, true
	}
	return "", "", false
}

// ParseShellArgs 供右键分流：从参数里认出 Git 标记并返回操作名与仓库路径。
// repo 可能为空串，表示 Background 空白处那种靠进程 cwd 的调用。命中不到任何
// Git 标记时 ok=false（普通双击、压缩的 --archive 拉起的进程都走这一支，零影响）。
func ParseShellArgs(args []string) (op, repo string, ok bool) {
	o, raw, hit := firstGitArg(args)
	if !hit {
		return "", "", false
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return o, "", true
	}
	return o, raw, true
}
