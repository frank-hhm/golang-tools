<template>
    <!-- 镜像任务中心：左边清单、右边详情。关闭弹窗不会中断任务，页面右下角的浮标可以再叫回来 -->
    <a-modal :visible="visible" :width="900" :mask-closable="true" @update:visible="onVisible"
        @cancel="onVisible(false)">
        <template #title>
            <a-space :size="8">
                <span>镜像任务</span>
                <span v-if="running.length" class="com-state is-busy">
                    <com-badge status="processing" :animated="true" />
                    {{ running.length }} 个进行中
                </span>
                <span v-else-if="tasks.length" class="com-state is-dim">
                    <com-badge status="normal" />
                    全部结束
                </span>
                <span v-if="tasks.length" class="tc-count">{{ tasks.length }} 条</span>
            </a-space>
        </template>

        <div v-if="!tasks.length" class="tc-empty">
            <a-empty :image="false" description="还没有任务：拉取 / 推送 / 构建 / 导入 / 导出 / 清理都会出现在这里" />
        </div>

        <div v-else class="tc-body">
            <!-- 左：任务清单（无标题卡片），点一行切换右侧详情 -->
            <a-card class="tc-list" size="small" :body-style="{ padding: '6px' }">
                <div v-for="t in tasks" :key="t.id" class="tc-item" :class="{ active: t.id === activeId }"
                    @click="select(t.id)">
                    <div class="tc-item-top">
                        <span class="tc-op">{{ OP_TEXT[t.op] }}</span>
                        <a-tooltip :content="t.name" :disabled="!t.name">
                            <span class="tc-name mono">{{ t.name }}</span>
                        </a-tooltip>
                        <span class="tc-pct">{{ Math.round(t.percent) }}%</span>
                    </div>
                    <div class="tc-bar" :class="stateClass(t)"><i :style="{ width: barWidth(t.percent) }"></i></div>
                    <div class="tc-item-sub">
                        <span class="tc-state" :class="stateClass(t)">{{ taskStatusText(t) }}</span>
                        <span v-if="t.busy && t.speed" class="tc-speed">{{ t.speed }}</span>
                        <span v-if="t.busy && t.stageText" class="tc-stage">{{ t.stageText }}</span>
                    </div>
                </div>
            </a-card>

            <!-- 右：选中任务的详情（无标题卡片） -->
            <a-card v-if="active" class="tc-detail" size="small" :body-style="{ padding: '12px' }">
                <div class="tc-head">
                    <div class="tc-head-line">
                        <span class="tc-op">{{ OP_TEXT[active.op] }}</span>
                        <span class="com-state" :class="taskStatusClass(active)">
                            <com-badge :status="taskStatusBadge(active)"
                                :animated="active.busy && !active.stalled" />
                            {{ taskStatusText(active) }}
                        </span>
                        <a-tag v-if="active.busy && active.speed" size="small" color="arcoblue">{{ active.speed }}</a-tag>
                        <span class="tc-time">{{ timeText(active) }}</span>
                    </div>
                    <a-tooltip :content="active.name" :disabled="!active.name">
                        <div class="tc-title mono">{{ active.name }}</div>
                    </a-tooltip>
                </div>

                <div class="tc-prog">
                    <div class="tc-bar big" :class="stateClass(active)">
                        <i :style="{ width: barWidth(active.percent) }"></i>
                    </div>
                    <span class="tc-bigpct">{{ Math.round(active.percent) }}<em>%</em></span>
                </div>

                <div v-if="hasMeta(active)" class="tc-meta">
                    <span v-if="bytesTrustworthy(active.total)" class="tc-chip">{{ fmtBytes(active.bytes) }} / {{ fmtBytes(active.total) }}</span>
                    <span v-else-if="bytesTrustworthy(active.bytes)" class="tc-chip">{{ fmtBytes(active.bytes) }}</span>
                    <span v-if="active.layers" class="tc-chip">分层 {{ active.doneLayers }}/{{ active.layers }}</span>
                    <span v-if="active.busy && active.stageText" class="tc-chip stage">{{ active.stageText }}</span>
                </div>
                <div v-if="active.note" class="tc-note" :class="{ bad: active.failed, warn: active.busy && active.stalled }">{{ active.note }}</div>

                <!-- 拉取：分层进度用表格，行高统一、看得见每一层在干什么 -->
                <template v-if="active.op === 'pull'">
                    <a-table class="tc-table" :data="layerRows" :pagination="false" size="small"
                        row-key="key" :bordered="false" stripe :scroll="{ y: 236 }">
                    <template #columns>
                        <a-table-column title="分层" :width="132">
                            <template #cell="{ record }">
                                <a-tooltip :content="record.key" :disabled="!record.key">
                                    <span class="tc-layer mono">{{ record.layer }}</span>
                                </a-tooltip>
                            </template>
                        </a-table-column>
                        <a-table-column title="进度">
                            <template #cell="{ record }">
                                <div class="tc-rowprog">
                                    <div class="tc-bar" :class="record.status === 'done' ? 'st-success' : 'st-normal'">
                                        <i :style="{ width: barWidth(record.percent) }"></i>
                                    </div>
                                    <span class="tc-rowpct">{{ Math.round(record.percent || 0) }}%</span>
                                </div>
                            </template>
                        </a-table-column>
                        <a-table-column title="状态" :width="96">
                            <template #cell="{ record }">
                                <span :class="record.status === 'done' ? 'tc-done' : 'dim'">{{
                                    layerStatusText(record.text) }}</span>
                            </template>
                        </a-table-column>
                    </template>
                    <template #empty>
                        <a-empty :image="false" description="等待分层信息…" />
                    </template>
                    </a-table>
                    <!-- 后端把完整过程写进日志文件，这里留一份关键提示（开始/卡住/失败/完成） -->
                    <div v-if="active.logs.length" class="tc-log small">
                        <div v-for="(l, i) in active.logs" :key="'pl' + i" class="tc-log-line">{{ l }}</div>
                    </div>
                </template>
                <!-- 其余任务：逐步日志（构建的 Step 行 / 导入的 Loaded image 行） -->
                <div v-else-if="active.logs.length" class="tc-log">
                    <div v-for="(l, i) in active.logs" :key="'tl' + i" class="tc-log-line">{{ l }}</div>
                </div>
                <div v-else class="tc-placeholder">该任务只上报整体进度，暂无逐步日志</div>
            </a-card>
        </div>

        <!-- 主动作固定到底部：内容区怎么滚都不影响它 -->
        <template #footer>
            <div class="tc-foot">
                <span class="tc-hint">{{ hint }}</span>
                <a-space :size="8">
                    <a-button v-if="active && active.busy" size="small" status="danger" @click="emit('cancel', active.id)">
                        <template #icon><icon-close /></template>取消此任务
                    </a-button>
                    <a-button v-if="running.length > 1" size="small" status="warning" @click="emit('cancel-all')">
                        全部取消
                    </a-button>
                    <a-button v-if="active && active.retry && (!active.busy || active.stalled)" size="small"
                        @click="emit('retry', active.id)">
                        <template #icon><icon-refresh /></template>重试
                    </a-button>
                    <a-button size="small" :disabled="!finished.length" @click="emit('clear')">清空已结束</a-button>
                    <a-button size="small" @click="onVisible(false)">关闭</a-button>
                </a-space>
            </div>
        </template>
    </a-modal>
