package sniffer

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/shirou/gopsutil/v3/process"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

var (
	globalEngine *Engine         // 1. 创建一个全局的抓包引擎实例
	engineMutex  sync.Mutex      // 2. 使用互斥锁保护全局实例，防止并发问题
	packetCh     chan PacketInfo // 3. 创建一个全局的数据包通道

	// --- 新增的全局变量 ---
	portProcessMap = make(map[uint16]string) // 用于存储 端口 -> 进程名 的映射
	mapMutex       sync.RWMutex              // 用于保护 portProcessMap 的读写锁
	mapCtx         context.Context           // 用于控制映射更新协程的生命周期
	mapCancel      context.CancelFunc        // 用于停止映射更新协程
)

// Config 抓包配置
type Config struct {
	Devices   []string // 网卡名称列表
	Protocols []string // 协议列表，如 ["tcp", "udp"]
	Ports     []string // 端口列表，如 ["80", "443"]，如果为空则不限制端口
}

// HttpRequest HTTP 请求信息
type HttpRequest struct {
	Method      string            `json:"method"`                // GET, POST, PUT, DELETE 等
	URL         string            `json:"url"`                   // 请求 URL
	Path        string            `json:"path"`                  // 请求路径
	Query       string            `json:"query,omitempty"`       // 查询参数
	Host        string            `json:"host,omitempty"`        // Host 头
	ContentType string            `json:"contentType,omitempty"` // Content-Type
	Headers     map[string]string `json:"headers,omitempty"`     // 所有请求头
	Body        string            `json:"body,omitempty"`        // 请求体
}

// HttpResponse HTTP 响应信息
type HttpResponse struct {
	StatusCode  int               `json:"statusCode"`            // 状态码
	Status      string            `json:"status"`                // 状态描述, e.g. "200 OK"
	ContentType string            `json:"contentType,omitempty"` // Content-Type
	Headers     map[string]string `json:"headers,omitempty"`     // 所有响应头
	Body        string            `json:"body,omitempty"`        // 响应体
}

// PacketInfo 解析后的数据包结构
type PacketInfo struct {
	// --- 基础信息 ---
	Time      string `json:"time"`              // 抓包时间 (格式化后的字符串)
	Device    string `json:"device"`            // 网卡设备名
	Protocol  string `json:"protocol"`          // 协议类型: TCP, UDP, HTTP, HTTPS
	Direction string `json:"direction"`         // 方向: "outgoing" 或 "incoming"
	Process   string `json:"process,omitempty"` // 关联的进程名

	// --- 网络层 (Layer 3) ---
	SrcIP   string `json:"srcIp"`
	DstIP   string `json:"dstIp"`
	SrcPort uint16 `json:"srcPort"`
	DstPort uint16 `json:"dstPort"`

	// --- TCP 信息 ---
	Flags string `json:"flags,omitempty"` // TCP 标志位
	Seq   uint32 `json:"seq,omitempty"`
	Ack   uint32 `json:"ack,omitempty"`

	// --- HTTP 请求/响应 ---
	Request  *HttpRequest  `json:"request,omitempty"`
	Response *HttpResponse `json:"response,omitempty"`

	// --- 原始数据 ---
	RawPayload string `json:"rawPayload,omitempty"` // 原始 payload (截断)
	PacketSize int    `json:"packetSize"`           // 数据包大小
}

