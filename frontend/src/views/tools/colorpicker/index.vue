<template>
    <a-card class="cp-card" size="small" :title="isWin ? undefined : '颜色取色器'">
        <div ref="bodyEl" class="cp-body">
            <!-- 工具栏：自动复制开关 + 屏幕取色 -->
            <div ref="barEl" class="cp-bar">
                <a-checkbox v-model="autoCopy">自动将所选颜色复制到剪贴板</a-checkbox>
                <a-button size="small" @click="pickFromScreen">
                    <template #icon>
                        <icon-bg-colors />
                    </template>屏幕取色
                </a-button>
            </div>

            <div class="cp-main">
                <!-- 左：饱和度/明度方块（随窗口高度自适应） -->
                <div ref="svEl" class="cp-sv" :style="svStyle" @pointerdown="onSvDown">
                    <div class="cp-marker" :style="svMarkerStyle"></div>
                </div>

                <!-- 中：色相条 -->
                <div ref="hueEl" class="cp-hue" @pointerdown="onHueDown">
                    <div class="cp-hue-marker" :style="hueMarkerStyle"></div>
                </div>

                <!-- 右：取色数值面板 -->
                <div ref="panelEl" class="cp-panel">
                    <div class="cp-swatches">
                        <a-tooltip content="新颜色">
                            <div class="cp-swatch" :style="{ background: hexColor }"></div>
                        </a-tooltip>
                        <a-tooltip content="原颜色">
                            <div class="cp-swatch" :style="{ background: oldColor }"></div>
                        </a-tooltip>
                    </div>

                    <div class="cp-grid">
                        <div class="cp-row">
                            <label>R:</label>
                            <a-input-number class="cp-input-rgb" :model-value="rgb.r" :min="0" :max="255" size="mini"
                                hide-button @change="(v: number | undefined) => setChannel('r', v ?? 0)" />
                            <label class="cp-tag">C:</label>
                            <a-input-number :model-value="cmyk.c" :min="0" :max="100" size="mini" hide-button disabled
                                class="cp-ro" />
                            <span class="cp-pct">%</span>
                        </div>
                        <div class="cp-row">
                            <label>G:</label>
                            <a-input-number class="cp-input-rgb" :model-value="rgb.g" :min="0" :max="255" size="mini"
                                hide-button @change="(v: number | undefined) => setChannel('g', v ?? 0)" />
                            <label class="cp-tag">M:</label>
                            <a-input-number :model-value="cmyk.m" :min="0" :max="100" size="mini" hide-button disabled
                                class="cp-ro" />
                            <span class="cp-pct">%</span>
                        </div>
                        <div class="cp-row">
                            <label>B:</label>
                            <a-input-number class="cp-input-rgb" :model-value="rgb.b" :min="0" :max="255" size="mini"
                                hide-button @change="(v: number | undefined) => setChannel('b', v ?? 0)" />
                            <label class="cp-tag">Y:</label>
                            <a-input-number :model-value="cmyk.y" :min="0" :max="100" size="mini" hide-button disabled
                                class="cp-ro" />
                            <span class="cp-pct">%</span>
                        </div>
                        <div class="cp-row">
                            <span class="cp-gap"></span>
                            <label class="cp-tag">K:</label>
                            <a-input-number :model-value="cmyk.k" :min="0" :max="100" size="mini" hide-button disabled
                                class="cp-ro" />
                            <span class="cp-pct">%</span>
                        </div>
                    </div>

                    <div class="cp-modes">
                        <span class="cp-mode-active">
                            H: {{ hsv.h }}°&nbsp;&nbsp;S: {{ hsv.s }}%&nbsp;&nbsp;V: {{ hsv.v }}%
                        </span>
                    </div>
                    <!-- <div class="cp-modes">
                        <span class="cp-mode-sub">
                            H: {{ hsl.h }}°&nbsp;&nbsp;S: {{ hsl.s }}%&nbsp;&nbsp;L: {{ hsl.l }}%
                        </span>
                    </div> -->
                </div>
            </div>

            <!-- 底部：常用格式输出（点击复制） -->
            <div class="cp-out">
                <div class="cp-field">
                    <label>HTML:</label>
                    <a-input :model-value="htmlHex" size="mini" readonly @click="copyText(htmlHex)" />
                </div>
                <div class="cp-field">
                    <label>Hex:</label>
                    <a-input :model-value="bgrHex" size="mini" readonly @click="copyText(bgrHex)" />
                </div>
                <div class="cp-field">
                    <label>R,G,B:</label>
                    <a-input :model-value="rgbStr" size="mini" readonly @click="copyText(rgbStr)" />
                </div>
            </div>
        </div>
    </a-card>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onMounted, reactive, ref } from "vue";
