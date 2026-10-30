package app

// ============================================================
// 文件压缩的宿主侧接线（右键挂载 + 进程间转交 + 压缩前选项小窗）
//
// 分工与快捷搜索同构：压缩/解压的编排都在 tools/archive（那层不认识窗口），
// 跨进程转交在 services/archivehost，本文件只做三件事——按开关挂或不挂右键与
// 监听、维护那个可复用的选项小窗、把引擎够不着的"开窗/发事件/弹通知"注回给它。
//
// 全文件不带 build tag：这些绑定方法要对前端可见，而 Wails 的方法 id 由方法名
// 决定，一旦被 tag 挡掉，Web 面板调用时就是 unknown bound method id。
// 平台专属的注册表读写留在 tools/archive/shell_windows.go，非 Windows 那边
// ShellStatus/Install/Uninstall 一律回 sysx.ErrUnsupported，这里只把它记进日志。
//
// 开关语义（设置里的「启用文件压缩右键」）：关掉时这套一个都不挂——注册表里
// 自家那几把键删干净、SendTo 快捷方式删掉、转交监听不起；开着才挂。
// ============================================================

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"Tools/services/applog"
	"Tools/services/archivehost"
	"Tools/services/sysx"
	"Tools/tools/archive"
	"Tools/tools/launcher"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	// archivePanelWinName 选项小窗窗口名，兼作单例判据（与面板窗口同一套做法）。
	archivePanelWinName = "win:archive"
	archivePanelWinW    = 620
	archivePanelWinH    = 470
	// archivePanelBlurGrace 显示后多久之内不理会"失焦"事件：与快捷搜索面板
	// 同一坑——WebView2 冷建时 Show 之后可能马上来一次 focus/blur 交替。
	archivePanelBlurGrace = 600 * time.Millisecond
)

var (
	// 压缩可以多项并发（右键多选、工具页连着发两包），故取消把手按 jobId 存，
	// 不像仿站那样只留一条。用包级变量而非 App 字段，省得动 App 结构体。
	archiveMu      sync.Mutex
	archiveCancels = map[string]context.CancelFunc{}
	// archiveServer 本机转交监听，只在总开关开着时存在。
	archiveServer *archivehost.Server
	// 选项小窗：窗口对象长期复用，archivePanelReq 是这次要压的条目。
	archivePanelWin     application.Window
	archivePanelShown   bool
	archivePanelShownAt time.Time // 最近一次点亮的时刻：给失焦自动收起留一段宽限期
	archivePanelReq     []string
	// archivePanelOnStart 命令行里带来的 --archive-panel 条目：主窗口就绪前
	// 只能先记着，ServiceStartup 里再呼出面板。
	archivePanelOnStart []string
)

/* ============================ 命令行分流 ============================ */

// ArchiveCLICommand 认出右键 verb / SendTo 拉起来的那条命令行。
// 返回操作类型与路径清单；ok=false 表示这不是压缩发起的进程，照旧走正常启动。
func ArchiveCLICommand(args []string) (op string, paths []string, ok bool) {
	for i, arg := range args {
		switch arg {
		case archive.ArgArchive:
			return archive.OpCompress, cleanPathArgs(args[i+1:]), true
		case archive.ArgExtract:
			return archive.OpExtract, cleanPathArgs(args[i+1:]), true
		}
	}
	return "", nil, false
}

// IsArchivePanelRequest 这条命令行是不是「发送到 → 压缩 (Tools)…」——
// 它要先问一下选项，不能像 verb 那样直接开工。
func IsArchivePanelRequest(args []string) bool {
	for _, arg := range args {
		if arg == archive.ArgPanel {
			return true
		}
	}
	return false
}

// ArchivePanelPathsFromArgs 小窗要预先填好的路径：--archive-panel 之后跟的那些参数。
func ArchivePanelPathsFromArgs(args []string) []string {
	for i, arg := range args {
		if arg == archive.ArgPanel {
			return cleanPathArgs(args[i+1:])
		}
	}
	return nil
}

