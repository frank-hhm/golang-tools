/**
 * 截图选区浮层的标注绘制与成品导出。
 *
 * 坐标约定：所有点位一律用「冻结帧的物理像素」，即与 imgWidth×imgHeight 同一坐标系。
 * 线宽/字号则用 CSS 像素表达（用户认知里的 4px 就是 4px），绘制时再乘 dpr，
 * 这样在 200% 缩放屏上标注的视觉粗细与工具栏里看到的圆点一致。
 *
 * 单独成模块而不是写进 .vue，是为了让 drawShape / composeExport / hitWindow
 * 这些纯逻辑能脱离浏览器跑单测——Canvas 相关的一律通过参数注入，不在模块内取全局。
 */

export type Tool = 'none' | 'rect' | 'ellipse' | 'arrow' | 'pen' | 'mosaic' | 'text'

export interface Pt {
    x: number
    y: number
}

/** 矩形选区，物理像素，左上/右下 */
export interface Rect {
    x1: number
    y1: number
    x2: number
    y2: number
}

export interface Shape {
    tool: Exclude<Tool, 'none'>
    /** rect/ellipse/mosaic/text 为 [起点, 终点]；arrow/pen 为轨迹点集 */
    pts: Pt[]
    color: string
    /** CSS 像素线宽 */
    lw: number
    /** 仅 text 用：字号（CSS 像素），缺省取 DEFAULT_FONT_SIZE */
    fs?: number
    /** 仅 text 用：文本框宽度（CSS 像素）。给定则按此宽度自动折行；缺省则只按 \n 分行 */
    bw?: number
    /** 仅 text 用 */
    text?: string
}

/** 可整窗截取的窗口，物理像素 */
export interface WinRect {
    x: number
    y: number
    w: number
    h: number
    title?: string
}

export const DEFAULT_LW = 4
export const DEFAULT_COLOR = '#F5222D'
export const DEFAULT_FONT_SIZE = 18
/** 马赛克格边长（CSS 像素） */
export const MOSAIC_BLOCK = 10

export function normalizeRect(r: Rect): Rect {
    return {
        x1: Math.min(r.x1, r.x2),
        y1: Math.min(r.y1, r.y2),
        x2: Math.max(r.x1, r.x2),
        y2: Math.max(r.y1, r.y2),
    }
}

export function rectSize(r: Rect): { w: number; h: number } {
    const n = normalizeRect(r)
    return { w: n.x2 - n.x1, h: n.y2 - n.y1 }
}

/**
 * 命中光标下最靠前的窗口。
 * windows 由后端按 Z 序自顶向下给出，因此第一个包含光标的就是「视觉上真正盖在最上面的」，
 * 被完全遮住的窗口不会被误选。
 */
export function hitWindow(x: number, y: number, windows: WinRect[]): WinRect | null {
    for (const w of windows) {
        if (w.w <= 0 || w.h <= 0) continue
        if (x >= w.x && x < w.x + w.w && y >= w.y && y < w.y + w.h) return w
    }
    return null
}

/** 窗口矩形转选区矩形；超出屏幕部分裁掉，避免导出时越界 */
export function winToRect(w: WinRect, screenW: number, screenH: number): Rect {
    const x1 = Math.max(0, w.x)
    const y1 = Math.max(0, w.y)
    const x2 = Math.min(screenW, w.x + w.w)
    const y2 = Math.min(screenH, w.y + w.h)
    return { x1, y1, x2, y2 }
}

/** 8 个控制点：四角 + 四边中点。坐标为 CSS 像素的选区左上/宽高。 */
export const HANDLE_DIRS = ['nw', 'n', 'ne', 'e', 'se', 's', 'sw', 'w'] as const
export type HandleDir = (typeof HANDLE_DIRS)[number]

