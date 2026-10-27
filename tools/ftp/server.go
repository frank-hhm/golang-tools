package ftp

// ============================================================
// 内嵌 SFTP 服务端
//
// 一条链路：net.Listener → ssh.ServerConn（密码校验 bcrypt）
//          → session 通道 → subsystem "sftp" → pkg/sftp.RequestServer
//          → sandboxHandler（os.Root，账号根目录即沙箱根）
//
// 进程内单实例：同时只跑一个监听，端口与「文件传输服务」在 portcheck 里登记，
// 与其它工具实例的端口查重共用同一套判定。
// ============================================================

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go-tools/services/applog"
	"go-tools/tools/portcheck"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/crypto/ssh"
)

// 服务端端口在端口查重里的登记键
const serverPortKey = "ftp:server"

// srvConn 一条 SSH 连接
type srvConn struct {
	info ServerConn
	conn *ssh.ServerConn
}

// serverRuntime 进程内服务端状态（不落盘，重启应用即重置）
type serverRuntime struct {
	mu        sync.Mutex
	ln        net.Listener
	cfg       ServerConfig
	root      string
	startedAt int64
	errText   string
	conns     map[string]*srvConn
	seq       int
	logs      []ServerLog
	logSeq    int64
	stopping  atomic.Bool
}

var srv = &serverRuntime{conns: map[string]*srvConn{}}

// handshaking 正在认证（握手 + bcrypt 校验）的连接数，由 serverMaxHandshaking 限流
var handshaking atomic.Int32

func init() {
	// 已登记端口参与全工具查重：即使服务停着，别的实例也不该抢这个端口
	portcheck.Register(func() []portcheck.Owner {
		cfg, err := LoadServerConfig()
		if err != nil || cfg.Port <= 0 {
			return nil
		}
		return []portcheck.Owner{{
			Key:   serverPortKey,
			Label: "文件传输服务",
			Ports: []int{cfg.Port},
		}}
	})
}

func (s *serverRuntime) logf(level, format string, args ...any) {
	text := fmt.Sprintf(format, args...)
	entry := ServerLog{Time: time.Now().Format("2006-01-02 15:04:05"), Level: level, Text: text}

	s.mu.Lock()
	s.logSeq++
	entry.Seq = s.logSeq
	s.logs = append(s.logs, entry)
	if len(s.logs) > serverLogLimit {
		s.logs = s.logs[len(s.logs)-serverLogLimit:]
	}
	s.mu.Unlock()

	emit(EventServerLog, entry)
	switch level {
	case LogError:
		applog.Errorf("ftp", "%s", text)
	case LogWarn:
		applog.Warnf("ftp", "%s", text)
	default:
		applog.Infof("ftp", "%s", text)
	}
}

func (s *serverRuntime) setErr(msg string) {
	s.mu.Lock()
	s.errText = msg
	s.mu.Unlock()
}

/* ==================== 启动 / 停止 ==================== */

// StartServer 按落盘配置启动服务端；已运行则直接返回当前状态
func StartServer() (ServerStatus, error) {
	srv.mu.Lock()
	running := srv.ln != nil
	srv.mu.Unlock()
	if running {
		return GetServerStatus(), nil
	}

	cfg, err := LoadServerConfig()
	if err != nil {
		return GetServerStatus(), err
	}
	root, err := ServerRoot(cfg)
	if err != nil {
		srv.setErr(err.Error())
		return GetServerStatus(), err
	}
	signer, err := hostKeySigner()
	if err != nil {
		srv.setErr(err.Error())
		return GetServerStatus(), err
	}
	if msg := portcheck.Taken(cfg.Port, serverPortKey); msg != "" {
		srv.setErr(msg)
		srv.logf(LogError, "启动失败：%s", msg)
		return GetServerStatus(), errors.New(msg)
	}

	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	ln, lerr := net.Listen("tcp", addr)
	if lerr != nil {
		msg := fmt.Sprintf("监听 %s 失败: %v", addr, lerr)
		srv.setErr(msg)
		srv.logf(LogError, "启动失败：%s", msg)
		return GetServerStatus(), errors.New(msg)
	}

	srv.mu.Lock()
	srv.ln = ln
	srv.cfg = cfg
	srv.root = root
	srv.startedAt = time.Now().Unix()
	srv.errText = ""
	srv.stopping.Store(false)
	srv.mu.Unlock()

	srv.logf(LogInfo, "服务已启动：%s（根目录 %s）", ln.Addr().String(), root)
	if enabledAccountCount() == 0 {
		srv.logf(LogWarn, "当前没有启用中的账号，客户端无法登录")
	}
	go srv.acceptLoop(ln, signer)

	st := GetServerStatus()
	emit(EventServer, st)
	return st, nil
}

