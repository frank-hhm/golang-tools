//go:build windows

package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"go-tools/appdirs"
	"go-tools/services/applog"
	"go-tools/services/config"
	"go-tools/services/winui"
	"go-tools/tools/docker"
	"go-tools/tools/mysql"
	"go-tools/tools/nginx"
	"go-tools/tools/npm"
	"go-tools/tools/ocr"
	"go-tools/tools/php"
	"go-tools/tools/portcheck"
	"go-tools/tools/redis"
	"go-tools/tools/screenshot"
	"go-tools/tools/sniffer"
	"go-tools/tools/usbserial"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"golang.org/x/sys/windows"
)

// App struct
type App struct {
	app      *application.App
	ctx      context.Context
	engine   *sniffer.Engine
	packetCh chan sniffer.PacketInfo

	snifferMu      sync.Mutex
	mitm           *sniffer.Mitm
	snifferRunning bool

	recMu         sync.Mutex
	recWidget     application.Window // 录制悬浮小窗
	recToolWin    application.Window // 发起录制的视频工具窗口（录制期间隐藏）
	videoSessions map[string]*videoSaveSession

	toastMu  sync.Mutex
	toastWin application.Window // 原生消息通知小窗（屏幕右下角，自动消失）

	notifyMu  sync.Mutex
	notifyWin application.Window // 系统风格通知卡片（屏幕右下角，驻留至手动关闭）

	// 截图选区浮层（WebView 版）：Go 侧只持有冻结帧与窗口句柄
	capMu    sync.Mutex
	capWinMu sync.Mutex               // 串行化浮层窗口的创建（启动预热与热键调起可能撞在一起）
	capFrame *screenshot.CaptureFrame // 抓屏结果，供浮层页面拉取
	capWin   application.Window       // 全屏浮层窗口（首次冷建，之后隐藏复用，不每次重建）
	capBusy  bool                     // 一次截图进行中，拒绝重入
	capReady bool                     // 浮层已回报本次冻结帧就绪，看门狗据此判断要不要销毁
	capGen   uint64                   // 截图轮次：兜底定时器只处置自己那一轮，不得误杀已完结的浮层

	// 图钉置顶：每张钉住的截图开一个无边框置顶小窗，id → 图片数据/窗口
	pinMu   sync.Mutex
	pinSeq  int
	pinData map[int]string
	pinWins map[int]application.Window

	// 主窗口 + 系统托盘：关闭主窗口时隐藏到托盘，托盘右键提供“打开主界面/设置/退出”
	mainWindow  application.Window // 主窗口句柄（由 main 注入）
	trayIcon    []byte             // 托盘图标字节（PNG/ICO）
	tray        *application.SystemTray
	defaultLogo []byte      // 内置默认 logo PNG 字节
	quitting    atomic.Bool // 为 true 时表示真正退出，关闭钩子不再拦截隐藏
}

// videoSaveSession 录制完成后分块上传的视频缓冲（用于原生“另存为”写盘）
type videoSaveSession struct {
	name string
	data bytes.Buffer
}

// VideoStatus 录制悬浮小窗展示用的状态（由录制工具窗经 Go 转发）
type VideoStatus struct {
	State   string `json:"state"`   // recording|paused|saving|cancelsave|savefail|saved|closed
	Elapsed int    `json:"elapsed"` // 已录制秒数
	Note    string `json:"note"`    // 进度/路径/错误信息
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{videoSessions: map[string]*videoSaveSession{}}
}

// SetApp 注入 Wails v3 应用实例，供事件推送使用
func (a *App) SetApp(app *application.App) {
	a.app = app
}

// SetMainWindow 注入主窗口句柄，供托盘“打开主界面”及关闭到托盘使用
func (a *App) SetMainWindow(w application.Window) {
	a.mainWindow = w
}

// SetTrayIcon 注入托盘图标字节（PNG/ICO）
func (a *App) SetTrayIcon(b []byte) {
	a.trayIcon = b
}

// SetDefaultLogo 注入内置默认 logo 字节
func (a *App) SetDefaultLogo(b []byte) {
	a.defaultLogo = b
}

// GetAppLogo 返回当前应用 logo 的 base64 PNG 字符串（自定义路径优先，否则内置默认）
func (a *App) GetAppLogo() string {
	cfg := config.CurrentConfig()
	if cfg.General.AppLogo != "" {
		if data, err := os.ReadFile(cfg.General.AppLogo); err == nil && len(data) > 0 {
			return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
		}
	}
	if len(a.defaultLogo) > 0 {
		return "data:image/png;base64," + base64.StdEncoding.EncodeToString(a.defaultLogo)
	}
	return ""
}

// ChooseAppLogo 弹出文件选择器让用户选取 logo 图片，返回绝对路径
func (a *App) ChooseAppLogo() (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	path, err := a.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AllowsOtherFileTypes(true).
		SetTitle("选择 Logo 图片").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return path, nil
}

// ApplyAppName 将当前配置的应用名应用到主窗口标题和托盘提示
func (a *App) ApplyAppName() {
	cfg := config.CurrentConfig()
	if a.mainWindow != nil {
		a.mainWindow.SetTitle(cfg.General.AppName)
	}
	if a.tray != nil {
		a.tray.SetTooltip(cfg.General.AppName)
		// 更新托盘图标为当前 logo
		logoData := a.getLogoBytes()
		if len(logoData) > 0 {
			a.tray.SetIcon(logoData)
		}
	}
}

// getLogoBytes 返回当前生效的 logo 字节（自定义路径优先，否则内置默认）
func (a *App) getLogoBytes() []byte {
	cfg := config.CurrentConfig()
	if cfg.General.AppLogo != "" {
		if data, err := os.ReadFile(cfg.General.AppLogo); err == nil && len(data) > 0 {
			return data
		}
	}
	return a.defaultLogo
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// ServiceStartup Wails v3 (beta.12) 服务生命周期钩子：应用启动时由框架自动调用。
// 注意：旧版 ApplicationStartup(ctx) 在 beta.12 中已不被识别，只有
// ServiceStartup(ctx, options) error 会被 services.go 中的 ServiceStartup 接口断言命中；
// 写错名字不会报错但永远不会被调用（热键/浮动球/托盘均依赖此入口）。
func (a *App) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	a.startup(ctx)
	// 启动全局热键（工具 Shortcut 由 nativeHost.Hotkeys 给出），a.app 已由 SetApp 注入
	winui.StartHotkeys(nativeHost{a})
	// 启动时按设置显示浮动球
	if config.CurrentConfig().Ball.Enabled {
		if err := a.BallOpen(); err != nil {
			applog.Warnf("ball", "启动时显示浮动球失败: %v", err)
		}
	}
	// 预热截图浮层：WebView2 开窗 + 页面 bootstrap 是首次截图最贵的一笔（实测 ~300ms），
	// 挪到启动空闲期付掉，之后每次截图都只走“推帧 + 复用窗口”。
	go func() {
		time.Sleep(2 * time.Second)
		a.PrewarmCaptureOverlay()
	}()
	// 关闭主窗口时隐藏到托盘（不退出），并提供托盘右键菜单
	a.setupCloseToTray()
	a.setupTray()
	// 开机自启：按配置同步注册（Enable 幂等，程序移动后也能修正旧路径）
	if config.CurrentConfig().General.AutoStart {
		a.applyAutoStart(true)
	}
	return nil
}

// ServiceShutdown 应用退出时停止所有由本程序拉起的本地服务实例（Redis + MySQL + PHP + Nginx + npm 静态站点）。
func (a *App) ServiceShutdown() error {
	redis.StopAll()
	mysql.StopAll()
	php.StopAll()
	nginx.StopAll()
	npm.StopAll()
	return nil
}

// applyAutoStart 按设置同步开机自启注册（Windows：HKCU\...\Run 注册表键）。
func (a *App) applyAutoStart(enabled bool) {
	if a.app == nil {
		return
	}
	var err error
	if enabled {
		err = a.app.Autostart.Enable()
	} else {
		err = a.app.Autostart.Disable()
	}
	if err != nil {
		applog.Warnf("config", "设置开机自启失败(enabled=%v): %v", enabled, err)
	} else {
		applog.Infof("config", "开机自启已同步: enabled=%v", enabled)
	}
}

/* ============================ 系统托盘 / 关闭到托盘 ============================ */

// setupCloseToTray 为主窗口注册关闭钩子：非真正退出时，点“关闭”仅隐藏窗口到托盘。
func (a *App) setupCloseToTray() {
	if a.mainWindow == nil {
		return
	}
	a.mainWindow.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if a.quitting.Load() {
			return // 真正退出：不拦截，走默认销毁
		}
		e.Cancel() // 取消默认关闭，仅隐藏到托盘
		a.mainWindow.Hide()
		applog.Infof("tray", "主窗口关闭请求已拦截：隐藏到托盘")
	})
}

// setupTray 创建系统托盘图标及右键菜单（打开主界面 / 基本设置 / 退出）。
func (a *App) setupTray() {
	if a.app == nil {
		return
	}
	tray := a.app.SystemTray.New()
	if len(a.trayIcon) > 0 {
		tray.SetIcon(a.trayIcon)
	}
	tray.SetTooltip(config.CurrentConfig().General.AppName)

	menu := application.NewMenu()
	menu.Add("打开主界面").OnClick(func(*application.Context) { a.showMainWindow() })
	menu.Add("基本设置").OnClick(func(*application.Context) { a.openSettingsFromTray() })
	menu.AddSeparator()
	menu.Add("退出").OnClick(func(*application.Context) { a.RealQuit() })
	tray.SetMenu(menu)

	// 单击托盘图标：显示主窗口；保留右键菜单
	tray.OnClick(func() { a.showMainWindow() })
	a.tray = tray
	applog.Infof("tray", "系统托盘已创建")
}

// showMainWindow 显示并聚焦主窗口（从托盘唤起）。
func (a *App) showMainWindow() {
	if a.mainWindow == nil {
		return
	}
	a.mainWindow.Show()
	a.mainWindow.UnMinimise()
	a.mainWindow.Focus()
}

// openSettingsFromTray 从托盘打开“设置”（单例工具窗口）。
// 注意：需走 /win 前缀的空白布局路由（BlankLayout 自绘标题栏+菜单栏），
// 与前端 useToolWindow 的 "/win"+path 约定保持一致，否则新窗口没有原有菜单栏。
func (a *App) openSettingsFromTray() {
	if err := a.OpenToolWindow("/win/tools/settings", "设置", true, false, 0, 0); err != nil {
		applog.Warnf("tray", "打开设置失败: %v", err)
	}
}

// RealQuit 真正退出程序：置退出标志（使关闭钩子不再拦截）、销毁托盘，再终止应用。
func (a *App) RealQuit() {
	applog.Infof("tray", "托盘菜单：真正退出")
	a.quitting.Store(true)
	// 截图浮层先销毁：它是透明全屏置顶层，哪怕只是隐藏态留在原地，退出流程一旦
	// 卡住（WebView2 收尾慢）就会变成整个桌面点不动，等进程真退了才恢复。
	a.destroyCaptureOverlay()
	if a.tray != nil {
		a.tray.Destroy()
		a.tray = nil
	}
	if a.app != nil {
		a.app.Quit()
	}
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}

