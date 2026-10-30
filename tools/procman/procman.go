// Package procman 进程管理面板引擎：进程快照 + 结束/挂起/重启/优先级/属性等操作。
//
// 入口在首页 CPU 卡片（/tools/procman，不注册为工具项）。
//
// 诚实边界：
//   - 非管理员时结束进程自动走 winrun 提权 taskkill 兜底（UAC 一次）；
//     挂起/恢复/重启/优先级失败直接报「需要管理员权限」，不做连环提权。
//   - 挂起状态无法低成本回显，不提供「已挂起」列。
//   - 非 Windows：快照可看（gopsutil），全部操作返回 ErrUnsupported。
//   - 图标 / 描述 / 公司名都是 best-effort：取不到留空，绝不阻塞快照。
package procman

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"Tools/services/sysx"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
)

// ProcRow 一个进程的列表行（平铺数据，进程树由前端按 ppid 组装）。
type ProcRow struct {
	Pid          int     `json:"pid"`
	Ppid         int     `json:"ppid"`
	Name         string  `json:"name"`
	Exe          string  `json:"exe"`
	CpuPct       float64 `json:"cpuPct"`
	PrivateBytes uint64  `json:"privateBytes"`
	WorkingSet   uint64  `json:"workingSet"`
	Description  string  `json:"description"`
	Company      string  `json:"company"`
	StartedAt    int64   `json:"startedAt"` // 毫秒时间戳，取不到为 0
	Threads      int     `json:"threads"`
}

// Summary 底部状态栏：CPU 使用 / 物理内存 / 提交负荷 / 进程数 + 本应用 pid（前端高亮用）。
type Summary struct {
	CpuPct    float64 `json:"cpuPct"`
	MemPct    float64 `json:"memPct"`
	CommitPct float64 `json:"commitPct"`
	ProcCount int     `json:"procCount"`
	OwnPid    int     `json:"ownPid"`
}

// Snapshot 一次快照结果。
type Snapshot struct {
	Procs []ProcRow `json:"procs"`
	Sum   Summary   `json:"sum"`
}

// ProcInfo 属性弹窗数据（ProcRow + 只有点开才值得取的重字段）。
type ProcInfo struct {
	ProcRow
	Cmdline    string `json:"cmdline"`
	User       string `json:"user"`
	ParentName string `json:"parentName"`
	Bitness    string `json:"bitness"`
}

// ErrUnsupported 非 Windows 平台调用操作类函数时返回。
var ErrUnsupported = errors.New("当前平台不支持该操作")

// rawProc 平台采集层的原始行：CPU% 由公共层差分算出，平台只管报累计秒数。
type rawProc struct {
	row    ProcRow
	cpuSec float64 // 进程累计 CPU 时间（用户+内核，秒）
	ok     bool    // 能否打开进程取到计时/内存（系统进程常为 false）
}

// 平台实现入口（无构建标签的前向声明 Go 写不了）：
// collectProcs / killProcs / suspendProc / restartProc / setProcPriority /
// procInfo / iconOfExe / openExePath / queryVer 定义在 procman_windows.go 与
// procman_other.go，由下方与 SnapshotProcs 等公共逻辑直接调用。

var (
	cores = float64(runtime.NumCPU())

	mu       sync.Mutex
	prev     = map[int]procSample{}   // pid -> 上次采样的累计 CPU 时间（差分算 CPU%）
	lastRows = map[int]ProcRow{}      // 最近一次快照，树杀/属性/图标都查这份缓存
	verCache = map[string][2]string{} // 大写 exe 路径 -> {描述, 公司}，永久缓存
	verQueue chan string              // 后台预热队列（快照从不阻塞在文件 IO 上）
	verOnce  sync.Once
)

type procSample struct {
	cpuSec float64
	ts     time.Time
}

// SnapshotProcs 采一次全量快照：行数据 + CPU 差分 + 版本信息（走缓存）+ 状态栏汇总。
func SnapshotProcs() Snapshot {
	now := time.Now()
	raws := collectProcs()

	mu.Lock()
	seen := make(map[int]bool, len(raws))
	procs := make([]ProcRow, 0, len(raws))
	for _, r := range raws {
		row := r.row
		if r.ok {
			if p, hit := prev[row.Pid]; hit {
				row.CpuPct = cpuPct(p.cpuSec, r.cpuSec, now.Sub(p.ts).Seconds(), cores)
			}
			prev[row.Pid] = procSample{cpuSec: r.cpuSec, ts: now}
		}
		seen[row.Pid] = true
		if row.Exe != "" {
			key := normPath(row.Exe)
			if v, hit := verCache[key]; hit {
				row.Description, row.Company = v[0], v[1]
			} else {
				queueVer(key, row.Exe)
			}
		}
		lastRows[row.Pid] = row
		procs = append(procs, row)
	}
	for pid := range prev {
		if !seen[pid] {
			delete(prev, pid)
		}
	}
	for pid := range lastRows {
		if !seen[pid] {
			delete(lastRows, pid)
		}
	}
	mu.Unlock()

	sort.Slice(procs, func(i, j int) bool { return procs[i].Pid < procs[j].Pid })
	return Snapshot{Procs: procs, Sum: summarize(len(procs))}
}

