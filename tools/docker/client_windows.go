//go:build windows

package docker

// ============================================================
// Docker Engine HTTP 客户端（Windows 命名管道直连，零第三方依赖）
//
// Docker Desktop 在 \\.\pipe\docker_engine 上提供标准 Engine API。Go 的
// net 包不支持命名管道，这里用 x/sys/windows 自建 net.Conn：
//   CreateFile(FILE_FLAG_OVERLAPPED) → ReadFile/WriteFile + 事件等待
//   → 需要中止时 CancelIoEx（SetDeadline 到期或 Close）
// 端点解析顺序：DOCKER_HOST 环境变量 → 默认管道列表（逐个实探）。
// 端点不是解析一次定终身：每轮重扫候选、拨不通就作废，详见「端点与客户端」一节。
// ============================================================

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/windows"
)

// defaultPipes 默认探测的命名管道候选（Docker Desktop / 原生 Engine 先后顺序）。
// 候选会每轮重扫：Desktop 重启、切虚拟化后端都可能把引擎挪到另一条管道上，
// 只认第一条会把面板永久钉在「引擎不可用」上。
var defaultPipes = []string{
	`\\.\pipe\docker_engine`,
	`\\.\pipe\dockerDesktopLinuxEngine`,
	`\\.\pipe\dockerDesktopWindowsEngine`,
	`\\.\pipe\dockerBackend`,
}

const errDaemonUnavailable = "无法连接 Docker 引擎（请确认 Docker Desktop 或 Docker Engine 正在运行）"

// classifyDialError 给单个候选管道的拨号失败起个短名。
// 管道不存在 / 拒绝访问 / 忙 的下一步动作不同，混成一句“没应答”帮不上忙。
func classifyDialError(err error) string {
	switch {
	case err == nil:
		return "正常"
	case errors.Is(err, windows.ERROR_FILE_NOT_FOUND), errors.Is(err, windows.ERROR_PATH_NOT_FOUND):
		return "管道不存在"
	case errors.Is(err, windows.ERROR_ACCESS_DENIED):
		return "拒绝访问"
	case errors.Is(err, windows.ERROR_PIPE_BUSY):
		return "管道忙"
	case errors.Is(err, windows.ERROR_PIPE_NOT_CONNECTED), errors.Is(err, windows.ERROR_BROKEN_PIPE):
		return "管道已断开"
	}
	return "拨号失败"
}

// daemonUnavailableHint Windows 侧不额外补命令：启动/安装都有按钮可点，
// errDaemonUnavailable 里已经写明要确认 Desktop 或 Engine 正在运行。
func daemonUnavailableHint([]error) string { return "" }

/* ============================ 命名管道 Conn ============================ */

type pipeConn struct {
	h windows.Handle

	mu      sync.Mutex
	closed  bool
	pending int

	deadline atomic.Value // time.Time；零值表示不限
	readEv   atomic.Pointer[windows.Overlapped]
	writeEv  atomic.Pointer[windows.Overlapped]
}

// dialPipe 打开命名管道（实例全忙时短暂重试）
func dialPipe(path string) (net.Conn, error) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for attempt := 0; attempt < 8; attempt++ {
		h, err := windows.CreateFile(p,
			windows.GENERIC_READ|windows.GENERIC_WRITE,
			0, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED, 0)
		if err == nil {
			c := &pipeConn{h: h}
			c.deadline.Store(time.Time{})
			return c, nil
		}
		lastErr = err
		if err == windows.ERROR_PIPE_BUSY {
			time.Sleep(120 * time.Millisecond)
			continue
		}
		break
	}
	return nil, lastErr
}

