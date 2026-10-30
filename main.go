//go:build windows && !server

package main

import (
	"embed"
	"encoding/json"
	"log"
	"os"
	"strings"
	"time"

	appsvc "Tools/services/app"
	"Tools/services/applog"
	"Tools/services/config"
	"Tools/services/tasks"
	"Tools/tools/archive"
	"Tools/tools/docker"
	"Tools/tools/downloader"
	"Tools/tools/fileview"
	"Tools/tools/ftp"
	"Tools/tools/guard"
	"Tools/tools/mysql"
	"Tools/tools/nginx"
	"Tools/tools/npm"
	"Tools/tools/php"
	"Tools/tools/redis"
	"Tools/tools/screenshot"
	"Tools/tools/siteclone"
	"Tools/tools/sniffer"
	"Tools/tools/srvstate"
	"Tools/tools/term"
	"Tools/tools/usbserial"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

// 默认应用 logo
//
//go:embed logo.png
var defaultLogoPNG []byte

// 托盘图标（Windows 支持 PNG/ICO 字节）
//
//go:embed build/windows/icon.ico
var trayIconData []byte

func init() {
	// Register a custom event whose associated data type is string.
	// This is not required, but the binding generator will pick up registered events
	// and provide a strongly typed JS/TS API for them.
	application.RegisterEvent[string]("time")
	application.RegisterEvent[sniffer.Flow]("sniffer:flow")
	application.RegisterEvent[usbserial.SerialData]("serial:data")
	application.RegisterEvent[usbserial.ConnStatusEvent]("serial:status")
	application.RegisterEvent[redis.MonitorLine]("redis:monitor")
	application.RegisterEvent[redis.InstallProgress]("redis:install")
	application.RegisterEvent[redis.ServerLogLine]("redis:serverlog")
	application.RegisterEvent[string]("video:saved")
	application.RegisterEvent[string]("video:cmd")
	application.RegisterEvent[appsvc.VideoStatus]("video:status")
	application.RegisterEvent[appsvc.UIMessage]("ui:message")
	application.RegisterEvent[string]("ball:action")
	application.RegisterEvent[string]("tool:activate")
	// 复用浮层时把新冻结帧直接随事件送出，免得页面再回 Go 取一次（帧体几百 KB）
	application.RegisterEvent[*screenshot.CaptureFrame]("capture:newframe")
	application.RegisterEvent[mysql.InstallProgress]("mysql:install")
	application.RegisterEvent[php.InstallProgress]("php:install")
	application.RegisterEvent[nginx.InstallProgress]("nginx:install")
	application.RegisterEvent[npm.InstallProgress]("npm:install")
	application.RegisterEvent[npm.InstallProgress]("npm:npmver")
	application.RegisterEvent[docker.PullProgress]("docker:pull")
	application.RegisterEvent[docker.ContainerLogLine]("docker:serverlog")
	application.RegisterEvent[docker.SetupProgress]("docker:setup")
	// 终端会话：输出流与断开通知
	application.RegisterEvent[term.DataEvent]("term:data")
	application.RegisterEvent[term.ExitEvent]("term:exit")
	// 文件传输：任务进度、远端断线，以及服务端状态/日志
	application.RegisterEvent[ftp.Transfer]("ftp:transfer")
	application.RegisterEvent[ftp.SessionEvent]("ftp:session")
	application.RegisterEvent[ftp.ServerStatus]("ftp:server")
	application.RegisterEvent[ftp.ServerLog]("ftp:serverlog")
	// 统一的安装/下载任务增量：前端只认这一种形状
	application.RegisterEvent[tasks.Task](tasks.UpdateEvent)
	// 仿站工具：扒站/整站下载与离线改写的进度推送
	application.RegisterEvent[siteclone.Progress]("siteclone:progress")
	// 文件压缩：压缩/解压进度（右键发起时用户不在页面上，靠任务中心回看）
	application.RegisterEvent[archive.Progress]("archive:progress")
	// 下载管理：分块/续传/队列的进度与终态（负载 downloader.Task）
	application.RegisterEvent[downloader.Task]("download:progress")
	// 进程守护：守护列表与运行态（启动/退出/重启）的实时推送
	application.RegisterEvent[guard.Snapshot]("guard:status")
}

func main() {
	checkAssets(assets)

	// 加载全局配置（用户配置目录 config.json）并初始化可配置文件日志
	startupCfg := config.LoadAppConfig()
	logDir := applog.Init(config.ApplogConfig(startupCfg.Log))
	applog.Infof("app", "=== Tools 启动 === 日志目录: %s (enabled=%v level=%s)", logDir, startupCfg.Log.Enabled, startupCfg.Log.Level)

	// 应用 MySQL 工具级设置（SQL 执行日志开关与目录）
	mysqlSettings := startupCfg.ToolSettings["mysql"]
	sqlLogDir := mysql.ConfigureSQLLog(mysqlSettings.SQLLogEnabled, mysqlSettings.SQLLogDir)
	applog.Infof("mysql", "SQL 日志: enabled=%v dir=%s", mysqlSettings.SQLLogEnabled, sqlLogDir)

	// Create an instance of the app structure
	appInstance := appsvc.NewApp()

	// 右键 verb 与「发送到」拉起来的命令行分流（方案见 docs/plan/计划-文件压缩右键集成）：
	//   --archive / --extract → 本机有实例在跑就转交，没有就本进程压完退出（不建任何窗口）
	//   --archive-panel       → 记下来，正常起 GUI，主窗口就绪后开选项小窗
	// 只认这三个显式标记，普通双击启动零影响 —— 因此不必开 SingleInstance，
	// wails3 dev 与安装实例照旧能并存。
	if op, paths, ok := appsvc.ArchiveCLICommand(os.Args); ok {
		appInstance.RunArchiveCLI(op, paths)
		os.Exit(0)
	}
	if appsvc.IsArchivePanelRequest(os.Args) {
		appInstance.SetArchiveArgs(os.Args)
	}

	// Create a new Wails application by providing the necessary options.
	// Variables 'Name' and 'Description' are for application metadata.
	// 'Assets' configures the asset server with the 'FS' variable pointing to the frontend files.
	// 'Bind' is a list of Go struct instances. The frontend has access to the methods of these instances.
	// 'Mac' options tailor the application when running an macOS.
	app := application.New(application.Options{
		Name:        "Tools",
		Description: "A demo of using raw HTML & CSS",
		Services: []application.Service{
			application.NewService(&appsvc.GreetService{}),
			application.NewService(appInstance),
			application.NewService(screenshot.NewScreenshotService()),
		},
		Assets: application.AssetOptions{
			// 最外层包一个 Range 端点：查看器的大文件（音视频/大 PDF）不走 base64，
			// 直接 GET /fileview/media?path=..；其余路径原样透传给资源服务
			Handler: fileview.MediaHandler(application.AssetFileServerFS(assets)),
		},
		Windows: application.WindowsOptions{
			// 录制期间工具窗口处于隐藏状态，保持其 JS 定时器/捕获回调不被后台节流
			AdditionalBrowserArgs: []string{
				"--disable-background-timer-throttling",
				"--disable-backgrounding-occluded-windows",
				"--disable-renderer-backgrounding",
			},
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// 注入应用实例，使 App 可以向前端推送事件
	appInstance.SetApp(app)
	appInstance.SetNativeNotify(true) // 桌面版能弹原生通知卡

	// 所有工具包共用同一个发射器：
	//  1. 先交给 tasks 记账（离开页面也记得装到哪了，终态还能弹通知）；
	//  2. 再原样转给前端。
	tasks.SetEmitter(func(name string, data ...any) { app.Event.Emit(name, data...) })
	emit := func(name string, data ...any) {
		tasks.Observe(name, data...)
		if strings.HasSuffix(name, srvstate.EventSuffix) {
			notifyServerExit(appInstance, name, data...)
		}
		if name == ftp.EventTransfer {
			notifyTransferDone(appInstance, data...)
		}
		app.Event.Emit(name, data...)
	}

	// 安装/下载任务结束弹 Windows 系统原生通知（右下角 Toast，自动进通知中心）：
	// 标题按任务类型区分（安装完成 / 下载完成 / 操作完成），正文是具体版本或镜像。
	tasks.SetNotifier(func(t tasks.Task) {
		switch t.Status {
		case tasks.StatusDone:
			_ = appInstance.ShowSystemNotification(srvstate.DoneTitle(t.Tool, t.Kind), srvstate.ClipNotice(t.Title, 80))
		case tasks.StatusError:
			_ = appInstance.ShowSystemNotification(srvstate.FailTitle(t.Tool, t.Kind), srvstate.ClipNotice(t.Title+"："+t.Note, 120))
		}
	})

	usbserial.SetEmitter(emit)
	redis.SetEmitter(emit)
	mysql.SetEmitter(emit)
	docker.SetEmitter(emit)
	php.SetEmitter(emit)
	nginx.SetEmitter(emit)
	npm.SetEmitter(emit)
	term.SetEmitter(emit)
	ftp.SetEmitter(emit)
	guard.SetEmitter(emit)
	downloader.SetEmitter(emit)
	// 服务端若配了“随应用启动”就在这里拉起（失败只记日志，不阻断启动）
	ftp.StartAutoServer()
	// 守护工具：按“启用守护”自动拉起登记的程序
	guard.StartAutoGuard()

	// Create a new window with the necessary options.
	// 'Title' is the title of the window.
	// 'Mac' options tailor the window when running on macOS.
	// 'BackgroundColour' is the background colour of the window.
	// 'URL' is the URL that will be loaded into the webview.
	mainWindow := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: startupCfg.General.AppName,
		// Window sized to the golden ratio (1000 / 618 ≈ 1.618).
		Width:  1240,
		Height: 800,
		// 无边框：主窗口顶部改用前端 BasicLayout 自绘深色标题栏（与工具窗口 BlankLayout 一致），
		// 右上角仅保留“设置”入口与“关闭”按钮（关闭仍隐藏到托盘）。
		Frameless: true,
		// 禁用双击标题栏最大化：移除 WS_MAXIMIZEBOX 样式（Win32 双击拖拽区的默认处理会检查该样式），
		// 主窗口固定为非最大化尺寸，自适应交给前端布局
		MaximiseButtonState: application.ButtonHidden,
		// 主窗口固定尺寸：去掉 WS_THICKFRAME，拖边框不能缩放（前端的边缘缩放热区也不启用）
		DisableResize: true,
		Windows: application.WindowsWindow{
			// 启用 WebView2 非客户区支持，使 app-region: drag 自绘标题栏可拖动
			NonClientRegionSupport: true,
		},
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(6, 7, 15),
		URL:              "/",
	})

	// 注入主窗口与托盘图标：关闭主窗口时隐藏到托盘而非退出，托盘右键提供“设置/退出”
	appInstance.SetMainWindow(mainWindow)
	appInstance.SetTrayIcon(trayIconData)
	appInstance.SetDefaultLogo(defaultLogoPNG)

	// Create a goroutine that emits an event containing the current time every second.
	// The frontend can listen to this event and update the UI accordingly.
	go func() {
		for {
			now := time.Now().Format(time.RFC1123)
			app.Event.Emit("time", now)
			time.Sleep(time.Second)
		}
	}()

	// Run the application. This blocks until the application has been exited.
	err := app.Run()

	// 兜底收尾：正常退出时服务的 ServiceShutdown 已经关过一遍（幂等）。
	// 这里再补一手，避免将来有哪条退出路径绕过服务生命周期，
	// 把远端 shell 一直挂在服务器上。
	term.CloseAll()
	ftp.CloseAll()
	// 守护工具：一并结束由其拉起的子进程
	guard.Shutdown()

	// If an error occurred while running the application, log it and exit.
	if err != nil {
		log.Fatal(err)
	}
}

// notifyServerExit 把「实例异常退出」事件转成 Windows 系统原生通知。
//
// 工具包只负责在进程/容器退出时发事件（并区分崩溃与用户主动停止），
// 文案与弹窗留在这里：面板/浏览器构建没有系统通知中心，自然不会走到这条路径。
func notifyServerExit(a *appsvc.App, event string, data ...any) {
	if len(data) == 0 {
		return
	}
	buf, err := json.Marshal(data[0])
	if err != nil {
		return
	}
	var ev srvstate.Event
	if err := json.Unmarshal(buf, &ev); err != nil || ev.Tool == "" {
		log.Printf("实例退出通知：解析 %s 负载失败: %v", event, err)
		return
	}
	if err := a.ShowSystemNotification(ev.Title(), srvstate.ClipNotice(ev.Content(), 120)); err != nil {
		log.Printf("实例退出通知：%s 弹出失败: %v", event, err)
	}
}

// notifyTransferDone 把「上传/下载结束」事件转成系统原生通知。
//
// 只在终态弹：进度事件每 200ms 一次，不能跟着弹。前端页面内不再重复弹，
// 这样即使用户已经切到其它工具页，也能知道大文件传完了。
func notifyTransferDone(a *appsvc.App, data ...any) {
	if len(data) == 0 {
		return
	}
	buf, err := json.Marshal(data[0])
	if err != nil {
		return
	}
	var tr ftp.Transfer
	if err := json.Unmarshal(buf, &tr); err != nil || tr.ID == "" {
		return
	}
	var title, body string
	switch tr.State {
	case ftp.StateDone:
		if tr.Dir == ftp.DirUp {
			title = "上传完成"
		} else {
			title = "下载完成"
		}
		body = tr.Name
	case ftp.StateFailed:
		if tr.Dir == ftp.DirUp {
			title = "上传失败"
		} else {
			title = "下载失败"
		}
		body = tr.Name
		if tr.Err != "" {
			body += "：" + tr.Err
		}
	default:
		// 排队/进行中/已取消不打扰
		return
	}
	if err := a.ShowSystemNotification(title, srvstate.ClipNotice(body, 120)); err != nil {
		log.Printf("传输通知：%s 弹出失败: %v", title, err)
	}
}
