// Package fileview 本地文件内容读写与类型判定：文件查看器工具页的后端原语。
//
// 磁盘管理（tools/disk）能整卷浏览但没有"读内容/写内容"，本包补齐这块；
// 音视频等大文件不经过绑定层，走 media.go 的 Range 流式端点。
// 绑定层在 services/app/fileview.go，页面在 frontend/src/views/tools/viewer。
//
// 全部逻辑跨平台零 cgo：Linux 面板里浏览的是服务器本机文件（已在登录墙后），
// 与磁盘工具"无 root 约束、可整卷浏览"的口径一致。
package fileview

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"Tools/services/sysx"
)

// 内容上限：读走 Wails 事件/返回值，灌太满会把绑定层噎住；
// 写回来的多是浏览器处理产物（图片/PDF），512MB 封顶防误操作。
const (
	MaxTextInline  = 2 << 20   // 文本内联上限：2MB
	MaxBinInline   = 25 << 20  // data URL 内联上限：25MB
	MaxWriteBinary = 512 << 20 // FileWriteBinary 解码后上限：512MB
)

// FileText 一次文本读取的结果。
type FileText struct {
	Content   string `json:"content"`
	Size      int64  `json:"size"`      // 文件实际字节数
	Truncated bool   `json:"truncated"` // 是否因超过 MaxTextInline 被截断
	Binary    bool   `json:"binary"`    // 含 NUL 或非合法 UTF-8，前端应转只读/HEX 视图
}

// FileBinary 一次二进制读取的结果。Kind：inline | tooLarge。
type FileBinary struct {
	Kind    string `json:"kind"`
	DataURL string `json:"dataURL"` // Kind=inline 时为 data:<mime>;base64,...
	Size    int64  `json:"size"`    // 文件实际字节数
}

// 类型判定的扩展名表：三级"扩展名 → MIME → 兜底 binary"，前端与后端同口径。
var kindByExt = map[string]string{
	"png": "image", "jpg": "image", "jpeg": "image", "gif": "image",
	"webp": "image", "bmp": "image", "svg": "image", "ico": "image",
	"pdf": "pdf",
	"xls": "sheet", "xlsx": "sheet", "csv": "sheet",
	"mp3": "audio", "wav": "audio", "m4a": "audio", "ogg": "audio",
	"mp4": "video", "webm": "video", "mov": "video",
	"js": "code", "ts": "code", "jsx": "code", "tsx": "code", "vue": "code",
	"json": "code", "html": "code", "htm": "code", "css": "code",
	"go": "code", "py": "code", "php": "code", "sh": "code",
	"yaml": "code", "yml": "code", "toml": "code",
	"txt": "text", "md": "text", "log": "text", "ini": "text",
	"conf": "text", "env": "text",
}

// imageMime 图片扩展名的显式 MIME：系统注册表不可靠（Windows 上 .ico 等
// 可能被别的软件改写过），且 Linux 容器经常没有 /etc/mime.types。
var imageMime = map[string]string{
	"png":  "image/png",
	"jpg":  "image/jpeg",
	"jpeg": "image/jpeg",
	"gif":  "image/gif",
	"webp": "image/webp",
	"bmp":  "image/bmp",
	"svg":  "image/svg+xml",
	"ico":  "image/x-icon",
}

// Classify 按"扩展名 → 系统 MIME → 兜底 binary"三级判定文件类型。
// kind ∈ image|pdf|sheet|text|code|audio|video|binary；mime 取不到时 octet-stream。
func Classify(path string) (kind, mimeStr string) {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	if k, ok := kindByExt[ext]; ok {
		if m, ok := imageMime[ext]; ok {
			return k, m
		}
		if m := mime.TypeByExtension("." + ext); m != "" {
			return k, m
		}
		return k, "application/octet-stream"
	}
	if m := mime.TypeByExtension("." + ext); m != "" {
		return "binary", m
	}
	return "binary", "application/octet-stream"
}

// ReadText 读取文本内容：上限 MaxTextInline，超限截断并标记；
// 含 NUL 或非合法 UTF-8 判为二进制，只回小段供前端提示。
func ReadText(path string) (FileText, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return FileText{}, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return FileText{}, err
	}
	if st.IsDir() {
		return FileText{}, fmt.Errorf("目标是目录，不能按文本读取: %s", abs)
	}
	f, err := os.Open(abs)
	if err != nil {
		return FileText{}, fmt.Errorf("打开文件失败: %w", err)
	}
	defer f.Close()
	// 多读 1 字节即可判断"是否超限"，不必整读大文件
	buf, err := readAtMost(f, MaxTextInline+1)
	if err != nil {
		return FileText{}, fmt.Errorf("读取文件失败: %w", err)
	}
	truncated := len(buf) > MaxTextInline
	if truncated {
		// 按字节截断可能切在多字节字符中间：退回到上一个完整 rune，
		// 既避免把残段误判成非法 UTF-8（进而误报二进制），前端拿到的也是干净文本
		buf = trimPartialRune(buf[:MaxTextInline])
	}
	out := FileText{Size: st.Size(), Truncated: truncated}
	if bytes.IndexByte(buf, 0) >= 0 || !utf8.Valid(buf) {
		out.Binary = true
		// 只回开头小段（同样裁到完整 rune），前端拿去渲染 HEX 或提示
		out.Content = string(trimPartialRune(buf[:min(len(buf), 4096)]))
		return out, nil
	}
	out.Content = string(buf)
	return out, nil
}

