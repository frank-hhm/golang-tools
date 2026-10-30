//go:build !windows

package portman

import psprocess "github.com/shirou/gopsutil/v3/process"

// procNames 非 Windows：一次 Pids() + 每 pid 读 /proc，没有 Windows 那套「每次调用重开
// 全表快照」的坑，直接按 sniffer 的老办法走 gopsutil。
func procNames() map[int32]string {
	pids, err := psprocess.Pids()
	if err != nil {
		return map[int32]string{}
	}
	out := make(map[int32]string, len(pids))
	for _, pid := range pids {
		p, err := psprocess.NewProcess(pid)
		if err != nil {
			continue
		}
		if name, err := p.Name(); err == nil && name != "" {
			out[pid] = name
		}
	}
	return out
}