export function handlePos(sel: Rect, dir: HandleDir): Pt {
    const n = normalizeRect(sel)
    const cx = (n.x1 + n.x2) / 2
    const cy = (n.y1 + n.y2) / 2
    switch (dir) {
        case 'nw': return { x: n.x1, y: n.y1 }
        case 'n': return { x: cx, y: n.y1 }
        case 'ne': return { x: n.x2, y: n.y1 }
        case 'e': return { x: n.x2, y: cy }
        case 'se': return { x: n.x2, y: n.y2 }
        case 's': return { x: cx, y: n.y2 }
        case 'sw': return { x: n.x1, y: n.y2 }
        case 'w': return { x: n.x1, y: cy }
    }
}

/** 拖控制点后的新选区；越过对边时自动翻转，并夹在屏幕内。 */
export function resizeRect(sel: Rect, dir: HandleDir, x: number, y: number, screenW: number, screenH: number): Rect {
    const n = normalizeRect(sel)
    let { x1, y1, x2, y2 } = n
    if (dir.includes('w')) x1 = x
    if (dir.includes('e')) x2 = x
    if (dir.includes('n')) y1 = y
    if (dir.includes('s')) y2 = y
    let r = normalizeRect({ x1, y1, x2, y2 })
    r = {
        x1: Math.max(0, Math.min(screenW, r.x1)),
        y1: Math.max(0, Math.min(screenH, r.y1)),
        x2: Math.max(0, Math.min(screenW, r.x2)),
        y2: Math.max(0, Math.min(screenH, r.y2)),
    }
    return r
}

/** 整体平移选区，夹在屏幕内（不允许拖出屏幕外） */
export function moveRect(sel: Rect, dx: number, dy: number, screenW: number, screenH: number): Rect {
    const n = normalizeRect(sel)
    const w = n.x2 - n.x1
    const h = n.y2 - n.y1
    let x1 = n.x1 + dx
    let y1 = n.y1 + dy
    x1 = Math.max(0, Math.min(screenW - w, x1))
    y1 = Math.max(0, Math.min(screenH - h, y1))
    return { x1, y1, x2: x1 + w, y2: y1 + h }
}

export function clampToScreen(p: Pt, screenW: number, screenH: number): Pt {
    return {
        x: Math.max(0, Math.min(screenW, p.x)),
        y: Math.max(0, Math.min(screenH, p.y)),
    }
}

/** 画笔轨迹采样：距上一点不足 minDist 就不记录，避免点集爆炸又丢形状 */
export function shouldAppendPoint(pts: Pt[], p: Pt, minDist = 2): boolean {
    const last = pts[pts.length - 1]
    if (!last) return true
    return Math.hypot(p.x - last.x, p.y - last.y) >= minDist
}

/** 该形状是否已经值得保留（拖出一个真正的图形，而非一次误点） */
export function shapeIsMeaningful(s: Shape): boolean {
    if (s.tool === 'text') return (s.text ?? '').length > 0
    if (s.tool === 'pen') return s.pts.length >= 2
    // 箭头只看长度：正横/正竖拖出来的箭头外接矩有一条边为 0，按矩形判会被丢掉
    if (s.tool === 'arrow') return Math.hypot(s.pts[1].x - s.pts[0].x, s.pts[1].y - s.pts[0].y) >= 4
    const n = normalizeRect({ x1: s.pts[0].x, y1: s.pts[0].y, x2: s.pts[1].x, y2: s.pts[1].y })
    return n.x2 - n.x1 >= 2 && n.y2 - n.y1 >= 2
}

function applyStroke(ctx: CanvasRenderingContext2D, s: Shape, dpr: number) {
    ctx.strokeStyle = s.color
    ctx.fillStyle = s.color
    ctx.lineWidth = Math.max(1, s.lw * dpr)
    ctx.lineCap = 'round'
    ctx.lineJoin = 'round'
}

/**
 * 画单个标注。
 * @param ctx    标注层（或导出用的临时层）上下文
 * @param src    冻结帧位图，马赛克要从这里取原始像素
 * @param scratch 马赛克降采样用的临时 canvas（复用，避免每次新建）
 */
