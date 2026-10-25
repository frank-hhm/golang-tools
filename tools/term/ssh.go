package term

// ============================================================
// SSH 会话管理
//
// 一条会话 = 一个 *ssh.Client + 一个带 PTY 的 shell。
// 远端输出经 term:data 事件推给前端（Base64），会话结束推 term:exit。
// 会话按 id 存在进程内表里，前端只拿 id 做写入/改窗口尺寸/关闭。
// ============================================================

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

var emitter func(name string, data ...any)

// SetEmitter 注入事件发射器（绑定 Wails v3 app.Event.Emit）
func SetEmitter(fn func(name string, data ...any)) {
	emitter = fn
}

func emit(name string, payload any) {
	if emitter != nil {
		emitter(name, payload)
	}
}

// session 一条终端会话：前端写入走 write，改尺寸走 resize，关闭走 shut
type session struct {
	info   SessionInfo
	write  io.Writer
	resize func(cols, rows int) error
	shut   func()
	once   sync.Once
}

var (
	sessMu   sync.Mutex
	sessions = map[string]*session{}
	sessSeq  int
)

func nextSessionID() string {
	sessSeq++
	return fmt.Sprintf("t%d", sessSeq)
}

// emitWriter 把远端输出块转成 term:data 事件；返回 len 以免 ssh 层把它当短写
type emitWriter struct{ id string }

func (w emitWriter) Write(p []byte) (int, error) {
	if len(p) > 0 {
		emit("term:data", DataEvent{SessionID: w.id, Data: base64.StdEncoding.EncodeToString(p)})
	}
	return len(p), nil
}

// OpenSSH 按记录建立 SSH 会话并起一个交互 shell
func OpenSSH(hostID string, cols, rows int) (SessionInfo, error) {
	h, pass, err := credential(hostID)
	if err != nil {
		return SessionInfo{}, err
	}
	if h.Auth == AuthKey && pass == "" && h.KeyPath == "" {
		return SessionInfo{}, fmt.Errorf("该记录缺少私钥文件，请重新编辑")
	}
	if h.Auth == AuthPassword && pass == "" {
		return SessionInfo{}, fmt.Errorf("该记录没有保存密码，请编辑后重新输入")
	}

	cols, rows = clampSize(cols, rows)

	cfg := &ssh.ClientConfig{
		User: h.User,
		// 个人自用工具：不维护 known_hosts，首次连接不做指纹确认
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         12 * time.Second,
	}
	if h.Auth == AuthKey {
		raw, rerr := os.ReadFile(h.KeyPath)
		if rerr != nil {
			return SessionInfo{}, fmt.Errorf("读取私钥失败: %v", rerr)
		}
		signer, perr := ssh.ParsePrivateKey(raw)
		if perr != nil {
			return SessionInfo{}, fmt.Errorf("私钥解析失败（带口令的私钥暂不支持）: %v", perr)
		}
		cfg.Auth = []ssh.AuthMethod{ssh.PublicKeys(signer)}
	} else {
		cfg.Auth = []ssh.AuthMethod{ssh.Password(pass), ssh.KeyboardInteractive(
			func(name, instruction string, questions []string, echos []bool) ([]string, error) {
				// 部分服务端把密码校验走 keyboard-interactive，答案统一填密码
				answers := make([]string, len(questions))
				for i := range answers {
					answers[i] = pass
				}
				return answers, nil
			}),
		}
	}

	addr := net.JoinHostPort(h.Address, strconv.Itoa(h.Port))
	cli, derr := ssh.Dial("tcp", addr, cfg)
	if derr != nil {
		return SessionInfo{}, fmt.Errorf("SSH 连接失败: %v", derr)
	}

	sess, serr := cli.NewSession()
	if serr != nil {
		_ = cli.Close()
		return SessionInfo{}, fmt.Errorf("打开 SSH 会话失败: %v", serr)
	}
	stdin, ierr := sess.StdinPipe()
	if ierr != nil {
		_ = sess.Close()
		_ = cli.Close()
		return SessionInfo{}, fmt.Errorf("获取会话输入失败: %v", ierr)
	}

	id := nextSessionID()
	// 先挂输出再要终端：Shell() 之后远端立刻吐 banner/MOTD，晚挂会丢开头几帧
	sess.Stdout = emitWriter{id: id}
	sess.Stderr = emitWriter{id: id}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 14400, ssh.TTY_OP_OSPEED: 14400}
	if terr := sess.RequestPty("xterm-256color", rows, cols, modes); terr != nil {
		_ = sess.Close()
		_ = cli.Close()
		return SessionInfo{}, fmt.Errorf("申请终端失败: %v", terr)
	}
	if terr := sess.Shell(); terr != nil {
		_ = sess.Close()
		_ = cli.Close()
		return SessionInfo{}, fmt.Errorf("启动 shell 失败: %v", terr)
	}

	s := &session{
		info:  SessionInfo{ID: id, Title: h.Name, Kind: "ssh"},
		write: stdin,
		resize: func(cols, rows int) error {
			return sess.WindowChange(rows, cols)
		},
	}
	s.shut = func() {
		_ = sess.Close()
		_ = cli.Close()
	}

	sessMu.Lock()
	sessions[id] = s
	sessMu.Unlock()

	go func() {
		werr := sess.Wait()
		closeSession(id, exitReason(werr))
	}()

	return s.info, nil
}