import { useRoute } from "vue-router";
import { Window } from "@wailsio/runtime";
import { Message } from "@arco-design/web-vue";

/* ============ 颜色模型：以 HSV 为主，派生其余格式 ============ */
const hsv = reactive({ h: 173, s: 86, v: 11 });
const oldColor = ref("#000000");
const autoCopy = ref(false);

/** 原生独立窗口（/win/*，空白布局）：窗口标题栏已显示工具名，卡片不再重复大标题 */
const route = useRoute();
const isWin = computed(() => !!route.meta?.blank);

const clamp = (n: number, min: number, max: number) => Math.min(max, Math.max(min, n));
const toHex2 = (n: number) => clamp(Math.round(n), 0, 255).toString(16).padStart(2, "0").toUpperCase();

function hsvToRgb(h: number, s: number, v: number) {
    s /= 100; v /= 100;
    const c = v * s;
    const x = c * (1 - Math.abs(((h / 60) % 2) - 1));
    const m = v - c;
    let r = 0, g = 0, b = 0;
    if (h < 60) { r = c; g = x; }
    else if (h < 120) { r = x; g = c; }
    else if (h < 180) { g = c; b = x; }
    else if (h < 240) { g = x; b = c; }
    else if (h < 300) { r = x; b = c; }
    else { r = c; b = x; }
    return { r: Math.round((r + m) * 255), g: Math.round((g + m) * 255), b: Math.round((b + m) * 255) };
}

function rgbToHsv(r: number, g: number, b: number) {
    r /= 255; g /= 255; b /= 255;
    const max = Math.max(r, g, b), min = Math.min(r, g, b), d = max - min;
    let h = 0;
    if (d !== 0) {
        if (max === r) h = 60 * (((g - b) / d) % 6);
        else if (max === g) h = 60 * ((b - r) / d + 2);
        else h = 60 * ((r - g) / d + 4);
    }
    if (h < 0) h += 360;
    const s = max === 0 ? 0 : d / max;
    return { h: Math.round(h), s: Math.round(s * 100), v: Math.round(max * 100) };
}

function rgbToHsl(r: number, g: number, b: number) {
    r /= 255; g /= 255; b /= 255;
    const max = Math.max(r, g, b), min = Math.min(r, g, b);
    const l = (max + min) / 2;
    const d = max - min;
    let h = 0, s = 0;
    if (d !== 0) {
        if (max === r) h = 60 * (((g - b) / d) % 6);
        else if (max === g) h = 60 * ((b - r) / d + 2);
        else h = 60 * ((r - g) / d + 4);
        if (h < 0) h += 360;
        s = d / (1 - Math.abs(2 * l - 1));
    }
    return { h: Math.round(h), s: Math.round(s * 100), l: Math.round(l * 100) };
}

function rgbToCmyk(r: number, g: number, b: number) {
    const rr = r / 255, gg = g / 255, bb = b / 255;
    const k = 1 - Math.max(rr, gg, bb);
    if (k >= 1) return { c: 0, m: 0, y: 0, k: 100 };
    const c = (1 - rr - k) / (1 - k);
    const m = (1 - gg - k) / (1 - k);
    const y = (1 - bb - k) / (1 - k);
    return { c: Math.round(c * 100), m: Math.round(m * 100), y: Math.round(y * 100), k: Math.round(k * 100) };
}

function hexToRgb(hex: string) {
    const m = hex.replace("#", "");
    return {
        r: parseInt(m.substring(0, 2), 16),
        g: parseInt(m.substring(2, 4), 16),
        b: parseInt(m.substring(4, 6), 16),
    };
}

const rgb = computed(() => hsvToRgb(hsv.h, hsv.s, hsv.v));
const hsl = computed(() => rgbToHsl(rgb.value.r, rgb.value.g, rgb.value.b));
const cmyk = computed(() => rgbToCmyk(rgb.value.r, rgb.value.g, rgb.value.b));
const hexColor = computed(() => `#${toHex2(rgb.value.r)}${toHex2(rgb.value.g)}${toHex2(rgb.value.b)}`);
const htmlHex = computed(() => hexColor.value);
const bgrHex = computed(() => `${toHex2(rgb.value.b)}${toHex2(rgb.value.g)}${toHex2(rgb.value.r)}`);
const rgbStr = computed(() => `${rgb.value.r},${rgb.value.g},${rgb.value.b}`);

/* ============ 编辑入口 ============ */
function setChannel(ch: "r" | "g" | "b", val: number) {
    const cur = { ...rgb.value };
    cur[ch] = clamp(Math.round(val || 0), 0, 255);
    const nh = rgbToHsv(cur.r, cur.g, cur.b);
    hsv.h = nh.h; hsv.s = nh.s; hsv.v = nh.v;
}

