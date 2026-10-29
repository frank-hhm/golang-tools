//go:build windows

package browser

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var procPeekMessageW = user32.NewProc("PeekMessageW")

const (
	wsOverlappedWindow = 0x00CF0000
	pmRemove           = 0x0001
)

// createTestWindow 建一个最小宿主窗口：网页区只需要一个真实 HWND，
// 这里用系统自带的 STATIC 类，省掉注册窗口类的样板代码。
//
// 注意：这个窗口由测试线程创建，所以测试线程必须持续抽消息——它扮演的正是
// 真实运行时里 Wails 主线程的角色。宿主子窗口挂在它下面时，Win32 会向这个
// 线程同步发消息，这里不抽就会把空间线程卡死（第一版就是这么踩坑的）。
func createTestWindow(t *testing.T, title string) uintptr {
	t.Helper()
	cls, err := syscall.UTF16PtrFromString("STATIC")
	if err != nil {
		t.Fatalf("类名字符串转换失败: %v", err)
	}
	ttl, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		t.Fatalf("标题字符串转换失败: %v", err)
	}
	h, _, callErr := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(cls)),
		uintptr(unsafe.Pointer(ttl)),
		wsOverlappedWindow|wsVisible,
		120, 120, 960, 720,
		0, 0, 0, 0,
	)
	if h == 0 {
		t.Fatalf("CreateWindowExW 失败: %v", callErr)
	}
	return h
}

