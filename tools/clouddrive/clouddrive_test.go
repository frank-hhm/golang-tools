package clouddrive

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain 把账号表指向临时目录，测试不污染真实 appdirs（token 落盘走 tools/secret）。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "clouddrive-test")
	if err != nil {
		panic(err)
	}
	setBaseDirForTest(dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// fakeGoogle 一个假的 Google OAuth + Drive 后端，覆盖适配器用到的全部端点。
type fakeGoogle struct {
	srv       *httptest.Server
	mu        sync.Mutex
	tokenHits int
	listResp  string
	media     []byte
	lastAuth  string // 记录最近一次请求带的 Authorization
	failCode  int    // >0 时 Drive 请求返回该状态
}

func (f *fakeGoogle) bumpToken() {
	f.mu.Lock()
	f.tokenHits++
	f.mu.Unlock()
}

func (f *fakeGoogle) tokenCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenHits
}

func newFakeGoogle(t *testing.T) *fakeGoogle {
	f := &fakeGoogle{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.lastAuth = r.Header.Get("Authorization")
		switch {
		case r.URL.Path == "/token":
			_ = r.ParseForm()
			w.Header().Set("Content-Type", "application/json")
			if r.Form.Get("grant_type") == "authorization_code" {
				_, _ = w.Write([]byte(`{"access_token":"AT1","refresh_token":"RT1","expires_in":3600}`))
			} else {
				f.bumpToken()
				_, _ = w.Write([]byte(`{"access_token":"AT2","expires_in":3600}`))
			}
		case r.URL.Path == "/drive/about":
			_, _ = w.Write([]byte(`{"quotaBytesUsed":"100","quotaBytesTotal":"1000"}`))
		case r.URL.Path == "/drive/files" && r.Method == http.MethodGet:
			resp := f.listResp
			if resp == "" {
				resp = `{"files":[` +
					`{"id":"d1","name":"Folder","mimeType":"application/vnd.google-apps.folder","modifiedTime":"2026-01-01T00:00:00Z","parents":["root"]},` +
					`{"id":"f1","name":"a.txt","mimeType":"text/plain","size":"1234","modifiedTime":"2026-01-02T03:04:05Z","parents":["root"]}]}`
			}
			_, _ = w.Write([]byte(resp))
		case r.URL.Path == "/drive/files" && r.Method == http.MethodPost:
			ct := r.Header.Get("Content-Type")
			if strings.HasPrefix(ct, "multipart/") {
				_, _ = w.Write([]byte(`{"id":"up1","name":"uploaded","mimeType":"application/octet-stream","size":"5","modifiedTime":"2026-01-03T00:00:00Z"}`))
			} else {
				bs, _ := io.ReadAll(r.Body)
				var meta map[string]any
				_ = json.Unmarshal(bs, &meta)
				_, _ = w.Write([]byte(fmt.Sprintf(`{"id":"dir9","name":%q,"mimeType":"application/vnd.google-apps.folder"}`, meta["name"])))
			}
		case strings.HasSuffix(r.URL.Path, "/permissions"):
			_, _ = w.Write([]byte(`{"id":"perm1"}`))
		case strings.HasPrefix(r.URL.Path, "/drive/files/"):
			id := strings.TrimPrefix(r.URL.Path, "/drive/files/")
			if f.failCode > 0 {
				http.Error(w, `{"error":{"message":"boom"}}`, f.failCode)
				return
			}
			switch r.Method {
			case http.MethodGet:
				if r.URL.Query().Get("alt") == "media" {
					w.Header().Set("Content-Length", fmt.Sprintf("%d", len(f.media)))
					_, _ = w.Write(f.media)
				} else {
					_, _ = w.Write([]byte(`{"parents":["oldParent"],"id":"` + id + `"}`))
				}
			case http.MethodPatch:
				_, _ = w.Write([]byte(`{"id":"` + id + `"}`))
			case http.MethodDelete:
				w.WriteHeader(http.StatusNoContent)
			}
		default:
			http.Error(w, "unhandled "+r.URL.Path, 500)
		}
	}))
	// 把包级端点变量指向假服务器，测试结束还原。
	oldTok, oldBase, oldUp, oldWeb, oldAuth := googleTokenURL, driveBase, driveUploadBase, driveWebBase, googleAuthURL
	googleTokenURL = f.srv.URL + "/token"
	driveBase = f.srv.URL + "/drive"
	driveUploadBase = f.srv.URL + "/drive"
	driveWebBase = f.srv.URL
	googleAuthURL = f.srv.URL + "/auth"
	t.Cleanup(func() {
		googleTokenURL, driveBase, driveUploadBase, driveWebBase, googleAuthURL = oldTok, oldBase, oldUp, oldWeb, oldAuth
		f.srv.Close()
	})
	return f
}

