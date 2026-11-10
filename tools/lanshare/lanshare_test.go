package lanshare

import (
	"archive/zip"
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain 抢在任何 baseDir() 之前把数据目录指向临时目录：
// 链接落盘与访问日志都会碰它，不能污染真实应用数据。
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "lanshare-test-*")
	if err != nil {
		panic(err)
	}
	setBaseDirForTest(dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// newTestServer 起一个只挂了路由的沙箱服务（不监听端口，全部经 httptest 驱动）。
func newTestServer(t *testing.T) (*httpServer, string) {
	t.Helper()
	inbox := filepath.Join(t.TempDir(), "inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	rootObj, err := os.OpenRoot(inbox)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = rootObj.Close() })
	s := &httpServer{inbox: inbox, root: rootObj, lim: newRateLimiter(ratePerMinute, time.Minute)}
	return s, inbox
}

func postUpload(t *testing.T, s *httpServer, names map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for name, content := range names {
		fw, err := mw.CreateFormFile("file", name)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.WriteString(fw, content)
	}
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, "/upload", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	s.handle(w, req)
	return w
}

func TestUploadLandsAndAvoidsDuplicate(t *testing.T) {
	s, inbox := newTestServer(t)

	w := postUpload(t, s, map[string]string{"报告.txt": "hello"})
	if w.Code != http.StatusOK {
		t.Fatalf("上传应成功: %d %s", w.Code, w.Body.String())
	}
	bs, err := os.ReadFile(filepath.Join(inbox, "报告.txt"))
	if err != nil || string(bs) != "hello" {
		t.Fatalf("落盘内容不符: %q %v", bs, err)
	}

	// 同名再来一次 → 重名避让，不覆盖首个
	w = postUpload(t, s, map[string]string{"报告.txt": "second"})
	if w.Code != http.StatusOK {
		t.Fatalf("第二次上传应成功: %d", w.Code)
	}
	first, _ := os.ReadFile(filepath.Join(inbox, "报告.txt"))
	if string(first) != "hello" {
		t.Fatalf("首个文件被覆盖: %q", first)
	}
	second, err := os.ReadFile(filepath.Join(inbox, "报告-1.txt"))
	if err != nil || string(second) != "second" {
		t.Fatalf("重名避让失败: %q %v", second, err)
	}
	if got := s.uploadCnt.Load(); got != 2 {
		t.Fatalf("上传统计应为 2，实际 %d", got)
	}
}

func TestUploadRejectsBadNamesAndExecutables(t *testing.T) {
	s, inbox := newTestServer(t)

	// 路径穿越：文件名里的 ../ 被剥掉，只落 basename
	w := postUpload(t, s, map[string]string{"../../evil.txt": "x"})
	if w.Code != http.StatusOK {
		t.Fatalf("应剥掉穿越段后接收: %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(inbox, "evil.txt")); err != nil {
		t.Fatalf("净化后应按 basename 落盘: %v", err)
	}
	entries, _ := os.ReadDir(inbox)
	for _, e := range entries {
		if strings.Contains(e.Name(), "..") {
			t.Fatalf("收件箱出现穿越名: %s", e.Name())
		}
	}

	// 可执行类型被拒（纯拒绝，无任何其他文件时返回 400）：
	// 默认黑名单空=全收，这里显式注入危险类型再验拦截。
	SetBlockExt([]string{"exe"})
	defer SetBlockExt(nil)
	s2, _ := newTestServer(t)
	w = postUpload(t, s2, map[string]string{"payload.exe": "MZ"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("可执行文件应被拒: %d %s", w.Code, w.Body.String())
	}
}

func TestShareLinkDownloadExpiryAndCount(t *testing.T) {
	s, _ := newTestServer(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "doc.pdf")
	if err := os.WriteFile(target, []byte("PDFDATA"), 0o644); err != nil {
		t.Fatal(err)
	}

	l, err := CreateLink(target, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if l.Token == "" || l.Bytes != 7 {
		t.Fatalf("链接字段异常: %+v", l)
	}

	get := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.handle(w, httptest.NewRequest(http.MethodGet, "/s/"+l.Token, nil))
		return w
	}

	w := get()
	if w.Code != http.StatusOK || w.Body.String() != "PDFDATA" {
		t.Fatalf("首次下载应成功: %d %q", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "doc.pdf") {
		t.Fatalf("Content-Disposition 缺文件名: %q", cd)
	}
	// Range 续传能力（ServeContent 提供）
	wr := httptest.NewRecorder()
	rq := httptest.NewRequest(http.MethodGet, "/s/"+l.Token, nil)
	rq.Header.Set("Range", "bytes=0-2")
	s.handle(wr, rq)
	if wr.Code != http.StatusPartialContent || wr.Body.String() != "PDF" {
		t.Fatalf("Range 应回 206/部分内容: %d %q", wr.Code, wr.Body.String())
	}

	// 次数用尽 → 404（整次 + Range 续传各扣一次，共 3 次额度已用完）
	if w := get(); w.Code != http.StatusOK {
		t.Fatalf("第 3 次仍在额度内: %d", w.Code)
	}
	if w := get(); w.Code != http.StatusNotFound {
		t.Fatalf("额度用尽后应 404: %d", w.Code)
	}
	if links, err := ListLinks(); err != nil || links[0].Downloads != 3 {
		t.Fatalf("下载计数应持久化为 3: %+v err=%v", links, err)
	}

	// 未知 token / 实体被删 / 撤销 都回 404
	we := httptest.NewRecorder()
	s.handle(we, httptest.NewRequest(http.MethodGet, "/s/nope123", nil))
	if we.Code != http.StatusNotFound {
		t.Fatalf("未知 token 应 404: %d", we.Code)
	}
	if err := RevokeLink(l.Token); err != nil {
		t.Fatal(err)
	}
	if w := get(); w.Code != http.StatusNotFound {
		t.Fatalf("撤销后应 404: %d", w.Code)
	}
}

func TestLinkExpiryAndDirRejected(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a.txt")
	_ = os.WriteFile(target, []byte("x"), 0o644)

	// 目录不支持分享（需打包，顺延）
	if _, err := CreateLink(dir, 0, 0); err == nil {
		t.Fatal("目录应被拒绝")
	}
	// 过期时间在过去 → 判活为 false
	_, err := CreateLink(target, -1, 0) // hours<=0 视为不过期，此处只验证创建成功
	if err != nil {
		t.Fatal(err)
	}
	dead := Link{Token: "t", Path: target, ExpiryAt: time.Now().Add(-time.Hour).UnixMilli()}
	if dead.alive(time.Now()) {
		t.Fatal("已过期链接不应可用")
	}
	missing := Link{Token: "t", Path: filepath.Join(dir, "gone.txt")}
	if missing.alive(time.Now()) {
		t.Fatal("实体不存在的链接不应可用")
	}
}

func TestSanitizeName(t *testing.T) {
	cases := map[string]string{
		"a.txt":             "a.txt",
		`C:\win\path\x.txt`: "x.txt",
		"../../etc/passwd":  "passwd", // basename 优先，穿越段整体消失
		".htaccess":         "htaccess",
		"  spa ce.txt ":     "spa ce.txt",
		"":                  "",
		"bad<>|name.txt":    "bad___name.txt",
	}
	for in, want := range cases {
		if got := sanitizeName(in); got != want {
			t.Errorf("sanitizeName(%q)=%q, 期望 %q", in, got, want)
		}
	}
	for _, s := range []string{"a/../b.txt", "C:\\x\\y.txt"} {
		got := sanitizeName(s)
		if strings.ContainsAny(got, `/:\\`) || strings.Contains(got, "..") {
			t.Errorf("净化结果仍含路径字符: %q -> %q", s, got)
		}
	}
	// 默认黑名单为空 = 全收（用户诉求）；未注入时任何类型都不拦。
	SetBlockExt(nil)
	if uploadBlocked("Run.EXE") || uploadBlocked("x.ps1") || uploadBlocked("x.pdf") {
		t.Error("默认空黑名单应全收，不应拦任何类型")
	}
	// 注入危险类型后按扩展名（不区分大小写）拦截，其余放行。
	SetBlockExt([]string{"exe", "ps1"})
	t.Cleanup(func() { SetBlockExt(nil) })
	if !uploadBlocked("Run.EXE") || !uploadBlocked("x.ps1") || uploadBlocked("x.pdf") {
		t.Error("可执行黑名单判定不符")
	}
}

func TestRateLimiter(t *testing.T) {
	lim := newRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !lim.Allow("1.2.3.4") {
			t.Fatalf("前 3 次应放行，第 %d 次被拒", i+1)
		}
	}
	if lim.Allow("1.2.3.4") {
		t.Fatal("第 4 次应被限")
	}
	if !lim.Allow("9.9.9.9") {
		t.Fatal("另一个 IP 不应受影响")
	}
	short := newRateLimiter(1, 10*time.Millisecond)
	if !short.Allow("x") {
		t.Fatal("首次应放行")
	}
	time.Sleep(20 * time.Millisecond)
	if !short.Allow("x") {
		t.Fatal("窗口滑过后应重新放行")
	}
}

func TestShareURLNeedsRunning(t *testing.T) {
	_ = Stop() // 保险：本用例不依赖服务在跑
	if _, err := ShareURL("tok"); err == nil {
		t.Fatal("服务未运行时应报错，而不是回一个假 URL")
	}
}

func TestIndexPageServed(t *testing.T) {
	s, inbox := newTestServer(t)
	w := httptest.NewRecorder()
	s.handle(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("首页应 200: %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "局域网互传") || !strings.Contains(w.Body.String(), filepath.Base(inbox)) {
		t.Fatal("首页应含标题与收件箱路径")
	}
	// 未知路径 404；错误方法 404
	w = httptest.NewRecorder()
	s.handle(w, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("未知路径应 404: %d", w.Code)
	}
	w = httptest.NewRecorder()
	s.handle(w, httptest.NewRequest(http.MethodPost, "/", strings.NewReader("")))
	if w.Code != http.StatusNotFound {
		t.Fatalf("POST / 应 404: %d", w.Code)
	}
}

// findGrant 在共享的 grants.json 里按路径找出刚登记的那条。
func findGrant(t *testing.T, path string) Grant {
	t.Helper()
	abs, _ := filepath.Abs(path)
	gs, err := ListGrants()
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range gs {
		if g.Path == abs {
			return g
		}
	}
	t.Fatalf("未找到授权: %s", abs)
	return Grant{}
}

func TestGrantFileDownloadAndRevoke(t *testing.T) {
	s, _ := newTestServer(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "photo.jpg")
	if err := os.WriteFile(target, []byte("IMAGEDATA"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := AddGrant(target, false, 0); err != nil {
		t.Fatal(err)
	}
	g := findGrant(t, target)
	get := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.handle(w, httptest.NewRequest(http.MethodGet, "/g/"+g.ID, nil))
		return w
	}
	w := get()
	if w.Code != http.StatusOK || w.Body.String() != "IMAGEDATA" {
		t.Fatalf("授权文件应可下载: %d %q", w.Code, w.Body.String())
	}
	if err := RevokeGrant(g.ID); err != nil {
		t.Fatal(err)
	}
	if w := get(); w.Code != http.StatusNotFound {
		t.Fatalf("撤销后应 404: %d", w.Code)
	}
	// 未知 id 404
	wu := httptest.NewRecorder()
	s.handle(wu, httptest.NewRequest(http.MethodGet, "/g/nope123", nil))
	if wu.Code != http.StatusNotFound {
		t.Fatalf("未知授权 id 应 404: %d", wu.Code)
	}
}

func TestGrantDirectoryZipAndRecursive(t *testing.T) {
	s, _ := newTestServer(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(filepath.Join(sub, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(sub, "a.txt"), []byte("AAA"), 0o644)
	_ = os.WriteFile(filepath.Join(sub, "inner", "b.txt"), []byte("BB"), 0o644)
	if _, err := AddGrant(sub, true, 0); err != nil {
		t.Fatal(err)
	}
	g := findGrant(t, sub)

	// 目录整包 → 流式 zip，可被 zip.Reader 解析出各条目
	w := httptest.NewRecorder()
	s.handle(w, httptest.NewRequest(http.MethodGet, "/g/"+g.ID, nil))
	if w.Code != http.StatusOK {
		t.Fatalf("目录打包应 200: %d", w.Code)
	}
	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("打包结果不是合法 zip: %v", err)
	}
	got := map[string]string{}
	for _, f := range zr.File {
		rc, _ := f.Open()
		bs, _ := io.ReadAll(rc)
		rc.Close()
		got[f.Name] = string(bs)
	}
	if got["pkg/a.txt"] != "AAA" || got["pkg/inner/b.txt"] != "BB" {
		t.Fatalf("zip 条目不符: %v", got)
	}

	// 递归授权：逐项下载子文件
	wf := httptest.NewRecorder()
	s.handle(wf, httptest.NewRequest(http.MethodGet, "/g/"+g.ID+"/inner/b.txt", nil))
	if wf.Code != http.StatusOK || wf.Body.String() != "BB" {
		t.Fatalf("递归子项下载失败: %d %q", wf.Code, wf.Body.String())
	}
	// 目录穿越被拒（joinGrant 挡住 ../）
	wt := httptest.NewRecorder()
	s.handle(wt, httptest.NewRequest(http.MethodGet, "/g/"+g.ID+"/../../etc/passwd", nil))
	if wt.Code != http.StatusNotFound {
		t.Fatalf("穿越请求应 404: %d", wt.Code)
	}
	_ = RevokeGrant(g.ID)
}

func TestGrantNonRecursiveBlocksBrowse(t *testing.T) {
	s, _ := newTestServer(t)
	dir := t.TempDir()
	sub := filepath.Join(dir, "ro")
	_ = os.MkdirAll(sub, 0o755)
	_ = os.WriteFile(filepath.Join(sub, "x.txt"), []byte("X"), 0o644)
	if _, err := AddGrant(sub, false, 0); err != nil {
		t.Fatal(err)
	}
	g := findGrant(t, sub)
	// 非递归：整包可下，但逐项浏览被拒
	wsub := httptest.NewRecorder()
	s.handle(wsub, httptest.NewRequest(http.MethodGet, "/g/"+g.ID+"/x.txt", nil))
	if wsub.Code != http.StatusNotFound {
		t.Fatalf("非递归授权逐项浏览应 404: %d", wsub.Code)
	}
	wzip := httptest.NewRecorder()
	s.handle(wzip, httptest.NewRequest(http.MethodGet, "/g/"+g.ID, nil))
	if wzip.Code != http.StatusOK {
		t.Fatalf("非递归授权整包应 200: %d", wzip.Code)
	}
	_ = RevokeGrant(g.ID)
}

func TestGrantPinGate(t *testing.T) {
	s, _ := newTestServer(t)
	s.pin = "1234"
	dir := t.TempDir()
	target := filepath.Join(dir, "sec.txt")
	_ = os.WriteFile(target, []byte("SECRET"), 0o644)
	if _, err := AddGrant(target, false, 0); err != nil {
		t.Fatal(err)
	}
	g := findGrant(t, target)
	// 无 PIN → 401
	wno := httptest.NewRecorder()
	s.handle(wno, httptest.NewRequest(http.MethodGet, "/g/"+g.ID, nil))
	if wno.Code != http.StatusUnauthorized {
		t.Fatalf("缺 PIN 应 401: %d", wno.Code)
	}
	// 错 PIN → 401
	wbad := httptest.NewRecorder()
	s.handle(wbad, httptest.NewRequest(http.MethodGet, "/g/"+g.ID+"?pin=0000", nil))
	if wbad.Code != http.StatusUnauthorized {
		t.Fatalf("错 PIN 应 401: %d", wbad.Code)
	}
	// 对 PIN → 200
	wok := httptest.NewRecorder()
	s.handle(wok, httptest.NewRequest(http.MethodGet, "/g/"+g.ID+"?pin=1234", nil))
	if wok.Code != http.StatusOK || wok.Body.String() != "SECRET" {
		t.Fatalf("对 PIN 应放行: %d %q", wok.Code, wok.Body.String())
	}
	_ = RevokeGrant(g.ID)
}

func TestGrantExpired(t *testing.T) {
	now := time.Now()
	dead := Grant{ID: "d", Path: t.TempDir(), ExpiryAt: now.Add(-time.Hour).UnixMilli()}
	if dead.alive(now) {
		t.Fatal("已过期授权不应可用")
	}
	ok := Grant{ID: "o", Path: t.TempDir()}
	if !ok.alive(now) {
		t.Fatal("未过期长期授权应可用")
	}
}

// TestPinPortalGate 验证设了 PIN 后首页真正被拦：未授权回 PIN 输入页，
// 输入正确 PIN 后落 cookie 并 302，带 cookie 再访问才放行。
func TestPinPortalGate(t *testing.T) {
	s, _ := newTestServer(t)
	s.pin = "1234"

	// 未授权访问首页 → 401 + PIN 输入页
	w := httptest.NewRecorder()
	s.handle(w, httptest.NewRequest(http.MethodGet, "/", nil))
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "访问需要 PIN") {
		t.Fatalf("未授权访问应回 PIN 页: %d %s", w.Code, w.Body.String())
	}

	// 提交错误 PIN → 仍 200 表单，不下发 cookie
	form := url.Values{"pin": {"0000"}, "next": {"/"}}
	req := httptest.NewRequest(http.MethodPost, "/pin", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	s.handle(w, req)
	if w.Code != http.StatusOK || len(w.Result().Cookies()) != 0 {
		t.Fatalf("错误 PIN 不应放行: %d cookie=%d", w.Code, len(w.Result().Cookies()))
	}

	// 提交正确 PIN → 302 + cookie
	form = url.Values{"pin": {"1234"}, "next": {"/"}}
	req = httptest.NewRequest(http.MethodPost, "/pin", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	s.handle(w, req)
	if w.Code != http.StatusFound || len(w.Result().Cookies()) == 0 {
		t.Fatalf("正确 PIN 应 302 并落 cookie: %d", w.Code)
	}
	ck := w.Result().Cookies()[0]

	// 带 cookie 再访问首页 → 200
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: ck.Name, Value: ck.Value})
	w = httptest.NewRecorder()
	s.handle(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "上传到这台机器") {
		t.Fatalf("带 cookie 应放行首页: %d", w.Code)
	}
}

// TestSafeNextTarget 验证登录后跳转目标只允许站内绝对路径，挡协议相对开放重定向。
func TestSafeNextTarget(t *testing.T) {
	cases := map[string]string{
		"/s/abc":     "/s/abc",
		"/":          "/",
		"//evil.com": "/",
		`/\evil`:     "/",
		"http://x/y": "/",
		"":           "/",
	}
	for in, want := range cases {
		if got := safeNextTarget(in); got != want {
			t.Fatalf("safeNextTarget(%q)=%q 期望 %q", in, got, want)
		}
	}
}
