package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"Tools/appdirs"
	"Tools/services/applog"
)

// ============================================================
// 全局应用配置（Go 侧 JSON 文件持久化，与 redis/mysql 连接配置同目录）
//
// 位置：os.UserConfigDir()/Tools/config.json
// 内容：日志设置（开关/级别/路径）+ 全局目录布局（插件/数据/日志基础目录）
//       + 可编辑工具列表（含快捷标记、全局热键等）
// 读取方：启动与设置保存时转交给日志器（services/applog）、目录布局（appdirs）、
// 浮动球与全局热键（经 services/app 的 nativeHost 适配注入 services/winui）、前端设置页/首页。
// 本包集中应用配置的类型与持久化；AppConfig 等类型出现在绑定方法签名里，会被 Wails 生成进前端模型。
// ============================================================

// LogConfig 日志设置
type LogConfig struct {
	Enabled bool   `json:"enabled"` // 是否写文件日志
	Level   string `json:"level"`   // debug | info | warn | error（最低记录级别）
	Path    string `json:"path"`    // 日志目录；空 = 默认（配置目录下 Tools/logs）。按级别+日期自动分文件：Error20261022.log / Info20261022.log 等
}

// BallConfig 桌面浮动球设置
type BallConfig struct {
	Enabled bool   `json:"enabled"` // 是否在启动时显示浮动球（设置页可即时显隐）
	Icon    string `json:"icon"`    // 球心图标值（icon-xxx 或 Arco 名，供设置页回显；像素数据在配置目录 ball_icon.png）
	X       int    `json:"x"`       // 上次拖动后的窗口左上角 X（物理像素，含阴影留白）
	Y       int    `json:"y"`       // 上次拖动后的窗口左上角 Y
	HasPos  bool   `json:"hasPos"`  // 是否已记录位置；false 时启动停靠工作区右下角
	// HideAllShortcut 「显示/隐藏全部窗口」全局热键（形如 Ctrl+Alt+H），空 = 不注册。
	// 老配置没这段：读进来是空串（不注册），避免默默抢占用户自定义过的组合键；新默认带 Ctrl+Alt+H。
	HideAllShortcut string `json:"hideAllShortcut"`
	// DockEdge 拖到屏幕边缘时自动收缩成胶囊 tab；指针型：nil = 开启（与 ToolConfig.Home 同一套路），
	// 只有显式 false 才关闭，normalizeConfig 把 nil 兜成 true。
	DockEdge *bool `json:"dockEdge"`
}

// ThemeConfig 主题令牌配置。mode 沿用 light/dark/auto 语义（前端据此切 body[arco-theme]、
// 浮动球与窗口首帧底色据此定深浅，后端也消费这一维）；其余维度（预设色板 / 自定义主色 /
// 圆角 / 密度 / 高对比度 / 减少动效 / 玻璃 / 背景）与明暗正交，后端只持久化不消费，由前端投射进
// --app-* 设计令牌。glass/background 已从短名升级为对象，但 glass 仍兼容老字符串（见 ThemeGlass）。
type ThemeConfig struct {
	Mode       string          `json:"mode"`       // light | dark | auto，默认 light
	Preset     string          `json:"preset"`     // 预设皮肤名（default/graphite/ocean/forest/sunset…）；custom=自定义主色，默认 default
	Primary    string          `json:"primary"`    // 自定义主色 hex（Preset=custom 时生效）；空=未设
	Harmony    string          `json:"harmony"`    // 色彩和谐模式 analogous|complementary|split|triadic，默认 analogous（前端消费，后端仅持久化）
	Radius     string          `json:"radius"`     // small | default | large，默认 default
	Density    string          `json:"density"`    // compact | standard | loose，默认 standard（只管留白/间距）
	FontSize   string          `json:"fontSize"`   // small | default | large | xlarge，默认 default（字号，独立于密度，前端消费后端仅持久化）
	Contrast   string          `json:"contrast"`   // normal | high，默认 normal
	Motion     string          `json:"motion"`     // on | reduced，默认 on
	Flush      bool            `json:"flush"`      // 无边距模式：卡片去左右内边距、不铺背景，描边固定归无（前端消费，后端仅持久化，默认 false）
	Glass      ThemeGlass      `json:"glass"`      // 表面玻璃材质参数；兼容老字符串 off|surface（见 UnmarshalJSON）
	Background ThemeBackground `json:"background"` // 全局背景主题（网格/风景/暗夜纹理/自定义图/原生）
}

// ThemeGlass 表面磨砂/Acrylic 仿材质参数。数值全部存整数避免前后端 float 序列化歧义，
// 前端按各自除数还原：alpha/border ÷100（0..1）、shadow ÷100（0..0.5）、noise ÷1000（0..0.12）、blur 直接 px。
type ThemeGlass struct {
	On       bool   `json:"on"`       // 是否启用表面玻璃（旧 glass!="off" 的等价开关）
	Material string `json:"material"` // frosted | acrylic，acrylic 才消费 noise
	Alpha    int    `json:"alpha"`    // 底透明度 0-100（÷100）
	Blur     int    `json:"blur"`     // 模糊半径 0-40 px
	Border   int    `json:"border"`   // 描边不透明度 0-100（÷100）
	Shadow   int    `json:"shadow"`   // 投影强度 0-50（÷100）
	Noise    int    `json:"noise"`    // 噪点强度 0-120（÷1000），仅 acrylic 消费
}

// ThemeBackground 全局背景主题档。
type ThemeBackground struct {
	Type  string `json:"type"`  // none | mesh | photo | dark | image | native，默认 none
	Image string `json:"image"` // 导入后的绝对路径，仅 type=image；其它档清空
}

// 玻璃/背景维度的合法集与默认，前后端各写一次，靠变更文档与单测锁住口径。
const (
	glassMaterialFrosted = "frosted"
	glassMaterialAcrylic = "acrylic"
	backgroundNone       = "none"
	backgroundImage      = "image"
)

// legacySurfaceGlass 把阶段一的 glass=="surface" 旧值映射成当前 tokens.scss 里那组观感，
// 保证老用户升级后磨砂观感逐像素不变（alpha 0.72 / blur 16 / border 0.3 / shadow 0.18）。
func legacySurfaceGlass() ThemeGlass {
	return ThemeGlass{On: true, Material: glassMaterialFrosted, Alpha: 72, Blur: 16, Border: 30, Shadow: 18, Noise: 0}
}

// defaultGlass 默认关闭（纯 no-op），材质取 frosted。
func defaultGlass() ThemeGlass {
	return ThemeGlass{Material: glassMaterialFrosted}
}

func validGlassMaterial(v string) bool {
	return v == glassMaterialFrosted || v == glassMaterialAcrylic
}

func validBackgroundType(v string) bool {
	switch v {
	case "none", "mesh", "photo", "dark", "image", "native":
		return true
	}
	return false
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// normalizeThemeGlass 夹区间 + 材质合法化，idempotent，不覆盖合法值。
func normalizeThemeGlass(g *ThemeGlass) {
	if !validGlassMaterial(g.Material) {
		g.Material = glassMaterialFrosted
	}
	g.Alpha = clampInt(g.Alpha, 0, 100)
	g.Blur = clampInt(g.Blur, 0, 40)
	g.Border = clampInt(g.Border, 0, 100)
	g.Shadow = clampInt(g.Shadow, 0, 50)
	g.Noise = clampInt(g.Noise, 0, 120)
}

// normalizeThemeBackground 背景档合法化；非 image 档清空残留路径。
func normalizeThemeBackground(b *ThemeBackground) {
	if !validBackgroundType(b.Type) {
		b.Type = backgroundNone
	}
	b.Image = strings.TrimSpace(b.Image)
	if b.Type != backgroundImage {
		b.Image = ""
	}
}

// UnmarshalJSON 兼容老配置里 glass 是裸字符串（"off"/"surface"）的形态：字符串按语义映射成对象，
// 对象走常规解码，null/空保持零值（随后 normalizeThemeGlass 兜默认）。避免老用户升级后磨砂配置丢失。
func (g *ThemeGlass) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if trimmed[0] == '"' {
		if strings.Trim(trimmed, "\"") == "surface" {
			*g = legacySurfaceGlass()
		} else {
			*g = defaultGlass()
		}
		return nil
	}
	type alias ThemeGlass
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*g = ThemeGlass(a)
	return nil
}

// 主题 mode 的合法三态与预设默认名：前后端各写一次，靠变更文档与单测锁住口径。
const (
	ThemeModeLight       = "light"
	ThemeModeDark        = "dark"
	ThemeModeAuto        = "auto"
	defaultThemePreset   = "default"
	defaultThemeHarmony  = "analogous"
	defaultThemeFontSize = "default"
)

// validThemeHarmony 和谐模式合法集（与前端 palette.HARMONY_OFFSETS 键一致）。
func validThemeHarmony(v string) bool {
	switch v {
	case "analogous", "complementary", "split", "triadic":
		return true
	}
	return false
}

// validThemeFontSize 字号档合法集（与前端 theme.FONT_SCALE_MAP 键一致）。
func validThemeFontSize(v string) bool {
	switch v {
	case "small", "default", "large", "xlarge":
		return true
	}
	return false
}

// defaultThemeConfig 返回一份合法的主题默认配置（仅 mode 有后端语义，其余取各自默认短名）。
func defaultThemeConfig() ThemeConfig {
	return ThemeConfig{
		Mode:       ThemeModeLight,
		Preset:     defaultThemePreset,
		Harmony:    defaultThemeHarmony,
		Radius:     "default",
		Density:    "standard",
		FontSize:   defaultThemeFontSize,
		Contrast:   "normal",
		Motion:     "on",
		Glass:      defaultGlass(),
		Background: ThemeBackground{Type: backgroundNone},
	}
}

// UnmarshalJSON 向后兼容老配置：早期 config.json 里 theme 是裸字符串（"light"/"dark"/"auto"），
// 现升级为对象。这里容忍两种形态——JSON 字符串按 mode 解析并补齐其余默认，对象走常规解码，
// null/空按零值（随后 normalizeThemeConfig 兜默认）。避免老用户升级后配置报错或主题丢失。
func (t *ThemeConfig) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	// 老格式：带引号的裸字符串
	if trimmed[0] == '"' {
		*t = defaultThemeConfig()
		switch strings.Trim(trimmed, "\"") {
		case ThemeModeDark:
			t.Mode = ThemeModeDark
		case ThemeModeAuto:
			t.Mode = ThemeModeAuto
		default:
			t.Mode = ThemeModeLight
		}
		return nil
	}
	// 新格式：对象。用别名类型规避递归进入本方法。
	type alias ThemeConfig
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*t = ThemeConfig(a)
	return nil
}

