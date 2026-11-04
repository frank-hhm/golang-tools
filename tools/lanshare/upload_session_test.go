package lanshare

// upload_session 协议级集成测试（计划 §2 / §8：P3 补做）。
//
// 全部经真实 handle 路由驱动（httptest），覆盖：
//   - 分片拼回 + 乱序 + 末片不满 + 完成落收件箱
//   - 重传命中同 sid 回 have 续传
//   - 未完成就 complete → 400
//   - 会话数超 maxSessions → init 429
//   - 超 TTL 会话被 GC 清盘
//   - abort 删会话 + 发 canceled 事件
//   - Progress 携带 ID/Status（done 终态）

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// contentByte 可复现的分片内容指纹（按偏移取模，便于拼回后逐字节校验）。
func contentByte(i int64) byte { return byte(i % 251) }

func expectContent(size int64) []byte {
	b := make([]byte, size)
	for i := range b {
		b[i] = contentByte(int64(i))
	}
	return b
}

// resetUploads 清空 uploads 目录与内存会话表（会话态是包级全局，跨用例必须隔离）。
func resetUploads(t *testing.T) {
	t.Helper()
	dir, err := uploadsDir()
	if err != nil {
		t.Fatal(err)
	}
	_ = os.RemoveAll(dir)
	sessionsGuard.Lock()
	for k := range liveSessions {
		delete(liveSessions, k)
	}
	sessionsGuard.Unlock()
	t.Cleanup(func() {
		_ = os.RemoveAll(dir)
		sessionsGuard.Lock()
		for k := range liveSessions {
			delete(liveSessions, k)
		}
		sessionsGuard.Unlock()
	})
}

type initResp struct {
	Sid       string `json:"sid"`
	ChunkSize int64  `json:"chunkSize"`
	Have      []int  `json:"have"`
}

func doInit(t *testing.T, s *httpServer, name string, size, mtime int64) (int, initResp) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"name": name, "size": size, "mtime": mtime})
	req := httptest.NewRequest(http.MethodPost, "/upload/init", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	s.handle(w, req)
	var r initResp
	if w.Code == http.StatusOK {
		_ = json.Unmarshal(w.Body.Bytes(), &r)
	}
	return w.Code, r
}

func doPart(t *testing.T, s *httpServer, sid string, idx int, data []byte) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPut, fmt.Sprintf("/upload/part?sid=%s&idx=%d", sid, idx), bytes.NewReader(data))
	w := httptest.NewRecorder()
	s.handle(w, req)
	return w.Code
}

func doComplete(t *testing.T, s *httpServer, sid string) int {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/upload/complete?sid="+sid, nil)
	w := httptest.NewRecorder()
	s.handle(w, req)
	return w.Code
}

func captureEvents(t *testing.T) *[]Progress {
	t.Helper()
	evs := &[]Progress{}
	SetEmitter(func(_ string, data ...any) {
		if len(data) > 0 {
			if p, ok := data[0].(Progress); ok {
				*evs = append(*evs, p)
			}
		}
	})
	t.Cleanup(func() { SetEmitter(nil) })
	return evs
}

func hasEvent(evs []Progress, id, status string) bool {
	for _, p := range evs {
		if p.ID == id && p.Status == status {
			return true
		}
	}
	return false
}

