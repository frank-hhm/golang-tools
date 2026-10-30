package app

import (
	"Tools/tools/downloader"
)

/*
下载管理绑定层：把 tools/downloader 的引擎单例薄薄包一层供前端调用。

真正的分块 / 续传 / 限速 / 重试 / 持久化都在 tools/downloader，这里只转发，
不掺业务。全部方法跨平台（不加 build tag）：Linux 面板里下载的是服务器本机
SaveDir，语义与桌面一致（见 AGENTS.md「绑定方法不能藏在 build tag 后面」）。

进度不经返回值，而经注入的 emitter 发 "download:progress"（负载 downloader.Task）：
main / main_server 的 init 里 RegisterEvent，emit 顺带进 services/tasks 任务中心，
于是离开页面也看得到进度、终态还能弹通知。
*/

// DownloadAdd 新建下载任务。name/saveDir 留空走默认（从 URL 推名、落数据目录/downloads）。
// autoStart=true 立即排队开跑；scheduledAt>0（unix 毫秒）到点自动开始。
func (a *App) DownloadAdd(url, name, saveDir string, connections int, proxy string, mirrors []string, scheduledAt int64, autoStart bool) (*downloader.Task, error) {
	return downloader.Instance().Add(url, downloader.AddOpts{
		Name:        name,
		SaveDir:     saveDir,
		Connections: connections,
		Proxy:       proxy,
		Mirrors:     mirrors,
		ScheduledAt: scheduledAt,
		AutoStart:   autoStart,
	})
}

// DownloadList 全部任务快照（按加入顺序）。
func (a *App) DownloadList() []downloader.Task {
	return downloader.Instance().List()
}

// DownloadStart 立即开始 / DownloadResume 续传 / DownloadRetry 重试：
// 三者对引擎而言都是"重新入队"，区别只在语义与前端按钮。
func (a *App) DownloadStart(id string) error  { return downloader.Instance().Start(id) }
func (a *App) DownloadResume(id string) error { return downloader.Instance().Resume(id) }
func (a *App) DownloadRetry(id string) error  { return downloader.Instance().Retry(id) }

// DownloadPause 暂停（保留断点）；DownloadStop 停止（清理临时文件）。
func (a *App) DownloadPause(id string) error { return downloader.Instance().Pause(id) }
func (a *App) DownloadStop(id string) error  { return downloader.Instance().Stop(id) }

// DownloadDelete 移除任务并删除其文件与续传 meta。
func (a *App) DownloadDelete(id string) error { return downloader.Instance().Delete(id) }

// DownloadSetConnections 改单任务分块数（下次下载生效）。
func (a *App) DownloadSetConnections(id string, n int) error {
	return downloader.Instance().SetConnections(id, n)
}

// DownloadSetSpeed 限速（bps）：id 非空改单任务，id 为空改全局。
func (a *App) DownloadSetSpeed(id string, bps int64) error {
	if id == "" {
		downloader.Instance().SetGlobalLimit(bps)
		return nil
	}
	return downloader.Instance().SetSpeedLimit(id, bps)
}

// DownloadSetProxy 设单任务代理（http/https，下次下载生效）。
func (a *App) DownloadSetProxy(id, proxy string) error {
	return downloader.Instance().SetProxy(id, proxy)
}

// DownloadSelectSource 手动选定实际使用的镜像源（本期唯一的选源方式）。
func (a *App) DownloadSelectSource(id, url string) error {
	return downloader.Instance().SelectSource(id, url)
}

// DownloadProbeSources 并发探测多镜像，返回各源延迟/吞吐/可用性（供手动选）。
func (a *App) DownloadProbeSources(mirrors []string) []downloader.SourceStat {
	return downloader.ProbeSources(mirrors)
}

// DownloadProbeProxies 经各代理访问 testURL 测延迟/吞吐/可用性。
func (a *App) DownloadProbeProxies(list []string, testURL string) []downloader.ProxyStat {
	return downloader.ProbeProxies(list, testURL)
}

// DownloadGlobalStats 顶栏统计：合计下行速度 + 各相位计数 + 全局限速。
func (a *App) DownloadGlobalStats() downloader.Stats {
	return downloader.Instance().GlobalStats()
}

// DownloadDefaultDir 当前生效的全局默认下载目录（新建任务 saveDir 留空即落这里）。
// 取全局目录配置里的「下载目录」，未配置则 <应用目录>/downloads。
func (a *App) DownloadDefaultDir() string {
	return downloader.DefaultSaveDir()
}

// DownloadSetGlobalLimit 改全局限速（bps，0=解除），即时作用于所有在跑任务。
func (a *App) DownloadSetGlobalLimit(bps int64) {
	downloader.Instance().SetGlobalLimit(bps)
}
