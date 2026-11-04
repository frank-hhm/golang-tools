package app

import (
	"errors"
	"fmt"
	"strings"

	"Tools/services/applog"
	"Tools/services/config"
	"Tools/tools/lanshare"
)

// errNotRunning 服务未启动时的统一错误（供拼 URL 类绑定复用）。
var errNotRunning = errors.New("互传服务未运行，请先启动")

/*
局域网互传 / 分享链接（lanshare）绑定层

内核在 tools/lanshare：一个 HTTP 监听同时提供「收件箱上传」与「/s/<token>
临时分享链接」。文档中心与磁盘页的「分享」都落在这里——只登记链接，不搬文件。

不加 build tag：纯 net/http 实现，Linux 面板一样能跑（面板下这是独立端口、
不经 panel 登录墙，前端必须提示「该地址对局域网开放」）。
*/

// LanShareStatus 当前服务运行态（未运行也返回，Running=false）。
func (a *App) LanShareStatus() lanshare.Status {
	return lanshare.StatusNow()
}

// LanShareStart 启动服务：port<=0 用默认端口（占用自动 +1，最多试 20 个）；
// host 传 "127.0.0.1" 只供本机，空串监听全部网卡；autoStopMin>0 到点自停；
// pin 非空时下载/链接需带对 PIN（仅挡无意访问，不是访问控制）。
func (a *App) LanShareStart(port int, host string, autoStopMin int, pin string) (lanshare.Status, error) {
	st, err := lanshare.Start(port, host, autoStopMin, pin)
	if err != nil {
		applog.Warnf("lanshare", "启动失败: %v", err)
		return st, err
	}
	applog.Infof("lanshare", "服务启动: 端口=%d 地址=%v", st.Port, st.URLs)
	return st, nil
}

// LanShareStop 停止服务并释放端口。
func (a *App) LanShareStop() error {
	if err := lanshare.Stop(); err != nil {
		applog.Warnf("lanshare", "停止失败: %v", err)
		return err
	}
	applog.Infof("lanshare", "服务已停止")
	return nil
}

// LanShareCreateLink 为本机文件登记一条分享链接：hours<=0 不过期，
// maxDownloads<=0 不限次数。返回体含 token，URL 由前端用 LanShareStatus 拼。
func (a *App) LanShareCreateLink(path string, hours, maxDownloads int) (lanshare.Link, error) {
	l, err := lanshare.CreateLink(path, hours, maxDownloads)
	if err != nil {
		applog.Warnf("lanshare", "创建分享失败 %s: %v", path, err)
		return lanshare.Link{}, err
	}
	applog.Infof("lanshare", "已创建分享: %s（token=%s 小时=%d 次数=%d）", l.Name, l.Token, hours, maxDownloads)
	return l, nil
}

// LanShareListLinks 全部链接（含已失效项，前端标灰）。
func (a *App) LanShareListLinks() ([]lanshare.Link, error) {
	return lanshare.ListLinks()
}

// LanShareRevokeLink 撤销一条链接（只删登记，不动实体文件）。
func (a *App) LanShareRevokeLink(token string) error {
	if err := lanshare.RevokeLink(token); err != nil {
		applog.Warnf("lanshare", "撤销失败 token=%s: %v", token, err)
		return err
	}
	applog.Infof("lanshare", "已撤销分享: token=%s", token)
	return nil
}

// LanShareURL 拼一条可直接发送的完整链接（服务未运行时报错）。
func (a *App) LanShareURL(token string) (string, error) {
	return lanshare.ShareURL(token)
}

// LanShareInboxFiles 列出收件箱已收到的文件（名/大小/修改时间，倒序）。
func (a *App) LanShareInboxFiles() ([]lanshare.InboxFile, error) {
	return lanshare.ListInbox()
}

// LanShareUploadSessions 列出接收中的切片上传会话（可续传/进行中），
// 供互传页展示与强踢。服务停着时仍能列出磁盘上残留的半成品会话。
func (a *App) LanShareUploadSessions() ([]lanshare.UploadSession, error) {
	return lanshare.ListUploadSessions()
}

