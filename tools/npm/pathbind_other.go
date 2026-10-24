//go:build !windows

package npm

import (
	"fmt"
	"runtime"
)

// PathBindStatus 非 Windows 平台不提供 PATH 绑定（本工具主要面向 Windows 的 npm 生效链）
func PathBindStatus() (*NpmPathBind, error) {
	return &NpmPathBind{Platform: runtime.GOOS, Note: "PATH 强制绑定目前仅支持 Windows。"}, nil
}

// BindNodeToPath 非 Windows 平台不支持
func BindNodeToPath(entryDir string, system bool) error {
	return fmt.Errorf("强制绑定本地 npm（改写 PATH）目前仅支持 Windows")
}

// UndoPathBind 非 Windows 平台不支持
func UndoPathBind() (string, error) {
	return "", fmt.Errorf("强制绑定本地 npm（改写 PATH）目前仅支持 Windows")
}
