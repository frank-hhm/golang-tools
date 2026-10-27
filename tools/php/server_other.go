//go:build !windows

package php

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/shirou/gopsutil/v3/process"

	"go-tools/services/sysx"
)

// binFileName 可执行文件名（Linux 系统包的 php 不带扩展名）。
const binFileName = "php"

// cgiFileName FastCGI 后端可执行文件名（Linux 上属单独的 php-cgi 包，可能未安装）。
const cgiFileName = "php-cgi"

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

// extIniValue 决定写进 php.ini 的扩展加载值。
//
// Linux 上扩展是 extension_dir 里的 <name>.so（多数由发行版包提供、注册在
// scan 目录里），写扩展名即可，php 自己补 .so；也不能像 Windows 那样要求
// “dll 必须在 <php>/ext 里才允许启用”，否则系统装的扩展一个都开不了。
func extIniValue(name, phpDir string) (string, bool) {
	_ = phpDir
	return name, true
}

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

// findPidsByName 按进程名找 PID。
//
// Windows 靠 tasklist 解析镜像名；Linux 没有“镜像名”这层包装，直接看进程 comm：
// 系统包 php-cli 的主进程名就是 php，个别发行版的包装脚本会让它带版本后缀
// （php8.2），一并认出来，但 php-fpm 这类带横线的名字不会被误伤。
func findPidsByName(name string) []int {
	want := strings.ToLower(strings.TrimSuffix(name, ".exe"))
	procs, err := process.Processes()
	if err != nil {
		return nil
	}
	var out []int
	for _, p := range procs {
		n, err := p.Name()
		if err != nil {
			continue
		}
		if !sameBinaryName(strings.ToLower(strings.TrimSpace(n)), want) {
			continue
		}
		if pid := int(p.Pid); pid > 0 && pid != os.Getpid() {
			out = append(out, pid)
		}
	}
	return out
}

// sameBinaryName 判断进程名是不是同一个二进制：完全同名，或“名字 + 纯版本后缀”。
// 后者覆盖 Debian 系的 php8.2 / php8.3 这类带版本号的进程名。
func sameBinaryName(got, want string) bool {
	if got == want {
		return true
	}
	if !strings.HasPrefix(got, want) {
		return false
	}
	for _, r := range got[len(want):] {
		if (r < '0' || r > '9') && r != '.' {
			return false
		}
	}
	return true
}
