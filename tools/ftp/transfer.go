package ftp

// ============================================================
// 传输队列
//
// 上传/下载都拆成"一个文件一个任务"：目录会先展开成若干任务，目录本身也占一个
// 建目录任务。好处是进度单一、失败可重试、取消能精确清理半截文件；
// 代价是前端队列里看到的行数会比较多（和 Xftp 的行为一致）。
//
// 进度事件 ftp:transfer 按任务发快照，200ms 节流一次；终态一定发。
// ============================================================

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/pkg/sftp"
)

const (
	transferWorkers    = 2 // 同时进行的文件数
	progressInterval   = 200 * time.Millisecond
	maxTransferEntries = 5000 // 单次展开的目录项上限，防手滑拖整个盘
	maxTransferRows    = 200  // 内存里保留的任务条数
	copyBufferSize     = 64 << 10
)

var errCanceled = errors.New("已取消")

// task 一个传输任务。t 的并发读写都在 mu 下进行。
type task struct {
	mu        sync.Mutex
	t         Transfer
	isDir     bool
	sessionID string
	canceled  atomic.Bool

	speedAt   time.Time
	speedDone int64
}

var (
	taskMu  sync.Mutex
	tasks   = map[string]*task{}
	taskSeq int
	queue   = make(chan *task, 256)
	workOne sync.Once
)

func nextTaskID() string {
	taskSeq++
	return fmt.Sprintf("x%d", taskSeq)
}

func newTask(sessionID, dir, name, local, remote string, total int64, isDir bool) *task {
	t := &task{
		t: Transfer{
			ID:      nextTaskID(),
			Dir:     dir,
			Name:    name,
			Local:   local,
			Remote:  remote,
			Total:   total,
			State:   StateQueued,
			Started: time.Now().Unix(),
		},
		isDir:     isDir,
		sessionID: sessionID,
		speedAt:   time.Now(),
	}
	taskMu.Lock()
	tasks[t.t.ID] = t
	if len(tasks) > maxTransferRows {
		pruneLocked()
	}
	taskMu.Unlock()
	return t
}

// pruneLocked 只清理已结束的最旧任务；进行中的一律保留
func pruneLocked() {
	type row struct {
		id      string
		started int64
		state   string
	}
	rows := make([]row, 0, len(tasks))
	for id, t := range tasks {
		t.mu.Lock()
		rows = append(rows, row{id: id, started: t.t.Started, state: t.t.State})
		t.mu.Unlock()
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].started < rows[j].started })
	for _, r := range rows {
		if len(tasks) <= maxTransferRows {
			return
		}
		if r.state == StateQueued || r.state == StateRunning {
			continue
		}
		delete(tasks, r.id)
	}
}

func enqueue(list []*task) {
	ensureWorkers()
	for _, t := range list {
		queue <- t
	}
}

func ensureWorkers() {
	workOne.Do(func() {
		for i := 0; i < transferWorkers; i++ {
			go worker()
		}
	})
}

func worker() {
	for t := range queue {
		runTask(t)
	}
}

func (t *task) addDone(n int64) {
	t.mu.Lock()
	t.t.Done += n
	now := time.Now()
	// 每秒算一次速度，避免每 200ms 都做一次除法导致数字乱跳
	if now.Sub(t.speedAt) >= time.Second {
		sec := now.Sub(t.speedAt).Seconds()
		t.t.Speed = int64(float64(t.t.Done-t.speedDone) / sec)
		t.speedDone = t.t.Done
		t.speedAt = now
	}
	t.mu.Unlock()
}

func (t *task) snapshot() Transfer {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.t
}

func (t *task) setState(state string) {
	t.mu.Lock()
	t.t.State = state
	t.mu.Unlock()
}

func (t *task) finish(state, note string) {
	t.mu.Lock()
	t.t.State = state
	t.t.Err = note
	t.t.Ended = time.Now().Unix()
	if state == StateDone {
		t.t.Speed = 0
		if t.t.Total > 0 {
			t.t.Done = t.t.Total
		}
	}
	snap := t.t
	t.mu.Unlock()
	emit(EventTransfer, snap)
}

// progressReader 读源数据时累计进度、按 200ms 节流推送、并响应取消
type progressReader struct {
	r    io.Reader
	t    *task
	last time.Time
}

func (p *progressReader) Read(b []byte) (int, error) {
	if p.t.canceled.Load() {
		return 0, errCanceled
	}
	n, err := p.r.Read(b)
	if n > 0 {
		p.t.addDone(int64(n))
		if now := time.Now(); now.Sub(p.last) >= progressInterval {
			p.last = now
			emit(EventTransfer, p.t.snapshot())
		}
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return n, err
	}
	return n, err
}

