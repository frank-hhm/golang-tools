//go:build windows

package sshgen

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// tightenKeyACL 把私钥文件的 NTFS 权限收紧到「仅当前用户可访问」。
//
// 这不是加固，是可用性前提。Windows 版 OpenSSH（ssh.exe / ssh-keygen.exe）在加载
// 私钥之前会检查文件 ACL：只要还有别人——包括从父目录继承下来的用户、组，甚至某个
// 已注销账号残留的 SID——能读到它，就打印 "WARNING: UNPROTECTED PRIVATE KEY FILE"
// 并把这把钥匙整个忽略掉，连接时表现为莫名的 Permission denied。
//
// os.WriteFile 传的 0600 在 NTFS 上表达不了这件事（文件继承目录 ACL），所以必须显式
// 清掉继承、再只授予当前用户。实测数据目录（<data>/ssh）继承来的 ACL 里就带着这类
// 残留 SID，不收紧生成的钥匙一律用不了。
func tightenKeyACL(path string) error {
	dom := strings.TrimSpace(os.Getenv("USERDOMAIN"))
	name := strings.TrimSpace(os.Getenv("USERNAME"))
	if name == "" {
		return fmt.Errorf("取不到当前用户名，无法收紧私钥权限")
	}
	principal := name
	if dom != "" && !strings.Contains(name, `\`) {
		principal = dom + `\` + name
	}
	out, err := exec.Command("icacls.exe", path, "/inheritance:r", "/grant:r", principal+":F").CombinedOutput()
	if err != nil {
		return fmt.Errorf("收紧私钥权限失败: %v｜%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
