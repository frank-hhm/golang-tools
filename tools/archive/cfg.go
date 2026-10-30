package archive

import (
	"Tools/services/config"
	"Tools/services/sysx"
)

// ============================================================
// 配置桥（与 tools/launcher/cfg.go 同一套做法）
//
// 引擎读配置只走这一层：开关、默认格式、默认级别都在这一个文件里落地，
// 策略改动不会散到命名与压缩流程里去。依赖方向照旧是 tools → services/config，
// 反向不允许。
// ============================================================

// Enabled 总开关的导出视图。关掉时宿主不挂右键、不留 SendTo 快捷方式、
// 不起转交监听；工具页本身不看不开这个开关（压缩是纯功能，不占系统位置）。
func Enabled() bool { return config.ArchiveEnabled() }

// SendToWanted SendTo 那条快捷方式该不该存在。
//
// 总开关关着时它必然也是 false：挂载状态以总开关为准，不能让一个
// 已经关掉的特性在「发送到」里留下快捷方式。
func SendToWanted() bool {
	a := config.ArchiveCfg()
	return a.Enabled && a.SendTo
}

// MountWanted 右键 verb 该不该存在（= 总开关）。
func MountWanted() bool { return Enabled() }

// ApplyDefaults 把设置里的默认格式与默认级别补进没填的字段。
//
// 只在字段为空时补：右键那条路没有界面，靠的就是这两个默认值；
// 而工具页与选项小窗都是用户当场选过的，不能被配置里的默认值盖回去。
func ApplyDefaults(o *Options) {
	cfg := config.ArchiveCfg()
	if o.Format == "" {
		switch cfg.Format {
		case config.ArchiveFormatTarGz:
			o.Format = sysx.ArchiveTarGz
		default:
			o.Format = sysx.ArchiveZip
		}
	}
	if o.Level <= 0 {
		o.Level = cfg.Level
	}
	o.Level = sysx.NormalizeLevel(o.Level)
}
