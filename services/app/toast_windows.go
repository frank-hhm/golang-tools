//go:build windows

package app

// ============================================================
// 未打包桌面应用的 Toast 身份登记（logo + 点击跳转的前提）
//
// Windows 要把一条原生 Toast「归属」到本应用（才允许加载本地 logo 图、点击才谈得上
// 激活），前提是有一个已注册的 AppUserModelID。官方推荐做法是给开始菜单快捷方式设
// System.AppUserModel.ID 属性，但那只能走 COM——真机验证：手写 COM 每次都访问违例
// （登记子进程退出码 0xC0000005），logo/点击这条路走不通；放子进程里崩虽然不连累主
// 程序，但也永远登记不上，等于白搭。
//
// 故改用纯注册表登记（BurntToast 同款，完全不碰 COM，零崩溃风险）：
//   - HKCU\Software\Classes\AppUserModelId\<AUMID> 写 DisplayName/IconUri → 注册 AUMID；
//   - HKCU\Software\Classes\tools → 点 Toast 时按协议拉起本程序（点击跳转）。
// 两者都是普通注册表写，安全，直接在主进程启动时同步做完，不再需要子进程隔离。
// ============================================================

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"unsafe"

	"Tools/services/applog"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// toastAUMID 本应用登记的 AppUserModelID。注册表键、进程、CreateToastNotifier 三处
// 必须是同一个串。取与应用显示名一致的 "Tools"。
const toastAUMID = "Tools"

var toastOnce sync.Once

// 诊断：记录最近一次登记尝试的结果文本，供日志与前端「通知诊断」查看。
var (
	toastDiagMu      sync.Mutex
	toastRegisterMsg string
)

// aumidRegPath 本 AUMID 在注册表里的登记键路径（相对 HKCU）。
func aumidRegPath() string { return `Software\Classes\AppUserModelId\` + toastAUMID }

// toastRegistered 本 AUMID 是否已在注册表登记（键存在即算）。
func toastRegistered() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, aumidRegPath(), registry.READ)
	if err != nil {
		return false
	}
	_ = k.Close()
	return true
}

// ensureToastRegistered 启动时调一次：设进程 AUMID + 注册协议 + 注册表登记 AUMID。
// 全是安全操作（无 COM），就地同步做完。同进程内只做一次。
func ensureToastRegistered(a *App) {
	toastOnce.Do(func() {
		setProcessAUMID(toastAUMID)
		exe, err := selfExePath()
		if err != nil {
			setToastDiag("取程序路径失败，无法登记: " + err.Error())
			applog.Warnf("notify", "取程序路径失败，跳过 Toast 身份登记: %v", err)
			return
		}
		if err := registerProtocol(exe); err != nil {
			applog.Warnf("notify", "注册 %s: 协议失败: %v", navigateScheme, err)
		}
		// 通知归属图标（IconUri）必须是一张图片：优先用落盘的 logo PNG，取不到才退回 exe。
		// 给 IconUri 填 .exe 路径时 WinRT 渲染不出，通知只会显示默认地球图标。
		icon := ""
		if a != nil {
			icon = a.toastLogoPath()
		}
		if err := registerAUMID(effectiveAppName(), exe, icon); err != nil {
			setToastDiag("登记 AppUserModelID 失败: " + err.Error())
			applog.Warnf("notify", "登记 AppUserModelID 失败: %v", err)
			return
		}
		setToastDiag("已登记（注册表 AppUserModelId + 协议），Toast 带 logo、可点击跳转")
		applog.Infof("notify", "Toast 身份登记完成（注册表）: AUMID=%s icon=%s", toastAUMID, icon)
	})
}

// setProcessAUMID 给当前进程显式设置 AppUserModelID（纯 shell32 调用，无 COM，安全）。
func setProcessAUMID(aumid string) {
	p, err := windows.UTF16PtrFromString(aumid)
	if err != nil {
		return
	}
	shell32 := windows.NewLazySystemDLL("shell32.dll")
	setAppID := shell32.NewProc("SetCurrentProcessExplicitAppUserModelID")
	_, _, _ = setAppID.Call(uintptr(unsafe.Pointer(p)))
}

// selfExePath 当前可执行文件的绝对路径。
func selfExePath() (string, error) { return os.Executable() }

// registerProtocol 把 <scheme>: 写进 HKCU\Software\Classes（无需管理员）。点 Toast 时
// Windows 用该协议以 <scheme>:<目标> 重新拉起本程序，主入口据此分流（见 navigate.go）。
func registerProtocol(exe string) error {
	key := `Software\Classes\` + navigateScheme
	root, _, err := registry.CreateKey(registry.CURRENT_USER, key, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer root.Close()
	_ = root.SetStringValue("", "URL:Tools Protocol")
	_ = root.SetStringValue("URL Protocol", "")
	cmd, _, err := registry.CreateKey(registry.CURRENT_USER, key+`\shell\open\command`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer cmd.Close()
	_ = cmd.SetStringValue("", fmt.Sprintf(`"%s" "%%1"`, exe))
	return nil
}

// registerAUMID 在 HKCU\Software\Classes\AppUserModelId\<AUMID> 写 DisplayName/IconUri，
// 把 AUMID 注册为本机通知应用。这是未打包应用让 Toast 归属本应用（logo/点击前提）的
// 纯注册表做法——完全不碰 COM，因此不会像 .lnk 快捷方式那样访问违例崩进程。
// iconPath 是通知归属图标的图片路径；为空才退回 exe（exe 当图标 WinRT 渲染不出）。
func registerAUMID(displayName, exe, iconPath string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, aumidRegPath(), registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if strings.TrimSpace(displayName) == "" {
		displayName = "Tools"
	}
	if err := k.SetStringValue("DisplayName", displayName); err != nil {
		return err
	}
	_ = k.SetDWordValue("ShowInSettings", 0)
	icon := iconPath
	if icon == "" {
		icon = exe
	}
	if icon != "" {
		// 通知设置列表里的图标；Toast 卡片内的 logo 走 XML 的 appLogoOverride（见 runToast）。
		_ = k.SetStringValue("IconUri", icon)
	}
	return nil
}

// setToastDiag 记录最近一次登记尝试的结果文本。
func setToastDiag(msg string) {
	toastDiagMu.Lock()
	toastRegisterMsg = msg
	toastDiagMu.Unlock()
}

// toastStatusText 汇总登记状态供前端「通知诊断」显示。
func toastStatusText() string {
	toastDiagMu.Lock()
	msg := toastRegisterMsg
	toastDiagMu.Unlock()
	if msg == "" {
		msg = "（本次启动尚未尝试登记）"
	}
	exe, _ := selfExePath()
	status := "未登记：Toast 无 logo、点击不跳转"
	if toastRegistered() {
		status = "已登记：Toast 带 logo，target 非空时可点击跳转"
	}
	return fmt.Sprintf("状态：%s\n最近尝试：%s\n程序：%s\nAUMID：%s\n注册表键：HKCU\\%s\n协议：%s:",
		status, msg, exe, toastAUMID, aumidRegPath(), navigateScheme)
}
