package portman

import (
	"syscall"
	"testing"
)

func TestProtoOf(t *testing.T) {
	// Type 是 socket 类型号，不是字符串：这条最容易想当然，钉住它
	if got := protoOf(syscall.SOCK_STREAM); got != "tcp" {
		t.Fatalf("SOCK_STREAM => %q, want tcp", got)
	}
	if got := protoOf(syscall.SOCK_DGRAM); got != "udp" {
		t.Fatalf("SOCK_DGRAM => %q, want udp", got)
	}
	if got := protoOf(9999); got != "" {
		t.Fatalf("未知 socket 类型应返回空串，实际 %q", got)
	}
}

func TestNormIP(t *testing.T) {
	cases := []struct{ in, want string }{
		{"fe80::1%eth0", "fe80::1"},
		{"::ffff:127.0.0.1", "127.0.0.1"},
		{"0.0.0.0", "0.0.0.0"}, // 地址不改写：v4/v6 的绑定范围是有用信息
		{"::", "::"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normIP(c.in); got != c.want {
			t.Fatalf("normIP(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestNormState(t *testing.T) {
	if got := normState("udp", ""); got != "-" {
		t.Fatalf("UDP 无连接状态应为 -，实际 %q", got)
	}
	if got := normState("tcp", ""); got != "-" {
		t.Fatalf("空状态应为 -，实际 %q", got)
	}
	if got := normState("tcp", "established"); got != "ESTABLISHED" {
		t.Fatalf("状态应统一大写，实际 %q", got)
	}
}

func TestSummarize(t *testing.T) {
	rows := []PortRow{
		{Proto: "tcp", Lport: 80, State: "LISTEN", Listen: true, Pid: 10},
		{Proto: "tcp", Lport: 80, State: "LISTEN", Listen: true, Pid: 10}, // v6 上的同一端口 → 去重
		{Proto: "udp", Lport: 53, State: "-", Listen: true, Pid: 10},
		{Proto: "tcp", Lport: 80, Rport: 1234, State: "ESTABLISHED", Pid: 20},
		{Proto: "tcp", Lport: 9, Rport: 1, State: "TIME_WAIT", Pid: 0}, // 归属看不到 → 不计进程
	}
	got := summarize(rows, []string{"host"})
	if got.ListenCount != 2 || got.ConnCount != 2 || got.ProcCount != 2 {
		t.Fatalf("summarize = %+v, want listen 2 / conn 2 / proc 2", got)
	}
	if len(got.Hosts) != 1 || got.Hosts[0] != "host" {
		t.Fatalf("Hosts = %v, want [host]", got.Hosts)
	}
}

// 真机冒烟：这台机器拿得到端口表时，验不变量（协议合法、端口在范围、按端口升序、汇总不为 0）。
func TestSnapshotPortsInvariants(t *testing.T) {
	s, err := SnapshotPorts()
	if err != nil {
		t.Skipf("环境拿不到端口表: %v", err)
	}
	if len(s.Rows) == 0 {
		t.Skip("这台机器上没有 inet socket")
	}
	prev := -1
	for _, r := range s.Rows {
		if r.Proto != "tcp" && r.Proto != "udp" {
			t.Fatalf("非法协议 %q（行 %+v）", r.Proto, r)
		}
		if r.Lport < 0 || r.Lport > 65535 {
			t.Fatalf("端口越界 %d（行 %+v）", r.Lport, r)
		}
		if r.Lport < prev {
			t.Fatalf("未按本地端口升序：%d 排在 %d 之后", r.Lport, prev)
		}
		prev = r.Lport
	}
	if s.Sum.ListenCount == 0 {
		t.Fatalf("任何机器都该有监听端口，汇总 = %+v", s.Sum)
	}
}
