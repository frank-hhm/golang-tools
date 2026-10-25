//go:build windows && !server

package sysx

// nativeWindows 标记当前构建能否真正弹出原生窗口（Windows 桌面版为 true）。
//
// -tags server 的 Web 运行模式同样跑在 Windows 上，也能编译通过，
// 但 Wails 的 Window 在 server 下是空实现，所以算 false。
const nativeWindows = true