// OpenToolWindow 在新的 Wails 原生窗口中打开指定前端路由（hash 路由）。
// path 形如 "/tools/timestamp"；title 为新窗口标题；
// single 为 true 时为该路由登记窗口名，已存在则直接复用（展示+取消最小化+聚焦），不重复创建；
// fullscreen 为 true 时新窗口最大化铺满显示器（非排他全屏，避免顶部自绘标题栏被遮挡）；
// width/height 为新窗口视口尺寸，<=0 时继承默认尺寸（1000x718）；
// path 的 query 带 fs=0（工具配置“禁用全屏”）时，隐藏最大化按钮、禁止标题栏双击最大化，并禁用边框拖拽缩放。
func (a *App) OpenToolWindow(path string, title string, single bool, fullscreen bool, width int, height int) error {
	if a.app == nil {
		return fmt.Errorf("应用尚未初始化")
	}
	if path == "" {
		return fmt.Errorf("path 不能为空")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if title == "" {
		title = "go-tools"
	}
	startState := application.WindowStateNormal
	if fullscreen {
		// 用最大化铺满显示器（而非排他全屏），保证自绘标题栏/菜单不被遮挡
		startState = application.WindowStateMaximised
	}
	// 视口尺寸：未设定（<=0）则继承默认 1000x718
	if width <= 0 {
		width = 1000
	}
	if height <= 0 {
		height = 718
	}
	opts := application.WebviewWindowOptions{
		Title:            title,
		Width:            width,
		Height:           height,
		BackgroundColour: application.NewRGB(6, 7, 15),
		StartState:       startState,
		// 无边框：标题栏与“视图”菜单均由前端 BlankLayout 自绘（菜单在标题右侧）
		Frameless: true,
		Windows: application.WindowsWindow{
			// 启用 WebView2 非客户区支持，使 app-region: drag 自绘标题栏可拖动
			NonClientRegionSupport: true,
		},
		URL: "/#" + path,
	}
	// “禁用全屏”（前端拼 fs=0）：除 BlankLayout 隐藏最大化按钮外，
	// 再移除 WS_MAXIMIZEBOX 样式——Win32 对标题栏双击（WM_NCLBUTTONDBLCLK）的
	// 默认处理会检查该样式，去掉后双击拖拽区也不再最大化
	if strings.Contains(path, "fs=0") {
		opts.MaximiseButtonState = application.ButtonHidden
		// 禁用全屏的工具窗固定尺寸：去掉 WS_THICKFRAME，前端运行时的边缘拖拽缩放（drag.js）
		// 与 wails:resize 原生缩放循环都随之失效
		opts.DisableResize = true
	}
	// 单例窗口：以路由路径（去掉 ?query，如 ?inst=/?fs=0）作为唯一名称，已存在则聚焦复用
	if single {
		cleanPath := path
		if qi := strings.Index(cleanPath, "?"); qi >= 0 {
			cleanPath = cleanPath[:qi]
		}
		name := "win:" + cleanPath
		if w, ok := a.app.Window.GetByName(name); ok {
			w.Show()
			w.UnMinimise()
			w.Focus()
			applog.Infof("window", "复用单例窗口: %s (%s)", path, title)
			return nil
		}
		opts.Name = name
	}
	applog.Infof("window", "新建窗口: %s (%s) single=%v fullscreen=%v %dx%d", path, title, single, fullscreen, width, height)
	a.app.Window.NewWithOptions(opts)
	return nil
}

/* ============================ 串口 / 网络调试 ============================ */

// GetSerialPorts 获取系统串口列表
func (a *App) GetSerialPorts() ([]usbserial.SerialDeviceInfo, error) {
	return usbserial.ListSerialPorts()
}

// GetLocalIPs 获取本机可用 IPv4 地址列表（供 TCP/UDP 本地地址选择）
func (a *App) GetLocalIPs() []string {
	return usbserial.LocalIPs()
}

// OpenSerial 打开串口
func (a *App) OpenSerial(cfg usbserial.ConnConfig) error {
	return usbserial.OpenSerial(cfg)
}

// OpenTCPClient 作为 TCP 客户端连接远端
func (a *App) OpenTCPClient(cfg usbserial.ConnConfig) error {
	return usbserial.OpenTCPClient(cfg)
}

// ListenTCPServer 作为 TCP 服务端侦听
func (a *App) ListenTCPServer(cfg usbserial.ConnConfig) error {
	return usbserial.ListenTCPServer(cfg)
}

// OpenUDP 打开 UDP 连接
func (a *App) OpenUDP(cfg usbserial.ConnConfig) error {
	return usbserial.OpenUDP(cfg)
}

// CloseConnection 关闭当前连接（串口/TCP/UDP 通用）
func (a *App) CloseConnection() error {
	return usbserial.Close()
}

// SendSerialHex 以 hex 字符串发送数据到当前连接对端，返回发送字节数
func (a *App) SendSerialHex(hexData string) (int, error) {
	return usbserial.SendHex(hexData)
}

// SetSerialControlBits 实时切换串口 RTS / DTR 电平
func (a *App) SetSerialControlBits(rts, dtr bool) error {
	return usbserial.SetControlBits(rts, dtr)
}

// ConnectionStatus 返回当前连接状态
func (a *App) ConnectionStatus() usbserial.ConnStatus {
	return usbserial.Status()
}

// GetDevices 暴露给前端：获取网卡列表
//
//	func (a *App) GetDevicesSniffer() ([]sniffer.DeviceInfo, error) {
//		return sniffer.GetDevices()
//	}
func (a *App) GetDevicesSniffer() ([]sniffer.PcapDeviceInfo, error) {
	return sniffer.GetPcapDevices()
}

// StartCaptureSniffer 暴露给前端：开始抓包
// 注意：devices 参数现在直接接收 Npcap 的真实设备名 (pcapName)
// 新增 ports 参数，例如传入 ["80"] 或 ["80", "443"]
func (a *App) StartCaptureSniffer(devices []string, protocols []string, ports []string) error {
	fmt.Printf("[Sniffer] 收到抓包请求 - 网卡: %v, 协议: %v, 端口: %v\n", devices, protocols, ports)
	a.app.Event.Emit("packet:update", "收到抓包请求")

	// 将 ports 传递给底层
	packetCh, err := sniffer.StartCapture(a.ctx, devices, protocols, ports)
	if err != nil {
		fmt.Printf("[Sniffer] 抓包启动失败: %v\n", err)
		return err
	}

	fmt.Println("[Sniffer] 抓包已成功启动，开始监听数据包...")

	// 启动后台协程，将抓到的包实时推送到前端
	go func() {
		for pkt := range packetCh {
			if pkt.Request != nil {
				fmt.Printf("[Sniffer] %s %s%s -> %s:%d\n", pkt.Request.Method, pkt.Request.Host, pkt.Request.URL, pkt.DstIP, pkt.DstPort)
			} else if pkt.Response != nil {
				fmt.Printf("[Sniffer] HTTP Response %s from %s:%d\n", pkt.Response.Status, pkt.SrcIP, pkt.SrcPort)
			} else {
				fmt.Printf("[Sniffer] %s %s:%d -> %s:%d [%s]\n", pkt.Protocol, pkt.SrcIP, pkt.SrcPort, pkt.DstIP, pkt.DstPort, pkt.Flags)
			}
			a.app.Event.Emit("packet:update", pkt)
		}
		fmt.Println("[Sniffer] 数据包通道已关闭，抓包协程退出。")
	}()

	return nil
}

// StopCapture 暴露给前端：停止抓包
func (a *App) StopCaptureSniffer() {
	// 3. 调用 sniffer 包的 StopCapture 函数
	sniffer.StopCapture()
}

/* ================= MITM 代理抓包（HTTP/HTTPS 解密） ================= */

// StartSnifferProxy 启动 MITM 代理：安装根证书、设置系统代理、开始捕获。
// 每捕获一条请求/响应，通过 "sniffer:flow" 事件推送给前端。
func (a *App) StartSnifferProxy(port int) error {
	a.snifferMu.Lock()
	defer a.snifferMu.Unlock()
	if a.snifferRunning {
		return fmt.Errorf("抓包代理已在运行")
	}
	if port <= 0 || port > 65535 {
		port = 8888
	}

	m, err := sniffer.NewMitm()
	if err != nil {
		return fmt.Errorf("初始化代理失败: %w", err)
	}
	if err := m.Start(port, func(f sniffer.Flow) {
		if a.app != nil {
			a.app.Event.Emit("sniffer:flow", f)
		}
	}); err != nil {
		return err
	}

	// 安装根证书 + 设置系统代理（失败仅告警，不影响代理本身）
	if err := sniffer.InstallCA(m.CertPath()); err != nil {
		fmt.Printf("[Sniffer] 安装根证书失败: %v\n", err)
	}
	if err := sniffer.SetSystemProxy(port); err != nil {
		fmt.Printf("[Sniffer] 设置系统代理失败: %v\n", err)
	}

	a.mitm = m
	a.snifferRunning = true
	fmt.Printf("[Sniffer] MITM 代理已启动，监听 127.0.0.1:%d\n", port)
	return nil
}

// StopSnifferProxy 停止 MITM 代理：还原系统代理并卸载根证书
func (a *App) StopSnifferProxy() error {
	a.snifferMu.Lock()
	defer a.snifferMu.Unlock()
	if !a.snifferRunning || a.mitm == nil {
		return nil
	}
	sniffer.ClearSystemProxy()
	a.mitm.Stop()
	sniffer.UninstallCA()
	a.mitm = nil
	a.snifferRunning = false
	fmt.Println("[Sniffer] MITM 代理已停止，系统代理已还原")
	return nil
}

// SnifferProxyStatus 返回代理运行状态与端口
func (a *App) SnifferProxyStatus() (bool, int) {
	a.snifferMu.Lock()
	defer a.snifferMu.Unlock()
	if a.mitm != nil {
		return a.snifferRunning, a.mitm.Port()
	}
	return a.snifferRunning, 0
}

// GetSnifferFlows 返回后台缓冲的抓包记录，供抓包窗口重开时回填
// （页面关闭期间服务继续捕获的数据在这里，服务未运行/已停止时返回空）
func (a *App) GetSnifferFlows() []sniffer.Flow {
	a.snifferMu.Lock()
	defer a.snifferMu.Unlock()
	if !a.snifferRunning || a.mitm == nil {
		return nil
	}
	return a.mitm.Flows()
}

// ClearSnifferFlows 清空后台抓包缓冲（前端「清空」按钮同步调用，避免重开窗口旧数据复现）
func (a *App) ClearSnifferFlows() {
	a.snifferMu.Lock()
	defer a.snifferMu.Unlock()
	if a.mitm != nil {
		a.mitm.ResetFlows()
	}
}

// SnifferSuggestPort 从 from 起探测一个可用的代理端口（跳过实例登记与系统监听）
func (a *App) SnifferSuggestPort(from int) int { return sniffer.SuggestPort(from) }

// SnifferPortTaken 报告代理端口能否使用，返回占用原因，空串表示可用
func (a *App) SnifferPortTaken(port int) string { return sniffer.PortTaken(port) }

/* ============================ Redis 管理 ============================ */

// RedisOpen 建立并登记一个 Redis 连接
func (a *App) RedisOpen(cfg redis.ConnConfig) error {
	return redis.Open(cfg)
}

// RedisClose 关闭指定 Redis 连接
func (a *App) RedisClose(id string) error {
	return redis.Close(id)
}

// RedisStatus 获取服务器状态概览
func (a *App) RedisStatus(id string) (redis.ServerStatus, error) {
	return redis.Status(id)
}

// RedisScan 增量扫描键（pattern 为空默认 *，limit 限制返回数量）
func (a *App) RedisScan(id, pattern string, count, limit int) (redis.ScanResult, error) {
	return redis.Scan(id, pattern, count, limit)
}

// RedisGetKey 读取键详情（按类型返回结构）
func (a *App) RedisGetKey(id, key string) (redis.KeyValue, error) {
	return redis.GetKey(id, key)
}

// RedisSetString 写入/更新 string 类型键
func (a *App) RedisSetString(id, key, value string, ttl int) error {
	return redis.SetString(id, key, value, ttl)
}

// RedisBatchSetString 批量生成 string 键：keyPrefix+序号 / valuePrefix+序号，序号 [start,end]（含），ttl 秒（<=0 不过期）。返回写入数量
func (a *App) RedisBatchSetString(id, keyPrefix, valuePrefix string, start, end, ttl int) (int64, error) {
	return redis.BatchSetString(id, keyPrefix, valuePrefix, start, end, ttl)
}

// RedisDeleteKeys 删除若干键，返回删除数量
func (a *App) RedisDeleteKeys(id string, keys []string) (int64, error) {
	return redis.DeleteKeys(id, keys)
}

// RedisSetTTL 设置/清除键过期时间
func (a *App) RedisSetTTL(id, key string, ttl int) error {
	return redis.SetTTL(id, key, ttl)
}

// RedisSlowlog 拉取慢查询日志
func (a *App) RedisSlowlog(id string, count int) ([]redis.SlowlogEntry, error) {
	return redis.Slowlog(id, count)
}

// RedisStartMonitor 开始监控命令（逐条通过 redis:monitor 事件推送）
func (a *App) RedisStartMonitor(cfg redis.ConnConfig) error {
	return redis.StartMonitor(cfg)
}

// RedisStopMonitor 停止监控命令
func (a *App) RedisStopMonitor() error {
	return redis.StopMonitor()
}

// RedisSaveConnections 将连接配置保存到用户配置文件，返回文件路径
func (a *App) RedisSaveConnections(configs []redis.ConnConfig) (string, error) {
	return redis.SaveConnections(configs)
}

// RedisLoadConnections 从配置文件读取连接列表
func (a *App) RedisLoadConnections() ([]redis.ConnConfig, error) {
	return redis.LoadConnections()
}

// RedisConnectionsFilePath 返回配置文件路径
func (a *App) RedisConnectionsFilePath() string {
	return redis.ConnectionsFilePath()
}

/* ============================ Redis 本地服务管理 ============================ */

// RedisSrvListVersions 列出本地可用版本（内置/已下载/本机登记，按版本降序）
func (a *App) RedisSrvListVersions() ([]redis.ServerVersion, error) { return redis.ListVersions() }

// RedisSrvRemoteVersions 拉取线上可安装版本（GitHub redis-windows，>=7.0）
func (a *App) RedisSrvRemoteVersions() ([]redis.RemoteVersion, error) {
	return redis.FetchRemoteVersions()
}

// RedisSrvInstall 异步安装版本，进度经 redis:install 事件推送
func (a *App) RedisSrvInstall(version string) error { return redis.InstallVersion(version) }

// RedisSrvRemoveVersion 删除版本（内置不可删，运行中实例引用时拒绝）
func (a *App) RedisSrvRemoveVersion(version string) error { return redis.RemoveVersion(version) }

// RedisSrvAddLocal 注册本机已有的 redis-server 目录
func (a *App) RedisSrvAddLocal(dir string) error { return redis.AddLocalVersion(dir) }

// RedisSrvChooseDir 弹原生目录选择框（供添加本机 Redis 目录），取消返回空串
func (a *App) RedisSrvChooseDir() (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	dir, err := a.app.Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		AllowsOtherFileTypes(false).
		SetTitle("选择 redis-server 所在目录").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return dir, nil
}

// RedisSrvListInstances 列出全部实例配置
func (a *App) RedisSrvListInstances() ([]redis.InstanceConfig, error) { return redis.ListInstances() }

// RedisSrvSaveInstance 新增/更新实例配置（ID 为空则新建）
func (a *App) RedisSrvSaveInstance(cfg redis.InstanceConfig) error { return redis.SaveInstance(cfg) }

// RedisSrvRemoveInstance 删除实例配置（运行中先停止）
func (a *App) RedisSrvRemoveInstance(id string) error { return redis.RemoveInstance(id) }

// RedisSrvStart 启动实例（隐藏窗口拉起 redis-server，日志经 redis:serverlog 事件推送）
func (a *App) RedisSrvStart(id string) error { return redis.StartInstance(id) }

// RedisSrvStop 停止实例（优雅 SHUTDOWN NOSAVE，兑底 Kill）
func (a *App) RedisSrvStop(id string) error { return redis.StopInstance(id) }

// RedisSrvStatus 实例运行状态（PING+INFO 实时探测）
func (a *App) RedisSrvStatus(id string) (redis.InstanceStatus, error) {
	return redis.GetInstanceStatus(id)
}

// RedisSrvLogs 返回实例最近 n 行日志
func (a *App) RedisSrvLogs(id string, lines int) ([]string, error) { return redis.Logs(id, lines) }

// RedisSrvSuggestPort 从 from 起探测可用端口
func (a *App) RedisSrvSuggestPort(from int) int { return redis.SuggestPort(from) }

// RedisSrvPortTaken 报告端口能否给实例用（任意工具实例已登记或进程监听），返回占用原因，空串表示可用
func (a *App) RedisSrvPortTaken(port int, excludeId string) string {
	return redis.PortTaken(port, excludeId)
}

// RedisSrvDockerImages 本地 redis 系镜像 tag（Docker 模式表单候选）
func (a *App) RedisSrvDockerImages() ([]string, error) { return redis.DockerImages() }

// RedisSrvDefaultImage Docker 模式默认镜像
func (a *App) RedisSrvDefaultImage() string { return redis.DefaultDockerImage }

// RedisSrvDockerAvailable Docker 引擎是否就绪（不可用时返回原因，仅提示不阻断保存）
func (a *App) RedisSrvDockerAvailable() (bool, string) { return redis.DockerAvailable() }

// RedisSrvServersDir 返回离线/插件目录 <exedir>/plugins/redis（内置版本与离线 zip 放置位置），供前端展示指引
func (a *App) RedisSrvServersDir() string {
	dir, err := redis.OfflineDir()
	if err != nil {
		return ""
	}
	return dir
}

/* ======================== MySQL 本地服务管理 ======================== */

// MySQLSrvListVersions 聚合三渠道本地可用版本
func (a *App) MySQLSrvListVersions() ([]mysql.ServerVersion, error) { return mysql.ListVersions() }

// MySQLSrvRemoteVersions 拉取线上可安装版本（dev.mysql.com 官方 zip）
func (a *App) MySQLSrvRemoteVersions() ([]mysql.RemoteVersion, error) {
	return mysql.FetchRemoteVersions()
}

// MySQLSrvInstall 异步下载安装 MySQL 版本
func (a *App) MySQLSrvInstall(version string) error { return mysql.InstallVersion(version) }

// MySQLSrvRemoveVersion 删除版本（内置不可删，运行中实例引用时拒绝）
func (a *App) MySQLSrvRemoveVersion(version string) error { return mysql.RemoveVersion(version) }

// MySQLSrvAddLocal 注册本机已有的 MySQL 目录
func (a *App) MySQLSrvAddLocal(dir string) error { return mysql.AddLocalVersion(dir) }

// MySQLSrvChooseDir 弹原生目录选择框（供添加本机 MySQL 目录），取消返回空串
func (a *App) MySQLSrvChooseDir() (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	dir, err := a.app.Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		AllowsOtherFileTypes(false).
		SetTitle("选择 mysqld.exe 所在目录").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return dir, nil
}

// MySQLSrvListInstances 列出全部实例配置
func (a *App) MySQLSrvListInstances() ([]mysql.InstanceConfig, error) { return mysql.ListInstances() }

// MySQLSrvSaveInstance 新增/更新实例配置（ID 为空则新建）
func (a *App) MySQLSrvSaveInstance(cfg mysql.InstanceConfig) error { return mysql.SaveInstance(cfg) }

// MySQLSrvRemoveInstance 删除实例配置（运行中先停止）
func (a *App) MySQLSrvRemoveInstance(id string) error { return mysql.RemoveInstance(id) }

// MySQLSrvStart 启动实例
func (a *App) MySQLSrvStart(id string) error { return mysql.StartInstance(id) }

// MySQLSrvStop 停止实例（SHUTDOWN + 兆底 Kill）
func (a *App) MySQLSrvStop(id string) error { return mysql.StopInstance(id) }

// MySQLSrvStatus 实例运行状态
func (a *App) MySQLSrvStatus(id string) (mysql.InstanceStatus, error) {
	return mysql.GetInstanceStatus(id)
}

// MySQLSrvLogs 返回实例 mysql.err 尾部 n 行
func (a *App) MySQLSrvLogs(id string, lines int) ([]string, error) { return mysql.Logs(id, lines) }

// MySQLSrvSuggestPort 从 from 起探测可用端口
func (a *App) MySQLSrvSuggestPort(from int) int { return mysql.SuggestPort(from) }

// MySQLSrvPortTaken 报告端口能否给实例用（任意工具实例已登记或进程监听），返回占用原因，空串表示可用
func (a *App) MySQLSrvPortTaken(port int, excludeId string) string {
	return mysql.PortTaken(port, excludeId)
}

// MySQLSrvPluginsDir 返回插件目录 <exedir>/plugins/mysql，供前端展示指引
func (a *App) MySQLSrvPluginsDir() string {
	dir, err := mysql.PluginsDir()
	if err != nil {
		return ""
	}
	return dir
}

// MySQLSrvDockerImages 本地 mysql/mariadb/percona 系镜像 tag（Docker 模式表单候选）
func (a *App) MySQLSrvDockerImages() ([]string, error) { return mysql.DockerImages() }

// MySQLSrvDefaultImage Docker 模式默认镜像
func (a *App) MySQLSrvDefaultImage() string { return mysql.DefaultDockerImage }

/* ======================== Docker 引擎与容器管理 ======================== */

// DockerSrvEngineStatus 探测 Docker 引擎；不可达也返回（Available=false + 可读 Error）
func (a *App) DockerSrvEngineStatus() (docker.EngineInfo, error) { return docker.GetEngineStatus() }

// DockerSrvListContainers 列出容器（all=false 仅运行中），运行中的附带 CPU/内存
func (a *App) DockerSrvListContainers(all bool) ([]docker.Container, error) {
	return docker.ListContainers(all)
}

// DockerSrvInspectContainer 单容器实时状态
func (a *App) DockerSrvInspectContainer(id string) (*docker.ContainerState, error) {
	return docker.InspectContainer(id)
}

// DockerSrvContainerMetrics 单容器实时资源占用（容器监控弹窗按 1s 轮询，无缓存）
func (a *App) DockerSrvContainerMetrics(id string) (*docker.ContainerMetrics, error) {
	return docker.ContainerMetricsOf(id)
}

// DockerSrvStartContainer 启动容器
func (a *App) DockerSrvStartContainer(id string) error { return docker.StartContainer(id) }

// DockerSrvStopContainer 停止容器（timeoutSec 优雅退出等待秒，<=0 用 10）
func (a *App) DockerSrvStopContainer(id string, timeoutSec int) error {
	return docker.StopContainer(id, timeoutSec)
}

// DockerSrvRestartContainer 重启容器
func (a *App) DockerSrvRestartContainer(id string) error { return docker.RestartContainer(id) }

// DockerSrvRemoveContainer 删除容器（force=true 先杀再删）
func (a *App) DockerSrvRemoveContainer(id string, force bool) error {
	return docker.RemoveContainer(id, force)
}

// DockerSrvRenameContainer 重命名容器
func (a *App) DockerSrvRenameContainer(id, name string) error {
	return docker.RenameContainer(id, name)
}

// DockerSrvListImages 本地镜像列表
func (a *App) DockerSrvListImages() ([]docker.Image, error) { return docker.ListImages() }

// DockerSrvRemoveImage 删除本地镜像（force=true 连带删除子引用）
func (a *App) DockerSrvRemoveImage(image string, force bool) error {
	return docker.RemoveImage(image, force)
}

// DockerSrvPullImage 拉取镜像，分层与整体进度经 "docker:pull" 事件推送；可被 CancelImageOp 取消。
// 用户主动取消不是故障：这里吸收为 nil（取消态由 stage=canceled 事件送达），
// 否则 Wails 会把“点了取消”记成「Binding call failed」的 ERR。
func (a *App) DockerSrvPullImage(image string) error {
	err := docker.PullImage(image)
	if docker.IsCanceled(err) {
		return nil
	}
	return err
}

// DockerSrvCancelImageOp 取消镜像长任务：target 为空表示全部取消，
// 否则按任务名（如 "pull mysql:8.4"）精确匹配；返回被取消的任务名
func (a *App) DockerSrvCancelImageOp(target string) (string, error) {
	return docker.CancelImageOp(target)
}

// DockerSrvCurrentImageOp 当前正在跑的长任务名（多个用“、”连接，空=空闲）
func (a *App) DockerSrvCurrentImageOp() string { return docker.CurrentImageOp() }

// DockerSrvPushImage 推送本地镜像到它 tag 里写的仓库（进度经 "docker:imageop"）
func (a *App) DockerSrvPushImage(ref string) error { return docker.PushImage(ref) }

// DockerSrvPushImageTo 把本地镜像以目标引用（仓库/命名空间/名:tag）推出去，必要时先打 tag
func (a *App) DockerSrvPushImageTo(localRef, targetRef string) error {
	return docker.PushImageTo(localRef, targetRef)
}

// DockerSrvExportImage 导出镜像为 tar 包，outPath 空时落到默认数据目录
func (a *App) DockerSrvExportImage(ref, outPath string) (string, error) {
	if strings.TrimSpace(outPath) == "" {
		outPath = docker.DefaultExportPath(ref)
	}
	return docker.ExportImage(ref, outPath)
}

// DockerSrvImportImage 从 dockersave 的 tar 包导入镜像（ref 空则沿用包内 tag）
func (a *App) DockerSrvImportImage(tarPath, ref string) (string, error) {
	return docker.ImportImage(tarPath, ref)
}

// DockerSrvBuildImage 用目录里的 Dockerfile 构建镜像（进度经 "docker:imageop"）
func (a *App) DockerSrvBuildImage(dir, dockerfile, tag string) (string, error) {
	return docker.BuildImage(dir, dockerfile, tag)
}

// DockerSrvPruneImages 清理无用镜像（all=false 只清悬空镜像）
func (a *App) DockerSrvPruneImages(all bool) (docker.PruneReport, error) {
	return docker.PruneImages(all)
}

// DockerSrvRunImage 由镜像创建容器（可选发端口/环境变量/立即启动）
func (a *App) DockerSrvRunImage(spec docker.ImageRunSpec) (string, error) {
	return docker.RunImage(spec)
}

/* ---------- 网络 / 存储卷 / 编排 ---------- */

// DockerSrvListNetworks 列出 Docker 网络（含内置 bridge/host/none）
func (a *App) DockerSrvListNetworks() ([]docker.Network, error) { return docker.ListNetworks() }

// DockerSrvCreateNetwork 新建自定义网络（driver 空=bridge，subnet 空=IPAM 自动分配）
func (a *App) DockerSrvCreateNetwork(spec docker.NetworkSpec) (string, error) {
	return docker.CreateNetwork(spec)
}

// DockerSrvRemoveNetwork 删除网络（仍有容器接入时 daemon 拒绝）
func (a *App) DockerSrvRemoveNetwork(id string) error { return docker.RemoveNetwork(id) }

// DockerSrvListVolumes 列出存储卷（inUse = 引用它的容器数）
func (a *App) DockerSrvListVolumes() ([]docker.Volume, error) { return docker.ListVolumes() }

// DockerSrvCreateVolume 新建存储卷
func (a *App) DockerSrvCreateVolume(spec docker.VolumeSpec) (string, error) {
	return docker.CreateVolume(spec)
}

// DockerSrvRemoveVolume 删除存储卷（仍被容器占用时 daemon 拒绝）
func (a *App) DockerSrvRemoveVolume(name string) error { return docker.RemoveVolume(name) }

// DockerSrvPruneVolumes 清理没有任何容器引用的存储卷
func (a *App) DockerSrvPruneVolumes() (docker.VolumePruneReport, error) {
	return docker.PruneVolumes()
}

// DockerSrvListComposeProjects 按 compose 标签聚合出的编排项目（Engine API 没有 compose 端点）
func (a *App) DockerSrvListComposeProjects() ([]docker.ComposeProject, error) {
	return docker.ListComposeProjects()
}

// DockerSrvComposeAction 对编排项目批量操作：start | stop | restart | down
func (a *App) DockerSrvComposeAction(project, action string) (string, error) {
	return docker.ComposeAction(project, action)
}

// DockerSrvChooseFile 弹原生文件选择框（导入镜像包用），取消返回空串
func (a *App) DockerSrvChooseFile(title string) (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	if strings.TrimSpace(title) == "" {
		title = "选择文件"
	}
	return a.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AddFilter("镜像包 (*.tar)", "*.tar").
		SetTitle(title).
		PromptForSingleSelection()
}

// DockerSrvChooseSaveFile 弹原生保存框（导出镜像包用），取消返回空串
func (a *App) DockerSrvChooseSaveFile(title, defaultName string) (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	if strings.TrimSpace(title) == "" {
		title = "保存为"
	}
	return a.app.Dialog.SaveFile().
		AddFilter("镜像包 (*.tar)", "*.tar").
		SetFilename(defaultName).
		SetMessage(title).
		PromptForSingleSelection()
}

/* ======================== Docker 仓库、加速站与线上镜像 ======================== */

// DockerSrvListRegistries 已配置的私有仓库（不含密码，只标是否已保存）
func (a *App) DockerSrvListRegistries() []docker.Registry { return docker.ListRegistries() }

// DockerSrvSaveRegistry 新增/更新仓库；password 空=沿用旧密码，clearPass=true 才清空
func (a *App) DockerSrvSaveRegistry(r docker.Registry, password string, clearPass bool) (docker.Registry, error) {
	return docker.SaveRegistry(r, password, clearPass)
}

// DockerSrvDeleteRegistry 删除仓库配置
func (a *App) DockerSrvDeleteRegistry(id string) error { return docker.DeleteRegistry(id) }

// DockerSrvTestRegistry 用已保存凭据探一次仓库连通性
func (a *App) DockerSrvTestRegistry(id string) (string, error) { return docker.TestRegistry(id) }

// DockerSrvRegistryRepos 列出私有仓库里的镜像（catalog + tag）
func (a *App) DockerSrvRegistryRepos(id string) ([]docker.HubRepo, error) {
	return docker.RegistryRepos(id)
}

// DockerSrvSearchHub 在 Docker Hub 搜线上镜像（Star 数/来源/简介）
func (a *App) DockerSrvSearchHub(query string, page int) ([]docker.HubRepo, error) {
	return docker.SearchHub(query, page)
}

// DockerSrvDaemonConfig 读引擎 daemon.json 的加速站与不安全仓库
func (a *App) DockerSrvDaemonConfig() (docker.DaemonConfig, error) { return docker.GetDaemonConfig() }

// DockerSrvSetDaemonConfig 只改 registry-mirrors 与 insecure-registries，其余键原样保留并备份
func (a *App) DockerSrvSetDaemonConfig(mirrors, insecure []string) (string, error) {
	return docker.SetDaemonConfig(mirrors, insecure)
}

// DockerSrvRestartEngine 重启 Docker 引擎（改完 daemon.json 后让它生效）
func (a *App) DockerSrvRestartEngine() error { return docker.RestartEngine() }

// DockerSrvProbeMirror 探一个加速站能不能真下载：连通 → 清单 → 层数据三段计时。
// 不返回 error：探不通是结果（grade=fail/empty），不应混进绑定调用异常里。
func (a *App) DockerSrvProbeMirror(mirrorURL, sample string) docker.MirrorProbe {
	return docker.ProbeMirror(mirrorURL, sample)
}

// DockerSrvProbeMirrors 一次探完多个站（界面上的「全部测速」）
func (a *App) DockerSrvProbeMirrors(mirrorURLs []string, sample string) []docker.MirrorProbe {
	return docker.ProbeMirrors(mirrorURLs, sample)
}

// DockerSrvContainerLogs 读容器日志尾部 n 行（含已停止容器）
func (a *App) DockerSrvContainerLogs(id string, lines int) ([]string, error) {
	return docker.GetContainerLogs(id, lines)
}

// DockerSrvLogsFollow 开启日志跟随，新行经 "docker:serverlog" 事件推送（幂等）
func (a *App) DockerSrvLogsFollow(id string) error { return docker.StreamContainerLogs(id) }

// DockerSrvLogsStop 停止日志跟随（弹窗关闭时调用）
func (a *App) DockerSrvLogsStop(id string) { docker.StopContainerLogs(id) }

// DockerSrvLogsBuffered 取当前跟随环形缓冲（弹窗刚打开时补齐历史）
func (a *App) DockerSrvLogsBuffered(id string, lines int) []string {
	return docker.ContainerLogRing(id, lines)
}

/* ======================== Docker Desktop 安装向导 ======================== */

// DockerSrvLicenseNotes 安装前需如实告知的要点（前端展示在感叹号悬停提示里）
func (a *App) DockerSrvLicenseNotes() []string { return docker.LicenseNotes() }

// DockerSrvPluginsDir docker 默认存放目录（全局插件目录下的 docker，供前端展示“留空即用这里”）
func (a *App) DockerSrvPluginsDir() string {
	dir, err := appdirs.Plugins("docker")
	if err != nil {
		return ""
	}
	return dir
}

// DockerSrvCheckEnv 只读体检（不装不改）：已装/WSL/管道/引擎/离线包 + 待办清单；dir 空=默认存放目录
func (a *App) DockerSrvCheckEnv(dir string) (docker.EnvCheck, error) { return docker.CheckEnv(dir) }

// DockerSrvBuildInstallerURL 把用户输入（空/build 号/完整 URL）换算成直链，供展示
func (a *App) DockerSrvBuildInstallerURL(input string) string { return docker.BuildInstallerURL(input) }

// DockerSrvProbeInstaller 下载前探一次：直链有效性与真实体积（防把 404 页当安装包）
func (a *App) DockerSrvProbeInstaller(input string) (docker.ProbeInfo, error) {
	return docker.ProbeInstaller(input)
}

// DockerSrvDownloadInstaller 下载安装包（支持续传，取消会删掉半截文件），进度走 "docker:setup"；
// dir 为空时落到默认 <插件目录>\docker，否则用指定的另存目录。
func (a *App) DockerSrvDownloadInstaller(input, dir string) (string, error) {
	return docker.DownloadInstaller(input, dir)
}

// DockerSrvAbortSetup 取消进行中的下载并删除已下载的半截文件（安装器启动后无法安全中断，返回提示）
func (a *App) DockerSrvAbortSetup() error { return docker.AbortSetup() }

// DockerSrvInstallDocker 提权静默安装并拉起引擎；安装协议不设勾选门禁，要点由前端感叹号提示展示。
// installerPath 为空时按 dir（空=默认存放目录）找离线包，没有则按 input 下载；进度走 "docker:setup"。
func (a *App) DockerSrvInstallDocker(installerPath, input, dir string, webDownload bool) error {
	return docker.InstallDocker(installerPath, input, dir, webDownload)
}

// DockerSrvEnableWsl2 提权执行 wsl --install --no-distribution（可能需重启才生效）
func (a *App) DockerSrvEnableWsl2() error { return docker.EnableWsl2() }

// DockerSrvUpdateWsl 提权执行 wsl --update：WSL 组件在但内核过旧/损坏时，
// Docker Desktop 切 WSL2 会静默沿用 Hyper-V，这里是那个情形的修复入口
func (a *App) DockerSrvUpdateWsl() error { return docker.UpdateWsl() }

// DockerSrvStartEngine 已安装但引擎未运行时，以普通用户身份拉起并等待就绪（可被「停止」叫停）
func (a *App) DockerSrvStartEngine() error { return docker.StartEngine() }

// DockerSrvStopEngine 停止 Docker Desktop 与其系统服务（会叫停在跑的启动等待）
func (a *App) DockerSrvStopEngine() error { return docker.StopEngine() }

// DockerSrvSwitchBackend 代改 Docker Desktop 虚拟化后端（true=WSL2，false=Hyper-V）。
// 会先停 Docker Desktop 再改配置并回读校验；引擎起来后还会用内核串核验实际后端，
// 回落到了另一个后端时报错而不是谎报成功。confirmed 必须为 true（切换会新建空 VM）。
func (a *App) DockerSrvSwitchBackend(useWsl2, confirmed bool) error {
	return docker.SwitchBackend(useWsl2, confirmed)
}

// DockerSrvEnableHyperV 提权启用 Hyper-V 角色（dism，需重启才生效）；confirm 为硬门禁
func (a *App) DockerSrvEnableHyperV(confirm bool) error { return docker.EnableHyperV(confirm) }

// DockerSuggestPort 从 from 起探测一个可用的宿主机端口（跳过实例登记与系统监听）
func (a *App) DockerSuggestPort(from int) int { return portcheck.Suggest(from, 200) }

// DockerPortTaken 报告宿主机端口能否发布容器端口，返回占用原因，空串表示可用
func (a *App) DockerPortTaken(port int) string { return portcheck.Taken(port, "") }

/* ============================ PHP 本地服务管理 ============================ */

// PhpSrvListVersions 聚合三渠道本地可用 PHP 版本
func (a *App) PhpSrvListVersions() ([]php.ServerVersion, error) { return php.ListVersions() }

// PhpSrvRemoteVersions 拉取线上可安装版本（windows.php.net）
func (a *App) PhpSrvRemoteVersions() ([]php.RemoteVersion, error) { return php.FetchRemoteVersions() }

// PhpSrvInstall 异步安装 PHP 版本（nts=true 下载 NTS 构型），进度经 php:install 事件推送
func (a *App) PhpSrvInstall(version string, nts bool) error { return php.InstallVersion(version, nts) }

// PhpSrvRemoveVersion 删除版本（内置不可删，运行中实例引用时拒绝）
func (a *App) PhpSrvRemoveVersion(version string) error { return php.RemoveVersion(version) }

// PhpSrvAddLocal 注册本机已有的 PHP 目录（含 php.exe）
func (a *App) PhpSrvAddLocal(dir string) error { return php.AddLocalVersion(dir) }

// PhpSrvChooseDir 弹原生目录选择框（供添加本机 PHP 目录），取消返回空串
func (a *App) PhpSrvChooseDir() (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	dir, err := a.app.Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		AllowsOtherFileTypes(false).
		SetTitle("选择 php.exe 所在目录").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return dir, nil
}

// PhpSrvListInstances 列出全部 PHP 实例配置
func (a *App) PhpSrvListInstances() ([]php.InstanceConfig, error) { return php.ListInstances() }

// PhpSrvSaveInstance 新增/更新实例配置（ID 为空则新建）
func (a *App) PhpSrvSaveInstance(cfg php.InstanceConfig) error { return php.SaveInstance(cfg) }

// PhpSrvRemoveInstance 删除实例配置（运行中先停止）
func (a *App) PhpSrvRemoveInstance(id string) error { return php.RemoveInstance(id) }

// PhpSrvStart 启动实例（本地：php -S 内置服务器隐藏窗口；Docker：创建/启动容器）
func (a *App) PhpSrvStart(id string) error { return php.StartInstance(id) }

// PhpSrvStop 停止实例（本地：kill 进程树；Docker：停止容器）
func (a *App) PhpSrvStop(id string) error { return php.StopInstance(id) }

// PhpSrvRestart 重启实例
func (a *App) PhpSrvRestart(id string) error { return php.RestartInstance(id) }

// PhpSrvStatus 实例运行状态（端口探测 + ini 概览）
func (a *App) PhpSrvStatus(id string) (php.InstanceStatus, error) { return php.GetInstanceStatus(id) }

// PhpSrvLogs 返回实例最近 n 行日志
func (a *App) PhpSrvLogs(id string, lines int) ([]string, error) { return php.Logs(id, lines) }

// PhpSrvSuggestPort 从 from 起探测可用端口（跳过其它实例已登记与系统已监听的）
func (a *App) PhpSrvSuggestPort(from int) int { return php.SuggestPort(from) }

// PhpSrvPortTaken 报告端口能否给实例用（任意工具实例已登记或进程监听），返回占用原因，空串表示可用
func (a *App) PhpSrvPortTaken(port int, excludeId string) string {
	return php.PortTaken(port, excludeId)
}

// PhpSrvDockerImages 本地 php 系镜像 tag（Docker 模式表单候选）
func (a *App) PhpSrvDockerImages() ([]string, error) { return php.DockerImages() }

// PhpSrvDefaultImage Docker 模式默认镜像
func (a *App) PhpSrvDefaultImage() string { return php.DefaultDockerImage }

// PhpSrvDockerAvailable Docker 引擎是否就绪（不可用时返回原因，仅提示不阻断保存）
func (a *App) PhpSrvDockerAvailable() (bool, string) { return php.DockerAvailable() }

// PhpSrvPluginsDir 返回插件目录 <exedir>/plugins/php，供前端展示指引
func (a *App) PhpSrvPluginsDir() string {
	dir, err := php.PluginsDir()
	if err != nil {
		return ""
	}
	return dir
}

// PhpSrvGetConfigText 读取实例 php.ini 全文
func (a *App) PhpSrvGetConfigText(id string) (string, error) { return php.GetConfigText(id) }

// PhpSrvSaveConfigText 保存实例 php.ini（先备份后校验，加载失败回滚）
func (a *App) PhpSrvSaveConfigText(id, text string) error { return php.SaveConfigText(id, text) }

// PhpSrvGetCommon 读取实例常用配置项（内存/上传/时区等）
func (a *App) PhpSrvGetCommon(id string) (*php.CommonFields, error) { return php.GetCommon(id) }

// PhpSrvSetCommon 写入实例常用配置项
func (a *App) PhpSrvSetCommon(id string, f php.CommonFields) error { return php.SetCommon(id, f) }

// PhpSrvExtensions 列出实例候选扩展（可用/已启用/已加载）
func (a *App) PhpSrvExtensions(id string) ([]php.ExtensionInfo, error) { return php.Extensions(id) }

// PhpSrvSetExtension 启用/禁用扩展，返回提示信息
func (a *App) PhpSrvSetExtension(id, name string, enable bool) (string, error) {
	return php.SetExtension(id, name, enable)
}

// PhpSrvLoadedExtensions 实测已加载扩展名列表（php -m）
func (a *App) PhpSrvLoadedExtensions(id string) ([]string, error) { return php.LoadedExtensions(id) }

// PhpSrvExtCatalog 常用扩展在线清单（PECL）：已安装 / 可下载 / 下载中
func (a *App) PhpSrvExtCatalog(id string, force bool) ([]php.RemoteExtensionInfo, error) {
	return php.ExtCatalog(id, force)
}

// PhpSrvExtRefresh 清空 PECL 线上索引缓存（配合 PhpSrvExtCatalog(force=true) 使用）
func (a *App) PhpSrvExtRefresh() { php.RefreshExtCatalog() }

// PhpSrvExtInstall 异步下载安装扩展 dll 到版本 ext 目录，enable=true 顺带启用；进度经 php:ext-install 事件推送
func (a *App) PhpSrvExtInstall(id, ext string, enable bool) error {
	return php.InstallExtension(id, ext, enable)
}

// PhpSrvExtRemove 删除由本工具下载的扩展 dll（内置扩展不可删）
func (a *App) PhpSrvExtRemove(id, ext string) error { return php.RemoveExtension(id, ext) }

// PhpSrvDisableFunctions 读取实例禁用函数列表
func (a *App) PhpSrvDisableFunctions(id string) ([]string, error) { return php.DisableFunctions(id) }

// PhpSrvSetDisableFunctions 全量设置禁用函数列表
func (a *App) PhpSrvSetDisableFunctions(id string, list []string) error {
	return php.SetDisableFunctions(id, list)
}

// PhpSrvAddDisableFunctions 追加禁用函数
func (a *App) PhpSrvAddDisableFunctions(id string, names []string) error {
	return php.AddDisableFunctions(id, names)
}

// PhpSrvRemoveDisableFunctions 移除若干禁用函数
func (a *App) PhpSrvRemoveDisableFunctions(id string, names []string) error {
	return php.RemoveDisableFunctions(id, names)
}

// PhpSrvCommonlyDisabled 返回常见建议禁用函数列表（供前端快捷添加）
func (a *App) PhpSrvCommonlyDisabled() []string { return php.CommonlyDisabled() }

// PhpSrvInfo 返回实例 php -i 关键配置概览
func (a *App) PhpSrvInfo(id string) (*php.PHPInfo, error) { return php.Info(id) }

/* ============================ Nginx 本地服务管理 ============================ */

// NginxSrvListVersions 聚合三渠道本地可用 Nginx 版本
func (a *App) NginxSrvListVersions() ([]nginx.ServerVersion, error) { return nginx.ListVersions() }

// NginxSrvRemoteVersions 拉取线上可安装版本（nginx.org 官方 zip）
func (a *App) NginxSrvRemoteVersions() ([]nginx.RemoteVersion, error) {
	return nginx.FetchRemoteVersions()
}

// NginxSrvInstall 异步安装 Nginx 版本，进度经 nginx:install 事件推送
func (a *App) NginxSrvInstall(version string) error { return nginx.InstallVersion(version) }

// NginxSrvRemoveVersion 删除版本（内置不可删，运行中实例引用时拒绝）
func (a *App) NginxSrvRemoveVersion(version string) error { return nginx.RemoveVersion(version) }

// NginxSrvAddLocal 注册本机已有的 Nginx 目录（含 nginx.exe）
func (a *App) NginxSrvAddLocal(dir string) error { return nginx.AddLocalVersion(dir) }

// NginxSrvChooseDir 弹原生目录选择框（供添加本机 Nginx 目录），取消返回空串
func (a *App) NginxSrvChooseDir() (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	dir, err := a.app.Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		AllowsOtherFileTypes(false).
		SetTitle("选择 nginx.exe 所在目录").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return dir, nil
}

// NginxSrvListInstances 列出全部 Nginx 实例配置
func (a *App) NginxSrvListInstances() ([]nginx.InstanceConfig, error) { return nginx.ListInstances() }

// NginxSrvSaveInstance 新增/更新实例配置（ID 为空则新建）
func (a *App) NginxSrvSaveInstance(cfg nginx.InstanceConfig) error { return nginx.SaveInstance(cfg) }

// NginxSrvRemoveInstance 删除实例配置（运行中先停止）
func (a *App) NginxSrvRemoveInstance(id string) error { return nginx.RemoveInstance(id) }

// NginxSrvStart 启动实例（-p prefix -c conf，隐藏窗口）
func (a *App) NginxSrvStart(id string) error { return nginx.StartInstance(id) }

// NginxSrvStop 停止实例（nginx -s quit + 兑底 kill）
func (a *App) NginxSrvStop(id string) error { return nginx.StopInstance(id) }

// NginxSrvRestart 重启实例
func (a *App) NginxSrvRestart(id string) error { return nginx.RestartInstance(id) }

// NginxSrvReload 平滑重载配置
func (a *App) NginxSrvReload(id string) error { return nginx.ReloadInstance(id) }

// NginxSrvCheckConfig 执行 nginx -t 配置检查，返回输出文本
func (a *App) NginxSrvCheckConfig(id string) (string, error) { return nginx.CheckConfig(id) }

// NginxSrvStatus 实例运行状态（端口探测 + master/worker 进程）
func (a *App) NginxSrvStatus(id string) (nginx.InstanceStatus, error) {
	return nginx.GetInstanceStatus(id)
}

// NginxSrvLogs 返回实例最近 n 行日志
func (a *App) NginxSrvLogs(id string, lines int) ([]string, error) { return nginx.Logs(id, lines) }

// NginxSrvSuggestPort 从 from 起探测可用端口
func (a *App) NginxSrvSuggestPort(from int) int { return nginx.SuggestPort(from) }

// NginxSrvPortTaken 报告端口能否给实例用（含其它实例的 HTTP/HTTPS 登记与系统监听），
// 返回占用原因，空串表示可用
func (a *App) NginxSrvPortTaken(port int, excludeId string) string {
	return nginx.PortTaken(port, excludeId)
}

// NginxSrvPluginsDir 返回插件目录 <exedir>/plugins/nginx，供前端展示指引
func (a *App) NginxSrvPluginsDir() string {
	dir, err := nginx.PluginsDir()
	if err != nil {
		return ""
	}
	return dir
}

// NginxSrvGetConfigText 读取实例 nginx.conf 全文
func (a *App) NginxSrvGetConfigText(id string) (string, error) { return nginx.GetConfigText(id) }

// NginxSrvSaveConfigText 保存实例 nginx.conf（先备份后校验，失败回滚；容器模式不做 -t 不回滚）
func (a *App) NginxSrvSaveConfigText(id, text string) error { return nginx.SaveConfigText(id, text) }

// NginxSrvDockerImages 本地 nginx 系镜像 tag（Docker 模式表单候选）
func (a *App) NginxSrvDockerImages() ([]string, error) { return nginx.DockerImages() }

// NginxSrvDefaultImage Docker 模式默认镜像
func (a *App) NginxSrvDefaultImage() string { return nginx.DefaultDockerImage }

// NginxSrvDockerAvailable Docker 引擎是否就绪（不可用时返回原因，仅提示不阻断保存）
func (a *App) NginxSrvDockerAvailable() (bool, string) { return nginx.DockerAvailable() }

/* ============================ npm 本地服务管理 ============================ */

// NpmSrvListVersions 聚合三渠道本地可用 Node.js 版本
func (a *App) NpmSrvListVersions() ([]npm.ServerVersion, error) { return npm.ListVersions() }

// NpmSrvRemoteVersions 拉取线上可安装版本（nodejs.org dist index.json）
func (a *App) NpmSrvRemoteVersions() ([]npm.RemoteVersion, error) { return npm.FetchRemoteVersions() }

// NpmSrvInstall 异步安装 Node.js 版本，进度经 npm:install 事件推送
func (a *App) NpmSrvInstall(version string) error { return npm.InstallVersion(version) }

// NpmSrvRemoveVersion 删除版本（内置不可删，运行中实例引用时拒绝）
func (a *App) NpmSrvRemoveVersion(version string) error { return npm.RemoveVersion(version) }

// NpmSrvAddLocal 注册本机已有的 Node.js 目录（含 node.exe）
func (a *App) NpmSrvAddLocal(dir string) error { return npm.AddLocalVersion(dir) }

// NpmSrvScanLocal 扫描本机已安装的 Node.js（PATH / 注册表 / nvm 等）并登记为本机来源，
// 返回本次新增登记的版本（已登记过的不重复计入）
func (a *App) NpmSrvScanLocal() ([]npm.ServerVersion, error) { return npm.ScanLocalNodes() }

// NpmSrvChooseDir 弹原生目录选择框（供添加本机 Node.js 目录），取消返回空串
func (a *App) NpmSrvChooseDir() (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	dir, err := a.app.Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		AllowsOtherFileTypes(false).
		SetTitle("选择 node.exe 所在目录").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return dir, nil
}

// NpmSrvListInstances 列出全部静态站点实例配置
func (a *App) NpmSrvListInstances() ([]npm.InstanceConfig, error) { return npm.ListInstances() }

// NpmSrvSaveInstance 新增/更新实例配置（ID 为空则新建）
func (a *App) NpmSrvSaveInstance(cfg npm.InstanceConfig) error { return npm.SaveInstance(cfg) }

// NpmSrvRemoveInstance 删除实例配置（运行中先停止）
func (a *App) NpmSrvRemoveInstance(id string) error { return npm.RemoveInstance(id) }

// NpmSrvStart 启动静态站点（本地：内置 Go HTTP 服务器；Docker：nginx 容器）
func (a *App) NpmSrvStart(id string) error { return npm.StartInstance(id) }

// NpmSrvStop 停止静态站点（本地：优雅 Shutdown；Docker：停容器）
func (a *App) NpmSrvStop(id string) error { return npm.StopInstance(id) }

// NpmSrvRestart 重启实例（容器模式没有平滑重载通道，重启即重启容器）
func (a *App) NpmSrvRestart(id string) error { return npm.RestartInstance(id) }

// NpmSrvStatus 实例运行状态（端口探测 + 请求计数）
func (a *App) NpmSrvStatus(id string) (npm.InstanceStatus, error) { return npm.GetInstanceStatus(id) }

// NpmSrvLogs 返回实例最近 n 行访问日志
func (a *App) NpmSrvLogs(id string, lines int) ([]string, error) { return npm.Logs(id, lines) }

// NpmSrvSuggestPort 从 from 起探测可用端口
func (a *App) NpmSrvSuggestPort(from int) int { return npm.SuggestPort(from) }

// NpmSrvPortTaken 报告端口是否已被占用（其它站点登记或进程监听），返回占用原因，空串表示可用
func (a *App) NpmSrvPortTaken(port int, excludeId string) string {
	return npm.PortTaken(port, excludeId)
}

// NpmSrvPluginsDir 返回插件目录 <exedir>/plugins/npm，供前端展示指引
func (a *App) NpmSrvPluginsDir() string {
	dir, err := npm.PluginsDir()
	if err != nil {
		return ""
	}
	return dir
}

// NpmSrvDockerImages 本地 nginx 系镜像 tag（静态站点 Docker 模式表单候选）
func (a *App) NpmSrvDockerImages() ([]string, error) { return npm.DockerImages() }

// NpmSrvDefaultImage Docker 模式默认镜像
func (a *App) NpmSrvDefaultImage() string { return npm.DefaultDockerImage }

// NpmSrvDockerAvailable Docker 引擎是否就绪（不可用时返回原因，仅提示不阻断保存）
func (a *App) NpmSrvDockerAvailable() (bool, string) { return npm.DockerAvailable() }

// NpmGetConfigText 读取托管 .npmrc 全文
func (a *App) NpmGetConfigText() (string, error) { return npm.GetConfigText() }

// NpmSaveConfigText 保存托管 .npmrc
func (a *App) NpmSaveConfigText(text string) error { return npm.SaveConfigText(text) }

// NpmConfigItems 列出托管 .npmrc 配置键值
func (a *App) NpmConfigItems() ([]npm.NpmConfigKV, error) { return npm.ConfigItems() }

// NpmSetConfigItem 设置单条 npm 配置
func (a *App) NpmSetConfigItem(key, value string) error { return npm.SetConfigItem(key, value) }

// NpmGetRegistry 返回当前生效的 registry
func (a *App) NpmGetRegistry() string { return npm.GetRegistry() }

// NpmSetRegistry 设置 registry
func (a *App) NpmSetRegistry(url string) error { return npm.SetRegistry(url) }

// NpmCommonRegistries 返回常用镜像源列表（供前端快捷选择）
func (a *App) NpmCommonRegistries() []npm.NpmConfigKV { return npm.CommonRegistries() }

// NpmUserNpmrc 返回用户全局 ~/.npmrc 的路径/内容/是否存在，并附带托管 registry 同步能力
func (a *App) NpmUserNpmrc() (string, string, bool) { return npm.UserNpmrc() }

// NpmApplyToUserNpmrc 将托管配置同步到用户全局 ~/.npmrc（先备份），返回备份文件路径
func (a *App) NpmApplyToUserNpmrc() (string, error) { return npm.ApplyToUserNpmrc() }

// NpmGlobalPackages 列出指定 Node 版本的全局已安装包
func (a *App) NpmGlobalPackages(version string) ([]npm.NpmPackage, error) {
	return npm.GlobalPackages(version)
}

// NpmInstallGlobal 异步安装全局包，进度经 npm:install 事件推送
func (a *App) NpmInstallGlobal(version, pkg string) error { return npm.InstallGlobal(version, pkg) }

// NpmUninstallGlobal 异步卸载全局包，进度经 npm:install 事件推送
func (a *App) NpmUninstallGlobal(version, pkg string) error { return npm.UninstallGlobal(version, pkg) }

// NpmVersionOf 返回指定 Node 版本配套的 npm 版本号
func (a *App) NpmVersionOf(version string) string { return npm.NpmVersion(version) }

/* ============================ 包管理器与全局包扫描 ============================ */

// NpmPackageManagers 探测本机可用的包管理器（随 Node 发行的 npm / corepack，
// 全局安装的 yarn / pnpm / cnpm，原生安装的 bun）
func (a *App) NpmPackageManagers() ([]npm.PackageManager, error) { return npm.ListPackageManagers() }

// NpmScanGlobalPackages 扫描各包管理器的全局安装根，汇总本机已装的包
// （直读磁盘 package.json，不起子进程，npm CLI 坏了也能列出来）
func (a *App) NpmScanGlobalPackages() ([]npm.NpmPackage, error) { return npm.ScanGlobalPackages() }

// NpmInstallWith 用指定包管理器全局安装（自动适配 yarn global add / pnpm add -g 等语法）
func (a *App) NpmInstallWith(manager, pkg string) error { return npm.InstallGlobalWith(manager, pkg) }

// NpmUninstallWith 用指定包管理器全局卸载；installedAt 传入该包的磁盘目录，用于反查装它的管理器
func (a *App) NpmUninstallWith(manager, pkg, installedAt string) error {
	return npm.UninstallGlobalWith(manager, pkg, installedAt)
}

// NpmManagerVersion 执行一次 <管理器> --version 校准版本号
func (a *App) NpmManagerVersion(manager string) (string, error) {
	return npm.RunManagerVersion(manager)
}

/* ============================ npm 本体版本与命令行生效链 ============================ */

// NpmCliChain 算出「cmd 里敲 npm 到底执行哪一份 npm」：PATH × PATHEXT 命中的转发壳，
// 加上它内部会跳转的全局 prefix 覆盖层，返回完整生效链与诊断提示
func (a *App) NpmCliChain() (*npm.NpmChain, error) { return npm.InspectNpmChain() }

// NpmCliReleases 当前 registry 上可用的 npm 版本清单（附带与指定 Node 的兼容性判定）
func (a *App) NpmCliReleases(nodeVersion string) (*npm.NpmReleaseList, error) {
	return npm.FetchNpmVersions(nodeVersion)
}

// NpmCliRefreshReleases 清缓存重拉版本清单（切换失败或刚改过镜像源后用）
func (a *App) NpmCliRefreshReleases(nodeVersion string) (*npm.NpmReleaseList, error) {
	return npm.RefreshNpmVersions(nodeVersion)
}

// NpmCliSwitch 把指定层的 npm 换成指定版本，进度经 npm:npmver 事件推送。
// layer = bundled（Node 自带层）| override（全局 prefix 覆盖层）；entryDir 为入口目录。
// Go 直接下 tarball 解压替换，不经过任何 npm CLI，避开自我覆盖与 PATH 权限问题。
func (a *App) NpmCliSwitch(layer, entryDir, npmVersion string) error {
	return npm.SwitchNpm(layer, entryDir, npmVersion)
}

// NpmCliUndoSwitch 撤销上一步切换，回 .npm-prev 里留存的旧版本
func (a *App) NpmCliUndoSwitch(layer, entryDir string) error {
	return npm.RollbackNpm(layer, entryDir)
}

// NpmCliRemoveOverride 删掉 prefix 覆盖层的 npm，让 cmd 回到该 Node 自带的版本
func (a *App) NpmCliRemoveOverride(entryDir string) error { return npm.RemoveNpmOverride(entryDir) }

// NpmPathStatus 读取当前用户 PATH 的 Node 绑定情况（强制切换本地 npm 命令）
func (a *App) NpmPathStatus() (*npm.NpmPathBind, error) { return npm.PathBindStatus() }

// NpmPathBind 把选定 Node 目录前置到 PATH 并移除其它 Node 目录；system=true 改系统 PATH（需管理员），否则改用户 PATH
func (a *App) NpmPathBind(entryDir string, system bool) error {
	return npm.BindNodeToPath(entryDir, system)
}

// NpmPathUnbind 恢复绑定前备份的用户 PATH
func (a *App) NpmPathUnbind() (string, error) { return npm.UndoPathBind() }

// AppDataDir 返回全局数据目录下的服务子目录（不存在则创建），如 "videos" → <数据目录>/videos；
// 数据基础目录默认 <程序目录>/data，设置页修改后新落点随即生效
func (a *App) AppDataDir(sub string) (string, error) {
	sub = filepath.Base(filepath.FromSlash(strings.TrimSpace(sub)))
	if sub == "" || sub == "." || sub == ".." {
		return "", fmt.Errorf("非法数据子目录名")
	}
	return appdirs.Data(sub)
}

/* ============================ MySQL 管理 ============================ */

// MySQLOpen 建立并登记一个 MySQL 连接
func (a *App) MySQLOpen(cfg mysql.ConnConfig) error { return mysql.Open(cfg) }

// MySQLClose 关闭指定连接
func (a *App) MySQLClose(id string) error { return mysql.Close(id) }

// MySQLListDatabases 列出数据库
func (a *App) MySQLListDatabases(id string) ([]string, error) { return mysql.ListDatabases(id) }

// MySQLListTables 列出基表
func (a *App) MySQLListTables(id, database string) ([]mysql.TableInfo, error) {
	return mysql.ListTables(id, database)
}

// MySQLListViews 列出视图
func (a *App) MySQLListViews(id, database string) ([]mysql.ViewInfo, error) {
	return mysql.ListViews(id, database)
}

// MySQLListRoutines 列出函数/存储过程
func (a *App) MySQLListRoutines(id, database string) ([]mysql.RoutineInfo, error) {
	return mysql.ListRoutines(id, database)
}

// MySQLTableSchema 返回表结构（列/索引/DDL）
func (a *App) MySQLTableSchema(id, database, table string) (mysql.TableSchema, error) {
	return mysql.GetTableSchema(id, database, table)
}

// MySQLAddColumn 新增列
func (a *App) MySQLAddColumn(id, database, table string, col mysql.ColumnDef) error {
	return mysql.AddColumn(id, database, table, col)
}

// MySQLModifyColumn 修改/重命名列
func (a *App) MySQLModifyColumn(id, database, table, oldName string, col mysql.ColumnDef) error {
	return mysql.ModifyColumn(id, database, table, oldName, col)
}

// MySQLDropColumn 删除列
func (a *App) MySQLDropColumn(id, database, table, col string) error {
	return mysql.DropColumn(id, database, table, col)
}

// MySQLTableData 分页浏览表数据
func (a *App) MySQLTableData(id, database, table, filter, orderBy string, desc bool, page, size int) (mysql.QueryResult, error) {
	return mysql.TableData(id, database, table, filter, orderBy, desc, page, size)
}

// MySQLUpdateCell 按主键更新单元格
func (a *App) MySQLUpdateCell(id, database, table, pkCol, pkVal, col, val string) error {
	return mysql.UpdateCell(id, database, table, pkCol, pkVal, col, val)
}

// MySQLInsertRow 插入一行
func (a *App) MySQLInsertRow(id, database, table string, data map[string]string) (int64, error) {
	return mysql.InsertRow(id, database, table, data)
}

// MySQLDeleteRow 按主键删除一行
func (a *App) MySQLDeleteRow(id, database, table, pkCol, pkVal string) error {
	return mysql.DeleteRow(id, database, table, pkCol, pkVal)
}

// MySQLExecute 执行 SQL（查询返回结果集，其余返回受影响行）
func (a *App) MySQLExecute(id, database, sqlText string) (mysql.QueryResult, error) {
	return mysql.Execute(id, database, sqlText)
}

// MySQLBeginTx 开启事务，返回事务 ID
func (a *App) MySQLBeginTx(id, database string) (string, error) { return mysql.BeginTx(id, database) }

// MySQLTxExecute 在事务中执行语句
func (a *App) MySQLTxExecute(txID, sqlText string) (mysql.QueryResult, error) {
	return mysql.TxExecute(txID, sqlText)
}

// MySQLCommitTx 提交事务
func (a *App) MySQLCommitTx(txID string) error { return mysql.CommitTx(txID) }

// MySQLRollbackTx 回滚事务
func (a *App) MySQLRollbackTx(txID string) error { return mysql.RollbackTx(txID) }

// MySQLBackup 生成 SQL 备份文本
func (a *App) MySQLBackup(id, database string, tables []string) (string, error) {
	return mysql.Backup(id, database, tables)
}

// MySQLSaveConnections 保存连接配置到文件
func (a *App) MySQLSaveConnections(configs []mysql.ConnConfig) (string, error) {
	return mysql.SaveConnections(configs)
}

// MySQLLoadConnections 从文件读取连接列表
func (a *App) MySQLLoadConnections() ([]mysql.ConnConfig, error) { return mysql.LoadConnections() }

// MySQLDeleteRows 按单列主键批量删除多行，返回受影响行数
func (a *App) MySQLDeleteRows(id, database, table, pkCol string, pkVals []string) (int64, error) {
	return mysql.DeleteRows(id, database, table, pkCol, pkVals)
}

/* ---------------- MySQL SQL 执行日志（工具级设置） ---------------- */

// MySQLGetSQLLogSettings 返回 SQL 日志开关、目录与现存文件列表
func (a *App) MySQLGetSQLLogSettings() mysql.SQLLogSettings {
	st := mysql.SQLLogStatus()
	st.Dir = config.CurrentConfig().ToolSettings["mysql"].SQLLogDir
	return st
}

// MySQLSetSQLLogSettings 保存 SQL 日志开关与目录（立即生效并落盘），返回实际写入目录
func (a *App) MySQLSetSQLLogSettings(enabled bool, dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	resolved := mysql.ConfigureSQLLog(enabled, dir)

	_, err := config.Update(func(cfg *config.AppConfig) {
		if cfg.ToolSettings == nil {
			cfg.ToolSettings = map[string]config.ToolSettings{}
		}
		cfg.ToolSettings["mysql"] = config.ToolSettings{SQLLogEnabled: enabled, SQLLogDir: dir}
	})
	if err != nil {
		applog.Errorf("mysql", "保存 SQL 日志设置失败: %v", err)
		return resolved, err
	}
	applog.Infof("mysql", "SQL 日志设置已更新: enabled=%v dir=%s", enabled, resolved)
	return resolved, nil
}

// MySQLChooseSQLLogDir 弹出原生目录选择框（取消返回空串）
func (a *App) MySQLChooseSQLLogDir() (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	dir, err := a.app.Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		AllowsOtherFileTypes(false).
		SetTitle("选择 SQL 日志目录").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return dir, nil
}

// MySQLOpenSQLLogDir 在资源管理器中打开 SQL 日志目录
func (a *App) MySQLOpenSQLLogDir() error {
	dir := mysql.SQLLogStatus().ResolvedDir
	if _, err := os.Stat(dir); err != nil {
		_ = os.MkdirAll(dir, 0o755)
	}
	cmd := exec.Command("explorer.exe", dir)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		applog.Errorf("mysql", "打开 SQL 日志目录失败: %v", err)
		return err
	}
	return nil
}

