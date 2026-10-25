//go:build !windows

package docker

// ============================================================
// Docker 后端识别的非 Windows 实现
//
// backendFromKernel 在 Windows 上靠内核串区分 Docker Desktop 的虚拟化后端：
// 带 microsoft-standard-WSL2 是 WSL2，否则是 Hyper-V（见 backend_windows.go）。
// 非 Windows 上 daemon 直接跑在宿主内核里，没有「虚拟化后端」这个概念，
// 面板里对应行显示"原生"即可。
// ============================================================

// backendFromKernel 非 Windows 没有 Desktop 后端可判，统一给"原生"
func backendFromKernel(kernel string) string { return "native" }

// cancelBackendWait 与 Windows 版的启动等待取消同签名（backend_windows.go 里有实现），
// 非 Windows 没有 Desktop 进程要等，绑定时直接返回。
func cancelBackendWait() {}