/* ============ 取色方块 / 色相条交互 ============ */
const svEl = ref<HTMLElement | null>(null);
const hueEl = ref<HTMLElement | null>(null);
const bodyEl = ref<HTMLElement | null>(null);
const barEl = ref<HTMLElement | null>(null);
const panelEl = ref<HTMLElement | null>(null);
let draggingSV = false;
let draggingHue = false;

const svStyle = computed(() => ({
    background:
        "linear-gradient(to top, #000, rgba(0,0,0,0))," +
        `linear-gradient(to right, #fff, hsl(${hsv.h}, 100%, 50%))`,
}));
const svMarkerStyle = computed(() => ({
    left: `${hsv.s}%`,
    top: `${100 - hsv.v}%`,
}));
const hueMarkerStyle = computed(() => ({
    top: `${(hsv.h / 360) * 100}%`,
}));

function updateSv(e: PointerEvent) {
    const el = svEl.value;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const sx = clamp((e.clientX - r.left) / r.width, 0, 1);
    const sy = clamp((e.clientY - r.top) / r.height, 0, 1);
    hsv.s = Math.round(sx * 100);
    hsv.v = Math.round((1 - sy) * 100);
}
function updateHue(e: PointerEvent) {
    const el = hueEl.value;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const yy = clamp((e.clientY - r.top) / r.height, 0, 1);
    hsv.h = Math.round(yy * 359);
}
function onMove(e: PointerEvent) {
    if (draggingSV) updateSv(e);
    else if (draggingHue) updateHue(e);
}
function onUp() {
    draggingSV = false;
    draggingHue = false;
    window.removeEventListener("pointermove", onMove);
    window.removeEventListener("pointerup", onUp);
}
function onSvDown(e: PointerEvent) {
    draggingSV = true;
    updateSv(e);
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    e.preventDefault();
}
function onHueDown(e: PointerEvent) {
    draggingHue = true;
    updateHue(e);
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    e.preventDefault();
}

/* ============ 剪贴板 & 屏幕取色 ============ */
const {
    proxy: { $GoTools },
} = getCurrentInstance() as any;

async function copyText(t: string) {
    try {
        await navigator.clipboard.writeText(t);
        Message.success("已复制 " + t);
    } catch {
        Message.error("复制失败");
    }
}

/** 将取到的颜色应用到当前选色，并按需自动复制 */
function applyPickedHex(hex: string) {
    oldColor.value = hexColor.value;
    const c = hexToRgb(hex);
    const nh = rgbToHsv(c.r, c.g, c.b);
    hsv.h = nh.h;
    hsv.s = nh.s;
    hsv.v = nh.v;
    if (autoCopy.value) void copyText(htmlHex.value);
}

async function pickFromScreen() {
    // 优先：后端原生取色覆盖层（冻结屏幕+放大镜，单击取色）。
    if (typeof $GoTools?.PickColorFromScreen === "function") {
        try {
            const hex = await $GoTools.PickColorFromScreen();
            if (hex) applyPickedHex(hex);
        } catch (err) {
            console.error("屏幕取色失败:", err);
            Message.error("屏幕取色失败: " + err);
        }
        return;
    }
    // 回退：Chromium EyeDropper（由本窗口承载）
    const ED = (window as any).EyeDropper;
    if (typeof ED !== "function") {
        Message.warning("当前环境不支持屏幕取色（需 WebView2 / Chrome 95+）");
        return;
    }
    try {
        const res = await new ED().open();
        if (res && res.sRGBHex) applyPickedHex(res.sRGBHex);
    } catch {
        /* 用户按 ESC 取消取色，静默处理 */
    }
}

/* ============ 原生窗口内容自适应 ============ */

/**
 * 按内容自然高度调整窗口尺寸，消除底部多余空白（仅 /win 独立窗口生效）。
 * 数值面板以首末子元素推算自然高度（不受 flex 纵向拉伸影响）；
 * 窗口上下留白由卡片视口偏移反推；尺寸均为 DIP（与 CSS px 等值）。
 */
