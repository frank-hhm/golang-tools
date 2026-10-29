//go:build windows

package nginx

import "Tools/services/winrun"

// hostsPath Windows 系统 hosts 文件位置。
const hostsPath = `C:\Windows\System32\drivers\etc\hosts`

// openHostsFile 以管理员权限（UAC）拉起记事本打开 hosts。
// hosts 位于受保护目录，非提权的编辑器能看不能存，因此这里直接走提权，
// 用户在 UAC 点"否"会返回可识别的错误（winrun.UACCancelledError）。
func openHostsFile() error {
	return winrun.OpenElevated("notepad.exe", hostsPath)
}
