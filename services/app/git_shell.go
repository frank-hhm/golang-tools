package app

// ============================================================
// Git 右键集成的宿主侧接线（P4）：挂载 + 进程间转交 + 开工作台定位到仓库
//
// 与文件压缩（archive.go）同构：引擎在 tools/gitcmd（不认识窗口），跨进程转交
// 在 services/githost，本文件只做三件事——按开关挂或不挂右键与监听、把右键拉起的
// 命令行分流（认仓库 / 转交 / 冷启动定位）、把「开窗 + 发事件」注回给引擎够不着的前端。
//
// 全文件不带 build tag：GitShellStatus/Install/Uninstall/GetPendingGit 是绑定方法，
// 方法 id 由方法名决定，一旦被 tag 挡掉，前端调用就是 unknown bound method id。
// 平台专属的注册表读写留在 tools/gitcmd/shell_windows.go，非 Windows 那层
// GitShellStatus/Install/Uninstall 一律回 sysx.ErrUnsupported，这里只记进日志。
//
// 开关语义（设置里的「启用 Git 右键集成」）：关掉时这套一个都不挂——注册表里
// 自家那几把键（ToolsGit*）删干净、转交监听不起；开着才挂。与压缩的键互不干涉。
// ============================================================

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"Tools/services/applog"
	"Tools/services/config"
	"Tools/services/githost"
	"Tools/services/sysx"
	"Tools/tools/gitcmd"
)

// gitToolRoute Git 工作台在主窗口里的路由（app:navigate 用它把页面切过去）。
const gitToolRoute = "/tools/git"

var (
	// gitMu 保护 gitServer 与 gitPending。
	gitMu sync.Mutex
	// gitServer 本机转交监听，只在右键集成开关开着时存在；读写都受 gitMu 保护。
	gitServer *githost.Server
	// gitPending 冷启动（无在跑实例）带来的 {op,repo}：前端挂载后经 GetPendingGit 取走。
	gitPending string
)

/* ============================ 命令行分流 ============================ */

// GitCLICommand 认出右键 verb 拉起来的那条命令行。返回操作名与仓库路径；
// ok=false 表示这不是 Git 右键发起的进程，照旧走正常启动。只认 --git-* 精确标记，
// 与压缩的 --archive/--extract/--archive-panel 天然不冲突。
func GitCLICommand(args []string) (op, repo string, ok bool) {
	return gitcmd.ParseShellArgs(args)
}

// HandleGitCLI 右键拉起来的那个短进程该干的活。返回 true = 本进程应立即退出：
//   - 不在任何仓库内 → toast 说明后退出（不改菜单可见性，判断在程序内做）；
//   - 命中仓库且有实例在跑 → 转交给它（登记 + 拉前台 + 路由到 Git 页）后退出；
//
// 返回 false = 没有可转交的实例，本进程正常起 GUI，并把它记成 pending，
// 主窗口就绪后由前端取走、定位到该仓库。
func (a *App) HandleGitCLI(op, repo string) bool {
	ctx := context.Background()
	root, hit := gitcmd.ResolveRepo(ctx, repo)
	if !hit {
		a.notifyGit("这里不是 Git 仓库", "右键的位置不在任何 Git 工作树内。已登记的仓库可在「Git 管理」工具里打开。")
		return true
	}
	// 先登记进仓库表（按路径去重）：无论转交还是自开，前端列表里都已经有它。
	if _, err := gitcmd.AddRepo(ctx, root); err != nil {
		applog.Warnf("git", "登记仓库失败: %v", err)
	}
	if githost.TrySend(githost.Request{Op: op, Repo: root, From: "shell"}) {
		applog.Infof("git", "已转交给正在运行的实例: op=%s repo=%s", op, root)
		return true
	}
	applog.Infof("git", "没有可转交的实例，本进程起 GUI 并定位: op=%s repo=%s", op, root)
	a.SetPendingGit(op, root)
	// 冷启动顺带把导航也记成 Git 页：主窗口就绪后 consumePendingNavigate 路由过去。
	a.SetPendingNavigate(gitToolRoute)
	return false
}

// notifyGit 无界面分支弹系统通知。headless 进程弹完就退出，故同步等 Toast 交出去
// （与压缩 notifyArchive 同一套：不能扔个 goroutine 就走）。
func (a *App) notifyGit(title, content string) {
	if err := a.notifySync(title, clipNotice(content)); err != nil {
		applog.Debugf("git", "通知弹出失败: %v", err)
	}
}

/* ============================ 冷启动 pending ============================ */

// SetPendingGit 记下冷启动要打开的仓库与操作，等前端挂载后取。
func (a *App) SetPendingGit(op, repo string) {
	b, _ := json.Marshal(githost.Request{Op: op, Repo: repo})
	gitMu.Lock()
	gitPending = string(b)
	gitMu.Unlock()
}

// GetPendingGit 前端挂载时取走并清空冷启动定位（取一次即消费）。返回的是
// githost.Request 的 JSON；无 pending 时为空串。非桌面平台没有右键链路，恒空。
func (a *App) GetPendingGit() string {
	gitMu.Lock()
	defer gitMu.Unlock()
	p := gitPending
	gitPending = ""
	return p
}

