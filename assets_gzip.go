package main

import (
	"bytes"
	"compress/gzip"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// gzipAssets 给前端静态资源补上 gzip 压缩，只在 server 模式用。
//
// 动机：前端主包单个就有 1.6MB，公网带宽小的时候这种单文件最容易在中途
// 被截断或超时（浏览器报 net::ERR_CONTENT_LENGTH_MISMATCH 一类），整个页面
// 就白屏了。Wails 的 server 实现没有内建压缩，这里在外层补一层。
//
// 做法是启动时把 dist 下的 js/css 预先压一遍放进内存，请求时直接写压缩后的
// 字节：避免每个请求都花几十毫秒压 1.6MB。代价是常驻内存多一两 MB，对面板
// 这种体量完全值得。
func gzipAssets(next http.Handler, assets fs.FS) http.Handler {
	cache := make(map[string][]byte, 128)
	_ = fs.WalkDir(assets, "frontend/dist", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(path.Ext(p)) {
		case ".js", ".css":
		default:
			return nil
		}
		data, err := fs.ReadFile(assets, p)
		if err != nil {
			return nil
		}
		var buf bytes.Buffer
		zw, err := gzip.NewWriterLevel(&buf, gzip.BestSpeed)
		if err != nil {
			return nil
		}
		if _, err := zw.Write(data); err != nil {
			return nil
		}
		if err := zw.Close(); err != nil {
			return nil
		}
		// embed 里的路径是 frontend/dist/assets/x.js，URL 是 /assets/x.js
		cache["/"+strings.TrimPrefix(p, "frontend/dist/")] = buf.Bytes()
		return nil
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		body, ok := cache[r.URL.Path]
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		// 客户端不支持 gzip 时仍交给 Wails 处理，不在这里手工降级
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		// 按扩展名显式给 Content-Type：ES module 要求 JS 的 MIME 正确，
		// 而 mime.TypeByExtension 在 Linux 上依赖系统 /etc/mime.types，不可靠。
		w.Header().Set("Content-Type", assetContentType(r.URL.Path))
		// 文件名都带内容 hash，可以长期缓存；index.html 不走这里
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(body))
	})
}

func assetContentType(p string) string {
	switch strings.ToLower(path.Ext(p)) {
	case ".js":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	}
	return "application/octet-stream"
}
