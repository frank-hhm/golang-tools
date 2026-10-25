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
//	WAILS_SERVER_HOST=0.0.0.0 WAILS_SERVER_PORT=8080 ./go-tools-server
//
// 端口默认 8080、主机默认 localhost（Wails 自带的环境变量约定）。

import (
	"embed"
	"log"
	"os"
	"time"

	appsvc "go-tools/services/app"
	"go-tools/services/applog"
	"go-tools/services/config"
	"go-tools/tools/docker"
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
}

func main() {
	// 配置与日志初始化与桌面版走同一套逻辑，日志目录也一致
	startupCfg := config.LoadAppConfig()
	logDir := applog.Init(config.ApplogConfig(startupCfg.Log))
	applog.Infof("app", "=== go-tools 启动（server 模式）=== 日志目录: %s (enabled=%v level=%s)", logDir, startupCfg.Log.Enabled, startupCfg.Log.Level)

	mysqlSettings := startupCfg.ToolSettings["mysql"]
	sqlLogDir := mysql.ConfigureSQLLog(mysqlSettings.SQLLogEnabled, mysqlSettings.SQLLogDir)
	applog.Infof("mysql", "SQL 日志: enabled=%v dir=%s", mysqlSettings.SQLLogEnabled, sqlLogDir)

	appInstance := appsvc.NewApp()
	app := application.New(application.Options{
		Name:        "go-tools",
		Description: "go-tools 服务器面板",
		Services: []application.Service{
			application.NewService(&appsvc.GreetService{}),
			application.NewService(appInstance),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		// Host/Port 留空即由 Wails 的默认值与 WAILS_SERVER_HOST / WAILS_SERVER_PORT 决定
		Server: application.ServerOptions{},
	})

	appInstance.SetApp(app)
	appInstance.SetTrayIcon(trayIconData)
	appInstance.SetDefaultLogo(defaultLogoPNG)

	// 事件发射器：各工具包把进度/日志推给前端，与桌面版完全相同
	usbserial.SetEmitter(func(name string, data ...any) { app.Event.Emit(name, data...) })
	redis.SetEmitter(func(name string, data ...any) { app.Event.Emit(name, data...) })
	mysql.SetEmitter(func(name string, data ...any) { app.Event.Emit(name, data...) })
	docker.SetEmitter(func(name string, data ...any) { app.Event.Emit(name, data...) })
	php.SetEmitter(func(name string, data ...any) { app.Event.Emit(name, data...) })
	nginx.SetEmitter(func(name string, data ...any) { app.Event.Emit(name, data...) })
	npm.SetEmitter(func(name string, data ...any) { app.Event.Emit(name, data...) })
	term.SetEmitter(func(name string, data ...any) { app.Event.Emit(name, data...) })

	host := os.Getenv("WAILS_SERVER_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("WAILS_SERVER_PORT")
	if port == "" {
		port = "8080"
	}
	applog.Infof("app", "监听 http://%s:%s （外部访问请设 WAILS_SERVER_HOST=0.0.0.0）", host, port)

	// 前端有个每秒一次的时钟事件，桌面版靠它刷新界面状态，这里保持一致
	go func() {
		for {
			app.Event.Emit("time", time.Now().Format(time.RFC1123))
			time.Sleep(time.Second)
		}
	}()

	err := app.Run()

	// 兜底收尾：service 生命周期正常时会关一遍，这里防退出路径绕过，把远端 shell 挂住
	term.CloseAll()

	if err != nil {
		log.Fatal(err)
	}
}
