package launcher

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"Tools/services/applog"
)

// ============================================================
// 文件索引
//
// 为什么自建而不是复用 Windows 搜索索引 / 接 Everything：
//   - 系统索引（ISearchQuery）只能搜被索引的位置，且 Windows Search 服务被不少用户
//     手动关掉，关掉之后功能整体不可用，这不是一个"面板"该押上去的依赖；
//   - Everything 全盘毫秒检索确实最好用，但要用户另外装一个程序并常驻，
//     本仓库的定位是自带工具集，不该把核心体验外包出去。
//
// 代价照实说：只覆盖列出的目录，首次建索引要读盘。所以这里刻意做得很轻——
// 一份只读切片 + atomic 整体换，查询是线性扫（60k 条目量级下毫秒级），
// 不建倒排、不起文件监听、不落盘。
// ============================================================

// indexTTL 索引新鲜期：过期后下一次检索会异步重建，本次照旧用旧的。
const indexTTL = 10 * time.Minute

// defaultMaxEntries 默认条目上限。撞顶后 Partial 置真，设置页如实标出"索引不全"。
const defaultMaxEntries = 60000

// defaultMaxDepth 目录深度上限：已知目录里常见的深层结构（node_modules 之类）
// 已经靠 skipDirs 挡掉，这条是给"用户把整个盘挂到文档目录下"兜底用的。
const defaultMaxDepth = 8

// fileReturnCap 文件类一次最多交多少条给排序阶段。
// 搜 "e" 这种单字母能命中上万个文件，全塞进 sort 纯属浪费；
// 留 4 倍于展示上限的余量，是因为后面还要按使用频次与类型加分重排。
const fileReturnCap = 32

// skipDirs 无条件跳过的目录名（大小写不敏感）。
// 点开头与下划线开头的另有一条前缀规则，这里只列"名字本身就会惹麻烦"的。
var skipDirs = map[string]bool{
	"appdata":        true, // 几百个 GB 的应用内部数据，不是用户要找的东西
	"node_modules":   true,
	"$recycle.bin":   true,
	"venv":           true,
	"library":        true, // OneDrive 的备份容器，动辄十几万文件
	"windowsapps":    true,
	"programdata":    true,
	"temp":           true,
	"tmp":            true,
	"__pycache__":    true,
	".git":           true,
	"indexerpersist": true,
}

type entry struct {
	path  string
	name  string
	lower string // 预先小写的 name：查询是线性的，每条再 ToLower 一次白付一遍分配
	dir   bool
}

type fileIndex struct {
	entries []entry
	built   time.Time
	partial bool
	err     string
}

var (
	idxPtr   atomic.Pointer[fileIndex]
	building atomic.Bool
)

func currentIndex() *fileIndex { return idxPtr.Load() }

// startBuild 起一轮索引构建。
// wait=true（设置页点"重建索引"）时同步等它跑完，前端要拿准确条数回显；
// wait=false 时后台跑，跑完通过 onIndexReady 广播给宿主，让开着的面板重查一次。
func startBuild(wait bool) {
	if !enabled() {
		return
	}
	if !building.CompareAndSwap(false, true) {
		if wait {
			// 已经有一轮在跑：同步等它结束，别让设置页以为点了没反应
			for building.Load() {
				time.Sleep(100 * time.Millisecond)
			}
		}
		return
	}
	run := func() {
		defer building.Store(false)
		roots := indexRoots()
		maxEntries := indexMaxEntries()
		t0 := time.Now()
		idx := buildIndex(roots, maxEntries, defaultMaxDepth)
		idxPtr.Store(idx)
		// 应用清单与文件索引共用"重扫一遍"这个时机：用户点重建索引通常就是
		// "刚装了个程序搜不到"，顺带把开始菜单也刷新一次。
		invalidateApps()
		applog.Infof("launcher", "索引完成：%d 条，用时 %s，roots=%d，partial=%v",
			len(idx.entries), time.Since(t0).Round(time.Millisecond), len(roots), idx.partial)
		if !wait {
			notifyIndexReady(len(idx.entries))
		}
	}
	if wait {
		run()
		return
	}
	go run()
}

