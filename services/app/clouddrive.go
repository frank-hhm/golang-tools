package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"Tools/appdirs"
	"Tools/services/applog"
	"Tools/services/config"
	"Tools/tools/clouddrive"
)

/*
云盘管理（clouddrive）绑定层

内核在 tools/clouddrive：Provider 抽象 + registry + 账号表（token 走 tools/secret
加密）+ Google Drive v3 适配器。计划-云盘管理工具 PR1 只落地 Google，阿里/百度是后续。

不加 build tag：纯 net/http + os，Linux 面板构建同样存在（AGENTS「绑定方法不能藏在
build tag 后面」）。OAuth 走「粘贴回跳 URL/授权码」的手工流程，桌面与无头一套代码。
敏感 token 只在 Go 侧解密使用，绑定返回值一律是 AccountView（不含密文）。
*/

// cloudEnabled 云盘总开关（config.Cloud.Enabled）。关掉时拒绝一切云操作，
// 不凭空产生网络行为。
func cloudEnabled() bool { return config.CloudCfg().Enabled }

// withCloudCtx 给单次云操作套一个上限超时，避免前端被挂死的请求卡住。
func withCloudCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Minute)
}

// CloudListAccounts 全部云盘账号（不含任何密文）。
func (a *App) CloudListAccounts() ([]clouddrive.AccountView, error) {
	return clouddrive.ListAccounts()
}

// CloudAddAccount 新增一个未授权的账号（kind 目前只支持 google）。
func (a *App) CloudAddAccount(kind, label string) (clouddrive.AccountView, error) {
	v, err := clouddrive.AddAccount(kind, label)
	if err != nil {
		applog.Warnf("clouddrive", "添加账号失败 kind=%s: %v", kind, err)
	}
	return v, err
}

// CloudAddObjectStore 一步创建对象存储账号（阿里 OSS / 腾讯 COS / 通用 S3）：这类
// 账号没有 OAuth 授权码流程，创建时 bucket/endpoint/region + AccessKey 一次填齐。
// bucket/endpoint/region 属非敏感配置（回前端可显示）；akID/akSecret 只进加密账号表
// （tools/secret），绝不回前端，改密钥只能重填。endpoint/region 可留空（按 kind 推导默认域名）。
func (a *App) CloudAddObjectStore(kind, label, bucket, endpoint, region, akID, akSecret string) (clouddrive.AccountView, error) {
	if !cloudEnabled() {
		return clouddrive.AccountView{}, fmt.Errorf("云盘功能未启用，请到设置 → 云盘管理打开")
	}
	v, err := clouddrive.AddObjectStore(kind, label, bucket, endpoint, region, akID, akSecret)
	if err != nil {
		applog.Warnf("clouddrive", "添加对象存储账号失败 kind=%s bucket=%s: %v", kind, bucket, err)
		return clouddrive.AccountView{}, err
	}
	applog.Infof("clouddrive", "已添加对象存储账号 %s（kind=%s bucket=%s）", v.ID, v.Kind, v.Bucket)
	return v, nil
}

// CloudRemoveAccount 删除账号（连带 token；不动云端实体）。
func (a *App) CloudRemoveAccount(id string) error {
	if err := clouddrive.RemoveAccount(id); err != nil {
		applog.Warnf("clouddrive", "删除账号失败 id=%s: %v", id, err)
		return err
	}
	applog.Infof("clouddrive", "已删除账号 id=%s", id)
	return nil
}

// CloudRenameAccount 修改账号显示名。
func (a *App) CloudRenameAccount(id, label string) error {
	return clouddrive.RenameAccount(id, label)
}

// CloudClientReady 用户是否已在设置里填好 Google OAuth 客户端凭证。
func (a *App) CloudClientReady() bool {
	return clouddrive.ClientReady()
}

// CloudChooseFile 弹原生文件选择框（上传源），取消/不可用时返回空串。
// server 模式下 Wails Dialog 为空实现，前端应允许手输路径。
func (a *App) CloudChooseFile(title string) (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	if strings.TrimSpace(title) == "" {
		title = "选择本地文件"
	}
	return dlgSingle(a.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AddFilter("所有文件 (*.*)", "*.*").
		SetTitle(title).
		PromptForSingleSelection())
}

// CloudAuthURL 生成 Google 授权 URL，供用户点开授权（需先启用云盘并配好客户端凭证）。
func (a *App) CloudAuthURL(acctID string) (string, error) {
	if !cloudEnabled() {
		return "", fmt.Errorf("云盘功能未启用，请到设置 → 云盘管理打开")
	}
	u, err := clouddrive.AuthURL(acctID, acctID)
	if err != nil {
		return "", err
	}
	return u, nil
}

// CloudExchange 用授权后回跳的整串 URL（或裸授权码）为账号换取并保存 token。
func (a *App) CloudExchange(acctID, codeOrRedirect string) error {
	if !cloudEnabled() {
		return fmt.Errorf("云盘功能未启用，请到设置 → 云盘管理打开")
	}
	ctx, cancel := withCloudCtx()
	defer cancel()
	if err := clouddrive.ExchangeCode(ctx, acctID, codeOrRedirect); err != nil {
		applog.Warnf("clouddrive", "授权换 token 失败 id=%s: %v", acctID, err)
		return err
	}
	applog.Infof("clouddrive", "账号授权成功 id=%s", acctID)
	return nil
}

