package ftp

// ============================================================
// SFTP 客户端会话与远端文件操作
//
// 一条会话 = 一个 *ssh.Client + 一个 *sftp.Client；会话按 id 存在进程内表里，
// 前端只拿 id 调目录浏览/上传/下载/删除等操作。
// 远端路径一律用 path（POSIX）处理，不能用 filepath——Windows 的反斜杠
// 拼进远端路径会让服务端当成文件名的一部分。
// ============================================================

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

var emitter func(name string, data ...any)

// SetEmitter 注入事件发射器（main.go 里与其它工具共用同一个 emit）
func SetEmitter(fn func(name string, data ...any)) {
	emitter = fn
}

func emit(name string, payload any) {
	if emitter != nil {
		emitter(name, payload)
	}
}

// session 一条 SFTP 会话。mu 只保护目录类操作，不覆盖传输：
// 上传下载各持独立文件句柄，pkg/sftp 本身支持并发请求。
type session struct {
	mu   sync.Mutex
	info SessionInfo
	cfg  Connection
	ssh  *ssh.Client
	cli  *sftp.Client
	once sync.Once
}

var (
	sessMu   sync.Mutex
	sessions = map[string]*session{}
	sessSeq  int
)

func nextSessionID() string {
	sessSeq++
	return fmt.Sprintf("f%d", sessSeq)
}

// OpenSession 按记录建立 SFTP 会话，返回会话句柄
func OpenSession(connID string) (SessionInfo, error) {
	c, pass, err := credential(connID)
	if err != nil {
		return SessionInfo{}, err
	}
	if c.Auth == AuthKey && c.KeyPath == "" {
		return SessionInfo{}, fmt.Errorf("该记录缺少私钥文件，请重新编辑")
	}
	if c.Auth != AuthKey && pass == "" {
		return SessionInfo{}, fmt.Errorf("该记录没有保存密码，请编辑后重新输入")
	}

	cfg := &ssh.ClientConfig{
		User: c.User,
		// 个人自用工具：不维护 known_hosts，首次连接不做指纹确认
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         12 * time.Second,
	}
	if c.Auth == AuthKey {
		raw, rerr := os.ReadFile(c.KeyPath)
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

	addr := net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	sshCli, derr := ssh.Dial("tcp", addr, cfg)
	if derr != nil {
		return SessionInfo{}, fmt.Errorf("SFTP 连接失败: %v", derr)
	}
	cli, serr := sftp.NewClient(sshCli, sftp.MaxPacket(32*1024))
	if serr != nil {
		_ = sshCli.Close()
		return SessionInfo{}, fmt.Errorf("SFTP 握手失败: %v", serr)
	}

	home := cleanRemote(c.RemoteDir)
	if strings.TrimSpace(c.RemoteDir) == "" {
		if wd, werr := cli.Getwd(); werr == nil && wd != "" {
			home = cleanRemote(wd)
		} else {
			home = "/"
		}
	}

	id := nextSessionID()
	s := &session{
		info: SessionInfo{
			ID:    id,
			Title: c.Name,
			Host:  c.Host,
			Port:  c.Port,
			User:  c.User,
			Home:  home,
		},
		cfg: c,
		ssh: sshCli,
		cli: cli,
	}

	sessMu.Lock()
	sessions[id] = s
	sessMu.Unlock()

	// 远端断开时通知前端；主动 Close 时 closeSession 已经先把会话摘掉，
	// 这里再进来会拿不到 s，自然不重复发事件。
	go func() {
		_ = sshCli.Wait()
		closeSession(id, "连接已断开")
	}()

	return s.info, nil
}

// CloseSession 主动关闭会话（不再推 ftp:session，前端自己知道）
func CloseSession(sessionID string) error {
	closeSession(sessionID, "")
	return nil
}

// CloseAll 应用退出时统一收尾：先断开客户端会话，再停服务端监听
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
	_ = StopServer()
}

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
		_ = s.cli.Close()
		_ = s.ssh.Close()
	})
	if reason != "" {
		emit(EventSession, SessionEvent{SessionID: id, Reason: reason})
	}
}

