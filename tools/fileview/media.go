package fileview

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"

	"Tools/services/sysx"
	"Tools/tools/docspace"
)

// MediaPath 音视频/大文件的流式端点路径。
// 前端 <video>/<audio>/<iframe> 直接 src 指过来，桌面 WebView2 与 server 构建同域可用。
const MediaPath = "/fileview/media"

// MediaHandler 在最外层拦 GET /fileview/media?path=..&name=..&dl=0|1 与
// GET /fileview/thumb?src=&sid=&path=&w=..，其余路径原样透传 next
// （不影响 assets、绑定与 WS）。
//
// 为什么要它：大文件不适合 base64 灌进绑定（ReadBinaryDataURL 到 25MB 就回
// tooLarge），音视频边下边播、PDF 交给浏览器内置查看器都要靠 Range 请求；
// http.ServeContent 自带 Range/206/Content-Type/Last-Modified，不必手写。
// 缩略图同域同机制：一次列表页可能几百张图，只能靠 <img> 并发懒加载。
//
// 安全口径与磁盘工具一致：可整卷浏览、无 root 约束。桌面版请求只来自内嵌
// WebView（同源）；server 构建所有对外流量先过 services/panel 登录墙。
func MediaHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case MediaPath:
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				http.Error(w, "仅支持 GET/HEAD", http.StatusMethodNotAllowed)
				return
			}
			serveMedia(w, r)
		case ThumbPath:
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				http.Error(w, "仅支持 GET/HEAD", http.StatusMethodNotAllowed)
				return
			}
			serveThumb(w, r)
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// serveMedia 真正把文件交出去：经 docref 按源类型分发（local 走 os，sftp 走会话
// 访问器），只承认"单个已存在的文件"，目录与不存在都拒绝。
//
// 兼容：旧调用只带 ?path=（无 src）时，docspace.Parse 缺省按 local 处理，行为不变；
// 新调用 ?src=&sid=&path= 让远端文件同样走 Range 流式预览。
func serveMedia(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ref, err := docspace.ParseQuery(q)
	if err != nil {
		if errors.Is(err, sysx.ErrUnsupported) {
			http.Error(w, "该源类型暂不支持在线预览", http.StatusNotImplemented)
			return
		}
		http.Error(w, "缺少或非法的 path/src 参数", http.StatusBadRequest)
		return
	}

	f, err := ref.OpenRead()
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "文件不存在", http.StatusNotFound)
			return
		}
		http.Error(w, "打开文件失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil {
		http.Error(w, "读取文件信息失败", http.StatusInternalServerError)
		return
	}
	if st.IsDir() {
		http.Error(w, "目标是目录", http.StatusBadRequest)
		return
	}

	// dl=1 强制浏览器下载：server 构建没有原生保存对话框，靠这个头触发
	name := strings.TrimSpace(q.Get("name"))
	if name == "" {
		name = baseName(ref.Path)
	}
	if q.Get("dl") == "1" {
		w.Header().Set("Content-Disposition", contentDisposition(name))
	}
	// 文件名决定 MIME（扩展名判定与 Classify 同口径），ServeContent 不会再猜；
	// docspace.File 内嵌 io.ReadSeeker，local 与 sftp 都能直接交 Range 流式。
	http.ServeContent(w, r, name, st.ModTime(), f)
}

// baseName 取路径末段：同时按 / 与 \ 切，兼容 local（Windows 反斜杠）与 sftp（POSIX 正斜杠）。
func baseName(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return "download"
	}
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		p = p[i+1:]
	}
	if p == "" {
		return "download"
	}
	return p
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