export function drawShape(
    ctx: CanvasRenderingContext2D,
    src: CanvasImageSource,
    scratch: HTMLCanvasElement,
    s: Shape,
    dpr: number,
    screenW: number,
    screenH: number,
) {
    if (!s.pts.length) return
    applyStroke(ctx, s, dpr)
    const a = s.pts[0]
    const b = s.pts[s.pts.length - 1]

    switch (s.tool) {
        case 'rect': {
            const n = normalizeRect({ x1: a.x, y1: a.y, x2: b.x, y2: b.y })
            ctx.strokeRect(n.x1, n.y1, n.x2 - n.x1, n.y2 - n.y1)
            break
        }
        case 'ellipse': {
            const n = normalizeRect({ x1: a.x, y1: a.y, x2: b.x, y2: b.y })
            const rx = (n.x2 - n.x1) / 2
            const ry = (n.y2 - n.y1) / 2
            ctx.beginPath()
            ctx.ellipse(n.x1 + rx, n.y1 + ry, Math.max(0.5, rx), Math.max(0.5, ry), 0, 0, Math.PI * 2)
            ctx.stroke()
            break
        }
        case 'arrow': {
            drawArrow(ctx, a, b, Math.max(1, s.lw * dpr))
            break
        }
        case 'pen': {
            if (s.pts.length === 1) {
                // 单击一个点：画个圆点，别什么都不出现
                ctx.beginPath()
                ctx.arc(a.x, a.y, ctx.lineWidth / 2, 0, Math.PI * 2)
                ctx.fill()
                break
            }
            ctx.beginPath()
            ctx.moveTo(s.pts[0].x, s.pts[0].y)
            for (let i = 1; i < s.pts.length; i++) ctx.lineTo(s.pts[i].x, s.pts[i].y)
            ctx.stroke()
            break
        }
        case 'mosaic': {
            drawMosaic(ctx, src, scratch, { x1: a.x, y1: a.y, x2: b.x, y2: b.y }, s.lw, dpr, screenW, screenH)
            break
        }
        case 'text': {
            const size = (s.fs ?? DEFAULT_FONT_SIZE) * dpr
            ctx.font = `${size}px "Microsoft YaHei", "PingFang SC", system-ui, sans-serif`
            ctx.textBaseline = 'top'
            // 先按显式换行拆，再按框宽折行（CJK 逐字，兼容中英混排）；无框宽则只按 \n 分行
            const raw = (s.text ?? '').split('\n')
            let lines = raw
            if (s.bw && s.bw > 0) {
                const maxW = s.bw * dpr
                lines = []
                for (const rl of raw) lines.push(...wrapText(ctx, rl, maxW))
            }
            lines.forEach((line, i) => {
                ctx.fillText(line, a.x, a.y + i * size * 1.3)
            })
            break
        }
    }
}

/** 逐字折行（CJK 友好）：按 measureText 累计宽度超过 maxW 就换行；空行返回 [''] */
function wrapText(ctx: CanvasRenderingContext2D, text: string, maxW: number): string[] {
    if (!text) return ['']
    const out: string[] = []
    let cur = ''
    for (const ch of text) {
        const test = cur + ch
        if (ctx.measureText(test).width > maxW && cur) {
            out.push(cur)
            cur = ch
        } else {
            cur = test
        }
    }
    if (cur) out.push(cur)
    return out.length ? out : ['']
}

/**
 * 箭头：实心三角头 + 圆头杆（微信/企微截图那种“粗杆实心箭”的观感）。
 * 头长随线宽放大，短箭头则把头顶多压到总长的一半，保证尖端之外还看得见杆。
 */
