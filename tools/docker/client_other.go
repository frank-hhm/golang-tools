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

// defaultSockets 默认探测的 socket 路径（root 模式与 rootless 模式的常见位置）。
//
// /var/run 在多数发行版上是 /run 的符号链接，两个都列上是为了兼容手工改过
// tmpfs 挂载的机器（某些容器镜像里只有其中一个），重复项会在后面去重。
var defaultSockets = []string{"/var/run/docker.sock", "/run/docker.sock"}

// candidateEndpoints 默认候选：系统级 socket，再加上 rootless 的运行时目录。
// 与 Windows 的多管道候选同理——每轮重扫，引擎换了位置下一轮就能跟上。
func candidateEndpoints() []endpoint {
	paths := make([]string, 0, 4)
	paths = append(paths, defaultSockets...)
	if dir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR")); dir != "" {
		paths = append(paths, filepath.Join(dir, "docker.sock"))
	} else {
		// systemd 服务环境不一定带 XDG_RUNTIME_DIR，rootless 的 socket
		// 实际落在 /run/user/<uid>/docker.sock，按 uid 补一个候选
		paths = append(paths, fmt.Sprintf("/run/user/%d/docker.sock", os.Geteuid()))
	}
	seen := make(map[string]bool, len(paths))
	out := make([]endpoint, 0, len(paths))
	for _, p := range paths {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
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

// classifyDialError 给单个候选的拨号失败起个短名。
//
// “权限不足 / 不存在 / 无人监听”在拨号层都是失败，但用户要做的下一步完全不同，
// 混在一起只能给出一句没用的“请确认服务正在运行”。
func classifyDialError(err error) string {
	switch {
	case err == nil:
		return "正常"
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return "权限不足"
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOENT):
		return "socket 不存在"
	case errors.Is(err, syscall.ECONNREFUSED):
		return "socket 在但无人监听"
	case errors.Is(err, syscall.ENOTSOCK):
		return "路径不是 socket"
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, os.ErrDeadlineExceeded):
		return "探测超时"
	}
	return oneLine(truncate(err.Error(), 60))
}

// daemonUnavailableHint 引擎连不上时的下一步动作（按候选的真实失败原因归类）。
// 与 Windows 侧同名：那边有“安装 / 启动 Desktop”按钮可点，这里只能给命令。
func daemonUnavailableHint(errs []error) string {
	denied, missing, refused := false, 0, false
	for _, err := range errs {
		switch {
		case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
			denied = true
		case errors.Is(err, syscall.ECONNREFUSED):
			refused = true
		case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOENT):
			missing++
		}
	}
	switch {
	case denied:
		return "提示：当前用户无权访问 /var/run/docker.sock。把运行面板的用户加入 docker 组（sudo usermod -aG docker $USER，重新登录后生效），或让面板以 root 运行。"
	case len(errs) > 0 && missing == len(errs) && inContainer():
		// 容器部署（宝塔/青龙/自建镜像）最常见的一坑：宿主机的 socket 不会
		// 自动出现在容器里，必须显式挂载
		return "提示：面板运行在容器内，而容器里没有 docker.sock。启动容器时加上 -v /var/run/docker.sock:/var/run/docker.sock 并确认当前用户在 docker 组，否则容器内永远看不到宿主机引擎。"
	case len(errs) > 0 && missing == len(errs):
		return "提示：没有找到 docker.sock，说明 Docker Engine 还没安装或还没启动。先看 systemctl status docker；未安装可执行 sudo apt install docker.io（Debian/Ubuntu）或 sudo dnf install docker-ce（RHEL 系）。若 daemon 只监听 TCP，请设置 DOCKER_HOST=tcp://127.0.0.1:2375。"
	case refused:
		return "提示：socket 存在但没有进程应答，通常是 docker 服务刚停或刚重启：systemctl status docker、journalctl -u docker -n 50。"
	}
	return ""
}

// inContainer 面板自己是否跑在容器里。
//
// 只用来看“为什么没有 docker.sock”：容器内必须显式挂载宿主 socket，
// 与“没装 docker”是两件完全不同的事，给的修复动作也不一样。
func inContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	if data, err := os.ReadFile("/proc/1/cgroup"); err == nil {
		s := string(data)
		return strings.Contains(s, "docker") || strings.Contains(s, "containerd") || strings.Contains(s, "kubepods")
	}
	return false
}

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
