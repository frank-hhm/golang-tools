<template>
    <a-row class="shot-page" :gutter="6">
        <!-- 左侧约 25%：截图操作 -->
        <a-col :span="5" class="side-col">
            <a-card class="side-card" size="small" :body-style="{ padding: 0 }">
                <template #title>截图操作</template>
                <template #extra>
                    <a-tag size="small" :color="hasImage ? 'green' : 'gray'">
                        {{ hasImage ? "已有截图" : "暂无截图" }}
                    </a-tag>
                </template>
                <div class="side-body">
                    <a-button type="primary" status="success" long @click="recapture">
                        <template #icon><icon-camera /></template>重新截图
                    </a-button>
                    <a-button long :disabled="!hasImage" @click="copyImage">
                        <template #icon><icon-copy /></template>复制图片
                    </a-button>
                    <a-button long :disabled="!hasImage" @click="saveFile">
                        <template #icon><icon-save /></template>另存为…
                    </a-button>
                    <a-button long :disabled="!hasImage" @click="pinIt">
                        <template #icon><icon-pushpin /></template>图钉置顶
                    </a-button>
                    <a-divider style="margin: 4px 0" />
                    <div class="tip">
                        预览区已内置标注工具：矩形 / 椭圆 / 箭头 / 画笔 / 马赛克 / 文字，
                        并可撤销、旋转、缩放、裁剪。改完「复制 / 另存 / 图钉」拿到的都是叠加标注后的成品。
                    </div>
                </div>
            </a-card>
        </a-col>

        <!-- 右侧：可编辑预览 -->
        <a-col :span="19" class="main-col">
            <a-card class="main-panel" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space :size="8">
                        <span>截图预览</span>
                        <a-tag size="small">PNG</a-tag>
                    </a-space>
                </template>

                <div class="ed-wrap">
                    <!-- 工具栏 -->
                    <div class="ed-bar">
                        <div class="ed-group">
                            <a-tooltip v-for="t in TOOLS" :key="t.tool" :content="t.tip" :mouse-enter-delay="600">
                                <a-button size="mini" :type="tool === t.tool ? 'primary' : 'text'"
                                    :disabled="!hasImage" @click="pickTool(t.tool)">
                                    <component :is="t.icon" v-if="t.icon" />
                                    <svg v-else class="ed-svg" viewBox="0 0 48 48">
                                        <rect v-if="t.shape === 'rect'" x="7" y="11" width="34" height="26" rx="3" />
                                        <ellipse v-if="t.shape === 'ellipse'" cx="24" cy="24" rx="17" ry="13" />
                                        <path v-if="t.shape === 'text'" d="M10 12h28M24 12v26" />
                                    </svg>
                                </a-button>
                            </a-tooltip>
                            <a-tooltip content="撤销 (Ctrl+Z)" :mouse-enter-delay="600">
                                <a-button size="mini" type="text" :disabled="!shapes.length" @click="undo">
                                    <icon-undo />
                                </a-button>
                            </a-tooltip>
                        </div>

                        <i class="ed-sep"></i>

                        <div class="ed-group">
                            <a-tooltip content="向左旋转 90°" :mouse-enter-delay="600">
                                <a-button size="mini" type="text" :disabled="!hasImage" @click="rotate(-90)">
                                    <svg class="ed-svg" viewBox="0 0 48 48">
                                        <path d="M14 14l-6 6 6 6M8 20h16a12 12 0 1 1-12 12" />
                                    </svg>
                                </a-button>
                            </a-tooltip>
                            <a-tooltip content="向右旋转 90°" :mouse-enter-delay="600">
                                <a-button size="mini" type="text" :disabled="!hasImage" @click="rotate(90)">
                                    <svg class="ed-svg" viewBox="0 0 48 48">
                                        <path d="M34 14l6 6-6 6M40 20H24a12 12 0 1 0 12 12" />
                                    </svg>
                                </a-button>
                            </a-tooltip>
                            <a-tooltip content="裁剪" :mouse-enter-delay="600">
                                <a-button size="mini" :type="cropMode ? 'primary' : 'text'" :disabled="!hasImage"
                                    @click="toggleCrop">
                                    <svg class="ed-svg" viewBox="0 0 48 48">
                                        <path d="M14 6v28h28M6 14h28v28" />
                                    </svg>
                                </a-button>
                            </a-tooltip>
                        </div>

                        <i class="ed-sep"></i>

                        <div class="ed-group ed-zoom">
                            <a-button size="mini" type="text" :disabled="!hasImage" @click="zoomBy(1 / 1.25)">
                                <icon-minus />
                            </a-button>
                            <span class="ed-zpct" @click="zoomFit" title="点击适应窗口">{{ Math.round(zoom * 100) }}%</span>
                            <a-button size="mini" type="text" :disabled="!hasImage" @click="zoomBy(1.25)">
                                <icon-plus />
                            </a-button>
                            <a-tooltip content="实际大小 (100%)" :mouse-enter-delay="600">
                                <a-button size="mini" type="text" :disabled="!hasImage" @click="zoomReset">1:1</a-button>
                            </a-tooltip>
                        </div>

                        <div class="ed-spacer"></div>

                        <div class="ed-group" v-if="cropMode">
                            <a-button size="mini" type="primary" :disabled="!cropSel" @click="applyCrop">应用裁剪</a-button>
                            <a-button size="mini" type="text" status="danger" @click="cancelCrop">取消</a-button>
                        </div>
                    </div>

                    <!-- 子栏：文字用字号，其余图形用线宽；都带配色 -->
                    <div v-if="showSub" class="ed-sub">
                        <template v-if="tool === 'text'">
                            <a-button v-for="f in FONT_SIZES" :key="'fs' + f.v" size="mini"
                                :type="fontSize === f.v ? 'primary' : 'text'" @click="fontSize = f.v">{{ f.label }}</a-button>
                        </template>
                        <template v-else>
                            <a-button v-for="(w, i) in LINE_WIDTHS" :key="'lw' + i" size="mini"
                                :type="lw === w ? 'primary' : 'text'" @click="lw = w">
                                <i class="ed-dot" :style="{ width: 4 + i * 3 + 'px', height: 4 + i * 3 + 'px' }"></i>
                            </a-button>
                        </template>
                        <i class="ed-sep ed-sep-dark"></i>
                        <a-button v-for="c in COLORS" :key="'cl' + c" size="mini" type="text"
                            :class="{ 'ed-color-on': color === c }" @click="color = c">
                            <i class="ed-swatch" :style="{ background: c }"></i>
                        </a-button>
                    </div>

                    <!-- 画布舞台 -->
                    <div ref="bodyRef" class="ed-body" :class="{ 'ed-crop-mode': cropMode }">
                        <div v-if="!hasImage" class="ed-empty">
                            <a-empty description="暂无截图，点击左侧「重新截图」进行区域捕获" />
                        </div>
                        <div v-else class="ed-stage" :style="stageStyle">
                            <canvas ref="viewRef" class="ed-canvas" :width="imgW" :height="imgH"
                                @mousedown="onDown"></canvas>
                            <!-- 裁剪框 -->
                            <div v-if="cropSel" class="ed-cropbox" :style="cropStyle"></div>
                            <!-- 文字就地编辑框 -->
                            <textarea v-if="textEdit" ref="textRef" v-model="textEdit.text" class="ed-textarea"
                                :style="textStyle" spellcheck="false"
                                @keydown.esc.stop.prevent="cancelText" @keydown.enter.exact.prevent="commitText"
                                @blur="commitText"></textarea>
                        </div>
                    </div>
                </div>
            </a-card>
        </a-col>
    </a-row>
