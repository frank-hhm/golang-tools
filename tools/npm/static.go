package npm

// ============================================================
// 内置静态站点服务器（Go 实现，替代 http-server 等外部 npm 包）
//
// 为什么自带而不是 npm 装 http-server：
//   - 零网络/零依赖，注册表被墙或离线也能用；
//   - 生命周期完全可控（http.Server.Shutdown 优雅停止），主页状态监听稳定；
//   - 记录请求数/字节数，状态面板能给出真实吞吐数据。
//
// 能力：静态文件 + 列目录（可关）、SPA 回退 index.html（可开）、
// /__gonfo__ 轻量状态探针（返回 JSON：版本/运行时长/计数）。
// ============================================================

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// StaticOptions 静态服务器选项
type StaticOptions struct {
	SPA       bool
	Directory bool
}

// StaticServer 一个运行中的静态站点服务
type StaticServer struct {
	id        string
	port      int
	docRoot   string
	opts      StaticOptions
	logFile   *os.File
	srv       *http.Server
	started   time.Time
	requests  int64
	bytesSent int64
}

// statusProbe 状态探针路径（前端/监控可直接访问）
const statusProbe = "/__gonfo__"

// serveHTTP 处理一个请求
func (s *StaticServer) serveHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "405 Method Not Allowed", http.StatusMethodNotAllowed)
		s.log(r, http.StatusMethodNotAllowed, 0)
		return
	}
	clean := filepath.Clean("/" + r.URL.Path)
	if clean == statusProbe {
		s.writeStatus(w)
		s.log(r, http.StatusOK, 0)
		return
	}
	fp := filepath.Join(s.docRoot, clean)
	// 防目录穿越：拼接结果必须仍在站点根内
	if fp != s.docRoot && !strings.HasPrefix(fp, s.docRoot+string(os.PathSeparator)) {
		http.Error(w, "403 Forbidden", http.StatusForbidden)
		s.log(r, http.StatusForbidden, 0)
		return
	}
	st, err := os.Stat(fp)
	if err != nil || !strings.HasPrefix(fp, s.docRoot) {
		if err != nil && os.IsNotExist(err) && s.opts.SPA && !s.isAssetPath(clean) {
			s.serveFile(w, r, filepath.Join(s.docRoot, "index.html"), false)
			return
		}
		http.Error(w, "404 Not Found: "+clean, http.StatusNotFound)
		s.log(r, http.StatusNotFound, 0)
		return
	}
	if st.IsDir() {
		if !strings.HasSuffix(r.URL.Path, "/") {
			http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
			s.log(r, http.StatusMovedPermanently, 0)
			return
		}
		if idx := filepath.Join(fp, "index.html"); fileExists(idx) {
			s.serveFile(w, r, idx, false)
			return
		}
		if s.opts.Directory {
			s.serveListing(w, r, fp)
			return
		}
		http.Error(w, "403 Forbidden: 目录列表已关闭", http.StatusForbidden)
		s.log(r, http.StatusForbidden, 0)
		return
	}
	s.serveFile(w, r, fp, false)
}

// isAssetPath 判断是否像静态资源请求（带扩展名 → 不回退 index.html）
func (s *StaticServer) isAssetPath(p string) bool {
	return filepath.Ext(p) != ""
}

func (s *StaticServer) serveFile(w http.ResponseWriter, r *http.Request, path string, _ bool) {
	f, err := os.Open(path)
	if err != nil {
		http.Error(w, "404 Not Found", http.StatusNotFound)
		s.log(r, http.StatusNotFound, 0)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		s.log(r, http.StatusInternalServerError, 0)
		return
	}
	if !st.Mode().IsRegular() {
		http.Error(w, "403 Forbidden", http.StatusForbidden)
		s.log(r, http.StatusForbidden, 0)
		return
	}
	http.ServeContent(w, r, filepath.Base(path), st.ModTime(), f)
	// ServeContent 内部写入，字节数用 Content-Length 估算即可
	n, _ := strconv.ParseInt(w.Header().Get("Content-Length"), 10, 64)
	s.log(r, http.StatusOK, n)
}

