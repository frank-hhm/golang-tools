package app

import (
	"Tools/services/applog"
	"Tools/services/config"
	"Tools/tools/docspace"
)

/*
备份计划（P4）绑定层

计划管理（增删查/启用位）与手动「立即备份」都在 tools/docspace 的 backup.go，
增量拷贝引擎在 tools/backup。进度经 backup:progress 事件接任务中心，前端无需
单独轮询——任务面板自动出现「备份 <名称>」条目。

不加 build tag：local→local 的目录同步纯标准库实现，Linux 面板一样能跑。
自动调度（P4 顺延项，已补做）：全局单协程轮询在 tools/docspace/sched.go，
双闸门 = 全局 config.DocSpace.BackupAuto + 计划 Enabled&&IntervalS>0。
*/

// BackupList 返回全部备份计划（按 id 稳定序）。
func (a *App) BackupList() ([]docspace.BackupPlan, error) {
	s, err := docspace.Global()
	if err != nil {
		return nil, err
	}
	return s.ListBackups()
}

// BackupAdd 新增备份计划：src 必须是已存在目录，dst 允许暂不存在（跑时创建）。
func (a *App) BackupAdd(name, src, dst string) (docspace.BackupPlan, error) {
	s, err := docspace.Global()
	if err != nil {
		return docspace.BackupPlan{}, err
	}
	p, err := s.AddBackup(name, src, dst)
	if err != nil {
		applog.Errorf("docspace", "新建备份计划失败: %v", err)
		return docspace.BackupPlan{}, err
	}
	applog.Infof("docspace", "新建备份计划: %s（%s → %s）", p.Name, p.Src, p.Dst)
	return p, nil
}

// BackupRemove 删除备份计划（只删登记，不动已备份出去的实体）。
func (a *App) BackupRemove(id string) error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.RemoveBackup(id); err != nil {
		applog.Errorf("docspace", "删除备份计划失败 id=%s: %v", id, err)
		return err
	}
	applog.Infof("docspace", "已删除备份计划: id=%s", id)
	return nil
}

// BackupSetEnabled 切换计划启用位（自动调度的计划级闸门之一；手动备份不受它限制）。
func (a *App) BackupSetEnabled(id string, on bool) error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.SetBackupEnabled(id, on); err != nil {
		applog.Errorf("docspace", "切换备份计划启用位失败 id=%s: %v", id, err)
		return err
	}
	return nil
}

// BackupRunNow 异步执行一次增量备份，立即返回；进度看任务中心。
// 同一计划正在跑时报错，避免叠跑。纯增量拷贝、绝不删目标。
func (a *App) BackupRunNow(id string) error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.RunBackup(id); err != nil {
		applog.Warnf("docspace", "启动备份失败 id=%s: %v", id, err)
		return err
	}
	applog.Infof("docspace", "已启动备份: id=%s", id)
	return nil
}

// BackupSetInterval 设置计划的自动调度间隔（秒）；<=0 表示「仅手动」。
// 真正会不会自动跑，还受全局开关（SetDocSpaceBackupAuto）与启用位约束。
func (a *App) BackupSetInterval(id string, secs int) error {
	s, err := docspace.Global()
	if err != nil {
		return err
	}
	if err := s.SetBackupInterval(id, secs); err != nil {
		applog.Errorf("docspace", "设置备份间隔失败 id=%s: %v", id, err)
		return err
	}
	applog.Infof("docspace", "备份间隔已设: id=%s secs=%d", id, secs)
	return nil
}

// SetDocSpaceBackupAuto 开/关备份自动调度全局总闸（默认关）。
// 关掉后调度协程每轮直接返回；重新打开无需重启，下一轮轮询即生效。
func (a *App) SetDocSpaceBackupAuto(on bool) error {
	_, err := config.Update(func(cfg *config.AppConfig) {
		if cfg.DocSpace == nil {
			cfg.DocSpace = &config.DocSpaceConfig{}
		}
		cfg.DocSpace.BackupAuto = on
	})
	if err != nil {
		applog.Errorf("docspace", "切换备份自动调度失败: %v", err)
		return err
	}
	applog.Infof("docspace", "备份自动调度: on=%v", on)
	return nil
}
