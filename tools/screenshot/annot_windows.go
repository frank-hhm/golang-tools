//go:build windows

package screenshot

import (
	"image"
	"math"
)

// ============================================================
// 选区标注编辑器（在选区遮罩层内实现，微信/Snipaste 风格）
//
// 选区确定后在选区下方浮出一条工具栏：矩形 / 椭圆 / 箭头 / 画笔 /
// 马赛克 五种绘图工具，外加 撤销 / 取消 / 确认 三个动作按钮；
// 选中绘图工具后再展开一条子工具栏，提供 4 档线宽 + 6 色调色板。
//
// 所有图形以「屏幕坐标」记录，边画边合成到 work DIB 上实时预览；
// 点确认后再统一绘制到冻结帧（FrozenFrame）上，工具栏本身不入图。
// ============================================================

// ToolType 绘图工具类型
type ToolType int

const (
	ToolNone ToolType = iota
	ToolRect
	ToolEllipse
	ToolArrow
	ToolPen
	ToolMosaic
)

// 动作按钮 ID（与 ToolType 共用一个 int 空间，取负值避免冲突）
const (
	actUndo    = -1
	actCancel  = -2
	actConfirm = -3
	actBlurAll = -4
)

// 子工具栏条目 ID
const (
	subColorBase = -10 // -10 .. -15 依次是 6 种颜色
	subSizeBase  = -20 // -20 .. -23 依次是 4 档线宽
)

// ============================================================
// 图形记录
// ============================================================

// Shape 一个已完成的标注图形（坐标为屏幕绝对坐标）
type Shape struct {
	Type   ToolType
	X1, Y1 int32
	X2, Y2 int32
	RGB    [3]byte
	LW     int
	Pts    []POINT // 画笔轨迹
}

// btnArea 一个可点击区域（工具栏按钮）
type btnArea struct {
	x, y, w, h int
	id         int
}

func (b btnArea) hit(x, y int32) bool {
	return x >= int32(b.x) && x < int32(b.x+b.w) && y >= int32(b.y) && y < int32(b.y+b.h)
}

// ============================================================
// 工具栏几何 / 调色板
// ============================================================

const (
	annBtn    = 30 // 按钮边长
	annGap    = 3  // 按钮间距
	annPad    = 6  // 工具栏内边距
	annSep    = 9  // 分隔区宽度
	annBlurW  = 72 // 「一键打码」文字按钮宽度
	annBarH   = annBtn + annPad*2
	annSubH   = 26 + annPad*2
	annMargin = 6 // 工具栏与选区的间距
)

// annTools 工具栏里的绘图工具（顺序即显示顺序）
var annTools = []ToolType{ToolRect, ToolEllipse, ToolArrow, ToolPen, ToolMosaic}

// annPalette 调色板：红 橙 黄 绿 蓝 白（与参考工具接近）
var annPalette = [][3]byte{
	{0xF5, 0x22, 0x2D},
	{0xFF, 0x9A, 0x00},
	{0xF7, 0xDF, 0x00},
	{0x23, 0xC4, 0x7A},
	{0x1E, 0x9B, 0xFF},
	{0xFF, 0xFF, 0xFF},
}

// annSizes 4 档线宽
var annSizes = []int{2, 4, 7, 11}

// ============================================================
// 编辑状态
// ============================================================

// annotState 标注编辑状态。只在选区线程（LockOSThread）里访问，无需加锁。
type annotState struct {
	show bool     // 工具栏是否显示
	tool ToolType // 当前绘图工具（ToolNone = 未选中）

	shapes []Shape

	// 正在绘制中的一个图形
	drawing bool
	cur     Shape

	// 工具栏布局：bar* 是主工具栏，sub* 是子工具栏（线宽/颜色）。
	// 两者必须分开保存——子栏可能翻转到主栏上侧，混用会破坏命中判定。
	sw, sh             int
	barX, barY, barW   int
	subX, subY, subW   int
	toolBtns           []btnArea
	actBtns            []btnArea
	subBtns            []btnArea
	subVisible         bool

	sel image.Rectangle // 当前选区（屏幕坐标）
}

var gAnnot *annotState

// annDefaultRGB / annDefaultLW 新会话的默认颜色与线宽（红色 4px）
var annDefaultRGB = [3]byte{0xF5, 0x22, 0x2D}

