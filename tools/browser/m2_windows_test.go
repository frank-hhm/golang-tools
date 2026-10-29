//go:build windows

package browser

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// waitEvent 等一个指定 kind 的事件（不把 error 事件当致命错误，
// 因为多标签场景里偶尔会有「导航被打断」这类无害上报）。
func waitEvent(t *testing.T, events <-chan Event, kind string, timeout time.Duration) Event {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pumpOnce()
		for {
			select {
			case ev := <-events:
				if ev.Kind == kind {
					return ev
				}
				if ev.Kind == "crash" {
					t.Fatalf("网页区崩溃: %s", ev.Message)
				}
			default:
				goto drained
			}
		}
	drained:
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待事件 %q 超时", kind)
	return Event{}
}

// waitTabTitle 等某个标签报到指定标题。
func waitTabTitle(t *testing.T, events <-chan Event, tabID, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var seen []string
	for time.Now().Before(deadline) {
		pumpOnce()
		for {
			select {
			case ev := <-events:
				if len(seen) < 40 {
					seen = append(seen, ev.Kind+"/"+ev.TabID+"/"+ev.Title+ev.Message)
				}
				if ev.Kind == "title" && ev.TabID == tabID && ev.Title == want {
					return
				}
			default:
				goto drained
			}
		}
	drained:
		time.Sleep(5 * time.Millisecond)
	}
	t.Logf("已收到的相关事件: %v", seen)
	t.Fatalf("等待标签 %s 标题 %q 超时", tabID, want)
}

// waitCond 一边抽消息一边等条件成立。
func waitCond(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pumpOnce()
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等待条件超时: %s", what)
}