// normalizeThemeConfig 补齐主题配置合法范围：mode 收敛到 light/dark/auto 三态，其余维度
// 空值回落各自默认短名。全是 idempotent 修正，不覆盖用户填的合法值。
func normalizeThemeConfig(t *ThemeConfig) {
	switch t.Mode {
	case ThemeModeLight, ThemeModeDark, ThemeModeAuto:
	default:
		t.Mode = ThemeModeLight
	}
	if strings.TrimSpace(t.Preset) == "" {
		t.Preset = defaultThemePreset
	}
	if !validThemeHarmony(strings.TrimSpace(t.Harmony)) {
		t.Harmony = defaultThemeHarmony
	}
	if strings.TrimSpace(t.Radius) == "" {
		t.Radius = "default"
	}
	if strings.TrimSpace(t.Density) == "" {
		t.Density = "standard"
	}
	if !validThemeFontSize(strings.TrimSpace(t.FontSize)) {
		t.FontSize = defaultThemeFontSize
	}
	if strings.TrimSpace(t.Contrast) == "" {
		t.Contrast = "normal"
	}
	if strings.TrimSpace(t.Motion) == "" {
		t.Motion = "on"
	}
	normalizeThemeGlass(&t.Glass)
	normalizeThemeBackground(&t.Background)
	t.Primary = strings.TrimSpace(t.Primary)
}

// GeneralConfig 应用通用设置
type GeneralConfig struct {
	AutoStart bool        `json:"autoStart"` // 是否注册开机（Windows 登录时）自动启动，写入 HKCU\...\Run 注册表键
	AppName   string      `json:"appName"`   // 应用显示名称（窗口标题/托盘提示），默认 "Tools"
	AppLogo   string      `json:"appLogo"`   // 自定义 logo 图片绝对路径，空表示使用内置默认
	Theme     ThemeConfig `json:"theme"`     // 主题配置（模式 + 令牌维度）：mode 后端消费，其余仅持久化
}

// PathsConfig 全局基础目录（plugins/data/logs 各自下按服务名建子目录，downloads 为单基目录，见 appdirs/layout.go）；
// 空 = 默认 <应用目录>/plugins|data|logs|downloads。修改保存后，各服务配置与另存/下载文件默认目录随即落新路径。
type PathsConfig struct {
	Plugins   string `json:"plugins"`   // 插件目录：服务程序安装位置（php/nginx/mysql/redis/npm）
	Data      string `json:"data"`      // 数据目录：服务配置缓存/实例工作目录/截图录像等另存默认位置
	Logs      string `json:"logs"`      // 日志目录：各服务日志与程序日志（logs/<服务名>）
	Downloads string `json:"downloads"` // 下载目录：下载管理器默认落点，空=默认 <应用目录>/downloads
}

// ToolConfig 单个工具项（首页列表 / 浮动球快捷 / 全局热键 共用）
type ToolConfig struct {
	ID                string `json:"id"`                // 稳定唯一标识（编辑/删除用）
	Key               string `json:"key"`               // 内置功能标识（对应工具类型）
	Name              string `json:"name"`              // 显示标题
	Path              string `json:"path"`              // 前端路由，如 /tools/timestamp（与 Event 二选一）
	Event             string `json:"event"`             // 直接触发的事件（如 screenshot），空表示走 Path
	Icon              string `json:"icon"`              // 图标：Arco 图标名（如 camera）或图标库 class（如 icon-lianjie）
	Single            bool   `json:"single"`            // 窗口单例
	Fullscreen        bool   `json:"fullscreen"`        // 新窗口最大化启动
	DisableFullscreen bool   `json:"disableFullscreen"` // 禁用全屏/最大化：隐藏工具窗口标题栏的最大化按钮与“切换全屏”菜单
	Width             int    `json:"width"`             // 视口宽（<=0 继承默认）
	Height            int    `json:"height"`            // 视口高（<=0 继承默认）
	Shortcut          string `json:"shortcut"`          // 全局热键，如 "Ctrl+Alt+S"，空表示未设置
	Quick             bool   `json:"quick"`             // 是否出现在浮动球快捷菜单
	// Home 是否在主页“快捷操作”区直接展示；用指针以区分“未设置”与“显式关闭”。
	// nil（旧配置/默认项）视为主页展示，前端按 home !== false 处理；关闭后仍可在主页“更多”弹窗里找到。
	Home *bool `json:"home,omitempty"`
}

// ToolSettings 单个工具的专属设置（按 ToolConfig.Key 索引，目前仅 mysql 使用）
type ToolSettings struct {
	SQLLogEnabled bool   `json:"sqlLogEnabled"` // 是否记录 SQL 执行日志（手动 SQL 与增删改/DDL）
	SQLLogDir     string `json:"sqlLogDir"`     // SQL 日志目录；空 = 默认（程序目录下 logs）
}

// BrowserSpace 一个浏览器空间。
//
// 空间是本程序浏览器的隔离单位：一个空间 = 一套独立的 WebView2 profile
// （cookie/登录态/缓存互不相通）+ 一套独立的代理参数（出口 IP 互不相通）。
// 同一空间内的多个标签页共享登录态；跨空间不共享。
type BrowserSpace struct {
	ID       string `json:"id"`       // 稳定标识（同时用作 profile 子目录名）
	Name     string `json:"name"`     // 显示名，如"工作""测试"
	Homepage string `json:"homepage"` // 空间首页；空 = 用全局 browser.homepage
	// ProxyMode 代理模式：system / direct / custom；空 = 用全局 browser.proxyMode
	ProxyMode string `json:"proxyMode"`
	// Proxy 代理地址，如 http://127.0.0.1:7890 / socks5://127.0.0.1:1080（ProxyMode=custom 时生效）
	Proxy string `json:"proxy"`
	// ProxyBypass 代理绕过列表（Chrome 语法），如 "localhost,127.0.0.1,*.local"
	ProxyBypass string `json:"proxyBypass"`
	UserAgent   string `json:"userAgent"`  // 自定义 UA；空 = 默认
	ProfileDir  string `json:"profileDir"` // profile 目录；空 = <数据目录>/browser/<id>
	// DisableWebSecurity 关掉同源策略（仅调试空间用，换空间/重启程序后生效）
	DisableWebSecurity bool `json:"disableWebSecurity"`
}

// BrowserConfig 自研浏览器（嵌入式 WebView2 网页区）的全局配置。
//
// 所有字段都在 config.json 里，随整份应用配置一起上传/同步云端：
// 在别的机器上下载回来即可得到同样的空间与代理设置（profile 目录里的登录态
// 属于运行时数据，不进配置）。
type BrowserConfig struct {
	Enabled bool `json:"enabled"` // 是否启用浏览器面板（关掉不影响其他工具）
	// MaxSpaces 空间数量上限（设置页可改）。超限禁止新建并提示。
	MaxSpaces    int            `json:"maxSpaces"`
	Homepage     string         `json:"homepage"`     // 全局首页，空间未单独设置时使用
	SearchURL    string         `json:"searchURL"`    // 地址栏搜索模板，%s 会被替换为查询词
	ProxyMode    string         `json:"proxyMode"`    // 全局默认代理模式：system / direct / custom
	Proxy        string         `json:"proxy"`        // 全局默认代理地址（ProxyMode=custom 时使用）
	ProxyBypass  string         `json:"proxyBypass"`  // 全局默认代理绕过列表
	UserAgent    string         `json:"userAgent"`    // 全局默认 UA
	DefaultSpace string         `json:"defaultSpace"` // 启动时默认打开的空间 ID
	Spaces       []BrowserSpace `json:"spaces"`       // 空间列表（至少一个）
}

// LauncherScopes 快捷搜索的检索范围：勾选哪几类参与检索。
type LauncherScopes struct {
	Apps     bool `json:"apps"`     // 已安装应用（开始菜单 + Program Files 快捷方式）
	Tools    bool `json:"tools"`    // 本程序内置工具（与首页/浮动球同一份列表）
	Windows  bool `json:"windows"`  // 运行中的窗口
	Files    bool `json:"files"`    // 已索引目录下的本地文件
	Commands bool `json:"commands"` // 内置命令、算式求值、URL 直达
}

// LauncherConfig 快捷搜索启动器（Alt+Space 面板）设置。
type LauncherConfig struct {
	// Enabled 总开关。关掉时整套服务都不挂载：不注册全局热键、不建面板窗口、
	// 不扫盘建索引；已开着的窗口也会在保存后被收起。默认关闭，避免用户没启用
	// 就凭空多一个全局热键和一轮启动扫描。
	Enabled    bool           `json:"enabled"`
	Shortcut   string         `json:"shortcut"`   // 全局热键，如 "Alt+Space"
	Scopes     LauncherScopes `json:"scopes"`     // 检索范围
	Dirs       []string       `json:"dirs"`       // 额外参与文件索引的目录（已知目录默认包含）
	MaxEntries int            `json:"maxEntries"` // 文件索引条目上限；<=0 用默认值
}

// DefaultLauncherShortcut 快捷搜索的默认热键。
const DefaultLauncherShortcut = "Alt+Space"

// DefaultLauncherMaxEntries 文件索引默认条目上限：够覆盖个人目录树，
// 又不至于让用户误设一个把整块盘扫下来的值（扫描本身是异步的，但内存与耗时都要有界）。
const DefaultLauncherMaxEntries = 60000

// defaultLauncherConfig 默认配置：关闭状态，但把范围与热键都填好，
// 用户在设置页第一次打开开关就能看到完整可编辑的面板。
func defaultLauncherConfig() *LauncherConfig {
	return &LauncherConfig{
		Enabled:    false,
		Shortcut:   DefaultLauncherShortcut,
		Scopes:     LauncherScopes{Apps: true, Tools: true, Windows: true, Files: true, Commands: true},
		MaxEntries: DefaultLauncherMaxEntries,
	}
}

