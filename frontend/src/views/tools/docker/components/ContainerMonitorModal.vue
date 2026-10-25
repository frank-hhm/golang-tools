<template>
    <!-- 容器实时监控：页面只传容器身份，采样与绘制都在弹窗内闭环，关掉即停。
         区块统一用 a-card 承载（标题走 #title、窗口选择/当前值走 #extra），曲线交给 ECharts：
         悬停即出坐标轴提示，比自绘 canvas 多一份“看得回历史”的能力 -->
    <a-modal :visible="visible" :title="`【${name || id || '容器'}】实时监控`" :width="1080" :ok-text="'关闭'"
        :hide-cancel="true" :body-style="{ height: '596px', overflow: 'auto' }" @ok="close" @cancel="close">
        <div class="mon">
            <!-- 先给结论再看曲线：限额与瞬时流量放最上面 -->
            <a-card class="card-head-tool mon-base" size="small">
                <template #title>基础信息</template>
                <template #extra>
                    <!-- 窗口只决定“看多长”，采样一直在存，切窗口不丢数据 -->
                    <a-radio-group v-model="windowSec" type="button" size="small">
                        <a-radio :value="60">1 分钟</a-radio>
                        <a-radio :value="300">5 分钟</a-radio>
                        <a-radio :value="600">10 分钟</a-radio>
                    </a-radio-group>
                </template>

                <a-row :gutter="[24, 6]">
                    <a-col :xs="24" :sm="12" :md="8">
                        <div class="bi">
                            <span class="k">内存限额</span>
                            <b>{{ memLimitText }}</b>
                        </div>
                    </a-col>
                    <a-col :xs="24" :sm="12" :md="8">
                        <div class="bi">
                            <span class="k">流量情况</span>
                            <b>上行 {{ fmtRate(netTxRate) }} · 下行 {{ fmtRate(netRxRate) }}</b>
                        </div>
                    </a-col>
                    <a-col :xs="24" :sm="12" :md="8">
                        <div class="bi">
                            <span class="k">采集</span>
                            <b>{{ samples.length }} 点 · {{ windowLabel }}</b>
                        </div>
                    </a-col>
                </a-row>
            </a-card>

            <!-- 采样失败的常见原因：容器刚停或正在重启。不弹 toast，来回 1 秒一次会刷屏 -->
            <!-- Alert 没有 content 属性，正文必须走默认插槽，否则渲染出空白条 -->
            <a-alert v-if="err" type="warning">{{ err }}</a-alert>

            <div ref="gridRef" class="mon-grid">
                <a-card class="card-head-tool chart-card" size="small">
                    <template #title>CPU</template>
                    <template #extra>
                        <a-tooltip :content="cpuText"><span class="mon-val">{{ cpuText }}</span></a-tooltip>
                    </template>
                    <div ref="cpuRef" class="mon-chart"></div>
                </a-card>
                <a-card class="card-head-tool chart-card" size="small">
                    <template #title>内存</template>
                    <template #extra>
                        <a-tooltip :content="memText"><span class="mon-val">{{ memText }}</span></a-tooltip>
                    </template>
                    <div ref="memRef" class="mon-chart"></div>
                </a-card>
                <a-card class="card-head-tool chart-card" size="small">
                    <template #title>磁盘IO</template>
                    <template #extra>
                        <a-tooltip :content="diskText"><span class="mon-val">{{ diskText }}</span></a-tooltip>
                    </template>
                    <div ref="diskRef" class="mon-chart"></div>
                </a-card>
                <a-card class="card-head-tool chart-card" size="small">
                    <template #title>网络IO</template>
                    <template #extra>
                        <a-tooltip :content="netText"><span class="mon-val">{{ netText }}</span></a-tooltip>
                    </template>
                    <div ref="netRef" class="mon-chart"></div>
                </a-card>
            </div>
        </div>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onBeforeUnmount, ref, watch } from "vue";
import dayjs from "dayjs";
import * as echarts from "echarts/core";
import type { ECharts } from "echarts/core";
import { LineChart } from "echarts/charts";
import { GridComponent, LegendComponent, TooltipComponent } from "echarts/components";
import { CanvasRenderer } from "echarts/renderers";
import { fmtBytes } from "../helpers";

// 按需注册：只引折线图 + 网格/图例/提示，别整包引入（echarts 全量会多背几百 KB）
echarts.use([LineChart, GridComponent, LegendComponent, TooltipComponent, CanvasRenderer]);

const props = defineProps<{
    visible: boolean;
    /** 容器短 ID（/containers/{id}/stats 接受短 ID） */
    id: string;
    name: string;
}>();

