package lanshare

// 切片上传会话（计划-局域网互传上传任务与切片队列 §2）。
//
// 方向与 tools/downloader 相反：下载是 Go 主动拉远端，这里是浏览器主动推到
// Go 接收侧。切片、队列、暂停由发送侧（门户页 / Vue 面板）驱动，服务端只做三件事：
//   - 按分片下标 WriteAt 落到会话稀疏文件（可乱序、可续传）
//   - 记录会话位图（同名同尺寸重传命中同 sid → 回 have 补未传片）
//   - 分片齐后 uniqueCreate 落收件箱
//
// 会话态落盘 appdirs.Data("lanshare")/uploads/<sid>/：data.part（稀疏）+ meta.json。
// sid = sha1(name|size|mtime) 前 16 位十六进制——服务端权威生成，客户端传不进来，
// 也就没法借 sid 注入路径。
//
// 跨平台零 cgo、无 build tag：纯 net/http + os + 文件 WriteAt，Linux 面板构建同样存在。

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"sync"
	"time"

	"Tools/services/applog"
)

const (
	uploadChunkSize = 4 << 20 // 分片大小 4 MiB（随 init 响应下发，前端按它切）
	maxSessions     = 8       // 活跃/可续传会话上限，超了 init 返回 429
	sessionTTL      = 24 * time.Hour
)

// sidPattern 只接受 16 位十六进制 sid，挡目录穿越。
var sidPattern = regexp.MustCompile(`^[0-9a-f]{16}$`)

// UploadSession 供管理页/绑定展示的会话快照（列可续传会话、强踢用）。
type UploadSession struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Received  int64  `json:"received"`
	Percent   int    `json:"percent"`
	UpdatedAt int64  `json:"updatedAt"`
}

// session 内存态会话；位图与 meta 读写由自身 mu 串行，落盘一份 meta.json。
type session struct {
	sid       string
	dir       string
	partPath  string
	metaPath  string
	mu        sync.Mutex
	name      string
	size      int64
	mtime     int64 // 客户端文件指纹三分量之一，与 sid 同源，重传校验用
	chunkSize int64
	total     int
	done      map[int]bool
	createdAt int64
	updatedAt int64
	lastEmit  int64 // running 进度按秒节流用（毫秒时间戳）
}

// sessionsGuard 保护 liveSessions 这张表（同一 sid 复用一个 session 对象）。
var (
	sessionsGuard sync.Mutex
	liveSessions  = map[string]*session{}
)

