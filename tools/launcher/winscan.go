package launcher

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// ============================================================
// 运行中的窗口
//
// "搜到窗口并切过去"这条能力天生平台专属（Windows 是 EnumWindows，Linux 是
// 各桌面环境的 wmctrl/EWMH），所以枚举与激活都写在 winscan_windows.go /
// winscan_other.go 里。这里只放两边共用的打分与截断，免得两套实现各抄一遍排序。
//
// 一个刻意的顺序：标题先筛、exe 路径后取。取一个窗口背后的 exe 要
// OpenProcess + QueryFullProcessImageName，桌面上的窗口动辄上百个，全量解析
// 纯属白费；只有最终要展示的那几条才配付这笔钱。
// ============================================================

// winReturnCap 一次最多交多少条窗口结果给排序阶段。
const winReturnCap = 24

// excludedHWND 本程序自己的快捷搜索面板窗口，检索时要跳过它：
// 用户敲字时面板是开着的，不把"快捷搜索"自己滤掉，列表里会永远挂着一条自己。
var excludedHWND uintptr

// ExcludeWindow 由宿主在建好面板窗口后告知其句柄（传 0 表示不跳过任何窗口）。
func ExcludeWindow(hwnd uintptr) { excludedHWND = hwnd }

// BringWindow 把一个顶层窗口调到前台。宿主拿它拉起自己的面板窗口：
// “切到某个窗口”这件事本包已经有一套完整的 Win32 做法（最小化先还原、
// SetForegroundWindow 失败再 AttachThreadInput 兜底），在宿主里重写一份只会多两套
// 各自漂移的实现。非 Windows 下返回 ErrUnsupported。
func BringWindow(hwnd uintptr) error {
	if hwnd == 0 {
		return fmt.Errorf("窗口句柄不可用")
	}
	return activateWindow(strconv.FormatUint(uint64(hwnd), 10))
}

// windowCand 平台层交上来的一个候选窗口。
type windowCand struct {
	HWND  uintptr // 给 windowExePath / activateWindow 用
	ID    string  // HWND 的十进制文本，作为结果的执行句柄
	Title string  // 窗口标题
}

// windowItems 枚举可见窗口，按标题打分，取前若干条并补上 exe 路径。
func windowItems(q string) []Item {
	nq := normName(q)
	lq := lower(q)
	if nq == "" {
		return nil
	}
	type winHit struct {
		it   Item
		hwnd uintptr
	}
	var hits []winHit
	enumerateWindows(func(c windowCand) {
		lt := lower(c.Title)
		if !strings.Contains(lt, lq) && !wordHeadHit(lt, nq) {
			return
		}
		s := scoreText(q, c.Title, "")
		if s <= 0 {
			return
		}
		hits = append(hits, winHit{
			it:   Item{Type: TypeWindow, ID: c.ID, Title: c.Title, Score: s},
			hwnd: c.HWND,
		})
	})
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].it.Score > hits[j].it.Score })
	if len(hits) > winReturnCap {
		hits = hits[:winReturnCap]
	}
	out := make([]Item, 0, len(hits))
	for i := range hits {
		hits[i].it.Sub = windowExePath(hits[i].hwnd)
		out = append(out, hits[i].it)
	}
	return out
}