// LauncherSection 取快捷搜索设置。配置里从未出现过这一段（老配置）时返回默认值，
// 调用方因此永远不用判空。
func LauncherSection(cfg AppConfig) LauncherConfig {
	if cfg.Launcher == nil {
		d := defaultLauncherConfig()
		return *d
	}
	return *cfg.Launcher
}

// LauncherCfg 当前缓存配置里的快捷搜索设置，供 tools/launcher 直接读。
func LauncherCfg() LauncherConfig { return LauncherSection(CurrentConfig()) }

// LauncherEnabled 快捷搜索总开关。false 时宿主不注册热键、不建窗口、不建索引。
func LauncherEnabled() bool { return LauncherCfg().Enabled }

// ArchiveConfig 文件压缩设置（工具页 + 系统右键挂载）。
type ArchiveConfig struct {
	// Enabled 总开关。关掉时整套系统挂载都不存在：不写注册表 verb、不留 SendTo
	// 快捷方式、不起转交用的回环监听。默认关闭 —— 往用户的右键菜单里加东西
	// 必须是他主动要的，不能是程序装好就默默出现的。
	Enabled bool `json:"enabled"`
	// SendTo 子开关：只在 Enabled 为真时有意义，单独控制「发送到 → 压缩 (Tools)…」
	// 那条快捷方式。关掉它不会动注册表里的 verb，只删快捷方式。
	SendTo bool `json:"sendTo"`
	// Format 默认压缩格式（zip / targz）：右键那一下没有界面可选，用的就是它。
	Format string `json:"format"`
	// Level 默认压缩级别 1~9；<=0 用默认值。
	Level int `json:"level"`
}

// GitConfig Git 可视化设置（工作台 + 系统右键挂载）。
type GitConfig struct {
	// ShellEnabled 右键集成总开关。关掉时整套系统挂载都不存在：不写注册表 verb、
	// 不起转交监听。默认关闭 —— 往用户的右键菜单里加东西必须是他主动要的，
	// 不能是程序装好就默默出现的。Git 工作台页本身不受此开关影响（它是普通工具，
	// 一直可打开）；此开关只管「挂进资源管理器右键」这一段。
	ShellEnabled bool `json:"shellEnabled"`
}

// DocSpaceConfig 文档中心设置（分类/版本/回收站的策略参数 + 总开关）。
type DocSpaceConfig struct {
	// Enabled 总开关。关掉时查看器左栏只留「本机浏览」、不出现「文档中心」态；
	// 独立的文档中心工具页仍可打开（入口在工具表，不受此开关隐藏）。
	Enabled bool `json:"enabled"`
	// VersionKeep 每个条目保留的版本快照数上限：存新快照后裁掉最旧的，<=0 视为不裁剪。
	VersionKeep int `json:"versionKeep"`
	// TrashKeepDays 回收站保留天数：移入超过该天数的实体在访问回收站时自动彻底清理，<=0 视为不自动清理。
	TrashKeepDays int `json:"trashKeepDays"`
	// BackupAuto 备份自动调度总开关：关掉时调度协程每轮直接返回，不碰任何计划；
	// 打开后仍只跑 Enabled 且 IntervalS>0 的计划。默认 false：老配置等价于关，
	// 升级不引入隐藏自动行为。
	BackupAuto bool `json:"backupAuto"`
}

// 文档中心默认策略：默认可见（功能已注册且跨平台安全），每条目留 20 版，回收站留 30 天。
const (
	DefaultDocSpaceVersionKeep = 20
	DefaultDocSpaceTrashDays   = 30
)

// defaultDocSpaceConfig 返回带默认策略、默认启用的文档中心设置（自动调度默认关）。
func defaultDocSpaceConfig() *DocSpaceConfig {
	return &DocSpaceConfig{Enabled: true, VersionKeep: DefaultDocSpaceVersionKeep, TrashKeepDays: DefaultDocSpaceTrashDays, BackupAuto: false}
}

// DocSpaceSection 取文档中心设置。配置里从未出现过这一段（老配置）时返回默认值，
// 调用方因此永远不用判空。
func DocSpaceSection(cfg AppConfig) DocSpaceConfig {
	if cfg.DocSpace == nil {
		return *defaultDocSpaceConfig()
	}
	return *cfg.DocSpace
}

// DocSpaceCfg 当前缓存配置里的文档中心设置，供 services/app 绑定层读取。
func DocSpaceCfg() DocSpaceConfig { return DocSpaceSection(CurrentConfig()) }

// normalizeDocSpaceConfig 补齐文档中心设置的合法范围（保留数/天数非正回落默认，不覆盖用户填的合法值）。
func normalizeDocSpaceConfig(cfg *AppConfig) {
	d := cfg.DocSpace
	if d == nil {
		return
	}
	if d.VersionKeep < 0 {
		d.VersionKeep = DefaultDocSpaceVersionKeep
	}
	if d.VersionKeep > 1000 {
		d.VersionKeep = 1000
	}
	if d.TrashKeepDays < 0 {
		d.TrashKeepDays = DefaultDocSpaceTrashDays
	}
	if d.TrashKeepDays > 3650 {
		d.TrashKeepDays = 3650
	}
}

// CloudConfig 云盘管理设置（计划-云盘管理工具 PR1）。
// 敏感 token/BDUSS **不进这里**，只进 tools/secret 加密的账号表；此处只放
// OAuth 应用凭证与落地目录等非运行期密钥的配置项。
type CloudConfig struct {
	// Enabled 总开关。关掉时前端隐藏「云盘管理」入口、不构造任何 provider。
	// 默认关闭：联网功能必须用户主动开，不能在老配置里凭空多出网络行为。
	Enabled bool `json:"enabled"`
	// GoogleClientID / GoogleClientSecret 用户自备的 Google Cloud OAuth 客户端凭证
	// （不内置，避免密钥随程序泄露/轮换困难）。留空则无法完成 Google 授权。
	GoogleClientID     string `json:"googleClientId"`
	GoogleClientSecret string `json:"googleClientSecret"`
	// GoogleRedirectURI OAuth 授权回调地址，须与 Google 控制台登记的一致。默认本机
	// 回环：桌面/无头都走「用户粘贴回跳 URL」手工换 token，不依赖系统浏览器。
	GoogleRedirectURI string `json:"googleRedirectUri"`
	// SaveDir 云盘文件下载落地目录；留空走 appdirs.Data("clouddrive")/downloads。
	SaveDir string `json:"saveDir"`
	// Concurrency 跨盘转存并发度（<=0 用默认）。本期 PR1 只做单连接，留给后续 PR。
	Concurrency int `json:"concurrency"`
}

// DefaultCloudRedirectURI 默认 OAuth 回调：本机回环，用户在浏览器授权后把
// 回跳 URL 粘回来换 token（桌面与无头面板同一套做法，不靠系统浏览器自动回调）。
const DefaultCloudRedirectURI = "http://localhost:8458/callback"

// defaultCloudConfig 云盘默认设置：默认关闭、回调填好、并发取 3。
func defaultCloudConfig() *CloudConfig {
	return &CloudConfig{Enabled: false, GoogleRedirectURI: DefaultCloudRedirectURI, Concurrency: 3}
}

// CloudSection 取云盘设置。老配置从未有这一段时返回默认值，调用方不用判空。
func CloudSection(cfg AppConfig) CloudConfig {
	if cfg.Cloud == nil {
		return *defaultCloudConfig()
	}
	return *cfg.Cloud
}

// CloudCfg 当前缓存配置里的云盘设置，供 tools/clouddrive 与绑定层读取。
func CloudCfg() CloudConfig { return CloudSection(CurrentConfig()) }

// normalizeCloudConfig 补齐云盘设置合法范围：并发非正回落默认，回调留空回落默认。
func normalizeCloudConfig(cfg *AppConfig) {
	c := cfg.Cloud
	if c == nil {
		return
	}
	if c.Concurrency <= 0 {
		c.Concurrency = 3
	}
	if c.Concurrency > 16 {
		c.Concurrency = 16
	}
	if c.GoogleRedirectURI == "" {
		c.GoogleRedirectURI = DefaultCloudRedirectURI
	}
}

// ProxyItem 共享代理池里的一条代理。ID 稳定供前端 diff，URL 直接喂引擎/浏览器的
// --proxy-server（http/https；socks5 视依赖树，本期不保证）。
type ProxyItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// MirrorItem 常用镜像模板（新建任务时的候选，手动选，不自动切）。
type MirrorItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// DownloadConfig 下载管理器设置（计划-下载管理器 PR3）。引擎级护栏参数 + 全局
// 落地目录 + 共享代理池 / 镜像池。代理 URL 可能内嵌凭据，但这是用户本机配置文件、
// 与 Browser.Proxy 同级（明文落 config.json），不额外加密，保持一致。
type DownloadConfig struct {
	// Enabled 总开关。关掉时前端隐藏「下载管理」入口、不初始化引擎参数。
	// 默认开启：下载是本地核心能力（无网络自发行为），不像云盘需用户主动开。
	Enabled bool `json:"enabled"`
	// SaveDir 全局默认下载目录；留空走 appdirs.Downloads()。改后新建任务即时生效。
	SaveDir string `json:"saveDir"`
	// MaxRunningTasks 同时下载的任务数上限，超了排队（<=0 回落默认）。
	MaxRunningTasks int `json:"maxRunningTasks"`
	// MaxTotalParts 全局并发块数上限（信号量容量，<=0 回落默认）。
	MaxTotalParts int `json:"maxTotalParts"`
	// DefaultConnections 单任务默认分块数（<=0 回落默认）。
	DefaultConnections int `json:"defaultConnections"`
	// GlobalSpeedLimitBps 全局下行限速（字节/秒），0=不限。
	GlobalSpeedLimitBps int64 `json:"globalSpeedLimitBps"`
	// ProxyPool 共享代理池：下载选源与浏览器 custom 代理都从这里挑，减少重复录入。
	ProxyPool []ProxyItem `json:"proxyPool"`
	// Mirrors 常用镜像模板（新建任务候选）。
	Mirrors []MirrorItem `json:"mirrors"`
}