// LanShareAbortUpload 强踢一个卡死/占位的接收会话（删半成品目录，腾出会话名额）。
func (a *App) LanShareAbortUpload(sid string) error {
	if err := lanshare.AbortUpload(sid); err != nil {
		applog.Warnf("lanshare", "强踢上传会话失败 sid=%s: %v", sid, err)
		return err
	}
	applog.Infof("lanshare", "已强踢上传会话: sid=%s", sid)
	return nil
}

// LanShareAddGrant 授权一个本机路径供门户长期下载。目录可给 recursive
// （是否允许逐项浏览）；hours<=0 表示长期有效。
func (a *App) LanShareAddGrant(path string, recursive bool, hours int) (lanshare.Grant, error) {
	g, err := lanshare.AddGrant(path, recursive, hours)
	if err != nil {
		applog.Warnf("lanshare", "授权失败 %s: %v", path, err)
		return lanshare.Grant{}, err
	}
	applog.Infof("lanshare", "已授权下载: %s（id=%s 递归=%v）", g.Name, g.ID, recursive)
	return g, nil
}

// LanShareListGrants 全部授权（含已失效项，前端标灰）。
func (a *App) LanShareListGrants() ([]lanshare.Grant, error) {
	return lanshare.ListGrants()
}

// LanShareRevokeGrant 撤销一条授权（只删登记，不动实体）。
func (a *App) LanShareRevokeGrant(id string) error {
	if err := lanshare.RevokeGrant(id); err != nil {
		applog.Warnf("lanshare", "撤销授权失败 id=%s: %v", id, err)
		return err
	}
	applog.Infof("lanshare", "已撤销授权: id=%s", id)
	return nil
}

// LanShareGrantURL 拼一条可直接发送的授权下载 URL（服务未运行时报错）。
// 设了 PIN 时附 ?pin=，否则裸链。
func (a *App) LanShareGrantURL(id string) (string, error) {
	st := lanshare.StatusNow()
	if !st.Running {
		return "", errNotRunning
	}
	if len(st.URLs) == 0 {
		return "", fmt.Errorf("服务未绑定可访问地址")
	}
	u := st.URLs[0] + "/g/" + id
	if st.Pin != "" {
		u += "?pin=" + st.Pin
	}
	return u, nil
}

// LanShareChoosePath 弹原生文件/目录选择框（授权下载源），取消/不可用时返回空串。
// server 模式下 Wails Dialog 为空实现，前端应允许手输路径。
func (a *App) LanShareChoosePath(title string) (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	if strings.TrimSpace(title) == "" {
		title = "选择要授权下载的文件或目录"
	}
	return dlgSingle(a.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(true).
		SetTitle(title).
		PromptForSingleSelection())
}

// LanShareSettings 返回互传设置段（老配置无此段时返回默认值：空黑名单=全收、
// 启动默认 120 分钟后自停）。供互传页回填黑名单与启动控件、设置页分区回填。
func (a *App) LanShareSettings() config.LanShareConfig {
	return config.LanShareCfg()
}

// SaveLanShareSettings 保存互传设置（接收黑名单 + 启动默认值 port/host/autoStopMin/pin）。
// 空黑名单 = 全收。落盘后立即回灌单例（ApplyLanShareConfig），
// 对已在跑的服务即时生效，无需重启；启动默认值下次启动时生效。
func (a *App) SaveLanShareSettings(in config.LanShareConfig) error {
	_, err := config.Update(func(cfg *config.AppConfig) {
		if cfg.LanShare == nil {
			cfg.LanShare = &config.LanShareConfig{}
		}
		cfg.LanShare.BlockExt = in.BlockExt
		cfg.LanShare.Port = in.Port
		cfg.LanShare.Host = in.Host
		cfg.LanShare.AutoStopMin = in.AutoStopMin
		cfg.LanShare.Pin = in.Pin
	})
	if err != nil {
		applog.Errorf("lanshare", "保存接收设置失败: %v", err)
		return err
	}
	ApplyLanShareConfig()
	applog.Infof("lanshare", "已保存接收类型黑名单: %d 项", len(config.LanShareCfg().BlockExt))
	return nil
}

// ApplyLanShareConfig 把当前 config.LanShare 的接收黑名单回灌 lanshare 单例。
// main 启动期调一次完成初始化，SaveLanShareSettings 保存后再调。
func ApplyLanShareConfig() {
	lanshare.SetBlockExt(config.LanShareCfg().BlockExt)
}
