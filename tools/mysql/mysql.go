package mysql

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "github.com/go-sql-driver/mysql"
)

/* ============================ 数据结构 ============================ */

// ConnConfig MySQL 连接配置。
type ConnConfig struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"`
}

// TableInfo 表信息。
type TableInfo struct {
	Name       string `json:"name"`
	Engine     string `json:"engine"`
	Comment    string `json:"comment"`
	Rows       int64  `json:"rows"`
	DataLength int64  `json:"dataLength"`
	CreateTime string `json:"createTime"`
	UpdateTime string `json:"updateTime"`
	Collation  string `json:"collation"`
}

// ViewInfo 视图信息。
type ViewInfo struct {
	Name    string `json:"name"`
	Comment string `json:"comment"`
}

// RoutineInfo 函数/存储过程信息。
type RoutineInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"` // FUNCTION / PROCEDURE
	ReturnType string `json:"returnType"`
	Comment    string `json:"comment"`
	Definition string `json:"definition"`
}

// ColumnInfo 列信息。
type ColumnInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Key      string `json:"key"`
	Default  string `json:"default"`
	Extra    string `json:"extra"`
	Comment  string `json:"comment"`
	Position int    `json:"position"`
}

// IndexInfo 索引信息。
type IndexInfo struct {
	Name    string   `json:"name"`
	Unique  bool     `json:"unique"`
	Primary bool     `json:"primary"`
	Type    string   `json:"type"`
	Columns []string `json:"columns"`
	Comment string   `json:"comment"`
}

// TableSchema 表结构（列 + 索引 + DDL）。
type TableSchema struct {
	Columns []ColumnInfo `json:"columns"`
	Indexes []IndexInfo  `json:"indexes"`
	DDL     string       `json:"ddl"`
	PK      string       `json:"pk"` // 单主键列名（用于行定位）
}

// ColumnDef 列定义（用于新增/修改列）。
type ColumnDef struct {
	Name          string `json:"name"`
	Type          string `json:"type"` // 如 varchar(255)、int、datetime
	Nullable      bool   `json:"nullable"`
	Default       string `json:"default"`       // 默认值原文（空表示无默认）
	AutoIncrement bool   `json:"autoIncrement"` // 自增
	PrimaryKey    bool   `json:"primaryKey"`    // 主键
	Comment       string `json:"comment"`
	After         string `json:"after"` // 新增时定位到某列之后
}

// QueryResult 通用查询/执行结果。
type QueryResult struct {
	Columns  []string         `json:"columns"`
	Rows     []map[string]any `json:"rows"`
	Affected int64            `json:"affected"`
	LastID   int64            `json:"lastID"`
	Elapsed  int64            `json:"elapsed"` // 毫秒
	Note     string           `json:"note"`
}

/* ============================ 连接管理 ============================ */

var (
	mu    sync.Mutex
	conns = map[string]*sql.DB{}  // id -> DB
	txs   = map[string]*txEntry{} // txID -> tx
	txSeq int64
)

type txEntry struct {
	db       *sql.DB
	tx       *sql.Tx
	connID   string
	database string
}

func dsnOf(cfg ConnConfig, database string) string {
	host := cfg.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := cfg.Port
	if port <= 0 {
		port = 3306
	}
	user := cfg.User
	if user == "" {
		user = "root"
	}
	// 允许多语句、解析时间、超时
	return fmt.Sprintf("%s:%s@tcp(%s)/%s?parseTime=true&charset=utf8mb4&timeout=6s&readTimeout=30s&writeTimeout=30s&multiStatements=true",
		user, cfg.Password, net.JoinHostPort(host, strconv.Itoa(port)), database)
}

// Open 建立并登记一个连接（同 id 先关闭旧连接）。
func Open(cfg ConnConfig) error {
	if cfg.ID == "" {
		return fmt.Errorf("连接 ID 不能为空")
	}
	db, err := sql.Open("mysql", dsnOf(cfg, cfg.Database))
	if err != nil {
		return err
	}
	db.SetMaxOpenConns(5)
	db.SetConnMaxLifetime(time.Hour)
	if err := db.Ping(); err != nil {
		db.Close()
		return fmt.Errorf("连接失败: %w", err)
	}
	mu.Lock()
	if old, ok := conns[cfg.ID]; ok {
		old.Close()
	}
	conns[cfg.ID] = db
	mu.Unlock()
	return nil
}