// sessionMeta 落盘结构（done 存已收分片下标列表，重传据此回 have）。
type sessionMeta struct {
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	Mtime     int64  `json:"mtime"`
	ChunkSize int64  `json:"chunkSize"`
	Total     int    `json:"total"`
	Done      []int  `json:"done"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

/* ============================ 目录与主键 ============================ */

func uploadsDir() (string, error) {
	base, err := baseDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "uploads")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// makeSID 服务端权威指纹：同 name|size|mtime 命中同会话，实现断点续传。
func makeSID(name string, size, mtime int64) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s|%d|%d", name, size, mtime)
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// chunkLen 第 idx 片的实际字节数（末片可能不满 chunkSize）。
func chunkLen(idx int, size, chunkSize int64) int64 {
	off := int64(idx) * chunkSize
	if off >= size {
		return 0
	}
	if size-off < chunkSize {
		return size - off
	}
	return chunkSize
}

// received 由已收位图 + 总大小确定性地算出（重传载入后无需信任旧字节数）。
func (s *session) received() int64 {
	var n int64
	for idx := range s.done {
		n += chunkLen(idx, s.size, s.chunkSize)
	}
	return n
}

func (s *session) percent() int {
	if s.size <= 0 {
		return 0
	}
	p := int(s.received() * 100 / s.size)
	if p > 99 && !s.allDone() {
		p = 99
	}
	if s.allDone() {
		p = 100
	}
	return p
}

func (s *session) allDone() bool {
	return len(s.done) >= s.total
}

/* ============================ 载入 / 落盘 ============================ */

// getOrCreateSession 命中内存则复用；否则尝试从磁盘 meta 恢复；再不行按 init 参数新建。
// isNew=false 表示命中已有会话（续传），init 用它决定要不要占新会话名额。
func getOrCreateSession(sid, name string, size, mtime, chunkSize int64) (s *session, isNew bool, err error) {
	uploads, err := uploadsDir()
	if err != nil {
		return nil, false, err
	}
	sessionsGuard.Lock()
	defer sessionsGuard.Unlock()
	if exist, ok := liveSessions[sid]; ok {
		return exist, false, nil
	}
	// 从磁盘恢复（服务重启后 .part 仍在）
	dir := filepath.Join(uploads, sid)
	if b, readErr := os.ReadFile(filepath.Join(dir, "meta.json")); readErr == nil {
		var m sessionMeta
		if json.Unmarshal(b, &m) == nil && m.Name == name && m.Size == size && m.Mtime == mtime {
			s = &session{
				sid: sid, dir: dir,
				partPath: filepath.Join(dir, "data.part"), metaPath: filepath.Join(dir, "meta.json"),
				name: m.Name, size: m.Size, mtime: m.Mtime, chunkSize: m.ChunkSize, total: m.Total,
				done: map[int]bool{}, createdAt: m.CreatedAt, updatedAt: m.UpdatedAt,
			}
			for _, idx := range m.Done {
				if idx >= 0 && idx < s.total {
					s.done[idx] = true
				}
			}
			liveSessions[sid] = s
			return s, false, nil
		}
	}
	// 新建
	now := time.Now().UnixMilli()
	s = &session{
		sid: sid, dir: dir,
		partPath: filepath.Join(dir, "data.part"), metaPath: filepath.Join(dir, "meta.json"),
		name: name, size: size, mtime: mtime, chunkSize: chunkSize,
		total: int((size + chunkSize - 1) / chunkSize),
		done:  map[int]bool{}, createdAt: now, updatedAt: now,
	}
	if s.total < 1 {
		s.total = 1
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false, err
	}
	liveSessions[sid] = s
	return s, true, nil
}

// save 原子写 meta.json（先 .tmp 再 rename）。调用方须持有 s.mu。
func (s *session) save() error {
	done := make([]int, 0, len(s.done))
	for idx := range s.done {
		done = append(done, idx)
	}
	sort.Ints(done)
	b, err := json.Marshal(sessionMeta{
		Name: s.name, Size: s.size, Mtime: s.mtime, ChunkSize: s.chunkSize,
		Total: s.total, Done: done, CreatedAt: s.createdAt, UpdatedAt: s.updatedAt,
	})
	if err != nil {
		return err
	}
	tmp := s.metaPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.metaPath)
}

// drop 从内存表摘除并删目录（调用方决定何时删盘）。
func (s *session) drop() {
	sessionsGuard.Lock()
	delete(liveSessions, s.sid)
	sessionsGuard.Unlock()
}

/* ============================ 路由处理（挂 *httpServer，复用沙箱/上传计数） ============================ */

// handleUploadInit POST /upload/init  body {name,size,mtime} → {sid,chunkSize,have}
func (s *httpServer) handleUploadInit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name  string `json:"name"`
		Size  int64  `json:"size"`
		Mtime int64  `json:"mtime"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, "请求体解析失败", http.StatusBadRequest)
		return
	}
	name := sanitizeName(req.Name)
	if name == "" || uploadBlocked(name) {
		http.Error(w, "非法文件名或类型黑名单", http.StatusBadRequest)
		return
	}
	if req.Size <= 0 || req.Size > maxUploadBytes {
		http.Error(w, "文件大小非法或超出上限", http.StatusRequestEntityTooLarge)
		return
	}
	sid := makeSID(name, req.Size, req.Mtime)
	sess, isNew, err := getOrCreateSession(sid, name, req.Size, req.Mtime, uploadChunkSize)
	if err != nil {
		http.Error(w, "会话初始化失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	// 新会话：先落 meta（带新鲜 updatedAt），再 GC 过期会话并数名额。
	// 必须先 save 后 gc——否则 gc 会把尚无 meta.json 的本会话目录当孤儿删掉，
	// 导致后续分片 OpenFile 找不到目录、名额也永远数不满。
	if isNew {
		sess.mu.Lock()
		sess.mtime = req.Mtime
		saveErr := sess.save()
		sess.mu.Unlock()
		if saveErr != nil {
			_ = os.RemoveAll(sess.dir)
			sess.drop()
			http.Error(w, "会话落盘失败: "+saveErr.Error(), http.StatusInternalServerError)
			return
		}
		// GC 后存活数（已含本会话）超上限 → 回滚本会话并拒绝
		if gcExpiredSessions() > maxSessions {
			_ = os.RemoveAll(sess.dir)
			sess.drop()
			http.Error(w, "接收会话已满，请稍后重试", http.StatusTooManyRequests)
			return
		}
	}
	have := make([]int, 0, len(sess.done))
	sess.mu.Lock()
	for idx := range sess.done {
		have = append(have, idx)
	}
	sess.mu.Unlock()
	sort.Ints(have)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"sid": sid, "chunkSize": uploadChunkSize, "have": have,
	})
}