const annDefaultLW = 4

func newAnnotState() *annotState {
	// 默认不选中任何绘图工具：这样选区确定后直接双击/回车即可确认，
	// 不会因为在选区内点击而误画标注。
	return &annotState{cur: Shape{RGB: annDefaultRGB, LW: annDefaultLW}}
}

// resetForNewSel 换了新选区：丢掉全部标注与工具栏
func (a *annotState) resetForNewSel() {
	a.show = false
	a.shapes = a.shapes[:0]
	a.drawing = false
	a.subVisible = false
	a.tool = ToolNone
	a.cur = Shape{RGB: a.cur.RGB, LW: a.cur.LW}
}

// subNeeded 子工具栏当前是否需要空间（选中绘图工具时才展开）
func (a *annotState) subNeeded() bool { return a.tool != ToolNone }

// layout 依据选区与屏幕尺寸计算工具栏位置。
// 优先放在选区下侧外部，并为子栏预留空间（避免切换工具时工具栏跳动）；
// 放不下就挤到选区内侧底部，保证两条栏都始终可见。
func (a *annotState) layout(sel image.Rectangle, sw, sh int) {
	a.sel = sel
	a.sw, a.sh = sw, sh

	nTool, nAct := len(annTools), 3
	barW := annPad*2 + nTool*(annBtn+annGap) + annSep + nAct*(annBtn+annGap) - annGap
	subW := a.subWidth()
	a.barW = barW
	if subW > a.barW {
		a.barW = subW
	}

	x := sel.Min.X
	if x+a.barW > sw {
		x = sw - a.barW
	}
	if x < 0 {
		x = 0
	}

	need := annBarH
	if a.subNeeded() {
		need += annSubH + 2
	}
	y := sel.Max.Y + annMargin
	if y+need > sh {
		y = sel.Max.Y - need - 2 // 挤到选区内侧底部
	}
	if y < 0 {
		y = 0
	}
	a.barX, a.barY = x, y

	bx := x + annPad
	by := y + annPad
	a.toolBtns = a.toolBtns[:0]
	for _, t := range annTools {
		a.toolBtns = append(a.toolBtns, btnArea{bx, by, annBtn, annBtn, int(t)})
		bx += annBtn + annGap
	}
	bx += annSep - annGap
	a.actBtns = []btnArea{
		{bx, by, annBtn, annBtn, actUndo},
		{bx + annBtn + annGap, by, annBtn, annBtn, actCancel},
		{bx + 2*(annBtn+annGap), by, annBtn, annBtn, actConfirm},
	}

	if a.subNeeded() {
		a.layoutSub()
	} else {
		a.subBtns = a.subBtns[:0]
		a.subVisible = false
	}
}

// subWidth 子工具栏宽度（马赛克时额外加「一键打码」按钮）
func (a *annotState) subWidth() int {
	w := annPad*2 + len(annSizes)*(annBtn+annGap) + annSep + len(annPalette)*(annBtn+annGap) - annGap
	if a.tool == ToolMosaic {
		w += annBlurW + annGap
	}
	return w
}

// layoutSub 子工具栏（线宽 + 颜色），默认放在主工具栏下侧；放不下就翻到上侧。
// 只写 sub* 字段，绝不修改主工具栏几何。
func (a *annotState) layoutSub() {
	sw, sh := a.sw, a.sh
	a.subBtns = a.subBtns[:0]
	w := a.subWidth()
	a.subW = w

	x := a.barX
	if x+w > sw {
		x = sw - w
	}
	if x < 0 {
		x = 0
	}
	y := a.barY + annBarH + 2
	if y+annSubH > sh {
		y = a.barY - annSubH - 2
	}
	if y < 0 {
		y = 0
	}
	a.subX, a.subY = x, y

	bx, by := x+annPad, y+annPad
	for i := range annSizes {
		a.subBtns = append(a.subBtns, btnArea{bx, by, annBtn, annBtn, subSizeBase - i})
		bx += annBtn + annGap
	}
	bx += annSep - annGap
	for i := range annPalette {
		a.subBtns = append(a.subBtns, btnArea{bx, by, annBtn, annBtn, subColorBase - i})
		bx += annBtn + annGap
	}
	if a.tool == ToolMosaic {
		a.subBtns = append(a.subBtns, btnArea{bx, by, annBlurW, annBtn, actBlurAll})
	}
	a.subVisible = true
}

