package panel

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Options 构造面板服务需要的展示信息。
type Options struct {
	Title   string // 应用名（取自 config.json 的 general.appName）
	Version string // 版本号，展示在登录页页脚
	Logo    string // Logo 的 data URI，可空（空则用应用名首字母）
}

// Server 是面板对外的 HTTP 入口：登录鉴权 + 安全入口 + 反向代理到 Wails 内部服务。
//
// 之所以走"自建前端服务 + 反代 Wails"而不是给 Wails 加中间件：
// Wails 的 server 模式对 /wails/events、/wails/stream/ws 两个 WebSocket 端点
// 是直接挂在它的 mux 上的，拿不到中间件钩子；而这两个端点会广播各工具的
// 日志/终端输出。只有在最外层统一入口才盖得住全部路径。
type Server struct {
	mu       sync.RWMutex
	set      Settings
	httpSrv  *http.Server
	ln       net.Listener
	started  time.Time
	upstream *url.URL
	proxy    *httputil.ReverseProxy
	tmpl     *template.Template
	opts     Options
	fails    *limiter
}

// New 构造面板服务。upstreamAddr 形如 127.0.0.1:34567（Wails 内部监听地址）。
func New(upstreamAddr string, set Settings, opts Options) (*Server, error) {
	u, err := url.Parse("http://" + upstreamAddr)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("login").Parse(loginPageHTML)
	if err != nil {
		return nil, err
	}
	s := &Server{
		set:      set,
		started:  time.Now(),
		upstream: u,
		tmpl:     tmpl,
		opts:     opts,
		fails:    newLimiter(5, 10*time.Minute, 10*time.Minute),
	}

	proxy := httputil.NewSingleHostReverseProxy(u)
	// 关键：不要让代理自己加 Accept-Encoding/gzip 解压。
	// Wails 侧已把 assets 预压成 gzip 并自己写 Content-Encoding，
	// 代理若声称支持 gzip 又解压，Content-Length 与实际字节会对不上。
	proxy.Transport = &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		DisableCompression:  true,
		MaxIdleConnsPerHost: 32,
		IdleConnTimeout:     90 * time.Second,
	}
	// 终端输出、docker logs 这类流式响应：立刻往下刷，别攒
	proxy.FlushInterval = 50 * time.Millisecond
	dir := proxy.Director
	proxy.Director = func(r *http.Request) {
		origHost := r.Host
		dir(r)
		// 有意不把 r.Host 改成上游地址：
		//  - Wails 的 WebSocket 升级（/wails/events、/wails/stream/ws）默认校验
		//    Origin 与 Host 同源，改成 127.0.0.1:内部端口后握手会被拒，
		//    前端事件通道断开 -> 终态/日志推送全掉；
		//  - 上游只听回环，Host 头对它没有任何路由意义。
		r.Header.Set("X-Forwarded-Host", origHost)
		if ip, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			if prev := r.Header.Get("X-Forwarded-For"); prev != "" {
				r.Header.Set("X-Forwarded-For", prev+", "+ip)
			} else {
				r.Header.Set("X-Forwarded-For", ip)
			}
		}
		// 外层还有 nginx / 其它反代时保留它给出的协议，否则按本层连接判断
		if r.Header.Get("X-Forwarded-Proto") == "" {
			if r.TLS != nil {
				r.Header.Set("X-Forwarded-Proto", "https")
			} else {
				// 前面的 TLS 可能已被 nginx 终结，它一般会带上这个头
				r.Header.Set("X-Forwarded-Proto", "http")
			}
		}
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		// 后端还没起来（启动早期）或已退出：给一个能看懂的提示，别让浏览器空转
		log.Printf("panel: 代理请求失败 %s: %v", r.URL.Path, err)
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("面板后端暂不可用，请稍候刷新；若持续出现请查看 logs/app 下的日志。\n"))
	}
	s.proxy = proxy
	return s, nil
}

