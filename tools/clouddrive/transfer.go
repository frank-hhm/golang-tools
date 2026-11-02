package clouddrive

// 云→云跨盘转存（计划-云盘管理工具 PR3）。
//
// 数据通路：src.Open 拿只读流 → 包一层计数 reader 周期发进度 → dst.Upload 收流。
// 三家适配器的 Upload 现下都是「整段缓冲」实现（Google multipart / 阿里 PUT /
// 百度 superupload 各算 md5），因此这里把流直接读进内存而不是先落临时文件：
// 少一次磁盘读写、也不需要临时目录清理；超大文件由调用方按 provider 的 size 判断
// 是否合适（超限时进度事件里会如实报读取失败，不会静默截断）。
//
// 转存是异步的：StartTransfer 立刻回一个传输 id，后台协程跑 Copy，
// 前端凭 CloudListTransfers 轮询 + cloud:progress 事件刷新；CancelTransfer 通过
// context 取消（计数 reader 会检查 ctx，源流读取随之中断）。
//
// 全跨平台零 cgo：只有 io / context / sync，Linux 面板构建同样可用。

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"Tools/services/applog"
)

// ErrCanceled 转存被用户取消（Copy 用它区分「取消」与「真失败」）。
var ErrCanceled = errors.New("转存已取消")