// setTool 切换绘图工具，同步展开/收起子工具栏（并重新布局避免跳动）。
func (a *annotState) setTool(t ToolType) {
	if a.tool == t {
		a.tool = ToolNone // 再点一次同一个工具 = 收起子栏
	} else {
		a.tool = t
	}
	a.layout(a.sel, a.sw, a.sh)
}

// ============================================================
// 事件处理：返回 consumed=true 表示这次点击被工具栏按钮消化，不再走选区/起笔逻辑
// ============================================================

// onClick 处理一次按下。返回 (consumed, confirm, cancel)。
// 只有真正命中按钮才返回 consumed=true；其余地方透传给调用方，
// 由它决定是在选区内起笔还是重新拖框。
func (a *annotState) onClick(x, y int32) (bool, bool, bool) {
	if !a.show {
		return false, false, false
	}
	if a.subVisible {
		for _, b := range a.subBtns {
			if !b.hit(x, y) {
				continue
			}
			switch {
			case b.id <= subColorBase && b.id > subColorBase-10:
				a.cur.RGB = annPalette[subColorBase-b.id]
			case b.id <= subSizeBase && b.id > subSizeBase-10:
				a.cur.LW = annSizes[subSizeBase-b.id]
			case b.id == actBlurAll:
				// 一键打码：整个选区覆一层马赛克
				a.shapes = append(a.shapes, Shape{
					Type: ToolMosaic,
					X1:   int32(a.sel.Min.X), Y1: int32(a.sel.Min.Y),
					X2:   int32(a.sel.Max.X), Y2: int32(a.sel.Max.Y),
					LW:   10,
				})
			}
			return true, false, false
		}
	}
	for _, b := range a.actBtns {
		if !b.hit(x, y) {
			continue
		}
		switch b.id {
		case actUndo:
			a.undo()
			return true, false, false
		case actCancel:
			return true, false, true
		case actConfirm:
			return true, true, false
		}
		return true, false, false
	}
	for _, b := range a.toolBtns {
		if b.hit(x, y) {
			a.setTool(ToolType(b.id))
			return true, false, false
		}
	}
	// 落在底板空白/按钮缝隙里：吃掉这次点击。
	// 否则会被当成“重新拖框”而把刚选好的选区连同全部标注一起冲掉。
	if a.inBar(x, y) {
		return true, false, false
	}
	return false, false, false
}

// inBar 判断点是否落在主/子工具栏底板内（用于提示光标状态，不参与命中分发）
func (a *annotState) inBar(x, y int32) bool {
	p := image.Pt(int(x), int(y))
	if p.In(image.Rect(a.barX, a.barY, a.barX+a.barW, a.barY+annBarH)) {
		return true
	}
	if a.subVisible && p.In(image.Rect(a.subX, a.subY, a.subX+a.subW, a.subY+annSubH)) {
		return true
	}
	return false
}

func (a *annotState) undo() {
	if len(a.shapes) > 0 {
		a.shapes = a.shapes[:len(a.shapes)-1]
	}
}

// startDraw / moveDraw / endDraw 绘制一个图形的生命周期。
// 只在选区内部起笔，坐标会被夹到选区矩形内，避免画到遮罩上。

func (a *annotState) clamp(p POINT) POINT {
	r := a.sel
	if p.X < int32(r.Min.X) {
		p.X = int32(r.Min.X)
	}
	if p.X > int32(r.Max.X) {
		p.X = int32(r.Max.X)
	}
	if p.Y < int32(r.Min.Y) {
		p.Y = int32(r.Min.Y)
	}
	if p.Y > int32(r.Max.Y) {
		p.Y = int32(r.Max.Y)
	}
	return p
}

func (a *annotState) canDraw(x, y int32) bool {
	if !a.show || a.tool == ToolNone {
		return false
	}
	p := image.Pt(int(x), int(y))
	// 工具栏挤进选区时，底板上不起笔（避开死区误画）
	return p.In(a.sel) && !a.inBar(x, y)
}

