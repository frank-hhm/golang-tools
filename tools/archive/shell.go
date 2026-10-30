package archive

// ============================================================
// 系统右键挂载的公共定义
//
// 挂载本身只在 Windows 上有实现（shell_windows.go）：HKCU 静态 verb
// 加 SendTo 快捷方式。非 Windows 一律回 sysx.ErrUnsupported，前端按
// Capabilities 把整块挂载区藏起来。
//
// 这一层单独成文件是因为状态结构、命令行标记这些必须两头共用：
// main.go 的分流要认得标记，设置页要认得状态，而它们都不能依赖
// 只有 windows 才编译进来的那个文件。
// ============================================================

// 自己占的 verb 键名。带程序标识，卸载时只删这两个名字底下的键。
const (
	VerbCompress = "GoToolsArchive"
	VerbExtract  = "GoToolsExtract"
)

// 命令行分流标记：注册表里写的和 main.go 里解析的都取自这里，
// 两头各写一遍字符串迟早对不上。
const (
	ArgArchive = "--archive"
	ArgExtract = "--extract"
	ArgPanel   = "--archive-panel"
)

// 菜单文案。Win11 的二级菜单里 Windows 自带的「压缩为 ZIP 文件」也在，
// 带上程序名用户才知道自己点的是哪一个。
const (
	LabelCompress = "压缩 (Tools)"
	LabelExtract  = "解压 (Tools)"
	LabelSendTo   = "压缩 (Tools)…"
)

// ShellInfo 挂载状态。扁平结构，直接给前端用。
type ShellInfo struct {
	Supported   bool     `json:"supported"`
	Exe         string   `json:"exe"`         // 当前程序路径
	Compress    int      `json:"compress"`    // 压缩 verb 的槽位态（多个键取最差）
	Extract     int      `json:"extract"`     // 解压 verb 的槽位态
	SendTo      int      `json:"sendTo"`      // SendTo 快捷方式的槽位态
	Stale       bool     `json:"stale"`       // 有键指向的不是当前 exe（程序挪过位置）
	Detail      string   `json:"detail"`      // 差在哪一项的人话说明
	MountedKeys []string `json:"mountedKeys"` // 已确认挂在当前 exe 上的项
	ProblemKeys []string `json:"problemKeys"` // 缺失或失配的项
}

// 挂载槽位的态。每个 verb 组底下有好几个键，逐个读回再汇总。
const (
	SlotAbsent  = 0 // 压根没挂
	SlotCurrent = 1 // 挂了，且指向当前 exe
	SlotForeign = 2 // 挂了，但记的是别的路径（要修）
	SlotDamaged = 3 // 键在但读不出来（权限不够，或那个位置被别的程序占了）
)

// SlotText 槽位态的中文说明（前端直接显示，不再拼判断）。
func SlotText(slot int) string {
	switch slot {
	case SlotCurrent:
		return "已挂载"
	case SlotForeign:
		return "指向旧路径"
	case SlotDamaged:
		return "读取失败"
	default:
		return "未挂载"
	}
}

// worstSlot 取一组槽位里最差的那个：只要有一处没挂好，整体就不算挂好。
//
// 读不出来排在指错之前：前者意味着连改都改不了（权限/被占），
// 报成"指向旧路径"反而会让用户以为点一下修复就行。
func worstSlot(slots []int) int {
	if len(slots) == 0 {
		return SlotAbsent
	}
	worst := SlotCurrent
	for _, s := range slots {
		switch {
		case s == SlotDamaged:
			return SlotDamaged
		case s == SlotForeign && worst != SlotAbsent:
			worst = SlotForeign
		case s == SlotAbsent:
			worst = SlotAbsent
		}
	}
	return worst
}

// NeedsRepair 挂载开着但键里记的路径已经不是当前 exe —— 启动时静默重写一次。
//
// 只认 Stale：SlotDamaged 那一档可能是位置被别的程序占了，
// 擅自覆写别人的键不是"自修复"，是抢地盘。
func NeedsRepair(info ShellInfo) bool {
	return info.Stale
}

// AllMounted 三条通道是否都指向当前 exe（用于「已挂载」这个整体状态显示）。
func AllMounted(info ShellInfo) bool {
	return info.Compress == SlotCurrent && info.Extract == SlotCurrent && info.SendTo == SlotCurrent
}
