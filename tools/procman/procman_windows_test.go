//go:build windows

package procman

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// explorer 的 /select 参数形态是这里唯一的坑：引号只能包路径。
// 记死这条，避免以后有人「顺手」改回 exec.Command 传参（含空格的路径会静默失效）。
func TestExplorerSelectLine(t *testing.T) {
	cases := []struct{ in, want string }{
		{`C:\Windows\notepad.exe`, `explorer.exe /select,"C:\Windows\notepad.exe"`},
		{`C:\Program Files\Qoder CN IDE\Qoder CN IDE.exe`, `explorer.exe /select,"C:\Program Files\Qoder CN IDE\Qoder CN IDE.exe"`},
	}
	for _, c := range cases {
		if got := explorerSelectLine(c.in); got != c.want {
			t.Fatalf("explorerSelectLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// 图标依赖 shell32 的 ExtractIconExW：函数挂错 DLL 时早期版本会直接 panic
// （表现为前端一片「Binding call failed」），这个测试就是钉住它能真拿到 PNG。
func TestExtractIconDataURL(t *testing.T) {
	sysRoot := os.Getenv("SystemRoot")
	if sysRoot == "" {
		sysRoot = `C:\Windows`
	}
	exe := filepath.Join(sysRoot, "System32", "cmd.exe")
	if _, err := os.Stat(exe); err != nil {
		t.Skipf("测试环境没有 %s", exe)
	}
	got := extractIconDataURL(exe)
	if !strings.HasPrefix(got, "data:image/png;base64,") || len(got) < 64 {
		t.Fatalf("extractIconDataURL(%q) 没拿到 PNG dataURL，实际 %q", exe, truncateFor(got))
	}
	// 不存在的路径只要求“不 panic”：实测 shell 会给一个通用文件图标而非 0，
	// 所以不断言空串（早期版本在这里直接 panic 把绑定调用炸掉）。
	extractIconDataURL(filepath.Join(sysRoot, "no-such-dir-9f2a", "nope.exe"))
}

// iconCache 被前端并发调（图标加载限流 6 路），没锁就是 fatal error；-race 下跑这个。
func TestIconOfExeConcurrentCache(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = iconOfExe(exe)
		}()
	}
	wg.Wait()
}

func truncateFor(s string) string {
	if len(s) > 48 {
		return s[:48] + "..."
	}
	return s
}
