// Package docspace 文档中心的元数据与统一文件源（docref）。
//
// 两条主线：
//   - docref（本文件）：一条引用 = 源类型 + 会话 + 路径，把「读 / 写 / Range / Stat」
//     对 local 与 sftp 统一分发，于是预览、编辑、解压、播放对多种源共用一套代码；
//     磁盘管理与文件传输只需交出一个 ref。webdav / cloud 本期只读预览（经 net/http 或
//     clouddrive provider 把远程文件拉回内存，包装成统一的只读 File）。
//   - store（store.go）：分类、标签、版本索引、回收站、加密登记、分享、备份计划的
//     JSON 持久化。文件本体原地不动，只有快照/密文/回收站实体落 data/docspace/。
//
// 全平台零 cgo：Linux 面板里 local 源浏览的是服务器本机文件（已在登录墙后），口径与
// 磁盘工具一致；sftp 源经 tools/ftp 的会话访问器。
package docspace

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"Tools/services/sysx"
	"Tools/tools/clouddrive"
	"Tools/tools/ftp"
)

// 源类型常量。SrcLocal 是缺省值，保证旧的 ?path= 调用（无 src）继续可用。
const (
	SrcLocal  = "local"
	SrcSFTP   = "sftp"
	SrcWebDAV = "webdav"
	SrcCloud  = "cloud"
)

// File 是一个已打开文件的最小操作集：预览(Read/Seek)、Range(ServeContent 走 ReaderAt)、
// 写回(Write)、取元信息(Stat/Name)。local 由 *os.File 满足，sftp 由 ftp.RemoteFile 满足。
type File interface {
	io.ReadWriteCloser
	io.ReaderAt
	io.Seeker
	Stat() (os.FileInfo, error)
	Name() string
}

// Ref 统一文件源引用。Path 对 local 是本机绝对/相对路径，对 sftp 是远端 POSIX 路径。
type Ref struct {
	Src       string `json:"src"`
	SessionID string `json:"sid"`
	Path      string `json:"path"`
}

// Parse 从三元组构造并校验 Ref。src 空视为 local（向后兼容）。
// local 要求 path 非空；sftp 额外要求会话 id 非空（没会话连不上远端）。
// webdav 要求 path 是合法 http(s) URL；cloud 要求账号 id 与文件 id 均非空。
// 此处只校验三元组、不建连（不静默当 local，避免读错源）。
func Parse(src, sid, p string) (Ref, error) {
	r := Ref{
		Src:       strings.ToLower(strings.TrimSpace(src)),
		SessionID: strings.TrimSpace(sid),
		Path:      strings.TrimSpace(p),
	}
	if r.Src == "" {
		r.Src = SrcLocal
	}
	switch r.Src {
	case SrcLocal:
		if r.Path == "" {
			return Ref{}, fmt.Errorf("local 源缺少 path")
		}
	case SrcSFTP:
		if r.SessionID == "" {
			return Ref{}, fmt.Errorf("sftp 源缺少会话 id")
		}
		if r.Path == "" {
			return Ref{}, fmt.Errorf("sftp 源缺少 path")
		}
	case SrcWebDAV:
		// webdav 本期只读预览：Path 是完整的 http(s) URL（可内嵌 user:pass 基本认证）。
		if r.Path == "" {
			return Ref{}, fmt.Errorf("webdav 源缺少 URL")
		}
		if err := checkWebDAVURL(r.Path); err != nil {
			return Ref{}, err
		}
	case SrcCloud:
		// 云盘源（Google Drive，经 tools/clouddrive）：SessionID 是账号 id，Path 是云端文件 id。
		// 本期只读预览/取流（写回与直链下载留给后续 PR）。
		if r.SessionID == "" {
			return Ref{}, fmt.Errorf("cloud 源缺少账号 id")
		}
		if r.Path == "" {
			return Ref{}, fmt.Errorf("cloud 源缺少文件 id")
		}
	default:
		return Ref{}, fmt.Errorf("未知源类型: %s", r.Src)
	}
	return r, nil
}

// ParseQuery 从 URL 查询参数解析 Ref（media 端点与前端拼链接共用）。
func ParseQuery(q url.Values) (Ref, error) {
	return Parse(q.Get("src"), q.Get("sid"), q.Get("path"))
}

