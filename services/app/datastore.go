package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"Tools/appdirs"
	"Tools/services/applog"
	"Tools/services/config"
	"Tools/services/datastore"
)

/*
数据保存位置（本地 / MySQL / Redis 外存副本）绑定层

引擎在 services/datastore：本地文件永远是权威可读源，MySQL/Redis 是"再写一份持久副本"，
变更靠扫描 mtime+size+sha256 发现，不要求任何存量 store 改自己的写路径。

不加 build tag：MySQL 驱动与 RESP 客户端都是纯 Go，Linux 面板同样能开外存；
平台差异（如果有）留在引擎内部，不藏在这一层——藏起来前端就再也调不到这些方法了。
*/

// StorageSettings 设置页读写的那一份保存位置设置。
//
// 不直接把 config.StorageConfig 当参数类型：那段里存的就是绑定信息本身，前端来回传
// 一整份容易把没渲染的字段（比如 exclude）顺手清空，所以这里逐字段落回 config，
// 而不是整段替换。
type StorageSettings struct {
	Local            bool     `json:"local"`
	MySQL            bool     `json:"mysql"`
	Redis            bool     `json:"redis"`
	MySQLInstance    string   `json:"mysqlInstance"`
	MySQLDatabase    string   `json:"mysqlDatabase"`
	RedisInstance    string   `json:"redisInstance"`
	RedisDB          int      `json:"redisDb"`
	IncludeSensitive bool     `json:"includeSensitive"`
	Exclude          []string `json:"exclude"`
	IntervalSec      int      `json:"intervalSec"`
}

// DataStoreStatus 当前保存位置全貌（主页提示条与设置页共用这一份）。
func (a *App) DataStoreStatus() datastore.View {
	return datastore.Snapshot()
}

// DataStoreSettings 回填设置页控件。老配置没有 storage 段时给默认值（只勾本地）。
func (a *App) DataStoreSettings() StorageSettings {
	return toStorageSettings(config.StorageCfg())
}

// DataStoreListInstances 列出某个位置可选的本机服务实例（下拉里带运行状态）。
// kind 取 "mysql" | "redis"。返回体不含任何凭据字段。
func (a *App) DataStoreListInstances(kind string) ([]datastore.InstanceItem, error) {
	return datastore.ListInstances(kind)
}

// DataStoreListDBOptions 列出库选择器的候选项：MySQL 是实例已有的库，Redis 是 0~15 序号。
// 实例没起来时 MySQL 这一项会返回错误，前端据此提示「先启动实例再选库」，同时保留手填库名。
func (a *App) DataStoreListDBOptions(kind, instanceID string) ([]datastore.DBOption, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	return datastore.ListDBOptions(ctx, kind, strings.TrimSpace(instanceID))
}

// DataStoreTestSink 按当前已保存的绑定试连一次，回报一句人话。
// 只试不改：不动引擎里正在用的连接与重试时钟，用户看着按钮试就行。
func (a *App) DataStoreTestSink(kind string) (string, error) {
	cfg := config.StorageCfg()
	s := toStorageSettings(cfg)
	switch kind {
	case "mysql":
		if !s.MySQL || s.MySQLInstance == "" {
			return "", fmt.Errorf("还没有勾选并绑定 MySQL 保存位置")
		}
		return datastore.SinkInfo(ctxBackground(), "mysql", s.MySQLInstance, s.MySQLDatabase, 0)
	case "redis":
		if !s.Redis || s.RedisInstance == "" {
			return "", fmt.Errorf("还没有勾选并绑定 Redis 保存位置")
		}
		return datastore.SinkInfo(ctxBackground(), "redis", s.RedisInstance, "", s.RedisDB)
	default:
		return "", fmt.Errorf("未知保存位置类型: %s", kind)
	}
}

