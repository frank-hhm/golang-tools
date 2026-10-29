package app

// 快捷搜索启动器（Alt+Space 面板）的宿主侧接线。
//
// 分工：检索/打分/执行动作都在 tools/launcher 里（那层不认识窗口），本文件只做
// 三件事——按开关挂或不挂服务、维护那个单例无边框面板窗口、把引擎够不着的
// "开窗/发事件/锁屏/退出" 能力注回给它。
//
// 全文件不带 build tag：这里的 LauncherSearch 等方法要对前端可见，而 Wails 的方法 id
// 由方法名决定，一旦被 tag 挡掉，Linux 面板调用时就是 unknown bound method id。
// 平台专属的那一小块（锁工作站）才拆去 launcher_windows.go / launcher_other.go。
//
// 开关语义（设置里的「启用快捷搜索」）：关掉时这套服务一个都不挂——
// Hotkeys() 不交出这条热键（重载后原注册被注销）、面板窗口销毁、索引丢弃不再扫盘。

import (
	"fmt"
	"time"

	"Tools/services/applog"
	"Tools/services/config"
	"Tools/services/sysx"
	"Tools/tools/launcher"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	// launcherWinName 面板窗口名，兼作单例判据（与截图浮层同一套做法）。
	launcherWinName = "win:launcher"
	launcherWinW    = 720
	launcherWinH    = 460
	// launcherTopRatio 面板在 work area 里的纵向位置：贴 PowerToys Run 的习惯，
	// 落在屏幕上三分之一处，比垂直居中更顺手（鼠标原地就能点到第一条结果）。
	launcherTopRatio = 0.18
	// launcherBlurGrace 显示后多久之内不理会"失焦"事件。
	// WebView2 冷建时 Show 之后可能马上来一次 focus/blur 交替，没有这道闸门的话
	// 面板会在用户眼前闪一下就没了——那种故障比"点了没反应"更难排查。
	launcherBlurGrace = 600 * time.Millisecond
)

// launcherMounted 这套服务在当前构建上到底能不能挂、该不该挂。
//
// 两个条件缺一不可：设置里的总开关（用户意愿），以及当前构建能开原生窗口
// （-tags server 的 Web 面板里 Wails 的 Window 是空实现，NewWithOptions 会"成功"
// 却永远不出现一个窗口；Alt+Space 挂在那儿只会白白占用系统热键）。
func (a *App) launcherMounted() bool {
	return launcher.Enabled() && sysx.Caps().NativeWindows && a.app != nil
}

// PrewarmLauncher 预建隐藏的面板窗口，把"新建 WebView2 窗口 + 页面 bootstrap"
// 这笔几百毫秒的开销从热键按下的那一下挪到启动空闲期。
// 窗口是真隐藏创建的（不叠加任何 StartState），不会挡桌面点击，也不会被窗口枚举
// 收进搜索结果（建好后立刻 ExcludeWindow 报了句柄）。
func (a *App) PrewarmLauncher() {
	if !a.launcherMounted() {
		return
	}
	if _, err := a.ensureLauncherWindow(); err != nil {
		applog.Warnf("launcher", "预热面板窗口失败: %v", err)
	}
}