// handleUploadPart PUT /upload/part?sid=&idx=  body 为该分片原始字节
func (s *httpServer) handleUploadPart(w http.ResponseWriter, r *http.Request) {
	sid := safeSegment(r.URL.Query().Get("sid"))
	if !sidPattern.MatchString(sid) {
		http.Error(w, "非法会话 id", http.StatusBadRequest)
		return
	}
	idx, err := strconv.Atoi(r.URL.Query().Get("idx"))
	if err != nil || idx < 0 {
		http.Error(w, "非法分片下标", http.StatusBadRequest)
		return
	}
	sess := findLiveSession(sid)
	if sess == nil {
		http.Error(w, "会话不存在或已结束", http.StatusNotFound)
		return
	}
	sess.mu.Lock()
	if idx >= sess.total {
		sess.mu.Unlock()
		http.Error(w, "分片下标越界", http.StatusBadRequest)
		return
	}
	want := chunkLen(idx, sess.size, sess.chunkSize)
	// 单片上限 = chunkSize 再留 1 字节容错
	r.Body = http.MaxBytesReader(w, r.Body, uploadChunkSize+1)
	data, err := io.ReadAll(r.Body)
	if err != nil || int64(len(data)) != want {
		sess.mu.Unlock()
		http.Error(w, "分片长度不符或读取失败", http.StatusBadRequest)
		return
	}
	f, err := os.OpenFile(sess.partPath, os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		sess.mu.Unlock()
		http.Error(w, "分片落盘失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_, err = f.WriteAt(data, int64(idx)*sess.chunkSize)
	_ = f.Close()
	if err != nil {
		sess.mu.Unlock()
		http.Error(w, "分片写入失败", http.StatusInternalServerError)
		return
	}
	sess.done[idx] = true
	sess.updatedAt = time.Now().UnixMilli()
	_ = sess.save()
	pct := sess.percent()
	name := sess.name
	needEmit := time.Now().UnixMilli()-sess.lastEmit >= 1000
	if needEmit {
		sess.lastEmit = time.Now().UnixMilli()
	}
	sess.mu.Unlock()

	if needEmit {
		emitProgress(Progress{Op: "upload", ID: sid, Name: name, Stage: "running", Status: "running", Percent: pct})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "percent": pct})
}

// handleUploadComplete POST /upload/complete?sid=  校验分片齐 + 落收件箱
func (s *httpServer) handleUploadComplete(w http.ResponseWriter, r *http.Request) {
	sid := safeSegment(r.URL.Query().Get("sid"))
	if !sidPattern.MatchString(sid) {
		http.Error(w, "非法会话 id", http.StatusBadRequest)
		return
	}
	sess := findLiveSession(sid)
	if sess == nil {
		http.Error(w, "会话不存在或已结束", http.StatusNotFound)
		return
	}
	sess.mu.Lock()
	if !sess.allDone() {
		miss := sess.total - len(sess.done)
		sess.mu.Unlock()
		http.Error(w, fmt.Sprintf("分片未收齐（缺 %d 片）", miss), http.StatusBadRequest)
		return
	}
	name := sess.name
	size := sess.size
	partPath := sess.partPath
	sess.mu.Unlock()

	src, err := os.Open(partPath)
	if err != nil {
		emitProgress(Progress{Op: "upload", ID: sid, Name: name, Stage: "error", Status: "error", Note: "分片文件缺失"})
		http.Error(w, "分片文件缺失", http.StatusInternalServerError)
		return
	}
	defer src.Close()
	dst, err := s.uniqueCreate(name)
	if err != nil {
		http.Error(w, "收件箱写入失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	n, err := io.CopyN(dst, src, size)
	_ = dst.Close()
	if err != nil && err != io.EOF {
		_ = s.root.Remove(name)
		accessLine(r, "upload", name, 0, false)
		emitProgress(Progress{Op: "upload", ID: sid, Name: name, Stage: "error", Status: "error", Note: "合并写盘中断"})
		http.Error(w, "合并写盘失败", http.StatusInternalServerError)
		return
	}
	s.uploadCnt.Add(1)
	accessLine(r, "upload", name, n, true)
	// 收干净：先释放 .part 句柄再删目录——Windows 下删仍被占用的文件会失败（defer
	// 的 src.Close() 要到函数返回才跑，故这里提前关，defer 再关一次是无害 no-op）。
	_ = src.Close()
	_ = os.RemoveAll(sess.dir)
	sess.drop()
	emitProgress(Progress{Op: "upload", ID: sid, Name: name, Stage: "done", Status: "done", Percent: 100, Note: fmt.Sprintf("已接收 %s", humanSize(n))})
	applog.Infof("lanshare", "切片上传完成: %s (%d B, sid=%s)", name, n, sid)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "已接收：\n"+fmt.Sprintf("%s (%d B)\n", dstNameOf(dst), n))
}

// handleUploadAbort DELETE /upload/abort?sid=
func (s *httpServer) handleUploadAbort(w http.ResponseWriter, r *http.Request) {
	sid := safeSegment(r.URL.Query().Get("sid"))
	if !sidPattern.MatchString(sid) {
		http.Error(w, "非法会话 id", http.StatusBadRequest)
		return
	}
	name := abortSession(sid)
	emitProgress(Progress{Op: "upload", ID: sid, Name: name, Stage: "canceled", Status: "canceled", Note: "会话已取消"})
	w.WriteHeader(http.StatusOK)
}

// handleUploadSessionGet GET /upload/session?sid= → {have, received, percent}
func (s *httpServer) handleUploadSessionGet(w http.ResponseWriter, r *http.Request) {
	sid := safeSegment(r.URL.Query().Get("sid"))
	if !sidPattern.MatchString(sid) {
		http.Error(w, "非法会话 id", http.StatusBadRequest)
		return
	}
	sess := findLiveSession(sid)
	if sess == nil {
		http.Error(w, "会话不存在或已结束", http.StatusNotFound)
		return
	}
	sess.mu.Lock()
	have := make([]int, 0, len(sess.done))
	for idx := range sess.done {
		have = append(have, idx)
	}
	rec, pct := sess.received(), sess.percent()
	sess.mu.Unlock()
	sort.Ints(have)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"have": have, "received": rec, "percent": pct})
}