// Query 回填成 URL 查询参数。local 也显式写 src，便于前端与端点统一按三元组处理。
func (r Ref) Query() url.Values {
	v := url.Values{}
	src := r.Src
	if src == "" {
		src = SrcLocal
	}
	v.Set("src", src)
	if r.SessionID != "" {
		v.Set("sid", r.SessionID)
	}
	v.Set("path", r.Path)
	return v
}

// String 人类可读形式（日志用）：local:C:\a\b、sftp#f3:/x/y。
// webdav 会抹掉 URL 里的凭证，避免日志汄露 user:pass。
func (r Ref) String() string {
	src := r.Src
	if src == "" {
		src = SrcLocal
	}
	if src == SrcSFTP {
		return src + "#" + r.SessionID + ":" + r.Path
	}
	if src == SrcWebDAV {
		return src + ":" + redactURL(r.Path)
	}
	if src == SrcCloud {
		return src + "#" + r.SessionID + ":" + r.Path
	}
	return src + ":" + r.Path
}

// open 按 flag 打开文件并按源分发。
func (r Ref) open(flag int) (File, error) {
	switch r.Src {
	case SrcLocal:
		abs, err := filepath.Abs(r.Path)
		if err != nil {
			return nil, err
		}
		f, err := os.OpenFile(abs, flag, 0o644)
		if err != nil {
			return nil, err
		}
		return f, nil
	case SrcSFTP:
		return ftp.OpenRemoteFile(r.SessionID, r.Path, flag)
	case SrcWebDAV:
		// webdav 只读：只允 O_RDONLY（预览/Range）；写回类 flag 一律不支持。
		if flag&(os.O_RDWR|os.O_WRONLY|os.O_APPEND) != 0 {
			return nil, fmt.Errorf("webdav 源本期只读，不支持写回: %w", sysx.ErrUnsupported)
		}
		return httpFetch(r.Path)
	case SrcCloud:
		// 云盘只读：经 provider.Open 把整文件读进内存包装成 memFile（预览/Range）。
		if flag&(os.O_RDWR|os.O_WRONLY|os.O_APPEND) != 0 {
			return nil, fmt.Errorf("cloud 源本期只读，不支持写回: %w", sysx.ErrUnsupported)
		}
		return cloudFetch(r.SessionID, r.Path)
	default:
		return nil, sysx.ErrUnsupported
	}
}

// OpenRead 只读打开（预览 / Range 流式）。
func (r Ref) OpenRead() (File, error) { return r.open(os.O_RDONLY) }

// OpenWrite 读写打开（编辑写回）：要求文件已存在，新建走磁盘工具，避免误凭空造文件。
func (r Ref) OpenWrite() (File, error) {
	if r.Src == SrcLocal {
		abs, err := filepath.Abs(r.Path)
		if err != nil {
			return nil, err
		}
		if st, err := os.Stat(abs); err != nil {
			return nil, err
		} else if st.IsDir() {
			return nil, fmt.Errorf("目标是目录，不能写入: %s", abs)
		}
	}
	return r.open(os.O_RDWR)
}

// Stat 取文件元信息（打开后 Stat 再关，跨源统一）。
func (r Ref) Stat() (os.FileInfo, error) {
	f, err := r.OpenRead()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

// ReadAll 整体读取文件字节（版本快照用）。maxBytes>0 时超过即拒绝，避免把超大文件
// 一口吃进内存；local 与 sftp 都可读，走统一的 OpenRead + io.ReadAll。
func (r Ref) ReadAll(maxBytes int64) ([]byte, error) {
	f, err := r.OpenRead()
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if maxBytes > 0 {
		if st, err := f.Stat(); err == nil && st.Size() > maxBytes {
			return nil, fmt.Errorf("文件过大（%d 字节 > 上限 %d），不纳入快照", st.Size(), maxBytes)
		}
	}
	return io.ReadAll(f)
}

// WriteAll 用给定字节整体覆盖写回已存在的文件（版本还原用）。要求目标已存在且非目录，
// 与 OpenWrite 同口径，避免凭空造文件。local 完整支持；sftp 依赖远端对 O_TRUNC 的实现，
// 尽力而为（真正的远端在线编辑在 P2 走 docref 统一写回）。
func (r Ref) WriteAll(data []byte) error {
	if r.Src == SrcLocal {
		abs, err := filepath.Abs(r.Path)
		if err != nil {
			return err
		}
		if st, err := os.Stat(abs); err != nil {
			return err
		} else if st.IsDir() {
			return fmt.Errorf("目标是目录，不能写入: %s", abs)
		}
	}
	f, err := r.open(os.O_WRONLY | os.O_TRUNC)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// ============================================================
// WebDAV 只读分支（P4）：用 stdlib net/http 把远程文件读进来供预览/Range。
// 本期不实现写回与云盘 provider（那需 M6–M9 的 provider+凭证体系）。
// 凭证只从调用者自己提供的 URL userinfo 里取（本地预览工具，由用户自行控制）。
// ============================================================

// maxWebDAVSize 单次拉取上限：webdav 读是把整文件进内存，超限拒绝避免 OOM。
const maxWebDAVSize = 256 << 20

var webdavClient = &http.Client{Timeout: 30 * time.Second}

// checkWebDAVURL 校验 Path 是 http(s) 绝对 URL。
func checkWebDAVURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("webdav URL 不合法: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("webdav 仅支持 http(s)，got %q", u.Scheme)
	}
	if u.Host == "" {
		return fmt.Errorf("webdav URL 缺少主机名")
	}
	return nil
}

// redactURL 抹掉 URL 里的 user:pass，专用于日志。
func redactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return raw
	}
	u.User = url.User(u.User.Username()) // 只留用户名，密码一律抹掉
	return u.String()
}

