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
 * Canvas 指针时钟（美化版，视觉对齐参考设计稿）：
 *  - 外圈粗圆环 + 表盘内阴影（ring 内侧一圈柔和暗色 inner shadow）
 *  - 粉色发光秒针（shadowBlur 光晕），连续平滑走动
 *  - 盘面信息跑动：年份大字水印、"M月D日"、"星期X" 药丸徽章、"HH:mm:ss:百分秒" 毫秒跳动
 *  - size 控制边长，所有刻度/文字/指针按 rem 等比缩放；按 devicePixelRatio 渲染，高分屏不发虚
 *  - rAF 每帧重绘（毫秒信息持续跑动），组件卸载自动停掉动画
 */
import { onBeforeUnmount, onMounted, ref, watch } from "vue";

interface Props {
    /** 表盘边长（px，正方形） */
    size?: number;
    /** 最外圈圆环颜色（内阴影也据此着色） */
    ringColor?: string;
    /** 表盘底色 */
    faceColor?: string;
    /** 数字/日期/时间文字颜色 */
    numColor?: string;
    /** 时针颜色 */
    hourColor?: string;
    /** 分针颜色 */
    minuteColor?: string;
    /** 秒针颜色（含光晕与中心轴点） */
    secondColor?: string;
    /** 是否显示盘面日期/星期/年份/毫秒时间信息 */
    showInfo?: boolean;
}

const props = withDefaults(defineProps<Props>(), {
    size: 320,
    ringColor: "#5b1578",
    faceColor: "#ffffff",
    numColor: "#8e24aa",
    hourColor: "#5b1578",
    minuteColor: "#9c36b7",
    secondColor: "#ff4fa3",
    showInfo: true,
});

const el = ref<HTMLCanvasElement>();
let raf = 0;

const WEEKDAYS = ["星期日", "星期一", "星期二", "星期三", "星期四", "星期五", "星期六"];

/** #rrggbb → rgba(r,g,b,a)，用于内阴影/水印/徽章底色 */
function rgba(hex: string, alpha: number): string {
    const m = /^#?([0-9a-f]{6})$/i.exec(hex.trim());
    if (!m) return hex;
    const v = parseInt(m[1], 16);
    return `rgba(${(v >> 16) & 255},${(v >> 8) & 255},${v & 255},${alpha})`;
}

function pad2(n: number): string {
    return String(n).padStart(2, "0");
}