/* ============================ 会话表操作（磁盘为唯一真源，内存加速） ============================ */

func findLiveSession(sid string) *session {
	sessionsGuard.Lock()
	if s, ok := liveSessions[sid]; ok {
		sessionsGuard.Unlock()
		return s
	}
	sessionsGuard.Unlock()
	// 内存没有 → 从磁盘 meta 恢复（服务重启后仍能续传/查询）
	uploads, err := uploadsDir()
	if err != nil {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(uploads, sid, "meta.json"))
	if err != nil {
		return nil
	}
	var m sessionMeta
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	sess := &session{
		sid: sid, dir: filepath.Join(uploads, sid),
		partPath: filepath.Join(uploads, sid, "data.part"), metaPath: filepath.Join(uploads, sid, "meta.json"),
		name: m.Name, size: m.Size, chunkSize: m.ChunkSize, total: m.Total,
		done: map[int]bool{}, createdAt: m.CreatedAt, updatedAt: m.UpdatedAt, mtime: m.Mtime,
	}
	for _, idx := range m.Done {
		if idx >= 0 && idx < sess.total {
			sess.done[idx] = true
		}
	}
	sessionsGuard.Lock()
	liveSessions[sid] = sess
	sessionsGuard.Unlock()
	return sess
}

// abortSession 删盘 + 摘表，回被取消会话的文件名（供进度事件）。
func abortSession(sid string) string {
	sess := findLiveSession(sid)
	if sess == nil {
		return sid
	}
	name := sess.name
	_ = os.RemoveAll(sess.dir)
	sess.drop()
	return name
}