// StopServer 停止监听并断开所有在线连接
func StopServer() error {
	srv.stopping.Store(true)
	srv.mu.Lock()
	ln := srv.ln
	conns := make([]*srvConn, 0, len(srv.conns))
	for _, c := range srv.conns {
		conns = append(conns, c)
	}
	srv.ln = nil
	srv.conns = map[string]*srvConn{}
	srv.startedAt = 0
	srv.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}
	for _, c := range conns {
		_ = c.conn.Close()
	}
	if ln != nil {
		srv.logf(LogInfo, "服务已停止")
		emit(EventServer, GetServerStatus())
	}
	return nil
}

// StartAutoServer 应用启动时按配置自动拉起（失败只记日志，不阻断启动）
func StartAutoServer() {
	cfg, err := LoadServerConfig()
	if err != nil || !cfg.AutoStart {
		return
	}
	if _, err := StartServer(); err != nil {
		srv.logf(LogWarn, "自动启动失败：%v", err)
	}
}

// acceptLoop 接受连接直到监听器关闭
func (s *serverRuntime) acceptLoop(ln net.Listener, signer ssh.Signer) {
	for {
		nc, err := ln.Accept()
		if err != nil {
			if s.stopping.Load() || errors.Is(err, net.ErrClosed) {
				return
			}
			s.logf(LogError, "监听中断：%v", err)
			s.setErr("监听中断: " + err.Error())
			// 监听器已经不可用，把状态收回去，避免界面显示"运行中"却连不上
			s.mu.Lock()
			if s.ln == ln {
				s.ln = nil
				s.startedAt = 0
			}
			s.mu.Unlock()
			emit(EventServer, GetServerStatus())
			return
		}
		go s.handleConn(nc, signer)
	}
}

/* ==================== 连接与认证 ==================== */

// dummyPassHash 账号不存在时也跑一次 bcrypt 用的固定假哈希，首次使用时才生成。
var dummyPassHash = sync.OnceValue(func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("go-tools-sftp-dummy"), bcrypt.DefaultCost)
	if err != nil {
		return nil // 生成失败就退化成不比对：功能优先，不影响正常登录
	}
	return h
})

