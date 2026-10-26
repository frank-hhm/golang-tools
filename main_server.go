//go:build server

package main

// Linux / 容器部署用的无头入口：Wails 的 server 模式（-tags server）用 net/http
// 起服务，事件走 WebSocket，前端仍然是同一份 embed 的 dist。
//
// 与桌面版的差异只在"壳"：没有主窗口、没有托盘、没有截图浮层与内嵌浏览器，
// 业务方法（App 上的 400+ 个绑定）完全共用同一份实现。
//
// 启动：
//
//	go build -tags server -o go-tools-server .
//	./go-tools-server                 # 按 panel.json 的端口对外提供服务
//	./go-tools-server port 9000       # 改端口（见 services/panel 的 CLI 说明）
//
// 部署形态（重要）：
//
//	                    ┌──────────────────────────────┐
//	公网 :8080  ──────► │ services/panel（登录/入口/限速）│ ──► 127.0.0.1:<临时端口> Wails
//	                    └──────────────────────────────┘
//
// Wails 只监听回环地址，所有对外流量都先过面板的鉴权层：这样连 /wails/runtime
// 上的绑定方法（能装包、能起进程、能开 shell）和 /wails/events、/wails/stream/ws
// 这两个 WebSocket 端点也一并被挡住。

import (
	"embed"
	"encoding/base64"
	"fmt"
	"log"
	"net"
	"os"
	"runtime/debug"
	"time"

	appsvc "go-tools/services/app"
	"go-tools/services/applog"
	"go-tools/services/config"
	"go-tools/services/panel"
	"go-tools/services/tasks"
	"go-tools/tools/docker"
	"go-tools/tools/ftp"
	"go-tools/tools/mysql"
	"go-tools/tools/nginx"
	"go-tools/tools/npm"
	"go-tools/tools/php"
	"go-tools/tools/redis"
	"go-tools/tools/sniffer"
	"go-tools/tools/term"
	"go-tools/tools/usbserial"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var assets embed.FS

//go:embed logo.png
var defaultLogoPNG []byte

//go:embed build/windows/icon.ico
var trayIconData []byte

func init() {
	// 与桌面版保持一致的事件类型注册；capture:newframe 依赖 Windows 的截图浮层，
	// 这里不注册（前端监听它不会有任何事件到达，属于预期）。
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
	application.RegisterEvent[mysql.InstallProgress]("mysql:install")
	application.RegisterEvent[php.InstallProgress]("php:install")
	application.RegisterEvent[nginx.InstallProgress]("nginx:install")
	application.RegisterEvent[npm.InstallProgress]("npm:install")
	application.RegisterEvent[npm.InstallProgress]("npm:npmver")
	application.RegisterEvent[docker.PullProgress]("docker:pull")
	application.RegisterEvent[docker.ContainerLogLine]("docker:serverlog")
	application.RegisterEvent[docker.SetupProgress]("docker:setup")
	application.RegisterEvent[term.DataEvent]("term:data")
	application.RegisterEvent[term.ExitEvent]("term:exit")
	// 文件传输：任务进度、远端断线，以及服务端状态/日志
	application.RegisterEvent[ftp.Transfer]("ftp:transfer")
	application.RegisterEvent[ftp.SessionEvent]("ftp:session")
	application.RegisterEvent[ftp.ServerStatus]("ftp:server")
	application.RegisterEvent[ftp.ServerLog]("ftp:serverlog")
	// 统一的安装/下载任务增量：前端只认这一种形状
	application.RegisterEvent[tasks.Task](tasks.UpdateEvent)
}

func main() {
	// 管理命令（改密码/端口/安全入口）走 CLI，不启动服务：
	// 面板进不去的场景恰恰是最需要这些命令的时候。
	if panel.IsCLI(os.Args) {
		os.Exit(panel.RunCLI(os.Args))
	}

	checkAssets(assets)

	// 配置与日志初始化与桌面版走同一套逻辑，日志目录也一致
	startupCfg := config.LoadAppConfig()
	logDir := applog.Init(config.ApplogConfig(startupCfg.Log))
	applog.Infof("app", "=== go-tools 启动（server 模式）=== 日志目录: %s (enabled=%v level=%s)", logDir, startupCfg.Log.Enabled, startupCfg.Log.Level)

	mysqlSettings := startupCfg.ToolSettings["mysql"]
	sqlLogDir := mysql.ConfigureSQLLog(mysqlSettings.SQLLogEnabled, mysqlSettings.SQLLogDir)
	applog.Infof("mysql", "SQL 日志: enabled=%v dir=%s", mysqlSettings.SQLLogEnabled, sqlLogDir)

	// 面板访问控制配置；首次运行时生成随机管理员密码并打印出来
	panelSet, generated, err := panel.Load()
	if err != nil {
		log.Fatalf("读取面板配置失败: %v", err)
	}
	if generated != "" {
		printInitBanner(panelSet, generated)
		applog.Infof("panel", "首次启动已生成管理员密码（账号 %s），请登录后立即修改", panelSet.Username)
	}

	// 旧部署方式用 WAILS_SERVER_HOST/PORT 直连 Wails；现在对外端口由 panel.json 管，
	// 这两个环境变量会让 Wails 监听公网并绕过鉴权，必须清掉。
	if v := os.Getenv("WAILS_SERVER_HOST"); v != "" {
		applog.Infof("panel", "已忽略 WAILS_SERVER_HOST=%s（对外监听改由 panel.json 的 host 决定）", v)
		_ = os.Unsetenv("WAILS_SERVER_HOST")
	}
	if v := os.Getenv("WAILS_SERVER_PORT"); v != "" {
		applog.Infof("panel", "已忽略 WAILS_SERVER_PORT=%s（对外端口改由 panel.json 的 port 决定）", v)
		_ = os.Unsetenv("WAILS_SERVER_PORT")
	}

	// Wails 内部服务只监听回环，端口交给系统分配，避免与对外端口抢
	innerPort, err := freeLoopbackPort()
	if err != nil {
		log.Fatalf("分配内部端口失败: %v", err)
	}

	appInstance := appsvc.NewApp()
	app := application.New(application.Options{
		Name:        "go-tools",
		Description: "go-tools 服务器面板",
		Services: []application.Service{
			application.NewService(&appsvc.GreetService{}),
			application.NewService(appInstance),
		},
		Assets: application.AssetOptions{
			// 包一层 gzip：主包 1.6MB，公网直传容易中途被截断导致白屏
			Handler: gzipAssets(application.AssetFileServerFS(assets), assets),
		},
		// 只监听本机：对外的监听、鉴权与限速都由 services/panel 负责
		Server: application.ServerOptions{Host: "127.0.0.1", Port: innerPort},
	})

	appInstance.SetApp(app)
	appInstance.SetTrayIcon(trayIconData)
	appInstance.SetDefaultLogo(defaultLogoPNG)
	// 面板跑在浏览器里，没有原生通知窗口可开：终态通知由前端 Notification 负责
	appInstance.SetNativeNotify(false)

	// 事件发射器：各工具包把进度/日志推给前端，与桌面版完全相同
	// 不同的是这里多一道 tasks.Observe：浏览器标签页关掉后，重新打开还能
	// 从快照里把“装到哪了”取回来，而不是从零开始。
	tasks.SetEmitter(func(name string, data ...any) { app.Event.Emit(name, data...) })
	emit := func(name string, data ...any) {
		tasks.Observe(name, data...)
		app.Event.Emit(name, data...)
	}
	tasks.SetNotifier(func(t tasks.Task) {
		// 没有原生窗口：只落日志（系统通知交给浏览器 Notification）
		applog.Infof("task", "%s %s（%s）", t.Title, t.Status, t.Note)
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
	// 面板部署里服务端同样可开：配了自动启动就在进程内拉起
	ftp.StartAutoServer()

	// 对外服务：登录鉴权 + 安全入口 + 反向代理到上面的回环地址
	panelSrv, err := panel.New(fmt.Sprintf("127.0.0.1:%d", innerPort), panelSet, panel.Options{
		Title:   startupCfg.General.AppName,
		Version: versionString(),
		Logo:    dataURI(defaultLogoPNG),
	})
	if err != nil {
		log.Fatalf("初始化面板服务失败: %v", err)
	}
	if err := panelSrv.Start(); err != nil {
		// 端口被占用属于最常见的启动失败，直接报清楚，不要带着半个服务跑
		log.Fatalf("面板服务启动失败: %v\n提示: 可用 ./go-tools-server port <其它端口> 修改后重试", err)
	}
	applog.Infof("panel", "面板监听 %s（登录=%v 安全入口=%q），访问 %s",
		panelSet.ListenAddr(), panelSet.Enabled, panelSet.EntryPath, panelSet.AccessURL())

	// 前端有个每秒一次的时钟事件，桌面版靠它刷新界面状态，这里保持一致
	go func() {
		for {
			app.Event.Emit("time", time.Now().Format(time.RFC1123))
			time.Sleep(time.Second)
		}
	}()

	runErr := app.Run()

	panelSrv.Stop()

	// 兜底收尾：service 生命周期正常时会关一遍，这里防退出路径绕过，把远端 shell 挂住
	term.CloseAll()
	ftp.CloseAll()

	if runErr != nil {
		log.Fatal(runErr)
	}
}

// freeLoopbackPort 向系统要一个空闲的回环端口给 Wails 用。
func freeLoopbackPort() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	if addr, ok := ln.Addr().(*net.TCPAddr); ok {
		return addr.Port, nil
	}
	return 0, fmt.Errorf("无法获取端口: %v", ln.Addr())
}

// printInitBanner 把首次生成的账号密码打到终端。
//
// 这类信息只出现一次，且不写进日志文件（日志可能被同步/转发出去），
// 所以用醒目的框直接打在 stdout 上。
func printInitBanner(s panel.Settings, password string) {
	fmt.Print("\n")
	fmt.Println("┌──────────────── 面板初始化完成 ────────────────")
	fmt.Printf("│  账号: %s\n", s.Username)
	fmt.Printf("│  密码: %s\n", password)
	fmt.Printf("│  地址: %s\n", s.AccessURL())
	fmt.Println("│  提示: 登录后请在「设置 - 面板与安全」里修改密码或开启安全入口")
	fmt.Println("│        忘密码可在服务器执行: ./go-tools-server passwd")
	fmt.Println("└────────────────────────────────────────────────")
	fmt.Print("\n")
}

// dataURI 把内嵌 Logo 转成登录页可直接用的 data URI（登录页不经过前端资源）。
func dataURI(png []byte) string {
	if len(png) == 0 {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// versionString 版本号：优先取构建信息里的模块版本（打了 tag 的构建），
// 开发构建兜底成 "(devel)"。仅用于登录页页脚与管理接口的信息展示。
func versionString() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return "go-tools " + info.Main.Version
	}
	return "go-tools (devel)"
}
