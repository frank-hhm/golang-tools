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
func preparePrefixDirs(prefix string) error {
	return filepath.Walk(filepath.Join(prefix, "temp"), func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || !info.IsDir() {
			return nil
		}
		if chErr := os.Chmod(p, 0o777); chErr != nil && !os.IsPermission(chErr) {
			return chErr
		}
		return nil
	})
}

// killTree 结束进程（POSIX 下直接 Kill）
func killTree(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}
