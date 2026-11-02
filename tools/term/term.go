package term

// ============================================================
// 终端连接记录（主机表）
//
// 落盘：<data>/term/hosts.json；密码只存 DPAPI 密文（dpapi_windows.go），
// 回给前端的记录里密码恒为空串，只有真正建连时才解密使用。
// “系统”一项只决定默认端口与展示，远端到底给什么 shell 由 SSH 服务端决定。
// ============================================================

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"Tools/appdirs"
)

const (
	SystemLinux   = "linux"
	SystemWindows = "windows"

	AuthPassword = "password"
	AuthKey      = "key"
)

// Host 一条连接记录（前端列表/表单共用）。
//
// 凭据体系（P0）后，登录方式优先取自 CredID 指向的 Credential；
// Password/KeyPath/Auth/User 仍是旧字段，保留读取以兼容迁移前的存量记录，
// 新记录由前端「选凭据」填写 CredID，不再内联密码。
type Host struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	System    string   `json:"system"`             // linux | windows
	Address   string   `json:"address"`            // IP 或域名
	Port      int      `json:"port"`               // SSH 端口
	User      string   `json:"user"`               // SSH 账号（旧字段，凭据未给账号时兜底）
	Auth      string   `json:"auth"`               // password | key（旧字段）
	Password  string   `json:"password,omitempty"` // 明文只在此刻出现，落盘前即加密（旧字段）
	KeyPath   string   `json:"keyPath,omitempty"`  // 私钥文件路径（旧字段，auth=key）
	CredID    string   `json:"credID,omitempty"`   // 关联的凭据 id（P0：一处改密多机生效）
	Group     string   `json:"group,omitempty"`    // 分组名（左栏分组树）
	Tags      []string `json:"tags,omitempty"`     // 标签（列表筛选）
	Color     string   `json:"color,omitempty"`    // 列表圆点/标签配色
	Note      string   `json:"note,omitempty"`     // 备注，列表里显示为“名称”
	UpdatedAt int64    `json:"updatedAt"`
}

// SessionInfo 打开终端后回给前端的句柄
type SessionInfo struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind"` // ssh | docker
}

// DataEvent 终端输出：Base64 编码，既避开 JSON 转义又能原样带过二进制控制序列
type DataEvent struct {
	SessionID string `json:"sessionId"`
	Data      string `json:"data"`
}

// ExitEvent 会话结束（reason 直接给前端当提示语）
type ExitEvent struct {
	SessionID string `json:"sessionId"`
	Reason    string `json:"reason"`
}

// hostRecord 落盘形态：密码换成 DPAPI 密文
type hostRecord struct {
	Host
	PassEnc string `json:"passEnc,omitempty"`
}

var storeMu sync.Mutex

// dataDir <data>/term 目录（hosts.json / credentials.json / known_hosts 同放一处）
func dataDir() (string, error) {
	return appdirs.Data("term")
}

// storePath <data>/term/hosts.json（目录随全局 data 基础目录走）
func storePath() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "hosts.json"), nil
}

func readRecords() ([]hostRecord, error) {
	path, err := storePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []hostRecord{}, nil
		}
		return nil, fmt.Errorf("读取终端记录失败: %w", err)
	}
	var recs []hostRecord
	if len(data) > 0 {
		if err := json.Unmarshal(data, &recs); err != nil {
			return nil, fmt.Errorf("终端记录文件损坏: %w", err)
		}
	}
	// 旧记录升格为凭据（幂等，失败原样返回不阻塞启动）
	recs = migrateHostCreds(recs)
	return recs, nil
}

func writeRecords(recs []hostRecord) error {
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

// toHost 去掉密文与明文密码，得到可以安全交给前端的记录
func (r hostRecord) toHost() Host {
	h := r.Host
	h.Password = ""
	return h
}

// ListHosts 记录列表（新建的排在前面）
func ListHosts() ([]Host, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	recs, err := readRecords()
	if err != nil {
		return nil, err
	}
	out := make([]Host, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.toHost())
	}
	return out, nil
}

