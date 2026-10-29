package term

// ============================================================
// 容器终端（复用 SSH 那套会话/事件通道）
//
// 与 SSH 的唯一区别是“数据从哪来”：这里读的是 Docker Engine 的
// exec 升级流（tools/docker.OpenExec），会话表、输入、改尺寸、
// 断开通知全部复用 ssh_windows.go 里的同一份实现。
// 因此前端只要把 openSession 换成 TermOpenDocker，就能用同一个终端组件。
// ============================================================

import (
	"encoding/base64"
	"strings"

	"Tools/tools/docker"
)

// OpenDocker 在容器内起一个 shell，title 用于标签显示（一般传容器名）
func OpenDocker(containerID, title string, cols, rows int) (SessionInfo, error) {
	cols, rows = clampSize(cols, rows)
	stream, err := docker.OpenExec(containerID, nil, rows, cols)
	if err != nil {
		return SessionInfo{}, err
	}

	id := nextSessionID()
	name := strings.TrimSpace(title)
	if name == "" {
		name = containerID
		if len(name) > 12 {
			name = name[:12]
		}
	}
	s := &session{
		info:  SessionInfo{ID: id, Title: name, Kind: "docker"},
		write: stream,
		resize: func(cols, rows int) error {
			return stream.Resize(rows, cols)
		},
	}
	s.shut = func() { _ = stream.Close() }

	sessMu.Lock()
	sessions[id] = s
	sessMu.Unlock()

	go func() {
		buf := make([]byte, 8192)
		for {
			n, rerr := stream.Read(buf)
			if n > 0 {
				emit("term:data", DataEvent{SessionID: id, Data: base64.StdEncoding.EncodeToString(buf[:n])})
			}
			if rerr != nil {
				closeSession(id, "容器终端已断开")
				return
			}
		}
	}()

	return s.info, nil
}