// DataStoreSaveSettings 保存保存位置设置并立即重建外存连接，随后后台补一轮同步。
//
// 库名先校验再落盘：这是唯一会被拼进 DDL 的用户输入，等真去建表才发现不合法，
// 用户看到的就只是一句莫名的连接错误。
func (a *App) DataStoreSaveSettings(in StorageSettings) (string, error) {
	mysqlDB := strings.TrimSpace(in.MySQLDatabase)
	if mysqlDB == "" {
		mysqlDB = config.DefaultStorageDatabase
	}
	if in.MySQL && !config.StorageIdentOK(mysqlDB) {
		return "", fmt.Errorf("MySQL 库名不合法（只允许字母、数字、下划线，不能以数字开头）：%q", mysqlDB)
	}
	redisInst := strings.TrimSpace(in.RedisInstance)
	mysqlInst := strings.TrimSpace(in.MySQLInstance)
	if in.MySQL && mysqlInst == "" {
		return "", fmt.Errorf("勾选了 MySQL 但还没选择实例")
	}
	if in.Redis && redisInst == "" {
		return "", fmt.Errorf("勾选了 Redis 但还没选择实例")
	}
	if !in.Local && !in.MySQL && !in.Redis {
		return "", fmt.Errorf("至少要选一个保存位置")
	}

	cfg, err := config.Update(func(c *config.AppConfig) {
		if c.Storage == nil {
			c.Storage = &config.StorageConfig{}
		}
		c.Storage.Local = in.Local
		c.Storage.MySQL = in.MySQL
		c.Storage.Redis = in.Redis
		c.Storage.MySQLInstance = mysqlInst
		c.Storage.MySQLDatabase = mysqlDB
		c.Storage.RedisInstance = redisInst
		c.Storage.RedisDB = in.RedisDB
		c.Storage.IncludeSensitive = in.IncludeSensitive
		c.Storage.Exclude = in.Exclude
		c.Storage.IntervalSec = in.IntervalSec
	})
	if err != nil {
		applog.Errorf("datastore", "保存数据保存位置设置失败: %v", err)
		return "", err
	}
	next := config.StorageCfg()
	datastore.Configure(next)
	// 保存本身不做网络等待：绑定刚改完，队列里可能还压着旧实例没写完的份，
	// 补同步放后台，结果靠 datastore:status 事件与状态刷新回报给用户。
	go datastore.SyncNow()
	applog.Infof("datastore", "保存位置已更新：本地=%v MySQL=%v(%s/%s) Redis=%v(%s/db%d) 含凭据=%v",
		next.Local, next.MySQL, next.MySQLInstance, next.MySQLDatabase,
		next.Redis, next.RedisInstance, next.RedisDB, next.IncludeSensitive)
	return cfg, nil
}

// DataStoreListDocs 受管文档清单（含没被纳入的那些，附"为什么不外存"的人话原因）。
func (a *App) DataStoreListDocs() ([]datastore.DocView, error) {
	v := datastore.Snapshot()
	return v.Docs, nil
}

// DataStoreSyncNow 立即把当前所有应外存的文档强制写一遍远端（设置页按钮）。
func (a *App) DataStoreSyncNow() error {
	if err := datastore.SyncNow(); err != nil {
		applog.Warnf("datastore", "手动同步未完成: %v", err)
		return err
	}
	applog.Infof("datastore", "手动同步完成")
	return nil
}

// DataStoreRestore 把远端副本拉回本地，返回实际覆盖的份数。
// 这里是用户主动点的，所以不看本地勾选——和启动那一次"远端优先"是两回事。
func (a *App) DataStoreRestore() (int, error) {
	n, err := datastore.Restore()
	if err != nil {
		applog.Warnf("datastore", "从远端恢复失败: %v", err)
		return n, err
	}
	applog.Infof("datastore", "已从远端恢复 %d 份文档", n)
	// 覆盖的是磁盘上的文件，正在跑的各服务不会自动重读；广播一次让相关页面自己刷新，
	// 具体要不要重启服务由用户决定（设置页文案里写清楚了）。
	a.emit("datastore:restored", n)
	return n, nil
}

// DataStoreStartBound 启动这个位置绑定的服务实例（主页提示条上的「启动」按钮）。
func (a *App) DataStoreStartBound(kind string) error {
	if err := datastore.StartBound(kind); err != nil {
		applog.Warnf("datastore", "启动 %s 绑定实例失败: %v", kind, err)
		return err
	}
	applog.Infof("datastore", "已请求启动 %s 绑定实例", kind)
	return nil
}

// DataStoreSetLocalOverride 记下主页弹窗里选的"本次使用默认数据"。只影响当前进程，
// 不改设置里的勾选：下次启动还得被提醒一次，而不是被自己的一次临时选择永久改掉配置。
func (a *App) DataStoreSetLocalOverride(useLocal bool) {
	datastore.SetSessionLocal(useLocal)
}

// DataStoreOpenLocalDir 在系统文件管理器里打开本地数据目录——用户问"到底存在哪儿"，
// 这是最实在的一个出口（远端那两侧靠设置页的实例信息与「测试连接」看）。
func (a *App) DataStoreOpenLocalDir() error {
	return a.DiskOpenInExplorer(appdirs.DataBase())
}

func toStorageSettings(c config.StorageConfig) StorageSettings {
	return StorageSettings{
		Local:            c.Local,
		MySQL:            c.MySQL,
		Redis:            c.Redis,
		MySQLInstance:    c.MySQLInstance,
		MySQLDatabase:    c.MySQLDatabase,
		RedisInstance:    c.RedisInstance,
		RedisDB:          c.RedisDB,
		IncludeSensitive: c.IncludeSensitive,
		Exclude:          c.Exclude,
		IntervalSec:      c.IntervalSec,
	}
}

// ctxBackground 试连接这类一次性动作的上下文：超时由被调方自己按最坏情况兜（几秒），
// 这里只负责给个非 nil 的起点，不在绑定层再抄一份时限。
func ctxBackground() context.Context {
	return context.Background()
}

// emit 向前端广播一个事件；应用还没建好窗口时静默忽略（启动早期调到了也不该 panic）。
func (a *App) emit(name string, data any) {
	if a.app == nil {
		return
	}
	a.app.Event.Emit(name, data)
}
