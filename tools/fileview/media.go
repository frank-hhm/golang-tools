package fileview

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// MediaPath 音视频/大文件的流式端点路径。
// 前端 <video>/<audio>/<iframe> 直接 src 指过来，桌面 WebView2 与 server 构建同域可用。
const MediaPath = "/fileview/media"

// MediaHandler 在最外层拦 GET /fileview/media?path=..&name=..&dl=0|1，
// 其余路径原样透传 next（不影响 assets、绑定与 WS）。
//
// 为什么要它：大文件不适合 base64 灌进绑定（ReadBinaryDataURL 到 25MB 就回
// tooLarge），音视频边下边播、PDF 交给浏览器内置查看器都要靠 Range 请求；
// http.ServeContent 自带 Range/206/Content-Type/Last-Modified，不必手写。
//
// 安全口径与磁盘工具一致：可整卷浏览、无 root 约束。桌面版请求只来自内嵌
// WebView（同源）；server 构建所有对外流量先过 services/panel 登录墙。
func MediaHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != MediaPath {
			next.ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "仅支持 GET/HEAD", http.StatusMethodNotAllowed)
			return
		}
		serveMedia(w, r)
	})
}

// serveMedia 真正把文件交出去：只承认"单个已存在的文件"，目录与不存在都拒绝。
func serveMedia(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p := strings.TrimSpace(q.Get("path"))
	if p == "" {
		http.Error(w, "缺少 path 参数", http.StatusBadRequest)
		return
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		http.Error(w, "路径不合法", http.StatusBadRequest)
		return
	}
	st, err := os.Stat(abs)
	if err != nil {
		http.Error(w, "文件不存在", http.StatusNotFound)
		return
	}
	if st.IsDir() {
		http.Error(w, "目标是目录", http.StatusBadRequest)
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		http.Error(w, "打开文件失败", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	// dl=1 强制浏览器下载：server 构建没有原生保存对话框，靠这个头触发
	if q.Get("dl") == "1" {
		name := strings.TrimSpace(q.Get("name"))
		if name == "" {
			name = filepath.Base(abs)
		}
		w.Header().Set("Content-Disposition", contentDisposition(name))
	}
	// 文件名决定 MIME（扩展名判定与 Classify 同口径），ServeContent 不会再猜
	http.ServeContent(w, r, filepath.Base(abs), st.ModTime(), f)
}

// contentDisposition 拼下载头：filename 走 ASCII 兜底，中文原名用 RFC 5987。
// 引号与分号是分隔符，必须从名字里剔掉，否则会截断指令。
func contentDisposition(name string) string {
	safe := strings.NewReplacer(`"`, "", `\`, "", ";", "", "\r", "", "\n", "").Replace(name)
	if safe == "" {
		safe = "download"
	}
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		safe, url.PathEscape(name))
}