// MySQLClearSQLLog 清空 SQL 日志文件，返回删除的文件数
func (a *App) MySQLClearSQLLog() (int, error) {
	n, err := mysql.ClearSQLLog()
	if err != nil {
		applog.Errorf("mysql", "清空 SQL 日志失败: %v", err)
		return n, err
	}
	applog.Infof("mysql", "已清空 SQL 日志: %d 个文件", n)
	return n, nil
}

/* ============================ 视频录制 ============================ */

// VideoOpenWidget 打开录制悬浮小窗（由视频录制工具页在用户确认共享、录制器已启动后调用）。
// 录制期间会隐藏视频录制工具窗口，避免其入镜形成递归镜像（“无限缩小”）。
// mode: screen|region；fx/fy/fw/fh 为区域相对整屏的裁剪比例（screen 模式忽略）；
// anchorX/anchorY 为小窗锚定的屏幕右下角坐标（DIP）；filename 为建议保存文件名。
func (a *App) VideoOpenWidget(mode string, fps int, fx, fy, fw, fh float64, anchorX, anchorY int, filename string) error {
	if a.app == nil {
		return fmt.Errorf("应用尚未初始化")
	}
	if fps <= 0 {
		fps = 30
	}
	const widgetW, widgetH = 340, 76
	x := anchorX - widgetW - 12
	y := anchorY - widgetH - 12
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}

	// 隐藏（而非关闭）发起录制的工具窗口：录制完成后由 VideoWidgetDone 恢复显示
	if w, ok := a.app.Window.GetByName("win:/win/tools/video"); ok {
		a.recToolWin = w
		w.Hide()
	}

	// 小窗单例：已存在则先关闭重建
	a.recMu.Lock()
	if a.recWidget != nil {
		a.recWidget.Close()
		a.recWidget = nil
	}
	a.recMu.Unlock()

	url := fmt.Sprintf("/#/widget/recorder?mode=%s&fps=%d&fx=%g&fy=%g&fw=%g&fh=%g&name=%s",
		mode, fps, fx, fy, fw, fh, filename)
	w := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "win:recwidget",
		Title:            "屏幕录制",
		Width:            widgetW,
		Height:           widgetH,
		X:                x,
		Y:                y,
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		StartState:       application.WindowStateNormal,
		BackgroundColour: application.NewRGB(24, 24, 28),
		URL:              url,
		Windows: application.WindowsWindow{
			// 无边框小窗支持前端 app-region: drag 拖动
			NonClientRegionSupport: true,
		},
	})
	a.recMu.Lock()
	a.recWidget = w
	a.recMu.Unlock()
	return nil
}

