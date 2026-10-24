<template>
    <div v-if="!ready" class="cap-loading"></div>
    <div v-else class="cap" :class="{ 'cap-draw': tool !== 'none' }" @mousedown="onDown" @contextmenu.prevent="onCancel">
        <!-- 冻结帧：整屏背景。浮层不透明，所以不需要 Wails 的透明窗口支持 -->
        <div class="cap-bg" :style="{ backgroundImage: `url(${imgUrl})` }"></div>

        <!-- 压暗层：用一个 box-shadow 在「镂空矩形」外圈铺满全屏，比切四块矩形省事且无缝 -->
        <div v-if="!hole" class="cap-dim"></div>
        <div v-else class="cap-hole" :style="holeStyle"></div>

        <!-- 标注层：物理像素 1:1，指针事件全部交给根节点统一分发 -->
        <canvas ref="annotRef" class="cap-annot" :width="physW" :height="physH"></canvas>

        <!-- 选区边框与控制点。放在标注层之上，否则控制点会被标注挡住点不到；
             不绑 mousedown——事件会冒泡到根节点统一处理，重复绑会一次按下触发两遍 -->
        <div v-if="sel" class="cap-sel" :style="selStyle">
            <div v-for="d in HANDLE_DIRS" :key="d" class="cap-handle" :class="'h-' + d" :style="handleStyle(d)"
                @mousedown.stop="onHandleMouseDown(d, $event)"></div>
        </div>

        <!-- 悬停窗口名：整窗截取前告诉用户即将拿到的是哪个应用 -->
        <div v-if="!sel && hoverTitle" class="cap-tag" :style="winNameStyle">{{ hoverTitle }}</div>
        <div v-if="sel" class="cap-tag" :style="sizeStyle">{{ pxW }} × {{ pxH }}</div>

        <!-- 放大镜：只在还没定框时出现，定框后让位给工具栏 -->
        <div v-if="showMagnifier" class="cap-mag" :style="magStyle">
            <canvas ref="magRef" class="cap-mag-canvas" width="150" height="150"></canvas>
            <div class="cap-mag-info">
                <span>{{ magCoord }}</span>
                <span class="cap-mag-rgb"><i :style="{ background: hoverColor }"></i>{{ hoverColor }}</span>
            </div>
        </div>

        <!-- 文字标注框：就地可编辑，拖边框移动、拖四角缩放（如图）。真 textarea 才有输入法候选窗；
             整块 .stop 掉 mousedown，避免在框内点击定位光标时冒泡到根 onDown 把框提前提交 -->
        <div v-if="editBox" class="cap-textbox" :style="boxStyle" @mousedown.stop="onBoxDown">
            <textarea ref="textRef" v-model="editBox.text" class="cap-textarea" :style="areaStyle"
                spellcheck="false" @contextmenu.stop.prevent @keydown.esc.stop.prevent="commitBox"></textarea>
            <div v-for="d in BOX_HANDLES" :key="d" class="cap-box-h" :class="'bh-' + d"
                @mousedown.stop="onBoxHandle(d, $event)"></div>
        </div>

        <!-- 工具栏：贴在选区右下角 -->
        <div v-if="sel && !toolbarHidden" ref="barRef" class="cap-bar" :style="barStyle" @mousedown.stop>
            <div class="cap-main">
                <a-tooltip v-for="t in TOOLS" :key="t.tool" :content="t.tip" :mouse-enter-delay="600">
                    <a-button size="mini" type="text" @click="pickTool(t.tool)">
                        <component :is="t.icon" v-if="t.icon" />
                        <!-- Arco 没有空心矩形/椭圆/T 字，按其 48 网格 + 4px 圆头描边补齐 -->
                        <svg v-else class="cap-svg" viewBox="0 0 48 48">
                            <rect v-if="t.shape === 'rect'" x="7" y="11" width="34" height="26" rx="3" />
                            <ellipse v-if="t.shape === 'ellipse'" cx="24" cy="24" rx="17" ry="13" />
                            <path v-if="t.shape === 'text'" d="M10 12h28M24 12v26" />
                        </svg>
                    </a-button>
                </a-tooltip>
                <i class="cap-sep"></i>
                <a-tooltip content="撤销 (Ctrl+Z)" :mouse-enter-delay="600">
                    <a-button size="mini" type="text" :disabled="!shapes.length" @click="undo"><icon-undo /></a-button>
                </a-tooltip>
                <a-tooltip content="保存 PNG 文件" :mouse-enter-delay="600">
                    <a-button size="mini" type="text" @click="saveFile"><icon-save /></a-button>
                </a-tooltip>
                <a-tooltip content="打开预览窗口" :mouse-enter-delay="600">
                    <a-button size="mini" type="text" @click="finish(true)"><icon-eye /></a-button>
                </a-tooltip>
                <a-tooltip content="图钉置顶" :mouse-enter-delay="600">
                    <a-button size="mini" type="text" @click="pinIt"><icon-pushpin /></a-button>
                </a-tooltip>
                <a-tooltip content="提取文字" :mouse-enter-delay="600">
                    <a-button size="mini" type="text" :loading="ocrLoading" @click="runOcr"><icon-translate /></a-button>
                </a-tooltip>
                <i class="cap-sep"></i>
                <a-tooltip content="取消截图 (Esc)" :mouse-enter-delay="600">
                    <a-button size="mini" type="text" status="danger" @click="onCancel"><icon-close /></a-button>
                </a-tooltip>
                <a-tooltip content="复制并关闭 (Enter)" :mouse-enter-delay="600">
                    <a-button size="mini" type="text" status="success" @click="finish(false)"><icon-check /></a-button>
                </a-tooltip>
            </div>

            <!-- 子栏：文字用字号 小/中/大，其余图形用线宽圆点；马赛克不需要子栏 -->
            <div v-if="showSub" class="cap-sub">
                <template v-if="tool === 'text'">
                    <a-button v-for="f in FONT_SIZES" :key="'fs' + f.v" size="mini"
                        :type="fontSize === f.v ? 'primary' : 'text'" @click="setFontSize(f.v)">{{ f.label }}</a-button>
                </template>
                <template v-else>
                    <a-button v-for="(w, i) in LINE_WIDTHS" :key="'lw' + i" size="mini"
                        :type="lw === w ? 'primary' : 'text'" @click="lw = w">
                        <i class="cap-dot" :style="{ width: 4 + i * 3 + 'px', height: 4 + i * 3 + 'px' }"></i>
                    </a-button>
                </template>
                <i class="cap-sep cap-sep-dark"></i>
                <a-button v-for="c in COLORS" :key="'cl' + c" size="mini" type="text"
                    :class="{ 'cap-color-on': color === c }" @click="setColor(c)">
                    <i class="cap-swatch" :style="{ background: c }"></i>
                </a-button>
            </div>
        </div>

        <!-- 提取文字结果：就地弹窗展示，可编辑后复制；不关浮层，方便继续操 -->
        <a-modal v-model:visible="ocrVisible" title="提取文字" :width="560" :footer="false" :mask-closable="true"
            draggable>
            <a-spin :loading="ocrLoading" class="cap-ocr-spin">
                <a-textarea v-model="ocrText" :auto-size="{ minRows: 6, maxRows: 14 }"
                    :placeholder="ocrLoading ? '正在识别…' : '识别结果将显示在这里…'" class="cap-ocr-text" />
            </a-spin>
            <div class="cap-ocr-foot">
                <span class="cap-ocr-hint">{{ ocrLoading ? "识别中…" : ocrText ? `共 ${ocrText.length} 字` : "" }}</span>
                <a-button type="primary" size="small" :disabled="!ocrText || ocrLoading" @click="copyOcr">
                    <icon-copy /> 复制文本
                </a-button>
            </div>
        </a-modal>
    </div>
