//go:build !windows

package sniffer

import (
	"context"
	"fmt"
	"runtime"

	"go-tools/tools/portcheck"
)

// 非 Windows 平台没有 pcap 抓包能力：gopacket/pcap 需要 cgo + libpcap，
// 而且这套能力（枚举网卡、抓链路层帧、解密 HTTPS）属于桌面端调试工具，
// Linux 的面板不提供。这里只保留同名符号让上层能跨平台编译，
// 调用一律返回明确错误，不做静默降级——否则前端会以为"抓到了 0 个包"。
//
// MITM 代理（mitm.go / ca.go）是纯 Go 实现，不受这里影响，两端都能用。

// PcapDeviceInfo 与 Windows 实现保持相同字段，前端拿到的 JSON 结构不变。
type PcapDeviceInfo struct {
	Name        string   `json:"name"`        // 友好名称，如 "以太网"，用于前端展示
	PcapName    string   `json:"pcapName"`    // Npcap 真实设备名，用于后端抓包
	Description string   `json:"description"` // 网卡描述信息
	IPs         []string `json:"ips"`         // 该网卡绑定的 IP 地址列表
}

// PacketInfo 抓到的数据包。非 Windows 平台不会产出任何实例。
type PacketInfo struct{}

// GetPcapDevices 非 Windows 平台不可用。
func GetPcapDevices() ([]PcapDeviceInfo, error) {
	return nil, fmt.Errorf("抓包依赖 pcap 驱动，%s 上不支持", runtime.GOOS)
}

// StartCapture 非 Windows 平台不可用。
func StartCapture(ctx context.Context, devices []string, protocols []string, ports []string) (<-chan PacketInfo, error) {
	return nil, fmt.Errorf("抓包依赖 pcap 驱动，%s 上不支持", runtime.GOOS)
}

// StopCapture 非 Windows 平台空操作。
func StopCapture() {}

// 下面两个是纯逻辑（不碰 pcap），proxy 端口选择在两端都要用，
// 所以不能跟着 sniffer.go 一起被 windows 标签关掉。

// PortTaken 报告代理监听端口能否使用：先看各工具实例的登记端口，再看系统里是否已被监听。
// 返回非空字符串即占用原因，空串表示可用。
func PortTaken(port int) string { return portcheck.Taken(port, "") }

// SuggestPort 从 from 起找一个可用的代理端口（默认基准 8888）：
// 既跳过被实例登记的端口，也跳过系统里已绑不上的端口
func SuggestPort(from int) int {
	if from <= 0 {
		from = 8888
	}
	return portcheck.Suggest(from, 100)
}
