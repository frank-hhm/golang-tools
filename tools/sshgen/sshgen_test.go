package sshgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestGenerateThreeTypes(t *testing.T) {
	dir := t.TempDir()
	for _, typ := range []string{"ed25519", "rsa", "ecdsa"} {
		kp, err := Generate(KeyOptions{Type: typ, Dir: dir, Name: "k_" + typ, Comment: "t@" + typ})
		if err != nil {
			t.Fatalf("%s: %v", typ, err)
		}
		if _, err := os.Stat(kp.PrivPath); err != nil {
			t.Fatalf("%s 私钥缺失: %v", typ, err)
		}
		if _, err := os.Stat(kp.PubPath); err != nil {
			t.Fatalf("%s 公钥缺失: %v", typ, err)
		}
		// 公钥行可解析且指纹一致
		pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(kp.PubKeyLine))
		if err != nil {
			t.Fatalf("%s 解析公钥行: %v", typ, err)
		}
		if ssh.FingerprintSHA256(pub) != kp.FingerprintSHA256 {
			t.Fatalf("%s 指纹不一致", typ)
		}
	}
}

func TestGenerateRejectsBadParams(t *testing.T) {
	dir := t.TempDir()
	if _, err := Generate(KeyOptions{Type: "dsa", Dir: dir}); err == nil {
		t.Error("未知类型应报错")
	}
	if _, err := Generate(KeyOptions{Type: "rsa", Bits: 1024, Dir: dir}); err == nil {
		t.Error("RSA 1024 应被拒绝")
	}
	if _, err := Generate(KeyOptions{Type: "ecdsa", Bits: 224, Dir: dir}); err == nil {
		t.Error("ECDSA 224 应被拒绝")
	}
}

func TestGeneratePassphrase(t *testing.T) {
	dir := t.TempDir()
	kp, err := Generate(KeyOptions{Type: "ed25519", Dir: dir, Name: "enc", Passphrase: "s3cret"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(kp.PrivPath)
	if _, err := ssh.ParsePrivateKey(raw); err == nil {
		t.Fatal("带口令私钥不应能被无口令解析")
	}
	if _, err := ssh.ParsePrivateKeyWithPassphrase(raw, []byte("s3cret")); err != nil {
		t.Fatalf("带口令解析失败: %v", err)
	}
}

func TestGenerateUniqueAvoidance(t *testing.T) {
	dir := t.TempDir()
	a, err := Generate(KeyOptions{Type: "ed25519", Dir: dir, Name: "same"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := Generate(KeyOptions{Type: "ed25519", Dir: dir, Name: "same"})
	if err != nil {
		t.Fatal(err)
	}
	if a.PrivPath == b.PrivPath {
		t.Fatal("重名应避让出不同文件")
	}
	if filepath.Dir(a.PrivPath) != filepath.Dir(b.PrivPath) {
		t.Fatal("应落在同一目录")
	}
}

func TestCommandsIPv6SpacePort(t *testing.T) {
	c := CommandsOf(HostRef{Address: "2001:db8::1", Port: 2222, User: "root", KeyPath: `C:\Users\me\my key`})
	if !strings.Contains(c.SSH, "[2001:db8::1]") {
		t.Errorf("ssh 缺 IPv6 方括号: %s", c.SSH)
	}
	if !strings.Contains(c.SSH, "-p 2222") {
		t.Errorf("ssh 缺 -p: %s", c.SSH)
	}
	if !strings.Contains(c.SSH, `-i "C:\Users\me\my key"`) {
		t.Errorf("ssh 缺带引号 -i: %s", c.SSH)
	}
	if !strings.Contains(c.SCP, "-P 2222") {
		t.Errorf("scp 应用 -P: %s", c.SCP)
	}
	if !strings.Contains(c.SFTP, "-P 2222") {
		t.Errorf("sftp 应用 -P: %s", c.SFTP)
	}
	if !strings.Contains(c.ConfigBlock, "Port 2222") {
		t.Errorf("config 缺 Port: %s", c.ConfigBlock)
	}
}

func TestCommandsDefaultPortNoFlag(t *testing.T) {
	c := CommandsOf(HostRef{Address: "example.com", Port: 22, User: "bob"})
	if strings.Contains(c.SSH, "-p ") {
		t.Errorf("22 端口不应带 -p: %s", c.SSH)
	}
	if strings.Contains(c.ConfigBlock, "Port ") {
		t.Errorf("22 端口 config 不应有 Port 行: %s", c.ConfigBlock)
	}
}

func TestConfigAliasSanitized(t *testing.T) {
	c := CommandsOf(HostRef{Address: "h", User: "u", Alias: "my alias~x"})
	first := strings.SplitN(c.ConfigBlock, "\n", 2)[0]
	alias := strings.TrimSpace(strings.TrimPrefix(first, "Host"))
	if alias != "myaliasx" {
		t.Errorf("别名未净化: %q (整行 %q)", alias, first)
	}
}

func TestValidateRejectsBadKey(t *testing.T) {
	bad := []string{
		"",
		"not a key",
		"ssh-rsa NOTBASE64!!! x",
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAA\nrm -rf /",
	}
	for _, b := range bad {
		if _, err := validateAuthorizedKey(b); err == nil {
			t.Errorf("应拒绝: %q", b)
		}
	}
}

func TestValidateAcceptsGenerated(t *testing.T) {
	dir := t.TempDir()
	kp, err := Generate(KeyOptions{Type: "ed25519", Dir: dir, Name: "v", Comment: "c"})
	if err != nil {
		t.Fatal(err)
	}
	line, err := validateAuthorizedKey(kp.PubKeyLine)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(line, "ssh-ed25519 ") {
		t.Errorf("规范行前缀异常: %s", line)
	}
}

func TestBlobRoundTrip(t *testing.T) {
	h := HostRef{Address: "h1", Port: 2200, User: "u", Alias: "a1", KeyPath: "/k", PubKey: "ssh-ed25519 AAAA c", Comment: "cm"}
	s, err := ExportConn(h)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(s), "password") {
		t.Error("导出不应含 password 字段")
	}
	got, err := ImportConn(s)
	if err != nil {
		t.Fatal(err)
	}
	if got.Address != h.Address || got.Port != h.Port || got.User != h.User || got.PubKey != h.PubKey {
		t.Fatalf("往返不一致: %+v", got)
	}
	if _, err := ImportConn(`{"kind":"other","v":1,"address":"x"}`); err == nil {
		t.Error("错误 kind 应被拒绝")
	}
	if _, err := ImportConn(`{"kind":"gtools.ssh-conn","v":99,"address":"x"}`); err == nil {
		t.Error("错误版本应被拒绝")
	}
}
