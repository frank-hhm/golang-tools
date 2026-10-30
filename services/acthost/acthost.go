// Package acthost 把「唤起已运行实例并跳到某个面板」的命令转交给已经在跑的主程序。
//
// 为什么需要：系统通知中心里的原生 Toast 被点击时，Windows 通过 tools: 协议
// 重新拉起 exe。这时候主程序多半已经开着（关到托盘、常驻），再启一个完整 GUI 会
// 托盘重复、全局热键注册失败、配置并发写。所以只把「要跳到哪个面板」这一件事递给
// 正在跑的那个实例，递不动就说明主程序没跑 —— 那本次启动自己起 GUI 并跳到目标即可。
//
// 传输沿用 archivehost 的成熟做法：只绑 127.0.0.1 的回环 TCP + 一个记录端点的文件 +
// 每条连接先验一个启动时随机生成的令牌。端口系统分配、本机其它进程猜不到；令牌顺带
// 解决「残留端点文件指向别人」的问题。命名管道标准库不支持，回环 TCP 能设超时、可单测。
//
// 与 archivehost 分开成两个包：archive 那条链路有它自己的负载形状与测试，不为了
// 复用把导航命令硬塞进它的 Request；两者只是恰好用了同一种传输。
package acthost

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"Tools/appdirs"
)

// dialTimeout 拨号超时。回环连接本地毫秒级；给到 400ms 容下主程序正忙的那一下。
const dialTimeout = 400 * time.Millisecond

// ioTimeout 单条连接的读写上限。请求就是一条 JSON，超这一档只能是异常。
const ioTimeout = 3 * time.Second

// maxLine 一行允许的最大长度。导航命令负载很小，超限认定是异常输入。
const maxLine = 1 << 16

const ackLine = "ok"

// Request 一次转交的导航命令。
//
// Target 是给前端的导航描述：一条前端路由（如 "/tools/download"）或工具 key，
// 由前端决定是开原生工具窗还是页面内跳转。Kind 预留（route/tool），当前前端按
// Target 是否为 "/" 开头自行判断，Kind 只用于日志与将来扩展。
type Request struct {
	Target string `json:"target"`
	Kind   string `json:"kind"`
	From   string `json:"from"` // protocol / cli，只用于日志分来源
}

// envelope 一次握手的全部内容：令牌 + 负载。
type envelope struct {
	Token string  `json:"token"`
	Cmd   Request `json:"cmd"`
}

// endpoint 落在文件里的端点信息。
type endpoint struct {
	Addr  string `json:"addr"`
	Token string `json:"token"`
	PID   int    `json:"pid"`
}

// Server 正在监听的端点。
type Server struct {
	ln    net.Listener
	token string
	file  string
	onCmd func(Request)
	wg    sync.WaitGroup
	mu    sync.Mutex
	done  bool
}

// Start 起监听并登记端点文件。
//
// onCmd 在每个连接上同步调用，所以它必须自己异步化 —— 主程序侧是把导航命令丢给
// 前端事件，瞬间返回。失败只应当记日志，不要因此退出：两个 GUI 并存是既有行为，
// 谁先起来谁写端点文件，后起来的那个接不上而已。
func Start(onCmd func(Request)) (*Server, error) {
	if onCmd == nil {
		return nil, fmt.Errorf("acthost: 没有处理命令的回调")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("起回环监听失败: %w", err)
	}
	token, err := newToken()
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	file, err := endpointFile()
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	s := &Server{ln: ln, token: token, file: file, onCmd: onCmd}
	if err := s.writeEndpoint(); err != nil {
		_ = ln.Close()
		return nil, err
	}
	s.wg.Add(1)
	go s.acceptLoop()
	return s, nil
}

// endpointFile 端点文件路径：<数据目录>/acthost/endpoint.json。
//
// 落在数据目录而不是临时目录：协议拉起的进程与主程序读的是同一个约定，
// 中间没有任何握手机制，路径必须双方都提前知道。
func endpointFile() (string, error) {
	dir, err := appdirs.Data("acthost")
	if err != nil {
		return "", fmt.Errorf("准备 acthost 数据目录失败: %w", err)
	}
	return filepath.Join(dir, "endpoint.json"), nil
}

func newToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("生成端点令牌失败: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func (s *Server) writeEndpoint() error {
	body, err := json.Marshal(endpoint{Addr: s.ln.Addr().String(), Token: s.token, PID: os.Getpid()})
	if err != nil {
		return err
	}
	tmp := s.file + ".part"
	if err := os.WriteFile(tmp, append(body, '\n'), 0o600); err != nil {
		return fmt.Errorf("写端点文件失败: %w", err)
	}
	return os.Rename(tmp, s.file)
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		cn, err := s.ln.Accept()
		if err != nil {
			return // Stop() 关掉监听后必然走到这里
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.handle(cn)
		}()
	}
}

func (s *Server) handle(cn net.Conn) {
	defer cn.Close()
	_ = cn.SetDeadline(time.Now().Add(ioTimeout))
	line, err := readLine(cn)
	if err != nil {
		return
	}
	var env envelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		_, _ = io.WriteString(cn, "bad request\n")
		return
	}
	if env.Token != s.token {
		// 令牌不对：不回详情，免得给探测者线索
		_, _ = io.WriteString(cn, "no\n")
		return
	}
	// ack 先回：调用方只关心"主程序接下了没有"，成不成由前端事件流说话。
	if _, err := io.WriteString(cn, ackLine+"\n"); err != nil {
		return
	}
	s.onCmd(env.Cmd)
}

// Stop 关监听、撤端点文件、等在途连接处理完。幂等。
func (s *Server) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.done = true
	s.mu.Unlock()

	_ = s.ln.Close()
	// 只删自己写的那份：另一个实例可能已经接手续跑，端点文件现在归它
	if cur, err := readEndpoint(); err == nil && cur.Token == s.token {
		_ = os.Remove(s.file)
	}
	s.wg.Wait()
}

// TrySend 把导航命令递给正在监听的主程序。
//
// 返回 false = 主程序没跑（端点文件不在/连不上/令牌不匹配），调用方应当自己起 GUI
// 并跳到目标。一次拨号一条命令：点一次通知就是一条命令行，没有攒批的必要。
func TrySend(cmd Request) bool {
	ep, err := readEndpoint()
	if err != nil {
		return false
	}
	cn, err := net.DialTimeout("tcp", ep.Addr, dialTimeout)
	if err != nil {
		return false
	}
	defer cn.Close()
	_ = cn.SetDeadline(time.Now().Add(ioTimeout))

	body, err := json.Marshal(envelope{Token: ep.Token, Cmd: cmd})
	if err != nil {
		return false
	}
	if _, err := cn.Write(append(body, '\n')); err != nil {
		return false
	}
	ack, err := readLine(cn)
	if err != nil {
		return false
	}
	return strings.TrimSpace(ack) == ackLine
}

// readEndpoint 读端点文件。
func readEndpoint() (endpoint, error) {
	var ep endpoint
	file, err := endpointFile()
	if err != nil {
		return ep, err
	}
	body, err := os.ReadFile(file)
	if err != nil {
		return ep, fmt.Errorf("读端点文件失败: %w", err)
	}
	if err := json.Unmarshal(body, &ep); err != nil {
		return ep, fmt.Errorf("端点文件坏了: %w", err)
	}
	if ep.Addr == "" || ep.Token == "" {
		return ep, fmt.Errorf("端点文件不完整")
	}
	return ep, nil
}

// readLine 读一行 JSON。超过 maxLine 直接判异常，不把内存交给对端决定。
func readLine(r io.Reader) (string, error) {
	br := bufio.NewReaderSize(r, 1<<14)
	line, err := br.ReadBytes('\n')
	if len(line) > maxLine {
		return "", fmt.Errorf("请求过长")
	}
	if len(line) == 0 && err != nil {
		return "", err
	}
	return strings.TrimRight(string(line), "\r\n"), nil
}
