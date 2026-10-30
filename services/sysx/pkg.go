package sysx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"Tools/appdirs"
	"Tools/services/applog"
)

// installedMarker 安装版标记文件名。
//
// 与各工具包既有约定保持完全一致（tools/nginx|mysql|redis|php|npm 都在用）：
// 目录里有这个文件 = 在线/离线安装版（允许删除）；没有 = 发布内置版（不允许删除）。
const installedMarker = ".Tools-installed"

// pkg 基于 appdirs 布局的包管理器。
type pkg struct {
	// root 可注入的插件根目录解析（测试用）；nil 表示走全局 appdirs。
	root func(service string) (string, error)
}

// newPkg 创建包管理器。
func newPkg() Pkg { return pkg{} }

// PluginsDir 返回某服务的插件根目录（不存在则创建）。
func (p pkg) PluginsDir(service string) (string, error) {
	svc := strings.TrimSpace(service)
	if svc == "" {
		return "", fmt.Errorf("service 不能为空")
	}
	if p.root != nil {
		return p.root(svc)
	}
	dir, err := appdirs.Plugins(svc)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建插件目录失败: %w", err)
	}
	return dir, nil
}

// Versions 列出已就位的版本（目录名即版本号，按名称排序）。
func (p pkg) Versions(service string) ([]Version, error) {
	root, err := p.PluginsDir(service)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]Version, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		dir := filepath.Join(root, e.Name())
		out = append(out, Version{
			Name:      e.Name(),
			Dir:       dir,
			Installed: isInstalledDir(dir),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Install 解包归档到 <plugins>/<service>/<version> 并写安装标记。
//
// 做法刻意分成三步，避免"半截目录被当成已安装"：
//  1. 解到同级临时目录 .<version>.installing
//  2. 单层顶层目录时提上来，写安装标记
//  3. 覆盖旧的同名版本后整体 rename 就位
func (p pkg) Install(service, version, archive string) (string, error) {
	ver, err := validateVersion(version)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(archive); err != nil || st.IsDir() {
		return "", fmt.Errorf("归档文件不可用: %s", archive)
	}
	root, err := p.PluginsDir(service)
	if err != nil {
		return "", err
	}

	staging := filepath.Join(root, "."+ver+".installing")
	_ = os.RemoveAll(staging) // 上次失败的残留
	if err := extractArchive(context.Background(), archive, staging, nil); err != nil {
		_ = os.RemoveAll(staging)
		return "", err
	}
	if err := flattenSingleRoot(staging); err != nil {
		_ = os.RemoveAll(staging)
		return "", fmt.Errorf("整理解包目录失败: %w", err)
	}
	mark := fmt.Sprintf("%s\nsource=%s\n", time.Now().Format(time.RFC3339), filepath.Base(archive))
	if err := writeFileAtomic(filepath.Join(staging, installedMarker), []byte(mark), 0o644); err != nil {
		_ = os.RemoveAll(staging)
		return "", fmt.Errorf("写安装标记失败: %w", err)
	}

	dst := filepath.Join(root, ver)
	if err := os.RemoveAll(dst); err != nil {
		_ = os.RemoveAll(staging)
		return "", fmt.Errorf("清理旧版本失败: %w", err)
	}
	if err := os.Rename(staging, dst); err != nil {
		_ = os.RemoveAll(staging)
		return "", fmt.Errorf("安装就位失败: %w", err)
	}
	applog.Infof("sysx", "安装 %s/%s 完成（来源 %s）", service, ver, filepath.Base(archive))
	return dst, nil
}

// Remove 删除一个安装版；发布内置版（无标记）拒绝删除。
func (p pkg) Remove(service, version string) error {
	ver, err := validateVersion(version)
	if err != nil {
		return err
	}
	root, err := p.PluginsDir(service)
	if err != nil {
		return err
	}
	dir := filepath.Join(root, ver)
	if !isInstalledDir(dir) {
		return fmt.Errorf("版本 %s 不是安装版（发布内置），不允许删除", ver)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("删除版本失败: %w", err)
	}
	applog.Infof("sysx", "删除版本 %s/%s", service, ver)
	return nil
}

// isInstalledDir 目录里是否有安装标记。
func isInstalledDir(dir string) bool {
	st, err := os.Stat(filepath.Join(dir, installedMarker))
	return err == nil && !st.IsDir()
}

// validateVersion 版本号会直接当目录名，必须挡住路径穿越与空值。
func validateVersion(version string) (string, error) {
	ver := strings.TrimSpace(version)
	if ver == "" {
		return "", fmt.Errorf("版本号不能为空")
	}
	if strings.ContainsAny(ver, `/\`) || strings.Contains(ver, "..") {
		return "", fmt.Errorf("版本号非法（不能含路径分隔符）: %s", version)
	}
	return ver, nil
}