// Start 绑定端口并开始对外服务。
func (s *Server) Start() error {
	s.mu.RLock()
	addr := s.set.ListenAddr()
	s.mu.RUnlock()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("监听 %s 失败: %w", addr, err)
	}
	srv := &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 15 * time.Second,
		// 不设 WriteTimeout：/wails/stream 是长连接，写超时会把终端会话掐断
		IdleTimeout: 120 * time.Second,
	}
	s.mu.Lock()
	s.ln, s.httpSrv = ln, srv
	s.mu.Unlock()

	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("panel: 服务退出: %v", err)
		}
	}()
	return nil
}

// Stop 关闭对外服务（进程退出时调用）。
func (s *Server) Stop() {
	s.mu.Lock()
	srv := s.httpSrv
	s.mu.Unlock()
	if srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
}

// Settings 返回当前生效配置的副本。
func (s *Server) Settings() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.set
}

// Apply 保存并即时应用新配置：改端口会重新 Listen，无需重启进程。
//
// 返回的 moved 表示监听地址是否真的变了（前端据此提示用户换地址访问）。
// 新地址绑不上（端口占用/无权限）时回滚到旧配置并重新监听，错误一并返回。
func (s *Server) Apply(next Settings) (saved Settings, moved bool, err error) {
	cur := s.Settings()
	next.Secret = cur.Secret // 密钥不接受外部覆盖，换掉会让所有已登录会话失效
	next.Epoch = cur.Epoch
	if normalizeEntry(next.EntryPath) != cur.EntryPath || strings.TrimSpace(next.Username) != cur.Username {
		next.Epoch = cur.Epoch + 1
	}
	saved, err = Save(next)
	if err != nil {
		return cur, false, err
	}

	needRebind := saved.ListenAddr() != cur.ListenAddr()
	s.mu.Lock()
	s.set = saved
	s.mu.Unlock()
	if !needRebind {
		return saved, false, nil
	}

	if rerr := s.rebind(); rerr != nil {
		// 回滚：文件与内存都恢复旧配置，并确保旧地址仍可服务
		if rollback, rberr := Save(cur); rberr == nil {
			s.mu.Lock()
			s.set = rollback
			s.mu.Unlock()
		}
		_ = s.rebind()
		return cur, false, fmt.Errorf("新地址 %s 无法监听（端口被占用或无权限）: %w", saved.ListenAddr(), rerr)
	}
	return saved, true, nil
}

// rebind 按当前配置重新监听；成功后再关旧监听，失败则旧监听保持不动。
func (s *Server) rebind() error {
	s.mu.RLock()
	addr := s.set.ListenAddr()
	oldSrv := s.httpSrv
	s.mu.RUnlock()

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	srv := &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 15 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	s.mu.Lock()
	s.ln, s.httpSrv = ln, srv
	s.mu.Unlock()

	go func() {
		if serr := srv.Serve(ln); serr != nil && serr != http.ErrServerClosed {
			log.Printf("panel: 服务退出: %v", serr)
		}
	}()
	if oldSrv != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = oldSrv.Shutdown(ctx)
		}()
	}
	return nil
}