const emit = defineEmits<{ (e: "update:visible", v: boolean): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

/** 采样上限：1 秒 1 点，600 点 = 最近 10 分钟（窗口选择只决定显示多长，数据一直留着） */
const HISTORY = 600;
/** 后端原始采样（累计计数器也在这里，速率在渲染时按相邻点求差） */
const samples = ref<any[]>([]);
const err = ref("");

/** 显示窗口（秒）：默认 5 分钟，够看一次抖动又不至于挤成一团 */
const windowSec = ref(300);
const windowLabel = computed(() =>
    windowSec.value === 60 ? "最近 1 分钟" : windowSec.value === 300 ? "最近 5 分钟" : "最近 10 分钟"
);

const gridRef = ref<HTMLElement>();
const cpuRef = ref<HTMLElement>();
const memRef = ref<HTMLElement>();
const diskRef = ref<HTMLElement>();
const netRef = ref<HTMLElement>();

type ChartKey = "cpu" | "mem" | "disk" | "net";
const KEYS: ChartKey[] = ["cpu", "mem", "disk", "net"];
const charts: Partial<Record<ChartKey, ECharts>> = {};

let timer: any = null;
let inflight = false;
let ro: ResizeObserver | null = null;
let sizeTimer: any = null;
/** 关闭后的延迟销毁：等淡出动画走完，否则图上会先白一下 */
let closeTimer: any = null;

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

/** 刻度/提示用：fmtBytes 把 0 写成 "-"，坐标轴原点得写 0 B */
function fmtByte(v: number): string {
    return v > 0 ? fmtBytes(v) : "0 B";
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

/* ============================ 图表 ============================ */

type Point = [number, number];

/** 原值序列：直接按时间轴取 [时间, 值] */
function valuePoints(key: string): Point[] {
    return samples.value.map((s) => [s.at, s[key] || 0] as Point);
}

/** 累计计数器的差分成速率序列（首点无前值记 0） */
function ratePoints(key: string): Point[] {
    const out: Point[] = [];
    for (let i = 0; i < samples.value.length; i++) {
        if (i === 0) {
            out.push([samples.value[i].at, 0]);
            continue;
        }
        const dt = (samples.value[i].at - samples.value[i - 1].at) / 1000;
        const dv = (samples.value[i][key] || 0) - (samples.value[i - 1][key] || 0);
        out.push([samples.value[i].at, dt > 0 && dv >= 0 ? dv / dt : 0]);
    }
    return out;
}

/** 主题色取 CSS 变量（读 body，暗黑主题的变量集挂在 body 上）；换主题后下一帧就跟着变 */
function token(name: string, fallback: string): string {
    const v = getComputedStyle(document.body).getPropertyValue(name).trim();
    if (!v) return fallback;
    return /^(#|rgb|hsl)/i.test(v) ? v : `rgb(${v})`;
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

function maxOf(series: Point[][]): number {
    let m = 0;
    for (const s of series) for (const p of s) if (p[1] > m) m = p[1];
    return m;
}

function elOf(key: ChartKey): HTMLElement | undefined {
    if (key === "cpu") return cpuRef.value;
    if (key === "mem") return memRef.value;
    if (key === "disk") return diskRef.value;
    return netRef.value;
}

/** 首次打开时建实例；已存在就留着复用（关闭只是 dispose，重开再建） */
function ensureCharts() {
    for (const k of KEYS) {
        const el = elOf(k);
        if (!el || charts[k]) continue;
        charts[k] = echarts.init(el);
    }
}

function disposeCharts() {
    for (const k of KEYS) {
        charts[k]?.dispose();
        charts[k] = undefined;
    }
}

function resizeCharts() {
    for (const k of KEYS) charts[k]?.resize();
}

interface ChartCfg {
    series: { name: string; color: string; data: Point[] }[];
    /** Y 轴上限 */
    yMax: number;
    /** Y 轴刻度文案（要短） */
    axisFmt: (v: number) => string;
    /** 悬停提示里的数值文案（可以更长，带单位） */
    tipFmt: (v: number) => string;
    xMin: number;
    xMax: number;
}

function applyChart(key: ChartKey, cfg: ChartCfg) {
    const chart = charts[key];
    if (!chart) return;

    const labelColor = token("--color-text-3", "#86909c");
    const textColor = token("--color-text-1", "#1d2129");
    const gridColor = token("--color-border-2", "#e5e6eb");
    const axisColor = token("--color-text-4", "#c9cdd4");
    const popupBg = token("--color-bg-popup", "#ffffff");
    const { axisFmt, tipFmt } = cfg;

    chart.setOption({
        animation: false,
        textStyle: { color: labelColor, fontSize: 11 },
        grid: { left: 4, right: 12, top: 26, bottom: 0, containLabel: true },
        legend: {
            top: 0,
            left: "center",
            icon: "circle",
            itemWidth: 8,
            itemHeight: 8,
            itemGap: 16,
            textStyle: { color: labelColor, fontSize: 11 },
        },
        tooltip: {
            trigger: "axis",
            backgroundColor: popupBg,
            borderColor: gridColor,
            borderWidth: 1,
            padding: [6, 10],
            extraCssText: "border-radius:6px;box-shadow:0 4px 14px rgba(0,0,0,0.18);",
            axisPointer: {
                type: "line",
                lineStyle: { color: axisColor, width: 1, type: "dashed" },
                // 轴上的时间标签也跟着亮：截图里那个蓝色小框就是它
                label: {
                    show: true,
                    backgroundColor: token("--primary-6", "#165dff"),
                    color: "#fff",
                    padding: [2, 6],
                    borderRadius: 4,
                    formatter: (p: any) => dayjs(p.value).format("HH:mm:ss"),
                },
            },
            // 与截图一致：第一行时间，下面每序列一行「圆点 + 名称：值」
            formatter: (ps: any) => {
                const arr: any[] = Array.isArray(ps) ? ps : [ps];
                if (!arr.length) return "";
                const head = `<div style="color:${labelColor};margin-bottom:3px">时间：${dayjs(arr[0].value?.[0]).format("YYYY-MM-DD HH:mm:ss")}</div>`;
                const rows = arr
                    .map(
                        (p: any) =>
                            `<div style="display:flex;align-items:center;gap:6px;color:${textColor}">` +
                            `<i style="width:8px;height:8px;border-radius:50%;background:${p.color};display:inline-block"></i>` +
                            `<span>${p.seriesName}：${tipFmt(Number(p.value?.[1]) || 0)}</span></div>`
                    )
                    .join("");
                return head + rows;
            },
        },
        xAxis: {
            type: "time",
            min: cfg.xMin,
            max: cfg.xMax,
            axisLine: { lineStyle: { color: gridColor } },
            axisTick: { show: false },
            // hideOverlap：窗口拉长后时间标签会互相压字，让它自己抽稀
            axisLabel: { color: labelColor, hideOverlap: true, formatter: (v: any) => dayjs(v).format("HH:mm:ss") },
            splitLine: { show: false },
        },
        yAxis: {
            type: "value",
            min: 0,
            max: cfg.yMax,
            splitNumber: 4,
            axisLine: { show: false },
            axisTick: { show: false },
            axisLabel: { color: labelColor, formatter: (v: any) => axisFmt(Number(v)) },
            splitLine: { lineStyle: { color: gridColor, opacity: 0.6 } },
        },
        series: cfg.series.map((s) => ({
            name: s.name,
            type: "line",
            showSymbol: false,
            smooth: false,
            lineStyle: { width: 1.5, color: s.color },
            itemStyle: { color: s.color },
            areaStyle: { opacity: 0.1, color: s.color },
            emphasis: { focus: "series" },
            data: s.data,
        })),
    });
}

/** 重画全部四张图：窗口按最近采样点倒推，右端贴住最后一个点 */
function renderAll() {
    ensureCharts();

    const n = samples.value.length;
    const now = Date.now();
    const lastAt = n ? samples.value[n - 1].at : now;
    const firstAt = n ? samples.value[0].at : now;
    const span = windowSec.value * 1000;
    // 数据还没攒满时不要空出一大段：左端跟着最早的点走；一点都没有就先占满整个窗口
    const xMin = n ? Math.max(firstAt, lastAt - span) : lastAt - span;
    const xMax = lastAt;
    const clip = (list: Point[]) => (n ? list.filter((p) => p[0] >= xMin) : list);

    const blue = token("--blue-6", "#165dff");
    const pink = token("--magenta-6", "#d950c7");
    const green = token("--green-6", "#00b42a");
    const orange = token("--orange-6", "#ff7d00");

    // CPU 固定 0~100%：动态上限会让"几乎没占用"看起来也像有起伏
    applyChart("cpu", {
        series: [{ name: "CPU", color: blue, data: clip(valuePoints("cpuPercent")) }],
        yMax: 100,
        axisFmt: (v) => `${Math.round(v)}%`,
        tipFmt: (v) => `${v.toFixed(1)}%`,
        xMin,
        xMax,
    });

    const memUsed = clip(valuePoints("memUsed"));
    const memCache = clip(valuePoints("memCache"));
    const memLimit = last.value?.memLimit || 0;
    applyChart("mem", {
        series: [
            { name: "内存", color: blue, data: memUsed },
            { name: "缓存", color: pink, data: memCache },
        ],
        yMax: memLimit > 0 ? memLimit : niceMax(maxOf([memUsed, memCache]), 1024),
        axisFmt: (v) => fmtByte(v),
        tipFmt: (v) => fmtByte(v),
        xMin,
        xMax,
    });

    const dRead = clip(ratePoints("diskRead"));
    const dWrite = clip(ratePoints("diskWrite"));
    applyChart("disk", {
        series: [
            { name: "读取", color: pink, data: dRead },
            { name: "写入", color: green, data: dWrite },
        ],
        yMax: niceMax(maxOf([dRead, dWrite]), 1024),
        axisFmt: (v) => fmtByte(v),
        tipFmt: (v) => fmtRate(v),
        xMin,
        xMax,
    });

    const nRx = clip(ratePoints("netRx"));
    const nTx = clip(ratePoints("netTx"));
    applyChart("net", {
        series: [
            { name: "上行", color: orange, data: nTx },
            { name: "下行", color: blue, data: nRx },
        ],
        yMax: niceMax(maxOf([nRx, nTx]), 1024),
        axisFmt: (v) => fmtByte(v),
        tipFmt: (v) => fmtRate(v),
        xMin,
        xMax,
    });
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
            if (samples.value.length > HISTORY) samples.value.splice(0, samples.value.length - HISTORY);
            err.value = "";
        }
    } catch (e: any) {
        // 容器停了/重启中会走到这里；保留旧曲线，顶上换 a-alert 说明
        err.value = "采样失败：" + (e?.message || e);
    } finally {
        inflight = false;
        renderAll();
        if (props.visible) timer = setTimeout(tick, 1000);
    }
}

/** 容器换了或换窗口：重画即可，采样不重启 */
watch(windowSec, () => renderAll());

watch(
    () => props.visible,
    async (v) => {
        stop();
        ro?.disconnect();
        ro = null;
        if (sizeTimer) {
            clearTimeout(sizeTimer);
            sizeTimer = null;
        }
        if (!v) {
            closeTimer = setTimeout(() => {
                closeTimer = null;
                disposeCharts();
            }, 260);
            return;
        }
        if (closeTimer) {
            clearTimeout(closeTimer);
            closeTimer = null;
        }
        disposeCharts(); // 重开一律重建，避开指向已移除节点的旧实例
        samples.value = [];
        err.value = "";
        await nextTick();
        ensureCharts();
        renderAll(); // 先把坐标轴画出来，别让用户盯着空白
        bindResize();
        // 弹窗展开动画结束后再量一次宽度：首帧初始化时容器可能还是 0 宽
        sizeTimer = setTimeout(resizeCharts, 320);
        tick();
    }
);

/** 弹窗宽度固定，但窗口缩放/系统缩放会改容器宽：交给 ResizeObserver 重排 */
function bindResize() {
    const el = gridRef.value;
    if (!el || typeof ResizeObserver === "undefined") return;
    ro = new ResizeObserver(() => resizeCharts());
    ro.observe(el);
}

function close() {
    emit("update:visible", false);
}

onBeforeUnmount(() => {
    stop();
    if (sizeTimer) clearTimeout(sizeTimer);
    if (closeTimer) clearTimeout(closeTimer);
    ro?.disconnect();
    ro = null;
    disposeCharts();
});
</script>

<style scoped>
.mon {
    display: flex;
    flex-direction: column;
    gap: 10px;
}

/* 基础信息：卡片头里放窗口选择，正文一行放完，窄窗自动按栅格换行 */
.mon-base :deep(.arco-card-body) {
    padding: 8px 14px;
}

.bi {
    display: flex;
    align-items: baseline;
    gap: 8px;
    min-width: 0;
    font-size: 13px;
}

.bi .k {
    color: var(--color-text-3);
    flex: none;
}

.bi b {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 500;
    color: var(--color-text-1);
    font-variant-numeric: tabular-nums;
}

/* 2×2 图表栅格：窄窗（<860px）单列堆叠，避免曲线被压成一条 */
.mon-grid {
    display: grid;
    grid-template-columns: 1fr 1fr;
    gap: 10px;
}

.chart-card :deep(.arco-card-body) {
    padding: 4px 6px 2px;
}

/* 当前值：卡片头右上的小字，长了省略，悬停看全 */
.mon-val {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    font-variant-numeric: tabular-nums;
    max-width: 300px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    display: inline-block;
    vertical-align: middle;
}

/* 图表容器：宽度跟卡片走，高度固定；ECharts 自己按容器尺寸渲染 */
.mon-chart {
    width: 100%;
    height: 176px;
}

@media (max-width: 860px) {
    .mon-grid {
        grid-template-columns: 1fr;
    }
}
</style>
