package ftp

// 服务端测试：沙箱路径、读写权限、账号落盘、真实 SFTP 端到端。
// 这些用例会改写 appdirs 指向的目录，统一先切到 t.TempDir()，不碰真实数据。

import (
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-tools/appdirs"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func useTempData(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	if err := appdirs.Set(filepath.Join(base, "plugins"), filepath.Join(base, "data"), filepath.Join(base, "logs")); err != nil {
		t.Fatalf("切换临时数据目录失败: %v", err)
	}
}

func TestSandboxRel(t *testing.T) {
	h := &sandboxHandler{}
	cases := map[string]string{
		"":              ".",
		"/":             ".",
		"/a/b":          "a/b",
		"/a/./b/../c":   "a/c",
		`\a\b`:          "a/b",
		"//a//b/":       "a/b",
		"/../../../etc": "etc",
	}
	for in, want := range cases {
		got, err := h.rel(in)
		if err != nil {
			t.Fatalf("rel(%q) 报错: %v", in, err)
		}
		if got != want {
			t.Errorf("rel(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

func TestSandboxEscapeBlocked(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(base, "secret.txt")
	if err := os.WriteFile(secret, []byte("top-secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	h, err := newSandboxHandler(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	for _, p := range []string{"/../secret.txt", "/..%2fsecret.txt", "/../root/../secret.txt"} {
		if r, rerr := h.Fileread(sftp.NewRequest("Get", p)); rerr == nil {
			if f, ok := r.(io.Closer); ok {
				_ = f.Close()
			}
			t.Fatalf("路径 %q 不应能读到根目录外的文件", p)
		}
	}

	// 软链也是越界手段：能建就验证一下，建不了（Windows 无权限）就跳过
	if err := os.Symlink(secret, filepath.Join(root, "link.txt")); err == nil {
		if r, rerr := h.Fileread(sftp.NewRequest("Get", "/link.txt")); rerr == nil {
			if f, ok := r.(io.Closer); ok {
				_ = f.Close()
			}
			t.Fatal("不应跟随软链跳出账号根目录")
		}
	}
}

func TestSandboxReadWrite(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	h, err := newSandboxHandler(root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	// 写：真实客户端会带 Write|Creat|Trunc（0x2|0x8|0x10），
	// 这里手动置位，避免只测到「没置位就不建文件」这种非真实路径
	put := sftp.NewRequest("Put", "/hello.txt")
	put.Flags = 0x2 | 0x8 | 0x10
	w, err := h.Filewrite(put)
	if err != nil {
		t.Fatalf("写入打开失败: %v", err)
	}
	if _, werr := w.WriteAt([]byte("hello-sftp"), 0); werr != nil {
		t.Fatalf("写入失败: %v", werr)
	}
	if c, ok := w.(io.Closer); ok {
		_ = c.Close()
	}

	// 读
	r, err := h.Fileread(sftp.NewRequest("Get", "/hello.txt"))
	if err != nil {
		t.Fatalf("读取打开失败: %v", err)
	}
	buf := make([]byte, len("hello-sftp"))
	if _, rerr := r.ReadAt(buf, 0); rerr != nil && rerr != io.EOF {
		t.Fatalf("读取失败: %v", rerr)
	}
	if string(buf) != "hello-sftp" {
		t.Fatalf("内容 = %q", string(buf))
	}

	// 列目录：文件和目录都要在，且名字不含路径前缀
	lister, err := h.Filelist(sftp.NewRequest("List", "/"))
	if err != nil {
		t.Fatalf("列目录失败: %v", err)
	}
	infos := make([]os.FileInfo, 8)
	n, _ := lister.ListAt(infos, 0)
	names := map[string]bool{}
	for _, fi := range infos[:n] {
		names[fi.Name()] = fi.IsDir()
	}
	if isDir, ok := names["hello.txt"]; !ok || isDir {
		t.Fatalf("列目录缺少 hello.txt 或类型不对: %v", names)
	}
	if isDir, ok := names["docs"]; !ok || !isDir {
		t.Fatalf("列目录缺少 docs 或类型不对: %v", names)
	}

	// Stat 单条
	st, err := h.Filelist(sftp.NewRequest("Stat", "/hello.txt"))
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}
	one := make([]os.FileInfo, 1)
	if n, _ := st.ListAt(one, 0); n != 1 || one[0].Name() != "hello.txt" {
		t.Fatalf("Stat 结果不对: %v", one[:n])
	}

	// Mkdir / Rename / Remove
	if err := h.Filecmd(sftp.NewRequest("Mkdir", "/newdir")); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	ren := sftp.NewRequest("Rename", "/hello.txt")
	ren.Target = "/docs/moved.txt"
	if err := h.Filecmd(ren); err != nil {
		t.Fatalf("重命名失败: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "moved.txt")); err != nil {
		t.Fatalf("重命名后文件不在目标位置: %v", err)
	}
	if err := h.Filecmd(sftp.NewRequest("Remove", "/docs/moved.txt")); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	if err := h.Filecmd(sftp.NewRequest("Rmdir", "/newdir")); err != nil {
		t.Fatalf("删目录失败: %v", err)
	}
}

func TestSandboxReadOnly(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	h, err := newSandboxHandler(root, true)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()

	if _, err := h.Filewrite(sftp.NewRequest("Put", "/b.txt")); err == nil {
		t.Fatal("只读账号不应能打开写入")
	}
	if err := h.Filecmd(sftp.NewRequest("Mkdir", "/d")); err == nil {
		t.Fatal("只读账号不应能建目录")
	}
	ren := sftp.NewRequest("Rename", "/a.txt")
	ren.Target = "/c.txt"
	if err := h.Filecmd(ren); err == nil {
		t.Fatal("只读账号不应能重命名")
	}
	if _, err := h.Fileread(sftp.NewRequest("Get", "/a.txt")); err != nil {
		t.Fatalf("只读账号应能读取: %v", err)
	}
}

func TestAccountSaveAndGuard(t *testing.T) {
	useTempData(t)

	if _, _, err := SaveAccount(Account{Username: "u1", Password: "123456", RootDir: "../escape", Enabled: true}); err == nil {
		t.Fatal("账号目录含 .. 必须被拒绝")
	}
	if _, _, err := SaveAccount(Account{Username: "u1", Password: "123", Enabled: true}); err == nil {
		t.Fatal("过短密码必须被拒绝")
	}

	list, id, err := SaveAccount(Account{Username: "alice", Password: "123456", Note: "测试", Enabled: true})
	if err != nil {
		t.Fatalf("保存账号失败: %v", err)
	}
	if len(list) != 1 || list[0].Password != "" {
		t.Fatalf("返回列表不应带明文密码: %+v", list)
	}
	if list[0].RootDir != "alice" {
		t.Fatalf("账号目录默认应为用户名，实际 %q", list[0].RootDir)
	}

	cfg, _ := LoadServerConfig()
	root, err := ServerRoot(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if st, serr := os.Stat(filepath.Join(root, "alice")); serr != nil || !st.IsDir() {
		t.Fatalf("账号目录应已创建: %v", serr)
	}

	rec, ok := findAccount("ALICE")
	if !ok || rec.PassHash == "" {
		t.Fatal("用户名查找应忽略大小写且带哈希")
	}

	// 重名（大小写不同）要拒绝
	if _, _, err := SaveAccount(Account{Username: "ALICE", Password: "123456", Enabled: true}); err == nil {
		t.Fatal("重名账号必须被拒绝")
	}
	// 编辑时留空密码＝沿用旧哈希
	if _, _, err := SaveAccount(Account{ID: id, Username: "alice", RootDir: "alice", Enabled: true}); err != nil {
		t.Fatalf("编辑账号失败: %v", err)
	}
	if rec2, _ := findAccount("alice"); rec2.PassHash != rec.PassHash {
		t.Fatal("留空密码不应改变原密码")
	}
	if _, err := RemoveAccount(id); err != nil {
		t.Fatalf("删除账号失败: %v", err)
	}
	if _, ok := findAccount("alice"); ok {
		t.Fatal("删除后不应还能查到")
	}
}

func TestServerEndToEnd(t *testing.T) {
	useTempData(t)

	if _, _, err := SaveAccount(Account{Username: "alice", Password: "123456", RootDir: "alice", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := SaveAccount(Account{Username: "reader", Password: "123456", RootDir: "shared", ReadOnly: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	if _, err := SaveServerConfig(ServerConfig{Host: "127.0.0.1", Port: port}); err != nil {
		t.Fatal(err)
	}
	st, err := StartServer()
	if err != nil {
		t.Fatalf("启动服务端失败: %v", err)
	}
	defer func() { _ = StopServer() }()
	if st.State != ServerRunning {
		t.Fatalf("状态应为 running，实际 %q", st.State)
	}

	dial := func(user string) (*ssh.Client, *sftp.Client) {
		t.Helper()
		cli, derr := ssh.Dial("tcp", net.JoinHostPort("127.0.0.1", itoa(port)), &ssh.ClientConfig{
			User:            user,
			Auth:            []ssh.AuthMethod{ssh.Password("123456")},
			HostKeyCallback: ssh.InsecureIgnoreHostKey(),
			Timeout:         5 * time.Second,
		})
		if derr != nil {
			t.Fatalf("SSH 连接失败(%s): %v", user, derr)
		}
		sc, serr := sftp.NewClient(cli)
		if serr != nil {
			t.Fatalf("SFTP 握手失败: %v", serr)
		}
		return cli, sc
	}

	cli, sc := dial("alice")
	defer cli.Close()
	defer sc.Close()

	if wd, werr := sc.Getwd(); werr != nil || wd != "/" {
		t.Fatalf("登录目录应为 /，实际 %q (%v)", wd, werr)
	}
	if err := sc.MkdirAll("/sub/dir"); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	f, err := sc.Create("/sub/hello.txt")
	if err != nil {
		t.Fatalf("创建文件失败: %v", err)
	}
	if _, err := f.Write([]byte("end-to-end")); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	_ = f.Close()
	if st, serr := sc.Stat("/sub/hello.txt"); serr != nil || st.Size() != int64(len("end-to-end")) {
		t.Fatalf("Stat 结果不对: %v %v", st, serr)
	}
	if err := sc.Rename("/sub/hello.txt", "/sub/renamed.txt"); err != nil {
		t.Fatalf("重命名失败: %v", err)
	}
	entries, err := sc.ReadDir("/sub")
	if err != nil {
		t.Fatalf("列目录失败: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.Name() == "renamed.txt" {
			found = true
		}
	}
	if !found {
		t.Fatalf("目录里找不到重命名后的文件: %v", entries)
	}
	if err := sc.Remove("/sub/renamed.txt"); err != nil {
		t.Fatalf("删除失败: %v", err)
	}

	// 密码错误必须被拒
	if c, derr := ssh.Dial("tcp", net.JoinHostPort("127.0.0.1", itoa(port)), &ssh.ClientConfig{
		User:            "alice",
		Auth:            []ssh.AuthMethod{ssh.Password("wrong-pass")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	}); derr == nil {
		_ = c.Close()
		t.Fatal("错误密码不应能连上")
	}

	// 只读账号：能读不能写
	rcli, rsc := dial("reader")
	defer rcli.Close()
	defer rsc.Close()
	if _, err := rsc.Create("/nope.txt"); err == nil {
		t.Fatal("只读账号不应能创建文件")
	}

	// 在线连接列表要能看到两条
	conns := ListServerConns()
	if len(conns) < 2 {
		t.Fatalf("在线连接数 = %d，期望至少 2", len(conns))
	}
	for _, c := range conns {
		if strings.TrimSpace(c.ClientIP) == "" || c.Username == "" {
			t.Fatalf("连接信息不完整: %+v", c)
		}
	}
}

// 连接注册必须原子：服务已停止、连接数已满时都不能把握手完成的连接加回连接表
func TestAddConnGuards(t *testing.T) {
	useTempData(t)

	_ = StopServer()
	if _, ok := srv.addConn(Account{Username: "ghost"}, "127.0.0.1", nil); ok {
		t.Fatal("服务停止时不应注册连接")
	}

	// 运行态用一个临时 listener 冒充，把连接表填满
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.mu.Lock()
	srv.ln = ln
	srv.conns = map[string]*srvConn{}
	for i := 0; i < serverMaxConns; i++ {
		srv.conns["full"+itoa(i)] = &srvConn{}
	}
	srv.mu.Unlock()
	defer func() {
		// 测试里填的是空壳连接（conn 为 nil），先清表再停，避免 StopServer 去 Close nil
		srv.mu.Lock()
		srv.conns = map[string]*srvConn{}
		srv.mu.Unlock()
		_ = StopServer()
	}()

	if _, ok := srv.addConn(Account{Username: "ghost"}, "127.0.0.1", nil); ok {
		t.Fatal("连接数已满时不应注册连接")
	}

	srv.mu.Lock()
	delete(srv.conns, "full0")
	srv.mu.Unlock()
	if id, ok := srv.addConn(Account{Username: "ghost"}, "127.0.0.1", nil); !ok || id == "" {
		t.Fatal("腾出空位后应能注册连接")
	}
}

// 账号策略在连接建立之后变更：新开的通道必须按当前策略建沙箱
func TestAccountPolicyTakesEffectOnNewChannel(t *testing.T) {
	useTempData(t)

	_, id, err := SaveAccount(Account{Username: "alice", Password: "123456", RootDir: "alice", Enabled: true})
	if err != nil || id == "" {
		t.Fatalf("建账号失败: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	if _, err := SaveServerConfig(ServerConfig{Host: "127.0.0.1", Port: port}); err != nil {
		t.Fatal(err)
	}
	if _, err := StartServer(); err != nil {
		t.Fatalf("启动服务端失败: %v", err)
	}
	defer func() { _ = StopServer() }()

	cli, err := ssh.Dial("tcp", net.JoinHostPort("127.0.0.1", itoa(port)), &ssh.ClientConfig{
		User:            "alice",
		Auth:            []ssh.AuthMethod{ssh.Password("123456")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("SSH 连接失败: %v", err)
	}
	defer cli.Close()

	sc, err := sftp.NewClient(cli)
	if err != nil {
		t.Fatalf("SFTP 握手失败: %v", err)
	}
	if f, cerr := sc.Create("/ok.txt"); cerr != nil {
		t.Fatalf("读写账号应能建文件: %v", cerr)
	} else {
		_ = f.Close()
	}
	_ = sc.Close()

	// 改成只读：同一连接上新开的通道必须按只读策略建沙箱
	if _, _, err := SaveAccount(Account{ID: id, Username: "alice", RootDir: "alice", ReadOnly: true, Enabled: true}); err != nil {
		t.Fatalf("改只读失败: %v", err)
	}
	ro, err := sftp.NewClient(cli)
	if err != nil {
		t.Fatalf("开新通道失败: %v", err)
	}
	defer ro.Close()
	if f, cerr := ro.Create("/blocked.txt"); cerr == nil {
		_ = f.Close()
		t.Fatal("改成只读后新通道不应能创建文件")
	}
	if _, serr := ro.Stat("/ok.txt"); serr != nil {
		t.Fatalf("只读账号应能读: %v", serr)
	}

	// 禁用：连新会话都不该建起来
	if _, _, err := SaveAccount(Account{ID: id, Username: "alice", RootDir: "alice", ReadOnly: true, Enabled: false}); err != nil {
		t.Fatalf("禁用失败: %v", err)
	}
	if sc2, cerr := sftp.NewClient(cli); cerr == nil {
		_ = sc2.Close()
		t.Fatal("账号禁用后不应还能开新会话")
	}
}

// itoa 端口转字符串（测试里避免多引一个包）
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	buf := [8]byte{}
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	return string(buf[i:])
}
