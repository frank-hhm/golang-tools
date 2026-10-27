<template>
    <!-- 总览：数据全部来自父页面已拉到的容器 / 镜像 / 仓库清单，不额外打接口。
         版式对齐首页：区块用 a-card（标题走 #title，筛选/搜索/版本提示走 #extra），
         指标用首页同款 stat-card（彩色圆角图标 + 标签 + 大号数值 + 明细） -->
    <div class="ov">
        <a-card class="card-head-tool ov-card">
            <template #title>
                <span class="ov-title">资源概览</span>
            </template>
            <template #extra>
                <!-- 引擎版本属于「这一屏数据从哪来」，放卡片右上角，不占正文 -->
                <a-tooltip :content="engineLabel || '引擎版本未知'">
                    <span class="ov-note">{{ engineLabel || '—' }}</span>
                </a-tooltip>
            </template>

            <a-row :gutter="[12, 12]">
                <a-col v-for="c in cards" :key="c.key" :xs="24" :sm="12" :md="8" :xl="4">
                    <common-stat-card :icon="c.icon" :icon-value="c.p" :tone="c.tone" :label="c.title" :value="c.value"
                        :detail="c.sub" :tip="c.tip" :ready="c.ready" />
                </a-col>
            </a-row>
        </a-card>

        <!-- fill：容器列表吃掉可视区剩下的高度，卡片墙过多时在卡片内部滚，卡片下沿不会悬空 -->
        <a-card class="card-head-tool ov-card fill">
            <template #title>
                <span class="ov-title">容器列表</span>
                <span v-if="filtered.length" class="ov-count">{{ filtered.length }}</span>
            </template>
            <template #extra>
                <a-space :size="8" wrap>
                    <a-select v-model="stateFilter" size="small" :options="stateOptions" class="ov-select" />
                    <a-input v-model="keyword" size="small" class="ov-search" placeholder="搜索容器名称或镜像名" allow-clear>
                        <template #prefix><icon-search /></template>
                    </a-input>
                    <a-button size="small" :loading="loading" @click="emit('refresh')">
                        <template #icon><icon-refresh /></template>
                    </a-button>
                </a-space>
            </template>

            <div v-if="!filtered.length" class="ov-empty">
                <a-empty :image="false" :description="containers.length ? '没有匹配的容器' : '暂无容器'" />
            </div>
            <div v-else class="cc-grid">
                <div v-for="c in filtered" :key="c.id" class="cc" :class="{ 'is-off': !isRunning(c) }"
                    @click="onOpen(c)">
                    <div class="cc-top">
                        <div class="cc-logo">
                            <icon-computer :size="20" />
                        </div>
                        <div class="cc-meta">
                            <div class="cc-name-row">
                                <a-tooltip :content="c.name || '(未命名)'">
                                    <span class="cc-name">{{ c.name || '(未命名)' }}</span>
                                </a-tooltip>
                                <com-badge :status="isRunning(c) ? 'success' : 'normal'" :animated="isRunning(c)" />
                            </div>
                            <a-tooltip :content="c.image || '-'">
                                <div class="cc-desc mono">{{ c.image || '-' }}</div>
                            </a-tooltip>
                        </div>

                    </div>
                    <a-space>
                        <div class="cc-time">创建时间：{{ fmtTime(c.created) }}</div>
                        <!-- 卡片整体可点：一句“监控/日志 →”把去向说清楚，别让人靠猜 -->
                        <a-button class="cc-open" size="mini">{{ isRunning(c) ? '监控' : '日志' }}</a-button>
                        <!-- 终端单独一个入口（会拦掉冒泡，不然卡片点击会盖过去） -->
                        <a-button v-if="isRunning(c)" class="cc-open" size="mini" @click.stop="emit('open-term', c)">终端</a-button>
                    </a-space>

                    <div class="cc-bars">
                        <div class="bar-row">
                            <span class="k">CPU 使用率</span>
                            <span class="v">{{ isRunning(c) ? (c.cpuPct || 0).toFixed(1) + '%' : '-' }}</span>
                            <span class="bar">
                                <i :style="{ width: barWidth(c.cpuPct) }"></i>
                            </span>
                        </div>
                        <div class="bar-row">
                            <span class="k">内存使用情况</span>
                            <!-- memUsage 是 "12.3 MiB / 15.6 GiB" 这种长串，卡片窄时省略号收尾，悬停看全 -->
                            <a-tooltip :content="c.memUsage || '-'" :disabled="!isRunning(c) || !c.memUsage">
                                <span class="v">{{ isRunning(c) ? c.memUsage || '-' : '-' }}</span>
                            </a-tooltip>
                            <span class="bar">
                                <i :style="{ width: barWidth(c.memPct) }"></i>
                            </span>
                        </div>
                    </div>
                </div>
            </div>
        </a-card>
    </div>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
// 卡片图标走单文件导入，别从 icon 的 barrel 里拿（会把整套图标都带进包）
import IconComputer from "@arco-design/web-vue/es/icon/icon-computer";
import dayjs from "dayjs";
import { fmtBytes } from "../helpers";

