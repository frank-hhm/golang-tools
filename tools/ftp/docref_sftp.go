package ftp

import (
	"io"
	"os"
)

// ============================================================
// 供 tools/docspace 的 docref 使用的远端文件访问器
//
// docref 要把「预览 / Range 流式 / 写回」对 local 与 sftp 两种源统一起来。
// local 直接用 os 即可；sftp 需要一个「按会话 id + 远端路径拿到一个可
// ReadAt/Seek/Write 的文件句柄」的入口。会话表与 *sftp.Client 都是本包私有，
// 故在此导出一个最小接口，避免 docspace 反向依赖 pkg/sftp。
// ============================================================

// RemoteFile 是跨包暴露的远端 SFTP 文件最小视图：可读写、可 Seek、可 Range 读、
// 可 Stat/Close。*sftp.File 天然满足这些方法，用接口收口以隐藏 pkg/sftp 依赖。
type RemoteFile interface {
	io.ReadWriteCloser
	io.ReaderAt
	io.Seeker
	Stat() (os.FileInfo, error)
	Name() string
}

// OpenRemoteFile 按会话 id 与远端路径打开文件供 docref 读写。flag 用 os.O_RDONLY /
// os.O_RDWR 等标准位。建句柄在会话锁内完成（复用同一 *sftp.Client），返回后句柄独立使用。
func OpenRemoteFile(sessionID, remotePath string, flag int) (RemoteFile, error) {
	s, err := getSession(sessionID)
	if err != nil {
		return nil, err
	}
	rp := cleanRemote(remotePath)
	s.mu.Lock()
	f, err := s.cli.OpenFile(rp, flag)
	s.mu.Unlock()
	if err != nil {
		return nil, err
	}
	return f, nil
}
