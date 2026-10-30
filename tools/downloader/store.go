package downloader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ============================================================
// 两类持久化，各管各的：
//   1) 续传元数据 <savePath>.gtdl.json —— 记每块已下偏移，Resume 时只补未完成区间；
//   2) 任务清单 downloads/tasks.json —— 重启后把任务列回来（running→paused，不自动续跑）。
// 都走「写临时文件再 rename」的原子落盘，避免中途崩溃留下半截 JSON。
// ============================================================

/* ---------------- 续传 meta ---------------- */

// meta 单任务的续传元数据。
type meta struct {
	URL     string        `json:"url"`
	Size    int64         `json:"size"`
	Conns   int           `json:"conns"`
	Chunked bool          `json:"chunked"`
	Parts   map[int]int64 `json:"parts"` // index -> 已下字节
}

// matchChunked 判断这份 meta 能否用于本次分块续传：布局（url/size/conns）必须完全一致，
// 否则块边界会错位，宁可从头下。
func (m *meta) matchChunked(url string, size int64, conns int) bool {
	return m != nil && m.Chunked && m.Conns == conns && m.Size == size && strings.EqualFold(m.URL, url)
}

func metaForParts(url string, size int64, conns int, chunked bool, parts []partState) *meta {
	m := &meta{URL: url, Size: size, Conns: conns, Chunked: chunked}
	if chunked {
		m.Parts = make(map[int]int64, len(parts))
		for i := range parts {
			p := &parts[i]
			m.Parts[p.index] = p.done.Load()
		}
	}
	return m
}

func metaPath(savePath string) string { return savePath + metaSuffix }

func loadMeta(savePath string) *meta {
	buf, err := os.ReadFile(metaPath(savePath))
	if err != nil {
		return nil
	}
	var m meta
	if err := json.Unmarshal(buf, &m); err != nil {
		return nil
	}
	return &m
}

func saveMeta(savePath string, m *meta) {
	buf, err := json.Marshal(m)
	if err != nil {
		return
	}
	_ = atomicWrite(metaPath(savePath), buf)
}

func removeMeta(savePath string) {
	_ = os.Remove(metaPath(savePath))
}

/* ---------------- 任务清单 ---------------- */

// Store 把任务清单落在一处（downloads/tasks.json）。
type Store struct {
	dir string
}

// NewStore 指定存放目录（不存在会建）；dir 为空则所有操作退化为无持久化。
func NewStore(dir string) *Store {
	if dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	return &Store{dir: dir}
}

func (s *Store) enabled() bool { return s != nil && s.dir != "" }

func (s *Store) path() string { return filepath.Join(s.dir, "tasks.json") }

// Load 读回任务清单；running/queued 归一成 paused（重启不自动续跑，避免下载风暴）。
func (s *Store) Load() ([]*Task, error) {
	if !s.enabled() {
		return nil, nil
	}
	buf, err := os.ReadFile(s.path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var tasks []*Task
	if err := json.Unmarshal(buf, &tasks); err != nil {
		return nil, err
	}
	for _, t := range tasks {
		switch t.Status {
		case StatusRunning, StatusQueued:
			t.Status = StatusPaused
			t.Stage = "stalled"
		}
	}
	return tasks, nil
}

// Save 原子写全量清单（Manager 每次状态变更后调用；量小，全量重写最省心）。
func (s *Store) Save(tasks []*Task) error {
	if !s.enabled() {
		return nil
	}
	buf, err := json.MarshalIndent(tasks, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(s.path(), buf)
}

/* ---------------- 原子写 ---------------- */

// atomicWrite 先写同目录 .tmp 再 rename 落回；Windows 上 rename 不覆盖已存在目标，
// 失败时先删再 rename（与 fileview 同一套兜底）。
func atomicWrite(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(path)
		if err2 := os.Rename(tmp, path); err2 != nil {
			_ = os.Remove(tmp)
			return err2
		}
	}
	return nil
}