const props = defineProps<{
    /** 容器清单（含已停止，取决于容器页的「显示已停止」开关） */
    containers: any[];
    images: any[];
    registries: any[];
    /** 网络（含内置 bridge/host/none，自带 builtin 标记） */
    networks: any[];
    /** 存储卷（inUse = 引用它的容器数） */
    volumes: any[];
    /** 按 compose 标签聚合出的编排项目 */
    composes: any[];
    loading: boolean;
    /** 引擎版本串，写在资源概览右上角，读不到就不显示 */
    engineLabel?: string;
}>();

const emit = defineEmits<{
    (e: "refresh"): void;
    (e: "open-monitor", c: any): void;
    (e: "open-logs", c: any): void;
    (e: "open-term", c: any): void;
}>();

/* ============================ 资源概览 ============================ */

interface StatCard {
    key: string;
    title: string;
    value: string;
    sub: string;
    ready: boolean;
    /** 图标名，见 common-stat-icon（container / compose / image / network / volume / registry） */
    icon: string;
    /** 活跃比例 0-100：容器=运行占比、网络=有容器接入占比、存储卷=在用占比 */
    p?: number;
    /** stat-circle 的色调后缀（tone-blue / tone-cyan ...） */
    tone: string;
    tip: string;
}

const cards = computed<StatCard[]>(() => {
    const running = props.containers.filter((c) => c.state === "running").length;
    // 镜像体积可能是 -1（服务端没给），这种不计入合计，避免出现负的占用空间
    const imgSize = props.images.reduce((n, i) => n + (i.size > 0 ? i.size : 0), 0);
    // 内置网络（bridge/host/none）是 daemon 自带的，单列出来才不会看着“怎么这么多”
    const customNets = props.networks.filter((n) => !n.builtin).length;
    const netsOn = props.networks.filter((n) => (n.containers || 0) > 0).length;
    const volsOn = props.volumes.filter((v) => (v.inUse || 0) > 0).length;
    const projOn = props.composes.filter((p) => p.status === "running").length;
    const ctrInProjects = props.composes.reduce((n, p) => n + (p.total || 0), 0);
    return [
        {
            key: "container",
            title: "容器数量",
            value: String(props.containers.length),
            sub: `${running} 个运行中`,
            ready: true,
            icon: "container",
            p: props.containers.length ? (running / props.containers.length) * 100 : 0,
            tone: "blue",
            tip: running !== props.containers.length ? "统计范围跟随容器页的「显示已停止」开关" : "",
        },
        {
            key: "compose",
            title: "编排数量",
            value: String(props.composes.length),
            sub: `${projOn} 个全部运行`,
            ready: true,
            icon: "compose",
            tone: "purple",
            tip: ctrInProjects
                ? `按 com.docker.compose.* 标签聚合，共 ${ctrInProjects} 个容器；Engine API 没有 compose 端点`
                : "按 com.docker.compose.* 标签聚合，Engine API 没有 compose 端点",
        },
        {
            key: "image",
            title: "镜像数量",
            value: String(props.images.length),
            sub: imgSize > 0 ? `占用空间 ${fmtBytes(imgSize)}` : "占用空间未知",
            ready: true,
            icon: "image",
            tone: "cyan",
            tip: imgSize > 0 ? "本地镜像层体积合计（多 tag 共用层会重复计）" : "",
        },
        {
            key: "network",
            title: "网络数量",
            value: String(props.networks.length),
            sub: `自定义 ${customNets} 个`,
            ready: true,
            icon: "network",
            p: props.networks.length ? (netsOn / props.networks.length) * 100 : 0,
            tone: "orange",
            tip: netsOn ? `${netsOn} 个网络里有容器接入` : "暂无容器接入任何网络",
        },
        {
            key: "volume",
            title: "存储卷数量",
            value: String(props.volumes.length),
            sub: `${volsOn} 个在用`,
            ready: true,
            icon: "volume",
            p: props.volumes.length ? (volsOn / props.volumes.length) * 100 : 0,
            tone: "green",
            tip: volsOn !== props.volumes.length ? "其余卷没有任何容器引用，可在存储卷页清理" : "全部卷都有容器引用",
        },
        {
            key: "registry",
            title: "镜像仓库数量",
            value: String(props.registries.length),
            sub: "已配置的仓库",
            ready: true,
            icon: "registry",
            tone: "blue",
            tip: "",
        },
    ];
});

/* ============================ 容器卡片 ============================ */

const keyword = ref("");
const stateFilter = ref<"all" | "running" | "stopped">("all");
const stateOptions = [
    { label: "全部", value: "all" },
    { label: "运行中", value: "running" },
    { label: "已停止", value: "stopped" },
];

