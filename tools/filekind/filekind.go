// Package filekind 按扩展名判定文件类型（kind）与 MIME，是「一个文件是什么」的唯一口径。
//
// 为什么要单独成包：判定逻辑原先长在 tools/fileview 里，但文档中心（tools/docspace）
// 也要给条目打类型标签做多类型管理，而 fileview 已经 import 了 docspace（走 docref
// 统一文件源），docspace 反过来 import fileview 就成环。把纯函数抽到这里，两边各自
// 依赖它，分层才干净。
//
// 全平台零 cgo、零外部依赖：Linux 面板与 Windows 桌面同一份代码。
package filekind

import (
	"mime"
	"path/filepath"
	"strings"
)

// 类型常量。前端筛选 chips 与图标配色都按这套字符串对齐。
const (
	KindImage   = "image"
	KindPDF     = "pdf"
	KindSheet   = "sheet"
	KindDoc     = "doc"
	KindText    = "text"
	KindCode    = "code"
	KindAudio   = "audio"
	KindVideo   = "video"
	KindArchive = "archive"
	KindBinary  = "binary" // 未知/其他，兜底
)

// Order 是类型在前端 chips 与统计卡里的展示顺序，也是「按类型分组」的固定序，
// 避免 Go map 遍历无序导致每次刷新 chips 顺序跳变。
var Order = []string{
	KindDoc, KindSheet, KindPDF, KindText, KindCode,
	KindImage, KindAudio, KindVideo, KindArchive, KindBinary,
}

// ext2kind 扩展名（不含点、小写）→ 类型。
var ext2kind = map[string]string{
	"png": KindImage, "jpg": KindImage, "jpeg": KindImage, "gif": KindImage,
	"webp": KindImage, "bmp": KindImage, "svg": KindImage, "ico": KindImage,
	"pdf": KindPDF,
	"xls": KindSheet, "xlsx": KindSheet, "csv": KindSheet,
	"docx": KindDoc,
	"mp3":  KindAudio, "wav": KindAudio, "m4a": KindAudio, "ogg": KindAudio,
	"mp4": KindVideo, "webm": KindVideo, "mov": KindVideo,
	"js": KindCode, "ts": KindCode, "jsx": KindCode, "tsx": KindCode, "vue": KindCode,
	"json": KindCode, "html": KindCode, "htm": KindCode, "css": KindCode,
	"go": KindCode, "py": KindCode, "php": KindCode, "sh": KindCode,
	"yaml": KindCode, "yml": KindCode, "toml": KindCode,
	"txt": KindText, "md": KindText, "log": KindText, "ini": KindText,
	"conf": KindText, "env": KindText,
}

// ext2mime 图片扩展名的显式 MIME：系统注册表不可靠（Windows 上 .ico 等可能被别的
// 软件改写过），且 Linux 容器经常没有 /etc/mime.types。
var ext2mime = map[string]string{
	"png":  "image/png",
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"gif":  "image/gif",
	"webp": "image/webp",
	"bmp":  "image/bmp",
	"svg":  "image/svg+xml",
	"ico":  "image/x-icon",
}

// StdlibDecodable 标出标准库 image 包能直接解码的位图扩展名。
// 缩略图生成只吃这几种：webp/bmp 需要 x/image 依赖，svg 是矢量（交给 <img> 直接显示），
// 与其引入新依赖换边角格式，不如让前端对这些退类型图标。
var StdlibDecodable = map[string]bool{"png": true, "jpg": true, "jpeg": true, "gif": true}

// Ext 取小写、不带点的扩展名。
// .tar.gz 要特判：filepath.Ext 只会拿到 .gz，类型与展示都对不上；
// .tgz 本身就是一个完整扩展名，不能也报成 tar.gz。
func Ext(path string) string {
	if strings.HasSuffix(strings.ToLower(filepath.Base(path)), ".tar.gz") {
		return "tar.gz"
	}
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
}

// Classify 按「扩展名表 → 系统 MIME → 兜底 binary」三级判定，返回类型、MIME、扩展名。
// kind ∈ image|pdf|sheet|doc|text|code|audio|video|archive|binary；mime 取不到时 octet-stream。
func Classify(path string) (kind, mimeStr, ext string) {
	ext = Ext(path)
	// 归档先判：只认引擎真能列举的 zip / tar.gz / tgz（rar/7z/裸 tar 归 binary 开本机程序）。
	switch ext {
	case "zip":
		return KindArchive, "application/zip", ext
	case "tar.gz", "tgz":
		return KindArchive, "application/gzip", ext
	}
	if k, ok := ext2kind[ext]; ok {
		if m, ok := ext2mime[ext]; ok {
			return k, m, ext
		}
		if m := mime.TypeByExtension("." + ext); m != "" {
			return k, m, ext
		}
		return k, "application/octet-stream", ext
	}
	if m := mime.TypeByExtension("." + ext); m != "" {
		return KindBinary, m, ext
	}
	return KindBinary, "application/octet-stream", ext
}

// Kind 只要类型，调用方不关心 MIME 时用这个。
func Kind(path string) string {
	k, _, _ := Classify(path)
	return k
}

// Thumbable 判定该路径能否生成位图缩略图：类型是 image 且扩展名在标准库解码器覆盖内。
// 先过这一道再去读文件，省下大量无谓的网络与磁盘 IO。
func Thumbable(path string) bool {
	k, _, ext := Classify(path)
	return k == KindImage && StdlibDecodable[ext]
}
