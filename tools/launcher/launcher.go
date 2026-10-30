// Package launcher 快捷搜索启动器的检索引擎：一次输入同时命中已安装应用、本程序
// 内置工具、运行中的窗口、已知目录下的本地文件，外加算式求值、系统命令与 URL 直达。
//
// 入口是 Alt+Space 呼出的独立置顶小窗（services/app/launcher*.go 负责窗口生命周期，
// frontend/src/views/widget/launcher.vue 负责界面），不注册为工具项、不进主页网格。
//
// 依赖方向：本包只允许向下依赖 services/config（读工具列表与检索范围）、
// services/sysx（图标/打开器/跨平台边界）与 appdirs（usage 落盘）。
// 需要主程序才做得动的事（激活工具窗、执行命令、把算式结果写进剪贴板、把某个
// 窗口调到前台）一律走 SetHostRunner 注入的回调，引擎绝不反向 import 宿主。
//
// 诚实边界（v1）：
//   - 只索引设置里列出的目录（默认已知目录 + 用户追加），不是全盘检索；
//   - 不覆盖 Microsoft Store / UWP 应用（未接 shell:AppsFolder）；
//   - 中文不支持拼音首字母，"mb" 命中不了"幕布"；
//   - Linux / server 构建没有原生窗口，整个面板不可用，这里只保证能编过。
package launcher

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"Tools/appdirs"
	"Tools/services/applog"
)

// 结果类型：Type 决定图标缺省字形与执行分支，ID 是执行句柄。
const (
	TypeApp     = "app"     // ID = 目标可执行文件路径（解析不出来时是 .lnk 自身）
	TypeTool    = "tool"    // ID = 工具 ID（与热键/浮动球用的是同一个标识，宿主按它派发）
	TypeWindow  = "window"  // ID = HWND 十进制
	TypeFile    = "file"    // ID = 绝对路径
	TypeCalc    = "calc"    // ID = 求值结果文本（回车即复制）
	TypeCommand = "command" // ID = 命令 ID
	TypeURL     = "url"     // ID = 补全后的完整 URL
)

// MaxResults 一次检索最多返回多少条（前端列表高度按它算）。
const MaxResults = 20

// Item 一条检索结果。
type Item struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Title  string `json:"title"`
	Sub    string `json:"sub"`  // 副标题：路径 / "Win32 application" / "表达式求值"
	Icon   string `json:"icon"` // PNG dataURL；空则前端按 Type 画字形
	Score  int    `json:"score"`
	Danger bool   `json:"danger"` // 危险命令（退出程序）：前端标红
}

// Result 一次检索的返回体。
type Result struct {
	Items      []Item `json:"items"`
	IndexReady bool   `json:"indexReady"` // 文件索引已就绪（false 时文件类结果为空或不全）
	IndexCount int    `json:"indexCount"`
}

// Scopes 检索范围开关，来自配置。
type Scopes struct {
	Apps     bool `json:"apps"`
	Tools    bool `json:"tools"`
	Windows  bool `json:"windows"`
	Files    bool `json:"files"`
	Commands bool `json:"commands"` // 同时管算式与 URL 直达
}

// perTypeCap 每类最多几条：不让"报告"这种命中几十个文件的前缀把列表刷满，
// 也让应用/工具/窗口三类始终有机会露脸。
var perTypeCap = map[string]int{
	TypeApp:     6,
	TypeTool:    5,
	TypeWindow:  5,
	TypeFile:    8,
	TypeCommand: 5,
	TypeCalc:    1,
	TypeURL:     1,
}