</template>

<script setup lang="ts">
import { computed } from "vue";
import {
    OP_TEXT, bytesTrustworthy, fmtBytes, layerStatusText, taskProgressStatus, taskStatusBadge, taskStatusClass,
    taskStatusText,
    type ImageTask,
} from "../helpers";

const props = defineProps<{
    visible: boolean;
    /** 页面持有的任务清单（已按开始时间排好） */
    tasks: ImageTask[];
    /** 当前在右侧展示的任务 */
    activeId: string;
}>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "update:activeId", v: string): void;
    (e: "cancel", id: string): void;
    (e: "cancel-all"): void;
    (e: "retry", id: string): void;
    (e: "clear"): void;
}>();

const running = computed(() => props.tasks.filter((t) => t.busy));
const finished = computed(() => props.tasks.filter((t) => !t.busy));
const active = computed(() => props.tasks.find((t) => t.id === props.activeId));

/** 分层进度转表格数据：按层 ID 排序，事件到达顺序不同也不会让行来回跳 */
const layerRows = computed(() => {
    const map = active.value?.layerMap || {};
    return Object.keys(map).sort().map((k) => map[k]);
});

const hint = computed(() => {
    // 卡住的任务最需要的是一个“重来”按钮，先把这句放在最前面
    if (props.tasks.some((t) => t.busy && t.stalled)) {
        return "有任务长时间没有新进度：可以点「重试」重新拉取，或先看下方提示与日志里的最后一层状态";
    }
    if (running.value.length) return "关闭弹窗不会中断任务：页面右下角的浮标可以随时把它叫回这里";
    if (props.tasks.some((t) => t.failed)) return "失败的任务可以先看上方说明，再点「重试」";
    return "所有任务都已结束，点「清空已结束」把清单收拾干净";
});

