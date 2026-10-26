//go:build windows

package term

// 密码加密复用 tools/secret（Windows 上是 DPAPI），这里只保留原来的包内函数名，
// 避免 term 与 ftp 各维护一份 crypt32 调用。

import "go-tools/tools/secret"

func protectSecret(plain string) (string, error) { return secret.Protect(plain) }

func unprotectSecret(enc string) (string, error) { return secret.Unprotect(enc) }
