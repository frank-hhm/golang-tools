package sniffer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// maxBodyCapture 请求/响应体最大捕获字节数，超出截断
const maxBodyCapture = 512 * 1024

// Flow 一次完整的 HTTP(S) 请求/响应记录
type Flow struct {
	ID          int64  `json:"id"`
	Time        string `json:"time"`
	Protocol    string `json:"protocol"` // HTTP / HTTPS / WS
	Method      string `json:"method"`
	URL         string `json:"url"`
	Host        string `json:"host"`
	Path        string `json:"path"`
	StatusCode  int    `json:"statusCode"`
	Status      string `json:"status"`
	Duration    int64  `json:"duration"` // 毫秒
	ReqSize     int    `json:"reqSize"`
	RespSize    int    `json:"respSize"`
	Process     string `json:"process"`
	Pid         int32  `json:"pid"`
	ContentType string `json:"contentType"`

	RequestHeaders  map[string][]string `json:"requestHeaders"`
	ResponseHeaders map[string][]string `json:"responseHeaders"`
	RequestBody     string              `json:"requestBody"`
	ResponseBody    string              `json:"responseBody"`
}

// Mitm MITM 代理引擎
type Mitm struct {
	ca       *CA
	listener net.Listener
	port     int
	emit     func(Flow)

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	idSeq     int64
	procMap   *ProcessMap
	transport *http.Transport

	connsMu sync.Mutex
	conns   map[net.Conn]struct{}
	stopped bool
}

// NewMitm 创建引擎（加载或生成根 CA）
func NewMitm() (*Mitm, error) {
	ca, err := LoadOrGenerateCA()
	if err != nil {
		return nil, err
	}
	return &Mitm{
		ca:      ca,
		procMap: &ProcessMap{},
		conns:   make(map[net.Conn]struct{}),
		transport: &http.Transport{
			Proxy:                 nil,
			TLSClientConfig:       &tls.Config{InsecureSkipVerify: true},
			MaxIdleConnsPerHost:   8,
			DisableCompression:    false,
			ResponseHeaderTimeout: 60 * time.Second,
		},
	}, nil
}

// CertPath 暴露根证书路径，供安装到系统
func (m *Mitm) CertPath() string { return m.ca.CertPath() }

// Port 返回监听端口
func (m *Mitm) Port() int { return m.port }

// Start 在指定端口启动代理，emit 回调用于推送捕获结果
func (m *Mitm) Start(port int, emit func(Flow)) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("监听端口 %d 失败: %w", port, err)
	}
	m.port = port
	m.emit = emit
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.listener = ln
	m.procMap.Start(m.ctx)

	m.wg.Add(1)
	go m.acceptLoop()
	return nil
}

// Stop 停止代理并等待连接收敛
func (m *Mitm) Stop() {
	if m.cancel != nil {
		m.cancel()
	}
	if m.listener != nil {
		m.listener.Close()
	}
	// 主动关闭所有活动连接，解除阻塞中的 ReadRequest，
	// 否则 keep-alive 长连接会让收敛过程长时间挂起。
	m.connsMu.Lock()
	m.stopped = true
	for c := range m.conns {
		c.Close()
	}
	m.conns = make(map[net.Conn]struct{})
	m.connsMu.Unlock()
	// 释放 transport 中的空闲上游连接；进行中的 RoundTrip 已由请求 ctx 取消。
	if m.transport != nil {
		m.transport.CloseIdleConnections()
	}
	// 给连接协程一个短暂的收敛窗口；超时后立即返回，绝不无限阻塞 wg.Wait()，
	// 避免个别卡在慢上游上的请求把 StopSnifferProxy 拖死。
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(1500 * time.Millisecond):
	}
}

// trackConn 登记活动连接；若代理已停止则返回 false（调用方应直接关闭连接）
func (m *Mitm) trackConn(c net.Conn) bool {
	m.connsMu.Lock()
	defer m.connsMu.Unlock()
	if m.stopped {
		return false
	}
	m.conns[c] = struct{}{}
	return true
}

func (m *Mitm) untrackConn(c net.Conn) {
	m.connsMu.Lock()
	delete(m.conns, c)
	m.connsMu.Unlock()
}

func (m *Mitm) acceptLoop() {
	defer m.wg.Done()
	for {
		conn, err := m.listener.Accept()
		if err != nil {
			select {
			case <-m.ctx.Done():
				return
			default:
				continue
			}
		}
		m.wg.Add(1)
		go func(c net.Conn) {
			defer m.wg.Done()
			m.handleConn(c)
		}(conn)
	}
}

func (m *Mitm) handleConn(client net.Conn) {
	if !m.trackConn(client) {
		client.Close()
		return
	}
	defer func() {
		m.untrackConn(client)
		client.Close()
	}()
	br := bufio.NewReader(client)
	for {
		req, err := http.ReadRequest(br)
		if err != nil {
			return
		}
		if req.Method == http.MethodConnect {
			m.handleConnect(client, req)
			return // CONNECT 后由 TLS 循环接管，结束即关闭
		}
		if !m.handlePlain(client, req) {
			return
		}
	}
}

// handlePlain 处理明文 HTTP 代理请求，返回是否保持连接
func (m *Mitm) handlePlain(client net.Conn, req *http.Request) bool {
	start := time.Now()
	host := req.URL.Host
	if host == "" {
		host = req.Host
	}
	reqBody := drainBody(req.Body)
	req.Body = io.NopCloser(bytes.NewReader(reqBody))
	req.Header.Del("Accept-Encoding")
	// 绑定代理生命周期 ctx：Stop() 取消时可立即中断进行中的上游请求
	req = req.WithContext(m.ctx)

	resp, err := m.transport.RoundTrip(req)
	if err != nil {
		io.WriteString(client, "HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n")
		return false
	}
	respBody := drainBody(resp.Body)
	resp.Body = io.NopCloser(bytes.NewReader(respBody))
	resp.Write(client)

	fullURL := req.URL.String()
	if !strings.HasPrefix(fullURL, "http") {
		fullURL = "http://" + host + req.URL.RequestURI()
	}
	m.capture("HTTP", req, reqBody, resp, respBody, start, fullURL, host, client)
	return keepAlive(req, resp)
}

