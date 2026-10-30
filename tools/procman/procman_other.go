//go:build !windows

package procman

import (
	"path/filepath"

	"Tools/services/sysx"

	"github.com/shirou/gopsutil/v3/process"
)

// collectProcs 非 Windows 走 gopsutil（没有 Windows 那套每进程重开快照的坑，
// 这里的调用频率下开销可接受）。
func collectProcs() []rawProc {
	ps, err := process.Processes()
	if err != nil {
		return nil
	}
	out := make([]rawProc, 0, len(ps))
	for _, p := range ps {
		name, _ := p.Name()
		row := ProcRow{Pid: int(p.Pid), Name: name}
		if pp, err := p.Ppid(); err == nil {
			row.Ppid = int(pp)
		}
		if exe, err := p.Exe(); err == nil {
			row.Exe = exe
		}
		if ct, err := p.CreateTime(); err == nil {
			row.StartedAt = ct
		}
		if nt, err := p.NumThreads(); err == nil {
			row.Threads = int(nt)
		}
		r := rawProc{}
		if mi, err := p.MemoryInfo(); err == nil && mi != nil {
			row.WorkingSet = mi.RSS
			row.PrivateBytes = mi.VMS
		}
		if t, err := p.Times(); err == nil && t != nil {
			r.cpuSec = t.User + t.System
			r.ok = true
		}
		r.row = row
		out = append(out, r)
	}
	return out
}

func queryVer(string) (string, string) { return "", "" }

func killProcs([]int) error             { return ErrUnsupported }
func suspendProc(int, bool) error       { return ErrUnsupported }
func restartProc(int) error             { return ErrUnsupported }
func setProcPriority(int, string) error { return ErrUnsupported }
func iconOfExe(string) string           { return "" }

func procInfo(pid int) (ProcInfo, error) {
	mu.Lock()
	row, hit := lastRows[pid]
	parent := lastRows[row.Ppid].Name
	mu.Unlock()
	if !hit {
		return ProcInfo{}, ErrUnsupported
	}
	info := ProcInfo{ProcRow: row, ParentName: parent}
	if p, err := process.NewProcess(int32(pid)); err == nil {
		if c, err := p.Cmdline(); err == nil {
			info.Cmdline = c
		}
		if u, err := p.Username(); err == nil {
			info.User = u
		}
	}
	return info, nil
}

func openExePath(exe string) error {
	return sysx.Open().Dir(filepath.Dir(exe))
}