// typeBonus 分数同档时的稳定次序：算式/网址 > 内置工具 > 应用 > 命令 > 窗口 > 文件。
//
// 算式与 URL 排最前：它们只在“整串输入就是一个算式/一个网址”时才产出，意图已经
// 确定，不该被一个同名的应用或文件压住。内置工具高于应用：这个启动器是本程序自己的
// 门面，“docker”“redis”这类词先命中本程序的管理页比先命中外部同名程序更有用
// （应用真要找，它的名字一般更长，前缀分不会输）。
//
// 档位差只到 30 量级：匹配分本身按 100 跳档，所以它只拨同档的近手，
// 不拿“名字完全相等”换“类型优先”。使用频次（usageBonus，最高 200）仍大于这里
// 的全部档位差，用户真正常点的条目照样能排到最前。
var typeBonus = map[string]int{
	TypeCalc:    110, // 只有整串是合法算式才产出，独占一条时理应排第一
	TypeURL:     100, // 整串就是个网址，同理
	TypeTool:    90,
	TypeApp:     60,
	TypeCommand: 40,
	TypeWindow:  30,
	TypeFile:    0,
}

// HostRunner 执行需要宿主能力的条目。kind 取 TypeTool / TypeCommand / TypeCalc，
// id 为对应 Item.ID。
type HostRunner func(kind, id string) error

var (
	hostMu   sync.RWMutex
	runner   HostRunner
	usageMu  sync.Mutex
	usage    map[string]int // Item.ID -> 执行次数
	usageErr string
)

// SetHostRunner 由宿主在启动时注入（重复调用以最后一次为准）。
func SetHostRunner(fn HostRunner) {
	hostMu.Lock()
	runner = fn
	hostMu.Unlock()
}

// ============================================================
// 检索
// ============================================================

// Search 聚合各 provider 的结果：统一打分排序后按类型限量、总量截断，最后补图标。
//
// 以 ">" 开头时只保留命令类（PowerToys 惯例），用来在"我就想找个设置项"时屏蔽噪音。
func Search(q string, sc Scopes) Result {
	q = strings.TrimSpace(q)
	res := Result{}
	if idx := currentIndex(); idx != nil {
		res.IndexCount = len(idx.entries)
		res.IndexReady = !idx.built.IsZero()
	}
	if q == "" {
		return res
	}
	cmdOnly := strings.HasPrefix(q, ">")
	if cmdOnly {
		q = strings.TrimSpace(q[1:])
		if q == "" {
			return res
		}
	}

	var items []Item
	if !cmdOnly {
		if sc.Apps {
			items = append(items, appItems(q)...)
		}
		if sc.Tools {
			items = append(items, toolItems(q)...)
		}
		if sc.Windows {
			items = append(items, windowItems(q)...)
		}
		if sc.Files {
			items = append(items, fileItems(q)...)
		}
		if sc.Commands {
			if it, ok := calcItem(q); ok {
				items = append(items, it)
			}
			if it, ok := urlItem(q); ok {
				items = append(items, it)
			}
		}
	}
	if sc.Commands {
		items = append(items, commandItems(q)...)
	}

	items = dedup(items)
	limit := topK(items, MaxResults)
	fillIcons(limit)
	res.Items = limit
	return res
}

// dedup 同一路径可能被两个 provider 各给一次（开始菜单里的 .lnk 与它指向的 exe
// 同时进了文件索引）。留分数高的那条。
func dedup(items []Item) []Item {
	seen := make(map[string]int, len(items))
	out := items[:0]
	for _, it := range items {
		key := strings.ToLower(it.ID)
		if at, hit := seen[key]; hit {
			if it.Score > out[at].Score {
				out[at] = it
			}
			continue
		}
		seen[key] = len(out)
		out = append(out, it)
	}
	return out
}

// topK 先按分数排序，再按类型限额收口，最后截到 total。
// 限额后不再补别的类型：宁可少给两条，也不让"文件"这一类把列表占满。
func topK(items []Item, total int) []Item {
	usage := usageSnapshot()
	for i := range items {
		items[i].Score += typeBonus[items[i].Type] + usageBonus(usage[items[i].ID])
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		if c := strings.Compare(items[i].Title, items[j].Title); c != 0 {
			return c < 0
		}
		return items[i].ID < items[j].ID
	})
	used := map[string]int{}
	out := make([]Item, 0, total)
	for _, it := range items {
		if len(out) >= total {
			break
		}
		if used[it.Type] >= perTypeCap[it.Type] {
			continue
		}
		used[it.Type]++
		out = append(out, it)
	}
	return out
}

