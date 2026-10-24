//go:build windows

package screenshot

import (
	"image"
	"image/color"
	"testing"
)

// ============================================================
// 标注编辑器的纯逻辑 / 像素测试（不需要真实屏幕与鼠标）
//
// 覆盖：工具栏布局与命中、图形生命周期（起笔/移动/收笔/撤销）、
// 坐标夹取、DIB 预览绘制与 RGBA 成品绘制两条路径。
// ============================================================

const (
	testSW = 1920
	testSH = 1080
)

func newTestAnnot(sel image.Rectangle) *annotState {
	a := newAnnotState()
	a.show = true
	a.layout(sel, testSW, testSH)
	return a
}

// centerOf 返回一个按钮中心的屏幕坐标，用于模拟点击
func centerOf(b btnArea) (int32, int32) {
	return int32(b.x + b.w/2), int32(b.y + b.h/2)
}

func barBottom(a *annotState) int {
	bottom := a.barY + annBarH
	if a.subVisible && a.subY+annSubH > bottom {
		bottom = a.subY + annSubH
	}
	return bottom
}

func TestLayoutPrefersBelowSelection(t *testing.T) {
	sel := image.Rect(100, 100, 600, 500)
	a := newTestAnnot(sel)

	if a.barY != sel.Max.Y+annMargin {
		t.Fatalf("工具栏应贴在选区下侧外部，got barY=%d want=%d", a.barY, sel.Max.Y+annMargin)
	}
	if a.barX != sel.Min.X {
		t.Fatalf("工具栏左边缘应对齐选区，got barX=%d", a.barX)
	}
	if a.subVisible {
		t.Fatalf("未选中绘图工具时不应展开子工具栏")
	}
	if len(a.toolBtns) != len(annTools) || len(a.actBtns) != 3 {
		t.Fatalf("按钮数量不对 tools=%d acts=%d", len(a.toolBtns), len(a.actBtns))
	}
}

// 选区贴近屏幕底边时，工具栏必须翻到选区内侧，且两条栏都完整可见
func TestLayoutFlipsInsideWhenNoRoom(t *testing.T) {
	sel := image.Rect(100, 100, 600, testSH-30)
	a := newTestAnnot(sel)

	if a.barY >= sel.Max.Y {
		t.Fatalf("放不下时工具栏应进入选区内部，got barY=%d selMax.Y=%d", a.barY, sel.Max.Y)
	}
	if barBottom(a) > testSH {
		t.Fatalf("主工具栏超出屏幕：bottom=%d sh=%d", barBottom(a), testSH)
	}

	a.setTool(ToolRect)
	if !a.subVisible {
		t.Fatalf("选中工具后应展开子工具栏")
	}
	if barBottom(a) > testSH || a.subY < 0 {
		t.Fatalf("选中工具后工具栏整体不在屏内：subY=%d bottom=%d", a.subY, barBottom(a))
	}
}

// 右侧贴边时工具栏不得跑出屏幕
func TestLayoutClampsToScreenWidth(t *testing.T) {
	sel := image.Rect(testSW-200, 100, testSW-20, 500)
	a := newTestAnnot(sel)
	a.setTool(ToolMosaic) // 最宽的一条栏

	if a.barX < 0 || a.barX+a.barW > testSW {
		t.Fatalf("主工具栏横向越界：barX=%d barW=%d sw=%d", a.barX, a.barW, testSW)
	}
	if a.subX < 0 || a.subX+a.subW > testSW {
		t.Fatalf("子工具栏横向越界：subX=%d subW=%d sw=%d", a.subX, a.subW, testSW)
	}
}

