package lanshare

// 支撑层：目录解析、文件名净化、每 IP 限频、访问日志。
// 都是小件，集中在一个文件里，避免为三行代码开五个文件。

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"Tools/appdirs"
)

/* ==================== 目录 ==================== */

var (
	baseOnce  sync.Once
	baseCache string
	baseErr   error
	baseOver  string // 测试注入用：非空则优先于 appdirs（见 setBaseDirForTest）
)

func baseDir() (string, error) {
	baseOnce.Do(func() {
		if baseOver != "" {
			baseCache = baseOver
		} else {
			baseCache, baseErr = appdirs.Data("lanshare")
		}
		if baseErr == nil {
			baseErr = os.MkdirAll(filepath.Join(baseCache, "inbox"), 0o755)
		}
	})
	return baseCache, baseErr
}

// setBaseDirForTest 只能在任何 baseDir 调用之前用（sync.Once 之后改无效），
// 单测里用来把数据目录指向临时目录，不污染真实应用数据。
func setBaseDirForTest(dir string) {
	baseOver = dir
}

func inboxDir() (string, error) {
	base, err := baseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "inbox"), nil
}

func inboxCached() string {
	p, _ := inboxDir()
	return p
}

// InboxFile 收件箱条目，供管理页列表展示（只读元数据，不搬实体）。
type InboxFile struct {
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	ModAt int64  `json:"modAt"` // UnixMilli
}

// ListInbox 列出收件箱里的文件（不含子目录），按修改时间倒序。
// 目录不存在视作空收件箱返回空切片而非报错——服务还没跑时前端也要能渲染。
func ListInbox() ([]InboxFile, error) {
	dir, err := inboxDir()
	if err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []InboxFile{}, nil
		}
		return nil, err
	}
	out := []InboxFile{}
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, InboxFile{Name: e.Name(), Size: info.Size(), ModAt: info.ModTime().UnixMilli()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModAt > out[j].ModAt })
	return out, nil
}

/* ==================== 名字净化 ==================== */

// sanitizeName 只保留 basename、去路径分隔符与控制字符、去前导点（防 .htaccess
// 这类隐藏文件），限长 200。上传侧的第一道护栏，第二道是 os.Root 沙箱。
func sanitizeName(raw string) string {
	raw = filepath.Base(strings.ReplaceAll(raw, "\\", "/"))
	raw = strings.ReplaceAll(raw, "..", "")
	var b strings.Builder
	for _, r := range raw {
		if r == 0 || !unicode.IsPrint(r) {
			continue
		}
		switch r {
		case '/', ':', '*', '?', '"', '<', '>', '|':
			r = '_'
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	out = strings.TrimLeft(out, ".")
	if len(out) > 200 {
		ext := filepath.Ext(out)
		if len(ext) > 10 {
			ext = ""
		}
		out = out[:200-len(ext)] + ext
	}
	return out
}

// uploadBlocked 收件箱不收的类型：默认黑名单为空 = 全类型全收（用户诉求「默认全类型
// 所有文件」）。黑名单由 services/app 经 SetBlockExt 从 config.LanShare 回灌（tools 层
// 不反向依赖配置包）；填入可执行/脚本类即可找回安全默认，挡「用户随手双击运行」。
var (
	blockMu  sync.RWMutex
	blockExt = map[string]bool{} // 键：小写、带前导点的扩展名（如 ".exe"）
)

// SetBlockExt 整体替换接收类型黑名单。入参每项归一：TrimSpace + 小写 + 补前导点，
// 忽略空串与纯点；去重。传空切片 = 清空黑名单（全收）。启动与保存设置时调用。
func SetBlockExt(exts []string) {
	m := make(map[string]bool, len(exts))
	for _, e := range exts {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || e == "." {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		m[e] = true
	}
	blockMu.Lock()
	blockExt = m
	blockMu.Unlock()
}

func uploadBlocked(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == "" {
		return false
	}
	blockMu.RLock()
	blocked := blockExt[ext]
	blockMu.RUnlock()
	return blocked
}

/* ==================== 限频 ==================== */

type rateLimiter struct {
	mu   sync.Mutex
	cap  int
	win  time.Duration
	hits map[string][]time.Time
}

func newRateLimiter(cap int, win time.Duration) *rateLimiter {
	return &rateLimiter{cap: cap, win: win, hits: map[string][]time.Time{}}
}

// Allow 滑动窗口计数。内存随 IP 数增长，但每次调用顺手清掉窗口外的空项，
// 局域网场景的量级（几十台设备）不必上 LRU。
func (r *rateLimiter) Allow(ip string) bool {
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	keep := r.hits[ip][:0]
	for _, t := range r.hits[ip] {
		if now.Sub(t) < r.win {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		delete(r.hits, ip)
	} else {
		r.hits[ip] = keep
	}
	if len(keep) >= r.cap {
		r.hits[ip] = keep
		return false
	}
	r.hits[ip] = append(keep, now)
	return true
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

/* ==================== 访问日志 ==================== */

type accessRec struct {
	Time int64  `json:"time"`
	IP   string `json:"ip"`
	Op   string `json:"op"`
	Name string `json:"name"`
	Byte int64  `json:"byte"`
	OK   bool   `json:"ok"`
}

// accessLine 追加一行 JSONL。失败静默——访问日志是事后核对用的辅助，
// 绝不能因为它自己写不下去而阻断传输。
func accessLine(r *http.Request, op, name string, n int64, ok bool) {
	base, err := baseDir()
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(base, "access.log"), os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o640)
	if err != nil {
		return
	}
	defer f.Close()
	_ = json.NewEncoder(f).Encode(accessRec{
		Time: time.Now().UnixMilli(),
		IP:   remoteIP(r),
		Op:   op,
		Name: name,
		Byte: n,
		OK:   ok,
	})
}
