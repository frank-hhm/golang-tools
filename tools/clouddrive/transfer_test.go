package clouddrive

// 跨盘转存（PR3）与进度事件（PR4）的单测：
// 用两两 fake provider（Google / 阿里 / 百度的 httptest 假后端）跑真实适配器代码路径，
// 按字节校验「源内容 == 目标收到内容」，并验证进度事件相位单调、记录表裁剪、
// 取消与失败透出。只验到 mock 层，不代表真网可用。

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// resetTransfers 清空转存记录表（进程内全局态，测试之间必须隔离）。
func resetTransfers() {
	trMu.Lock()
	transfers = map[string]*Transfer{}
	trCancels = map[string]context.CancelFunc{}
	trOrder = nil
	trMu.Unlock()
}

// captureProgress 把 emitter 换成内存收集器（全局 captured）；测试结束还原为 nil。
var captured func() []Progress

func captureProgress(t *testing.T) {
	t.Helper()
	var (
		mu  sync.Mutex
		got []Progress
	)
	SetEmitter(func(name string, data ...any) {
		if name != "cloud:progress" || len(data) == 0 {
			return
		}
		if p, ok := data[0].(Progress); ok {
			mu.Lock()
			got = append(got, p)
			mu.Unlock()
		}
	})
	captured = func() []Progress {
		mu.Lock()
		defer mu.Unlock()
		out := make([]Progress, len(got))
		copy(out, got)
		return out
	}
	t.Cleanup(func() { SetEmitter(nil); captured = nil })
}