// newTestProvider 直接构造 googleProvider（绕开 config 的客户端凭证读取），并预置账号 token。
func newTestProvider(t *testing.T, access string, expiry int64) *googleProvider {
	t.Helper()
	v, err := AddAccount(KindGoogle, "测试账号")
	if err != nil {
		t.Fatal(err)
	}
	if err := saveToken(v.ID, tokenBlob{Access: access, Refresh: "RT1", Expiry: expiry}); err != nil {
		t.Fatal(err)
	}
	return &googleProvider{
		acctID: v.ID, clientID: "cid", secret: "sec",
		redirect: "http://localhost:8458/callback", hc: &http.Client{},
	}
}

func TestStoreRoundTripAndRedact(t *testing.T) {
	v, err := AddAccount(KindGoogle, " 我的谷歌盘 ")
	if err != nil {
		t.Fatal(err)
	}
	if v.Label != "我的谷歌盘" {
		t.Fatalf("Label 未 trim: %q", v.Label)
	}
	if v.HasToken {
		t.Fatal("新账号不应有 token")
	}
	// 账号表里存的密文绝不能出现在回前端的 view 里。
	list, err := ListAccounts()
	if err != nil {
		t.Fatal(err)
	}
	bs, _ := json.Marshal(list)
	if strings.Contains(string(bs), "secretEnc") || strings.Contains(string(bs), "SecretEnc") {
		t.Fatal("AccountView 泄露了密文字段")
	}
	// token 往返：密文落盘，load 回来结构一致。
	if err := saveToken(v.ID, tokenBlob{Access: "A", Refresh: "R", Expiry: 123}); err != nil {
		t.Fatal(err)
	}
	b, err := loadToken(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Access != "A" || b.Refresh != "R" || b.Expiry != 123 {
		t.Fatalf("token 往返不一致: %+v", b)
	}
	if err := RemoveAccount(v.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := loadToken(v.ID); err == nil {
		t.Fatal("删除后仍能读到账号")
	}
}

func TestAddAccountRejectsUnknownKind(t *testing.T) {
	if _, err := AddAccount("onedrive", "x"); err == nil {
		t.Fatal("未知 kind 应被拒绝")
	}
}

func TestExtractCode(t *testing.T) {
	cases := map[string]string{
		"http://localhost:8458/callback?code=ABC123&scope=x": "ABC123",
		"https://x/y?state=s&code=ZZ&extra=1":                "ZZ",
		"PLAINCODE":                                          "PLAINCODE",
		"http://localhost/callback?error=access_denied":      "",
		"code=QWE asd":                                       "QWE",
	}
	for in, want := range cases {
		if got := extractCode(in); got != want {
			t.Errorf("extractCode(%q)=%q want %q", in, got, want)
		}
	}
}

func TestExchangeStoresToken(t *testing.T) {
	_ = newFakeGoogle(t)
	v, err := AddAccount(KindGoogle, "ex")
	if err != nil {
		t.Fatal(err)
	}
	g := &googleProvider{acctID: v.ID, clientID: "cid", secret: "sec", redirect: "http://localhost:8458/callback", hc: &http.Client{}}
	if err := g.Exchange(context.Background(), "http://localhost:8458/callback?code=ABC"); err != nil {
		t.Fatalf("Exchange 失败: %v", err)
	}
	b, err := loadToken(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b.Access != "AT1" || b.Refresh != "RT1" {
		t.Fatalf("Exchange 未存对 token: %+v", b)
	}
	// AuthURL 含 offline + consent，确保拿得到 refresh_token。
	u, err := g.AuthURL("st")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(u, "access_type=offline") || !strings.Contains(u, "prompt=consent") {
		t.Fatalf("AuthURL 缺关键参数: %s", u)
	}
}

func TestListParsesEntries(t *testing.T) {
	f := newFakeGoogle(t)
	g := newTestProvider(t, "AT", time.Now().Add(time.Hour).UnixMilli())
	entries, err := g.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("期望 2 条，got %d", len(entries))
	}
	if !entries[0].IsDir || entries[0].Name != "Folder" {
		t.Fatalf("目录解析错: %+v", entries[0])
	}
	if entries[1].IsDir || entries[1].Size != 1234 || entries[1].MTime == 0 {
		t.Fatalf("文件解析错: %+v", entries[1])
	}
	if f.lastAuth != "Bearer AT" {
		t.Fatalf("未带上 access token: %q", f.lastAuth)
	}
}

func TestTokenRefreshOnExpiry(t *testing.T) {
	f := newFakeGoogle(t)
	// access 已过期：应触发 refresh，用回 AT2。
	g := newTestProvider(t, "OLD", time.Now().Add(-time.Minute).UnixMilli())
	if _, err := g.List(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	if f.tokenCount() != 1 {
		t.Fatalf("期望刷新一次，got %d", f.tokenCount())
	}
	if f.lastAuth != "Bearer AT2" {
		t.Fatalf("刷新后未用新 token: %q", f.lastAuth)
	}
}

func TestMkdirRenameMoveDeleteShareQuota(t *testing.T) {
	_ = newFakeGoogle(t)
	g := newTestProvider(t, "AT", time.Now().Add(time.Hour).UnixMilli())
	ctx := context.Background()

	e, err := g.Mkdir(ctx, "", "NewDir")
	if err != nil || e.ID != "dir9" || !e.IsDir {
		t.Fatalf("Mkdir: %+v err=%v", e, err)
	}
	if err := g.Rename(ctx, "f1", "ren.txt"); err != nil {
		t.Fatalf("Rename: %v", err)
	}
	if err := g.Move(ctx, "f1", "d1"); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if err := g.Delete(ctx, "f1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	link, err := g.ShareLink(ctx, "f1")
	if err != nil || !strings.Contains(link, "/file/d/f1/view") {
		t.Fatalf("ShareLink: %q err=%v", link, err)
	}
	q, err := g.Quota(ctx)
	if err != nil || q.Used != 100 || q.Total != 1000 {
		t.Fatalf("Quota: %+v err=%v", q, err)
	}
	if err := g.Test(ctx); err != nil {
		t.Fatalf("Test: %v", err)
	}
}

func TestOpenStream(t *testing.T) {
	f := newFakeGoogle(t)
	f.media = []byte("hello cloud bytes")
	g := newTestProvider(t, "AT", time.Now().Add(time.Hour).UnixMilli())
	rc, size, err := g.Open(context.Background(), "f1")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	bs, _ := io.ReadAll(rc)
	if string(bs) != "hello cloud bytes" {
		t.Fatalf("下载内容不一致: %q", bs)
	}
	if size != int64(len(f.media)) {
		t.Fatalf("size=%d want %d", size, len(f.media))
	}
}

func TestUpload(t *testing.T) {
	_ = newFakeGoogle(t)
	g := newTestProvider(t, "AT", time.Now().Add(time.Hour).UnixMilli())
	e, err := g.Upload(context.Background(), "d1", "uploaded", strings.NewReader("12345"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "up1" || e.Name != "uploaded" {
		t.Fatalf("Upload 响应解析错: %+v", e)
	}
}

func TestErrorSurface(t *testing.T) {
	f := newFakeGoogle(t)
	f.failCode = http.StatusNotFound
	g := newTestProvider(t, "AT", time.Now().Add(time.Hour).UnixMilli())
	if err := g.Delete(context.Background(), "missing"); err == nil {
		t.Fatal("404 应返回错误")
	} else if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("错误应带上 API 详情: %v", err)
	}
}

func TestRegistryNew(t *testing.T) {
	_ = newFakeGoogle(t)
	v, err := AddAccount(KindGoogle, "reg")
	if err != nil {
		t.Fatal(err)
	}
	p, err := New(v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "Google Drive" {
		t.Fatalf("provider 名不对: %s", p.Name())
	}
	if KindSupported(KindGoogle) == false {
		t.Fatal("google 应已注册")
	}
	if KindSupported("dropbox") {
		t.Fatal("未实现的种类不应报支持")
	}
	if _, err := New("nonexistent"); err == nil {
		t.Fatal("不存在的账号应报错")
	}
}
