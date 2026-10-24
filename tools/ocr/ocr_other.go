//go:build !windows

// 非 Windows 平台占位：识别依赖 WinRT（Windows.Media.Ocr），只有 Windows 才有。
// 存在意义是让本包在无构建约束的 `go build ./...` 下依然有 Image 入口。
package ocr

import "fmt"

// Image 非 Windows 平台不支持文字识别。
func Image(data string) (string, error) {
	return "", fmt.Errorf("文字识别仅支持 Windows")
}
