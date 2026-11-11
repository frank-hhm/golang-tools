package datastore

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"Tools/services/config"
	"Tools/tools/mysql"

	_ "github.com/go-sql-driver/mysql"
)

// mysqlTable 外存表名。写死，不给用户改：库名是唯一需要拼进 DDL 的标识符，
// 表名跟着可改就得多一道校验与一堆"改表名后旧数据在哪"的麻烦事。
const mysqlTable = "tools_data_store"

// mysqlSink 把文档副本写进本机某个 MySQL 实例的一张表。
//
// 连接是自己建的一份（mysql.DSN + sql.Open），不借 SQL 页的连接登记表：那边的 id 由
// 用户手建、生命周期跟着页面走，外存要是搭在那上面，用户关个连接页就把备份打断，
// 两边的错误也会互相串。
type mysqlSink struct {
	instID string
	dbName string

	mu     sync.Mutex
	db     *sql.DB
	colsAt time.Time // 上次确认表结构就绪的时间，用于限频重复 DDL
}

func newMySQLSink(instID, dbName string) *mysqlSink {
	return &mysqlSink{instID: instID, dbName: dbName}
}

func (s *mysqlSink) Kind() string { return "mysql" }

// Prepare 建连 + 建库建表。库名先过 config.StorageIdentOK 才敢拼进 DDL，
// 其余语句一律参数绑定。
func (s *mysqlSink) Prepare(ctx context.Context) error {
	if !config.StorageIdentOK(s.dbName) {
		return fmt.Errorf("MySQL 库名不合法（只允许字母数字下划线、不以数字开头、长度 1~64）：%q", s.dbName)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil && time.Since(s.colsAt) < 30*time.Second {
		return nil
	}
	cfg, err := mysql.InstanceConnConfig(s.instID)
	if err != nil {
		return err
	}
	db := s.db
	if db == nil {
		// 先连到服务器本身（不带库名），因为库还没建
		db, err = sql.Open("mysql", mysql.DSN(cfg, ""))
		if err != nil {
			return fmt.Errorf("连接 MySQL 失败: %w", err)
		}
		db.SetMaxOpenConns(2)
		db.SetConnMaxLifetime(10 * time.Minute)
	}
	pctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	if err := db.PingContext(pctx); err != nil {
		if s.db == nil {
			_ = db.Close()
		}
		return fmt.Errorf("MySQL 实例无响应（服务没启动还是端口不对？）: %w", err)
	}
	if _, err := db.ExecContext(pctx, "CREATE DATABASE IF NOT EXISTS `"+s.dbName+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_general_ci"); err != nil {
		if s.db == nil {
			_ = db.Close()
		}
		return fmt.Errorf("建库 %s 失败: %w", s.dbName, err)
	}
	// 连到目标库上再建表：DDL 里的库名已经校验过，这里换成"当前库"就好，之后所有
	// 读写语句都不必再带库名前缀。
	if _, err := db.ExecContext(pctx, "USE `"+s.dbName+"`"); err != nil {
		if s.db == nil {
			_ = db.Close()
		}
		return fmt.Errorf("切到库 %s 失败: %w", s.dbName, err)
	}
	if _, err := db.ExecContext(pctx, createStoreTableSQL); err != nil {
		if s.db == nil {
			_ = db.Close()
		}
		return fmt.Errorf("建表 %s.%s 失败: %w", s.dbName, mysqlTable, err)
	}
	if s.db == nil {
		s.db = db
	}
	s.colsAt = time.Now()
	return nil
}

// createStoreTableSQL 表结构（计划文档第六节）。digest 存的是本地明文摘要，
// 加密文档也能靠它判断本地与远端是否一致。
const createStoreTableSQL = `CREATE TABLE IF NOT EXISTS ` + mysqlTable + ` (
	data_key    VARCHAR(191) NOT NULL PRIMARY KEY,
	payload     LONGBLOB     NOT NULL,
	digest      CHAR(64)     NOT NULL,
	size_bytes  BIGINT       NOT NULL,
	encrypted   TINYINT      NOT NULL DEFAULT 0,
	synced_at   DATETIME(3)  NOT NULL,
	updated_at  TIMESTAMP    NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`

func (s *mysqlSink) Put(ctx context.Context, key string, payload []byte, meta DocMeta) error {
	if err := s.Prepare(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return fmt.Errorf("MySQL 外存尚未就绪")
	}
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	_, err := db.ExecContext(pctx,
		"INSERT INTO "+mysqlTable+" (data_key, payload, digest, size_bytes, encrypted, synced_at)"+
			" VALUES (?, ?, ?, ?, ?, ?)"+
			" ON DUPLICATE KEY UPDATE payload=VALUES(payload), digest=VALUES(digest),"+
			" size_bytes=VALUES(size_bytes), encrypted=VALUES(encrypted), synced_at=VALUES(synced_at)",
		key, payload, meta.Digest, meta.Size, boolToTiny(meta.Encrypted), time.UnixMilli(meta.SyncedAt))
	if err != nil {
		return fmt.Errorf("写入 %s 失败: %w", key, err)
	}
	return nil
}

func (s *mysqlSink) Fetch(ctx context.Context, key string) ([]byte, *DocMeta, error) {
	if err := s.Prepare(ctx); err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return nil, nil, fmt.Errorf("MySQL 外存尚未就绪")
	}
	pctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	var (
		payload []byte
		digest  string
		size    int64
		enc     int
		syncAt  time.Time
	)
	row := db.QueryRowContext(pctx,
		"SELECT payload, digest, size_bytes, encrypted, synced_at FROM "+mysqlTable+" WHERE data_key = ?", key)
	switch err := row.Scan(&payload, &digest, &size, &enc, &syncAt); err {
	case nil:
		return payload, &DocMeta{Digest: digest, Size: size, Encrypted: enc != 0, SyncedAt: syncAt.UnixMilli()}, nil
	case sql.ErrNoRows:
		return nil, nil, nil
	default:
		return nil, nil, fmt.Errorf("读取 %s 失败: %w", key, err)
	}
}

