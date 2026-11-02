package sshgen

import (
	"encoding/json"
	"fmt"
	"strings"
)

// 连接凭据导出/导入：带版本号的 JSON，只含 host/port/user/keyPath/公钥/注释/别名，
// 绝不含密码或私钥正文（计划 §零：导出内容不包含机密）。

const (
	connKind    = "gtools.ssh-conn"
	connVersion = 1
)

type connBlob struct {
	Kind    string `json:"kind"`
	V       int    `json:"v"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	User    string `json:"user"`
	Alias   string `json:"alias"`
	KeyPath string `json:"keyPath"`
	PubKey  string `json:"pubKey"`
	Comment string `json:"comment"`
}

// ExportConn 导出一份可分享/导入的连接凭据（不含任何机密）。
func ExportConn(h HostRef) (string, error) {
	if strings.TrimSpace(h.Address) == "" {
		return "", fmt.Errorf("缺少主机地址，无法导出凭据")
	}
	b := connBlob{
		Kind:    connKind,
		V:       connVersion,
		Address: h.Address,
		Port:    h.Port,
		User:    h.User,
		Alias:   h.Alias,
		KeyPath: h.KeyPath,
		PubKey:  h.PubKey,
		Comment: h.Comment,
	}
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return "", fmt.Errorf("序列化凭据失败: %w", err)
	}
	return string(data), nil
}

// ImportConn 解析并校验一份连接凭据（kind 与版本必须匹配）。
func ImportConn(text string) (HostRef, error) {
	var b connBlob
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &b); err != nil {
		return HostRef{}, fmt.Errorf("连接凭据解析失败: %w", err)
	}
	if b.Kind != connKind {
		return HostRef{}, fmt.Errorf("不是 go-tools 的连接凭据（kind=%q）", b.Kind)
	}
	if b.V != connVersion {
		return HostRef{}, fmt.Errorf("不支持的凭据版本 v=%d（当前支持 v=%d）", b.V, connVersion)
	}
	if strings.TrimSpace(b.Address) == "" {
		return HostRef{}, fmt.Errorf("凭据缺少主机地址")
	}
	return HostRef{
		Address: b.Address,
		Port:    b.Port,
		User:    b.User,
		Alias:   b.Alias,
		KeyPath: b.KeyPath,
		PubKey:  b.PubKey,
		Comment: b.Comment,
	}, nil
}
