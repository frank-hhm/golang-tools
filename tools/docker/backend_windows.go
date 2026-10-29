//go:build windows

package docker

// ============================================================
// Docker Desktop 虚拟化后端：识别 WSL2 / Hyper-V，并按用户显式确认代改配置
//
// 关键事实（决定了本文件的边界）：
//   - 两个后端对外暴露的都是同一条命名管道与同一套 Engine API，
//     所以容器/镜像/日志/MySQL 容器模式不需要因为换后端而改任何代码。
//   - 后端开关由 Docker Desktop 自己维护，落在
//     %APPDATA%\Docker\settings-store.json（新版本）或 settings.json，
//     键名 wslEngineEnabled。程序运行中直接改会被它回写覆盖，
//     因此切换必须：先停 Desktop → 改文件 → 回读校验 → 再拉起。
//   - 切到 Hyper-V 会新建一个空的 Linux VM：原有容器与已拉取的镜像都不在了
//     （本工具 bind mount 的数据目录不受影响），所以动作前有硬门禁。
//   - Hyper-V 角色需要 Pro/Enterprise/Education + 提权启用 + 重启，
//     启用角色属于改系统组件，一律 SW_SHOW 提权且不代用户点重启。
// ============================================================

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"Tools/services/winrun"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	settingsStoreName  = "settings-store.json"
	settingsLegacyName = "settings.json"
	// engineTypeKey Docker Desktop 4.3x+ 的后端开关，取值 wsl2 | docker-vmm | hyper-v。
	// 本机 4.96.3 实测：写成 wslEngineEnabled 时它只记一条
	// “unknown settings found: {wslEngineEnabled:true}” 然后照旧跑 hyper-v。
	engineTypeKey    = "EngineTypeWindows"
	engineTypeWsl2   = "wsl2"
	engineTypeHyperv = "hyper-v"
	engineTypeVmm    = "docker-vmm"
	// wslEngineKey 旧版本布尔键，仅老版本 Docker Desktop 认
	wslEngineKey     = "wslEngineEnabled"
	hypervFeature    = "Microsoft-Hyper-V-All"
	vmmsPath         = `C:\Windows\System32\vmms.exe`
	settingsStickyTO = 15 * time.Second
)

/* ============================ 配置文件 ============================ */

// dockerSettingsDir 返回 %APPDATA%\Docker（Docker Desktop 自己写配置的地方）
func dockerSettingsDir() (string, error) {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		return "", fmt.Errorf("取不到 APPDATA 环境变量")
	}
	dir := filepath.Join(appdata, "Docker")
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("未找到 Docker Desktop 配置目录: %s（未安装或尚未首次启动过）", dir)
	}
	return dir, nil
}

// findSettingsFile 定位承载 wslEngineEnabled 的配置文件：
// 新版用 settings-store.json，旧版用 settings.json；两个都在时以 store 为准。
func findSettingsFile() (string, map[string]any, error) {
	dir, err := dockerSettingsDir()
	if err != nil {
		return "", nil, err
	}
	for _, name := range []string{settingsStoreName, settingsLegacyName} {
		p := filepath.Join(dir, name)
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			return "", nil, fmt.Errorf("配置文件格式无法解析: %s（%v）", p, err)
		}
		if _, ok := m[wslEngineKey]; ok {
			return p, m, nil
		}
		// store 里没这个键：再看旧文件；都没有则用 store 作为写入目标
	}
	return filepath.Join(dir, settingsStoreName), map[string]any{}, nil
}

// settingFileForWrite 取当前生效的配置文件路径（只读用途，不改内容；取不到返回空）
func settingFileForWrite() string {
	p, _, err := findSettingsFile()
	if err != nil {
		return ""
	}
	return p
}

// engineChoice 后端开关的读取结果：键名随 Docker Desktop 版本变化，读写都得认两种。
type engineChoice struct {
	File      string // 承载该键的配置文件
	Key       string // 实际键名
	Value     string // 文件里的原值
	Set       bool   // 文件里是否显式写了这个键
	LegacyVal string // 遗留的旧键 wslEngineEnabled 原值（新版本不读它，只用于解释历史操作）
}

