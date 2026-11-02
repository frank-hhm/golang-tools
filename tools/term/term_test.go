package term

// SSH P0/P1 单测：凭据升格迁移、凭据 CRUD 与悬挂引用清理、分组标签、
// known_hosts 三档策略（tofu 首记/匹配/不匹配阻断、strict 拒绝未知）、
// ~/.ssh/config 解析；并用进程内 SSH 服务器跑一遍真实握手（密码认证 + 首记）。
// 所有用例先把 appdirs 指到 t.TempDir()，不碰真实数据。

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"testing"

	"golang.org/x/crypto/ssh"

	"Tools/appdirs"
)

func useTempData(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	if err := appdirs.Set(filepath.Join(base, "plugins"), filepath.Join(base, "data"), filepath.Join(base, "logs")); err != nil {
		t.Fatalf("切换临时数据目录失败: %v", err)
	}
}

/* ==================== P0：凭据迁移 ==================== */

func TestMigrationPromotesLegacyHost(t *testing.T) {
	useTempData(t)

	// 以旧式内联密码保存一条主机（无 credID）：落 host.PassEnc
	list, err := SaveHost(Host{Name: "旧机", Address: "10.0.0.1", Port: 22, User: "root", Auth: AuthPassword, Password: "s3cret"})
	if err != nil {
		t.Fatalf("SaveHost 失败: %v", err)
	}
	hostID := list[0].ID
	if list[0].CredID != "" {
		t.Fatalf("保存后不应立刻有 credID，迁移在读时触发")
	}

	// 再读一次触发迁移
	hosts, err := ListHosts()
	if err != nil {
		t.Fatalf("ListHosts 失败: %v", err)
	}
	if hosts[0].CredID == "" {
		t.Fatalf("迁移后 credID 应被回填")
	}

	creds, err := ListCredentials()
	if err != nil {
		t.Fatalf("ListCredentials 失败: %v", err)
	}
	if len(creds) != 1 {
		t.Fatalf("迁移应生成 1 条凭据，得到 %d", len(creds))
	}
	if creds[0].User != "root" || creds[0].Auth != AuthPassword {
		t.Errorf("迁移凭据字段不对: %+v", creds[0])
	}

	// resolve 应解出明文密码
	_, spec, err := resolve(hostID)
	if err != nil {
		t.Fatalf("resolve 失败: %v", err)
	}
	if spec.Password != "s3cret" {
		t.Errorf("resolve 密码 = %q, want s3cret", spec.Password)
	}

	// 重复读：幂等，不产生第二条凭据
	for i := 0; i < 3; i++ {
		if _, err := ListHosts(); err != nil {
			t.Fatalf("重复 ListHosts 失败: %v", err)
		}
	}
	creds, _ = ListCredentials()
	if len(creds) != 1 {
		t.Fatalf("重复迁移应仍为 1 条凭据，得到 %d", len(creds))
	}
}

/* ==================== P0：凭据 CRUD + 悬挂引用 ==================== */