// 回归：layoutSub 不得覆盖主工具栏几何（否则命中判定与绘制会错位）
func TestLayoutSubDoesNotClobberMainBar(t *testing.T) {
	a := newTestAnnot(image.Rect(100, 100, 600, 500))
	beforeX, beforeY, beforeW := a.barX, a.barY, a.barW

	a.layoutSub()

	if a.barX != beforeX || a.barY != beforeY || a.barW != beforeW {
		t.Fatalf("layoutSub 改动了主工具栏几何：(%d,%d,%d) -> (%d,%d,%d)",
			beforeX, beforeY, beforeW, a.barX, a.barY, a.barW)
	}
	if a.subY != a.barY+annBarH+2 {
		t.Fatalf("子栏应紧贴主栏下侧，got subY=%d barY=%d", a.subY, a.barY)
	}
	// 子栏按钮的绝对 y 必须与 drawBars 使用的 subY 一致，否则点不到
	for _, b := range a.subBtns {
		if b.y != a.subY+annPad {
			t.Fatalf("子栏按钮 y=%d 与 subY=%d 不匹配", b.y, a.subY)
		}
	}
}

func TestToolbarHitTest(t *testing.T) {
	a := newTestAnnot(image.Rect(100, 100, 600, 500))

	// 工具按钮逐个点击都应被消化
	for i := range a.toolBtns {
		consumed, confirm, cancel := a.onClick(centerOf(a.toolBtns[i]))
		if !consumed || confirm || cancel {
			t.Fatalf("工具按钮 %d 点击未被消化: consumed=%v confirm=%v cancel=%v", i, consumed, confirm, cancel)
		}
	}

	find := func(id int) btnArea {
		for _, b := range a.actBtns {
			if b.id == id {
				return b
			}
		}
		t.Fatalf("未找到动作按钮 id=%d", id)
		return btnArea{}
	}
	if c, confirm, cancel := a.onClick(centerOf(find(actConfirm))); !c || !confirm || cancel {
		t.Fatalf("确认按钮返回 (%v,%v,%v)，应为 (true,true,false)", c, confirm, cancel)
	}
	if c, confirm, cancel := a.onClick(centerOf(find(actCancel))); !c || confirm || !cancel {
		t.Fatalf("取消按钮返回 (%v,%v,%v)，应为 (true,false,true)", c, confirm, cancel)
	}
}

func TestClickOutsideBarsFallsThroughToDrawing(t *testing.T) {
	a := newTestAnnot(image.Rect(100, 100, 600, 500))
	a.setTool(ToolRect)

	x, y := int32(300), int32(300)
	if consumed, _, _ := a.onClick(x, y); consumed {
		t.Fatalf("选区空白处的点击不应被工具栏吞掉，否则无法起笔")
	}
	if !a.canDraw(x, y) {
		t.Fatalf("选中工具后选区空白处应可起笔")
	}
	if a.canDraw(int32(a.sel.Max.X+5), y) {
		t.Fatalf("选区外不应起笔")
	}
	// 工具栏挤进选区时，底板上不起笔
	a2 := newTestAnnot(image.Rect(100, 100, 600, testSH - 30))
	a2.setTool(ToolRect)
	if a2.canDraw(int32(a2.barX+annBtn/2), int32(a2.barY+annBtn/2)) {
		t.Fatalf("工具栏底板上不应起笔")
	}
}

// 点在按钮缝隙（底板空白）必须被消化为无操作。
// 否则会落到 windowProc 的“开始新拖框”分支，把选区和已画标注一起冲掉。
func TestClickInBarGapIsConsumed(t *testing.T) {
	a := newTestAnnot(image.Rect(100, 100, 600, 500))
	a.setTool(ToolRect)

	// 主栏缝：两个工具按钮之间
	mainGapX := a.toolBtns[1].x - annGap/2
	mainGapY := a.toolBtns[1].y + annBtn/2
	// 子栏缝：两个色块之间
	subGapX := a.subBtns[len(a.subBtns)-1].x - annGap/2
	subGapY := a.subBtns[len(a.subBtns)-1].y + annBtn/2

	for _, p := range [][2]int{{mainGapX, mainGapY}, {subGapX, subGapY}} {
		x, y := int32(p[0]), int32(p[1])
		for _, b := range append(append(append([]btnArea{}, a.toolBtns...), a.actBtns...), a.subBtns...) {
			if b.hit(x, y) {
				t.Fatalf("测试前提：(%d,%d) 不应命中任何按钮", x, y)
			}
		}
		consumed, confirm, cancel := a.onClick(x, y)
		if !consumed || confirm || cancel {
			t.Fatalf("(%d,%d) 缝隙点击应被消化为无操作，got (%v,%v,%v)", x, y, consumed, confirm, cancel)
		}
		if a.canDraw(x, y) {
			t.Fatalf("(%d,%d) 在工具栏底板上不应起笔", x, y)
		}
	}
	if len(a.shapes) != 0 {
		t.Fatalf("缝隙点击不应产生任何图形，got %d", len(a.shapes))
	}
}

