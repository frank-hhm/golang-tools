//go:build !windows

package app

import (
	"fmt"

	"Tools/services/sysx"
)

// restartSelf 非 Windows（主要是 -tags server 的 Linux 面板部署）不自行 fork 新进程：
// 那种场景下进程生命周期归 systemd / docker 管，自己再起一个只会和托管层抢同一个
// 监听端口，并且 systemd 本来就会因进程退出把它拉回来。方法照常存在（不藏 build tag），
// 如实返回不支持，调用方记日志、退化成一次普通退出。
func restartSelf() error {
	return fmt.Errorf("%w：当前平台的重启请交给 systemd / 容器编排，或手动重新启动程序", sysx.ErrUnsupported)
}
