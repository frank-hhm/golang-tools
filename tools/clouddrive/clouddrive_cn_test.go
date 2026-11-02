package clouddrive

// 阿里 / 百度逆向适配器的 httptest 假后端单测（PR2）。
// 只验证「请求形态 + 响应解析 + 凭证流转 + 错误透出」这层契约，不代表真网可用。

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

/* ============================ 阿里云盘 ============================ */

type fakeAliyun struct {
	srv       *httptest.Server
	mu        sync.Mutex
	tokenHits int
	media     []byte
	uploaded  []byte // 最近一次 PUT 上去的内容（跨盘转存按字节校验用）
	failMsg   string // 非空则 /v2 操作（除 drive/get）回 200 + code/message
	lastAuth  string
}

func (f *fakeAliyun) setUploaded(b []byte) {
	f.mu.Lock()
	f.uploaded = b
	f.mu.Unlock()
}

func (f *fakeAliyun) gotUploaded() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uploaded
}

// setMedia 注入下载内容（假服务在 newTestAliyun 里才创建，外部只能经由 provider 拿到它）。
func (f *fakeAliyun) setMedia(b []byte) {
	f.mu.Lock()
	f.media = b
	f.mu.Unlock()
}

func (f *fakeAliyun) bump() {
	f.mu.Lock()
	f.tokenHits++
	f.mu.Unlock()
}

func (f *fakeAliyun) hits() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenHits
}

func newFakeAliyun(t *testing.T) *fakeAliyun {
	f := &fakeAliyun{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.lastAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		base := "http://" + r.Host
		if f.failMsg != "" && strings.HasPrefix(r.URL.Path, "/v2/") && r.URL.Path != "/v2/drive/get" {
			_ = json.NewEncoder(w).Encode(map[string]any{"code": "Boom", "message": f.failMsg})
			return
		}
		switch r.URL.Path {
		case "/token/refresh":
			f.bump()
			_, _ = w.Write([]byte(`{"access_token":"AT","refresh_token":"RT2","expires_in":3600,"default_drive_id":"drive-1"}`))
		case "/v2/file/list":
			_, _ = w.Write([]byte(`{"items":[` +
				`{"file_id":"d1","name":"Folder","type":"folder","updated_at":"2026-01-01T00:00:00Z"},` +
				`{"file_id":"f1","name":"a.txt","type":"file","size":1234,"updated_at":"2026-01-02T03:04:05Z","content_type":"text/plain"}]}`))
		case "/v2/drive/get":
			_, _ = w.Write([]byte(`{"used_size":100,"total_size":1000}`))
		case "/v2/user/get":
			_, _ = w.Write([]byte(`{"default_drive_id":"drive-1"}`))
		case "/v2/file/create":
			bs, _ := io.ReadAll(r.Body)
			var m map[string]any
			_ = json.Unmarshal(bs, &m)
			if m["type"] == "folder" {
				_, _ = w.Write([]byte(`{"file_id":"dir9","name":"NewDir","type":"folder"}`))
			} else {
				_, _ = w.Write([]byte(`{"file_id":"up1","upload_id":"u1","upload_url":"` + base + `/put"}`))
			}
		case "/v2/file/complete":
			_, _ = w.Write([]byte(`{"file_id":"up1","name":"uploaded","type":"file","size":5}`))
		case "/v2/file/rename", "/v2/file/move", "/v2/recyclebin/trash":
			_, _ = w.Write([]byte(`{}`))
		case "/v2/share_link/create":
			_, _ = w.Write([]byte(`{"share_id":"sh1"}`))
		case "/v2/file/get_download_url":
			_, _ = w.Write([]byte(`{"url":"` + base + `/getfile","size":17}`))
		case "/put": // 上传 PUT
			bs, _ := io.ReadAll(r.Body)
			f.setUploaded(bs)
			w.WriteHeader(http.StatusOK)
		case "/getfile": // 下载 GET
			_, _ = w.Write(f.media)
		default:
			http.Error(w, "unhandled "+r.URL.Path, 500)
		}
	}))
	oldTok, oldAPI := aliyunTokenURL, aliyunAPIBase
	aliyunTokenURL = f.srv.URL + "/token/refresh"
	aliyunAPIBase = f.srv.URL + "/v2"
	t.Cleanup(func() {
		aliyunTokenURL, aliyunAPIBase = oldTok, oldAPI
		f.srv.Close()
	})
	return f
}

