// Package portman 端口管理面板引擎：本机 TCP/UDP 端口 ↔ 归属进程 的对照快照。
//
// 入口在首页「网络」卡片（/tools/portman），不注册为工具项、不进设置页。
//
// 成本口径照 tools/sniffer/procmap.go 踩过的坑：一轮快照 = 一次全连接表 + 一次进程
// 快照，绝不 per-PID 调 gopsutil 查名字（Windows 上每次 p.Name() 都要重开一次
// Toolhelp 快照）。exe 路径按 pid 缓存，只在进程名变了（pid 被复用）时才重查。
//
// 诚实边界：
//   - 非管理员看不到别的用户/系统进程持有的 socket 归属，这类行 pid 为 0、名字记 "?"，
//     照实显示，不猜。
//   - 只列本机端口（含出向连接），不做端口扫描、不做局域网发现。
//   - 结束进程 / 图标 / 定位 / 在线搜索全部复用 procman，本包只管端口表。
package portman

import (
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"

	psnet "github.com/shirou/gopsutil/v3/net"
	psprocess "github.com/shirou/gopsutil/v3/process"
)

// PortRow 一条监听/连接记录（平铺，过滤与聚合交给前端）。
type PortRow struct {
	Proto  string `json:"proto"`  // tcp | udp
	Listen bool   `json:"listen"` // 是否监听类（UDP 无连接状态，一律算监听）
	Laddr  string `json:"laddr"`  // 本地地址，已归一（去掉 v6 的 %zone 与 ::ffff: 前缀）
	Lport  int    `json:"lport"`
	Raddr  string `json:"raddr"` // 远程地址；监听行为空
	Rport  int    `json:"rport"`
	State  string `json:"state"` // LISTEN / ESTABLISHED / TIME_WAIT ...；UDP 为 "-"
	Pid    int    `json:"pid"`
	Name   string `json:"name"` // 进程名；取不到为 "?"
	Exe    string `json:"exe"`  // 可执行文件路径；权限不足时为空
}

// Summary 面板顶部概览。
type Summary struct {
	ListenCount int      `json:"listenCount"` // 监听端口数（按 协议+端口 去重）
	ConnCount   int      `json:"connCount"`   // 非监听（已连接/等待中）的记录行数
	ProcCount   int      `json:"procCount"`   // 占着端口的进程数（pid 去重）
	Hosts       []string `json:"hosts"`       // 本机主机名 + 网卡 IPv4，纯展示
}

// Snapshot 一次端口快照。
type Snapshot struct {
	Rows []PortRow `json:"rows"`
	Sum  Summary   `json:"sum"`
}

// SnapshotPorts 采一次端口表：一次全连接表 + 一次进程快照 + 少量 exe 路径补查。
func SnapshotPorts() (Snapshot, error) {
	conns, err := psnet.Connections("inet") // tcp+udp、v4+v6，各一次系统调用
	if err != nil {
		return Snapshot{}, err
	}
	names := procNames()

	pids := make([]int, 0, len(conns))
	seen := make(map[int]bool, 64)
	for _, c := range conns {
		if c.Pid > 0 && !seen[int(c.Pid)] {
			seen[int(c.Pid)] = true
			pids = append(pids, int(c.Pid))
		}
	}
	exes := exeMap(pids, names)

	rows := make([]PortRow, 0, len(conns))
	for _, c := range conns {
		proto := protoOf(c.Type)
		if proto == "" {
			continue // 不是 TCP/UDP 的 socket 不参与
		}
		r := PortRow{
			Proto: proto,
			Laddr: normIP(c.Laddr.IP),
			Lport: int(c.Laddr.Port),
			Raddr: normIP(c.Raddr.IP),
			Rport: int(c.Raddr.Port),
			State: normState(proto, c.Status),
			Pid:   int(c.Pid),
			Exe:   exes[int(c.Pid)],
		}
		r.Listen = proto == "udp" || r.State == "LISTEN"
		if n := names[c.Pid]; n != "" {
			r.Name = n
		} else {
			r.Name = "?"
		}
		rows = append(rows, r)
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Lport != rows[j].Lport {
			return rows[i].Lport < rows[j].Lport
		}
		if rows[i].Proto != rows[j].Proto {
			return rows[i].Proto < rows[j].Proto
		}
		if rows[i].Pid != rows[j].Pid {
			return rows[i].Pid < rows[j].Pid
		}
		return rows[i].Rport < rows[j].Rport
	})
	return Snapshot{Rows: rows, Sum: summarize(rows, hostAddrs())}, nil
}