func (a *annotState) startDraw(x, y int32) {
	p := a.clamp(POINT{x, y})
	a.drawing = true
	a.cur = Shape{Type: a.tool, X1: p.X, Y1: p.Y, X2: p.X, Y2: p.Y, RGB: a.cur.RGB, LW: a.cur.LW}
	if a.tool == ToolPen {
		a.cur.Pts = []POINT{p}
	}
}

func (a *annotState) moveDraw(x, y int32) {
	if !a.drawing {
		return
	}
	p := a.clamp(POINT{x, y})
	a.cur.X2, a.cur.Y2 = p.X, p.Y
	if a.tool == ToolPen {
		// 采样去重：距离上一点 >=2px 才记，轨迹更平滑且点数可控
		last := a.cur.Pts[len(a.cur.Pts)-1]
		if abs32(p.X-last.X) >= 2 || abs32(p.Y-last.Y) >= 2 {
			a.cur.Pts = append(a.cur.Pts, p)
		}
	}
}

func (a *annotState) endDraw() {
	if !a.drawing {
		return
	}
	a.drawing = false
	s := a.cur
	ok := false
	if s.Type == ToolPen {
		ok = len(s.Pts) > 1
	} else {
		ok = abs32(s.X2-s.X1) >= 2 && abs32(s.Y2-s.Y1) >= 2
	}
	if ok {
		a.shapes = append(a.shapes, s)
	}
	a.cur = Shape{Type: ToolNone, RGB: s.RGB, LW: s.LW}
}

func abs32(v int32) int32 {
	if v < 0 {
		return -v
	}
	return v
}

// ============================================================
// DIB 绘图原语（32 位 BGRA，自上而下，直接写像素）
// ============================================================

func dibDot(dib []byte, sw, sh, x, y, lw int, r, g, b byte) {
	half := lw / 2
	for dy := -half; dy <= half; dy++ {
		py := y + dy
		if py < 0 || py >= sh {
			continue
		}
		for dx := -half; dx <= half; dx++ {
			px := x + dx
			if px < 0 || px >= sw {
				continue
			}
			o := (py*sw + px) * 4
			dib[o] = b
			dib[o+1] = g
			dib[o+2] = r
			dib[o+3] = 255
		}
	}
}

func dibLine(dib []byte, sw, sh, x1, y1, x2, y2, lw int, r, g, b byte) {
	dx, dy := x2-x1, y2-y1
	length := math.Hypot(float64(dx), float64(dy))
	if length < 1 {
		dibDot(dib, sw, sh, x1, y1, lw, r, g, b)
		return
	}
	ux, uy := float64(dx)/length, float64(dy)/length
	for t := 0.0; t <= length; t += 0.5 {
		dibDot(dib, sw, sh, x1+int(ux*t), y1+int(uy*t), lw, r, g, b)
	}
}

// dibStrokeRectD 按任意两点顺序描矩形边框
func dibStrokeRectD(dib []byte, sw, sh, x1, y1, x2, y2, lw int, r, g, b byte) {
	ax, bx := x1, x2
	if ax > bx {
		ax, bx = bx, ax
	}
	ay, by := y1, y2
	if ay > by {
		ay, by = by, ay
	}
	dibLine(dib, sw, sh, ax, ay, bx, ay, lw, r, g, b)
	dibLine(dib, sw, sh, bx, ay, bx, by, lw, r, g, b)
	dibLine(dib, sw, sh, bx, by, ax, by, lw, r, g, b)
	dibLine(dib, sw, sh, ax, by, ax, ay, lw, r, g, b)
}

// dibEllipse 以两点矩形为外接框画椭圆（参数方程采样，步数随周长自适应）
func dibEllipse(dib []byte, sw, sh, x1, y1, x2, y2, lw int, r, g, b byte) {
	cx, cy := float64(x1+x2)/2, float64(y1+y2)/2
	rx, ry := math.Abs(float64(x2-x1))/2, math.Abs(float64(y2-y1))/2
	if rx < 1 || ry < 1 {
		return
	}
	steps := int(math.Max(60, (rx+ry)*2))
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps) * 2 * math.Pi
		dibDot(dib, sw, sh, int(cx+rx*math.Cos(t)), int(cy+ry*math.Sin(t)), lw, r, g, b)
	}
}