// cleanPathArgs 丢掉空串与别的标记，只留路径并取绝对形式。
func cleanPathArgs(args []string) []string {
	out := make([]string, 0, len(args))
	for _, a := range args {
		a = strings.TrimSpace(a)
		if a == "" || strings.HasPrefix(a, "--") {
			continue
		}
		if abs, err := filepath.Abs(a); err == nil {
			a = abs
		}
		out = append(out, a)
	}
	return out
}

// RunArchiveCLI 右键拉起来的那个进程该干的活：本机有主程序在跑就转交给它
// （进度、任务中心、通知都在那边），没有就本进程压完退出。
//
// 无界面分支不建窗口、不注册热键、不起托盘、不建索引，结果只靠系统通知交代；
// 中途的进度进日志，出事时用户还能翻到。
func (a *App) RunArchiveCLI(op string, paths []string) {
	o := archive.Options{Op: op, Paths: paths}
	archive.ApplyDefaults(&o)
	if err := archive.Normalize(&o); err != nil {
		applog.Errorf("archive", "命令行发起失败: %v", err)
		a.notifyArchive(archiveFailTitle(o.Op), err.Error())
		return
	}
	if archivehost.TrySend(archivehost.Request{Options: o, From: "shell"}) {
		applog.Infof("archive", "已转交给正在运行的实例: op=%s 项=%d", o.Op, len(o.Paths))
		return
	}
	applog.Infof("archive", "没有可转交的实例，本进程直接干活: op=%s 项=%d", o.Op, len(o.Paths))

	jobID := newArchiveJobID()
	res, err := archive.Run(context.Background(), o, jobID, func(p archive.Progress) {
		if p.Msg != "" {
			applog.Debugf("archive", "[%s] %s %v/%d", p.Stage, p.Msg, p.Percent, p.Files)
		}
	})
	if err != nil {
		applog.Errorf("archive", "命令行任务失败: %v", err)
		a.notifyArchive(archiveFailTitle(o.Op), err.Error())
		return
	}
	applog.Infof("archive", "命令行任务结束: out=%s 文件=%d 字节=%d", res.Out, res.Files, res.Bytes)
	a.notifyArchive(archiveDoneTitle(o.Op), archiveResultLine(o, res))
}

// notifyArchive 无界面分支弹系统通知。
// headless 进程弹完就退出，故这里同步等 PowerShell 把 Toast 交出去，
// 不能像 ShowSystemNotification 那样扔个 goroutine 就走。
func (a *App) notifyArchive(title, content string) {
	if err := a.notifySync(title, clipNotice(content)); err != nil {
		applog.Debugf("archive", "通知弹出失败: %v", err)
	}
}

// clipNotice 通知正文限长：错误串可能带一大串路径。
func clipNotice(s string) string {
	r := []rune(s)
	if len(r) <= 120 {
		return s
	}
	return string(r[:120]) + "…"
}

// newArchiveJobID 任务号：时间戳足够，转交过来的任务在本机重号的可能性可以忽略。
func newArchiveJobID() string {
	return fmt.Sprintf("ar%d", time.Now().UnixNano())
}

func archiveDoneTitle(op string) string {
	if op == archive.OpExtract {
		return "解压完成"
	}
	return "压缩完成"
}

func archiveFailTitle(op string) string {
	if op == archive.OpExtract {
		return "解压失败"
	}
	return "压缩失败"
}

// archiveResultLine 通知正文里那一行人话。
func archiveResultLine(o archive.Options, res archive.Result) string {
	name := filepath.Base(res.Out)
	if o.Op == archive.OpExtract {
		return fmt.Sprintf("%s → %s（%d 项）", name, res.Out, res.Files)
	}
	line := fmt.Sprintf("%s（%s，%d 个文件）", name, humanBytes(res.Bytes), res.Files)
	if len(res.Skipped) > 0 {
		line += fmt.Sprintf("，跳过 %d 项", len(res.Skipped))
	}
	return line
}

/* ============================ 任务发起（绑定方法） ============================ */

