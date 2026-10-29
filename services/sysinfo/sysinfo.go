package sysinfo

import (
	"fmt"
	"runtime"
	"time"

	"Tools/tools/docker"
	"Tools/tools/mysql"
	"Tools/tools/nginx"
	"Tools/tools/npm"
	"Tools/tools/php"
	"Tools/tools/redis"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/host"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/net"
)

// SystemInfo 系统概览信息（供前端面板展示）
type SystemInfo struct {
	Hostname    string     `json:"hostname"`
	OS          string     `json:"os"`
	Platform    string     `json:"platform"`
	Uptime      uint64     `json:"uptime"`
	BootTime    uint64     `json:"bootTime"`
	CPUUsage    float64    `json:"cpuUsage"`
	CPUCores    int        `json:"cpuCores"`
	CPUThreads  int        `json:"cpuThreads"`
	MemTotal    uint64     `json:"memTotal"`
	MemUsed     uint64     `json:"memUsed"`
	MemPercent  float64    `json:"memPercent"`
	DiskTotal   uint64     `json:"diskTotal"`
	DiskUsed    uint64     `json:"diskUsed"`
	DiskPercent float64    `json:"diskPercent"`
	NetSent     uint64     `json:"netSent"` // 启动以来累计发送字节
	NetRecv     uint64     `json:"netRecv"` // 启动以来累计接收字节
	Goroutines  int        `json:"goroutines"`
	AppMemAlloc uint64     `json:"appMemAlloc"`
	Services    []SvcEntry `json:"services"`
}

// PortEntry 一个监听端口
type PortEntry struct {
	Proto   string `json:"proto"`
	Port    int    `json:"port"`
	PID     int    `json:"pid"`
	Process string `json:"process"`
	Address string `json:"address"`
}

// SvcEntry 由本程序管理的本地服务实例状态
type SvcEntry struct {
	ID      string `json:"id"`
	Type    string `json:"type"` // redis | mysql | php | nginx | npm | docker
	Name    string `json:"name"`
	Port    int    `json:"port"`
	Running bool   `json:"running"`
	Version string `json:"version"`
}

// Snapshot 采集系统快照（CPU/内存/磁盘/网络 + 本程序管理的服务实例）
func Snapshot() *SystemInfo {
	info := &SystemInfo{}

	// 主机
	if h, err := host.Info(); err == nil {
		info.Hostname = h.Hostname
		info.OS = fmt.Sprintf("%s %s", h.OS, h.PlatformVersion)
		info.Platform = h.Platform
		info.Uptime = h.Uptime
		info.BootTime = h.BootTime
	}

	// CPU：物理核 + 逻辑线程；取不到时退回 runtime.NumCPU()
	info.CPUThreads = runtime.NumCPU()
	if phys, err := cpu.Counts(false); err == nil && phys > 0 {
		info.CPUCores = phys
	} else {
		info.CPUCores = runtime.NumCPU()
	}
	if logi, err := cpu.Counts(true); err == nil && logi > 0 {
		info.CPUThreads = logi
	}
	if percentages, err := cpu.Percent(200*time.Millisecond, false); err == nil && len(percentages) > 0 {
		info.CPUUsage = percentages[0]
	}

	// 内存
	if vm, err := mem.VirtualMemory(); err == nil {
		info.MemTotal = vm.Total
		info.MemUsed = vm.Used
		info.MemPercent = vm.UsedPercent
	}

	// Go 运行时
	info.Goroutines = runtime.NumGoroutine()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	info.AppMemAlloc = ms.Alloc

	// 磁盘（取样路径按平台定义在 rootfs_*.go：Windows 是 C:，其它平台是 /）
	if du, err := disk.Usage(rootFS); err == nil {
		info.DiskTotal = du.Total
		info.DiskUsed = du.Used
		info.DiskPercent = du.UsedPercent
	}

	// 网络累计字节
	if nc, err := net.IOCounters(false); err == nil && len(nc) > 0 {
		info.NetSent = nc[0].BytesSent
		info.NetRecv = nc[0].BytesRecv
	}

	// 本程序管理的服务实例
	info.Services = ActiveServices()

	return info
}

// ActiveServices 汇总本程序管理的各服务实例状态
func ActiveServices() []SvcEntry {
	var out []SvcEntry
	// Redis instances
	if redisInsts, _ := redis.ListInstances(); redisInsts != nil {
		for _, inst := range redisInsts {
			running := false
			if st, err := redis.GetInstanceStatus(inst.ID); err == nil {
				running = st.Running
			}
			out = append(out, SvcEntry{ID: inst.ID, Type: "redis", Name: inst.Name, Port: inst.Port, Running: running, Version: inst.Version})
		}
	}
	// MySQL instances
	if mysqlInsts, _ := mysql.ListInstances(); mysqlInsts != nil {
		for _, inst := range mysqlInsts {
			running := false
			if st, err := mysql.GetInstanceStatus(inst.ID); err == nil {
				running = st.Running
			}
			out = append(out, SvcEntry{ID: inst.ID, Type: "mysql", Name: inst.Name, Port: inst.Port, Running: running, Version: inst.Version})
		}
	}
	// PHP instances
	if phpInsts, _ := php.ListInstances(); phpInsts != nil {
		for _, inst := range phpInsts {
			running, version := false, inst.Version
			if st, err := php.GetInstanceStatus(inst.ID); err == nil {
				running = st.Running
				if st.Version != "" {
					version = st.Version // Docker 实例的配置里没有本机版本，取镜像推导出的展示版本
				}
			}
			out = append(out, SvcEntry{ID: inst.ID, Type: "php", Name: inst.Name, Port: inst.Port, Running: running, Version: version})
		}
	}
	// Nginx instances
	if nginxInsts, _ := nginx.ListInstances(); nginxInsts != nil {
		for _, inst := range nginxInsts {
			running := false
			if st, err := nginx.GetInstanceStatus(inst.ID); err == nil {
				running = st.Running
			}
			out = append(out, SvcEntry{ID: inst.ID, Type: "nginx", Name: inst.Name, Port: inst.Port, Running: running, Version: inst.Version})
		}
	}
	// npm 静态站点 instances
	if npmInsts, _ := npm.ListInstances(); npmInsts != nil {
		for _, inst := range npmInsts {
			running := false
			if st, err := npm.GetInstanceStatus(inst.ID); err == nil {
				running = st.Running
			}
			out = append(out, SvcEntry{ID: inst.ID, Type: "npm", Name: inst.Name, Port: inst.Port, Running: running, Version: inst.Version})
		}
	}
	// Docker 引擎：只上报一条聚合状态（容器在 Docker 面板内逐个管理，不混进服务实例总览）
	if eng, _ := docker.EngineStatusCached(5 * time.Second); eng.Available {
		out = append(out, SvcEntry{ID: "docker-engine", Type: "docker", Name: "Docker 引擎", Running: true, Version: eng.Version})
	}
	return out
}