func TestCredentialCRUDAndDangling(t *testing.T) {
	useTempData(t)

	creds, err := SaveCredential(Credential{Name: "生产账号", Auth: AuthPassword, User: "deploy", Password: "pw1"})
	if err != nil {
		t.Fatalf("SaveCredential 失败: %v", err)
	}
	if len(creds) != 1 {
		t.Fatalf("应有 1 条凭据，得到 %d", len(creds))
	}
	cid := creds[0].ID

	// 两台主机共用同一凭据
	h1, err := SaveHost(Host{Name: "A", Address: "1.1.1.1", Port: 22, CredID: cid})
	if err != nil {
		t.Fatalf("SaveHost A 失败: %v", err)
	}
	if _, err := SaveHost(Host{Name: "B", Address: "2.2.2.2", Port: 22, CredID: cid}); err != nil {
		t.Fatalf("SaveHost B 失败: %v", err)
	}
	_ = h1

	// 删除凭据后，引用它的主机 credID 应被清空
	if _, err := DeleteCredential(cid); err != nil {
		t.Fatalf("DeleteCredential 失败: %v", err)
	}
	hosts, _ := ListHosts()
	for _, h := range hosts {
		if h.CredID != "" {
			t.Errorf("主机 %s 仍引用已删除凭据: credID=%q", h.Name, h.CredID)
		}
	}

	// resolve 走凭据：改密一处生效
	nc, err := SaveCredential(Credential{Name: "共享", Auth: AuthPassword, User: "u", Password: "newpw"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveHost(Host{Name: "C", Address: "3.3.3.3", Port: 22, CredID: nc[0].ID}); err != nil {
		t.Fatal(err)
	}
	hosts, _ = ListHosts()
	var ch Host
	for _, h := range hosts {
		if h.Name == "C" {
			ch = h
		}
	}
	_, spec, err := resolve(ch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if spec.Password != "newpw" || spec.User != "u" {
		t.Errorf("凭据解析不对: %+v", spec)
	}
}

/* ==================== P0：分组与标签 ==================== */

func TestGroupAndTags(t *testing.T) {
	useTempData(t)
	if _, err := SaveHost(Host{Name: "G", Address: "4.4.4.4", Port: 22, Auth: AuthPassword, User: "r", Password: "p", Group: "生产", Tags: []string{"web", "web", " db ", ""}, Color: "#f60"}); err != nil {
		t.Fatalf("SaveHost 失败: %v", err)
	}
	hosts, _ := ListHosts()
	h := hosts[0]
	if h.Group != "生产" || h.Color != "#f60" {
		t.Errorf("分组/颜色不对: %+v", h)
	}
	want := []string{"web", "db"}
	if len(h.Tags) != len(want) {
		t.Fatalf("标签去重/清洗不对: %v", h.Tags)
	}
	for i := range want {
		if h.Tags[i] != want[i] {
			t.Errorf("标签[%d]=%q want %q", i, h.Tags[i], want[i])
		}
	}
}

/* ==================== P1：known_hosts 三档策略 ==================== */

func genSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestHostKeyCallbackTOFU(t *testing.T) {
	useTempData(t)
	if _, err := SetHostKeyPolicy(HostKeyTOFU); err != nil {
		t.Fatal(err)
	}
	k1 := genSigner(t)
	k2 := genSigner(t)
	remote := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 22}

	cb := hostKeyCallback("127.0.0.1", 22)
	// 首次未知 → 记录并放行
	if err := cb("127.0.0.1:22", remote, k1.PublicKey()); err != nil {
		t.Fatalf("tofu 首次连接应放行，却报错: %v", err)
	}
	// 再次同键 → 匹配放行
	if err := cb("127.0.0.1:22", remote, k1.PublicKey()); err != nil {
		t.Fatalf("tofu 同指纹应放行: %v", err)
	}
	// 换了主机键 → 硬阻断
	if err := cb("127.0.0.1:22", remote, k2.PublicKey()); err == nil {
		t.Fatal("tofu 指纹变化应阻断，却放行了")
	}
	entries, _ := ListKnownHosts()
	if len(entries) != 1 {
		t.Fatalf("known_hosts 应记录 1 条，得到 %d", len(entries))
	}
}

func TestHostKeyCallbackStrict(t *testing.T) {
	useTempData(t)
	if _, err := SetHostKeyPolicy(HostKeyStrict); err != nil {
		t.Fatal(err)
	}
	k := genSigner(t)
	remote := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 22}
	cb := hostKeyCallback("127.0.0.1", 22)
	if err := cb("127.0.0.1:22", remote, k.PublicKey()); err == nil {
		t.Fatal("strict 模式下未知主机应被拒绝")
	}
	entries, _ := ListKnownHosts()
	if len(entries) != 0 {
		t.Fatalf("strict 不应自动记录，得到 %d 条", len(entries))
	}
}

/* ==================== P1：~/.ssh/config 解析 ==================== */

func TestParseSSHConfig(t *testing.T) {
	data := []byte(`
# 全局
User fallback

Host web1
  HostName 203.0.113.10
  User ubuntu
  Port 2222
  IdentityFile ~/.ssh/id_web
  ProxyJump bastion

Host jump
  HostName 203.0.113.1
  User ec2-user
  ForwardAgent yes

Host *
  StrictHostKeyChecking ask

Host some
  Match all
    User should-be-ignored
`)
	drafts := parseSSHConfigBytes(data)
	byAlias := map[string]HostDraft{}
	for _, d := range drafts {
		byAlias[d.Alias] = d
	}
	web, ok := byAlias["web1"]
	if !ok {
		t.Fatalf("未解析到 web1: %+v", drafts)
	}
	if web.HostName != "203.0.113.10" || web.User != "ubuntu" || web.Port != 2222 {
		t.Errorf("web1 字段不对: %+v", web)
	}
	if web.IdentityFile == "" || filepath.Base(web.IdentityFile) != "id_web" {
		t.Errorf("web1 IdentityFile 不对: %q", web.IdentityFile)
	}
	if len(web.Unsupported) != 1 || web.Unsupported[0] != "ProxyJump" {
		t.Errorf("web1 应标注 ProxyJump 未生效: %+v", web.Unsupported)
	}
	jump := byAlias["jump"]
	if len(jump.Unsupported) != 1 || jump.Unsupported[0] != "ForwardAgent" {
		t.Errorf("jump 应标注 ForwardAgent 未生效: %+v", jump.Unsupported)
	}
	star, ok := byAlias["*"]
	if !ok || !star.Wildcard {
		t.Errorf("* 模式应被标记为 wildcard: %+v", star)
	}
	// Match 块内指令应被忽略，some 主机不应带 User=should-be-ignored
	some := byAlias["some"]
	if some.User == "should-be-ignored" {
		t.Errorf("Match 块指令应被忽略: %+v", some)
	}
}

/* ==================== P1：进程内 SSH 服务器端到端（真实握手） ==================== */

func startTestSSHServer(t *testing.T, signer ssh.Signer, wantUser, wantPass string) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{
		PasswordCallback: func(meta ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			if meta.User() == wantUser && string(pass) == wantPass {
				return nil, nil
			}
			return nil, fmt.Errorf("认证被拒绝")
		},
	}
	cfg.AddHostKey(signer)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func(c net.Conn) {
				sc, _, _, herr := ssh.NewServerConn(c, cfg)
				if herr != nil {
					_ = c.Close()
					return
				}
				// Test() 只需握手 + 认证，不建 channel；保持连接直到客户端关闭
				_ = sc.Wait()
				_ = sc.Close()
			}(conn)
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		<-done
	}
}

func TestEndToEndPasswordWithTOFU(t *testing.T) {
	useTempData(t)
	if _, err := SetHostKeyPolicy(HostKeyTOFU); err != nil {
		t.Fatal(err)
	}
	signer := genSigner(t)
	addr, stop := startTestSSHServer(t, signer, "root", "pw")
	defer stop()

	hostOnly, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(portStr)

	h := Host{Name: "本机测试", Address: hostOnly, Port: port, User: "root", Auth: AuthPassword, Password: "pw"}
	ms, err := Test(h)
	if err != nil {
		t.Fatalf("首次握手（tofu 记录）失败: %v", err)
	}
	if ms < 0 {
		t.Errorf("握手耗时异常: %d", ms)
	}
	entries, _ := ListKnownHosts()
	if len(entries) != 1 {
		t.Fatalf("known_hosts 应记录 1 条，得到 %d", len(entries))
	}
	// 第二次复用已记录指纹，仍成功
	if _, err := Test(h); err != nil {
		t.Fatalf("二次握手应放行: %v", err)
	}
}