// ArchiveCompress 压缩选定的条目，立即返回 jobId；进度走 archive:progress 事件。
func (a *App) ArchiveCompress(opts archive.Options) (string, error) {
	opts.Op = archive.OpCompress
	return a.startArchiveJob(opts)
}

// ArchiveExtract 解包，dest 为空时解到归档同级的同名文件夹。
func (a *App) ArchiveExtract(opts archive.Options) (string, error) {
	opts.Op = archive.OpExtract
	return a.startArchiveJob(opts)
}

// ArchiveCancel 取消一个在跑的任务；jobId 空时取消全部（面板上的"全部停止"）。
//
// 第一个返回值是「有没有真的动到一个在跑的任务」：false 表示这一笔其实早就结束了。
// 前端必须拿它把"进行中"收回去——解压几个小文件这种毫秒级任务，终态事件常常跑在
// 绑定方法的返回值之前，界面一旦错过就没有任何机会自己结束；而取消一个已结束的任务
// 按定义不会再产生事件，不把这个信息递回去，那个按钮就永远停在"解压中…"。
func (a *App) ArchiveCancel(jobID string) (bool, error) {
	archiveMu.Lock()
	var targets []context.CancelFunc
	if strings.TrimSpace(jobID) == "" {
		for _, cancel := range archiveCancels {
			targets = append(targets, cancel)
		}
	} else if cancel, ok := archiveCancels[jobID]; ok {
		targets = append(targets, cancel)
	}
	archiveMu.Unlock()
	for _, cancel := range targets {
		cancel()
	}
	if len(targets) == 0 {
		applog.Debugf("archive", "取消：任务 %s 已不在跑", jobID)
		return false, nil
	}
	return true, nil
}

// ArchiveSuggestName 按命名规则预览成品路径（前端改名框旁边的提示、
// 选项小窗的默认值都取它）。只算不建，用户还没点开工。
func (a *App) ArchiveSuggestName(opts archive.Options) (string, error) {
	if opts.Op == "" {
		opts.Op = archive.OpCompress
	}
	archive.ApplyDefaults(&opts)
	if err := archive.Normalize(&opts); err != nil {
		return "", err
	}
	return archive.SuggestOut(opts)
}

// ArchiveScan 预扫描选中条目的文件数与总字节，供前端在开工前提示"这一下有多大"。
func (a *App) ArchiveScan(paths []string) archive.Preflight {
	return archive.PreflightOf(paths)
}

// ArchiveChooseFiles 多选要打包的文件（目录靠 ChooseDir，拖放走原生 drop）。
// 取消当成"没选"处理：返回空列表而不报错，与仓库里其他对话框一致。
func (a *App) ArchiveChooseFiles() ([]string, error) {
	if a.app == nil {
		return nil, fmt.Errorf("应用尚未初始化")
	}
	picked, err := a.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		SetTitle("选择要压缩的文件").
		PromptForMultipleSelection()
	if err != nil {
		if isDialogCanceled(err) {
			return nil, nil
		}
		return nil, err
	}
	return picked, nil
}

// startArchiveJob 起一笔任务：校验、发号、后台跑、进度转事件。
//
// 校验不过就直接返回错误——前端与转交两条路都靠这一点把"没选东西""格式不认识"
// 这类问题当场说清楚，而不是起个任务再让它立刻失败。
func (a *App) startArchiveJob(o archive.Options) (string, error) {
	archive.ApplyDefaults(&o)
	if err := archive.Normalize(&o); err != nil {
		return "", err
	}

	ctx, cancel := context.WithCancel(context.Background())
	jobID := newArchiveJobID()
	archiveMu.Lock()
	archiveCancels[jobID] = cancel
	archiveMu.Unlock()

	applog.Infof("archive", "开始任务 %s: op=%s 项=%d 格式=%s 级别=%d",
		jobID, o.Op, len(o.Paths), o.Format, o.Level)
	go func() {
		defer func() {
			archiveMu.Lock()
			delete(archiveCancels, jobID)
			archiveMu.Unlock()
		}()
		// Run 自己会送 done / error / canceled 三种终态事件，这里不再补发，
		// 否则任务中心会看到同一个终态两次。
		res, err := archive.Run(ctx, o, jobID, func(p archive.Progress) { a.emitArchive(p) })
		if err != nil {
			applog.Errorf("archive", "任务 %s 失败: %v", jobID, err)
			return
		}
		applog.Infof("archive", "任务 %s 结束: out=%s 文件=%d 字节=%d", jobID, res.Out, res.Files, res.Bytes)
	}()
	return jobID, nil
}

