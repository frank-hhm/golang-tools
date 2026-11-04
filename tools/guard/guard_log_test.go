package guard

// ReadLog / LogPath 的单元测试：写一段带多行的日志文件，验证「小文件全量返回」与
// 「大文件按尾部 KB 截断且从整行边界起」。日志落在 appdirs.Data("guard") 下，
// 测试结束删除临时文件，不污染真实登记项。

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestReadLogTail(t *testing.T) {
	// 用随机 id 避免与真实登记项撞车
	id := fmt.Sprintf("unittest-%d", os.Getpid())
	p, err := logPath(id)
	if err != nil {
		t.Fatalf("logPath 失败: %v", err)
	}
	defer os.Remove(p)

	// 构造 200 行、每行定长且行首带序号，方便校验整行边界
	var b strings.Builder
	const lines = 200
	for i := 1; i <= lines; i++ {
		fmt.Fprintf(&b, "line-%03d %s\n", i, strings.Repeat("x", 40))
	}
	full := b.String()
	if err := os.WriteFile(p, []byte(full), 0o644); err != nil {
		t.Fatalf("写日志失败: %v", err)
	}

	// 1) 尾部窗口足够大：应返回全文
	all, err := ReadLog(id, 1024) // 1MB，远大于全文
	if err != nil {
		t.Fatalf("ReadLog 全文失败: %v", err)
	}
	if all != full {
		t.Fatalf("全文返回不符：期望 %d 字节，得到 %d 字节", len(full), len(all))
	}

	// 2) 尾部窗口很小：应截断且从整行边界（\n 之后）起，末行完整保留
	tail, err := ReadLog(id, 1) // 1KB
	if err != nil {
		t.Fatalf("ReadLog 尾部失败: %v", err)
	}
	if len(tail) >= len(full) {
		t.Fatalf("尾部窗口应短于全文：全文 %d，尾部 %d", len(full), len(tail))
	}
	if !strings.HasSuffix(tail, fmt.Sprintf("line-%03d %s\n", lines, strings.Repeat("x", 40))) {
		t.Fatalf("尾部未以最后一行结束: %q", tail[len(tail)-60:])
	}
	// 首行必须是从某个 line-\d+ 开头（不是半截行）
	firstLine := strings.SplitN(tail, "\n", 2)[0]
	if !strings.HasPrefix(firstLine, "line-") {
		t.Fatalf("首行非整行边界（疑似半截）: %q", firstLine)
	}

	// 3) 文件不存在：返回空串而非错误
	if s, err := ReadLog("no-such-id-xyz", 0); err != nil || s != "" {
		t.Fatalf("不存在文件应返回空且无错：got %q err %v", s, err)
	}
}

func TestLogPathMatches(t *testing.T) {
	id := fmt.Sprintf("unittest-path-%d", os.Getpid())
	p1, err := LogPath(id)
	if err != nil {
		t.Fatalf("LogPath 失败: %v", err)
	}
	p2, err := logPath(id)
	if err != nil {
		t.Fatalf("logPath 失败: %v", err)
	}
	if p1 != p2 {
		t.Fatalf("LogPath 与内部 logPath 不一致: %q vs %q", p1, p2)
	}
}
