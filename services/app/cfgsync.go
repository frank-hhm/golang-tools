package app

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"go-tools/services/applog"
	"go-tools/services/config"
	"go-tools/services/sysx"
)

/*
配置导出 / 导入（云端同步与换机迁移）

导出的是一份"可搬运"的配置快照：工具列表、各工具设置、浏览器空间与代理、
主题、开机自启等都在里面，拿另一台机器导入即可得到同样的环境。

刻意不导出机器绑定数据（导出了只会误导，或者把目标机搞坏）：
  - 浮动球位置：屏幕像素坐标，换机无意义
  - 三类基础目录（plugins/data/logs）：目标机上的绝对路径
  - Logo 绝对路径：图片本体在配置目录里，不随 JSON 走
  - MySQL SQL 日志目录：同样是绝对路径

导入时这些字段一律保留本机当前值，只覆盖"设置"部分。
*/

// ExportConfig 弹保存框导出一份配置快照，返回实际写入的路径（取消返回空串）。
func (a *App) ExportConfig() (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	path, err := a.app.Dialog.SaveFile().
		SetFilename(fmt.Sprintf("go-tools-config-%s.json", time.Now().Format("20060102"))).
		SetMessage("导出配置").
		AddFilter("配置文件 (*.json)", "*.json").
		AddFilter("所有文件 (*.*)", "*.*").
		PromptForSingleSelection()
	if err != nil {
		if isDialogCanceled(err) {
			return "", nil
		}
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", nil
	}

	cfg := portableConfig(config.CurrentConfig())
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		applog.Errorf("config", "导出配置失败: %v", err)
		return "", err
	}
	applog.Infof("config", "配置已导出: %s（%d 字节，已剔除机器绑定字段）", path, len(data))
	return path, nil
}

// ImportConfig 弹打开框导入配置快照，返回被导入的文件路径（取消返回空串）。
//
// 覆盖范围就是 ExportConfig 导出的那些字段；目录、Logo、球位等机器绑定项
// 保留本机当前值。导入后跑一遍 SaveConfig，让日志器/热键/自启/浮动球全部跟着生效。
func (a *App) ImportConfig() (string, error) {
	if a.app == nil {
		return "", fmt.Errorf("应用尚未初始化")
	}
	path, err := a.app.Dialog.OpenFile().
		CanChooseFiles(true).
		CanChooseDirectories(false).
		AddFilter("配置文件 (*.json)", "*.json").
		AddFilter("所有文件 (*.*)", "*.*").
		SetTitle("导入配置").
		PromptForSingleSelection()
	if err != nil {
		if isDialogCanceled(err) {
			return "", nil
		}
		return "", err
	}
	if strings.TrimSpace(path) == "" {
		return "", nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var in config.AppConfig
	if err := json.Unmarshal(data, &in); err != nil {
		return "", fmt.Errorf("配置文件不是合法的 JSON：%v", err)
	}
	if len(in.Tools) == 0 {
		// 文件里没写工具列表时不能把现有列表清空，按“没写”处理
		in.Tools = config.CurrentConfig().Tools
	}

	cur := config.CurrentConfig()
	in.Ball.X, in.Ball.Y, in.Ball.HasPos = cur.Ball.X, cur.Ball.Y, cur.Ball.HasPos
	in.General.AppLogo = cur.General.AppLogo
	in.Paths = cur.Paths
	if ts, ok := in.ToolSettings["mysql"]; ok {
		ts.SQLLogDir = ""
		in.ToolSettings["mysql"] = ts
	}

	if _, err := a.SaveConfig(in); err != nil {
		applog.Errorf("config", "导入配置失败: %v", err)
		return "", err
	}
	applog.Infof("config", "配置已导入: %s（目录/Logo/球位保留本机设置）", path)
	return path, nil
}

// OpenConfigDir 在文件管理器中打开配置文件所在目录（手工备份/替换 config.json 用）。
func (a *App) OpenConfigDir() error {
	dir, err := config.ConfigDir()
	if err != nil {
		return err
	}
	// 走 sysx：Windows 用 explorer，Linux 用 xdg-open，调用处不写平台分支
	if err := sysx.Open().Dir(dir); err != nil {
		applog.Errorf("config", "打开配置目录失败: %v", err)
		return err
	}
	return nil
}

// portableConfig 去掉机器绑定字段，只留可搬运的设置。
func portableConfig(cfg config.AppConfig) config.AppConfig {
	cfg.Ball.X, cfg.Ball.Y, cfg.Ball.HasPos = 0, 0, false
	cfg.General.AppLogo = ""
	cfg.Paths = config.PathsConfig{}
	if ts, ok := cfg.ToolSettings["mysql"]; ok {
		ts.SQLLogDir = ""
		cfg.ToolSettings["mysql"] = ts
	}
	return cfg
}
