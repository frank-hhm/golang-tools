package app

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"Tools/appdirs"
	"Tools/services/applog"
	"Tools/services/sysx"
)

/*
主题背景图导入 / 读取（跨平台）

背景图作为一等公民主题档（ThemeConfig.Background.Type=="image"）落盘到 data/theme/background.<ext>，
配置里只存绝对路径；前端渲染时经 ReadFileDataURI 转 data: URI 当 CSS 背景，避免为三构建各写一套静态路由。

两条导入路径按能力分流（前端据 caps.nativeWindows 选择，不靠"调用失败再回退"）：
  - 桌面（Windows 原生窗）：ImportThemeBackground 弹原生文件对话框，体验最好；
  - 无头面板 / Windows Web：无原生对话框，前端用 <input type=file> 读成 dataURL，
    再走 SaveThemeBackgroundDataURI 落盘——同样持久化、跨窗口共享、重启仍在。
*/

// themeBgMaxBytes 背景图体积上限：8 MB。转 dataURI 后约 ×1.34，仍远小于 WebView 单资源承受范围。
const themeBgMaxBytes = 8 << 20

// themeBgExtMIME 允许的背景图扩展名 → data URI MIME。不在表内的一律拒绝。
var themeBgExtMIME = map[string]string{
	".png":  "image/png",
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".webp": "image/webp",
	".gif":  "image/gif",
	".bmp":  "image/bmp",
}

// themeBgMIMEExt MIME → 落盘扩展名（dataURL 导入路径用）。jpeg 归一到 .jpg。
var themeBgMIMEExt = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
	"image/bmp":  ".bmp",
}

// ImportThemeBackground 弹原生图片选择框，把选中图导入 data/theme/background.<ext>，返回落点绝对路径。
// 取消返回空串；非桌面构建（无原生窗口）返回 sysx.ErrUnsupported，前端应改走文件输入 + SaveThemeBackgroundDataURI。
func (a *App) ImportThemeBackground() (string, error) {
	if !sysx.Caps().NativeWindows {
		return "", fmt.Errorf("%w：当前运行模式无原生文件对话框，请在页面里直接选择背景图", sysx.ErrUnsupported)
	}
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	path, err := a.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AddFilter("图片 (*.png;*.jpg;*.jpeg;*.webp;*.gif;*.bmp)", "*.png;*.jpg;*.jpeg;*.webp;*.gif;*.bmp").
		SetTitle("选择背景图片").
		PromptForSingleSelection()
	if err != nil {
		if isDialogCanceled(err) {
			return "", nil
		}
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	return importThemeBgFromPath(path)
}

// SaveThemeBackgroundDataURI 接收前端 <input type=file> 读出的 dataURL（"data:<mime>;base64,...."），
// 校验后落盘到 data/theme/background.<ext>，返回绝对路径。跨平台（无头面板浏览器上传同样走这里），不依赖原生窗口。
func (a *App) SaveThemeBackgroundDataURI(dataURI string) (string, error) {
	uri := strings.TrimSpace(dataURI)
	if uri == "" {
		return "", fmt.Errorf("背景图数据为空")
	}
	// 拆 "data:<mime>;base64,<payload>"
	if !strings.HasPrefix(uri, "data:") {
		return "", fmt.Errorf("不是合法的 dataURL")
	}
	rest := uri[len("data:"):]
	semi := strings.IndexByte(rest, ';')
	if semi < 0 {
		return "", fmt.Errorf("dataURL 缺少 MIME/base64 标记")
	}
	mime := rest[:semi]
	payload := rest[semi+1:]
	if !strings.HasPrefix(strings.ToLower(payload), "base64,") {
		return "", fmt.Errorf("dataURL 仅支持 base64 编码")
	}
	b64 := payload[len("base64,"):]
	ext, ok := themeBgMIMEExt[strings.ToLower(strings.TrimSpace(mime))]
	if !ok {
		return "", fmt.Errorf("不支持的图片类型：%s", mime)
	}
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("背景图解码失败: %v", err)
	}
	if len(data) == 0 {
		return "", fmt.Errorf("背景图内容为空")
	}
	if len(data) > themeBgMaxBytes {
		return "", fmt.Errorf("背景图过大（%d MB，上限 %d MB）", len(data)>>20, themeBgMaxBytes>>20)
	}
	dst, err := writeThemeBg(ext, data)
	if err != nil {
		return "", err
	}
	applog.Infof("theme", "背景图已上传落盘: %s（%d 字节）", dst, len(data))
	return dst, nil
}

// ReadFileDataURI 读取图片文件转成 data: URI，供前端当 CSS 背景用。限 8 MB、扩展名须在白名单，否则报错。
// 跨平台（Linux 面板浏览器同样可用），不依赖原生窗口。
func (a *App) ReadFileDataURI(path string) (string, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return "", fmt.Errorf("路径为空")
	}
	ext := strings.ToLower(filepath.Ext(p))
	mime, ok := themeBgExtMIME[ext]
	if !ok {
		return "", fmt.Errorf("不支持的图片格式：%s", ext)
	}
	st, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	if st.Size() > themeBgMaxBytes {
		return "", fmt.Errorf("背景图过大（%d MB，上限 %d MB）", st.Size()>>20, themeBgMaxBytes>>20)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data), nil
}

// importThemeBgFromPath 校验扩展名与大小后，把 src 复制到 data/theme/background.<ext>（同名覆盖），返回落点绝对路径。
func importThemeBgFromPath(src string) (string, error) {
	ext := strings.ToLower(filepath.Ext(src))
	if _, ok := themeBgExtMIME[ext]; !ok {
		return "", fmt.Errorf("不支持的图片格式：%s（仅 png/jpg/jpeg/webp/gif/bmp）", ext)
	}
	st, err := os.Stat(src)
	if err != nil {
		return "", err
	}
	if st.Size() > themeBgMaxBytes {
		return "", fmt.Errorf("背景图过大（%d MB，上限 %d MB）", st.Size()>>20, themeBgMaxBytes>>20)
	}
	in, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer in.Close()
	dir, err := appdirs.Data("theme")
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "background"+ext)
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	applog.Infof("theme", "背景图已导入: %s（%d 字节）", dst, st.Size())
	return dst, nil
}

// writeThemeBg 把解码后的图片字节写到 data/theme/background<ext>（0644，同名覆盖），返回绝对路径。
func writeThemeBg(ext string, data []byte) (string, error) {
	dir, err := appdirs.Data("theme")
	if err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "background"+ext)
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}