</template>

<script setup lang="ts">
/**
 * 截图选区编辑浮层。
 *
 * 由后端 OpenCaptureOverlay 打开：Go 先冻结主屏一帧、枚举可整窗截取的窗口，
 * 再建这个全屏无边框置顶窗，等本页回报就绪才显示。页面把冻结帧当背景铺满，
 * 视觉上等价于「屏幕被定住」，所以既不需要真正的透明窗口，也不会截到自己。
 *
 * 坐标只有两套：DOM 事件给的是 CSS 像素，一切存储与绘制都用物理像素。
 * 换算系数 dpr 由「图片宽 ÷ 视口宽」实测得出，比读系统 DPI 更可靠——它天然
 * 包含了浏览器缩放，而导出成品图必须落在图片像素上。
 */
import { computed, getCurrentInstance, nextTick, onBeforeUnmount, onMounted, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { IconArrowRise, IconCheck, IconClose, IconEye, IconMosaic, IconPen, IconSave, IconUndo } from "@arco-design/web-vue/es/icon";
import {
    DEFAULT_COLOR, DEFAULT_FONT_SIZE, DEFAULT_LW, HANDLE_DIRS, type HandleDir, type Pt, type Rect, type Shape,
    type Tool, type WinRect, canvasToPngDataURL, clampToScreen, composeExport, drawShape, handlePos, hitWindow,
    moveRect, normalizeRect, rectSize, resizeRect, shapeIsMeaningful, shouldAppendPoint, winToRect,
} from "./captureDraw";

const {
    proxy: { $GoTools },
} = getCurrentInstance() as any;

/** 与截图预览页约定的共享存储键（同源多窗口共享 localStorage） */
const STORAGE_KEY = "tool:screenshot";

interface Frame {
    image: string;
    w: number;
    h: number;
    dpr: number;
    cursorX: number;
    cursorY: number;
    windows: WinRect[];
}

/* ---------------- 会话数据 ---------------- */
const frame = ref<Frame | null>(null);
const ready = ref(false);
const dpr = ref(1);
const imgUrl = ref("");
const srcCanvas = ref<HTMLCanvasElement | null>(null);
const annotRef = ref<HTMLCanvasElement | null>(null);
const magRef = ref<HTMLCanvasElement | null>(null);
const textRef = ref<HTMLTextAreaElement | null>(null);
const barRef = ref<HTMLElement | null>(null);

const sel = ref<Rect | null>(null);
const hover = ref<WinRect | null>(null);
const cursor = ref<Pt>({ x: 0, y: 0 });
const picking = ref(false);
const liveDrag = ref<Rect>({ x1: 0, y1: 0, x2: 0, y2: 0 });
const tool = ref<Tool>("none");
const color = ref(DEFAULT_COLOR);
const lw = ref(DEFAULT_LW);
const fontSize = ref(DEFAULT_FONT_SIZE);
const shapes = ref<Shape[]>([]);
const live = ref<Shape | null>(null);
// 文字标注框：x/y/w/h 为文本内容区（物理像素），fs 为 CSS 字号，color 为文字色
const editBox = ref<{ x: number; y: number; w: number; h: number; fs: number; color: string; text: string } | null>(null);
const toolbarHidden = ref(false);
const hoverColor = ref("#000000");

/** pick=还没定框，edit=已定框可标注 */
const phase = ref<"pick" | "edit">("pick");

// 按下后的四种意图，用普通变量即可：只在 mousedown→mouseup 之间存活
type Press =
    | { kind: "none" }
    | { kind: "new"; from: Pt }
    | { kind: "move"; from: Pt; base: Rect }
    | { kind: "handle"; dir: HandleDir; base: Rect }
    | { kind: "draw" };
let press: Press = { kind: "none" };
/** 马赛克降采样用的临时画布，复用避免每次新建 */
let scratch: HTMLCanvasElement | null = null;

const physW = computed(() => frame.value?.w ?? 0);
const physH = computed(() => frame.value?.h ?? 0);

/* ---------------- 尺寸与坐标换算 ---------------- */
const toCss = (v: number) => v / dpr.value;
const toPhys = (v: number) => v * dpr.value;
function cssRect(r: Rect) {
    const n = normalizeRect(r);
    return { left: toCss(n.x1), top: toCss(n.y1), width: toCss(n.x2 - n.x1), height: toCss(n.y2 - n.y1) };
}

const pxW = computed(() => (sel.value ? Math.round(rectSize(sel.value).w) : 0));
const pxH = computed(() => (sel.value ? Math.round(rectSize(sel.value).h) : 0));

const hole = computed<Rect | null>(() => {
    if (sel.value) return sel.value;
    if (picking.value) return liveDrag.value;
    if (hover.value && frame.value) return winToRect(hover.value, frame.value.w, frame.value.h);
    return null;
});
const holeStyle = computed(() => {
    const r = cssRect(hole.value as Rect);
    return { left: r.left + "px", top: r.top + "px", width: r.width + "px", height: r.height + "px" };
});
const selStyle = computed(() => {
    const r = cssRect(sel.value as Rect);
    return { left: r.left + "px", top: r.top + "px", width: r.width + "px", height: r.height + "px" };
});
const hoverTitle = computed(() => hover.value?.title || "");
const winNameStyle = computed(() => {
    const f = frame.value;
    const w = hover.value;
    if (!f || !w) return {};
    const r = cssRect(winToRect(w, f.w, f.h));
    return { left: r.left + "px", top: Math.max(0, r.top - 24) + "px" };
});
const sizeStyle = computed(() => {
    const r = cssRect(sel.value as Rect);
    return { left: r.left + "px", top: Math.max(0, r.top - 24) + "px" };
});

function handleStyle(d: HandleDir) {
    // 控制点是 .cap-sel 的子元素，而 .cap-sel 自己 position:absolute，所以它们的
    // 定位包含块是选区而非屏幕：必须用「相对选区左上角」的偏移。之前直接拿绝对
    // 屏幕坐标当 left/top，等于把选区位置又叠加了一遍，八个点全部跑偏。
    const s = sel.value as Rect;
    const n = normalizeRect(s);
    const p = handlePos(s, d);
    // 居中交给 transform: translate(-50%,-50%)，这里只给角/边中点的相对坐标
    return { left: toCss(p.x - n.x1) + "px", top: toCss(p.y - n.y1) + "px" };
}

/* ---------------- 工具栏定位：贴选区右下，但尽量放选区外侧 ---------------- */
// 用 right 定位而不是算 left，栏宽随内容伸缩，加按钮也不用回来改常量。
// 垂直优先放选区下方；下方放不下翻到上方（仍是选区外部）。绝不默认
// 折回选区内侧——那样工具栏会盖住 SE / 角控制点，拖控制点的 mousedown 会被
// 栏上的 @mousedown.stop 吃掉，表现为“选区边框拖到按钮异常”。
const barStyle = computed(() => {
    const f = frame.value;
    if (!f) return {};
    const r = cssRect(sel.value as Rect);
    const vw = toCss(f.w);
    const vh = toCss(f.h);
    const sub = tool.value !== "none";
    const gh = MAIN_H + (sub ? SUB_H + 4 : 0);
    const right = Math.max(6, vw - (r.left + r.width));
    const below = r.top + r.height + BAR_GAP;
    const above = r.top - BAR_GAP - gh;
    let top: number;
    if (below + gh <= vh - 4) top = below;
    else if (above >= 4) top = above;
    // 上下都放不下（选区几乎顶满整屏）：只能贴到选区底部内侧，此时
    // 至少不遮挡拖拽，因为选区已足够大、控制点不在工具栏那一条里
    else top = Math.max(4, Math.min(vh - gh - 4, r.top + r.height - gh - 2));
    return { right: right + "px", top: top + "px" };
});
const MAIN_H = 38;
const SUB_H = 34;
const BAR_GAP = 8;

/* ---------------- 放大镜 ---------------- */
const MAG_N = 15;
const MAG_BOX = 150;
const showMagnifier = computed(() => phase.value === "pick" && !!srcCanvas.value && !editBox.value);
const magStyle = computed(() => {
    const f = frame.value;
    if (!f) return {};
    let x = cursor.value.x + 22;
    let y = cursor.value.y + 26;
    if (x + MAG_BOX + 20 > toCss(f.w)) x = cursor.value.x - MAG_BOX - 34;
    if (y + MAG_BOX + 46 > toCss(f.h)) y = cursor.value.y - MAG_BOX - 60;
    return { left: Math.max(2, x) + "px", top: Math.max(2, y) + "px" };
});
const magCoord = computed(() => {
    const f = frame.value;
    if (!f) return "";
    return `${Math.round(toPhys(cursor.value.x))},${Math.round(toPhys(cursor.value.y))}`;
});

/* ---------------- 工具与配色 ---------------- */
type ToolDef = { tool: Tool; tip: string; icon?: any; shape?: "rect" | "ellipse" | "text" };
const TOOLS: ToolDef[] = [
    { tool: "rect", tip: "矩形", shape: "rect" },
    { tool: "ellipse", tip: "椭圆", shape: "ellipse" },
    { tool: "arrow", tip: "箭头", icon: IconArrowRise },
    { tool: "pen", tip: "画笔", icon: IconPen },
    { tool: "mosaic", tip: "马赛克", icon: IconMosaic },
    { tool: "text", tip: "文字", shape: "text" },
];
const LINE_WIDTHS = [2, 4, 7, 11];
const COLORS = ["#165DFF", "#00B42A", "#FADC19", "#86909C", "#FFFFFF", "#F53F3F"];
// 文字字号：中 与 DEFAULT_FONT_SIZE 对齐，默认选中“中”
const FONT_SIZES = [
    { label: "小", v: 13 },
    { label: "中", v: DEFAULT_FONT_SIZE },
    { label: "大", v: 26 },
];
// 只有需要线宽/字号/颜色的工具才弹子栏；马赛克固定方块尺寸，不弹子栏
const SUB_TOOLS: Tool[] = ["rect", "ellipse", "arrow", "pen"];
const showSub = computed(() => tool.value === "text" || SUB_TOOLS.includes(tool.value));

function pickTool(t: Tool) {
    if (editBox.value) commitBox();
    tool.value = tool.value === t ? "none" : t;
}
function undo() {
    shapes.value.pop();
    redraw();
}

/* ---------------- 启动 ---------------- */
/**
 * 回写诊断日志。
 *
 * 浮层是隐藏创建、由前端自己点亮的全屏窗口：一旦 JS 在 CaptureReady 前抛错，
 * 用户只会看到黑屏或干脆没反应，而后端日志一片干净。没这条通道就只能靠猜。
 */
function report(note: string) {
    try {
        $GoTools?.CaptureLog?.(note);
    } catch {
        /* 诊断通道本身不可用时不能再影响主流程 */
    }
}

function installErrorReport() {
    window.addEventListener("error", (e) => report(`error: ${e.message} @ ${e.filename}:${e.lineno}`));
    window.addEventListener("unhandledrejection", (e: PromiseRejectionEvent) => {
        const r = e.reason as any;
        report(`reject: ${r?.stack || r?.message || r}`);
    });
}

/** offNewFrame 复用浮层时后端推送“有新冻结帧”事件的解绑句柄 */
let offNewFrame: any = null;
/** starting 防止冷挂载与 newframe 事件几乎同时触发时重复初始化 */
let starting = false;

onMounted(async () => {
    installErrorReport();
    // 事件与鼠标/键盘绑定只做一次：复用浮层时页面不会重新 mount，
    // 若放进 beginCapture 会随每次截图累积重复监听器。
    bindEvents();
    offNewFrame = Events.On("capture:newframe", () => {
        void beginCapture();
    });
    // 冷建路径：后端在开窗前已备好冻结帧，首挂载直接取帧即可。
    await beginCapture();
});

/**
 * 取回后端准备好的冻结帧并初始化一次截图会话。
 *
 * 冷建与复用两条路径都走这里：冷建时由 onMounted 触发一次，复用时由后端的
 * capture:newframe 事件触发。复用会先 resetSession 清掉上一轮的选区/标注，
 * 否则旧框会残留到新画面上。取不到帧（隐藏待命 / 帧未就绪）时静默返回，
 * 交给看门狗在超时后销毁坏窗口，不弹错误也不自行收场。
 */
async function beginCapture() {
    if (starting) return;
    starting = true;
    try {
        resetSession();
        let f: Frame | null = null;
        try {
            f = await $GoTools.GetCaptureFrame();
        } catch (e: any) {
            report(`GetCaptureFrame 异常: ${e?.message || e}`);
            f = null;
        }
        if (!f || !f.image) {
            report("无可用冻结帧，等待 capture:newframe");
            return;
        }
        frame.value = f;
        imgUrl.value = f.image;
        measureDpr();
        ready.value = true;
        try {
            await loadImage(f);
        } catch (e: any) {
            report(`画面解码失败: ${e?.message || e}`);
            return;
        }
        cursor.value = { x: toCss(f.cursorX), y: toCss(f.cursorY) };
        hover.value = hitWindow(f.cursorX, f.cursorY, f.windows || []);
        await nextTick();
        drawMagnifier();
        $GoTools.CaptureReady();
        // 显示与 Fullscreen() 之后视口才定型，再测一次 dpr，
        // 否则创建时的尺寸会被当成换算基准，整层布局都会偏
        await raf();
        const before = dpr.value;
        measureDpr();
        if (before !== dpr.value) {
            cursor.value = { x: toCss(f.cursorX), y: toCss(f.cursorY) };
            redraw();
            drawMagnifier();
        }
        report(`就绪 视口${window.innerWidth}x${window.innerHeight} 帧${f.w}x${f.h} `
            + `dpr ${before.toFixed(3)}→${dpr.value.toFixed(3)} 可截窗口${f.windows?.length ?? 0}`);
    } finally {
        starting = false;
    }
}

/** resetSession 清空一次截图会话的全部可变状态，供复用浮层时回到干净起点。 */
function resetSession() {
    frame.value = null;
    ready.value = false;
    imgUrl.value = "";
    srcCanvas.value = null;
    sel.value = null;
    hover.value = null;
    cursor.value = { x: 0, y: 0 };
    picking.value = false;
    liveDrag.value = { x1: 0, y1: 0, x2: 0, y2: 0 };
    tool.value = "none";
    color.value = DEFAULT_COLOR;
    lw.value = DEFAULT_LW;
    fontSize.value = DEFAULT_FONT_SIZE;
    shapes.value = [];
    live.value = null;
    editBox.value = null;
    toolbarHidden.value = false;
    hoverColor.value = "#000000";
    phase.value = "pick";
    press = { kind: "none" };
    boxDrag = null;
    ocrVisible.value = false;
    ocrText.value = "";
    ocrLoading.value = false;
}

function raf() {
    return new Promise<void>((resolve) => requestAnimationFrame(() => resolve()));
}

/**
 * dpr 实测：图片宽 ÷ 视口宽。
 * 窗口尚未显示时 innerWidth 可能还是 0，此时退回后端给的 DPI，等就绪后再测一次。
 */
function measureDpr() {
    const f = frame.value;
    if (!f) return;
    const vw = window.innerWidth;
    dpr.value = vw > 0 && f.w > 0 ? f.w / vw : f.dpr || 1;
}

/** 把冻结帧解到一张 1:1 物理像素画布上：放大镜取色与马赛克取样都要读它的原始像素 */
function loadImage(f: Frame) {
    return new Promise<void>((resolve, reject) => {
        const img = new Image();
        img.onload = () => {
            const c = document.createElement("canvas");
            c.width = f.w;
            c.height = f.h;
            const ctx = c.getContext("2d");
            if (!ctx) return reject(new Error("canvas 2d 上下文不可用"));
            ctx.drawImage(img, 0, 0);
            srcCanvas.value = c;
            scratch = document.createElement("canvas");
            resolve();
        };
        img.onerror = () => reject(new Error("PNG 解码失败"));
        img.src = f.image;
    });
}

onBeforeUnmount(() => {
    unbindEvents();
    if (offNewFrame) {
        try {
            offNewFrame();
        } catch {
            /* ignore */
        }
        offNewFrame = null;
    }
});

/* ---------------- 事件 ---------------- */
function bindEvents() {
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    window.addEventListener("keydown", onKey, true);
    window.addEventListener("dblclick", onDblClick);
    window.addEventListener("resize", onResize);
}
function unbindEvents() {
    window.removeEventListener("mousemove", onMove);
    window.removeEventListener("mouseup", onUp);
    window.removeEventListener("keydown", onKey, true);
    window.removeEventListener("dblclick", onDblClick);
    window.removeEventListener("resize", onResize);
}
function onResize() {
    measureDpr();
    redraw();
}

function evPt(e: MouseEvent): Pt {
    return { x: toPhys(e.clientX), y: toPhys(e.clientY) };
}
function inSel(p: Pt): boolean {
    if (!sel.value) return false;
    const n = normalizeRect(sel.value);
    return p.x >= n.x1 && p.x <= n.x2 && p.y >= n.y1 && p.y <= n.y2;
}

function onDown(e: MouseEvent) {
    const f = frame.value;
    if (!f || e.button !== 0) return;
    if (editBox.value) {
        commitBox();
        return;
    }
    const p = clampToScreen(evPt(e), f.w, f.h);
    // 定框后选中了绘图工具：在选区内按下就是作图
    if (phase.value === "edit" && tool.value !== "none" && inSel(p)) {
        if (tool.value === "text") {
            startBox(p);
            return;
        }
        live.value = { tool: tool.value as Exclude<Tool, "none">, pts: [p, p], color: color.value, lw: lw.value };
        press = { kind: "draw" };
        redraw();
        return;
    }
    if (phase.value === "edit") {
        if (inSel(p)) {
            // 未选中工具时按在选区里 = 挪动选区
            press = { kind: "move", from: p, base: sel.value as Rect };
        } else {
            // 按在选区外 = 重开一个框。必须先清空旧选区：hole 优先返回 sel，
            // 不清的话新拖出的矩形会被旧框盖住，看不到也定不下。
            press = { kind: "new", from: p };
            sel.value = null;
            phase.value = "pick";
            shapes.value = [];
            live.value = null;
            toolbarHidden.value = true;
        }
        redraw();
        return;
    }
    press = { kind: "new", from: p };
    picking.value = false;
    liveDrag.value = { x1: p.x, y1: p.y, x2: p.x, y2: p.y };
}

function onMove(e: MouseEvent) {
    const f = frame.value;
    if (!f) return;
    cursor.value = { x: e.clientX, y: e.clientY };
    const p = evPt(e);
    if (press.kind === "new") {
        picking.value = true;
        liveDrag.value = normalizeRect({ x1: press.from.x, y1: press.from.y, x2: p.x, y2: p.y });
        return;
    }
    if (press.kind === "move") {
        sel.value = moveRect(press.base, p.x - press.from.x, p.y - press.from.y, f.w, f.h);
        redraw();
        return;
    }
    if (press.kind === "handle") {
        sel.value = resizeRect(press.base, press.dir, p.x, p.y, f.w, f.h);
        redraw();
        return;
    }
    if (press.kind === "draw" && live.value) {
        const c = clampToScreen(p, f.w, f.h);
        if (live.value.tool === "pen") {
            if (shouldAppendPoint(live.value.pts, c)) live.value.pts.push(c);
        } else {
            live.value.pts[1] = c;
        }
        redraw();
        return;
    }
    if (phase.value === "pick") {
        hover.value = hitWindow(p.x, p.y, f.windows || []);
    }
    drawMagnifier();
}

function onUp(e: MouseEvent) {
    if (e.button !== 0) return;
    const f = frame.value;
    if (!f) return;
    const k = press;
    press = { kind: "none" };
    if (k.kind === "draw") {
        if (live.value && shapeIsMeaningful(live.value)) shapes.value.push(live.value);
        live.value = null;
        redraw();
        return;
    }
    if (k.kind === "move" || k.kind === "handle") {
        toolbarHidden.value = false;
        redraw();
        return;
    }
    if (k.kind !== "new") return;
    const p = clampToScreen(evPt(e), f.w, f.h);
    picking.value = false;
    const r = normalizeRect({ x1: k.from.x, y1: k.from.y, x2: p.x, y2: p.y });
    if (rectSize(r).w < 4 || rectSize(r).h < 4) {
        // 几乎没拖动 = 一次单击：命中窗口就整窗截取，否则维持原状等用户拖框
        const w = hitWindow(p.x, p.y, f.windows || []);
        if (w) {
            sel.value = winToRect(w, f.w, f.h);
            enterEdit(true);
        }
        return;
    }
    sel.value = r;
    enterEdit();
}

function enterEdit(fromWindow = false) {
    phase.value = "edit";
    hover.value = null;
    toolbarHidden.value = false;
    redraw();
    if (sel.value) {
        const s = rectSize(sel.value);
        report(`${fromWindow ? "整窗选中" : "拖框完成"} ${Math.round(s.w)}x${Math.round(s.h)}`);
    }
}

function onHandleMouseDown(dir: HandleDir, e: MouseEvent) {
    if (e.button !== 0) return;
    press = { kind: "handle", dir, base: sel.value as Rect };
}

/** 双击：普通情况下=复制并收场；选中绘图工具且落在选区内时不抢（那时双击属于作图）；
 *  唯独马赛克：双击选区内 = 一键把整个选区打码 */
function onDblClick(e: MouseEvent) {
    if (phase.value !== "edit" || editBox.value) return;
    if (tool.value === "mosaic" && inSel(evPt(e))) {
        blurAll();
        return;
    }
    if (tool.value !== "none" && inSel(evPt(e))) return;
    finish(false);
}

function onKey(e: KeyboardEvent) {
    if (editBox.value) return;
    if (e.key === "Escape") {
        e.preventDefault();
        onCancel();
    } else if (e.key === "Enter") {
        e.preventDefault();
        finish(false);
    } else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "z") {
        e.preventDefault();
        undo();
    }
}