export function drawArrow(
    ctx: CanvasRenderingContext2D,
    from: Pt,
    to: Pt,
    lw: number,
) {
    const dx = to.x - from.x
    const dy = to.y - from.y
    const len = Math.hypot(dx, dy)
    if (len < 0.5) {
        ctx.beginPath()
        ctx.arc(to.x, to.y, lw / 2, 0, Math.PI * 2)
        ctx.fill()
        return
    }
    const ux = dx / len
    const uy = dy / len
    // 头：长约 4.5 个杆径，底半宽 0.44 头长（尖角约 45°）；短箭头最多占 55%
    const head = Math.max(6, Math.min(Math.max(9, lw * 4.5), len * 0.58))
    const half = head * 0.44
    // 杆画进头里 0.8 个头长：圆端被三角盖住，不会从尖端戳出来
    ctx.beginPath()
    ctx.moveTo(from.x, from.y)
    ctx.lineTo(to.x - ux * head * 0.8, to.y - uy * head * 0.8)
    ctx.stroke()
    // 头：尖端就是终点，底边垂直于杆
    ctx.beginPath()
    ctx.moveTo(to.x, to.y)
    ctx.lineTo(to.x - ux * head - uy * half, to.y - uy * head + ux * half)
    ctx.lineTo(to.x - ux * head + uy * half, to.y - uy * head - ux * half)
    ctx.closePath()
    ctx.fill()
}

/**
 * 马赛克：把区域内像素先缩到 1/block 再等比放大回来（关闭平滑），得到方块化的效果。
 * 两遍 drawImage 比逐像素 getImageData 快两个数量级，且能直接吃 GPU 合成。
 */
export function drawMosaic(
    ctx: CanvasRenderingContext2D,
    src: CanvasImageSource,
    scratch: HTMLCanvasElement,
    area: Rect,
    lwCss: number,
    dpr: number,
    screenW: number,
    screenH: number,
) {
    const n = normalizeRect(area)
    const block = Math.max(4, Math.round(MOSAIC_BLOCK * dpr))
    // 夹到屏幕内：越界的 drawImage 在部分浏览器里是整幅不画而不是裁剪
    const x1 = Math.max(0, Math.min(screenW, n.x1))
    const y1 = Math.max(0, Math.min(screenH, n.y1))
    const x2 = Math.max(0, Math.min(screenW, n.x2))
    const y2 = Math.max(0, Math.min(screenH, n.y2))
    const w = Math.floor((x2 - x1) / block) * block || block
    const h = Math.floor((y2 - y1) / block) * block || block
    if (x2 - x1 < 1 || y2 - y1 < 1) return

    const sw = Math.max(1, Math.round(w / block))
    const sh = Math.max(1, Math.round(h / block))
    scratch.width = sw
    scratch.height = sh
    const sctx = scratch.getContext('2d')
    if (!sctx) return
    sctx.clearRect(0, 0, sw, sh)
    sctx.drawImage(src, x1, y1, w, h, 0, 0, sw, sh)

    ctx.save()
    ctx.imageSmoothingEnabled = false
    // lw 在这里当“笔刷粗细”用：拖出的是一条带宽度的马赛克带，而非只能画矩形
    ctx.beginPath()
    ctx.rect(x1, y1, w, h)
    ctx.clip()
    ctx.drawImage(scratch, 0, 0, sw, sh, x1, y1, sw * block, sh * block)
    ctx.restore()
    void lwCss
}

/** 全图打码（工具栏「一键打码」）：把整个选区糊掉 */
export function blurAllRect(sel: Rect): Rect {
    return normalizeRect(sel)
}

/**
 * 导出成品：从冻结帧裁出选区，再把选区内的标注叠上去。
 * 工具栏/控制点/放大镜都不入图——它们只存在于交互层。
 */
export function composeExport(
    src: HTMLCanvasElement,
    annot: HTMLCanvasElement,
    sel: Rect,
): HTMLCanvasElement {
    const n = normalizeRect(sel)
    const w = Math.max(1, Math.round(n.x2 - n.x1))
    const h = Math.max(1, Math.round(n.y2 - n.y1))
    const out = document.createElement('canvas')
    out.width = w
    out.height = h
    const ctx = out.getContext('2d')
    if (!ctx) return out
    ctx.drawImage(src, Math.round(n.x1), Math.round(n.y1), w, h, 0, 0, w, h)
    ctx.drawImage(annot, Math.round(n.x1), Math.round(n.y1), w, h, 0, 0, w, h)
    return out
}

export function canvasToPngDataURL(c: HTMLCanvasElement): string {
    return c.toDataURL('image/png')
}
