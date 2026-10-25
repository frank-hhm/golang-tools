//go:build windows

package winrun

// ============================================================
// 提权 helper 单测：只覆盖纯逻辑（UTF-16 解码判定、入参默认值），
// 不真正弹 UAC——那会要人点确认，也无法在 CI 里跑。
// ============================================================

import (
	"testing"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

func toUTF16Bytes(s string) string {
	u := utf16.Encode([]rune(s))
	buf := make([]byte, 0, len(u)*2)
	for _, v := range u {
		buf = append(buf, byte(v), byte(v>>8))
	}
	return string(buf)
}

func TestDecodeMaybeUTF16(t *testing.T) {
	// wsl.exe 在部分 Windows 版本用 UTF-16LE 输出，含大量 NUL
	if got := decodeMaybeUTF16(toUTF16Bytes("适用于 Linux 的 Windows 子系统")); got != "适用于 Linux 的 Windows 子系统" {
		t.Errorf("UTF-16 应解码为 UTF-8，得到 %q", got)
	}
	// 普通 ASCII/UTF-8 输出不能被动辄改写
	for _, s := range []string{"default: Ubuntu-24.04\n", "未安装\r\n", ""} {
		if got := decodeMaybeUTF16(s); got != s {
			t.Errorf("非 UTF-16 输入应原样返回: %q -> %q", s, got)
		}
	}
	// 极短输入直接透传，不误判
	if got := decodeMaybeUTF16("ab\x00"); got != "ab\x00" {
		t.Errorf("短输入应原样返回: %q", got)
	}
}

func TestUACCancelledErrorRecognized(t *testing.T) {
	var err error = &UACCancelledError{}
	if !IsUACCancelled(err) {
		t.Error("拒绝 UAC 必须可识别，调用方据此区别于真失败")
	}
	if IsUACCancelled(nil) {
		t.Error("nil 不应被判为取消")
	}
	if IsUACCancelled(errUnsupportedForTest()) {
		t.Error("普通错误不应被判为取消")
	}
}

func errUnsupportedForTest() error { return &timeoutErr{} }

type timeoutErr struct{}

func (timeoutErr) Error() string { return "提权进程超时未结束" }

// TestElevatedZeroMeansHidden 零值入参的含义：Show=0 走 SW_HIDE、Timeout=0 走默认时限，
// 调用方（写 PATH、装 Docker）只有显式传 SW_SHOW/长超时才会弹窗可见。
func TestElevatedZeroMeansHidden(t *testing.T) {
	e := Elevated{File: "reg.exe", Params: "query"}
	if e.Show != windows.SW_HIDE {
		t.Errorf("零值 Show 应等价 SW_HIDE: %d", e.Show)
	}
	if e.Timeout > 0 {
		t.Errorf("零值 Timeout 应为 0 并由实现回落默认: %v", e.Timeout)
	}
}
