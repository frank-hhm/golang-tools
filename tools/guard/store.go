package guard

import (
	"encoding/json"
	"os"
	"path/filepath"

	"Tools/appdirs"
)

// guardFile guard.json 落盘结构：登记项列表 + 守护参数。
type guardFile struct {
	Entries  []Entry       `json:"entries"`
	Settings guardSettings `json:"settings"`
}

func storePath() (string, error) {
	base, err := appdirs.Data("guard")
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "guard.json"), nil
}

// loadLocked 读取持久化列表与参数（调用方持 mu）。文件不存在/解析失败时保持默认，不报错。
func loadLocked() {
	path, err := storePath()
	if err != nil {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		return
	}
	var f guardFile
	if err := json.Unmarshal(data, &f); err != nil {
		return
	}
	if f.Entries != nil {
		entries = f.Entries
	}
	if f.Settings.RestartDelaySec > 0 {
		settings.RestartDelaySec = f.Settings.RestartDelaySec
	}
	if f.Settings.MaxFastFail > 0 {
		settings.MaxFastFail = f.Settings.MaxFastFail
	}
}

// saveLocked 写盘（调用方持 mu）。失败静默（下次变更会再试），不阻断守护运行。
func saveLocked() {
	path, err := storePath()
	if err != nil {
		return
	}
	f := guardFile{Entries: entries, Settings: settings}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o644)
}
