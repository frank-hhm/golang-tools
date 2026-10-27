package docker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mkDockerSettings 在临时 APPDATA 下造 Docker Desktop 配置目录，返回该目录。
// 用 t.Setenv 隔离，绝不碰真实的 %APPDATA%\Docker。
func mkDockerSettings(t *testing.T, files map[string]any) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "Docker")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("建配置目录失败: %v", err)
	}
	for name, content := range files {
		b, err := json.Marshal(content)
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatalf("写配置失败: %v", err)
		}
	}
	t.Setenv("APPDATA", filepath.Dir(dir))
	return dir
}

func readStore(t *testing.T, p string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", p, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("解析 %s 失败: %v", p, err)
	}
	return m
}

func TestBackendFromKernel(t *testing.T) {
	cases := []struct{ kernel, want string }{
		{"5.15.153.1-microsoft-standard-WSL2", "wsl2"},
		{"5.15.49-linuxkit", "hyperv"},
		{"", ""},
	}
	for _, c := range cases {
		if got := backendFromKernel(c.kernel); got != c.want {
			t.Errorf("backendFromKernel(%q) = %q, want %q", c.kernel, got, c.want)
		}
	}
}

func TestLabelOfBackend(t *testing.T) {
	for name, want := range map[string]string{"wsl2": "WSL2", "hyperv": "Hyper-V", "unknown": "未知", "": "未知"} {
		if got := labelOfBackend(name); got != want {
			t.Errorf("labelOfBackend(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSwitchGateError(t *testing.T) {
	if err := SwitchGateError(false, true); err != nil {
		t.Errorf("已确认时不应拦截: %v", err)
	}
	if err := SwitchGateError(true, true); err != nil {
		t.Errorf("切回 WSL2 已确认时不应拦截: %v", err)
	}
	err := SwitchGateError(false, false)
	if err == nil {
		t.Fatal("未确认必须拦截")
	}
	// 文案要讲清后果，不能只说"需要确认"
	for _, kw := range []string{"Hyper-V", "虚拟机", "镜像"} {
		if !strings.Contains(err.Error(), kw) {
			t.Errorf("拦截文案缺少 %q: %v", kw, err)
		}
	}
	if !strings.Contains(SwitchGateError(true, false).Error(), "WSL2") {
		t.Error("切回 WSL2 的拦截文案应指明 WSL2")
	}
}

func TestFindSettingsFilePrefersStore(t *testing.T) {
	dir := mkDockerSettings(t, map[string]any{
		settingsStoreName:  map[string]any{"wslEngineEnabled": true, "memoryInGB": float64(8)},
		settingsLegacyName: map[string]any{"wslEngineEnabled": false},
	})
	p, m, err := findSettingsFile()
	if err != nil {
		t.Fatalf("findSettingsFile 失败: %v", err)
	}
	if filepath.Base(p) != settingsStoreName {
		t.Errorf("应优先用 %s，实际 %s", settingsStoreName, p)
	}
	if m["wslEngineEnabled"] != true {
		t.Errorf("store 里的值应为 true，实际 %v", m["wslEngineEnabled"])
	}
	if _, err := os.Stat(filepath.Join(dir, settingsLegacyName)); err != nil {
		t.Error("旧配置文件不该被删掉")
	}
}

func TestFindSettingsFileFallbackLegacy(t *testing.T) {
	mkDockerSettings(t, map[string]any{
		settingsLegacyName: map[string]any{"wslEngineEnabled": false},
	})
	p, _, err := findSettingsFile()
	if err != nil {
		t.Fatalf("findSettingsFile 失败: %v", err)
	}
	if filepath.Base(p) != settingsLegacyName {
		t.Errorf("只有旧文件时应落到 %s，实际 %s", settingsLegacyName, p)
	}
	c := readEngineChoice()
	if c.Value != "false" || !c.Set || c.File != p || c.Key != wslEngineKey {
		t.Errorf("readEngineChoice = %+v, want false/该文件/true/旧键", c)
	}
}

func TestFindSettingsFileMissingDir(t *testing.T) {
	t.Setenv("APPDATA", t.TempDir())
	if _, _, err := findSettingsFile(); err == nil {
		t.Fatal("配置目录不存在时必须报错，不能让调用方以为读到了配置")
	}
	if p := readEngineChoice().File; p != "" {
		t.Errorf("出错时不应返回路径，实际 %q", p)
	}
	if settingFileForWrite() != "" {
		t.Error("settingFileForWrite 在目录缺失时应返回空串（切换据此拒绝执行）")
	}
}

func TestWriteEngineChoiceKeepsOtherKeys(t *testing.T) {
	dir := mkDockerSettings(t, map[string]any{
		settingsStoreName: map[string]any{
			"wslEngineEnabled":  true,
			"memoryInGB":        float64(8),
			"cpus":              float64(4),
			"proxyHttpBusiness": "http://127.0.0.1:8888",
		},
	})
	store := filepath.Join(dir, settingsStoreName)
	desc, err := writeEngineChoice(false)
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	m := readStore(t, store)
	if m["wslEngineEnabled"] != false {
		t.Errorf("目标键未改成 false: %v", m["wslEngineEnabled"])
	}
	// 别的键必须原样保留：这是 Docker Desktop 自己的文件，不能整体覆盖
	if m["memoryInGB"] != float64(8) || m["cpus"] != float64(4) || m["proxyHttpBusiness"] != "http://127.0.0.1:8888" {
		t.Errorf("其他配置项被改动了: %v", m)
	}
	if _, err := os.Stat(store + ".gobak"); err != nil {
		t.Errorf("应留下备份文件: %v", err)
	}
	if !strings.Contains(desc, "true") || !strings.Contains(desc, "false") {
		t.Errorf("变更说明应含前后值: %s", desc)
	}
	if leftover, _ := filepath.Glob(filepath.Join(dir, "*.gotmp")); len(leftover) != 0 {
		t.Errorf("临时文件未清理: %v", leftover)
	}
}

func TestWriteEngineChoiceAddsMissingKey(t *testing.T) {
	dir := mkDockerSettings(t, map[string]any{settingsStoreName: map[string]any{"memoryInGB": float64(4)}})
	if _, err := writeEngineChoice(true); err != nil {
		t.Fatalf("键缺失时应补上而不是报错: %v", err)
	}
	if got := readStore(t, filepath.Join(dir, settingsStoreName))[wslEngineKey]; got != true {
		t.Errorf("未补上目标键: %v", got)
	}
	// 原本没有该键 → 不该产生备份
	if _, err := os.Stat(filepath.Join(dir, settingsStoreName) + ".gobak"); err == nil {
		t.Error("键缺失时不应生成 .gobak")
	}
}

// Docker Desktop 4.96.3 的后端开关是 EngineTypeWindows（字符串），不是旧的 wslEngineEnabled。
// 写错键时它只记一条 “unknown settings found” 然后照旧跑 hyper-v：
// 这就是“提示切换成功、面板一直是 Hyper-V”的根因，必须两端都认。
func TestEngineTypeWindowsSchema(t *testing.T) {
	dir := mkDockerSettings(t, map[string]any{
		settingsStoreName: map[string]any{
			"SettingsVersion": float64(46),
			engineTypeKey:     "hyper-v",
			wslEngineKey:      true,
			"AutoStart":       false,
			"LicenseTermsVer": float64(2),
		},
	})
	store := filepath.Join(dir, settingsStoreName)
	c := readEngineChoice()
	if c.Key != engineTypeKey || c.Value != "hyper-v" || c.backendOf() != "hyperv" {
		t.Fatalf("新格式应读 %s，实际 %+v", engineTypeKey, c)
	}
	desc, err := writeEngineChoice(true)
	if err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	m := readStore(t, store)
	if m[engineTypeKey] != engineTypeWsl2 {
		t.Errorf("应把 %s 改成 %s: %v", engineTypeKey, engineTypeWsl2, m[engineTypeKey])
	}
	// 旧键一并改掉，避免文件自相矛盾
	if m[wslEngineKey] != true {
		t.Errorf("残留的旧键应跟着改: %v", m[wslEngineKey])
	}
	if m["SettingsVersion"] != float64(46) || m["LicenseTermsVer"] != float64(2) {
		t.Errorf("其他配置项被改动了: %v", m)
	}
	if !strings.Contains(desc, engineTypeKey) || !strings.Contains(desc, "hyper-v") {
		t.Errorf("变更说明应含键名与前后值: %s", desc)
	}
	if got := readEngineChoice(); got.backendOf() != "wsl2" {
		t.Errorf("回读应为 wsl2，实际 %+v", got)
	}
}

// 只有 EngineTypeWindows 没有 SettingsVersion 的也算新格式（升级中途可能就写成这样）
func TestUsesEngineType(t *testing.T) {
	if !usesEngineType(map[string]any{engineTypeKey: engineTypeWsl2}) {
		t.Error("有 EngineTypeWindows 应判为新格式")
	}
	if !usesEngineType(map[string]any{"SettingsVersion": float64(1)}) {
		t.Error("有 SettingsVersion 应判为新格式")
	}
	if usesEngineType(map[string]any{wslEngineKey: true}) {
		t.Error("旧文件不应判为新格式")
	}
	// 两个键都在（本机实测就是这种）：以新键为准
	if k := settingKeyFor(map[string]any{engineTypeKey: engineTypeHyperv, wslEngineKey: true}); k != engineTypeKey {
		t.Errorf("两个键都在时应选 %s，实际 %s", engineTypeKey, k)
	}
	// 只有旧键的老版本：不能被写成它不认的新键
	if k := settingKeyFor(map[string]any{wslEngineKey: true}); k != wslEngineKey {
		t.Errorf("只有旧键时应选 %s，实际 %s", wslEngineKey, k)
	}
}

func TestDetectBackend(t *testing.T) {
	t.Run("引擎内核串优先于配置文件", func(t *testing.T) {
		mkDockerSettings(t, map[string]any{settingsStoreName: map[string]any{"wslEngineEnabled": true}})
		b := detectBackend(EngineInfo{Kernel: "5.15.49-linuxkit"})
		if b.Name != "hyperv" || b.Source != "引擎内核串" {
			t.Errorf("应以运行事实为准，得到 %+v", b)
		}
		if b.SettingsFile == "" || b.SettingVal != "true" {
			t.Errorf("仍要回传配置文件与原值，便于提示改完未生效: %+v", b)
		}
	})
	t.Run("引擎没跑时按配置文件", func(t *testing.T) {
		mkDockerSettings(t, map[string]any{settingsStoreName: map[string]any{"wslEngineEnabled": false}})
		b := detectBackend(EngineInfo{})
		if b.Name != "hyperv" || b.Source != "配置文件" {
			t.Errorf(" %+v", b)
		}
	})
	t.Run("配置也没写过用出厂默认", func(t *testing.T) {
		mkDockerSettings(t, map[string]any{settingsStoreName: map[string]any{"memoryInGB": float64(2)}})
		b := detectBackend(EngineInfo{})
		if b.Name != "wsl2" || !strings.Contains(b.Source, "缺省") {
			t.Errorf(" %+v", b)
		}
		if b.SettingVal != "" {
			t.Errorf("未显式配置时 settingVal 应为空，实际 %q", b.SettingVal)
		}
	})
	t.Run("什么都读不到不崩", func(t *testing.T) {
		t.Setenv("APPDATA", t.TempDir())
		b := detectBackend(EngineInfo{})
		if b.Name != "wsl2" || b.Label != "WSL2" {
			t.Errorf(" %+v", b)
		}
	})
}

func TestBackendNotes(t *testing.T) {
	base := EnvCheck{Backend: Backend{Name: "wsl2", Label: "WSL2", Source: "配置文件",
		SettingsFile: `C:\x\settings-store.json`, SettingKey: engineTypeKey, SettingVal: engineTypeWsl2}}
	notes := strings.Join(backendNotes(base), "\n")
	for _, kw := range []string{"当前虚拟化后端：WSL2", engineTypeKey} {
		if !strings.Contains(notes, kw) {
			t.Errorf("体检文案缺少 %q:\n%s", kw, notes)
		}
	}
	// 有结论时排在最前，且带下一步
	warn := base
	warn.Backend.Verdict = "WSL2 没生效：配置里 wslEngineEnabled=true，引擎实际跑在 Hyper-V"
	warn.Backend.Fix = []string{"重跑一次「切换后端」"}
	lines := backendNotes(warn)
	if !strings.HasPrefix(lines[0], "WSL2 没生效") {
		t.Errorf("结论应排第一行，实际: %v", lines[:2])
	}
	if lines[1] != "重跑一次「切换后端」" {
		t.Errorf("结论后应紧跟下一步，实际: %v", lines[:2])
	}
	// Hyper-V 后端必须把 bind mount 的风险说出来，不能只报"当前 Hyper-V"
	hv := base
	hv.Backend = Backend{Name: "hyperv", Label: "Hyper-V", Source: "引擎内核串"}
	hv.HypervInstalled, hv.HypervRunning, hv.HypervSupported = true, true, true
	notes = strings.Join(backendNotes(hv), "\n")
	for _, kw := range []string{"File sharing", "数据卷", "可以切换"} {
		if !strings.Contains(notes, kw) {
			t.Errorf("Hyper-V 文案缺少 %q:\n%s", kw, notes)
		}
	}
	// 家庭版：不能给"点启用 Hyper-V"的错误指引
	home := base
	home.HypervSupported, home.EditionID = false, "Core"
	if !strings.Contains(strings.Join(backendNotes(home), "\n"), "不提供 Hyper-V") {
		t.Error("不支持的系统版本应直接说明只能用 WSL2")
	}
}

func TestEditionInfo(t *testing.T) {
	ed, supported := editionInfo()
	if ed == "" {
		t.Skip("读不到 EditionID（非 Windows 或注册表受限）")
	}
	if strings.Contains(ed, "Core") && supported {
		t.Errorf("家庭版 %q 不应判定为支持 Hyper-V", ed)
	}
	t.Logf("本机 EditionID=%q supported=%v", ed, supported)
}

func TestOrUnknown(t *testing.T) {
	if got := orUnknown("  "); got != "未知" {
		t.Errorf("空值应显示未知，实际 %q", got)
	}
	if got := orUnknown("Professional"); got != "Professional" {
		t.Errorf("原样返回才对，实际 %q", got)
	}
}

func TestBackendNameOf(t *testing.T) {
	if got := backendNameOf(true); got != "wsl2" {
		t.Errorf("true 应为 wsl2，实际 %q", got)
	}
	if got := backendNameOf(false); got != "hyperv" {
		t.Errorf("false 应为 hyperv，实际 %q", got)
	}
}

// switchVerify 是“提示切换成功、面板一直显示 Hyper-V”这类矛盾的堵口：
// 结论必须与 detectBackend 同源，不能只看配置写没写。
func TestSwitchVerify(t *testing.T) {
	t.Run("核上了才算成功", func(t *testing.T) {
		st, note := switchVerify("wsl2", switchEvidence{Eng: EngineInfo{Available: true, Kernel: "5.15.157.1-microsoft-standard-WSL2"}})
		if st != "ok" {
			t.Errorf("应为 ok，实际 %s / %s", st, note)
		}
		if !strings.Contains(note, "核验通过") {
			t.Errorf("成功也要说清依据：%s", note)
		}
	})
	t.Run("回落 Hyper-V 时报错不谎报", func(t *testing.T) {
		st, note := switchVerify("wsl2", switchEvidence{
			Eng:     EngineInfo{Available: true, Kernel: "7.0.14-linuxkit"},
			WslNote: "默认版本: 2；但 wsl -l -v 说：未安装适用于 Linux 的 Windows 子系统",
		})
		if st != "fallback" {
			t.Fatalf("应为 fallback，实际 %s", st)
		}
		for _, kw := range []string{"Hyper-V", "静默沿用", "更新 WSL2", "启用 WSL2", "未安装适用于 Linux"} {
			if !strings.Contains(note, kw) {
				t.Errorf("提示缺少 %q：%s", kw, note)
			}
		}
	})
	t.Run("内核串不带 wsl 但发行版在跑时不下结论", func(t *testing.T) {
		// 新版 Docker Desktop 可能用 linuxkit 内核串跑 WSL2 后端：不能误报失败
		st, note := switchVerify("wsl2", switchEvidence{
			Eng:       EngineInfo{Available: true, Kernel: "7.0.14-linuxkit"},
			DistroRun: true,
		})
		if st != "unknown" {
			t.Errorf("信号对不上时应为 unknown，实际 %s", st)
		}
		if !strings.Contains(note, "docker-desktop 发行版正在跑") {
			t.Errorf("应说清两个信号：%s", note)
		}
	})
	t.Run("反方向回落也算失败", func(t *testing.T) {
		st, note := switchVerify("hyperv", switchEvidence{Eng: EngineInfo{Available: true, Kernel: "6.6.36.2-microsoft-standard-WSL2"}})
		if st != "fallback" || !strings.Contains(note, "WSL2") {
			t.Errorf(" %s / %s", st, note)
		}
	})
	t.Run("内核串认不出只算未核验", func(t *testing.T) {
		st, note := switchVerify("wsl2", switchEvidence{Eng: EngineInfo{Available: true, Kernel: ""}})
		if st != "unknown" {
			t.Errorf("内核串缺失不能当成 ok，实际 %s", st)
		}
		if !strings.Contains(note, "刷新") {
			t.Errorf("应给出下一步：%s", note)
		}
	})
	t.Run("引擎未应答", func(t *testing.T) {
		st, note := switchVerify("wsl2", switchEvidence{Eng: EngineInfo{Available: false, Kernel: "5.15.157.1-microsoft-standard-WSL2"}})
		if st != "unknown" || !strings.Contains(note, "未应答") {
			t.Errorf(" %s / %s", st, note)
		}
	})
}

// wslBroken / wslNoDistro 得分开：前者意味着 WSL2 后端用不了（本机实测就卡在这），
// 后者只是还没发行版，Docker Desktop 会自建 docker-desktop，不能当成坏了。
func TestWslBrokenVsNoDistro(t *testing.T) {
	for _, s := range []string{
		"未安装适用于 Linux 的 Windows 子系统。可通过...",
		"The Windows Subsystem for Linux is not installed.",
		"无法启动服务。虚拟机平台未启用。",
		"wsl.exe : 命令未找到",
	} {
		if !wslBroken(s) {
			t.Errorf("应判为组件不可用：%q", s)
		}
		if wslNoDistro(s) {
			t.Errorf("%q 不属于“只是没发行版”", s)
		}
	}
	for _, s := range []string{"未找到任何发行版。请运行 wsl --install", "No distributions are installed"} {
		if !wslNoDistro(s) {
			t.Errorf("应判为没发行版：%q", s)
		}
		if wslBroken(s) {
			t.Errorf("%q 不该被判成组件不可用", s)
		}
		if !wslNegative(s) {
			t.Errorf("总结判据应包含它：%q", s)
		}
	}
	for _, s := range []string{"默认分发: docker-desktop\r\n默认版本: 2", "Default Distribution: docker-desktop"} {
		if wslNegative(s) {
			t.Errorf("不应误判：%q", s)
		}
	}
}

// 虚拟机平台未启用是 WSL2 后端特有的硬卡点（启用需重启），必须与一般的“WSL 没装”分开识别：
// 命中它才能尽早中止引擎等待并给出启用+重启指引。
func TestVmPlatformDisabledWords(t *testing.T) {
	for _, s := range []string{
		"无法启动服务。虚拟机平台未启用。",
		"Virtual Machine Platform is not enabled",
		"WSL  requires the Virtual Machine Platform Windows feature; it is not enabled",
	} {
		if !vmPlatformDisabledWords(s) {
			t.Errorf("应判为虚拟机平台未启用：%q", s)
		}
	}
	// 只提“未安装 WSL”而不涉虚拟机平台，或正常回显，都不该误判
	for _, s := range []string{
		"未安装适用于 Linux 的 Windows 子系统",
		"默认分发: docker-desktop 默认版本: 2",
	} {
		if vmPlatformDisabledWords(s) {
			t.Errorf("不该被判成虚拟机平台未启用：%q", s)
		}
	}
}

func TestWslStateLinesAndDistroRunning(t *testing.T) {
	out := "  NAME                   STATE           VERSION\n" +
		"* docker-desktop         Running         2\n" +
		"  Ubuntu-24.04           Stopped         2\n"
	if got := len(wslStateLines(out)); got != 2 {
		t.Fatalf("应数出 2 个发行版，实际 %d", got)
	}
	if !wslDistroRunning(out, "docker-desktop") {
		t.Error("docker-desktop 在跑")
	}
	if wslDistroRunning(out, "Ubuntu-24.04") {
		t.Error("Ubuntu 是 Stopped，不算在跑")
	}
	if wslDistroRunning(out, "no-such-distro") {
		t.Error("不存在的发行版应为 false")
	}
	// 中文系统的状态词也要认
	zh := "* docker-desktop         已停止            2\n"
	if wslDistroRunning(zh, "docker-desktop") {
		t.Error("已停止不算在跑")
	}
	if !wslDistroRunning("* docker-desktop         正在运行            2\n", "docker-desktop") {
		t.Error("正在运行应认得")
	}
	if got := wslStateLines("未安装适用于 Linux 的 Windows 子系统"); len(got) != 0 {
		t.Errorf("报错文案不该数出发行版：%v", got)
	}
}

// 本机实测的两种原文：中文“没有已安装的分发”、英文 No installed distribution
func TestWslNoDistroRealWording(t *testing.T) {
	s := "适用于 Linux 的 Windows 子系统没有已安装的分发。可按照以下说明安装分发来解决此问题"
	if !wslNoDistro(s) {
		t.Errorf("应认出真实措辞：%q", s)
	}
	if wslBroken(s) {
		t.Error("这只是没发行版，不是组件坏了")
	}
	if !wslNoDistro("WSL has no installed distribution") {
		t.Error("英文措辞也要认")
	}
}

// 星号后带一个空格（真实输出），不能把 "*" 当成发行版名
func TestParseWslDistros(t *testing.T) {
	out := "  NAME                   STATE           VERSION\n" +
		"* docker-desktop         Running         2\n" +
		"  Ubuntu-24.04           Stopped         2\n"
	got := parseWslDistros(out)
	if len(got) != 2 || got[0].Name != "docker-desktop" || got[0].State != "running" || got[1].State != "stopped" {
		t.Fatalf("解析异常: %+v", got)
	}
	if n := parseWslDistros("适用于 Linux 的 Windows 子系统没有已安装的分发。"); len(n) != 0 {
		t.Errorf("报错文案不该出解析结果: %+v", n)
	}
	s := wslState{Distros: got}
	if strings.Join(s.distroLabels(), ",") != "docker-desktop(running),Ubuntu-24.04(stopped)" {
		t.Errorf("展示串异常: %v", s.distroLabels())
	}
}

// 体检结论：配置要 WSL2、引擎却在 Hyper-V 时，必须一句话说清并给出下一步
func TestBackendVerdict(t *testing.T) {
	hv := Backend{Name: "hyperv", Label: "Hyper-V", Source: "引擎内核串", Kernel: "7.0.14-linuxkit"}

	t.Run("一致时不唠叨", func(t *testing.T) {
		ok := hv
		ok.SettingKey, ok.SettingVal, ok.SettingWant = engineTypeKey, engineTypeHyperv, "hyperv"
		if v, f := backendVerdict(EnvCheck{Backend: ok}); v != "" || len(f) != 0 {
			t.Errorf("一致时不该给结论: %q %v", v, f)
		}
	})

	t.Run("旧键被新版本忽略", func(t *testing.T) {
		b := hv
		b.SettingKey, b.SettingVal, b.SettingWant = wslEngineKey, "true", "wsl2"
		v, fix := backendVerdict(EnvCheck{Backend: b, WslInstalled: true})
		for _, kw := range []string{"WSL2 没生效", wslEngineKey, "Hyper-V", "7.0.14-linuxkit"} {
			if !strings.Contains(v, kw) {
				t.Errorf("结论缺少 %q: %s", kw, v)
			}
		}
		joined := strings.Join(fix, "\n")
		if !strings.Contains(joined, engineTypeKey) {
			t.Errorf("应指出新键名 %s: %v", engineTypeKey, fix)
		}
	})

	t.Run("新键也对但没生效", func(t *testing.T) {
		b := hv
		b.SettingKey, b.SettingVal, b.SettingWant = engineTypeKey, engineTypeWsl2, "wsl2"
		_, fix := backendVerdict(EnvCheck{Backend: b, WslInstalled: true})
		if strings.Contains(strings.Join(fix, "\n"), "旧键") {
			t.Errorf("键名对时不该把锅扣给旧键: %v", fix)
		}
		if !strings.Contains(strings.Join(fix, "\n"), "设置里手动选") {
			t.Errorf("应给出手动路径: %v", fix)
		}
	})

	t.Run("WSL2 本身不可用时点明", func(t *testing.T) {
		b := hv
		b.SettingKey, b.SettingVal, b.SettingWant = engineTypeKey, engineTypeWsl2, "wsl2"
		v, fix := backendVerdict(EnvCheck{Backend: b, WslInstalled: false, WslStatus: "未安装适用于 Linux 的 Windows 子系统"})
		if !strings.Contains(v, "WSL2 本身不可用") {
			t.Errorf("应点明 WSL2 不可用: %s", v)
		}
		if !strings.Contains(strings.Join(fix, "\n"), "wsl --update") {
			t.Errorf("应给出 wsl --update: %v", fix)
		}
	})

	t.Run("反向不一致也要说", func(t *testing.T) {
		b := Backend{Name: "wsl2", Label: "WSL2", Source: "引擎内核串", Kernel: "5.15.157.1-microsoft-standard-WSL2"}
		b.SettingKey, b.SettingVal, b.SettingWant = engineTypeKey, engineTypeHyperv, "hyperv"
		v, fix := backendVerdict(EnvCheck{Backend: b})
		if !strings.Contains(v, "要求的是 Hyper-V") || len(fix) == 0 {
			t.Errorf("结论: %s / %v", v, fix)
		}
	})

	t.Run("配置没写过不下结论", func(t *testing.T) {
		if v, _ := backendVerdict(EnvCheck{Backend: hv}); v != "" {
			t.Errorf("没显式配置时不该猜: %s", v)
		}
	})
}