// httpFetch 用 GET 拉回整个远程文件（带可选基本认证），包装成只读 File。
func httpFetch(raw string) (File, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	if pw, ok := u.User.Password(); ok {
		req.SetBasicAuth(u.User.Username(), pw)
	}
	resp, err := webdavClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("webdav 拉取失败: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("webdav 文件不存在: %s", redactURL(raw))
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("webdav 拉取异常状态: %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxWebDAVSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxWebDAVSize {
		return nil, fmt.Errorf("webdav 文件过大（>%d MB），本期不支持预览", maxWebDAVSize>>20)
	}
	name := path.Base(u.Path)
	if name == "" || name == "/" || name == "." {
		name = u.Host
	}
	return &memFile{name: name, data: data}, nil
}

// memFile 把内存字节适配成跨源统一的 File（只读）：Read/Seek/ReadAt/Stat，写一律 ErrUnsupported。
type memFile struct {
	name string
	data []byte
	r    *bytes.Reader
}

func (m *memFile) reader() *bytes.Reader {
	if m.r == nil {
		m.r = bytes.NewReader(m.data)
	}
	return m.r
}

func (m *memFile) Read(p []byte) (int, error) { return m.reader().Read(p) }
func (m *memFile) ReadAt(p []byte, off int64) (int, error) {
	return m.reader().ReadAt(p, off)
}
func (m *memFile) Seek(offset int64, whence int) (int64, error) {
	return m.reader().Seek(offset, whence)
}
func (m *memFile) Write([]byte) (int, error) { return 0, sysx.ErrUnsupported }
func (m *memFile) Close() error              { return nil }
func (m *memFile) Name() string              { return m.name }
func (m *memFile) Stat() (os.FileInfo, error) {
	return memInfo{name: m.name, size: int64(len(m.data))}, nil
}

// memInfo 一个只读远程文件的合成 FileInfo（无真实 mtime）。
type memInfo struct {
	name string
	size int64
}

func (i memInfo) Name() string       { return i.name }
func (i memInfo) Size() int64        { return i.size }
func (i memInfo) Mode() os.FileMode  { return 0o444 }
func (i memInfo) ModTime() time.Time { return time.Time{} }
func (i memInfo) IsDir() bool        { return false }
func (i memInfo) Sys() any           { return nil }

// ============================================================
// 云盘只读分支（docspace P4 / 云盘计划 PR1）：经 tools/clouddrive 的 provider
// 把远程文件读进内存，包装成统一的只读 File。写回与直链下载留给后续 PR。
// 体积上限沿用 maxWebDAVSize：超限拒绝，避免把大文件一口吃进内存。
// ============================================================

// cloudFetch 用账号 provider 打开云端文件并整段读回，包成 memFile。
func cloudFetch(acctID, fileID string) (File, error) {
	p, err := clouddrive.New(acctID)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	rc, _, err := p.Open(ctx, fileID)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxWebDAVSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxWebDAVSize {
		return nil, fmt.Errorf("云盘文件过大（>%d MB），本期不支持预览", maxWebDAVSize>>20)
	}
	name := fileID
	if base := path.Base(fileID); base != "" && base != "." && base != "/" {
		name = base
	}
	return &memFile{name: name, data: data}, nil
}