/* ============================ 转交回调（开工作台） ============================ */

// gitHandoffOpen 把主窗口拉到前台、路由到 Git 页，再广播 git:open 带上仓库与操作。
// app:navigate 复用既有导航（layouts 里 router.push），git:open 交给 Git 页定位。
func (a *App) gitHandoffOpen(op, repo string) {
	if a.mainWindow == nil {
		return
	}
	a.showMainWindow()
	a.mainWindow.EmitEvent("app:navigate", gitToolRoute)
	a.mainWindow.EmitEvent("git:open", githost.Request{Op: op, Repo: repo})
}

/* ============================ 右键挂载（绑定方法） ============================ */

// GitShellStatus 当前挂载状态（设置页展示聚合三态；非 Windows 回 supported=false）。
func (a *App) GitShellStatus() gitcmd.GitShellInfo {
	info, err := gitcmd.GitShellStatus()
	if err != nil && !errors.Is(err, sysx.ErrUnsupported) {
		applog.Warnf("git", "读取挂载状态失败: %v", err)
	}
	return info
}

// GitShellInstall 装上全部 Git 右键 verb。幂等：已挂好的键重写成同一份内容，
// 挪过位置的旧路径顺带修好。
func (a *App) GitShellInstall() error {
	if err := gitcmd.GitShellInstall(); err != nil {
		applog.Warnf("git", "挂载右键失败: %v", err)
		return err
	}
	applog.Infof("git", "Git 右键挂载完成")
	return nil
}

// GitShellUninstall 删干净自家写过的 ToolsGit* 键，别人的键、压缩的键一律不碰。
func (a *App) GitShellUninstall() error {
	if err := gitcmd.GitShellUninstall(); err != nil {
		applog.Warnf("git", "卸载右键失败: %v", err)
		return err
	}
	applog.Infof("git", "Git 右键已卸载")
	return nil
}

/* ============================ 开关的即时生效 ============================ */

// applyGitOnStart 启动时按开关挂好：开着 → 起转交监听 + 写注册表（顺带把挪过
// 位置的旧路径修好）；没开时一个都不挂（不留监听、不碰注册表）。
func (a *App) applyGitOnStart() {
	if !config.GitShellEnabled() {
		return
	}
	a.mountGitHost()
	go a.syncGitShell()
}

// applyGitSwitch 让开关立刻生效，不等重启：开启 → 起监听 + 写注册表；关闭 →
// 停监听 + 把自家那几把键删干净。调用点负责先落盘。挂载是注册表操作，放后台跑。
func (a *App) applyGitSwitch() {
	if config.GitShellEnabled() {
		a.mountGitHost()
		go a.syncGitShell()
		return
	}
	a.unmountGitHost()
	go a.unmountGitShell()
}

// mountGitHost 起转交监听。只为桌面右键存在；已有实例占端点时 Start 失败只记日志。
func (a *App) mountGitHost() {
	if !sysx.Caps().NativeWindows {
		return
	}
	gitMu.Lock()
	running := gitServer != nil
	gitMu.Unlock()
	if running {
		return
	}
	srv, err := githost.Start(func(req githost.Request) {
		applog.Infof("git", "收到打开转交(from=%s): op=%s repo=%s", req.From, req.Op, req.Repo)
		if _, err := gitcmd.AddRepo(context.Background(), req.Repo); err != nil {
			applog.Warnf("git", "转交的仓库登记失败: %v", err)
		}
		a.gitHandoffOpen(req.Op, req.Repo)
	})
	if err != nil {
		applog.Warnf("git", "转交监听启动失败（另一个实例可能已在运行）: %v", err)
		return
	}
	gitMu.Lock()
	gitServer = srv
	gitMu.Unlock()
	applog.Infof("git", "Git 转交监听已启动")
}

// unmountGitHost 停掉转交监听并收回端点文件（幂等）。
func (a *App) unmountGitHost() {
	gitMu.Lock()
	srv := gitServer
	gitServer = nil
	gitMu.Unlock()
	if srv == nil {
		return
	}
	srv.Stop()
	applog.Infof("git", "Git 转交监听已停止")
}

// syncGitShell 把注册表与设置对齐：开着就装（幂等，顺带自修复挪过位置的旧路径），
// 关着就删。非 Windows 上 GitShellInstall 回 ErrUnsupported，只留日志。
func (a *App) syncGitShell() {
	if err := a.GitShellInstall(); err != nil && errors.Is(err, sysx.ErrUnsupported) {
		applog.Debugf("git", "当前平台没有右键挂载能力: %v", err)
	}
}

// unmountGitShell 关掉开关时清挂载：没挂过就一次都不写，免得为了删不存在的键
// 把注册表打开一圈。
func (a *App) unmountGitShell() {
	info, err := gitcmd.GitShellStatus()
	if err != nil {
		if !errors.Is(err, sysx.ErrUnsupported) {
			applog.Debugf("git", "清挂载前读状态失败: %v", err)
		}
		return
	}
	if info.Slot == gitcmd.SlotAbsent {
		return
	}
	_ = a.GitShellUninstall()
}