// protoOf gopsutil 的 ConnectionStat.Type 是 socket 类型号（不是字符串！），
// Windows/Linux/Darwin 上都是 SOCK_STREAM=1 / SOCK_DGRAM=2。
func protoOf(sockType uint32) string {
	switch sockType {
	case syscall.SOCK_STREAM:
		return "tcp"
	case syscall.SOCK_DGRAM:
		return "udp"
	default:
		return ""
	}
}

// normIP 只去掉展示噪声：IPv6 的 %zone（%eth0 等）与 v4 映射前缀 ::ffff:。
// 地址本身不改写：0.0.0.0 与 :: 对开发者是熟面孔，替成「任意」反而把 v4/v6 绑定信息弄丢了。
func normIP(ip string) string {
	if i := strings.IndexByte(ip, '%'); i >= 0 {
		ip = ip[:i]
	}
	return strings.TrimPrefix(ip, "::ffff:")
}

// normState 状态文本：UDP 没有连接状态（gopsutil 返回空串），记成 "-"，
// 是否监听由 PortRow.Listen 表达（UDP 一律算监听），不在这里撒谎。
func normState(proto, status string) string {
	if proto == "udp" {
		return "-"
	}
	if status == "" {
		return "-"
	}
	return strings.ToUpper(status)
}

// summarize 汇总：监听端口数按「协议:端口」去重（同一个端口多地址/多进程只算一个），
// 连接数按行计，进程数按 pid 去重（0 是「看不到归属」，不算一个进程）。
func summarize(rows []PortRow, hosts []string) Summary {
	s := Summary{Hosts: hosts}
	seenPort := map[string]bool{}
	seenPid := map[int]bool{}
	for _, r := range rows {
		if r.Listen {
			key := r.Proto + ":" + strconv.Itoa(r.Lport)
			if !seenPort[key] {
				seenPort[key] = true
				s.ListenCount++
			}
		} else {
			s.ConnCount++
		}
		if r.Pid > 0 && !seenPid[r.Pid] {
			seenPid[r.Pid] = true
			s.ProcCount++
		}
	}
	return s
}

// exeCap 一轮最多补多少个 exe 路径：极端情况（几百个进程持有 socket）下宁可留空，
// 也不把一次快照变成长 IO。
const exeCap = 200

// exeCacheMax 缓存条数上限：pid 会随进程退出失效，攒太多直接整表重来（比逐个淘汰简单可靠）。
const exeCacheMax = 2048

type exeEntry struct {
	name string // 记录当时的进程名，用来识别 pid 复用
	exe  string
}

var (
	exeMu    sync.Mutex
	exeCache = map[int]exeEntry{}
)

// exeMap 给本轮真正持有 socket 的 pid 补可执行文件路径（定位/在线搜索要用）。
// 只有几十分之一秒的 OpenProcess 代价，且命中缓存后为零。
func exeMap(pids []int, names map[int32]string) map[int]string {
	out := make(map[int]string, len(pids))
	var toQuery []int
	exeMu.Lock()
	if len(exeCache) > exeCacheMax {
		exeCache = map[int]exeEntry{}
	}
	for _, pid := range pids {
		name := names[int32(pid)]
		if e, hit := exeCache[pid]; hit && e.name == name {
			out[pid] = e.exe
			continue
		}
		toQuery = append(toQuery, pid)
	}
	exeMu.Unlock()

	if len(toQuery) > exeCap {
		toQuery = toQuery[:exeCap]
	}
	for _, pid := range toQuery {
		exe := ""
		if p, err := psprocess.NewProcess(int32(pid)); err == nil {
			if e, err := p.Exe(); err == nil {
				exe = e
			}
		}
		out[pid] = exe
		exeMu.Lock()
		exeCache[pid] = exeEntry{name: names[int32(pid)], exe: exe}
		exeMu.Unlock()
	}
	return out
}

// hostAddrs 本机主机名 + 非回环 IPv4（只用来认机器；不做任何探测）。
func hostAddrs() []string {
	out := make([]string, 0, 4)
	if h, err := os.Hostname(); err == nil && h != "" {
		out = append(out, h)
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return out
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok || ipn.IP.IsLoopback() {
			continue
		}
		if v4 := ipn.IP.To4(); v4 != nil {
			out = append(out, v4.String())
		}
	}
	if len(out) > 6 {
		out = out[:6]
	}
	return out
}