// SaveHost 新建或更新一条记录。
// 关联凭据（CredID 非空）时登录方式取自凭据，不再校验内联 User/密码/私钥；
// 未关联凭据而直接填了密码/私钥的旧式记录照旧落 PassEnc（保留兼容路径）。
func SaveHost(in Host) ([]Host, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Address = strings.TrimSpace(in.Address)
	in.User = strings.TrimSpace(in.User)
	in.KeyPath = strings.TrimSpace(in.KeyPath)
	in.Note = strings.TrimSpace(in.Note)
	in.CredID = strings.TrimSpace(in.CredID)
	in.Group = strings.TrimSpace(in.Group)
	in.Color = strings.TrimSpace(in.Color)
	in.Tags = normalizeTags(in.Tags)
	if in.System != SystemWindows {
		in.System = SystemLinux
	}
	if in.Address == "" {
		return nil, fmt.Errorf("请填写服务器 IP 或域名")
	}
	if in.Port <= 0 || in.Port > 65535 {
		return nil, fmt.Errorf("端口需在 1-65535 之间")
	}
	if in.CredID == "" {
		// 旧式内联凭据：仍按原规则校验账号与认证方式
		if in.Auth != AuthKey {
			in.Auth = AuthPassword
		}
		if in.User == "" {
			return nil, fmt.Errorf("请填写 SSH 账号")
		}
		if in.Auth == AuthKey {
			if in.KeyPath == "" {
				return nil, fmt.Errorf("请选择私钥文件")
			}
			if _, err := os.Stat(in.KeyPath); err != nil {
				return nil, fmt.Errorf("私钥文件不可读: %v", err)
			}
		}
	} else {
		// 走凭据：主机自身不再存内联密码，认证细节全部交给凭据表
		in.Password = ""
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
	rec := hostRecord{Host: in}
	if idx >= 0 {
		// 编辑：没填新密码就把原密文带上，避免“保存一次密码就丢了”
		rec.ID = recs[idx].ID
		rec.PassEnc = recs[idx].PassEnc
	} else {
		if rec.ID == "" {
			rec.ID = fmt.Sprintf("h%d", time.Now().UnixNano())
		}
		if rec.Note == "" {
			rec.Note = fmt.Sprintf("服务器%d", len(recs)+1)
		}
	}
	if in.Password != "" {
		enc, err := protectSecret(in.Password)
		if err != nil {
			return nil, err
		}
		rec.PassEnc = enc
	} else if in.Auth == AuthKey {
		// 改成密钥登录后旧密码没有意义，顺手清掉
		rec.PassEnc = ""
	}
	// 列表标题：备注优先，其次 IP（表单里没有单独的“名称”项）
	if rec.Name == "" {
		rec.Name = rec.Note
	}
	if rec.Name == "" {
		rec.Name = rec.Address
	}

	if idx >= 0 {
		recs[idx] = rec
	} else {
		recs = append([]hostRecord{rec}, recs...)
	}
	if err := writeRecords(recs); err != nil {
		return nil, fmt.Errorf("保存终端记录失败: %w", err)
	}
	out := make([]Host, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.toHost())
	}
	return out, nil
}

// DeleteHost 删除一条记录
func DeleteHost(id string) ([]Host, error) {
	storeMu.Lock()
	defer storeMu.Unlock()
	recs, err := readRecords()
	if err != nil {
		return nil, err
	}
	kept := make([]hostRecord, 0, len(recs))
	for _, r := range recs {
		if r.ID != id {
			kept = append(kept, r)
		}
	}
	if len(kept) != len(recs) {
		if err := writeRecords(kept); err != nil {
			return nil, fmt.Errorf("保存终端记录失败: %w", err)
		}
	}
	out := make([]Host, 0, len(kept))
	for _, r := range kept {
		out = append(out, r.toHost())
	}
	return out, nil
}
