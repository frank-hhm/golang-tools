package fileview

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // 注册 gif 解码器
	"image/jpeg"
	_ "image/jpeg" // 注册 jpeg 解码器
	_ "image/png"  // 注册 png 解码器
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"Tools/services/sysx"
	"Tools/tools/docspace"
	"Tools/tools/filekind"
)

// ThumbPath 图片缩略图端点：GET /fileview/thumb?src=&sid=&path=&w=。
// 前端 <img> 直接指过来，与 media 端点共用同一套三元组参数与源分发。
const ThumbPath = "/fileview/thumb"

const (
	// thumbMaxSource 参与缩略图的源文件字节上限：要整张读进内存再解码，超了不值得，
	// 直接退类型图标（这么大的图当缩略图看也没意义）。
	thumbMaxSource = 25 << 20
	// thumbDefaultDim / thumbMaxDim 输出长边像素与允许上限。
	thumbDefaultDim = 320
	thumbMaxDim     = 640
	// thumbJPEGQuality 卡片尺寸下 80 足够，再高只是白涨体积。
	thumbJPEGQuality = 80
	// thumbCacheKeepFiles 缓存文件数上限，超出按最旧删除。单张几十 KB，512 张
	// 也就几十 MB，够日常翻用；老图删了重算即可。
	thumbCacheKeepFiles = 512
)

// serveThumb 出图片条目的缩略图 JPEG，命中磁盘缓存则直接回缓存。
//
// 为什么走 HTTP 端点而不是绑定返回值：几百张图 base64 灌进一次调用会把绑定层
// 噎住（与 media.go 同一个理由），而 <img> 天然支持并发懒加载与浏览器缓存。
//
// 缓存键含"实体大小 + 修改时间 + 目标尺寸"，所以文件一改键就变、旧图自然失效，
// 不需要显式清理；体积靠数量上限兜住。
//
// 安全口径与 serveMedia 一致（同一层拦截、同一套三元组参数）。
func serveThumb(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ref, err := docspace.ParseQuery(q)
	if err != nil {
		if errors.Is(err, sysx.ErrUnsupported) {
			http.Error(w, "该源类型暂不支持缩略图", http.StatusNotImplemented)
			return
		}
		http.Error(w, "缺少或非法的 path/src 参数", http.StatusBadRequest)
		return
	}
	// 先看后缀再碰文件：非图片、以及标准库解不了的 webp/bmp/svg 一律快速拒绝，
	// 省掉一次开文件与整张读入。
	if !filekind.Thumbable(ref.Path) {
		http.Error(w, "该类型不生成缩略图", http.StatusUnsupportedMediaType)
		return
	}
	dim := thumbDim(q.Get("w"))

	st, err := ref.Stat()
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "文件不存在", http.StatusNotFound)
			return
		}
		http.Error(w, "读取文件信息失败: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if st.IsDir() {
		http.Error(w, "目标是目录", http.StatusBadRequest)
		return
	}
	if st.Size() > thumbMaxSource {
		http.Error(w, "图片过大，不生成缩略图", http.StatusRequestEntityTooLarge)
		return
	}

	dir, dirErr := thumbCacheDir()
	if dirErr == nil {
		cacheFile := filepath.Join(dir, thumbKey(ref, st, dim)+".jpg")
		if cached, readErr := os.ReadFile(cacheFile); readErr == nil {
			writeThumbResponse(w, r, cached, st.ModTime())
			return
		}
		data, encErr := buildThumb(ref, dim)
		if encErr != nil {
			http.Error(w, "解码或缩放失败: "+encErr.Error(), http.StatusUnsupportedMediaType)
			return
		}
		// 落盘失败（只读目录、磁盘满）也要把图给出去，只是下次还得现算
		if writeErr := writeThumbCache(cacheFile, data); writeErr == nil {
			_ = pruneThumbCache(dir)
		}
		writeThumbResponse(w, r, data, st.ModTime())
		return
	}

	data, encErr := buildThumb(ref, dim)
	if encErr != nil {
		http.Error(w, "解码或缩放失败: "+encErr.Error(), http.StatusUnsupportedMediaType)
		return
	}
	writeThumbResponse(w, r, data, st.ModTime())
}

// thumbDim 解析并夹取目标长边：非法/缺省回默认值，过大压到上限，防被拿来出巨图。
func thumbDim(raw string) int {
	v, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || v <= 0 {
		return thumbDefaultDim
	}
	if v > thumbMaxDim {
		return thumbMaxDim
	}
	return v
}

