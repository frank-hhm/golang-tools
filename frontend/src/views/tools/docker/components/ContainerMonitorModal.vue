<template>
    <!-- 容器实时监控：页面只传容器身份，采样与绘制都在弹窗内闭环，关掉即停 -->
    <a-modal :visible="visible" :title="`【${name || id || '容器'}】实时监控`" :width="980" :ok-text="'关闭'"
        :hide-cancel="true" @ok="close" @cancel="close">
        <div ref="boxRef" class="mon">
            <!-- 先给结论再看曲线：限额与瞬时流量放最上面 -->
            <div class="mon-base">
                <div class="mon-base-item">
                    <span class="k">内存限额</span>
                    <b>{{ memLimitText }}</b>
                </div>
                <div class="mon-base-item">
                    <span class="k">流量情况</span>
                    <b>上行 {{ fmtRate(netTxRate) }} · 下行 {{ fmtRate(netRxRate) }}</b>
                </div>
                <div class="mon-base-item">
                    <span class="k">采集</span>
                    <b>{{ samples.length }} 点 / 秒级</b>
                </div>
            </div>

            <!-- 采样失败的常见原因：容器刚停或正在重启。不弹 toast，来回 1 秒一次会刷屏 -->
            <a-alert v-if="err" type="warning" :content="err" />

            <div class="mon-grid">
                <div class="mon-card">
                    <div class="mon-head"><span>CPU</span><span class="mon-val">{{ cpuText }}</span></div>
                    <canvas ref="cpuRef" class="mon-canvas"></canvas>
                </div>
                <div class="mon-card">
                    <div class="mon-head"><span>内存</span><span class="mon-val">{{ memText }}</span></div>
                    <canvas ref="memRef" class="mon-canvas"></canvas>
                </div>
                <div class="mon-card">
                    <div class="mon-head"><span>磁盘IO</span><span class="mon-val">{{ diskText }}</span></div>
                    <canvas ref="diskRef" class="mon-canvas"></canvas>
                </div>
                <div class="mon-card">
                    <div class="mon-head"><span>网络IO</span><span class="mon-val">{{ netText }}</span></div>
                    <canvas ref="netRef" class="mon-canvas"></canvas>
                </div>
            </div>
        </div>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { fmtBytes } from "../helpers";

const props = defineProps<{
    visible: boolean;
    /** 容器短 ID（/containers/{id}/stats 接受短 ID） */
    id: string;
    name: string;
}>();

