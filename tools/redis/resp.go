package redis

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Conn 是一个极简的 RESP（Redis 序列化协议）客户端，仅依赖标准库，
// 支持 RESP2 的简单字符串/错误/整数/批量/数组回复解析。
type Conn struct {
	nc  net.Conn
	r   *bufio.Reader
	w   *bufio.Writer
	mu  sync.Mutex    // 序列化同一连接上的命令读写，避免并发使用 bufio 破坏 RESP 流
	bad bool          // 连接上的 RESP 流已经对不齐（超时/半截断/超限），不能再复用
	to  time.Duration // 本连接的命令截止，0 用 cmdTimeout（状态探活需要更短）
}

const (
	// cmdTimeout 单条命令的读写截止：服务端接受连接却不回话时（hang 住的 redis、
	// 端口上根本不是 redis），不让调用方无限期阻在 readReply 里挂着协程与互斥锁
	cmdTimeout = 60 * time.Second
	// maxBulkSize 单个批量回复的字节上限：长度是自报的，不校验的话
	// 一行 "$4000000000" 就是一次 4GB 的 make，直接把这个进程顶到 OOM
	maxBulkSize = 128 << 20
	// maxArrayLen 单个数组回复的元素上限，同理防 "*2000000000" 把 make 打爆
	maxArrayLen = 1 << 20
)

// errProtoLimit 协议层主动设的下限拦截；报这个错时流里还有没读完的数据，连接必须废弃
var errProtoLimit = errors.New("redis: 回复超出可接受上限")

// dial 建立到 Redis 的 TCP 连接，按需执行 AUTH 与 SELECT。
func dial(addr, password string, db int, timeout time.Duration) (*Conn, error) {
	nc, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
	// 命令截止不拿建连超时当默认：探活用 600ms 建连，但 INFO / 大值读取需要更宽的窗口
	c := &Conn{nc: nc, r: bufio.NewReader(nc), w: bufio.NewWriter(nc)}
	if password != "" {
		if _, err := c.Command("AUTH", password); err != nil {
			nc.Close()
			return nil, fmt.Errorf("认证失败: %w", err)
		}
	}
	if db > 0 {
		if _, err := c.Command("SELECT", strconv.Itoa(db)); err != nil {
			nc.Close()
			return nil, fmt.Errorf("选择数据库 %d 失败: %w", db, err)
		}
	}
	return c, nil
}

// Close 关闭底层连接。
func (c *Conn) Close() error {
	if c == nil || c.nc == nil {
		return nil
	}
	return c.nc.Close()
}

// SetDeadline 设置读写截止时间（用于慢命令/监控控制）。
func (c *Conn) SetDeadline(t time.Time) error {
	return c.nc.SetDeadline(t)
}

// SetCmdTimeout 覆盖后续命令的读写截止：探活类调用不能拿着默认 60s 等，
// 否则一个 hang 住的 redis 会把状态轮询整段卡住。dial 已经按建连超时设了初始值。
func (c *Conn) SetCmdTimeout(d time.Duration) {
	c.mu.Lock()
	c.to = d
	c.mu.Unlock()
}

// deadlineOf 当前连接下一次命令的截止时刻（不监控 MONITOR 这种长期读取）
func (c *Conn) deadlineOf() time.Time {
	d := c.to
	if d <= 0 || d > cmdTimeout {
		d = cmdTimeout
	}
	return time.Now().Add(d)
}

// Command 发送一条命令并读取一条回复。同一连接上的调用通过互斥锁串行化。
func (c *Conn) Command(args ...string) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bad {
		return nil, errors.New("redis: 连接已不可用（上一条命令超时或回复异常），请重新连接")
	}
	_ = c.nc.SetDeadline(c.deadlineOf())
	err := c.writeCommand(args...)
	var v any
	if err == nil {
		v, err = c.readReply()
	}
	c.finish(err)
	return v, err
}

// finish 命令收尾：把致命错误标成废弃，否则清掉刚刚设的截止（MONITOR 等
// 长期读取不能继承这个窗口）。标废弃后不在此处 Close：调用方自己会 Close 或换连接。
func (c *Conn) finish(err error) {
	if fatalReplyErr(err) {
		c.bad = true
		return
	}
	_ = c.nc.SetDeadline(time.Time{})
}

// fatalReplyErr 超时、半截断、以及被上限拦下的超长回复，都意味着 RESP 流已经对不齐：
// 下一条命令会把上一条的残余当回复读，不废弃就会把错乱的数据当结果上报。
func fatalReplyErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, errProtoLimit) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, net.ErrClosed)
}

