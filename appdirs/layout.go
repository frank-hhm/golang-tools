package appdirs

// ============================================================
// 全局目录布局（plugins / data / logs + 服务名子目录）
//
//   - 首次启动（无任何配置）与每次保存设置时：应用 paths 配置 →
//     迁移旧版布局 → 确保 <基础目录>/<服务名> 标准布局存在。
//   - 服务子目录按实际消费方命名（php/nginx/mysql/redis/npm…），
//     data 下另含 www/screenshots/videos 共享目录，logs 下含 app（程序日志）。
//   - 设置页修改基础目录并保存后，各服务的新配置/新文件即落新目录
//     （路径每次经本包实时解析，无需重启）。
//
// 本文件由根目录 paths.go 归并而来：目录的"解析"与"布局落建"同属一个职责，
// 放在一起可避免根包与各服务包对目录常量的重复约定。
// ============================================================

import (
	"os"
	"path/filepath"
	"strings"
)

// 各基础目录下按实际服务预建的子目录
var (
	pluginServices = []string{"php", "nginx", "mysql", "redis", "npm"}
	dataServices   = []string{"php", "nginx", "mysql", "redis", "npm", "term", "www", "screenshots", "videos"}
	logServices    = []string{"app", "php", "nginx", "mysql", "redis", "npm"}
)

// ApplyLayout 应用基础目录配置并确保标准布局存在（启动与保存配置时调用，幂等）。
// 空值走默认 <应用目录>/plugins|data|logs|downloads。
func ApplyLayout(plugins, data, logs, downloads string) error {
	if err := Set(plugins, data, logs); err != nil {
		return err
	}
	if err := SetDownloads(downloads); err != nil {
		return err
	}
	migrateLegacy()
	ensureLayout()
	return nil
}

// ensureLayout 预建 <基础目录>/<服务名> 各级子目录与下载根目录（MkdirAll 幂等）
func ensureLayout() {
	for _, s := range pluginServices {
		_, _ = Plugins(s)
	}
	for _, s := range dataServices {
		_, _ = Data(s)
	}
	for _, s := range logServices {
		_, _ = Logs(s)
	}
	_, _ = Downloads()
}

// migrateLegacy 旧版布局一次性迁移（MoveIfAbsent 幂等，迁移过即空转）：
//   - %AppData%/Tools 下的服务登记/实例目录（php/npm/mysql/redis 旧数据家）→ data/<服务名>/
//   - <应用目录>/data 根下的 nginx 登记与实例目录 → data/nginx/
//   - <日志根>/*.log 散文件 → logs/app/；mysql-sql-*.log → logs/mysql/
func migrateLegacy() {
	dataRoot := DataBase()

	// 旧数据家：用户配置目录 Tools
	if base, err := os.UserConfigDir(); err == nil {
		old := filepath.Join(base, "Tools")
		for _, svc := range []string{"php", "nginx", "mysql", "redis", "npm"} {
			for _, name := range []string{svc + "-locals.json", svc + "-instances.json", svc + "-instances"} {
				_ = MoveIfAbsent(filepath.Join(old, name), filepath.Join(dataRoot, svc, name))
			}
		}
		for _, name := range []string{"npmrc", "npm-swaps.json"} {
			_ = MoveIfAbsent(filepath.Join(old, name), filepath.Join(dataRoot, "npm", name))
		}
		// redis 旧版下载缓存（含 locals.json 登记表）
		_ = MoveIfAbsent(filepath.Join(old, "redis-servers"), filepath.Join(dataRoot, "redis", "redis-servers"))
	}

	// nginx 旧版数据直接放 <应用目录>/data 根下
	oldData := filepath.Join(DefaultRoot(), "data")
	for _, name := range []string{"nginx-locals.json", "nginx-instances.json", "nginx-instances"} {
		_ = MoveIfAbsent(filepath.Join(oldData, name), filepath.Join(dataRoot, "nginx", name))
	}

	// 程序日志与 SQL 日志此前散放 <日志根> 下，现按服务名归置
	oldLogs := filepath.Join(DefaultRoot(), "logs")
	if entries, err := os.ReadDir(oldLogs); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".log") {
				continue
			}
			svc := "app"
			if strings.HasPrefix(e.Name(), "mysql-sql-") {
				svc = "mysql"
			}
			sub, err := Logs(svc)
			if err != nil {
				continue
			}
			_ = MoveIfAbsent(filepath.Join(oldLogs, e.Name()), filepath.Join(sub, e.Name()))
		}
	}
}
