// Package sshgen 是「SSH 生成助手」的内核：生成密钥对、生成 ssh/scp/sftp 命令
// 与 ~/.ssh/config 片段、经 SFTP 一键部署公钥、导出/导入可共享的连接凭据。
//
// 它是「共享组件」而非工具：不注册路由、不进 config.Tools，只作为 Go 能力挂在
// FTP 与终端两个既有面板的表单弹窗上（前端 components/business/ssh-gen）。
//
// 跨平台零 cgo：纯 golang.org/x/crypto/ssh + github.com/pkg/sftp，Linux 面板构建
// 同样可用。口令/密码只在入参内存中使用，绝不落盘、绝不回传前端。
package sshgen

import "Tools/appdirs"

// DefaultDir 返回密钥默认保存目录 <data>/ssh（不存在则创建）。
func DefaultDir() (string, error) {
	return appdirs.Data("ssh")
}

// KeyOptions 生成密钥对的参数。
type KeyOptions struct {
	Type       string `json:"type"`       // ed25519(默认) | rsa | ecdsa
	Bits       int    `json:"bits"`       // rsa 位数(2048/3072/4096)；ecdsa 曲线(256/384/521)
	Comment    string `json:"comment"`    // 注释，空则取 user@host
	Passphrase string `json:"passphrase"` // 私钥口令，仅本次派生用，不落盘不进缓存
	Dir        string `json:"dir"`        // 保存目录，空=DefaultDir()
	Name       string `json:"name"`       // 文件名基名，空=id_<type>
}

// KeyPair 生成结果。私钥只落盘，正文不回传。
type KeyPair struct {
	Type              string `json:"type"`
	Comment           string `json:"comment"`
	PrivPath          string `json:"privPath"`
	PubPath           string `json:"pubPath"`
	PubKeyLine        string `json:"pubKeyLine"` // authorized_keys 单行
	FingerprintSHA256 string `json:"fingerprintSHA256"`
	FingerprintMD5    string `json:"fingerprintMD5"`
	// ACLWarning 非空表示私钥的 NTFS 权限没收紧成功。Windows 版 OpenSSH 会拒用权限
	// 宽松的私钥（报 UNPROTECTED PRIVATE KEY FILE），所以这不是提示，是功能可能失效。
	ACLWarning string `json:"aclWarning"`
}

// HostRef 主机上下文（不含任何密码/私钥正文），用于生成命令片段与连接凭据。
type HostRef struct {
	Address string `json:"address"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	Alias   string `json:"alias"`   // ~/.ssh/config 里的 Host 别名
	KeyPath string `json:"keyPath"` // 私钥路径（可选）
	PubKey  string `json:"pubKey"`  // 公钥行（导出凭据用，可选）
	Comment string `json:"comment"`
}

// Commands 一段主机的常用命令与 config 片段。
type Commands struct {
	SSH         string `json:"ssh"`
	SCP         string `json:"scp"`
	SFTP        string `json:"sftp"`
	ConfigBlock string `json:"configBlock"`
}

// DeployReq 部署公钥的请求。含认证信息，仅内存使用，绝不落盘/回传。
type DeployReq struct {
	Address    string `json:"address"`
	Port       int    `json:"port"`
	User       string `json:"user"`
	Auth       string `json:"auth"`       // password(默认) | key
	Password   string `json:"password"`   // auth=password
	KeyPath    string `json:"keyPath"`    // auth=key
	Passphrase string `json:"passphrase"` // auth=key 私钥口令
	PubKey     string `json:"pubKey"`     // 待部署的公钥行
}

// DeployResult 部署结果。
type DeployResult struct {
	Added   bool   `json:"added"`   // 本次新增
	Existed bool   `json:"existed"` // 已存在（幂等，未重复追加）
	Path    string `json:"path"`    // 远端 authorized_keys 路径
	Line    string `json:"line"`    // 规范化后的公钥行
}