// 下载引擎默认护栏：与 tools/downloader 内部默认对齐（两端各写一次，靠注释锁定）。
const (
	defaultDownloadMaxRunning  = 3
	defaultDownloadMaxParts    = 32
	defaultDownloadConnections = 4
)

// defaultDownloadConfig 下载默认设置：开启、护栏取默认、池为空。
func defaultDownloadConfig() *DownloadConfig {
	return &DownloadConfig{
		Enabled:            true,
		MaxRunningTasks:    defaultDownloadMaxRunning,
		MaxTotalParts:      defaultDownloadMaxParts,
		DefaultConnections: defaultDownloadConnections,
	}
}

// DownloadSection 取下载设置。老配置从未有这一段时返回默认值，调用方不用判空。
func DownloadSection(cfg AppConfig) DownloadConfig {
	if cfg.Download == nil {
		return *defaultDownloadConfig()
	}
	return *cfg.Download
}

// DownloadCfg 当前缓存配置里的下载设置，供 tools/downloader 与绑定层读取。
func DownloadCfg() DownloadConfig { return DownloadSection(CurrentConfig()) }

// normalizeDownloadConfig 补齐下载设置合法范围：护栏非正回落默认，限速负值归零。
func normalizeDownloadConfig(cfg *AppConfig) {
	d := cfg.Download
	if d == nil {
		return
	}
	if d.MaxRunningTasks <= 0 {
		d.MaxRunningTasks = defaultDownloadMaxRunning
	}
	if d.MaxTotalParts <= 0 {
		d.MaxTotalParts = defaultDownloadMaxParts
	}
	if d.DefaultConnections <= 0 {
		d.DefaultConnections = defaultDownloadConnections
	}
	if d.GlobalSpeedLimitBps < 0 {
		d.GlobalSpeedLimitBps = 0
	}
}

// LanShareConfig 局域网互传设置（计划-SSH管理与局域网互传 P2 收尾 + 接收优化 §3.2）。
// BlockExt 是接收侧类型策略；Port/Host/AutoStopMin/PIN 是启动默认值：
// 运行期仍以互传页的启动入参为准，这里只作设置页分区落盘的预填来源。
type LanShareConfig struct {
	// BlockExt 收件箱拒收的扩展名黑名单（如 "exe"/".ps1"）。空 = 全类型全收，
	// 契合「默认全类型所有文件」；用户可「一键填入常见危险类型」找回安全默认。
	// 落盘/读取前由 normalizeLanShareConfig 统一成小写带前导点、去重去空。
	BlockExt []string `json:"blockExt,omitempty"`
	// Port 启动默认端口；<=0 取 lanshare 内置默认（占用自动 +1 重试照旧）。
	Port int `json:"port,omitempty"`
	// Host 启动默认监听地址：空=全部网卡，"127.0.0.1"=仅本机。
	Host string `json:"host,omitempty"`
	// AutoStopMin 到点自停分钟数；<=0 不自动停。默认沿用页内旧值 120。
	AutoStopMin int `json:"autoStopMin,omitempty"`
	// Pin 访问 PIN 默认值，空=免密。明文落 config.json（与 Download 代理池
	// 同口径：本机用户配置文件，不额外加密）；文档已注明 PIN 仅挡无意访问。
	Pin string `json:"pin,omitempty"`
}

// defaultLanShareConfig 互传默认设置：黑名单为空 = 全类型全收；
// 启动默认值沿用互传页旧写法：默认端口、全部网卡、120 分钟后自停、免密。
func defaultLanShareConfig() *LanShareConfig {
	return &LanShareConfig{AutoStopMin: 120}
}

// LanShareSection 取互传接收设置。老配置从未有这一段时返回默认值（空黑名单），调用方不用判空。
func LanShareSection(cfg AppConfig) LanShareConfig {
	if cfg.LanShare == nil {
		return *defaultLanShareConfig()
	}
	return *cfg.LanShare
}

// LanShareCfg 当前缓存配置里的互传接收设置，供 services/app 绑定层读取回灌。
func LanShareCfg() LanShareConfig { return LanShareSection(CurrentConfig()) }

// normalizeLanShareConfig 清洗互传设置：黑名单每项去空、小写、补前导点、去重；
// 端口越界回落 0（=用内置默认），autoStopMin 负值回落默认 120，host 去空白。
// nil 段跳过（调用方走 LanShareSection 拿默认值）；不覆盖用户填的合法值。
func normalizeLanShareConfig(cfg *AppConfig) {
	l := cfg.LanShare
	if l == nil {
		return
	}
	if l.Port < 0 || l.Port > 65535 {
		l.Port = 0
	}
	if l.AutoStopMin < 0 {
		l.AutoStopMin = 120
	}
	l.Host = strings.TrimSpace(l.Host)
	seen := map[string]bool{}
	out := make([]string, 0, len(l.BlockExt))
	for _, e := range l.BlockExt {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || e == "." {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		if seen[e] {
			continue
		}
		seen[e] = true
		out = append(out, e)
	}
	l.BlockExt = out
}

/* ==================== 数据保存位置（本地 / MySQL / Redis 外存副本） ==================== */

// DefaultStorageDatabase MySQL 外存库默认名（不存在时自动 CREATE DATABASE IF NOT EXISTS）。
const DefaultStorageDatabase = "tools_data"

// DefaultStorageIntervalSec 受管文档变更扫描的默认间隔秒数。
const DefaultStorageIntervalSec = 5

// MaxStorageIntervalSec 间隔秒上限：外存副本是备份类工作，不该有人拿它当高频同步用，
// 也顺手挡住设置页被填出一个吃掉 CPU 的取值。
const MaxStorageIntervalSec = 3600

// StorageConfig 数据保存位置设置：决定「我们自己的配置数据」除了本地文件之外，
// 还往哪里再写一份持久副本。语义见 docs/plan/计划-数据保存位置与外存副本-20261111.md：
//   - 本地永远是权威可读源（各服务存量代码直接 os.ReadFile，不改它们）；
//   - Local=true（默认）：本地同时算作受管保存位置，远端只是副本，远端不可用只标脏、不打扰；
//   - Local=false：本地降级为临时工作缓存，远端才是保存位置——启动时远端优先回写本地，
//     远端不可达则由前端弹窗让用户「启动服务」或「本次使用默认数据」；
//   - 这里只存实例 ID 与库坐标，真正的凭据仍在各服务自己的实例登记表里，
//     这一段本身不含任何密钥，可以放心落 config.json 并随配置导出/导入。
type StorageConfig struct {
	// Local 是否把本地文件算作保存位置，默认 true。
	Local bool `json:"local"`
	// MySQL 是否把副本写入一个 MySQL 实例，默认 false。
	MySQL bool `json:"mysql"`
	// Redis 是否把副本写入一个 Redis 实例，默认 false。
	Redis bool `json:"redis"`
	// MySQLInstance 绑定的 MySQL 实例 ID（tools/mysql 实例登记表里的 ID）。
	MySQLInstance string `json:"mysqlInstance,omitempty"`
	// MySQLDatabase 外存库名，留空取 DefaultStorageDatabase；只接受 [A-Za-z0-9_]{1,64}。
	MySQLDatabase string `json:"mysqlDatabase,omitempty"`
	// RedisInstance 绑定的 Redis 实例 ID。
	RedisInstance string `json:"redisInstance,omitempty"`
	// RedisDB Redis 库序号（命令侧的 SELECT n），0~15，越界回落 0。
	RedisDB int `json:"redisDb,omitempty"`
	// IncludeSensitive 勾选后把「含凭据」那一组文档也外存：写入前走信封加密。
	// 默认 false —— 主密钥只在本机，换机器解不开，跨机恢复从来不包括这一组。
	IncludeSensitive bool `json:"includeSensitive,omitempty"`
	// Exclude 显式排除的文档键（形如 mysql/mysql-instances.json），大小写敏感。
	Exclude []string `json:"exclude,omitempty"`
	// IntervalSec 变更扫描间隔秒；<=0 用 DefaultStorageIntervalSec。
	IntervalSec int `json:"intervalSec,omitempty"`
}

// defaultStorageConfig 默认设置：只勾本地、远端全关。老配置文件里根本没有这一段时
// 读回来是 nil，序列化随之省略，不会凭空给用户加一段他没写过的东西。
func defaultStorageConfig() *StorageConfig { return &StorageConfig{Local: true} }

// StorageSection 取数据保存位置设置。配置里从未出现过这一段时返回默认值，调用方不用判空。
func StorageSection(cfg AppConfig) StorageConfig {
	if cfg.Storage == nil {
		s := *defaultStorageConfig()
		normalizeStorage(&s)
		return s
	}
	s := *cfg.Storage
	normalizeStorage(&s)
	return s
}

// StorageCfg 当前缓存配置里的数据保存位置设置，供 services/datastore 读取。
func StorageCfg() StorageConfig { return StorageSection(CurrentConfig()) }

// StorageIdentOK 判断 MySQL 库名是否是可安全拼进 DDL 的标识符。库名是整条链路里
// 唯一必须由我们自己拼进 SQL 文本的部分（表名、列名都是写死的），所以卡得比正则
// 白名单更严也不为过：只许字母数字下划线、不以数字开头、长度 1~64。
func StorageIdentOK(s string) bool {
	if len(s) == 0 || len(s) > 64 || s[0] < 'A' || s[0] > 'z' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' {
			continue
		}
		return false
	}
	return true
}

