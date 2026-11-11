package term

// ============================================================
// 本地 shell 会话（cmd / PowerShell）
//
// 与 SSH / Docker 复用同一套会话表与事件通道（sessions / emitWriter /
// term:data / term:exit / Write / Resize / Close），前端只要把
// openSession 换成 TermOpenLocal 就能用同一个终端组件。
//
// 真正的「起进程 + 伪终端」是平台相关的：
//   - Windows：ConPTY（local_windows.go），交互式真终端；
//   - 其他平台：如实返回不支持（local_other.go）。
// ============================================================

import (
	"fmt"
	"os"
	"path/filepath"
)

// localShellCommand 把 shell 种类映射成可执行文件绝对路径与参数。
// 优先从 SystemRoot 下取系统自带解释器，取不到再回落到 PATH 上的裸名。
func localShellCommand(shell string) (name string, args []string, err error) {
	sys := os.Getenv("SystemRoot")
	switch normalizeShell(shell) {
	case ShellCmd:
		if sys != "" {
			p := filepath.Join(sys, "System32", "cmd.exe")
			if _, e := os.Stat(p); e == nil {
				return p, nil, nil
			}
		}
		return "cmd.exe", nil, nil
	case ShellPS:
		if sys != "" {
			p := filepath.Join(sys, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
			if _, e := os.Stat(p); e == nil {
				return p, []string{"-NoLogo"}, nil
			}
		}
		return "powershell.exe", []string{"-NoLogo"}, nil
	default:
		return "", nil, fmt.Errorf("不支持的本地 shell 类型：%q", shell)
	}
}

// OpenLocal 按一条本地连接记录起一个交互式 shell 会话。
func OpenLocal(hostID string, cols, rows int) (SessionInfo, error) {
	h, _, err := resolve(hostID)
	if err != nil {
		return SessionInfo{}, err
	}
	if h.Kind != KindLocal {
		return SessionInfo{}, fmt.Errorf("该连接不是本地终端记录")
	}
	cols, rows = clampSize(cols, rows)

	exe, args, cerr := localShellCommand(h.Shell)
	if cerr != nil {
		return SessionInfo{}, cerr
	}
	cwd, _ := os.UserHomeDir()

	id := nextSessionID()
	s, lerr := startLocalShell(id, h.Name, exe, args, cwd, cols, rows)
	if lerr != nil {
		return SessionInfo{}, lerr
	}
	sessMu.Lock()
	sessions[id] = s
	sessMu.Unlock()
	return s.info, nil
}