// CloudSetCredential 为「粘贴式」长期凭证的云盘写入凭据：阿里 refresh_token /
// 百度 BDUSS。google 不走这里（它用 CloudExchange 换 token）。value 只进加密账号表，
// 永不回前端。
func (a *App) CloudSetCredential(acctID, value string) error {
	if !cloudEnabled() {
		return fmt.Errorf("云盘功能未启用，请到设置 → 云盘管理打开")
	}
	if err := clouddrive.SetCredential(acctID, value); err != nil {
		applog.Warnf("clouddrive", "设置凭证失败 id=%s: %v", acctID, err)
		return err
	}
	applog.Infof("clouddrive", "已保存凭证 id=%s", acctID)
	return nil
}

// cloudProvider 取已启用且已授权的账号 provider。
func cloudProvider(acctID string) (clouddrive.Provider, error) {
	if !cloudEnabled() {
		return nil, fmt.Errorf("云盘功能未启用，请到设置 → 云盘管理打开")
	}
	return clouddrive.New(acctID)
}

// CloudTestAccount 校验账号可用（拉一次配额）。
func (a *App) CloudTestAccount(acctID string) error {
	p, err := cloudProvider(acctID)
	if err != nil {
		return err
	}
	ctx, cancel := withCloudCtx()
	defer cancel()
	return p.Test(ctx)
}