func TestSubBarColorAndSize(t *testing.T) {
	a := newTestAnnot(image.Rect(100, 100, 600, 500))
	a.setTool(ToolPen)

	var colorBtns, sizeBtns []btnArea
	for _, b := range a.subBtns {
		switch {
		case b.id <= subColorBase && b.id > subColorBase-10:
			colorBtns = append(colorBtns, b)
		case b.id <= subSizeBase && b.id > subSizeBase-10:
			sizeBtns = append(sizeBtns, b)
		}
	}
	if len(colorBtns) != len(annPalette) || len(sizeBtns) != len(annSizes) {
		t.Fatalf("子栏按钮数量不对 colors=%d sizes=%d", len(colorBtns), len(sizeBtns))
	}

	a.onClick(centerOf(colorBtns[4]))
	if a.cur.RGB != annPalette[4] {
		t.Fatalf("点击色块 4 未选中该颜色，got %v", a.cur.RGB)
	}
	a.onClick(centerOf(sizeBtns[3]))
	if a.cur.LW != annSizes[3] {
		t.Fatalf("点击粗细 3 未选中该线宽，got %d", a.cur.LW)
	}
}

func TestToolToggleCollapsesSubBar(t *testing.T) {
	a := newTestAnnot(image.Rect(100, 100, 600, 500))
	a.setTool(ToolArrow)
	if !a.subVisible || a.tool != ToolArrow {
		t.Fatalf("选中箭头后应展开子栏，got visible=%v tool=%v", a.subVisible, a.tool)
	}
	// 再点同一个工具 = 取消选择（toolBtns[2] 即 ToolArrow）
	a.onClick(centerOf(a.toolBtns[2]))
	if a.tool != ToolNone || a.subVisible {
		t.Fatalf("再次点击同一工具应收起子栏，got tool=%v visible=%v", a.tool, a.subVisible)
	}
	if a.canDraw(int32(a.sel.Min.X+20), int32(a.sel.Min.Y+20)) {
		t.Fatalf("未选中工具时不应起笔")
	}
}

func TestMosaicBlurAll(t *testing.T) {
	sel := image.Rect(100, 100, 600, 500)
	a := newTestAnnot(sel)
	a.setTool(ToolMosaic)

	var blur *btnArea
	for i := range a.subBtns {
		if a.subBtns[i].id == actBlurAll {
			blur = &a.subBtns[i]
		}
	}
	if blur == nil {
		t.Fatalf("马赛克工具下应出现「一键打码」按钮")
	}
	if consumed, _, _ := a.onClick(centerOf(*blur)); !consumed {
		t.Fatalf("一键打码按钮未被消化")
	}
	if len(a.shapes) != 1 || a.shapes[0].Type != ToolMosaic {
		t.Fatalf("一键打码应追加一个马赛克图形，got %d 个", len(a.shapes))
	}
	s := a.shapes[0]
	if int(s.X1) != sel.Min.X || int(s.Y1) != sel.Min.Y || int(s.X2) != sel.Max.X || int(s.Y2) != sel.Max.Y {
		t.Fatalf("一键打码应覆盖整个选区，got (%d,%d)-(%d,%d)", s.X1, s.Y1, s.X2, s.Y2)
	}

	// 非马赛克工具不应出现该按钮
	a.setTool(ToolRect)
	for _, b := range a.subBtns {
		if b.id == actBlurAll {
			t.Fatalf("矩形工具下不应出现「一键打码」按钮")
		}
	}
}