// normalizeStorage 清洗数据保存位置设置：库名去空白、非法或留空回落默认库；
// Redis 库序号越界回落 0；间隔秒非法回落默认；排除项归一成 slash 相对路径并去重去空；
// 一个远端都没勾时把 Redis/MySQL 的绑定留着（用户可能只是暂时关掉，下次开还回到原绑定）。
func normalizeStorage(s *StorageConfig) {
	s.MySQLDatabase = strings.TrimSpace(s.MySQLDatabase)
	if s.MySQLDatabase == "" || !StorageIdentOK(s.MySQLDatabase) {
		s.MySQLDatabase = DefaultStorageDatabase
	}
	s.MySQLInstance = strings.TrimSpace(s.MySQLInstance)
	s.RedisInstance = strings.TrimSpace(s.RedisInstance)
	if s.RedisDB < 0 || s.RedisDB > 15 {
		s.RedisDB = 0
	}
	if s.IntervalSec <= 0 {
		s.IntervalSec = DefaultStorageIntervalSec
	} else if s.IntervalSec > MaxStorageIntervalSec {
		s.IntervalSec = MaxStorageIntervalSec
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(s.Exclude))
	for _, k := range s.Exclude {
		k = strings.Trim(strings.ReplaceAll(strings.TrimSpace(k), "\\", "/"), "/")
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	s.Exclude = out
}

// normalizeStorageConfig 清洗 AppConfig 里的存储段：nil 段跳过（调用方走 StorageSection 拿默认值）。
func normalizeStorageConfig(cfg *AppConfig) {
	if cfg.Storage == nil {
		return
	}
	normalizeStorage(cfg.Storage)
}

// DefaultArchiveFormat 默认压缩格式。两个短名与 sysx.ArchiveZip / ArchiveTarGz
// 对齐；本包不导那个包（应用配置层保持零依赖，字面量在两端各定一次、靠单测锁住）。
const DefaultArchiveFormat = "zip"

// ArchiveFormatTarGz tar.gz 的短名，与 normalizeArchiveConfig 的归一目标同一处写出。
const ArchiveFormatTarGz = "targz"

// DefaultArchiveLevel 默认压缩级别：与标准库 compress/flate 的默认值一致，
// 快与压两者都不极端。
const DefaultArchiveLevel = 6

// defaultArchiveConfig 默认配置：总开关关掉，但格式与级别都填好，
// 用户第一次打开设置页就能看到完整可编辑的面板（与 launcher 同一套做法）。
func defaultArchiveConfig() *ArchiveConfig {
	return &ArchiveConfig{
		Enabled: false,
		SendTo:  false,
		Format:  DefaultArchiveFormat,
		Level:   DefaultArchiveLevel,
	}
}

// ArchiveSection 取文件压缩设置。配置里从未出现过这一段（老配置）时返回默认值，
// 调用方因此永远不用判空。
func ArchiveSection(cfg AppConfig) ArchiveConfig {
	if cfg.Archive == nil {
		return *defaultArchiveConfig()
	}
	return *cfg.Archive
}

// ArchiveCfg 当前缓存配置里的文件压缩设置，供 tools/archive 直接读。
func ArchiveCfg() ArchiveConfig { return ArchiveSection(CurrentConfig()) }

// ArchiveEnabled 文件压缩总开关。false 时宿主不挂右键、不起监听。
func ArchiveEnabled() bool { return ArchiveCfg().Enabled }

// defaultGitConfig Git 设置默认值：右键集成默认关（不往用户菜单里默默加东西）。
func defaultGitConfig() *GitConfig {
	return &GitConfig{ShellEnabled: false}
}

// GitSection 取 Git 设置。配置里从未出现过这一段（老配置）时返回默认值，
// 调用方因此永远不用判空。
func GitSection(cfg AppConfig) GitConfig {
	if cfg.Git == nil {
		return *defaultGitConfig()
	}
	return *cfg.Git
}

// GitCfg 当前缓存配置里的 Git 设置，供 tools/gitcmd 直接读。
func GitCfg() GitConfig { return GitSection(CurrentConfig()) }

// GitShellEnabled Git 右键集成总开关。false 时宿主不挂 verb、不起转交监听。
func GitShellEnabled() bool { return GitCfg().ShellEnabled }

// normalizeArchiveConfig 补齐文件压缩设置：格式合法性、级别范围。
// 全是 idempotent 修正，不会覆盖用户填的值；手改 config.json 把这一段删掉时
// （读回来是 nil）什么都不做，调用方走 ArchiveSection 拿到默认值。
func normalizeArchiveConfig(cfg *AppConfig) {
	a := cfg.Archive
	if a == nil {
		return
	}
	// 格式收到就归一成两个短名：“tar.gz”“tgz”这些同义词进不了下游，
	// 因为 archive.ExtFor 只认 sysx.ArchiveTarGz 那一个字，认不出的会默默落到 .zip
	switch v := strings.ToLower(strings.TrimSpace(a.Format)); v {
	case "zip", "":
		a.Format = DefaultArchiveFormat
	case "targz", "tar.gz", "tgz", "gz":
		a.Format = ArchiveFormatTarGz
	default:
		a.Format = DefaultArchiveFormat
	}
	if a.Level <= 0 {
		a.Level = DefaultArchiveLevel
	}
	if a.Level > 9 {
		a.Level = 9
	}
}

// normalizeLauncherConfig 补齐快捷搜索设置：热键兜底、上限兜范围、目录去空去重。
// 全是 idempotent 修正，不会覆盖用户填的值；手改 config.json 把这一段删掉时
// （读回来是 nil）什么都不做，调用方走 LauncherSection 拿到默认值。
func normalizeLauncherConfig(cfg *AppConfig) {
	l := cfg.Launcher
	if l == nil {
		return
	}
	l.Shortcut = strings.TrimSpace(l.Shortcut)
	if l.Shortcut == "" {
		l.Shortcut = DefaultLauncherShortcut
	}
	if l.MaxEntries <= 0 {
		l.MaxEntries = DefaultLauncherMaxEntries
	}
	seen := map[string]bool{}
	dirs := make([]string, 0, len(l.Dirs))
	for _, d := range l.Dirs {
		d = strings.TrimSpace(d)
		if d == "" || seen[strings.ToLower(d)] {
			continue
		}
		seen[strings.ToLower(d)] = true
		dirs = append(dirs, d)
	}
	l.Dirs = dirs
}

// AppConfig 应用总配置
type AppConfig struct {
	Log     LogConfig     `json:"log"`
	Ball    BallConfig    `json:"ball"`
	General GeneralConfig `json:"general"`
	Paths   PathsConfig   `json:"paths"`
	Browser BrowserConfig `json:"browser"`
	Tools   []ToolConfig  `json:"tools"`
	// ToolsDefault 用户「保存为默认」的功能列表快照：设置页「恢复默认列表」优先还原到这份，
	// 没存过（nil）才回落到内置基线。随整份配置导出/导入。
	ToolsDefault []ToolConfig            `json:"toolsDefault,omitempty"`
	ToolSettings map[string]ToolSettings `json:"toolSettings"` // 工具级设置，键为工具 Key（如 mysql）
	// Order 各列表的拖拽自定义顺序：键为“面”标识（如 php:list / nginx:list / home:tools），
	// 值为该面内条目 ID 的有序列表。主页与各服务列表各用独立键，互不共用。随整份配置导出/导入。
	Order map[string][]string `json:"order"`
	// Launcher 快捷搜索启动器设置。指针类型：老配置文件里没有这一段，读回来就是 nil，
	// 序列化时也随之省略（omitempty），不会凭空对象污染用户手写的配置。
	// 关掉时不挂热键、不建窗口、不建索引，见 LauncherConfig.Enabled。
	Launcher *LauncherConfig `json:"launcher,omitempty"`
	// Archive 文件压缩设置。同样是指针类型：老配置里没有这一段时读回来是 nil，
	// 序列化也随之省略，不会凭空给用户的 config.json 添一段他没写过的东西。
	// 关掉时不写注册表、不留 SendTo 快捷方式、不起转交监听，见 ArchiveConfig.Enabled。
	Archive *ArchiveConfig `json:"archive,omitempty"`
	// Git Git 可视化设置。指针类型：老配置里没有这一段时读回来是 nil，序列化随之省略。
	// 关掉时不写注册表 verb、不起转交监听，见 GitConfig.ShellEnabled。
	Git *GitConfig `json:"git,omitempty"`
	// DocSpace 文档中心设置。同样是指针类型：老配置里没有这一段时读回来是 nil，
	// 序列化也随之省略。见 DocSpaceConfig 各字段。
	DocSpace *DocSpaceConfig `json:"docSpace,omitempty"`
	// Cloud 云盘管理设置。指针类型：老配置里没有这一段时读回来是 nil，序列化随之
	// 省略。见 CloudConfig 各字段（token 不进这里，只进加密账号表）。
	Cloud *CloudConfig `json:"cloud,omitempty"`
	// Download 下载管理器设置。指针类型：老配置里没有这一段时读回来是 nil，序列化
	// 随之省略。见 DownloadConfig 各字段（护栏参数 + 全局目录 + 共享代理/镜像池）。
	Download *DownloadConfig `json:"download,omitempty"`
	// LanShare 局域网互传接收设置。同样是指针类型：老配置里没有这一段时读回来是 nil，
	// 序列化随之省略。见 LanShareConfig（接收类型黑名单，默认空=全收）。
	LanShare *LanShareConfig `json:"lanShare,omitempty"`
	// Storage 数据保存位置设置（本地 / MySQL / Redis 外存副本）。同样是指针类型：
	// 老配置里没有这一段时读回来是 nil，序列化随之省略。见 StorageConfig 各字段。
	Storage *StorageConfig `json:"storage,omitempty"`
}

// DefaultMaxSpaces 浏览器空间数量默认上限。
const DefaultMaxSpaces = 5

// defaultBrowserConfig 浏览器默认配置：一个跟随系统代理的默认空间。
// 不预置具体代理地址，避免用户不知情地把流量导到不存在的端口上。
func defaultBrowserConfig() BrowserConfig {
	return BrowserConfig{
		Enabled:      true,
		MaxSpaces:    DefaultMaxSpaces,
		Homepage:     "about:blank",
		SearchURL:    "https://www.bing.com/search?q=%s",
		ProxyMode:    "system",
		DefaultSpace: "default",
		Spaces: []BrowserSpace{
			{ID: "default", Name: "默认空间"},
		},
	}
}

var (
	appCfgMu      sync.RWMutex
	appCfgWriteMu sync.Mutex // 串行化整文件写入，避免并发保存（前端保存设置/拖动浮动球）互相覆盖
	appCfg        AppConfig
	appCfgSet     bool
)

// DefaultConfig 返回带默认日志设置与默认工具列表的配置。
func DefaultConfig() AppConfig {
	return AppConfig{
		Log:      LogConfig{Enabled: true, Level: "info", Path: ""},
		Ball:     BallConfig{Enabled: true, HideAllShortcut: "Ctrl+Alt+H", DockEdge: dockOn()},
		General:  GeneralConfig{AutoStart: false, AppName: "Tools", AppLogo: "", Theme: defaultThemeConfig()},
		Browser:  defaultBrowserConfig(),
		Launcher: defaultLauncherConfig(),
		Archive:  defaultArchiveConfig(),
		Git:      defaultGitConfig(),
		DocSpace: defaultDocSpaceConfig(),
		Cloud:    defaultCloudConfig(),
		Download: defaultDownloadConfig(),
		LanShare: defaultLanShareConfig(),
		Storage:  defaultStorageConfig(),
		ToolSettings: map[string]ToolSettings{
			// MySQL：默认开启 SQL 日志，目录留空走 DefaultSQLLogDir
			"mysql": {SQLLogEnabled: true},
		},
		Tools: []ToolConfig{
			{ID: "docker", Key: "docker", Name: "Docker管理", Path: "/tools/docker", Icon: "icon-docker", Single: true, Width: 1360, Height: 860},
			{ID: "browser", Key: "browser", Name: "浏览器", Path: "/tools/browser", Icon: "icon-liulanqi1", Single: true, Fullscreen: true},
			{ID: "speedtest", Key: "speedtest", Name: "网速测速", Path: "/tools/speedtest", Icon: "icon-cesu", Single: true, Width: 1080, Height: 720},
			{ID: "term", Key: "term", Name: "终端连接", Path: "/tools/term", Icon: "icon-terminal", Single: true, Width: 1200, Height: 800},
			{ID: "disk", Key: "disk", Name: "磁盘管理", Path: "/tools/disk", Icon: "icon-yingpan", Single: true, Width: 1360, Height: 860},
			{ID: "siteclone", Key: "siteclone", Name: "仿站工具", Path: "/tools/siteclone", Icon: "download", Single: true, Width: 1120, Height: 780, Home: homeOff()},
			{ID: "guard", Key: "guard", Name: "进程守护", Path: "/tools/guard", Icon: "apps", Single: true, Width: 1120, Height: 720, Home: homeOff()},
			{ID: "ftp", Key: "ftp", Name: "文件传输", Path: "/tools/ftp", Icon: "icon-ftp", Single: true, Width: 1360, Height: 860},
			{ID: "redis", Key: "redis", Name: "Redis管理", Path: "/tools/redis", Icon: "icon-icon-redis", Single: true, Width: 1360, Height: 860},
			{ID: "mysql", Key: "mysql", Name: "MySQL管理", Path: "/tools/mysql", Icon: "icon-MySQL", Single: true, Width: 1360, Height: 860},
			{ID: "php", Key: "php", Name: "PHP管理", Path: "/tools/php", Icon: "icon-php", Single: true, Width: 1360, Height: 860},
			{ID: "nginx", Key: "nginx", Name: "Nginx管理", Path: "/tools/nginx", Icon: "icon-ngnix", Single: true, Width: 1360, Height: 860},
			{ID: "npm", Key: "npm", Name: "Npm管理", Path: "/tools/npm", Icon: "icon-npm", Single: true, Width: 1360, Height: 860},
			{ID: "sniffer", Key: "sniffer", Name: "抓包工具", Path: "/tools/sniffer", Icon: "bug", Single: true, Fullscreen: true},
			{ID: "websocket", Key: "websocket", Name: "WebSocket调试", Path: "/tools/websocket", Icon: "icon-wangluo1", Single: true, Fullscreen: true, Width: 1360, Height: 860},
			{ID: "timestamp", Key: "timestamp", Name: "时间戳转换", Path: "/tools/timestamp", Icon: "icon-riqishijianzhuanhuanmiaoshu"},
			{ID: "usbserial", Key: "usbserial", Name: "USB调试", Path: "/tools/usbserial", Icon: "icon-usb1", Single: true, Width: 1360, Height: 860},
			{ID: "video", Key: "video", Name: "视频录制", Path: "/tools/video", Icon: "video-camera", Single: true, Fullscreen: true, Home: homeOff()},
			{ID: "screenshot", Key: "screenshot", Name: "截图", Event: "screenshot", Icon: "icon-jietu", Shortcut: "Ctrl+Alt+A", Quick: true},
			{ID: "colorpicker", Key: "colorpicker", Name: "颜色取色器", Path: "/tools/colorpicker", Icon: "icon-quseqi", Single: true, DisableFullscreen: true, Width: 480, Height: 276, Quick: true},
			{ID: "encdec", Key: "encdec", Name: "文字加密解密", Path: "/tools/encdec", Icon: "lock", Home: homeOff()},
			{ID: "strrandom", Key: "strrandom", Name: "随机字符串生成", Path: "/tools/strrandom", Icon: "icon-zifuchuan", Home: homeOff()},
			{ID: "qrcode", Key: "qrcode", Name: "二维码生成", Path: "/tools/qrcode", Icon: "icon-erweima3", Home: homeOff()},
			{ID: "hexconvert", Key: "hexconvert", Name: "进制转换", Path: "/tools/hexconvert", Icon: "swap", Home: homeOff()},
			{ID: "archive", Key: "archive", Name: "文件压缩", Path: "/tools/archive", Icon: "archive", Single: true, Width: 1120, Height: 760, Home: homeOff()},
			{ID: "download", Key: "download", Name: "下载管理", Path: "/tools/download", Icon: "icon-xiazai", Single: true, Width: 1280, Height: 820, Home: homeOff()},
			{ID: "docspace", Key: "docspace", Name: "文档中心", Path: "/tools/docspace", Icon: "folder", Single: true, Width: 1280, Height: 820, Home: homeOff()},
			{ID: "note", Key: "note", Name: "笔记", Path: "/tools/note", Icon: "icon-book", Single: true, Width: 1200, Height: 800, Home: homeOff()},
			{ID: "lanshare", Key: "lanshare", Name: "局域网互传", Path: "/tools/lanshare", Icon: "icon-wangluo1", Single: true, Width: 1280, Height: 820, Home: homeOff()},
			{ID: "clouddrive", Key: "clouddrive", Name: "云盘管理", Path: "/tools/clouddrive", Icon: "icon-yun", Single: true, Width: 1320, Height: 860, Home: homeOff()},
			{ID: "git", Key: "git", Name: "Git管理", Path: "/tools/git", Icon: "code", Single: true, Width: 1360, Height: 860, Home: homeOff()},
		},
	}
}

// homeOff 把默认工具标成“不在主页快捷操作区直接展示”：
// Home 是指针型，nil 被当成“未设置 = 展示”，要表达关闭只能显式给个 false。
func homeOff() *bool {
	f := false
	return &f
}

// dockOn 给 DockEdge 这类“nil = 开启”的指针型开关造一个 true：
// 与 homeOff 同套路，存量配置缺字段时由 normalizeConfig 兜成默认开启。
func dockOn() *bool {
	t := true
	return &t
}

// ApplogConfig 把配置里的 LogConfig 转成 applog 自己的配置类型，使日志包不反向依赖应用层。
func ApplogConfig(l LogConfig) applog.Config {
	return applog.Config{Enabled: l.Enabled, Level: l.Level, Path: l.Path}
}

// ConfigFilePath 导出版：用户配置目录里 config.json 的绝对路径。
// 「数据保存位置」把它当成一份受管文档看待，需要与内部写入用的是同一个来源。
func ConfigFilePath() (string, error) { return configFilePath() }

// configFilePath 返回配置文件路径，必要时创建目录。
func configFilePath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		home, herr := os.UserHomeDir()
		if herr != nil {
			return "", herr
		}
		base = filepath.Join(home, ".config")
	}
	dir := filepath.Join(base, "Tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// configDirMigrateOnce 保证配置目录改名迁移在进程内只尝试一次。
var configDirMigrateOnce sync.Once

// migrateConfigDir 一次性迁移：项目早期配置目录名为 go-tools，现统一为 Tools。
// 若旧目录存在且新目录尚未创建，则整体改名过去，保住 config.json 与其下各服务数据（ball_icon/日志/面板配置等）。
// 迁移失败则退回使用全新的 Tools 目录（相当于新装，老配置读不到）。
func migrateConfigDir() {
	configDirMigrateOnce.Do(func() {
		base, err := os.UserConfigDir()
		if err != nil {
			return
		}
		oldDir := filepath.Join(base, "go-tools")
		newDir := filepath.Join(base, "Tools")
		if _, err := os.Stat(newDir); err == nil {
			return // 新目录已存在：已迁移过或本就是新装
		}
		if _, err := os.Stat(oldDir); err != nil {
			return // 无旧目录，无需迁移
		}
		if err := os.Rename(oldDir, newDir); err != nil {
			applog.Warnf("config", "配置目录迁移失败（go-tools→Tools），将使用全新目录: %v", err)
			return
		}
		applog.Infof("config", "配置目录已迁移: %s → %s", oldDir, newDir)
	})
}

// ConfigDir 返回配置文件所在目录（不存在则创建）。
func ConfigDir() (string, error) {
	p, err := configFilePath()
	if err != nil {
		return "", err
	}
	return filepath.Dir(p), nil
}

// BallIconMaskPath 浮动球球心图标的 alpha 遮罩 PNG 路径（与 config.json 同目录）：
// 前端把选中图标渲染成黑色字形遮罩上传，Go 侧只取 alpha 通道按主题文字色着色。
func BallIconMaskPath() (string, error) {
	p, err := configFilePath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "ball_icon.png"), nil
}

