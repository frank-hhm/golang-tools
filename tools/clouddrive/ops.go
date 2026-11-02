package clouddrive

// 下载 / 上传进度外壳（计划-云盘管理工具 PR4）。
//
// 绑定层（services/app）自己掌控「打开源流 / 落盘 / 取消」的文件与目录逻辑，
// 这里只把 transfer.go 里的计数 + 节流 + 相位收放封装成一个可复用的外壳：
// 包一层计数 reader 按节流发 cloud:progress（Op 由构造方指定 download/upload），
// 成功/失败再补一条终态事件。跨平台零 cgo：只有 io / context / sync。

import (
	"context"
	"io"
	"sync"
	"time"
)

// OpTracker 是下载/上传的进度上报外壳（内部状态自持锁，可安全跨协程读）。
type OpTracker struct {
	mu     sync.Mutex
	base   Progress
	total  int64
	last   int
	lastAt time.Time
	trans  int64
}

// NewOpTracker 建一个进度外壳。id 传空自动生成；total<=0 表示大小未知（不定进度）。
func NewOpTracker(op, id, name string, total int64) *OpTracker {
	if id == "" {
		id = newTransferID()
	}
	if total < 0 {
		total = -1
	}
	return &OpTracker{
		base:  Progress{Op: op, ID: id, Name: name, Stage: StageRunning},
		total: total,
		last:  -1,
	}
}

// ID 本次操作的进度 id（任务中心按它归并同一条进度）。
func (t *OpTracker) ID() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.base.ID
}

// Wrap 用计数 reader 包住源流：读出字节即累计并按节流发进度；
// ctx 取消时中断读取并返回 ErrCanceled（供上层区分「取消」与「真失败」）。
func (t *OpTracker) Wrap(ctx context.Context, r io.Reader) io.Reader {
	return &countingReader{
		r:   r,
		ctx: ctx,
		report: func(trans int64) {
			t.mu.Lock()
			defer t.mu.Unlock()
			t.trans = trans
			p := t.base
			reportProgress(&p, t.total, trans, &t.last, &t.lastAt, false)
		},
	}
}

// Done 收尾一条成功事件（percent 拉满，note 由上层给可读结果）。
func (t *OpTracker) Done(note string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	p := t.base
	p.Stage = StageDone
	p.Percent = 100
	p.Note = note
	emitProgress(p)
}

// Fail 收尾一条失败事件。
func (t *OpTracker) Fail(err error) {
	if err == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	p := t.base
	p.Stage = StageError
	p.Note = clipNote(err.Error())
	emitProgress(p)
}
