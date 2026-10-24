package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ============================================================
// 全局可配置日志
//
//   - 由 config.json 的 Log 段控制：开关 / 日志目录。
//   - 只需配置一个目录；开启后 info / warn / error 全部记录，按“级别 + 当前日期”自动分文件写入，如：
//       Error20261022.log / Warn20261022.log / Info20261022.log / Debug20261022.log
//     跨零点自动切换到新日期文件（按级别各自滚动）。
//   - 每个关键操作与异常都应通过 opLogf / errLogf（或带 tag 的 appLogf）记录。
//   - 线程安全；InitLogger 可在设置变更后重复调用以切换目录/开关。
//   - 同时输出到文件与标准输出（开发期可见）。
// ============================================================

// 日志级别（数值越大越严重）
const (
	logDebug = iota
	logInfo
	logWarn
	logError
)

// logHandle 缓存某级别的当前文件句柄与其对应日期，便于跨零点滚动。
type logHandle struct {
	f    *os.File
	date string
}

var (
	loggerMu      sync.Mutex
	loggerEnabled bool
	logDir        string
	logHandles    = map[int]*logHandle{}
	loggerInited  bool
)

func levelName(l int) string {
	switch l {
	case logDebug:
		return "DEBUG"
	case logWarn:
		return "WARN"
	case logError:
		return "ERROR"
	default:
		return "INFO"
	}
}

// levelFilePrefix 级别对应的日志文件名前缀（大写首字母）。
func levelFilePrefix(l int) string {
	switch l {
	case logDebug:
		return "Debug"
	case logWarn:
		return "Warn"
	case logError:
		return "Error"
	default:
		return "Info"
	}
}

// InitLogger 依据日志配置初始化（或重新初始化）全局日志器。
// cfg.Path 为日志目录；为空则用默认目录。返回实际使用的日志目录。
func InitLogger(cfg LogConfig) string {
	dir := strings.TrimSpace(cfg.Path)
	if dir == "" {
		dir = defaultLogDir()
	}
	// 兼容历史：若给的是文件路径，取其所在目录
	if st, err := os.Stat(dir); err == nil && !st.IsDir() {
		dir = filepath.Dir(dir)
	}
	_ = os.MkdirAll(dir, 0o755)

	loggerMu.Lock()
	defer loggerMu.Unlock()

	// 目录变更：关闭并清空所有级别的旧句柄
	if logDir != dir {
		for _, h := range logHandles {
			if h.f != nil {
				_ = h.f.Close()
			}
		}
		logHandles = map[int]*logHandle{}
		logDir = dir
	}
	loggerEnabled = cfg.Enabled
	loggerInited = true
	return dir
}

// writeToFile 写入某级别当日文件（调用方须持有 loggerMu）。
func writeToFile(level int, line string) {
	if logDir == "" {
		return
	}
	date := time.Now().Format("20060102")
	h := logHandles[level]
	if h == nil {
		h = &logHandle{}
		logHandles[level] = h
	}
	// 首次或跨零点：（重新）打开对应日期文件
	if h.f == nil || h.date != date {
		if h.f != nil {
			_ = h.f.Close()
			h.f = nil
		}
		path := filepath.Join(logDir, levelFilePrefix(level)+date+".log")
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[log] 打开日志文件失败 %s: %v\n", path, err)
			return
		}
		h.f = f
		h.date = date
	}
	_, _ = h.f.WriteString(line + "\n")
}

// appLogf 记录一条日志：未启用文件日志时丢弃（所有级别写入各自分级文件）。
func appLogf(level int, tag, format string, args ...any) {
	// 未初始化时兜底：至少保证开发期 stdout 可见
	loggerMu.Lock()
	if !loggerInited {
		loggerMu.Unlock()
		if level >= logInfo {
			fmt.Printf("%s [%s] %s\n", time.Now().Format("2006-01-02 15:04:05.000"), levelName(level), fmt.Sprintf(format, args...))
		}
		return
	}
	enabled := loggerEnabled
	loggerMu.Unlock()

	if !enabled {
		return
	}

	line := fmt.Sprintf("%s [%s] [%s] %s",
		time.Now().Format("2006-01-02 15:04:05.000"), levelName(level), tag,
		fmt.Sprintf(format, args...))

	loggerMu.Lock()
	writeToFile(level, line)
	loggerMu.Unlock()
	fmt.Println(line)
}

// 便捷封装 --------------------------------------------------

// opLogf 记录一次正常操作（info 级）。
func opLogf(tag, format string, args ...any) { appLogf(logInfo, tag, format, args...) }

// errLogf 记录异常/错误（error 级）。
func errLogf(tag, format string, args ...any) { appLogf(logError, tag, format, args...) }

// warnLogf 记录告警（warn 级）。
func warnLogf(tag, format string, args ...any) { appLogf(logWarn, tag, format, args...) }

// dbgLogf 记录调试（debug 级）。
func dbgLogf(tag, format string, args ...any) { appLogf(logDebug, tag, format, args...) }

// errErr 若 err 非空则以 error 级记录并返回之（便于在 return 处内联埋点）。
func errErr(tag string, err error, format string, args ...any) error {
	if err != nil {
		msg := fmt.Sprintf(format, args...)
		appLogf(logError, tag, "%s: %v", msg, err)
	}
	return err
}