// dibArrow 直线 + 两端夹角 ±30° 的箭头（头部长度随线宽放大）。
// 参数与成品路径 drawArrow 保持一致，避免预览与成品的箭头大小/张角不一致。
func dibArrow(dib []byte, sw, sh, x1, y1, x2, y2, lw int, r, g, b byte) {
	dx, dy := x2-x1, y2-y1
	length := math.Hypot(float64(dx), float64(dy))
	if length < 1 {
		return
	}
	dibLine(dib, sw, sh, x1, y1, x2, y2, lw, r, g, b)

	size := math.Max(8, float64(lw)*3)
	angle := math.Atan2(float64(dy), float64(dx))
	for _, d := range []float64{-math.Pi / 6, math.Pi / 6} {
		dibLine(dib, sw, sh, x2, y2,
			x2-int(size*math.Cos(angle+d)), y2-int(size*math.Sin(angle+d)), lw, r, g, b)
	}
}

func dibPenPath(dib []byte, sw, sh int, pts []POINT, lw int, r, g, b byte) {
	for i := 1; i < len(pts); i++ {
		dibLine(dib, sw, sh, int(pts[i-1].X), int(pts[i-1].Y), int(pts[i].X), int(pts[i].Y), lw, r, g, b)
	}
}

// dibMosaic 把矩形区域按 blockSize 分块取平均色（自带 4x4 起）
func dibMosaic(dib []byte, sw, sh, x1, y1, x2, y2, blockSize int) {
	if blockSize < 4 {
		blockSize = 4
	}
	ax, bx := x1, x2
	if ax > bx {
		ax, bx = bx, ax
	}
	ay, by := y1, y2
	if ay > by {
		ay, by = by, ay
	}
	if ax < 0 {
		ax = 0
	}
	if ay < 0 {
		ay = 0
	}
	if bx > sw {
		bx = sw
	}
	if by > sh {
		by = sh
	}
	for y := ay; y < by; y += blockSize {
		for x := ax; x < bx; x += blockSize {
			ex, ey := min(x+blockSize, bx), min(y+blockSize, by)
			var rr, gg, bb, n int
			for sy := y; sy < ey; sy++ {
				row := sy * sw * 4
				for sx := x; sx < ex; sx++ {
					o := row + sx*4
					bb += int(dib[o])
					gg += int(dib[o+1])
					rr += int(dib[o+2])
					n++
				}
			}
			if n == 0 {
				continue
			}
			ab, ag, ar := byte(bb/n), byte(gg/n), byte(rr/n)
			for sy := y; sy < ey; sy++ {
				for sx := x; sx < ex; sx++ {
					o := (sy*sw + sx) * 4
					dib[o] = ab
					dib[o+1] = ag
					dib[o+2] = ar
					dib[o+3] = 255
				}
			}
		}
	}
}

// ============================================================
// 图形合成（DIB 预览 / RGBA 成品 两条路径共用一份分发逻辑）
// ============================================================

// renderShapeDIB 把一个图形画到 work DIB 上（实时预览）
func renderShapeDIB(dib []byte, sw, sh int, s Shape) {
	r, g, b := s.RGB[0], s.RGB[1], s.RGB[2]
	switch s.Type {
	case ToolRect:
		dibStrokeRectD(dib, sw, sh, int(s.X1), int(s.Y1), int(s.X2), int(s.Y2), s.LW, r, g, b)
	case ToolEllipse:
		dibEllipse(dib, sw, sh, int(s.X1), int(s.Y1), int(s.X2), int(s.Y2), s.LW, r, g, b)
	case ToolArrow:
		dibArrow(dib, sw, sh, int(s.X1), int(s.Y1), int(s.X2), int(s.Y2), s.LW, r, g, b)
	case ToolPen:
		dibPenPath(dib, sw, sh, s.Pts, s.LW, r, g, b)
	case ToolMosaic:
		dibMosaic(dib, sw, sh, int(s.X1), int(s.Y1), int(s.X2), int(s.Y2), max(s.LW*2, 8))
	}
}