// settingKeyFor 定出这份配置里该读写哪个键。以文件里实际存在的键为准，
// 这样老版本 Docker Desktop（只有 wslEngineEnabled）不会被写坏：
// 有 EngineTypeWindows → 新键；只有旧键 → 旧键；都没有则按是否带 SettingsVersion 判断。
func settingKeyFor(m map[string]any) string {
	if _, ok := m[engineTypeKey]; ok {
		return engineTypeKey
	}
	if _, ok := m[wslEngineKey]; ok {
		return wslEngineKey
	}
	if _, ok := m["SettingsVersion"]; ok {
		return engineTypeKey
	}
	return wslEngineKey
}

// usesEngineType 这份配置用的是新键吗
func usesEngineType(m map[string]any) bool {
	return settingKeyFor(m) == engineTypeKey
}

// readEngineChoice 读后端开关（新版本读 EngineTypeWindows，旧版本读 wslEngineEnabled）
func readEngineChoice() engineChoice {
	p, m, err := findSettingsFile()
	if err != nil {
		return engineChoice{}
	}
	c := engineChoice{File: p, Key: settingKeyFor(m)}
	if c.Key == engineTypeKey {
		// 新格式下旧键只是历史残留：Docker Desktop 会把它当“未知设置”记一笔后忽略，
		// 但仍要读出来，否则用户“我明明设过 WSL2”这件事无法解释
		if v, ok := m[wslEngineKey]; ok {
			c.LegacyVal = fmt.Sprint(v)
		}
	}
	v, ok := m[c.Key]
	if !ok {
		return c
	}
	c.Set = true
	switch b := v.(type) {
	case bool:
		c.Value = "false"
		if b {
			c.Value = "true"
		}
	case string:
		c.Value = strings.TrimSpace(b)
	default:
		c.Value = fmt.Sprint(v)
	}
	return c
}

// backendOf 把配置值归一化成面板的后端名：wsl2 | hyperv | ""（认不出的值不当意图）。
// docker-vmm 是 Docker 自带的轻量虚拟机，和 Hyper-V 一样属于“非 WSL2”，归入同一档。
func (c engineChoice) backendOf() string {
	if !c.Set {
		return ""
	}
	switch strings.ToLower(c.Value) {
	case engineTypeWsl2, "true":
		return "wsl2"
	case engineTypeHyperv, engineTypeVmm, "false":
		return "hyperv"
	}
	return ""
}

// describe 给日志用的一行描述，如 "EngineTypeWindows=hyper-v"
func (c engineChoice) describe() string {
	if c.Key == "" {
		return ""
	}
	if !c.Set {
		return c.Key + "=未写"
	}
	return c.Key + "=" + c.Value
}

// writeEngineChoice 只改后端开关这一个键，其余字段原样保留；
// 先备份 .gobak 再临时文件改名，避免半写把配置打坏。返回人类可读的变更说明。
func writeEngineChoice(useWsl2 bool) (string, error) {
	p, m, err := findSettingsFile()
	if err != nil {
		return "", err
	}
	key := settingKeyFor(m)
	var want any = useWsl2
	if key == engineTypeKey {
		want = engineTypeHyperv
		if useWsl2 {
			want = engineTypeWsl2
		}
	}
	prev, had := m[key]
	m[key] = want
	// 新格式里若还残留旧键，一并改成同一个意图：否则文件自相矛盾，排障时容易看错
	if key == engineTypeKey {
		if _, ok := m[wslEngineKey]; ok {
			m[wslEngineKey] = useWsl2
		}
	}
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return "", fmt.Errorf("序列化配置失败: %w", err)
	}
	if had {
		if b, err := os.ReadFile(p); err == nil {
			if err := os.WriteFile(p+".gobak", b, 0o644); err != nil {
				return "", fmt.Errorf("备份配置失败（未改动）: %w", err)
			}
		}
	}
	tmp := p + ".gotmp"
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", fmt.Errorf("配置目录不可用: %w", err)
	}
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return "", fmt.Errorf("写入临时配置失败: %w", err)
	}
	if err := os.Rename(tmp, p); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("配置就位失败: %w", err)
	}
	if !had {
		return fmt.Sprintf("%s：原本缺省 → %v，写入 %s", key, want, p), nil
	}
	return fmt.Sprintf("%s：%v → %v（备份 %s.gobak）", key, prev, want, filepath.Base(p)), nil
}

