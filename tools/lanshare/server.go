package lanshare

// 局域网互传 + 临时分享链接（计划-SSH管理与局域网互传 §四，本批只做「分享链接」
// 与收件箱上传两条最小闭环；grants 门户授权列表顺延，见交付说明）。
//
// 一个监听、一套路由，两种用法：
//   - /s/<token>        临时分享链接：有效期 + 下载次数上限，文档中心「分享」的落点
//   - POST /upload      收件箱上传：局域网设备浏览器直接传文件到 data/lanshare/inbox
//   - /                 极简首页（纯 HTML，无静态资源依赖）
//
// 安全边界（如实）：上传免密 = 局域网内任何设备都能往收件箱塞文件，靠大小上限、
// 扩展名黑名单、每 IP 限频与自动停止兜底，不靠认证；收件箱目录不可执行这一条
// 我们无法保证（取决于文件系统与用户习惯），界面必须提示「不要直接双击运行收件箱文件」。
// 下载侧只认显式创建的 token 链接，路径永不暴露在 URL 里。
//
// 跨平台零 cgo、无 build tag：纯 net/http + os.Root（Go 1.24+ 沙箱），
// Linux 面板构建同样存在。

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"Tools/services/applog"
	"Tools/tools/portcheck"
)

// 上限与默认值：写死在代码里而不是配置里——这一层的目的是"有护栏"，
// 可配参数留给后续（计划 §四 的 SetGrants/SetAutoStop 一并扩）。
const (
	DefaultPort       = 36490            // 监听基准端口，占用则 +1 重试
	maxUploadBytes    = 2 << 30          // 单文件 2 GiB
	readHeaderLimit   = 8 << 20          // 请求头上限，防畸形大包
	ratePerMinute     = 120              // 每 IP 每分钟请求数（含首页与下载）
	serverReadHdrs    = 15 * time.Second //
	serverIdleTimeout = 60 * time.Second
)

