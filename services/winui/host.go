// Package winui 收纳进程外的原生桌面界面：浮动球与全局热键。
//
// 两者共用同一套 Win32 过程与结构（ballRegisterClass/ballCreate/ballPostMsg/…
// 定义在 ball_windows.go），且都必须跑在各自专用的 OS 线程上泵消息，因此同包：
//   - ball_windows.go   透明置顶浮动球 + 自绘右键快捷菜单 + 后台任务角标
//   - hotkey_windows.go 消息-only 窗口上 RegisterHotKey 的全局热键
//
// 本包不读 config.json、不持有 *App：需要的配置与回调一律经 Host 由主程序注入，
// 这样原生层的像素/消息代码与"业务配置"彻底解耦，主程序也不会被绑定面污染。
package winui

// MenuItem 浮动球右键菜单的一项（ID 交回主程序派发）。
type MenuItem struct {
	ID    string
	Label string
}

// Hotkey 一条全局热键绑定：ID 为工具标识，Shortcut 形如 "Ctrl+Alt+S"。
type Hotkey struct {
	ID       string
	Shortcut string
}

// Host 由主程序（package main 的未导出适配类型）实现。
// 全部方法都可能被原生线程/球线程调用，实现方须保证并发安全。
type Host interface {
	// —— 浮动球 ——
	BallMenuItems() []MenuItem         // 标记为快捷（Quick=true）的工具
	BallTheme() string                 // light / dark / auto（auto 时由本包读系统注册表）
	BallLogoPath() string              // 自定义应用 logo 文件路径，空表示没有
	BallIconMaskPath() (string, error) // 球心图标 alpha 遮罩 PNG 路径
	BallDefaultLogo() []byte           // 内置默认 logo（PNG 字节）
	BallPosition() (x, y int, ok bool) // 上次落点（物理像素），false = 没有记录
	SaveBallPosition(x, y int) error
	BadgeCount() int        // 球面红点角标：后台运行中的任务数
	OnBallAction(id string) // 菜单选中项交回主程序派发
	ShowMainWindow()        // 单击球体唤起主界面

	// —— 全局热键 ——
	Hotkeys() []Hotkey      // 当前配置里所有设了 Shortcut 的工具
	DispatchTool(id string) // 命中热键后交给主程序：Go 直触发或事件回主窗口
}