// Engine 抓包引擎
type Engine struct {
	config Config
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// DeviceInfo 定义网卡信息结构体，方便前端解析
type DeviceInfo struct {
	Name        string   `json:"name"`        // 网卡名称，如 "eth0" 或 "en0"
	Description string   `json:"description"` // 网卡描述信息
	IPs         []string `json:"ips"`         // 该网卡绑定的 IP 地址列表
}

// decodeGBK 将 GBK 编码的字节切片转换为 UTF-8 字符串
func decodeGBK(s string) string {
	// 创建一个解码器
	decoder := simplifiedchinese.GBK.NewDecoder()
	// 执行转换
	result, _, err := transform.String(decoder, s)
	if err != nil {
		// 如果转换失败（例如本来就是 UTF-8），返回原字符串
		return s
	}
	return result
}

// PcapDeviceInfo 定义包含 Npcap 真实设备名的网卡信息结构体
type PcapDeviceInfo struct {
	Name        string   `json:"name"`        // 友好名称，如 "以太网"，用于前端展示
	PcapName    string   `json:"pcapName"`    // Npcap 真实设备名，如 "\Device\NPF{...}"，用于后端抓包
	Description string   `json:"description"` // 网卡描述信息
	IPs         []string `json:"ips"`         // 该网卡绑定的 IP 地址列表
}

func GetPcapDevices() ([]PcapDeviceInfo, error) {
	// 1. 使用 pcap 库一次性获取所有设备信息
	devices, err := pcap.FindAllDevs()
	if err != nil {
		return nil, fmt.Errorf("Npcap 驱动未安装或加载失败: %v", err)
	}

	var result []PcapDeviceInfo
	for _, device := range devices {
		// 2. 过滤掉没有描述且名称看起来不像友好名称的设备（可选，让列表更干净）
		// 在某些系统上，pcap 可能会返回一些内部接口，它们的 Name 是 \Device\NPF{...} 格式
		// 我们可以尝试优先使用 Description，如果没有则用 Name
		displayName := device.Description
		if displayName == "" {
			displayName = device.Name
		}

		// 3. 获取该网卡下的所有 IP 地址
		var ips []string
		for _, address := range device.Addresses {
			if ip := address.IP; ip != nil && !ip.IsLoopback() {
				// 过滤掉 IPv6 地址（如果需要保留 IPv6 请去掉此判断）
				if ip.To4() != nil {
					ips = append(ips, ip.String())
				}
			}
		}

		// 4. 将信息追加到结果中
		result = append(result, PcapDeviceInfo{
			Name:        displayName, // 用于展示的友好名称
			PcapName:    device.Name, // 用于抓包的真实名称
			Description: device.Description,
			IPs:         ips,
		})
	}

	return result, nil
}

// GetDevices 获取系统所有的网络设备
func GetDevices() ([]DeviceInfo, error) {
	// 1. 使用标准库获取所有网卡接口
	interfaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var result []DeviceInfo
	for _, iface := range interfaces {
		// 2. 过滤掉未启用或环回(Loopback)的网卡，让前端列表更干净
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// 3. 获取该网卡下的所有 IP 地址
		var ips []string
		addrs, err := iface.Addrs()
		if err != nil {
			continue // 获取 IP 失败则跳过该网卡
		}

		for _, addr := range addrs {
			// 尝试将地址解析为 IPNet，只保留有效的 IP
			if ipNet, ok := addr.(*net.IPNet); ok && !ipNet.IP.IsLoopback() {
				// 过滤掉 IPv6 地址（如果你的抓包工具只支持 IPv4），如果需要保留 IPv6 请去掉此判断
				if ipNet.IP.To4() != nil {
					ips = append(ips, ipNet.IP.String())
				}
			}
		}

		// 4. 获取描述信息
		// 注意：标准库的 Name 通常是类似 "eth0", "Ethernet 2" 这样的字符串
		// 它不会像 pcap 那样返回完整的硬件描述（如 "Realtek PCIe GbE..."）
		desc := iface.Name

		// 如果你之前写了 decodeGBK，这里依然可以套上（虽然标准库 Name 极少出现 GBK 乱码）
		// desc = decodeGBK(iface.Name)

		// 将网卡信息追加到结果中
		result = append(result, DeviceInfo{
			Name:        iface.Name,
			Description: desc,
			IPs:         ips,
		})
	}

	return result, nil
}

// NewEngine 创建引擎
func NewEngine(config Config) *Engine {
	return &Engine{config: config}
}

// Start 启动多网卡并发抓包
func (e *Engine) Start(ctx context.Context, packetChan chan<- PacketInfo) error {
	if len(e.config.Devices) == 0 {
		return fmt.Errorf("未选择任何网卡设备")
	}

	// --- 核心修改：动态构建 BPF 过滤器 ---
	var filterParts []string

	// 1. 拼接协议部分
	if len(e.config.Protocols) > 0 {
		filterParts = append(filterParts, "("+strings.Join(e.config.Protocols, " or ")+")")
	}

	// 2. 拼接端口部分
	if len(e.config.Ports) > 0 {
		portFilters := make([]string, len(e.config.Ports))
		for i, port := range e.config.Ports {
			portFilters[i] = "port " + port // BPF 语法: port 80
		}
		filterParts = append(filterParts, "("+strings.Join(portFilters, " or ")+")")
	}

	// 3. 组合最终的 BPF 过滤器（例如: "(tcp or udp) and (port 80 or port 443)"）
	bpfFilter := strings.Join(filterParts, " and ")

	// 创建可取消的 Context
	captureCtx, cancel := context.WithCancel(ctx)
	e.cancel = cancel

	// 为每个网卡启动独立的协程
	for _, device := range e.config.Devices {
		e.wg.Add(1)
		go e.captureDevice(captureCtx, device, bpfFilter, packetChan)
	}

	return nil
}

// Stop 优雅停止所有抓包
func (e *Engine) Stop() {
	if e.cancel != nil {
		e.cancel()
		e.wg.Wait()
	}
}

// captureDevice 单个网卡的抓包逻辑
func (e *Engine) captureDevice(ctx context.Context, device, bpf string, ch chan<- PacketInfo) {
	defer e.wg.Done()

	handle, err := pcap.OpenLive(device, 1600, true, 30*time.Second)
	if err != nil {
		fmt.Printf("打开网卡 [%s] 失败: %v\n", device, err)
		return
	}
	defer handle.Close()

	if bpf != "" {
		if err := handle.SetBPFFilter(bpf); err != nil {
			fmt.Printf("网卡 [%s] 设置 BPF 失败: %v\n", device, err)
			return
		}
	}

	packetSource := gopacket.NewPacketSource(handle, handle.LinkType())

	for {
		select {
		case <-ctx.Done():
			return
		case packet, ok := <-packetSource.Packets():
			if !ok {
				return
			}
			if info := parsePacket(packet, device); info != nil {
				select {
				case ch <- *info: // 非阻塞或带缓冲发送
				default:
					// 如果前端处理不过来，丢弃旧包，防止内存溢出
				}
			}
		}
	}
}

// parsePacket 解析数据包
func parsePacket(packet gopacket.Packet, device string) *PacketInfo {
	info := &PacketInfo{
		Time:       time.Now().Format("2006-01-02 15:04:05.000"),
		Device:     device,
		PacketSize: len(packet.Data()),
	}

	// 1. 解析网络层 (IP)
	if ipLayer := packet.Layer(layers.LayerTypeIPv4); ipLayer != nil {
		ip, _ := ipLayer.(*layers.IPv4)
		info.SrcIP = ip.SrcIP.String()
		info.DstIP = ip.DstIP.String()
		info.Protocol = ip.Protocol.String()
	}

	// 2. 解析传输层 (TCP/UDP)
	if tcpLayer := packet.Layer(layers.LayerTypeTCP); tcpLayer != nil {
		tcp, _ := tcpLayer.(*layers.TCP)
		info.SrcPort = uint16(tcp.SrcPort)
		info.DstPort = uint16(tcp.DstPort)
		info.Flags = formatFlags(tcp)
		info.Seq = tcp.Seq
		info.Ack = tcp.Ack
		info.Protocol = "TCP"

		// 判断方向：目标端口是 80/443 为 outgoing，源端口是 80/443 为 incoming
		if isWellKnownPort(info.DstPort) {
			info.Direction = "outgoing"
		} else if isWellKnownPort(info.SrcPort) {
			info.Direction = "incoming"
		}

		// 查找关联进程
		info.Process = getProcessByPort(info.SrcPort, info.DstPort)

		// 解析应用层 HTTP 数据
		if appLayer := packet.ApplicationLayer(); appLayer != nil {
			payload := appLayer.Payload()
			if len(payload) == 0 {
				return info
			}
			payloadStr := string(payload)

			// 保存原始 payload (截断到 2KB)
			if len(payloadStr) > 2048 {
				info.RawPayload = payloadStr[:2048] + "...(truncated)"
			} else {
				info.RawPayload = payloadStr
			}

			// 尝试解析为 HTTP 请求
			if req := tryParseHTTPRequest(payloadStr); req != nil {
				info.Protocol = "HTTP"
				info.Request = req
				info.Direction = "outgoing"
			} else if resp := tryParseHTTPResponse(payloadStr); resp != nil {
				// 尝试解析为 HTTP 响应
				info.Protocol = "HTTP"
				info.Response = resp
				info.Direction = "incoming"
			}
		}
	} else if udpLayer := packet.Layer(layers.LayerTypeUDP); udpLayer != nil {
		udp, _ := udpLayer.(*layers.UDP)
		info.SrcPort = uint16(udp.SrcPort)
		info.DstPort = uint16(udp.DstPort)
		info.Protocol = "UDP"
		info.Direction = "outgoing"
	}

	return info
}

// isWellKnownPort 判断是否是常见 Web 端口
func isWellKnownPort(port uint16) bool {
	webPorts := []uint16{80, 443, 8080, 8443, 3000, 5000, 8000, 8888, 9090}
	for _, p := range webPorts {
		if port == p {
			return true
		}
	}
	return false
}

// getProcessByPort 根据端口获取关联进程名
func getProcessByPort(srcPort, dstPort uint16) string {
	mapMutex.RLock()
	defer mapMutex.RUnlock()
	if name, ok := portProcessMap[srcPort]; ok {
		return name
	}
	if name, ok := portProcessMap[dstPort]; ok {
		return name
	}
	return ""
}

// tryParseHTTPRequest 尝试将 payload 解析为 HTTP 请求
func tryParseHTTPRequest(payloadStr string) *HttpRequest {
	// 支持的 HTTP 方法
	methods := []string{"GET", "POST", "PUT", "DELETE", "PATCH", "HEAD", "OPTIONS", "CONNECT", "TRACE"}

	// 检查是否以某个方法开头
	matched := false
	for _, m := range methods {
		if strings.HasPrefix(payloadStr, m+" ") {
			matched = true
			break
		}
	}
	if !matched {
		return nil
	}

	req := &HttpRequest{}

	// 分割 header 和 body
	parts := strings.SplitN(payloadStr, "\r\n\r\n", 2)
	headerSection := parts[0]
	if len(parts) > 1 {
		body := parts[1]
		if len(body) > 4096 {
			req.Body = body[:4096] + "...(truncated)"
		} else {
			req.Body = body
		}
	}

	lines := strings.Split(headerSection, "\r\n")
	if len(lines) == 0 {
		return nil
	}

	// 解析请求行: "GET /path?query HTTP/1.1"
	requestLine := lines[0]
	reqParts := strings.SplitN(requestLine, " ", 3)
	if len(reqParts) >= 2 {
		req.Method = reqParts[0]
		fullURL := reqParts[1]
		req.URL = fullURL

		// 分离 path 和 query
		if idx := strings.Index(fullURL, "?"); idx != -1 {
			req.Path = fullURL[:idx]
			req.Query = fullURL[idx+1:]
		} else {
			req.Path = fullURL
		}
	}

	// 解析请求头
	req.Headers = make(map[string]string)
	for _, line := range lines[1:] {
		kv := strings.SplitN(line, ": ", 2)
		if len(kv) == 2 {
			req.Headers[kv[0]] = kv[1]
			// 提取常用头
			switch strings.ToLower(kv[0]) {
			case "host":
				req.Host = kv[1]
			case "content-type":
				req.ContentType = kv[1]
			}
		}
	}

	return req
}

// tryParseHTTPResponse 尝试将 payload 解析为 HTTP 响应
func tryParseHTTPResponse(payloadStr string) *HttpResponse {
	// HTTP 响应以 "HTTP/" 开头
	if !strings.HasPrefix(payloadStr, "HTTP/") {
		return nil
	}

	resp := &HttpResponse{}

	// 分割 header 和 body
	parts := strings.SplitN(payloadStr, "\r\n\r\n", 2)
	headerSection := parts[0]
	if len(parts) > 1 {
		body := parts[1]
		if len(body) > 4096 {
			resp.Body = body[:4096] + "...(truncated)"
		} else {
			resp.Body = body
		}
	}

	lines := strings.Split(headerSection, "\r\n")
	if len(lines) == 0 {
		return nil
	}

	// 解析状态行: "HTTP/1.1 200 OK"
	statusLine := lines[0]
	statusParts := strings.SplitN(statusLine, " ", 3)
	if len(statusParts) >= 2 {
		resp.Status = strings.Join(statusParts[1:], " ")
		// 尝试解析状态码
		fmt.Sscanf(statusParts[1], "%d", &resp.StatusCode)
	}

	// 解析响应头
	resp.Headers = make(map[string]string)
	for _, line := range lines[1:] {
		kv := strings.SplitN(line, ": ", 2)
		if len(kv) == 2 {
			resp.Headers[kv[0]] = kv[1]
			if strings.ToLower(kv[0]) == "content-type" {
				resp.ContentType = kv[1]
			}
		}
	}

	return resp
}

func formatFlags(tcp *layers.TCP) string {
	var flags []string
	if tcp.SYN {
		flags = append(flags, "SYN")
	}
	if tcp.ACK {
		flags = append(flags, "ACK")
	}
	if tcp.FIN {
		flags = append(flags, "FIN")
	}
	if tcp.RST {
		flags = append(flags, "RST")
	}
	return strings.Join(flags, ",")
}

// StartCapture 启动抓包，并返回一个用于接收数据包的通道
func StartCapture(ctx context.Context, devices []string, protocols []string, ports []string) (<-chan PacketInfo, error) {
	engineMutex.Lock()
	defer engineMutex.Unlock()

	// 如果已有引擎在运行，先停止它
	if globalEngine != nil {
		globalEngine.Stop()
	}

	if len(devices) == 0 {
		return nil, fmt.Errorf("未选择任何网卡设备")
	}

	config := Config{
		Devices:   devices,
		Protocols: protocols,
		Ports:     ports, // 传入端口配置
	}

	// 创建新的引擎实例
	globalEngine = NewEngine(config)

	// 创建一个新的带缓冲的通道
	packetCh = make(chan PacketInfo, 100)

	// 启动抓包
	if err := globalEngine.Start(ctx, packetCh); err != nil {
		globalEngine = nil
		return nil, err
	}

	return packetCh, nil
}

// StopCapture 停止抓包
func StopCapture() {
	engineMutex.Lock()
	defer engineMutex.Unlock()

	if globalEngine != nil {
		globalEngine.Stop()
		globalEngine = nil
	}

	// 8. 关闭通道，通知接收方数据流结束
	if packetCh != nil {
		close(packetCh)
		packetCh = nil
	}
}

// GetPcapDeviceName 将标准网卡名称转换为 Npcap 的真实设备名
// 这个函数现在可以简化，或者在未来的重构中被 GetPcapDevices 完全取代
func GetPcapDeviceName(standardName string) (string, error) {
	devices, err := GetPcapDevices()
	if err != nil {
		return "", err
	}
	for _, dev := range devices {
		if dev.Name == standardName {
			return dev.PcapName, nil
		}
	}
	return "", fmt.Errorf("未找到网卡 [%s] 对应的 Npcap 抓包设备", standardName)
}

// startPortProcessMapUpdater 启动一个后台协程，定期更新端口到进程名的映射表
func startPortProcessMapUpdater(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Second) // 每秒更新一次
		defer ticker.Stop()

		// 立即执行一次，避免启动后有一秒的空白期
		updatePortProcessMap()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				updatePortProcessMap()
			}
		}
	}()
}

// updatePortProcessMap 扫描所有进程，构建端口到进程名的映射表
func updatePortProcessMap() {
	newMap := make(map[uint16]string)

	// 获取所有进程的 PID
	pids, err := process.Pids()
	if err != nil {
		return
	}

	for _, pid := range pids {
		p, err := process.NewProcess(pid)
		if err != nil {
			// 进程可能已退出，忽略错误
			continue
		}

		// 获取进程名
		name, err := p.Name()
		if err != nil {
			continue
		}

		// 获取该进程的所有网络连接
		conns, err := p.Connections()
		if err != nil || len(conns) == 0 {
			continue
		}

		// 遍历连接，将本地端口和进程名存入新映射表
		for _, conn := range conns {
			// --- 修复开始 ---
			// 在 gopsutil v3 中，conn.Laddr 是一个结构体，可以直接访问 Port 字段
			localPort := uint16(conn.Laddr.Port)
			if localPort > 0 {
				newMap[localPort] = name
			}
		}
	}

	// 原子性地替换旧映射表
	mapMutex.Lock()
	portProcessMap = newMap
	mapMutex.Unlock()
}
