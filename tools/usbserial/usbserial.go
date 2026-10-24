package usbserial

import (
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.bug.st/serial"
	"go.bug.st/serial/enumerator"
)

/* ============================ 事件推送 ============================ */

// emitter 由 main 注入（绑定到 Wails v3 的 app.Event.Emit），用于向前端推送事件。
var emitter func(name string, data ...any)

// SetEmitter 注入事件发射器。
func SetEmitter(fn func(name string, data ...any)) {
	emitter = fn
}

// emit 安全地向前端推送事件（未注入时静默忽略）。
func emit(name string, payload any) {
	if emitter != nil {
		emitter(name, payload)
	}
}

// SerialData 每次收到数据时推送给前端的事件负载。
type SerialData struct {
	Source string `json:"source"` // 数据来源标识（串口号 / 远端地址）
	Hex    string `json:"hex"`    // 收到的原始字节（hex 编码）
	Count  int    `json:"count"`  // 字节数
	Time   string `json:"time"`   // 服务器时间戳 HH:MM:SS.mmm
}

// ConnStatusEvent 连接状态变化时推送给前端的事件负载。
type ConnStatusEvent struct {
	Type   string `json:"type"`   // serial / tcpclient / tcpserver / udp / none
	Opened bool   `json:"opened"` // 是否已连接/侦听
	Label  string `json:"label"`  // 展示用连接描述
	Note   string `json:"note"`   // 额外说明（如客户端接入/断开）
}

/* ============================ 数据类型 ============================ */

// SerialDeviceInfo 串口设备信息
type SerialDeviceInfo struct {
	PortName     string `json:"port_name"`
	Description  string `json:"description"` // 展示描述：通信端口 / USB 串行设备
	IsUSB        bool   `json:"is_usb"`
	SerialNumber string `json:"serial_number"`
	Product      string `json:"product"`
	VendorID     string `json:"vendor_id"`
	ProductID    string `json:"product_id"`
}

// ConnConfig 统一连接配置（串口与网络参数合并在同一结构，前端按模式填相应字段）。
type ConnConfig struct {
	// --- 串口参数 ---
	Port     string `json:"port"`
	Baud     int    `json:"baud"`
	DataBits int    `json:"dataBits"` // 5/6/7/8，0 视为 8
	Parity   string `json:"parity"`   // none/odd/even/mark/space
	StopBits int    `json:"stopBits"` // 1/2，0 视为 1
	RTS      bool   `json:"rts"`
	DTR      bool   `json:"dtr"`
	// --- 网络参数（TCPClient/TCPServer/UDP）---
	LocalAddr  string `json:"localAddr"`
	LocalPort  int    `json:"localPort"`
	RemoteAddr string `json:"remoteAddr"`
	RemotePort int    `json:"remotePort"`
}

// ConnStatus 当前连接状态快照。
type ConnStatus struct {
	Type    string `json:"type"`
	Opened  bool   `json:"opened"`
	Label   string `json:"label"`
	RxBytes int64  `json:"rxBytes"`
	TxBytes int64  `json:"txBytes"`
}

/* ============================ 连接管理 ============================ */

// manager 维护单一活动连接（SSCOM 风格：一次只打开一个端口/连接）。
type manager struct {
	mu      sync.Mutex
	typ     string
	label   string
	opened  bool
	closer  func() error              // 关闭全部底层资源
	send    func([]byte) (int, error) // 发送字节到对端
	stopCh  chan struct{}             // 通知读协程退出
	wg      sync.WaitGroup            // 等待读协程退出
	port    serial.Port               // 串口句柄（用于实时切换 RTS/DTR）
	rxBytes int64
	txBytes int64
}

var mgr = &manager{}

// ListSerialPorts 获取所有串口列表（含 USB 与普通通信端口）。
func ListSerialPorts() ([]SerialDeviceInfo, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}
	var devices []SerialDeviceInfo
	for _, port := range ports {
		desc := "通信端口"
		if port.IsUSB {
			desc = "USB 串行设备"
		}
		devices = append(devices, SerialDeviceInfo{
			PortName:     port.Name,
			Description:  desc,
			IsUSB:        port.IsUSB,
			SerialNumber: port.SerialNumber,
			Product:      port.Product,
			VendorID:     port.VID,
			ProductID:    port.PID,
		})
	}
	return devices, nil
}

