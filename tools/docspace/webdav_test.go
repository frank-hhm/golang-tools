package docspace

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"Tools/services/sysx"
)

func TestWebDAVParseValidation(t *testing.T) {
	// 合法 http(s)
	if _, err := Parse(SrcWebDAV, "", "http://host/a.txt"); err != nil {
		t.Fatalf("合法 webdav URL 应通过: %v", err)
	}
	if _, err := Parse(SrcWebDAV, "", "https://host/a.txt"); err != nil {
		t.Fatalf("https 应通过: %v", err)
	}
	// 缺 URL / 非法 scheme / 缺主机
	for _, bad := range []string{"", "ftp://host/a", "http:///a.txt", "/just/a/path"} {
		if _, err := Parse(SrcWebDAV, "", bad); err == nil {
			t.Fatalf("非法 webdav URL 应被拒: %q", bad)
		}
	}
	// cloud 仍不支持
	if _, err := Parse(SrcCloud, "", "x"); err == nil {
		t.Fatal("cloud 应返回不支持")
	}
}

func TestWebDAVStringRedactsSecret(t *testing.T) {
	r := Ref{Src: SrcWebDAV, Path: "https://bob:secretpw@host/dir/file.txt"}
	got := r.String()
	if strings.Contains(got, "secretpw") {
		t.Fatalf("String 不应泄露口令: %s", got)
	}
	if !strings.Contains(got, "bob") {
		t.Fatalf("应保留用户名便于识别: %s", got)
	}
}

func TestWebDAVReadAndRange(t *testing.T) {
	body := "hello webdav world"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/file.txt" {
			http.ServeContent(w, req, "file.txt", time.Time{}, strings.NewReader(body))
			return
		}
		http.NotFound(w, req)
	}))
	defer srv.Close()

	r, err := Parse(SrcWebDAV, "", srv.URL+"/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	// 整体读取
	all, err := r.ReadAll(0)
	if err != nil {
		t.Fatal(err)
	}
	if string(all) != body {
		t.Fatalf("整体读取内容不对: %q", string(all))
	}
	// Stat 大小
	st, err := r.Stat()
	if err != nil || st.Size() != int64(len(body)) {
		t.Fatalf("Stat 不对: %v size=%v", err, st)
	}
	// ReaderAt 取子区间（Range 依赖）
	f, err := r.OpenRead()
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	buf := make([]byte, 6)
	if _, err := f.ReadAt(buf, 6); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "webdav" {
		t.Fatalf("ReadAt 结果不对: %q", string(buf))
	}
}

func TestWebDAVMissingAndWriteUnsupported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.NotFound(w, req)
	}))
	defer srv.Close()

	r, _ := Parse(SrcWebDAV, "", srv.URL+"/gone.txt")
	if _, err := r.OpenRead(); err == nil {
		t.Fatal("404 应报错")
	}
	// 写回不支持
	w := Ref{Src: SrcWebDAV, Path: srv.URL + "/x"}
	if _, err := w.open(os.O_RDWR); err == nil {
		t.Fatal("webdav 写回应返回不支持")
	} else if !strings.Contains(err.Error(), "只读") && !strings.Contains(err.Error(), sysx.ErrUnsupported.Error()) {
		t.Fatalf("写回错误应表明不支持: %v", err)
	}
}

func TestWebDAVBasicAuth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		u, p, ok := req.BasicAuth()
		if !ok || u != "alice" || p != "wonder" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("secret-bytes"))
	}))
	defer srv.Close()

	// 不带凭证 → 401 报错
	if _, err := Parse(SrcWebDAV, "", srv.URL+"/f"); err != nil {
		t.Fatal(err)
	}
	if _, err := (&Ref{Src: SrcWebDAV, Path: srv.URL + "/f"}).ReadAll(0); err == nil {
		t.Fatal("无凭证应失败")
	}
	// 带 user:pass → 成功
	withAuth := srv.URL + "/f"
	withAuth = strings.Replace(withAuth, "://", "://alice:wonder@", 1)
	data, err := (&Ref{Src: SrcWebDAV, Path: withAuth}).ReadAll(0)
	if err != nil {
		t.Fatalf("带凭证应成功: %v", err)
	}
	if string(data) != "secret-bytes" {
		t.Fatalf("内容不对: %q", string(data))
	}
}