// Close 关闭并移除连接。
func Close(id string) error {
	mu.Lock()
	defer mu.Unlock()
	if db, ok := conns[id]; ok {
		db.Close()
		delete(conns, id)
	}
	return nil
}

func getDB(id string) (*sql.DB, error) {
	mu.Lock()
	defer mu.Unlock()
	db, ok := conns[id]
	if !ok {
		return nil, fmt.Errorf("连接未打开")
	}
	return db, nil
}

/* ============================ 结构列表 ============================ */

// ListDatabases 列出所有数据库。
func ListDatabases(id string) ([]string, error) {
	db, err := getDB(id)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query("SELECT SCHEMA_NAME FROM information_schema.SCHEMATA ORDER BY SCHEMA_NAME")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err == nil {
			out = append(out, s)
		}
	}
	return out, nil
}

// ListTables 列出指定库的基表。
func ListTables(id, database string) ([]TableInfo, error) {
	db, err := getDB(id)
	if err != nil {
		return nil, err
	}
	q := `SELECT TABLE_NAME, IFNULL(ENGINE,''), IFNULL(TABLE_COMMENT,''), IFNULL(TABLE_ROWS,0),
	      IFNULL(DATA_LENGTH,0), IFNULL(DATE_FORMAT(CREATE_TIME,'%Y-%m-%d %H:%i:%s'),''),
	      IFNULL(DATE_FORMAT(UPDATE_TIME,'%Y-%m-%d %H:%i:%s'),''), IFNULL(TABLE_COLLATION,'')
	      FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_TYPE='BASE TABLE' ORDER BY TABLE_NAME`
	rows, err := db.Query(q, database)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TableInfo{}
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.Name, &t.Engine, &t.Comment, &t.Rows, &t.DataLength, &t.CreateTime, &t.UpdateTime, &t.Collation); err == nil {
			out = append(out, t)
		}
	}
	return out, nil
}

// ListViews 列出指定库的视图。
func ListViews(id, database string) ([]ViewInfo, error) {
	db, err := getDB(id)
	if err != nil {
		return nil, err
	}
	// 视图注释取自 information_schema.TABLES（VIEWS 表无 TABLE_COMMENT 列）
	rows, err := db.Query(`SELECT TABLE_NAME, IFNULL(TABLE_COMMENT,'') FROM information_schema.TABLES WHERE TABLE_SCHEMA=? AND TABLE_TYPE='VIEW' ORDER BY TABLE_NAME`, database)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ViewInfo{}
	for rows.Next() {
		var v ViewInfo
		if err := rows.Scan(&v.Name, &v.Comment); err == nil {
			out = append(out, v)
		}
	}
	return out, nil
}