func getSession(id string) (*session, error) {
	sessMu.Lock()
	s := sessions[id]
	sessMu.Unlock()
	if s == nil {
		return nil, fmt.Errorf("会话不存在或已关闭")
	}
	return s, nil
}

// ListDir 浏览远端目录（dir 为空时用登录后的起始目录）
func ListDir(sessionID, dir string) (Listing, error) {
	s, err := getSession(sessionID)
	if err != nil {
		return Listing{}, err
	}
	if strings.TrimSpace(dir) == "" {
		dir = s.info.Home
	}
	dir = cleanRemote(dir)

	s.mu.Lock()
	infos, rerr := s.cli.ReadDir(dir)
	s.mu.Unlock()
	if rerr != nil {
		return Listing{}, fmt.Errorf("打开目录 %s 失败: %v", dir, rerr)
	}

	out := Listing{Path: dir, Parts: remoteParts(dir)}
	if parent := path.Dir(dir); parent != dir {
		out.Parent = parent
	}
	out.Entries = make([]Entry, 0, len(infos))
	for _, info := range infos {
		name := info.Name()
		if name == "." || name == ".." {
			continue
		}
		e := Entry{
			Name:    name,
			Path:    path.Join(dir, name),
			IsDir:   info.IsDir(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
			Mode:    fmt.Sprintf("%03o", info.Mode().Perm()),
			Hidden:  strings.HasPrefix(name, "."),
			Link:    info.Mode()&os.ModeSymlink != 0,
			Ext:     strings.ToLower(strings.TrimPrefix(path.Ext(name), ".")),
		}
		if !e.IsDir {
			e.Size = info.Size()
			out.Files++
			out.Size += e.Size
		} else {
			out.Dirs++
		}
		out.Entries = append(out.Entries, e)
	}
	// 目录在前、文件在后，各自按名称（忽略大小写）排序
	sort.SliceStable(out.Entries, func(i, j int) bool {
		a, b := out.Entries[i], out.Entries[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return out, nil
}

// Mkdir 在 dir 下新建一层目录
func Mkdir(sessionID, dir, name string) error {
	s, err := getSession(sessionID)
	if err != nil {
		return err
	}
	if err := validateName(name); err != nil {
		return err
	}
	target := path.Join(cleanRemote(dir), strings.TrimSpace(name))
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.cli.Mkdir(target); err != nil {
		return fmt.Errorf("新建目录失败: %v", err)
	}
	return nil
}

// Rename 重命名（保持所在目录不变）
func Rename(sessionID, target, newName string) error {
	s, err := getSession(sessionID)
	if err != nil {
		return err
	}
	if err := validateName(newName); err != nil {
		return err
	}
	src := cleanRemote(target)
	dst := path.Join(path.Dir(src), strings.TrimSpace(newName))
	if src == dst {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.cli.Rename(src, dst); err != nil {
		return fmt.Errorf("重命名失败: %v", err)
	}
	return nil
}

// Remove 删除文件或空目录，返回成功个数。非空目录直接报错，避免误删整棵树。
func Remove(sessionID string, targets []string) (int, error) {
	s, err := getSession(sessionID)
	if err != nil {
		return 0, err
	}
	done := 0
	for _, t := range targets {
		p := cleanRemote(t)
		s.mu.Lock()
		info, lerr := s.cli.Lstat(p)
		if lerr == nil && info.IsDir() {
			entries, derr := s.cli.ReadDir(p)
			if derr == nil && len(entries) > 0 {
				s.mu.Unlock()
				return done, fmt.Errorf("目录 %s 不为空，请先清空后再删除", path.Base(p))
			}
			lerr = s.cli.RemoveDirectory(p)
		} else if lerr == nil {
			lerr = s.cli.Remove(p)
		}
		s.mu.Unlock()
		if lerr != nil {
			return done, fmt.Errorf("删除 %s 失败: %v", path.Base(p), lerr)
		}
		done++
	}
	return done, nil
}

// 预览限额：文本 1MB、图片 8MB，超出只给提示，不把大文件塞进事件
const (
	previewTextMax  = 1 << 20
	previewImageMax = 8 << 20
)

var imageTypes = map[string]string{
	"png":  "image/png",
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"gif":  "image/gif",
	"webp": "image/webp",
	"bmp":  "image/bmp",
	"ico":  "image/x-icon",
}

// PreviewFile 读取远端文件预览：文本原样返回，图片转 data URL，其余提示二进制
func PreviewFile(sessionID, filePath string) (Preview, error) {
	s, err := getSession(sessionID)
	if err != nil {
		return Preview{}, err
	}
	p := cleanRemote(filePath)

	s.mu.Lock()
	info, serr := s.cli.Stat(p)
	s.mu.Unlock()
	if serr != nil {
		return Preview{}, fmt.Errorf("读取文件信息失败: %v", serr)
	}
	if info.IsDir() {
		return Preview{}, fmt.Errorf("目录不能预览")
	}
	size := info.Size()
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(p), "."))

	s.mu.Lock()
	f, oerr := s.cli.Open(p)
	s.mu.Unlock()
	if oerr != nil {
		return Preview{}, fmt.Errorf("打开文件失败: %v", oerr)
	}
	defer f.Close()

	if imgType, ok := imageTypes[ext]; ok {
		if size > previewImageMax {
			return Preview{Kind: "tooLarge", Size: size}, nil
		}
		buf, rerr := io.ReadAll(io.LimitReader(f, previewImageMax))
		if rerr != nil {
			return Preview{}, fmt.Errorf("读取图片失败: %v", rerr)
		}
		return Preview{
			Kind: "image",
			Data: "data:" + imgType + ";base64," + base64.StdEncoding.EncodeToString(buf),
			Size: size,
		}, nil
	}

	buf, rerr := io.ReadAll(io.LimitReader(f, previewTextMax+1))
	if rerr != nil {
		return Preview{}, fmt.Errorf("读取文件失败: %v", rerr)
	}
	truncated := len(buf) > previewTextMax
	if truncated {
		buf = buf[:previewTextMax]
	}
	// 含 NUL 或不是合法 UTF-8 一律按二进制处理，避免前端渲染花屏
	if bytes.IndexByte(buf, 0) >= 0 || !utf8.Valid(buf) {
		return Preview{Kind: "binary", Size: size}, nil
	}
	return Preview{Kind: "text", Data: string(buf), Size: size, Truncated: truncated}, nil
}

// cleanRemote 规范化远端路径：统一正斜杠、强制绝对路径、折叠 . 与 ..
func cleanRemote(p string) string {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return "/"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	c := path.Clean(p)
	if c == "." || c == "" {
		return "/"
	}
	return c
}

// remoteParts 把路径拆成面包屑（从根开始）
func remoteParts(dir string) []Part {
	if dir == "/" {
		return []Part{{Name: "/", Path: "/"}}
	}
	parts := []Part{{Name: "/", Path: "/"}}
	cur := ""
	for _, seg := range strings.Split(strings.Trim(dir, "/"), "/") {
		if seg == "" {
			continue
		}
		cur += "/" + seg
		parts = append(parts, Part{Name: seg, Path: cur})
	}
	return parts
}

// validateName 新建/改名时的名称校验（远端是 POSIX，但不能让路径分隔符混进来）
func validateName(name string) error {
	n := strings.TrimSpace(name)
	if n == "" || n == "." || n == ".." {
		return fmt.Errorf("名称不能为空")
	}
	if strings.ContainsAny(n, "/\\") {
		return fmt.Errorf(`名称不能包含 / 或 \`)
	}
	if strings.ContainsRune(n, 0) {
		return fmt.Errorf("名称包含非法字符")
	}
	return nil
}
