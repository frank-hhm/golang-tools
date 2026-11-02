package sshgen

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"

	"Tools/tools/term"
)

/*
一键部署公钥（ssh-copy-id 式，但走 SFTP 读写授权文件，不拼远端 shell 命令）。

安全要点：
  - 写进远端 authorized_keys 的绝不是用户原文，而是先严格解析、再重新序列化的规范行
    —— 这是提权入口，畸形/多行/注入一律拒绝。
  - 主机密钥校验复用 term 的 known_hosts TOFU（term.HostKeyCallback），不另写不安全实现。
  - 全程走 SFTP 子系统，规避命令注入与 authorized_keys 权限被远端 umask 带偏。
  - 幂等：按公钥 wire 字节比对（忽略 comment 差异），同一把钥匙不因注释不同写两遍。

只保证 Linux/POSIX 远端；Windows 远端的 authorized_keys 要求严格 ACL 且管理员组走
administrators_authorized_keys，本期不做（连不上/写不进时如实报错，不静默成功）。
*/

// Deploy 把 req.PubKey 部署到远端 ~/.ssh/authorized_keys。
func Deploy(ctx context.Context, req DeployReq) (DeployResult, error) {
	name := req.User + "@" + req.Address
	fail := func(stage string, err error) (DeployResult, error) {
		emitProgress(Progress{Op: "deploy", Name: name, Stage: StageError, Note: err.Error()})
		return DeployResult{}, fmt.Errorf("%s: %w", stage, err)
	}

	emitProgress(Progress{Op: "deploy", Name: name, Stage: StageRunning, Percent: 5, Note: "校验公钥"})
	canon, err := validateAuthorizedKey(req.PubKey)
	if err != nil {
		return fail("公钥校验", err)
	}
	wantPub, _, _, _, _ := ssh.ParseAuthorizedKey([]byte(canon))
	wantBytes := wantPub.Marshal()

	cfg, err := deployClientConfig(req)
	if err != nil {
		return fail("认证配置", err)
	}

	port := req.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(req.Address, strconv.Itoa(port))

	emitProgress(Progress{Op: "deploy", Name: name, Stage: StageRunning, Percent: 20, Note: "连接远端"})
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fail("连接远端", err)
	}
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	}
	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return fail("SSH 握手", err)
	}
	client := ssh.NewClient(sshConn, chans, reqs)
	defer client.Close()

	emitProgress(Progress{Op: "deploy", Name: name, Stage: StageRunning, Percent: 45, Note: "打开 SFTP"})
	sc, err := sftp.NewClient(client)
	if err != nil {
		return fail("打开 SFTP", err)
	}
	defer sc.Close()

	home, err := sc.Getwd()
	if err != nil || strings.TrimSpace(home) == "" {
		return fail("定位家目录", fmt.Errorf("无法确定远端家目录: %v", err))
	}
	sshDir := path.Join(home, ".ssh")
	akPath := path.Join(sshDir, "authorized_keys")

	emitProgress(Progress{Op: "deploy", Name: name, Stage: StageRunning, Percent: 60, Note: "准备 ~/.ssh"})
	if err := sc.MkdirAll(sshDir); err != nil {
		return fail("创建 ~/.ssh", err)
	}
	_ = sc.Chmod(sshDir, 0o700)

	emitProgress(Progress{Op: "deploy", Name: name, Stage: StageRunning, Percent: 75, Note: "读取 authorized_keys"})
	// 先 Stat 判存在再读：读失败绝不吞掉（否则下面 O_TRUNC 会清空已有授权，等于踢掉别人所有钥匙）。
	var existing []byte
	if _, stErr := sc.Stat(akPath); stErr == nil {
		f, err := sc.Open(akPath)
		if err != nil {
			return fail("打开授权文件", err)
		}
		existing, err = io.ReadAll(f)
		_ = f.Close()
		if err != nil {
			return fail("读取授权文件", err)
		}
	} else if !os.IsNotExist(stErr) {
		return fail("检查授权文件", stErr)
	}

	for _, ln := range bytes.Split(existing, []byte("\n")) {
		t := bytes.TrimSpace(ln)
		if len(t) == 0 || bytes.HasPrefix(t, []byte("#")) {
			continue
		}
		if p, _, _, _, e := ssh.ParseAuthorizedKey(t); e == nil && bytes.Equal(p.Marshal(), wantBytes) {
			emitProgress(Progress{Op: "deploy", Name: name, Stage: StageDone, Percent: 100, Note: "公钥已存在，未重复添加"})
			return DeployResult{Existed: true, Path: akPath, Line: canon}, nil
		}
	}

	var buf bytes.Buffer
	buf.Write(existing)
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		buf.WriteByte('\n')
	}
	buf.WriteString(canon + "\n")

	emitProgress(Progress{Op: "deploy", Name: name, Stage: StageRunning, Percent: 90, Note: "写入 authorized_keys"})
	f, err := sc.OpenFile(akPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC)
	if err != nil {
		return fail("打开授权文件", err)
	}
	if _, err := f.Write(buf.Bytes()); err != nil {
		_ = f.Close()
		return fail("写入授权文件", err)
	}
	_ = f.Close()
	_ = sc.Chmod(akPath, 0o600)

	emitProgress(Progress{Op: "deploy", Name: name, Stage: StageDone, Percent: 100, Note: "公钥已部署"})
	return DeployResult{Added: true, Path: akPath, Line: canon}, nil
}

