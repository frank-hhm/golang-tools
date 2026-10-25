//go:build !windows || server

package sysx

// nativeWindows 标记当前构建能否真正弹出原生窗口。
//
// Linux 上没有 WebView 可挂；Windows 加 -tags server 时 Wails 的 Window 是空实现。
// 两种情况都不能指望"点一下弹出新窗口"，前端应改用页面内路由。
const nativeWindows = false