async function fitWindow() {
    const body = bodyEl.value, bar = barEl.value, panel = panelEl.value, sv = svEl.value;
    if (!isWin.value || !body || !bar || !panel || !sv || panel.children.length === 0) return;

    const first = panel.children[0] as HTMLElement;
    const last = panel.children[panel.children.length - 1] as HTMLElement;
    const panelNatural = last.offsetTop + last.offsetHeight - first.offsetTop;
    const rowNatural = Math.max(panelNatural, parseFloat(getComputedStyle(sv).minHeight) || 0);

    const cs = getComputedStyle(body);
    const padT = parseFloat(cs.paddingTop) || 0;
    const cardNatural = padT + bar.offsetHeight + (parseFloat(cs.rowGap) || 0) + rowNatural + (parseFloat(cs.paddingBottom) || 0);

    // .win-content 的上下留白：卡片视口顶 = 标题栏高度 + 留白
    const titlebarH = document.querySelector<HTMLElement>(".win-titlebar")?.clientHeight ?? 40;
    const pad = Math.max(0, body.getBoundingClientRect().top - padT - titlebarH);
    const innerH = Math.round(titlebarH + pad * 2 + cardNatural) + 2; // +2 容差，防面板出现滚动条

    try {
        const size = await Window.Size();
        if (size.width > 0) await Window.SetSize(size.width, innerH);
        // 后端建窗时用 Hidden 藏掉了窗口（避免先以配置高度闪一下再缩小），
        // 尺寸贴合完在这里才显示：开窗即最终大小。Go 侧另有超时兜底防卡隐。
        await Window.Show();
    } catch {
        /* 非原生窗口环境（浏览器调试等）忽略 */
    }
}

onMounted(() => {
    void nextTick(() => requestAnimationFrame(() => void fitWindow()));
});
</script>

<style scoped>
/* 卡片铺满窗口：高度由 fitWindow 按内容自适应，内部不产生冗余空白 */
.cp-card {
    height:calc( 100% - 2px);
    display: flex;
    flex-direction: column;
    background: var(--color-bg-1);
}

.cp-card :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: var(--base-padding);
}

.cp-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--base-padding);
}

/* 工具栏：自动复制开关 + 屏幕取色 */
.cp-bar {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--base-padding);
}

/* 主区：左取色方块 + 中色相条 + 右数值面板，高度弹性铺满 */
.cp-main {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: var(--base-padding);
    align-items: stretch;
}

.cp-sv {
    position: relative;
    flex: 1;
    min-width: 0;
    height: 128px;
    border-radius: var(--base-radius-small);
    border: 1px solid var(--color-border-1);
    cursor: crosshair;
    touch-action: none;
}

.cp-marker {
    position: absolute;
    width: 14px;
    height: 14px;
    border: 2px solid var(--color-border-2);
    border-radius: 50%;
    box-shadow: 0 0 0 1px rgba(0, 0, 0, 0.4);
    transform: translate(-50%, -50%);
    pointer-events: none;
}

.cp-hue {
    position: relative;
    width: 18px;
    max-height: 130px;
    flex: none;
    border-radius: var(--base-radius-small);
    cursor: pointer;
    touch-action: none;
    background: linear-gradient(to bottom,
            #f00 0%, #ff0 17%, #0f0 33%,
            #0ff 50%, #00f 67%, #f0f 83%, #f00 100%);
}

.cp-hue-marker {
    position: absolute;
    left: -3px;
    right: -3px;
    height: 4px;
    background: #fff;
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-default);
    transform: translateY(-50%);
    pointer-events: none;
}

/* 数值面板：宽度固定，纵向内容不足时内部滚动 */
.cp-panel {
    width: 180px;
    flex: none;
    display: flex;
    flex-direction: column;
    gap: 10px;
    overflow-y: auto;
}

.cp-swatches {
    flex: none;
    display: flex;
    gap: 0;
    border: 1px solid var(--color-border-1);
    border-radius: var(--base-radius-small);
    overflow: hidden;
    height: 16px;
}

.cp-swatch {
    flex: 1;
}

.cp-grid {
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.cp-row {
    display: flex;
    align-items: center;
    gap: 8px;
}

.cp-row label {
    flex: none;
    width: 30px;
    text-align: right;
    color: var(--color-text-2);
    font-size: var(--base-size-small);
}

.cp-tag {
    width: 18px !important;
}

/* K 行占位：与「RGB 输入框」等宽，使 K 与 C/M/Y 列对齐 */
.cp-gap {
    flex: none;
    width: 84px;
}

.cp-input-rgb {
    flex: none;
    width: 46px;
}

.cp-ro {
    flex: 1;
    min-width: 0;
}

.cp-pct {
    flex: none;
    color: var(--color-text-3);
    font-size: var(--base-size-small);
}

.cp-ro :deep(.arco-input-element) {
    color: var(--color-text-3);
}

.cp-modes {
    flex: none;
    font-size: var(--base-size-small);
}

.cp-mode-active {
    color: rgb(var(--arcoblue-6));
    font-weight: bold;
}

.cp-mode-sub {
    color: var(--color-text-2);
}

/* 底部：常用格式输出（点击复制） */
.cp-out {
    flex: none;
    display: flex;
    gap: var(--base-padding);
    border-top: 1px dashed var(--color-border-2);
    padding-top: 10px;
}

.cp-field {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 6px;
}

.cp-field label {
    flex: none;
    color: var(--color-text-2);
    font-size: var(--base-size-small);
}

.cp-field :deep(.arco-input) {
    flex: 1;
    min-width: 0;
}
</style>