// cpuPct 纯函数：两次累计 CPU 秒差 / (墙钟秒 × 核数) × 100，负差与冷启动都归 0。
func cpuPct(prevSec, curSec, wall, cores float64) float64 {
	if wall <= 0 || cores <= 0 || curSec < prevSec {
		return 0
	}
	pct := (curSec - prevSec) / (wall * cores) * 100
	if pct < 0 {
		return 0
	}
	return pct
}

// summarize 状态栏数据：cpu.Percent(0) 非阻塞（与上次调用差分），内存/提交走 gopsutil。
func summarize(n int) Summary {
	s := Summary{ProcCount: n, OwnPid: os.Getpid()}
	if p, err := cpu.Percent(0, false); err == nil && len(p) > 0 {
		s.CpuPct = p[0]
	}
	if m, err := mem.VirtualMemory(); err == nil {
		s.MemPct = m.UsedPercent
	}
	// 提交负荷 ≈ 页面文件使用率（Windows 语义；其他平台 swap 常为 0，留 0 不显示假数）
	if sw, err := mem.SwapMemory(); err == nil && sw.Total > 0 {
		s.CommitPct = sw.UsedPercent
	}
	return s
}

// Descendants pid 自身 + 最近快照里的全部子孙（跳 System/自身），供「结束进程树」。
func Descendants(pid int) []int {
	mu.Lock()
	defer mu.Unlock()
	children := map[int][]int{}
	for p, row := range lastRows {
		children[row.Ppid] = append(children[row.Ppid], p)
	}
	return descendants(children, pid)
}

// descendants 纯函数版（供单测）：children 是 ppid -> 子 pid 表；
// 根总是展开，但 System(4)/0/自身进程不会进入可杀集合。
func descendants(children map[int][]int, root int) []int {
	self := os.Getpid()
	out := []int{}
	stack := []int{root}
	seen := map[int]bool{}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if seen[cur] {
			continue
		}
		seen[cur] = true
		if cur != 0 && cur != 4 && cur != self {
			out = append(out, cur)
		}
		stack = append(stack, children[cur]...)
	}
	return out
}

// Kill 结束一个进程或整棵进程树。Windows 下权限不足自动提权（一次 UAC）。
func Kill(pid int, tree bool) error {
	if pid == os.Getpid() {
		return errors.New("不能结束本应用自己的进程")
	}
	if pid == 4 || pid == 0 {
		return errors.New("不能结束系统进程")
	}
	pids := []int{pid}
	if tree {
		pids = Descendants(pid)
	}
	if len(pids) == 0 {
		return errors.New("没有可结束的进程")
	}
	return killProcs(pids)
}

// Suspend 挂起（on=true）/ 恢复（on=false）进程。
func Suspend(pid int, on bool) error { return suspendProc(pid, on) }

// Restart 以原路径与命令行重启进程（先杀再起；起不来提示原进程可能是管理员权限）。
func Restart(pid int) error { return restartProc(pid) }

// SetPriority 调整优先级：high | normal | low。
func SetPriority(pid int, class string) error { return setProcPriority(pid, class) }

// Info 进程属性（重字段：命令行/用户/位数只在这里取）。
func Info(pid int) (ProcInfo, error) { return procInfo(pid) }

// Icon 进程图标的 PNG dataURL；取不到返回 ""。
func Icon(pid int) string {
	mu.Lock()
	exe := lastRows[pid].Exe
	mu.Unlock()
	return iconOfExe(exe)
}

// IconOf 按 exe 路径取图标：给「手里只有路径、进程不在本包快照里」的调用方用（如端口面板）。
func IconOf(exe string) string { return iconOfExe(exe) }

// OpenPath 在系统文件管理器中定位进程 exe。
func OpenPath(pid int) error {
	mu.Lock()
	exe := lastRows[pid].Exe
	mu.Unlock()
	return OpenExe(exe)
}

// OpenExe 按 exe 路径定位（端口面板这类不走本包快照的入口用）。
func OpenExe(exe string) error {
	if exe == "" {
		return errors.New("未取到可执行文件路径（可能是权限不足）")
	}
	return openExePath(exe)
}

// OpenVt 计算进程 exe 的 SHA256，用系统默认浏览器打开 VirusTotal 文件页。
func OpenVt(pid int) error {
	mu.Lock()
	exe := lastRows[pid].Exe
	mu.Unlock()
	return OpenVtOf(exe)
}

// OpenVtOf 同 OpenVt，但直接给路径（不依赖本包快照）。
func OpenVtOf(exe string) error {
	if exe == "" {
		return errors.New("未取到进程的可执行文件路径")
	}
	f, err := os.Open(exe)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	return sysx.Open().URL("https://www.virustotal.com/gui/file/" + hex.EncodeToString(h.Sum(nil)))
}

// queueVer 把没查过版本信息的路径丢进预热队列；队列满说明后台忙，丢掉即可。
func queueVer(key, path string) {
	verOnce.Do(func() {
		verQueue = make(chan string, 512)
		go func() {
			for p := range verQueue {
				desc, company := queryVer(p)
				mu.Lock()
				verCache[normPath(p)] = [2]string{desc, company}
				mu.Unlock()
			}
		}()
	})
	select {
	case verQueue <- path:
	default:
	}
}

func normPath(p string) string { return strings.ToUpper(p) }
