//go:build windows

package archive

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"

	"golang.org/x/sys/windows/registry"
)

// ============================================================
// 系统右键挂载（HKCU 静态 verb + SendTo 快捷方式）
//
// 只写 HKCU\Software\Classes：不需要管理员，也不碰 HKCR 的合并视图。
// 代价是 Win11 的顶层右键菜单进不来（那需要 IExplorerCommand COM +
// MSIX 打包），本程序只出现在「显示更多选项」的二级菜单里。
//
// 键名一律带程序标识（GoToolsArchive / GoToolsExtract），卸载时只删
// 这两个名字底下的键，绝不清空 shell 里别家的条目。
// ============================================================

// compressVerbKeys 压缩挂在「任意文件」与「任意目录」两类对象上。
//
// 不挂 Folder（桌面/库根）而挂 Directory：后者才对真正的文件夹右键生效，
// 挂在 Folder 上会连「此电脑」这种虚拟节点也一起挂。
var compressVerbKeys = []string{
	`Software\Classes\*\shell\` + VerbCompress,
	`Software\Classes\Directory\shell\` + VerbCompress,
}

// extractVerbKeys 解压只挂在认得的扩展名上：对任意文件提供"解压"是纯噪音。
//
// .tar.gz 这条按方案写上了，但它可能压根不会被命中：资源管理器算扩展名
// 只取最后一个点（"a.tar.gz" → ".gz"），于是真正生效的会是 .gz 的关联。
// 留在清单里是因为写多一条无害（键没人查就静静躺着），命中与否属于
// 必须真机验证的项，不在代码里假装已知。
var extractVerbKeys = []string{
	`Software\Classes\SystemFileAssociations\.zip\shell\` + VerbExtract,
	`Software\Classes\SystemFileAssociations\.tgz\shell\` + VerbExtract,
	`Software\Classes\SystemFileAssociations\.tar.gz\shell\` + VerbExtract,
}

// selfExe 当前可执行文件的绝对路径。
func selfExe() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("取不到程序自身路径: %w", err)
	}
	if abs, aerr := filepath.Abs(exe); aerr == nil {
		exe = abs
	}
	// 去掉 \\?\ 前缀：注册表里写这种形式资源管理器认不了
	if long := `\\?\`; strings.HasPrefix(exe, long) {
		exe = exe[len(long):]
	}
	return filepath.Clean(exe), nil
}

// commandLine 拼 verb 的 command 值。
//
// 压缩用 %V：多选时把每个选中项各自带引号并成一条命令行；%1 只给第一个，
// 等于把用户其余的选择悄悄吞掉。解压天然只针对单个归档，用 %1。
func commandLine(exe, arg, token string) string {
	return `"` + exe + `" ` + arg + ` "` + token + `"`
}

// ShellStatus 逐键读回当前挂载状态。
func ShellStatus() (ShellInfo, error) {
	var info ShellInfo
	info.Supported = true
	exe, err := selfExe()
	if err != nil {
		return info, err
	}
	info.Exe = exe

	check := func(keys []string, arg, name string) int {
		slots := make([]int, 0, len(keys))
		for _, k := range keys {
			s := readSlot(k, exe, arg)
			slots = append(slots, s)
			item := name + " · " + shortKey(k)
			if s == SlotCurrent {
				info.MountedKeys = append(info.MountedKeys, item)
			} else {
				info.ProblemKeys = append(info.ProblemKeys, item+"（"+SlotText(s)+"）")
			}
		}
		return worstSlot(slots)
	}

	info.Compress = check(compressVerbKeys, ArgArchive, LabelCompress)
	info.Extract = check(extractVerbKeys, ArgExtract, LabelExtract)
	st, err := sendToSendTo(exe)
	if err != nil {
		return info, err
	}
	info.SendTo = st
	if st == SlotCurrent {
		info.MountedKeys = append(info.MountedKeys, LabelSendTo)
	} else {
		info.ProblemKeys = append(info.ProblemKeys, LabelSendTo+"（"+SlotText(st)+"）")
	}
	info.Stale = info.Compress == SlotForeign || info.Extract == SlotForeign || st == SlotForeign
	info.Detail = describe(info)
	return info, nil
}

// describe 把三个槽位态拼成一句给人看的话。
func describe(i ShellInfo) string {
	if AllMounted(i) {
		return "右键菜单与 SendTo 都已挂在当前程序上"
	}
	if i.Stale {
		return "程序换过位置，菜单里记的还是旧路径，重新挂载一次即修好"
	}
	if i.Compress == SlotDamaged || i.Extract == SlotDamaged || i.SendTo == SlotDamaged {
		return "有挂载项读不出来：可能是键被别的程序占了，或当前账户没写权限"
	}
	return "尚未挂载到系统右键菜单"
}

// shortKey 把长键路径缩成 UI 上看得懂的一段。
func shortKey(k string) string {
	base := filepath.Base(filepath.Dir(k)) // ...\SystemFileAssociations\.zip\shell\X → .zip
	if base == "shell" || base == `\shell` {
		base = filepath.Base(filepath.Dir(filepath.Dir(k)))
	}
	return base
}

// readSlot 读一个 verb 键的 command 默认值，判断它指没指向前。
func readSlot(verbKey, exe, arg string) int {
	k, err := registry.OpenKey(registry.CURRENT_USER, verbKey+`\command`, registry.QUERY_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return SlotAbsent
		}
		return SlotDamaged
	}
	defer k.Close()
	val, _, err := k.GetStringValue("")
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return SlotAbsent
		}
		return SlotDamaged
	}
	switch {
	case strings.Contains(val, arg) && pathRefersTo(val, exe):
		return SlotCurrent
	case strings.Contains(val, arg):
		return SlotForeign
	default:
		// 键名是我们的，值里却没有我们的标记：这个位置被别的程序占了，
		// 不能报"已挂载"，也不报"指向旧路径"（那不是我们挪的位置）
		return SlotDamaged
	}
}

// pathRefersTo 命令行里出现的程序路径是否就是 exe（大小写不敏感）。
func pathRefersTo(cmdline, exe string) bool {
	q := `"` + exe + `"`
	return strings.Contains(strings.ToLower(cmdline), strings.ToLower(q)) ||
		strings.Contains(strings.ToLower(cmdline), strings.ToLower(exe))
}

// ShellInstall 写入全部右键挂载。幂等：已存在就覆写，挪过位置的键顺带修好。
//
// sendTo=false 时不碰 SendTo 目录（已有快捷方式也不删，交给 Uninstall 统一收）。
func ShellInstall(sendTo bool) error {
	exe, err := selfExe()
	if err != nil {
		return err
	}
	var errs []string
	for _, k := range compressVerbKeys {
		if err := writeVerb(k, LabelCompress, commandLine(exe, ArgArchive, "%V"), exe, true); err != nil {
			errs = append(errs, err.Error())
		}
	}
	for _, k := range extractVerbKeys {
		if err := writeVerb(k, LabelExtract, commandLine(exe, ArgExtract, "%1"), exe, false); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if sendTo {
		if err := installSendTo(exe); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("挂载未完成：%s", strings.Join(errs, "；"))
	}
	return nil
}

// writeVerb 写一个 verb 键：显示名、多选支持、图标，以及底下的 command。
func writeVerb(verbKey, label, cmdline, exe string, multi bool) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, verbKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("写 %s 失败: %w", verbKey, err)
	}
	defer k.Close()
	if err := k.SetStringValue("MUIVerb", label); err != nil {
		return fmt.Errorf("写菜单文案失败: %w", err)
	}
	if multi {
		// MultiSelectModel=Player 才允许一次选中多个条目，缺了它多选时整项不出现
		if err := k.SetStringValue("MultiSelectModel", "Player"); err != nil {
			return fmt.Errorf("写多选标记失败: %w", err)
		}
	}
	if err := k.SetStringValue("Icon", `"`+exe+`",0`); err != nil {
		return fmt.Errorf("写图标失败: %w", err)
	}
	ck, _, err := registry.CreateKey(k, "command", registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("写 command 失败: %w", err)
	}
	defer ck.Close()
	if err := ck.SetStringValue("", cmdline); err != nil {
		return fmt.Errorf("写命令行失败: %w", err)
	}
	return nil
}

// ShellUninstall 删干净自己写过的键与 SendTo 快捷方式。
//
// 只按自己的 verb 名删，绝不遍历 shell 键树做"清空"：那会把别家程序的
// 菜单项一起删掉，而用户不会认为这是本程序干的。
func ShellUninstall() error {
	var errs []string
	for _, k := range append(append([]string{}, compressVerbKeys...), extractVerbKeys...) {
		if err := removeVerbTree(k); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if lnk, err := sendToPath(); err == nil {
		if err := os.Remove(lnk); err != nil && !os.IsNotExist(err) {
			errs = append(errs, fmt.Sprintf("删除 %s 失败: %v", filepath.Base(lnk), err))
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("卸载未完成: %s", strings.Join(errs, "；"))
	}
	return nil
}

// removeVerbTree 先删 command 子键再删 verb 键本身：
// 注册表不允许删还有子键的键，反过来写必失败。
func removeVerbTree(verbKey string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, verbKey, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil // 本来就没有，视作已卸载
		}
		return fmt.Errorf("打开 %s 失败: %w", verbKey, err)
	}
	defer k.Close()
	// 先把剩下的子键清空（老版本可能写过 IconOverlay 之类），再删 verb 本身
	if names, nerr := k.ReadSubKeyNames(-1); nerr == nil {
		for _, n := range names {
			_ = registry.DeleteKey(k, n)
		}
	}
	if err := registry.DeleteKey(registry.CURRENT_USER, verbKey); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("删除 %s 失败: %w", verbKey, err)
	}
	return nil
}

// sendToPath SendTo 目录里快捷方式的目标路径。
func sendToPath() (string, error) {
	dir, err := sendToDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, LabelSendTo+".lnk"), nil
}

func sendToDir() (string, error) {
	appdata := os.Getenv("APPDATA")
	if appdata == "" {
		return "", fmt.Errorf("取不到 APPDATA，SendTo 无法挂载")
	}
	return filepath.Join(appdata, "Microsoft", "Windows", "SendTo"), nil
}

// lnkScript 用一行 WScript.Shell 造快捷方式：沿用 toast 通知那套
// 「脚本走 stdin、参数走环境变量」的写法，避开 PowerShell 的引号转义，
// 也不为了造个 .lnk 去引第三方 COM 包。
const lnkScript = `$ws = New-Object -ComObject WScript.Shell
$sc = $ws.CreateShortcut($env:ARC_LNK)
$sc.TargetPath = $env:ARC_EXE
$sc.Arguments = $env:ARC_ARGS
$sc.IconLocation = $env:ARC_EXE + ',0'
$sc.Description = '把选中的文件交给 Tools 压缩'
$sc.Save()
`

// installSendTo 写 SendTo 快捷方式。SendTo 天然支持多选，
// 每个选中项各传一个参数，与 verb 的 %V 是两种展开形态（都要真机确认）。
func installSendTo(exe string) error {
	lnk, err := sendToPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(lnk), 0o755); err != nil {
		return fmt.Errorf("建 SendTo 目录失败: %w", err)
	}
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-Command", "-")
	cmd.Stdin = strings.NewReader(lnkScript)
	cmd.Env = append(os.Environ(),
		"ARC_LNK="+lnk, "ARC_EXE="+exe, "ARC_ARGS="+ArgPanel)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW：不闪现 PowerShell 控制台（Win11 上 HideWindow 藏不住 Windows Terminal）
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("生成快捷方式失败: %v %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// sendToSendTo 判断 SendTo 快捷方式的状态。
//
// .lnk 里的目标路径以 UTF-16 存。读回"它到底指向哪儿"正规做法是实例化
// Shell Link COM 再取 TargetPath，为一个状态显示不值得；这里把文件内容
// 当 UTF-16LE 序列解码出来再找当前 exe 的路径，够判出「在不在、指没指错」。
func sendToSendTo(exe string) (int, error) {
	lnk, err := sendToPath()
	if err != nil {
		return SlotAbsent, err
	}
	data, err := os.ReadFile(lnk)
	if err != nil {
		if os.IsNotExist(err) {
			return SlotAbsent, nil
		}
		return SlotDamaged, nil
	}
	if containsUTF16(data, exe) || strings.Contains(strings.ToLower(string(data)), strings.ToLower(exe)) {
		return SlotCurrent, nil
	}
	// 快捷方式在，但里面找不到当前路径：要么程序挪过位置，
	// 要么这个 .lnk 是别人放的（名字撞车），不能报"已挂载"
	return SlotForeign, nil
}

// containsUTF16 在 buf 里找一个字符串的 UTF-16LE 形态（大小写不敏感）。
//
// 两个起始偏移都试：结构体对齐让路径通常落在偶数字节上，但 LinkInfo 里
// 的绝对路径前头有变长字段，落单时按偶偏移会整个漏掉。
func containsUTF16(buf []byte, s string) bool {
	if s == "" || len(buf) < 4 {
		return false
	}
	needle := strings.ToLower(s)
	for _, off := range []int{0, 1} {
		if off >= len(buf) {
			continue
		}
		window := buf[off:]
		units := make([]uint16, 0, len(window)/2)
		for i := 0; i+1 < len(window); i += 2 {
			units = append(units, uint16(window[i])|uint16(window[i+1])<<8)
		}
		if strings.Contains(strings.ToLower(string(utf16.Decode(units))), needle) {
			return true
		}
	}
	return false
}