// Status 返回当前连接状态快照。
func Status() ConnStatus {
	mgr.mu.Lock()
	typ, label, opened := mgr.typ, mgr.label, mgr.opened
	mgr.mu.Unlock()
	return ConnStatus{
		Type:    typ,
		Opened:  opened,
		Label:   label,
		RxBytes: atomic.LoadInt64(&mgr.rxBytes),
		TxBytes: atomic.LoadInt64(&mgr.txBytes),
	}
}

// begin 关闭既有连接并初始化一个新的活动连接状态。
func begin(typ, label string) {
	_ = Close()
	mgr.mu.Lock()
	mgr.typ = typ
	mgr.label = label
	mgr.opened = true
	mgr.stopCh = make(chan struct{})
	mgr.rxBytes = 0
	mgr.txBytes = 0
	mgr.mu.Unlock()
	emit("serial:status", ConnStatusEvent{Type: typ, Opened: true, Label: label})
}

// Close 关闭当前连接，停止所有读协程并释放底层资源。
func Close() error {
	mgr.mu.Lock()
	if !mgr.opened {
		mgr.mu.Unlock()
		return nil
	}
	mgr.opened = false
	typ, label := mgr.typ, mgr.label
	stop := mgr.stopCh
	closer := mgr.closer
	mgr.closer = nil
	mgr.send = nil
	mgr.port = nil
	mgr.mu.Unlock()

	if stop != nil {
		close(stop)
	}
	if closer != nil {
		_ = closer()
	}
	mgr.wg.Wait()
	emit("serial:status", ConnStatusEvent{Type: "none", Opened: false, Label: label, Note: fmt.Sprintf("已关闭 %s", typLabel(typ))})
	return nil
}

func typLabel(t string) string {
	switch t {
	case "serial":
		return "串口"
	case "tcpclient":
		return "TCP 客户端"
	case "tcpserver":
		return "TCP 服务端"
	case "udp":
		return "UDP"
	default:
		return "连接"
	}
}

// startReader 为给定 reader 启动一个读协程，收到数据即推送 serial:data 事件。
func startReader(r io.Reader, source string) {
	mgr.wg.Add(1)
	go func() {
		defer mgr.wg.Done()
		buf := make([]byte, 2048)
		for {
			select {
			case <-mgr.stopCh:
				return
			default:
			}
			n, err := r.Read(buf)
			if n > 0 {
				atomic.AddInt64(&mgr.rxBytes, int64(n))
				emit("serial:data", SerialData{
					Source: source,
					Hex:    hex.EncodeToString(buf[:n]),
					Count:  n,
					Time:   time.Now().Format("15:04:05.000"),
				})
			}
			if err != nil {
				if isTimeout(err) {
					continue
				}
				if errors.Is(err, io.EOF) || strings.Contains(err.Error(), "use of closed") {
					return
				}
				return
			}
		}
	}()
}

// SendHex 将 hex 字符串解码为字节并发送到当前连接的对端，返回发送字节数。
func SendHex(hexStr string) (int, error) {
	data, err := hex.DecodeString(strings.ReplaceAll(strings.TrimSpace(hexStr), " ", ""))
	if err != nil {
		return 0, fmt.Errorf("HEX 数据非法: %v", err)
	}
	if len(data) == 0 {
		return 0, nil
	}
	mgr.mu.Lock()
	if !mgr.opened || mgr.send == nil {
		mgr.mu.Unlock()
		return 0, fmt.Errorf("当前没有可用的连接")
	}
	sender := mgr.send
	mgr.mu.Unlock()

	n, err := sender(data)
	if err != nil {
		return n, err
	}
	atomic.AddInt64(&mgr.txBytes, int64(n))
	return n, nil
}

// SetControlBits 实时切换串口的 RTS / DTR 电平。
func SetControlBits(rts, dtr bool) error {
	mgr.mu.Lock()
	p := mgr.port
	mgr.mu.Unlock()
	if p == nil {
		return fmt.Errorf("串口未打开")
	}
	if err := p.SetRTS(rts); err != nil {
		return err
	}
	return p.SetDTR(dtr)
}

/* ============================ 串口 ============================ */