// emitArchive 把进度推给前端（app 未就绪时忽略）。
//
// 事件同时被 main 里的 emit 代理喂给任务中心记账，所以右键发起、用户早离开
// 页面的那一批也能在任务列表里回看。
func (a *App) emitArchive(p archive.Progress) {
	if a.app != nil {
		a.app.Event.Emit("archive:progress", p)
	}
}

/* ============================ 右键挂载（绑定方法） ============================ */

// ArchiveShellStatus 当前挂载状态（设置页展示三态；非 Windows 回 supported=false）。
func (a *App) ArchiveShellStatus() archive.ShellInfo {
	info, err := archive.ShellStatus()
	if err != nil {
		if !errors.Is(err, sysx.ErrUnsupported) {
			applog.Warnf("archive", "读取挂载状态失败: %v", err)
		}
		return info
	}
	return info
}

// ArchiveShellInstall 按当前设置装上右键（SendTo 那条独立开关决定给不给）。
// 幂等：已经挂好的键重写成同一份内容，不会留下半截状态。
func (a *App) ArchiveShellInstall() error {
	if err := archive.ShellInstall(archive.SendToWanted()); err != nil {
		applog.Warnf("archive", "挂载右键失败: %v", err)
		return err
	}
	applog.Infof("archive", "右键挂载完成: sendTo=%v", archive.SendToWanted())
	return nil
}

// ArchiveShellUninstall 删干净自家写过的键与快捷方式，别人的键一律不碰。
func (a *App) ArchiveShellUninstall() error {
	if err := archive.ShellUninstall(); err != nil {
		applog.Warnf("archive", "卸载右键失败: %v", err)
		return err
	}
	applog.Infof("archive", "右键已卸载")
	return nil
}

/* ============================ 开关的即时生效 ============================ */

// SetArchiveArgs 主入口把 os.Args 交进来。
//
// --archive-panel 的语义是“正常起 GUI，主窗口就绪后开选项小窗”，而
// ServiceStartup 拿不到命令行，只能在这一刻递一手。
func (a *App) SetArchiveArgs(args []string) {
	archiveMu.Lock()
	archivePanelOnStart = ArchivePanelPathsFromArgs(args)
	archiveMu.Unlock()
}

// applyArchiveOnStart 启动时把这套东西按开关挂好：
//
//   - 开着 → 起转交监听 + 写注册表（这次启动顺带把挪过位置的旧路径修好）
//   - 带着 --archive-panel 来的 → 呼出选项小窗（开关关着就不建这个窗）
//
// 没开时一个都不挂：不留监听、不碰注册表、不建面板窗。
func (a *App) applyArchiveOnStart() {
	if !archive.Enabled() {
		return
	}
	a.mountArchiveHost()
	go a.syncArchiveShell()

	archiveMu.Lock()
	paths := archivePanelOnStart
	archivePanelOnStart = nil
	archiveMu.Unlock()
	if len(paths) == 0 {
		return
	}
	if err := a.ArchivePanelOpen(paths); err != nil {
		applog.Debugf("archive", "启动时呼出压缩面板未成功: %v", err)
	}
}

// applyArchiveSwitch 让开关立刻生效，不等重启：
//
//	开启 → 起本机转交监听 + 写注册表与 SendTo（挪过位置的失配一并修好）
//	关闭 → 停监听 + 把自家那几把键和快捷方式删干净
//
// 调用点负责先落盘再调它，这里只读当前配置。挂载是注册表操作，几毫秒到
// 几十毫秒，放后台跑别压在这次"保存设置"的返回里。
func (a *App) applyArchiveSwitch() {
	if archive.Enabled() {
		a.mountArchiveHost()
		go a.syncArchiveShell()
		return
	}
	a.unmountArchiveHost()
	go a.unmountArchiveShell()
}

