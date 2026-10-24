package nginx

import (
	"path/filepath"
	"strings"

	"github.com/shirou/gopsutil/v3/process"
)

// nginxPids 返回当前所有 nginx 进程 PID（master + worker，按镜像名匹配）
func nginxPids() []int {
	procs, err := process.Processes()
	if err != nil {
		return nil
	}
	var out []int
	for _, p := range procs {
		name, err := p.Name()
		if err != nil {
			continue
		}
		if strings.EqualFold(name, "nginx.exe") || strings.EqualFold(name, "nginx") {
			out = append(out, int(p.Pid))
		}
	}
	return out
}

// nginxPidsForPrefix 返回命令行中包含指定 prefix 路径的 nginx 进程 PID（仅 master 会带 -p 参数）
// 用于精确定位本实例进程，避免误杀其他实例或本机其他 nginx
func nginxPidsForPrefix(prefix string) []int {
	if prefix == "" {
		return nil
	}
	procs, err := process.Processes()
	if err != nil {
		return nil
	}
	// 统一为小写正斜杠进行比对（Windows 路径大小写无关）
	normPrefix := strings.ToLower(filepath.ToSlash(prefix))
	var out []int
	for _, p := range procs {
		name, err := p.Name()
		if err != nil || (!strings.EqualFold(name, "nginx.exe") && !strings.EqualFold(name, "nginx")) {
			continue
		}
		cmdline, err := p.Cmdline()
		if err != nil || cmdline == "" {
			continue
		}
		if strings.Contains(strings.ToLower(filepath.ToSlash(cmdline)), normPrefix) {
			out = append(out, int(p.Pid))
		}
	}
	return out
}
