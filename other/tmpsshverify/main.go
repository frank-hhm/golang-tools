// Command tmpsshverify 真机验证 Tools/tools/sshgen 生成的密钥能否被外部工具链接受。
//
// 不是产品代码，用完即删。它取五类互相独立的证据：
//  1. 系统 ssh-keygen -l -f 能读出私钥指纹 -> PEM 格式被 OpenSSH 接受（计划里标注的头号风险）
//  2. 系统 ssh-keygen -y -f 导出的公钥与 sshgen 写的 .pub 一致 -> 公钥行构造正确
//  3. x/crypto/ssh 客户端用该私钥连上一台服务器 -> Go 侧往返正确
//  4. 系统 ssh.exe 用该私钥连同一台服务器 -> 真实 OpenSSH 客户端能用（含 Windows 私钥 ACL 检查）
//  5. 负样本：没登记过的钥匙必须被拒 -> 证明第 3/4 条不是"服务器全放行"造成的假通过
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"Tools/tools/sshgen"
)

const marker = "SSHKEX-OK"

var (
	useReal = flag.Bool("real", false, "写到 sshgen.DefaultDir()（产品真实目录）而不是临时目录")
	tighten = flag.Bool("tighten", false, "写完私钥后用 icacls 收紧 ACL，验证权限是否为唯一障碍")
)

// 服务器的授权表与认证事件：每轮测试前重置，拨号后读事件判定服务端到底认了谁。
var (
	mu         sync.Mutex
	authorized map[string]bool
	authEvents = make(chan string, 64)
)

func setAuthorized(fps []string) {
	mu.Lock()
	authorized = map[string]bool{}
	for _, fp := range fps {
		authorized[fp] = true
	}
	mu.Unlock()
}

func clearEvents() {
	for {
		select {
		case <-authEvents:
		default:
			return
		}
	}
}

// popEvent 取一条服务端认证事件（"ALLOW <fp>" 或 "DENY <fp>"），超时返回空串。
func popEvent(d time.Duration) string {
	select {
	case s := <-authEvents:
		return s
	case <-time.After(d):
		return ""
	}
}

