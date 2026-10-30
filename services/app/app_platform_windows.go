//go:build windows

package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"Tools/appdirs"
	"Tools/services/applog"
	"Tools/services/config"
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

// toastPSScript 通过环境变量 TOAST_TITLE / TOAST_CONTENT / TOAST_APP / TOAST_AUMID /
// TOAST_LOGO / TOAST_LAUNCH 发送 WinRT Toast 的 PowerShell 脚本（Windows 10+ 内建能力，
// 无需任何依赖；标题+正文两行模板）。关键变量：
//
//	TOAST_AUMID   已注册的 AppUserModelID；CreateToastNotifier 用它，通知才归属本应用
//	              （logo/点击激活的前提）。空则回退应用名。
//	TOAST_LAUNCH  点击跳转的 tools: 协议 URL；非空时给 toast 根节点加 protocol 激活。
const toastPSScript = `
$ErrorActionPreference = 'Stop'
$t = $env:TOAST_TITLE
$c = $env:TOAST_CONTENT
$app = $env:TOAST_APP
if ([string]::IsNullOrWhiteSpace($app)) { $app = "Tools" }
$aumid = $env:TOAST_AUMID
if ([string]::IsNullOrWhiteSpace($aumid)) { $aumid = $app }
function Emit($s) { try { $s | Out-File -FilePath $env:TOAST_RESULT -Append -Encoding ascii } catch {}; Write-Output $s }
Emit ("START aumid=" + $aumid)
try {
[void][Windows.UI.Notifications.ToastNotificationManager,Windows.UI.Notifications,ContentType=WindowsRuntime]
[void][Windows.Data.Xml.Dom.XmlDocument,Windows.Data.Xml.Dom.XmlDocument,ContentType=WindowsRuntime]
$tpl = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$txts = $tpl.SelectNodes("//text")
$txts.Item(0).AppendChild($tpl.CreateTextNode($t)) > $null
$txts.Item(1).AppendChild($tpl.CreateTextNode($c)) > $null
$logo = $env:TOAST_LOGO
if (-not [string]::IsNullOrWhiteSpace($logo)) {
$img = $tpl.CreateElement("image")
$img.SetAttribute("src", $logo) | Out-Null
$img.SetAttribute("placement", "appLogoOverride") | Out-Null
$img.SetAttribute("hintCrop", "circle") | Out-Null
$binding = $tpl.SelectSingleNode("//binding")
if ($binding) { [void]$binding.AppendChild($img) }
}
$launch = $env:TOAST_LAUNCH
if (-not [string]::IsNullOrWhiteSpace($launch)) {
$root = $tpl.DocumentElement
$root.SetAttribute("launch", $launch) | Out-Null
$root.SetAttribute("arguments", $launch) | Out-Null
$root.SetAttribute("activationType", "protocol") | Out-Null
}
# 关键诊断：Windows 对该 AUMID 的通知授权状态。返回非 Enabled 时 Show() 不报错却
# 什么也不弹（这正是「登记成功但通知卡不出现」的隐蔽成因）——把它打出来。
$setting = [Windows.UI.Notifications.ToastNotificationManager]::GetSettingForToastNotifier($aumid)
Emit ("AUMID=" + $aumid)
Emit ("SETTING=" + $setting)
$notifier = [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($aumid)
$toast = [Windows.UI.Notifications.ToastNotification]::new($tpl)
$notifier.Show($toast)
Emit "SHOW_OK"
} catch {
Emit ("PS_ERR=" + $_.Exception.Message)
exit 1
}
`

// ShowSystemNotification 直接调用 Windows 系统通知中心弹原生 Toast
// （系统右下角渲染、自动消失、可进入操作中心/专注助手），异步执行不阻塞调用方。
// 不带点击跳转；要跳转用 ShowSystemNotificationWithTarget。
func (a *App) ShowSystemNotification(title string, content string) error {
	go func() { _ = a.runToast(title, content, "") }()
	return nil
}

// notifyWithTarget 平台实现：弹一条可选带点击跳转目标的 Toast（异步）。
// target 是一条前端路由（如 "/tools/download"），空则只弹不跳。
func (a *App) notifyWithTarget(title string, content string, target string) error {
	go func() { _ = a.runToast(title, content, target) }()
	return nil
}

// notifySync 同步版的系统通知：等 Toast 真的交给系统渲染进程再返回。
//
// 无界面那条分支（右键拉起、压完就退出）非用不可：进程在 os.Exit 时把 goroutine
// 一起带走，异步版的通知十有八九还没弹出来就被杀了。
func (a *App) notifySync(title string, content string) error {
	return a.runToast(title, content, "")
}