// buildThumb 整张读入 → 解码 → 等比缩放 → 编 JPEG。读上限交给 docref 统一把关。
func buildThumb(ref docspace.Ref, dim int) ([]byte, error) {
	raw, err := ref.ReadAll(thumbMaxSource)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, scaleToRGBA(img, dim), &jpeg.Options{Quality: thumbJPEGQuality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// scaleToRGBA 等比缩放到长边不超过 dim，并铺平为不透明 RGBA。
//
// 用盒式平均而不是按步长抽样：抽样在缩小比例不整除时会漏行漏列，截图里的细字
// 和网格会出摩尔纹。带 alpha 的图按白底合成（JPEG 没有透明通道，不合成会发黑）。
func scaleToRGBA(src image.Image, dim int) *image.RGBA {
	b := src.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 {
		return image.NewRGBA(image.Rect(0, 0, 1, 1))
	}
	nw, nh := sw, sh
	if scale := float64(dim) / float64(max(sw, sh)); scale < 1 {
		nw = maxInt(1, int(float64(sw)*scale+0.5))
		nh = maxInt(1, int(float64(sh)*scale+0.5))
	}
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		sy0, sy1 := b.Min.Y+y*sh/nh, b.Min.Y+(y+1)*sh/nh
		if sy1 <= sy0 {
			sy1 = sy0 + 1
		}
		if sy1 > b.Max.Y {
			sy1 = b.Max.Y
		}
		for x := 0; x < nw; x++ {
			sx0, sx1 := b.Min.X+x*sw/nw, b.Min.X+(x+1)*sw/nw
			if sx1 <= sx0 {
				sx1 = sx0 + 1
			}
			if sx1 > b.Max.X {
				sx1 = b.Max.X
			}
			var sr, sg, sb, sa, n uint64
			for yy := sy0; yy < sy1; yy++ {
				for xx := sx0; xx < sx1; xx++ {
					r, g, bl, a := src.At(xx, yy).RGBA()
					sr += uint64(r)
					sg += uint64(g)
					sb += uint64(bl)
					sa += uint64(a)
					n++
				}
			}
			if n == 0 {
				n = 1
			}
			// 16 位均值降到 8 位，再按平均 alpha 与白底合成
			r8, g8, b8 := uint8((sr/n)>>8), uint8((sg/n)>>8), uint8((sb/n)>>8)
			a8 := uint8((sa / n) >> 8)
			if a8 < 255 {
				inv := uint64(255 - a8)
				r8 = uint8((uint64(r8)*uint64(a8) + 255*inv) / 255)
				g8 = uint8((uint64(g8)*uint64(a8) + 255*inv) / 255)
				b8 = uint8((uint64(b8)*uint64(a8) + 255*inv) / 255)
				a8 = 255
			}
			dst.SetRGBA(x, y, color.RGBA{R: r8, G: g8, B: b8, A: a8})
		}
	}
	return dst
}

// thumbKey 缓存键。路径可能含分隔符与中文，统一压进 sha1 保证文件名安全；
// ref.String() 对 webdav 会抹掉凭证，正好也避免键里带上 user:pass。
func thumbKey(ref docspace.Ref, st os.FileInfo, dim int) string {
	sum := sha1.Sum([]byte(fmt.Sprintf("%s|%d|%d|%d", ref.String(), st.Size(), st.ModTime().UnixNano(), dim)))
	return hex.EncodeToString(sum[:])
}

// thumbCacheDir 缓存目录（文档库内 thumb/）。拿不到库目录时返回错误，调用方
// 退化成"每次现算、不落盘"，不影响出图。
func thumbCacheDir() (string, error) {
	s, err := docspace.Global()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(s.Root(), "thumb")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// writeThumbCache 先写唯一临时名再 rename 落定：并发请求同一张图时不会互相截断，
// 读者永远只会看到完整文件。
func writeThumbCache(path string, data []byte) error {
	tmp := path + "." + strconv.FormatInt(time.Now().UnixNano(), 36) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// pruneThumbCache 数量超上限时按修改时间从旧到新删到阈值内。清理失败不影响响应。
func pruneThumbCache(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	type item struct {
		path string
		mod  time.Time
	}
	list := make([]item, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		list = append(list, item{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	if len(list) <= thumbCacheKeepFiles {
		return nil
	}
	sort.Slice(list, func(i, j int) bool { return list[i].mod.Before(list[j].mod) })
	for _, it := range list[:len(list)-thumbCacheKeepFiles] {
		_ = os.Remove(it.path)
	}
	return nil
}

// writeThumbResponse 吐字节：Last-Modified 用实体时间，浏览器与 WebView 的缓存
// 因此能跟着源文件改动自动失效；HEAD 只回头不写体。
func writeThumbResponse(w http.ResponseWriter, r *http.Request, data []byte, mod time.Time) {
	h := w.Header()
	h.Set("Content-Type", "image/jpeg")
	h.Set("Cache-Control", "public, max-age=86400")
	h.Set("Last-Modified", mod.UTC().Format(http.TimeFormat))
	h.Set("Content-Length", strconv.Itoa(len(data)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

// maxInt 显式整型取大，避开与内置 max 在混合类型处的歧义。
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
