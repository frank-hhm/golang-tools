package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ============================================================
// 排序与聚合
// ============================================================

func mkItem(typ, id, title string, score int) Item {
	return Item{Type: typ, ID: id, Title: title, Score: score}
}

func TestDedupKeepsHigherScore(t *testing.T) {
	items := []Item{
		mkItem(TypeFile, `C:\A\chrome.exe`, "chrome.exe", 100),
		mkItem(TypeApp, `C:\A\chrome.exe`, "Chrome", 900),
	}
	got := dedup(items)
	if len(got) != 1 {
		t.Fatalf("同一路径应折叠成一条，实得 %d", len(got))
	}
	if got[0].Type != TypeApp || got[0].Score != 900 {
		t.Fatalf("应保留分数高的那条：%+v", got[0])
	}
}

func TestDedupIsCaseInsensitive(t *testing.T) {
	got := dedup([]Item{
		mkItem(TypeApp, `C:\A\Chrome.LNK`, "Chrome", 900),
		mkItem(TypeFile, `c:\a\chrome.lnk`, "chrome.lnk", 100),
	})
	if len(got) != 1 {
		t.Fatalf("Windows 路径大小写不同不该算两条：%+v", got)
	}
}

func TestTopKPerTypeCap(t *testing.T) {
	var items []Item
	for i := 0; i < 40; i++ {
		items = append(items, mkItem(TypeFile, string(rune('a'+i%26))+string(rune('0'+i)), "报告", 100+i))
	}
	for i := 0; i < 10; i++ {
		items = append(items, mkItem(TypeApp, "app-"+string(rune('a'+i)), "应用", 90-i))
	}
	out := topK(items, MaxResults)
	files, apps := 0, 0
	for _, it := range out {
		switch it.Type {
		case TypeFile:
			files++
		case TypeApp:
			apps++
		}
	}
	// 两类各自撞到自己的限额，总数因而不等于 MaxResults：宁可少给两条，
	// 也不让"文件"把列表占满到一条应用都看不见。
	if files != perTypeCap[TypeFile] || apps != perTypeCap[TypeApp] {
		t.Fatalf("限额没生效：file=%d/%d app=%d/%d", files, perTypeCap[TypeFile], apps, perTypeCap[TypeApp])
	}
	if len(out) != files+apps || len(out) > MaxResults {
		t.Fatalf("总数 %d 与分类数不自洽", len(out))
	}
}

func TestTopKOwnToolsBeatApps(t *testing.T) {
	// “docker”这类词：外部应用 Docker Desktop 与本程序工具 Docker管理 都在前缀档，
	// 实际分差只来自长度惩罚的几十分。启动器是本程序自己的门面，工具必须排在前。
	out := topK([]Item{
		mkItem(TypeApp, "app-docker", "Docker Desktop", 686),
		mkItem(TypeTool, "docker", "Docker管理", 692),
	}, MaxResults)
	if out[0].Type != TypeTool {
		t.Fatalf("同档下本程序工具应排在外部应用前：%+v", out)
	}
	// 但类型优先不跨档：应用名字完全相等时照样压过工具的前缀命中。
	out = topK([]Item{
		mkItem(TypeApp, "app-chrome", "chrome", scoreExact),
		mkItem(TypeTool, "browser", "浏览器", scorePrefix),
	}, MaxResults)
	if out[0].Type != TypeApp {
		t.Fatalf("类型加分不该压过匹配档位：%+v", out)
	}
	// 算式与网址是“整串就是这个意图”（calc.go 直接给 scoreExact），也不允许被工具压住。
	out = topK([]Item{
		mkItem(TypeTool, "calc", "计算器", scoreExact),
		mkItem(TypeCalc, "1+1", "2", scoreExact),
	}, MaxResults)
	if out[0].Type != TypeCalc {
		t.Fatalf("算式应始终排第一：%+v", out)
	}
}

func TestTopKUsageBonus(t *testing.T) {
	// 用过 10 次以上的条目加分相同（封顶），保证"常用"能压过同名次的冷门结果。
	usageMu.Lock()
	usage = map[string]int{"a": 3, "b": 200}
	usageMu.Unlock()
	defer func() {
		usageMu.Lock()
		usage = nil
		usageMu.Unlock()
	}()
	if usageBonus(3) != 60 || usageBonus(200) != usageBonus(10) {
		t.Fatalf("使用频次加分没封顶：%d %d", usageBonus(3), usageBonus(200))
	}
}

