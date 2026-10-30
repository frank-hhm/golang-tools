package app

// ============================================================
// 系统通知点击 → 跳转到对应面板（宿主侧接线）
//
// 链路：Windows 通知中心的原生 Toast 带 activationType=protocol +
// launch=tools:<目标>。点它时 Windows 用注册在 HKCU 的 tools: 协议
// 重新拉起本 exe，命令行里就多了个 tools:<目标> 参数。主入口据此分流：
//
//   - 已有实例在跑 → 通过 acthost（回环 TCP）把目标递给那个实例，本进程退出；
//     收到目标的一方把主窗口拉到前台并广播 app:navigate，前端切到对应面板。
//   - 没有实例 → 本进程正常起 GUI，把目标记成 pending，前端挂载后取走再跳。
//
// 全文件不带 build tag：ShowSystemNotificationWithTarget / GetPendingNavigate
// 是绑定方法，必须跨平台存在（Linux 构建里被 tag 挡掉就是 unknown bound
// method id）。平台专属的注册与弹窗在 toast_windows.go / app_platform_other.go。
// ============================================================

import (
	"net/url"
	"strings"
	"sync"

	"Tools/services/acthost"
	"Tools/services/applog"
	"Tools/services/sysx"
)

// navigateScheme 系统通知点击走的本机协议名。注册在 HKCU\Software\Classes\tools，
// 与 Toast 的 activationType=protocol + launch=tools:<目标> 配套。取与应用名一致的 "tools"。
const navigateScheme = "tools"

var (
	navMu      sync.Mutex
	navPending string          // 冷启动带来的目标：前端挂载后经 GetPendingNavigate 取走
	navServer  *acthost.Server // 运行实例的转交监听（第二实例把目标递进来）
)

// NavigationTarget 从命令行里认出「系统通知点击」拉起时带的 tools: 目标。
// 返回去掉协议前缀后的导航目标（一条前端路由，如 "/tools/download"）；
// ok=false 表示这不是协议拉起的进程，照旧正常启动。
func NavigationTarget(args []string) (string, bool) {
	for _, arg := range args {
		if t, ok := parseNavigateArg(arg); ok {
			return t, true
		}
	}
	return "", false
}

// parseNavigateArg 解析单个命令行参数是不是 tools: 目标。
// 协议 URL 可能带 // 主机位或百分号编码（路径里的 / 会被编码成 %2F），统一还原。
func parseNavigateArg(arg string) (string, bool) {
	arg = strings.TrimSpace(arg)
	prefix := navigateScheme + ":"
	if !strings.HasPrefix(strings.ToLower(arg), prefix) {
		return "", false
	}
	target := arg[len(prefix):]
	target = strings.TrimPrefix(target, "//")
	if dec, err := url.PathUnescape(target); err == nil {
		target = dec
	}
	if target == "" {
		return "", false
	}
	return target, true
}

// navigateURL 把前端路由拼成协议 URL（供 Toast 的 launch 用）。
// 路径里的 / 百分号编码，避免被当成协议的不透明主体截断。
func navigateURL(target string) string {
	return navigateScheme + ":" + url.PathEscape(target)
}

// HandoffNavigate 协议拉起的第二实例该做的第一件事：把目标递给正在运行的实例。
// 递成功返回 true（调用方随即退出，不再建窗口）；false = 没有运行实例，
// 本进程自己起 GUI 并把 target 作为 pending 交给前端。
func HandoffNavigate(target string) bool {
	return acthost.TrySend(acthost.Request{Target: target, Kind: "route", From: "protocol"})
}

// SetPendingNavigate 记下冷启动目标，等前端挂载后取。
func (a *App) SetPendingNavigate(target string) {
	if target == "" {
		return
	}
	navMu.Lock()
	navPending = target
	navMu.Unlock()
}

// GetPendingNavigate 前端挂载时取走并清空冷启动目标（取一次即消费）。
// 非桌面平台永远空串：没有协议、也就不会有 pending。
func (a *App) GetPendingNavigate() string {
	navMu.Lock()
	defer navMu.Unlock()
	t := navPending
	navPending = ""
	return t
}

// mountNavigateHost 起转交监听（仅桌面）：协议拉起的第二实例把导航目标递进来，
// 这里收到后把主窗口拉到前台并广播 app:navigate。
//
// 与压缩的转交监听同构：另一个实例已占端点时 Start 失败只记日志，不报错——
// 两个 GUI 并存是既有行为，谁先起来谁接点击，后起来的接不到而已。
func (a *App) mountNavigateHost() {
	if !sysx.Caps().NativeWindows {
		return
	}
	navMu.Lock()
	running := navServer != nil
	navMu.Unlock()
	if running {
		return
	}
	srv, err := acthost.Start(func(req acthost.Request) {
		applog.Infof("notify", "收到导航转交: target=%s from=%s", req.Target, req.From)
		a.navigateMainWindow(req.Target)
	})
	if err != nil {
		applog.Warnf("notify", "导航转交监听启动失败（可能已有实例）: %v", err)
		return
	}
	navMu.Lock()
	navServer = srv
	navMu.Unlock()
	applog.Infof("notify", "导航转交监听已启动")
}

// unmountNavigateHost 收回监听与端点文件（幂等）。
func (a *App) unmountNavigateHost() {
	navMu.Lock()
	srv := navServer
	navServer = nil
	navMu.Unlock()
	if srv != nil {
		srv.Stop()
	}
}

// navigateMainWindow 把主窗口拉到前台，再广播 app:navigate 让前端切到目标面板。
//
// 只发给主窗口（不全局广播）：工具窗口是另起的 webview，全局发会把它们也带跑，
// 与 home:open 同一套做法。
func (a *App) navigateMainWindow(target string) {
	if target == "" || a.mainWindow == nil {
		return
	}
	a.showMainWindow()
	a.mainWindow.EmitEvent("app:navigate", target)
}

// ShowSystemNotificationWithTarget 弹一条带点击跳转的系统通知：点 Toast 时经
// tools: 协议把用户带回 target 对应的面板（一条前端路由，如 "/tools/download"）。
//
// target 为空时等价于 ShowSystemNotification（只弹不跳）。非 Windows 平台由
// notifyWithTarget 回 sysx.ErrUnsupported，方法本身照常存在（绑定 id 跨平台）。
func (a *App) ShowSystemNotificationWithTarget(title string, content string, target string) error {
	return a.notifyWithTarget(title, content, target)
}

// emitNotifyFallback 原生系统通知发不出去时的降级：把通知转给主窗口前端，由前端
// 用 a-notification 在当前 WebView 里弹一条（至少用户看得到）。无主窗口（无头/CLI
// 分支、或窗口已销毁）则静默忽略——那种场景本来也没有页可弹。
func (a *App) emitNotifyFallback(title string, content string) {
	if a.mainWindow == nil {
		return
	}
	a.mainWindow.EmitEvent("app:notify", map[string]string{"title": title, "content": content})
}

// GetToastStatus 返回系统通知身份登记的诊断文本，供前端「通知诊断」查看：
// 未登记成功时 Toast 没有 logo、点击也不跳转——这里给出原因。方法跨平台存在
// （绑定 id 不能藏在 build tag 后），非 Windows 由 toastStatusText 回不支持提示。
func (a *App) GetToastStatus() string {
	return toastStatusText()
}