/** 进度条配色：跟状态标签同一套语义色，扫一眼就知道那一条是在跑、成了还是断了 */
const STATE_CLASS: Record<string, string> = {
    normal: "st-normal",
    success: "st-success",
    danger: "st-danger",
    warning: "st-warning",
};

function stateClass(t: ImageTask): string {
    return STATE_CLASS[taskProgressStatus(t)] || "st-normal";
}

/** 0% 也要留一条底轨：宽度给 0，条子本身还在，不会只剩一个孤零零的圆点 */
function barWidth(percent: number): string {
    const p = Number(percent) || 0;
    return `${Math.min(100, Math.max(0, p))}%`;
}

function hasMeta(t: ImageTask): boolean {
    return !!(bytesTrustworthy(t.total) || bytesTrustworthy(t.bytes) || t.layers || (t.busy && t.stageText));
}

function onVisible(v: boolean) {
    emit("update:visible", v);
}

function select(id: string) {
    emit("update:activeId", id);
}

function clock(t: number): string {
    const d = new Date(t || Date.now());
    const pad = (n: number) => n.toString().padStart(2, "0");
    return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
}

function durText(ms: number): string {
    const s = Math.max(0, Math.round(ms / 1000));
    if (s < 60) return `${s} 秒`;
    if (s < 3600) return `${Math.floor(s / 60)} 分 ${s % 60} 秒`;
    return `${Math.floor(s / 3600)} 小时 ${Math.floor((s % 3600) / 60)} 分`;
}

/** 时间说明只拼已经发生的事实，不在这里跑秒表（没有事件时不会刷新，静止比乱跳好） */
function timeText(t: ImageTask): string {
    if (!t.startedAt) return "";
    if (t.busy) return `${clock(t.startedAt)} 开始`;
    if (t.endedAt) return `${clock(t.startedAt)} → ${clock(t.endedAt)} · 用时 ${durText(t.endedAt - t.startedAt)}`;
    return `${clock(t.startedAt)} 开始`;
}
</script>

