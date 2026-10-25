//go:build !windows

package docker

// ============================================================
// Docker Engine 客户端 —— 非 Windows 平台钩子（unix socket）
//
// 与 client_windows.go 提供同一组函数，供平台无关的 client.go 调用：
//   candidateEndpoints / dialEndpointConn / dialSaysEndpointWrong
//   isEndpointBusy / isTransportEndpointErr / shortEndpointAddr / parseDOCKERHOST
//
// Linux 上 Docker Engine 监听 /var/run/docker.sock（rootless 模式在
// $XDG_RUNTIME_DIR/docker.sock），协议与 Windows 完全相同，只是换了条
// 本地传输通道，所以上层的端点重扫、版本协商、闸门熔断都能原样复用。
// ============================================================

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// errDaemonUnavailable 端点连不上时的统一提示：与 Windows 各一份，措辞对齐各自平台的启动方式
const errDaemonUnavailable = "无法连接 Docker 引擎（请确认 docker 服务正在运行：systemctl status docker）"

// defaultSockets 默认探测的 socket 路径（root 模式与 rootless 模式的常见位置）
var defaultSockets = []string{"/var/run/docker.sock"}

// candidateEndpoints 默认候选：系统级 socket，再加上 rootless 的运行时目录。
// 与 Windows 的多管道候选同理——每轮重扫，引擎换了位置下一轮就能跟上。
func candidateEndpoints() []endpoint {
	paths := make([]string, 0, 2)
	paths = append(paths, defaultSockets...)
	if dir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); dir != "" {
		paths = append(paths, filepath.Join(dir, "docker.sock"))
	}
	out := make([]endpoint, 0, len(paths))
	for _, p := range paths {
		out = append(out, endpoint{network: "unix", addr: p, desc: "unix://" + p})
	}
	return out
}

// dialEndpointConn 按端点类型拨号：tcp 直连远端 daemon，其余走本地 unix socket
func dialEndpointConn(ctx context.Context, ep endpoint) (net.Conn, error) {
	var d net.Dialer
	switch ep.network {
	case "tcp":
		return d.DialContext(ctx, "tcp", ep.addr)
	case "unix":
		return d.DialContext(ctx, "unix", ep.addr)
	default:
		return nil, fmt.Errorf("未知的端点类型 %q（%s）", ep.network, ep.addr)
	}
}

// dialSaysEndpointWrong socket 不存在 / 不是 socket / 没人监听，都说明引擎不在这个
// 端点上：作废后重扫（rootless 与系统级 socket 可能互换）。
func dialSaysEndpointWrong(err error) bool {
	return errors.Is(err, fs.ErrNotExist) ||
		errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ENOTSOCK) ||
		errors.Is(err, syscall.ECONNREFUSED)
}

// isEndpointBusy unix socket 没有「实例被占满」的概念，内核会排队接受连接，
// 所以任何失败都不该被当成「引擎在忙」。
func isEndpointBusy(err error) bool { return false }

// isTransportEndpointErr 拨号/读写阶段的错误是否属于「端点层」（而非引擎返回的业务错误）
func isTransportEndpointErr(err error) bool {
	return errors.Is(err, fs.ErrNotExist) ||
		errors.Is(err, syscall.ENOENT) ||
		errors.Is(err, syscall.ENOTSOCK) ||
		errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.EPIPE)
}

// shortEndpointAddr unix socket 路径本身已经够短，原样展示
func shortEndpointAddr(ep endpoint) string { return ep.addr }

// parseDOCKERHOST 支持 unix:///var/run/docker.sock、tcp://host:port、http://host:port，
// npipe:// 是 Windows 专属，这里明确报错而不是静默忽略。
func parseDOCKERHOST(h string) (endpoint, error) {
	switch {
	case strings.HasPrefix(h, "unix://"):
		p := strings.TrimPrefix(h, "unix://")
		if p == "" {
			return endpoint{}, fmt.Errorf("unix:// 端点缺少路径: %s", h)
		}
		return endpoint{network: "unix", addr: p, desc: h}, nil
	case strings.HasPrefix(h, "tcp://"), strings.HasPrefix(h, "http://"):
		u, err := url.Parse(h)
		if err != nil {
			return endpoint{}, err
		}
		host := u.Host
		if u.Port() == "" {
			host += ":2375"
		}
		return endpoint{network: "tcp", addr: host, desc: h}, nil
	case strings.HasPrefix(h, "npipe://"):
		return endpoint{}, fmt.Errorf("非 Windows 平台不支持 npipe:// 端点: %s", h)
	default:
		return endpoint{}, fmt.Errorf("未知协议: %s", h)
	}
}