func (s *StaticServer) serveListing(w http.ResponseWriter, r *http.Request, dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		http.Error(w, "500 Internal Server Error", http.StatusInternalServerError)
		s.log(r, http.StatusInternalServerError, 0)
		return
	}
	type item struct {
		Name  string
		IsDir bool
		Size  int64
		Time  time.Time
	}
	var dirs, files []item
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		it := item{Name: e.Name(), IsDir: e.IsDir(), Size: info.Size(), Time: info.ModTime()}
		if it.IsDir {
			dirs = append(dirs, it)
		} else {
			files = append(files, it)
		}
	}
	sort.Slice(dirs, func(i, j int) bool { return dirs[i].Name < dirs[j].Name })
	sort.Slice(files, func(i, j int) bool { return files[i].Name < files[j].Name })

	base := r.URL.Path
	if !strings.HasSuffix(base, "/") {
		base += "/"
	}
	var b strings.Builder
	b.WriteString("<!doctype html><meta charset=\"utf-8\"><title>Index of " + html.EscapeString(base) + "</title>")
	b.WriteString("<style>body{font:14px/1.6 Consolas,monospace;margin:24px}a{text-decoration:none;color:#08c}a:hover{text-decoration:underline}" +
		"table{border-collapse:collapse;width:100%}th,td{padding:4px 12px 4px 0;text-align:left;font-weight:normal}" +
		"th{color:#888;border-bottom:1px solid #ddd}.size{text-align:right;color:#888}</style>")
	b.WriteString("<h2>Index of " + html.EscapeString(base) + "</h2><table><tr><th>Name</th><th class=\"size\">Size</th><th>Modified</th></tr>")
	if base != "/" {
		b.WriteString("<tr><td><a href=\"../\">..</a></td><td></td><td></td></tr>")
	}
	writeRow := func(it item) {
		name := it.Name
		href := name
		if it.IsDir {
			name += "/"
			href += "/"
		}
		size := "-"
		if !it.IsDir {
			size = humanBytes(it.Size)
		}
		b.WriteString(fmt.Sprintf("<tr><td><a href=\"%s\">%s</a></td><td class=\"size\">%s</td><td>%s</td></tr>",
			href, html.EscapeString(name), size, it.Time.Format("2006-01-02 15:04")))
	}
	for _, it := range dirs {
		writeRow(it)
	}
	for _, it := range files {
		writeRow(it)
	}
	b.WriteString("</table><p style=\"color:#888;margin-top:16px\">served by go-tools · Node " +
		html.EscapeString(nodeOfInstance(s.id)) + "</p>")

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, b.String())
	s.log(r, http.StatusOK, int64(b.Len()))
}

func (s *StaticServer) writeStatus(w http.ResponseWriter) {
	payload := map[string]any{
		"id":            s.id,
		"running":       true,
		"node":          nodeOfInstance(s.id),
		"port":          s.port,
		"docRoot":       s.docRoot,
		"uptimeSeconds": int64(time.Since(s.started).Seconds()),
		"requests":      atomic.LoadInt64(&s.requests),
		"bytesSent":     atomic.LoadInt64(&s.bytesSent),
		"pid":           os.Getpid(),
		"server":        "go-tools static",
		"spa":           s.opts.SPA,
		"directory":     s.opts.Directory,
		"timestamp":     time.Now().UnixMilli(),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(payload)
}

// log 写访问日志（不记录探针请求）
func (s *StaticServer) log(r *http.Request, status int, n int64) {
	atomic.AddInt64(&s.requests, 1)
	if n > 0 {
		atomic.AddInt64(&s.bytesSent, n)
	}
	if s.logFile == nil || r.URL.Path == statusProbe {
		return
	}
	_, _ = fmt.Fprintf(s.logFile, "%s %s %s %s -> %d %s\n",
		time.Now().Format("2006-01-02 15:04:05"), r.RemoteAddr, r.Method,
		r.URL.RequestURI(), status, humanBytes(n))
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func humanBytes(v int64) string {
	if v <= 0 {
		return "0 B"
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	f := float64(v)
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return strconv.FormatInt(v, 10) + " B"
	}
	return strconv.FormatFloat(f, 'f', 1, 64) + " " + units[i]
}

// freePort 探测可用端口
func freePort(port int) error {
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return err
	}
	_ = ln.Close()
	return nil
}
