package sniffer

import (
	"context"
	"strings"
	"sync"
	"time"

	psnet "github.com/shirou/gopsutil/v3/net"
)

// ProcessInfo 端口关联的进程信息
type ProcessInfo struct {
	Pid  int32
	Name string
}

// ProcessMap 维护 本地端口 -> 进程 的映射，供 MITM 归属请求发起应用。
//
// 刷新成本是这块的全部风险：早期实现是「遍历每个 PID → 打开进程 → 查名字 →
// 查它自己的连接」，而 gopsutil 在 Windows 上
//   - p.Name() 每次都要 CreateToolhelp32Snapshot 一次；
//   - p.Connections() 每次都把整张 ExtendedTcpTable/UdpTable 拉一遍再按 pid 过滤。
//
// 于是 300 个进程的一轮刷新 = 300 次进程快照 + 1200 次全表读取，每 1.5 秒来一遍，
// 空闲机器上就能吃掉一个核。现在收敛成「一次全表 + 一次进程快照」。
type ProcessMap struct {
	mu      sync.RWMutex
	byPort  map[uint16]ProcessInfo
	running bool
}

// Start 启动后台协程，定期刷新端口映射；同一实例重复调用只会有一份协程。
func (pm *ProcessMap) Start(ctx context.Context) {
	pm.mu.Lock()
	if pm.running {
		pm.mu.Unlock()
		return
	}
	pm.running = true
	pm.mu.Unlock()

	pm.refresh()
	go func() {
		ticker := time.NewTicker(refreshEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				pm.mu.Lock()
				pm.running = false
				pm.mu.Unlock()
				return
			case <-ticker.C:
				pm.refresh()
			}
		}
	}()
}

// refreshEvery 端口归属的刷新节奏：一次全表 + 一次进程快照的代价很小，
// 1.5 秒足够跟上「刚启动的进程开始发请求」这类变化，不必更密。
const refreshEvery = 1500 * time.Millisecond

// Lookup 根据本地端口查找进程
func (pm *ProcessMap) Lookup(port uint16) ProcessInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.byPort[port]
}

func (pm *ProcessMap) refresh() {
	conns, err := psnet.Connections("inet") // tcp+udp，v4+v6，一次系统调用一套表
	if err != nil {
		// 拿不到表（权限、瞬时失败）就保留上一轮结果，不把映射清空导致面板全变未知
		return
	}
	names := procNames()

	newMap := make(map[uint16]ProcessInfo, len(conns))
	for _, c := range conns {
		lp := uint16(c.Laddr.Port)
		if lp == 0 {
			continue
		}
		if _, exists := newMap[lp]; exists {
			continue
		}
		name := names[c.Pid]
		if name == "" {
			continue
		}
		newMap[lp] = ProcessInfo{Pid: c.Pid, Name: strings.TrimSuffix(name, ".exe")}
	}

	pm.mu.Lock()
	pm.byPort = newMap
	pm.mu.Unlock()
}

// sharedPorts 全局共享的一份端口归属：MITM 与 pcap 抓包同时开时也只扫一份
var sharedPorts = &ProcessMap{}

// startSharedPortMap 供 pcap 引擎在抓包期间复用同一份端口映射
func startSharedPortMap(ctx context.Context) { sharedPorts.Start(ctx) }

// lookupPort 查共享映射里的端口归属
func lookupPort(port uint16) ProcessInfo { return sharedPorts.Lookup(port) }