func runTask(t *task) {
	if t.canceled.Load() {
		t.finish(StateCanceled, "")
		return
	}
	t.setState(StateRunning)
	emit(EventTransfer, t.snapshot())

	s, err := getSession(t.sessionID)
	if err != nil {
		t.finish(StateFailed, err.Error())
		return
	}

	if t.isDir {
		if t.t.Dir == DirUp {
			err = s.cli.MkdirAll(t.t.Remote)
		} else {
			err = os.MkdirAll(t.t.Local, 0o755)
		}
		if err != nil {
			t.finish(StateFailed, fmt.Sprintf("创建目录失败: %v", err))
			return
		}
		t.finish(StateDone, "")
		return
	}

	if t.t.Dir == DirUp {
		err = doUpload(s, t)
	} else {
		err = doDownload(s, t)
	}
	switch {
	case errors.Is(err, errCanceled):
		removePartial(t, s)
		t.finish(StateCanceled, "")
	case err != nil:
		t.finish(StateFailed, err.Error())
	default:
		t.finish(StateDone, "")
	}
}

func doUpload(s *session, t *task) error {
	// 父目录可能因并发/中断还没建出来，建一次（已存在时 MkdirAll 返回 nil）
	if err := s.cli.MkdirAll(path.Dir(t.t.Remote)); err != nil {
		return fmt.Errorf("创建远端目录失败: %v", err)
	}
	src, err := os.Open(t.t.Local)
	if err != nil {
		return fmt.Errorf("打开本地文件失败: %v", err)
	}
	defer src.Close()
	dst, err := s.cli.Create(t.t.Remote)
	if err != nil {
		return fmt.Errorf("创建远端文件失败: %v", err)
	}
	defer dst.Close()

	pr := &progressReader{r: src, t: t}
	if _, cerr := io.CopyBuffer(dst, pr, make([]byte, copyBufferSize)); cerr != nil {
		return cerr
	}
	return nil
}

func doDownload(s *session, t *task) error {
	if err := os.MkdirAll(filepath.Dir(t.t.Local), 0o755); err != nil {
		return fmt.Errorf("创建本地目录失败: %v", err)
	}
	src, err := s.cli.Open(t.t.Remote)
	if err != nil {
		return fmt.Errorf("打开远端文件失败: %v", err)
	}
	defer src.Close()
	dst, err := os.Create(t.t.Local)
	if err != nil {
		return fmt.Errorf("创建本地文件失败: %v", err)
	}
	defer dst.Close()

	pr := &progressReader{r: src, t: t}
	if _, cerr := io.CopyBuffer(dst, pr, make([]byte, copyBufferSize)); cerr != nil {
		return cerr
	}
	return nil
}

// removePartial 取消/失败后删掉写了一半的目标文件，避免留下坏文件
func removePartial(t *task, s *session) {
	_ = t
	_ = s
	if t.isDir {
		return
	}
	if t.t.Dir == DirUp {
		_ = s.cli.Remove(t.t.Remote)
		return
	}
	_ = os.Remove(t.t.Local)
}

// Upload 把本地文件/目录上传到远端目录；目录递归展开成多个任务
func Upload(sessionID string, localPaths []string, remoteDir string) ([]Transfer, error) {
	if _, err := getSession(sessionID); err != nil {
		return nil, err
	}
	paths := cleanPathList(localPaths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("请选择要上传的文件")
	}
	remoteDir = cleanRemote(remoteDir)

	var created []*task
	for _, lp := range paths {
		abs, aerr := filepath.Abs(lp)
		if aerr != nil {
			return nil, fmt.Errorf("本地路径无效: %v", aerr)
		}
		info, serr := os.Stat(abs)
		if serr != nil {
			return nil, fmt.Errorf("读取 %s 失败: %v", filepath.Base(abs), serr)
		}
		if !info.IsDir() {
			created = append(created, newTask(sessionID, DirUp, info.Name(), abs,
				path.Join(remoteDir, info.Name()), info.Size(), false))
			continue
		}

		base := filepath.Base(abs)
		count := 0
		werr := filepath.WalkDir(abs, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Type()&fs.ModeSymlink != 0 {
				return nil // 不跟随软链，避免目录环
			}
			rel, rerr := filepath.Rel(abs, p)
			if rerr != nil {
				return rerr
			}
			relSlash := filepath.ToSlash(rel)
			if relSlash == "." {
				created = append(created, newTask(sessionID, DirUp, base, abs,
					path.Join(remoteDir, base), 0, true))
				return nil
			}
			count++
			if count > maxTransferEntries {
				return fmt.Errorf("目录项超过 %d，请分目录上传", maxTransferEntries)
			}
			name := path.Join(base, relSlash)
			target := path.Join(remoteDir, base, relSlash)
			if d.IsDir() {
				created = append(created, newTask(sessionID, DirUp, name, p, target, 0, true))
				return nil
			}
			fi, ierr := d.Info()
			if ierr != nil {
				return ierr
			}
			created = append(created, newTask(sessionID, DirUp, name, p, target, fi.Size(), false))
			return nil
		})
		if werr != nil {
			return nil, werr
		}
	}
	enqueue(created)
	return snapshots(created), nil
}