// compositeShapes 把全部标注画到冻结帧（成品图，不含工具栏）
func compositeShapes(img *image.RGBA, shapes []Shape) {
	for _, s := range shapes {
		switch s.Type {
		case ToolRect:
			drawRect(img, int(s.X1), int(s.Y1), int(s.X2), int(s.Y2), s.RGB[0], s.RGB[1], s.RGB[2], s.LW)
		case ToolEllipse:
			compositeEllipse(img, s)
		case ToolArrow:
			drawArrow(img, int(s.X1), int(s.Y1), int(s.X2), int(s.Y2), s.RGB[0], s.RGB[1], s.RGB[2], s.LW)
		case ToolPen:
			for i := 1; i < len(s.Pts); i++ {
				drawLine(img, int(s.Pts[i-1].X), int(s.Pts[i-1].Y), int(s.Pts[i].X), int(s.Pts[i].Y),
					s.RGB[0], s.RGB[1], s.RGB[2], s.LW)
			}
		case ToolMosaic:
			drawMosaic(img, int(s.X1), int(s.Y1), int(s.X2), int(s.Y2), max(s.LW*2, 8))
		}
	}
}

// compositeEllipse 在 *image.RGBA 上画椭圆（screenshot_service.go 里没有现成实现）
func compositeEllipse(img *image.RGBA, s Shape) {
	cx, cy := float64(s.X1+s.X2)/2, float64(s.Y1+s.Y2)/2
	rx, ry := math.Abs(float64(s.X2-s.X1))/2, math.Abs(float64(s.Y2-s.Y1))/2
	if rx < 1 || ry < 1 {
		return
	}
	steps := int(math.Max(60, (rx+ry)*2))
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps) * 2 * math.Pi
		drawDot(img, int(cx+rx*math.Cos(t)), int(cy+ry*math.Sin(t)), s.RGB[0], s.RGB[1], s.RGB[2], s.LW)
	}
}

// ============================================================
// 工具栏绘制
// ============================================================

// drawBars 画主工具栏 + 子工具栏（含图标与选中态）
func (a *annotState) drawBars(dib []byte, sw, sh int, workDC uintptr) {
	// layout 未跑过（理论不应发生）时直接跳过，不拿空切片下表、不让遮罩层崩溃
	if a.barW <= 0 || len(a.actBtns) == 0 {
		selLogf("drawBars 跳过：工具栏未布局 barW=%d actBtns=%d", a.barW, len(a.actBtns))
		return
	}

	// 主工具栏底板：深灰 + 1px 亮边
	dibFill(dib, sw, sh, a.barX, a.barY, a.barX+a.barW, a.barY+annBarH, 0x2B, 0x2B, 0x2B)
	dibStrokeRect(dib, sw, sh, a.barX, a.barY, a.barX+a.barW, a.barY+annBarH, 1, 0x55, 0x55, 0x55)

	for _, b := range a.toolBtns {
		if a.tool == ToolType(b.id) {
			dibFill(dib, sw, sh, b.x, b.y, b.x+b.w, b.y+b.h, 0x1E, 0x6F, 0xC8)
		}
		drawToolIcon(dib, sw, sh, b, ToolType(b.id))
	}
	// 分隔线
	sepX := a.actBtns[0].x - annSep/2
	dibFill(dib, sw, sh, sepX, a.barY+annPad, sepX+1, a.barY+annBarH-annPad, 0x55, 0x55, 0x55)

	for _, b := range a.actBtns {
		switch b.id {
		case actUndo:
			drawUndoIcon(dib, sw, sh, b)
		case actCancel:
			drawCrossIcon(dib, sw, sh, b, 0xF5, 0x50, 0x50)
		case actConfirm:
			drawCheckIcon(dib, sw, sh, b, 0x3D, 0xD0, 0x8B)
		}
	}

	if !a.subVisible {
		return
	}
	dibFill(dib, sw, sh, a.subX, a.subY, a.subX+a.subW, a.subY+annSubH, 0x2B, 0x2B, 0x2B)
	dibStrokeRect(dib, sw, sh, a.subX, a.subY, a.subX+a.subW, a.subY+annSubH, 1, 0x55, 0x55, 0x55)

	for _, b := range a.subBtns {
		switch {
		case b.id <= subColorBase && b.id > subColorBase-10:
			i := subColorBase - b.id
			c := annPalette[i]
			dibFill(dib, sw, sh, b.x+5, b.y+5, b.x+b.w-5, b.y+b.h-5, c[0], c[1], c[2])
			if a.cur.RGB == c {
				dibStrokeRect(dib, sw, sh, b.x+3, b.y+3, b.x+b.w-3, b.y+b.h-3, 2, 0xFF, 0xFF, 0xFF)
			}
		case b.id <= subSizeBase && b.id > subSizeBase-10:
			i := subSizeBase - b.id
			if annSizes[i] == a.cur.LW {
				dibFill(dib, sw, sh, b.x, b.y, b.x+b.w, b.y+b.h, 0x4A, 0x4A, 0x4A)
			}
			d := annSizes[i] + 3
			dibDot(dib, sw, sh, b.x+b.w/2, b.y+b.h/2, d, 0xE6, 0xE6, 0xE6)
		case b.id == actBlurAll:
			dibFill(dib, sw, sh, b.x, b.y, b.x+b.w, b.y+b.h, 0x3A, 0x3A, 0x3A)
			dibStrokeRect(dib, sw, sh, b.x, b.y, b.x+b.w, b.y+b.h, 1, 0x66, 0x66, 0x66)
			drawTextOn(workDC, b.x+6, b.y+7, "一键打码", 0x00FFFFFF)
		}
	}
}

