//go:build !windows

package docker

// ============================================================
// 仓库密码的本地"加密"（非 Windows 降级实现）
//
// Windows 用 DPAPI：密文与当前用户、当前机器绑定，拷走也解不开。
// 非 Windows 没有等价的内核级用户绑定加密，这里退化为 base64 编码——
// 它不提供机密性，只是让密码不以便于肉眼阅读的明文出现在配置里，
// 真正的保护来自配置文件本身的 0600 权限（见 services/config 的落盘方式）。
//
// 不要把这个函数当成"加密"来宣传：面板里如果需要展示安全说明，
// 应当明确告诉用户非 Windows 平台上的仓库密码只是权限保护。
// ============================================================

import (
	"encoding/base64"
	"fmt"
)

// dpapiProtect 与 Windows 版同签名，便于 registry.go 不写平台分支
func dpapiProtect(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	return base64.StdEncoding.EncodeToString([]byte(plain)), nil
}

// dpapiUnprotect 解码 base64；格式不对时明确报错而不是返回半个密码
func dpapiUnprotect(enc string) (string, error) {
	if enc == "" {
		return "", nil
	}
	raw, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", fmt.Errorf("密文格式错误: %v", err)
	}
	return string(raw), nil
}
