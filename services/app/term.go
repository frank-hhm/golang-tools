package app

// ============================================================
// 终端连接管理：把 tools/term 暴露给前端（$GoTools.Term*）
//
// 记录 CRUD 走 TermListHosts / TermSaveHost / TermDeleteHost；
// 会话走 TermOpenSSH → TermWrite / TermResize / TermClose，
// 远端输出与断线经 term:data / term:exit 事件推送（见 main.go 的事件注册）。
//
// 底层 tools/term 是纯 Go（SSH / Docker exec）且已跨平台，所以这个文件
// 不再加 windows 约束：Linux 的 server 模式里终端是真实可用的功能，
// 不是桩。唯一的 Windows 味是 TermChooseKeyFile 的原生选文件框，
// server 模式下 Wails 的 Dialog 是空实现，前端应允许手输路径。
// ============================================================

import (
	"fmt"
	"Tools/tools/term"
)

// TermChooseKeyFile 弹原生文件选择框选 SSH 私钥，取消返回空串
func (a *App) TermChooseKeyFile() (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	path, err := a.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AllowsOtherFileTypes(true).
		SetTitle("选择 SSH 私钥").
		PromptForSingleSelection()
	if err != nil {
		if isDialogCanceled(err) {
			return "", nil
		}
		return "", err
	}
	return path, nil
}

// TermListHosts 终端连接记录列表（密码字段恒为空）
func (a *App) TermListHosts() ([]term.Host, error) {
	return term.ListHosts()
}

// TermSaveHost 新增/更新一条记录，返回更新后的完整列表
func (a *App) TermSaveHost(h term.Host) ([]term.Host, error) {
	return term.SaveHost(h)
}

// TermDeleteHost 删除记录，返回更新后的完整列表
func (a *App) TermDeleteHost(id string) ([]term.Host, error) {
	return term.DeleteHost(id)
}

// TermOpenSSH 按记录打开一条 SSH 终端会话，返回会话句柄
func (a *App) TermOpenSSH(hostID string, cols, rows int) (term.SessionInfo, error) {
	return term.OpenSSH(hostID, cols, rows)
}

// TermTestSSH 按表单参数测试 SSH 连通性（只握手不建会话），返回握手耗时毫秒
func (a *App) TermTestSSH(h term.Host) (int64, error) {
	return term.Test(h)
}

// TermOpenDocker 在容器内打开一条交互终端（复用同一套会话/事件通道）
func (a *App) TermOpenDocker(containerID, title string, cols, rows int) (term.SessionInfo, error) {
	return term.OpenDocker(containerID, title, cols, rows)
}

// TermWrite 终端按键输入（data 为 Base64）
func (a *App) TermWrite(sessionID string, data string) error {
	return term.Write(sessionID, data)
}

// TermResize 前端终端尺寸变化时同步远端 PTY
func (a *App) TermResize(sessionID string, cols, rows int) error {
	return term.Resize(sessionID, cols, rows)
}

// TermClose 主动关闭会话
func (a *App) TermClose(sessionID string) error {
	return term.Close(sessionID)
}