/* ============================ 后端判定 ============================ */

// backendFromKernel 用引擎上报的内核串判后端：
// WSL2 后端内核固定带 microsoft-standard-WSL2 后缀，Hyper-V 后端是 Docker 自带 VM 内核。
func backendFromKernel(kernel string) string {
	l := strings.ToLower(kernel)
	if l == "" {
		return ""
	}
	if strings.Contains(l, "wsl") {
		return "wsl2"
	}
	return "hyperv"
}

// labelOfBackend 前端展示名
func labelOfBackend(name string) string {
	switch name {
	case "wsl2":
		return "WSL2"
	case "hyperv":
		return "Hyper-V"
	default:
		return "未知"
	}
}

// detectBackend 综合判定当前后端。优先级：引擎内核串（运行事实）> 配置文件（用户意图）> 缺省。
// 二者不一致时以内核串为准，并把配置文件一并回传，便于提示"改完没生效/需要重启引擎"。
func detectBackend(eng EngineInfo) Backend {
	b := Backend{Name: "unknown", Label: labelOfBackend("unknown"), Source: "无"}
	c := readEngineChoice()
	b.SettingKey, b.SettingVal, b.SettingsFile = c.Key, c.Value, c.File
	b.LegacyVal = c.LegacyVal
	b.SettingWant = c.backendOf()
	b.Kernel = eng.Kernel
	if k := backendFromKernel(eng.Kernel); k != "" {
		b.Name, b.Label = k, labelOfBackend(k)
		b.Source = "引擎内核串"
		return b
	}
	if b.SettingWant != "" {
		b.Name, b.Label = b.SettingWant, labelOfBackend(b.SettingWant)
		b.Source = "配置文件"
		return b
	}
	// 引擎没跑、配置也没显式写过：Docker Desktop 的出厂默认是 WSL2 后端
	b.Name, b.Label, b.Source = "wsl2", labelOfBackend("wsl2"), "缺省（Docker Desktop 默认 WSL2）"
	return b
}

/* ============================ Hyper-V 状态 ============================ */