// WriteText 覆盖保存文本：仅允许写回已存在的文件（新建走磁盘工具的 DiskNewFile），
// 先落 .part 再改名原子替换，半途失败不会把原文件毁掉。
func WriteText(path, content string) error {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return err
	}
	if len(content) > MaxTextInline {
		return fmt.Errorf("内容 %d 字节，超过保存上限 %d 字节", len(content), MaxTextInline)
	}
	if st, err := os.Stat(abs); err != nil {
		return err
	} else if st.IsDir() {
		return fmt.Errorf("目标是目录，不能按文本保存: %s", abs)
	}
	part := abs + ".part"
	// O_CREATE 防"Stat 之后文件被别人删掉"的竞态：那种情况宁可报错也不无中生有
	if err := writePart(part, []byte(content), 0o644); err != nil {
		return err
	}
	if err := atomicRename(part, abs); err != nil {
		_ = os.Remove(part)
		return fmt.Errorf("原子替换失败: %w", err)
	}
	return nil
}

// ReadBinaryDataURL 把小文件整个读成 data URL（图片/PDF 内联预览）；
// 超过 MaxBinInline 只回 tooLarge，前端改走 Range 端点或下载。
func ReadBinaryDataURL(path, mimeStr string) (FileBinary, error) {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return FileBinary{}, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return FileBinary{}, err
	}
	if st.IsDir() {
		return FileBinary{}, fmt.Errorf("目标是目录: %s", abs)
	}
	if st.Size() > MaxBinInline {
		return FileBinary{Kind: "tooLarge", Size: st.Size()}, nil
	}
	if strings.TrimSpace(mimeStr) == "" {
		_, mimeStr = Classify(abs)
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return FileBinary{}, fmt.Errorf("读取文件失败: %w", err)
	}
	return FileBinary{
		Kind:    "inline",
		DataURL: "data:" + mimeStr + ";base64," + base64.StdEncoding.EncodeToString(data),
		Size:    st.Size(),
	}, nil
}

// WriteBinary 把浏览器处理产物（图片/PDF）原子写回磁盘：可新建也可覆盖，
// base64 编码后传输（与所有绑定同口径），上限 MaxWriteBinary。
func WriteBinary(path, dataBase64 string) error {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return err
	}
	if st, err := os.Stat(abs); err == nil && st.IsDir() {
		return fmt.Errorf("目标是目录，不能写入: %s", abs)
	}
	// 编码后体积先粗筛（base64 恒为 4/3 膨胀），精确判定在解码后
	if len(dataBase64) > MaxWriteBinary+MaxWriteBinary/3+4 {
		return fmt.Errorf("内容过大（base64 %d 字节），上限 %d 字节", len(dataBase64), MaxWriteBinary)
	}
	data, err := base64.StdEncoding.DecodeString(dataBase64)
	if err != nil {
		return fmt.Errorf("base64 解码失败: %w", err)
	}
	if len(data) > MaxWriteBinary {
		return fmt.Errorf("内容 %d 字节，超过写入上限 %d 字节", len(data), MaxWriteBinary)
	}
	part := abs + ".part"
	if err := writePart(part, data, 0o644); err != nil {
		return err
	}
	if err := atomicRename(part, abs); err != nil {
		_ = os.Remove(part)
		return fmt.Errorf("原子替换失败: %w", err)
	}
	return nil
}

// OpenSystem 交给系统按扩展名关联的默认程序打开（Windows 走 ShellExecuteW，
// Linux 走 xdg-open）。headless 环境返回 sysx.ErrUnsupported，不吞成假成功。
func OpenSystem(path string) error {
	abs, err := filepath.Abs(strings.TrimSpace(path))
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err != nil {
		return err
	}
	return sysx.Open().Launch(abs)
}

// writePart 把内容写进 .part 临时文件：Create 截断旧残留、同步落盘。
func writePart(part string, data []byte, perm os.FileMode) error {
	f, err := os.OpenFile(part, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("创建临时文件失败: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		_ = os.Remove(part)
		return fmt.Errorf("写入临时文件失败: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(part)
		return fmt.Errorf("关闭临时文件失败: %w", err)
	}
	return nil
}

// atomicRename 把 .part 换成目标文件。Windows 上改名不会覆盖已存在的目标
// （POSIX rename 语义在两边不一致），先删再换；删除与改名之间文件短暂缺席，
// 只影响"边写边看"的极端场景，比毁掉原文件划算。
func atomicRename(part, target string) error {
	if err := os.Rename(part, target); err == nil {
		return nil
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(part, target)
}

// readAtMost 最多读 max 字节（LimitReader 到量即止，普通文件更短也不算错）。
func readAtMost(f *os.File, max int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(f, max))
}

// trimPartialRune 去掉末尾不完整的 UTF-8 序列（按字节截断的收尾保护）。
// 从尾部回扫定位最后一个 rune 的首字节，若该字节声明的长度大于实际剩余，
// 说明这是个被截断的残段，整段丢掉。
func trimPartialRune(buf []byte) []byte {
	if len(buf) == 0 {
		return buf
	}
	i := len(buf) - 1
	for i > 0 && buf[i]&0xC0 == 0x80 { // 0x80xxxx 是续接字节，往前找到首字节
		i--
	}
	if len(buf)-i < utf8ByteLen(buf[i]) { // 首字节要求的字节数 > 实际剩余 → 残缺
		return buf[:i]
	}
	return buf
}

func utf8ByteLen(first byte) int {
	switch {
	case first&0x80 == 0:
		return 1
	case first&0xE0 == 0xC0:
		return 2
	case first&0xF0 == 0xE0:
		return 3
	default:
		return 4
	}
}
