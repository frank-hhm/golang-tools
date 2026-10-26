//go:build !windows

package redis

import "syscall"

// binFileName 可执行文件名。
//
// Linux 的系统包装的是不带扩展名的 redis-server；插件目录里的版本同样如此，
// 只有 Windows 的绿色版叫 redis-server.exe。找文件的地方统一用它。
const binFileName = "redis-server"

// systemPackaged 是否走系统包管理器安装。
//
// Linux 上 Redis 官方只发源码，没有可下载的便携二进制，所以"在线安装"只能是
// "让发行版仓库装"。Windows 上是自带的 msys2 构建 zip，保持原样。
const systemPackaged = true

// systemPkgNames 系统包候选（按优先级）。
//
// RHEL 系叫 redis，Debian 系叫 redis-server，列两个让查询自己挑。
var systemPkgNames = []string{"redis", "redis-server"}

// hideWindowAttr 非 Windows 平台无需隐藏窗口
func hideWindowAttr() *syscall.SysProcAttr {
	return nil
}
