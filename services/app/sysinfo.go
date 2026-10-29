package app

import (
	"Tools/services/sysinfo"
)

// GetSystemInfo 采集系统快照（绑定入口，实际采集在 services/sysinfo）
func (a *App) GetSystemInfo() (*sysinfo.SystemInfo, error) {
	return sysinfo.Snapshot(), nil
}

// activeBackgroundCount 统计当前在后台运行的任务数（受管服务实例 + 抓包代理），
// 供浮动球 Badge 判定：>0 显示角标，=0 隐藏。
func (a *App) activeBackgroundCount() int {
	n := 0
	for _, s := range sysinfo.ActiveServices() {
		if s.Running {
			n++
		}
	}
	a.snifferMu.Lock()
	if a.snifferRunning {
		n++
	}
	a.snifferMu.Unlock()
	return n
}
