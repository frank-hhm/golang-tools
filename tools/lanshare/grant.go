package lanshare

// 授权下载门户路由（/g/<id>[/子路径]）。
//
// /g/ ：常驻门户里被显式授权的本机路径。
//   - 目录不带子路径 → 流式 zip 整包下载（递归授权才允许逐项浏览，否则只给整包）。
//   - 单文件 / 递归目录的子项 → 逐项下载，路径经 joinGrant 防穿越，未授权一律 404。

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// handleGrant 处理 /g、/g/、/g/<id>、/g/<id>/<子路径>。
func (s *httpServer) handleGrant(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/g")
	rest = strings.TrimPrefix(rest, "/")
	parts := strings.SplitN(rest, "/", 2)
	id := safeSegment(parts[0])
	if id == "" {
		// /g 或 /g/：门户入口，回极简授权列表页（无静态资源依赖）。
		s.serveGrantIndex(w)
		return
	}
	sub := ""
	if len(parts) > 1 {
		sub = parts[1]
	}

	g, ok := touchGrant(id, time.Now())
	if !ok {
		http.NotFound(w, r)
		accessLine(r, "download", id, 0, false)
		return
	}
	st, err := os.Stat(g.Path)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if st.IsDir() {
		if sub == "" {
			// 目录整包流式 zip
			name := g.Name + ".zip"
			w.Header().Set("Content-Disposition", disposition(name))
			w.Header().Set("Content-Type", "application/zip")
			total := dirBytes(g.Path)
			if err := writeZipStream(r.Context(), w, g.Path, name, total); err != nil {
				accessLine(r, "pack", name, 0, false)
				return
			}
			accessLine(r, "pack", name, 0, true)
			return
		}
		if !g.Recursive {
			http.NotFound(w, r) // 非递归授权不允许逐项浏览
			accessLine(r, "download", g.Name+"/"+sub, 0, false)
			return
		}
		full, err := joinGrant(g.Path, sub)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		fst, err := os.Stat(full)
		if err != nil || fst.IsDir() {
			http.NotFound(w, r) // 只下放文件，目录项不列举
			return
		}
		s.serveGrantFile(w, r, full, filepath.Base(full))
		return
	}

	// 授权实体是单文件：忽略子路径
	s.serveGrantFile(w, r, g.Path, g.Name)
}

// serveGrantFile 打开并回一个被授权的文件，保留 Range 续传能力。
func (s *httpServer) serveGrantFile(w http.ResponseWriter, r *http.Request, full, name string) {
	f, err := os.Open(full)
	if err != nil {
		http.NotFound(w, r)
		accessLine(r, "download", name, 0, false)
		return
	}
	defer f.Close()
	w.Header().Set("Content-Disposition", disposition(name))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, name, time.UnixMilli(0), f)
	accessLine(r, "download", name, 0, true)
	emitProgress(Progress{Op: "download", Name: name, Stage: "done", Percent: 100, Note: "已下发"})
}

// serveGrantIndex 授权门户入口页：只提示「用链接或从应用内授权」，不泄露路径。
func (s *httpServer) serveGrantIndex(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, grantIndexHTML)
}

const grantIndexHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>局域网互传</title><style>
body{font-family:system-ui,-apple-system,"Segoe UI",sans-serif;margin:0;padding:24px;background:#f6f7f9;color:#1d2129}
.card{background:#fff;border:1px solid #e5e6eb;border-radius:10px;padding:20px;max-width:560px;margin:0 auto}
h1{font-size:18px;margin:0 0 8px}p{font-size:13px;color:#4e5969;line-height:1.7}
a{color:#165dff}
</style></head><body><div class="card"><h1>局域网互传门户</h1>
<p>这里是被授权内容的下载入口。请通过应用内「分享/授权」生成的具体链接访问；<a href="/">回到上传页</a>。</p>
<p>未经授权的路径一律返回 404，不显示是否存在。</p></div></body></html>`
