//go:build windows

package php

import "fmt"

// ============================================================
// PHP 扩展管理 —— Windows 平台
//
// Windows 走 pecl.go 里的“下载 php_<ext>.dll”主流程（systemPackaged=false），
// 下面的系统包实现只是为了让公共层的分支能编译；真被调用到说明平台判定错了。
// ============================================================

var errSystemPackagedOnly = fmt.Errorf("系统包模式的扩展管理只在 Linux 上可用")

func extCatalogSystem(*InstanceConfig, string) ([]RemoteExtensionInfo, error) {
	return nil, errSystemPackagedOnly
}

func installExtensionSystem(*InstanceConfig, string, bool) error {
	return errSystemPackagedOnly
}

func searchExtensionSystem(string) (*ExtSearchResult, error) {
	return nil, errSystemPackagedOnly
}

func removeExtensionSystem(string) error {
	return errSystemPackagedOnly
}