function onCancel() {
    try {
        $GoTools.CaptureCancel();
    } catch {
        /* 后端已收场时忽略 */
    }
}

/* ---------------- 文字标注框（可拖动 / 可缩放） ---------------- */
// 四角缩放柄；移动则抓框体四周的 padding 边带
const BOX_HANDLES = ["nw", "ne", "sw", "se"] as const;
type BoxHandle = (typeof BOX_HANDLES)[number];
// 框体相对文本内容额外留出的抓手边带（CSS px）：既是移动区也是 resize 柄的落点
const BOX_PAD = 7;
const BOX_MIN_CSS = 24; // 文本框最小宽高（CSS px）

// 字号/颜色对“当前正在输入的文本框”实时生效：否则选中小/中/大只改默认值、对已弹出的框无效
function setFontSize(v: number) {
    fontSize.value = v;
    if (editBox.value) editBox.value.fs = v;
}
function setColor(c: string) {
    color.value = c;
    if (editBox.value) editBox.value.color = c;
}

function startBox(p: Pt) {
    const fs = fontSize.value;
    const w = Math.round(toPhys(220));
    const h = Math.round(toPhys(fs * 1.3 + 6));
    editBox.value = { x: Math.round(p.x), y: Math.round(p.y), w, h, fs, color: color.value, text: "" };
    clampBox();
    // 渲染后再聚焦；WebView 里 nextTick 时元素可能尚未可聚焦，补一个宏任务兜底
    nextTick(() => {
        textRef.value?.focus();
        setTimeout(() => textRef.value?.focus(), 0);
    });
}