// OpenSerial 按配置打开串口并启动读取。
func OpenSerial(cfg ConnConfig) error {
	if strings.TrimSpace(cfg.Port) == "" {
		return fmt.Errorf("串口号不能为空")
	}
	if cfg.Baud <= 0 {
		cfg.Baud = 9600
	}
	if cfg.DataBits <= 0 {
		cfg.DataBits = 8
	}
	mode := &serial.Mode{
		BaudRate: cfg.Baud,
		DataBits: cfg.DataBits,
		Parity:   mapParity(cfg.Parity),
		StopBits: mapStopBits(cfg.StopBits),
		InitialStatusBits: &serial.ModemOutputBits{
			RTS: cfg.RTS,
			DTR: cfg.DTR,
		},
	}
	port, err := serial.Open(strings.ToUpper(cfg.Port), mode)
	if err != nil {
		return fmt.Errorf("打开串口 %s 失败: %v", cfg.Port, err)
	}
	if err := port.SetReadTimeout(200 * time.Millisecond); err != nil {
		port.Close()
		return fmt.Errorf("设置读超时失败: %v", err)
	}

	begin("serial", cfg.Port)
	mgr.mu.Lock()
	mgr.port = port
	mgr.closer = func() error { return port.Close() }
	mgr.send = port.Write
	mgr.mu.Unlock()

	startReader(port, cfg.Port)
	return nil
}

func mapParity(p string) serial.Parity {
	switch strings.ToLower(p) {
	case "even":
		return serial.EvenParity
	case "odd":
		return serial.OddParity
	case "mark":
		return serial.MarkParity
	case "space":
		return serial.SpaceParity
	default:
		return serial.NoParity
	}
}

func mapStopBits(s int) serial.StopBits {
	switch s {
	case 2:
		return serial.TwoStopBits
	default:
		return serial.OneStopBit
	}
}

/* ============================ TCP 客户端 ============================ */

// OpenTCPClient 连接远端 TCP 服务器。
func OpenTCPClient(cfg ConnConfig) error {
	if strings.TrimSpace(cfg.RemoteAddr) == "" {
		return fmt.Errorf("远程地址不能为空")
	}
	addr := net.JoinHostPort(cfg.RemoteAddr, fmt.Sprintf("%d", cfg.RemotePort))
	var dialer net.Dialer
	dialer.Timeout = 5 * time.Second
	if cfg.LocalPort > 0 {
		dialer.LocalAddr = &net.TCPAddr{IP: net.ParseIP(cfg.LocalAddr), Port: cfg.LocalPort}
	}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("连接 %s 失败: %v", addr, err)
	}
	begin("tcpclient", addr)
	mgr.mu.Lock()
	mgr.closer = conn.Close
	mgr.send = conn.Write
	mgr.mu.Unlock()
	startReader(conn, addr)
	return nil
}

/* ============================ TCP 服务端 ============================ */

// ListenTCPServer 在本地地址侦听 TCP 连接，支持多个客户端接入。
func ListenTCPServer(cfg ConnConfig) error {
	if cfg.LocalPort <= 0 {
		return fmt.Errorf("本地侦听端口不能为空")
	}
	bind := &net.TCPAddr{IP: net.ParseIP(cfg.LocalAddr), Port: cfg.LocalPort}
	if cfg.LocalAddr == "" {
		bind.IP = net.IPv4zero
	}
	ln, err := net.ListenTCP("tcp", bind)
	if err != nil {
		return fmt.Errorf("侦听失败: %v", err)
	}
	label := net.JoinHostPort(defaultStr(cfg.LocalAddr, "0.0.0.0"), fmt.Sprintf("%d", cfg.LocalPort))
	begin("tcpserver", label)

	var (
		cMu     sync.Mutex
		clients = map[net.Conn]struct{}{}
	)
	closeAll := func() error {
		cMu.Lock()
		for c := range clients {
			c.Close()
		}
		cMu.Unlock()
		return ln.Close()
	}

	mgr.mu.Lock()
	mgr.closer = closeAll
	mgr.send = func(b []byte) (int, error) {
		cMu.Lock()
		defer cMu.Unlock()
		if len(clients) == 0 {
			return 0, fmt.Errorf("尚无客户端连接")
		}
		var last int
		var lastErr error
		for c := range clients {
			n, err := c.Write(b)
			if err != nil {
				lastErr = err
				continue
			}
			last = n
		}
		return last, lastErr
	}
	mgr.mu.Unlock()

	mgr.wg.Add(1)
	go func() {
		defer mgr.wg.Done()
		for {
			select {
			case <-mgr.stopCh:
				return
			default:
			}
			conn, err := ln.Accept()
			if err != nil {
				select {
				case <-mgr.stopCh:
					return
				default:
				}
				if ne, ok := err.(net.Error); ok && ne.Timeout() {
					continue
				}
				return
			}
			remote := conn.RemoteAddr().String()
			cMu.Lock()
			clients[conn] = struct{}{}
			cMu.Unlock()
			emit("serial:status", ConnStatusEvent{Type: "tcpserver", Opened: true, Label: label, Note: "客户端接入: " + remote})

			mgr.wg.Add(1)
			go func(c net.Conn) {
				defer mgr.wg.Done()
				buf := make([]byte, 2048)
				for {
					select {
					case <-mgr.stopCh:
						return
					default:
					}
					n, err := c.Read(buf)
					if n > 0 {
						atomic.AddInt64(&mgr.rxBytes, int64(n))
						emit("serial:data", SerialData{
							Source: remote,
							Hex:    hex.EncodeToString(buf[:n]),
							Count:  n,
							Time:   time.Now().Format("15:04:05.000"),
						})
					}
					if err != nil {
						break
					}
				}
				cMu.Lock()
				delete(clients, conn)
				cMu.Unlock()
				c.Close()
				emit("serial:status", ConnStatusEvent{Type: "tcpserver", Opened: true, Label: label, Note: "客户端断开: " + remote})
			}(conn)
		}
	}()
	return nil
}

