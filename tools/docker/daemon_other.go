//go:build !windows

package docker

import (
	"fmt"
	"os"
	"path/filepath"
)

// etcDockerDir 系统级 Docker 配置目录，单测可以替换掉
var etcDockerDir = "/etc/docker"

// daemonFilePath 加速站配置的位置。
//
// Linux 上 daemon 读的是 /etc/docker/daemon.json（root 权限），不是用户目录下的
// ~/.docker/daemon.json —— 后者只影响 docker CLI 的凭据与上下文。写错地方的后果是
// 面板显示"已保存"但加速站根本没生效，所以这里必须指向系统级路径。
//
// 目录由 Docker 包自己维护，这里不主动创建：没有 /etc/docker 说明引擎可能还没装，
// 让调用方拿到明确的报错比静默造一个目录更好。
func daemonFilePath() (string, error) {
	if _, err := os.Stat(etcDockerDir); err != nil {
		return "", fmt.Errorf("未找到 %s（Docker Engine 可能尚未安装）: %v", etcDockerDir, err)
	}
	return filepath.Join(etcDockerDir, daemonJSONName), nil
}
