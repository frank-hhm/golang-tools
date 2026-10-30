package downloader

import (
	"context"
	"fmt"
	"time"
)

// ============================================================
// Fetch：把「下载一个文件」这件事交给引擎跑，同时阻塞等它到终态。
//
// 为什么要有它：mysql / php / redis / nginx / npm 的版本包、php 扩展包、docker 安装器，
// 过去各自写了一份单流 http 下载循环（读 body → 写文件 → 按 5% 步进发进度事件）。
// 现在统一改走本引擎下载，好处是这些下载天然出现在「下载管理」页，能暂停 / 续传 /
// 限速 / 排队；工具侧只需在 onProgress 里把百分比桥接回自己的 install 事件，页面零改动。
//
// 实现取舍：用轮询 m.Get(id) 而非订阅事件通道——引擎的进度事件是「瞬时广播」，
// 通道满了会丢事件，丢掉终态那一条就会把调用方永久挂住；轮询读实时状态最稳，
// 且 onProgress 只在百分比变化时回调，事件量与原「5% 步进」同量级。
// ============================================================

// FetchOpts 阻塞式下载的参数。零值走引擎默认（名从 URL 推、落默认下载目录、按扩展名归类）。
type FetchOpts struct {
	Name        string // 目标文件名（落盘名 = SaveDir/Name）
	SaveDir     string // 落盘目录（空则用引擎默认下载目录）
	Category    string // 分类；安装类下载传 "安装"
	Connections int    // 分块数（0 走引擎默认）
	Proxy       string
}

// Fetch 包级入口：用进程单例下载 url，阻塞至终态。
// 成功返回落盘绝对路径与任务 id；失败 / 取消返回 error（任务 id 仍回传，便于调用方清理）。
// ctx 取消 = 停止该下载任务（引擎置 canceled，保留断点）。onProgress 可为 nil。
func Fetch(ctx context.Context, url string, opts FetchOpts, onProgress func(pct int, downloaded, size int64, speed float64)) (savePath, taskID string, err error) {
	return Instance().Fetch(ctx, url, opts, onProgress)
}

// Fetch 实例级实现，语义同包级 Fetch。
func (m *Manager) Fetch(ctx context.Context, url string, opts FetchOpts, onProgress func(pct int, downloaded, size int64, speed float64)) (savePath, taskID string, err error) {
	t, err := m.Add(url, AddOpts{
		Name:        opts.Name,
		SaveDir:     opts.SaveDir,
		Category:    opts.Category,
		Connections: opts.Connections,
		Proxy:       opts.Proxy,
		AutoStart:   true,
	})
	if err != nil {
		return "", "", err
	}
	id := t.ID

	tick := time.NewTicker(400 * time.Millisecond)
	defer tick.Stop()
	lastPct := -1
	for {
		cur, ok := m.Get(id)
		if !ok {
			return "", id, fmt.Errorf("下载任务已不存在")
		}
		terminal := cur.Status == StatusDone || cur.Status == StatusError || cur.Status == StatusCanceled
		// paused 不算终态：用户在下载页按了暂停，回头还能继续，安装不该被提前判死；
		// 真放弃走「停止」（canceled）或删除任务（快照消失），Fetch 才会收手报错。
		if onProgress != nil && (cur.Percent != lastPct || terminal) {
			onProgress(cur.Percent, cur.Downloaded, cur.Size, cur.Speed)
			lastPct = cur.Percent
		}
		switch cur.Status {
		case StatusDone:
			return cur.SavePath, id, nil
		case StatusError:
			msg := cur.Err
			if msg == "" {
				msg = "下载失败"
			}
			return "", id, fmt.Errorf("%s", msg)
		case StatusCanceled:
			return "", id, fmt.Errorf("下载已取消")
		}
		select {
		case <-ctx.Done():
			_ = m.Stop(id)
			return "", id, ctx.Err()
		case <-tick.C:
		}
	}
}

// Get 读单条任务实时快照（供 Fetch 轮询与其它内部调用方用）。
func (m *Manager) Get(id string) (Task, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t := m.tasks[id]
	if t == nil {
		return Task{}, false
	}
	return cloneTask(t), true
}