</template>

<script setup lang="ts">
/**
 * 截图预览 + 标注编辑器。
 *
 * 复用截图浮层的绘制引擎 captureDraw.ts：坐标一律用图片自然像素（dpr=1），
 * base 画布存当前图像（旋转/裁剪会烘焙进它），shapes 是尚未烘焙的标注，
 * 显示与导出都是「base + shapes」的实时合成。旋转会同步变换标注点保持可编辑，
 * 裁剪则先烘焙标注再裁图（裁剪是收束性操作，无需再改标注）。
 */
import { computed, getCurrentInstance, nextTick, onMounted, onUnmounted, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { IconArrowRise, IconMosaic, IconPen } from "@arco-design/web-vue/es/icon";
import {
    DEFAULT_COLOR, DEFAULT_FONT_SIZE, DEFAULT_LW,
    type Pt, type Rect, type Shape, type Tool,
    canvasToPngDataURL, clampToScreen, drawShape, normalizeRect, shapeIsMeaningful, shouldAppendPoint,
} from "@/views/widget/captureDraw";

const {
    proxy: { $GoTools },
} = getCurrentInstance() as any;

/** 与首页事件约定的共享存储键（同源多窗口共享 localStorage） */
const STORAGE_KEY = "tool:screenshot";

/* ---------------- 工具与配色 ---------------- */
type ToolDef = { tool: Tool; tip: string; icon?: any; shape?: "rect" | "ellipse" | "text" };
const TOOLS: ToolDef[] = [
    { tool: "rect", tip: "矩形", shape: "rect" },
    { tool: "ellipse", tip: "椭圆", shape: "ellipse" },
    { tool: "arrow", tip: "箭头", icon: IconArrowRise },
    { tool: "pen", tip: "画笔", icon: IconPen },
    { tool: "mosaic", tip: "马赛克", icon: IconMosaic },
    { tool: "text", tip: "文字（点击画布输入）", shape: "text" },
];
const LINE_WIDTHS = [2, 4, 7, 11];
const COLORS = ["#165DFF", "#00B42A", "#FADC19", "#86909C", "#FFFFFF", "#F53F3F"];
const FONT_SIZES = [
    { label: "小", v: 13 },
    { label: "中", v: DEFAULT_FONT_SIZE },
    { label: "大", v: 26 },
];
const SUB_TOOLS: Tool[] = ["rect", "ellipse", "arrow", "pen"];
const showSub = computed(() => tool.value === "text" || SUB_TOOLS.includes(tool.value));

/* ---------------- 状态 ---------------- */
const hasImage = ref(false);
const tool = ref<Tool>("none");
const color = ref(DEFAULT_COLOR);
const lw = ref(DEFAULT_LW);
const fontSize = ref(DEFAULT_FONT_SIZE);

const imgW = ref(0);
const imgH = ref(0);
const zoom = ref(1);

const shapes = ref<Shape[]>([]);
const live = ref<Shape | null>(null);

const cropMode = ref(false);
const cropSel = ref<Rect | null>(null);
const liveCrop = ref<Rect | null>(null);

const textEdit = ref<{ x: number; y: number; w: number; fs: number; color: string; text: string } | null>(null);

const viewRef = ref<HTMLCanvasElement | null>(null);
const bodyRef = ref<HTMLElement | null>(null);
const textRef = ref<HTMLTextAreaElement | null>(null);

/** 当前图像位图（旋转/裁剪后的结果），标注叠加在它之上 */
let base: HTMLCanvasElement | null = null;
/** 马赛克降采样临时画布，复用避免每次新建 */
const scratch = document.createElement("canvas");

const stageStyle = computed(() => ({
    width: imgW.value * zoom.value + "px",
    height: imgH.value * zoom.value + "px",
}));
const cropStyle = computed(() => {
    const r = liveCrop.value || cropSel.value;
    if (!r) return { display: "none" };
    const n = normalizeRect(r);
    return {
        left: n.x1 * zoom.value + "px",
        top: n.y1 * zoom.value + "px",
        width: (n.x2 - n.x1) * zoom.value + "px",
        height: (n.y2 - n.y1) * zoom.value + "px",
    };
});
const textStyle = computed(() => {
    const t = textEdit.value!;
    return {
        left: t.x * zoom.value + "px",
        top: t.y * zoom.value + "px",
        width: t.w * zoom.value + "px",
        fontSize: t.fs * zoom.value + "px",
        lineHeight: t.fs * 1.3 * zoom.value + "px",
        color: t.color,
        caretColor: t.color,
    };
});

/* ---------------- 图像加载与绘制 ---------------- */
function setImageFromCanvas(c: HTMLCanvasElement) {
    base = c;
    imgW.value = c.width;
    imgH.value = c.height;
    hasImage.value = true;
    nextTick(() => {
        redraw();
        zoomFit();
    });
}

function loadImageData(dataUrl: string) {
    if (!dataUrl) return;
    const im = new Image();
    im.onload = () => {
        const c = document.createElement("canvas");
        c.width = im.naturalWidth || im.width;
        c.height = im.naturalHeight || im.height;
        const ctx = c.getContext("2d");
        if (!ctx) return;
        ctx.drawImage(im, 0, 0);
        shapes.value = [];
        live.value = null;
        cropMode.value = false;
        cropSel.value = null;
        setImageFromCanvas(c);
    };
    im.src = dataUrl;
}

/** 把 base + 所有标注合成到一张画布（导出、旋转、裁剪都基于它） */
function flatten(): HTMLCanvasElement | null {
    if (!base) return null;
    const out = document.createElement("canvas");
    out.width = base.width;
    out.height = base.height;
    const ctx = out.getContext("2d");
    if (!ctx) return null;
    ctx.drawImage(base, 0, 0);
    for (const s of shapes.value) drawShape(ctx, base, scratch, s, 1, out.width, out.height);
    return out;
}

function redraw() {
    const c = viewRef.value;
    if (!c || !base) return;
    if (c.width !== imgW.value) c.width = imgW.value;
    if (c.height !== imgH.value) c.height = imgH.value;
    const ctx = c.getContext("2d");
    if (!ctx) return;
    ctx.clearRect(0, 0, c.width, c.height);
    ctx.drawImage(base, 0, 0);
    ctx.save();
    ctx.beginPath();
    ctx.rect(0, 0, c.width, c.height);
    ctx.clip();
    for (const s of shapes.value) drawShape(ctx, base, scratch, s, 1, c.width, c.height);
    if (live.value) drawShape(ctx, base, scratch, live.value, 1, c.width, c.height);
    ctx.restore();
}

/* ---------------- 坐标换算 ---------------- */
function evPt(e: MouseEvent): Pt {
    const c = viewRef.value!;
    const r = c.getBoundingClientRect();
    return { x: (e.clientX - r.left) / zoom.value, y: (e.clientY - r.top) / zoom.value };
}

/* ---------------- 交互：绘制 / 裁剪 ---------------- */
type Press =
    | { kind: "none" }
    | { kind: "draw" }
    | { kind: "crop"; from: Pt };
let press: Press = { kind: "none" };

function pickTool(t: Tool) {
    if (textEdit.value) commitText();
    cropMode.value = false;
    cropSel.value = null;
    tool.value = tool.value === t ? "none" : t;
}

function onDown(e: MouseEvent) {
    if (e.button !== 0 || !base) return;
    if (textEdit.value) { commitText(); return; }
    const p = clampToScreen(evPt(e), imgW.value, imgH.value);
    if (cropMode.value) {
        liveCrop.value = { x1: p.x, y1: p.y, x2: p.x, y2: p.y };
        cropSel.value = null;
        press = { kind: "crop", from: p };
        return;
    }
    if (tool.value === "text") {
        startText(p);
        return;
    }
    if (tool.value !== "none") {
        live.value = { tool: tool.value as Exclude<Tool, "none">, pts: [p, p], color: color.value, lw: lw.value };
        press = { kind: "draw" };
        redraw();
    }
}

function onMove(e: MouseEvent) {
    if (!base) return;
    const p = clampToScreen(evPt(e), imgW.value, imgH.value);
    if (press.kind === "draw" && live.value) {
        if (live.value.tool === "pen") {
            if (shouldAppendPoint(live.value.pts, p)) live.value.pts.push(p);
        } else {
            live.value.pts[1] = p;
        }
        redraw();
    } else if (press.kind === "crop") {
        liveCrop.value = normalizeRect({ x1: press.from.x, y1: press.from.y, x2: p.x, y2: p.y });
    }
}

function onUp() {
    if (press.kind === "draw") {
        if (live.value && shapeIsMeaningful(live.value)) shapes.value.push(live.value);
        live.value = null;
        redraw();
    } else if (press.kind === "crop") {
        if (liveCrop.value) {
            const n = normalizeRect(liveCrop.value);
            if (n.x2 - n.x1 >= 4 && n.y2 - n.y1 >= 4) cropSel.value = n;
        }
        liveCrop.value = null;
    }
    press = { kind: "none" };
}

function undo() {
    shapes.value.pop();
    redraw();
}

/* ---------------- 文字 ---------------- */
function startText(p: Pt) {
    const w = Math.max(120, Math.min(320, imgW.value - p.x));
    textEdit.value = { x: p.x, y: p.y, w, fs: fontSize.value, color: color.value, text: "" };
    nextTick(() => textRef.value?.focus());
}
function commitText() {
    const t = textEdit.value;
    if (!t) return;
    textEdit.value = null;
    const text = t.text.replace(/\s+$/, "");
    if (text) {
        shapes.value.push({ tool: "text", pts: [{ x: t.x, y: t.y }], color: t.color, lw: lw.value, fs: t.fs, bw: t.w, text });
        redraw();
    }
}
function cancelText() {
    textEdit.value = null;
}

/* ---------------- 旋转 / 缩放 / 裁剪 ---------------- */
function rotate(deg: number) {
    if (!base) return;
    if (textEdit.value) commitText();
    const flat = flatten();
    if (!flat) return;
    const cw = deg > 0; // 顺时针
    const out = document.createElement("canvas");
    out.width = flat.height;
    out.height = flat.width;
    const ctx = out.getContext("2d");
    if (!ctx) return;
    ctx.translate(cw ? out.width : 0, cw ? 0 : out.height);
    ctx.rotate((cw ? 90 : -90) * Math.PI / 180);
    ctx.drawImage(flat, 0, 0);
    // 标注点随之旋转（保持可编辑）
    const W = flat.width, H = flat.height;
    for (const s of shapes.value) {
        s.pts = s.pts.map((pt) => (cw ? { x: H - pt.y, y: pt.x } : { x: pt.y, y: W - pt.x }));
    }
    shapes.value = [...shapes.value];
    base = out;
    imgW.value = out.width;
    imgH.value = out.height;
    cropSel.value = null;
    nextTick(() => { redraw(); zoomFit(); });
}

function clampZoom(z: number) {
    return Math.max(0.1, Math.min(8, z));
}
function zoomBy(f: number) {
    zoom.value = clampZoom(zoom.value * f);
}
function zoomReset() {
    zoom.value = 1;
}
function zoomFit() {
    const body = bodyRef.value;
    if (!body || !imgW.value || !imgH.value) return;
    const pad = 24;
    const zw = (body.clientWidth - pad) / imgW.value;
    const zh = (body.clientHeight - pad) / imgH.value;
    zoom.value = clampZoom(Math.min(zw, zh, 1));
}

function toggleCrop() {
    if (textEdit.value) commitText();
    cropMode.value = !cropMode.value;
    if (cropMode.value) { tool.value = "none"; cropSel.value = null; }
}
function cancelCrop() {
    cropMode.value = false;
    cropSel.value = null;
    liveCrop.value = null;
}
function applyCrop() {
    const sel = cropSel.value;
    if (!sel || !base) return;
    const flat = flatten();
    if (!flat) return;
    const n = normalizeRect(sel);
    const x = Math.round(n.x1), y = Math.round(n.y1);
    const w = Math.max(1, Math.round(n.x2 - n.x1)), h = Math.max(1, Math.round(n.y2 - n.y1));
    const out = document.createElement("canvas");
    out.width = w;
    out.height = h;
    const ctx = out.getContext("2d");
    if (!ctx) return;
    ctx.drawImage(flat, x, y, w, h, 0, 0, w, h);
    base = out;
    shapes.value = [];
    live.value = null;
    cropMode.value = false;
    cropSel.value = null;
    imgW.value = w;
    imgH.value = h;
    nextTick(() => { redraw(); zoomFit(); });
}

/* ---------------- 导出 ---------------- */
function exportDataURL(): string {
    const flat = flatten();
    return flat ? canvasToPngDataURL(flat) : "";
}

async function saveFile() {
    if (!hasImage.value) return;
    if (textEdit.value) commitText();
    const data = exportDataURL();
    if (!data) return;
    try {
        const path = await $GoTools.SaveCaptureFile(data);
        if (path) {
            try { $GoTools.ShowSystemNotification("截图已保存", path); } catch { /* 通知失败不影响 */ }
        }
    } catch (e: any) {
        Message.error("保存失败：" + (e?.message || e));
    }
}

async function copyImage() {
    if (!hasImage.value) return;
    if (textEdit.value) commitText();
    const data = exportDataURL();
    if (!data) return;
    try {
        await $GoTools.CopyToClipboard(data);
        Message.success("图片已复制到剪贴板");
    } catch (e: any) {
        Message.error("复制失败：" + (e?.message || e));
    }
}

async function pinIt() {
    if (!hasImage.value) return;
    if (textEdit.value) commitText();
    const data = exportDataURL();
    if (!data) return;
    try {
        // 图钉出现在屏幕中央附近：给一个合理尺寸，后端按 dpr 换算几何
        const w = Math.min(imgW.value, 900);
        const h = Math.round(w * imgH.value / imgW.value);
        await $GoTools.OpenPinnedImage(data, 120, 120, w, h);
    } catch (e: any) {
        Message.error("图钉失败：" + (e?.message || e));
    }
}

/** 重新触发区域截图（走选区浮层，成品由浮层写回共享存储并广播） */
const recapture = async () => {
    try {
        if (typeof $GoTools?.OpenCaptureOverlay === "function") {
            await $GoTools.OpenCaptureOverlay();
            return;
        }
        const data = await $GoTools.CaptureSelectedRegion("png");
        if (!data) return;
        loadImageData(data);
        Message.success("截图完成");
    } catch (e: any) {
        Message.error("截图失败：" + (e?.message || e));
    }
};

/* ---------------- 生命周期 ---------------- */
const reloadFromStorage = () => {
    let raw = "";
    try { raw = localStorage.getItem(STORAGE_KEY) || ""; } catch { /* */ }
    if (raw) loadImageData(raw);
};

let offUpdated: (() => void) | null = null;
onMounted(() => {
    reloadFromStorage();
    window.addEventListener("mousemove", onMove);
    window.addEventListener("mouseup", onUp);
    window.addEventListener("keydown", onKey);
    try {
        const r = Events.On("screenshot:updated", () => reloadFromStorage());
        if (typeof r === "function") offUpdated = r as () => void;
    } catch { /* */ }
});
onUnmounted(() => {
    window.removeEventListener("mousemove", onMove);
    window.removeEventListener("mouseup", onUp);
    window.removeEventListener("keydown", onKey);
    offUpdated?.();
    offUpdated = null;
});

function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") {
        if (textEdit.value) cancelText();
        else if (cropMode.value) cancelCrop();
        return;
    }
    const target = e.target as HTMLElement;
    if (target && (target.tagName === "TEXTAREA" || target.tagName === "INPUT")) return;
    if ((e.ctrlKey || e.metaKey) && (e.key === "z" || e.key === "Z")) {
        e.preventDefault();
        undo();
    }
}
</script>

