//go:build !windows

package winrun

// ============================================================
// 提权执行 helper - 非 Windows 平台桩
//
// 保持与 windows 实现同签名，使 tools/docker、tools/npm 等调用方
// 在任意 GOOS 下可编译（这些功能本身只在 Windows 有意义）。
// ============================================================

import (
	"fmt"
	"time"
)

var errUnsupported = fmt.Errorf("非 Windows 平台不支持提权执行")

// Elevated 与 windows 实现同构
type Elevated struct {
	File      string
	Params    string
	Directory string
	Show      int32
	Timeout   time.Duration
}

// UACCancelledError 非 Windows 下不会出现，仅为签名对齐
type UACCancelledError struct{}

func (UACCancelledError) Error() string { return errUnsupported.Error() }

// IsUACCancelled 判断错误是否源自用户拒绝提权
func IsUACCancelled(err error) bool {
	_, ok := err.(*UACCancelledError)
	return ok
}

func RunElevatedWait(Elevated) (int, error) { return -1, errUnsupported }

func StartDetached(string, ...string) error { return errUnsupported }

func RunHidden(time.Duration, string, ...string) (string, error) { return "", errUnsupported }

func RunHiddenEnv(time.Duration, []string, string, ...string) (string, error) {
	return "", errUnsupported
}
