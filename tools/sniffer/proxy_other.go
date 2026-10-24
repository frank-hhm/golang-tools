//go:build !windows

package sniffer

import "fmt"

// 非 Windows 平台的占位实现（本项目主要面向 Windows）

func SetSystemProxy(port int) error {
	return fmt.Errorf("系统代理设置仅支持 Windows")
}

func ClearSystemProxy() error {
	return fmt.Errorf("系统代理设置仅支持 Windows")
}

func InstallCA(certPath string) error {
	return fmt.Errorf("证书安装仅支持 Windows，请手动信任根证书: %s", certPath)
}

func UninstallCA() error {
	return nil
}
