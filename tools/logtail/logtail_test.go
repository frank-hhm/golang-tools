package logtail

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLinesWholeFile(t *testing.T) {
	p := write(t, "a.log", "l1\nl2\nl3\n")
	got, err := Lines(p, 10)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "l1|l2|l3" {
		t.Fatalf("行数不对: %#v", got)
	}
	for i, l := range got {
		if strings.HasPrefix(l, "…") {
			t.Errorf("整体读到时不该出现截断标记，第 %d 行=%q", i, l)
		}
	}
}

func TestLinesMissingAndEmpty(t *testing.T) {
	got, err := Lines(filepath.Join(t.TempDir(), "none.log"), 5)
	if err != nil || got != nil {
		t.Fatalf("缺失文件应返回 nil,nil，得到 %#v / %v", got, err)
	}
	p := write(t, "e.log", "")
	if got, err := Lines(p, 5); err != nil || len(got) != 0 {
		t.Fatalf("空文件应无行，得到 %#v / %v", got, err)
	}
}

func TestLinesNoTrailingNewline(t *testing.T) {
	p := write(t, "nt.log", "a\nb\nc")
	got, err := Lines(p, 5)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "a|b|c" {
		t.Fatalf("末行不带换行也要拿到，得到 %#v", got)
	}
}

func TestLinesTailOnly(t *testing.T) {
	// 20000 行、每行 80 字节（约 1.6MB），远超单块 64KB：必须只取末尾 200 行
	var b strings.Builder
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&b, "line-%05d%s\n", i, strings.Repeat("x", 68))
	}
	p := write(t, "big.log", b.String())
	got, err := Lines(p, 200)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 200 {
		t.Fatalf("应拿到 200 行，实际 %d", len(got))
	}
	if !strings.HasPrefix(got[199], "line-19999") {
		t.Fatalf("最后一行应是 line-19999，实际 %q", got[199])
	}
	if !strings.HasPrefix(got[0], "line-19800") {
		t.Fatalf("首行应是 line-19800，实际 %q", got[0])
	}
	for _, l := range got {
		if strings.HasPrefix(l, "…") {
			t.Fatalf("恰好落在整行边界上，不该有截断标记：%q", l)
		}
	}
}

func TestLinesCRLF(t *testing.T) {
	p := write(t, "crlf.log", "a 1\r\nb 2\r\n")
	got, err := Lines(p, 5)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "a 1|b 2" {
		t.Fatalf("应去掉行尾 \\r，得到 %#v", got)
	}
}

func TestScanIncremental(t *testing.T) {
	p := write(t, "acc.log", "a 1\nb 2\n")
	var seen []string
	off, err := Scan(p, 0, func(line string) { seen = append(seen, line) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen, "|") != "a 1|b 2" {
		t.Fatalf("首轮应看到两行，得到 %#v", seen)
	}
	// 追加后只应看到新增部分（CRLF 结尾同样要去掉 \r）
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("c 3\r\nd 4\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	seen = nil
	off2, err := Scan(p, off, func(line string) { seen = append(seen, line) })
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen, "|") != "c 3|d 4" {
		t.Fatalf("增量应只看到新增两行，得到 %#v", seen)
	}
	// 再来一轮没有新增：必须一行都不给，偏移不前进
	seen = nil
	off3, err := Scan(p, off2, func(line string) { seen = append(seen, line) })
	if err != nil || len(seen) != 0 || off3 != off2 {
		t.Fatalf("无新增时应保持偏移不变，得到 %d / %#v / %v", off3, seen, err)
	}
	// 截断（轮转）后偏移越界必须回到 0 重读，不能静默丢内容
	if err := os.WriteFile(p, []byte("z 9\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	seen = nil
	if _, err := Scan(p, off2, func(line string) { seen = append(seen, line) }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen, "|") != "z 9" {
		t.Fatalf("轮转后应从头读，得到 %#v", seen)
	}
}

func TestScanMissingFile(t *testing.T) {
	off, err := Scan(filepath.Join(t.TempDir(), "none.log"), 100, func(string) {})
	if err != nil || off != 0 {
		t.Fatalf("缺失文件应返回 0,nil，得到 %d / %v", off, err)
	}
}

func TestTail(t *testing.T) {
	p := write(t, "t.log", "x\ny\nz\n")
	if got := Tail(p, 2); got != "y\nz" {
		t.Fatalf("Tail 结果不对: %q", got)
	}
	if got := Tail(filepath.Join(t.TempDir(), "none.log"), 2); got != "" {
		t.Fatalf("缺失文件 Tail 应为空串，得到 %q", got)
	}
}