// pipeExistsOnce 一次 CreateFile 判定管道在不在：不重试、不读写。
// 这里不能靠 os.Stat：命名管道上没有可用实例时它会一直等，体检环节被拖住
// 面板就“跟踪不到状态”了；只取查询属性（SQOS + IDENTIFICATION）不会排队等实例。
// 忙/正在关闭/权限不足都说明有人在监听，只有「找不到这个名字」才算不存在。
func pipeExistsOnce(path string) bool {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, err := windows.CreateFile(p, 0, 0, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
	if err == nil {
		_ = windows.CloseHandle(h)
		return true
	}
	switch err {
	case windows.ERROR_FILE_NOT_FOUND, windows.ERROR_PATH_NOT_FOUND,
		windows.Errno(233), syscall_ERRNO_PIPE_MISSING:
		return false
	}
	return true
}

func (c *pipeConn) curDeadline() time.Time {
	v, _ := c.deadline.Load().(time.Time)
	return v
}

// blockingOp 执行一次重叠 IO 并等待完成；到期或句柄关闭时以 CancelIoEx 中止
func (c *pipeConn) blockingOp(isRead bool, b []byte) (int, error) {
	if isRead && len(b) == 0 {
		return 0, nil
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, net.ErrClosed
	}
	c.pending++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.pending--
		needClose := c.closed && c.pending == 0
		c.mu.Unlock()
		if needClose {
			_ = windows.CloseHandle(c.h)
		}
	}()

	ev, err := windows.CreateEvent(nil, 0, 0, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = windows.CloseHandle(ev) }()

	// Overlapped 必须活到本次调用结束，故置于堆上并登记到 conn（Close 需 CancelIoEx 它）
	ov := &windows.Overlapped{HEvent: ev}
	if isRead {
		c.readEv.Store(ov)
		defer c.readEv.Store(nil)
	} else {
		c.writeEv.Store(ov)
		defer c.writeEv.Store(nil)
	}

	var done uint32
	if isRead {
		err = windows.ReadFile(c.h, b, &done, ov)
	} else {
		err = windows.WriteFile(c.h, b, &done, ov)
	}
	if err != nil && err != windows.ERROR_IO_PENDING {
		if isRead && err == windows.ERROR_BROKEN_PIPE {
			return 0, io.EOF // 对端关闭：按 EOF 语义交给 http 层
		}
		return 0, err
	}

	timeout := uint32(windows.INFINITE)
	var stopWatch func()
	if dl := c.curDeadline(); !dl.IsZero() {
		d := time.Until(dl)
		if d <= 0 {
			_ = windows.CancelIoEx(c.h, ov)
			_, _ = windows.WaitForSingleObject(ev, windows.INFINITE)
			return 0, os.ErrDeadlineExceeded
		}
		timeout = uint32(d.Milliseconds())
		if timeout < 1 {
			timeout = 1
		}
		stopWatch = startWatchdog(c.h, ov, time.Duration(timeout)*time.Millisecond)
		defer stopWatch()
	}

	wait, err := windows.WaitForSingleObject(ev, timeout)
	if err != nil {
		return 0, err
	}
	if wait == uint32(windows.WAIT_TIMEOUT) {
		_ = windows.CancelIoEx(c.h, ov)
		_, _ = windows.WaitForSingleObject(ev, windows.INFINITE)
		return 0, os.ErrDeadlineExceeded
	}
	done = 0
	if err := windows.GetOverlappedResult(c.h, ov, &done, true); err != nil {
		if isRead && err == windows.ERROR_BROKEN_PIPE {
			return 0, io.EOF
		}
		if err == windows.ERROR_OPERATION_ABORTED {
			return 0, os.ErrDeadlineExceeded
		}
		return 0, err
	}
	return int(done), nil
}

// startWatchdog 超时窗口后再兜底取消一次（防对端静默不应答）
func startWatchdog(h windows.Handle, ov *windows.Overlapped, after time.Duration) func() {
	timer := time.AfterFunc(after+500*time.Millisecond, func() {
		_ = windows.CancelIoEx(h, ov)
	})
	return func() { timer.Stop() }
}

func (c *pipeConn) Read(b []byte) (int, error)  { return c.blockingOp(true, b) }
func (c *pipeConn) Write(b []byte) (int, error) { return c.blockingOp(false, b) }

func (c *pipeConn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	hasPending := c.pending > 0
	ovR, ovW := c.readEv.Load(), c.writeEv.Load()
	c.mu.Unlock()

	_ = windows.CancelIoEx(c.h, nil)
	if ovR != nil {
		_ = windows.CancelIoEx(c.h, ovR)
	}
	if ovW != nil {
		_ = windows.CancelIoEx(c.h, ovW)
	}
	if hasPending {
		return nil // 由最后一笔在途 IO 结束时关闭句柄
	}
	return windows.CloseHandle(c.h)
}

func (c *pipeConn) setDeadline(t time.Time) error {
	c.deadline.Store(t)
	c.mu.Lock()
	hasPending := c.pending > 0
	c.mu.Unlock()
	// 只有「设了新期限且确实有在途 IO」才取消让它按新期限重试。
	// 清空期限（time.Time{}，http 层读完响应会这么做）绝不能取消：
	// 那会把同一条 keep-alive 连接上正在等待下一次响应的读打断
	if !t.IsZero() && hasPending {
		_ = windows.CancelIoEx(c.h, nil)
	}
	return nil
}

func (c *pipeConn) SetDeadline(t time.Time) error      { return c.setDeadline(t) }
func (c *pipeConn) SetReadDeadline(t time.Time) error  { return c.setDeadline(t) }
func (c *pipeConn) SetWriteDeadline(t time.Time) error { return c.setDeadline(t) }

