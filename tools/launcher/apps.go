package launcher

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ============================================================
// 已安装应用清单
//
// 来源就是开始菜单那几个目录（当前用户 + 全体用户，外加桌面快捷方式）——
// 这也正是 Windows 自己"搜索应用"看的那几处。装个程序必然往里放一条 .lnk，
// 不必去读注册表的 UninstallString（那个字段格式千奇百怪，还常常指向卸载器而非程序本体）。
//
// 不解析 .lnk 背后指向哪个 exe（那要 COM 的 IShellLinkW，也就要求每个跑检索的
// 线程都 CoInitializeEx，goroutine 会在系统线程间漂移，是个能咬人的坑）：
//   - 启动直接 ShellExecuteW 那条 .lnk，shell 自己会解析目标，行为与双击一致；
//   - 图标交给 sysx.Icon（SHGetFileInfoW 默认就跟 .lnk 取到目标程序的图标）；
//   - ID 用 .lnk 路径，代价是"同一个程序的多条快捷方式"在使用频次上是各算各的。
//     开始菜单里一个程序通常只有一条启动快捷方式，这点误差可以接受。
//
// 清单带 TTL 缓存：装/卸程序是低频事件，没必要每次敲字都重扫一遍。
// ============================================================

// appTTL 应用清单的保鲜期；重建文件索引时会顺带把它一起作废。
const appTTL = 10 * time.Minute

// appMaxDepth / appMaxCount 扫描边界。开始菜单正常深度 2~3 层，
// 给到 6 层是留给"某套件塞了一堆子文件夹"的情况；上限则防止一个误放的
// 巨大目录树把清单撑爆。
const (
	appMaxDepth = 6
	appMaxCount = 4000
)

// appReturnCap 一次检索最多交多少条应用给排序阶段（展示上限由 perTypeCap 收口）。
const appReturnCap = 24

// appExts 哪些扩展名算"一个应用"，值作为结果的副标题。
var appExts = map[string]string{
	".lnk": "应用程序",
	".exe": "应用程序",
	".url": "网页快捷方式",
}

type appEntry struct {
	name  string // 展示名：文件名去掉扩展名
	path  string // 快捷方式或 exe 的绝对路径
	lower string // normName(name)，预先算好，检索是线性的
	kind  string // 副标题
}

var (
	appsMu   sync.Mutex
	appsAt   time.Time
	appsList []appEntry
)

// loadApps 返回应用清单，过期或未建时同步扫一次。
//
// 同步而不是异步：清单只有一两千条、纯目录枚举，首次呼出面板时扫一次（约几十毫秒）
// 换来"刚装的程序立刻能搜到"，比"先给个空列表、下次再说"诚实。
func loadApps() []appEntry {
	appsMu.Lock()
	defer appsMu.Unlock()
	if appsList != nil && time.Since(appsAt) < appTTL {
		return appsList
	}
	appsList = scanApps()
	appsAt = time.Now()
	return appsList
}

// invalidateApps 让下一次检索重扫清单（重建索引时调用）。
func invalidateApps() {
	appsMu.Lock()
	appsList = nil
	appsAt = time.Time{}
	appsMu.Unlock()
}

// scanApps 遍历开始菜单目录，收出应用清单。
func scanApps() []appEntry {
	roots := startMenuRoots()
	out := make([]appEntry, 0, 256)
	seen := map[string]bool{}
	for _, root := range roots {
		base := filepath.Clean(root)
		if base == "" {
			continue
		}
		rootDepth := strings.Count(base, string(filepath.Separator))
		filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				if d != nil && d.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if d.IsDir() {
				if p == base {
					return nil
				}
				if strings.Count(p, string(filepath.Separator))-rootDepth > appMaxDepth || skipEntry(d.Name(), true) {
					return fs.SkipDir
				}
				return nil
			}
			// 链接/重解析点一律不看：OneDrive 占位符会指到云端，软链可能成环。
			if d.Type()&fs.ModeSymlink != 0 {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			kind, ok := appExts[ext]
			if !ok {
				return nil
			}
			name := strings.TrimSuffix(filepath.Base(p), filepath.Ext(p))
			if name = strings.TrimSpace(name); name == "" {
				return nil
			}
			key := lower(p)
			if seen[key] {
				return nil
			}
			seen[key] = true
			out = append(out, appEntry{name: name, path: p, lower: normName(name), kind: kind})
			if len(out) >= appMaxCount {
				return filepath.SkipAll
			}
			return nil
		})
	}
	return out
}

// appItems 按名字（其次按路径）匹配应用清单。
func appItems(q string) []Item {
	list := loadApps()
	if len(list) == 0 {
		return nil
	}
	nq := normName(q)
	hits := make([]Item, 0, 8)
	for i := range list {
		e := &list[i]
		// 先做最便宜的字符判断，只有可能的候选才付 scoreText 的开销。
		if !strings.HasPrefix(e.lower, nq) && !strings.Contains(e.lower, nq) && !strings.Contains(lower(e.path), lower(q)) {
			continue
		}
		s := scoreText(q, e.name, e.path)
		if s <= 0 {
			continue
		}
		hits = append(hits, Item{
			Type: TypeApp, ID: e.path, Title: e.name, Sub: e.kind, Score: s,
		})
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].Score > hits[j].Score })
	if len(hits) > appReturnCap {
		hits = hits[:appReturnCap]
	}
	return hits
}
