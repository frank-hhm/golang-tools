package mysql

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"Tools/appdirs"
)

// ============================================================
// SQL 执行日志（MySQL 工具专属）
//
//   - 目录/开关由 AppConfig.ToolSettings["mysql"] 控制，App 启动与保存设置时调用 ConfigureSQLLog。
//   - 按天滚动写入 mysql-sql-YYYYMMDD.log；目录为空时落到程序目录下 logs（与设置页日志默认目录一致）。
//   - 记录手动执行的 SQL 与所有写操作（UpdateCell/InsertRow/DeleteRow/DDL），
//     数据浏览类自动查询（TableData）不记录，避免刷屏。
//   - 线程安全；写失败只提示到 stderr，不影响业务执行。
// ============================================================

// SQLLogFile 单个 SQL 日志文件信息。
type SQLLogFile struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
}

// SQLLogSettings SQL 日志的开关、目录与现存文件（供设置弹窗展示）。
type SQLLogSettings struct {
	Enabled     bool         `json:"enabled"`
	Dir         string       `json:"dir"`         // 配置目录（空表示默认）
	DefaultDir  string       `json:"defaultDir"`  // 默认目录（程序目录下 logs），供界面提示
	ResolvedDir string       `json:"resolvedDir"` // 实际写入目录
	Files       []SQLLogFile `json:"files"`
	TotalSize   int64        `json:"totalSize"`
}

const sqlLogFilePrefix = "mysql-sql-"

var (
	sqlLogMu      sync.Mutex
	sqlLogEnabled bool
	sqlLogDir     string
	sqlLogFile    *os.File
	sqlLogDate    string
)

// DefaultSQLLogDir 默认 SQL 日志目录：日志基础目录/mysql（基础目录默认 <应用目录>/logs，设置页可改）。
func DefaultSQLLogDir() string {
	if dir, err := appdirs.Logs("mysql"); err == nil {
		return dir
	}
	return filepath.Join(appdirs.LogsBase(), "mysql")
}

// resolveSQLLogDir 规范化日志目录（空=默认；误填文件路径时取所在目录）。
func resolveSQLLogDir(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return DefaultSQLLogDir()
	}
	if st, err := os.Stat(dir); err == nil && !st.IsDir() {
		return filepath.Dir(dir)
	}
	return dir
}

// ConfigureSQLLog 应用 SQL 日志开关与目录，返回实际目录（目录为空时使用默认目录）。
func ConfigureSQLLog(enabled bool, dir string) string {
	resolved := resolveSQLLogDir(dir)
	sqlLogMu.Lock()
	defer sqlLogMu.Unlock()
	if resolved != sqlLogDir {
		closeSQLLogFileLocked()
	}
	sqlLogEnabled = enabled
	sqlLogDir = resolved
	return resolved
}

// SQLLogStatus 返回当前 SQL 日志开关、实际目录与现存文件。
func SQLLogStatus() SQLLogSettings {
	sqlLogMu.Lock()
	defer sqlLogMu.Unlock()
	st := SQLLogSettings{Enabled: sqlLogEnabled, DefaultDir: DefaultSQLLogDir(), ResolvedDir: sqlLogDir}
	if st.ResolvedDir == "" {
		st.ResolvedDir = DefaultSQLLogDir()
	}
	st.Files = listSQLLogFilesLocked(st.ResolvedDir)
	for _, f := range st.Files {
		st.TotalSize += f.Size
	}
	return st
}

// ClearSQLLog 清空 SQL 日志文件（仅删除 mysql-sql-*.log），返回删除的文件数。
func ClearSQLLog() (int, error) {
	sqlLogMu.Lock()
	defer sqlLogMu.Unlock()
	// Windows 下打开中的文件无法删除，先关闭句柄
	closeSQLLogFileLocked()
	dir := sqlLogDir
	if dir == "" {
		dir = DefaultSQLLogDir()
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), sqlLogFilePrefix) {
			continue
		}
		if err := os.Remove(filepath.Join(dir, e.Name())); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// logSQL 记录一次 SQL 执行；未启用或目录不可用时静默跳过。
func logSQL(connID, database, sqlText string, elapsed time.Duration, affected int64, note string, execErr error) {
	sqlLogMu.Lock()
	defer sqlLogMu.Unlock()
	if !sqlLogEnabled || sqlLogDir == "" {
		return
	}
	line := formatSQLLog(connID, database, sqlText, elapsed, affected, note, execErr)
	if err := appendSQLLogLineLocked(line); err != nil {
		fmt.Fprintf(os.Stderr, "[sqllog] 写入日志失败: %v\n", err)
	}
}

// formatSQLLog 生成单行日志：时间 [连接] [库] [状态 耗时 行数] SQL [错误]。
func formatSQLLog(connID, database, sqlText string, elapsed time.Duration, affected int64, note string, execErr error) string {
	conn := strings.TrimSpace(connID)
	if conn == "" {
		conn = "-"
	}
	db := strings.TrimSpace(database)
	if db == "" {
		db = "-"
	}
	status := "OK"
	if execErr != nil {
		status = "FAIL"
	}
	detail := ""
	if execErr != nil {
		detail = " | ERROR " + execErr.Error()
	} else if affected > 0 {
		detail = fmt.Sprintf(" | affected=%d", affected)
	} else if note != "" {
		detail = " | " + note
	}
	// 压缩换行与连续空白，超长截断，保证一条日志一行
	sqlOne := strings.Join(strings.Fields(sqlText), " ")
	if len(sqlOne) > 4000 {
		sqlOne = sqlOne[:4000] + " …"
	}
	return fmt.Sprintf("%s [%s] [db=%s] [%s %dms] %s%s",
		time.Now().Format("2006-01-02 15:04:05.000"), conn, db, status, elapsed.Milliseconds(), sqlOne, detail)
}

// appendSQLLogLineLocked 追加一行到当日日志文件（调用方须持有 sqlLogMu）。
func appendSQLLogLineLocked(line string) error {
	date := time.Now().Format("20060102")
	if sqlLogFile == nil || sqlLogDate != date {
		closeSQLLogFileLocked()
		if err := os.MkdirAll(sqlLogDir, 0o755); err != nil {
			return err
		}
		path := filepath.Join(sqlLogDir, sqlLogFilePrefix+date+".log")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		sqlLogFile = f
		sqlLogDate = date
	}
	_, err := sqlLogFile.WriteString(line + "\n")
	return err
}

// closeSQLLogFileLocked 关闭当前日志句柄（调用方须持有 sqlLogMu）。
func closeSQLLogFileLocked() {
	if sqlLogFile != nil {
		_ = sqlLogFile.Close()
		sqlLogFile = nil
	}
	sqlLogDate = ""
}

// listSQLLogFilesLocked 列出目录下的 SQL 日志文件（按修改时间倒序）。
func listSQLLogFilesLocked(dir string) []SQLLogFile {
	out := []SQLLogFile{}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), sqlLogFilePrefix) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, SQLLogFile{
			Name:    e.Name(),
			Size:    info.Size(),
			ModTime: info.ModTime().Format("2006-01-02 15:04:05"),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModTime > out[j].ModTime })
	return out
}
