package ftp

// ============================================================
// SFTP 子系统的沙箱文件访问层
//
// pkg/sftp 的服务端是"请求 → Handler"模型，默认实现直接操作宿主机路径，
// 那样账号就能用 ../ 或软链走出目录。这里全部经 os.Root（Go 1.25）：
// 内核/运行时层面拒绝跨出根的路径与软链，不存在 TOCTOU 空窗。
//
// 相对路径：SFTP 客户端发来的都是"服务端视角的绝对路径"（/a/b），
// 这里先 path.Clean 归一，再剥掉前导斜杠交给 os.Root。
// ============================================================

import (
	"io"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
)

// sandboxHandler 一个账号根目录上的 Handler 集合
type sandboxHandler struct {
	root     *os.Root
	readOnly bool
}

// newSandboxHandler 打开账号根目录。root 必须是已存在的目录。
func newSandboxHandler(root string, readOnly bool) (*sandboxHandler, error) {
	r, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	return &sandboxHandler{root: r, readOnly: readOnly}, nil
}

// Close 释放根目录句柄
func (h *sandboxHandler) Close() error { return h.root.Close() }

// rel SFTP 绝对路径 → os.Root 内的相对路径（根目录为 "."）
func (h *sandboxHandler) rel(p string) (string, error) {
	q := path.Clean("/" + strings.ReplaceAll(strings.TrimSpace(p), "\\", "/"))
	if q == "/" || q == "." {
		return ".", nil
	}
	rel := strings.TrimPrefix(q, "/")
	if rel == "" || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", sftp.ErrSSHFxPermissionDenied
	}
	return rel, nil
}

// denied 只读账号的写操作
func denied() error { return sftp.ErrSSHFxPermissionDenied }

/* ==================== 读 ==================== */

// Fileread 打开文件供下载
func (h *sandboxHandler) Fileread(r *sftp.Request) (io.ReaderAt, error) {
	rel, err := h.rel(r.Filepath)
	if err != nil {
		return nil, err
	}
	return h.root.Open(rel)
}

/* ==================== 写 ==================== */

// Filewrite 打开文件供上传（SFTP 的 Put 路径）
func (h *sandboxHandler) Filewrite(r *sftp.Request) (io.WriterAt, error) {
	if h.readOnly {
		return nil, denied()
	}
	rel, err := h.rel(r.Filepath)
	if err != nil {
		return nil, err
	}
	fl := r.Pflags()
	flags := os.O_WRONLY
	if fl.Creat || fl.Trunc {
		flags |= os.O_CREATE
	}
	if fl.Trunc {
		flags |= os.O_TRUNC
	}
	if fl.Excl {
		flags |= os.O_EXCL
	}
	return h.root.OpenFile(rel, flags, 0o644)
}

// OpenFile 读写同句柄（部分客户端编辑文件时会这样开）
func (h *sandboxHandler) OpenFile(r *sftp.Request) (sftp.WriterAtReaderAt, error) {
	rel, err := h.rel(r.Filepath)
	if err != nil {
		return nil, err
	}
	fl := r.Pflags()
	wantWrite := fl.Write || fl.Append || fl.Creat || fl.Trunc
	if wantWrite && h.readOnly {
		return nil, denied()
	}
	flags := os.O_RDONLY
	if wantWrite {
		if fl.Read {
			flags = os.O_RDWR
		} else {
			flags = os.O_WRONLY
		}
	}
	if fl.Creat {
		flags |= os.O_CREATE
	}
	if fl.Trunc {
		flags |= os.O_TRUNC
	}
	if fl.Excl {
		flags |= os.O_EXCL
	}
	return h.root.OpenFile(rel, flags, 0o644)
}

/* ==================== 目录与元数据 ==================== */