/* ============================ UDP ============================ */

// OpenUDP 绑定本地端口收发 UDP。若配置了远端地址则默认发往该远端。
func OpenUDP(cfg ConnConfig) error {
	bindIP := net.ParseIP(cfg.LocalAddr)
	if cfg.LocalAddr == "" {
		bindIP = net.IPv4zero
	}
	laddr := &net.UDPAddr{IP: bindIP, Port: cfg.LocalPort}
	pc, err := net.ListenUDP("udp", laddr)
	if err != nil {
		return fmt.Errorf("UDP 绑定失败: %v", err)
	}

	var remote *net.UDPAddr
	if cfg.RemoteAddr != "" && cfg.RemotePort > 0 {
		remote, err = net.ResolveUDPAddr("udp", net.JoinHostPort(cfg.RemoteAddr, fmt.Sprintf("%d", cfg.RemotePort)))
		if err != nil {
			pc.Close()
			return fmt.Errorf("UDP 远端地址非法: %v", err)
		}
	}
	label := net.JoinHostPort(defaultStr(cfg.LocalAddr, "0.0.0.0"), fmt.Sprintf("%d", cfg.LocalPort))
	begin("udp", label)

	// 记录最近一次收到的远端，未配置固定远端时回发到此地址。
	var (
		rMu      sync.Mutex
		lastAddr *net.UDPAddr
	)
	mgr.mu.Lock()
	mgr.closer = pc.Close
	mgr.send = func(b []byte) (int, error) {
		target := remote
		if target == nil {
			rMu.Lock()
			target = lastAddr
			rMu.Unlock()
		}
		if target == nil {
			return 0, fmt.Errorf("未指定远端地址且尚无收到的数据")
		}
		return pc.WriteTo(b, target)
	}
	mgr.mu.Unlock()

	mgr.wg.Add(1)
	go func() {
		defer mgr.wg.Done()
		buf := make([]byte, 2048)
		for {
			select {
			case <-mgr.stopCh:
				return
			default:
			}
			n, addr, err := pc.ReadFromUDP(buf)
			if n > 0 {
				rMu.Lock()
				lastAddr = addr
				rMu.Unlock()
				atomic.AddInt64(&mgr.rxBytes, int64(n))
				emit("serial:data", SerialData{
					Source: addr.String(),
					Hex:    hex.EncodeToString(buf[:n]),
					Count:  n,
					Time:   time.Now().Format("15:04:05.000"),
				})
			}
			if err != nil {
				if isTimeout(err) {
					continue
				}
				select {
				case <-mgr.stopCh:
					return
				default:
					return
				}
			}
		}
	}()
	return nil
}

/* ============================ 工具函数 ============================ */

// LocalIPs 返回本机所有非回环 IPv4 地址，供前端本地地址下拉使用。
func LocalIPs() []string {
	var ips []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if ipnet, ok := a.(*net.IPNet); ok {
				if ip4 := ipnet.IP.To4(); ip4 != nil {
					ips = append(ips, ip4.String())
				}
			}
		}
	}
	return ips
}

func defaultStr(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// isTimeout 判断错误是否为读/写超时（串口与网络库的超时错误形式不一）。
func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out")
}