function commitBox() {
    const b = editBox.value;
    if (!b) return;
    editBox.value = null;
    const text = b.text.replace(/\s+$/, "");
    if (!text) return;
    shapes.value.push({
        tool: "text",
        pts: [{ x: b.x, y: b.y }],
        color: b.color,
        lw: lw.value,
        fs: b.fs,
        bw: toCss(b.w),
        text,
    });
    redraw();
}

// 把框夹回选区内（含最小尺寸保护）
function clampBox() {
    const b = editBox.value;
    if (!b || !sel.value) return;
    const n = normalizeRect(sel.value);
    const minW = toPhys(BOX_MIN_CSS);
    const minH = toPhys(BOX_MIN_CSS);
    if (b.w < minW) b.w = minW;
    if (b.h < minH) b.h = minH;
    if (b.x + b.w > n.x2) b.x = n.x2 - b.w;
    if (b.y + b.h > n.y2) b.y = n.y2 - b.h;
    if (b.x < n.x1) b.x = n.x1;
    if (b.y < n.y1) b.y = n.y1;
}

const boxStyle = computed(() => {
    const b = editBox.value;
    if (!b) return {};
    return {
        left: toCss(b.x) - BOX_PAD + "px",
        top: toCss(b.y) - BOX_PAD + "px",
        width: toCss(b.w) + BOX_PAD * 2 + "px",
        height: toCss(b.h) + BOX_PAD * 2 + "px",
    };
});
const areaStyle = computed(() => {
    const b = editBox.value;
    if (!b) return {};
    return {
        width: toCss(b.w) + "px",
        height: toCss(b.h) + "px",
        color: b.color,
        caretColor: b.color,
        fontSize: b.fs + "px",
        lineHeight: b.fs * 1.3 + "px",
    };
});

