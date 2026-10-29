package mysql

import (
	"fmt"
	"strings"
	"time"
)

// Backup 生成指定库（可选限定表）的 SQL 文本转储，模仿 mysqldump 的基本输出。
// tables 为空表示导出全部基表。返回完整 SQL 文本。
func Backup(id, database string, tables []string) (string, error) {
	db, err := getDB(id)
	if err != nil {
		return "", err
	}
	if database != "" {
		if _, err := db.Exec("USE " + qid(database)); err != nil {
			return "", err
		}
	}
	if len(tables) == 0 {
		list, err := ListTables(id, database)
		if err != nil {
			return "", err
		}
		for _, t := range list {
			tables = append(tables, t.Name)
		}
	}

	var b strings.Builder
	b.WriteString("-- Tools MySQL 备份\n")
	b.WriteString(fmt.Sprintf("-- 数据库: %s\n", database))
	b.WriteString(fmt.Sprintf("-- 生成时间: %s\n\n", time.Now().Format("2006-01-02 15:04:05")))
	b.WriteString("SET NAMES utf8mb4;\nSET FOREIGN_KEY_CHECKS=0;\n\n")

	for _, tbl := range tables {
		var tname, ddl string
		if err := db.QueryRow("SHOW CREATE TABLE "+qual(database, tbl)).Scan(&tname, &ddl); err != nil {
			return "", fmt.Errorf("获取表 %s 结构失败: %w", tbl, err)
		}
		b.WriteString("-- ----------------------------\n")
		b.WriteString(fmt.Sprintf("-- 表结构: %s\n", tbl))
		b.WriteString("-- ----------------------------\n")
		b.WriteString("DROP TABLE IF EXISTS " + qid(tbl) + ";\n")
		b.WriteString(ddl + ";\n\n")

		// 数据
		rows, err := db.Query("SELECT * FROM " + qual(database, tbl))
		if err != nil {
			return "", fmt.Errorf("读取表 %s 数据失败: %w", tbl, err)
		}
		cols, _ := rows.Columns()
		var insertLines []string
		for rows.Next() {
			vals := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range vals {
				ptrs[i] = &vals[i]
			}
			if err := rows.Scan(ptrs...); err != nil {
				rows.Close()
				return "", err
			}
			literals := make([]string, len(cols))
			for i := range cols {
				literals[i] = sqlLiteral(normalize(vals[i]))
			}
			insertLines = append(insertLines, "("+strings.Join(literals, ",")+")")
		}
		rows.Close()
		if len(insertLines) > 0 {
			b.WriteString(fmt.Sprintf("-- 数据: %s (%d 行)\n", tbl, len(insertLines)))
			// 每 50 行合并为一条 INSERT，控制单条长度
			const batch = 50
			for i := 0; i < len(insertLines); i += batch {
				end := i + batch
				if end > len(insertLines) {
					end = len(insertLines)
				}
				b.WriteString("INSERT INTO " + qid(tbl) + " VALUES\n" + strings.Join(insertLines[i:end], ",\n") + ";\n")
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("SET FOREIGN_KEY_CHECKS=1;\n")
	return b.String(), nil
}

// sqlLiteral 将值转为 SQL 字面量（用于备份文本）。
func sqlLiteral(v any) string {
	switch t := v.(type) {
	case nil:
		return "NULL"
	case string:
		return "'" + escapeSQLString(t) + "'"
	case int64:
		return fmt.Sprintf("%d", t)
	case float64:
		return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", t), "0"), ".")
	default:
		return "'" + escapeSQLString(fmt.Sprintf("%v", t)) + "'"
	}
}

func escapeSQLString(s string) string {
	r := strings.NewReplacer(
		"\\", "\\\\",
		"'", "\\'",
		"\n", "\\n",
		"\r", "\\r",
		"\x00", "\\0",
		"\x1a", "\\Z",
	)
	return r.Replace(s)
}
