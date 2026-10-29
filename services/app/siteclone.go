package app

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"go-tools/services/applog"
	"go-tools/tools/siteclone"
)

// 仿站同一时刻只允许一个任务：cancel 存在包级，起 goroutine 跑 siteclone.Run，
// 进度经 siteclone:progress 事件推给前端。用包级变量而非 App 字段，省得动 App 结构体。
var (
	sitecloneMu     sync.Mutex
	sitecloneCancel context.CancelFunc
)

// SiteCloneStart 启动一次仿站抓取，立即返回 jobId；结束/取消前重复调用会被拒。
func (a *App) SiteCloneStart(opts siteclone.Options) (string, error) {
	if strings.TrimSpace(opts.URL) == "" {
		return "", fmt.Errorf("请先填写要抓取的 URL")
	}
	if strings.TrimSpace(opts.OutDir) == "" {
		return "", fmt.Errorf("请先选择输出目录")
	}

	sitecloneMu.Lock()
	if sitecloneCancel != nil {
		sitecloneMu.Unlock()
		return "", fmt.Errorf("已有仿站任务在进行中，请先取消")
	}
	ctx, cancel := context.WithCancel(context.Background())
	sitecloneCancel = cancel
	sitecloneMu.Unlock()

	jobID := fmt.Sprintf("sc%d", time.Now().UnixNano())
	applog.Infof("siteclone", "开始仿站: %s mode=%s out=%s", opts.URL, opts.Mode, opts.OutDir)
	go func() {
		defer func() {
			sitecloneMu.Lock()
			sitecloneCancel = nil
			sitecloneMu.Unlock()
		}()
		res, err := siteclone.Run(ctx, opts, jobID, func(p siteclone.Progress) {
			a.emitSiteClone(p)
		})
		if err != nil {
			applog.Errorf("siteclone", "仿站失败: %v", err)
			a.emitSiteClone(siteclone.Progress{JobID: jobID, Stage: "error", Status: "fail", Msg: err.Error()})
			return
		}
		applog.Infof("siteclone", "仿站结束: 页数=%d 文件=%d 字节=%d 根=%s", res.Pages, res.Files, res.Bytes, res.Root)
	}()
	return jobID, nil
}

// SiteCloneCancel 取消当前仿站任务；无任务时静默返回。
func (a *App) SiteCloneCancel() error {
	sitecloneMu.Lock()
	defer sitecloneMu.Unlock()
	if sitecloneCancel != nil {
		sitecloneCancel()
	}
	return nil
}

// emitSiteClone 把进度事件推给前端（app 未就绪时忽略）。
func (a *App) emitSiteClone(p siteclone.Progress) {
	if a.app != nil {
		a.app.Event.Emit("siteclone:progress", p)
	}
}
