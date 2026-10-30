//go:build !windows

package launcher

import (
	"path/filepath"

	"Tools/services/sysx"
)

// revealPath 非 Windows 没有"选中某个文件"这层 shell 语义，退化成打开所在目录。
// 没有图形环境时 sysx.Open 自己会返回 ErrUnsupported，不在这里另造一套错误。
func revealPath(p string) error {
	return sysx.Open().Dir(filepath.Dir(p))
}
