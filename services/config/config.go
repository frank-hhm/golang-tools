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
}

// GeneralConfig 应用通用设置
type GeneralConfig struct {
	AutoStart bool   `json:"autoStart"` // 是否注册开机（Windows 登录时）自动启动，写入 HKCU\...\Run 注册表键
	AppName   string `json:"appName"`   // 应用显示名称（窗口标题/托盘提示），默认 "Tools"
	AppLogo   string `json:"appLogo"`   // 自定义 logo 图片绝对路径，空表示使用内置默认
	Theme     string `json:"theme"`     // 主题模式：light / dark / auto，默认 light（前端消费，后端仅持久化）
}

// PathsConfig 全局三类基础目录（各自下按服务名建子目录，见 appdirs/layout.go）；
// 空 = 默认 <应用目录>/plugins|data|logs。修改保存后，各服务配置与另存文件默认目录随即落新路径。
type PathsConfig struct {
	Plugins string `json:"plugins"` // 插件目录：服务程序安装位置（php/nginx/mysql/redis/npm）
	Data    string `json:"data"`    // 数据目录：服务配置缓存/实例工作目录/截图录像等另存默认位置
	Logs    string `json:"logs"`    // 日志目录：各服务日志与程序日志（logs/<服务名>）
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

// AppConfig 应用总配置
type AppConfig struct {
	Log          LogConfig               `json:"log"`
	Ball         BallConfig              `json:"ball"`
	General      GeneralConfig           `json:"general"`
	Paths        PathsConfig             `json:"paths"`
	Browser      BrowserConfig           `json:"browser"`
	Tools        []ToolConfig            `json:"tools"`
	ToolSettings map[string]ToolSettings `json:"toolSettings"` // 工具级设置，键为工具 Key（如 mysql）
	// Order 各列表的拖拽自定义顺序：键为“面”标识（如 php:list / nginx:list / home:tools），
	// 值为该面内条目 ID 的有序列表。主页与各服务列表各用独立键，互不共用。随整份配置导出/导入。
	Order map[string][]string `json:"order"`
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
		Log:     LogConfig{Enabled: true, Level: "info", Path: ""},
		Ball:    BallConfig{Enabled: true},
		General: GeneralConfig{AutoStart: false, AppName: "Tools", AppLogo: "", Theme: "light"},
		Browser: defaultBrowserConfig(),
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
			{ID: "imageBase64", Key: "imageBase64", Name: "图片base64", Path: "/tools/imageBase64", Icon: "icon-img", Home: homeOff()},
			{ID: "hexconvert", Key: "hexconvert", Name: "进制转换", Path: "/tools/hexconvert", Icon: "swap", Home: homeOff()},
		},
	}
}

// homeOff 把默认工具标成“不在主页快捷操作区直接展示”：
// Home 是指针型，nil 被当成“未设置 = 展示”，要表达关闭只能显式给个 false。
func homeOff() *bool {
	f := false
	return &f
}

// ApplogConfig 把配置里的 LogConfig 转成 applog 自己的配置类型，使日志包不反向依赖应用层。
func ApplogConfig(l LogConfig) applog.Config {
	return applog.Config{Enabled: l.Enabled, Level: l.Level, Path: l.Path}
}

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
	if err := appdirs.ApplyLayout(cfg.Paths.Plugins, cfg.Paths.Data, cfg.Paths.Logs); err != nil {
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
	cfg.Paths.Plugins = strings.TrimSpace(cfg.Paths.Plugins)
	cfg.Paths.Data = strings.TrimSpace(cfg.Paths.Data)
	cfg.Paths.Logs = strings.TrimSpace(cfg.Paths.Logs)
	normalizeBrowserConfig(&cfg.Browser)
	switch cfg.General.Theme {
	case "light", "dark", "auto":
	default:
		cfg.General.Theme = "light"
	}
	for i := range cfg.Tools {
		if cfg.Tools[i].ID == "" {
			cfg.Tools[i].ID = cfg.Tools[i].Key
		}
		if fixed, ok := legacyToolIcons[cfg.Tools[i].Icon]; ok {
			cfg.Tools[i].Icon = fixed
		}
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
	cfg.General.Theme = theme
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