// handleConnect 处理 HTTPS CONNECT，完成 TLS MITM 后循环处理内部请求
func (m *Mitm) handleConnect(client net.Conn, req *http.Request) {
	host, _, err := net.SplitHostPort(req.Host)
	if err != nil {
		host = req.Host // 可能没有端口
	}

	if _, werr := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); werr != nil {
		return
	}

	leaf, cerr := m.ca.CertForHost(host)
	if cerr != nil {
		return
	}
	tlsServer := tls.Server(client, &tls.Config{Certificates: []tls.Certificate{*leaf}})
	if herr := tlsServer.Handshake(); herr != nil {
		return
	}
	defer tlsServer.Close()

	br := bufio.NewReader(tlsServer)
	for {
		inner, rerr := http.ReadRequest(br)
		if rerr != nil {
			return
		}
		inner.URL.Scheme = "https"
		inner.URL.Host = host
		if inner.Host == "" {
			inner.Host = req.Host
		}

		start := time.Now()
		reqBody := drainBody(inner.Body)
		inner.Body = io.NopCloser(bytes.NewReader(reqBody))
		inner.Header.Del("Accept-Encoding")

		isUpgrade := strings.EqualFold(inner.Header.Get("Upgrade"), "websocket")

		// 绑定代理生命周期 ctx：Stop() 取消时可立即中断进行中的上游请求
		inner = inner.WithContext(m.ctx)
		resp, terr := m.transport.RoundTrip(inner)
		if terr != nil {
			tlsServer.Write([]byte("HTTP/1.1 502 Bad Gateway\r\nContent-Length: 0\r\n\r\n"))
			return
		}
		respBody := drainBody(resp.Body)
		resp.Body = io.NopCloser(bytes.NewReader(respBody))
		resp.Write(tlsServer)

		proto := "HTTPS"
		if isUpgrade || resp.StatusCode == http.StatusSwitchingProtocols {
			proto = "WS"
		}
		fullURL := "https://" + host + inner.URL.RequestURI()
		m.capture(proto, inner, reqBody, resp, respBody, start, fullURL, host, tlsServer)

		if proto == "WS" || !keepAlive(inner, resp) {
			return
		}
	}
}

// capture 组装 Flow 并推送
func (m *Mitm) capture(proto string, req *http.Request, reqBody []byte, resp *http.Response, respBody []byte, start time.Time, fullURL, host string, conn net.Conn) {
	path := req.URL.RequestURI()

	var reqHdrs = cloneHeaders(req.Header)
	var respHdrs map[string][]string
	if resp != nil {
		respHdrs = cloneHeaders(resp.Header)
	}

	status := ""
	statusCode := 0
	contentType := ""
	if resp != nil {
		statusCode = resp.StatusCode
		status = strings.TrimPrefix(resp.Status, fmt.Sprintf("%d ", statusCode))
		contentType = resp.Header.Get("Content-Type")
	}

	proc := m.procOf(conn)

	f := Flow{
		ID:              atomic.AddInt64(&m.idSeq, 1),
		Time:            start.Format("2006-01-02 15:04:05.000"),
		Protocol:        proto,
		Method:          req.Method,
		URL:             fullURL,
		Host:            host,
		Path:            path,
		StatusCode:      statusCode,
		Status:          status,
		Duration:        time.Since(start).Milliseconds(),
		ReqSize:         len(reqBody),
		RespSize:        len(respBody),
		Process:         proc.Name,
		Pid:             proc.Pid,
		ContentType:     contentType,
		RequestHeaders:  reqHdrs,
		ResponseHeaders: respHdrs,
		RequestBody:     truncateString(reqBody),
		ResponseBody:    truncateString(respBody),
	}
	if m.emit != nil {
		m.emit(f)
	}
}

// procOf 依据连接的本地端口（即发起应用的临时端口）定位进程
func (m *Mitm) procOf(conn net.Conn) ProcessInfo {
	if conn == nil {
		return ProcessInfo{}
	}
	ra := conn.RemoteAddr().String()
	if idx := strings.LastIndex(ra, ":"); idx >= 0 {
		if p, err := strconv.Atoi(ra[idx+1:]); err == nil {
			return m.procMap.Lookup(uint16(p))
		}
	}
	return ProcessInfo{}
}

/* ---------- 工具函数 ---------- */

func drainBody(rc io.ReadCloser) []byte {
	if rc == nil {
		return nil
	}
	defer rc.Close()
	b, _ := io.ReadAll(io.LimitReader(rc, maxBodyCapture))
	return b
}

func truncateString(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if len(b) > maxBodyCapture {
		return string(b[:maxBodyCapture]) + "...(truncated)"
	}
	return string(b)
}

func cloneHeaders(h http.Header) map[string][]string {
	out := make(map[string][]string, len(h))
	for k, v := range h {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func keepAlive(req *http.Request, resp *http.Response) bool {
	if strings.EqualFold(req.Header.Get("Connection"), "close") {
		return false
	}
	if resp != nil && strings.EqualFold(resp.Header.Get("Connection"), "close") {
		return false
	}
	// HTTP/1.0 默认短连接
	if req.ProtoMajor == 1 && req.ProtoMinor == 0 {
		return strings.EqualFold(req.Header.Get("Connection"), "keep-alive")
	}
	return true
}
