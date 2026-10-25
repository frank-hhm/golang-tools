//go:build windows

package npm

// ============================================================
// PATH 绑定（强制切换本地 npm 命令）
//
// 把选定的 Node 目录提到 PATH 最前，并移除其中其它含 node.exe 的目录，
// 使 cmd 里 `npm` 必然解析到它。支持两种作用域：
//   - 用户 PATH（HKCU\Environment）：免管理员，但若系统 PATH 里也有 Node，
//     会被系统项盖过（Windows 组合顺序为「系统 PATH + 用户 PATH」）。
//   - 系统 PATH（HKLM\...\Session Manager\Environment）：真正全局生效，
//     需要管理员授权（触发 UAC 提权，用 reg.exe 以 REG_EXPAND_SZ 写回）。
//
// 首次绑定前按作用域各自备份原始 PATH，支持一键撤销（撤销会恢复所有已备份
// 的作用域）。改动均需新开终端才生效。
// ============================================================

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"go-tools/services/winrun"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	userEnvKey = `Environment`
	sysEnvKey  = `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`
	sysEnvFull = `HKLM\SYSTEM\CurrentControlSet\Control\Session Manager\Environment`
)

var pctEnvRe = regexp.MustCompile(`%([^%]+)%`)

// expandPct 手工展开 %VAR%（仅用于判断目录是不是 Node，写回时仍保留原始 %VAR%）
func expandPct(s string) string {
	return pctEnvRe.ReplaceAllStringFunc(s, func(m string) string {
		return os.Getenv(m[1 : len(m)-1])
	})
}

func splitPathTokens(raw string) []string {
	var out []string
	for _, t := range strings.Split(raw, ";") {
		t = strings.Trim(strings.TrimSpace(t), `"`)
		if t == "" {
			continue
		}
		out = append(out, t)
	}
	return out
}

func isNodeDir(dir string) bool {
	if dir == "" {
		return false
	}
	st, err := os.Stat(filepath.Join(dir, "node.exe"))
	return err == nil && !st.IsDir()
}

// readPathValue 读取指定注册表位置 Path 的原始（未展开）值
func readPathValue(root registry.Key, subKey string) (raw string, exists bool, err error) {
	k, err := registry.OpenKey(root, subKey, registry.QUERY_VALUE)
	if err != nil {
		return "", false, err
	}
	defer k.Close()
	v, _, err := k.GetStringValue("Path")
	if err != nil {
		if err == registry.ErrNotExist {
			return "", false, nil
		}
		return "", false, err
	}
	return v, true, nil
}

type pathBackup struct {
	Raw  string `json:"raw"`
	Time string `json:"time"`
}

func pathBackupFile(system bool) (string, error) {
	d, err := goToolsDir()
	if err != nil {
		return "", err
	}
	name := "npm-path-backup.json"
	if system {
		name = "npm-path-backup-machine.json"
	}
	return filepath.Join(d, name), nil
}

func loadPathBackup(system bool) *pathBackup {
	p, err := pathBackupFile(system)
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var b pathBackup
	if err := json.Unmarshal(data, &b); err != nil {
		return nil
	}
	return &b
}

// backupPathIfAbsent 首次绑定前备份该作用域的原始 PATH（已存在则保留最早的原值，撤销才干净）
func backupPathIfAbsent(system bool, raw string) {
	p, err := pathBackupFile(system)
	if err != nil {
		return
	}
	if _, statErr := os.Stat(p); statErr == nil {
		return
	}
	b := pathBackup{Raw: raw, Time: time.Now().Format("2006-01-02 15:04:05")}
	if data, err := json.MarshalIndent(b, "", "  "); err == nil {
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, data, 0o644)
	}
}

