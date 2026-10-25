//go:build windows

package sniffer

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// procNames 一次 Toolhelp32 快照拿到全表 pid -> 进程名。
//
// 为什么不用 gopsutil 的 p.Name()：它在 Windows 上每次调用都新开一个
// CreateToolhelp32Snapshot 再遍历到目标 pid，几百个进程就是几百次快照，
// 而这正是每 1.5 秒都要跑一遍的刷新里最贵的一笔。整表一次快照一次遍历即可。
func procNames() map[int32]string {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
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