// 拖动框：mode=move 抓边带平移，mode=resize 拖角柄改尺寸
let boxDrag: {
    mode: "move" | "resize";
    dir: string;
    start: Pt;
    base: { x: number; y: number; w: number; h: number };
} | null = null;

function onBoxDown(e: MouseEvent) {
    // 点文字区交给光标（不移动）；角柄各自处理
    const el = e.target as HTMLElement;
    if (el.classList.contains("cap-textarea") || el.classList.contains("cap-box-h")) return;
    beginBoxDrag("move", "", e);
}
function onBoxHandle(dir: BoxHandle, e: MouseEvent) {
    beginBoxDrag("resize", dir, e);
}
function beginBoxDrag(mode: "move" | "resize", dir: string, e: MouseEvent) {
    const b = editBox.value;
    if (!b) return;
    boxDrag = { mode, dir, start: evPt(e), base: { x: b.x, y: b.y, w: b.w, h: b.h } };
    window.addEventListener("mousemove", onBoxDragMove);
    window.addEventListener("mouseup", onBoxDragUp);
    e.preventDefault();
}
function onBoxDragMove(e: MouseEvent) {
    if (!boxDrag || !editBox.value) return;
    const p = evPt(e);
    const dx = p.x - boxDrag.start.x;
    const dy = p.y - boxDrag.start.y;
    const b = editBox.value;
    const base = boxDrag.base;
    if (boxDrag.mode === "move") {
        b.x = base.x + dx;
        b.y = base.y + dy;
    } else {
        let x1 = base.x, y1 = base.y, x2 = base.x + base.w, y2 = base.y + base.h;
        const dir = boxDrag.dir;
        if (dir.includes("w")) x1 = base.x + dx;
        if (dir.includes("e")) x2 = base.x + base.w + dx;
        if (dir.includes("n")) y1 = base.y + dy;
        if (dir.includes("s")) y2 = base.y + base.h + dy;
        b.x = Math.min(x1, x2);
        b.y = Math.min(y1, y2);
        b.w = Math.abs(x2 - x1);
        b.h = Math.abs(y2 - y1);
    }
    clampBox();
}
function onBoxDragUp() {
    boxDrag = null;
    window.removeEventListener("mousemove", onBoxDragMove);
    window.removeEventListener("mouseup", onBoxDragUp);
}

