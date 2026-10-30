//go:build windows

package app

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
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
function Emit($s) { try { $s | Out-File -FilePath $env:TOAST_RESULT -Append -Encoding ascii } catch {}; try { Write-Output $s } catch {} }
Emit ("START aumid=" + $aumid)
try {
Emit "STEP=1load"
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
Emit ("AUMID=" + $aumid)
Emit "STEP=6show"
$notifier = [Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier($aumid)
$toast = [Windows.UI.Notifications.ToastNotification]::new($tpl)
$notifier.Show($toast)
Emit "SHOW_OK"
# SETTING 诊断（Windows 对该 AUMID 的通知授权状态）改为尽力而为、且放在 Show 之后：
# GetSettingForToastNotifier 在 Windows PowerShell 5.1 上可能不存在（抛 MethodNotFound），
# 若放在 Show 前 + $ErrorActionPreference=Stop，会直接中止脚本、通知永不弹出——
# 这正是「Toast 不出现」的真凶，绝不能为诊断牺牲投递。
try {
$setting = [Windows.UI.Notifications.ToastNotificationManager]::GetSettingForToastNotifier($aumid)
Emit ("SETTING=" + $setting)
} catch {
Emit ("SETTING=unavailable:" + $_.Exception.GetType().Name)
}
Emit "END"
} catch {
Emit ("PS_ERR=" + $_.Exception.Message)
Emit "END"
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
	// 脚本落临时 .ps1 用 -File 执行：此前经 stdin(-Command -) 传整段脚本，真机复现
	// 「PowerShell 只执行到第一条 Emit(START) 就以退出码 0 结束，try/catch 都没跑到」——
	// PowerShell 先解析整段再执行，try 都没进说明 stdin 首行后即被截断，改 -File 彻底规避。
	// 脚本含中文注释，PowerShell 5.1 以 -File 读取时需 UTF-8 BOM 才能正确解码多字节字符。
	psPath := ""
	if f, e := os.CreateTemp("", "go-tools-toast-*.ps1"); e == nil {
		psPath = f.Name()
		_, _ = f.Write(append([]byte("\xEF\xBB\xBF"), []byte(toastPSScript)...))
		_ = f.Close()
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-File", psPath)
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
	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start).Milliseconds()
	// 双来源诊断：以结果文件为准（PowerShell 经 stdin -Command - 执行时 stdout 管道常被丢弃），
	// stdout 兜底；两路都留痕，用于区分「脚本真没跑到底」与「跑了但输出丢了」。
	stdoutDiag := strings.TrimSpace(out.String())
	fileDiag := ""
	if resPath != "" {
		if b, e := os.ReadFile(resPath); e == nil {
			fileDiag = strings.TrimSpace(string(b))
		}
		_ = os.Remove(resPath)
	}
	if psPath != "" {
		_ = os.Remove(psPath)
	}
	diag := fileDiag
	if diag == "" {
		diag = stdoutDiag
	}
	if err != nil {
		// 原生 Toast 都没能交给系统渲染 → 降级为页内 a-notification
		applog.Warnf("notify", "Toast 发送失败 耗时=%dms: %v 目标=%q aumid=%q 文件输出=[%s] stdout输出=[%s]", elapsed, err, target, aumid, fileDiag, stdoutDiag)
		a.emitNotifyFallback(title, content)
		return err
	}
	// 退出码 0 却没有 END 哨兵：脚本多半在 START 之后被中途终止（崩溃/被杀），或结果文件写入被
	// Emit 内的 catch 吞掉——这正是「日志只到 START、查不到 SETTING」的判据，单独告警便于定位卡在第几步。
	if !strings.Contains(diag, "END") {
		applog.Warnf("notify", "Toast 疑似异常 耗时=%dms：PowerShell 退出码=0 但诊断缺少 END 哨兵（脚本可能中途终止或输出丢失），目标=%q aumid=%q 文件输出=[%s] stdout输出=[%s]", elapsed, target, aumid, fileDiag, stdoutDiag)
		return nil
	}
	applog.Infof("notify", "Toast 已提交渲染 耗时=%dms 目标=%q aumid=%q PowerShell输出: %s", elapsed, target, aumid, diag)
	return nil
}

// toastLogoPath 把当前 logo（自定义文件优先，否则内置默认）落一份 PNG 到数据目录，
// 返回其绝对文件路径（供注册表 IconUri 用）。失败回空串。
func (a *App) toastLogoPath() string {
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
	return p
}

// toastLogoURI 把 toastLogoPath 的路径转成 WinRT Toast 可用的 file:/// URI（供
// appLogoOverride 内联图用）。失败回空串：通知照常弹，只是没图，不影响主流程。
func (a *App) toastLogoURI() string {
	p := a.toastLogoPath()
	if p == "" {
		return ""
	}
	// file:/// + 正斜杠绝对路径；空格转 %20，避免 URI 截断
	return "file:///" + strings.ReplaceAll(filepath.ToSlash(p), " ", "%20")
}