func TestDrawLifecycleAndUndo(t *testing.T) {
	sel := image.Rect(100, 100, 200, 200)
	a := newTestAnnot(sel)
	a.setTool(ToolRect)

	a.startDraw(120, 120)
	a.moveDraw(150, 160)
	a.endDraw()
	if len(a.shapes) != 1 {
		t.Fatalf("拖出一个矩形后应有 1 个图形，got %d", len(a.shapes))
	}

	// 过小（近似单击）的图形不入库
	a.startDraw(130, 130)
	a.moveDraw(131, 131)
	a.endDraw()
	if len(a.shapes) != 1 {
		t.Fatalf("1px 的碎图不应入库，got %d", len(a.shapes))
	}

	// 越界坐标必须夹进选区
	a.startDraw(0, 0)
	a.moveDraw(5000, 5000)
	a.endDraw()
	if len(a.shapes) != 2 {
		t.Fatalf("越界拖动应收敛为选区满幅矩形，got %d", len(a.shapes))
	}
	last := a.shapes[1]
	if last.X1 != int32(sel.Min.X) || last.Y1 != int32(sel.Min.Y) ||
		last.X2 != int32(sel.Max.X) || last.Y2 != int32(sel.Max.Y) {
		t.Fatalf("越界坐标未夹到选区边界，got (%d,%d)-(%d,%d)", last.X1, last.Y1, last.X2, last.Y2)
	}

	// 撤销按后进先出
	a.undo()
	if len(a.shapes) != 1 || a.shapes[0].X2 != 150 {
		t.Fatalf("撤销删掉了错误的图形，剩余 %+v", a.shapes)
	}
	a.undo()
	a.undo() // 空栈撤销不应 panic
	if len(a.shapes) != 0 {
		t.Fatalf("连续撤销后应为空，got %d", len(a.shapes))
	}
}

func TestPenSamplesPoints(t *testing.T) {
	a := newTestAnnot(image.Rect(0, 0, 500, 500))
	a.setTool(ToolPen)

	a.startDraw(10, 10)
	a.moveDraw(11, 11) // 距离 <2px，应被去重
	a.moveDraw(30, 30)
	a.moveDraw(30, 30) // 重复点，应被去重
	a.endDraw()

	if len(a.shapes) != 1 {
		t.Fatalf("画笔应产出一条轨迹，got %d", len(a.shapes))
	}
	if got := len(a.shapes[0].Pts); got != 2 {
		t.Fatalf("画笔轨迹去重后应剩 2 个点，got %d", got)
	}
}

// resetForNewSel 必须清干净图形并收起工具栏，但保留用户选好的颜色/线宽
func TestResetForNewSel(t *testing.T) {
	a := newTestAnnot(image.Rect(100, 100, 600, 500))
	a.setTool(ToolRect)
	a.startDraw(150, 200)
	a.moveDraw(300, 400)
	a.endDraw()
	if len(a.shapes) == 0 {
		t.Fatalf("前置条件：应先有一个图形")
	}
	a.onClick(centerOf(a.subBtns[0]))
	keepRGB, keepLW := a.cur.RGB, a.cur.LW

	a.resetForNewSel()

	if a.show || a.subVisible || a.tool != ToolNone || len(a.shapes) != 0 || a.drawing {
		t.Fatalf("resetForNewSel 未复位状态 show=%v sub=%v tool=%v shapes=%d drawing=%v",
			a.show, a.subVisible, a.tool, len(a.shapes), a.drawing)
	}
	if a.cur.RGB != keepRGB || a.cur.LW != keepLW {
		t.Fatalf("重新选区应保留用户已选的颜色/线宽，got %v/%d want %v/%d",
			a.cur.RGB, a.cur.LW, keepRGB, keepLW)
	}
}

