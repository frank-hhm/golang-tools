package downloader

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ============================================================
// 外部流源任务测试：注册假 SourceResolver，直接喂 streamParams 调
// downloadStream（纯函数式入口，无需起整个 Manager），校验单连接搬运、
// 未知长度读至 EOF、零字节硬失败不空转重试、未登记 kind 拒绝。
// ============================================================

// fakeResolver 返回固定字节的只读流；openErr 非空则 Open 直接失败。
type fakeResolver struct {
	data     []byte
	size     int64 // 传给 Open 的返回值：-1 表示未知，走读至 EOF
	openErr  error
	openHits int
}

func (f *fakeResolver) Open(ctx context.Context, params map[string]string) (io.ReadCloser, int64, error) {
	f.openHits++
	if f.openErr != nil {
		return nil, 0, f.openErr
	}
	return io.NopCloser(bytes.NewReader(f.data)), f.size, nil
}

// flakyResolver 前 failFirst 次 Open 返回「先吐 failWrites 字节再报错」的流（模拟
// 传输中途断开，written>0 走重试路径），之后返回完整正确的流。
type flakyResolver struct {
	data       []byte
	failFirst  int
	failWrites int
	hits       int
}

func (f *flakyResolver) Open(ctx context.Context, params map[string]string) (io.ReadCloser, int64, error) {
	f.hits++
	if f.hits <= f.failFirst {
		return io.NopCloser(&errReader{data: f.data[:f.failWrites], err: errors.New("连接中断")}), int64(len(f.data)), nil
	}
	return io.NopCloser(bytes.NewReader(f.data)), int64(len(f.data)), nil
}

