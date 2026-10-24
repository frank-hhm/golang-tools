//go:build !windows

package main

import (
	"fmt"
	"os"
)

// go-tools 的完整功能依赖 Windows 平台能力（Win32 浮动球、全局热键、托盘、截图选屏、
// PHP/MySQL/Redis 本地服务管理等），实现见 main.go / app.go（go:build windows）。
// 本文件仅为非 Windows 平台提供可编译的占位入口，避免交叉编译/IDE 分析时报“缺少 main 函数”。
func main() {
	fmt.Fprintln(os.Stderr, "go-tools 仅支持 Windows 平台")
	os.Exit(1)
}
