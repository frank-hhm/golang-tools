//go:build !windows

package sniffer

import (
	"github.com/shirou/gopsutil/v3/process"
)

// procNames 非 Windows 兜底：逐个 PID 读名字（Linux 走 /proc，一次读一个文件，
// 没有 Windows 那种「每个进程一次快照」的放大问题）。
func procNames() map[int32]string {
	pids, err := process.Pids()
	if err != nil {
		return nil
	}
	out := make(map[int32]string, len(pids))
	for _, pid := range pids {
		p, err := process.NewProcess(pid)
		if err != nil {
			continue
		}
		if name, err := p.Name(); err == nil && name != "" {
			out[pid] = name
		}
	}
	return out
}