// validateAuthorizedKey 严格校验并规范化公钥行：必须单行、可解析、行尾无多余内容，
// 返回重新序列化后的规范行（type base64 [comment]）。绝不把用户原文写进远端。
func validateAuthorizedKey(pubLine string) (string, error) {
	line := strings.TrimSpace(pubLine)
	if line == "" {
		return "", fmt.Errorf("公钥为空")
	}
	if strings.ContainsAny(line, "\n\r") {
		return "", fmt.Errorf("公钥必须是单行")
	}
	pub, comment, _, rest, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil {
		return "", fmt.Errorf("公钥格式无法解析: %w", err)
	}
	if strings.TrimSpace(string(rest)) != "" {
		return "", fmt.Errorf("公钥行尾含多余内容，已拒绝")
	}
	s := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(pub)))
	if comment != "" {
		s += " " + comment
	}
	return s, nil
}

// deployClientConfig 按认证方式组装 ssh.ClientConfig；主机密钥走 term 的 known_hosts 校验。
func deployClientConfig(req DeployReq) (*ssh.ClientConfig, error) {
	port := req.Port
	if port == 0 {
		port = 22
	}
	cfg := &ssh.ClientConfig{
		User:            req.User,
		HostKeyCallback: term.HostKeyCallback(req.Address, port),
		Timeout:         12 * time.Second,
	}
	switch strings.ToLower(strings.TrimSpace(req.Auth)) {
	case "key":
		if strings.TrimSpace(req.KeyPath) == "" {
			return nil, fmt.Errorf("请先选择私钥文件")
		}
		raw, err := os.ReadFile(req.KeyPath)
		if err != nil {
			return nil, fmt.Errorf("读取私钥失败: %w", err)
		}
		var signer ssh.Signer
		if req.Passphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(raw, []byte(req.Passphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(raw)
		}
		if err != nil {
			if req.Passphrase == "" {
				return nil, fmt.Errorf("私钥解析失败（带口令的私钥请填写口令）: %w", err)
			}
			return nil, fmt.Errorf("私钥口令不正确: %w", err)
		}
		cfg.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	default:
		if req.Password == "" {
			return nil, fmt.Errorf("请输入密码或改用密钥认证")
		}
		pass := req.Password
		cfg.Auth = []ssh.AuthMethod{
			ssh.Password(pass),
			ssh.KeyboardInteractive(func(_, _ string, questions []string, _ []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = pass
				}
				return answers, nil
			}),
		}
	}
	return cfg, nil
}