// exitReason 把 ssh.Wait 的错误翻成一句能给用户看的话
func exitReason(err error) string {
	if err == nil {
		return "远端已关闭会话"
	}
	var ee *ssh.ExitError
	if errors.As(err, &ee) {
		if ee.ExitStatus() == 0 {
			return "远端已关闭会话"
		}
		return fmt.Sprintf("远端进程退出（code %d）", ee.ExitStatus())
	}
	if errors.Is(err, io.EOF) {
		return "连接已断开"
	}
	return "连接已断开: " + err.Error()
}

// Write 前端按键 → 远端（Base64 进来，便于原样传递控制字符）
func Write(sessionID, dataB64 string) error {
	sessMu.Lock()
	s := sessions[sessionID]
	sessMu.Unlock()
	if s == nil || s.write == nil {
		return fmt.Errorf("终端会话不存在或已关闭")
	}
	data, err := base64.StdEncoding.DecodeString(dataB64)
	if err != nil {
		return fmt.Errorf("终端输入格式错误: %v", err)
	}
	if len(data) == 0 {
		return nil
	}
	_, werr := s.write.Write(data)
	return werr
}

// Resize 跟随前端终端尺寸变化调整远端 PTY
func Resize(sessionID string, cols, rows int) error {
	sessMu.Lock()
	s := sessions[sessionID]
	sessMu.Unlock()
	if s == nil || s.resize == nil {
		return nil
	}
	cols, rows = clampSize(cols, rows)
	return s.resize(cols, rows)
}

// Close 主动关闭一条会话（会推一条 term:exit 让前端收尾）
func Close(sessionID string) error {
	closeSession(sessionID, "")
	return nil
}

// CloseAll 应用退出时统一收尾
func CloseAll() {
	sessMu.Lock()
	ids := make([]string, 0, len(sessions))
	for id := range sessions {
		ids = append(ids, id)
	}
	sessMu.Unlock()
	for _, id := range ids {
		closeSession(id, "")
	}
}

// closeSession 幂等关闭：从表里摘掉 → 关底层连接 → 推 exit（reason 空表示前端自己关的）
func closeSession(id, reason string) {
	sessMu.Lock()
	s, ok := sessions[id]
	if ok {
		delete(sessions, id)
	}
	sessMu.Unlock()
	if !ok {
		return
	}
	s.once.Do(func() {
		if s.shut != nil {
			s.shut()
		}
	})
	if reason != "" {
		emit("term:exit", ExitEvent{SessionID: id, Reason: reason})
	}
}

// clampSize 终端行列兜底：前端首帧可能量出 0
func clampSize(cols, rows int) (int, int) {
	if cols < 20 {
		cols = 100
	}
	if cols > 500 {
		cols = 500
	}
	if rows < 5 {
		rows = 30
	}
	if rows > 300 {
		rows = 300
	}
	return cols, rows
}