func newTestAliyun(t *testing.T) (*aliyunProvider, *fakeAliyun) {
	t.Helper()
	f := newFakeAliyun(t)
	v, err := AddAccount(KindAliyun, "阿里测试")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveToken(v.ID, tokenBlob{Refresh: "RT-INIT"}); err != nil {
		t.Fatal(err)
	}
	return &aliyunProvider{acctID: v.ID, hc: &http.Client{}}, f
}

func TestAliyunListAndRefresh(t *testing.T) {
	a, f := newTestAliyun(t)
	entries, err := a.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("期望 2 条，got %d", len(entries))
	}
	if !entries[0].IsDir || entries[0].ID != "d1" {
		t.Fatalf("目录解析错: %+v", entries[0])
	}
	if entries[1].IsDir || entries[1].Size != 1234 || entries[1].MTime == 0 {
		t.Fatalf("文件解析错: %+v", entries[1])
	}
	if f.hits() != 1 {
		t.Fatalf("期望刷新一次令牌，got %d", f.hits())
	}
	if f.lastAuth != "Bearer AT" {
		t.Fatalf("未带上 access token: %q", f.lastAuth)
	}
	// 刷新后 drive_id 应缓存进账号 token 包。
	b, _ := loadToken(a.acctID)
	if b.DriveID != "drive-1" || b.Refresh != "RT2" {
		t.Fatalf("drive_id/refresh 未回写: %+v", b)
	}
}

func TestAliyunMutationsQuotaShare(t *testing.T) {
	a, _ := newTestAliyun(t)
	ctx := context.Background()
	e, err := a.Mkdir(ctx, "", "NewDir")
	if err != nil || e.ID != "dir9" || !e.IsDir {
		t.Fatalf("Mkdir: %+v err=%v", e, err)
	}
	if err := a.Rename(ctx, "f1", "ren.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := a.Move(ctx, "f1", "d1"); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if err := a.Delete(ctx, "f1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	link, err := a.ShareLink(ctx, "f1")
	if err != nil || !strings.Contains(link, "/s/sh1") {
		t.Fatalf("ShareLink: %q err=%v", link, err)
	}
	q, err := a.Quota(ctx)
	if err != nil || q.Used != 100 || q.Total != 1000 {
		t.Fatalf("Quota: %+v err=%v", q, err)
	}
	if err := a.Test(ctx); err != nil {
		t.Fatalf("Test: %v", err)
	}
}

func TestAliyunOpenUpload(t *testing.T) {
	a, f := newTestAliyun(t)
	f.media = []byte("hello aliyun bytes")
	rc, size, err := a.Open(context.Background(), "f1")
	if err != nil {
		t.Fatal(err)
	}
	bs, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(bs) != "hello aliyun bytes" {
		t.Fatalf("下载内容不一致: %q", bs)
	}
	if size <= 0 {
		t.Fatalf("size 应 >0，got %d", size)
	}
	e, err := a.Upload(context.Background(), "d1", "uploaded", strings.NewReader("12345"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "up1" {
		t.Fatalf("Upload 响应解析错: %+v", e)
	}
}

func TestAliyunErrorSurface(t *testing.T) {
	a, f := newTestAliyun(t)
	f.failMsg = "boom"
	if err := a.Delete(context.Background(), "f1"); err == nil {
		t.Fatal("应返回错误")
	} else if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("错误应带 API 详情: %v", err)
	}
}

/* ============================ 百度网盘 ============================ */

type fakeBaidu struct {
	srv        *httptest.Server
	mu         sync.Mutex
	media      []byte
	uploaded   []byte // 最近一次上传上去的内容
	listResp   string
	failErrno  int
	lastCookie string
}

func (f *fakeBaidu) setUploaded(b []byte) {
	f.mu.Lock()
	f.uploaded = b
	f.mu.Unlock()
}

func (f *fakeBaidu) gotUploaded() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.uploaded
}

// setMedia 注入下载内容（同 fakeAliyun.setMedia）。
func (f *fakeBaidu) setMedia(b []byte) {
	f.mu.Lock()
	f.media = b
	f.mu.Unlock()
}

// getMedia 安全读取下载内容。
func (f *fakeBaidu) getMedia() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.media
}

