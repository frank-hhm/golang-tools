package tasks

import (
	"testing"
	"time"
)

// resetForTest 清空登记表与注入的回调，避免用例之间互相污染
func resetForTest(t *testing.T) {
	t.Helper()
	mu.Lock()
	items = map[string]*Task{}
	emitter = nil
	notifier = nil
	mu.Unlock()
}

// installEvent 造一条与 tools/* 的 InstallProgress 同形的负载
type installEvent struct {
	Version string
	Stage   string
	Percent int
	Note    string
}

func TestObserveMergesIntoOneTask(t *testing.T) {
	resetForTest(t)

	var events []Task
	SetEmitter(func(name string, data ...any) {
		if name != UpdateEvent || len(data) != 1 {
			t.Fatalf("增量事件形状不对: %s %v", name, data)
		}
		events = append(events, data[0].(Task))
	})

	Observe("mysql:install", installEvent{Version: "8.4.6", Stage: "info", Note: "开始下载"})
	Observe("mysql:install", installEvent{Version: "8.4.6", Stage: "downloading", Percent: 40, Note: "40%"})
	Observe("mysql:install", installEvent{Version: "8.4.6", Stage: "downloading", Percent: 30, Note: "重发"})
	Observe("mysql:install", installEvent{Version: "8.4.6", Stage: "done", Note: "安装完成"})

	list := Snapshot()
	if len(list) != 1 {
		t.Fatalf("同一版本应合并成一条任务，实际 %d 条", len(list))
	}
	got := list[0]
	if got.ID != "mysql|install|8.4.6" || got.Tool != "mysql" || got.Title != "MySQL 8.4.6" {
		t.Fatalf("任务标识错误: %+v", got)
	}
	if got.Status != StatusDone || got.Percent != 100 || got.EndedAt == 0 {
		t.Fatalf("终态处理错误: %+v", got)
	}
	if len(events) != 4 {
		t.Fatalf("每条事件都应推一次增量，实际 %d", len(events))
	}
	// 百分比只涨不跌：重发的 30% 不能把 40% 打回去
	if events[2].Percent != 40 {
		t.Fatalf("百分比回退: %d", events[2].Percent)
	}
	if len(got.Logs) == 0 || got.Logs[len(got.Logs)-1] != "安装完成" {
		t.Fatalf("完成说明未进日志: %#v", got.Logs)
	}
}

func TestNotifyOnlyOncePerTerminal(t *testing.T) {
	resetForTest(t)

	notified := make(chan Task, 4)
	SetNotifier(func(t Task) { notified <- t })

	Observe("redis:install", installEvent{Version: "7.4.1", Stage: "downloading", Percent: 10})
	select {
	case tk := <-notified:
		t.Fatalf("进行中不该通知: %+v", tk)
	default:
	}

	Observe("redis:install", installEvent{Version: "7.4.1", Stage: "done"})
	Observe("redis:install", installEvent{Version: "7.4.1", Stage: "done"})

	// 通知在独立 goroutine 里发，要等它到再断言
	var first Task
	select {
	case first = <-notified:
	case <-time.After(2 * time.Second):
		t.Fatal("完成通知超时未发出")
	}
	if first.Status != StatusDone || first.Title != "Redis 7.4.1" {
		t.Fatalf("通知内容错误: %+v", first)
	}
	// 重复的 done 不能再弹一次
	select {
	case dup := <-notified:
		t.Fatalf("同一终态重复通知: %+v", dup)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestRetryResetsProgress(t *testing.T) {
	resetForTest(t)

	Observe("php:install", installEvent{Version: "8.3.36", Stage: "downloading", Percent: 80, Note: "80%"})
	Observe("php:install", installEvent{Version: "8.3.36", Stage: "error", Note: "网络中断"})

	failed := Snapshot()[0]
	if failed.Status != StatusError || failed.Note != "网络中断" {
		t.Fatalf("失败态错误: %+v", failed)
	}

	Observe("php:install", installEvent{Version: "8.3.36", Stage: "info", Note: "重试"})
	again := Snapshot()[0]
	if again.Status != StatusRunning || again.Percent != 0 || again.EndedAt != 0 {
		t.Fatalf("重试应从 0 重新开始: %+v", again)
	}
	if again.StartedAt < failed.StartedAt {
		t.Fatalf("重试后开始时间应更新: %d < %d", again.StartedAt, failed.StartedAt)
	}
}

func TestDockerEventsShape(t *testing.T) {
	resetForTest(t)

	// docker:setup 没有版本号；docker:pull 只有 overall；imageop 靠 op 区分动词
	Observe("docker:setup", map[string]any{"stage": "downloading", "percent": 55, "note": "下载安装包"})
	Observe("docker:pull", map[string]any{"image": "mysql:8.4", "stage": "downloading", "overall": 42, "status": "Downloading"})
	Observe("docker:imageop", map[string]any{"op": "push", "image": "app:1.0", "stage": "downloading", "percent": 20})

	byID := map[string]Task{}
	for _, tk := range Snapshot() {
		byID[tk.ID] = tk
	}
	if tk, ok := byID["docker|setup|engine"]; !ok || tk.Title != "Docker 引擎" || tk.Percent != 55 {
		t.Fatalf("引擎安装登记错误: %+v", byID)
	}
	if tk, ok := byID["docker|pull|mysql:8.4"]; !ok || tk.Percent != 42 {
		t.Fatalf("拉取任务应吃 overall 百分比: %+v", byID)
	}
	if tk, ok := byID["docker|op|app:1.0"]; !ok || tk.Title != "推送 app:1.0" {
		t.Fatalf("镜像操作标题错误: %+v", byID)
	}
}

func TestClearKeepsRunning(t *testing.T) {
	resetForTest(t)

	Observe("nginx:install", installEvent{Version: "1.29.1", Stage: "downloading", Percent: 5})
	Observe("nginx:install", installEvent{Version: "1.28.0", Stage: "done"})

	Clear("nginx|install|1.29.1") // 进行中的删不掉
	if len(Snapshot()) != 2 {
		t.Fatalf("进行中的任务不应被清掉")
	}
	Clear("nginx|install|1.28.0")
	if n := len(Snapshot()); n != 1 {
		t.Fatalf("已结束的任务应可单独清理，剩 %d 条", n)
	}
	if Snapshot()[0].Status != StatusRunning {
		t.Fatalf("清错对象了: %+v", Snapshot()[0])
	}
}

func TestSnapshotOrdersRunningFirst(t *testing.T) {
	resetForTest(t)

	Observe("mysql:install", installEvent{Version: "8.0.40", Stage: "done"})
	Observe("mysql:install", installEvent{Version: "8.4.6", Stage: "downloading", Percent: 12})

	list := Snapshot()
	if len(list) != 2 || list[0].Name != "8.4.6" {
		t.Fatalf("进行中的任务应排在最前: %+v", list)
	}
}

func TestTrimNote(t *testing.T) {
	if got := trimNote("a\n\nb\t c"); got != "a b c" {
		t.Fatalf("多行说明未压平: %q", got)
	}
	long := make([]rune, 400)
	for i := range long {
		long[i] = 'x'
	}
	if n := len([]rune(trimNote(string(long)))); n != 301 {
		t.Fatalf("超长说明应截断到 300+省略号，实际 %d", n)
	}
}
