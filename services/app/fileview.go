package app

import (
	"Tools/tools/fileview"
)

/*
文件查看器绑定层：本地文件内容的读与写。

真正的读写、类型判定都在 tools/fileview，这里只按 disk.go 的惯例薄薄包一层，
供前端调用。全部方法跨平台（不加 build tag）：Linux 面板里浏览的是服务器本机
文件，语义与磁盘管理一致。大文件的大头（音视频）不经过这里，走 MediaHandler
的 Range 端点。
*/

// FileReadText 读取文本内容（上限 2MB，超限截断；二进制会标记并只回小段）。
func (a *App) FileReadText(path string) (fileview.FileText, error) {
	return fileview.ReadText(path)
}

// FileWriteText 覆盖保存文本（仅已存在文件，原子写回）。
func (a *App) FileWriteText(path, content string) error {
	return fileview.WriteText(path, content)
}

// FileReadBinary 把小文件读成 data URL（图片/PDF 内联预览），超限回 tooLarge。
func (a *App) FileReadBinary(path, mime string) (fileview.FileBinary, error) {
	return fileview.ReadBinaryDataURL(path, mime)
}

// FileWriteBinary 把浏览器处理产物（图片/PDF）原子写回磁盘，可新建可覆盖。
func (a *App) FileWriteBinary(path, dataBase64 string) error {
	return fileview.WriteBinary(path, dataBase64)
}

// FileOpenSystem 用系统默认程序打开文件；无图形环境回 sysx.ErrUnsupported。
func (a *App) FileOpenSystem(path string) error {
	return fileview.OpenSystem(path)
}

// FileClassify 判定文件类型（kind, mime）：前端也有一份表，以后端为准。
func (a *App) FileClassify(path string) (string, string) {
	return fileview.Classify(path)
}