// mountArchiveHost 起转交监听。
//
// 转交只为桌面右键存在：无头服务端既没有资源管理器也没有 verb，起监听只是
// 白占一个回环端口与数据目录里的一个文件。
//
// 已经有实例占着端点时 Start 会失败，只记日志不报错：两个 GUI 并存是既有行为，
// 谁先起来谁接右键，后起来的那个接不到而已。
func (a *App) mountArchiveHost() {
	if !sysx.Caps().NativeWindows {
		return
	}
	archiveMu.Lock()
	running := archiveServer != nil
	archiveMu.Unlock()
	if running {
		return
	}
	srv, err := archivehost.Start(func(req archivehost.Request) {
		applog.Infof("archive", "收到转交任务(from=%s): op=%s 项=%d", req.From, req.Op, len(req.Paths))
		if _, err := a.startArchiveJob(req.Options); err != nil {
			applog.Warnf("archive", "转交的任务没起来: %v", err)
		}
	})
	if err != nil {
		applog.Warnf("archive", "转交监听启动失败（另一个实例可能已在运行）: %v", err)
		return
	}
	archiveMu.Lock()
	archiveServer = srv
	archiveMu.Unlock()
	applog.Infof("archive", "转交监听已启动")
}

// unmountArchiveHost 停掉转交监听并收回端点文件（幂等）。
func (a *App) unmountArchiveHost() {
	archiveMu.Lock()
	srv := archiveServer
	archiveServer = nil
	archiveMu.Unlock()
	if srv == nil {
		return
	}
	srv.Stop()
	applog.Infof("archive", "转交监听已停止")
}

// syncArchiveShell 把注册表与设置对齐：开着就装（幂等，顺带自修复挪过位置的
// 旧路径），关着就删。非 Windows 上 ShellInstall 回 ErrUnsupported，只留日志。
func (a *App) syncArchiveShell() {
	if err := a.ArchiveShellInstall(); err != nil && errors.Is(err, sysx.ErrUnsupported) {
		applog.Debugf("archive", "当前平台没有右键挂载能力: %v", err)
	}
}

// unmountArchiveShell 关掉开关时清挂载：没挂过就一次都不写，
// 免得为了删不存在的键把注册表打开一圈。
func (a *App) unmountArchiveShell() {
	info, err := archive.ShellStatus()
	if err != nil {
		if !errors.Is(err, sysx.ErrUnsupported) {
			applog.Debugf("archive", "清挂载前读状态失败: %v", err)
		}
		return
	}
	if info.Compress == archive.SlotAbsent && info.Extract == archive.SlotAbsent && info.SendTo == archive.SlotAbsent {
		return
	}
	_ = a.ArchiveShellUninstall()
}

/* ============================ 压缩前的选项小窗 ============================ */

// ArchivePanelOpen 呼出选项小窗（SendTo 那条 --archive-panel 走这里，工具页也能调）。
// paths 是已经选好的条目，窗口打开就直接填进列表。
func (a *App) ArchivePanelOpen(paths []string) error {
	if !sysx.Caps().NativeWindows || a.app == nil {
		return fmt.Errorf("%w：当前运行模式没有原生窗口，压缩选项请在工具页里改", sysx.ErrUnsupported)
	}
	// 建窗与点亮都不在主线程上直接做：调用点可能是热键线程，也可能是
	// ServiceStartup（见 launcher.go 同段说明）。
	go func() {
		if err := a.openArchivePanel(paths); err != nil {
			applog.Warnf("archive", "打开压缩选项面板失败: %v", err)
		}
	}()
	return nil
}