// InitAppDirs 应用全局目录配置并预建服务子目录（启动与保存配置时调用，幂等）。
// 布局本身的解析与迁移规则都在 appdirs 包内（appdirs/layout.go）。
func InitAppDirs(cfg AppConfig) {
	if err := appdirs.ApplyLayout(cfg.Paths.Plugins, cfg.Paths.Data, cfg.Paths.Logs, cfg.Paths.Downloads); err != nil {
		applog.Errorf("paths", "创建全局目录失败: %v", err)
	}
}

// normalizeBrowserConfig 补齐浏览器配置：至少保留一个空间、ID 唯一、
// 默认空间/首页/搜索模板非空。全部是幂等的修正，不会覆盖用户填的值。
func normalizeBrowserConfig(b *BrowserConfig) {
	if strings.TrimSpace(b.Homepage) == "" {
		b.Homepage = "about:blank"
	}
	if strings.TrimSpace(b.SearchURL) == "" {
		b.SearchURL = "https://www.bing.com/search?q=%s"
	}
	b.MaxSpaces = normalizeMaxSpaces(b.MaxSpaces)
	b.ProxyMode = normalizeProxyMode(b.ProxyMode)
	b.Proxy = strings.TrimSpace(b.Proxy)
	b.ProxyBypass = strings.TrimSpace(b.ProxyBypass)
	b.UserAgent = strings.TrimSpace(b.UserAgent)
	if b.ProxyMode == "custom" && b.Proxy == "" {
		// 选了自定义代理却没填地址：回落到跟随系统，别把流量导到空代理上
		b.ProxyMode = "system"
	}

	seen := make(map[string]bool, len(b.Spaces))
	out := make([]BrowserSpace, 0, len(b.Spaces))
	for i := range b.Spaces {
		sp := b.Spaces[i]
		sp.ID = strings.TrimSpace(sp.ID)
		if sp.ID == "" {
			sp.ID = "space" + strconv.Itoa(i+1)
		}
		if seen[sp.ID] {
			// 重复 ID 会让两个空间共用一个 profile，必须拆开
			sp.ID = sp.ID + "-" + strconv.Itoa(i+1)
		}
		seen[sp.ID] = true
		sp.Name = strings.TrimSpace(sp.Name)
		if sp.Name == "" {
			sp.Name = sp.ID
		}
		sp.Proxy = strings.TrimSpace(sp.Proxy)
		sp.ProxyBypass = strings.TrimSpace(sp.ProxyBypass)
		sp.UserAgent = strings.TrimSpace(sp.UserAgent)
		sp.ProfileDir = strings.TrimSpace(sp.ProfileDir)
		// 空间代理模式留空 = 跟随全局，不能强行填成 system（否则再也跟不上全局变更）
		if strings.TrimSpace(sp.ProxyMode) != "" {
			sp.ProxyMode = normalizeProxyMode(sp.ProxyMode)
			if sp.ProxyMode == "custom" && sp.Proxy == "" {
				sp.ProxyMode = ""
			}
		}
		out = append(out, sp)
	}
	if len(out) == 0 {
		out = defaultBrowserConfig().Spaces
		seen = map[string]bool{out[0].ID: true}
	}
	// 空间数超过上限时只保留前面的：上限是用户明确设置的约束
	if len(out) > b.MaxSpaces {
		out = out[:b.MaxSpaces]
		seen = make(map[string]bool, len(out))
		for _, sp := range out {
			seen[sp.ID] = true
		}
	}
	b.Spaces = out
	if !seen[b.DefaultSpace] {
		b.DefaultSpace = out[0].ID
	}
}

