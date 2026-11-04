<template>
    <canvas ref="el" class="common-canvas-clock" :style="{ width: size + 'px', height: size + 'px' }"></canvas>
</template>
<script lang="ts">
export default {
    name: "common-canvas-clock",
};
</script>
<script lang="ts" setup>
/**
 * Canvas 指针时钟（贴合项目 Arco 主题，含暗黑模式）：
 *  - 颜色属性缺省时读 Arco CSS 变量（--primary-6/--color-bg-2/--color-text-1/3），
 *    浅色/暗黑切换（body[arco-theme] 变化）自动换肤；显式传值则固定配色
 *  - 外圈粗圆环 + 表盘内阴影（内阴影随圆环颜色自动着色）
 *  - 主色秒针带光晕，连续平滑走动；中心轴点同色
 *  - 盘面信息跑动：年份大字水印、"M月D日"、"星期X" 药丸徽章、"HH:mm:ss:百分秒" 毫秒跳动
 *  - size 控制边长，所有刻度/文字/指针按 rem 等比缩放；按 devicePixelRatio 渲染，高分屏不发虚
 *  - rAF 每帧重绘（毫秒信息持续跑动），组件卸载自动停掉动画与主题监听
 */
import { onBeforeUnmount, onMounted, ref, watch } from "vue";

interface Props {
    /** 表盘边长（px，正方形） */
    size?: number;
    /** 最外圈圆环颜色；缺省跟随主题主色 */
    ringColor?: string;
    /** 表盘底色；缺省跟随卡片底色 */
    faceColor?: string;
    /** 数字/日期/时间文字颜色；缺省跟随一级文本色 */
    numColor?: string;
    /** 时针颜色；缺省跟随一级文本色 */
    hourColor?: string;
    /** 分针颜色；缺省跟随三级文本色 */
    minuteColor?: string;
    /** 秒针颜色（含光晕与中心轴点）；缺省跟随主题主色 */
    secondColor?: string;
    /** 是否显示盘面日期/星期/年份/毫秒时间信息 */
    showInfo?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
    size: 320,
    ringColor: "",
    faceColor: "",
    numColor: "",
    hourColor: "",
    minuteColor: "",
    secondColor: "",
    showInfo: true,
});

const el = ref<HTMLCanvasElement>();
let raf = 0;

const WEEKDAYS = ["星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"];

/** 缺省颜色跟随的 Arco 主题变量（暗黑模式由 body[arco-theme=dark] 自动整套切换）；
 *  浅色下圆环用中性填充灰（与首页快捷操作格子同源），暗黑下回退主色（fill-2 在暗底上几乎不可见） */
const THEME_TOKENS = {
    ringColor: "--color-fill-2",
    faceColor: "--color-bg-2",
    numColor: "--color-text-1",
    hourColor: "--color-text-1",
    minuteColor: "--color-text-3",
    secondColor: "--primary-6",
} as const;
/** 按明暗模式差异化的 token（仅浅色/暗黑各用不同变量时在此登记）：
 *  暗黑下 text-3 只有 0.5 白、primary 蓝在暗盘上对比不足，分针提亮一档、秒针直接用白 */
const DARK_TOKEN_OVERRIDES: Partial<Record<ColorKey, string>> = {
    ringColor: "--primary-6",
    minuteColor: "--color-text-2",
    secondColor: "--color-text-1",
};
type ColorKey = keyof typeof THEME_TOKENS;
/** 变量读取失败时的兜底（浅色 Arco 默认值） */
const FALLBACK: Record<ColorKey, string> = {
    ringColor: "#f2f3f5",
    faceColor: "#ffffff",
    numColor: "rgb(29,33,41)",
    hourColor: "rgb(29,33,41)",
    minuteColor: "rgb(78,89,105)",
    secondColor: "#165dff",
};

/** 解析并缓存当前配色与字体：显式 prop 优先，否则读 CSS 变量（Arco 三元组变量拼成 rgb()）；
 *  字体跟随 body 的 font-family（style.css 的 Nunito + 系统回退），canvas 不会继承 CSS 字体需显式读取 */
let palette: Record<ColorKey, string> | null = null;
let fontFamily = "sans-serif";
/** 当前是否暗黑模式（resolvePalette 时刷新）：水印淡度等按此差异化 */
let darkMode = false;
/** 水印文字透明度：暗黑下近白水痕更扎眼，统一压到 6 成 */
function wmAlpha(base: number): number {
    return darkMode ? base * 0.6 : base;
}
function resolvePalette(): Record<ColorKey, string> {
    if (palette) return palette;
    const cs = getComputedStyle(document.body);
    fontFamily = cs.fontFamily || "sans-serif";
    const dark = document.body.getAttribute("arco-theme") === "dark";
    darkMode = dark;
    const p = {} as Record<ColorKey, string>;
    for (const k of Object.keys(THEME_TOKENS) as ColorKey[]) {
        if (props[k]) {
            p[k] = props[k];
            continue;
        }
        const token = dark ? DARK_TOKEN_OVERRIDES[k] || THEME_TOKENS[k] : THEME_TOKENS[k];
        let v = cs.getPropertyValue(token).trim();
        if (/^[\d.,\s]+$/.test(v) && v.split(",").length === 3) v = `rgb(${v})`; // Arco 三元组变量（如 --primary-6: 22,93,255）
        p[k] = v || FALLBACK[k];
    }
    palette = p;
    return p;
}