// ServeHTTP 是面板的总入口：按路径分流到登录页、管理接口或反代。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	set := s.Settings()
	path := r.URL.Path

	// 健康检查：给 systemd / 反向代理探活用，不含任何敏感信息
	if path == "/health" {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
		return
	}

	if path == "/login" {
		if r.Method == http.MethodPost {
			s.handleLoginPost(w, r, set)
		} else {
			s.handleLoginPage(w, r, set, "")
		}
		return
	}
	if path == "/logout" || path == "/api/panel/logout" {
		clearSession(w)
		if wantsJSON(r) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true})
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	if strings.HasPrefix(path, "/api/panel/") {
		s.handleAPI(w, r, set)
		return
	}

	// 安全入口：知道入口路径的人先拿到入口标记，再被引到首页。
	// 容许末尾多一个斜杠：用户从聊天窗口/地址栏粘过来常带 /，不能因为多一个字符就 404。
	if set.EntryPath != "" && (path == set.EntryPath || path == set.EntryPath+"/") && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
		setEntryCookie(w, set, true)
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}

	if !set.Enabled {
		s.proxy.ServeHTTP(w, r)
		return
	}

	_, logged := verifySession(set, sessionCookieValue(r))
	if !logged {
		// 开了安全入口却没带标记：对外表现得像一个不存在的主机，
		// 扫描器扫到这个端口也只会得到 404，不会暴露"这里有个面板"。
		if !hasEntryCookie(r, set) {
			http.NotFound(w, r)
			return
		}
		if wantsHTML(r) {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(path), http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set(markerHeader, "required")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"未登录或登录已过期"}`))
		return
	}
	s.proxy.ServeHTTP(w, r)
}

// markerHeader 标记"这个 401 是面板的登录墙"，
// 前端据此跳登录页，而不是把错误当业务异常弹出来。
const markerHeader = "X-Panel-Auth"

func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request, set Settings, errMsg string) {
	// 关闭登录保护时不存在"会话"，登录页只是个死胡同：直接回首页，
	// 避免书签/历史里残留的 /login 把人卡在一个永远登不进的面板上。
	if !set.Enabled {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusFound)
		return
	}
	if _, ok := verifySession(set, sessionCookieValue(r)); ok {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusFound)
		return
	}
	opts := s.opts
	initial := "GT"
	if rs := []rune(strings.TrimSpace(opts.Title)); len(rs) > 0 {
		initial = strings.ToUpper(string(rs[0]))
	}
	action := "/login"
	data := struct {
		Title, Version, Initial, User, Error, Next, Action, Addr string
		// Logo 是 data URI：html/template 默认会把 data: 协议替换成 #ZgotmplZ，
		// 这里显式声明为可信 URL（内容来自程序内置的 logo 字节）
		Logo template.URL
	}{
		Title:   opts.Title,
		Version: opts.Version,
		Logo:    template.URL(opts.Logo),
		Initial: initial,
		User:    set.Username,
		Error:   errMsg,
		Next:    safeNext(r.URL.Query().Get("next")),
		Action:  action,
		Addr:    fmt.Sprintf("%d 端口", set.Port),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = s.tmpl.Execute(w, data)
}

func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request, set Settings) {
	if err := r.ParseForm(); err != nil {
		s.handleLoginPage(w, r, set, "表单解析失败，请重试")
		return
	}
	// Origin 校验：同源之外的浏览器表单不能直接打进来（Cookie 是 SameSite=Lax 已挡一层）
	if !sameOrigin(r) {
		s.handleLoginPage(w, r, set, "拒绝来自非本站的登录请求")
		return
	}
	ip := clientIP(r)
	if wait, blocked := s.fails.blocked(ip); blocked {
		s.handleLoginPage(w, r, set, fmt.Sprintf("尝试次数过多，请 %d 分钟后再试", int(wait.Minutes())+1))
		return
	}
	user := strings.TrimSpace(r.PostFormValue("username"))
	pass := r.PostFormValue("password")
	if user != set.Username || !set.CheckPassword(pass) {
		s.fails.fail(ip)
		log.Printf("panel: 登录失败 user=%q ip=%s", user, ip)
		s.handleLoginPage(w, r, set, "账号或密码不正确")
		return
	}
	s.fails.reset(ip)
	setSessionCookie(w, set, sessionTTL(set))
	log.Printf("panel: 登录成功 user=%q ip=%s", user, ip)
	http.Redirect(w, r, safeNext(r.PostFormValue("next")), http.StatusFound)
}

// handleAPI 处理面板自身的管理接口（全部要求已登录 + 自定义请求头防 CSRF）。
func (s *Server) handleAPI(w http.ResponseWriter, r *http.Request, set Settings) {
	// 关闭登录保护时不存在会话，管理接口按"本机管理员"放行。
	// 否则设置页刚把开关关掉，下一帧 getPanelInfo() 就拿到 401，
	// 前端 request() 会当成"登录过期"踢回 /login——而 /login 又被上面
	// 重定向回首页，表现为面板闪一下变空白。
	_, logged := verifySession(set, sessionCookieValue(r))
	if set.Enabled && !logged {
		w.Header().Set(markerHeader, "required")
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": "未登录或登录已过期"})
		return
	}
	// 自定义头不能跨站点直接带：配合 SameSite=Lax Cookie 挡住 CSRF
	if r.Method == http.MethodPost && r.Header.Get(csrfHeader) == "" {
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "缺少请求头 " + csrfHeader})
		return
	}

	switch r.URL.Path {
	case "/api/panel/info":
		s.apiInfo(w, set)
	case "/api/panel/settings":
		s.apiSaveSettings(w, r, set)
	case "/api/panel/password":
		s.apiPassword(w, r, set)
	default:
		http.NotFound(w, r)
	}
}

// csrfHeader 面板管理接口必须带的自定义请求头。
const csrfHeader = "X-Requested-With"

func (s *Server) apiInfo(w http.ResponseWriter, set Settings) {
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled":       set.Enabled,
		"username":      set.Username,
		"host":          set.Host,
		"port":          set.Port,
		"entryPath":     set.EntryPath,
		"sessionHours":  set.SessionHours,
		"updatedAt":     set.UpdatedAt,
		"accessURL":     set.AccessURL(),
		"listen":        set.ListenAddr(),
		"uptime":        int(time.Since(s.started).Seconds()),
		"version":       s.opts.Version,
		"passwordIsSet": set.PasswordHash != "",
	})
}

type settingsReq struct {
	Host         *string `json:"host"`
	Port         *int    `json:"port"`
	EntryPath    *string `json:"entryPath"`
	Username     *string `json:"username"`
	SessionHours *int    `json:"sessionHours"`
	Enabled      *bool   `json:"enabled"`
}

func (s *Server) apiSaveSettings(w http.ResponseWriter, r *http.Request, set Settings) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "只支持 POST"})
		return
	}
	var req settingsReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体不是合法的 JSON"})
		return
	}
	next := set
	if req.Host != nil {
		next.Host = *req.Host
	}
	if req.Port != nil {
		next.Port = *req.Port
	}
	if req.EntryPath != nil {
		if err := ValidateEntry(*req.EntryPath); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
			return
		}
		next.EntryPath = normalizeEntry(*req.EntryPath)
	}
	if req.Username != nil {
		next.Username = strings.TrimSpace(*req.Username)
	}
	if req.SessionHours != nil {
		next.SessionHours = *req.SessionHours
	}
	if req.Enabled != nil {
		next.Enabled = *req.Enabled
	}

	wasUser := set.Username
	saved, moved, err := s.Apply(next)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	// 账号或入口变了会推进会话代数：给当前浏览器补发一次 Cookie，避免自己被踢下线
	if saved.Epoch != set.Epoch {
		setSessionCookie(w, saved, sessionTTL(saved))
		setEntryCookie(w, saved, saved.EntryPath == "" || hasEntryCookie(r, saved))
	}
	// 刚打开登录保护：此刻浏览器还没有会话，不补发就会在下一帧被 401 打回登录页
	if !set.Enabled && saved.Enabled {
		setSessionCookie(w, saved, sessionTTL(saved))
	}
	msg := "配置已保存并生效"
	if moved {
		msg = fmt.Sprintf("监听地址已切换，请改用 %s 访问", saved.AccessURL())
	} else if wasUser != saved.Username {
		msg = "账号已更新，其它设备需重新登录"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"message":   msg,
		"accessURL": saved.AccessURL(),
		"moved":     moved,
		"info": map[string]any{
			"username":  saved.Username,
			"host":      saved.Host,
			"port":      saved.Port,
			"entryPath": saved.EntryPath,
			"enabled":   saved.Enabled,
			"accessURL": saved.AccessURL(),
			"updatedAt": saved.UpdatedAt,
			"listen":    saved.ListenAddr(),
		},
	})
}

func (s *Server) apiPassword(w http.ResponseWriter, r *http.Request, set Settings) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "只支持 POST"})
		return
	}
	var req struct {
		Current string `json:"current"`
		Next    string `json:"next"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "请求体不是合法的 JSON"})
		return
	}
	if !set.CheckPassword(req.Current) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "当前密码不正确"})
		return
	}
	if len(strings.TrimSpace(req.Next)) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "新密码至少 8 位"})
		return
	}
	saved, err := SetPassword(req.Next)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": err.Error()})
		return
	}
	s.mu.Lock()
	s.set = saved
	s.mu.Unlock()
	// 会话代数已变：当前浏览器续一个新 Cookie，其它设备自然失效
	setSessionCookie(w, saved, sessionTTL(saved))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "message": "密码已更新，其它设备需重新登录"})
}