// usageBonus 用过越多次越靠前，但封顶：一个被点过 50 次的文件不该永远压住新应用。
func usageBonus(n int) int {
	if n > 10 {
		n = 10
	}
	return n * 20
}

// fillIcons 只给最终要展示的这几条取图标（≤20 次 shell 调用，且 sysx.Icon 内部有缓存）。
// 取不到就留空，前端按类型画字形——绝不为一个图标失败而丢掉整条结果。
func fillIcons(items []Item) {
	for i := range items {
		if items[i].Icon != "" {
			continue
		}
		switch items[i].Type {
		case TypeApp, TypeFile:
			items[i].Icon = iconOf(items[i].ID)
		case TypeWindow:
			items[i].Icon = iconOf(items[i].Sub) // 窗口的 Sub 存的是 exe 路径
		case TypeTool:
			// 内置工具的图标是前端图标库的 class，不是 PNG；由 launcher.vue 自己画。
		}
	}
}

// ============================================================
// 执行
// ============================================================

// Execute 执行一条结果，并在成功后累加使用频次（下次同类输入排更前）。
func Execute(it Item) error {
	var err error
	switch it.Type {
	case TypeApp:
		err = launchApp(it)
	case TypeFile:
		err = launchFile(it.ID)
	case TypeWindow:
		err = activateWindow(it.ID)
	case TypeURL:
		err = openURL(it.ID)
	case TypeTool, TypeCommand, TypeCalc:
		err = runHost(it.Type, it.ID)
	default:
		err = ErrNoRunner
	}
	if err == nil {
		bumpUsage(it.ID)
	}
	return err
}

// OpenLocation 在资源管理器里定位这条结果对应的文件（Alt+Enter）。
// 只有带文件路径的类型有意义，其余返回 ErrNoLocation。
func OpenLocation(it Item) error {
	switch it.Type {
	case TypeApp, TypeFile, TypeWindow:
		p := it.ID
		if it.Type == TypeWindow {
			p = it.Sub
		}
		if p == "" {
			return ErrNoLocation
		}
		return revealPath(p)
	}
	return ErrNoLocation
}

func runHost(kind, id string) error {
	hostMu.RLock()
	r := runner
	hostMu.RUnlock()
	if r == nil {
		return ErrNoRunner
	}
	return r(kind, id)
}

// ============================================================
// 使用频次（落 <数据目录>/launcher/usage.json）
// ============================================================

func usagePath() (string, error) {
	dir, err := appdirs.Data("launcher")
	if err != nil {
		return "", err
	}
	return dir + string(os.PathSeparator) + "usage.json", nil
}

// loadUsage 只在第一次用到时读盘；失败记进 usageErr 供设置页展示，不反复重试。
var loadUsageOnce sync.Once

func usageSnapshot() map[string]int {
	loadUsageOnce.Do(loadUsage)
	usageMu.Lock()
	defer usageMu.Unlock()
	if usage == nil {
		return map[string]int{}
	}
	return usage
}

func loadUsage() {
	p, err := usagePath()
	if err != nil {
		usageErr = err.Error()
		return
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if !os.IsNotExist(err) {
			usageErr = err.Error()
		}
		return
	}
	m := map[string]int{}
	if err := json.Unmarshal(data, &m); err != nil {
		usageErr = err.Error()
		return
	}
	usageMu.Lock()
	usage = m
	usageMu.Unlock()
}

func bumpUsage(id string) {
	if id == "" {
		return
	}
	usageMu.Lock()
	if usage == nil {
		usage = map[string]int{}
	}
	usage[id]++
	// 只用了几百个条目时不必操心；真养出几千条说明用户早不用那几个老文件了，整表清掉重来。
	if len(usage) > 2048 {
		usage = map[string]int{id: 1}
	}
	snap := make(map[string]int, len(usage))
	for k, v := range usage {
		snap[k] = v
	}
	usageMu.Unlock()

	p, err := usagePath()
	if err != nil {
		return
	}
	data, err := json.MarshalIndent(snap, "", " ")
	if err != nil {
		return
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		applog.Warnf("launcher", "使用频次落盘失败: %v", err)
	}
}