// ArchivePanelHide 前端按 Esc / 开工之后收起面板（窗口保留复用）。
func (a *App) ArchivePanelHide() {
	archiveMu.Lock()
	win := archivePanelWin
	archivePanelShown = false
	archiveMu.Unlock()
	if win == nil {
		return
	}
	application.InvokeAsync(func() { win.Hide() })
}

// ArchivePanelPending 面板页挂载时取自带的条目：冷建时 show 事件可能比页面
// 就绪更早，页面靠这个兜底拿一次。
func (a *App) ArchivePanelPending() []string {
	archiveMu.Lock()
	defer archiveMu.Unlock()
	return append([]string(nil), archivePanelReq...)
}

// openArchivePanel 建窗（复用已有）+ 点亮 + 把条目交给页面。
func (a *App) openArchivePanel(paths []string) error {
	archiveMu.Lock()
	archivePanelReq = append([]string(nil), paths...)
	archiveMu.Unlock()

	win, err := a.ensureArchivePanelWindow()
	if err != nil {
		return err
	}
	x, y := archivePanelGeometry()
	application.InvokeAsync(func() {
		win.SetPosition(x, y)
		win.Show()
		if err := launcher.BringWindow(uintptr(win.NativeWindow())); err != nil {
			applog.Debugf("archive", "面板置顶失败: %v", err)
		}
		archiveMu.Lock()
		archivePanelShown = true
		archivePanelShownAt = time.Now()
		archiveMu.Unlock()
		// 复用已有窗口时页面不会重新 mount，靠事件通知它换一批条目
		win.EmitEvent("archive:show", paths)
	})
	return nil
}

// ensureArchivePanelWindow 拿到面板窗口，没有就建一个。
func (a *App) ensureArchivePanelWindow() (application.Window, error) {
	archiveMu.Lock()
	if archivePanelWin != nil {
		win := archivePanelWin
		archiveMu.Unlock()
		return win, nil
	}
	archiveMu.Unlock()

	if a.app == nil {
		return nil, fmt.Errorf("应用尚未初始化")
	}
	x, y := archivePanelGeometry()
	win := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             archivePanelWinName,
		Title:            "压缩选项",
		X:                x,
		Y:                y,
		Width:            archivePanelWinW,
		Height:           archivePanelWinH,
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		Hidden:           true, // 由 openArchivePanel 点亮，创建即隐藏
		StartState:       application.WindowStateNormal,
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/#/widget/archive",
		Windows: application.WindowsWindow{
			// 一次性交互面板，不在任务栏占位
			HiddenOnTaskbar: true,
			// 必须关掉无边框装饰，否则 DWM 的玻璃底板会透过透明 WebView
			// 在卡片外面铺一层灰（原因见 launcher.go 同处注释）。
			DisableFramelessWindowDecorations: true,
		},
	})
	if win == nil {
		return nil, fmt.Errorf("面板窗口创建失败")
	}
	archiveMu.Lock()
	archivePanelWin = win
	archiveMu.Unlock()
	// 失焦即收起：这是这类面板的基本礼仪——用户点了别处就是不想压了。
	win.RegisterHook(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		archiveMu.Lock()
		shown := archivePanelShown && time.Since(archivePanelShownAt) > archivePanelBlurGrace
		archiveMu.Unlock()
		if shown {
			a.ArchivePanelHide()
		}
	})
	applog.Infof("archive", "压缩选项面板已创建 %dx%d @%d,%d", archivePanelWinW, archivePanelWinH, x, y)
	return win, nil
}

// archivePanelGeometry 面板位置：水平居中、纵向落在屏幕上部（与快捷搜索同一条
// 视觉基线，用户按下右键后眼睛本来就在那一带）。
func archivePanelGeometry() (int, int) {
	l, t, r, b := workAreaDIP()
	w, h := archivePanelWinW, archivePanelWinH
	if r-l < w {
		w = r - l
	}
	if b-t < h {
		h = b - t
	}
	return l + (r-l-w)/2, t + (b-t-h)/3
}

/* ============================ 人类可读的体积 ============================ */

// humanBytes 把字节数说成人话（通知正文与进度提示共用）。
func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
