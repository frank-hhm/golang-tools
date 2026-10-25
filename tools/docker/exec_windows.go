//go:build windows

package docker

// ============================================================
// 容器交互式终端（docker exec）
//
// Engine API 两跳：
//   1) POST /containers/{id}/exec 创建 exec，拿到 exec id
//   2) POST /exec/{id}/start 带 Upgrade: tcp 升级成双向流（Tty=true 时是裸字节流）
// 升级成功后 resp.Body 就是可读可写的连接，直接当 Unix socket 用。
// 改窗口尺寸走 POST /exec/{id}/resize。
// ============================================================

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// ExecStream 容器内终端：Read/Write 都是裸字节（已 Tty，不做多路复用帧解析）
type ExecStream struct {
	execID string
	rwc    io.ReadWriteCloser
}

// OpenExec 在容器里起一个交互 shell，argv 为空时用默认 sh
func OpenExec(containerID string, argv []string, rows, cols int) (*ExecStream, error) {
	id := strings.TrimSpace(containerID)
	if id == "" {
		return nil, fmt.Errorf("容器 ID 为空")
	}
	if len(argv) == 0 {
		argv = []string{"/bin/sh", "-c", "command -v bash >/dev/null 2>&1 && exec bash || exec sh"}
	}
	initClients()

	var created struct {
		ID string `json:"Id"`
	}
	body := map[string]any{
		"AttachStdin":  true,
		"AttachStdout": true,
		"AttachStderr": true,
		"Tty":          true,
		"Cmd":          argv,
	}
	if err := do(http.MethodPost, "/containers/"+url.PathEscape(id)+"/exec", nil, body, &created); err != nil {
		return nil, err
	}
	if created.ID == "" {
		return nil, fmt.Errorf("创建容器终端失败：Engine 未返回 exec id")
	}

	startBody, _ := json.Marshal(map[string]any{"Detach": false, "Tty": true})
	u := url.URL{Scheme: "http", Host: "localhost", Path: prefix() + "/exec/" + created.ID + "/start"}
	req, err := http.NewRequest(http.MethodPost, u.String(), bytes.NewReader(startBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	// 声明协议升级：daemon 会回 101，并把 socket 直接交给这条连接
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "tcp")

	resp, err := streamCli.Do(req)
	if err != nil {
		return nil, unavailableErr(err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols && resp.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(io.LimitReader(resp.Body, apiErrBodyMax))
		_ = resp.Body.Close()
		return nil, apiError(resp.StatusCode, data)
	}
	rwc, ok := resp.Body.(io.ReadWriteCloser)
	if !ok {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("Docker 未返回可读写的终端流")
	}

	s := &ExecStream{execID: created.ID, rwc: rwc}
	_ = s.Resize(rows, cols)
	return s, nil
}

// Resize 同步容器内 PTY 尺寸（失败不致命，继续用旧尺寸）
func (s *ExecStream) Resize(rows, cols int) error {
	if s == nil || s.execID == "" {
		return nil
	}
	q := url.Values{}
	q.Set("h", strconv.Itoa(rows))
	q.Set("w", strconv.Itoa(cols))
	return do(http.MethodPost, "/exec/"+s.execID+"/resize", q, nil, nil)
}

func (s *ExecStream) Read(p []byte) (int, error)  { return s.rwc.Read(p) }
func (s *ExecStream) Write(p []byte) (int, error) { return s.rwc.Write(p) }

func (s *ExecStream) Close() error {
	if s == nil || s.rwc == nil {
		return nil
	}
	return s.rwc.Close()
}
