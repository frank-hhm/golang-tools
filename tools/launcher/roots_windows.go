//go:build windows

package launcher

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// ============================================================
// Windows 已知目录
//
// x/sys 只导出了 FOLDERID 常量和一个未导出的 shGetKnownFolderPath，所以这里
// 自己挂 shell32!SHGetKnownFolderPath。挂之前一律 Find() 探一遍：
// LazyProc 找不到导出函数是 panic 而不是 error（本仓库在 ExtractIconExW 上栽过）。
// ============================================================

var (
	modShell32      = windows.NewLazySystemDLL("shell32.dll")
	modOle32        = windows.NewLazySystemDLL("ole32.dll")
	procKnownFolder = modShell32.NewProc("SHGetKnownFolderPath")
	procMemFree     = modOle32.NewProc("CoTaskMemFree")
)

// knownFolder 取一个已知目录的绝对路径；拿不到返回空串（调用方自己跳过）。
func knownFolder(id *windows.KNOWNFOLDERID) string {
	if id == nil || procKnownFolder.Find() != nil {
		return ""
	}
	// buf 直接以 unsafe.Pointer 形态拿着：SHGetKnownFolderPath 写回的是一个
	// 由系统分配的 WCHAR*，先转成 uintptr 再转回来会被 go vet 判成 uintptr 滥用
	// （中间那段时间 GC 完全可以动那块内存），拿 Pointer 才是合规写法。
	var buf unsafe.Pointer
	r, _, _ := procKnownFolder.Call(uintptr(unsafe.Pointer(id)), 0, 0, uintptr(unsafe.Pointer(&buf)))
	if int32(r) != 0 || buf == nil {
		return ""
	}
	if procMemFree.Find() == nil {
		defer procMemFree.Call(uintptr(buf))
	}
	return windows.UTF16PtrToString((*uint16)(buf))
}

// defaultRoots 参与文件索引的已知目录。
// OneDrive（SkyDrive）单列一份：现在大量用户的"文档"其实只在云上那份里。
func defaultRoots() []string {
	return compactDirs(
		knownFolder(windows.FOLDERID_Documents),
		knownFolder(windows.FOLDERID_Downloads),
		knownFolder(windows.FOLDERID_Desktop),
		knownFolder(windows.FOLDERID_Pictures),
		knownFolder(windows.FOLDERID_SkyDrive),
	)
}

// startMenuRoots 应用清单的来源目录：开始菜单（当前用户 + 全体用户）与桌面快捷方式。
// 桌面也扫一遍是照实抄 Windows 自己的行为——开始菜单搜索本来就把桌面 .lnk 算作应用。
func startMenuRoots() []string {
	return compactDirs(
		knownFolder(windows.FOLDERID_Programs),
		knownFolder(windows.FOLDERID_CommonPrograms),
		knownFolder(windows.FOLDERID_Desktop),
		knownFolder(windows.FOLDERID_PublicDesktop),
	)
}

// recentRoot "最近使用"目录（里面全是 .lnk，指向用户最近打开过的文件）。
func recentRoot() string { return knownFolder(windows.FOLDERID_Recent) }
