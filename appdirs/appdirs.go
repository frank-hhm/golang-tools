// Package appdirs 全局管理应用的三类基础目录（插件 plugins / 数据 data / 日志 logs）
// 及 <基础目录>/<服务名> 的服务子目录。
//
// 默认落在可执行文件同级目录（绿色便携布局）：<应用目录>/{plugins,data,logs}；
// 可由 config.json 的 paths 段覆盖，设置页修改后重新 Set 即全局即时生效
// （各服务每次解析路径都实时读取本包，不做二次缓存）。
//
// 约定：
//   - plugins/<服务名>：服务程序安装位置（php/nginx/mysql/redis/npm 等）
//   - data/<服务名>   ：服务的数据缓存（实例登记、配置、工作目录等）；
//     另含共享伪服务目录 www / screenshots / videos（站点根、截图、录像默认落点）
//   - logs/<服务名>   ：服务日志（app=程序日志、mysql=SQL 日志、php=实例服务日志等）
package appdirs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	mu          sync.RWMutex
	pluginsBase string
	dataBase    string
	logsBase    string
)

// DefaultRoot 三类目录的默认基准：可执行文件所在目录；取不到时退回用户配置目录 go-tools
func DefaultRoot() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "."
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "go-tools")
}

// Set 应用基础目录配置（空值=默认 <应用目录>/xxx），并确保三个根目录存在。
// 启动加载配置与设置页保存时调用，幂等。
func Set(plugins, data, logs string) error {
	root := DefaultRoot()
	resolve := func(v, def string) string {
		if strings.TrimSpace(v) == "" {
			return filepath.Join(root, def)
		}
		return filepath.Clean(v)
	}
	p, d, l := resolve(plugins, "plugins"), resolve(data, "data"), resolve(logs, "logs")

	mu.Lock()
	pluginsBase, dataBase, logsBase = p, d, l
	mu.Unlock()

	for _, dir := range []string{p, d, l} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return nil
}

// PluginsBase 插件根目录（未 Set 时回退默认，永不为空）
func PluginsBase() string {
	mu.RLock()
	b := pluginsBase
	mu.RUnlock()
	if b == "" {
		return filepath.Join(DefaultRoot(), "plugins")
	}
	return b
}

// DataBase 数据根目录（未 Set 时回退默认，永不为空）
func DataBase() string {
	mu.RLock()
	b := dataBase
	mu.RUnlock()
	if b == "" {
		return filepath.Join(DefaultRoot(), "data")
	}
	return b
}

// LogsBase 日志根目录（未 Set 时回退默认，永不为空）
func LogsBase() string {
	mu.RLock()
	b := logsBase
	mu.RUnlock()
	if b == "" {
		return filepath.Join(DefaultRoot(), "logs")
	}
	return b
}

// Plugins 返回 <插件目录>/<服务名>（不存在则创建）
func Plugins(service string) (string, error) { return sub(PluginsBase(), service) }

// Data 返回 <数据目录>/<服务名>（不存在则创建）
func Data(service string) (string, error) { return sub(DataBase(), service) }

// Logs 返回 <日志目录>/<服务名>（不存在则创建）
func Logs(service string) (string, error) { return sub(LogsBase(), service) }

func sub(base, service string) (string, error) {
	svc := filepath.Base(filepath.FromSlash(strings.TrimSpace(service)))
	if svc == "" || svc == "." || svc == string(filepath.Separator) {
		return "", fmt.Errorf("非法服务目录名: %q", service)
	}
	dir := filepath.Join(base, svc)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// MoveIfAbsent 一次性布局迁移：old 存在且 new 不存在时把 old 移到 new。
// 跨卷/占用导致 rename 失败时回退复制（文件复制后删除源，目录仅复制保留源）。
// old 不存在或 new 已存在则静默跳过，幂等可重复调用。
func MoveIfAbsent(oldPath, newPath string) error {
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return nil
	}
	if _, err := os.Stat(oldPath); err != nil {
		return nil
	}
	if _, err := os.Stat(newPath); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
		return err
	}
	if err := os.Rename(oldPath, newPath); err == nil {
		return nil
	}
	st, err := os.Stat(oldPath)
	if err != nil {
		return err
	}
	if st.IsDir() {
		return copyTree(oldPath, newPath)
	}
	if err := copyFile(oldPath, newPath); err != nil {
		return err
	}
	return os.Remove(oldPath)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, rerr := filepath.Rel(src, path)
		if rerr != nil {
			return rerr
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}
