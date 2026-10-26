package ftp

import "testing"

func TestCleanRemote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "/"},
		{"/", "/"},
		{"   ", "/"},
		{"www", "/www"},
		{"/var/www", "/var/www"},
		{`\var\www`, "/var/www"},
		{"/var/./www/../log", "/var/log"},
		{"../../etc", "/etc"},
	}
	for _, c := range cases {
		if got := cleanRemote(c.in); got != c.want {
			t.Errorf("cleanRemote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestRemoteParts(t *testing.T) {
	parts := remoteParts("/var/www")
	if len(parts) != 3 {
		t.Fatalf("remoteParts 长度 = %d, want 3", len(parts))
	}
	if parts[0].Path != "/" || parts[1].Name != "var" || parts[2].Path != "/var/www" {
		t.Errorf("remoteParts(/var/www) = %+v", parts)
	}

	root := remoteParts("/")
	if len(root) != 1 || root[0].Path != "/" {
		t.Errorf("remoteParts(/) = %+v", root)
	}
}

func TestValidateName(t *testing.T) {
	ok := []string{"a.txt", "目录", "a b", ".env"}
	for _, name := range ok {
		if err := validateName(name); err != nil {
			t.Errorf("validateName(%q) 不该报错: %v", name, err)
		}
	}
	bad := []string{"", "  ", ".", "..", "a/b", `a\b`, "a\x00b"}
	for _, name := range bad {
		if err := validateName(name); err == nil {
			t.Errorf("validateName(%q) 应该报错", name)
		}
	}
}

func TestRelRemote(t *testing.T) {
	cases := []struct{ root, p, want string }{
		{"/var/www", "/var/www", "."},
		{"/var/www", "/var/www/a.txt", "a.txt"},
		{"/var/www", "/var/www/sub/a.txt", "sub/a.txt"},
	}
	for _, c := range cases {
		got, err := relRemote(c.root, c.p)
		if err != nil || got != c.want {
			t.Errorf("relRemote(%q,%q) = %q,%v want %q", c.root, c.p, got, err, c.want)
		}
	}
	if _, err := relRemote("/var/www", "/var/www2/a.txt"); err == nil {
		t.Error("relRemote 应拒绝前缀相同但不同目录的路径")
	}
}

func TestTaskLifecycle(t *testing.T) {
	task := newTask("s1", DirUp, "a.txt", `C:\tmp\a.txt`, "/tmp/a.txt", 100, false)
	if snap := task.snapshot(); snap.State != StateQueued || snap.Total != 100 {
		t.Fatalf("新任务状态不对: %+v", snap)
	}
	task.addDone(40)
	task.setState(StateRunning)
	task.addDone(60)
	task.finish(StateDone, "")
	snap := task.snapshot()
	if snap.State != StateDone || snap.Done != snap.Total || snap.Ended == 0 {
		t.Fatalf("完成后状态不对: %+v", snap)
	}
}