func (s *mysqlSink) Metas(ctx context.Context) (map[string]DocMeta, error) {
	if err := s.Prepare(ctx); err != nil {
		return nil, err
	}
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return nil, fmt.Errorf("MySQL 外存尚未就绪")
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	rows, err := db.QueryContext(pctx, "SELECT data_key, digest, size_bytes, encrypted, synced_at FROM "+mysqlTable)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]DocMeta{}
	for rows.Next() {
		var (
			key    string
			digest string
			size   int64
			enc    int
			syncAt time.Time
		)
		if err := rows.Scan(&key, &digest, &size, &enc, &syncAt); err != nil {
			continue
		}
		out[key] = DocMeta{Digest: digest, Size: size, Encrypted: enc != 0, SyncedAt: syncAt.UnixMilli()}
	}
	return out, rows.Err()
}

// ListDatabases 列出该实例上已有的库名，给设置页的下拉用。
func (s *mysqlSink) ListDatabases(ctx context.Context) ([]string, error) {
	cfg, err := mysql.InstanceConnConfig(s.instID)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", mysql.DSN(cfg, ""))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	pctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	rows, err := db.QueryContext(pctx, "SELECT SCHEMA_NAME FROM information_schema.SCHEMATA ORDER BY SCHEMA_NAME")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			out = append(out, name)
		}
	}
	return out, nil
}

// CountDocs 数一下外存表里已有几份文档，只给设置页的「测试连接」回报一句人话。
func (s *mysqlSink) CountDocs(ctx context.Context) (int64, error) {
	if err := s.Prepare(ctx); err != nil {
		return 0, err
	}
	s.mu.Lock()
	db := s.db
	s.mu.Unlock()
	if db == nil {
		return 0, fmt.Errorf("MySQL 外存尚未就绪")
	}
	pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var n int64
	if err := db.QueryRowContext(pctx, "SELECT COUNT(*) FROM "+mysqlTable).Scan(&n); err != nil {
		return 0, fmt.Errorf("统计已有副本失败: %w", err)
	}
	return n, nil
}

func (s *mysqlSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db != nil {
		err := s.db.Close()
		s.db = nil
		s.colsAt = time.Time{}
		return err
	}
	return nil
}

func boolToTiny(b bool) int {
	if b {
		return 1
	}
	return 0
}