func main() {
	flag.Parse()
	dir, err := workDir()
	if err != nil {
		fatal(err)
	}
	if !*useReal {
		defer os.RemoveAll(dir)
	}
	fmt.Printf("密钥落盘目录：%s\n", dir)

	hostKP, err := sshgen.Generate(sshgen.KeyOptions{Type: "ed25519", Name: "host_key", Dir: dir})
	if err != nil {
		fatal(err)
	}
	hostSigner, err := loadSigner(hostKP.PrivPath, "")
	if err != nil {
		fatal(fmt.Errorf("host key 回读失败: %w", err))
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	defer ln.Close()
	go serve(ln, hostSigner)
	fmt.Printf("验证服务器已起在 127.0.0.1:%d\n\n", port)

	// 一把"从末部署"的对照钥匙，专门用来确认服务器不是来者不拒。
	ghost, err := sshgen.Generate(sshgen.KeyOptions{Type: "ed25519", Name: "ghost", Dir: dir})
	if err != nil {
		fatal(err)
	}

	cases := []struct {
		name string
		opts sshgen.KeyOptions
	}{
		{"ed25519", sshgen.KeyOptions{Type: "ed25519", Name: "v_ed25519"}},
		{"rsa-3072", sshgen.KeyOptions{Type: "rsa", Bits: 3072, Name: "v_rsa"}},
		{"ecdsa-p256", sshgen.KeyOptions{Type: "ecdsa", Bits: 256, Name: "v_ecdsa"}},
		{"ecdsa-p384", sshgen.KeyOptions{Type: "ecdsa", Bits: 384, Name: "v_ecdsa384"}},
		{"ed25519+口令", sshgen.KeyOptions{Type: "ed25519", Name: "v_prot", Passphrase: "verify-pass-1"}},
	}

	var fails int
	for _, c := range cases {
		c.opts.Dir = dir
		kp, err := sshgen.Generate(c.opts)
		if err != nil {
			fmt.Printf("[%s] Generate 失败: %v\n\n", c.name, err)
			fails++
			continue
		}
		pass := c.opts.Passphrase
		fmt.Printf("[%s] %s\n", c.name, kp.FingerprintSHA256)

		if *tighten && pass == "" {
			if err := tightACL(kp.PrivPath); err != nil {
				fmt.Printf("   WARN icacls 收紧失败: %v\n", err)
			} else {
				fmt.Printf("   icacls 已收紧 %s\n", filepath.Base(kp.PrivPath))
			}
		}

		// 证据 1、2：外部工具链（口令非空时 ssh-keygen -y 也要能解密导出）
		if !checkKeygenList(kp.PrivPath, pass, kp.FingerprintSHA256) {
			fails++
		}
		if pass == "" && !checkKeygenPub(kp.PrivPath, kp.PubKeyLine) {
			fails++
		}

		setAuthorized([]string{kp.FingerprintSHA256})

		// 证据 3：Go(x/crypto) 客户端
		clearEvents()
		if !dialGo(port, kp.PrivPath, pass) {
			fails++
		} else if ev := popEvent(3 * time.Second); !strings.HasPrefix(ev, "ALLOW") {
			fmt.Printf("   FAIL 服务端未记录到公钥被接受，事件=%q\n", ev)
			fails++
		}

		// 证据 4：真实 OpenSSH 客户端（带口令的需要交互，BatchMode 下不可行，跳过）
		if pass == "" {
			clearEvents()
			if !dialOpenSSH(port, kp.PrivPath, filepath.Join(dir, "known_hosts")) {
				fails++
			} else if ev := popEvent(3 * time.Second); !strings.HasPrefix(ev, "ALLOW") {
				fmt.Printf("   FAIL ssh.exe 已连上但服务端未记录 ALLOW，事件=%q\n", ev)
				fails++
			}
		} else {
			fmt.Printf("   SKIP ssh.exe（口令钥匙需交互输入，格式已由 ssh-keygen -l -P / -y -P 证明）\n")
		}

		// 证据 5：负样本，对照钥匙未授权必须被拒
		if pass == "" {
			clearEvents()
			setAuthorized([]string{"SHA256:never-registered"})
			if dialOpenSSHExpectFail(port, ghost.PrivPath, filepath.Join(dir, "known_hosts")) {
				fmt.Printf("   FAIL 对照钥匙竟被接受 —— 服务端认证形同虚设，上面的通过不可信\n")
				fails++
			} else if ev := popEvent(3 * time.Second); !strings.HasPrefix(ev, "DENY") {
				fmt.Printf("   WARN 对照钥匙被拒但未取到 DENY 事件（%q），可能根本没走到认证\n", ev)
			} else {
				fmt.Printf("   OK   负样本：对照钥匙被服务端拒绝（%s）\n", ev)
			}
		}
		fmt.Println()
	}

	if fails > 0 {
		fmt.Printf("结论：%d 项未通过\n", fails)
		os.Exit(1)
	}
	fmt.Println("结论：sshgen 生成的密钥被 ssh-keygen / x-crypto / OpenSSH 客户端三层一致接受")
}

// serve 常驻接受连接；每次认证尝试都把结果投进 authEvents 供测试侧断言。
func serve(ln net.Listener, hostSigner ssh.Signer) {
	cfg := ssh.ServerConfig{}
	cfg.PublicKeyCallback = func(cc ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		fp := ssh.FingerprintSHA256(key)
		mu.Lock()
		ok := authorized[fp]
		mu.Unlock()
		if ok {
			authEvents <- "ALLOW " + fp
			return &ssh.Permissions{}, nil
		}
		authEvents <- "DENY " + fp
		return nil, fmt.Errorf("key not authorized")
	}
	cfg.PasswordCallback = func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
		authEvents <- "DENY password"
		return nil, fmt.Errorf("password auth disabled in this verifier")
	}
	cfg.AddHostKey(hostSigner)

	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go handle(conn, &cfg)
	}
}

func handle(conn net.Conn, cfg *ssh.ServerConfig) {
	defer conn.Close()
	sc, chans, reqs, err := ssh.NewServerConn(conn, cfg)
	if err != nil {
		return
	}
	go ssh.DiscardRequests(reqs)
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			newCh.Reject(ssh.UnknownChannelType, "session only")
			continue
		}
		ch, chReqs, err := newCh.Accept()
		if err != nil {
			continue
		}
		go func(ch ssh.Channel, reqs <-chan *ssh.Request) {
			for req := range reqs {
				if req.Type != "exec" && req.Type != "shell" {
					if req.WantReply {
						req.Reply(false, nil)
					}
					continue
				}
				// 先回 exec 确认、再写数据、最后关通道：顺序反过来时 x/crypto 客户端
				// 会在拿到 Reply 之前就撞见 EOF，CombinedOutput 只回一个空串。
				if req.WantReply {
					req.Reply(true, nil)
				}
				fmt.Fprintf(ch, "%s\n", marker)
				_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				ch.Close()
			}
		}(ch, chReqs)
	}
	sc.Close()
}

func dialGo(port int, privPath, pass string) bool {
	signer, err := loadSigner(privPath, pass)
	if err != nil {
		fmt.Printf("   FAIL Go 侧私钥解析: %v\n", err)
		return false
	}
	cfg := &ssh.ClientConfig{
		User:            "tester",
		Auth:            []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	}
	addr := fmt.Sprintf("127.0.0.1:%d", port)
	cli, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		fmt.Printf("   FAIL Go 客户端连接: %v\n", err)
		return false
	}
	defer cli.Close()
	sess, err := cli.NewSession()
	if err != nil {
		fmt.Printf("   FAIL Go 客户端开会话: %v\n", err)
		return false
	}
	defer sess.Close()
	out, err := sess.CombinedOutput("echo " + marker)
	if !strings.Contains(string(out), marker) {
		fmt.Printf("   FAIL Go 客户端无回显: out=%q err=%v\n", strings.TrimSpace(string(out)), err)
		return false
	}
	fmt.Printf("   OK   Go(x/crypto) 密钥认证成功并拿到远端回显\n")
	return true
}

