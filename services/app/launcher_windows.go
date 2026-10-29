//go:build windows

package app

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// lockWorkstation 锁定当前用户会话（命令面板里的"锁屏"）。
//
// x/sys 没导出 LockWorkStation，挂 LazyProc；先 Find() 探一遍——导出函数
// 找不到时是 panic 而不是 error。
//
// 锁屏需要桌面会话，Web 面板（-tags server）根本没有这条命令的入口：
// 上层 launcherMounted 已经把整套服务挡在外面，这里只按返回值如实报错。
func lockWorkstation() error {
	user32 := windows.NewLazySystemDLL("user32.dll")
	proc := user32.NewProc("LockWorkStation")
	if err := proc.Find(); err != nil {
		return err
	}
	// 返回值是 BOOL；0 表示失败（组策略禁锁屏等）。err 由 syscall 附带，
	// 但 Win32 对“成功”也会留一个脏的 LastError，所以判定只看 r，err 只进文案。
	if r, _, err := proc.Call(); r == 0 {
		return fmt.Errorf("锁定工作站失败: %v", err)
	}
	return nil
}