// normalizeMaxSpaces 把空间上限限定在一个合理区间（0/负数走默认值）。
func normalizeMaxSpaces(n int) int {
	if n <= 0 {
		return DefaultMaxSpaces
	}
	if n > 20 {
		return 20
	}
	return n
}

// normalizeProxyMode 把代理模式收敛到 system / direct / custom 三态之一。
func normalizeProxyMode(m string) string {
	switch strings.ToLower(strings.TrimSpace(m)) {
	case "direct":
		return "direct"
	case "custom":
		return "custom"
	default:
		return "system"
	}
}

// BrowserSpaceByID 按 ID 取空间；id 为空时取默认空间。
func BrowserSpaceByID(cfg AppConfig, id string) (BrowserSpace, bool) {
	if strings.TrimSpace(id) == "" {
		id = cfg.Browser.DefaultSpace
	}
	for _, sp := range cfg.Browser.Spaces {
		if sp.ID == id {
			return sp, true
		}
	}
	return BrowserSpace{}, false
}

// ResolveBrowserSpace 取空间并把「留空即跟随全局」的字段补齐。
//
// 配置模型是「全局默认 + 空间覆盖」：空间里没填的项一律回落全局，
// 这样改一次全局默认就能同时影响所有没单独设置过的空间。
func ResolveBrowserSpace(cfg AppConfig, id string) (BrowserSpace, bool) {
	sp, ok := BrowserSpaceByID(cfg, id)
	if !ok {
		return sp, false
	}
	g := cfg.Browser
	if strings.TrimSpace(sp.Homepage) == "" {
		sp.Homepage = g.Homepage
	}
	if strings.TrimSpace(sp.ProxyMode) == "" {
		sp.ProxyMode = g.ProxyMode
		sp.Proxy = g.Proxy
		sp.ProxyBypass = g.ProxyBypass
	} else if sp.ProxyMode == "custom" && strings.TrimSpace(sp.Proxy) == "" {
		sp.Proxy = g.Proxy
		sp.ProxyBypass = g.ProxyBypass
	}
	if strings.TrimSpace(sp.UserAgent) == "" {
		sp.UserAgent = g.UserAgent
	}
	return sp, true
}

// BrowserProfileDir 返回空间的 profile 目录（不存在则创建）。
// 空 ProfileDir 落到 <数据目录>/browser/<空间ID>，跟其他服务数据同样受 paths 配置控制。
func BrowserProfileDir(sp BrowserSpace) (string, error) {
	if sp.ProfileDir != "" {
		if err := os.MkdirAll(sp.ProfileDir, 0o755); err != nil {
			return "", err
		}
		return sp.ProfileDir, nil
	}
	base, err := appdirs.Data("browser")
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, sp.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

// legacyToolIcons 旧版默认数据中不存在于 Arco 的图标 → 当前图标，避免历史配置图标空白。
var legacyToolIcons = map[string]string{
	"color-picker": "palette",
	"random":       "icon-mofa",
	"connection":   "icon-lianjie",
	"usb":          "icon-xinpian",
}

// retiredToolKeys 已下线入口的内置工具 id，存量配置里残留的条目必须剔除，否则工具表/主页会多出一个条目。
// 剔除原因分两类：imageBase64 前端路由与页面都删了（不删会点开白页），能力已并入文件查看器的图片编辑；
// viewer 只撤工具列表入口、模块功能保留（路由与页面都在，由磁盘/FTP/文档中心/局域网互传“在查看器中打开”程序化拉起）。
var retiredToolKeys = []string{"imageBase64", "viewer"}

// normalizeConfig 补齐缺省值（级别非法回退 info；工具 ID 缺失用 Key 兜底；旧图标修正；AppName 空回退 "Tools"；工具设置表非空）。
func normalizeConfig(cfg *AppConfig) {
	if cfg.Order == nil {
		cfg.Order = map[string][]string{}
	}
	if cfg.ToolSettings == nil {
		cfg.ToolSettings = map[string]ToolSettings{}
	}
	if _, ok := cfg.ToolSettings["mysql"]; !ok {
		cfg.ToolSettings["mysql"] = ToolSettings{SQLLogEnabled: true}
	}
	switch cfg.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		cfg.Log.Level = "info"
	}
	if cfg.General.AppName == "" {
		cfg.General.AppName = "Tools"
	}
	if cfg.Ball.DockEdge == nil {
		cfg.Ball.DockEdge = dockOn()
	}
	cfg.Paths.Plugins = strings.TrimSpace(cfg.Paths.Plugins)
	cfg.Paths.Data = strings.TrimSpace(cfg.Paths.Data)
	cfg.Paths.Logs = strings.TrimSpace(cfg.Paths.Logs)
	normalizeBrowserConfig(&cfg.Browser)
	normalizeLauncherConfig(cfg)
	normalizeArchiveConfig(cfg)
	normalizeDocSpaceConfig(cfg)
	normalizeCloudConfig(cfg)
	normalizeDownloadConfig(cfg)
	normalizeLanShareConfig(cfg)
	normalizeStorageConfig(cfg)
	normalizeThemeConfig(&cfg.General.Theme)
	for i := range cfg.Tools {
		if cfg.Tools[i].ID == "" {
			cfg.Tools[i].ID = cfg.Tools[i].Key
		}
		if fixed, ok := legacyToolIcons[cfg.Tools[i].Icon]; ok {
			cfg.Tools[i].Icon = fixed
		}
		// 下载管理的入口已从主页“快捷操作”移到任务抽屉标题栏右侧，默认不再占主页一格。
		// 存量配置里 download 的 Home 多为 nil（旧默认=展示），这里显式改为关闭；
		// 用户手动开过（home:true 非 nil）的原样保留，不覆盖其选择。
		if cfg.Tools[i].Key == "download" && cfg.Tools[i].Home == nil {
			cfg.Tools[i].Home = homeOff()
		}
	}
	// 剔掉已下线工具的条目与排序 id（不先滤就会被下面的补录当成“用户手动移过”残留）
	for _, retired := range retiredToolKeys {
		kept := cfg.Tools[:0:0]
		for _, t := range cfg.Tools {
			if t.Key == retired || t.ID == retired {
				continue
			}
			kept = append(kept, t)
		}
		cfg.Tools = kept
		for k, ids := range cfg.Order {
			fs := ids[:0:0]
			for _, id := range ids {
				if id != retired {
					fs = append(fs, id)
				}
			}
			if len(fs) == 0 {
				delete(cfg.Order, k)
				continue
			}
			cfg.Order[k] = fs
		}
		delete(cfg.ToolSettings, retired)
	}
	// 补齐存量配置中缺失的新增默认工具（仅限下面列出的，尊重用户手动移除的其他工具）
	// 图标与 DefaultConfig 的基线保持一致，别让补录出来的工具长回旧样子
	for _, extra := range []struct{ id, key, name, path, icon string }{
		{"php", "php", "PHP管理", "/tools/php", "icon-php"},
		{"nginx", "nginx", "Nginx管理", "/tools/nginx", "icon-ngnix"},
		{"docker", "docker", "Docker管理", "/tools/docker", "icon-docker"},
		{"term", "term", "终端连接", "/tools/term", "icon-terminal"},
		{"ftp", "ftp", "文件传输", "/tools/ftp", "icon-ftp"},
		{"npm", "npm", "Npm管理", "/tools/npm", "icon-npm"},
		{"speedtest", "speedtest", "网速测速", "/tools/speedtest", "icon-cesu"},
		{"browser", "browser", "浏览器", "/tools/browser", "icon-liulanqi1"},
		{"disk", "disk", "磁盘管理", "/tools/disk", "icon-yingpan"},
		{"siteclone", "siteclone", "仿站工具", "/tools/siteclone", "download"},
		{"guard", "guard", "进程守护", "/tools/guard", "apps"},
		{"archive", "archive", "文件压缩", "/tools/archive", "archive"},
		{"download", "download", "下载管理", "/tools/download", "icon-xiazai"},
		{"docspace", "docspace", "文档中心", "/tools/docspace", "folder"},
		{"note", "note", "笔记", "/tools/note", "icon-book"},
		{"lanshare", "lanshare", "局域网互传", "/tools/lanshare", "icon-wangluo1"},
		{"clouddrive", "clouddrive", "云盘管理", "/tools/clouddrive", "icon-yun"},
		{"git", "git", "Git管理", "/tools/git", "code"},
	} {
		found := false
		for _, t := range cfg.Tools {
			if t.Key == extra.key {
				found = true
				break
			}
		}
		if !found {
			// 插入到 settings 之前，保持工具列表观感一致
			ins := len(cfg.Tools)
			for i, t := range cfg.Tools {
				if t.Key == "settings" {
					ins = i
					break
				}
			}
			newTool := ToolConfig{ID: extra.id, Key: extra.key, Name: extra.name, Path: extra.path, Icon: extra.icon, Single: true, Width: 1200, Height: 800}
			if extra.key == "speedtest" {
				newTool.Width, newTool.Height = 1080, 720
			}
			if extra.key == "browser" {
				// 浏览器面板直接满屏：地址栏/标签栏由前端自画，别留空白边
				newTool.Width, newTool.Height = 0, 0
				newTool.Fullscreen = true
			}
			if extra.key == "disk" {
				// 文件管理器列多：默认给大一点的窗口，不留空白边
				newTool.Width, newTool.Height = 1360, 860
			}
			if extra.key == "ftp" {
				// 双栏文件管理要满屏，左右两栏都不挤
				newTool.Width, newTool.Height = 0, 0
				newTool.Fullscreen = true
			}
			if extra.key == "siteclone" {
				newTool.Width, newTool.Height = 1120, 780
				newTool.Home = homeOff()
			}
			if extra.key == "guard" {
				newTool.Width, newTool.Height = 1120, 720
				newTool.Home = homeOff()
			}
			if extra.key == "archive" {
				newTool.Width, newTool.Height = 1120, 760
				newTool.Home = homeOff()
			}
			if extra.key == "download" {
				// 左分类树 + 右列表/分块详情，宽窗才展得开 FlashGet 式布局
				newTool.Width, newTool.Height = 1280, 820
				// 入口在任务抽屉标题栏，不占主页快捷操作一格
				newTool.Home = homeOff()
			}
			if extra.key == "docspace" {
				// 左分类树 + 右条目列表，跟查看器/下载一样要宽窗才不挤；主页不占格，走工具表进入
				newTool.Width, newTool.Height = 1280, 820
				newTool.Home = homeOff()
			}
			if extra.key == "note" {
				// 笔记：左清单 + 右 wangeditor 编辑区，走工具表进入，不占主页格
				newTool.Width, newTool.Height = 1200, 800
				newTool.Home = homeOff()
			}
			cfg.Tools = append(cfg.Tools[:ins:ins], append([]ToolConfig{newTool}, cfg.Tools[ins:]...)...)
		}
	}
}

