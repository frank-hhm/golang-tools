package launcher

import (
	"math"
	"strconv"
	"strings"
)

// ============================================================
// 算式求值
//
// 用递归下降而不是调度场算法：语法就四条产生式，写成调度场要多维护一张
// 优先级表和一条逆波兰，读的人反而要在两种表示之间来回翻译。
//
// 只有"整串看起来就是个算式"才产出结果条目（见 looksLikeExpr）：
// 否则用户敲 "3 - 2 个苹果" 这种搜索词时会被塞一条计算器结果，纯属添乱。
// ============================================================

// exprCharset 算式允许的字符。出现别的一律不当算式处理。
const exprCharset = "0123456789+-*/%^().eE \t"

// looksLikeExpr 粗筛：含数字、含运算符、只含允许的字符。
func looksLikeExpr(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	digit, op := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if strings.IndexByte(exprCharset, c) < 0 {
			return false
		}
		switch {
		case c >= '0' && c <= '9':
			digit = true
		case c == '+' || c == '-' || c == '*' || c == '/' || c == '%' || c == '^':
			op = true
		}
	}
	return digit && op
}

// EvalExpr 求一个算式的值；语法错误、除零、结果非有限值都返回 ok=false。
func EvalExpr(s string) (float64, bool) {
	p := &exprParser{s: strings.TrimSpace(s)}
	v, ok := p.parseExpr()
	if !ok {
		return 0, false
	}
	p.skipSpace()
	if p.i != len(p.s) { // 还有字符没吃掉：说明是"搜索词"而不是算式
		return 0, false
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

type exprParser struct {
	s string
	i int
}

func (p *exprParser) skipSpace() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\t') {
		p.i++
	}
}

// peek 返回下一个非空白字符（不消费）。
func (p *exprParser) peek() (byte, bool) {
	p.skipSpace()
	if p.i >= len(p.s) {
		return 0, false
	}
	return p.s[p.i], true
}

// parseExpr: term (('+'|'-') term)*
func (p *exprParser) parseExpr() (float64, bool) {
	left, ok := p.parseTerm()
	if !ok {
		return 0, false
	}
	for {
		c, more := p.peek()
		if !more || (c != '+' && c != '-') {
			return left, true
		}
		p.i++
		right, ok := p.parseTerm()
		if !ok {
			return 0, false
		}
		if c == '+' {
			left += right
		} else {
			left -= right
		}
	}
}

// parseTerm: factor (('*'|'/'|'%') factor)*
func (p *exprParser) parseTerm() (float64, bool) {
	left, ok := p.parseFactor()
	if !ok {
		return 0, false
	}
	for {
		c, more := p.peek()
		if !more || (c != '*' && c != '/' && c != '%') {
			return left, true
		}
		p.i++
		right, ok := p.parseFactor()
		if !ok {
			return 0, false
		}
		switch c {
		case '*':
			left *= right
		case '/':
			if right == 0 {
				return 0, false // 除零不产出条目：给个 "∞" 只会让人以为是 bug
			}
			left /= right
		case '%':
			if right == 0 {
				return 0, false
			}
			left = math.Mod(left, right)
		}
	}
}

// parseFactor: unary ('^' factor)?  —— 幂是右结合，所以递归调 parseFactor 而不是 parseUnary。
func (p *exprParser) parseFactor() (float64, bool) {
	base, ok := p.parseUnary()
	if !ok {
		return 0, false
	}
	if c, more := p.peek(); more && c == '^' {
		p.i++
		exp, ok := p.parseFactor()
		if !ok {
			return 0, false
		}
		return math.Pow(base, exp), true
	}
	return base, true
}

// parseUnary: ('+'|'-') unary | primary
func (p *exprParser) parseUnary() (float64, bool) {
	c, more := p.peek()
	if more && (c == '+' || c == '-') {
		p.i++
		v, ok := p.parseUnary()
		if !ok {
			return 0, false
		}
		if c == '-' {
			return -v, true
		}
		return v, true
	}
	return p.parsePrimary()
}

// parsePrimary: number | '(' expr ')'
func (p *exprParser) parsePrimary() (float64, bool) {
	c, more := p.peek()
	if !more {
		return 0, false
	}
	if c == '(' {
		p.i++
		v, ok := p.parseExpr()
		if !ok {
			return 0, false
		}
		if c2, more2 := p.peek(); !more2 || c2 != ')' {
			return 0, false // 括号不配平
		}
		p.i++
		return v, true
	}
	return p.parseNumber()
}

// parseNumber: 整数/小数/科学计数（1e3、2.5E-4）。
func (p *exprParser) parseNumber() (float64, bool) {
	start := p.i
	for p.i < len(p.s) {
		c := p.s[p.i]
		if c >= '0' && c <= '9' {
			p.i++
			continue
		}
		if c == '.' {
			p.i++
			continue
		}
		if c == 'e' || c == 'E' {
			p.i++
			if p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
				p.i++
			}
			continue
		}
		break
	}
	if p.i == start {
		return 0, false
	}
	f, err := strconv.ParseFloat(p.s[start:p.i], 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

// formatNum 结果文本：整值走千分位整数，非整值最多 10 位小数再去尾零。
func formatNum(f float64) string {
	if math.Abs(f-math.Trunc(f)) < 1e-12 && math.Abs(f) < 1e15 {
		return groupThousands(strconv.FormatInt(int64(math.Trunc(f)), 10))
	}
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if len(s) > 24 { // 科学计数或超长小数：原样给出，不做千分位
		return s
	}
	ip := s
	fp := ""
	if neg := strings.HasPrefix(s, "-"); neg {
		ip = s[1:]
	}
	if dot := strings.IndexByte(ip, '.'); dot >= 0 {
		fp = ip[dot:]
		ip = ip[:dot]
	}
	ip = groupThousands(ip)
	if strings.HasPrefix(s, "-") {
		ip = "-" + ip
	}
	return ip + fp
}

// groupThousands 给整数部分加千分位逗号。
func groupThousands(intPart string) string {
	neg := strings.HasPrefix(intPart, "-")
	if neg {
		intPart = intPart[1:]
	}
	if len(intPart) <= 3 {
		if neg {
			return "-" + intPart
		}
		return intPart
	}
	var b strings.Builder
	b.Grow(len(intPart) + len(intPart)/3 + 1)
	pre := len(intPart) % 3
	for i, c := range intPart {
		if i > 0 && (i-pre)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// calcItem 整串是合法算式时给一条结果条目（回车 = 把结果送进剪贴板）。
func calcItem(q string) (Item, bool) {
	if !looksLikeExpr(q) {
		return Item{}, false
	}
	v, ok := EvalExpr(q)
	if !ok {
		return Item{}, false
	}
	text := formatNum(v)
	return Item{
		Type:  TypeCalc,
		ID:    text,
		Title: text,
		Sub:   "回车复制到剪贴板",
		Score: scoreExact,
	}, true
}
