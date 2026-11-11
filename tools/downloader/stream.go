package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// runStreamTask 外部流源任务的编排：解析源 → 单连接搬运 → 收尾。与 runTask 平级，
// 唯一区别是字节来自注册的 SourceResolver 而非 HTTP Range。取消 / 停止 / 错误都交给
// finish 统一落终态（runner.kind 已区分 pause / stop）。
func (m *Manager) runStreamTask(t Task, r *runner, ctx context.Context) {
	err := downloadStream(streamParams{
		ctx:      ctx,
		kind:     t.SourceKind,
		params:   t.SourceParams,
		savePath: t.SavePath,
		tb:       r.tb,
		maxRetry: defaultMaxRetry,
		onProgress: func(size, dl int64) {
			m.updateProgress(t.ID, r, size, dl, nil)
		},
	})
	m.finish(t.ID, r, err)
}

// streamParams downloadStream 的入参（对齐 dlParams，但源是 resolver 不是 url）。
type streamParams struct {
	ctx        context.Context
	kind       string
	params     map[string]string
	savePath   string
	tb         *tokenBucket
	maxRetry   int
	onProgress func(size, downloaded int64)
}

// downloadStream 从注册的源解析器取流，单连接写 <savePath>.part 再 rename 落回。
//
// 流不支持 Range，所以每次尝试（含重试与重启后的续传）都从头整段重下——这是刻意如实的
// 取舍：半截续传在偏移对不上时会写出坏文件，宁可重下也不赌。大文件边下边写 .part，
// 不整段进内存。
func downloadStream(p streamParams) error {
	if p.maxRetry <= 0 {
		p.maxRetry = defaultMaxRetry
	}
	resolver, ok := lookupSource(p.kind)
	if !ok {
		return fmt.Errorf("未注册的下载源类型：%q", p.kind)
	}
	tmp := p.savePath + partSuffix
	var lastErr error
	for attempt := 0; attempt <= p.maxRetry; attempt++ {
		if p.ctx.Err() != nil {
			return p.ctx.Err()
		}
		if attempt > 0 && !sleepBackoff(p.ctx, attempt) {
			return p.ctx.Err()
		}
		written, err := streamCopyOnce(p, resolver, tmp)
		if err == nil {
			return renamePart(tmp, p.savePath)
		}
		if isFatal(err) || errors.Is(err, context.Canceled) {
			return err
		}
		// 一个字节都没拿到就失败：多半是账号不存在 / 无权限 / 配置类硬失败，
		// 退避重试只是空转，直接透出真实原因。
		if written == 0 {
			return err
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("下载失败")
	}
	return lastErr
}

// streamCopyOnce 取一次流写到 tmp（截断重来），返回已写字节数供上层判断「是否真开始过」。
func streamCopyOnce(p streamParams, resolver SourceResolver, tmp string) (int64, error) {
	rc, size, err := resolver.Open(p.ctx, p.params)
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return 0, err
	}
	var written int64
	w := &offsetWriter{f: f, off: 0, tb: p.tb, ctx: p.ctx}
	w.onWrite = func(n int64) {
		written += n
		if p.onProgress != nil {
			p.onProgress(size, written)
		}
	}
	if size > 0 {
		_, err = io.Copy(w, io.LimitReader(rc, size))
	} else {
		_, err = io.Copy(w, rc)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return written, err
	}
	return written, nil
}

// renamePart 把 .part 落成正式文件；Windows 上 rename 不覆盖已存在目标，先删再 rename。
func renamePart(tmp, dst string) error {
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(dst)
		if err2 := os.Rename(tmp, dst); err2 != nil {
			return err2
		}
	}
	return nil
}