/* ---------------- 绘制 ---------------- */
function redraw() {
    const f = frame.value;
    const c = annotRef.value;
    const src = srcCanvas.value;
    if (!f || !c || !src || !scratch) return;
    const ctx = c.getContext("2d");
    if (!ctx) return;
    ctx.setTransform(1, 0, 0, 1, 0, 0);
    ctx.clearRect(0, 0, f.w, f.h);
    if (!sel.value) return;
    const n = normalizeRect(sel.value);
    ctx.save();
    ctx.beginPath();
    ctx.rect(n.x1, n.y1, n.x2 - n.x1, n.y2 - n.y1);
    ctx.clip();
    for (const s of shapes.value) drawShape(ctx, src, scratch, s, dpr.value, f.w, f.h);
    if (live.value) drawShape(ctx, src, scratch, live.value, dpr.value, f.w, f.h);
    ctx.restore();
}

function drawMagnifier() {
    const mc = magRef.value;
    const src = srcCanvas.value;
    if (!mc || !src || !showMagnifier.value) return;
    const ctx = mc.getContext("2d");
    if (!ctx) return;
    // 取整后再定位，保证中心那一格正好落在光标所指像素上（半像素会让整幅插值糊掉）
    const sx = Math.round(toPhys(cursor.value.x)) - ((MAG_N - 1) >> 1);
    const sy = Math.round(toPhys(cursor.value.y)) - ((MAG_N - 1) >> 1);
    ctx.imageSmoothingEnabled = false;
    ctx.clearRect(0, 0, MAG_BOX, MAG_BOX);
    ctx.drawImage(src, sx, sy, MAG_N, MAG_N, 0, 0, MAG_BOX, MAG_BOX);
    const cell = MAG_BOX / MAG_N;
    ctx.strokeStyle = "rgba(255,255,255,.28)";
    ctx.lineWidth = 1;
    for (let i = 1; i < MAG_N; i++) {
        ctx.beginPath();
        ctx.moveTo(i * cell, 0);
        ctx.lineTo(i * cell, MAG_BOX);
        ctx.stroke();
        ctx.beginPath();
        ctx.moveTo(0, i * cell);
        ctx.lineTo(MAG_BOX, i * cell);
        ctx.stroke();
    }
    ctx.strokeStyle = "#165DFF";
    ctx.strokeRect(((MAG_BOX - cell) / 2) | 0, ((MAG_BOX - cell) / 2) | 0, cell, cell);
    const d = ctx.getImageData((MAG_BOX / 2) | 0, (MAG_BOX / 2) | 0, 1, 1).data;
    hoverColor.value = "#" + [d[0], d[1], d[2]].map((v) => v.toString(16).padStart(2, "0")).join("");
}