// VideoWidgetDone 小窗退出：重新显示工具窗口并推送退出事件（保留 recToolWin 引用供另存对话框挂父窗）
func (a *App) VideoWidgetDone(savePath string) {
	if a.app == nil {
		return
	}
	a.recMu.Lock()
	if a.recWidget != nil {
		a.recWidget.Close()
		a.recWidget = nil
	}
	a.recMu.Unlock()
	if a.recToolWin != nil {
		a.recToolWin.Show()
		a.recToolWin.UnMinimise()
		a.recToolWin.Focus()
	}
	a.app.Event.Emit("video:saved", savePath)
}

// VideoCmd 悬浮小窗 → 录制工具窗的命令转发（start|pause|resume|stop|cancel）
func (a *App) VideoCmd(command string) {
	if a.app == nil {
		return
	}
	a.app.Event.Emit("video:cmd", command)
}

// VideoReport 录制工具窗 → 悬浮小窗的状态上报
func (a *App) VideoReport(state string, elapsed int, note string) {
	if a.app == nil {
		return
	}
	a.app.Event.Emit("video:status", VideoStatus{State: state, Elapsed: elapsed, Note: note})
}

/* ============================ Go 端消息提示（原生桌面通知小窗） ============================ */

// UIMessage 通知载荷（预留给 WebView 内桥接展示；当前 ShowMessage 走原生小窗，不再使用此事件）
type UIMessage struct {
	Level    string `json:"level"`    // info|success|warning|error
	Text     string `json:"text"`     // 提示内容
	Duration int    `json:"duration"` // 毫秒，0 = 前端默认
}