// Transfer 一笔转存的运行态快照（内存态，不落盘：应用重启即清空）。
type Transfer struct {
	ID        string `json:"id"`
	Op        string `json:"op"` // 恒为 OpTransfer，留字段与 Progress 对齐
	SrcAcct   string `json:"srcAcct"`
	SrcPath   string `json:"srcPath"`
	DstAcct   string `json:"dstAcct"`
	DstPath   string `json:"dstPath"`
	Name      string `json:"name"`
	Size      int64  `json:"size"` // 总字节，未知为 -1
	Trans     int64  `json:"trans"`
	Stage     string `json:"stage"`
	Note      string `json:"note"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

const (
	// keepTransfers 转存记录保留条数（内存里只留最近若干条供前端回看）。
	keepTransfers = 50
	// progressThrottleMs 进度事件最小间隔：避免每秒几十条把任务中心刷爆。
	progressThrottleMs = 300
)

var (
	trMu       sync.Mutex
	transfers  = map[string]*Transfer{}
	trCancels  = map[string]context.CancelFunc{}
	trOrder    []string
	newTrIDMu  sync.Mutex
	newTrIDSeq int
)

// newTransferID 生成进程内唯一的转存 id（时间戳 + 自增，避免同毫秒撞车）。
func newTransferID() string {
	newTrIDMu.Lock()
	defer newTrIDMu.Unlock()
	newTrIDSeq++
	return fmt.Sprintf("t%d%d", time.Now().UnixNano(), newTrIDSeq)
}

// snapshotLocked 复制一份可安全交给前端的快照（调用方持 trMu）。
func (t *Transfer) snapshotLocked() Transfer {
	c := *t
	return c
}

// recordLocked 把记录插到有序表头并裁剪超长部分（调用方持 trMu）。
func recordLocked(t *Transfer) {
	transfers[t.ID] = t
	trOrder = append([]string{t.ID}, trOrder...)
	for len(trOrder) > keepTransfers {
		old := trOrder[len(trOrder)-1]
		if old != t.ID {
			delete(transfers, old)
			delete(trCancels, old)
		}
		trOrder = trOrder[:len(trOrder)-1]
	}
}

// ListTransfers 最近若干条转存记录（新的在前）。
func ListTransfers() []Transfer {
	trMu.Lock()
	defer trMu.Unlock()
	out := make([]Transfer, 0, len(trOrder))
	for _, id := range trOrder {
		if t, ok := transfers[id]; ok {
			out = append(out, t.snapshotLocked())
		}
	}
	return out
}

// GetTransfer 单条转存快照。
func GetTransfer(id string) (Transfer, bool) {
	trMu.Lock()
	defer trMu.Unlock()
	t, ok := transfers[strings.TrimSpace(id)]
	if !ok {
		return Transfer{}, false
	}
	return t.snapshotLocked(), true
}

// CancelTransfer 取消一笔转存：置 canceled 并撤销其 context，
// 后台协程在下一个读块处退出（三家适配器的 Open 都是 http body 流，
// ctx 取消会让底层请求尽快返回错误）。
func CancelTransfer(id string) error {
	id = strings.TrimSpace(id)
	trMu.Lock()
	t, ok := transfers[id]
	cancel := trCancels[id]
	var name string
	if ok {
		name = t.Name
		if t.Stage == StageDone || t.Stage == StageError || t.Stage == StageCanceled {
			trMu.Unlock()
			return fmt.Errorf("该转存已结束，无需取消")
		}
		t.Stage = StageCanceled
		t.Note = "已请求取消"
		t.UpdatedAt = time.Now().UnixMilli()
	}
	trMu.Unlock()
	if !ok {
		return fmt.Errorf("转存记录不存在: %s", id)
	}
	if cancel != nil {
		cancel()
	}
	emitProgress(Progress{Op: OpTransfer, ID: id, Name: name, Stage: StageCanceled, Note: "已取消"})
	return nil
}

// statEntry 在源账号里按 id 找一条目录项（拿大小与显示名）。
// 目录列举是转存前的一次额外往返，换来确定「总字节」这一进度基准。
func statEntry(ctx context.Context, p Provider, dirID, id string) (Entry, error) {
	list, err := p.List(ctx, dirID)
	if err != nil {
		return Entry{}, err
	}
	for _, e := range list {
		if e.ID == id {
			return e, nil
		}
	}
	return Entry{}, fmt.Errorf("源文件不存在或不可见: %s", id)
}

// countingReader 包一层已读字节计数，由调用方的 report 回调节流发事件，
// 并在 ctx 取消时直接返回错误，让 io.Copy 尽早收手。
type countingReader struct {
	r           io.Reader
	ctx         context.Context
	report      func(trans int64)
	transferred int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, ErrCanceled
	}
	n, err := c.r.Read(p)
	if n > 0 {
		c.transferred += int64(n)
		c.report(c.transferred)
	}
	return n, err
}

// reportProgress 把「已传字节」换算成事件：百分比变化或距上次发送超过节流窗口才发，
// 总大小未知时 percent 恒 0（前端画不定进度）。
func reportProgress(p *Progress, total int64, trans int64, last *int, lastAt *time.Time, force bool) {
	pct := 0
	if total > 0 {
		pct = int(trans * 100 / total)
		if pct > 99 && !force {
			pct = 99 // 未完成前最多报到 99%，避免「看起来已经完了」
		}
		if pct > 100 {
			pct = 100
		}
	}
	now := time.Now()
	if !force && pct == *last && now.Sub(*lastAt) < time.Duration(progressThrottleMs)*time.Millisecond {
		return
	}
	*last = pct
	*lastAt = now
	p.Percent = pct
	p.Note = fmt.Sprintf("已传输 %s / %s", humanBytes(trans), humanBytesOrUnknown(total))
	emitProgress(*p)
}

// CopyParams Copy 的入参（字段多于两个时结构体比位置参数好读）。
type CopyParams struct {
	Src         Provider
	SrcID       string
	Dst         Provider
	DstParentID string
	Name        string // 目标文件名，空则沿用源 id 的 basename
	Size        int64  // 已知总字节，未知传 -1
	ProgressID  string // 归属的转存 id（进度事件与记录都挂它）
	NameHint    string // 展示名（源条目名），空则回退 Name
}

// Copy 把源文件转存到目标账号目录下，阻塞至完成 / ctx 取消 / 出错。
// 成功返回目标条目。ctx 同时用于取消：计数 reader 与调用方的 provider 请求都吃它。
func Copy(ctx context.Context, p CopyParams) (Entry, error) {
	if p.Src == nil || p.Dst == nil {
		return Entry{}, fmt.Errorf("源或目标 provider 为空")
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = baseOf(p.SrcID)
	}
	if name == "" {
		return Entry{}, fmt.Errorf("未指定目标文件名")
	}

	rc, size, err := p.Src.Open(ctx, p.SrcID)
	if err != nil {
		return Entry{}, fmt.Errorf("打开源文件失败: %w", err)
	}
	defer rc.Close()
	if size > 0 {
		p.Size = size
	}
	if p.Size < 0 {
		p.Size = -1
	}

	showName := strings.TrimSpace(p.NameHint)
	if showName == "" {
		showName = name
	}
	base := Progress{Op: OpTransfer, ID: p.ProgressID, Name: showName, Stage: StageRunning}
	pct, lastAt := -1, time.Time{}
	tracker := &countingReader{r: rc, ctx: ctx}
	tracker.report = func(trans int64) {
		trMu.Lock()
		if t, ok := transfers[p.ProgressID]; ok {
			t.Trans = trans
			t.UpdatedAt = time.Now().UnixMilli()
		}
		trMu.Unlock()
		reportProgress(&base, p.Size, trans, &pct, &lastAt, false)
	}

	e, err := p.Dst.Upload(ctx, p.DstParentID, name, tracker, p.Size)
	if err != nil {
		if ctx.Err() != nil || errors.Is(err, ErrCanceled) {
			return Entry{}, ErrCanceled
		}
		return Entry{}, err
	}
	final := base
	final.Stage = StageDone
	final.Percent = 100
	final.Note = fmt.Sprintf("转存完成（%s）", humanBytes(tracker.transferred))
	emitProgress(final)
	return e, nil
}

// StartTransfer 起一笔后台转存，立刻回传输 id（不阻塞绑定调用）。
// dirID 是源条目所在目录（用于取大小与显示名，取不到不阻断转存）。
func StartTransfer(srcAcct, srcID, srcDir, dstAcct, dstParent, name string) (Transfer, error) {
	src, err := New(srcAcct)
	if err != nil {
		return Transfer{}, err
	}
	dst, err := New(dstAcct)
	if err != nil {
		return Transfer{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = baseOf(srcID)
	}
	if name == "" {
		return Transfer{}, fmt.Errorf("未指定源文件名")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
	id := newTransferID()
	t := &Transfer{
		ID: id, Op: OpTransfer,
		SrcAcct: srcAcct, DstAcct: dstAcct,
		Name: name, Size: -1, Stage: StageRunning,
		Note: "排队中", CreatedAt: time.Now().UnixMilli(),
	}
	t.UpdatedAt = t.CreatedAt

	// 先把源条目大小问出来：进度百分比与「是否适合整段缓冲」都依赖它。
	if e, err := statEntry(ctx, src, srcDir, srcID); err == nil {
		t.Size = e.Size
		t.Name = pickName(e.Name, name)
		name = pickName(e.Name, name)
	} else {
		applog.Warnf("clouddrive", "转存取源条目信息失败（继续按未知大小传输）: %v", err)
	}

	trMu.Lock()
	recordLocked(t)
	trCancels[id] = cancel
	snap := t.snapshotLocked()
	trMu.Unlock()

	go runTransfer(ctx, cancel, id, src, dst, srcID, dstParent, name, snap.Size)
	return snap, nil
}

// runTransfer 后台执行一次转存，把终态写回记录并发进度事件。
func runTransfer(ctx context.Context, cancel context.CancelFunc, id string, src, dst Provider, srcID, dstParent, name string, size int64) {
	defer cancel()
	defer func() {
		// context 由本协程负责收尾，记录留到 ListTransfers 的裁剪里
		trMu.Lock()
		delete(trCancels, id)
		trMu.Unlock()
	}()

	setNote := func(stage, note string) {
		trMu.Lock()
		if t, ok := transfers[id]; ok {
			t.Stage, t.Note = stage, note
			t.UpdatedAt = time.Now().UnixMilli()
		}
		trMu.Unlock()
	}

	setNote(StageRunning, "传输中")
	emitProgress(Progress{Op: OpTransfer, ID: id, Name: name, Stage: StageRunning, Note: "开始转存"})

	e, err := Copy(ctx, CopyParams{
		Src: src, SrcID: srcID, Dst: dst, DstParentID: dstParent,
		Name: name, Size: size, ProgressID: id, NameHint: name,
	})
	switch {
	case err == nil:
		setNote(StageDone, "完成: "+pickName(e.Path, e.ID))
		trMu.Lock()
		if t, ok := transfers[id]; ok {
			if e.Size > 0 {
				t.Size = e.Size
				t.Trans = e.Size
			}
		}
		trMu.Unlock()
		applog.Infof("clouddrive", "跨盘转存完成 %s → %s", name, e.ID)
	case errors.Is(err, ErrCanceled), ctx.Err() != nil:
		setNote(StageCanceled, "已取消")
	default:
		setNote(StageError, err.Error())
		emitProgress(Progress{Op: OpTransfer, ID: id, Name: name, Stage: StageError, Note: clipNote(err.Error())})
		applog.Warnf("clouddrive", "跨盘转存失败 %s: %v", name, err)
	}
}

// pickName 优先用源条目名，为空时回退。
func pickName(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// baseOf 从各家 id 里取一个可作文件名的 basename（百度用路径作 id）。
func baseOf(id string) string {
	id = strings.ReplaceAll(strings.TrimSpace(id), "\\", "/")
	parts := strings.Split(id, "/")
	return parts[len(parts)-1]
}

// humanBytes 字节数可读化（进度提示与 Note 用，前端也有一份同款）。
func humanBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d %s", n, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

// humanBytesOrUnknown 总大小未知时如实回「未知」，不假报 0 B。
func humanBytesOrUnknown(n int64) string {
	if n <= 0 {
		return "未知"
	}
	return humanBytes(n)
}

// clipNote 把错误信息压到一行长度，避免 Note 撑爆任务中心一行。
func clipNote(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if r := []rune(s); len(r) > 120 {
		return string(r[:120]) + "…"
	}
	return s
}