// Status 服务运行态（回前端展示）。
type Status struct {
	Running     bool     `json:"running"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	URLs        []string `json:"urls"`        // 局域网可访问地址（含 http://）
	Inbox       string   `json:"inbox"`       // 收件箱绝对路径
	StartedAt   int64    `json:"startedAt"`   // UnixMilli
	AutoStopAt  int64    `json:"autoStopAt"`  // 0 = 不自动停
	ReqCount    int64    `json:"reqCount"`    // 累计请求数（粗略活跃度）
	UploadCount int64    `json:"uploadCount"` // 累计成功上传数
	Pin         string   `json:"pin"`         // 可选 PIN（空=免密）；仅挡无意访问，不是访问控制
	GrantPath   string   `json:"grantPath"`   // 授权门户路径（/g/ 前缀，供二维码与门户入口）
	LastErr     string   `json:"lastErr"`
}

var (
	mu      sync.Mutex
	srv     *httpServer
	lastErr string
)

type httpServer struct {
	ln         net.Listener
	http       *http.Server
	inbox      string
	root       *os.Root // inbox 的沙箱句柄
	startedAt  int64
	autoStopAt int64
	autoTimer  *time.Timer
	reqCount   atomic.Int64
	uploadCnt  atomic.Int64
	lim        *rateLimiter
	pin        string // 可选 PIN（空=免密）
}

// 服务只在跑着的时候占用端口（自动 +1 选口，不写盘登记固定口），
// 所以这里报的是实时刻用而非预占；与 ftp/nginx 那类固定端口登记不同。
func init() {
	portcheck.Register(func() []portcheck.Owner {
		mu.Lock()
		st := statusLocked()
		mu.Unlock()
		if !st.Running || st.Port <= 0 {
			return nil
		}
		return []portcheck.Owner{{Key: "lanshare", Label: "局域网互传", Ports: []int{st.Port}}}
	})
}

// Start 启动服务。port<=0 用 DefaultPort；占用则向后最多重试 20 个。
// host 为空监听全部网卡（0.0.0.0）；只想要本机能传 "127.0.0.1"。
// autoStopMin>0 时到点自停（符合「不启用就不挂载」）；pin 非空时下载/上传需带对 PIN。
func Start(port int, host string, autoStopMin int, pin string) (Status, error) {
	mu.Lock()
	if srv != nil {
		st := statusLocked()
		mu.Unlock()
		return st, nil
	}
	mu.Unlock()

	if port <= 0 {
		port = DefaultPort
	}
	inbox, err := inboxDir()
	if err != nil {
		return Status{}, err
	}

	var (
		ln    net.Listener
		last  error
		usedP int
	)
	for p := port; p < port+20; p++ {
		addr := net.JoinHostPort(host, strconv.Itoa(p))
		ln, last = net.Listen("tcp", addr)
		if last == nil {
			usedP = p
			break
		}
	}
	if ln == nil {
		msg := fmt.Sprintf("端口 %d~%d 全部被占用: %v", port, port+19, last)
		setErr(msg)
		return Status{}, fmt.Errorf("%s", msg)
	}

	rootObj, err := os.OpenRoot(inbox)
	if err != nil {
		_ = ln.Close()
		return Status{}, fmt.Errorf("收件箱沙箱初始化失败: %w", err)
	}

	s := &httpServer{
		ln:        ln,
		inbox:     inbox,
		root:      rootObj,
		startedAt: time.Now().UnixMilli(),
		lim:       newRateLimiter(ratePerMinute, time.Minute),
		pin:       strings.TrimSpace(pin),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handle)
	s.http = &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(usedP)),
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout(),
		IdleTimeout:       serverIdleTimeout,
	}

	mu.Lock()
	srv = s
	lastErr = ""
	if autoStopMin > 0 {
		s.autoStopAt = time.Now().Add(time.Duration(autoStopMin) * time.Minute).UnixMilli()
		s.autoTimer = time.AfterFunc(time.Duration(autoStopMin)*time.Minute, func() {
			applog.Infof("lanshare", "到达自动停止时间，服务下线")
			_ = Stop()
		})
	}
	st := statusLocked()
	mu.Unlock()

	go func() {
		if err := s.http.Serve(ln); err != nil && err != http.ErrServerClosed {
			setErr(err.Error())
			applog.Warnf("lanshare", "服务异常退出: %v", err)
			_ = Stop()
		}
	}()
	applog.Infof("lanshare", "服务已启动: %s（收件箱 %s）", st.URLs, inbox)
	return st, nil
}

// Stop 优雅停止：先摘 httpServer，再关监听与沙箱句柄。
func Stop() error {
	mu.Lock()
	s := srv
	srv = nil
	mu.Unlock()
	if s == nil {
		return nil
	}
	if s.autoTimer != nil {
		s.autoTimer.Stop()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = s.http.Shutdown(ctx)
	_ = s.ln.Close()
	_ = s.root.Close()
	applog.Infof("lanshare", "服务已停止")
	return nil
}

// StatusNow 当前运行态。
func StatusNow() Status {
	mu.Lock()
	defer mu.Unlock()
	return statusLocked()
}

func statusLocked() Status {
	if srv == nil {
		return Status{Running: false, Inbox: inboxCached(), LastErr: lastErr}
	}
	_, portStr, _ := net.SplitHostPort(srv.ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	host := srv.http.Addr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	return Status{
		Running:     true,
		Host:        host,
		Port:        port,
		URLs:        urlsFor(host, port),
		Inbox:       srv.inbox,
		StartedAt:   srv.startedAt,
		AutoStopAt:  srv.autoStopAt,
		ReqCount:    srv.reqCount.Load(),
		UploadCount: srv.uploadCnt.Load(),
		Pin:         srv.pin,
		GrantPath:   "/g/",
		LastErr:     lastErr,
	}
}

func setErr(msg string) {
	mu.Lock()
	lastErr = msg
	mu.Unlock()
}

func readHeaderTimeout() time.Duration { return serverReadHdrs }

// urlsFor 监听地址 → 可访问 URL 列表。0.0.0.0/空 时展开为全部局域网 IPv4，
// 界面据此提示"把这个地址发给同一网络的设备"。
func urlsFor(host string, port int) []string {
	base := "http://localhost:" + strconv.Itoa(port)
	if host != "" && host != "0.0.0.0" && host != "::" && host != "127.0.0.1" {
		return []string{"http://" + host + ":" + strconv.Itoa(port)}
	}
	out := []string{base}
	for _, ip := range lanAddrs() {
		u := "http://" + ip + ":" + strconv.Itoa(port)
		if u != base {
			out = append(out, u)
		}
	}
	return out
}

func lanAddrs() []string {
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
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			var ip net.IP
			switch v := a.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.To4() == nil {
				continue
			}
			s := ip.String()
			if !seen[s] {
				seen[s] = true
				out = append(out, s)
			}
		}
	}
	return out
}

/* ============================ 路由 ============================ */

func (s *httpServer) handle(w http.ResponseWriter, r *http.Request) {
	s.reqCount.Add(1)
	if !s.lim.Allow(remoteIP(r)) {
		http.Error(w, "请求过于频繁", http.StatusTooManyRequests)
		return
	}
	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/":
		s.serveIndex(w)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/s/"):
		if !s.pinOK(r) {
			http.Error(w, "需要正确 PIN", http.StatusUnauthorized)
			return
		}
		s.handleDownload(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/g"):
		if !s.pinOK(r) {
			http.Error(w, "需要正确 PIN", http.StatusUnauthorized)
			return
		}
		s.handleGrant(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/upload":
		s.handleUpload(w, r)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

// pinOK 未设 PIN 时恒真；设了则查询参数 ?pin= 必须逐字相等。
// 失败与未授权下载一样不区分细节，只回一个笼统 401。
func (s *httpServer) pinOK(r *http.Request) bool {
	if s.pin == "" {
		return true
	}
	return r.URL.Query().Get("pin") == s.pin
}

func (s *httpServer) serveIndex(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, indexHTMLFor(s.inbox))
}

// handleUpload 流式落收件箱：不整读进内存，边收边写，超上限即断。
// 文件名净化 + os.Root 沙箱 + 重名避让（name-1.ext / name-2.ext）。
func (s *httpServer) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		http.Error(w, "上传解析失败或超出大小上限", http.StatusBadRequest)
		return
	}
	defer r.Body.(io.Closer).Close()
	saved := []string{}
	for _, fh := range r.MultipartForm.File["file"] {
		name := sanitizeName(fh.Filename)
		if name == "" || uploadBlocked(name) {
			continue
		}
		src, err := fh.Open()
		if err != nil {
			continue
		}
		dst, err := s.uniqueCreate(name)
		if err != nil {
			src.Close()
			http.Error(w, "收件箱写入失败: "+err.Error(), http.StatusInternalServerError)
			return
		}
		n, err := io.Copy(dst, src)
		_ = src.Close()
		if err != nil {
			_ = dst.Close()
			_ = s.root.Remove(name)
			accessLine(r, "upload", name, 0, false)
			http.Error(w, "上传中断（超出大小上限或写盘失败）", http.StatusRequestEntityTooLarge)
			return
		}
		_ = dst.Close()
		s.uploadCnt.Add(1)
		saved = append(saved, fmt.Sprintf("%s (%d B)", name, n))
		accessLine(r, "upload", name, n, true)
	}
	if len(saved) == 0 {
		emitProgress(Progress{Op: "upload", Name: "收件箱上传", Stage: "error", Note: "无可用文件/类型黑名单/非法名"})
		http.Error(w, "没有可保存的文件（空白名单外类型/非法名称/超大）", http.StatusBadRequest)
		return
	}
	emitProgress(Progress{Op: "upload", Name: saved[len(saved)-1], Stage: "done", Percent: 100, Note: fmt.Sprintf("已接收 %d 个", len(saved))})
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "已接收：\n"+strings.Join(saved, "\n")+"\n")
}

// uniqueCreate 在沙箱内创建不重名的文件：foo.zip → foo-1.zip → foo-2.zip …
func (s *httpServer) uniqueCreate(name string) (*os.File, error) {
	ext := filepath.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; ; i++ {
		candidate := name
		if i > 0 {
			candidate = stem + "-" + strconv.Itoa(i) + ext
		}
		f, err := s.root.OpenFile(candidate, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			return f, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
	}
}
