//go:build windows

package sniffer

import (
	"fmt"
	"os/exec"
	"runtime"
	"syscall"
	"time"

	"golang.org/x/sys/windows/registry"
)

const inetSettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

var (
	wininet               = syscall.NewLazyDLL("wininet.dll")
	internetSetOptionW    = wininet.NewProc("InternetSetOptionW")
	internetOptionChanged = uintptr(39) // INTERNET_OPTION_SETTINGS_CHANGED
	internetOptionRefresh = uintptr(37) // INTERNET_OPTION_REFRESH
)

// savedProxy 记录修改前的系统代理，用于停止时还原
type savedProxy struct {
	enabled bool
	server  string
	saved   bool
}

var lastProxy savedProxy

// applyProxyChanges 通知系统立即刷新代理设置
func applyProxyChanges() {
	internetSetOptionW.Call(0, internetOptionChanged, 0, 0)
	internetSetOptionW.Call(0, internetOptionRefresh, 0, 0)
	runtime.GC()
	_ = time.Nanosecond
}

// SetSystemProxy 备份原设置并将系统代理指向 127.0.0.1:port
func SetSystemProxy(port int) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开代理注册表失败: %w", err)
	}
	defer k.Close()

	// 备份
	if !lastProxy.saved {
		en, _, errEn := k.GetIntegerValue("ProxyEnable")
		srv, _, errSrv := k.GetStringValue("ProxyServer")
		if errEn == registry.ErrNotExist && errSrv == registry.ErrNotExist {
			lastProxy = savedProxy{enabled: false, server: "", saved: true}
		} else {
			lastProxy = savedProxy{enabled: en == 1, server: srv, saved: true}
		}
	}

	if err := k.SetStringValue("ProxyServer", fmt.Sprintf("127.0.0.1:%d", port)); err != nil {
		return err
	}
	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return err
	}
	applyProxyChanges()
	return nil
}

// ClearSystemProxy 还原为修改前的系统代理设置
func ClearSystemProxy() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, inetSettingsKey, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if lastProxy.saved {
		if lastProxy.server != "" {
			k.SetStringValue("ProxyServer", lastProxy.server)
		}
		v := uint32(0)
		if lastProxy.enabled {
			v = 1
		}
		k.SetDWordValue("ProxyEnable", v)
		lastProxy = savedProxy{}
	} else {
		k.SetDWordValue("ProxyEnable", 0)
	}
	applyProxyChanges()
	return nil
}

// hideWindow 隐藏子进程控制台窗口
func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
}

// InstallCA 将根证书安装到当前用户的受信任根存储（无需管理员）
func InstallCA(certPath string) error {
	cmd := exec.Command("certutil", "-addstore", "-user", "Root", certPath)
	hideWindow(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("安装证书失败: %v, %s", err, string(out))
	}
	return nil
}

// UninstallCA 从当前用户受信任根存储移除根证书
func UninstallCA() error {
	cmd := exec.Command("certutil", "-delstore", "-user", "Root", CACommonName)
	hideWindow(cmd)
	// 即使未找到也忽略错误
	_ = cmd.Run()
	return nil
}
