//go:build windows

package docker

import (
	"fmt"
	"os"
	"path/filepath"
)

// daemonFilePath 加速站配置的位置：Docker Desktop 用用户级
// %USERPROFILE%\.docker\daemon.json，目录不存在时顺手建出来。
func daemonFilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".docker")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建 %s 失败: %v", dir, err)
	}
	return filepath.Join(dir, daemonJSONName), nil
}
