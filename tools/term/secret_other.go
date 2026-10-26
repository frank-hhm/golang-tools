//go:build !windows

package term

// 密码加密复用 tools/secret：非 Windows 平台按 plain: 前缀存明文（文件 0600），
// 遇到 Windows DPAPI 密文会明确报错。

import "go-tools/tools/secret"

func protectSecret(plain string) (string, error) { return secret.Protect(plain) }

func unprotectSecret(enc string) (string, error) { return secret.Unprotect(enc) }