// LoadAppConfig 读取配置；文件不存在/解析失败时回落默认值。结果缓存为当前配置。
// 首次启动（无任何配置文件）时：自动生成全局目录布局（plugins/data/logs + 服务子目录）
// 并立即落盘默认配置；同时应用 paths 配置（目录布局随即生效，幂等）。
func LoadAppConfig() AppConfig {
	// 先做配置目录改名迁移（go-tools→Tools），再读取，确保读到迁移过来的旧配置
	migrateConfigDir()
	cfg := DefaultConfig()
	firstRun := false
	// 老版本配置里没有 browser 段：规范化后补写一次，让 config.json 始终是完整的
	// （整份配置要能上传/同步到云端，缺段会导致同步后丢设置）。
	needBrowserSection := false
	if path, err := configFilePath(); err == nil {
		if _, serr := os.Stat(path); serr != nil && os.IsNotExist(serr) {
			firstRun = true
		}
		if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
			var loaded AppConfig
			if err := json.Unmarshal(data, &loaded); err == nil {
				if loaded.Log.Level != "" || loaded.Log.Path != "" {
					cfg.Log = loaded.Log
				}
				cfg.Ball = loaded.Ball
				cfg.General = loaded.General
				cfg.Paths = loaded.Paths
				// 老配置里没有 browser 段：只要读到了空间列表或首页就认为用户配置过
				if loaded.Browser.Homepage != "" || len(loaded.Browser.Spaces) > 0 {
					cfg.Browser = loaded.Browser
				} else if !firstRun {
					needBrowserSection = true
				}
				if len(loaded.Tools) > 0 {
					cfg.Tools = loaded.Tools
				}
				if loaded.ToolSettings != nil {
					cfg.ToolSettings = loaded.ToolSettings
				}
				if loaded.Order != nil {
					cfg.Order = loaded.Order
				}
				// 快捷搜索：nil 与“已关闭”语义一致，直接拿读回来的那一份覆盖默认值。
				if loaded.Launcher != nil {
					cfg.Launcher = loaded.Launcher
				}
				// 文件压缩：同上，nil 就保住默认值（总开关默认关）。
				if loaded.Archive != nil {
					cfg.Archive = loaded.Archive
				}
				// Git：同上，nil 就保住默认值（右键集成默认关）。
				if loaded.Git != nil {
					cfg.Git = loaded.Git
				}
				// 文档中心：同上，nil 就保住默认值（默认启用 + 保留数/天数走默认）。
				if loaded.DocSpace != nil {
					cfg.DocSpace = loaded.DocSpace
				}
				// 云盘：同上，nil 就保住默认值（默认关闭）。此前漏了这一段，导致
				// 重启后用户已保存的云盘设置被默认值抹回，这里补上（一并修复持久化）。
				if loaded.Cloud != nil {
					cfg.Cloud = loaded.Cloud
				}
				// 下载：同上，nil 就保住默认值（默认开启 + 护栏走默认）。
				if loaded.Download != nil {
					cfg.Download = loaded.Download
				}
				// 互传接收：同上，nil 就保住默认值（黑名单空=全收）。
				if loaded.LanShare != nil {
					cfg.LanShare = loaded.LanShare
				}
				// 数据保存位置：同上，nil 就保住默认值（只勾本地，远端全关）。
				if loaded.Storage != nil {
					cfg.Storage = loaded.Storage
				}
			}
		}
	}
	normalizeConfig(&cfg)
	// 应用全局目录布局：设置基础目录 → 迁移旧布局 → 确保服务子目录存在
	InitAppDirs(cfg)
	appCfgMu.Lock()
	appCfg = cfg
	appCfgSet = true
	appCfgMu.Unlock()
	if firstRun {
		// 首次启动：把默认配置（含目录布局）立即写入 config.json
		appCfgWriteMu.Lock()
		_, _ = writeAppConfigLocked(cfg)
		appCfgWriteMu.Unlock()
		applog.Infof("config", "首次启动：已生成默认配置与全局目录（plugins/data/logs + 服务子目录）")
	} else if needBrowserSection {
		// 老配置升级：把新增的 browser 段（空间/代理/首页）补写回文件，
		// 这样设置页与将来的云同步看到的是同一份完整配置。
		appCfgWriteMu.Lock()
		_, _ = writeAppConfigLocked(cfg)
		appCfgWriteMu.Unlock()
		applog.Infof("config", "配置升级：已补写 browser 段")
	}
	return cfg
}

// CurrentConfig 返回当前缓存配置（惰性加载）。
func CurrentConfig() AppConfig {
	appCfgMu.RLock()
	c, set := appCfg, appCfgSet
	appCfgMu.RUnlock()
	if !set {
		return LoadAppConfig()
	}
	return c
}

// SaveAppConfig 规范化并持久化配置，同时更新当前缓存；返回文件路径。
func SaveAppConfig(cfg AppConfig) (string, error) {
	appCfgWriteMu.Lock()
	defer appCfgWriteMu.Unlock()
	return writeAppConfigLocked(cfg)
}

// Update 在写锁内读—改—写当前配置并落盘（返回文件路径）：
// 供调用方在持锁期间基于最新缓存做增量修改，避免与拖动球位/主题等并发保存互相覆盖。
func Update(mutate func(cfg *AppConfig)) (string, error) {
	appCfgWriteMu.Lock()
	defer appCfgWriteMu.Unlock()
	cfg := CurrentConfig()
	mutate(&cfg)
	return writeAppConfigLocked(cfg)
}

// SaveBallPos 仅更新浮动球位置并落盘：读改写处于同一写锁内，
// 不会覆盖其他设置的并发修改。
func SaveBallPos(x, y int) error {
	appCfgWriteMu.Lock()
	defer appCfgWriteMu.Unlock()

	appCfgMu.RLock()
	cfg, set := appCfg, appCfgSet
	appCfgMu.RUnlock()
	if !set {
		cfg = LoadAppConfig()
	}
	cfg.Ball.X = x
	cfg.Ball.Y = y
	cfg.Ball.HasPos = true
	_, err := writeAppConfigLocked(cfg)
	return err
}

// SaveAppTheme 仅更新主题模式（light/dark/auto）并落盘：与 SaveBallPos 同样读改写同锁，
// 供浮动球等原生 UI 跟随应用当前浅/暗黑模式。
func SaveAppTheme(theme string) error {
	appCfgWriteMu.Lock()
	defer appCfgWriteMu.Unlock()

	appCfgMu.RLock()
	cfg, set := appCfg, appCfgSet
	appCfgMu.RUnlock()
	if !set {
		cfg = LoadAppConfig()
	}
	cfg.General.Theme.Mode = theme
	_, err := writeAppConfigLocked(cfg)
	return err
}

// writeAppConfigLocked 调用方需持有 appCfgWriteMu：规范化 → 落盘 → 更新缓存。
func writeAppConfigLocked(cfg AppConfig) (string, error) {
	normalizeConfig(&cfg)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	path, err := configFilePath()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	appCfgMu.Lock()
	appCfg = cfg
	appCfgSet = true
	appCfgMu.Unlock()
	return path, nil
}