// drawToolIcon 画一个绘图工具图标（浅灰色，居中于按钮）
func drawToolIcon(dib []byte, sw, sh int, b btnArea, t ToolType) {
	ir, ig, ib := byte(0xE6), byte(0xE6), byte(0xE6)
	cx, cy := b.x+b.w/2, b.y+b.h/2
	switch t {
	case ToolRect:
		dibStrokeRect(dib, sw, sh, cx-9, cy-7, cx+9, cy+7, 2, ir, ig, ib)
	case ToolEllipse:
		dibEllipse(dib, sw, sh, cx-9, cy-7, cx+9, cy+7, 2, ir, ig, ib)
	case ToolArrow:
		dibArrow(dib, sw, sh, cx-8, cy+8, cx+8, cy-8, 2, ir, ig, ib)
	case ToolPen:
		dibPenPath(dib, sw, sh, []POINT{
			{int32(cx - 9), int32(cy + 7)}, {int32(cx - 3), int32(cy - 4)},
			{int32(cx + 2), int32(cy + 3)}, {int32(cx + 9), int32(cy - 7)},
		}, 2, ir, ig, ib)
	case ToolMosaic:
		for i := 0; i < 3; i++ {
			for j := 0; j < 3; j++ {
				if (i+j)%2 == 0 {
					x0 := cx - 9 + i*6
					y0 := cy - 9 + j*6
					dibFill(dib, sw, sh, x0, y0, x0+5, y0+5, ir, ig, ib)
				}
			}
		}
	}
}

func drawUndoIcon(dib []byte, sw, sh int, b btnArea) {
	ir, ig, ib := byte(0xE6), byte(0xE6), byte(0xE6)
	cx, cy := b.x+b.w/2, b.y+b.h/2
	// 半圆弧 + 左端箭头
	for i := 0; i <= 24; i++ {
		t := float64(i) / 24 * math.Pi
		dibDot(dib, sw, sh, cx-int(8*math.Cos(t)), cy-int(6*math.Sin(t))+2, 2, ir, ig, ib)
	}
	dibLine(dib, sw, sh, cx-8, cy-4, cx-8, cy+2, 2, ir, ig, ib)
	dibLine(dib, sw, sh, cx-8, cy-4, cx-3, cy-4, 2, ir, ig, ib)
}

func drawCrossIcon(dib []byte, sw, sh int, b btnArea, r, g, bl byte) {
	cx, cy := b.x+b.w/2, b.y+b.h/2
	dibLine(dib, sw, sh, cx-7, cy-7, cx+7, cy+7, 3, r, g, bl)
	dibLine(dib, sw, sh, cx+7, cy-7, cx-7, cy+7, 3, r, g, bl)
}

func drawCheckIcon(dib []byte, sw, sh int, b btnArea, r, g, bl byte) {
	cx, cy := b.x+b.w/2, b.y+b.h/2
	dibLine(dib, sw, sh, cx-8, cy, cx-2, cy+6, 3, r, g, bl)
	dibLine(dib, sw, sh, cx-2, cy+6, cx+9, cy-6, 3, r, g, bl)
}