// ShowMessage 在 WebView 之外的桌面（屏幕右上角）弹出一个原生置顶通知小窗，
// 自动消失；同一时间仅保留最新一条通知。
func (a *App) ShowMessage(level string, text string) {
	if a.app == nil {
		return
	}
	if level == "" {
		level = "info"
	}
	const toastW, toastH = 360, 64
	// 屏幕右下角（与系统通知位置一致，基于含 DPI 换算的工作区，避开任务栏）
	_, _, waRight, waBottom := workAreaDIP()
	x := waRight - toastW - 16
	y := waBottom - toastH - 16
	if x < 16 {
		x = 16
	}
	if y < 16 {
		y = 16
	}

	// 新通知替换旧通知
	a.toastMu.Lock()
	if a.toastWin != nil {
		a.toastWin.Close()
		a.toastWin = nil
	}
	a.toastMu.Unlock()

	w := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             fmt.Sprintf("win:toast-%d", time.Now().UnixNano()),
		Title:            "通知",
		Width:            toastW,
		Height:           toastH,
		X:                x,
		Y:                y,
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		StartState:       application.WindowStateNormal,
		BackgroundColour: application.NewRGB(255, 255, 255),
		URL: fmt.Sprintf("/#/widget/toast?level=%s&text=%s",
			url.QueryEscape(level), url.QueryEscape(text)),
		Windows: application.WindowsWindow{
			// 无边框通知窗支持前端 app-region: drag 拖动
			NonClientRegionSupport: true,
		},
	})
	a.toastMu.Lock()
	a.toastWin = w
	a.toastMu.Unlock()
}