// TestMultiTabAndWindowOpen 覆盖 M2 的多标签链路：
// 新建/切换/关闭标签、标签数上限、页面 window.open 转内嵌新标签。
func TestMultiTabAndWindowOpen(t *testing.T) {
	mux := http.NewServeMux()
	page := func(title, link string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			body := "<!doctype html><html><head><title>" + title + "</title></head><body>" + link + "</body></html>"
			_, _ = io.WriteString(w, body)
		}
	}
	mux.HandleFunc("/", page("TAB-ONE", ""))
	mux.HandleFunc("/two", page("TAB-TWO", ""))
	mux.HandleFunc("/popup", page("TAB-POPUP", ""))
	mux.HandleFunc("/blank", page("TAB-LINK", `<a id="pop" href="/popup" target="_blank">open</a>`))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	hwnd := createTestWindow(t, "Tools browser probe m2 tabs")
	defer procDestroyWindow.Call(hwnd)

	m := NewManager()
	events := make(chan Event, 512)
	m.OnEvent = func(ev Event) {
		select {
		case events <- ev:
		default:
		}
	}
	// 测试结束先收摊再关 httptest：否则未关的标签会挂着连接，srv.Close 会阻塞很久
	defer func() { m.CloseAll(); pumpFor(2 * time.Second) }()

	openSpace(t, m, Options{
		ID:       "m2",
		DataPath: tempProfileDir(t),
		HWND:     hwnd,
		Bounds:   Rect{X: 0, Y: 0, W: 900, H: 600},
		URL:      srv.URL,
		MaxTabs:  3,
	})
	st, err := m.SpaceState("m2")
	if err != nil {
		t.Fatalf("SpaceState: %v", err)
	}
	if len(st.Tabs) != 1 {
		t.Fatalf("打开空间后应只有 1 个标签，实际 %d", len(st.Tabs))
	}
	first := st.Tabs[0].ID
	waitTabTitle(t, events, first, "TAB-ONE", 40*time.Second)

	// 新建第二个标签并立即激活：两个 controller 共用同一份 environment
	id2, err := m.NewTab("m2", srv.URL+"/two", true)
	if err != nil {
		t.Fatalf("NewTab: %v", err)
	}
	waitTabTitle(t, events, id2, "TAB-TWO", 40*time.Second)
	waitCond(t, 5*time.Second, "活动标签切到第二个", func() bool {
		st, err := m.SpaceState("m2")
		return err == nil && len(st.Tabs) == 2 && st.ActiveTab == id2
	})

	// 切回第一个标签
	if err := m.ActivateTab("m2", first); err != nil {
		t.Fatalf("ActivateTab: %v", err)
	}
	waitCond(t, 5*time.Second, "活动标签切回第一个", func() bool {
		st, _ := m.SpaceState("m2")
		return st.ActiveTab == first
	})

	// 页面 window.open：应被拦下来变成内嵌新标签，而不是弹系统窗口
	if err := m.NavigateTab("m2", first, srv.URL+"/blank"); err != nil {
		t.Fatalf("NavigateTab: %v", err)
	}
	waitTabTitle(t, events, first, "TAB-LINK", 20*time.Second)
	if err := m.Eval("m2", `window.open("/popup", "_blank")`); err != nil {
		t.Fatalf("Eval: %v", err)
	}
	ev := waitEvent(t, events, "newWindow", 20*time.Second)
	if ev.URL == "" || ev.TabID == "" {
		t.Fatalf("newWindow 事件缺字段: %+v", ev)
	}
	waitTabTitle(t, events, ev.TabID, "TAB-POPUP", 20*time.Second)
	st, _ = m.SpaceState("m2")
	if len(st.Tabs) != 3 {
		t.Fatalf("window.open 之后应有 3 个标签，实际 %d", len(st.Tabs))
	}

	// 关掉 window.open 出来的那个，剩下 2 个
	if err := m.CloseTab("m2", ev.TabID); err != nil {
		t.Fatalf("CloseTab: %v", err)
	}
	waitCond(t, 5*time.Second, "标签数回到 2", func() bool {
		st, _ := m.SpaceState("m2")
		return len(st.Tabs) == 2
	})

	// 关掉标签之后，同一个空间仍然能正常导航
	if err := m.NavigateTab("m2", first, srv.URL); err != nil {
		t.Fatalf("NavigateTab: %v", err)
	}
	waitTabTitle(t, events, first, "TAB-ONE", 20*time.Second)

	// 标签上限：MaxTabs=3，已经 2 个，再开一个到 3，第 4 个必须被拒
	if _, err := m.NewTab("m2", srv.URL+"/two", false); err != nil {
		t.Fatalf("第 3 个标签应当允许: %v", err)
	}
	if _, err := m.NewTab("m2", srv.URL, false); err == nil {
		t.Fatalf("超过 MaxTabs 应当报错")
	}
	st, _ = m.SpaceState("m2")
	if len(st.Tabs) != 3 {
		t.Fatalf("应有 3 个标签，实际 %d", len(st.Tabs))
	}

	// 关最后一个标签：应自动补一个首页标签，而不是留下空壳
	for {
		st, _ = m.SpaceState("m2")
		if len(st.Tabs) <= 1 {
			break
		}
		if err := m.CloseTab("m2", st.Tabs[0].ID); err != nil {
			t.Fatalf("CloseTab: %v", err)
		}
		pumpFor(300 * time.Millisecond)
	}
	if err := m.CloseTab("m2", st.Tabs[0].ID); err != nil {
		t.Fatalf("关最后一个标签: %v", err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		pumpOnce()
		if st, err := m.SpaceState("m2"); err == nil && len(st.Tabs) == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	st, _ = m.SpaceState("m2")
	if len(st.Tabs) != 1 {
		t.Fatalf("关光之后应自动补一个标签，实际 %d", len(st.Tabs))
	}

	m.CloseAll()
	pumpFor(2 * time.Second)
	if st := m.Stats(); st.Spaces != 0 {
		t.Fatalf("CloseAll 后空间数应为 0，实际 %d", st.Spaces)
	}
}

// TestIdleTabAutoSuspend 验证「非活动标签自动挂起」：
// 把空闲阈值调小，两个标签切来切去之后，闲置的那个应被挂起。
func TestIdleTabAutoSuspend(t *testing.T) {
	old := idleSuspendAfter
	idleSuspendAfter = 500 * time.Millisecond
	defer func() { idleSuspendAfter = old }()

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><head><title>SUSP</title></head><body>s</body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	hwnd := createTestWindow(t, "Tools browser probe m2 suspend")
	defer procDestroyWindow.Call(hwnd)

	m := NewManager()
	events := make(chan Event, 512)
	m.OnEvent = func(ev Event) {
		select {
		case events <- ev:
		default:
		}
	}

	openSpace(t, m, Options{
		ID:       "sus",
		DataPath: tempProfileDir(t),
		HWND:     hwnd,
		Bounds:   Rect{X: 0, Y: 0, W: 800, H: 500},
		URL:      srv.URL,
	})
	st, _ := m.SpaceState("sus")
	first := st.Tabs[0].ID
	waitTabTitle(t, events, first, "SUSP", 40*time.Second)

	id2, err := m.NewTab("sus", srv.URL, true)
	if err != nil {
		t.Fatalf("NewTab: %v", err)
	}
	waitTabTitle(t, events, id2, "SUSP", 40*time.Second)

	// 阈值 500ms，非活动的第一个标签应很快被挂起
	waitCond(t, 20*time.Second, "非活动标签被挂起", func() bool {
		st, err := m.SpaceState("sus")
		if err != nil {
			return false
		}
		for _, tb := range st.Tabs {
			if tb.ID == first {
				return tb.Suspended
			}
		}
		return false
	})

	// 切回去应该能唤醒（页面状态不丢，标题还在）
	if err := m.ActivateTab("sus", first); err != nil {
		t.Fatalf("ActivateTab: %v", err)
	}
	waitCond(t, 10*time.Second, "切回后取消挂起", func() bool {
		st, _ := m.SpaceState("sus")
		for _, tb := range st.Tabs {
			if tb.ID == first {
				return !tb.Suspended
			}
		}
		return false
	})

	m.CloseAll()
	pumpFor(2 * time.Second)
}