// handleConn 一条 SSH 连接的生命周期
func (s *serverRuntime) handleConn(nc net.Conn, signer ssh.Signer) {
	ip := clientIP(nc.RemoteAddr())

	// 认证阶段先过并发闸门：这里挡在 bcrypt 之前，超出的连接直接关掉，
	// 避免被并发试密码打满 CPU（默认监听 0.0.0.0，局域网/公网都能碰）
	if n := handshaking.Add(1); n > serverMaxHandshaking {
		handshaking.Add(-1)
		s.logf(LogWarn, "来自 %s 的连接被拒绝：认证并发已达上限 %d", ip, serverMaxHandshaking)
		_ = nc.Close()
		return
	}
	defer handshaking.Add(-1)

	_ = nc.SetDeadline(time.Now().Add(30 * time.Second))

	cfg := &ssh.ServerConfig{
		ServerVersion: "SSH-2.0-go-tools-sftp",
		PasswordCallback: func(meta ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
			return s.checkPassword(meta, pass, ip)
		},
	}
	cfg.AddHostKey(signer)

	conn, chans, reqs, err := ssh.NewServerConn(nc, cfg)
	if err != nil {
		s.logf(LogWarn, "来自 %s 的连接握手失败：%v", ip, err)
		_ = nc.Close()
		return
	}
	_ = nc.SetDeadline(time.Time{})
	defer conn.Close()

	acc, ok := accountByUsername(conn.User())
	if !ok || !acc.Enabled {
		s.logf(LogWarn, "拒绝连接 %s@%s：账号不存在或已禁用", conn.User(), ip)
		return
	}

	id, ok := s.addConn(acc, ip, conn)
	if !ok {
		// 停止服务发生在握手期间：不能把这条连接再加回去，否则界面显示“已停止”
		// 却还有人在读写（账号在下发后又被禁用的情况由 serveChannel 复查兜底）
		s.logf(LogWarn, "拒绝连接 %s@%s：服务已停止或在线连接数已达上限 %d", conn.User(), ip, serverMaxConns)
		return
	}
	defer s.removeConn(id)

	go ssh.DiscardRequests(reqs)

	// 单连接同时开的 sftp 通道数也要封顶：每个通道一个 goroutine + 一个 os.Root
	// 句柄，不限的话一条连接就能把进程的文件句柄耗尽
	open := make(chan struct{}, serverMaxChannelsPerConn)
	for newCh := range chans {
		if newCh.ChannelType() != "session" {
			_ = newCh.Reject(ssh.UnknownChannelType, "仅支持 SFTP 会话")
			continue
		}
		select {
		case open <- struct{}{}:
		default:
			s.logf(LogWarn, "拒绝通道 %s@%s：同一连接已开 %d 个会话", conn.User(), ip, serverMaxChannelsPerConn)
			_ = newCh.Reject(ssh.Prohibited, "同一连接打开的会话数已达上限")
			continue
		}
		ch, chReqs, aerr := newCh.Accept()
		if aerr != nil {
			<-open
			continue
		}
		go func(c ssh.Channel, r <-chan *ssh.Request) {
			defer func() { <-open }()
			s.serveChannel(c, r, acc)
		}(ch, chReqs)
	}
}

// checkPassword 密码校验：账号存在 → 启用 → bcrypt 比对
func (s *serverRuntime) checkPassword(meta ssh.ConnMetadata, pass []byte, ip string) (*ssh.Permissions, error) {
	rec, ok := findAccount(meta.User())
	if !ok || rec.PassHash == "" {
		// 账号不存在时也做一次同代价的 bcrypt 比对：只在“存在/不存在”分支上
		// 不同会让响应时间差出一次哈希（约 50-100ms），可被拿来枚举有效用户名
		if h := dummyPassHash(); h != nil {
			_ = bcrypt.CompareHashAndPassword(h, pass)
		}
		s.logf(LogWarn, "登录失败 %s@%s：账号不存在", meta.User(), ip)
		return nil, errors.New("用户名或密码错误")
	}
	if !rec.Enabled {
		s.logf(LogWarn, "登录失败 %s@%s：账号已禁用", rec.Username, ip)
		return nil, errors.New("该账号已被禁用")
	}
	if bcrypt.CompareHashAndPassword([]byte(rec.PassHash), pass) != nil {
		s.logf(LogWarn, "登录失败 %s@%s：密码错误", rec.Username, ip)
		return nil, errors.New("用户名或密码错误")
	}
	s.logf(LogInfo, "登录成功 %s@%s", rec.Username, ip)
	return nil, nil
}

// serveChannel 处理一个 session 通道：只认 subsystem=sftp
func (s *serverRuntime) serveChannel(ch ssh.Channel, reqs <-chan *ssh.Request, acc Account) {
	defer ch.Close()

	// 每条通道都按账号“当前”配置建沙箱：连接建立之后才改的只读/根目录，
	// 客户端重新开通道立刻按新策略生效（仍在用的老通道由保存账号时统一断连）
	if cur, ok := accountByUsername(acc.Username); ok {
		acc = cur
	}
	if !acc.Enabled {
		s.logf(LogWarn, "拒绝会话 %s：账号已禁用", acc.Username)
		return
	}

	cfg, root := s.currentConfig()
	_ = cfg
	abs, err := accountAbs(root, acc.RootDir)
	if err != nil {
		s.logf(LogError, "账号 %s 的目录非法：%v", acc.Username, err)
		return
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		s.logf(LogError, "账号 %s 的目录不可用：%v", acc.Username, err)
		return
	}
	handler, err := newSandboxHandler(abs, acc.ReadOnly)
	if err != nil {
		s.logf(LogError, "打开账号 %s 的目录失败：%v", acc.Username, err)
		return
	}
	defer handler.Close()

	for req := range reqs {
		if req.Type != "subsystem" {
			_ = req.Reply(false, nil)
			continue
		}
		var sub struct{ Name string }
		if err := ssh.Unmarshal(req.Payload, &sub); err != nil || sub.Name != "sftp" {
			_ = req.Reply(false, nil)
			continue
		}
		_ = req.Reply(true, nil)

		rs := sftp.NewRequestServer(ch, sftp.Handlers{
			FileGet:  handler,
			FilePut:  handler,
			FileCmd:  handler,
			FileList: handler,
		})
		serveErr := rs.Serve()
		_ = rs.Close()
		if serveErr != nil && !errors.Is(serveErr, io.EOF) && !errors.Is(serveErr, io.ErrUnexpectedEOF) {
			s.logf(LogWarn, "会话结束（%s）：%v", acc.Username, serveErr)
		}
		return
	}
}