// ensureLauncherWindow 拿到面板窗口，没有就建一个（建窗串行化：
// 启动预热与热键调起可能撞在一起）。
func (a *App) ensureLauncherWindow() (application.Window, error) {
	a.launcherMu.Lock()
	defer a.launcherMu.Unlock()
	if a.launcherWin != nil {
		return a.launcherWin, nil
	}
	if a.app == nil {
		return nil, fmt.Errorf("应用尚未初始化")
	}
	x, y, w, h := launcherPanelGeometry()
	win := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             launcherWinName,
		Title:            "快捷搜索",
		X:                x,
		Y:                y,
		Width:            w,
		Height:           h,
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		Hidden:           true, // 由 ToggleLauncher 点亮，创建即隐藏（见 capture.go 同段说明）
		StartState:       application.WindowStateNormal,
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		URL:              "/#/widget/launcher",
		Windows: application.WindowsWindow{
			// 一次性交互面板，不在任务栏占位
			HiddenOnTaskbar: true,
			// 必须关掉无边框装饰。Frameless 且保留装饰时，Wails 会 DwmExtendFrameIntoClientArea
			// 把窗口框架铺进客户区来换 Win11 圆角与投影，那块 DWM 玻璃底板会透过透明
			// WebView 显示成一层灰纱：卡片以外的整片窗口区域全被染暗，看着像页面自己
			// 铺了个底色。卡片的圆角与投影本来就不靠窗口这一层，交给 CSS 画。
			DisableFramelessWindowDecorations: true,
		},
	})
	if win == nil {
		return nil, fmt.Errorf("面板窗口创建失败")
	}
	a.launcherWin = win
	// 把自己从"运行中的窗口"检索结果里摘掉：否则搜"快捷"永远能搜到面板自己。
	if hwnd := launcherWindowHWND(win); hwnd != 0 {
		launcher.ExcludeWindow(hwnd)
	}
	// 失焦即收起：这是这类面板的基本礼仪——用户点了别处就是不想搜了。
	// 只认点亮超过 launcherBlurGrace 之后的第一次失焦，避免冷建时的 focus/blur 交替
	// 把刚出来的面板当场收掉。
	win.RegisterHook(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		a.launcherMu.Lock()
		ok := a.launcherShown && time.Since(a.launcherShownAt) > launcherBlurGrace
		a.launcherMu.Unlock()
		if ok {
			a.hideLauncher()
		}
	})
	applog.Infof("launcher", "面板窗口已创建（隐藏）%dx%d @%d,%d", w, h, x, y)
	return win, nil
}

// launcherPanelGeometry 面板的位置与尺寸：水平居中、纵向落在屏幕上部。
// 每次点亮都重算一遍，用户拖到另一台显示器/改了 DPI 也不会开到屏幕外。
func launcherPanelGeometry() (x, y, w, h int) {
	w, h = launcherWinW, launcherWinH
	l, t, r, b := workAreaDIP()
	if r-l < w {
		w = r - l
		x = l
	} else {
		x = l + (r-l-w)/2
	}
	if b-t < h {
		h = b - t
		y = t
	} else {
		y = t + int(float64(b-t)*launcherTopRatio)
	}
	return x, y, w, h
}

// launcherWindowHWND 取面板窗口的 Win32 句柄；非 Windows 或取不到时为 0。
func launcherWindowHWND(win application.Window) uintptr {
	if win == nil {
		return 0
	}
	return uintptr(win.NativeWindow())
}

// LauncherToggle 呼出或收起搜索面板（全局热键走这里，前端也能调）。
// 绑定方法，不带 build tag：Web 面板里它返回错误而不是消失。
func (a *App) LauncherToggle() error {
	return a.toggleLauncher()
}

func (a *App) toggleLauncher() error {
	if !a.launcherMounted() {
		if !launcher.Enabled() {
			return fmt.Errorf("快捷搜索未启用，请先在设置里打开")
		}
		return fmt.Errorf("%w：当前运行模式没有原生窗口，快捷搜索只在桌面版可用", sysx.ErrUnsupported)
	}
	win, err := a.ensureLauncherWindow()
	if err != nil {
		return err
	}
	a.launcherMu.Lock()
	hide := a.launcherShown
	a.launcherMu.Unlock()
	if hide {
		a.hideLauncher()
		return nil
	}
	x, y, _, _ := launcherPanelGeometry()
	// Show/SetPosition 必须回主线程：热键回调发生在热键线程上。
	application.InvokeAsync(func() {
		win.SetPosition(x, y)
		win.Show()
		// 聚焦不走 win.Focus()：它底下是 chromium 的 MoveFocus，失败会 os.Exit(1)
		// 把整个程序带走（本仓库踩过）。这里只要窗口到前台，输入框焦点交给页面自己。
		if err := launcher.BringWindow(launcherWindowHWND(win)); err != nil {
			applog.Debugf("launcher", "面板置顶失败: %v", err)
		}
		a.launcherMu.Lock()
		a.launcherShown = true
		a.launcherShownAt = time.Now()
		a.launcherMu.Unlock()
		// 复用已有窗口时页面不会重新 mount，靠这个事件通知前端清空输入框并聚焦
		win.EmitEvent("launcher:show")
	})
	return nil
}

