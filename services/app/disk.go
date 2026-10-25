//go:build windows

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"go-tools/services/applog"
	"go-tools/tools/disk"
)

/*
磁盘管理（资源管理器）绑定层

页面只负责画：盘符总览、路径栏、文件列表/网格、工具栏。
真正的读写、删除、复制都在 tools/disk，这里只做三件事：
  1. 把参数校验一遍（空路径、目标目录不存在）；
  2. 把"用户选择了什么"这类桌面交互（打开/保存对话框）接过来；
  3. 危险操作留一条日志（删了哪些、移动到哪），出问题能倒查。
*/

// DiskDrives 枚举本机盘符（含容量与使用率），页面顶部总览用。
func (a *App) DiskDrives() []disk.Drive {
	return disk.Drives()
}

// DiskHome 默认浏览起点（用户主目录）。
func (a *App) DiskHome() string {
	return disk.Home()
}

// DiskList 浏览目录；path 为空时回到用户主目录。
func (a *App) DiskList(path string) (disk.Listing, error) {
	return disk.List(path)
}

// DiskMkdir 新建目录，返回新目录路径。
func (a *App) DiskMkdir(parent, name string) (string, error) {
	full, err := disk.Mkdir(parent, name)
	if err != nil {
		return "", err
	}
	applog.Infof("disk", "新建目录: %s", full)
	return full, nil
}

// DiskNewFile 新建空文件，返回文件路径。
func (a *App) DiskNewFile(parent, name string) (string, error) {
	full, err := disk.NewFile(parent, name)
	if err != nil {
		return "", err
	}
	applog.Infof("disk", "新建文件: %s", full)
	return full, nil
}

// DiskRename 同级改名，返回新路径。
func (a *App) DiskRename(path, newName string) (string, error) {
	full, err := disk.Rename(path, newName)
	if err != nil {
		return "", err
	}
	applog.Infof("disk", "重命名: %s → %s", path, full)
	return full, nil
}

// DiskDelete 删除一批路径；permanent=false 走系统回收站，true 直接删。
func (a *App) DiskDelete(paths []string, permanent bool) (int, error) {
	if len(paths) == 0 {
		return 0, fmt.Errorf("没有选中任何文件")
	}
	var (
		n   int
		err error
	)
	if permanent {
		n, err = disk.PermanentDelete(paths)
	} else {
		n, err = disk.RecycleBin(paths)
	}
	if err != nil {
		applog.Errorf("disk", "删除失败（permanent=%v）: %v", permanent, err)
		return n, err
	}
	applog.Infof("disk", "已删除 %d 项（permanent=%v）: %s", n, permanent, strings.Join(paths, " | "))
	return n, nil
}

// DiskCopy 把一批路径复制到目标目录。
func (a *App) DiskCopy(paths []string, dstDir string) (int, error) {
	n, err := disk.Copy(paths, dstDir)
	if err != nil {
		applog.Errorf("disk", "复制失败: %v", err)
		return n, err
	}
	applog.Infof("disk", "已复制 %d 项到 %s", n, dstDir)
	return n, nil
}

// DiskMove 把一批路径移动到目标目录。
func (a *App) DiskMove(paths []string, dstDir string) (int, error) {
	n, err := disk.Move(paths, dstDir)
	if err != nil {
		applog.Errorf("disk", "移动失败: %v", err)
		return n, err
	}
	applog.Infof("disk", "已移动 %d 项到 %s", n, dstDir)
	return n, nil
}

// DiskDirSize 递归计算目录/文件大小（列表里"计算"操作与属性面板用）。
func (a *App) DiskDirSize(path string) (int64, error) {
	return disk.DirSize(path)
}

// DiskSearch 按名称搜索；includeSub=false 只搜当前层。
func (a *App) DiskSearch(root, keyword string, includeSub bool) ([]disk.Entry, error) {
	return disk.Search(root, keyword, includeSub, 500)
}

// DiskSearchContent 在文本文件里搜索内容，返回命中行（有内置上限，不会扫爆磁盘）。
func (a *App) DiskSearchContent(root, keyword string, includeSub bool) ([]disk.Match, error) {
	return disk.SearchContent(root, keyword, includeSub)
}