// ToastClose 关闭通知小窗（通知页倒计时结束或用户点关闭时调用）
func (a *App) ToastClose() {
	if a.app == nil {
		return
	}
	a.toastMu.Lock()
	if a.toastWin != nil {
		a.toastWin.Close()
		a.toastWin = nil
	}
	a.toastMu.Unlock()
}

// workAreaDIP 返回主显示器工作区（已排除任务栏）的 DIP 坐标（left, top, right, bottom），
// 按系统 DPI 将物理像素换算为 Wails 窗口使用的 DIP
func workAreaDIP() (int, int, int, int) {
	user32 := windows.NewLazySystemDLL("user32.dll")
	dpi := uintptr(96)
	if r, _, _ := user32.NewProc("GetDpiForSystem").Call(); r > 0 {
		dpi = r
	}
	scale := float64(dpi) / 96.0
	var rc struct {
		Left, Top, Right, Bottom int32
	}
	const spiGetWorkArea = 0x0030
	user32.NewProc("SystemParametersInfoW").Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&rc)), 0)
	return int(float64(rc.Left) / scale), int(float64(rc.Top) / scale),
		int(float64(rc.Right) / scale), int(float64(rc.Bottom) / scale)
}

// ShowNotification 在屏幕右下角弹出一张系统风格的通知卡片（仿 Windows 通知：
// 应用名 + 标题 + 内容 + 关闭钮），驻留直到用户点 ✕ 或代码调用 NotifyClose。
func (a *App) ShowNotification(title string, content string) {
	if a.app == nil {
		return
	}
	const notifyW, notifyH = 380, 132
	_, _, waRight, waBottom := workAreaDIP()
	x := waRight - notifyW - 16
	y := waBottom - notifyH - 16
	if x < 16 {
		x = 16
	}
	if y < 16 {
		y = 16
	}

	// 新通知替换旧通知
	a.notifyMu.Lock()
	if a.notifyWin != nil {
		a.notifyWin.Close()
		a.notifyWin = nil
	}
	a.notifyMu.Unlock()

	w := a.app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             fmt.Sprintf("win:notify-%d", time.Now().UnixNano()),
		Title:            "通知",
		Width:            notifyW,
		Height:           notifyH,
		X:                x,
		Y:                y,
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		StartState:       application.WindowStateNormal,
		BackgroundColour: application.NewRGB(243, 243, 243),
		URL: fmt.Sprintf("/#/widget/notify?title=%s&content=%s",
			url.QueryEscape(title), url.QueryEscape(content)),
		Windows: application.WindowsWindow{
			// 无边框通知卡支持前端 app-region: drag 拖动
			NonClientRegionSupport: true,
		},
	})
	a.notifyMu.Lock()
	a.notifyWin = w
	a.notifyMu.Unlock()
}