// editionInfo 读系统版本：Hyper-V 角色只在 Pro/Enterprise/Education 上提供。
// 用 EditionID（英文不变量），不用 ProductName 里的本地化文案。
func editionInfo() (editionID string, supported bool) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`, registry.READ)
	if err != nil {
		return "", false
	}
	defer k.Close()
	ed, _, _ := k.GetStringValue("EditionID")
	ed = strings.TrimSpace(ed)
	switch ed {
	case "Professional", "Enterprise", "Education", "ServerRdsh", "ProfessionalEducation", "ProfessionalStudent":
		return ed, true
	}
	l := strings.ToLower(ed)
	return ed, strings.Contains(l, "enterprise") || strings.Contains(l, "professional") || strings.Contains(l, "education")
}

// hypervisorRunning 查管理程序是否已在跑（普通权限可查 WMI）。
// 取不到就返回 false：只影响提示措辞，不影响任何写操作。
func hypervisorRunning() bool {
	out, err := winrun.RunHidden(20*time.Second, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"(Get-CimInstance -ClassName Win32_ComputerSystem).HypervisorPresent")
	if err != nil {
		return false
	}
	return strings.Contains(strings.ToLower(oneLine(out)), "true")
}

/* ============================ 体检集成 ============================ */

// fillBackend 把后端与 Hyper-V 状态补进体检结果（只读，不改任何东西）
func fillBackend(c *EnvCheck) {
	eng, _ := GetEngineStatus()
	if eng.Available {
		c.EngineAvailable = true
	}
	c.Backend = detectBackend(eng)
	c.Backend.Verdict, c.Backend.Fix = backendVerdict(*c)
	ed, supported := editionInfo()
	c.EditionID, c.HypervSupported = ed, supported
	c.HypervInstalled = fileExists(vmmsPath)
	c.HypervRunning = hypervisorRunning()
}

// backendVerdict 把“配置想要的后端”和“引擎实际跑的后端”对起来看，得出一句话结论。
// 一致时返回空（不唠叨）；不一致时把依据、原因和下一步都说清，
// 因为 Docker Desktop 在目标后端不可用时会静默沿用旧后端，从不报错。
func backendVerdict(c EnvCheck) (string, []string) {
	b := c.Backend
	if b.SettingWant == "" || b.Name == "unknown" || b.SettingWant == b.Name {
		return "", nil
	}
	wslPart := ""
	if !c.WslInstalled {
		wslPart = "；而且 WSL2 本身不可用（" + oneLine(truncate(c.WslStatus, 120)) + "）"
	}
	verdict := fmt.Sprintf("WSL2 没生效：配置里 %s=%s，引擎实际跑在 %s（内核 %s）%s。",
		orUnknown(b.SettingKey), orUnknown(b.SettingVal), b.Label, orUnknown(b.Kernel), wslPart)
	var fix []string
	if b.SettingWant == "wsl2" {
		if !c.WslInstalled {
			fix = append(fix, "先点「更新 WSL2」（wsl --update），必要时重启计算机，再回来重跑一次切换")
		}
		if b.SettingKey == wslEngineKey {
			fix = append(fix, "Docker Desktop 新版的后端开关是 "+engineTypeKey+"（wsl2 / hyper-v / docker-vmm），旧键 "+
				wslEngineKey+" 会被当成未知设置丢弃；重跑一次「切换后端」会自动改对新键，或直接在它自己的设置里手动选 WSL2")
		} else {
			fix = append(fix, "配置键已写对但 Docker Desktop 没采纳：从托盘彻底退出后重新打开，或在它的设置里手动选 WSL2，再回来刷新")
		}
		fix = append(fix, "面板上的「虚拟化后端」取的是引擎内核串，就是实际状态，以它为准")
		return verdict, fix
	}
	verdict = fmt.Sprintf("配置里 %s=%s，要求的是 %s，但引擎实际跑在 %s（内核 %s）%s。",
		orUnknown(b.SettingKey), orUnknown(b.SettingVal), labelOfBackend(b.SettingWant), b.Label,
		orUnknown(b.Kernel), wslPart)
	fix = append(fix, "重跑一次「切换后端」把 %s 写进去并核验；若仍不生效，从托盘退出 Docker Desktop 后重新打开再试",
		labelOfBackend(b.SettingWant))
	fix = append(fix, "面板上的「虚拟化后端」取的是引擎内核串，就是实际状态，以它为准")
	return verdict, fix
}

// backendNotes 后端相关的待办文案（前端逐行展示）
func backendNotes(c EnvCheck) []string {
	var n []string
	// 结论与下一步排在最前：用户第一眼要看到的是“到底生效了没”
	if v := strings.TrimSpace(c.Backend.Verdict); v != "" {
		n = append(n, v)
		n = append(n, c.Backend.Fix...)
	}
	n = append(n, fmt.Sprintf("当前虚拟化后端：%s（依据：%s）", c.Backend.Label, c.Backend.Source))
	switch {
	case c.HypervInstalled && c.HypervRunning:
		n = append(n, "Hyper-V 角色已安装且管理程序在跑，可以切换到 Hyper-V 后端。")
	case c.HypervInstalled:
		n = append(n, "Hyper-V 角色已安装，但管理程序未在跑：切换后若引擎起不来，重启计算机再试。")
	case c.HypervSupported:
		n = append(n, "Hyper-V 角色尚未安装（系统版本支持）：可点「启用 Hyper-V」提权安装，需重启计算机才生效。")
	default:
		n = append(n, fmt.Sprintf("当前系统版本（%s）不提供 Hyper-V 角色，只能用 WSL2 后端。", orUnknown(c.EditionID)))
	}
	if c.Backend.Name == "hyperv" {
		n = append(n, "Hyper-V 后端下 bind mount 走文件共享：Windows 目录需授权 File sharing，且 chown/AIO 语义与 WSL2 不同，MySQL 这类要改文件属主的数据库建议用数据卷。")
	}
	if c.Backend.SettingsFile != "" {
		n = append(n, fmt.Sprintf("后端开关记录在 %s 的 %s=%s，切换会先停 Docker Desktop 再改并回读校验。",
			c.Backend.SettingsFile, orUnknown(c.Backend.SettingKey), orUnknown(c.Backend.SettingVal)))
	}
	// 旧键还在且与真生效的键不一致：把“以前切过但没生效”这件事点明
	if lv := strings.ToLower(c.Backend.LegacyVal); lv != "" {
		legacyWant := "hyperv"
		if lv == "true" {
			legacyWant = "wsl2"
		}
		if legacyWant != c.Backend.SettingWant {
			n = append(n, fmt.Sprintf("文件里还留着旧键 %s=%s：新版 Docker Desktop 把后端开关改成了 %s，旧键会被当成未知设置直接忽略，所以以前那次切换没有生效。",
				wslEngineKey, c.Backend.LegacyVal, engineTypeKey))
		}
	}
	return n
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "未知"
	}
	return s
}

/* ============================ 启用 Hyper-V ============================ */

// SwitchGateError 切换动作的硬门禁：任何"改系统组件/清 VM"级别的动作都必须显式确认
func SwitchGateError(useWsl2, confirmed bool) error {
	if confirmed {
		return nil
	}
	to := "Hyper-V"
	if useWsl2 {
		to = "WSL2"
	}
	return fmt.Errorf("切换到 %s 后端会新建一个空的 Linux 虚拟机：现有容器与已拉取的镜像不会保留（bind mount 的数据目录不受影响），请显式确认后再执行", to)
}

// EnableHyperV 提权启用 Hyper-V 角色（含管理工具），不自动重启。
// confirm 是硬门禁：这是改系统组件的动作，未经确认一律拒绝。
func EnableHyperV(confirm bool) error {
	if !confirm {
		err := fmt.Errorf("启用 Hyper-V 会添加系统组件并需要重启计算机，请先确认已知晓")
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}
	ed, supported := editionInfo()
	if !supported {
		err := fmt.Errorf("当前系统版本（%s）不提供 Hyper-V 角色，无法启用；请改用 WSL2 后端", orUnknown(ed))
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}
	if fileExists(vmmsPath) {
		emitSetup(SetupProgress{Stage: "done", Percent: 100, Note: "Hyper-V 角色似乎已安装（vmms.exe 存在），无需重复启用"})
		return nil
	}
	emitSetup(SetupProgress{Stage: "info",
		Note: "即将提权启用 Hyper-V（dism /enable-feature " + hypervFeature + "），完成后需重启计算机才生效"})
	code, err := winrun.RunElevatedWait(winrun.Elevated{
		File:    "dism.exe",
		Params:  fmt.Sprintf("/online /enable-feature /featurename:%s /all /norestart", hypervFeature),
		Show:    windows.SW_SHOW,
		Timeout: wslInstallTO,
	})
	if err != nil {
		if winrun.IsUACCancelled(err) {
			emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
			return err
		}
		note := fmt.Sprintf("启用 Hyper-V 失败: %v（退出码 %d）。也可在「启用或关闭 Windows 功能」里勾选 Hyper-V 后重启", err, code)
		emitSetup(SetupProgress{Stage: "error", Note: note})
		return fmt.Errorf("%s", note)
	}
	if code != 0 {
		note := fmt.Sprintf("dism 退出码 %d，未确认启用成功；请查看上方 dism 窗口输出", code)
		emitSetup(SetupProgress{Stage: "error", Note: note})
		return fmt.Errorf("%s", note)
	}
	c, _ := CheckEnv("") // 只看 Hyper-V 角色状态，与包存放目录无关
	note := "Hyper-V 组件已提交启用："
	if c.HypervInstalled {
		note += "已检测到 vmms.exe，重启计算机后管理程序才会真正在跑"
	} else {
		note += "需要重启计算机才生效，重启后再回到本面板切换后端"
	}
	emitSetup(SetupProgress{Stage: "done", Percent: 100, Note: note})
	return nil
}

// backendNameOf 目标后端的内部名
func backendNameOf(useWsl2 bool) string {
	if useWsl2 {
		return "wsl2"
	}
	return "hyperv"
}

// switchEvidence 切换核验用到的三份事实：全部由调用方取好，函数本身不碰外部命令，便于单测
type switchEvidence struct {
	Eng       EngineInfo
	Cfg       string // 实际写入的配置，如 "EngineTypeWindows=wsl2"
	WslNote   string // wslProbe 的 WSL2 现状说明
	DistroRun bool   // docker-desktop 发行版在跑（“引擎确实在用 WSL2”的正面证据）
}

// switchVerify 切换后的核验：只用面板同一套判据（内核串）得结论，
// 避开“这里说切好了、那里还写着 Hyper-V”。
//   - ok：引擎内核串就是目标后端
//   - unknown：证据不够或两个信号互相矛盾（不算成功，只算已改配置）
//   - fallback：引擎跑在另一个后端上（Docker Desktop 在 WSL2 不可用时静默沿用旧后端）
func switchVerify(want string, ev switchEvidence) (status, note string) {
	if !ev.Eng.Available {
		return "unknown", fmt.Sprintf("配置已改为 %s，但引擎未应答，无法确认实际后端", labelOfBackend(want))
	}
	got := backendFromKernel(ev.Eng.Kernel)
	switch {
	case got == want:
		return "ok", fmt.Sprintf("已切换到 %s 后端并核验通过（内核 %s）", labelOfBackend(want), orUnknown(ev.Eng.Kernel))
	case got == "":
		return "unknown", fmt.Sprintf("配置已改写且引擎已应答，但内核串认不出后端（%s）：请稍后刷新面板确认实际后端",
			orUnknown(ev.Eng.Kernel))
	case want == "wsl2" && ev.DistroRun:
		// 新版 Docker Desktop 的内核串可能不带 wsl，但 WSL 里的 docker-desktop 确实在跑：
		// 此时不能断言失败，只能说“两个信号对不上”，以后端标签为准
		return "unknown", fmt.Sprintf(
			"配置已改为 WSL2，引擎内核串是 %s（认作 %s），但 WSL 里的 docker-desktop 发行版正在跑：两者信号对不上，"+
				"以后端标签为准，不拦你继续用", orUnknown(ev.Eng.Kernel), labelOfBackend(got))
	default:
		wslPart := ""
		if strings.TrimSpace(ev.WslNote) != "" {
			wslPart = "当前 WSL2 现状：" + ev.WslNote + "。"
		}
		cfg := strings.TrimSpace(ev.Cfg)
		if cfg == "" {
			cfg = fmt.Sprintf("%s=%v", wslEngineKey, want == "wsl2")
		}
		return "fallback", fmt.Sprintf(
			"配置已写入 %s，但引擎实际跑在 %s 后端（内核 %s）：Docker Desktop 在目标后端不可用时会静默沿用旧后端，不报错。%s",
			cfg, labelOfBackend(got), orUnknown(ev.Eng.Kernel), wslPart) +
			"若目标是 WSL2：先点「更新 WSL2」（wsl --update）或「启用 WSL2」，必要时重启计算机后确认 wsl --status 与 wsl -l -v 都正常，再回来切换；" +
			"若打算就用当前后端：无需再动，容器管理一切照常（bind mount 需授权文件共享）。"
	}
}

/* ============================ 切换后端 ============================ */

// desktopProcs 停 Docker Desktop 要收尾的进程名：GUI 主进程带着 settings 回写，
// 必须先干掉，改文件才不会被覆盖。
var desktopProcs = []string{"Docker Desktop.exe", "com.docker.backend.exe", "com.docker.build.exe"}

// stopDesktop 结束用户态进程并停掉系统服务（提权），返回处理摘要
func stopDesktop() (string, error) {
	var parts []string
	for _, name := range desktopProcs {
		if _, err := winrun.RunHidden(20*time.Second, "taskkill.exe", "/IM", name, "/T", "/F"); err != nil {
			parts = append(parts, name+": 未在运行或已结束")
			continue
		}
		parts = append(parts, name+": 已结束")
	}
	// com.docker.service 跑在 LocalSystem 下，普通权限停不动，提权停一次
	if _, err := winrun.RunElevatedWait(winrun.Elevated{
		File:    "net.exe",
		Params:  "stop com.docker.service",
		Show:    windows.SW_HIDE,
		Timeout: 60 * time.Second,
	}); err != nil {
		if winrun.IsUACCancelled(err) {
			return strings.Join(parts, "；"), err
		}
		// 服务本来就停着不算失败，把结论留在摘要里由用户判断
		parts = append(parts, "com.docker.service: 停止返回异常（可能本就未运行）")
	} else {
		parts = append(parts, "com.docker.service: 已停止")
	}
	// 给文件句柄与回写留个空档，避免改完立刻被覆盖
	time.Sleep(2 * time.Second)
	return strings.Join(parts, "；"), nil
}

// waitForSettingStuck 回读校验：Docker Desktop 若有残留进程回写配置，这里能抓到
func waitForSettingStuck(wantWsl2 bool) (string, bool) {
	deadline := time.Now().Add(settingsStickyTO)
	var file string
	for {
		c := readEngineChoice()
		file = c.File
		if c.Set && (c.backendOf() == "wsl2") == wantWsl2 {
			return file, true
		}
		if time.Now().After(deadline) {
			return file, false
		}
		time.Sleep(time.Second)
	}
}

/* ============================ 停止引擎 ============================ */

// StopEngine 停止 Docker Desktop 及其系统服务（面板上的「停止」）：
// 先叫停可能还在跑的「启动等待」，再结束 GUI/后台进程并提权停 com.docker.service。
// 停完引擎不可达是预期结果，不会自动拉起。
func StopEngine() error {
	CancelStart()
	emitSetup(SetupProgress{Stage: "info", Note: "正在停止 Docker Desktop 与其系统服务（需管理员权限停服务）…"})
	sum, err := stopDesktop()
	if err != nil {
		if winrun.IsUACCancelled(err) {
			note := "已取消停止（未授予管理员权限）：Docker Desktop 仍在运行；" + sum
			emitSetup(SetupProgress{Stage: "error", Note: note})
			return fmt.Errorf("%s", note)
		}
		emitSetup(SetupProgress{Stage: "error", Note: "停止失败: " + err.Error()})
		return err
	}
	// 停完再探一次：给的是“停了还是还活着”的事实，而不是“已发出停止命令”
	if eng, _ := GetEngineStatus(); eng.Available {
		note := "已发出停止命令，但引擎仍在应答（" + eng.Version + "）：可能有别的 Docker Engine 在跑；" + sum
		emitSetup(SetupProgress{Stage: "error", Note: note})
		return fmt.Errorf("%s", note)
	}
	emitSetup(SetupProgress{Stage: "done", Note: "Docker Desktop 已停止：" + sum})
	return nil
}

// RestartEngine 重启引擎（改 daemon.json 后让它生效）：停 → 拉起 → 等就绪
func RestartEngine() error {
	if err := StopEngine(); err != nil {
		return err
	}
	return StartEngine()
}

// SwitchBackend 代改 Docker Desktop 后端：停应用 → 改后端开关（新版 EngineTypeWindows /
// 旧版 wslEngineEnabled，按文件现有 schema 选）→ 回读校验 → 拉起 → 用内核串核验实际后端。
// useWsl2=true 切回 WSL2；false 切 Hyper-V（要求角色已安装，否则拒绝并给下一步）。
func SwitchBackend(useWsl2, confirmed bool) error {
	if err := SwitchGateError(useWsl2, confirmed); err != nil {
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}
	exe, ok := locateDesktopExe()
	if !ok {
		err := fmt.Errorf("未找到 Docker Desktop.exe，无法切换后端；请先完成安装")
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}
	if !useWsl2 && !fileExists(vmmsPath) {
		_, supported := editionInfo()
		tip := "可点「启用 Hyper-V」提权安装组件，装完需重启计算机"
		if !supported {
			tip = "当前系统版本不提供 Hyper-V 角色，请改用 WSL2 后端"
		}
		err := fmt.Errorf("Hyper-V 角色尚未安装（未检测到 vmms.exe），此时切换后引擎必然起不来；%s", tip)
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}
	if settingFileForWrite() == "" {
		// 连配置目录都没有：说明从没成功启动过 Desktop，此时改后端没有意义
		err := fmt.Errorf("未找到 Docker Desktop 配置目录（%%APPDATA%%\\Docker），请先让它正常启动一次")
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}

	to := "Hyper-V"
	if useWsl2 {
		to = "WSL2"
	}
	// 切到 WSL2 前先探一下 WSL 健不健康：不健康时 Docker Desktop 不报错，
	// 只是沿用旧的 Hyper-V，先把风险说清楚，最后还会用内核串核验。
	// 本机实测的命门：wsl --status 能正常回“默认版本: 2”，但 WSL 其实没装成。
	wslNote := ""
	if useWsl2 {
		ready, st := wslProbe()
		wslNote = st
		if !ready {
			emitSetup(SetupProgress{Stage: "info",
				Note: "WSL2 现状异常（" + st + "）：仍继续切换，但引擎可能沿用 Hyper-V；切换结果会以内核串为准核验"})
		} else {
			emitSetup(SetupProgress{Stage: "info", Note: "WSL2 现状：" + st})
		}
	}
	emitSetup(SetupProgress{Stage: "info", Note: "正在停止 Docker Desktop 与其系统服务…"})
	sum, err := stopDesktop()
	if err != nil {
		emitSetup(SetupProgress{Stage: "error", Note: "停止 Docker Desktop 失败（未改动配置）: " + err.Error()})
		return err
	}
	emitSetup(SetupProgress{Stage: "info", Note: "停止结果：" + sum})

	desc, err := writeEngineChoice(useWsl2)
	if err != nil {
		emitSetup(SetupProgress{Stage: "error", Note: "改配置失败: " + err.Error()})
		return err
	}
	emitSetup(SetupProgress{Stage: "info", Note: "配置已写入 " + desc})

	file, stuck := waitForSettingStuck(useWsl2)
	if !stuck {
		note := fmt.Sprintf("配置被回写覆盖（%v 内未见目标值，文件 %s）：Docker Desktop 可能仍在运行，请从托盘彻底退出后重试；本次未拉起引擎", settingsStickyTO, file)
		emitSetup(SetupProgress{Stage: "error", Note: note})
		return fmt.Errorf("%s", note)
	}

	emitSetup(SetupProgress{Stage: "starting", Note: "正在以 " + to + " 后端启动 Docker Desktop，首次切换要建虚拟机，可能要几分钟…"})
	if err := startDesktop(exe); err != nil {
		emitSetup(SetupProgress{Stage: "error", Note: err.Error()})
		return err
	}
	if err := waitEngineReadyLong(10 * time.Minute); err != nil {
		note := err.Error() + "；后端已改为 " + to + "，若长时间不起，请在 Docker Desktop 设置页确认或切回原后端"
		emitSetup(SetupProgress{Stage: "error", Note: note})
		return fmt.Errorf("%s", note)
	}
	// 引擎起了不等于切到了：必须用面板同一套判据（内核串）再核一遍，
	// 否则会出现“提示切换成功、面板一直显示 Hyper-V”的矛盾。
	status, note := switchVerify(backendNameOf(useWsl2), switchEvidence{
		Eng:       mustEngineStatus(),
		Cfg:       readEngineChoice().describe(),
		WslNote:   wslNote,
		DistroRun: useWsl2 && wslDockerDistroRunning(),
	})
	switch status {
	case "ok":
		emitSetup(SetupProgress{Stage: "done", Percent: 100,
			Note: note + "。容器列表可能是空的，这是新虚拟机的正常表现"})
		return nil
	case "unknown":
		emitSetup(SetupProgress{Stage: "done", Percent: 100, Note: note})
		return nil
	default:
		emitSetup(SetupProgress{Stage: "error", Note: note})
		return fmt.Errorf("%s", note)
	}
}

// mustEngineStatus 取引擎现状，出错时返回不可用状态（由调用方解读）
func mustEngineStatus() EngineInfo {
	eng, _ := GetEngineStatus()
	return eng
}

// waitEngineReadyLong 后端切换后的等待预算要大于首次安装：Hyper-V 建 VM 明显更慢
func waitEngineReadyLong(budget time.Duration) error {
	deadline := time.Now().Add(budget)
	for i := 0; time.Now().Before(deadline); i++ {
		time.Sleep(3 * time.Second)
		eng, _ := GetEngineStatus()
		if eng.Available {
			if b := detectBackend(eng); b.Name != "unknown" {
				emitSetup(SetupProgress{Stage: "info", Note: "引擎已应答，后端识别为 " + b.Label + "（依据：" + b.Source + "）"})
			}
			return nil
		}
		if i%10 == 9 {
			left := int(time.Until(deadline).Seconds())
			emitSetup(SetupProgress{Stage: "starting",
				Note: fmt.Sprintf("引擎仍未就绪（预算还剩约 %d 秒）：切换后端要新建虚拟机，比平时启动慢得多", left)})
		}
	}
	return fmt.Errorf("引擎在 %v 内未就绪", budget)
}