// addConn 注册一条在线连接；服务已停止或连接数已满时返回 false
//
// 运行态校验、上限判定与登记必须在同一把锁里完成：否则“停止服务”
// 期间完成的握手会在这之后又把自己加回连接表，停止状态名不副实。
func (s *serverRuntime) addConn(acc Account, ip string, conn *ssh.ServerConn) (string, bool) {
	s.mu.Lock()
	if s.ln == nil || len(s.conns) >= serverMaxConns {
		s.mu.Unlock()
		return "", false
	}
	s.seq++
	id := fmt.Sprintf("c%d", s.seq)
	s.conns[id] = &srvConn{conn: conn, info: ServerConn{
		ID:          id,
		Username:    acc.Username,
		ClientIP:    ip,
		ConnectedAt: time.Now().Unix(),
		ReadOnly:    acc.ReadOnly,
	}}
	s.mu.Unlock()
	emit(EventServer, GetServerStatus())
	return id, true
}

func (s *serverRuntime) removeConn(id string) {
	s.mu.Lock()
	if _, ok := s.conns[id]; ok {
		delete(s.conns, id)
	}
	s.mu.Unlock()
	emit(EventServer, GetServerStatus())
}

func (s *serverRuntime) currentConfig() (ServerConfig, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cfg, s.root
}

/* ==================== 状态与运维 ==================== */

// GetServerStatus 当前状态快照（停止时也带配置里的端口与根目录）
func GetServerStatus() ServerStatus {
	cfg, err := LoadServerConfig()
	if err != nil {
		cfg = normalizeServerConfig(ServerConfig{})
	}
	st := ServerStatus{
		State:     ServerStopped,
		Host:      cfg.Host,
		Port:      cfg.Port,
		AutoStart: cfg.AutoStart,
		Accounts:  enabledAccountCount(),
		LocalIPs:  LANAddrs(),
	}
	if root, rerr := ServerRoot(cfg); rerr == nil {
		st.RootDir = root
	}

	srv.mu.Lock()
	if srv.ln != nil {
		st.State = ServerRunning
		st.Addr = srv.ln.Addr().String()
		st.StartedAt = srv.startedAt
		st.RootDir = srv.root
	}
	st.Error = srv.errText
	conns := make([]ServerConn, 0, len(srv.conns))
	for _, c := range srv.conns {
		conns = append(conns, c.info)
	}
	srv.mu.Unlock()

	sort.Slice(conns, func(i, j int) bool { return conns[i].ConnectedAt < conns[j].ConnectedAt })
	st.Conns = conns
	return st
}

// ListServerLogs 服务端日志（旧在前）
func ListServerLogs() []ServerLog {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	out := make([]ServerLog, len(srv.logs))
	copy(out, srv.logs)
	return out
}

// ListServerConns 在线连接（早连的在前）
func ListServerConns() []ServerConn {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	out := make([]ServerConn, 0, len(srv.conns))
	for _, c := range srv.conns {
		out = append(out, c.info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ConnectedAt < out[j].ConnectedAt })
	return out
}