// ============================================================
// 像素级校验：DIB 预览路径与 RGBA 成品路径都要真的画出东西
// ============================================================

var testShapes = []Shape{
	{Type: ToolRect, X1: 20, Y1: 20, X2: 100, Y2: 80, RGB: [3]byte{0xF5, 0x22, 0x2D}, LW: 3},
	{Type: ToolEllipse, X1: 20, Y1: 20, X2: 100, Y2: 80, RGB: [3]byte{0x1E, 0x9B, 0xFF}, LW: 3},
	{Type: ToolArrow, X1: 20, Y1: 120, X2: 150, Y2: 40, RGB: [3]byte{0xF7, 0xDF, 0x00}, LW: 3},
	{Type: ToolPen, RGB: [3]byte{0x23, 0xC4, 0x7A}, LW: 3,
		Pts: []POINT{{20, 150}, {60, 170}, {120, 150}, {160, 180}}},
	{Type: ToolMosaic, X1: 30, Y1: 30, X2: 90, Y2: 70, RGB: [3]byte{0xFF, 0xFF, 0xFF}, LW: 6},
}

// newPatternDIB 造一块颜色随位置变化的“伪屏幕”缓冲（BGRA，自上而下）。
// 必须非均匀，否则马赛克取均值后与原图无差异，测不出来。
func newPatternDIB(w, h int) []byte {
	dib := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			o := (y*w + x) * 4
			dib[o] = byte(x % 251)
			dib[o+1] = byte(y % 249)
			dib[o+2] = byte((x + y) % 253)
			dib[o+3] = 255
		}
	}
	return dib
}

func countDibDiff(a, b []byte) int {
	n := 0
	for i := 0; i < len(a); i += 4 {
		if a[i] != b[i] || a[i+1] != b[i+1] || a[i+2] != b[i+2] {
			n++
		}
	}
	return n
}

func dibColorAt(dib []byte, sw, x, y int) (byte, byte, byte) {
	o := (y*sw + x) * 4
	return dib[o+2], dib[o+1], dib[o]
}

func TestRenderShapeDIBAllTools(t *testing.T) {
	const w, h = 200, 200
	for _, s := range testShapes {
		s := s
		t.Run(toolName(s.Type), func(t *testing.T) {
			dib := newPatternDIB(w, h)
			base := make([]byte, len(dib))
			copy(base, dib)

			renderShapeDIB(dib, w, h, s)
			if n := countDibDiff(dib, base); n == 0 {
				t.Fatalf("%s 未向 DIB 写入任何像素", toolName(s.Type))
			}
		})
	}
}

func TestRenderShapeDIBRectIsHollowStroke(t *testing.T) {
	const w, h = 200, 200
	red := [3]byte{0xF5, 0x22, 0x2D}
	dib := newPatternDIB(w, h)
	base := make([]byte, len(dib))
	copy(base, dib)

	renderShapeDIB(dib, w, h, Shape{Type: ToolRect, X1: 20, Y1: 20, X2: 100, Y2: 80, RGB: red, LW: 3})

	if r, g, b := dibColorAt(dib, w, 60, 20); r != red[0] || g != red[1] || b != red[2] {
		t.Fatalf("矩形上边框颜色不对，got RGB(%d,%d,%d)", r, g, b)
	}
	if r, g, b := dibColorAt(dib, w, 60, 50); r == red[0] && g == red[1] && b == red[2] {
		t.Fatalf("矩形是空心描边，内部不应被填充")
	}
}

func newPatternRGBA(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: byte(x % 251), G: byte(y % 249), B: byte((x + y) % 253), A: 255})
		}
	}
	return img
}

