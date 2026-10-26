//go:build !windows

package php

// ============================================================
// PHP 扩展管理 —— 非 Windows 平台
//
// Windows 的扩展是 php.net 预编译的 php_<ext>.dll（见 pecl.go 主流程）；
// Linux 没有这套产物：扩展由发行版编译好随包分发（php-curl / php-gd …），
// 少数只在 pecl 上有的（redis/swoole/ssh2）需要 php-dev + gcc 现场编译。
//
// 所以这里不做下载，只回答三件事：装没装、开没开、下一步该执行什么命令。
// 下载 Windows dll 到 /usr/bin/ext 既不可能生效，也会让用户以为装上了。
// ============================================================

import (
	"fmt"
	"strings"

	"go-tools/services/sysx"
)

// extPkgName 扩展对应的发行版包名：Debian 与 RHEL 系的惯例都是 php-<ext>。
func extPkgName(ext string) string { return "php-" + ext }

// extInstallCmd 给用户的安装命令（非 root 或仓库里没有时用）
func extInstallCmd(ext string) string {
	mgr := sysx.PkgManager()
	if mgr == "" {
		return fmt.Sprintf("用发行版包管理器安装 %s", extPkgName(ext))
	}
	return fmt.Sprintf("sudo %s install -y %s", mgr, extPkgName(ext))
}

// extCatalogSystem 系统包模式下的扩展目录。
//
// Installed 看 php -m（系统扩展由 scan 目录加载），Enabled 看实例 php.ini；
// 没装的项直接把要执行的命令写进 Reason，前端原样展示即可。
func extCatalogSystem(cfg *InstanceConfig, id string) ([]RemoteExtensionInfo, error) {
	loaded := map[string]bool{}
	if names, err := LoadedExtensions(id); err == nil {
		for _, n := range names {
			loaded[strings.ToLower(strings.TrimSpace(n))] = true
		}
	}
	text, _ := GetConfigText(id)
	out := make([]RemoteExtensionInfo, 0, len(peclCatalog))
	for _, c := range peclCatalog {
		item := RemoteExtensionInfo{Name: c.Name, Label: c.Label, Description: c.Desc}
		item.Installed = loaded[strings.ToLower(c.Name)]
		item.Enabled = iniHasActiveExt(text, c.Name)
		if item.Installed {
			item.Matchable = true
			out = append(out, item)
			continue
		}
		item.Reason = "Linux 上扩展由发行版提供：" + extInstallCmd(c.Name) + "（装完重启实例生效）"
		out = append(out, item)
	}
	return out, nil
}

// installExtensionSystem 交给包管理器安装扩展。
//
// enable 参数在这里没有意义：发行版包会把 extension= 写进自己的 scan 目录，
// 面板不需要（也不该）再往实例 php.ini 里加一行。
func installExtensionSystem(cfg *InstanceConfig, ext string, enable bool) error {
	_ = cfg
	_ = enable
	pkg := extPkgName(ext)
	if len(sysx.PackageCandidates([]string{pkg})) == 0 {
		return fmt.Errorf("发行版仓库里没有 %s；该扩展可能只能编译安装：sudo pecl install %s（需要 php-dev/php-pear 与 gcc/make）", pkg, ext)
	}
	if !sysx.Caps().IsRoot {
		return fmt.Errorf("安装扩展需要 root：请在服务器执行 %s", extInstallCmd(ext))
	}
	if _, busy := peclBusy.Load(ext); busy {
		return fmt.Errorf("扩展 %s 正在安装中", ext)
	}
	peclBusy.Store(ext, struct{}{})
	go func() {
		defer peclBusy.Delete(ext)
		extEmit(InstallProgress{Version: ext, Stage: "info", Note: "正在安装 " + pkg + "…"})
		err := sysx.InstallPackages([]string{pkg}, func(line string) {
			if s := strings.TrimSpace(line); s != "" {
				extEmit(InstallProgress{Version: ext, Stage: "info", Note: s})
			}
		})
		if err != nil {
			extEmit(InstallProgress{Version: ext, Stage: "error", Note: err.Error()})
			return
		}
		extEmit(InstallProgress{Version: ext, Stage: "done", Percent: 100,
			Note: "已安装 " + pkg + "（发行版会写入自己的 scan 目录，重启实例后生效）"})
	}()
	return nil
}

// searchExtensionSystem Linux 没有“线上 dll 包”可深挖，只给包名与命令。
func searchExtensionSystem(ext string) (*ExtSearchResult, error) {
	return &ExtSearchResult{
		Reason: "Linux 上没有可下载的扩展包；请执行 " + extInstallCmd(ext) + "，" +
			"或 sudo pecl install " + ext + " 编译安装",
	}, nil
}

// removeExtensionSystem 系统包统一由包管理器卸载；面板只负责 ini 开关。
func removeExtensionSystem(ext string) error {
	mgr := sysx.PkgManager()
	if mgr == "" {
		return fmt.Errorf("系统包安装的扩展请用发行版包管理器卸载：卸载 %s", extPkgName(ext))
	}
	return fmt.Errorf("系统包安装的扩展请用包管理器卸载：sudo %s remove -y %s（只想关闭请用「禁用」）", mgr, extPkgName(ext))
}
