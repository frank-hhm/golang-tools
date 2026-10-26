package srvstate

import "testing"

func TestFlagAbnormal(t *testing.T) {
	cases := []struct {
		name     string
		ready    bool
		stopping bool
		want     bool
	}{
		{"启动期退出不通知", false, false, false},
		{"就绪后崩溃要通知", true, false, true},
		{"用户主动停止不通知", true, true, false},
		{"未就绪时主动停止也不通知", false, true, false},
	}
	for _, c := range cases {
		var f Flag
		if c.ready {
			f.MarkReady()
		}
		if c.stopping {
			f.MarkStopping()
		}
		if got := f.Abnormal(); got != c.want {
			t.Errorf("%s: Abnormal() = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestEventNotice(t *testing.T) {
	ev := NewEvent("redis", "local", "inst-1", "测速", 6379, 1)
	if ev.Title() != "Redis 异常退出" {
		t.Errorf("Title() = %q", ev.Title())
	}
	want := "测速（端口 6379）已退出，退出码 1"
	if ev.Content() != want {
		t.Errorf("Content() = %q, want %q", ev.Content(), want)
	}

	// 退出码未知：不拼「退出码 -1」
	docker := NewEvent("mysql", "docker", "inst-2", "", 3306, -1)
	if got := docker.Content(); got != "inst-2（端口 3306）已退出" {
		t.Errorf("docker Content() = %q", got)
	}
	if ev.At <= 0 {
		t.Errorf("At 未填写")
	}
}

func TestToolName(t *testing.T) {
	for tool, want := range map[string]string{
		"redis": "Redis", "mysql": "MySQL", "php": "PHP", "nginx": "Nginx", "other": "other",
	} {
		if got := ToolName(tool); got != want {
			t.Errorf("ToolName(%q) = %q, want %q", tool, got, want)
		}
	}
}

func TestTaskTitles(t *testing.T) {
	done := map[string]string{
		"install": "Redis 安装完成",
		"ext":     "PHP 安装完成",
		"pull":    "Docker 下载完成",
		"op":      "Docker 操作完成",
		"npmver":  "Node/npm 任务完成",
	}
	for kind, want := range done {
		tool := "redis"
		switch kind {
		case "ext":
			tool = "php"
		case "pull", "op":
			tool = "docker"
		case "npmver":
			tool = "npm"
		}
		if got := DoneTitle(tool, kind); got != want {
			t.Errorf("DoneTitle(%q,%q) = %q, want %q", tool, kind, got, want)
		}
	}
	if got := FailTitle("php", "ext"); got != "PHP 安装失败" {
		t.Errorf("FailTitle(php,ext) = %q", got)
	}
	if got := FailTitle("nginx", "unknown"); got != "Nginx 任务失败" {
		t.Errorf("FailTitle(nginx,unknown) = %q", got)
	}
}

func TestClipNotice(t *testing.T) {
	if got := ClipNotice("启动失败\n详见日志\r\n尾部", 0); got != "启动失败 详见日志 尾部" {
		t.Errorf("换行未压成一行: %q", got)
	}
	if got := ClipNotice("一二三四五", 3); got != "一二三…" {
		t.Errorf("截断 = %q", got)
	}
	if got := ClipNotice(" 短 ", 10); got != "短" {
		t.Errorf("TrimSpace = %q", got)
	}
}