// ---------------- 小工具 ----------------

func sessionCookieValue(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json") ||
		strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") ||
		strings.HasPrefix(r.URL.Path, "/api/")
}

func wantsHTML(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	if r.URL.Path == "/" || r.URL.Path == "/index.html" {
		return true
	}
	accept := r.Header.Get("Accept")
	return strings.Contains(accept, "text/html")
}

// sameOrigin 判断跨站表单：Origin 存在时必须与 Host 一致。
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		// 老浏览器或直接 curl 时不带 Origin：交由账号密码本身把关
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

// safeNext 只允许站内相对路径，杜绝登录后被带去外站。
func safeNext(next string) string {
	next = strings.TrimSpace(next)
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.HasPrefix(next, "/login") {
		return "/"
	}
	return next
}

// clientIP 取真实来源 IP：直连取 RemoteAddr；本机反代（nginx 同机）时取 XFF 首段。
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if isLoopback(host) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			if first, _, ok := strings.Cut(xff, ","); ok {
				return strings.TrimSpace(first)
			}
			return strings.TrimSpace(xff)
		}
	}
	return host
}

func isLoopback(host string) bool {
	if host == "::1" || host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// ---------------- 登录失败限速 ----------------

// limiter 简单的失败计数限速：同一 IP 在 window 内失败 maxFail 次即锁定 lock 时长。
// 只保护登录接口，进程重启清零（内存态，够用且不会积累敏感数据）。
type limiter struct {
	mu      sync.Mutex
	maxFail int
	window  time.Duration
	lock    time.Duration
	m       map[string]*attempt
}

type attempt struct {
	fails int
	first time.Time
	last  time.Time
	until time.Time
}

func newLimiter(maxFail int, window, lock time.Duration) *limiter {
	return &limiter{maxFail: maxFail, window: window, lock: lock, m: make(map[string]*attempt)}
}

// blocked 判断是否处于锁定期，返回还需等待的时长。
func (l *limiter) blocked(ip string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.m[ip]
	if a == nil || a.until.IsZero() {
		return 0, false
	}
	wait := time.Until(a.until)
	if wait <= 0 {
		delete(l.m, ip)
		return 0, false
	}
	return wait, true
}

func (l *limiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	a := l.m[ip]
	if a == nil || now.Sub(a.first) > l.window {
		a = &attempt{first: now}
		l.m[ip] = a
	}
	a.fails++
	a.last = now
	if a.fails >= l.maxFail {
		a.until = now.Add(l.lock)
		// 触发锁定后清空窗口，锁定期满重新计数
		a.fails = 0
		a.first = now
	}
	// 顺手清理过期的键，避免长期运行时 map 无限增长
	if len(l.m) > 1024 {
		for k, v := range l.m {
			if now.Sub(v.last) > l.window && v.until.Before(now) {
				delete(l.m, k)
			}
		}
	}
}

func (l *limiter) reset(ip string) {
	l.mu.Lock()
	delete(l.m, ip)
	l.mu.Unlock()
}
