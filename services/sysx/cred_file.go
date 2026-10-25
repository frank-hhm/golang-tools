//go:build !windows

package sysx

import (
	"os"
	"path/filepath"
)

// ============================================================
// 非 Windows 的凭据落盘：0600 权限文件
//
// 注意：这里**不是加密**，只是把文件权限收到属主可读写（目录 0700 / 文件 0600）。
// 在共享主机上这仍然等于"root 或同属主可读"，所以：
//   - 面板进程应使用专用低权用户运行；
//   - 若后续要更强保护，把 platformCred() 换成 libsecret（Secret Service）实现即可，
//     调用方只认 Cred 接口，不需要改业务代码。
// ============================================================

// fileCred 基于文件权限的凭据实现。
type fileCred struct{}

// platformCred 返回非 Windows 平台的凭据实现。
func platformCred() Cred { return fileCred{} }

// Save 以 0600 原子写入（临时文件 + rename，避免读到半截内容）。
func (fileCred) Save(path string, secret []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, secret, 0o600); err != nil {
		return err
	}
	// 显式 chmod：WriteFile 的 perm 会被 umask 削弱
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// Load 读取明文；文件不存在返回 (nil, nil)。
func (fileCred) Load(path string) ([]byte, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return raw, nil
}

// Delete 删除；不存在不算错误。
func (fileCred) Delete(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