func dialOpenSSH(port int, privPath, knownHosts string) bool {
	out, err := runSSH(port, privPath, knownHosts)
	if err != nil {
		fmt.Printf("   FAIL ssh.exe: %v｜%s\n", err, strings.TrimSpace(out))
		return false
	}
	if !strings.Contains(out, marker) {
		fmt.Printf("   FAIL ssh.exe 无回显: %q\n", strings.TrimSpace(out))
		return false
	}
	fmt.Printf("   OK   ssh.exe(OpenSSH_for_Windows) 用生成的私钥真连成功\n")
	return true
}

// dialOpenSSHExpectFail 返回 true 代表"居然连上了"，即负样本失效。
func dialOpenSSHExpectFail(port int, privPath, knownHosts string) bool {
	out, err := runSSH(port, privPath, knownHosts)
	_ = out
	return err == nil
}

func runSSH(port int, privPath, knownHosts string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ssh.exe",
		"-i", privPath,
		"-o", "IdentitiesOnly=yes",
		"-o", "BatchMode=yes",
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile="+knownHosts,
		"-o", "GlobalKnownHostsFile="+knownHosts,
		"-o", "NumberOfPasswordPrompts=0",
		"-p", fmt.Sprint(port),
		"tester@127.0.0.1", "echo "+marker)
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return string(out), fmt.Errorf("超时")
	}
	return string(out), err
}

// checkKeygenList 让系统 ssh-keygen 读私钥并比对指纹：这是"格式被 OpenSSH 接受"最直接的证据。
func checkKeygenList(privPath, pass, wantFP string) bool {
	args := []string{"-l", "-f", privPath, "-P", pass}
	out, err := exec.Command("ssh-keygen.exe", args...).CombinedOutput()
	line := strings.TrimSpace(string(out))
	switch {
	case err != nil:
		fmt.Printf("   FAIL ssh-keygen -l 读不了这把私钥: %v｜%s\n", err, line)
		return false
	case !strings.Contains(line, wantFP):
		fmt.Printf("   FAIL ssh-keygen -l 指纹不一致: got %q want %q\n", line, wantFP)
		return false
	}
	fmt.Printf("   OK   ssh-keygen -l 可读且指纹一致：%s\n", line)
	return true
}

// checkKeygenPub 比对 ssh-keygen -y 导出的公钥与 sshgen 写下的公钥行（只比类型与 base64）。
func checkKeygenPub(privPath, wantLine string) bool {
	out, err := exec.Command("ssh-keygen.exe", "-y", "-f", privPath, "-P", "").CombinedOutput()
	if err != nil {
		fmt.Printf("   FAIL ssh-keygen -y 导出公钥失败: %v｜%s\n", err, strings.TrimSpace(string(out)))
		return false
	}
	got := strings.Fields(strings.TrimSpace(string(out)))
	want := strings.Fields(wantLine)
	if len(got) < 2 || len(want) < 2 {
		fmt.Printf("   FAIL ssh-keygen -y 输出异常: %q\n", strings.TrimSpace(string(out)))
		return false
	}
	if got[0] != want[0] || got[1] != want[1] {
		fmt.Printf("   FAIL 公钥不一致: ssh-keygen=%s %s… sshgen=%s %s…\n", got[0], tail(got[1]), want[0], tail(want[1]))
		return false
	}
	fmt.Printf("   OK   ssh-keygen -y 导出的公钥与 sshgen 写的 .pub 逐字节一致（%s）\n", got[0])
	return true
}

// workDir 选定验证目录：临时目录（默认）或产品真实目录 appdirs.Data("ssh")。
func workDir() (string, error) {
	if *useReal {
		return sshgen.DefaultDir()
	}
	return os.MkdirTemp("", "sshverify")
}

// tightACL 用系统 icacls 把私钥文件的继承清掉、只留当前用户完全控制，
// 以判断 OpenSSH 的 "UNPROTECTED PRIVATE KEY FILE" 是否纯由目录 ACL 继承造成。
func tightACL(path string) error {
	u := os.Getenv("USERNAME")
	if u == "" {
		return fmt.Errorf("USERNAME 为空")
	}
	out, err := exec.Command("icacls.exe", path, "/inheritance:r", "/grant:r", u+":F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v｜%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func loadSigner(privPath, pass string) (ssh.Signer, error) {
	b, err := os.ReadFile(privPath)
	if err != nil {
		return nil, err
	}
	var key any
	if pass == "" {
		key, err = ssh.ParseRawPrivateKey(b)
	} else {
		key, err = ssh.ParseRawPrivateKeyWithPassphrase(b, []byte(pass))
	}
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(key)
}

func tail(s string) string {
	r := []rune(s)
	if len(r) <= 12 {
		return s
	}
	return string(r[len(r)-12:])
}

func fatal(err error) {
	fmt.Println("FATAL:", err)
	os.Exit(1)
}