// UsageReset 清空使用频次（设置页"重建索引"旁边给个退路）。
func UsageReset() error {
	p, err := usagePath()
	if err != nil {
		return err
	}
	usageMu.Lock()
	usage = map[string]int{}
	usageMu.Unlock()
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ============================================================
// 内置工具（读配置里的 tools 列表，与主页/浮动球同一份数据）
// ============================================================

// toolItems 把配置项折算成检索条目：Title 用显示名，ID 用工具标识，
// 交给宿主决定是开窗还是发事件。
//
// ID 取 t.ID（旧配置可能没写）而非路由：本程序所有原生入口（全局热键、
// 浮动球菜单）都拿工具标识当派发钥匙，这里跟着它们走，宿主才不用再做一次
// "路径→工具"的反查（同一个路径理论上能被两个工具项共用）。
func toolItems(q string) []Item {
	var out []Item
	for _, t := range currentTools() {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			name = strings.TrimSpace(t.Key)
		}
		if name == "" {
			continue
		}
		s := scoreText(q, name, t.Path)
		if s <= 0 {
			continue
		}
		id := strings.TrimSpace(t.ID)
		if id == "" {
			id = strings.TrimSpace(t.Key)
		}
		sub := "本程序工具"
		if p := strings.TrimSpace(t.Path); p != "" {
			sub = p
		}
		out = append(out, Item{
			Type: TypeTool, ID: id, Title: name,
			Sub:   sub,
			Score: s,
		})
	}
	return out
}

// currentTools 取配置里的工具列表：单独抽出来，是为了让单测能塞一份假列表进来看排序。
var currentTools = defaultCurrentTools

// defaultCurrentTools 读真实配置。config 包内部有缓存，每次调用不是读盘。
func defaultCurrentTools() []toolRef {
	cfg := configTools()
	out := make([]toolRef, 0, len(cfg))
	for _, t := range cfg {
		out = append(out, toolRef{ID: t.ID, Key: t.Key, Name: t.Name, Path: t.Path, Event: t.Event})
	}
	return out
}

// SetToolsForTest 仅供测试注入假工具表；传 nil 恢复真实配置。
func SetToolsForTest(list []toolRef) {
	if list == nil {
		currentTools = defaultCurrentTools
		return
	}
	currentTools = func() []toolRef { return list }
}

// toolRef 引擎视角的一个工具项（只拿用得着的五个字段，不跟 config 类型绑死）。
type toolRef struct {
	ID    string
	Key   string
	Name  string
	Path  string
	Event string
}

// ============================================================
// 索引生命周期（宿主只在功能开启时才调这几个）
// ============================================================

// EnsureIndex 索引为空或过期时异步重建；已在建或还新鲜就直接返回。
// 非阻塞：本次检索照旧用上一份索引，绝不为了"新一点"把用户敲下回车的那一下卡住。
func EnsureIndex() {
	if !enabled() {
		return
	}
	cur := currentIndex()
	if cur != nil && time.Since(cur.built) < indexTTL {
		return
	}
	startBuild(false)
}

// RebuildIndex 同步重建一次索引（设置页点"重建索引"时调用，前端转圈等它）。
func RebuildIndex() {
	startBuild(true)
}

// IndexInfo 索引状态，供设置页展示。
type IndexInfo struct {
	Count    int      `json:"count"`
	Building bool     `json:"building"`
	Partial  bool     `json:"partial"` // 撞到条目上限，索引不全
	LastErr  string   `json:"lastErr"`
	Roots    []string `json:"roots"`
	Age      string   `json:"age"` // 上次构建距今（人话），未建过为"从未"
}

// GetIndexInfo 返回索引状态快照。
func GetIndexInfo() IndexInfo {
	info := IndexInfo{Roots: indexRoots()}
	if cur := currentIndex(); cur != nil {
		info.Count = len(cur.entries)
		info.Partial = cur.partial
		info.LastErr = cur.err
		if !cur.built.IsZero() {
			info.Age = time.Since(cur.built).Round(time.Second).String()
		}
	}
	info.Building = building.Load()
	return info
}
