package sniffer

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/process"
)

// ProcessInfo 端口关联的进程信息
type ProcessInfo struct {
	Pid  int32
	Name string
}

// ProcessMap 维护 本地端口 -> 进程 的映射，供 MITM 归属请求发起应用
type ProcessMap struct {
	mu    sync.RWMutex
	byPid map[uint16]ProcessInfo
}

// Start 启动后台协程，定期刷新端口映射
func (pm *ProcessMap) Start(ctx context.Context) {
	pm.refresh()
	go func() {
		ticker := time.NewTicker(1500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				pm.refresh()
			}
		}
	}()
}

// Lookup 根据本地端口查找进程
func (pm *ProcessMap) Lookup(port uint16) ProcessInfo {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	return pm.byPid[port]
}

func (pm *ProcessMap) refresh() {
	newMap := make(map[uint16]ProcessInfo)
	pids, err := process.Pids()
	if err != nil {
		return
	}
	for _, pid := range pids {
		p, err := process.NewProcess(pid)
		if err != nil {
			continue
		}
		name, err := p.Name()
		if err != nil || name == "" {
			continue
		}
		name = strings.TrimSuffix(name, ".exe")
		conns, err := p.Connections()
		if err != nil {
			continue
		}
		for _, conn := range conns {
			lp := uint16(conn.Laddr.Port)
			if lp > 0 {
				if _, exists := newMap[lp]; !exists {
					newMap[lp] = ProcessInfo{Pid: pid, Name: name}
				}
			}
		}
	}
	pm.mu.Lock()
	pm.byPid = newMap
	pm.mu.Unlock()
}