<style scoped>
.tc-count {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.tc-empty {
    padding: 24px 0;
}

/* 清单 + 详情：整体高度写死（切任务时窗口不跟着跳），短窗口下随视口收缩 */
.tc-body {
    display: flex;
    gap: 12px;
    height: min(428px, calc(100vh - 230px));
    min-height: 0;
}

/* 两张无标题卡片：卡片本身不滚，滚动交给卡片 body */
.tc-list {
    flex: none;
    width: 292px;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.tc-list :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
}

.tc-item {
    position: relative;
    padding: 8px 10px 8px 12px;
    border-radius: var(--border-radius-small);
    cursor: pointer;
    transition: background-color 0.15s;
}

.tc-item + .tc-item {
    margin-top: 2px;
}

.tc-item:hover {
    background: var(--color-fill-1);
}

/* 选中：底色拉开一层，和 hover 区分开 */
.tc-item.active {
    background: var(--color-fill-2);
}

/* 选中项左侧一道灰色竖条：清单里一眼看出在看哪一条 */
.tc-item.active::before {
    content: "";
    position: absolute;
    left: 0;
    top: 9px;
    bottom: 9px;
    width: 3px;
    border-radius: 2px;
    background: var(--color-text-3);
}

.tc-item-top {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
}

/* 操作类型做成小色块，省得和镜像名混在一起 */
.tc-op {
    flex: none;
    padding: 0 4px;
    border-radius: 2px;
    background: var(--color-fill-2);
    color: var(--color-text-3);
    font-size: 11px;
    line-height: 16px;
}

.tc-item.active .tc-op {
    background: var(--color-bg-2);
}

.tc-name {
    flex: 1;
    min-width: 0;
    font-size: var(--base-size-small);
    color: var(--color-text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.tc-pct {
    flex: none;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    font-variant-numeric: tabular-nums;
}

.tc-item-sub {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
    font-size: 12px;
    color: var(--color-text-3);
}

.tc-state.st-normal {
    color: rgb(var(--arcoblue-6));
}

.tc-state.st-success {
    color: rgb(var(--green-6));
}

.tc-state.st-danger {
    color: rgb(var(--red-6));
}

.tc-state.st-warning {
    color: rgb(var(--orange-6));
}

.tc-speed {
    flex: none;
    color: rgb(var(--arcoblue-6));
    font-variant-numeric: tabular-nums;
}

.tc-stage {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

/* 细进度条：底轨常在，填充按百分比走；进行中用中性灰，不用蓝色 */
.tc-bar {
    position: relative;
    height: 4px;
    margin: 6px 0 5px;
    border-radius: 2px;
    background: var(--color-fill-3);
    overflow: hidden;
}

.tc-bar > i {
    position: absolute;
    top: 0;
    bottom: 0;
    left: 0;
    border-radius: inherit;
    background: var(--color-text-3);
    transition: width 0.25s ease;
}

.tc-bar.st-success > i {
    background: rgb(var(--green-6));
}

.tc-bar.st-danger > i {
    background: rgb(var(--red-6));
}

.tc-bar.st-warning > i {
    background: rgb(var(--orange-6));
}

.tc-detail {
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.tc-detail :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
}

.tc-head {
    display: flex;
    flex-direction: column;
    gap: 4px;
    min-width: 0;
}

.tc-head-line {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
}

.tc-time {
    margin-left: auto;
    flex: none;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    font-variant-numeric: tabular-nums;
}

.tc-title {
    font-size: var(--base-size-default);
    font-weight: 500;
    color: var(--color-text-1);
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

/* 大进度条 + 大号百分比：数字单独占位，不被条子挤成省略号 */
.tc-prog {
    display: flex;
    align-items: center;
    gap: 12px;
    margin: 10px 0 8px;
}

.tc-bar.big {
    flex: 1;
    height: 8px;
    margin: 0;
    border-radius: 4px;
}

.tc-bigpct {
    flex: none;
    min-width: 56px;
    text-align: right;
    font-size: 20px;
    font-weight: 600;
    line-height: 1;
    color: var(--color-text-1);
    font-variant-numeric: tabular-nums;
}

.tc-bigpct em {
    margin-left: 2px;
    font-size: 12px;
    font-style: normal;
    font-weight: 400;
    color: var(--color-text-3);
}

/* 字节 / 分层 / 阶段做成一排小片，扫读比一串文字快 */
.tc-meta {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    font-variant-numeric: tabular-nums;
}

.tc-chip {
    padding: 0 6px;
    border-radius: 2px;
    background: var(--color-fill-1);
    line-height: 20px;
}

.tc-chip.stage {
    color: rgb(var(--arcoblue-6));
}

/* 结论 / 失败归因：左侧竖线区分于普通说明 */
.tc-note {
    margin-top: 8px;
    padding: 6px 8px;
    border-left: 3px solid var(--color-border-2);
    border-radius: var(--border-radius-small);
    background: var(--color-fill-1);
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    line-height: 1.6;
    word-break: break-all;
    white-space: pre-wrap;
}

.tc-note.bad {
    border-left-color: rgb(var(--red-6));
}

/* 还在这跑但长时间没进度：橙色提醒，不是失败 */
.tc-note.warn {
    border-left-color: rgb(var(--orange-6));
    color: var(--color-text-1);
}

.tc-table {
    margin-top: 10px;
}

.tc-table :deep(.arco-table-th) {
    background: var(--color-fill-1);
    font-weight: 500;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

/* 层 ID 是一长串哈希：单行截断 + 悬停看全文，绝不折行把行高撑开 */
.tc-layer {
    display: block;
    font-size: 12px;
    color: var(--color-text-3);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.tc-rowprog {
    display: flex;
    align-items: center;
    gap: 8px;
}

.tc-rowprog .tc-bar {
    flex: 1;
    height: 3px;
    margin: 0;
}

.tc-rowpct {
    flex: none;
    width: 34px;
    text-align: right;
    font-size: 12px;
    color: var(--color-text-3);
    font-variant-numeric: tabular-nums;
}

.tc-done {
    color: rgb(var(--green-6));
}

/* 构建 / 导入的逐步日志：限高滚动 */
.tc-log {
    margin-top: var(--base-margin);
    flex: 1;
    min-height: 0;
    max-height: 300px;
    overflow-y: auto;
    padding: 6px 8px;
    border-radius: var(--border-radius-small);
    background: var(--color-fill-1);
    font-family: Consolas, monospace;
    font-size: var(--base-size-small);
    line-height: 1.6;
}

.tc-log-line {
    word-break: break-all;
}

/* 拉取详情里的提示历史：只占几行，不抢分层表格的高度 */
.tc-log.small {
    flex: none;
    margin-top: 8px;
    max-height: 84px;
}

.tc-placeholder {
    margin-top: var(--base-margin);
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.mono {
    font-family: Consolas, monospace;
}

.dim {
    color: var(--color-text-3);
}

/* footer：左边一句状态说明，右边主动作 */
.tc-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    width: 100%;
}

.tc-hint {
    flex: 1;
    min-width: 0;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    text-align: left;
}
</style>
