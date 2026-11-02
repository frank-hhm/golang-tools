package app

import (
	"context"
	"time"

	"Tools/tools/sshgen"
)

/*
SSH 生成助手（sshgen）绑定层——共享组件，不入工具、不注册路由。

内核在 tools/sshgen：纯 x/crypto/ssh + pkg/sftp，跨平台零 cgo。
不加 build tag：Linux 面板构建同样存在（AGENTS「绑定方法不能藏在 build tag 后面」），
否则前端调用会报 unknown bound method id。

口令/密码只作入参在内存中使用，绝不落盘、绝不回传前端；部署走 known_hosts TOFU +
SFTP 读写授权文件（不拼远端 shell 命令）。
*/

// SSHDefaultDir 密钥默认保存目录 <data>/ssh。
func (a *App) SSHDefaultDir() (string, error) {
	return sshgen.DefaultDir()
}

// SSHGenKey 生成密钥对并落盘，返回路径与公钥信息（不含私钥正文）。
func (a *App) SSHGenKey(o sshgen.KeyOptions) (sshgen.KeyPair, error) {
	return sshgen.Generate(o)
}

// SSHCommands 由主机上下文生成 ssh/scp/sftp 命令与 config 片段。
func (a *App) SSHCommands(h sshgen.HostRef) (sshgen.Commands, error) {
	return sshgen.CommandsOf(h), nil
}

// SSHExportConn 导出可分享/导入的连接凭据（不含机密）。
func (a *App) SSHExportConn(h sshgen.HostRef) (string, error) {
	return sshgen.ExportConn(h)
}

// SSHImportConn 解析连接凭据。
func (a *App) SSHImportConn(text string) (sshgen.HostRef, error) {
	return sshgen.ImportConn(text)
}

// SSHDeploy 一键部署公钥到远端（SFTP 写 authorized_keys，幂等）。
func (a *App) SSHDeploy(req sshgen.DeployReq) (sshgen.DeployResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	return sshgen.Deploy(ctx, req)
}
