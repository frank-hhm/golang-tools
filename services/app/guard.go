package app

import (
	"fmt"
	"strings"

	"go-tools/services/config"
	"go-tools/tools/guard"
)

// 进程守护工具的绑定层：把 tools/guard 引擎的能力暴露给前端，状态变更经 guard:status 事件回推。

// GuardGetState 返回当前守护列表与运行态（含开机自启、重启参数）。
func (a *App) GuardGetState() guard.Snapshot { return guard.GetState() }

// GuardUpsert 新增或更新一条守护登记项（ID 为空即新增）。
func (a *App) GuardUpsert(e guard.Entry) error { return guard.Upsert(e) }

// GuardRemove 删除一条登记项（先停止其进程）。
func (a *App) GuardRemove(id string) error { return guard.Remove(id) }

// GuardClear 清空全部登记项（先停止全部进程）。
func (a *App) GuardClear() error { return guard.Clear() }

// GuardSetEnabled 切换某项「启用守护」。
func (a *App) GuardSetEnabled(id string, enabled bool) error { return guard.SetEnabled(id, enabled) }

// GuardStart 手动启动一个登记项。
func (a *App) GuardStart(id string) error { return guard.StartOne(id) }

// GuardStop 手动停止一个登记项（主动停止，不触发自动重启）。
func (a *App) GuardStop(id string) error { return guard.StopOne(id) }

// GuardStartAll 启用所有程序（拉起全部「启用守护」项）。
func (a *App) GuardStartAll() error { return guard.StartAll() }

// GuardStopAll 关闭所有程序（主动停止全部在跑/在守护项）。
func (a *App) GuardStopAll() error { guard.StopAll(); return nil }

// GuardSetSettings 更新重启策略参数：重启间隔（秒）与连续快速失败上限。
func (a *App) GuardSetSettings(delaySec, maxFastFail int) error {
	return guard.SetSettings(delaySec, maxFastFail)
}

// GuardSetAutoStart 切换开机自启：复用全局 General.AutoStart（写配置 + 同步 Wails Autostart 注册），
// 并推送一次状态让前端开关与设置页保持一致。
func (a *App) GuardSetAutoStart(enabled bool) error {
	if _, err := config.Update(func(cfg *config.AppConfig) {
		cfg.General.AutoStart = enabled
	}); err != nil {
		return err
	}
	a.applyAutoStart(enabled)
	guard.PushStatus()
	return nil
}

// GuardChooseProgram 弹原生文件选择框挑选要守护的程序，取消返回空串。
func (a *App) GuardChooseProgram(title string) (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	if strings.TrimSpace(title) == "" {
		title = "选择要守护的程序"
	}
	return dlgSingle(a.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		SetTitle(title).
		PromptForSingleSelection())
}
