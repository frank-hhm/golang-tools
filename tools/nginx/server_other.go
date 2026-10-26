//go:build !windows

package nginx

import (
	"os"
	"path/filepath"
	"syscall"
)

// 平台事实：Linux 的系统包把 nginx 装成无扩展名的 /usr/sbin/nginx，
// 且没有“解压一份到 plugins 目录”这条路，安装只能走发行版仓库。
const binFileName = "nginx"
const systemPackaged = true

// RHEL/CentOS 与 Debian/Ubuntu 的包名一致，都是 nginx
var systemPkgNames = []string{"nginx"}

// systemConfDir 系统 nginx 的配置目录（mime.types / fastcgi_params 在这里）
const systemConfDir = "/etc/nginx"

// hideWindowAttr 非 Windows 无控制台窗口问题
func hideWindowAttr() *syscall.SysProcAttr { return nil }

// preparePrefixDirs 让 worker 能写临时目录。
//
// 发行版的 nginx 编译时带了 --user=nginx（Debian 是 www-data）：master 以 root 跑，
// worker 降权；实例目录是 root 建的 0755，worker 连 client_body_temp 都写不进去，
// 上传直接 500。nginx 只会往这些目录里落临时文件，放开宽度不影响配置安全。
//
// 必须在启动前把子目录建出来：目录不存在时只 chmod 已存在项等于什么都没做，
// master 会用 0755 自建，worker 依旧写不进去。
func preparePrefixDirs(prefix string) error {
	tempRoot := filepath.Join(prefix, "temp")
	dirs := []string{tempRoot}
	for _, name := range []string{"client_body_temp", "proxy_temp", "fastcgi_temp", "uwsgi_temp", "scgi_temp"} {
		dirs = append(dirs, filepath.Join(tempRoot, name))
	}
	for _, d := range dirs {
		// MkdirAll 的权限会被 umask 衰减，所以建完再显式改一次
		if err := os.MkdirAll(d, 0o777); err != nil && !os.IsPermission(err) {
			return err
		}
		if err := os.Chmod(d, 0o777); err != nil && !os.IsPermission(err) {
			return err
		}
	}
	return nil
}

// killTree 结束进程（POSIX 下直接 Kill）
func killTree(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