func newFakeBaidu(t *testing.T) *fakeBaidu {
	f := &fakeBaidu{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.lastCookie = r.Header.Get("Cookie")
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		switch r.URL.Path {
		case "/rest/2.0/xpan/file":
			if q.Get("method") == "list" {
				resp := f.listResp
				if resp == "" {
					resp = `{"list":[` +
						`{"fs_id":1,"server_filename":"Folder","path":"/Folder","isdir":1,"size":0,"server_mtime":1735689600},` +
						`{"fs_id":2,"server_filename":"a.txt","path":"/a.txt","isdir":0,"size":1234,"server_mtime":1735689600}]}`
				}
				_, _ = w.Write([]byte(resp))
				return
			}
			http.Error(w, "unhandled file method", 500)
		case "/rest/2.0/xpan/multimedia":
			if f.failErrno != 0 {
				_, _ = w.Write([]byte(`{"errno":-9,"request_id":1}`))
				return
			}
			switch q.Get("opera") {
			case "precreate":
				_, _ = w.Write([]byte(`{"errno":0,"uploadid":"uid1"}`))
			default: // create / rename / move / delete
				_, _ = w.Write([]byte(`{"errno":0,"request_id":1}`))
			}
		case "/rest/2.0/pcs/quota":
			_, _ = w.Write([]byte(`{"total_size":1000,"used_size":100,"request_id":1}`))
		case "/rest/2.0/pcs/file":
			bs := f.getMedia()
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(bs)))
			w.Write(bs)
		case "/rest/2.0/pcs/superfile2":
			// 转存/上传落点：multipart 的 file 部分即分块内容，按字节校验用
			if mf, err := r.MultipartReader(); err == nil {
				for {
					p, perr := mf.NextPart()
					if perr != nil {
						break
					}
					if p.FormName() == "file" {
						bs, _ := io.ReadAll(p)
						f.setUploaded(bs)
					}
					_ = p.Close()
				}
			}
			_, _ = w.Write([]byte(`{"md5":"chunkmd5","request_id":1}`))
		default:
			http.Error(w, "unhandled "+r.URL.Path, 500)
		}
	}))
	oldWeb, oldPCS := baiduWebBase, baiduPCSBase
	baiduWebBase = f.srv.URL
	baiduPCSBase = f.srv.URL
	t.Cleanup(func() {
		baiduWebBase, baiduPCSBase = oldWeb, oldPCS
		f.srv.Close()
	})
	return f
}

func newTestBaidu(t *testing.T) (*baiduProvider, *fakeBaidu) {
	t.Helper()
	f := newFakeBaidu(t)
	v, err := AddAccount(KindBaidu, "百度测试")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveToken(v.ID, tokenBlob{Cookie: "BDUSSVAL"}); err != nil {
		t.Fatal(err)
	}
	return &baiduProvider{acctID: v.ID, hc: &http.Client{}}, f
}

func TestBaiduListAndCookie(t *testing.T) {
	b, f := newTestBaidu(t)
	entries, err := b.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("期望 2 条，got %d", len(entries))
	}
	if !entries[0].IsDir || entries[0].ID != "/Folder" {
		t.Fatalf("目录解析错: %+v", entries[0])
	}
	if entries[1].Size != 1234 || entries[1].MTime != 1735689600*1000 {
		t.Fatalf("文件解析错: %+v", entries[1])
	}
	if !strings.Contains(f.lastCookie, "BDUSS=BDUSSVAL") {
		t.Fatalf("未带上 BDUSS cookie: %q", f.lastCookie)
	}
}

