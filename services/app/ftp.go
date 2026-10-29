package app

// ============================================================
// 文件传输（Xftp 式）：把 tools/ftp 暴露给前端（$GoTools.Ftp*）
//
// 连接记录 CRUD 走 FtpListConnections / FtpSaveConnection / FtpDeleteConnection；
// 会话与文件操作走 FtpOpenSession → FtpListDir / FtpUpload / FtpDownload …
// 传输进度与断线经 ftp:transfer / ftp:session 事件推送（见 main.go 的事件注册）。
//
// 底层 tools/ftp 是纯 Go（SFTP 走 x/crypto/ssh + pkg/sftp），这个文件不加
// windows 约束：Linux 的 server 模式里文件传输同样是真实功能。
// 只有选本地文件/目录的对话框是 Windows 味，面板模式允许前端手输路径。
// ============================================================

import (
	"fmt"
	"go-tools/tools/ftp"
	"strings"
)

/* ==================== 连接记录 ==================== */

// FtpListConnections 连接记录列表（密码字段恒为空）
func (a *App) FtpListConnections() ([]ftp.Connection, error) {
	return ftp.ListConnections()
}

// FtpSaveConnection 新增/更新一条记录，返回更新后的完整列表
func (a *App) FtpSaveConnection(c ftp.Connection) ([]ftp.Connection, error) {
	return ftp.SaveConnection(c)
}

// FtpDeleteConnection 删除记录，返回更新后的完整列表
func (a *App) FtpDeleteConnection(id string) ([]ftp.Connection, error) {
	return ftp.DeleteConnection(id)
}

/* ==================== 会话与文件操作 ==================== */

// FtpOpenSession 按记录建立 SFTP 会话，返回会话句柄
func (a *App) FtpOpenSession(connID string) (ftp.SessionInfo, error) {
	return ftp.OpenSession(connID)
}

// FtpTestConnection 按表单参数测试 SFTP 连通性（只握手不开子系统），返回握手耗时毫秒
func (a *App) FtpTestConnection(c ftp.Connection) (int64, error) {
	return ftp.Test(c)
}

// FtpCloseSession 主动关闭会话
func (a *App) FtpCloseSession(sessionID string) error {
	return ftp.CloseSession(sessionID)
}

// FtpListDir 浏览远端目录
func (a *App) FtpListDir(sessionID, dir string) (ftp.Listing, error) {
	return ftp.ListDir(sessionID, dir)
}

// FtpMkdir 在远端目录下新建一层目录
func (a *App) FtpMkdir(sessionID, dir, name string) error {
	return ftp.Mkdir(sessionID, dir, name)
}

// FtpRename 重命名远端文件/目录
func (a *App) FtpRename(sessionID, target, newName string) error {
	return ftp.Rename(sessionID, target, newName)
}

// FtpRemove 删除远端文件或空目录
func (a *App) FtpRemove(sessionID string, targets []string) (int, error) {
	return ftp.Remove(sessionID, targets)
}

// FtpPreview 预览远端文件（文本/图片）
func (a *App) FtpPreview(sessionID, path string) (ftp.Preview, error) {
	return ftp.PreviewFile(sessionID, path)
}

/* ==================== 传输队列 ==================== */

// FtpUpload 上传本地文件/目录到远端目录
func (a *App) FtpUpload(sessionID string, localPaths []string, remoteDir string) ([]ftp.Transfer, error) {
	return ftp.Upload(sessionID, localPaths, remoteDir)
}

// FtpDownload 下载远端文件/目录到本地目录
func (a *App) FtpDownload(sessionID string, remotePaths []string, localDir string) ([]ftp.Transfer, error) {
	return ftp.Download(sessionID, remotePaths, localDir)
}

// FtpCancelTransfer 取消一个传输任务
func (a *App) FtpCancelTransfer(id string) error {
	return ftp.CancelTransfer(id)
}

// FtpRetryTransfer 重试一个失败/取消的任务
func (a *App) FtpRetryTransfer(id string) (ftp.Transfer, error) {
	return ftp.RetryTransfer(id)
}

// FtpListTransfers 当前传输任务列表（新的在前）
func (a *App) FtpListTransfers() []ftp.Transfer {
	return ftp.ListTransfers()
}

/* ==================== 本地选择框 ==================== */

// FtpChooseKeyFile 选 SSH 私钥文件，取消返回空串
func (a *App) FtpChooseKeyFile() (string, error) {
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

// FtpChooseLocalFiles 选本机要上传的文件（可多选）
func (a *App) FtpChooseLocalFiles() ([]string, error) {
	if a.app == nil {
		return nil, fmt.Errorf("应用尚未初始化")
	}
	picked, err := a.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		SetTitle("选择要上传的文件").
		PromptForMultipleSelection()
	if err != nil {
		if isDialogCanceled(err) {
			return nil, nil
		}
		return nil, err
	}
	return picked, nil
}

// FtpChooseLocalFolder 选本机目录（上传整棵目录 / 下载目标目录）
func (a *App) FtpChooseLocalFolder(title string) (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	if strings.TrimSpace(title) == "" {
		title = "选择本地目录"
	}
	picked, err := a.app.Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		SetTitle(title).
		PromptForSingleSelection()
	if err != nil {
		if isDialogCanceled(err) {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(picked), nil
}