function draw() {
    const canvas = el.value;
    if (!canvas) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;

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

    // ① 外圈粗圆环
    ctx.beginPath();
    ctx.lineWidth = ringW;
    ctx.strokeStyle = props.ringColor;
    ctx.arc(0, 0, r - ringW / 2, 0, 2 * Math.PI);
    ctx.stroke();

    // ② 表盘底 + 内阴影：裁剪到表盘圆内，先铺底色，再沿边缘叠一圈向内的柔和渐变
    ctx.save();
    ctx.beginPath();
    ctx.arc(0, 0, faceR, 0, 2 * Math.PI);
    ctx.clip();
    ctx.fillStyle = props.faceColor;
    ctx.fillRect(-faceR, -faceR, faceR * 2, faceR * 2);
    // 内阴影：从 0.55R 起三次曲线平滑渐深，多站点过渡，避免硬边/色带
    const shade = ctx.createRadialGradient(0, 0, faceR * 0.55, 0, 0, faceR);
    for (let i = 0; i <= 12; i++) {
        const t = i / 12;
        shade.addColorStop(t, rgba(props.ringColor, 0.3 * t * t * t));
    }
    ctx.fillStyle = shade;
    ctx.fillRect(-faceR, -faceR, faceR * 2, faceR * 2);

    if (props.showInfo) {
        // ③ 年份大字水印（跑动信息的一部分，垫在指针下方）
        ctx.font = `bold ${52 * rem}px Arial`;
        ctx.textAlign = "center";
        ctx.textBaseline = "middle";
        ctx.fillStyle = rgba(props.numColor, 0.08);
        ctx.fillText(String(now.getFullYear()), 0, 2 * rem);
    }
    ctx.restore();

    // ④ 数字 1~12（12 在正上方，顺时针）
    ctx.font = `bold ${15 * rem}px Arial`;
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    ctx.fillStyle = props.numColor;
    for (let i = 1; i <= 12; i++) {
        const rad = (2 * Math.PI / 12) * i - Math.PI / 2;
        ctx.fillText(String(i), Math.cos(rad) * (faceR - 26 * rem), Math.sin(rad) * (faceR - 26 * rem));
    }

    // ⑤ 日期 / 星期 / 毫秒时间 跑动信息（淡色，与年份水印同一观感）
    if (props.showInfo) {
        ctx.fillStyle = rgba(props.numColor, 0.35);
        // 上方：M月D日
        ctx.font = `bold ${13 * rem}px Arial`;
        ctx.fillText(`${now.getMonth() + 1}月${now.getDate()}日`, 0, -faceR * 0.42);
        // 下方：HH:mm:ss:百分秒（毫秒级跳动）
        ctx.fillText(`${pad2(now.getHours())}:${pad2(now.getMinutes())}:${pad2(now.getSeconds())}:${pad2(Math.floor(ms / 10))}`, 0, faceR * 0.46);
        // 右侧：星期药丸徽章
        const week = WEEKDAYS[now.getDay()];
        ctx.font = `bold ${9 * rem}px Arial`;
        const tw = ctx.measureText(week).width;
        const bw = tw + 12 * rem;
        const bh = 18 * rem;
        const bx = faceR * 0.44;
        roundRect(ctx, bx - bw / 2, -bh / 2, bw, bh, 4 * rem);
        ctx.fillStyle = rgba(props.numColor, 0.08);
        ctx.fill();
        ctx.fillStyle = rgba(props.numColor, 0.35);
        ctx.fillText(week, bx, 0.5 * rem);
    }

    // ⑥ 时针：粗圆头短针
    drawHand(ctx, (h / 12) * 2 * Math.PI, 9 * rem, faceR * 0.46, props.hourColor);
    // ⑦ 分针：粗圆头长针
    drawHand(ctx, (m / 60) * 2 * Math.PI, 7 * rem, faceR * 0.72, props.minuteColor);
    // ⑧ 秒针：粉色细针 + 光晕，尾部过中心
    ctx.save();
    ctx.shadowColor = rgba(props.secondColor, 0.8);
    ctx.shadowBlur = 8 * rem;
    drawHand(ctx, (s / 60) * 2 * Math.PI, 2 * rem, faceR * 0.86, props.secondColor, 14 * rem);
    ctx.restore();

    // ⑨ 中心轴点（秒针同色）
    ctx.beginPath();
    ctx.fillStyle = props.secondColor;
    ctx.arc(0, 0, 6 * rem, 0, 2 * Math.PI);
    ctx.fill();

    ctx.restore();
}

/** 画一根圆头指针：angle 为 12 点方向起顺时针弧度 */
function drawHand(ctx: CanvasRenderingContext2D, angle: number, width: number, len: number, color: string, tail = 8) {
    ctx.save();
    ctx.beginPath();
    ctx.rotate(angle - Math.PI / 2);
    ctx.strokeStyle = color;
    ctx.lineWidth = width;
    ctx.lineCap = "round";
    ctx.moveTo(0, tail);
    ctx.lineTo(0, -len);
    ctx.stroke();
    ctx.restore();
}

/** 圆角矩形路径（徽章底） */
function roundRect(ctx: CanvasRenderingContext2D, x: number, y: number, w: number, h: number, rad: number) {
    ctx.beginPath();
    ctx.moveTo(x + rad, y);
    ctx.arcTo(x + w, y, x + w, y + h, rad);
    ctx.arcTo(x + w, y + h, x, y + h, rad);
    ctx.arcTo(x, y + h, x, y, rad);
    ctx.arcTo(x, y, x + w, y, rad);
    ctx.closePath();
}

/** 毫秒信息持续跑动，rAF 每帧重绘 */
function loop() {
    draw();
    raf = requestAnimationFrame(loop);
}

onMounted(() => {
    loop();
});
onBeforeUnmount(() => cancelAnimationFrame(raf));

// size/颜色/信息开关变化立即重绘（下一帧也会生效，这里消除视觉延迟）
watch(() => [props.size, props.showInfo, props.ringColor, props.faceColor, props.numColor, props.hourColor, props.minuteColor, props.secondColor], draw);
</script>
<style scoped>
.common-canvas-clock {
    display: block;
}
</style>
