//go:build windows

package launcher

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// revealPath 在资源管理器里打开所在目录并选中该文件。
//
// 命令行必须原样交给 CreateProcess：走 exec.Command 传参时，Go 会把含空格的整段
// （连 /select, 前缀一起）用引号括起来，explorer 自己的解析认不出来，
// 结果是弹个默认页而不是定位到目标（带空格的路径必现）。
func revealPath(p string) error {
	cmdLine := `explorer.exe /select,"` + p + `"`
	lp, err := windows.UTF16PtrFromString(cmdLine)
	if err != nil {
		return err
	}
	var si windows.StartupInfo
	si.Cb = uint32(unsafe.Sizeof(si))
	var pi windows.ProcessInformation
	// explorer 起来就退、退出码还常常是 1，所以只当"拉起来了"看待，不等结果。
	if err := windows.CreateProcess(nil, lp, nil, nil, false, 0, nil, nil, &si, &pi); err != nil {
		return err
	}
	windows.CloseHandle(pi.Process)
	windows.CloseHandle(pi.Thread)
	return nil
}
