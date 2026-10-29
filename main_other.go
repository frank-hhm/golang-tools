//go:build !windows && !server

package main

import (
	"fmt"
	"os"
)

// 非 Windows 又没有 -tags server 时的占位入口。
//
// 两条真正可用的构建是：
//   - Windows 桌面版：go build .（main.go）
//   - Linux/容器无头版：go build -tags server .（main_server.go）
//
// 本文件只保证在其它组合下 go build ./... 不会报“缺少 main 函数”，
// 真跑起来会直接退出并提示该用哪个 tag。
func main() {
	fmt.Fprintln(os.Stderr, "Tools 在非 Windows 平台需要 -tags server 构建（go build -tags server .）")
	os.Exit(1)
}
