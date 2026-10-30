//go:build windows

package portman

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// procNames 一次 Toolhelp32 快照拿全表 pid -> 进程名。
// 不用 gopsutil 的 p.Name()：它在 Windows 上每次调用都重开一次进程快照，
// 几百个 socket 归属就是几百次全表遍历（tools/sniffer 为此专门拆了平台文件）。
func procNames() map[int32]string {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return map[int32]string{}
	}
	defer windows.CloseHandle(snap)

	out := map[int32]string{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		out[int32(e.ProcessID)] = windows.UTF16ToString(e.ExeFile[:])
	}
	return out
}