func countRGBAChange(img *image.RGBA, base []byte) int {
	n := 0
	for i := 0; i < len(img.Pix); i += 4 {
		if img.Pix[i] != base[i] || img.Pix[i+1] != base[i+1] || img.Pix[i+2] != base[i+2] {
			n++
		}
	}
	return n
}

func TestCompositeShapesAllTools(t *testing.T) {
	const w, h = 200, 200
	for _, s := range testShapes {
		s := s
		t.Run(toolName(s.Type), func(t *testing.T) {
			img := newPatternRGBA(w, h)
			base := make([]byte, len(img.Pix))
			copy(base, img.Pix)

			compositeShapes(img, []Shape{s})
			if n := countRGBAChange(img, base); n == 0 {
				t.Fatalf("%s 未改变成品图像任何像素", toolName(s.Type))
			}
		})
	}
}

// 预览与成品的覆盖范围应大致相当（差太多说明两条路径坐标系不一致）
func TestPreviewAndCompositeSimilarCoverage(t *testing.T) {
	const w, h = 200, 200
	s := Shape{Type: ToolRect, X1: 20, Y1: 20, X2: 100, Y2: 80, RGB: [3]byte{0xF5, 0x22, 0x2D}, LW: 4}

	dib := newPatternDIB(w, h)
	dibBase := make([]byte, len(dib))
	copy(dibBase, dib)
	renderShapeDIB(dib, w, h, s)
	nDib := countDibDiff(dib, dibBase)

	img := newPatternRGBA(w, h)
	imgBase := make([]byte, len(img.Pix))
	copy(imgBase, img.Pix)
	compositeShapes(img, []Shape{s})
	nImg := countRGBAChange(img, imgBase)

	if nDib == 0 || nImg == 0 {
		t.Fatalf("两条路径都应写入像素：dib=%d rgba=%d", nDib, nImg)
	}
	ratio := float64(nDib) / float64(nImg)
	if ratio < 0.5 || ratio > 2.0 {
		t.Fatalf("预览与成品覆盖差异过大：dib=%d rgba=%d ratio=%.2f", nDib, nImg, ratio)
	}
}

func toolName(t ToolType) string {
	switch t {
	case ToolRect:
		return "rect"
	case ToolEllipse:
		return "ellipse"
	case ToolArrow:
		return "arrow"
	case ToolPen:
		return "pen"
	case ToolMosaic:
		return "mosaic"
	}
	return "none"
}

// 工具栏绘制不应 panic（字体未初始化时文字自动跳过）
func TestDrawBars(t *testing.T) {
	a := newTestAnnot(image.Rect(100, 100, 600, 500))
	dib := newPatternDIB(testSW, testSH)
	base := make([]byte, len(dib))
	copy(base, dib)

	a.drawBars(dib, testSW, testSH, 0)
	if countDibDiff(dib, base) == 0 {
		t.Fatalf("工具栏未画出任何底板像素")
	}
	// 主栏底板色
	if r, g, b := dibColorAt(dib, testSW, a.barX+2, a.barY+2); r != 0x2B || g != 0x2B || b != 0x2B {
		t.Fatalf("主工具栏底板缺失，got RGB(%d,%d,%d)", r, g, b)
	}

	a.setTool(ToolMosaic)
	a.drawBars(dib, testSW, testSH, 0)
	if r, g, b := dibColorAt(dib, testSW, a.subX+2, a.subY+2); r != 0x2B || g != 0x2B || b != 0x2B {
		t.Fatalf("子工具栏底板缺失，got RGB(%d,%d,%d)", r, g, b)
	}
}

// 未布局时 drawBars 必须安全跳过（防御遮罩层崩溃）
func TestDrawBarsBeforeLayout(t *testing.T) {
	a := newAnnotState()
	a.show = true
	dib := newPatternDIB(100, 100)
	base := make([]byte, len(dib))
	copy(base, dib)

	a.drawBars(dib, 100, 100, 0)
	if countDibDiff(dib, base) != 0 {
		t.Fatalf("未布局时不应绘制任何东西")
	}
}