<style scoped>
/* 页面根容器：a-row 铺满工具窗口内容区 */
.shot-page {
    height: 100%;
    min-height: 0;
    flex-wrap: nowrap;
    background: var(--color-fill-1);
}

.side-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.side-card {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.side-card :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.side-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--base-padding);
    padding: var(--base-padding);
    overflow-y: auto;
}

.tip {
    margin-top: auto;
    color: var(--color-text-3);
    font-size: var(--base-size-small);
    line-height: 1.8;
}

.main-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.main-panel {
    width: 100%;
    height: 100%;
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    background: var(--color-bg-2);
}

.main-panel :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

/* 编辑器整体纵向：工具栏 + 子栏 + 画布舞台 */
.ed-wrap {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.ed-bar,
.ed-sub {
    display: flex;
    align-items: center;
    gap: 2px;
    padding: 6px 10px;
    background: var(--color-bg-3);
    border-bottom: 1px solid var(--color-border-2);
    flex-wrap: wrap;
}

.ed-group {
    display: flex;
    align-items: center;
    gap: 2px;
}

.ed-spacer {
    flex: 1;
}

.ed-sep {
    width: 1px;
    height: 18px;
    margin: 0 6px;
    background: var(--color-border-3);
}

.ed-zoom {
    gap: 4px;
}

.ed-zpct {
    min-width: 44px;
    text-align: center;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    cursor: pointer;
    user-select: none;
}

.ed-svg {
    width: 18px;
    height: 18px;
    fill: none;
    stroke: currentColor;
    stroke-width: 4;
    stroke-linecap: round;
    stroke-linejoin: round;
}

.ed-dot {
    display: inline-block;
    border-radius: 50%;
    background: var(--color-text-1);
}

.ed-swatch {
    display: inline-block;
    width: 14px;
    height: 14px;
    border-radius: 3px;
    border: 1px solid var(--color-border-3);
}

.ed-color-on {
    box-shadow: inset 0 0 0 2px var(--color-bg-1), 0 0 0 1px rgb(var(--primary-6));
    border-radius: 4px;
}

.ed-body {
    flex: 1;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 12px;
    overflow: auto;
    background: var(--color-fill-2);
}

.ed-crop-mode {
    cursor: crosshair;
}

.ed-empty {
    display: flex;
    align-items: center;
    justify-content: center;
    width: 100%;
    height: 100%;
}

.ed-stage {
    position: relative;
    flex: none;
    box-shadow: 0 0 0 1px var(--color-border-3), 0 6px 20px rgba(0, 0, 0, 0.18);
    background: var(--color-bg-1);
}

.ed-canvas {
    display: block;
    width: 100%;
    height: 100%;
    image-rendering: auto;
}

.ed-cropbox {
    position: absolute;
    border: 1px dashed rgb(var(--primary-6));
    background: rgba(var(--primary-6), 0.12);
    pointer-events: none;
    box-sizing: border-box;
}

.ed-textarea {
    position: absolute;
    box-sizing: border-box;
    padding: 2px 4px;
    margin: 0;
    border: 1px dashed rgb(var(--primary-6));
    background: rgba(255, 255, 255, 0.04);
    color: inherit;
    font-family: "Microsoft YaHei", "PingFang SC", system-ui, sans-serif;
    line-height: 1.3;
    resize: none;
    overflow: hidden;
    outline: none;
    white-space: pre-wrap;
    word-break: break-word;
    min-width: 40px;
    min-height: 20px;
}
</style>