// KickServerConn 踢掉一条在线连接
func KickServerConn(id string) error {
	srv.mu.Lock()
	c := srv.conns[id]
	srv.mu.Unlock()
	if c == nil {
		return fmt.Errorf("连接不存在或已断开")
	}
	_ = c.conn.Close()
	srv.logf(LogInfo, "已断开连接 %s（%s@%s）", id, c.info.Username, c.info.ClientIP)
	return nil
}

// DisconnectUser 断开某个账号的全部在线连接，返回断开条数（禁用账号时用）
func DisconnectUser(username string) int {
	victims := []*srvConn{}
	srv.mu.Lock()
	for id, c := range srv.conns {
		if strings.EqualFold(c.info.Username, username) {
			victims = append(victims, c)
			delete(srv.conns, id)
		}
	}
	srv.mu.Unlock()
	for _, c := range victims {
		_ = c.conn.Close()
	}
	if len(victims) > 0 {
		srv.logf(LogInfo, "账号 %s 的 %d 条在线连接已断开", username, len(victims))
		emit(EventServer, GetServerStatus())
	}
	return len(victims)
}

// RemoveAccount 删除账号并断开该账号的在线连接
func RemoveAccount(id string) ([]Account, error) {
	list, name, err := deleteAccountRec(id)
	if err != nil {
		return nil, err
	}
	n := DisconnectUser(name)
	srv.logf(LogInfo, "账号 %s 已删除%s", name, map[bool]string{true: "，在线连接已断开", false: ""}[n > 0])
	emit(EventServer, GetServerStatus())
	return list, nil
}

// UpdateServerConfig 保存监听配置；运行中则重启让新端口/根目录生效
func UpdateServerConfig(cfg ServerConfig) (ServerStatus, error) {
	running := false
	srv.mu.Lock()
	running = srv.ln != nil
	srv.mu.Unlock()

	if cfg.Port > 0 && cfg.Port <= 65535 {
		if msg := portcheck.Taken(cfg.Port, serverPortKey); msg != "" {
			return GetServerStatus(), errors.New(msg)
		}
	}
	if _, err := SaveServerConfig(cfg); err != nil {
		return GetServerStatus(), err
	}
	if !running {
		return GetServerStatus(), nil
	}
	_ = StopServer()
	if _, err := StartServer(); err != nil {
		return GetServerStatus(), err
	}
	return GetServerStatus(), nil
}

/* ==================== 主机密钥与工具函数 ==================== */

// hostKeySigner 读取（或首次生成）服务端主机密钥。
// 用 ed25519：生成快、指纹短，客户端第一次连接看到的就是它。
func hostKeySigner() (ssh.Signer, error) {
	dir, err := serverDir()
	if err != nil {
		return nil, err
	}
	p := filepath.Join(dir, "host_key.pem")
	if raw, rerr := os.ReadFile(p); rerr == nil {
		if signer, perr := parseHostKey(raw); perr == nil {
			return signer, nil
		} else {
			srv.logf(LogWarn, "主机密钥无法解析，将重新生成（客户端会提示指纹变化）：%v", perr)
		}
	}

	_, priv, gerr := ed25519.GenerateKey(rand.Reader)
	if gerr != nil {
		return nil, fmt.Errorf("生成主机密钥失败: %v", gerr)
	}
	der, merr := x509.MarshalPKCS8PrivateKey(priv)
	if merr != nil {
		return nil, fmt.Errorf("编码主机密钥失败: %v", merr)
	}
	block := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	if werr := os.WriteFile(p, block, 0o600); werr != nil {
		return nil, fmt.Errorf("保存主机密钥失败: %v", werr)
	}
	srv.logf(LogInfo, "已生成服务端主机密钥：%s", p)
	return ssh.NewSignerFromKey(priv)
}

func parseHostKey(raw []byte) (ssh.Signer, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("不是 PEM 格式")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(key)
}

// LANAddrs 本机局域网 IPv4（用于界面提示"局域网内用这个地址连"）
func LANAddrs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := []string{}
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, aerr := ifi.Addrs()
		if aerr != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || ip4.IsLoopback() || ip4.IsLinkLocalUnicast() || ip4.IsUnspecified() {
				continue
			}
			s := ip4.String()
			if seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// clientIP 从 RemoteAddr 取 IP（去掉端口，IPv6 也兼容）
func clientIP(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return addr.String()
	}
	return host
}