// waitAllTransfers 等到所有转存记录离开 running（避免后台协程在假服务关闭后继续访网）。
func waitAllTransfers() {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		busy := false
		for _, rec := range ListTransfers() {
			if rec.Stage == StageRunning {
				busy = true
				break
			}
		}
		if !busy {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// errProvider 一个只用于「打开流成功 / 上传必失败」的 provider 替身。
type errProvider struct {
	upErr error
	open  func() (io.ReadCloser, int64, error)
}

func (e *errProvider) Name() string               { return "替身盘" }
func (e *errProvider) Test(context.Context) error { return nil }
func (e *errProvider) List(context.Context, string) ([]Entry, error) {
	return nil, nil
}
func (e *errProvider) Mkdir(context.Context, string, string) (Entry, error) {
	return Entry{}, e.upErr
}
func (e *errProvider) Rename(context.Context, string, string) error { return nil }
func (e *errProvider) Move(context.Context, string, string) error   { return nil }
func (e *errProvider) Delete(context.Context, string) error         { return nil }
func (e *errProvider) ShareLink(context.Context, string) (string, error) {
	return "", nil
}
func (e *errProvider) Open(ctx context.Context, _ string) (io.ReadCloser, int64, error) {
	return e.open()
}
func (e *errProvider) Upload(context.Context, string, string, io.Reader, int64) (Entry, error) {
	return Entry{}, e.upErr
}
func (e *errProvider) Quota(context.Context) (Quota, error) { return Quota{}, nil }

// blockReader 第一次交出若干字节后阻塞，直到上层关闭 proceed 或 ctx 取消：
// 用来确定性地制造「传输中途取消」。
type blockReader struct {
	first   []byte
	proceed chan struct{}
	ctx     context.Context
	once    sync.Once
}

func (b *blockReader) Read(p []byte) (int, error) {
	if len(b.first) > 0 {
		n := copy(p, b.first)
		b.first = b.first[n:]
		return n, nil
	}
	select {
	case <-b.proceed:
		return 0, io.EOF
	case <-b.ctx.Done():
		return 0, ErrCanceled
	}
}

func (b *blockReader) Close() error {
	b.once.Do(func() {
		if b.proceed != nil {
			close(b.proceed)
		}
	})
	return nil
}

func TestCopyGoogleToAliyunBytesMatch(t *testing.T) {
	resetTransfers()
	f := newFakeGoogle(t)
	f.media = []byte("cross-drive payload")
	g := newTestProvider(t, "AT", time.Now().Add(time.Hour).UnixMilli())
	a, af := newTestAliyun(t)

	e, err := Copy(context.Background(), CopyParams{
		Src: g, SrcID: "f1", Dst: a, DstParentID: "d1", Name: "copy.txt", NameHint: "a.txt",
	})
	if err != nil {
		t.Fatalf("Copy 失败: %v", err)
	}
	if e.ID != "up1" {
		t.Fatalf("目标条目解析错: %+v", e)
	}
	if got := string(af.gotUploaded()); got != "cross-drive payload" {
		t.Fatalf("目标收到内容不一致: %q", got)
	}
}

func TestCopyAliyunToBaiduBytesMatch(t *testing.T) {
	resetTransfers()
	a, af := newTestAliyun(t)
	b, bf := newTestBaidu(t)
	// 假服务在 newTestXxx 里才创建，必须通过 newTestXxx 拿到的 fake 注入下载内容。
	const payload = "aliyun original bytes"
	af.setMedia([]byte(payload))

	if _, err := Copy(context.Background(), CopyParams{
		Src: a, SrcID: "f1", Dst: b, DstParentID: "/Folder", Name: "copy.txt",
	}); err != nil {
		t.Fatalf("Copy 失败: %v", err)
	}
	if got := string(bf.gotUploaded()); got != payload {
		t.Fatalf("百度分块内容不一致: %q", got)
	}
}

func TestStartTransferRecordsAndMonotonicProgress(t *testing.T) {
	resetTransfers()
	captureProgress(t)
	// 先注册「等后台转存到终态」的 cleanup：它比 newTestXxx 注册的假服务还原靠后执行，
	// 避免全局端点变量被还原后、后台协程还去打真网。
	t.Cleanup(waitAllTransfers)

	f := newFakeGoogle(t)
	f.media = bytes.Repeat([]byte("x"), 64<<10)
	// 两个账号都走 registry.New（同指向假 Drive），假服务的 files.upload 恒回 up1。
	src, err := AddAccount(KindGoogle, "源盘")
	if err != nil {
		t.Fatal(err)
	}
	dst, err := AddAccount(KindGoogle, "目标盘")
	if err != nil {
		t.Fatal(err)
	}
	tok := tokenBlob{Access: "AT", Refresh: "RT", Expiry: time.Now().Add(time.Hour).UnixMilli()}
	if err := saveToken(src.ID, tok); err != nil {
		t.Fatal(err)
	}
	if err := saveToken(dst.ID, tok); err != nil {
		t.Fatal(err)
	}

	// 源条目 id 用 fake 里存在的 "f1"，dirID 传根：应顺带把显示名换成 a.txt
	tr, err := StartTransfer(src.ID, "f1", "", dst.ID, "", "")
	if err != nil {
		t.Fatalf("StartTransfer: %v", err)
	}
	if tr.ID == "" {
		t.Fatal("未回传转存 id")
	}
	if tr.Stage != StageRunning {
		t.Fatalf("刚发起应为 running: %+v", tr)
	}

	deadline := time.After(10 * time.Second)
	for {
		cur, ok := GetTransfer(tr.ID)
		if !ok {
			t.Fatal("转存记录不存在")
		}
		if cur.Stage == StageDone || cur.Stage == StageError || cur.Stage == StageCanceled {
			if cur.Stage != StageDone {
				t.Fatalf("转存未正常完成: %+v", cur)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("等待转存终态超时: %+v", cur)
		case <-time.After(20 * time.Millisecond):
		}
	}

	events := captured()
	if len(events) == 0 {
		t.Fatal("未发出任何 cloud:progress")
	}
	last := -1
	var sawRunning, sawDone bool
	for _, p := range events {
		if p.Op != OpTransfer || p.ID != tr.ID {
			t.Fatalf("事件归属错: %+v", p)
		}
		if p.Percent < last {
			t.Fatalf("百分比非单调: %d < %d", p.Percent, last)
		}
		last = p.Percent
		switch p.Stage {
		case StageRunning:
			sawRunning = true
		case StageDone:
			sawDone = true
		}
	}
	if !sawRunning || !sawDone {
		t.Fatalf("缺少 running/done 相位: %+v", events)
	}
	if last != 100 {
		t.Fatalf("末态百分比应为 100，got %d", last)
	}
	list := ListTransfers()
	if len(list) != 1 || list[0].Name != "a.txt" {
		t.Fatalf("转存记录异常: %+v", list)
	}
	if !strings.Contains(list[0].Note, "完成") {
		t.Fatalf("完成记录应写明已完成: %+v", list[0])
	}
}

func TestCopyCanceledByContext(t *testing.T) {
	resetTransfers()
	ctx, cancel := context.WithCancel(context.Background())
	br := &blockReader{first: []byte("head"), ctx: ctx, proceed: make(chan struct{})}
	open := func() (io.ReadCloser, int64, error) { return br, -1, nil }
	src := &errProvider{open: open}
	fakeDst := &uploadWaitProvider{errProvider: &errProvider{open: open}, started: make(chan struct{})}

	go func() {
		<-fakeDst.started
		cancel()
	}()
	_, err := Copy(ctx, CopyParams{Src: src, SrcID: "x", Dst: fakeDst, Name: "n.txt", Size: -1})
	if !errors.Is(err, ErrCanceled) {
		t.Fatalf("应报取消错误，got %v", err)
	}
}

func TestCopyUploadErrorSurfaces(t *testing.T) {
	resetTransfers()
	gf := newFakeGoogle(t)
	gf.media = []byte("some bytes")
	g := newTestProvider(t, "AT", time.Now().Add(time.Hour).UnixMilli())
	boom := errors.New("上游炸了")
	dst := &errProvider{upErr: boom, open: func() (io.ReadCloser, int64, error) { return nil, 0, nil }}
	if _, err := Copy(context.Background(), CopyParams{Src: g, SrcID: "f1", Dst: dst, Name: "x.txt"}); err == nil {
		t.Fatal("上传失败应透出错误")
	} else if !strings.Contains(err.Error(), "上游炸了") {
		t.Fatalf("错误信息丢失: %v", err)
	}
}

func TestCancelTransferRejectsUnknownAndFinished(t *testing.T) {
	resetTransfers()
	if err := CancelTransfer("nope"); err == nil {
		t.Fatal("不存在的记录应报错")
	}
	trMu.Lock()
	recordLocked(&Transfer{ID: "t1", Name: "a", Stage: StageDone})
	trMu.Unlock()
	if err := CancelTransfer("t1"); err == nil {
		t.Fatal("已结束的转存不应允许取消")
	}
}

func TestListTransfersTrimToKeep(t *testing.T) {
	resetTransfers()
	t.Cleanup(resetTransfers)
	trMu.Lock()
	for i := 0; i < keepTransfers+10; i++ {
		recordLocked(&Transfer{ID: "t" + itoa(i), Name: "n", Stage: StageDone})
	}
	trMu.Unlock()
	if got := len(ListTransfers()); got != keepTransfers {
		t.Fatalf("记录应裁剪到 %d 条，got %d", keepTransfers, got)
	}
}

func TestTransferHelpers(t *testing.T) {
	if got := baseOf("/a/b/c.txt"); got != "c.txt" {
		t.Fatalf("baseOf 路径型 id: %q", got)
	}
	if got := baseOf(`C:\x\y.log`); got != "y.log" {
		t.Fatalf("baseOf 反斜杠: %q", got)
	}
	if got := baseOf("1B2c3"); got != "1B2c3" {
		t.Fatalf("baseOf 原生 id 应原样: %q", got)
	}
	if humanBytes(0) != "0 B" || humanBytes(2048) != "2.0 KB" {
		t.Fatalf("humanBytes: %q %q", humanBytes(0), humanBytes(2048))
	}
	if humanBytesOrUnknown(-1) != "未知" {
		t.Fatal("未知大小应如实回「未知」")
	}
	long := strings.Repeat("啊", 200)
	if r := []rune(clipNote(long)); len(r) != 121 {
		t.Fatalf("clipNote 未截断: %d", len(r))
	}
	if clipNote("一行\n两行") != "一行 两行" {
		t.Fatalf("clipNote 未压行: %q", clipNote("一行\n两行"))
	}
}

// uploadWaitProvider 在 errProvider 基础上只改 Upload：先消费首个块（让计数
// reader 走到第二次 Read 处阻塞），再轮询 ctx——用于确定性地制造「传输中途取消」。
type uploadWaitProvider struct {
	*errProvider
	started chan struct{}
	once    sync.Once
}

// Upload 先消费掉首个块，之后一直读到 ctx 取消。
func (p *uploadWaitProvider) Upload(ctx context.Context, _, _ string, r io.Reader, _ int64) (Entry, error) {
	buf := make([]byte, 1)
	if _, err := r.Read(buf); err != nil {
		return Entry{}, err
	}
	p.once.Do(func() { close(p.started) })
	for {
		if ctx.Err() != nil {
			return Entry{}, ErrCanceled
		}
		if _, err := r.Read(buf); err != nil {
			if errors.Is(err, ErrCanceled) || ctx.Err() != nil {
				return Entry{}, ErrCanceled
			}
			return Entry{}, err
		}
	}
}

// itoa 小工具，避免为了测试再拉 strconv。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