func TestBaiduMutationsQuota(t *testing.T) {
	b, _ := newTestBaidu(t)
	ctx := context.Background()
	e, err := b.Mkdir(ctx, "", "NewDir")
	if err != nil || e.ID != "/NewDir" || !e.IsDir {
		t.Fatalf("Mkdir: %+v err=%v", e, err)
	}
	if err := b.Rename(ctx, "/a.txt", "ren.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := b.Move(ctx, "/a.txt", "/Folder"); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if err := b.Delete(ctx, "/a.txt"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	q, err := b.Quota(ctx)
	if err != nil || q.Used != 100 || q.Total != 1000 {
		t.Fatalf("Quota: %+v err=%v", q, err)
	}
	if err := b.Test(ctx); err != nil {
		t.Fatalf("Test: %v", err)
	}
	// 分享如实不支持。
	if _, err := b.ShareLink(ctx, "/a.txt"); err == nil {
		t.Fatal("百度分享应返回不支持的错误")
	}
}

func TestBaiduOpenUpload(t *testing.T) {
	b, f := newTestBaidu(t)
	f.media = []byte("hello baidu bytes")
	rc, _, err := b.Open(context.Background(), "/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	bs, _ := io.ReadAll(rc)
	_ = rc.Close()
	if string(bs) != "hello baidu bytes" {
		t.Fatalf("下载内容不一致: %q", bs)
	}
	e, err := b.Upload(context.Background(), "", "up.txt", strings.NewReader("12345"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if e.Name != "up.txt" || e.ID != "/up.txt" {
		t.Fatalf("Upload 结果错: %+v", e)
	}
}

func TestBaiduErrorSurface(t *testing.T) {
	b, f := newTestBaidu(t)
	f.failErrno = -9
	if err := b.Delete(context.Background(), "/a.txt"); err == nil {
		t.Fatal("errno≠0 应返回错误")
	} else if !strings.Contains(err.Error(), "errno") {
		t.Fatalf("错误应带 errno: %v", err)
	}
}

/* ============================ 凭证写入 & 注册 ============================ */

func TestSetCredentialByKind(t *testing.T) {
	av, err := AddAccount(KindAliyun, "ali")
	if err != nil {
		t.Fatal(err)
	}
	if err := SetCredential(av.ID, " refresh-token "); err != nil {
		t.Fatal(err)
	}
	b, err := loadToken(av.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Refresh != "refresh-token" || b.Cookie != "" {
		t.Fatalf("阿里凭证存错: %+v", b)
	}

	bv, err := AddAccount(KindBaidu, "bai")
	if err != nil {
		t.Fatal(err)
	}
	if err := SetCredential(bv.ID, "BDUSS-XYZ"); err != nil {
		t.Fatal(err)
	}
	bb, err := loadToken(bv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bb.Cookie != "BDUSS-XYZ" || bb.Refresh != "" {
		t.Fatalf("百度凭证存错: %+v", bb)
	}

	// google 不走手动凭证。
	gv, _ := AddAccount(KindGoogle, "g")
	if err := SetCredential(gv.ID, "x"); err == nil {
		t.Fatal("google 不应接受手动凭证")
	}
	// 空值拒绝。
	if err := SetCredential(av.ID, "  "); err == nil {
		t.Fatal("空凭证应被拒绝")
	}
}

func TestRegistryHasAliyunBaidu(t *testing.T) {
	if !KindSupported(KindAliyun) {
		t.Fatal("aliyun 应已注册")
	}
	if !KindSupported(KindBaidu) {
		t.Fatal("baidu 应已注册")
	}
	av, err := AddAccount(KindAliyun, "a")
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(av.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "阿里云盘" {
		t.Fatalf("provider 名不对: %s", p.Name())
	}
	bv, _ := AddAccount(KindBaidu, "b")
	pb, err := New(bv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if pb.Name() != "百度网盘" {
		t.Fatalf("provider 名不对: %s", pb.Name())
	}
}
