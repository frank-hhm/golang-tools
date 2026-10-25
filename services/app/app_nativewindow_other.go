//go:build !windows || server

package app

// nativeWindowsAvailable 标记当前构建是否能真正开原生窗口。
//
// Web 运行模式（-tags server，含 Windows 上的 server 构建）与 Linux 桌面之外的
// 场景都没有可用的原生窗口，前端必须改用 router.push 在同一个页面里切换路由。
const nativeWindowsAvailable = false
