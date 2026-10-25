package portcheck

import (
	"net"
	"testing"
)

// TestTaken 覆盖三种占用来源：其它实例登记、系统正在监听、排除自身
func TestTaken(t *testing.T) {
	// 真占一个端口，模拟系统里已有进程在监听
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("监听临时端口失败: %v", err)
	}
	defer ln.Close()
	listening := ln.Addr().(*net.TCPAddr).Port

	if Free(listening) == nil {
		t.Fatalf("端口 %d 正在被监听，Free 却判定为空闲", listening)
	}
	if r := Taken(listening, ""); r == "" {
		t.Fatalf("端口 %d 正在被监听，Taken 应报占用", listening)
	}

	// 登记一个没有进程在跑的“实例”，端口号仍应视为不可用
	free := Suggest(20000, 200)
	key := "test:" + string(rune('a'))
	Register(func() []Owner {
		return []Owner{{Key: key, Label: "测试实例「a」", Ports: []int{free}}}
	})
	if r := Taken(free, ""); r == "" {
		t.Fatalf("端口 %d 已被实例登记，Taken 应报占用", free)
	}
	// 编辑自身：端口没改时不算冲突
	if r := Taken(free, key); r != "" {
		t.Fatalf("排除自身后不该报占用，得到: %s", r)
	}
}

// TestSuggestSkipsRegistered Suggest 必须跳过已登记端口
func TestSuggestSkipsRegistered(t *testing.T) {
	from := Suggest(30000, 200)
	Register(func() []Owner {
		return []Owner{{Key: "test:skip", Label: "测试实例「b」", Ports: []int{from}}}
	})
	if got := Suggest(from, 5); got == from {
		t.Fatalf("端口 %d 已登记，Suggest 应跳过，得到: %d", from, got)
	}
}
