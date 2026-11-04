//go:build windows

package gitcmd

// ============================================================
// Git 系统右键挂载（HKCU 静态 verb）——仅 Windows。
//
// 只写 HKCU\Software\Classes：不需要管理员，也不碰 HKCR 的合并视图。
// 代价是 Win11 的顶层右键菜单进不来（那需要 IExplorerCommand COM + MSIX
// 打包），本程序只出现在「显示更多选项」的二级菜单里——与压缩同一取舍。
//
// 键名一律带程序标识（ToolsGitStatus / ToolsGitCommit …），卸载时只删这些
// 名字底下的键，绝不清空 shell 里别家的条目；也绝不碰压缩那套 GoTools* 的键。
// ============================================================

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// gitSelfExe 当前可执行文件的绝对路径。
func gitSelfExe() (string, error) {
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

// gitCommandLine 拼 verb 的 command 值。token 为空（Background 空白处那种）
// 就不带路径参数，进程以资源管理器给的 cwd 为准。
func gitCommandLine(exe, arg, token string) string {
	if token == "" {
		return `"` + exe + `" ` + arg
	}
	return `"` + exe + `" ` + arg + ` "` + token + `"`
}

// gitVerbKeys 一个 verb 在三类位置上的全部注册表键路径。
func gitVerbKeys(v gitVerb) []string {
	out := make([]string, 0, len(gitShellLocations))
	for _, loc := range gitShellLocations {
		out = append(out, loc.prefix+v.verbName())
	}
	return out
}

// GitShellStatus 逐键读回当前挂载状态，聚合成一个槽位。
func GitShellStatus() (GitShellInfo, error) {
	var info GitShellInfo
	info.Supported = true
	exe, err := gitSelfExe()
	if err != nil {
		return info, err
	}
	info.Exe = exe

	var slots []int
	stale := false
	for _, v := range gitVerbs {
		for _, loc := range gitShellLocations {
			key := loc.prefix + v.verbName()
			s := gitReadSlot(key, exe, v.arg)
			slots = append(slots, s)
			item := v.label + " · " + loc.short
			switch s {
			case SlotCurrent:
				info.MountedKeys = append(info.MountedKeys, item)
			case SlotForeign:
				stale = true
				info.ProblemKeys = append(info.ProblemKeys, item+"（"+SlotText(s)+"）")
			default:
				info.ProblemKeys = append(info.ProblemKeys, item+"（"+SlotText(s)+"）")
			}
		}
	}
	info.Slot = worstSlot(slots)
	info.Stale = stale
	info.Detail = gitDetail(info)
	return info, nil
}

// GitShellInstall 写入全部 Git verb 挂载。幂等：已存在就覆写，挪过位置的键顺带修好。
func GitShellInstall() error {
	exe, err := gitSelfExe()
	if err != nil {
		return err
	}
	var errs []string
	for _, v := range gitVerbs {
		for _, loc := range gitShellLocations {
			key := loc.prefix + v.verbName()
			cmdline := gitCommandLine(exe, v.arg, loc.token)
			// Background（空白处）不涉及多选，其余两类也按单选 %1，不置多选标记
			if err := gitWriteVerb(key, v.label, cmdline, exe); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("挂载未完成：%s", strings.Join(errs, "；"))
	}
	return nil
}

// gitWriteVerb 写一个 verb 键：显示名、图标，以及底下的 command。
func gitWriteVerb(verbKey, label, cmdline, exe string) error {
	k, _, err := registry.CreateKey(registry.CURRENT_USER, verbKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("写 %s 失败: %w", verbKey, err)
	}
	defer k.Close()
	if err := k.SetStringValue("MUIVerb", label); err != nil {
		return fmt.Errorf("写菜单文案失败: %w", err)
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

// GitShellUninstall 删干净自家写过的键。只按自家 verb 名删，绝不遍历 shell 键树清空。
func GitShellUninstall() error {
	var errs []string
	for _, v := range gitVerbs {
		for _, loc := range gitShellLocations {
			if err := gitRemoveVerbTree(loc.prefix + v.verbName()); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("卸载未完成: %s", strings.Join(errs, "；"))
	}
	return nil
}

// gitReadSlot 读一个 verb 键的 command 默认值，判断它指没指向前。
func gitReadSlot(verbKey, exe, arg string) int {
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
	case strings.Contains(val, arg) && gitPathRefersTo(val, exe):
		return SlotCurrent
	case strings.Contains(val, arg):
		return SlotForeign
	default:
		// 键名是我们的，值里却没有我们的标记：这个位置被别的程序占了，
		// 不能报"已挂载"，也不报"指向旧路径"（那不是我们挪的位置）
		return SlotDamaged
	}
}

// gitPathRefersTo 命令行里出现的程序路径是否就是 exe（大小写不敏感）。
func gitPathRefersTo(cmdline, exe string) bool {
	q := `"` + exe + `"`
	return strings.Contains(strings.ToLower(cmdline), strings.ToLower(q)) ||
		strings.Contains(strings.ToLower(cmdline), strings.ToLower(exe))
}

// gitRemoveVerbTree 先删 command 子键再删 verb 键本身：注册表不允许删还有子键的键。
func gitRemoveVerbTree(verbKey string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, verbKey, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil // 本来就没有，视作已卸载
		}
		return fmt.Errorf("打开 %s 失败: %w", verbKey, err)
	}
	defer k.Close()
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
