//go:build windows

package docker

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMirrorBaseURL(t *testing.T) {
	cases := map[string]string{
		"docker.m.daocloud.io":         "https://docker.m.daocloud.io",
		"https://docker.1ms.run/":      "https://docker.1ms.run",
		"http://10.0.0.5:5000":         "http://10.0.0.5:5000",
		"  https://x.example.com/v2  ": "https://x.example.com",
	}
	for in, want := range cases {
		got, err := mirrorBaseURL(in)
		if err != nil {
			t.Fatalf("mirrorBaseURL(%q) 意外报错: %v", in, err)
		}
		if got != want {
			t.Errorf("mirrorBaseURL(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"", "   ", "https://x.example.com/some/path"} {
		if _, err := mirrorBaseURL(bad); err == nil {
			t.Errorf("mirrorBaseURL(%q) 应当报错", bad)
		}
	}
}

func TestMirrorRepoTagUsesDaemonPath(t *testing.T) {
	// 加速站只代理 docker.io：单段名补 library/，前面写的仓库主机名要摘掉
	cases := []struct{ in, repo, tag string }{
		{"redis", "library/redis", "latest"},
		{"redis:7.2", "library/redis", "7.2"},
		{"library/redis:7.2", "library/redis", "7.2"},
		{"bitnami/redis", "bitnami/redis", "latest"},
		{"docker.1ms.run/library/redis", "library/redis", "latest"},
		{"docker.1ms.run/redis:7.2", "library/redis", "7.2"},
		{"", "library/alpine", "latest"},
	}
	for _, c := range cases {
		repo, tag := mirrorRepoTag(c.in)
		if repo != c.repo || tag != c.tag {
			t.Errorf("mirrorRepoTag(%q) = %q/%q, want %q/%q", c.in, repo, tag, c.repo, c.tag)
		}
	}
}

func TestMirrorVerdictGrades(t *testing.T) {
	// 一个字节都不给：就是「拉取卡在 Downloading」那种站
	p := MirrorProbe{ConnectMs: 40, ManifestMs: 300, BlobSize: 3 << 20, TtfbMs: 10 << 3}
	mirrorVerdict(&p)
	if p.Grade != "empty" || !strings.Contains(p.Hint, "卡在 Downloading") {
		t.Errorf("零字节应判 empty，实得 %s / %q", p.Grade, p.Hint)
	}

	// 字节数很少（错误页、元数据）也不当作下载成功
	p2 := MirrorProbe{Bytes: 100, TtfbMs: 20, Mbps: 0.04}
	mirrorVerdict(&p2)
	if p2.Grade != "empty" {
		t.Errorf("100 字节应判 empty，实得 %s", p2.Grade)
	}

	// 拿到了数据但很慢
	p3 := MirrorProbe{Bytes: mirrorProbeRange, TtfbMs: 200, Mbps: 0.3}
	mirrorVerdict(&p3)
	if p3.Grade != "slow" {
		t.Errorf("0.3 Mbps 应判 slow，实得 %s", p3.Grade)
	}
	p4 := MirrorProbe{Bytes: mirrorProbeRange, TtfbMs: 9000, Mbps: 6}
	mirrorVerdict(&p4)
	if p4.Grade != "slow" {
		t.Errorf("首字节 9 秒应判 slow，实得 %s", p4.Grade)
	}

	// 正常
	p5 := MirrorProbe{Bytes: mirrorProbeRange, TtfbMs: 120, Mbps: 12}
	mirrorVerdict(&p5)
	if p5.Grade != "ok" || p5.Hint != "" {
		t.Errorf("12 Mbps 应判 ok，实得 %s / %q", p5.Grade, p5.Hint)
	}
}

func TestMirrorNetHintTranslates(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{errors.New(`dial tcp: lookup docker.1ms.run: no such host`), "域名解析不了"},
		{errors.New(`dial tcp 1.2.3.4:443: connectex: Connection refused`), "拒绝连接"},
		{errors.New(`x509: certificate signed by unknown authority`), "证书握手失败"},
		{errors.New(`context deadline exceeded`), "连接超时"},
		{errors.New(`read tcp: connection reset by peer`), "中途掐断"},
	}
	for _, c := range cases {
		if got := mirrorNetHint(c.err); !strings.Contains(got, c.want) {
			t.Errorf("mirrorNetHint(%q) = %q, 应含 %q", c.err, got, c.want)
		}
	}
}

func TestMirrorErrorHintPrefersHTTPCode(t *testing.T) {
	if got := mirrorErrorHint(errors.New("清单 HTTP 404 missing")); !strings.Contains(got, "取不到这个名字") {
		t.Errorf("404 应归因到名字/代理范围, 实得 %q", got)
	}
	if got := mirrorErrorHint(errors.New("context deadline exceeded")); !strings.Contains(got, "超时") {
		t.Errorf("非状态码错误应回落到网络归因, 实得 %q", got)
	}
}

func TestReadMirrorBodyCountsAndStops(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 200<<10) // 200KB < 取样上限
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	got, ttfb, err := readMirrorBody(ctx, bytes.NewReader(data))
	if err != nil {
		t.Fatalf("读取不应报错: %v", err)
	}
	if got != int64(len(data)) {
		t.Errorf("字节数 = %d, want %d", got, len(data))
	}
	if ttfb > time.Second {
		t.Errorf("首字节耗时不该这么大（内存读者），实得 %v", ttfb)
	}

	// 数据比取样上限多：只读够一段就收，不去下一整层
	big := bytes.Repeat([]byte("y"), mirrorProbeRange+64<<10)
	got2, _, err2 := readMirrorBody(ctx, bytes.NewReader(big))
	if err2 != nil {
		t.Fatalf("读取不应报错: %v", err2)
	}
	if got2 < int64(mirrorProbeRange) {
		t.Errorf("应读满取样段 %d, 实得 %d", mirrorProbeRange, got2)
	}

	// 截止时间已过：读到一半的断流算正常收工，不当失败
	dead, cancel2 := context.WithCancel(context.Background())
	cancel2()
	got3, _, err3 := readMirrorBody(dead, errReader{})
	if err3 != nil {
		t.Errorf("超时后的读断应算完成, 实得 %v", err3)
	}
	if got3 != 10 {
		t.Errorf("已读到的字节应保留, 实得 %d", got3)
	}
}

// errReader 先给 10 字节再报错：模拟“发了一点就断”的站点
type errReader struct{}

func (errReader) Read(p []byte) (int, error) {
	n := copy(p, []byte("0123456789"))
	return n, errors.New("unexpected EOF")
}

func TestShortDigest(t *testing.T) {
	if got := shortDigest("sha256:0bcc39d79065abf2c1"); got != "0bcc39d79065" {
		t.Errorf("shortDigest = %q", got)
	}
	if got := shortDigest("abc"); got != "abc" {
		t.Errorf("短串应原样返回, 实得 %q", got)
	}
}