// gcExpiredSessions 删除超过 TTL 的会话目录，回删除后仍存活的会话数。
func gcExpiredSessions() int {
	uploads, err := uploadsDir()
	if err != nil {
		return 0
	}
	ents, err := os.ReadDir(uploads)
	if err != nil {
		return 0
	}
	nowMs := time.Now().UnixMilli()
	alive := 0
	for _, e := range ents {
		if !e.IsDir() || !sidPattern.MatchString(e.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(uploads, e.Name(), "meta.json"))
		if err != nil {
			_ = os.RemoveAll(filepath.Join(uploads, e.Name()))
			continue
		}
		var m sessionMeta
		if json.Unmarshal(b, &m) != nil {
			_ = os.RemoveAll(filepath.Join(uploads, e.Name()))
			continue
		}
		if nowMs-m.UpdatedAt > sessionTTL.Milliseconds() {
			_ = os.RemoveAll(filepath.Join(uploads, e.Name()))
			sessionsGuard.Lock()
			delete(liveSessions, e.Name())
			sessionsGuard.Unlock()
			continue
		}
		alive++
	}
	return alive
}

// ListUploadSessions 扫描 uploads 目录回可续传/进行中的会话快照（服务停着也能列）。
func ListUploadSessions() ([]UploadSession, error) {
	uploads, err := uploadsDir()
	if err != nil {
		return nil, err
	}
	gcExpiredSessions()
	ents, err := os.ReadDir(uploads)
	if err != nil {
		if os.IsNotExist(err) {
			return []UploadSession{}, nil
		}
		return nil, err
	}
	out := []UploadSession{}
	for _, e := range ents {
		if !e.IsDir() || !sidPattern.MatchString(e.Name()) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(uploads, e.Name(), "meta.json"))
		if err != nil {
			continue
		}
		var m sessionMeta
		if json.Unmarshal(b, &m) != nil {
			continue
		}
		var rec int64
		for _, idx := range m.Done {
			rec += chunkLen(idx, m.Size, m.ChunkSize)
		}
		pct := 0
		if m.Size > 0 {
			pct = int(rec * 100 / m.Size)
			if pct > 100 {
				pct = 100
			}
		}
		out = append(out, UploadSession{ID: e.Name(), Name: m.Name, Size: m.Size, Received: rec, Percent: pct, UpdatedAt: m.UpdatedAt})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt > out[j].UpdatedAt })
	return out, nil
}

// AbortUpload 供绑定强踢一个会话（服务未运行也能删盘上残留）。
func AbortUpload(sid string) error {
	if !sidPattern.MatchString(safeSegment(sid)) {
		return fmt.Errorf("非法会话 id")
	}
	abortSession(safeSegment(sid))
	return nil
}

/* ============================ 小工具 ============================ */

// dstNameOf 从已打开的收件箱文件取落盘名（uniqueCreate 可能做了重名避让）。
func dstNameOf(f *os.File) string {
	if f == nil {
		return ""
	}
	return filepath.Base(f.Name())
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