// Pipeline 一次性写入多条命令、单次 Flush，再依次读取全部回复，减少网络往返。
// 与 Command 共用连接锁，保证同一连接上的调用串行执行。
func (c *Conn) Pipeline(cmds [][]string) ([]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.bad {
		return nil, errors.New("redis: 连接已不可用（上一条命令超时或回复异常），请重新连接")
	}
	_ = c.nc.SetDeadline(c.deadlineOf())
	var err error
	for _, args := range cmds {
		if err = c.encodeCommand(args...); err != nil {
			c.finish(err)
			return nil, err
		}
	}
	if err = c.w.Flush(); err != nil {
		c.finish(err)
		return nil, err
	}
	out := make([]any, 0, len(cmds))
	for range cmds {
		var v any
		if v, err = c.readReply(); err != nil {
			c.finish(err)
			return nil, err
		}
		out = append(out, v)
	}
	c.finish(nil)
	return out, nil
}

func (c *Conn) writeCommand(args ...string) error {
	if err := c.encodeCommand(args...); err != nil {
		return err
	}
	return c.w.Flush()
}

// encodeCommand 仅向缓冲写入 RESP 字节，不刷新；供 Pipeline 批量写入后统一 Flush。
func (c *Conn) encodeCommand(args ...string) error {
	if _, err := c.w.WriteString("*" + strconv.Itoa(len(args)) + "\r\n"); err != nil {
		return err
	}
	for _, a := range args {
		if _, err := c.w.WriteString("$" + strconv.Itoa(len(a)) + "\r\n"); err != nil {
			return err
		}
		if _, err := c.w.WriteString(a + "\r\n"); err != nil {
			return err
		}
	}
	return nil
}

func (c *Conn) readReply() (any, error) {
	line, err := c.readLine()
	if err != nil {
		return nil, err
	}
	if len(line) == 0 {
		return nil, errors.New("redis: 空回复")
	}
	switch line[0] {
	case '+': // 简单字符串
		return line[1:], nil
	case '-': // 错误
		return nil, errors.New(line[1:])
	case ':': // 整数
		n, err := strconv.ParseInt(line[1:], 10, 64)
		if err != nil {
			return nil, err
		}
		return n, nil
	case '$': // 批量字符串
		n, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil // 批量 nil
		}
		if int64(n) > maxBulkSize {
			return nil, fmt.Errorf("%w: 单值 %d 字节（上限 %d）", errProtoLimit, n, maxBulkSize)
		}
		buf := make([]byte, n+2) // 含结尾 \r\n
		if _, err := io.ReadFull(c.r, buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	case '*': // 数组
		n, err := strconv.Atoi(line[1:])
		if err != nil {
			return nil, err
		}
		if n < 0 {
			return nil, nil // 数组 nil
		}
		if int64(n) > maxArrayLen {
			// 不拦这一行：make([]any, n) 就是十几 GB，还没读数据本身就能把内存打满
			return nil, fmt.Errorf("%w: 数组 %d 个元素（上限 %d）", errProtoLimit, n, maxArrayLen)
		}
		arr := make([]any, 0, min(n, 1024))
		for i := 0; i < n; i++ {
			v, err := c.readReply()
			if err != nil {
				return nil, err
			}
			arr = append(arr, v)
		}
		return arr, nil
	default:
		return nil, fmt.Errorf("redis: 协议错误 %q", line)
	}
}

func (c *Conn) readLine() (string, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return "", err
	}
	if len(line) > maxLineLen {
		return "", fmt.Errorf("%w: 一行 %d 字节（上限 %d）", errProtoLimit, len(line), maxLineLen)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// maxLineLen 一行协议头的上限：正常回复头不超过几十字节，
// 把“没换行符就一直积”的失控场景在 16MB 处掉闸
const maxLineLen = 16 << 20

/* ============================ 回复类型转换辅助 ============================ */

func asString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int64:
		return strconv.FormatInt(t, 10)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func asInt(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case string:
		n, _ := strconv.ParseInt(t, 10, 64)
		return n
	case float64:
		return int64(t)
	default:
		return 0
	}
}

func asFloat(v any) float64 {
	switch t := v.(type) {
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	case int64:
		return float64(t)
	case float64:
		return t
	default:
		return 0
	}
}

func asArray(v any) []any {
	if arr, ok := v.([]any); ok {
		return arr
	}
	return nil
}

// asStringSlice 将数组回复转为字符串切片。
func asStringSlice(v any) []string {
	arr := asArray(v)
	out := make([]string, 0, len(arr))
	for _, e := range arr {
		out = append(out, asString(e))
	}
	return out
}
