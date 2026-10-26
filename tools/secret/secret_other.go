//go:build !windows

package secret

// ============================================================
// 非 Windows 平台的密码落盘
//
// 没有 DPAPI。这里把密码按 plain: 前缀原样写进记录文件，文件本身是 0600，
// 只有属主可读。加前缀而不是原样返回，是为了区分"本平台存的明文"和
// "从 Windows 同步过来的 DPAPI 密文"——后者在本平台解不开，必须明确报错，
// 不能把密文当密码去登录。
// ============================================================

import (
	"fmt"
	"strings"
)

const plainPrefix = "plain:"

// Protect 非 Windows 平台：加 plain: 前缀存明文（文件权限由调用方保证 0600）
func Protect(plain string) (string, error) {
	return plainPrefix + plain, nil
}

// Unprotect 去掉 plain: 前缀；遇到 DPAPI 密文直接报错
func Unprotect(enc string) (string, error) {
	if after, ok := strings.CutPrefix(enc, plainPrefix); ok {
		return after, nil
	}
	if enc == "" {
		return "", nil
	}
	return "", fmt.Errorf("该密码是 Windows DPAPI 加密的，本平台无法解密，请重新录入")
}
