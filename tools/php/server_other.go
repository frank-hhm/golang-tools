//go:build !windows

package php

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"go-tools/services/sysx"
)

// binFileName 可执行文件名（Linux 系统包的 php 不带扩展名）。
const binFileName = "php"

// systemPackaged Linux 上走系统包管理器安装。
//
// PHP 官方不提供 Linux 的便携二进制，扩展也是编译产物——只有发行版仓库
// 这一条路能一次装齐 php-cli 加常用扩展。
const systemPackaged = true

// systemPkgNames 系统包候选：cli 是面板跑 `php -S` 的最低需求。
var systemPkgNames = []string{"php-cli", "php"}

// perBinaryIni php.ini 是否放在二进制同级目录。
//
// Windows 绿色版把 php.ini 摆在 php.exe 边上（PHP 默认加载位置）；Linux 的
// /usr/bin 是系统目录，普通用户写不进去，实例配置只能放实例目录、用 -c 指定。
const perBinaryIni = false

// rewriteExtDir 是否由面板改写 php.ini 里的 extension_dir。
//
// Windows 绿色版的扩展示意放在 <php>/ext，只能由面板指过去；Linux 上系统
// php.ini 里的 extension_dir 本来就是对的，跟着发行版走，不要动。
const rewriteExtDir = false

// iniTemplateCandidates php.ini 模板候选（按优先级）。
//
// Linux 上的扩展清单散在 scan 目录（/etc/php.d、conf.d）里，只有"系统 load 的
// 那一份 php.ini"抄过来才有意义，所以先问 `php --ini`。
func iniTemplateCandidates(phpDir string) []string {
	var out []string
	if bin := sysx.Which(binFileName); bin != "" {
		if data, err := exec.Command(bin, "--ini").Output(); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				if !strings.HasPrefix(line, "Loaded Configuration File:") {
					continue
				}
				if p := strings.TrimSpace(strings.TrimPrefix(line, "Loaded Configuration File:")); p != "" && !strings.Contains(p, "(none)") {
					out = append(out, p)
				}
			}
		}
	}
	return append(out, filepath.Join(phpDir, "php.ini"), "/etc/php.ini")
}

// hideWindowAttr 非 Windows 无控制台窗口问题
func hideWindowAttr() *syscall.SysProcAttr { return nil }

// killTree 结束进程（POSIX 下直接 Kill）
func killTree(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

// findPidsByName 非 Windows 不做镜像名兜底
func findPidsByName(name string) []int { return nil }
