package launcher

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"Tools/services/config"
)

// ============================================================
// 配置桥
//
// 引擎读配置只走这一层：好处是"开关/范围/目录"这些策略改动只碰一个文件，
// 坏处是引擎与 config 包有了耦合——但工具列表本来就长在 config 里，
// 再造一层注入接口只是把五行代码变成五十行。
// ============================================================

// indexNotifier 索引构建完成后的广播（宿主注入，用来给开着的面板发事件）。
var indexNotifier func(count int)

// SetIndexNotifier 由宿主注入；传 nil 取消。
func SetIndexNotifier(fn func(count int)) { indexNotifier = fn }

func notifyIndexReady(count int) {
	if fn := indexNotifier; fn != nil {
		fn(count)
	}
}

// enabled 总开关。关掉时下面这些挂载点全部空转：
// 不注册热键、不建窗口、不扫盘、不建索引。
func enabled() bool { return config.LauncherCfg().Enabled }

// Enabled 总开关的导出视图：宿主（services/app）靠它决定要不要挂热键、
// 要不要建面板窗口、要不要建索引。引擎内部的判断一律用未导出的 enabled，
// 两者同一个来源，不会出现“窗口能开但索引不建”这种半挂状态。
func Enabled() bool { return enabled() }

// ScopesFromConfig 当前检索范围，供宿主（绑定层）取用。
func ScopesFromConfig() Scopes {
	s := config.LauncherCfg().Scopes
	return Scopes{Apps: s.Apps, Tools: s.Tools, Windows: s.Windows, Files: s.Files, Commands: s.Commands}
}

// ShortcutFromConfig 全局热键文本（形如 "Alt+Space"）；未开启时为空串。
func ShortcutFromConfig() string {
	if !enabled() {
		return ""
	}
	return config.LauncherCfg().Shortcut
}

func indexMaxEntries() int {
	if n := config.LauncherCfg().MaxEntries; n > 0 {
		return n
	}
	return defaultMaxEntries
}

// indexRoots 参与文件索引的目录：平台已知目录 + 设置里追加的目录。
// 不存在/不是目录的一律丢掉——用户删了 OneDrive 之后配置里还留着旧路径是常态，
// 不该为此在每轮索引里刷一排错误日志。
func indexRoots() []string {
	out := make([]string, 0, 8)
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if ok, dir := statFile(p); !ok || !dir {
			return
		}
		key := strings.ToLower(filepath.Clean(p))
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, key)
	}
	for _, p := range defaultRoots() {
		add(p)
	}
	for _, p := range config.LauncherCfg().Dirs {
		add(p)
	}
	sort.Strings(out)
	return out
}

// configTools 配置里的工具列表（首页/浮动球用的同一份）。
func configTools() []config.ToolConfig {
	return config.CurrentConfig().Tools
}

// DefaultDirs 设置页展示用的"默认已纳入目录"清单（真实存在的那些）。
func DefaultDirs() []string {
	out := make([]string, 0, 8)
	for _, p := range defaultRoots() {
		if ok, dir := statFile(p); ok && dir {
			out = append(out, p)
		}
	}
	return out
}

// DirExists 给设置页校验"追加目录"输入用。
func DirExists(p string) bool {
	ok, dir := statFile(strings.TrimSpace(p))
	return ok && dir
}

// HomeDir 用户主目录，设置页"追加目录"的默认落点。
func HomeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}