/* ---------------- 导出 ---------------- */
function exportPNG(): string | null {
    commitBox();
    if (!sel.value || !srcCanvas.value || !annotRef.value) return null;
    const out = composeExport(srcCanvas.value, annotRef.value, normalizeRect(sel.value));
    return canvasToPngDataURL(out);
}

/**
 * 结束一次截图。
 * openPreview=false（勾号 / 回车 / 双击）：只写剪贴板并关闭浮层，不打扰当前工作；
 * openPreview=true（预览按钮）：额外打开或刷新截图预览窗口。
 */
async function finish(openPreview: boolean) {
    const data = exportPNG();
    if (!data) {
        onCancel();
        return;
    }
    try {
        localStorage.setItem(STORAGE_KEY, data);
    } catch {
        /* 超配额时忽略，仅影响预览窗回显 */
    }
    try {
        await $GoTools.CaptureFinish(data, openPreview);
    } catch (e: any) {
        report(`CaptureFinish 异常: ${e?.message || e}`);
        Message.error("复制失败：" + (e?.message || e));
    }
}

async function saveFile() {
    const data = exportPNG();
    if (!data) return;
    try {
        // 弹原生另存为对话框（默认目录=配置的 screenshots）；用户取消时返回空串，保持浮层
        const path = await $GoTools.SaveCaptureFile(data);
        if (path) {
            // 保存成功：走公共系统通知（原生 Toast）——浮层即将关闭，用 Message 看不见
            try {
                $GoTools.ShowSystemNotification("截图已保存", path);
            } catch {
                /* 通知失败不影响收场 */
            }
            onCancel();
        }
    } catch (e: any) {
        Message.error("保存失败：" + (e?.message || e));
    }
}

/* ---------------- 图钉置顶 / 提取文字 ---------------- */
const ocrVisible = ref(false);
const ocrLoading = ref(false);
const ocrText = ref("");

async function pinIt() {
    const data = exportPNG();
    if (!data || !sel.value) return;
    const n = normalizeRect(sel.value as Rect);
    try {
        // 选区物理坐标交给 Go，按 dpr 换算成置顶小窗的 DIP 几何
        await $GoTools.OpenPinnedImage(
            data,
            Math.round(n.x1),
            Math.round(n.y1),
            Math.round(n.x2 - n.x1),
            Math.round(n.y2 - n.y1),
        );
        // 钉好即收浮层：全屏浮层若不关，会盖在图钉上面
        onCancel();
    } catch (e: any) {
        Message.error("图钉失败：" + (e?.message || e));
    }
}

async function runOcr() {
    const data = exportPNG();
    if (!data) return;
    ocrText.value = "";
    ocrLoading.value = true;
    ocrVisible.value = true;
    try {
        ocrText.value = await $GoTools.OcrImage(data);
    } catch (e: any) {
        ocrVisible.value = false;
        Message.error(String(e?.message || e));
    } finally {
        ocrLoading.value = false;
    }
}

function copyOcr() {
    const ta = document.querySelector(".cap-ocr-text") as HTMLTextAreaElement | null;
    if (ta) {
        ta.focus();
        ta.select();
        try {
            if (document.execCommand("copy")) {
                Message.success("已复制文本");
                return;
            }
        } catch {
            /* 落到 clipboard API */
        }
    }
    navigator.clipboard
        ?.writeText(ocrText.value)
        .then(() => Message.success("已复制文本"))
        .catch(() => Message.error("复制失败"));
}

function blurAll() {
    if (!sel.value) return;
    const n = normalizeRect(sel.value);
    shapes.value.push({
        tool: "mosaic",
        pts: [{ x: n.x1, y: n.y1 }, { x: n.x2, y: n.y2 }],
        color: color.value,
        lw: lw.value,
    });
    redraw();
}

/* 马赛克保留“一键打码”能力但不占子栏：选中马赛克工具时双击选区即整块打码 */
</script>

<style scoped>
.cap {
    position: fixed;
    inset: 0;
    overflow: hidden;
    cursor: crosshair;
    user-select: none;
    /* 不能给背景色：窗口是透明窗口，这里一旦铺实色，首帧未画出冻结帧前
       又会变成整屏黑，黑闪回来。唯一的不透明层只能是下面的 .cap-bg 冻结帧 */
    background: transparent;
    outline: none;
}

.cap-loading {
    position: fixed;
    inset: 0;
    background: transparent;
}

.cap-bg {
    position: absolute;
    inset: 0;
    background-size: 100% 100%;
    background-repeat: no-repeat;
}

.cap-dim {
    position: absolute;
    inset: 0;
    background: rgba(0, 0, 0, .45);
}

.cap-hole {
    position: absolute;
    box-shadow: 0 0 0 100000px rgba(0, 0, 0, .45);
}

.cap-annot {
    position: absolute;
    inset: 0;
    width: 100%;
    height: 100%;
    pointer-events: none;
}

/* 选区：与参考图一致的亮绿 */
.cap-sel {
    position: absolute;
    border: 2px solid #00c853;
    box-sizing: border-box;
    cursor: move;
}