/** 任意 CSS 颜色（#hex / rgb() / rgba() / r,g,b 三元组）→ 带透明度的 rgba() */
function withAlpha(color: string, a: number): string {
    let r: number, g: number, b: number;
    const hex = /^#?([0-9a-f]{3}|[0-9a-f]{6})$/i.exec(color.trim());
    const rgb = /rgba?\(\s*([\d.]+)[,\s]+([\d.]+)[,\s]+([\d.]+)/i.exec(color.trim());
    const trip = /^([\d.]+)\s*,\s*([\d.]+)\s*,\s*([\d.]+)$/.exec(color.trim());
    const m = rgb || trip;
    if (hex) {
        const s = hex[1].length === 3 ? hex[1].split("").map((c) => c + c).join("") : hex[1];
        const v = parseInt(s, 16);
        r = (v >> 16) & 255; g = (v >> 8) & 255; b = v & 255;
    } else if (m) {
        [r, g, b] = [+m[1], +m[2], +m[3]];
    } else {
        return color; // 无法解析（如关键字色）时放弃加透明度
    }
    return `rgba(${r},${g},${b},${a})`;
}

function pad2(n: number): string {
    return String(n).padStart(2, "0");
}

function draw() {
    const canvas = el.value;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const pal = resolvePalette();

    // 逻辑尺寸 size，物理像素按 DPR 放大，保证高分屏清晰
    const dpr = window.devicePixelRatio || 1;
    const px = Math.round(props.size * dpr);
    if (canvas.width !== px || canvas.height !== px) {
        canvas.width = px;
        canvas.height = px;
    }
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);

    const r = props.size / 2; // 半径（逻辑像素）
    const rem = r / 100; // 相对缩放单位
    const ringW = 10 * rem; // 外圈圆环厚度
    const faceR = r - ringW; // 表盘（圆环内侧）半径

    const now = new Date();
    const ms = now.getMilliseconds();
    const s = now.getSeconds() + ms / 1000; // 秒针连续平滑
    const m = now.getMinutes() + s / 60;
    const h = (now.getHours() % 12) + m / 60;

    ctx.clearRect(0, 0, props.size, props.size);
    ctx.save();
    ctx.translate(r, r); // 原点移到表盘中心

    // ① 表盘底：铺满到最外沿（圆环现为半透明，底下不能留透明接缝），
    //    同时避免环与盘抗锯齿边缘间透出白缝
    const baseR = r;
    ctx.beginPath();
    ctx.fillStyle = pal.faceColor;
    ctx.arc(0, 0, baseR, 0, 2 * Math.PI);
    ctx.fill();

    // ② 内阴影：裁剪到同一圆（AA 边缘藏在圆环下），沿边缘叠一圈向内的柔和渐变；
    //    从 0.55R 起三次曲线平滑渐深，多站点过渡，避免硬边/色带
    ctx.save();
    ctx.beginPath();
    ctx.arc(0, 0, baseR, 0, 2 * Math.PI);
    ctx.clip();
    const shade = ctx.createRadialGradient(0, 0, faceR * 0.55, 0, 0, faceR);
    for (let i = 0; i <= 12; i++) {
        const t = i / 12;
        shade.addColorStop(t, withAlpha(pal.ringColor, 0.22 * t * t * t));
    }
    ctx.fillStyle = shade;
    ctx.fillRect(-baseR, -baseR, baseR * 2, baseR * 2);

    if (props.showInfo) {
        // 年份大字水印（跑动信息的一部分，垫在指针下方）
        ctx.font = `bold ${52 * rem}px ${fontFamily}`;
        ctx.textAlign = "center";
        ctx.textBaseline = "middle";
        ctx.fillStyle = withAlpha(pal.numColor, wmAlpha(0.08));
        ctx.fillText(String(now.getFullYear()), 0, 2 * rem);
    }
    ctx.restore();

    // ③ 外圈圆环：压在表盘底与内阴影的接缝上；缺省为中性浅灰（fill-2），实色描边
    ctx.beginPath();
    ctx.lineWidth = ringW;
    ctx.strokeStyle = pal.ringColor;
    ctx.arc(0, 0, r - ringW / 2, 0, 2 * Math.PI);
    ctx.stroke();

    // ④ 数字 1~12（12 在正上方，顺时针，贴近表盘边缘）
    ctx.font = `bold ${15 * rem}px ${fontFamily}`;
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    ctx.fillStyle = pal.numColor;
    for (let i = 1; i <= 12; i++) {
        const rad = (2 * Math.PI / 12) * i - Math.PI / 2;
        ctx.fillText(String(i), Math.cos(rad) * (faceR - 16 * rem), Math.sin(rad) * (faceR - 16 * rem));
    }

    // ⑤ 日期 / 星期 / 毫秒时间 跑动信息（淡色，与年份水印同一观感）
    if (props.showInfo) {
        // 上方：星期（纯文字，与年份水印同色同淡度，加粗补偿可读性）
        ctx.font = `bold ${13 * rem}px ${fontFamily}`;
        ctx.fillStyle = withAlpha(pal.numColor, wmAlpha(0.1));
        ctx.fillText(WEEKDAYS[now.getDay()], 0, -faceR * 0.58);
        // 星期下方：M月D日
        ctx.font = `bold ${13 * rem}px ${fontFamily}`;
        ctx.fillStyle = withAlpha(pal.numColor, wmAlpha(0.08));
        ctx.fillText(`${now.getMonth() + 1}月${now.getDate()}日`, 0, -faceR * 0.38);
        // 下方：HH:mm:ss:百分秒（毫秒级跳动，是持续跑动的有用信息，暗黑下反而略提亮保证可读）
        ctx.font = `bold ${13 * rem}px ${fontFamily}`;
        ctx.fillStyle = withAlpha(pal.numColor, darkMode ? 0.45 : 0.35);
        ctx.fillText(`${pad2(now.getHours())}:${pad2(now.getMinutes())}:${pad2(now.getSeconds())}:${pad2(Math.floor(ms / 10))}`, 0, faceR * 0.46);
    }

    // ⑥ 时针：圆头短针
    drawHand(ctx, (h / 12) * 2 * Math.PI, 4 * rem, faceR * 0.46, pal.hourColor);
    // ⑦ 分针：圆头长针
    drawHand(ctx, (m / 60) * 2 * Math.PI, 2.5 * rem, faceR * 0.6, pal.minuteColor);
    // ⑧ 秒针：主色细针 + 光晕，尾部过中心
    ctx.save();
    ctx.shadowColor = withAlpha(pal.secondColor, 0.8);
    ctx.shadowBlur = 8 * rem;
    drawHand(ctx, (s / 60) * 2 * Math.PI, 2 * rem, faceR * 0.76, pal.secondColor, 14 * rem);
    ctx.restore();

    // ⑨ 中心轴点（秒针同色）
    ctx.beginPath();
    ctx.fillStyle = pal.secondColor;
    ctx.arc(0, 0, 6 * rem, 0, 2 * Math.PI);
    ctx.fill();

    ctx.restore();
}