// ListRoutines 列出指定库的函数与存储过程。
func ListRoutines(id, database string) ([]RoutineInfo, error) {
	db, err := getDB(id)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT ROUTINE_NAME, ROUTINE_TYPE, IFNULL(DATA_TYPE,''), IFNULL(ROUTINE_COMMENT,''), IFNULL(ROUTINE_DEFINITION,'')
	      FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA=? ORDER BY ROUTINE_TYPE, ROUTINE_NAME`, database)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RoutineInfo{}
	for rows.Next() {
		var r RoutineInfo
		if err := rows.Scan(&r.Name, &r.Type, &r.ReturnType, &r.Comment, &r.Definition); err == nil {
			out = append(out, r)
		}
	}
	return out, nil
}

/* ============================ 表结构 ============================ */

// GetTableSchema 返回表的列、索引、DDL 与主键。
func GetTableSchema(id, database, table string) (TableSchema, error) {
	ts := TableSchema{}
	db, err := getDB(id)
	if err != nil {
		return ts, err
	}
	// 列
	crows, err := db.Query(`SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_KEY, IFNULL(COLUMN_DEFAULT,''), EXTRA, COLUMN_COMMENT, ORDINAL_POSITION
	      FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=? AND TABLE_NAME=? ORDER BY ORDINAL_POSITION`, database, table)
	if err != nil {
		return ts, err
	}
	defer crows.Close()
	for crows.Next() {
		var c ColumnInfo
		var nullable, pos string
		if err := crows.Scan(&c.Name, &c.Type, &nullable, &c.Key, &c.Default, &c.Extra, &c.Comment, &pos); err == nil {
			c.Nullable = nullable == "YES"
			c.Position, _ = strconv.Atoi(pos)
			ts.Columns = append(ts.Columns, c)
			if c.Key == "PRI" && ts.PK == "" {
				ts.PK = c.Name
			}
		}
	}
	// 索引
	irows, err := db.Query(`SELECT INDEX_NAME, NON_UNIQUE, SEQ_IN_INDEX, COLUMN_NAME, INDEX_TYPE, IFNULL(INDEX_COMMENT,'')
	      FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=? AND TABLE_NAME=? ORDER BY INDEX_NAME, SEQ_IN_INDEX`, database, table)
	if err != nil {
		return ts, err
	}
	defer irows.Close()
	idxMap := map[string]*IndexInfo{}
	order := []string{}
	for irows.Next() {
		var name string
		var nonUnique int
		var seq int
		var col, itype, comment string
		if err := irows.Scan(&name, &nonUnique, &seq, &col, &itype, &comment); err != nil {
			continue
		}
		ii, ok := idxMap[name]
		if !ok {
			ii = &IndexInfo{Name: name, Unique: nonUnique == 0, Primary: name == "PRIMARY", Type: itype, Comment: comment}
			idxMap[name] = ii
			order = append(order, name)
		}
		ii.Columns = append(ii.Columns, col)
	}
	for _, n := range order {
		ts.Indexes = append(ts.Indexes, *idxMap[n])
	}
	// DDL
	var tname, ddl string
	if err := db.QueryRow("SHOW CREATE TABLE "+qual(database, table)).Scan(&tname, &ddl); err == nil {
		ts.DDL = ddl
	}
	return ts, nil
}

/* ============================ 列设计（增/改/删） ============================ */

// AddColumn 向表新增一列。
func AddColumn(id, database, table string, col ColumnDef) error {
	db, err := getDB(id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(col.Name) == "" || strings.TrimSpace(col.Type) == "" {
		return fmt.Errorf("列名与类型不能为空")
	}
	sqlText := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s%s", qual(database, table), colDefSQL("", col), afterClause(col.After))
	_, err = execLogged(db, id, database, sqlText)
	return err
}

// ModifyColumn 修改表中的列（支持重命名：col.Name 与 oldName 不同时用 CHANGE）。
func ModifyColumn(id, database, table, oldName string, col ColumnDef) error {
	db, err := getDB(id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(oldName) == "" || strings.TrimSpace(col.Type) == "" {
		return fmt.Errorf("列名与类型不能为空")
	}
	def := colDefSQL(oldName, col)
	var sqlText string
	if col.Name != "" && col.Name != oldName {
		sqlText = fmt.Sprintf("ALTER TABLE %s CHANGE COLUMN %s %s%s", qual(database, table), qid(oldName), def, afterClause(col.After))
	} else {
		sqlText = fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s", qual(database, table), def)
	}
	_, err = execLogged(db, id, database, sqlText)
	return err
}

// DropColumn 删除表中的一列。
func DropColumn(id, database, table, col string) error {
	db, err := getDB(id)
	if err != nil {
		return err
	}
	if strings.TrimSpace(col) == "" {
		return fmt.Errorf("列名不能为空")
	}
	_, err = execLogged(db, id, database, "ALTER TABLE "+qual(database, table)+" DROP COLUMN "+qid(col))
	return err
}

// colDefSQL 拼装列定义片段（不含 ADD/MODIFY 关键字）。
func colDefSQL(oldName string, col ColumnDef) string {
	name := col.Name
	if name == "" {
		name = oldName
	}
	var b strings.Builder
	b.WriteString(qid(name) + " " + strings.TrimSpace(col.Type))
	if col.Nullable && !col.AutoIncrement {
		b.WriteString(" NULL")
	} else {
		b.WriteString(" NOT NULL")
	}
	d := strings.TrimSpace(col.Default)
	if d != "" && !col.AutoIncrement {
		up := strings.ToUpper(d)
		switch {
		case up == "NULL":
			b.WriteString(" DEFAULT NULL")
		case strings.HasPrefix(up, "CURRENT_TIMESTAMP"):
			b.WriteString(" DEFAULT " + up)
		case isNumericType(col.Type) && isNumericStr(d):
			b.WriteString(" DEFAULT " + d)
		default:
			b.WriteString(" DEFAULT " + quoteStr(d))
		}
	}
	if col.AutoIncrement {
		b.WriteString(" AUTO_INCREMENT")
	}
	if c := strings.TrimSpace(col.Comment); c != "" {
		b.WriteString(" COMMENT " + quoteStr(c))
	}
	if col.PrimaryKey {
		b.WriteString(" PRIMARY KEY")
	}
	return b.String()
}

func afterClause(after string) string {
	if strings.TrimSpace(after) == "" {
		return ""
	}
	return " AFTER " + qid(after)
}

func quoteStr(s string) string {
	return "'" + strings.NewReplacer("\\", "\\\\", "'", "\\'").Replace(s) + "'"
}

func isNumericType(t string) bool {
	lt := strings.ToLower(t)
	for _, k := range []string{"int", "decimal", "numeric", "float", "double", "real", "bit"} {
		if strings.Contains(lt, k) {
			return true
		}
	}
	return false
}

func isNumericStr(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil
}

/* ============================ 数据浏览与 CRUD ============================ */

// TableData 分页浏览表数据，支持过滤（WHERE 片段）与排序。
func TableData(id, database, table, filter, orderBy string, desc bool, page, size int) (QueryResult, error) {
	res := QueryResult{}
	db, err := getDB(id)
	if err != nil {
		return res, err
	}
	if page < 1 {
		page = 1
	}
	if size <= 0 || size > 1000 {
		size = 50
	}
	where := ""
	if strings.TrimSpace(filter) != "" {
		where = " WHERE " + strings.TrimSpace(filter)
	}
	var total int64
	_ = db.QueryRow("SELECT COUNT(*) FROM " + qual(database, table) + where).Scan(&total)

	order := ""
	if strings.TrimSpace(orderBy) != "" {
		dir := "ASC"
		if desc {
			dir = "DESC"
		}
		order = " ORDER BY " + qid(orderBy) + " " + dir
	}
	q := "SELECT * FROM " + qual(database, table) + where + order + " LIMIT " + strconv.Itoa(size) + " OFFSET " + strconv.Itoa((page-1)*size)
	start := time.Now()
	rows, err := db.Query(q)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	if err := scanRows(rows, &res); err != nil {
		return res, err
	}
	res.Affected = total
	res.Note = fmt.Sprintf("共 %d 行", total)
	res.Elapsed = time.Since(start).Milliseconds()
	return res, nil
}

// UpdateCell 依据主键更新单个单元格。
func UpdateCell(id, database, table, pkCol, pkVal, col, val string) error {
	db, err := getDB(id)
	if err != nil {
		return err
	}
	if pkCol == "" {
		return fmt.Errorf("该表无单列主键，无法定位行")
	}
	var arg any = val
	if val == "\x00NULL" {
		arg = nil
	}
	sqlText := "UPDATE " + qual(database, table) + " SET " + qid(col) + "=? WHERE " + qid(pkCol) + "=? LIMIT 1"
	_, err = execLogged(db, id, database, sqlText, arg, pkVal)
	return err
}

// InsertRow 依据列值映射插入一行。
func InsertRow(id, database, table string, data map[string]string) (int64, error) {
	db, err := getDB(id)
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, fmt.Errorf("无数据")
	}
	cols := []string{}
	ph := []string{}
	args := []any{}
	for k, v := range data {
		cols = append(cols, qid(k))
		ph = append(ph, "?")
		if v == "\x00NULL" {
			args = append(args, nil)
		} else {
			args = append(args, v)
		}
	}
	q := "INSERT INTO " + qual(database, table) + " (" + strings.Join(cols, ",") + ") VALUES (" + strings.Join(ph, ",") + ")"
	r, err := execLogged(db, id, database, q, args...)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

// DeleteRow 依据主键删除一行。
func DeleteRow(id, database, table, pkCol, pkVal string) error {
	db, err := getDB(id)
	if err != nil {
		return err
	}
	if pkCol == "" {
		return fmt.Errorf("该表无单列主键，无法定位行")
	}
	_, err = execLogged(db, id, database, "DELETE FROM "+qual(database, table)+" WHERE "+qid(pkCol)+"=? LIMIT 1", pkVal)
	return err
}

// DeleteRows 依据单主键批量删除多行，返回受影响行数。
func DeleteRows(id, database, table, pkCol string, pkVals []string) (int64, error) {
	db, err := getDB(id)
	if err != nil {
		return 0, err
	}
	if pkCol == "" {
		return 0, fmt.Errorf("该表无单列主键，无法定位行")
	}
	if len(pkVals) == 0 {
		return 0, fmt.Errorf("未选择要删除的行")
	}
	ph := make([]string, len(pkVals))
	args := make([]any, len(pkVals))
	for i, v := range pkVals {
		ph[i] = "?"
		args[i] = v
	}
	sqlText := "DELETE FROM " + qual(database, table) + " WHERE " + qid(pkCol) + " IN (" + strings.Join(ph, ",") + ")"
	r, err := execLogged(db, id, database, sqlText, args...)
	if err != nil {
		return 0, err
	}
	return r.RowsAffected()
}

/* ============================ SQL 执行 ============================ */

// Execute 执行一条语句：查询类返回结果集，其余返回受影响行。
func Execute(id, database, sqlText string) (QueryResult, error) {
	res := QueryResult{}
	db, err := getDB(id)
	if err != nil {
		return res, err
	}
	sqlText = strings.TrimSpace(strings.TrimRight(sqlText, ";"))
	if sqlText == "" {
		return res, fmt.Errorf("空语句")
	}
	// 若指定了库，先切换
	if database != "" {
		if _, err := db.Exec("USE " + qid(database)); err != nil {
			return res, err
		}
	}
	start := time.Now()
	head := strings.ToUpper(firstWord(sqlText))
	switch head {
	case "SELECT", "SHOW", "DESC", "DESCRIBE", "EXPLAIN", "WITH":
		rows, err := db.Query(sqlText)
		if err != nil {
			logSQL(id, database, sqlText, time.Since(start), 0, "", err)
			return res, err
		}
		defer rows.Close()
		if err := scanRows(rows, &res); err != nil {
			logSQL(id, database, sqlText, time.Since(start), 0, "", err)
			return res, err
		}
		res.Note = fmt.Sprintf("%d 行", len(res.Rows))
		logSQL(id, database, sqlText, time.Since(start), 0, res.Note, nil)
	default:
		r, err := execLogged(db, id, database, sqlText)
		if err != nil {
			return res, err
		}
		res.Affected, _ = r.RowsAffected()
		res.LastID, _ = r.LastInsertId()
		res.Note = fmt.Sprintf("受影响 %d 行", res.Affected)
	}
	res.Elapsed = time.Since(start).Milliseconds()
	return res, nil
}

// execLogged 执行写语句并写入 SQL 日志（日志附带参数值，便于排查）。
func execLogged(db *sql.DB, connID, database, sqlText string, args ...any) (sql.Result, error) {
	start := time.Now()
	r, err := db.Exec(sqlText, args...)
	var affected int64
	if r != nil {
		affected, _ = r.RowsAffected()
	}
	logSQL(connID, database, sqlWithArgs(sqlText, args), time.Since(start), affected, "", err)
	return r, err
}

// sqlWithArgs 将参数拼接到 SQL 末尾用于日志展示（仅用于日志，不参与执行）。
func sqlWithArgs(sqlText string, args []any) string {
	if len(args) == 0 {
		return sqlText
	}
	parts := make([]string, 0, len(args))
	for _, a := range args {
		if a == nil {
			parts = append(parts, "NULL")
			continue
		}
		parts = append(parts, fmt.Sprintf("%v", a))
	}
	return sqlText + " -- args=[" + strings.Join(parts, ", ") + "]"
}

/* ============================ 事务管理 ============================ */

// BeginTx 在指定连接/库上开启事务，返回事务 ID。
func BeginTx(id, database string) (string, error) {
	db, err := getDB(id)
	if err != nil {
		return "", err
	}
	ctxDB := db
	tx, err := ctxDB.Begin()
	if err != nil {
		return "", err
	}
	if database != "" {
		if _, err := tx.Exec("USE " + qid(database)); err != nil {
			tx.Rollback()
			return "", err
		}
	}
	mu.Lock()
	txSeq++
	txID := fmt.Sprintf("tx-%d-%d", time.Now().UnixNano(), txSeq)
	txs[txID] = &txEntry{db: db, tx: tx, connID: id, database: database}
	mu.Unlock()
	return txID, nil
}

// TxExecute 在事务中执行语句。
func TxExecute(txID, sqlText string) (QueryResult, error) {
	res := QueryResult{}
	mu.Lock()
	entry, ok := txs[txID]
	mu.Unlock()
	if !ok {
		return res, fmt.Errorf("事务不存在或已结束")
	}
	sqlText = strings.TrimSpace(strings.TrimRight(sqlText, ";"))
	head := strings.ToUpper(firstWord(sqlText))
	start := time.Now()
	if head == "SELECT" || head == "SHOW" || head == "DESC" || head == "DESCRIBE" || head == "EXPLAIN" || head == "WITH" {
		rows, err := entry.tx.Query(sqlText)
		if err != nil {
			logSQL(entry.connID, entry.database, sqlText, time.Since(start), 0, "", err)
			return res, err
		}
		defer rows.Close()
		if err := scanRows(rows, &res); err != nil {
			logSQL(entry.connID, entry.database, sqlText, time.Since(start), 0, "", err)
			return res, err
		}
		res.Note = fmt.Sprintf("%d 行", len(res.Rows))
		logSQL(entry.connID, entry.database, sqlText, time.Since(start), 0, res.Note, nil)
	} else {
		r, err := entry.tx.Exec(sqlText)
		if err != nil {
			logSQL(entry.connID, entry.database, sqlText, time.Since(start), 0, "", err)
			return res, err
		}
		res.Affected, _ = r.RowsAffected()
		res.Note = fmt.Sprintf("受影响 %d 行", res.Affected)
		logSQL(entry.connID, entry.database, sqlText, time.Since(start), res.Affected, res.Note, nil)
	}
	return res, nil
}

// CommitTx 提交事务。
func CommitTx(txID string) error {
	mu.Lock()
	entry, ok := txs[txID]
	if ok {
		delete(txs, txID)
	}
	mu.Unlock()
	if !ok {
		return fmt.Errorf("事务不存在或已结束")
	}
	return entry.tx.Commit()
}

// RollbackTx 回滚事务。
func RollbackTx(txID string) error {
	mu.Lock()
	entry, ok := txs[txID]
	if ok {
		delete(txs, txID)
	}
	mu.Unlock()
	if !ok {
		return fmt.Errorf("事务不存在或已结束")
	}
	return entry.tx.Rollback()
}

/* ============================ 连接配置持久化 ============================ */

func connectionsFilePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", herr
		}
		base = filepath.Join(home, ".config")
	}
	dir := filepath.Join(base, "go-tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "mysql-connections.json"), nil
}

// SaveConnections 持久化连接配置到文件。
func SaveConnections(configs []ConnConfig) (string, error) {
	path, err := connectionsFilePath()
	if err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(configs, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// LoadConnections 读取连接配置。
func LoadConnections() ([]ConnConfig, error) {
	path, err := connectionsFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []ConnConfig{}, nil
		}
		return nil, err
	}
	var configs []ConnConfig
	if err := json.Unmarshal(data, &configs); err != nil {
		return nil, err
	}
	return configs, nil
}

/* ============================ 辅助函数 ============================ */

// qid 反引号包裹标识符（列/库/表名）。
func qid(id string) string {
	return "`" + strings.ReplaceAll(id, "`", "``") + "`"
}

// qual 生成 db.table 限定名。
func qual(database, table string) string {
	if database == "" {
		return qid(table)
	}
	return qid(database) + "." + qid(table)
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t\n("); i > 0 {
		return s[:i]
	}
	return s
}

// scanRows 将 *sql.Rows 扫描进 QueryResult。
func scanRows(rows *sql.Rows, res *QueryResult) error {
	cols, err := rows.Columns()
	if err != nil {
		return err
	}
	res.Columns = cols
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		m := map[string]any{}
		for i, c := range cols {
			m[c] = normalize(vals[i])
		}
		res.Rows = append(res.Rows, m)
	}
	if res.Rows == nil {
		res.Rows = []map[string]any{}
	}
	return rows.Err()
}

// normalize 将驱动返回值规范化为可 JSON 序列化的值。
func normalize(v any) any {
	switch t := v.(type) {
	case []byte:
		return string(t)
	case nil:
		return nil
	case time.Time:
		return t.Format("2006-01-02 15:04:05")
	default:
		return t
	}
}
