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
	nc net.Conn
	r  *bufio.Reader
	w  *bufio.Writer
	mu sync.Mutex // 序列化同一连接上的命令读写，避免并发使用 bufio 破坏 RESP 流
}

// dial 建立到 Redis 的 TCP 连接，按需执行 AUTH 与 SELECT。
func dial(addr, password string, db int, timeout time.Duration) (*Conn, error) {
	nc, err := net.DialTimeout("tcp", addr, timeout)
	if err != nil {
		return nil, err
	}
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

// Command 发送一条命令并读取一条回复。同一连接上的调用通过互斥锁串行化。
func (c *Conn) Command(args ...string) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.writeCommand(args...); err != nil {
		return nil, err
	}
	return c.readReply()
}

// Pipeline 一次性写入多条命令、单次 Flush，再依次读取全部回复，减少网络往返。
// 与 Command 共用连接锁，保证同一连接上的调用串行执行。
func (c *Conn) Pipeline(cmds [][]string) ([]any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, args := range cmds {
		if err := c.encodeCommand(args...); err != nil {
			return nil, err
		}
	}
	if err := c.w.Flush(); err != nil {
		return nil, err
	}
	out := make([]any, 0, len(cmds))
	for range cmds {
		v, err := c.readReply()
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
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
		arr := make([]any, n)
		for i := 0; i < n; i++ {
			v, err := c.readReply()
			if err != nil {
				return nil, err
			}
			arr[i] = v
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
	return strings.TrimRight(line, "\r\n"), nil
}

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
