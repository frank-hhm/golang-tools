//go:build !windows

package sshgen

// tightenKeyACL 在非 Windows 上是空操作：落盘时的 0600 已由 POSIX 权限模型保证，
// OpenSSH 直接按 mode 位校验，不存在继承 ACL 这回事。
func tightenKeyACL(string) error { return nil }
