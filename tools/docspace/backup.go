package docspace

// 备份计划（P4）：元数据存 backups.json（Store 已预留 backups 段），
// 实际增量拷贝交给 tools/backup 引擎。IntervalS/Enabled 为自动调度闸门，
// 调度协程见 sched.go（全局总开关 config.DocSpace.BackupAuto，默认关）。
//
// 备份是破坏性最小的操作：纯增量、绝不删目标，因此不做二次确认级别的防护，
// 但仍拒绝 src==dst / 目标在源内这类会自我递归的配置（引擎层已挡）。

import (
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"Tools/tools/backup"
)

// 在飞保护：同一计划不允许并发跑（重复点“立即备份”会被拒）。
var (
	runningMu sync.Mutex
	running   = map[string]bool{}
)

// ListBackups 返回全部备份计划（按 id 稳定序）。
func (s *Store) ListBackups() ([]BackupPlan, error) {
	if err := s.Load(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]BackupPlan, len(s.backups))
	copy(out, s.backups)
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// AddBackup 新增一条备份计划。Src 必须是已存在的目录；Dst 允许暂不存在（跑时创建）。
func (s *Store) AddBackup(name, src, dst string) (BackupPlan, error) {
	if src == "" || dst == "" {
		return BackupPlan{}, fmt.Errorf("源目录与目标目录都不能为空")
	}
	if st, err := os.Stat(src); err != nil {
		return BackupPlan{}, fmt.Errorf("源目录不可用: %w", err)
	} else if !st.IsDir() {
		return BackupPlan{}, fmt.Errorf("源必须是目录: %s", src)
	}
	if name == "" {
		name = src
	}
	if err := s.Load(); err != nil {
		return BackupPlan{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	p := BackupPlan{
		ID:      newID("b"),
		Name:    name,
		Src:     src,
		Dst:     dst,
		Enabled: true,
	}
	s.backups = append(s.backups, p)
	if err := writeJSONAtomic(s.pathOf("backups.json"), s.backups); err != nil {
		s.backups = s.backups[:len(s.backups)-1] // 落盘失败回滚内存
		return BackupPlan{}, err
	}
	return p, nil
}

// RemoveBackup 删除一条备份计划（只删登记，不动已备份出去的实体）。
func (s *Store) RemoveBackup(id string) error {
	if err := s.Load(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx := -1
	for i := range s.backups {
		if s.backups[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("备份计划不存在: %s", id)
	}
	s.backups = append(s.backups[:idx], s.backups[idx+1:]...)
	return writeJSONAtomic(s.pathOf("backups.json"), s.backups)
}

// SetBackupEnabled 切换计划的启用位（为未来调度预留，本期不影响手动备份）。
func (s *Store) SetBackupEnabled(id string, on bool) error {
	if err := s.Load(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.backups {
		if s.backups[i].ID == id {
			s.backups[i].Enabled = on
			return writeJSONAtomic(s.pathOf("backups.json"), s.backups)
		}
	}
	return fmt.Errorf("备份计划不存在: %s", id)
}

// SetBackupInterval 设置计划的自动调度间隔（秒）。<=0 表示「仅手动」，
// 调度器永不碰；改动下一轮轮询即生效（无需重建 ticker）。
func (s *Store) SetBackupInterval(id string, secs int) error {
	if secs < 0 {
		secs = 0
	}
	if err := s.Load(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.backups {
		if s.backups[i].ID == id {
			s.backups[i].IntervalS = secs
			return writeJSONAtomic(s.pathOf("backups.json"), s.backups)
		}
	}
	return fmt.Errorf("备份计划不存在: %s", id)
}

// RunBackup 异步执行一次增量备份，立即返回；进度经 tools/backup 广播成
// backup:progress 事件（接任务中心），完成/失败回写计划的 LastRunAt/LastStatus。
// 同一计划正在跑时返回错误，避免叠跑。
func (s *Store) RunBackup(id string) error {
	if err := s.Load(); err != nil {
		return err
	}
	s.mu.RLock()
	var p BackupPlan
	found := false
	for _, b := range s.backups {
		if b.ID == id {
			p, found = b, true
			break
		}
	}
	s.mu.RUnlock()
	if !found {
		return fmt.Errorf("备份计划不存在: %s", id)
	}

	runningMu.Lock()
	if running[id] {
		runningMu.Unlock()
		return fmt.Errorf("该计划正在备份中，请等待完成")
	}
	running[id] = true
	runningMu.Unlock()

	go func() {
		defer func() {
			runningMu.Lock()
			delete(running, id)
			runningMu.Unlock()
		}()
		st, err := backup.Run(p.ID, p.Name, p.Src, p.Dst)
		status := "ok"
		if err != nil {
			status = "error: " + err.Error()
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		for i := range s.backups {
			if s.backups[i].ID == id {
				s.backups[i].LastRunAt = time.Now().UnixMilli()
				s.backups[i].LastStatus = status
				s.backups[i].Copied = st.Copied
				s.backups[i].Skipped = st.Skipped
				break
			}
		}
		if werr := writeJSONAtomic(s.pathOf("backups.json"), s.backups); werr != nil {
			// 结果回写失败不改备份本身（实体已拷好），仅内存与磁盘短暂不一致
			_ = werr
		}
	}()
	return nil
}
