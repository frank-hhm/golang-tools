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
	"Tools/tools/ftp"
	"Tools/tools/term"
	"fmt"
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

// resolveFtpConn 凭据归一（P2 收尾）：记录关联了 term 共享凭据时，把凭据的
// 账号/认证方式/私钥/口令叠加进连接视图——tools 包保持互不依赖，归一只在这一层做。
// 明文口令只在返回值内存里短暂存在，调用方不得落盘。
func resolveFtpConn(c ftp.Connection, pass string) (ftp.Connection, string) {
	if c.CredID == "" {
		return c, pass
	}
	sp, err := term.ResolveCredential(c.CredID)
	if err != nil {
		return c, pass
	}
	if sp.Auth != "" {
		c.Auth = sp.Auth
	}
	if sp.User != "" {
		c.User = sp.User
	}
	if sp.KeyPath != "" {
		c.KeyPath = sp.KeyPath
	}
	c.Passphrase = sp.Passphrase
	if sp.Password != "" {
		pass = sp.Password
	}
	return c, pass
}

// FtpOpenSession 按记录建立 SFTP 会话，返回会话句柄
func (a *App) FtpOpenSession(connID string) (ftp.SessionInfo, error) {
	c, pass, err := ftp.ResolveStored(connID)
	if err != nil {
		return ftp.SessionInfo{}, err
	}
	c, pass = resolveFtpConn(c, pass)
	return ftp.OpenSessionResolved(c, pass)
}

// FtpTestConnection 按表单参数测试 SFTP 连通性（只握手不开子系统），返回握手耗时毫秒
func (a *App) FtpTestConnection(c ftp.Connection) (int64, error) {
	if c.CredID != "" {
		c, _ = resolveFtpConn(c, c.Password)
	}
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

/* ==================== FTP ⇄ 终端互通（P2 收尾：凭据归一 + 互跳） ==================== */

// FtpOpenInTerminal 把一条 SFTP 连接导入为终端主机并返回主机 id（前端随后开
// /tools/term?host=<id> 自动连接）。已关联共享凭据的只引用同一 credID——
// 一处改密两端生效；内联密码/私钥的升格为一条共享凭据。
func (a *App) FtpOpenInTerminal(connID string) (string, error) {
	c, savedPass, err := ftp.ResolveStored(connID)
	if err != nil {
		return "", err
	}
	h := term.Host{Name: c.Name, Address: c.Host, Port: c.Port, User: c.User, CredID: c.CredID,
		Note: "由文件传输导入：" + c.Name}
	switch {
	case c.CredID != "":
		h.Auth = term.AuthPassword
		if sp, e := term.ResolveCredential(c.CredID); e == nil && sp.Auth != "" {
			h.Auth = sp.Auth
		}
	case c.Auth == ftp.AuthKey && c.KeyPath != "":
		cl, serr := term.SaveCredential(term.Credential{
			Name: c.Name + " 凭据", Auth: term.AuthKey, User: c.User, KeyPath: c.KeyPath,
		})
		if serr != nil {
			return "", serr
		}
		for _, x := range cl {
			if x.Name == c.Name+" 凭据" {
				h.CredID = x.ID
				break
			}
		}
		h.Auth = term.AuthKey
	default:
		// 密码取不到明文（未存或 DPAPI 解不开）时仍建主机，连接时终端页会提示补填
		if savedPass != "" {
			cl, serr := term.SaveCredential(term.Credential{
				Name: c.Name + " 凭据", Auth: term.AuthPassword, User: c.User, Password: savedPass,
			})
			if serr != nil {
				return "", serr
			}
			for _, x := range cl {
				if x.Name == c.Name+" 凭据" {
					h.CredID = x.ID
					break
				}
			}
		}
		h.Auth = term.AuthPassword
	}
	list, herr := term.SaveHost(h)
	if herr != nil {
		return "", herr
	}
	for _, x := range list {
		if x.Name == h.Name && x.Address == h.Address {
			return x.ID, nil
		}
	}
	return "", fmt.Errorf("导入终端主机失败：未在列表中找到对应记录")
}

// TermOpenInSFTP 把一条终端主机导入为 SFTP 连接并返回连接 id（前端随后开
// /tools/ftp?conn=<id>）。共享凭据直接引用；旧式内联密码/私钥升格为凭据。
func (a *App) TermOpenInSFTP(hostID string) (string, error) {
	h, spec, err := term.ResolveHostSpec(hostID)
	if err != nil {
		return "", err
	}
	c := ftp.Connection{ID: "", Name: h.Name, Proto: ftp.ProtoSFTP, Host: h.Address, Port: h.Port,
		User: h.User, CredID: h.CredID, Note: "由终端导入：" + h.Name}
	if c.Port <= 0 {
		c.Port = 22
	}
	if spec.User != "" {
		c.User = spec.User
	}
	if h.CredID == "" && (spec.Password != "" || spec.KeyPath != "") {
		// 未关联凭据的旧式主机：把解析出的密码/私钥升格为共享凭据，两套记录引用同一 credID
		cred := term.Credential{Name: h.Name + " 凭据", User: c.User}
		if spec.KeyPath != "" {
			cred.Auth = term.AuthKey
			cred.KeyPath = spec.KeyPath
			cred.Passphrase = spec.Passphrase
		} else {
			cred.Auth = term.AuthPassword
			cred.Password = spec.Password
		}
		cl, serr := term.SaveCredential(cred)
		if serr != nil {
			return "", serr
		}
		for _, x := range cl {
			if x.Name == h.Name+" 凭据" {
				c.CredID = x.ID
				break
			}
		}
	}
	list, serr := ftp.SaveConnection(c)
	if serr != nil {
		return "", serr
	}
	for _, x := range list {
		if x.Name == c.Name && x.Host == c.Host {
			return x.ID, nil
		}
	}
	return "", fmt.Errorf("导入 SFTP 连接失败：未在列表中找到对应记录")
}