// DiskUpload 选本机文件复制到当前目录（桌面版的"上传"），返回复制进来的路径。
func (a *App) DiskUpload(dstDir string) ([]string, error) {
	if a.app == nil {
		return nil, fmt.Errorf("应用尚未初始化")
	}
	picked, err := a.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		SetTitle("选择要复制到当前目录的文件").
		PromptForMultipleSelection()
	if err != nil {
		return nil, err
	}
	if len(picked) == 0 {
		return nil, nil
	}
	if _, err := disk.Copy(picked, dstDir); err != nil {
		applog.Errorf("disk", "复制到 %s 失败: %v", dstDir, err)
		return nil, err
	}
	applog.Infof("disk", "已复制 %d 个文件到 %s", len(picked), dstDir)
	return picked, nil
}

// DiskUploadFolder 选本机目录整棵复制到当前目录。
func (a *App) DiskUploadFolder(dstDir string) (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	picked, err := a.app.Dialog.OpenFile().
		CanChooseFiles(false).
		CanChooseDirectories(true).
		SetTitle("选择要复制到当前目录的文件夹").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(picked) == "" {
		return "", nil
	}
	if _, err := disk.Copy([]string{picked}, dstDir); err != nil {
		applog.Errorf("disk", "复制目录到 %s 失败: %v", dstDir, err)
		return "", err
	}
	applog.Infof("disk", "已复制目录 %s 到 %s", picked, dstDir)
	return picked, nil
}

// DiskDownload 把文件另存到本机别处（桌面版的"下载"）。
//
// 多选时返回实际保存的数量（逐个弹保存框太吵，这里只处理单个；
// 多选请在列表里用"复制"）。
func (a *App) DiskDownload(path string) (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	abs := strings.TrimSpace(path)
	if abs == "" {
		return "", fmt.Errorf("没有选中文件")
	}
	if st, err := os.Stat(abs); err != nil {
		return "", err
	} else if st.IsDir() {
		return "", fmt.Errorf("文件夹请用「复制」搬到目标目录")
	}
	target, err := a.app.Dialog.SaveFile().
		SetFilename(filepath.Base(abs)).
		SetMessage("另存为").
		AddFilter("所有文件 (*.*)", "*.*").
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(target) == "" {
		return "", nil
	}
	if _, err := disk.Copy([]string{abs}, filepath.Dir(target)); err != nil {
		return "", err
	}
	// Copy 会沿用原名，用户改过名字时再重命名一次
	saved := filepath.Join(filepath.Dir(target), filepath.Base(abs))
	if !strings.EqualFold(saved, target) {
		_ = os.Rename(saved, target)
	}
	applog.Infof("disk", "另存为: %s → %s", abs, target)
	return target, nil
}

// DiskFavorites 读取收藏的目录。
func (a *App) DiskFavorites() []string {
	return disk.Favorites()
}

// DiskSaveFavorites 覆盖保存收藏列表。
func (a *App) DiskSaveFavorites(list []string) error {
	return disk.SaveFavorites(list)
}

// DiskOpenInExplorer 在资源管理器里定位到该路径。
func (a *App) DiskOpenInExplorer(path string) error {
	if err := disk.RevealInExplorer(path); err != nil {
		applog.Errorf("disk", "打开资源管理器失败: %v", err)
		return err
	}
	return nil
}

// DiskOpenShell 在目录里打开命令行。
func (a *App) DiskOpenShell(path string) error {
	if err := disk.OpenShell(path); err != nil {
		applog.Errorf("disk", "打开命令行失败: %v", err)
		return err
	}
	return nil
}

// DiskOpenTrash 打开系统回收站（删除走回收站，得有个地方去看）。
func (a *App) DiskOpenTrash() error {
	return disk.OpenTrash()
}

// DiskOpenFile 用系统默认程序打开文件。
func (a *App) DiskOpenFile(path string) error {
	return disk.OpenFile(path)
}
