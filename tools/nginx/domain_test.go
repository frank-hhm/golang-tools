package nginx

import "testing"

func TestNormalizeDomains(t *testing.T) {
	cases := []struct {
		in    string
		want  string
		wantE bool
	}{
		{"", "", false},
		{"example.com", "example.com", false},
		{"Example.COM", "example.com", false},
		{"a.com, b.com;a.com", "a.com b.com", false}, // 去重 + 逗号/分号分隔
		{"localhost 127.0.0.1", "", false},           // 保留字丢弃
		{"*.example.com", "*.example.com", false},    // 泛域名
		{"bad name!", "", true},                      // 非法字符
		{"-lead.com", "", true},                      // 以连字符起始
	}
	for _, c := range cases {
		got, err := normalizeDomains(c.in)
		if c.wantE {
			if err == nil {
				t.Errorf("normalizeDomains(%q) 期望报错，实得 %q", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("normalizeDomains(%q) 意外报错: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("normalizeDomains(%q) = %q, 期望 %q", c.in, got, c.want)
		}
	}
}

func TestServerNameOf(t *testing.T) {
	if got := serverNameOf(""); got != "localhost" {
		t.Errorf("空域名应为 localhost，实得 %q", got)
	}
	if got := serverNameOf("example.com"); got != "example.com localhost" {
		t.Errorf("绑定域名应保留 localhost 兜底，实得 %q", got)
	}
}
