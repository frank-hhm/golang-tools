package launcher

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

// ============================================================
// 命令与 URL 直达
//
// 命令表在引擎里，执行器在宿主里：引擎只负责"哪条命令该在什么时候被搜出来"，
// 真正去开窗/锁屏/退出的是 services/app（它才知道 App 实例、窗口对象、winui 线程）。
// 这样 tools/launcher 不会反向 import 宿主，单测也能独立跑。
// ============================================================

var (
	// ErrNoRunner 宿主还没注入执行器（启动极早期或 Linux/server 构建）。
	ErrNoRunner = errors.New("快捷搜索的执行器尚未就绪")
	// ErrNoLocation 这条结果背后没有一个能在资源管理器里定位的文件。
	ErrNoLocation = errors.New("该结果没有可定位的文件")
)

// Command 一条可搜索的命令。
type Command struct {
	ID       string   `json:"id"`
	Title    string   `json:"title"`
	Sub      string   `json:"sub"`
	Keywords []string `json:"keywords"` // 额外匹配词（中英文都给，搜索框里敲 "lock" 也该找到锁屏）
	Danger   bool     `json:"danger"`   // 前端标红 + 副标题写清后果
}

// commands 固定清单。顺序无意义（排序靠分数），但读起来按"日常 → 收尾"排。
var commands = []Command{
	{ID: "open-settings", Title: "打开设置", Sub: "本程序设置页", Keywords: []string{"settings", "config", "设置"}},
	{ID: "open-home", Title: "打开主页", Sub: "本程序主窗口首页", Keywords: []string{"home", "dashboard", "主页", "首页"}},
	{ID: "toggle-ball", Title: "显示/隐藏浮动球", Sub: "桌面浮动球", Keywords: []string{"ball", "float", "浮动球"}},
	{ID: "open-config-dir", Title: "打开配置目录", Sub: "config.json 所在目录", Keywords: []string{"config dir", "配置目录"}},
	{ID: "open-log-dir", Title: "打开日志目录", Sub: "程序与服务日志", Keywords: []string{"log dir", "logs", "日志目录"}},
	{ID: "rebuild-index", Title: "重建搜索索引", Sub: "重新扫描已纳入的目录", Keywords: []string{"index", "reindex", "索引"}},
	{ID: "lock-workstation", Title: "锁屏", Sub: "锁定当前用户会话", Keywords: []string{"lock", "锁屏"}},
	{ID: "quit", Title: "退出程序", Sub: "将退出 Tools（含正在运行的后台任务）", Keywords: []string{"quit", "exit", "close", "退出"}, Danger: true},
}

// ListCommands 给设置页/帮助用：当前可搜的全部命令。
func ListCommands() []Command { return append([]Command(nil), commands...) }

// commandItems 按标题与关键词打分取最高。
// 关键词单独比一次而不是拼进标题一起比：标题里塞满英文别名会污染展示。
func commandItems(q string) []Item {
	var out []Item
	for _, c := range commands {
		s := scoreText(q, c.Title, "")
		for _, k := range c.Keywords {
			if ks := scoreText(q, k, ""); ks > s {
				s = ks
			}
		}
		if strings.EqualFold(strings.TrimSpace(q), c.ID) {
			s = scoreExact
		}
		if s <= 0 {
			continue
		}
		out = append(out, Item{
			Type: TypeCommand, ID: c.ID, Title: c.Title, Sub: c.Sub,
			Score: s, Danger: c.Danger,
		})
	}
	return out
}

// ============================================================
// URL 直达
// ============================================================

// tldRe 看起来就像个域名："名字.常见顶级域"，允许带路径。
// 只列常见这几类是刻意的：宁可少认一个域名（用户照样能敲全 URL），
// 也不能把 "file.doc"、"app.config" 这种文件名误判成网址。
var tldRe = regexp.MustCompile(`^[a-z0-9][a-z0-9\-._]*\.(com|cn|net|org|io|dev|top|xyz|co|me|edu|gov|info|biz|app|shop|vip|club|site|online)(/[^\s]*)?$`)

// urlItem 输入像网址时给一条"用系统浏览器打开"。
func urlItem(q string) (Item, bool) {
	s := strings.TrimSpace(q)
	if s == "" || strings.ContainsAny(s, " \t") {
		return Item{}, false
	}
	low := strings.ToLower(s)
	full := ""
	switch {
	case strings.HasPrefix(low, "https://") || strings.HasPrefix(low, "http://"):
		full = s
	case strings.HasPrefix(low, "www."):
		full = "https://" + s
	case tldRe.MatchString(low):
		full = "https://" + s
	default:
		return Item{}, false
	}
	// 再拿 url.Parse 兜一道：正则只管形状，真正能不能当网址解析以它为准。
	u, err := url.Parse(full)
	if err != nil || u.Host == "" {
		return Item{}, false
	}
	return Item{
		Type: TypeURL, ID: full, Title: full, Sub: "用默认浏览器打开",
		Score: scoreExact - 10,
	}, true
}
