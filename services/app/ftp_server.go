package app

// ============================================================
// 文件传输「服务端」绑定：把 tools/ftp 的服务端能力暴露给前端
//
// 与客户端绑定（ftp.go）分文件：客户端是"连出去"，服务端是"被人连进来"，
// 两块的状态与生命周期互不相干，混在一起容易看花。
// 服务端能力纯 Go 实现（x/crypto/ssh + pkg/sftp + os.Root），Linux 面板模式同样可用。
// ============================================================

import (
	"fmt"
	"strings"

	"go-tools/services/applog"
	"go-tools/tools/disk"
	"go-tools/tools/ftp"
)

/* ==================== 服务端状态与开关 ==================== */

// FtpServerStatus 服务端状态快照（含在线连接、局域网地址）
func (a *App) FtpServerStatus() ftp.ServerStatus {
	return ftp.GetServerStatus()
}

// FtpServerStart 启动 SFTP 服务端
func (a *App) FtpServerStart() (ftp.ServerStatus, error) {
	return ftp.StartServer()
}

// FtpServerStop 停止 SFTP 服务端（断开所有在线连接）
func (a *App) FtpServerStop() (ftp.ServerStatus, error) {
	if err := ftp.StopServer(); err != nil {
		return ftp.GetServerStatus(), err
	}
	return ftp.GetServerStatus(), nil
}

// FtpServerSaveConfig 保存监听配置；服务运行中会用新端口重启
func (a *App) FtpServerSaveConfig(cfg ftp.ServerConfig) (ftp.ServerStatus, error) {
	return ftp.UpdateServerConfig(cfg)
}

// FtpServerLogs 服务端日志（最近 300 条，旧在前）
func (a *App) FtpServerLogs() []ftp.ServerLog {
	return ftp.ListServerLogs()
}

/* ==================== 账号 ==================== */

// FtpListAccounts 虚拟账号列表（不含密码）
func (a *App) FtpListAccounts() ([]ftp.Account, error) {
	return ftp.ListAccounts()
}

// FtpSaveAccount 新增/更新虚拟账号；密码留空表示沿用原密码
func (a *App) FtpSaveAccount(acc ftp.Account) ([]ftp.Account, error) {
	list, id, err := ftp.SaveAccount(acc)
	if err != nil {
		return nil, err
	}
	applog.Infof("ftp", "账号 %s 已保存（ID %s）", acc.Username, id)
	if !acc.Enabled {
		// 禁用要立刻生效：只挡住下次登录的话，已经连着的会话还能继续读写
		if n := ftp.DisconnectUser(acc.Username); n > 0 {
			applog.Infof("ftp", "账号 %s 已禁用，断开 %d 条在线连接", acc.Username, n)
		}
	}
	return list, nil
}

// FtpDeleteAccount 删除虚拟账号（同时断开它的在线连接）
func (a *App) FtpDeleteAccount(id string) ([]ftp.Account, error) {
	return ftp.RemoveAccount(id)
}

/* ==================== 在线连接 ==================== */

// FtpServerConns 在线连接列表
func (a *App) FtpServerConns() []ftp.ServerConn {
	return ftp.ListServerConns()
}

// FtpKickServerConn 踢掉一条在线连接
func (a *App) FtpKickServerConn(id string) error {
	return ftp.KickServerConn(id)
}

/* ==================== 目录与防火墙 ==================== */

// FtpChooseServerRoot 选择服务端根目录；取消返回空串
func (a *App) FtpChooseServerRoot() (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	picked, err := a.app.Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		SetTitle("选择服务端根目录").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(picked), nil
}

// FtpOpenServerRoot 在文件管理器里打开服务端根目录（空则打开当前配置的根目录）
func (a *App) FtpOpenServerRoot(path string) error {
	target := strings.TrimSpace(path)
	if target == "" {
		cfg, err := ftp.LoadServerConfig()
		if err != nil {
			return err
		}
		root, err := ftp.ServerRoot(cfg)
		if err != nil {
			return err
		}
		target = root
	}
	return disk.RevealInExplorer(target)
}

// FtpFirewallAllow 在系统防火墙放行当前端口（Windows 会弹 UAC）
func (a *App) FtpFirewallAllow(port int) error {
	return ftp.AllowFirewall(port)
}

// FtpFirewallRemove 删除程序的防火墙放行规则
func (a *App) FtpFirewallRemove(port int) error {
	return ftp.RemoveFirewallRule(port)
}
