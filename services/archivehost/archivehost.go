// Package archivehost 把右键拉起的压缩任务转交给已经在跑的主程序。
//
// 为什么要这一层：右键菜单直接把路径交给 go-tools.exe，而主程序这时候
// 多半已经开着。再启一个完整 GUI 会托盘重复、全局热键注册失败、配置并发写；
// 引 Wails 的 SingleInstance 又会让开发实例与已安装实例互斥，牵连 wails3 dev。
// 所以只把"要压哪几个路径"这一件事递过去，递不动就说明主程序没跑，
// 那本次调用自己无界面压完也一样是对的。
//
// 传输用回环 TCP + 一个记录端点的文件，不用命名管道：
// 方案原本写的是 net.Listen("pipe", ...)，实测 Go 1.26 直接报
// "listen pipe: unknown network pipe" —— 标准库从来没注册过 pipe 这个网络，
// 要自己上 CreateNamedPipe + overlapped I/O 才做得出来（还得处理没有
// deadline 的阻塞）。回环 TCP 是标准库能力、能设超时、能被单测覆盖，
// 语义上和管道等价：本机、单向、一问一答。
//
// 只绑 127.0.0.1，并且每条连接先验一个启动时随机生成的令牌：端口是系统分的，
// 本机其它进程猜不到也占不上；令牌顺带解决"残留端点文件指向别人"的问题。
//
// 不在这里回传进度 —— 主程序那头的进度走 Wails 事件进任务中心，
// 已经退出的调用方没有资格当进度的接收端。
package archivehost

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
	"Tools/tools/archive"
)

// dialTimeout 拨号超时。回环连接是本地毫秒级；给到 400ms 是为了容下主程序
// 正忙于建索引的那一下，再长右键点击就像卡死了。
const dialTimeout = 400 * time.Millisecond

// ioTimeout 单条连接的读写上限。请求就是一条 JSON，超这一档只能是异常。
const ioTimeout = 3 * time.Second

// maxLine 一行允许的最大长度。一次多选可以把整盘文件都选中，
// 路径列表按 UTF-8 展开后长度可观；上限之外认定是异常输入。
const maxLine = 4 << 20

// Request 一次转交的负载。
//
// 内嵌 archive.Options：JSON 会把它的字段平铺进来，正好是方案里定的
// {op, paths, format, level, outDir} 形状；内嵌而不是另抄一份字段，
// 是为了让"加一个选项"只改一处，不会出现新旧两版负载。
type Request struct {
	archive.Options
	From string `json:"from"` // verb / sendto / cli，只在日志里用来分来源
}

// envelope 一次握手的全部内容：令牌 + 负载。
type envelope struct {
	Token string  `json:"token"`
	Job   Request `json:"job"`
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
	onJob func(Request)
	wg    sync.WaitGroup
	mu    sync.Mutex
	done  bool
}

// Start 起监听并登记端点文件。
//
// onJob 在每个连接上同步调用，所以它必须自己异步化 —— 主程序侧是
// 把任务丢进任务中心，瞬间返回。失败只应当记日志，不要因此退出：
// 两个 GUI 实例并存是本程序的既有行为，后起的那个没有端点文件写权限而已。
func Start(onJob func(Request)) (*Server, error) {
	if onJob == nil {
		return nil, fmt.Errorf("archivehost: 没有处理请求的回调")
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
	s := &Server{ln: ln, token: token, file: file, onJob: onJob}
	if err := s.writeEndpoint(); err != nil {
		// 端点文件写不出去，右键那条路就永远找不到这里；监听留着也没人接得上，
		// 直接收掉，让调用方把原因写进日志
		_ = ln.Close()
		return nil, err
	}
	s.wg.Add(1)
	go s.acceptLoop()
	return s, nil
}

// endpointFile 端点文件路径：<数据目录>/archive/endpoint.json。
//
// 落在数据目录而不是临时目录：右键拉起的进程与主程序读的是同一个约定，
// 中间没有任何握手机制，路径必须双方都提前知道。
func endpointFile() (string, error) {
	dir, err := appdirs.Data("archive")
	if err != nil {
		return "", fmt.Errorf("准备 archive 数据目录失败: %w", err)
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
		return // 对端连一行都没发完就断了，没什么可记的
	}
	var env envelope
	if err := json.Unmarshal([]byte(line), &env); err != nil {
		_, _ = io.WriteString(cn, "bad request\n")
		return
	}
	if env.Token != s.token {
		// 令牌不对：要么是残留端点文件被别的进程撞上了，要么是本机另有程序在试。
		// 不回错误详情，免得给探测者线索
		_, _ = io.WriteString(cn, "no\n")
		return
	}
	// ack 先回：调用方只关心"主程序接下了没有"，做成与否由事件流说话。
	// 回完就必须真的把任务排上去，所以 onJob 不许 panic（主程序侧自己兜）。
	if _, err := io.WriteString(cn, ackLine+"\n"); err != nil {
		return
	}
	s.onJob(env.Job)
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

const ackLine = "ok"

// TrySend 把请求递给正在监听的主程序。
//
// 返回 false = 主程序没跑（端点文件不在/连不上/令牌不匹配），
// 调用方应当自己把活干完。一次拨号一个请求：右键一次点击就是一条命令行，
// 没有攒批的必要。
func TrySend(job Request) bool {
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

	body, err := json.Marshal(envelope{Token: ep.Token, Job: job})
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
	// 只认明确的 ack：把任何杂音当成功，任务就会既没执行也没兜底
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
	br := bufio.NewReaderSize(r, 1<<20)
	line, err := br.ReadBytes('\n')
	if len(line) > maxLine {
		return "", fmt.Errorf("请求过长")
	}
	if len(line) == 0 && err != nil {
		return "", err
	}
	return trimEOL(string(line)), nil
}

func trimEOL(s string) string {
	return strings.TrimRight(s, "\r\n")
}
