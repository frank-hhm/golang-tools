//go:build windows

package app

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"unsafe"

	"Tools/tools/screenshot"

	"golang.org/x/sys/windows"
)

// platformState 汇总 App 里只在 Windows 上才有意义的字段。
//
// 单独成结构体是为了让 App 的定义能留在无标签的 app.go 里：server 构建时
// 换成空结构体即可，App 其余字段（应用句柄、上下文、各服务的锁）照旧。
type platformState struct {
	capFrame *screenshot.CaptureFrame // 抓屏结果，供浮层页面拉取
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

// toastPSScript 通过环境变量 TOAST_TITLE / TOAST_CONTENT / TOAST_APP 发送 WinRT Toast 的
// PowerShell 脚本（Windows 10+ 内建能力，无需任何依赖；标题+正文两行模板）。
// TOAST_APP 是通知卡片上显示的应用名（由调用方按配置的应用名传入，空则回退 Tools）。
const toastPSScript = `
$t = $env:TOAST_TITLE
$c = $env:TOAST_CONTENT
$app = $env:TOAST_APP
if ([string]::IsNullOrWhiteSpace($app)) { $app = "Tools" }
[void][Windows.UI.Notifications.ToastNotificationManager,Windows.UI.Notifications,ContentType=WindowsRuntime]
[void][Windows.Data.Xml.Dom.XmlDocument,Windows.Data.Xml.Dom.XmlDocument,ContentType=WindowsRuntime]
$tpl = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$txts = $tpl.SelectNodes("//text")
$txts.Item(0).AppendChild($tpl.CreateTextNode($t)) > $null
$txts.Item(1).AppendChild($tpl.CreateTextNode($c)) > $null
$notifier = [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($app)
$toast = [Windows.UI.Notifications.ToastNotification]::new($tpl)
$notifier.Show($toast)
`

// ShowSystemNotification 直接调用 Windows 系统通知中心弹原生 Toast
// （系统右下角渲染、自动消失、可进入操作中心/专注助手），异步执行不阻塞调用方。
func (a *App) ShowSystemNotification(title string, content string) error {
	if title == "" {
		title = effectiveAppName()
	}
	appName := effectiveAppName()
	go func() {
		// 脚本经 stdin 传入（-Command -），避免命令行引号转义问题
		cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive",
			"-ExecutionPolicy", "Bypass", "-Command", "-")
		cmd.Stdin = strings.NewReader(toastPSScript)
		// 内容经环境变量传递，避免命令行注入/转义问题；TOAST_APP 决定通知卡片上的应用名
		cmd.Env = append(os.Environ(), "TOAST_TITLE="+title, "TOAST_CONTENT="+content, "TOAST_APP="+appName)
		cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} // 不闪现 PowerShell 控制台窗口
		_ = cmd.Run()
	}()
	return nil
}
