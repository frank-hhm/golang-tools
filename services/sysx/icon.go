package sysx

import (
	"path/filepath"
	"strings"
	"sync"
)

// ============================================================
// 文件图标
//
// 给检索结果、进程列表之类"一个路径配一个小图"的场景用。产物是 32x32 的
// PNG dataURL，前端 img 标签能直接吃；取不到就返回空串，由调用方画占位字形。
//
// 为什么在这里做而不是各工具各做一份：图标这条路要碰 shell32/gdi32，
// 每个面板自己挂一遍 LazyProc 既重复又容易漏掉 Find() 探测（找不到导出函数
// 是 panic，不是 error）。
//
// 缓存按绝对路径小写归档：同一目录树里几千个文件常常只有几十种扩展名，
// shell 那边问来问去就是同一批图标。
// ============================================================

var (
	iconMu    sync.Mutex
	iconCache = map[string]string{}
)

// Icon 返回该路径经 shell 解析后的图标（PNG dataURL）；取不到返回空串。
//
// 对 .lnk 快捷方式会给出它所指向程序的图标（SHGetFileInfoW 默认解析链接），
// 对 docx/pdf 之类给出扩展名关联的图标，因此应用、文件、窗口三类结果能共用这一个入口。
func Icon(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	key := strings.ToLower(filepath.Clean(path))
	iconMu.Lock()
	cached, hit := iconCache[key]
	iconMu.Unlock()
	if hit {
		return cached
	}
	// 取图标是磁盘 IO + shell 调用，不持锁：让调用方（前端限流并发）真正并行。
	got := platformIcon(key)
	iconMu.Lock()
	iconCache[key] = got
	iconMu.Unlock()
	return got
}

// IconCacheReset 清空图标缓存（换主题/卸载程序后想让结果重新解析时用）。
func IconCacheReset() {
	iconMu.Lock()
	iconCache = map[string]string{}
	iconMu.Unlock()
}