const emit = defineEmits<{ (e: "update:visible", v: boolean): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

/** 图表窗口：1 秒 1 个采样点，60 点 = 最近一分钟 */
const MAX = 60;
/** 后端原始采样（累计计数器也在这里，速率在绘制时按相邻点求差） */
const samples = ref<any[]>([]);
const err = ref("");

const boxRef = ref<HTMLElement>();
const cpuRef = ref<HTMLCanvasElement>();
const memRef = ref<HTMLCanvasElement>();
const diskRef = ref<HTMLCanvasElement>();
const netRef = ref<HTMLCanvasElement>();

let timer: any = null;
let inflight = false;

const last = computed<any>(() => samples.value[samples.value.length - 1] || null);

/* ============================ 文案 ============================ */

/** 后端不限额时 limit=0，图上也照样写 0 B（与列表里 memUsage 的口径一致） */
const memLimitText = computed(() => (last.value && last.value.memLimit > 0 ? fmtBytes(last.value.memLimit) : "0 B"));
const cpuText = computed(() => (last.value ? `${(last.value.cpuPercent || 0).toFixed(1)}%` : "-"));
const memText = computed(() =>
    last.value ? `${fmtBytes(last.value.memUsed)} / ${memLimitText.value} · 缓存 ${fmtBytes(last.value.memCache)}` : "-"
);
const diskText = computed(() => `读 ${fmtRate(diskReadRate.value)} · 写 ${fmtRate(diskWriteRate.value)}`);
const netText = computed(() => `上行 ${fmtRate(netTxRate.value)} · 下行 ${fmtRate(netRxRate.value)}`);

function fmtRate(v: number): string {
    return v > 0 ? fmtBytes(v) + "/s" : "0 B/s";
}

/* ============================ 速率 ============================ */

const diskReadRate = computed(() => currentRate("diskRead"));
const diskWriteRate = computed(() => currentRate("diskWrite"));
const netRxRate = computed(() => currentRate("netRx"));
const netTxRate = computed(() => currentRate("netTx"));

/** 最新一点的瞬时速率；容器重启会让计数器归零，负增量按 0 处理 */
function currentRate(key: string): number {
    const n = samples.value.length;
    if (n < 2) return 0;
    const cur = samples.value[n - 1];
    const prev = samples.value[n - 2];
    const dt = (cur.at - prev.at) / 1000;
    const dv = (cur[key] || 0) - (prev[key] || 0);
    return dt > 0 && dv >= 0 ? dv / dt : 0;
}

/** 整段窗口的速率序列（与采样点一一对应，首点无前值时记 0） */
function rateSeries(key: string): number[] {
    const out: number[] = [];
    for (let i = 0; i < samples.value.length; i++) {
        if (i === 0) {
            out.push(0);
            continue;
        }
        const dt = (samples.value[i].at - samples.value[i - 1].at) / 1000;
        const dv = (samples.value[i][key] || 0) - (samples.value[i - 1][key] || 0);
        out.push(dt > 0 && dv >= 0 ? dv / dt : 0);
    }
    return out;
}

/* ============================ 绘制 ============================ */

type Series = { name: string; color: string; data: number[] };

/** 主题色从 CSS 变量取：换主题后下一次重绘就跟着变，不用在这里写死色值 */
function token(name: string, fallback: string): string {
    const v = getComputedStyle(document.documentElement).getPropertyValue(name).trim();
    return v ? `rgb(${v})` : fallback;
}

/** 把上界抬到"好看"的刻度（1/2/2.5/5/10 × 10^n） */
function niceMax(v: number, floor = 1): number {
    if (!(v > 0)) return floor;
    const target = Math.max(v, floor) * 1.15;
    const mag = Math.pow(10, Math.floor(Math.log10(target)));
    for (const m of [1, 2, 2.5, 5, 10]) {
        if (mag * m >= target) return mag * m;
    }
    return mag * 10;
}

function maxOf(series: number[][]): number {
    let m = 0;
    for (const s of series) for (const v of s) if (v > m) m = v;
    return m;
}

/** 左/中/右三个时间标签（与数据点对齐，空数据不给标签） */
function xTimes(): string[] {
    const n = samples.value.length;
    if (!n) return ["", "", ""];
    const at = (i: number) => {
        const idx = Math.max(0, Math.min(n - 1, i));
        return new Date(samples.value[idx].at).toLocaleTimeString("zh-CN", { hour12: false });
    };
    return [at(0), at(Math.floor((n - 1) / 2)), at(n - 1)];
}

/**
 * 一张折线图：横向网格 + 左侧刻度 + 底部三个时间 + 顶部居中图例。
 * 画布尺寸按 CSS 尺寸 × DPI 重设，避免 125%/150% 缩放下发糊
 */
function paint(canvas: HTMLCanvasElement | undefined, series: Series[], max: number, fmt: (v: number) => string) {
    if (!canvas) return;
    const w = canvas.clientWidth;
    const h = canvas.clientHeight;
    if (!w || !h) return; // 弹窗还没展开/被隐藏时没有尺寸，等下一次 tick 再画
    const dpr = window.devicePixelRatio || 1;
    const pw = Math.round(w * dpr);
    const ph = Math.round(h * dpr);
    if (canvas.width !== pw || canvas.height !== ph) {
        canvas.width = pw;
        canvas.height = ph;
    }
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
    ctx.clearRect(0, 0, w, h);

    const labelColor = token("--color-text-3", "rgb(134, 144, 156)");
    const gridColor = token("--color-border-2", "rgb(229, 230, 235)");
    const padL = 58;
    const padR = 10;
    const padT = 24;
    const padB = 22;
    const cw = Math.max(10, w - padL - padR);
    const ch = Math.max(10, h - padT - padB);
    const rows = 4;

    ctx.font = '11px "Segoe UI", system-ui, sans-serif';
    ctx.textBaseline = "middle";

    // 横向网格 + 左侧刻度（自上而下从 max 到 0）
    for (let i = 0; i <= rows; i++) {
        const y = padT + (ch * i) / rows;
        ctx.strokeStyle = gridColor;
        ctx.globalAlpha = i === rows ? 0.9 : 0.45;
        ctx.beginPath();
        ctx.moveTo(padL, y);
        ctx.lineTo(padL + cw, y);
        ctx.stroke();
        ctx.globalAlpha = 1;
        ctx.fillStyle = labelColor;
        ctx.textAlign = "right";
        ctx.fillText(fmt(max * (1 - i / rows)), padL - 8, y);
    }

    // 底部三个时间点
    const times = xTimes();
    ctx.textAlign = "left";
    ctx.textBaseline = "top";
    ctx.fillStyle = labelColor;
    times.forEach((t, i) => {
        if (!t) return;
        const x = padL + (cw * i) / 2;
        ctx.textAlign = i === 0 ? "left" : i === 2 ? "right" : "center";
        ctx.fillText(t, x, padT + ch + 6);
    });

    // 折线：新点贴右，数据不满一屏时左边留白，像常见的实时监控那样从右往左长
    const span = MAX - 1;
    for (const s of series) {
        const n = s.data.length;
        if (!n) continue;
        ctx.strokeStyle = s.color;
        ctx.lineWidth = 1.5;
        ctx.beginPath();
        for (let i = 0; i < n; i++) {
            const x = padL + (cw * (span - n + 1 + i)) / span;
            const y = padT + ch - (Math.min(Math.max(s.data[i], 0), max) / max) * ch;
            if (i === 0) ctx.moveTo(x, y);
            else ctx.lineTo(x, y);
        }
        ctx.stroke();
        // 线尾一个实心点：一眼看出当前值在哪
        const tipY = padT + ch - (Math.min(Math.max(s.data[n - 1], 0), max) / max) * ch;
        ctx.fillStyle = s.color;
        ctx.beginPath();
        ctx.arc(padL + cw, tipY, 2, 0, Math.PI * 2);
        ctx.fill();
    }

    // 图例：圆点 + 名称，居中放在绘图区上方
    const widths = series.map((s) => ctx.measureText(s.name).width + 16);
    let lx = padL + (cw - widths.reduce((a, b) => a + b, 0) - (series.length - 1) * 12) / 2;
    ctx.textBaseline = "middle";
    ctx.textAlign = "left";
    series.forEach((s, i) => {
        ctx.fillStyle = s.color;
        ctx.beginPath();
        ctx.arc(lx + 4, padT / 2, 4, 0, Math.PI * 2);
        ctx.fill();
        ctx.fillStyle = labelColor;
        ctx.fillText(s.name, lx + 12, padT / 2);
        lx += widths[i] + 12;
    });
}

function drawAll() {
    const blue = token("--blue-6", "rgb(22, 93, 255)");
    const pink = token("--magenta-6", "rgb(217, 91, 199)");
    const green = token("--green-6", "rgb(0, 180, 42)");
    const orange = token("--orange-6", "rgb(255, 125, 0)");

    // CPU 固定 0~100%：动态上限会让"几乎没占用"看起来也像有起伏
    paint(cpuRef.value, [{ name: "CPU", color: blue, data: samples.value.map((s) => s.cpuPercent || 0) }], 100,
        (v) => `${Math.round(v)}%`);

    const memUsed = samples.value.map((s) => s.memUsed || 0);
    const memCache = samples.value.map((s) => s.memCache || 0);
    const memLimit = last.value?.memLimit || 0;
    paint(memRef.value,
        [
            { name: "内存", color: blue, data: memUsed },
            { name: "缓存", color: pink, data: memCache },
        ],
        memLimit > 0 ? memLimit : niceMax(maxOf([memUsed, memCache]), 1024),
        (v) => fmtBytes(v) === "-" ? "0" : fmtBytes(v));

    const dRead = rateSeries("diskRead");
    const dWrite = rateSeries("diskWrite");
    paint(diskRef.value,
        [
            { name: "读取", color: pink, data: dRead },
            { name: "写入", color: green, data: dWrite },
        ],
        niceMax(maxOf([dRead, dWrite]), 1024),
        (v) => (v >= 1024 ? fmtBytes(v) : `${Math.round(v)} B`));

    const nRx = rateSeries("netRx");
    const nTx = rateSeries("netTx");
    paint(netRef.value,
        [
            { name: "上行", color: orange, data: nTx },
            { name: "下行", color: blue, data: nRx },
        ],
        niceMax(maxOf([nRx, nTx]), 1024),
        (v) => (v >= 1024 ? fmtBytes(v) : `${Math.round(v)} B`));
}

/* ============================ 采样 ============================ */

function stop() {
    if (timer) {
        clearTimeout(timer);
        timer = null;
    }
}

/** setTimeout 链而不是 setInterval：一次 daemon 卡顿不会把请求堆起来 */
async function tick() {
    if (!props.visible) return;
    if (inflight) {
        timer = setTimeout(tick, 400);
        return;
    }
    inflight = true;
    try {
        const m = await GoTools.DockerSrvContainerMetrics(props.id);
        if (m) {
            samples.value.push(m);
            if (samples.value.length > MAX) samples.value.splice(0, samples.value.length - MAX);
            err.value = "";
        }
    } catch (e: any) {
        // 容器停了/重启中会走到这里；保留旧曲线，顶上换 a-alert 说明
        err.value = "采样失败：" + (e?.message || e);
    } finally {
        inflight = false;
        drawAll();
        if (props.visible) timer = setTimeout(tick, 1000);
    }
}

watch(
    () => props.visible,
    async (v) => {
        stop();
        if (!v) return;
        samples.value = [];
        err.value = "";
        await nextTick();
        drawAll(); // 先把坐标轴画出来，别让用户盯着空白
        tick();
    }
);

function close() {
    emit("update:visible", false);
}

onMounted(() => window.addEventListener("resize", drawAll));
onBeforeUnmount(() => {
    stop();
    window.removeEventListener("resize", drawAll);
});
</script>

<style scoped>
.mon {
    display: flex;
    flex-direction: column;
    gap: 10px;
}

/* 基础信息条：一行放完，窄窗口自动换行 */
.mon-base {
    display: flex;
    flex-wrap: wrap;
    gap: 8px 28px;
    align-items: center;
    padding: 8px 12px;
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-small);
    font-size: 13px;
}

.mon-base-item {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
}

.mon-base-item .k {
    color: var(--color-text-3);
    flex: none;
}

.mon-base-item b {
    font-weight: 500;
    color: var(--color-text-1);
    font-variant-numeric: tabular-nums;
}

.mon-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
}

.mon-card {
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-small);
    padding: 6px 8px 2px;
    min-width: 0;
}

.mon-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    padding: 0 2px 2px;
    font-size: 13px;
    color: var(--color-text-2);
}

.mon-val {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    font-variant-numeric: tabular-nums;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

/* 画布高度固定，宽度跟卡片走（重绘时按 clientWidth 重设像素尺寸） */
.mon-canvas {
    display: block;
    width: 100%;
    height: 190px;
}
</style>
