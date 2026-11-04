package clouddrive

// 分块上传会话持久化（计划-云盘对象存储对接 PR-C ②）。
//
// 要解决的问题：大文件传到一半进程没了（崩溃、升级、用户关窗口），重启后再传同一个
// 文件，不该从第 1 片重来——云端那份 UploadId 下的已传分片还在，能接着用。
// 于是把「续传所需的坐标」落盘：<data>/clouddrive/uploads.json。
//
// 只存坐标，不存内容，更**不存任何凭证**：账号 id、目标 key、云端 UploadId、片大小、
// 已确认分片的片号/ETag/大小，加上源文件指纹（绝对路径 + 大小 + mtime）。指纹对不上
// 就视为没有会话（源内容变了，旧分片不能再要），避免把半新半旧的文件合成一个坏对象。
//
// 会话带 TTL：崩了之后再没重试过的记录会一直躺在盘上，而云端残留分片是计费项，
// 过期即丢记录并尽力 Abort（真正确切的清理手段是 bucket 的生命周期规则，见交付说明）。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"Tools/appdirs"
)

// osUploadSessionTTL 会话保留时长。
const osUploadSessionTTL = 24 * time.Hour

// osSessionPart 一个已在云端确认落地的分片。
type osSessionPart struct {
	Num  int    `json:"num"`
	ETag string `json:"etag"`
	Size int64  `json:"size"`
}

// osUploadSession 一次分块上传的可续传会话。
type osUploadSession struct {
	AcctID    string          `json:"acctId"`
	Key       string          `json:"key"`
	UploadID  string          `json:"uploadId"`
	PartSize  int64           `json:"partSize"`
	SrcPath   string          `json:"srcPath"`
	SrcSize   int64           `json:"srcSize"`
	SrcMTime  int64           `json:"srcMTime"` // UnixMilli
	Parts     []osSessionPart `json:"parts"`
	CreatedAt int64           `json:"createdAt"`
	UpdatedAt int64           `json:"updatedAt"`
}

// matches 判断一条会话是否还能用在「本次上传」上：同一个 key、同一个源文件指纹、
// 同一套片大小（片大小变了，已传分片的字节边界就对不齐）。
func (s *osUploadSession) matches(key, srcPath string, srcSize, srcMTime, partSize int64) bool {
	return s.Key == key && s.SrcPath == srcPath && s.SrcSize == srcSize &&
		s.SrcMTime == srcMTime && s.PartSize == partSize
}

// setPart 记下一片成功上传的分片（同片号覆盖）。
func (s *osUploadSession) setPart(num int, etag string, size int64) {
	for i := range s.Parts {
		if s.Parts[i].Num == num {
			s.Parts[i] = osSessionPart{Num: num, ETag: etag, Size: size}
			return
		}
	}
	s.Parts = append(s.Parts, osSessionPart{Num: num, ETag: etag, Size: size})
}

// syncParts 以**云端实况**为准重写分片表：只保留从第 1 片起连续存在的那些片
// （中间断号就没法合成完整对象，后面的片一律当没传过，稍后按同片号覆盖重传）。
// 回保留的字节总数，即续传时要在源文件里跳过的长度。
func (s *osUploadSession) syncParts(remote []osSessionPart) int64 {
	byNum := make(map[int]osSessionPart, len(remote))
	for _, p := range remote {
		byNum[p.Num] = p
	}
	var keep []osSessionPart
	var bytes int64
	for num := 1; ; num++ {
		p, ok := byNum[num]
		if !ok {
			break
		}
		keep = append(keep, p)
		bytes += p.Size
	}
	s.Parts = keep
	return bytes
}

// partList 按片号升序给出 CompleteMultipartUpload 需要的清单。
func (s *osUploadSession) partList() []osUploadPart {
	cp := append([]osSessionPart(nil), s.Parts...)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Num < cp[j].Num })
	out := make([]osUploadPart, 0, len(cp))
	for _, p := range cp {
		out = append(out, osUploadPart{PartNumber: p.Num, ETag: p.ETag})
	}
	return out
}

var uploadsMu sync.Mutex