// entryNpm 返回某 Node 目录的 Node 版本与生效 npm 版本（无子进程：用 derivePrefix + 读 package.json）
func entryNpm(dir string) (version, npmVer string) {
	exe := findNode(dir)
	if exe == "" {
		return "", ""
	}
	version = resolveNodeVersion(exe, dir)
	npmVer = readNpmLayer("bundled", localNpmDir(dir)).Version
	if prefix := derivePrefix(exe); prefix != "" {
		if ov := readNpmLayer("override", overrideNpmDir(prefix)); ov.Exists {
			npmVer = ov.Version
		}
	}
	return version, npmVer
}

func writeUserPath(raw string) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, userEnvKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("打开 HKCU\\Environment 失败: %w", err)
	}
	defer k.Close()
	if err := k.SetExpandStringValue("Path", raw); err != nil {
		return fmt.Errorf("写入用户 PATH 失败: %w", err)
	}
	return nil
}

// buildBoundPath 把 entryDir 前置、移除其它 Node 目录与 entryDir 重复项，返回新 PATH 字符串
// （非 Node 项原样保留，含 %VAR%）
func buildBoundPath(raw, entryDir string) string {
	keyEntry := pathKey(entryDir)
	out := []string{entryDir}
	for _, t := range splitPathTokens(raw) {
		exp := expandPct(t)
		if isNodeDir(exp) || pathKey(exp) == keyEntry {
			continue
		}
		out = append(out, t)
	}
	return strings.Join(out, ";")
}

// PathBindStatus 读取 PATH 的 Node 绑定情况（按「系统 PATH + 用户 PATH」的组合顺序判定生效项）
func PathBindStatus() (*NpmPathBind, error) {
	st := &NpmPathBind{Platform: runtime.GOOS}
	uraw, _, err := readPathValue(registry.CURRENT_USER, userEnvKey)
	if err != nil {
		return nil, fmt.Errorf("读取用户 PATH 失败: %w", err)
	}
	sraw, _, _ := readPathValue(registry.LOCAL_MACHINE, sysEnvKey)

	var sysNodes, usrNodes []string
	for _, t := range splitPathTokens(sraw) {
		if e := expandPct(t); isNodeDir(e) {
			sysNodes = append(sysNodes, e)
		}
	}
	for _, t := range splitPathTokens(uraw) {
		if e := expandPct(t); isNodeDir(e) {
			usrNodes = append(usrNodes, e)
		}
	}
	// 进程 PATH 组合顺序：系统在前、用户在后，故最靠前的 Node 即 cmd 实际用到的
	combined := append(append([]string{}, sysNodes...), usrNodes...)
	if len(combined) > 0 {
		st.Bound = true
		st.BoundDir = combined[0]
		st.BoundVersion, st.BoundNpm = entryNpm(combined[0])
		if len(sysNodes) > 0 {
			st.BoundScope = "system"
		} else {
			st.BoundScope = "user"
		}
		st.OtherNodeDirs = combined[1:]
	}
	st.SystemHasNode = len(sysNodes) > 0
	var notes []string
	if b := loadPathBackup(false); b != nil {
		st.CanUndo = true
		notes = append(notes, "用户 "+b.Time)
	}
	if b := loadPathBackup(true); b != nil {
		st.CanUndo = true
		notes = append(notes, "系统 "+b.Time)
	}
	st.UndoNote = strings.Join(notes, "；")
	st.Note = "可选「用户 PATH」（免管理员）或「系统 PATH（管理员）」强制绑定。Windows 组合顺序为「系统 PATH + 用户 PATH」，系统项优先：若系统 PATH 里有 Node，只有绑到系统 PATH 才能真正生效。改动需新开终端生效。"
	return st, nil
}

// BindNodeToPath 把 entryDir 提到指定作用域 PATH 最前，并移除其中其它 Node 目录。
// system=false 改用户 PATH（免管理员）；system=true 改系统 PATH（触发 UAC 提权）。
func BindNodeToPath(entryDir string, system bool) error {
	entryDir = strings.Trim(entryDir, `"`)
	if entryDir == "" || !isNodeDir(entryDir) {
		return fmt.Errorf("目标目录不是有效的 Node 目录（缺 node.exe）：%s", entryDir)
	}
	if system {
		return bindSystemPath(entryDir)
	}
	return bindUserPath(entryDir)
}

