//go:build !windows

package nginx

import (
	"fmt"

	"go-tools/services/sysx"
)

// hostsPath Linux/其它 POSIX 系统的 hosts 文件位置。
const hostsPath = "/etc/hosts"

// openHostsFile 交给桌面默认程序打开 hosts。
//
// 与 Windows 的本质差别：这里没有 UAC 一键提权。/etc/hosts 归 root 所有，
// 图形编辑器默认同样写不进去；无头服务器（面板 Web 模式）更根本没有桌面程序，
// xdg-open 会直接失败。因此如实把现实写进错误里，引导用户用 sudo 编辑。
func openHostsFile() error {
	if err := sysx.Open().File(hostsPath); err != nil {
		return fmt.Errorf("打开 hosts 失败：%v（Linux 下请用 sudo 编辑 %s）", err, hostsPath)
	}
	return nil
}