// Filecmd 目录/重命名/属性等一次性命令
func (h *sandboxHandler) Filecmd(r *sftp.Request) error {
	switch r.Method {
	case "Mkdir":
		if h.readOnly {
			return denied()
		}
		rel, err := h.rel(r.Filepath)
		if err != nil {
			return err
		}
		return h.root.Mkdir(rel, 0o755)

	case "Rmdir", "Remove":
		if h.readOnly {
			return denied()
		}
		rel, err := h.rel(r.Filepath)
		if err != nil {
			return err
		}
		return h.root.Remove(rel)

	case "Rename", "PosixRename":
		if h.readOnly {
			return denied()
		}
		from, err := h.rel(r.Filepath)
		if err != nil {
			return err
		}
		to, err := h.rel(r.Target)
		if err != nil {
			return err
		}
		return h.root.Rename(from, to)

	case "Setstat":
		if h.readOnly {
			return denied()
		}
		return h.setstat(r)

	case "Symlink", "Link":
		// 沙箱里不放软链/硬链：它们是走出根目录的经典手段，
		// 而普通用户的传输需求用不到
		return sftp.ErrSSHFxOpUnsupported
	}
	return sftp.ErrSSHFxOpUnsupported
}

// setstat chmod / 改时间 / 截断
func (h *sandboxHandler) setstat(r *sftp.Request) error {
	rel, err := h.rel(r.Filepath)
	if err != nil {
		return err
	}
	af := r.AttrFlags()
	fs := r.Attributes()
	if af.Size && fs != nil {
		f, oerr := h.root.OpenFile(rel, os.O_WRONLY, 0o644)
		if oerr != nil {
			return oerr
		}
		terr := f.Truncate(int64(fs.Size))
		_ = f.Close()
		if terr != nil {
			return terr
		}
	}
	if af.Permissions && fs != nil {
		mode := os.FileMode(fs.Mode & 0o777)
		if mode == 0 {
			mode = 0o644
		}
		if cerr := h.root.Chmod(rel, mode); cerr != nil {
			return cerr
		}
	}
	if af.Acmodtime && fs != nil {
		at := time.Unix(int64(fs.Atime), 0)
		mt := time.Unix(int64(fs.Mtime), 0)
		if cerr := h.root.Chtimes(rel, at, mt); cerr != nil {
			return cerr
		}
	}
	return nil
}

// Filelist 列目录 / Stat / Lstat（pkg/sftp 用同一个入口，按 Method 区分）
func (h *sandboxHandler) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	rel, err := h.rel(r.Filepath)
	if err != nil {
		return nil, err
	}
	if r.Method == "Stat" || r.Method == "Lstat" {
		st, serr := h.root.Stat(rel)
		if serr != nil {
			return nil, serr
		}
		return listerAt{st}, nil
	}

	f, oerr := h.root.Open(rel)
	if oerr != nil {
		return nil, oerr
	}
	defer f.Close()
	st, serr := f.Stat()
	if serr != nil {
		return nil, serr
	}
	if !st.IsDir() {
		return nil, sftp.ErrSSHFxFailure
	}
	entries, rerr := f.ReadDir(-1)
	if rerr != nil {
		return nil, rerr
	}
	out := make(listerAt, 0, len(entries))
	for _, e := range entries {
		info, ierr := e.Info()
		if ierr != nil {
			continue
		}
		out = append(out, info)
	}
	return out, nil
}

// RealPath 客户端登录后问"当前目录"，不解析软链，直接给归一后的虚拟路径
func (h *sandboxHandler) RealPath(p string) (string, error) {
	q := path.Clean("/" + strings.ReplaceAll(strings.TrimSpace(p), "\\", "/"))
	if q == "" || q == "." {
		return "/", nil
	}
	return q, nil
}

// Readlink 沙箱内不暴露宿主真实路径，也不允许建链
func (h *sandboxHandler) Readlink(string) (string, error) {
	return "", sftp.ErrSSHFxOpUnsupported
}

// listerAt 把 []os.FileInfo 适配成 pkg/sftp 的分页读取接口
type listerAt []os.FileInfo

// ListAt 按偏移量拷出最多 len(buf) 条；到底返回 io.EOF
func (l listerAt) ListAt(buf []os.FileInfo, offset int64) (int, error) {
	if offset >= int64(len(l)) {
		return 0, io.EOF
	}
	n := copy(buf, l[offset:])
	if n < len(buf) {
		return n, io.EOF
	}
	return n, nil
}

// Close 满足 pkg/sftp 对 ListerAt 可关闭的探测（无资源需要释放）
func (l listerAt) Close() error { return nil }