// errReader 读完 data 后返回非 EOF 错误，令 io.Copy 以 err 结束（触发重试而非截断落盘）。
type errReader struct {
	data []byte
	off  int
	err  error
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, r.err
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

func TestDownloadStreamWritesWholeFile(t *testing.T) {
	const kind = "test-ok"
	payload := genData(4096)
	RegisterSource(kind, &fakeResolver{data: payload, size: int64(len(payload))})

	dir := t.TempDir()
	save := filepath.Join(dir, "out.bin")
	err := downloadStream(streamParams{
		ctx: context.Background(), kind: kind, params: map[string]string{"x": "1"},
		savePath: save, maxRetry: 2,
	})
	if err != nil {
		t.Fatalf("downloadStream 期望成功，得：%v", err)
	}
	got, err := os.ReadFile(save)
	if err != nil {
		t.Fatalf("读回落盘文件失败：%v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("落盘内容与源字节不一致：got=%d want=%d", len(got), len(payload))
	}
	if _, err := os.Stat(save + partSuffix); !os.IsNotExist(err) {
		t.Fatalf("完成后 .part 应已被 rename 清除")
	}
}

func TestDownloadStreamUnknownSizeReadsToEOF(t *testing.T) {
	const kind = "test-eof"
	payload := genData(1000)
	RegisterSource(kind, &fakeResolver{data: payload, size: -1})

	save := filepath.Join(t.TempDir(), "eof.bin")
	if err := downloadStream(streamParams{
		ctx: context.Background(), kind: kind, savePath: save, maxRetry: 1,
	}); err != nil {
		t.Fatalf("size=-1 应读至 EOF 成功，得：%v", err)
	}
	got, _ := os.ReadFile(save)
	if len(got) != len(payload) {
		t.Fatalf("未知长度应读全部：%d want %d", len(got), len(payload))
	}
}

func TestDownloadStreamZeroByteFailureNoRetry(t *testing.T) {
	const kind = "test-hardfail"
	boom := errors.New("账号不存在")
	fr := &fakeResolver{openErr: boom}
	RegisterSource(kind, fr)

	save := filepath.Join(t.TempDir(), "never.bin")
	err := downloadStream(streamParams{
		ctx: context.Background(), kind: kind, savePath: save, maxRetry: 5,
	})
	if err == nil {
		t.Fatalf("Open 失败时 downloadStream 应返回错误")
	}
	// 一个字节都没拿到：直接透出真实原因，不空转退避（只 Open 一次）。
	if fr.openHits != 1 {
		t.Fatalf("零字节硬失败不应重试，实际 Open %d 次", fr.openHits)
	}
	if !errors.Is(err, boom) && err.Error() != boom.Error() {
		t.Fatalf("应透出原始硬失败原因 %v，得 %v", boom, err)
	}
}

func TestDownloadStreamUnregisteredKind(t *testing.T) {
	err := downloadStream(streamParams{
		ctx: context.Background(), kind: "no-such-kind-xyz",
		savePath: filepath.Join(t.TempDir(), "x"), maxRetry: 1,
	})
	if err == nil {
		t.Fatalf("未登记的 kind 应直接报错")
	}
}

// TestDownloadStreamRetriesAfterPartialWrite：中途断开（已写 >0 字节）应退避重试，
// 重下从头截断重来，最终落盘为完整正确内容。
func TestDownloadStreamRetriesAfterPartialWrite(t *testing.T) {
	const kind = "test-retry"
	payload := genData(3000)
	fr := &flakyResolver{data: payload, failFirst: 1, failWrites: 500}
	RegisterSource(kind, fr)

	save := filepath.Join(t.TempDir(), "retry.bin")
	err := downloadStream(streamParams{
		ctx: context.Background(), kind: kind, savePath: save, maxRetry: 3,
	})
	if err != nil {
		t.Fatalf("written>0 应重试后成功，得：%v", err)
	}
	if fr.hits != 2 {
		t.Fatalf("应重试一次（共 Open 2 次），实际 %d 次", fr.hits)
	}
	got, _ := os.ReadFile(save)
	if !bytes.Equal(got, payload) {
		t.Fatalf("重试后应落完整内容：%d want %d", len(got), len(payload))
	}
}

func TestDownloadStreamProgressCallback(t *testing.T) {
	const kind = "test-progress"
	payload := genData(2048)
	RegisterSource(kind, &fakeResolver{data: payload, size: int64(len(payload))})

	var lastDL int64
	var lastSize int64
	save := filepath.Join(t.TempDir(), "p.bin")
	err := downloadStream(streamParams{
		ctx: context.Background(), kind: kind, savePath: save, maxRetry: 1,
		onProgress: func(size, dl int64) { lastSize, lastDL = size, dl },
	})
	if err != nil {
		t.Fatalf("downloadStream 失败：%v", err)
	}
	if lastDL != int64(len(payload)) || lastSize != int64(len(payload)) {
		t.Fatalf("进度终值应为 size=%d dl=%d，得 size=%d dl=%d", len(payload), len(payload), lastSize, lastDL)
	}
}

func TestAddSourceRejectsUnregisteredKind(t *testing.T) {
	m := &Manager{}
	if _, err := m.AddSource("definitely-not-registered", map[string]string{"a": "b"}, AddOpts{Name: "f.bin"}); err == nil {
		t.Fatalf("AddSource 应拒绝未登记的 kind")
	}
}

func TestAddSourceRejectsEmptyKind(t *testing.T) {
	m := &Manager{}
	if _, err := m.AddSource("", map[string]string{"a": "b"}, AddOpts{Name: "f.bin"}); err == nil {
		t.Fatalf("空 kind 应被拒")
	}
}

// TestDownloadStreamCtxCancel 取消应中断搬运并返回 ctx.Err()，不留正式文件。
func TestDownloadStreamCtxCancel(t *testing.T) {
	const kind = "test-cancel"
	// 缓慢源：读一段就卡，制造可取消窗口。
	RegisterSource(kind, slowResolver{total: 1 << 20})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	save := filepath.Join(t.TempDir(), "cancel.bin")
	err := downloadStream(streamParams{
		ctx: ctx, kind: kind, savePath: save, maxRetry: 0,
	})
	if err == nil {
		t.Fatalf("超时取消应返回错误")
	}
	if _, err := os.Stat(save); !os.IsNotExist(err) {
		t.Fatalf("取消后不应产出正式文件")
	}
}

// slowResolver 每次 Read 睡眠一小会儿，令整体搬运超过测试超时。
type slowResolver struct{ total int64 }

func (s slowResolver) Open(ctx context.Context, params map[string]string) (io.ReadCloser, int64, error) {
	return io.NopCloser(&slowReader{n: s.total}), s.total, nil
}

type slowReader struct{ n int64 }

func (r *slowReader) Read(p []byte) (int, error) {
	if r.n <= 0 {
		return 0, io.EOF
	}
	time.Sleep(10 * time.Millisecond)
	p[0] = 'a'
	r.n--
	return 1, nil
}
