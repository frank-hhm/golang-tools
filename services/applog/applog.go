// Package applog 全局可配置日志（原根目录 logger.go）。
//
// 由 config.json 的 log 段控制开关与目录；只暴露 Info/Warn/Error/Debug 四个便捷
// 入口，各调用点带一个 tag（模块名，如 "capture"/"hotkey"/"config"）便于过滤。
package applog

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"Tools/appdirs"
)

// ============================================================
// 全局可配置日志
//
//   - 由 config.json 的 Log 段控制：开关 / 日志目录。
//   - 只需配置一个目录；开启后 info / warn / error 全部记录，按“级别 + 当前日期”自动分文件写入，如：
//       Error20261022.log / Warn20261022.log / Info20261022.log / Debug20261022.log
//     跨零点自动切换到新日期文件（按级别各自滚动）。
//   - 每个关键操作与异常都应通过 Info / Error（或带级别的 Log）记录。
//   - 线程安全；Init 可在设置变更后重复调用以切换目录/开关。
//   - 同时输出到文件与标准输出（开发期可见）。
// ============================================================

// 日志级别（数值越大越严重）
const (
	levelDebug = iota
	levelInfo
	levelWarn
	levelError
)

// Config 日志设置：Path 为空时使用 DefaultDir()。
type Config struct {
	Enabled bool   `json:"enabled"`
	Level   string `json:"level"`
	Path    string `json:"path"`
}

// logHandle 缓存某级别的当前文件句柄与其对应日期，便于跨零点滚动。
type logHandle struct {
	f    *os.File
	date string
}

var (
	mu      sync.Mutex
	enabled bool
	dir     string
	handles = map[int]*logHandle{}
	inited  bool
)

// DefaultDir 程序日志默认目录：日志基础目录/app（基础目录默认 <应用目录>/logs，设置页可改）。
func DefaultDir() string {
	if d, err := appdirs.Logs("app"); err == nil {
		return d
	}
	return filepath.Join(appdirs.DefaultRoot(), "logs", "app")
}

func levelName(l int) string {
	switch l {
	case levelDebug:
		return "DEBUG"
	case levelWarn:
		return "WARN"
	case levelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

// levelFilePrefix 级别对应的日志文件名前缀（大写首字母）。
func levelFilePrefix(l int) string {
	switch l {
	case levelDebug:
		return "Debug"
	case levelWarn:
		return "Warn"
	case levelError:
		return "Error"
	default:
		return "Info"
	}
}

// ResolveDir 返回该配置实际会使用的日志目录（仅解析，不创建目录、不切换句柄）。
func ResolveDir(cfg Config) string {
	d := strings.TrimSpace(cfg.Path)
	if d == "" {
		d = DefaultDir()
	}
	// 兼容历史：若给的是文件路径，取其所在目录
	if st, err := os.Stat(d); err == nil && !st.IsDir() {
		d = filepath.Dir(d)
	}
	return d
}

// Init 依据日志配置初始化（或重新初始化）全局日志器。
// cfg.Path 为日志目录；为空则用 DefaultDir()。返回实际使用的日志目录。
func Init(cfg Config) string {
	d := ResolveDir(cfg)
	_ = os.MkdirAll(d, 0o755)

	mu.Lock()
	defer mu.Unlock()

	// 目录变更：关闭并清空所有级别的旧句柄
	if dir != d {
		for _, h := range handles {
			if h.f != nil {
				_ = h.f.Close()
			}
		}
		handles = map[int]*logHandle{}
		dir = d
	}
	enabled = cfg.Enabled
	inited = true
	return d
}

// writeToFile 写入某级别当日文件（调用方须持有 mu）。
func writeToFile(level int, line string) {
	if dir == "" {
		return
	}
	date := time.Now().Format("20060102")
	h := handles[level]
	if h == nil {
		h = &logHandle{}
		handles[level] = h
	}
	// 首次或跨零点：（重新）打开对应日期文件
	if h.f == nil || h.date != date {
		if h.f != nil {
			_ = h.f.Close()
			h.f = nil
		}
		path := filepath.Join(dir, levelFilePrefix(level)+date+".log")
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

// Log 记录一条日志：未启用文件日志时丢弃（所有级别写入各自分级文件）。
func Log(level int, tag, format string, args ...any) {
	// 未初始化时兜底：至少保证开发期 stdout 可见
	mu.Lock()
	if !inited {
		mu.Unlock()
		if level >= levelInfo {
			fmt.Printf("%s [%s] %s\n", time.Now().Format("2006-01-02 15:04:05.000"), levelName(level), fmt.Sprintf(format, args...))
		}
		return
	}
	on := enabled
	mu.Unlock()

	if !on {
		return
	}

	line := fmt.Sprintf("%s [%s] [%s] %s",
		time.Now().Format("2006-01-02 15:04:05.000"), levelName(level), tag,
		fmt.Sprintf(format, args...))

	mu.Lock()
	writeToFile(level, line)
	mu.Unlock()
	fmt.Println(line)
}

// 级别常量（供需要显式指定级别的调用方使用）。
const (
	Debug = levelDebug
	Info  = levelInfo
	Warn  = levelWarn
	Error = levelError
)

// 便捷封装 --------------------------------------------------

// Infof 记录一次正常操作（info 级）。
func Infof(tag, format string, args ...any) { Log(levelInfo, tag, format, args...) }

// Errorf 记录异常/错误（error 级）。
func Errorf(tag, format string, args ...any) { Log(levelError, tag, format, args...) }

// Warnf 记录告警（warn 级）。
func Warnf(tag, format string, args ...any) { Log(levelWarn, tag, format, args...) }

// Debugf 记录调试（debug 级）。
func Debugf(tag, format string, args ...any) { Log(levelDebug, tag, format, args...) }

// Fail 若 err 非空则以 error 级记录并返回之（便于在 return 处内联埋点）。
func Fail(tag string, err error, format string, args ...any) error {
	if err != nil {
		msg := fmt.Sprintf(format, args...)
		Log(levelError, tag, "%s: %v", msg, err)
	}
	return err
}