const filtered = computed(() => {
    const kw = keyword.value.trim().toLowerCase();
    return props.containers.filter((c) => {
        if (stateFilter.value === "running" && c.state !== "running") return false;
        if (stateFilter.value === "stopped" && c.state === "running") return false;
        if (!kw) return true;
        return `${c.name || ""} ${c.image || ""}`.toLowerCase().includes(kw);
    });
});

function isRunning(c: any): boolean {
    return c.state === "running";
}

/** 运行时点击进监控，停着的看日志：两种状态都有可去的地方 */
function onOpen(c: any) {
    if (isRunning(c)) emit("open-monitor", c);
    else emit("open-logs", c);
}

/** 多核容器 CPU 能超过 100%，进度条只按 100% 收，数字仍显示真实值 */
function barWidth(pct: number): string {
    const v = Number.isFinite(pct) ? Math.max(0, Math.min(100, pct)) : 0;
    return `${v.toFixed(1)}%`;
}

function fmtTime(unixSec: number): string {
    if (!unixSec) return "-";
    return dayjs(unixSec * 1000).format("YYYY-MM-DD HH:mm:ss");
}
</script>

<style scoped>
.ov {
    display: flex;
    flex-direction: column;
    gap: var(--base-margin);
    padding: var(--base-padding);
    /* 撑满可视区：内容少时由容器列表卡拉伸到底，内容多时整体照常超出去给外层滚 */
    min-height: 100%;
    box-sizing: border-box;
}

/* 概览卡按内容高度，容器列表卡占剩余 */
.ov-card {
    flex: none;
}

.ov-card.fill {
    flex: 1;
    /* 窗口很矮时保底高度，再矮就交给外层滚动，不把卡片压成一条 */
    min-height: 220px;
    display: flex;
    flex-direction: column;
}

/* Arco 卡片默认是 block：要让它垂直分配空间，本体与 body 都得是 flex 列 */
.ov-card.fill :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.ov-count {
    margin-left: 6px;
    padding: 0 6px;
    border-radius: 8px;
    background: var(--color-fill-2);
    color: var(--color-text-3);
    font-size: 11px;
    font-weight: normal;
    line-height: 16px;
    vertical-align: 1px;
}

.ov-title {
    font-size: var(--base-size-default);
    font-weight: 600;
}

/* 版本提示：右对齐的一行小字，长串省略，悬停看全 */
.ov-note {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    max-width: 320px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    display: inline-block;
    vertical-align: middle;
}

.ov-select {
    width: 96px;
}

.ov-search {
    width: 220px;
}

.ov-empty {
    flex: 1;
    min-height: 120px;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: 24px 0;
}

/* 容器卡片墙：与首页 svc-cards 同款栅格（随宽度自适应，卡片少时靠左不拉伸）
   高度吃掉卡片 body 的剩余空间并在内部滚动，避免把整页顶高 */
.cc-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
    gap: var(--base-margin);
    align-content: start;
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding-right: 2px;
}

.cc {
    display: flex;
    flex-direction: column;
    gap: var(--base-margin);
    min-width: 0;
    padding: var(--base-padding);
    border: 1px solid var(--color-border-1);
    border-radius: var(--base-radius-default);
    cursor: pointer;
    transition: border-color 0.15s, box-shadow 0.15s;
}

.cc:hover {
    border-color: rgb(var(--primary-5));
    box-shadow: 0 4px 16px rgba(var(--primary-6), 0.08);
}

.cc-top {
    display: flex;
    align-items: center;
    gap: var(--base-margin);
    min-width: 0;
}

.cc-logo {
    width: 36px;
    height: 36px;
    border-radius: var(--base-radius-default);
    display: flex;
    align-items: center;
    justify-content: center;
    flex: none;
    background: rgba(var(--arcoblue-6), 0.08);
    color: rgb(var(--arcoblue-6));
}

.cc.is-off .cc-logo {
    background: var(--color-fill-2);
    color: var(--color-text-3);
}

.cc-meta {
    flex: 1 1 auto;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
}

.cc-name-row {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
}

.cc-name {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--base-size-default);
    font-weight: 600;
    color: var(--color-text-1);
}

.cc-desc {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.cc-open {
    flex: none;
    font-size: var(--base-size-small);
    color: rgb(var(--primary-6));
}

.cc-time {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

/* 指标行：标签 / 数值 / 进度条三段，进度条吃掉剩余宽度 */
.cc-bars {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding-top: var(--base-margin);
    border-top: 1px dashed var(--color-border-2);
}

.bar-row {
    display: grid;
    grid-template-columns: auto auto 1fr;
    align-items: center;
    gap: 8px;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.bar-row .v {
    color: var(--color-text-1);
    font-variant-numeric: tabular-nums;
    text-align: right;
    max-width: 150px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.bar {
    position: relative;
    display: block;
    height: 4px;
    min-width: 40px;
    border-radius: 2px;
    background: var(--color-fill-2);
    overflow: hidden;
}

.bar i {
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    border-radius: 2px;
    background: rgb(var(--primary-5));
    transition: width 0.3s;
}

.mono {
    font-family: Consolas, monospace;
}
</style>
