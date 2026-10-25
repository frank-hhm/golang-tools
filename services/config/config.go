package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go-tools/appdirs"
	"go-tools/services/applog"
)

// ============================================================
// 全局应用配置（Go 侧 JSON 文件持久化，与 redis/mysql 连接配置同目录）
//
// 位置：os.UserConfigDir()/go-tools/config.json
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
	Path    string `json:"path"`    // 日志目录；空 = 默认（配置目录下 go-tools/logs）。按级别+日期自动分文件：Error20261022.log / Info20261022.log 等
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
}

// ToolSettings 单个工具的专属设置（按 ToolConfig.Key 索引，目前仅 mysql 使用）
type ToolSettings struct {
	SQLLogEnabled bool   `json:"sqlLogEnabled"` // 是否记录 SQL 执行日志（手动 SQL 与增删改/DDL）
	SQLLogDir     string `json:"sqlLogDir"`     // SQL 日志目录；空 = 默认（程序目录下 logs）
}

// AppConfig 应用总配置
type AppConfig struct {
	Log          LogConfig               `json:"log"`
	Ball         BallConfig              `json:"ball"`
	General      GeneralConfig           `json:"general"`
	Paths        PathsConfig             `json:"paths"`
	Tools        []ToolConfig            `json:"tools"`
	ToolSettings map[string]ToolSettings `json:"toolSettings"` // 工具级设置，键为工具 Key（如 mysql）
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
		ToolSettings: map[string]ToolSettings{
			// MySQL：默认开启 SQL 日志，目录留空走 DefaultSQLLogDir
			"mysql": {SQLLogEnabled: true},
		},
		Tools: []ToolConfig{
			{ID: "screenshot", Key: "screenshot", Name: "截图", Event: "screenshot", Icon: "icon-jietu", Shortcut: "Ctrl+Alt+A", Quick: true},
			{ID: "colorpicker", Key: "colorpicker", Name: "颜色取色器", Path: "/tools/colorpicker", Icon: "icon-quseqi", Single: true, Width: 480, Height: 560, Quick: true},
			{ID: "video", Key: "video", Name: "视频录制", Path: "/tools/video", Icon: "video-camera", Single: true, Fullscreen: true, Quick: true},
			{ID: "qrcode", Key: "qrcode", Name: "二维码生成", Path: "/tools/qrcode", Icon: "icon-erweima3"},
			{ID: "timestamp", Key: "timestamp", Name: "时间戳转换", Path: "/tools/timestamp", Icon: "icon-riqishijianzhuanhuanmiaoshu"},
			{ID: "strrandom", Key: "strrandom", Name: "随机字符串生成", Path: "/tools/strrandom", Icon: "icon-zifuchuan"},
			{ID: "encdec", Key: "encdec", Name: "文字加密解密", Path: "/tools/encdec", Icon: "lock"},
			{ID: "hexconvert", Key: "hexconvert", Name: "进制转换", Path: "/tools/hexconvert", Icon: "swap"},
			{ID: "imageBase64", Key: "imageBase64", Name: "图片base64", Path: "/tools/imageBase64", Icon: "icon-img"},
			{ID: "sniffer", Key: "sniffer", Name: "抓包工具", Path: "/tools/sniffer", Icon: "bug", Single: true, Fullscreen: true, Quick: true},
			{ID: "websocket", Key: "websocket", Name: "WebSocket调试", Path: "/tools/websocket", Icon: "icon-wangluo1", Fullscreen: true},
			{ID: "speedtest", Key: "speedtest", Name: "网速测速", Path: "/tools/speedtest", Icon: "icon-wangluo1", Single: true, Width: 1080, Height: 720},
			{ID: "usbserial", Key: "usbserial", Name: "USB调试", Path: "/tools/usbserial", Icon: "icon-usb1"},
			{ID: "redis", Key: "redis", Name: "Redis管理", Path: "/tools/redis", Icon: "icon-icon-redis", Single: true, Fullscreen: true},
			{ID: "mysql", Key: "mysql", Name: "MySQL管理", Path: "/tools/mysql", Icon: "icon-MySQL", Single: true, Fullscreen: true},
			{ID: "php", Key: "php", Name: "PHP管理", Path: "/tools/php", Icon: "icon-php", Single: true, Width: 1200, Height: 800},
			{ID: "nginx", Key: "nginx", Name: "Nginx管理", Path: "/tools/nginx", Icon: "icon-ngnix", Single: true, Width: 1200, Height: 800},
			{ID: "docker", Key: "docker", Name: "Docker管理", Path: "/tools/docker", Icon: "computer", Single: true, Width: 1200, Height: 800},
			{ID: "term", Key: "term", Name: "终端连接", Path: "/tools/term", Icon: "code", Single: true, Fullscreen: true, Quick: true},
			{ID: "npm", Key: "npm", Name: "Npm管理", Path: "/tools/npm", Icon: "icon-npm", Single: true, Width: 1200, Height: 800},
		},
	}
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
	dir := filepath.Join(base, "go-tools")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
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

// legacyToolIcons 旧版默认数据中不存在于 Arco 的图标 → 当前图标，避免历史配置图标空白。
var legacyToolIcons = map[string]string{
	"color-picker": "palette",
	"random":       "icon-mofa",
	"connection":   "icon-lianjie",
	"usb":          "icon-xinpian",
}

// normalizeConfig 补齐缺省值（级别非法回退 info；工具 ID 缺失用 Key 兜底；旧图标修正；AppName 空回退 "Tools"；工具设置表非空）。
func normalizeConfig(cfg *AppConfig) {
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
	// 补齐存量配置中缺失的新增默认工具（仅限 php/nginx/npm/docker/speedtest，尊重用户手动移除的其他工具）
	for _, extra := range []struct{ id, key, name, path, icon string }{
		{"php", "php", "PHP管理", "/tools/php", "icon-php"},
		{"nginx", "nginx", "Nginx管理", "/tools/nginx", "icon-ngnix"},
		{"docker", "docker", "Docker管理", "/tools/docker", "computer"},
		{"term", "term", "终端连接", "/tools/term", "code"},
		{"npm", "npm", "Npm管理", "/tools/npm", "icon-npm"},
		{"speedtest", "speedtest", "网速测速", "/tools/speedtest", "icon-wangluo1"},
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
			cfg.Tools = append(cfg.Tools[:ins:ins], append([]ToolConfig{newTool}, cfg.Tools[ins:]...)...)
		}
	}
}

// LoadAppConfig 读取配置；文件不存在/解析失败时回落默认值。结果缓存为当前配置。
// 首次启动（无任何配置文件）时：自动生成全局目录布局（plugins/data/logs + 服务子目录）
// 并立即落盘默认配置；同时应用 paths 配置（目录布局随即生效，幂等）。
func LoadAppConfig() AppConfig {
	cfg := DefaultConfig()
	firstRun := false
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
				if len(loaded.Tools) > 0 {
					cfg.Tools = loaded.Tools
				}
				if loaded.ToolSettings != nil {
					cfg.ToolSettings = loaded.ToolSettings
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