.cap-draw .cap-sel {
    cursor: crosshair;
}

.cap-handle {
    position: absolute;
    /* 16px 透明命中区 + 居中的 9px 绿点：既好抓又保持参考图的细方点观感 */
    width: 16px;
    height: 16px;
    transform: translate(-50%, -50%);
    display: flex;
    align-items: center;
    justify-content: center;
}

.cap-handle::before {
    content: "";
    width: 9px;
    height: 9px;
    background: #00c853;
    border: 1px solid #fff;
    box-sizing: border-box;
    border-radius: 2px;
}

.h-nw, .h-se { cursor: nwse-resize }
.h-ne, .h-sw { cursor: nesw-resize }
.h-n, .h-s { cursor: ns-resize }
.h-e, .h-w { cursor: ew-resize }

.cap-tag {
    position: absolute;
    padding: 1px 6px;
    font-size: 12px;
    line-height: 18px;
    color: #fff;
    background: rgba(0, 0, 0, .68);
    border-radius: 3px;
    pointer-events: none;
    max-width: 60vw;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.cap-mag {
    position: absolute;
    background: rgba(28, 28, 32, .96);
    border: 1px solid rgba(255, 255, 255, .18);
    border-radius: 6px;
    padding: 8px;
    pointer-events: none;
}

.cap-mag-canvas {
    width: 150px;
    height: 150px;
    display: block;
    image-rendering: pixelated;
}

.cap-mag-info {
    display: flex;
    justify-content: space-between;
    align-items: center;
    color: #ddd;
    font-size: 11px;
    margin-top: 6px;
}

.cap-mag-rgb {
    display: flex;
    align-items: center;
    gap: 3px;
}

.cap-mag-rgb i {
    width: 10px;
    height: 10px;
    border: 1px solid rgba(255, 255, 255, .5);
    display: inline-block;
}

/* 文字标注框：外层虚线框（含 padding 抓手边带），内层透明 textarea 铺满文本区 */
.cap-textbox {
    position: absolute;
    box-sizing: border-box;
    padding: 7px;
    background: transparent;
    border: 1px dashed rgba(255, 255, 255, .9);
    box-shadow: 0 0 0 1px rgba(0, 0, 0, .25);
    cursor: move;
}

.cap-textarea {
    position: relative;
    box-sizing: border-box;
    display: block;
    background: transparent;
    border: none;
    outline: none;
    resize: none;
    overflow: hidden;
    padding: 0;
    margin: 0;
    font-family: "Microsoft YaHei", "PingFang SC", sans-serif;
    white-space: pre-wrap;
    word-break: break-word;
    cursor: text;
}

/* 四角缩放柄：白底黑框小方块 */
.cap-box-h {
    position: absolute;
    width: 10px;
    height: 10px;
    background: #fff;
    border: 1px solid #333;
    border-radius: 2px;
    box-sizing: border-box;
}
.bh-nw {
    left: 0;
    top: 0;
    cursor: nwse-resize;
}
.bh-ne {
    right: 0;
    top: 0;
    cursor: nesw-resize;
}
.bh-sw {
    left: 0;
    bottom: 0;
    cursor: nesw-resize;
}
.bh-se {
    right: 0;
    bottom: 0;
    cursor: nwse-resize;
}

/* ---------------- 工具栏：浅色主栏 + 深色子栏 ---------------- */
.cap-bar {
    position: absolute;
    display: flex;
    flex-direction: column;
    align-items: flex-end;
    gap: 4px;
    cursor: default;
}

.cap-main {
    display: flex;
    align-items: center;
    gap: 2px;
    height: 38px;
    padding: 0 5px;
    /* 背景/文字走 Arco 语义变量，随 body[arco-theme] 自动跟随浅/暗主题；无边框 */
    background: var(--color-bg-2);
    color: var(--color-text-1);
    border-radius: 5px;
    box-shadow: 0 4px 14px rgba(0, 0, 0, .28);
}

.cap-sub {
    display: flex;
    align-items: center;
    gap: 2px;
    height: 34px;
    padding: 0 6px;
    /* 子栏用高一级的 bg，两种主题下都比主栏深一档；无边框 */
    background: var(--color-bg-3);
    color: var(--color-text-2);
    border-radius: 5px;
    box-shadow: 0 4px 14px rgba(0, 0, 0, .28);
}

.cap-btn {
    width: 28px;
    height: 28px;
    display: flex;
    align-items: center;
    justify-content: center;
    border: none;
    background: transparent;
    cursor: pointer;
    padding: 0;
}

.cap-btn:hover:not(:disabled) { background: var(--color-fill-2); color: var(--color-text-1) }
.cap-btn:disabled { opacity: .35; cursor: default }
.cap-btn.on { background: var(--color-fill-3); color: var(--color-text-1) }

.cap-sub .cap-btn { color: var(--color-text-2) }
.cap-sub .cap-btn:hover:not(:disabled) { background: var(--color-fill-3); color: var(--color-text-1) }
.cap-sub .cap-btn.on { background: var(--color-fill-4); color: var(--color-text-1) }

.cap-btn-wide {
    width: auto;
    padding: 0 8px;
    font-size: 12px;
}

.cap-svg {
    width: 1em;
    height: 1em;
    fill: none;
    stroke: currentColor;
    stroke-width: 4;
    stroke-linecap: round;
    stroke-linejoin: round;
}

.cap-dot {
    display: block;
    border-radius: 50%;
    background: currentColor;
}

.cap-swatch {
    display: block;
    width: 16px;
    height: 16px;
    border-radius: 3px;
    border: 1px solid rgba(255, 255, 255, .35);
    box-sizing: border-box;
}

/* 颜色选中态：a-button 本身不加自定义形状，只给一个背景表示已选 */
.cap-color-on { background: var(--color-fill-3) !important; }

.cap-ocr-spin { display: block; width: 100%; }
.cap-ocr-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: 10px;
}
.cap-ocr-hint { font-size: 12px; color: var(--color-text-3); }

.cap-sep {
    width: 1px;
    height: 18px;
    margin: 0 3px;
    background: var(--color-border-2);
}

.cap-sep-dark { background: var(--color-border-3) }
</style>