// NotifyClose 关闭通知卡片
func (a *App) NotifyClose() {
	if a.app == nil {
		return
	}
	a.notifyMu.Lock()
	if a.notifyWin != nil {
		a.notifyWin.Close()
		a.notifyWin = nil
	}
	a.notifyMu.Unlock()
}

/* ============================ 桌面浮动球（Go 原生 Win32 分层窗口） ============================ */

// 浮动球原生菜单项动态取自配置里标记为 Quick 的工具（见 services/winui/ball_windows.go: ballQuickItems）。
// 选中项由 handleBallAction 派发：工具类以 "ball:action" 事件交回主窗口，"close" 直接关窗。

// BallOpen 打开桌面浮动球（Go 原生 Win32 分层窗口：真透明、置顶、可拖动、点击弹原生菜单）。幂等。
func (a *App) BallOpen() error {
	if a.app == nil {
		return fmt.Errorf("应用尚未初始化")
	}
	applog.Infof("ball", "BallOpen 调用")
	if err := winui.StartBall(nativeHost{a}); err != nil {
		return err
	}
	return nil
}

// BallClose 关闭桌面浮动球。
func (a *App) BallClose() {
	applog.Infof("ball", "BallClose 调用")
	winui.StopBall()
}

// nativeHost 把 *App 适配成 winui.Host：原生浮层与热键不读 config.json、不持窗口对象，
// 需要的配置与回调全部从这里要。适配方法挂在未导出类型上，
// 因而不会被 Wails 生成进前端绑定。
type nativeHost struct{ a *App }

func (h nativeHost) BallMenuItems() []winui.MenuItem {
	var out []winui.MenuItem
	for _, t := range config.CurrentConfig().Tools {
		if !t.Quick {
			continue
		}
		id := t.ID
		if id == "" {
			id = t.Key
		}
		label := t.Name
		if label == "" {
			label = t.Key
		}
		out = append(out, winui.MenuItem{ID: id, Label: label})
	}
	return out
}

func (h nativeHost) BallTheme() string { return config.CurrentConfig().General.Theme }

func (h nativeHost) BallLogoPath() string { return config.CurrentConfig().General.AppLogo }

func (h nativeHost) BallIconMaskPath() (string, error) { return config.BallIconMaskPath() }

func (h nativeHost) BallDefaultLogo() []byte { return h.a.defaultLogo }

func (h nativeHost) BallPosition() (int, int, bool) {
	b := config.CurrentConfig().Ball
	return b.X, b.Y, b.HasPos
}

func (h nativeHost) SaveBallPosition(x, y int) error { return config.SaveBallPos(x, y) }

func (h nativeHost) BadgeCount() int { return h.a.activeBackgroundCount() }

func (h nativeHost) OnBallAction(id string) { h.a.handleBallAction(id) }

func (h nativeHost) ShowMainWindow() { h.a.showMainWindow() }

func (h nativeHost) Hotkeys() []winui.Hotkey {
	var out []winui.Hotkey
	for _, t := range config.CurrentConfig().Tools {
		if strings.TrimSpace(t.Shortcut) == "" {
			continue
		}
		id := t.ID
		if id == "" {
			id = t.Key
		}
		out = append(out, winui.Hotkey{ID: id, Shortcut: t.Shortcut})
	}
	return out
}