// Download 把远端文件/目录下载到本地目录；目录递归展开成多个任务
func Download(sessionID string, remotePaths []string, localDir string) ([]Transfer, error) {
	s, err := getSession(sessionID)
	if err != nil {
		return nil, err
	}
	paths := cleanPathList(remotePaths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("请选择要下载的文件")
	}
	localDir = strings.TrimSpace(localDir)
	if localDir == "" {
		return nil, fmt.Errorf("请先选择本地目标目录")
	}
	absLocal, aerr := filepath.Abs(localDir)
	if aerr != nil {
		return nil, fmt.Errorf("本地路径无效: %v", aerr)
	}
	if info, serr := os.Stat(absLocal); serr != nil || !info.IsDir() {
		return nil, fmt.Errorf("本地目录不存在: %s", absLocal)
	}

	var created []*task
	for _, rp := range paths {
		root := cleanRemote(rp)
		base := path.Base(root)
		count := 0
		werr := walkRemote(s.cli, root, func(p string, info os.FileInfo) error {
			count++
			if count > maxTransferEntries {
				return fmt.Errorf("目录项超过 %d，请分目录下载", maxTransferEntries)
			}
			rel, rerr := relRemote(root, p)
			if rerr != nil {
				return rerr
			}
			if rel == "." {
				if info.IsDir() {
					created = append(created, newTask(sessionID, DirDown, base, filepath.Join(absLocal, base), p, 0, true))
				} else {
					created = append(created, newTask(sessionID, DirDown, base, filepath.Join(absLocal, base), p, info.Size(), false))
				}
				return nil
			}
			name := path.Join(base, rel)
			target := filepath.Join(absLocal, base, filepath.FromSlash(rel))
			if info.IsDir() {
				created = append(created, newTask(sessionID, DirDown, name, target, p, 0, true))
				return nil
			}
			created = append(created, newTask(sessionID, DirDown, name, target, p, info.Size(), false))
			return nil
		})
		if werr != nil {
			return nil, werr
		}
	}
	enqueue(created)
	return snapshots(created), nil
}

// walkRemote 先序遍历远端目录；symlink 不递归（按文件处理）
func walkRemote(cli *sftp.Client, root string, fn func(string, os.FileInfo) error) error {
	info, err := cli.Stat(root)
	if err != nil {
		return fmt.Errorf("读取 %s 失败: %v", path.Base(root), err)
	}
	var walk func(p string, fi os.FileInfo) error
	walk = func(p string, fi os.FileInfo) error {
		if err := fn(p, fi); err != nil {
			return err
		}
		if !fi.IsDir() {
			return nil
		}
		entries, rerr := cli.ReadDir(p)
		if rerr != nil {
			return fmt.Errorf("读取目录 %s 失败: %v", p, rerr)
		}
		for _, e := range entries {
			if e.Name() == "." || e.Name() == ".." {
				continue
			}
			if err := walk(path.Join(p, e.Name()), e); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root, info)
}

// relRemote 返回 p 相对 root 的路径（POSIX 形式，"." 表示 root 本身）
func relRemote(root, p string) (string, error) {
	if p == root {
		return ".", nil
	}
	prefix := root
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	if !strings.HasPrefix(p, prefix) {
		return "", fmt.Errorf("路径 %s 不在 %s 下", p, root)
	}
	return strings.TrimPrefix(p, prefix), nil
}

// CancelTransfer 取消一个任务；排队中的立即结束，运行中的由读循环中止
func CancelTransfer(id string) error {
	taskMu.Lock()
	t := tasks[id]
	taskMu.Unlock()
	if t == nil {
		return fmt.Errorf("任务不存在")
	}
	t.canceled.Store(true)
	t.mu.Lock()
	state := t.t.State
	t.mu.Unlock()
	if state == StateQueued {
		t.finish(StateCanceled, "")
	}
	return nil
}

// RetryTransfer 按原参数重建任务并重新入队
func RetryTransfer(id string) (Transfer, error) {
	taskMu.Lock()
	old := tasks[id]
	taskMu.Unlock()
	if old == nil {
		return Transfer{}, fmt.Errorf("任务不存在")
	}
	snap := old.snapshot()
	if snap.State != StateFailed && snap.State != StateCanceled {
		return Transfer{}, fmt.Errorf("只有失败或已取消的任务可以重试")
	}
	nt := newTask(old.sessionID, snap.Dir, snap.Name, snap.Local, snap.Remote, snap.Total, old.isDir)
	enqueue([]*task{nt})
	return nt.snapshot(), nil
}

// ListTransfers 当前内存里的任务（新的在前）
func ListTransfers() []Transfer {
	taskMu.Lock()
	list := make([]*task, 0, len(tasks))
	for _, t := range tasks {
		list = append(list, t)
	}
	taskMu.Unlock()
	out := make([]Transfer, 0, len(list))
	for _, t := range list {
		out = append(out, t.snapshot())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Started != out[j].Started {
			return out[i].Started > out[j].Started
		}
		return out[i].ID > out[j].ID
	})
	return out
}

func snapshots(list []*task) []Transfer {
	out := make([]Transfer, 0, len(list))
	for _, t := range list {
		out = append(out, t.snapshot())
	}
	return out
}

func cleanPathList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, p := range in {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