// pumpOnce 抽一轮窗口消息（非阻塞）：翻译 + 分发，然后交给调用方 sleep。
func pumpOnce() {
	var m msg
	for {
		r, _, _ := procPeekMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0, pmRemove)
		if r == 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// waitTitle 一边抽消息一边等一条指定标题的 title 事件；遇到 crash/error 直接失败。
func waitTitle(t *testing.T, events <-chan Event, want string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		pumpOnce()
		for {
			select {
			case ev := <-events:
				switch ev.Kind {
				case "title":
					if ev.Title == want {
						return
					}
					t.Logf("收到非目标标题: %q", ev.Title)
				case "crash", "error":
					t.Fatalf("网页区上报 %s: %s", ev.Kind, ev.Message)
				}
			default:
				goto drained
			}
		}
	drained:
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("等待标题 %q 超时", want)
}

// TestFindWindowByTitle 验证"按标题找 HWND"这条链路：
// Wails 不暴露窗口句柄，宿主只能靠我们自己设定的唯一标题反查。
func TestFindWindowByTitle(t *testing.T) {
	const title = "Tools browser probe 7f3a"
	hwnd := createTestWindow(t, title)
	defer pumpOnce()
	defer procDestroyWindow.Call(hwnd)

	got, err := FindWindowByTitle(title)
	if err != nil {
		t.Fatalf("FindWindowByTitle: %v", err)
	}
	if got != hwnd {
		t.Fatalf("句柄不匹配: got=%#x want=%#x", got, hwnd)
	}
}

// TestSpaceEmbedAndNavigate 是本包的核心自测：
// 起 WebView2 环境 → 建宿主子窗口 → 嵌入 → 导航到本地 HTTP 服务 → 取回 document.title，
// 再改尺寸 + 二次导航，确认同一空间可以复用。
func TestSpaceEmbedAndNavigate(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><head><title>M1-OK</title></head><body><h1>hello</h1></body></html>`)
	})
	mux.HandleFunc("/two", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><head><title>M1-OK-2</title></head><body><h1>second</h1></body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	hwnd := createTestWindow(t, "Tools browser probe space")
	defer procDestroyWindow.Call(hwnd)

	m := NewManager()
	events := make(chan Event, 256)
	m.OnEvent = func(ev Event) {
		select {
		case events <- ev:
		default:
		}
	}

	// Open 放到另一个 goroutine，测试线程继续抽消息（角色等同于 Wails 主线程）
	start := time.Now()
	done := make(chan error, 1)
	go func() {
		done <- m.Open(Options{
			ID:       "itest",
			DataPath: tempProfileDir(t),
			HWND:     hwnd,
			Bounds:   Rect{X: 0, Y: 0, W: 960, H: 680},
			URL:      srv.URL,
		})
	}()

	var openErr error
	opened := false
	deadline := time.Now().Add(40 * time.Second)
	for !opened && time.Now().Before(deadline) {
		pumpOnce()
		select {
		case openErr = <-done:
			opened = true
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	if !opened {
		t.Fatalf("Open 超时")
	}
	if openErr != nil {
		t.Fatalf("Open: %v", openErr)
	}
	t.Logf("环境就绪耗时 %s", time.Since(start).Round(time.Millisecond))

	waitTitle(t, events, "M1-OK", 40*time.Second)
	t.Logf("首次导航完成，累计 %s", time.Since(start).Round(time.Millisecond))

	if err := m.SetBounds("itest", Rect{X: 8, Y: 8, W: 480, H: 360}); err != nil {
		t.Fatalf("SetBounds: %v", err)
	}
	if err := m.Navigate("itest", srv.URL+"/two"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}
	waitTitle(t, events, "M1-OK-2", 40*time.Second)

	// 基线测量：空间数 / WebView2 进程数 / 常驻内存
	st := m.Stats()
	t.Logf("资源基线: 空间=%d  webview2进程=%d  合计内存=%.1fMB",
		st.Spaces, st.Processes, float64(st.RSSBytes)/(1024*1024))
	if st.Processes == 0 {
		t.Fatalf("没有采集到 msedgewebview2.exe 进程，统计逻辑可能失效")
	}
	if st.Spaces != 1 {
		t.Fatalf("空间数异常: %d", st.Spaces)
	}

	m.CloseAll()
	pumpFor(2 * time.Second)
	_ = fmt.Sprintf("%v", st)
}

// pumpFor 在给定时长内持续抽消息（收尾用）。
func pumpFor(d time.Duration) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		pumpOnce()
		time.Sleep(5 * time.Millisecond)
	}
}

// tempProfileDir 建一个临时 profile 目录。
//
// 这里不能用 t.TempDir()：空间销毁后 WebView2 的浏览器进程还要过一会儿才真正退出，
// EBWebView 下的 BrowserMetrics 等文件在那一刻仍被占用，t.TempDir() 的自动清理
// 会因“Access is denied”报错并把整个测试判成失败（与功能无关）。所以改成自己建目录、
// 清理时尽力而为，删不掉就留给系统临时目录回收。
func tempProfileDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "Tools-browser-test-*")
	if err != nil {
		t.Fatalf("建临时 profile 目录失败: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// openSpace 在后台打开一个空间，同时由测试线程继续抽消息
// （角色等同于真实运行时里被 WebView2 同步发消息的 Wails 主线程）。
func openSpace(t *testing.T, m *Manager, opts Options) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- m.Open(opts) }()
	deadline := time.Now().Add(40 * time.Second)
	for time.Now().Before(deadline) {
		pumpOnce()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("Open(%s): %v", opts.ID, err)
			}
			return
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
	t.Fatalf("Open(%s) 超时", opts.ID)
}

// TestSpaceEvalDevToolsAndMultiSpace 覆盖 M1 剩下几条交付链路：
// 脚本执行（Eval）、开发者工具、多空间并存、以及单个空间关闭不误伤其他空间。
func TestSpaceEvalDevToolsAndMultiSpace(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><head><title>EVAL-A</title></head><body>a</body></html>`)
	})
	mux.HandleFunc("/b", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><html><head><title>EVAL-B</title></head><body>b</body></html>`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	hwnd := createTestWindow(t, "Tools browser probe eval")
	defer procDestroyWindow.Call(hwnd)

	m := NewManager()
	events := make(chan Event, 256)
	m.OnEvent = func(ev Event) {
		select {
		case events <- ev:
		default:
		}
	}

	openSpace(t, m, Options{
		ID:       "a",
		DataPath: tempProfileDir(t),
		HWND:     hwnd,
		Bounds:   Rect{X: 0, Y: 0, W: 900, H: 600},
		URL:      srv.URL,
	})
	waitTitle(t, events, "EVAL-A", 40*time.Second)

	// 脚本执行：让脚本主动走我们自己注入的 title 上报通道，把执行结果带回来
	if err := m.Eval("a", `window.chrome.webview.postMessage("Tools:title:EVAL-OK:" + (1 + 1))`); err != nil {
		t.Fatalf("Eval: %v", err)
	}
	waitTitle(t, events, "EVAL-OK:2", 20*time.Second)

	// 开发者工具只要求不报错（真实运行时会弹一个独立调试窗口）
	if err := m.DevTools("a"); err != nil {
		t.Fatalf("DevTools: %v", err)
	}

	// 第二个空间：独立 profile / 独立 environment，与空间 a 并存
	openSpace(t, m, Options{
		ID:       "b",
		DataPath: tempProfileDir(t),
		HWND:     hwnd,
		Bounds:   Rect{X: 10, Y: 10, W: 400, H: 300},
		URL:      srv.URL + "/b",
	})
	waitTitle(t, events, "EVAL-B", 40*time.Second)
	if st := m.Stats(); st.Spaces != 2 {
		t.Fatalf("空间数应为 2，实际 %d", st.Spaces)
	}

	// 关掉 a 之后 b 仍能导航：确认销毁一个空间不会连带干掉同进程的其他空间
	m.Close("a")
	pumpFor(500 * time.Millisecond)
	if err := m.Navigate("b", srv.URL+"?again=1"); err != nil {
		t.Fatalf("Navigate(b): %v", err)
	}
	waitTitle(t, events, "EVAL-A", 20*time.Second)

	m.CloseAll()
	pumpFor(2 * time.Second)
	if st := m.Stats(); st.Spaces != 0 {
		t.Fatalf("CloseAll 后空间数应为 0，实际 %d", st.Spaces)
	}
}