// hideLauncher 收起面板（窗口对象保留复用，不销毁）。
func (a *App) hideLauncher() {
	a.launcherMu.Lock()
	win := a.launcherWin
	a.launcherShown = false
	a.launcherMu.Unlock()
	if win == nil {
		return
	}
	application.InvokeAsync(func() { win.Hide() })
}

// LauncherHide 前端按 Esc / 执行完一条结果后收起面板。
func (a *App) LauncherHide() { a.hideLauncher() }

// closeLauncherWindow 销毁面板窗口：只在设置里关掉开关时走这里。
// 只 Hide 不算“没挂载”——窗口照样占一个 WebView2 实例，用户明确要求
// 没开启就不要有这套东西。
func (a *App) closeLauncherWindow() {
	a.launcherMu.Lock()
	win := a.launcherWin
	a.launcherWin = nil
	a.launcherShown = false
	a.launcherMu.Unlock()
	if win == nil {
		return
	}
	launcher.ExcludeWindow(0)
	application.InvokeAsync(func() { win.Close() })
	applog.Infof("launcher", "快捷搜索已关闭，面板窗口销毁")
}

/* ============================ 引擎需要的宿主能力 ============================ */

// installLauncherHooks 把"只有宿主做得动"的两件事注回引擎：执行工具/命令、
// 索引建完后的广播。这两个钩子本身不扫盘不开窗，装上当摆设也不产生开销。
func (a *App) installLauncherHooks() {
	launcher.SetHostRunner(a.runLauncherHostItem)
	launcher.SetIndexNotifier(func(count int) {
		if a.app == nil {
			return
		}
		// 广播而非单发：开着面板的搜索结果要重算，设置页的索引状态也要跟着刷新
		a.app.Event.Emit("launcher:index", count)
	})
}

// runLauncherHostItem 执行需要宿主能力的条目：内置工具、内置命令、算式结果。
func (a *App) runLauncherHostItem(kind, id string) error {
	switch kind {
	case launcher.TypeTool:
		return a.launchToolByID(id)
	case launcher.TypeCommand:
		return a.runLauncherCommand(id)
	case launcher.TypeCalc:
		// 复制动作由前端 navigator.clipboard 完成（本仓库没有 Go 侧文本剪贴板 helper，
		// 为这一条再引一个依赖不值）。返回 nil 表示"执行成功"，引擎据此累加使用频次。
		return nil
	}
	return launcher.ErrNoRunner
}

// launchToolByID 按工具标识触发内置工具，与全局热键、浮动球菜单共用一段逻辑。
func (a *App) launchToolByID(id string) error {
	for _, t := range config.CurrentConfig().Tools {
		if t.ID == id || t.Key == id {
			a.dispatchToolID(id)
			return nil
		}
	}
	return fmt.Errorf("工具 %q 已不在设置的工具列表里", id)
}

// runLauncherCommand 执行引擎给出的内置命令。
// 命令清单在 tools/launcher/commands.go，这里负责它承诺的每一件事——
// 加一条命令却忘了在这里落地，前端只会看到一条点了没结果的结果。
func (a *App) runLauncherCommand(id string) error {
	switch id {
	case "open-settings":
		a.openSettingsFromTray()
	case "open-home":
		a.showMainWindow()
		if a.mainWindow != nil {
			a.mainWindow.EmitEvent("home:open")
		}
	case "toggle-ball":
		return a.toggleBallFromLauncher()
	case "open-config-dir":
		dir, err := config.ConfigDir()
		if err != nil {
			return err
		}
		return sysx.Open().Dir(dir)
	case "open-log-dir":
		return a.OpenLogDir()
	case "rebuild-index":
		// 扫盘要几秒，不能把面板卡在这儿：后台跑，跑完靠 launcher:index 事件让
		// 面板重查一次、设置页刷新条数。
		go launcher.RebuildIndex()
	case "lock-workstation":
		return lockWorkstation()
	case "quit":
		a.hideLauncher()
		a.RealQuit()
	default:
		return fmt.Errorf("未知命令 %q", id)
	}
	return nil
}

