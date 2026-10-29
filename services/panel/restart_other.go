//go:build !windows

package panel

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// restartPanel 重启面板服务本身。
//
// 优先交给 systemd：面板通常以 systemd unit 托管，由它重启最干净
// （不担心新旧进程抢同一端口、不担心日志句柄重复打开）。
// 没有 systemd 时明确告诉用户该怎么做，而不是盲目拉起一个"孤儿进程"——
// 那会留下两个进程抢端口，反而更难排查。
func restartPanel() error {
	if _, err := os.Stat("/run/systemd/system"); err != nil {
		return errors.New("未检测到 systemd，请用你的托管方式重启（如 docker restart <容器名>）")
	}
	unit := detectUnit()
	if unit == "" {
		return errors.New("未能识别面板的 systemd 服务名，可手动执行：sudo systemctl restart <服务名>")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("需要 root 权限，请执行：sudo systemctl restart %s", unit)
	}
	out, err := exec.Command("systemctl", "restart", unit).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("systemctl restart %s 失败：%s", unit, msg)
	}
	return nil
}

// detectUnit 识别当前进程所属的 systemd 服务名：
//   - 环境变量 GO_TOOLS_UNIT 可直接指定；
//   - 否则从 /proc/self/cgroup 里读形如 xxx.service 的项；
//   - 都拿不到时按常见命名试一遍。
func detectUnit() string {
	if v := strings.TrimSpace(os.Getenv("GO_TOOLS_UNIT")); v != "" {
		return v
	}
	if data, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			parts := strings.Split(line, "/")
			last := parts[len(parts)-1]
			if strings.HasSuffix(last, ".service") && !strings.Contains(last, "@") {
				return last
			}
		}
	}
	for _, name := range []string{"Tools-server", "Tools", "Tools-panel"} {
		if _, err := os.Stat(filepath.Join("/etc/systemd/system", name+".service")); err == nil {
			return name
		}
		if _, err := os.Stat(filepath.Join("/lib/systemd/system", name+".service")); err == nil {
			return name
		}
	}
	return ""
}
