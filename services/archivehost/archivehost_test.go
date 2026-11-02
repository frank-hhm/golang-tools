package archivehost

import (
	"testing"
	"time"

	"Tools/appdirs"
	"Tools/tools/archive"
)

// 机制层的边界测试已随抽取搬到 Tools/services/singleinstance；这里只保留
// 证明 archivehost 这层薄封装收发无碍的公共 API 回归（Start/TrySend/Stop）。

// isolate 把 appdirs 的数据目录指向临时目录，端点文件就不会落进真实布局。
func isolate(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := appdirs.Set(dir, dir, dir); err != nil {
		t.Fatal(err)
	}
}

func TestHandoffRoundTrip(t *testing.T) {
	isolate(t)
	got := make(chan Request, 1)
	s, err := Start(func(r Request) { got <- r })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	job := Request{
		Options: archive.Options{
			Op:     archive.OpCompress,
			Paths:  []string{`C:\tmp\a.txt`, `C:\tmp\b`},
			Format: "zip",
			Level:  6,
			OutDir: `C:\tmp`,
		},
		From: "verb",
	}
	if !TrySend(job) {
		t.Fatal("TrySend 返回 false：主程序明明在监听")
	}
	select {
	case r := <-got:
		if r.Op != archive.OpCompress || r.From != "verb" || r.Level != 6 {
			t.Fatalf("负载走样：%+v", r)
		}
		if len(r.Paths) != 2 || r.Paths[1] != `C:\tmp\b` {
			t.Fatalf("路径没原样递过去：%q", r.Paths)
		}
		if r.OutDir != `C:\tmp` {
			t.Fatalf("OutDir = %q", r.OutDir)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("onJob 没被调到")
	}
}

func TestTrySendWithoutServer(t *testing.T) {
	isolate(t)
	// 端点文件根本不存在：等价于「主程序没跑」，必须 false 让调用方自己压
	if TrySend(Request{Options: archive.Options{Op: archive.OpCompress, Paths: []string{"x"}}}) {
		t.Fatal("没有监听者却返回了 true")
	}
}
