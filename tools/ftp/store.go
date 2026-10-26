package ftp

// ============================================================
// 远程连接记录（Xftp 式的站点表）
//
// 落盘：<data>/ftp/connections.json；密码只存 DPAPI 密文（tools/secret），
// 回给前端的记录里密码恒为空串，只有真正建连时才解密使用。
// 与 tools/term 的 hosts.json 分开存：终端和文件传输是两类入口，
// 互不污染列表，也避免协议字段互相牵制。
// ============================================================

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"go-tools/appdirs"
	"go-tools/tools/secret"
)

// connRecord 落盘形态：密码换成密文
type connRecord struct {
	Connection
	PassEnc string `json:"passEnc,omitempty"`
}

var storeMu sync.Mutex

// storePath <data>/ftp/connections.json
func storePath() (string, error) {
	dir, err := appdirs.Data("ftp")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "connections.json"), nil
}

func readRecords() ([]connRecord, error) {
	path, err := storePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []connRecord{}, nil
		}
		return nil, fmt.Errorf("读取连接记录失败: %w", err)
	}
	recs := []connRecord{}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &recs); err != nil {
			return nil, fmt.Errorf("连接记录文件损坏: %w", err)
		}
	}
	return recs, nil
}

func writeRecords(recs []connRecord) error {
	path, err := storePath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(recs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// toConn 去掉密文与明文密码，得到可以安全交给前端的记录
func (r connRecord) toConn() Connection {
	c := r.Connection
	c.Password = ""
	return c
}

func listOf(recs []connRecord) []Connection {
	out := make([]Connection, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.toConn())
	}
	return out
}

// ListConnections 连接记录列表（新建的排在前面）
func ListConnections() ([]Connection, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	recs, err := readRecords()
	if err != nil {
		return nil, err
	}
	return listOf(recs), nil
}

// SaveConnection 新建或更新一条记录；密码留空表示"沿用原密码"
func SaveConnection(in Connection) ([]Connection, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Host = strings.TrimSpace(in.Host)
	in.User = strings.TrimSpace(in.User)
	in.KeyPath = strings.TrimSpace(in.KeyPath)
	in.Note = strings.TrimSpace(in.Note)
	in.RemoteDir = strings.TrimSpace(in.RemoteDir)

	if in.Proto == "" {
		in.Proto = ProtoSFTP
	}
	if in.Proto != ProtoSFTP {
		return nil, fmt.Errorf("当前版本仅支持 SFTP，FTP/FTPS 在后续版本提供")
	}
	if in.Auth != AuthKey {
		in.Auth = AuthPassword
	}
	if in.Host == "" {
		return nil, fmt.Errorf("请填写服务器 IP 或域名")
	}
	if in.User == "" {
		return nil, fmt.Errorf("请填写登录账号")
	}
	if in.Port <= 0 || in.Port > 65535 {
		return nil, fmt.Errorf("端口需在 1-65535 之间")
	}
	if in.Auth == AuthKey {
		if in.KeyPath == "" {
			return nil, fmt.Errorf("请选择私钥文件")
		}
		if _, err := os.Stat(in.KeyPath); err != nil {
			return nil, fmt.Errorf("私钥文件不可读: %v", err)
		}
	}
	if in.RemoteDir != "" && !strings.HasPrefix(in.RemoteDir, "/") {
		in.RemoteDir = "/" + in.RemoteDir
	}

	storeMu.Lock()
	defer storeMu.Unlock()
	recs, err := readRecords()
	if err != nil {
		return nil, err
	}

	in.UpdatedAt = time.Now().Unix()
	idx := -1
	for i := range recs {
		if recs[i].ID == in.ID {
			idx = i
			break
		}
	}
	rec := connRecord{Connection: in}
	if idx >= 0 {
		// 编辑：没填新密码就把原密文带上，避免"保存一次密码就丢了"
		rec.ID = recs[idx].ID
		rec.PassEnc = recs[idx].PassEnc
	} else {
		if rec.ID == "" {
			rec.ID = fmt.Sprintf("f%d", time.Now().UnixNano())
		}
	}
	if in.Password != "" {
		enc, err := secret.Protect(in.Password)
		if err != nil {
			return nil, err
		}
		rec.PassEnc = enc
	} else if in.Auth == AuthKey {
		// 改成密钥登录后旧密码没有意义，顺手清掉
		rec.PassEnc = ""
	}
	// 列表标题：名称优先，其次备注，最后主机
	if rec.Name == "" {
		rec.Name = rec.Note
	}
	if rec.Name == "" {
		rec.Name = rec.Host
	}

	if idx >= 0 {
		recs[idx] = rec
	} else {
		recs = append([]connRecord{rec}, recs...)
	}
	if err := writeRecords(recs); err != nil {
		return nil, fmt.Errorf("保存连接记录失败: %w", err)
	}
	return listOf(recs), nil
}

// DeleteConnection 删除一条记录
func DeleteConnection(id string) ([]Connection, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	recs, err := readRecords()
	if err != nil {
		return nil, err
	}
	kept := make([]connRecord, 0, len(recs))
	for _, r := range recs {
		if r.ID != id {
			kept = append(kept, r)
		}
	}
	if len(kept) != len(recs) {
		if err := writeRecords(kept); err != nil {
			return nil, fmt.Errorf("保存连接记录失败: %w", err)
		}
	}
	return listOf(kept), nil
}

// credential 取出建连所需的记录与明文密码（密码只在内存里存在这一小会儿）
func credential(id string) (Connection, string, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	recs, err := readRecords()
	if err != nil {
		return Connection{}, "", err
	}
	for _, r := range recs {
		if r.ID != id {
			continue
		}
		pass := ""
		if r.PassEnc != "" {
			pass, err = secret.Unprotect(r.PassEnc)
			if err != nil {
				return Connection{}, "", err
			}
		}
		return r.toConn(), pass, nil
	}
	return Connection{}, "", fmt.Errorf("连接记录不存在，可能已被删除")
}
