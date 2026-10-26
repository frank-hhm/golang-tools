//go:build !windows

package mysql

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"go-tools/services/sysx"
)

// binFileName 可执行文件名（Linux 系统包里的 mysqld 不带扩展名）。
const binFileName = "mysqld"

// systemPackaged Linux 上走系统包管理器安装。
//
// MySQL 官方虽然提供 Linux 的 generic tar.xz，但它依赖 glibc 版本、需要
// 手工建 mysql 用户和初始化目录，换成 dnf/apt 装一个是给用户省事得多的事。
const systemPackaged = true

// systemPkgNames 系统包候选（按优先级）：社区版两类命名都试一遍。
var systemPkgNames = []string{"mysql-server", "mysql-community-server"}

// runAsUserArgs 以 root 运行面板时必须告诉 mysqld 用哪个账户。
//
// mysqld 拒绝以 root 身份直接跑（"Please read Security section..."），而系统包
// 安装会顺手创建 mysql 用户。非 root 启动则不需要这个参数。
func runAsUserArgs() []string {
	if sysx.Caps().IsRoot {
		return []string{"--user=mysql"}
	}
	return nil
}

// prepareInstanceDir 把实例目录交给 mysql 用户。
//
// 两件事必须做：数据目录要能被 mysql 写（否则初始化直接失败），实例目录里
// 的 my.ini / mysql.err 同理（错误日志也由 mysqld 自己写）。
func prepareInstanceDir(dir string) error {
	if !sysx.Caps().IsRoot {
		return nil // 非 root 启动时 mysqld 用的就是当前用户，无需转交
	}
	u, err := user.Lookup("mysql")
	if err != nil {
		return fmt.Errorf("未找到 mysql 用户（系统包安装会创建它）：%v；可先执行 useradd -r -s /sbin/nologin mysql", err)
	}
	uid, _ := strconv.Atoi(u.Uid)
	gid, _ := strconv.Atoi(u.Gid)
	return filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if chErr := os.Chown(p, uid, gid); chErr != nil && !os.IsPermission(chErr) {
			return fmt.Errorf("调整 %s 属主失败: %w", p, chErr)
		}
		return nil
	})
}

// initFailHint 把 Linux 特有的两类失败翻译成下一步动作。
//
// 这两条都不是“配置写错了”，而是发行版的环境限制：报错原文只说 Permission denied，
// 用户看不到 AppArmor 这一层；root 那条则是 mysqld 自己的安全检查。
func initFailHint(output string) string {
	for _, kw := range []string{"Permission denied", "Can't create/write to file", "errno: 13"} {
		if strings.Contains(output, kw) {
			return "\n提示：Ubuntu/Debian 的 AppArmor 默认只允许 mysqld 读写 /var/lib/mysql，实例数据目录在别处会被拒绝。" +
				"可执行 sudo aa-complain /usr/sbin/mysqld 切到宽容模式，或在 /etc/apparmor.d/usr.sbin.mysqld 里为实例目录加一条规则后 reload。"
		}
	}
	if strings.Contains(output, "Security") {
		return "\n提示：mysqld 拒绝以 root 直接运行（需 --user），并要保证数据目录属于该用户；" +
			"若 mysql 用户不存在，先装系统包或 useradd -r -s /sbin/nologin mysql"
	}
	return ""
}

// myIniPlatformLines 平台相关的 socket / pid 文件位置。
//
// Linux 上 mysqld 默认把 socket 放到 /var/run/mysqld/mysqld.sock——那是全局唯一
// 路径，第二个实例启动时会因 bind 失败直接退出（“Another process ... is using unix
// socket file”）。多实例必须各用各的 socket 与 pid 文件，放在各自实例目录里。
func myIniPlatformLines(instDir string) string {
	return fmt.Sprintf("socket=%s\npid-file=%s\n",
		filepath.ToSlash(filepath.Join(instDir, "mysql.sock")),
		filepath.ToSlash(filepath.Join(instDir, "mysqld.pid")))
}

// hideWindowAttr 非 Windows 平台无需隐藏窗口
func hideWindowAttr() *syscall.SysProcAttr {
	return nil
}
