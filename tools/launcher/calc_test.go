package launcher

import (
	"math"
	"testing"
)

func TestEvalExpr(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"1+2*3", 7, true},
		{"(1+2)*3", 9, true},
		{"2^10", 1024, true},
		{"2^3^2", 512, true}, // 幂右结合：2^(3^2)
		{"-3+10", 7, true},
		{"10/4", 2.5, true},
		{"10%3", 1, true},
		{"1e3+1", 1001, true},
		{"1/0", 0, false},  // 除零不产出条目
		{"10%0", 0, false}, // 模零同理
		{"1+", 0, false},
		{"(1+2", 0, false},
		{"1 2", 0, false},
		{"abc", 0, false},
		{"", 0, false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, ok := EvalExpr(c.in)
			if ok != c.ok {
				t.Fatalf("ok = %v，期望 %v", ok, c.ok)
			}
			if c.ok && math.Abs(got-c.want) > 1e-9 {
				t.Fatalf("%q = %v，期望 %v", c.in, got, c.want)
			}
		})
	}
}

func TestLooksLikeExprRejectsSearchWords(t *testing.T) {
	// 关键负例：搜索词里带数字或减号时不能冒出计算器条目。
	for _, s := range []string{"3 - 2 个苹果", "报告 2026", "win10", "502 错误码", "纯数字"} {
		if looksLikeExpr(s) {
			t.Errorf("%q 不该被当成算式", s)
		}
	}
	for _, s := range []string{"1+1", "3 * 4", "(2+3)/5", "2^8"} {
		if !looksLikeExpr(s) {
			t.Errorf("%q 看起来就是算式", s)
		}
	}
}

func TestCalcItemRequiresWholeStringExpr(t *testing.T) {
	// "1+1 等于几" 里前缀是合法算式，但整串不是：不该给计算器结果。
	if _, ok := calcItem("1+1 等于几"); ok {
		t.Fatal("整串不是算式时不该产出计算器条目")
	}
	it, ok := calcItem("1+1")
	if !ok {
		t.Fatal("1+1 应产出条目")
	}
	if it.ID != "2" || it.Type != TypeCalc {
		t.Fatalf("条目不对：%+v", it)
	}
}

func TestFormatNum(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{1234567, "1,234,567"},
		{-1234, "-1,234"},
		{2.5, "2.5"},
		{1 / 3.0, "0.3333333333333333"},
		{1000000.25, "1,000,000.25"},
		{0, "0"},
	}
	for _, c := range cases {
		if got := formatNum(c.in); got != c.want {
			t.Errorf("formatNum(%v) = %q，期望 %q", c.in, got, c.want)
		}
	}
}
