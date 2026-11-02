package docspace

// 备份自动调度（P4 顺延项，2026-11-02 补做）。
//
// 形态：全局只起一条 goroutine、一分钟一个轮询 tick——不按计划各建 ticker
// （几十个计划就是几十个 timer，改间隔还得逐个重建；轮询天然跟随
// IntervalS/Enabled/LastRunAt 的任意变化，实现最简单）。
// 进程重启后的积压处理是「错过即补」：LastRunAt 距今超过 IntervalS 就触发，
// 语义与持续在跑一致。
//
// 双闸门，任一关着都不动任何计划：
//   - 全局开关 config.DocSpaceConfig.BackupAuto（默认 false：老配置等价于关，
//     行为与纯手动期一致，升级不带任何隐藏副作用）；
//   - 计划位 Enabled；IntervalS<=0 视为「仅手动」，调度器永不碰。
//
// 触发复用 Store.RunBackup：在飞保护、backup:progress 事件（任务中心）、
// LastRunAt/LastStatus 回写全部沿用，调度器不另写一条备份路径。

import (
	"sync"
	"time"

	"Tools/services/config"
)

// schedPoll 轮询间隔。一分钟粒度对「按小时/天」的备份足够，且唤醒成本可忽略。
const schedPoll = time.Minute

var schedOnce sync.Once

// StartBackupScheduler 启动备份调度协程（幂等，main.go / main_server.go 接线）。
// 开关关着时协程也只是每分一次读缓存配置，不建索引、不 listen、不写盘。
func StartBackupScheduler() {
	schedOnce.Do(func() {
		go func() {
			tick := time.NewTicker(schedPoll)
			defer tick.Stop()
			for range tick.C {
				sweepDue(time.Now())
			}
		}()
	})
}

// sweepDue 扫一轮：对每个到点的计划调 RunBackup。
// 失败只跳过该计划（下一轮自然重试），不中断整轮扫描。
func sweepDue(now time.Time) {
	if !config.DocSpaceCfg().BackupAuto {
		return
	}
	s, err := Global()
	if err != nil {
		return
	}
	plans, err := s.ListBackups()
	if err != nil {
		return
	}
	for _, p := range plans {
		if !dueAt(p, now) {
			continue
		}
		_ = s.RunBackup(p.ID) // 在飞保护/计划缺失等错误此处忽略，下轮再判
	}
}

// dueAt 判定计划是否到点：纯函数，便于单测（不依赖配置与 Store）。
func dueAt(p BackupPlan, now time.Time) bool {
	if !p.Enabled || p.IntervalS <= 0 {
		return false
	}
	return now.UnixMilli()-p.LastRunAt >= int64(p.IntervalS)*1000
}