// CloudList 列目录，dirID 空表示根目录。
func (a *App) CloudList(acctID, dirID string) ([]clouddrive.Entry, error) {
	p, err := cloudProvider(acctID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := withCloudCtx()
	defer cancel()
	return p.List(ctx, dirID)
}

// CloudQuota 容量配额。
func (a *App) CloudQuota(acctID string) (clouddrive.Quota, error) {
	p, err := cloudProvider(acctID)
	if err != nil {
		return clouddrive.Quota{}, err
	}
	ctx, cancel := withCloudCtx()
	defer cancel()
	return p.Quota(ctx)
}

// CloudMkdir 新建目录。
func (a *App) CloudMkdir(acctID, parentID, name string) (clouddrive.Entry, error) {
	p, err := cloudProvider(acctID)
	if err != nil {
		return clouddrive.Entry{}, err
	}
	ctx, cancel := withCloudCtx()
	defer cancel()
	return p.Mkdir(ctx, parentID, strings.TrimSpace(name))
}

// CloudRename 重命名。
func (a *App) CloudRename(acctID, id, newName string) error {
	p, err := cloudProvider(acctID)
	if err != nil {
		return err
	}
	ctx, cancel := withCloudCtx()
	defer cancel()
	return p.Rename(ctx, id, strings.TrimSpace(newName))
}

// CloudMove 移动到目标目录。
func (a *App) CloudMove(acctID, id, dstParentID string) error {
	p, err := cloudProvider(acctID)
	if err != nil {
		return err
	}
	ctx, cancel := withCloudCtx()
	defer cancel()
	return p.Move(ctx, id, dstParentID)
}

// CloudDelete 删除（云端实体，非仅本地登记）。
func (a *App) CloudDelete(acctID, id string) error {
	p, err := cloudProvider(acctID)
	if err != nil {
		return err
	}
	ctx, cancel := withCloudCtx()
	defer cancel()
	if err := p.Delete(ctx, id); err != nil {
		return err
	}
	applog.Infof("clouddrive", "已删除云盘条目 id=%s", id)
	return nil
}

// CloudShareLink 生成对外分享链接。
func (a *App) CloudShareLink(acctID, id string) (string, error) {
	p, err := cloudProvider(acctID)
	if err != nil {
		return "", err
	}
	ctx, cancel := withCloudCtx()
	defer cancel()
	return p.ShareLink(ctx, id)
}

// CloudDownload 把云端文件流式下载到本地目录 dstDir 下并命名 name。
// dstDir 留空走 config.Cloud.SaveDir 或 appdirs.Data("clouddrive")/downloads。
// 返回落盘的绝对路径。
func (a *App) CloudDownload(acctID, fileID, name, dstDir string) (string, error) {
	p, err := cloudProvider(acctID)
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(dstDir)
	if dir == "" {
		if cfgDir := strings.TrimSpace(config.CloudCfg().SaveDir); cfgDir != "" {
			dir = cfgDir
		} else {
			base, e := appdirs.Data("clouddrive")
			if e != nil {
				return "", e
			}
			dir = filepath.Join(base, "downloads")
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建下载目录失败: %w", err)
	}
	out := filepath.Join(dir, sanitizeBase(name))
	ctx, cancel := withCloudCtx()
	defer cancel()
	rc, size, err := p.Open(ctx, fileID)
	if err != nil {
		return "", err
	}
	defer rc.Close()
	tr := clouddrive.NewOpTracker(clouddrive.OpDownload, "", sanitizeBase(name), size)
	f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		tr.Fail(err)
		return "", err
	}
	if _, err := io.Copy(f, tr.Wrap(ctx, rc)); err != nil {
		_ = f.Close()
		tr.Fail(err)
		return "", fmt.Errorf("下载写盘失败: %w", err)
	}
	if err := f.Close(); err != nil {
		tr.Fail(err)
		return "", err
	}
	tr.Done(fmt.Sprintf("已下载到 %s", out))
	applog.Infof("clouddrive", "已下载 %s → %s", name, out)
	return out, nil
}

// CloudUpload 把本机文件 localPath 上传到 parentID 目录下，命名 name。
func (a *App) CloudUpload(acctID, parentID, name, localPath string) (clouddrive.Entry, error) {
	p, err := cloudProvider(acctID)
	if err != nil {
		return clouddrive.Entry{}, err
	}
	if strings.TrimSpace(name) == "" {
		name = filepath.Base(localPath)
	}
	f, err := os.Open(localPath)
	if err != nil {
		return clouddrive.Entry{}, fmt.Errorf("本地文件不可读: %w", err)
	}
	defer f.Close()
	var size int64 = -1
	if st, err := f.Stat(); err == nil {
		size = st.Size()
	}
	ctx, cancel := withCloudCtx()
	defer cancel()
	tr := clouddrive.NewOpTracker(clouddrive.OpUpload, "", name, size)
	e, err := p.Upload(ctx, parentID, name, tr.Wrap(ctx, f), size)
	if err != nil {
		tr.Fail(err)
		return clouddrive.Entry{}, err
	}
	tr.Done(fmt.Sprintf("已上传 %s", name))
	applog.Infof("clouddrive", "已上传 %s（%d 字节）", name, e.Size)
	return e, nil
}

// CloudTransfer 发起一笔跨盘转存（源账号文件 → 目标账号目录），
// 不阻塞：立刻回一条 Transfer 快照（含 id），后台推进并靠 cloud:progress 事件 +
// CloudListTransfers 刷新。srcDir 是源条目所在目录 id（用于取大小/显示名，取不到不阻断）。
func (a *App) CloudTransfer(srcAcct, srcID, srcDir, dstAcct, dstParent, name string) (clouddrive.Transfer, error) {
	if !cloudEnabled() {
		return clouddrive.Transfer{}, fmt.Errorf("云盘功能未启用，请到设置 → 云盘管理打开")
	}
	tr, err := clouddrive.StartTransfer(srcAcct, srcID, srcDir, dstAcct, dstParent, name)
	if err != nil {
		applog.Warnf("clouddrive", "发起转存失败 src=%s dst=%s: %v", srcAcct, dstAcct, err)
		return clouddrive.Transfer{}, err
	}
	applog.Infof("clouddrive", "已发起跨盘转存 %s: %s → %s", tr.ID, srcID, dstAcct)
	return tr, nil
}

// CloudListTransfers 最近若干条转存记录（新的在前）。
func (a *App) CloudListTransfers() []clouddrive.Transfer {
	return clouddrive.ListTransfers()
}

// CloudCancelTransfer 取消一笔进行中的转存。
func (a *App) CloudCancelTransfer(id string) error {
	if err := clouddrive.CancelTransfer(id); err != nil {
		applog.Warnf("clouddrive", "取消转存失败 id=%s: %v", id, err)
		return err
	}
	return nil
}

// sanitizeBase 只保留文件名basename并去掉非法字符，防止下载路径穿越。
func sanitizeBase(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.ReplaceAll(name, "..", "")
	if name == "" || name == "." || name == "/" {
		name = "download"
	}
	return name
}

/* ======================== 云盘设置 ======================== */

// CloudSettings 返回云盘设置段（老配置无此段时返回默认值）。
// token 不在此列，只进加密账号表。
func (a *App) CloudSettings() config.CloudConfig {
	return config.CloudCfg()
}

// SaveCloudSettings 保存云盘设置：总开关 + Google 客户端凭证 + 回调/落地目录/并发。
// clientSecret 传空串表示“沿用已保存的”，避免前端回显密钥与误抹。
func (a *App) SaveCloudSettings(enabled bool, clientID, clientSecret, redirect, saveDir string, concurrency int) error {
	_, err := config.Update(func(cfg *config.AppConfig) {
		if cfg.Cloud == nil {
			cfg.Cloud = &config.CloudConfig{}
		}
		cfg.Cloud.Enabled = enabled
		cfg.Cloud.GoogleClientID = strings.TrimSpace(clientID)
		if strings.TrimSpace(clientSecret) != "" {
			cfg.Cloud.GoogleClientSecret = strings.TrimSpace(clientSecret)
		}
		cfg.Cloud.GoogleRedirectURI = strings.TrimSpace(redirect)
		cfg.Cloud.SaveDir = strings.TrimSpace(saveDir)
		cfg.Cloud.Concurrency = concurrency
	})
	if err != nil {
		applog.Errorf("clouddrive", "保存云盘设置失败: %v", err)
		return err
	}
	applog.Infof("clouddrive", "已保存云盘设置: enabled=%v", enabled)
	return nil
}
