//go:build windows && !server

package app

// nativeWindowsAvailable 标记当前构建是否能真正开原生窗口。
//
// Windows 桌面版（默认构建）为 true；-tags server 的 Web 运行模式虽然也编译
// 通过（Wails 的 Window 在 server 下是空实现），但点了不会有任何窗口出现，
// 所以算 false，交给前端走路由。
const nativeWindowsAvailable = true