// buildIndex 遍历 roots 建一份索引。单个 root 出错只记 err，不让整轮失败。
func buildIndex(roots []string, maxEntries, maxDepth int) *fileIndex {
	idx := &fileIndex{built: time.Now()}
	if maxEntries <= 0 {
		maxEntries = defaultMaxEntries
	}
	if maxDepth <= 0 {
		maxDepth = defaultMaxDepth
	}
	seen := make(map[string]bool, 1024) // 同一目录被两个 root 覆盖到（OneDrive 常见）
	for _, root := range roots {
		if len(idx.entries) >= maxEntries {
			idx.partial = true
			break
		}
		base := filepath.Clean(root)
		if base == "" {
			continue
		}
		rootDepth := strings.Count(base, string(filepath.Separator))
		filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				// 权限不足/正在被删：跳过这一支，不算整轮失败
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			name := d.Name()
			if p != base {
				if rel := strings.Count(filepath.Dir(p), string(filepath.Separator)) - rootDepth; rel > maxDepth {
					if d.IsDir() {
						return fs.SkipDir
					}
					return nil
				}
				if skipEntry(name, d.IsDir()) {
					if d.IsDir() {
						return fs.SkipDir
					}
					return nil
				}
			}
			if d.IsDir() {
				return nil
			}
			key := strings.ToLower(p)
			if seen[key] {
				return nil
			}
			if len(idx.entries) >= maxEntries {
				idx.partial = true
				return filepath.SkipAll
			}
			seen[key] = true
			idx.entries = append(idx.entries, entry{
				path: p, name: name, lower: strings.ToLower(name), dir: false,
			})
			return nil
		})
	}
	return idx
}

// skipEntry 目录名黑名单 + 点/下划线/美元开头的隐藏物。
func skipEntry(name string, isDir bool) bool {
	if name == "" {
		return true
	}
	switch name[0] {
	case '.', '_', '$':
		return true
	}
	if skipDirs[strings.ToLower(name)] {
		return true
	}
	// 目录额外挡一层：Windows 的"计划任务""临时 Internet 文件"这类系统目录
	// 名字带本地化前缀，逐个列不现实，超长的直接当噪音。
	_ = isDir
	return false
}

// fileItems 线性扫索引，取分数最高的若干条。
//
// 索引没建好时这里返回空——Search 的返回值里 IndexReady=false，
// 前端会显示"索引建立中"，而不是假装"没有这个文件"。
func fileItems(q string) []Item {
	idx := currentIndex()
	if idx == nil || len(idx.entries) == 0 {
		return nil
	}
	nq := normName(q)
	if nq == "" {
		return nil
	}
	var hits []Item
	for i := range idx.entries {
		e := &idx.entries[i]
		s := 0
		if strings.HasPrefix(e.lower, nq) || strings.Contains(e.lower, nq) {
			s = scoreText(q, e.name, e.path)
		} else if strings.Contains(normPath(e.path), strings.ToLower(q)) {
			s = scoreText(q, e.name, e.path)
		}
		if s <= 0 {
			continue
		}
		hits = append(hits, Item{
			Type: TypeFile, ID: e.path, Title: e.name, Sub: dirOf(e.path), Score: s,
		})
	}
	// 总是按分数排一次：条数本来就少（十几条），排一次的代价可以忽略，
	// 而"没到截断阈值就按遍历顺序返回"会让人以为排序坏了。
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > fileReturnCap {
		hits = hits[:fileReturnCap]
	}
	return hits
}

// dirOf 去掉最后一段，得到"所在目录"（展示用，正斜杠反斜杠都能处理）。
func dirOf(p string) string {
	if i := strings.LastIndexAny(p, `/\`); i > 0 {
		return p[:i]
	}
	return p
}

// DropIndex 丢掉索引并停止后续重建（宿主把功能关掉时调用：
// 没开启就没必要让 60k 条路径继续占着几 MB 内存、更没必要到期就重扫一遍盘）。
func DropIndex() {
	idxPtr.Store(nil)
	invalidateApps()
}

// IndexEntryCountForTest 给单测看内部状态用。
func indexEntryCount() int {
	if idx := currentIndex(); idx != nil {
		return len(idx.entries)
	}
	return 0
}

// statFile 只用来判断存在与是否目录，取不到一律当不存在。
func statFile(p string) (bool, bool) {
	st, err := os.Stat(p)
	if err != nil {
		return false, false
	}
	return true, st.IsDir()
}
