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
	// 保存前后各取一次列表：账号策略一变就要让在线会话失效，
	// 只靠「下次登录生效」等于权限收回无效（老连接仍然按旧策略读写）
	before, _ := ftp.ListAccounts()
	list, id, err := ftp.SaveAccount(acc)
	if err != nil {
		return nil, err
	}
	applog.Infof("ftp", "账号 %s 已保存（ID %s）", acc.Username, id)
	for _, name := range staleUsernames(before, list, acc) {
		if n := ftp.DisconnectUser(name); n > 0 {
			applog.Infof("ftp", "账号 %s 的策略已变更，断开 %d 条在线连接", name, n)
		}
	}
	return list, nil
}

// staleUsernames 保存账号后需要立刻断开的用户名（改名时新旧名字都要断）。
//
// 禁用、改为只读、改根目录、改名、改密码都属于权限或凭据变化，必须断开；
// 反过来把只读放宽成读写、或只改备注，老会话继续用不影响安全，不踢人；
// 新建账号本来就没有会话。
func staleUsernames(before, after []ftp.Account, in ftp.Account) []string {
	old := pickAccount(before, in.ID, in.Username)
	now := pickAccount(after, in.ID, in.Username)
	if old == nil || now == nil {
		return nil
	}
	drop := !now.Enabled || now.ReadOnly || in.Password != "" ||
		old.RootDir != now.RootDir || !strings.EqualFold(old.Username, now.Username)
	if !drop {
		return nil
	}
	names := []string{now.Username}
	if !strings.EqualFold(old.Username, now.Username) {
		names = append(names, old.Username)
	}
	return names
}

// pickAccount 优先按 ID 匹配（改用户名时也能找回旧记录），否则按用户名忽略大小写
func pickAccount(list []ftp.Account, id, username string) *ftp.Account {
	for i := range list {
		if id != "" && list[i].ID == id {
			return &list[i]
		}
	}
	for i := range list {
		if strings.EqualFold(list[i].Username, username) {
			return &list[i]
		}
	}
	return nil
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