// runToast 组装环境变量并跑一次 PowerShell 发 Toast。
//
// 分层保障“通知一定弹得出来”：
//   - 启动时已登记成功（注册表 AppUserModelId 在）→ 用真实 AUMID 发，带 logo；target 非空时再带
//     tools: 协议点击跳转。这是完整形态。
//   - 未登记成功 → TOAST_AUMID 留空，脚本回退用显示名作伪 AUMID，发一条普通但
//     一定能弹出来的 Toast（给未注册的 AUMID 会被 Windows 静默抑制，比没 logo 更糟）。
//   - 连 PowerShell 都跑失败 → 降级：广播 app:notify 让前端在当前 WebView 里用
//     a-notification 弹一条（见 emitNotifyFallback）。
//
// 身份登记走纯注册表（无 COM，不会崩），见 toast_windows.go。
func (a *App) runToast(title string, content string, target string) error {
	if title == "" {
		title = effectiveAppName()
	}
	appName := effectiveAppName()
	aumid := ""
	launch := ""
	if toastRegistered() {
		aumid = toastAUMID
		if target != "" {
			launch = navigateURL(target)
		}
	}
	if target != "" && aumid == "" {
		applog.Warnf("notify", "已请求点击跳转(target=%s)但通知身份未登记成功，本次 Toast 不带跳转——点它不会跳面板；原因见上一条 notify 日志或测试页「通知诊断」。", target)
	}
	// 脚本经 stdin 传入（-Command -），避免命令行引号转义问题
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-Command", "-")
	cmd.Stdin = strings.NewReader(toastPSScript)
	// 诊断结果文件：脚本把 AUMID/SETTING/结果逐行写到这里。PowerShell 经 stdin(-Command -)
	// 执行时，stdout 管道在部分环境下拿不到内容（真机踩到：日志里「PowerShell输出」为空），
	// 故再落一份到文件，Go 读文件为准、stdout 兜底。
	resPath := ""
	if f, e := os.CreateTemp("", "go-tools-toast-*.txt"); e == nil {
		resPath = f.Name()
		_ = f.Close()
	}
	// 内容经环境变量传递，避免命令行注入/转义问题
	cmd.Env = append(os.Environ(),
		"TOAST_TITLE="+title, "TOAST_CONTENT="+content,
		"TOAST_APP="+appName, "TOAST_AUMID="+aumid,
		"TOAST_LOGO="+a.toastLogoURI(), "TOAST_LAUNCH="+launch,
		"TOAST_RESULT="+resPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW：不闪现 PowerShell 控制台窗口（Win11 上 HideWindow 藏不住 Windows Terminal）
	// 捕获 PowerShell 输出（含 AUMID / SETTING / SHOW_OK / PS_ERR 诊断行）——之前直接丢弃，
	// 导致「弹不出来却查不到原因」。无论成败都落一条 notify 日志，便于真机排查。
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	diag := strings.TrimSpace(out.String())
	if resPath != "" {
		if b, e := os.ReadFile(resPath); e == nil && len(strings.TrimSpace(string(b))) > 0 {
			diag = strings.TrimSpace(string(b))
		}
		_ = os.Remove(resPath)
	}
	if err != nil {
		// 原生 Toast 都没能交给系统渲染 → 降级为页内 a-notification
		applog.Warnf("notify", "Toast 发送失败: %v 目标=%q aumid=%q PowerShell输出: %s", err, target, aumid, diag)
		a.emitNotifyFallback(title, content)
		return err
	}
	applog.Infof("notify", "Toast 已提交渲染 目标=%q aumid=%q PowerShell输出: %s", target, aumid, diag)
	return nil
}

// toastLogoURI 把当前 logo（自定义文件优先，否则内置默认）落一份 PNG 到数据目录，
// 返回 WinRT Toast 可用的 file:/// URI（供 appLogoOverride 内联图用）。
// 失败回空串：通知照常弹，只是没图，不影响主流程。
func (a *App) toastLogoURI() string {
	var data []byte
	if cfg := config.CurrentConfig(); cfg.General.AppLogo != "" {
		if b, err := os.ReadFile(cfg.General.AppLogo); err == nil {
			data = b
		}
	}
	if len(data) == 0 {
		data = a.defaultLogo
	}
	if len(data) == 0 {
		return ""
	}
	dir, err := appdirs.Data("app")
	if err != nil {
		return ""
	}
	p := filepath.Join(dir, "toast_logo.png")
	// 内容变了才重写，避免每次通知都触发磁盘写
	if old, e := os.ReadFile(p); e != nil || !bytes.Equal(old, data) {
		if err := os.WriteFile(p, data, 0o644); err != nil {
			return ""
		}
	}
	// file:/// + 正斜杠绝对路径；空格转 %20，避免 URI 截断
	return "file:///" + strings.ReplaceAll(filepath.ToSlash(p), " ", "%20")
}