func TestToolItemsUsesConfiguredName(t *testing.T) {
	SetToolsForTest([]toolRef{
		{ID: "t-redis", Key: "redis", Name: "Redis管理", Path: "/tools/redis", Event: ""},
		{Key: "shot", Name: "", Path: "", Event: "screenshot"},
	})
	defer SetToolsForTest(nil)

	got := toolItems("redis")
	if len(got) != 1 || got[0].ID != "t-redis" || got[0].Type != TypeTool {
		t.Fatalf("工具结果不对：%+v", got)
	}
	// ID 空时（旧配置）回落到 Key：宿主拿它照样能在配置里找到那个工具。
	got = toolItems("shot")
	if len(got) != 1 || got[0].ID != "shot" {
		t.Fatalf("无 ID 工具应回落到 Key：%+v", got)
	}
}

// ============================================================
// URL 与命令
// ============================================================

func TestURLItem(t *testing.T) {
	for _, s := range []string{"https://example.com", "www.example.com", "example.cn", "example.com/path"} {
		it, ok := urlItem(s)
		if !ok {
			t.Errorf("%q 应识别为网址", s)
			continue
		}
		if !strings.HasPrefix(it.ID, "https://") {
			t.Errorf("%q 补全后不是完整 URL：%q", s, it.ID)
		}
	}
	// 关键负例：带扩展名的文件名不能被当成域名。
	for _, s := range []string{"report.docx", "app.config", "localhost:8080", "1+2", "普通搜索词"} {
		if it, ok := urlItem(s); ok {
			t.Errorf("%q 不该被当成网址（%q）", s, it.ID)
		}
	}
}

func TestCommandItemsMatchesKeywords(t *testing.T) {
	// 英文关键词也要能命中中文命令：敲 "lock" 该找到"锁屏"。
	got := commandItems("lock")
	found := false
	for _, it := range got {
		if it.ID == "lock-workstation" {
			found = true
			if it.Danger {
				t.Error("锁屏不是危险命令")
			}
		}
	}
	if !found {
		t.Fatalf("lock 没命中锁屏命令：%+v", got)
	}
	// 退出程序要带危险标记，前端据此标红。
	got = commandItems("退出")
	danger := false
	for _, it := range got {
		if it.ID == "quit" {
			danger = it.Danger
		}
	}
	if !danger {
		t.Fatal("退出命令没标成危险项")
	}
}

func TestSearchCmdOnlyPrefix(t *testing.T) {
	// ">" 前缀只看命令：应用/工具/文件/算式这类结果一律不掺进来。
	res := Search(">lock", Scopes{Apps: true, Tools: true, Files: true, Commands: true})
	if len(res.Items) == 0 {
		t.Fatal("\">lock\" 应至少找到锁屏命令")
	}
	for _, it := range res.Items {
		if it.Type != TypeCommand {
			t.Fatalf("命令模式混进了 %q", it.Type)
		}
	}
}

// ============================================================
// 文件索引
// ============================================================

func TestBuildIndexSkipsNoiseAndHonorsCap(t *testing.T) {
	root := t.TempDir()
	mk := func(rel string) {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mk("report-2026.md")
	mk("notes/todo.txt")
	mk("node_modules/acorn/index.js") // 该被跳过
	mk(".git/config")                 // 该被跳过
	mk("AppData/Local/tmp.dat")       // 该被跳过
	mk("sub/deep/a.bin")

	idx := buildIndex([]string{root}, 0, defaultMaxDepth)
	names := map[string]bool{}
	for _, e := range idx.entries {
		names[e.name] = true
	}
	for _, want := range []string{"report-2026.md", "todo.txt", "a.bin"} {
		if !names[want] {
			t.Errorf("%s 该进索引", want)
		}
	}
	for _, no := range []string{"index.js", "config", "tmp.dat"} {
		if names[no] {
			t.Errorf("%s 不该进索引", no)
		}
	}

	// 撞到上限时如实标 partial，而不是悄悄少给。
	limited := buildIndex([]string{root}, 2, defaultMaxDepth)
	if !limited.partial || len(limited.entries) != 2 {
		t.Fatalf("上限没生效：%d 条，partial=%v", len(limited.entries), limited.partial)
	}
}

func TestFileItemsRanksNameOverPath(t *testing.T) {
	root := t.TempDir()
	// 三条同查询词的候选：名字里就有 / 名字前缀 / 只在目录名里出现。
	for _, rel := range []string{"一季度.xlsx", "一季度总结.md", "一季度/明细.txt", "report.docx"} {
		p := filepath.Join(root, filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte("x"), 0o644)
	}
	idx := buildIndex([]string{root}, 0, defaultMaxDepth)
	idxPtr.Store(idx)
	defer idxPtr.Store(nil)

	got := fileItems("一季度")
	if len(got) != 3 {
		t.Fatalf("三条都该命中（含只命中目录名的），实得 %d：%+v", len(got), got)
	}
	// 完全相等 > 前缀 > 只命中路径。
	if got[0].Title != "一季度.xlsx" || got[1].Title != "一季度总结.md" || got[2].Title != "明细.txt" {
		t.Fatalf("排序不对：%q %q %q", got[0].Title, got[1].Title, got[2].Title)
	}
}