func bindUserPath(entryDir string) error {
	raw, _, err := readPathValue(registry.CURRENT_USER, userEnvKey)
	if err != nil {
		return fmt.Errorf("读取用户 PATH 失败: %w", err)
	}
	backupPathIfAbsent(false, raw)
	if err := writeUserPath(buildBoundPath(raw, entryDir)); err != nil {
		return err
	}
	broadcastEnv()
	return nil
}

func bindSystemPath(entryDir string) error {
	raw, _, err := readPathValue(registry.LOCAL_MACHINE, sysEnvKey)
	if err != nil {
		return fmt.Errorf("读取系统 PATH 失败: %w", err)
	}
	backupPathIfAbsent(true, raw)
	if err := writeSystemPathElevated(buildBoundPath(raw, entryDir)); err != nil {
		return err
	}
	broadcastEnv()
	return nil
}

// UndoPathBind 恢复所有已备份作用域（用户 / 系统）绑定前的 PATH
func UndoPathBind() (string, error) {
	var done []string
	if b := loadPathBackup(false); b != nil {
		if err := writeUserPath(b.Raw); err != nil {
			return "", err
		}
		if p, _ := pathBackupFile(false); p != "" {
			_ = os.Remove(p)
		}
		done = append(done, "用户 PATH")
	}
	if b := loadPathBackup(true); b != nil {
		if err := writeSystemPathElevated(b.Raw); err != nil {
			return "", err
		}
		if p, _ := pathBackupFile(true); p != "" {
			_ = os.Remove(p)
		}
		done = append(done, "系统 PATH")
	}
	if len(done) == 0 {
		return "", fmt.Errorf("没有找到可撤销的 PATH 备份")
	}
	broadcastEnv()
	return "已恢复绑定前的" + strings.Join(done, "、") + "。新开终端生效。", nil
}

// writeSystemPathElevated 以管理员权限把系统 PATH 写回（REG_EXPAND_SZ，保留 %VAR%）。
// 通过 ShellExecuteExW(runas) 提权运行 reg.exe add，避免 cmd 对 % 的二次展开。
func writeSystemPathElevated(value string) error {
	// 去掉结尾反斜杠，防止 \\\" 被当作转义引号破坏命令行参数
	value = strings.TrimRight(value, "\\")
	params := fmt.Sprintf(`add "%s" /v Path /t REG_EXPAND_SZ /d "%s" /f`, sysEnvFull, value)
	code, err := winrun.RunElevatedWait(winrun.Elevated{File: "reg.exe", Params: params, Show: windows.SW_HIDE})
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("写入系统 PATH 失败（reg 退出码 %d）", code)
	}
	return nil
}

// 提权执行的具体 Win32 细节收敛到 services/winrun（与 Docker Desktop 安装共用同一 helper）

const (
	hwndBroadcast   = 0xffff
	wmSettingChange = 0x1A
	smtoAbortIfHung = 0x0002
)

var (
	user32                 = windows.NewLazySystemDLL("user32.dll")
	procSendMessageTimeout = user32.NewProc("SendMessageTimeoutW")
)

// broadcastEnv 广播 WM_SETTINGCHANGE("Environment")，让新开的应用/资源管理器感知环境变更
func broadcastEnv() {
	envStr, err := windows.UTF16PtrFromString("Environment")
	if err != nil {
		return
	}
	var result uintptr
	procSendMessageTimeout.Call(
		uintptr(hwndBroadcast),
		uintptr(wmSettingChange),
		0,
		uintptr(unsafe.Pointer(envStr)),
		smtoAbortIfHung,
		5000,
		uintptr(unsafe.Pointer(&result)),
	)
}