// DispatchTool 热键命中：截图等 Go 侧能直接处理的工具就地调起（少走一趟主窗口 JS），
// 其余以 "tool:activate" 事件交回主窗口复用开窗逻辑。
func (h nativeHost) DispatchTool(id string) {
	if h.a == nil || h.a.app == nil {
		return
	}
	if h.a.TryActivateNativeTool(id) {
		return
	}
	h.a.app.Event.Emit("tool:activate", id)
}

// SaveBallIcon 保存浮动球球心图标：前端把选中图标（图标库/Arco）渲染成黑色 alpha 遮罩 PNG（base64）上传，
// Go 侧写入配置目录 ball_icon.png 并通知球线程重载；渲染时只取遮罩形状，颜色跟随主题文字色（text-1）。
// 图标值本身（icon-xxx / Arco 名）随 ball.icon 由 SaveConfig 持久化，供设置页回显。
func (a *App) SaveBallIcon(b64Png string) error {
	raw := strings.TrimSpace(b64Png)
	if strings.HasPrefix(raw, "data:") {
		if i := strings.Index(raw, ","); i >= 0 {
			raw = raw[i+1:]
		}
	}
	data, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return applog.Fail("ball", err, "球图标数据解析失败")
	}
	if len(data) == 0 {
		return a.ClearBallIcon()
	}
	p, err := config.BallIconMaskPath()
	if err != nil {
		return applog.Fail("ball", err, "球图标路径不可用")
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		return applog.Fail("ball", err, "球图标写入失败")
	}
	applog.Infof("ball", "球心图标已保存: %s (%d 字节)", p, len(data))
	winui.ReloadBallIcon()
	return nil
}

// ClearBallIcon 删除自定义球心图标，浮动球回退到应用 Logo/内置默认图。
func (a *App) ClearBallIcon() error {
	if p, err := config.BallIconMaskPath(); err == nil {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return applog.Fail("ball", err, "球图标删除失败")
		}
		applog.Infof("ball", "球心图标已清除: %s", p)
	}
	winui.ReloadBallIcon()
	return nil
}

// handleBallAction 处理浮动球菜单选择：close 直接关窗，Go 侧能直接处理的工具事件
// （截图）就地调起省一趟前端往返，其余以 "ball:action" 事件交回主窗口。
func (a *App) handleBallAction(id string) {
	applog.Infof("ball", "菜单选择: %s", id)
	if id == "close" {
		winui.StopBall()
		return
	}
	if a.app == nil {
		applog.Errorf("ball", "处理菜单动作时 app 未初始化: %s", id)
		return
	}
	if a.TryActivateNativeTool(id) {
		return
	}
	a.app.Event.Emit("ball:action", id)
}

/* ============================ 配置与日志（供设置页调用） ============================ */

// GetConfig 返回当前应用配置（日志设置 + 工具列表）。
func (a *App) GetConfig() config.AppConfig {
	return config.CurrentConfig()
}

// SetAppTheme 持久化当前主题模式（light/dark/auto）：供标题栏快速切换时同步写入配置，
// 以便浮动球原生菜单等 Go 侧 UI 跟随应用浅/暗黑模式。
func (a *App) SetAppTheme(theme string) error {
	if err := config.SaveAppTheme(theme); err != nil {
		return err
	}
	winui.ReloadBallIcon() // 球面底板/图标着色随主题重绘
	return nil
}

// SaveConfig 保存配置：写入用户配置目录 JSON + 重新初始化日志器 + 热键重载 + 浮动球显隐；返回日志目录。
func (a *App) SaveConfig(cfg config.AppConfig) (string, error) {
	// 浮动球位置由后端维护（拖动结束落盘），前端保存设置时沿用当前值，避免被零值覆盖。
	if cur := config.CurrentConfig(); cur.Ball.HasPos {
		cfg.Ball.X, cfg.Ball.Y, cfg.Ball.HasPos = cur.Ball.X, cur.Ball.Y, cur.Ball.HasPos
	}
	// 工具级设置由各工具页自行保存（如 MySQL 的 SQL 日志），设置页 payload 不含该字段，沿用当前值避免被清空。
	if len(cfg.ToolSettings) == 0 {
		cfg.ToolSettings = config.CurrentConfig().ToolSettings
	}
	path, err := config.SaveAppConfig(cfg)
	if err != nil {
		applog.Errorf("config", "保存配置失败: %v", err)
		return "", err
	}
	// 全局目录布局（paths）可能变更：重新应用并预建服务子目录，需在日志器与工具日志重配置之前
	config.InitAppDirs(cfg)
	logDir := applog.Init(config.ApplogConfig(cfg.Log))
	// SQL 日志默认目录随日志基础目录变更重解析（配置了固定目录的不受影响）
	ms := cfg.ToolSettings["mysql"]
	mysql.ConfigureSQLLog(ms.SQLLogEnabled, ms.SQLLogDir)
	applog.Infof("config", "配置已保存: %s（日志目录: %s, enabled=%v level=%s, ball=%v, autoStart=%v, paths=%s|%s|%s）", path, logDir, cfg.Log.Enabled, cfg.Log.Level, cfg.Ball.Enabled, cfg.General.AutoStart, cfg.Paths.Plugins, cfg.Paths.Data, cfg.Paths.Logs)
	// 热键可能变更：请求热键线程按新配置重载
	winui.ReloadHotkeys()
	// 开机自启随设置即时生效（Enable/Disable 均幂等）
	a.applyAutoStart(cfg.General.AutoStart)
	// 浮动球显隐随设置变更即时生效
	if cfg.Ball.Enabled {
		if err := a.BallOpen(); err != nil {
			applog.Warnf("ball", "按设置显示浮动球失败: %v", err)
		} else {
			winui.ReloadBallIcon() // 球已在跑时 BallOpen 幂等无感，主题/图标变更靠此重载生效
		}
	} else {
		a.BallClose()
	}
	// 广播配置变更：首页等窗口据此刷新工具列表（设置页会自动保存，前端自行去重）
	if a.app != nil {
		a.app.Event.Emit("config:saved", path)
	}
	return logDir, nil
}

// ChooseLogDir 弹出原生目录选择框，返回所选目录（取消返回空串）。
func (a *App) ChooseLogDir() (string, error) {
	return a.ChooseDir("选择日志目录")
}

// ChooseDir 弹原生目录选择框（供设置页选择插件/数据/日志等基础目录），取消返回空串。
func (a *App) ChooseDir(title string) (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	if strings.TrimSpace(title) == "" {
		title = "选择目录"
	}
	dir, err := a.app.Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		AllowsOtherFileTypes(false).
		SetTitle(title).
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return dir, nil
}

// OcrImage 对成品图（dataURL/base64 PNG）做文字识别，返回识别出的纯文本。
// 实现细节（WinRT + PowerShell 5.1 调用）在 tools/ocr，这里只保留绑定入口。
func (a *App) OcrImage(data string) (string, error) {
	return ocr.Image(data)
}

// GetLogDir 返回当前实际使用的日志目录。
func (a *App) GetLogDir() string {
	return applog.ResolveDir(config.ApplogConfig(config.CurrentConfig().Log))
}

// OpenLogDir 在系统资源管理器中打开当前日志目录（Windows: explorer）。
func (a *App) OpenLogDir() error {
	dir := a.GetLogDir()
	if _, err := os.Stat(dir); err != nil {
		_ = os.MkdirAll(dir, 0o755)
	}
	cmd := exec.Command("explorer.exe", dir)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Start(); err != nil {
		applog.Errorf("log", "打开日志目录失败: %v", err)
		return err
	}
	return nil
}

/* ---------------- 系统通知中心（Windows 原生 Toast） ---------------- */

// toastPSScript 通过环境变量 TOAST_TITLE / TOAST_CONTENT 发送 WinRT Toast 的
// PowerShell 脚本（Windows 10+ 内建能力，无需任何依赖；标题+正文两行模板）
const toastPSScript = `
$t = $env:TOAST_TITLE
$c = $env:TOAST_CONTENT
[void][Windows.UI.Notifications.ToastNotificationManager,Windows.UI.Notifications,ContentType=WindowsRuntime]
[void][Windows.Data.Xml.Dom.XmlDocument,Windows.Data.Xml.Dom.XmlDocument,ContentType=WindowsRuntime]
$tpl = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$txts = $tpl.SelectNodes("//text")
$txts.Item(0).AppendChild($tpl.CreateTextNode($t)) > $null
$txts.Item(1).AppendChild($tpl.CreateTextNode($c)) > $null
$notifier = [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("go-tools")
$toast = [Windows.UI.Notifications.ToastNotification]::new($tpl)
$notifier.Show($toast)
`

// ShowSystemNotification 直接调用 Windows 系统通知中心弹原生 Toast
// （系统右下角渲染、自动消失、可进入操作中心/专注助手），异步执行不阻塞调用方。
func (a *App) ShowSystemNotification(title string, content string) error {
	if title == "" {
		title = "go-tools"
	}
	go func() {
		// 脚本经 stdin 传入（-Command -），避免命令行引号转义问题
		cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive",
			"-ExecutionPolicy", "Bypass", "-Command", "-")
		cmd.Stdin = strings.NewReader(toastPSScript)
		// 内容经环境变量传递，避免命令行注入/转义问题
		cmd.Env = append(os.Environ(), "TOAST_TITLE="+title, "TOAST_CONTENT="+content)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} // 不闪现 PowerShell 控制台窗口
		_ = cmd.Run()
	}()
	return nil
}

// VideoSaveBegin 开始一次视频分块上传会话，返回会话 ID
func (a *App) VideoSaveBegin(filename string) (string, error) {
	if filename == "" {
		filename = fmt.Sprintf("recording-%s.mp4", time.Now().Format("20060102-150405"))
	}
	id := fmt.Sprintf("vss-%d", time.Now().UnixNano())
	a.recMu.Lock()
	defer a.recMu.Unlock()
	if a.videoSessions == nil {
		a.videoSessions = map[string]*videoSaveSession{}
	}
	a.videoSessions[id] = &videoSaveSession{name: filename}
	return id, nil
}

// VideoSaveChunk 追加一块 base64 编码的视频数据
func (a *App) VideoSaveChunk(session, base64Chunk string) error {
	a.recMu.Lock()
	defer a.recMu.Unlock()
	s, ok := a.videoSessions[session]
	if !ok {
		return fmt.Errorf("会话不存在或已结束")
	}
	raw, err := base64.StdEncoding.DecodeString(base64Chunk)
	if err != nil {
		return fmt.Errorf("解码分块失败: %w", err)
	}
	if s.data.Len()+len(raw) > 4<<30 { // 4GB 上限保护
		return fmt.Errorf("视频数据超出上限")
	}
	_, _ = s.data.Write(raw)
	return nil
}

// VideoSavePrompt 弹出原生“另存为”对话框选择目录并写盘；用户取消时返回空字符串
func (a *App) VideoSavePrompt(session string) (string, error) {
	a.recMu.Lock()
	s, ok := a.videoSessions[session]
	a.recMu.Unlock()
	if !ok {
		return "", fmt.Errorf("会话不存在或已结束")
	}
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(s.name)), ".")
	if ext == "" {
		ext = "mp4"
	}
	dlg := a.app.Dialog.SaveFile().
		SetFilename(s.name).
		SetMessage("另存录制视频").
		AddFilter("视频文件 (*."+ext+")", "*."+ext).
		AddFilter("所有文件 (*.*)", "*.*")
	// 默认落在程序目录下 data/videos（创建失败则不设定，退回系统默认位置）
	if dir, derr := a.AppDataDir("videos"); derr == nil {
		dlg = dlg.SetDirectory(dir)
	}
	a.recMu.Lock()
	parent := a.recToolWin // 小窗停止后已关闭，另存对话框挂在工具窗上
	a.recMu.Unlock()
	if parent != nil {
		dlg = dlg.AttachToWindow(parent)
	}
	path, err := dlg.PromptForSingleSelection()
	if err != nil {
		// 用户取消对话框：不算错误（Wails 内部返回 "cancelled by user"），保留会话以便重试另存
		if strings.Contains(strings.ToLower(err.Error()), "cancel") {
			return "", nil
		}
		return "", err
	}
	if path == "" {
		// 用户取消，保留会话以便重试另存
		return "", nil
	}
	if !strings.EqualFold(filepath.Ext(path), "."+ext) {
		path += "." + ext
	}
	if err := os.WriteFile(path, s.data.Bytes(), 0644); err != nil {
		return "", fmt.Errorf("写入文件失败: %w", err)
	}
	a.recMu.Lock()
	delete(a.videoSessions, session)
	a.recMu.Unlock()
	return path, nil
}

// VideoSaveAbort 放弃一次上传会话（不写盘）
func (a *App) VideoSaveAbort(session string) error {
	a.recMu.Lock()
	defer a.recMu.Unlock()
	delete(a.videoSessions, session)
	return nil
}