func (c *pipeConn) LocalAddr() net.Addr  { return pipeAddr("pipe") }
func (c *pipeConn) RemoteAddr() net.Addr { return pipeAddr("docker_engine") }
func (c *pipeConn) String() string       { return "pipeconn" }

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }

/* ============================ 平台钩子（被 client.go 调用） ============================ */

// candidateEndpoints 默认候选：Docker Desktop 与原生 Engine 的命名管道，按常见顺序。
// 每轮重扫：Desktop 重启或切虚拟化后端都可能把引擎挪到另一条管道上。
func candidateEndpoints() []endpoint {
	out := make([]endpoint, 0, len(defaultPipes))
	for _, p := range defaultPipes {
		out = append(out, endpoint{network: "pipe", addr: p, desc: "npipe://" + p})
	}
	return out
}

// dialEndpointConn 按端点类型拨号：tcp 直连，其余走命名管道
func dialEndpointConn(ctx context.Context, ep endpoint) (net.Conn, error) {
	if ep.network == "tcp" {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", ep.addr)
	}
	return dialPipe(ep.addr)
}

// dialSaysEndpointWrong 只有「这个端点根本打不开 / 对面正在退出」才判端点选错；
// 管道全忙(ERROR_PIPE_BUSY=231) 是引擎在忙不是端点错（dialPipe 内部已重试过），
// 把它也算进去会导致好好连着的端点被反复丢弃重扫
func dialSaysEndpointWrong(err error) bool {
	return errors.Is(err, windows.ERROR_FILE_NOT_FOUND) || // 2：管道不存在
		errors.Is(err, windows.ERROR_PATH_NOT_FOUND) || // 3
		errors.Is(err, windows.Errno(233)) || // ERROR_PIPE_NOT_AVAILABLE：实例都被关闭了
		errors.Is(err, windows.Errno(234)) || // ERROR_NO_DATA：管道正在关闭（Desktop 在退出）
		errors.Is(err, syscall_ERRNO_PIPE_MISSING)
}

// isEndpointBusy 管道实例全被占着：引擎在跑但正忙（拉镜像、看日志会这样）
func isEndpointBusy(err error) bool { return err == windows.ERROR_PIPE_BUSY }

// isTransportEndpointErr 拨号/读写阶段的错误是否属于「端点层」（而非引擎返回的业务错误）
func isTransportEndpointErr(err error) bool {
	return errors.Is(err, windows.ERROR_FILE_NOT_FOUND) ||
		errors.Is(err, windows.ERROR_PIPE_BUSY) ||
		errors.Is(err, windows.ERROR_BROKEN_PIPE) ||
		errors.Is(err, syscall_ERRNO_PIPE_MISSING)
}

// shortEndpointAddr 日志用的短名：\"\\.\pipe\dockerDesktopLinuxEngine" → dockerDesktopLinuxEngine
func shortEndpointAddr(ep endpoint) string {
	if ep.network != "pipe" {
		return ep.addr
	}
	return strings.TrimPrefix(ep.addr, `\\.\pipe\`)
}

// parseDOCKERHOST 支持 npipe:////./pipe/xxx、tcp://host:port、unix://（Windows 拒绝）
func parseDOCKERHOST(h string) (endpoint, error) {
	switch {
	case strings.HasPrefix(h, "npipe://"):
		p := strings.TrimPrefix(h, "npipe://")
		p = strings.ReplaceAll(p, "/", `\`)
		for strings.HasPrefix(p, `\`) {
			p = p[1:]
		}
		if !strings.HasPrefix(p, `.\pipe\`) {
			p = `.\pipe\` + p
		}
		full := `\\` + p
		return endpoint{network: "pipe", addr: full, desc: h}, nil
	case strings.HasPrefix(h, "tcp://"), strings.HasPrefix(h, "http://"):
		u, err := url.Parse(h)
		if err != nil {
			return endpoint{}, err
		}
		host := u.Host
		if u.Port() == "" {
			host += ":2375"
		}
		return endpoint{network: "tcp", addr: host, desc: h}, nil
	case strings.HasPrefix(h, "unix://"):
		return endpoint{}, fmt.Errorf("Windows 下不支持 unix:// 端点: %s", h)
	default:
		return endpoint{}, fmt.Errorf("未知协议: %s", h)
	}
}

// syscall_ERRNO_PIPE_MISSING 部分 x/sys 版本未导出该常量，这里按 Win32 码值补齐
var syscall_ERRNO_PIPE_MISSING = windows.Errno(230) // ERROR_PIPE_NOT_AVAILABLE