// toggleBallFromLauncher 命令面板里的"显示/隐藏浮动球"。
// 开关状态同时写回配置：球是设置页管着的显隐项，只改运行态不改配置的话，
// 下次保存设置会把它按旧值拨回去。
func (a *App) toggleBallFromLauncher() error {
	want := !config.CurrentConfig().Ball.Enabled
	if _, err := config.Update(func(cfg *config.AppConfig) { cfg.Ball.Enabled = want }); err != nil {
		return err
	}
	if !want {
		a.BallClose()
		return nil
	}
	return a.BallOpen()
}

/* ============================ 绑定方法（前端可见） ============================ */

// LauncherSearch 按当前设置的范围检索一次。空查询返回空列表（前端自己展示提示语）。
func (a *App) LauncherSearch(q string) launcher.Result {
	if !a.launcherMounted() {
		return launcher.Result{}
	}
	return launcher.Search(q, launcher.ScopesFromConfig())
}

// LauncherExecute 执行一条结果。
// 参数拆成类型 + ID 而不是整个 Item：绑定层传结构体要前后端都有一份模型，
// 而引擎真正需要的也只有这两个字段。
func (a *App) LauncherExecute(typ string, id string) error {
	if typ == "" || id == "" {
		return fmt.Errorf("结果不完整，无法执行")
	}
	if !a.launcherMounted() {
		return fmt.Errorf("快捷搜索未启用")
	}
	return launcher.Execute(launcher.Item{Type: typ, ID: id})
}

// LauncherOpenLocation 在资源管理器里定位这条结果背后的文件（Alt+Enter）。
// 窗口类结果的 exe 路径放在 Sub 里，故一并传进来。
func (a *App) LauncherOpenLocation(typ string, id string, sub string) error {
	if !a.launcherMounted() {
		return fmt.Errorf("快捷搜索未启用")
	}
	return launcher.OpenLocation(launcher.Item{Type: typ, ID: id, Sub: sub})
}

// LauncherIndexStatus 文件索引状态（设置页展示条数/是否不全/上次构建时间）。
// 未开启时也照常返回：设置页要在关闭状态下仍能讲清"现在没有索引"。
func (a *App) LauncherIndexStatus() launcher.IndexInfo {
	return launcher.GetIndexInfo()
}

// LauncherRebuildIndex 同步重建一次索引，前端转圈等它（首次扫盘可能要几秒）。
func (a *App) LauncherRebuildIndex() error {
	if !launcher.Enabled() {
		return fmt.Errorf("快捷搜索未启用")
	}
	launcher.RebuildIndex()
	return nil
}

// LauncherDefaultDirs 平台已知目录里真实存在的那些（设置页展示"默认已纳入"）。
func (a *App) LauncherDefaultDirs() []string { return launcher.DefaultDirs() }

// LauncherHomeDir 用户主目录，设置页"追加目录"输入框的默认起点。
func (a *App) LauncherHomeDir() string { return launcher.HomeDir() }

// LauncherDirExists 校验一个目录能不能纳入索引。
func (a *App) LauncherDirExists(p string) bool { return launcher.DirExists(p) }

// LauncherCommands 当前可搜的命令清单（设置页"能搜什么"的帮助文案）。
func (a *App) LauncherCommands() []launcher.Command { return launcher.ListCommands() }

// LauncherUsageReset 清空使用频次：排序被历史点击带偏时给一个退路。
func (a *App) LauncherUsageReset() error { return launcher.UsageReset() }

/* ============================ 开关的即时生效 ============================ */

// applyLauncherSwitch 保存设置后让开关立刻生效，不等重启：
//
//	开启 → 预热面板窗口 + 起首轮索引（热键那条由 Hotkeys() 在下一次重载时交出）
//	关闭 → 销毁面板窗口 + 丢掉索引（热键同样在 ReloadHotkeys 时被注销）
//
// 调用点负责先落盘再调它，这里只读当前配置。
func (a *App) applyLauncherSwitch() {
	if a.launcherMounted() {
		go func() {
			// 建窗与扫盘都不该压在这次"保存设置"的返回里
			a.PrewarmLauncher()
			launcher.EnsureIndex()
		}()
		return
	}
	a.closeLauncherWindow()
	launcher.DropIndex()
}
