//go:build !windows

package disk

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// 非 Windows 平台只保证能编译：盘符概念退化成根目录，回收站退化成永久删除。
// 本项目实际只会打包 Windows，这些实现仅用于 go vet / CI 在别的系统上跑通。

// Drives 返回根目录一项（拿不到精确容量时给 0，前端会显示未知）。
func Drives() []Drive {
	var st syscall.Statfs_t
	d := Drive{Root: "/", Kind: "fixed"}
	if err := syscall.Statfs("/", &st); err == nil {
		d.Total = st.Blocks * uint64(st.Bsize)
		d.Free = st.Bavail * uint64(st.Bsize)
		if d.Total >= d.Free {
			d.Used = d.Total - d.Free
		}
		if d.Total > 0 {
			d.UsedPct = float64(d.Used) / float64(d.Total) * 100
		}
	}
	return []Drive{d}
}

// DriveOf 返回路径所在盘符（非 Windows 上恒为根目录）。
func DriveOf(path string) (Drive, bool) {
	ds := Drives()
	if len(ds) == 0 {
		return Drive{}, false
	}
	return ds[0], true
}

// RecycleBin 非 Windows 上没有回收站语义，直接永久删除。
func RecycleBin(paths []string) (int, error) {
	return PermanentDelete(paths)
}

// RevealInExplorer 打开所在目录（桌面环境由调用方自己起文件管理器）。
func RevealInExplorer(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	return fmt.Errorf("当前平台不支持打开资源管理器: %s", abs)
}

// OpenShell 非 Windows 上不可用。
func OpenShell(path string) error {
	return fmt.Errorf("当前平台不支持打开命令行")
}

// OpenTrash 非 Windows 上不可用。
func OpenTrash() error {
	return fmt.Errorf("当前平台没有系统回收站")
}

// OpenFile 非 Windows 上不可用。
func OpenFile(path string) error {
	return fmt.Errorf("当前平台不支持用默认程序打开文件")
}

// Home 用户主目录。
func Home() string {
	if h, err := os.UserHomeDir(); err == nil && h != "" {
		return h
	}
	return "/"
}