// uploadStorePath 会话表路径（与账号表同目录，共用 baseOver 测试注入）。
func uploadStorePath() (string, error) {
	if baseOver != "" {
		if err := os.MkdirAll(baseOver, 0o755); err != nil {
			return "", err
		}
		return filepath.Join(baseOver, "uploads.json"), nil
	}
	dir, err := appdirs.Data("clouddrive")
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "uploads.json"), nil
}

func readUploadSessions() ([]osUploadSession, error) {
	p, err := uploadStorePath()
	if err != nil {
		return nil, err
	}
	bs, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return []osUploadSession{}, nil
		}
		return nil, fmt.Errorf("读取分块上传会话失败: %w", err)
	}
	out := []osUploadSession{}
	if len(bs) > 0 {
		if err := json.Unmarshal(bs, &out); err != nil {
			// 会话文件坏了就当作没有会话（重头传一次而已），不能让它卡死上传功能。
			return []osUploadSession{}, nil
		}
	}
	return out, nil
}

func writeUploadSessions(list []osUploadSession) error {
	p, err := uploadStorePath()
	if err != nil {
		return err
	}
	bs, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, bs, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// loadUploadSessions 取出某账号的未过期会话，过期记录就地删掉并回给调用方（让它
// 尽力 Abort 云端残留）。文件读坏当空表处理，不阻断上传。
func loadUploadSessions(acctID string, ttl time.Duration) (live, expired []osUploadSession, err error) {
	uploadsMu.Lock()
	defer uploadsMu.Unlock()
	all, err := readUploadSessions()
	if err != nil {
		return nil, nil, err
	}
	cutoff := time.Now().Add(-ttl).UnixMilli()
	var kept []osUploadSession
	for _, s := range all {
		if s.AcctID != acctID {
			kept = append(kept, s)
			continue
		}
		if s.UpdatedAt < cutoff || s.UploadID == "" || s.Key == "" {
			expired = append(expired, s)
			continue
		}
		live = append(live, s)
		kept = append(kept, s)
	}
	if len(expired) > 0 {
		if err := writeUploadSessions(kept); err != nil {
			return nil, nil, err
		}
	}
	return live, expired, nil
}

// putUploadSession 新增或覆盖一条会话（按账号 + key + UploadId 归并）。
func putUploadSession(s osUploadSession) error {
	uploadsMu.Lock()
	defer uploadsMu.Unlock()
	all, err := readUploadSessions()
	if err != nil {
		return err
	}
	s.UpdatedAt = time.Now().UnixMilli()
	if s.CreatedAt == 0 {
		s.CreatedAt = s.UpdatedAt
	}
	replaced := false
	for i := range all {
		if all[i].AcctID == s.AcctID && all[i].UploadID == s.UploadID && all[i].Key == s.Key {
			all[i] = s
			replaced = true
			break
		}
	}
	if !replaced {
		all = append(all, s)
	}
	return writeUploadSessions(all)
}

// removeUploadSession 删掉一条会话（Complete 成功、或确认云端已无此 UploadId 时）。
func removeUploadSession(acctID, uploadID string) error {
	uploadsMu.Lock()
	defer uploadsMu.Unlock()
	all, err := readUploadSessions()
	if err != nil {
		return err
	}
	kept := make([]osUploadSession, 0, len(all))
	found := false
	for _, s := range all {
		if s.AcctID == acctID && s.UploadID == uploadID {
			found = true
			continue
		}
		kept = append(kept, s)
	}
	if !found {
		return nil
	}
	return writeUploadSessions(kept)
}

// listUploadSessionsForTest 测试用：某账号当前落盘的会话（acctID 空 = 全部）。
func listUploadSessionsForTest(acctID string) []osUploadSession {
	uploadsMu.Lock()
	defer uploadsMu.Unlock()
	all, err := readUploadSessions()
	if err != nil {
		return nil
	}
	out := make([]osUploadSession, 0, len(all))
	for _, s := range all {
		if strings.TrimSpace(acctID) == "" || s.AcctID == acctID {
			out = append(out, s)
		}
	}
	return out
}