// TestUploadSessionRoundTripOutOfOrder：>1 片文件乱序上传，拼回落收件箱，发 done 事件。
func TestUploadSessionRoundTripOutOfOrder(t *testing.T) {
	s, inbox := newTestServer(t)
	resetUploads(t)
	evs := captureEvents(t)

	size := int64(uploadChunkSize) + 5000 // 2 片：满片 + 末片 5000
	full := expectContent(size)

	code, r := doInit(t, s, "rt.bin", size, 111)
	if code != http.StatusOK || r.Sid == "" || r.ChunkSize != uploadChunkSize || len(r.Have) != 0 {
		t.Fatalf("init 异常: %d %+v", code, r)
	}
	// 先发末片（乱序）
	if c := doPart(t, s, r.Sid, 1, full[uploadChunkSize:]); c != http.StatusOK {
		t.Fatalf("末片应 200: %d", c)
	}
	// 分片未齐就 complete → 400
	if c := doComplete(t, s, r.Sid); c != http.StatusBadRequest {
		t.Fatalf("缺片 complete 应 400: %d", c)
	}
	// 再补首片
	if c := doPart(t, s, r.Sid, 0, full[:uploadChunkSize]); c != http.StatusOK {
		t.Fatalf("首片应 200: %d", c)
	}
	if c := doComplete(t, s, r.Sid); c != http.StatusOK {
		t.Fatalf("齐片 complete 应 200: %d", c)
	}

	got, err := os.ReadFile(filepath.Join(inbox, "rt.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if int64(len(got)) != size {
		t.Fatalf("拼回长度不符: got=%d want=%d", len(got), size)
	}
	for i := range got {
		if got[i] != full[i] {
			t.Fatalf("第 %d 字节内容不符", i)
		}
	}
	// 会话目录应被清理
	if _, err := os.Stat(filepath.Join(mustUploadsDir(t), r.Sid)); !os.IsNotExist(err) {
		t.Fatalf("完成后会话目录应删除: %v", err)
	}
	if !hasEvent(*evs, r.Sid, "done") {
		t.Fatalf("应发 done 事件且 ID=sid: %+v", *evs)
	}
	// 会话列表不再含该 sid
	lst, _ := ListUploadSessions()
	for _, ss := range lst {
		if ss.ID == r.Sid {
			t.Fatal("完成后不应在会话列表里")
		}
	}
}

// TestUploadSessionResumeHave：中断后重传命中同 sid，init 回已收 have。
func TestUploadSessionResumeHave(t *testing.T) {
	s, inbox := newTestServer(t)
	resetUploads(t)

	size := int64(uploadChunkSize) + 100
	full := expectContent(size)
	const mtime = 20260101

	code, r := doInit(t, s, "resume.bin", size, mtime)
	if code != http.StatusOK {
		t.Fatalf("首次 init 应 200: %d", code)
	}
	if c := doPart(t, s, r.Sid, 0, full[:uploadChunkSize]); c != http.StatusOK {
		t.Fatalf("首片应 200: %d", c)
	}
	// 断点：重传（同 name|size|mtime）应命中同 sid，have 含已收的第 0 片
	code, r2 := doInit(t, s, "resume.bin", size, mtime)
	if code != http.StatusOK {
		t.Fatalf("重传 init 应 200: %d", code)
	}
	if r2.Sid != r.Sid {
		t.Fatalf("同文件指纹应命中同 sid: %s vs %s", r2.Sid, r.Sid)
	}
	if len(r2.Have) != 1 || r2.Have[0] != 0 {
		t.Fatalf("重传应回 have=[0]: %+v", r2.Have)
	}
	// 会话列表能看到中间态
	lst, _ := ListUploadSessions()
	var found bool
	for _, ss := range lst {
		if ss.ID == r.Sid {
			found = true
			if ss.Received != uploadChunkSize || ss.Percent <= 0 || ss.Percent >= 100 {
				t.Fatalf("中间态会话快照异常: %+v", ss)
			}
		}
	}
	if !found {
		t.Fatal("会话列表应能列出续传中的会话")
	}
	// 补齐末片并落定
	if c := doPart(t, s, r.Sid, 1, full[uploadChunkSize:]); c != http.StatusOK {
		t.Fatalf("末片应 200: %d", c)
	}
	if c := doComplete(t, s, r.Sid); c != http.StatusOK {
		t.Fatalf("complete 应 200: %d", c)
	}
	got, _ := os.ReadFile(filepath.Join(inbox, "resume.bin"))
	if !bytes.Equal(got, full) {
		t.Fatal("续传拼回内容不符")
	}
}

// TestUploadSessionCapacity429：会话数达 maxSessions 后新 init 返 429。
func TestUploadSessionCapacity429(t *testing.T) {
	s, _ := newTestServer(t)
	resetUploads(t)

	for i := 0; i < maxSessions; i++ {
		name := fmt.Sprintf("c%d.bin", i)
		if code, _ := doInit(t, s, name, 10, int64(1000+i)); code != http.StatusOK {
			t.Fatalf("第 %d 个会话应成功: %d", i, code)
		}
	}
	if code, _ := doInit(t, s, "overflow.bin", 10, 9999); code != http.StatusTooManyRequests {
		t.Fatalf("超 maxSessions 应 429，实得 %d", code)
	}
}

// TestUploadSessionTTLGc：超 TTL 会话被 gcExpiredSessions 清盘并从列表消失。
func TestUploadSessionTTLGc(t *testing.T) {
	s, _ := newTestServer(t)
	resetUploads(t)

	code, r := doInit(t, s, "old.bin", 12345, 77)
	if code != http.StatusOK {
		t.Fatalf("init 应 200: %d", code)
	}
	// 把 meta 的 updatedAt 改成 25h 前
	metaPath := filepath.Join(mustUploadsDir(t), r.Sid, "meta.json")
	b, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	var m sessionMeta
	if json.Unmarshal(b, &m) != nil {
		t.Fatal("meta 解析失败")
	}
	m.UpdatedAt = time.Now().Add(-25 * time.Hour).UnixMilli()
	nb, _ := json.Marshal(m)
	if err := os.WriteFile(metaPath, nb, 0o644); err != nil {
		t.Fatal(err)
	}
	if alive := gcExpiredSessions(); alive != 0 {
		t.Fatalf("过期会话清完后存活数应为 0，实得 %d", alive)
	}
	if _, err := os.Stat(filepath.Join(mustUploadsDir(t), r.Sid)); !os.IsNotExist(err) {
		t.Fatalf("过期会话目录应被删除: %v", err)
	}
	lst, _ := ListUploadSessions()
	if len(lst) != 0 {
		t.Fatalf("会话列表应为空: %+v", lst)
	}
}

// TestUploadSessionAbort：abort 删会话目录、发 canceled 事件、列表消失。
func TestUploadSessionAbort(t *testing.T) {
	s, _ := newTestServer(t)
	resetUploads(t)
	evs := captureEvents(t)

	code, r := doInit(t, s, "cancel.bin", uploadChunkSize+1, 55)
	if code != http.StatusOK {
		t.Fatalf("init 应 200: %d", code)
	}
	if c := doPart(t, s, r.Sid, 0, expectContent(uploadChunkSize)); c != http.StatusOK {
		t.Fatalf("首片应 200: %d", c)
	}
	req := httptest.NewRequest(http.MethodDelete, "/upload/abort?sid="+r.Sid, nil)
	w := httptest.NewRecorder()
	s.handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("abort 应 200: %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(mustUploadsDir(t), r.Sid)); !os.IsNotExist(err) {
		t.Fatalf("abort 后会话目录应删除: %v", err)
	}
	if !hasEvent(*evs, r.Sid, "canceled") {
		t.Fatalf("应发 canceled 事件: %+v", *evs)
	}
}

// TestUploadInitRespectsBlockExt：切片 init 链路的接收类型黑名单（计划 #1）。
// 默认空黑名单 = 全类型全收（.exe 的 init 也放行 200）；注入 exe 后同名 init → 400。
// 与 multipart 用例（TestUploadRejectsBadNamesAndExecutables）互补，把「默认全收 +
// 黑名单拦截」在两条真实上传链路都做成机器判定。
func TestUploadInitRespectsBlockExt(t *testing.T) {
	s, _ := newTestServer(t)
	resetUploads(t)

	// 默认全收：显式清空后，可执行类 init 应放行
	SetBlockExt(nil)
	defer SetBlockExt(nil)
	if code, r := doInit(t, s, "setup.exe", uploadChunkSize+1, 77); code != http.StatusOK || r.Sid == "" {
		t.Fatalf("默认空黑名单应全收, .exe init 应 200: %d %+v", code, r)
	}

	// 注入危险类型后：同类型 init 应被 400 拦（不落会话）
	SetBlockExt([]string{"exe"})
	if code, _ := doInit(t, s, "payload.exe", uploadChunkSize+1, 88); code != http.StatusBadRequest {
		t.Fatalf("黑名单应拦 .exe init: %d", code)
	}
	// 非黑名单类型仍可收
	if code, _ := doInit(t, s, "report.pdf", uploadChunkSize+1, 99); code != http.StatusOK {
		t.Fatalf("非黑名单类型应放行 init: %d", code)
	}
}

func mustUploadsDir(t *testing.T) string {
	t.Helper()
	d, err := uploadsDir()
	if err != nil {
		t.Fatal(err)
	}
	return d
}
