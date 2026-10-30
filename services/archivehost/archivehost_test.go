package archivehost

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"Tools/appdirs"
	"Tools/tools/archive"
)

// 转交这一层是纯逻辑 + 回环 TCP，能真跑起来验，不像命名管道那样
// 只能等真机。这里把三条边界钉住：递得到、令牌不对、以及主程序撤了之后
// TrySend 必须老实返回 false（返回 true 就等于任务凭空消失）。

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
	// 端点文件根本不存在：等价于"主程序没跑"，必须 false 让调用方自己压
	if TrySend(Request{Options: archive.Options{Op: archive.OpCompress, Paths: []string{"x"}}}) {
		t.Fatal("没有监听者却返回了 true")
	}

	// 端点文件在、但那头没人（主程序崩了留下的残留）：拨号失败同样要 false
	s, err := Start(func(Request) {})
	if err != nil {
		t.Fatal(err)
	}
	ep, err := readEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	s.Stop() // Stop 会删文件，这里手动放回，模拟进程被硬杀留下的残留
	body := `{"addr":"` + ep.Addr + `","token":"` + ep.Token + `","pid":1}`
	if err := os.WriteFile(epFileFor(t), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if TrySend(Request{Options: archive.Options{Op: archive.OpCompress, Paths: []string{"x"}}}) {
		t.Fatal("端点指向死掉的监听者，不该返回 true")
	}
}

func TestTokenMismatchIsRejected(t *testing.T) {
	isolate(t)
	var hit int32
	s, err := Start(func(Request) { atomic.AddInt32(&hit, 1) })
	if err != nil {
		t.Fatal(err)
	}
	defer s.Stop()

	cn, err := net.DialTimeout("tcp", s.ln.Addr().String(), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer cn.Close()
	_ = cn.SetDeadline(time.Now().Add(3 * time.Second))
	// 令牌抄错一个字符：不能用，也不能把任务排上去
	if _, err := cn.Write([]byte(`{"token":"deadbeef","job":{"op":"compress","paths":["x"]}}` + "\n")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	n, err := cn.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(buf[:n])) != "no" {
		t.Fatalf("错令牌应回 no，实得 %q", strings.TrimSpace(string(buf[:n])))
	}
	// 给 onJob 一点时间被误调，再确认它没被调
	time.Sleep(100 * time.Millisecond)
	if n := atomic.LoadInt32(&hit); n != 0 {
		t.Fatalf("令牌不对却执行了任务：%d 次", n)
	}
}

func TestStopRemovesOwnEndpoint(t *testing.T) {
	isolate(t)
	s, err := Start(func(Request) {})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readEndpoint(); err != nil {
		t.Fatalf("Start 后端点文件应可读: %v", err)
	}
	s.Stop()
	if _, err := os.Stat(epFileFor(t)); err == nil {
		t.Fatal("Stop 后端点文件还留着：右键会一直去拨一个没人听的地址")
	}
	// 幂等：重复 Stop 不能 panic
	s.Stop()
}

func TestReadLineRejectsOversized(t *testing.T) {
	long := strings.Repeat("a", maxLine+10) + "\n"
	if _, err := readLine(strings.NewReader(long)); err == nil {
		t.Fatal("超长行应被拒，而不是先吃进内存")
	}
	if got, err := readLine(strings.NewReader(`{"op":"compress"}` + "\r\n")); err != nil || got != `{"op":"compress"}` {
		t.Fatalf("CRLF 行没剔干净：%q err=%v", got, err)
	}
}

// epFileFor 端点文件的绝对路径（与 Start 用的同一套推导）。
func epFileFor(t *testing.T) string {
	t.Helper()
	p, err := endpointFile()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Clean(p)
}
