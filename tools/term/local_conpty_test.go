//go:build windows

package term

// 本地终端输入通路回归测试：走应用真实的调用链
//
//	OpenLocal(ConPTY 起 shell) → Write(按键) → term:data(输出)
//
// 盯的是「banner 与提示符正常、敲任何键都没反应」这类只在真实会话里暴露的问题：
// 单测 Win32 胶水不够，会话注册表与事件通道那一层也得串起来。
//
// 另外必须让本测试在「std 句柄被重定向」的环境里也跑得过——go test 正是这种环境，
// 而它曾经真实复现过该故障：子进程继承了父进程被重定向的 stdin，shell 就从那根
// 句柄读命令，ConPTY 写进来的按键全被丢弃（local_windows.go 里显式声明
// STARTF_USESTDHANDLES 就是为了堵住这条路）。
//
// 需要真起进程与伪控制台，仅 Windows 适用。

import (
	"encoding/base64"
	"strings"
	"sync"
	"testing"
	"time"
)

type outCapture struct {
	mu   sync.Mutex
	text string
}

func (c *outCapture) append(s string) {
	c.mu.Lock()
	c.text += s
	c.mu.Unlock()
}

// wait 等到输出里出现 sub 为止；超时则把已收到的全部内容打出来，便于定位。
func (c *outCapture) wait(t *testing.T, sub string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		c.mu.Lock()
		got := c.text
		c.mu.Unlock()
		if strings.Contains(got, sub) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s 内未等到 %q，实际输出：%q", timeout, sub, got)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestLocalShellWriteEchoes(t *testing.T) {
	useTempDataLocal(t)
	if _, err := ListHosts(); err != nil {
		t.Fatalf("补种本地默认项失败: %v", err)
	}

	for _, shell := range []struct {
		hostID string
		banner string
	}{
		{LocalIDCmd, "Microsoft Windows"},
		{LocalIDPS, "PS "},
	} {
		t.Run(shell.hostID, func(t *testing.T) {
			capture := &outCapture{}
			SetEmitter(func(name string, data ...any) {
				if name != "term:data" || len(data) == 0 {
					return
				}
				ev, ok := data[0].(DataEvent)
				if !ok {
					return
				}
				raw, derr := base64.StdEncoding.DecodeString(ev.Data)
				if derr != nil {
					return
				}
				capture.append(string(raw))
			})
			defer SetEmitter(nil)

			info, err := OpenLocal(shell.hostID, 100, 30)
			if err != nil {
				t.Fatalf("打开本地终端失败: %v", err)
			}
			defer func() { _ = Close(info.ID) }()

			// banner：证明输出通路与会话 id 都对得上
			capture.wait(t, shell.banner, 15*time.Second)

			marker := "PING_FROM_WRITE_" + shell.hostID
			if werr := Write(info.ID, base64.StdEncoding.EncodeToString([]byte("echo "+marker+"\r"))); werr != nil {
				t.Fatalf("写入按键失败: %v", werr)
			}
			// 回显与执行结果都要出现，才说明输入真进了 shell
			capture.wait(t, marker, 15*time.Second)
		})
	}
}