/** 画一根圆头指针：angle 为 12 点方向起顺时针弧度；针身本就沿 -y（12 点）绘制，
 *  canvas 正旋转即顺时针，直接 rotate(angle)，再减 π/2 会把全部针逆时针偏 90° */
function drawHand(ctx: CanvasRenderingContext2D, angle: number, width: number, len: number, color: string, tail = 8) {
    ctx.save();
    ctx.beginPath();
    ctx.rotate(angle);
    ctx.strokeStyle = color;
    ctx.lineWidth = width;
    ctx.lineCap = "round";
    ctx.moveTo(0, tail);
    ctx.lineTo(0, -len);
    ctx.stroke();
    ctx.restore();
}

/** 毫秒信息持续跑动，rAF 每帧重绘 */
function loop() {
    draw();
    raf = requestAnimationFrame(loop);
}

/** 主题切换（body 的 arco-theme/class/style 变化）时失效配色缓存，下一帧自动换肤 */
let mo: MutationObserver | null = null;

onMounted(() => {
    mo = new MutationObserver(() => (palette = null));
    mo.observe(document.body, { attributes: true, attributeFilter: ["arco-theme", "class", "style"] });
    loop();
});
onBeforeUnmount(() => {
    cancelAnimationFrame(raf);
    mo?.disconnect();
    mo = null;
});

// size/颜色/信息开关变化：失效缓存并立即重绘（下一帧也会生效，这里消除视觉延迟）
watch(
    () => [props.size, props.showInfo, props.ringColor, props.faceColor, props.numColor, props.hourColor, props.minuteColor, props.secondColor],
    () => {
        palette = null;
        draw();
    }
);
</script>
<style scoped>
.common-canvas-clock {
    display: block;
}
</style>
