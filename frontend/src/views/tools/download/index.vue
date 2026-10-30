<template>
    <div class="dl-page">
        <!-- ==================== 顶部统计卡 ==================== -->
        <div class="dl-stats">
            <common-stat-card label="下行速度" tone="blue" icon="net" :value="fmtSpeed(agg.speed)" unit=""
                :detail="agg.running ? `${agg.running} 个任务在跑` : '暂无下载'" />
            <common-stat-card label="进行中" tone="cyan" icon="download" :value="agg.running" unit="个" detail="含排队等待额度" />
            <common-stat-card label="队列中" tone="orange" icon="queue" :value="agg.queued" unit="个"
                detail="等待开始 / 手动触发" />
            <common-stat-card label="已完成" tone="green" icon="done" :value="agg.done" unit="个"
                :detail="`共 ${agg.total} 个任务`" />
        </div>

        <!-- ==================== 工具条 ==================== -->
        <a-card class="dl-bar card-head-tool" size="small" :body-style="{ padding: '8px 12px' }">
            <div class="dl-tools">
                <a-button size="small" @click="addVisible = true">
                    <template #icon><icon-plus /></template>新建任务
                </a-button>
                <a-divider direction="vertical" />
                <a-button size="small" :disabled="!canStartAll" @click="startAll">
                    <template #icon><icon-play-arrow /></template>全部开始
                </a-button>
                <a-button size="small" :disabled="!agg.running" @click="pauseAll">
                    <template #icon><icon-pause /></template>全部暂停
                </a-button>
                <a-divider direction="vertical" />
                <span class="dl-limit">
                    <icon-list class="dl-limit-ico" />全局限速
                    <a-select v-model="globalLimit" size="small" style="width: 128px" @change="applyGlobalLimit">
                        <a-option v-for="p in SPEED_PRESETS" :key="p.bps" :value="p.bps">{{ p.label }}</a-option>
                    </a-select>
                </span>
                <div class="dl-tools-spacer" />
                <a-tooltip content="刷新任务列表">
                    <a-button size="small" :loading="loading" @click="load">
                        <template #icon><icon-refresh /></template>
                    </a-button>
                </a-tooltip>
            </div>
        </a-card>

        <!-- ==================== 主体：左分类 / 右列表 + 详情 ==================== -->
        <div class="dl-body">
            <div class="dl-side">
                <div class="dl-side-title">分类</div>
                <div class="dl-cat">
                    <div v-for="c in CATEGORIES" :key="c" class="dl-cat-item" :class="{ on: cat === c }"
                        @click="cat = c">
                        <span class="dl-cat-ico">
                            <common-file-icon :is-dir="CATEGORY_ICON[c]?.dir" :ext="CATEGORY_ICON[c]?.ext || ''"
                                :size="18" />
                        </span>
                        <span class="dl-cat-name">{{ c }}</span>
                        <span class="dl-cat-num" :class="{ zero: !catCount(c) }">{{ catCount(c) }}</span>
                    </div>
                </div>
                <div class="dl-side-title">状态</div>
                <div class="dl-cat">
                    <div class="dl-cat-item" :class="{ on: group === '' }" @click="group = ''">
                        <span class="dl-cat-ico"><span class="dl-sdot sd-all"></span></span>
                        <span class="dl-cat-name">全部状态</span>
                    </div>
                    <div v-for="g in STATE_GROUPS" :key="g.key" class="dl-cat-item" :class="{ on: group === g.key }"
                        @click="group = g.key">
                        <span class="dl-cat-ico"><span class="dl-sdot" :class="'sd-' + g.key"></span></span>
                        <span class="dl-cat-name">{{ g.label }}</span>
                    </div>
                </div>
            </div>

            <div class="dl-main">
                <div class="dl-list">
                    <div v-if="loading && !tasks.length" class="dl-state"><a-spin /></div>
                    <div v-else-if="!filtered.length" class="dl-state">
                        <a-empty :description="tasks.length ? '该筛选下没有任务' : '还没有下载任务，点上方「新建任务」开始'" />
                    </div>

                    <common-item-card v-for="t in filtered" :key="t.id" class="dl-item" :active="t.id === selectedId"
                        :error="t.status === 'error'" pad="8px 10px 10px 12px" @click="selectedId = t.id">
                        <template #status>
                            <span class="dl-dot" :class="'dot-' + t.status"></span>
                        </template>
                        <template #title>
                            <span class="dl-name" :title="t.name">{{ t.name }}</span>
                        </template>
                        <template #extra>
                            <span class="dl-size">{{ t.size > 0 ? fmtBytes(t.size) : '大小未知' }}</span>
                            <a-tag size="small" :color="statusTagColor(t.status)">{{ statusLabel(t.status) }}</a-tag>
                        </template>

                        <common-task-progress variant="bare" :divided="false" :show-text="false" :show-header="false"
                            size="small" :status="normalizeStatus(t.status)" :percent="pctOf(t)"
                            :speed="t.status === 'running' && t.speed > 0 ? fmtSpeed(t.speed) : ''"
                            :indeterminate="t.status === 'running' && pctOf(t) === 0" @click="selectedId = t.id"
                            @resume="resume(t)" @retry="retry(t)" @cancel="stop(t)" @open="openFile(t)">
                            <template #meta>
                                <span class="dl-meta-text" :title="t.err || t.note">{{ t.err || progressText(t)
                                    }}</span>
                            </template>
                            <template #left>
                                <span class="dl-conn">×{{ t.connections || 1 }} 连接</span>
                                <span v-if="etaOf(t)" class="dl-eta">{{ etaOf(t) }}</span>
                            </template>
                            <template #actions>
                                <a-button v-if="t.status === 'running'" size="mini" @click.stop="pause(t)">暂停</a-button>
                                <a-button v-else-if="t.status === 'paused'" size="mini" type="primary"
                                    @click.stop="resume(t)">继续</a-button>
                                <a-button v-else-if="t.status === 'pending'" size="mini" type="primary"
                                    @click.stop="start(t)">开始</a-button>
                                <a-tooltip v-else-if="t.status === 'queued'" content="已在队列中，前面的任务让出额度后自动开始"
                                    position="tl">
                                    <a-button size="mini" disabled>开始</a-button>
                                </a-tooltip>
                                <a-button v-else-if="t.status === 'error' || t.status === 'canceled'" size="mini"
                                    @click.stop="retry(t)">重试</a-button>
                                <a-tooltip v-if="t.status === 'done'" content="打开所在目录">
                                    <a-button size="mini" @click.stop="openLocation(t)">
                                        <template #icon><icon-folder /></template>
                                    </a-button>
                                </a-tooltip>
                                <!-- 停止只在能被停止的状态出现：done/error/canceled 已是终态，再摆「停止」与左侧标签、右侧「重试」互相矛盾 -->
                                <a-button v-if="['running', 'queued', 'pending', 'paused'].includes(t.status)"
                                    size="mini" status="danger" @click.stop="stop(t)">停止</a-button>
                                <a-popconfirm content="删除任务及其已下载文件？" type="warning" ok-text="删除" cancel-text="取消"
                                    @ok="del(t)">
                                    <a-button size="mini" @click.stop>
                                        <template #icon><icon-delete /></template>
                                    </a-button>
                                </a-popconfirm>
                            </template>
                        </common-task-progress>
                    </common-item-card>
                </div>

                <!-- ==================== 选中任务详情（分块 / 连接 / 限速 / 源） ==================== -->
                <div v-if="selected" class="dl-detail">
                    <div class="dl-det-head">
                        <span class="dl-det-title">任务详情</span>
                        <span class="dl-det-path" :title="selected.savePath">{{ selected.savePath }}</span>
                    </div>

                    <div class="dl-det-grid">
                        <div class="dl-field">
                            <span class="dl-k">连接</span>
                            <a-input-number v-model="editConn" :min="1" :max="16" size="small" hide-button
                                @change="applyConn" />
                            <span class="dl-hint">下次下载生效</span>
                        </div>
                        <div class="dl-field">
                            <span class="dl-k">本任务限速</span>
                            <a-select v-model="editLimit" size="small" style="width: 120px" @change="applyLimit">
                                <a-option v-for="p in SPEED_PRESETS" :key="p.bps" :value="p.bps">{{ p.label
                                    }}</a-option>
                            </a-select>
                        </div>
                        <div v-if="selected.proxy" class="dl-field">
                            <span class="dl-k">代理</span>
                            <span class="dl-v" :title="selected.proxy">{{ selected.proxy }}</span>
                        </div>
                        <div v-if="selected.mirrors && selected.mirrors.length" class="dl-field">
                            <span class="dl-k">下载源</span>
                            <a-select :model-value="selected.source || selected.url" size="small" style="width: 220px"
                                @change="(v: any) => selectSource(v)">
                                <a-option v-for="(m, i) in mirrorOptions" :key="i" :value="m">{{ shortUrl(m)
                                    }}</a-option>
                            </a-select>
                        </div>
                    </div>

                    <div class="dl-parts">
                        <div class="dl-parts-title">分块进度（{{ selected.parts && selected.parts.length ?
                            selected.parts.length : 1
                            }} 块）</div>
                        <template v-if="selected.parts && selected.parts.length">
                            <div v-for="p in selected.parts" :key="p.index" class="dl-part">
                                <span class="dl-part-no">#{{ p.index + 1 }}</span>
                                <div class="dl-part-bar">
                                    <a-progress :percent="partPct(p.start, p.end, p.done) / 100" size="small"
                                        :show-text="false" :status="p.done > 0 ? 'normal' : 'normal'" />
                                </div>
                                <span class="dl-part-range">{{ partRange(p.start, p.end) }}</span>
                                <span class="dl-part-speed">{{ fmtSpeed(p.speed) }}</span>
                            </div>
                        </template>
                        <div v-else class="dl-part-empty">
                            {{ selected.status === 'running' ? '正在建立连接…' : '尚无分块进度' }}
                        </div>
                    </div>
                </div>
            </div>
        </div>

        <add-task-modal v-model:visible="addVisible" @created="onCreated" />
    </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import type { Task, Stats } from "bindings/Tools/tools/downloader/index";
import AddTaskModal from "./components/AddTaskModal.vue";
import { normalizeStatus } from "@/utils/taskProgress";
import {
    CATEGORIES, CATEGORY_ICON, STATE_GROUPS, SPEED_PRESETS,
    fmtBytes, fmtSpeed, pctOf, progressText, etaOf, statusLabel, statusTagColor,
    partRange, partPct, matchFilter,
} from "./helpers";

/*
下载管理工具页（/tools/download）

引擎在 tools/downloader（分块 / 续传 / 限速 / 重试 / 队列 / 分类），本页只做展示与操作转发：
- 列表每条任务复用全局组件 common-task-progress 渲染进度（与任务抽屉同一套进度行），
  卡片外壳用 common-item-card、顶部指标用 common-stat-card，遵循组件规范；
- 实时进度靠订阅 "download:progress"（负载 downloader.Task）增量更新，不轮询；
- 下方详情区展开选中任务的分块进度、连接数 / 限速 / 下载源手动调整。

Linux 面板语义一致：下载到服务器本机 SaveDir，无原生目录框时改手填路径。
*/

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const tasks = ref<Task[]>([]);
const stats = ref<Stats>({ speed: 0, running: 0, queued: 0, done: 0, total: 0, globalLimit: 0 });
const loading = ref(false);
const addVisible = ref(false);
const cat = ref<string>("全部");
const group = ref<string>("");
const selectedId = ref<string>("");
const globalLimit = ref<number>(0);

// 详情面板的本地编辑态：随选中任务切换复位
const editConn = ref(4);
const editLimit = ref<number>(0);

/** 顶部聚合：计数与合计下行速度都从当前任务列表即时算，省一次 RPC */
const agg = computed(() => {
    let running = 0, queued = 0, done = 0, speed = 0;
    for (const t of tasks.value) {
        if (t.status === "running") { running++; speed += t.speed || 0; }
        else if (t.status === "queued" || t.status === "pending") queued++;
        else if (t.status === "done") done++;
    }
    return { running, queued, done, total: tasks.value.length, speed };
});
const canStartAll = computed(() => tasks.value.some((t) => ["pending", "queued", "paused"].includes(t.status)));

const filtered = computed(() => tasks.value.filter((t) => matchFilter(t, cat.value, group.value)));
const selected = computed(() => tasks.value.find((t) => t.id === selectedId.value) || null);

const mirrorOptions = computed(() => {
    const s = selected.value;
    if (!s) return [];
    const set = new Set<string>([s.url, ...(s.mirrors || [])].filter(Boolean));
    return [...set];
});

watch(selected, (s) => {
    editConn.value = s?.connections || 4;
    editLimit.value = 0;
}, { immediate: true });

function catCount(c: string): number {
    if (c === "全部") return tasks.value.length;
    return tasks.value.filter((t) => t.category === c).length;
}

function shortUrl(u: string): string {
    try { const x = new URL(u); return x.host + x.pathname; } catch { return u; }
}

const errText = (e: any) => String(e?.message || e || "操作失败");

async function load() {
    loading.value = true;
    try {
        const list = await GoTools?.DownloadList();
        tasks.value = Array.isArray(list) ? list : [];
        const st = await GoTools?.DownloadGlobalStats();
        if (st) { stats.value = st; globalLimit.value = st.globalLimit || 0; }
        if (selectedId.value && !tasks.value.some((t) => t.id === selectedId.value)) selectedId.value = "";
    } catch (e: any) {
        Message.error(errText(e));
    } finally {
        loading.value = false;
    }
}

/** 增量并入一条任务：有则原地替换（保持加入顺序），无则追加 */
function upsert(t?: Task) {
    if (!t || !t.id) return;
    const i = tasks.value.findIndex((x) => x.id === t.id);
    if (i >= 0) tasks.value.splice(i, 1, t);
    else tasks.value.push(t);
}

function onCreated(t?: Task) {
    if (t?.id) { upsert(t); selectedId.value = t.id; }
    else load();
}

// ---------- 单任务动作 ----------
async function call(fn: () => Promise<any>, after?: () => void) {
    try { await fn(); after?.(); } catch (e: any) { Message.error(errText(e)); }
}
const start = (t: Task) => call(() => GoTools.DownloadStart(t.id));
const pause = (t: Task) => call(() => GoTools.DownloadPause(t.id));
const resume = (t: Task) => call(() => GoTools.DownloadResume(t.id));
const retry = (t: Task) => call(() => GoTools.DownloadRetry(t.id));
const stop = (t: Task) => call(() => GoTools.DownloadStop(t.id));
const del = (t: Task) => call(() => GoTools.DownloadDelete(t.id), () => {
    if (selectedId.value === t.id) selectedId.value = "";
    load();
});

function openLocation(t: Task) {
    if (!t.savePath) return;
    call(() => GoTools.DiskOpenInExplorer(t.savePath));
}
function openFile(t: Task) {
    if (!t.savePath) return;
    call(() => GoTools.FileOpenSystem(t.savePath));
}

// ---------- 批量 ----------
async function startAll() {
    const ts = tasks.value.filter((t) => ["pending", "queued", "paused"].includes(t.status));
    await Promise.allSettled(ts.map((t) => GoTools.DownloadStart(t.id)));
    load();
}
async function pauseAll() {
    const ts = tasks.value.filter((t) => t.status === "running");
    await Promise.allSettled(ts.map((t) => GoTools.DownloadPause(t.id)));
    load();
}

// ---------- 详情控制 ----------
function applyConn(v: any) {
    const s = selected.value; if (!s) return;
    call(() => GoTools.DownloadSetConnections(s.id, Number(v) || 4));
}
function applyLimit(v: any) {
    const s = selected.value; if (!s) return;
    call(() => GoTools.DownloadSetSpeed(s.id, Number(v) || 0));
}
function selectSource(url: string) {
    const s = selected.value; if (!s || !url) return;
    call(() => GoTools.DownloadSelectSource(s.id, url), () => { s.source = url; });
}
function applyGlobalLimit(v: any) {
    const bps = Number(v) || 0;
    call(() => GoTools.DownloadSetGlobalLimit(bps), () => { stats.value.globalLimit = bps; });
}

// ---------- 事件订阅 ----------
let offProgress: any = null;
onMounted(() => {
    try {
        offProgress = Events.On("download:progress", (ev: any) => {
            upsert((ev?.data ?? ev) as Task);
        });
    } catch { /* 非 Wails 环境忽略 */ }
    load();
});
onUnmounted(() => { if (offProgress) offProgress(); });
</script>

<style scoped>
.dl-page {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    gap: 10px;
}

/* 统计卡 */
.dl-stats {
    flex: none;
    display: grid;
    grid-template-columns: repeat(4, 1fr);
    gap: 10px;
}

.dl-stats> :deep(.stat-card) {
    height: auto;
}

/* 工具条 */
.dl-bar {
    flex: none;
}

.dl-tools {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
}

.dl-tools-spacer {
    flex: 1;
}

.dl-limit {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

.dl-limit-ico {
    color: var(--color-text-3);
}

/* 主体 */
.dl-body {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: 10px;
}

.dl-side {
    width: 180px;
    flex: none;
    min-height: 0;
    overflow-x: hidden;
    overflow-y: auto;
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-default);
    background: var(--color-bg-2);
}

.dl-side-title {
    position: relative;
    display: block;
    font-size: var(--base-size-small);
    font-weight: 600;
    letter-spacing: .5px;
    color: var(--color-text-2);
    padding: var(--base-padding);
}

/* 分区标题下划线：通栏（铺满面板宽度）的主色细线 */
.dl-side-title::after {
    content: "";
    position: absolute;
    left: 0;
    right: 0;
    bottom: 2px;
    height: 1px;
    border-radius: 2px;
    background: var(--color-border-2);
    opacity: .8;
}

.dl-cat {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: calc(var(--base-padding) / 2) var(--base-padding);
}

.dl-cat-item {
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 8px 6px 10px;
    border-radius: 6px;
    cursor: pointer;
    font-size: var(--base-size-default);
    color: var(--color-text-2);
    transition: background .15s ease, color .15s ease;
}

.dl-cat-item:hover {
    background: var(--color-fill-2);
}

/* 选中态：左侧短竖条 + 主色文字 + 选中背景 */
.dl-cat-item.on {
    background: var(--color-fill-2);
    color:var(--color-text-1);
}

.dl-cat-item.on::before {
    content: "";
    position: absolute;
    left: 0;
    top: 50%;
    transform: translateY(-50%);
    width: 3px;
    height: 15px;
    border-radius: 0 3px 3px 0;
    background: var(--color-text-1);
}

.dl-cat-ico {
    flex: none;
    width: 20px;
    height: 20px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
}

.dl-sdot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    display: inline-block;
}

.sd-all {
    background: var(--color-text-4);
}

.sd-active {
    background: rgb(var(--primary-6));
}

.sd-queue {
    background: rgb(var(--orange-6));
}

.sd-done {
    background: rgb(var(--success-6));
}

.sd-failed {
    background: rgb(var(--danger-6));
}

.dl-cat-name {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.dl-cat-num {
    flex: none;
    margin-left: auto;
    min-width: 20px;
    height: 18px;
    line-height: 18px;
    padding: 0 6px;
    text-align: center;
    font-size: 11px;
    color: var(--color-text-3);
    background: var(--color-fill-2);
    border-radius: 9px;
    font-variant-numeric: tabular-nums;
    transition: background .15s ease, color .15s ease;
}

.dl-cat-num.zero {
    opacity: .5;
}

.dl-cat-item.on .dl-cat-num {
    color: var(--color-text-1);
    opacity: 1;
}

/* 右栏 */
.dl-main {
    flex: 1;
    min-width: 0;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 10px;
}

.dl-list {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding-right: 2px;
}

.dl-state {
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
}

.dl-item {
    margin-bottom: 10px;
    cursor: pointer;
}

.dl-dot {
    flex: none;
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: var(--color-text-4);
}

.dot-running {
    background: rgb(var(--primary-6));
    animation: dl-pulse 1.4s ease-in-out infinite;
}

.dot-pending,
.dot-queued {
    background: var(--color-text-4);
}

.dot-paused {
    background: rgb(var(--orange-6));
}

.dot-done {
    background: rgb(var(--success-6));
}

.dot-error {
    background: rgb(var(--danger-6));
    box-shadow: 0 0 0 3px rgba(var(--danger-6), 0.16);
}

.dot-canceled {
    background: var(--color-text-4);
}

@keyframes dl-pulse {

    0%,
    100% {
        box-shadow: 0 0 0 0 rgba(var(--primary-6), 0.35);
    }

    50% {
        box-shadow: 0 0 0 4px rgba(var(--primary-6), 0);
    }
}

.dl-name {
    flex: 1;
    min-width: 0;
    font-size: var(--base-size-default);
    color: var(--color-text-1);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

.dl-size {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
}

.dl-meta-text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.dl-conn {
    flex: none;
    font-variant-numeric: tabular-nums;
}

.dl-eta {
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

/* 详情 */
.dl-detail {
    flex: none;
    max-height: 46%;
    overflow: auto;
    padding: 10px 12px;
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-default);
    background: var(--color-bg-2);
}

.dl-det-head {
    display: flex;
    align-items: baseline;
    gap: 10px;
    margin-bottom: 8px;
}

.dl-det-title {
    flex: none;
    font-weight: 600;
    color: var(--color-text-1);
}

.dl-det-path {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.dl-det-grid {
    display: flex;
    flex-wrap: wrap;
    gap: 12px 22px;
    margin-bottom: 10px;
}

.dl-field {
    display: inline-flex;
    align-items: center;
    gap: 8px;
    /* 字段不被 flex 压缩：标签/提示词永远单行，不给竖排换行留机会 */
    flex: none;
    white-space: nowrap;
}

.dl-k {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    white-space: nowrap;
}

.dl-v {
    font-size: var(--base-size-small);
    color: var(--color-text-1);
    max-width: 240px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.dl-hint {
    font-size: 11px;
    color: var(--color-text-4);
    white-space: nowrap;
}

.dl-parts {
    border-top: 1px dashed var(--color-border-2);
    padding-top: 8px;
}

.dl-parts-title {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    margin-bottom: 6px;
}

.dl-part {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: 2px 0;
}

.dl-part-no {
    flex: none;
    width: 32px;
    font-size: 11px;
    color: var(--color-text-3);
    font-variant-numeric: tabular-nums;
}

.dl-part-bar {
    flex: 1;
    min-width: 0;
}

.dl-part-range {
    flex: none;
    width: 168px;
    text-align: right;
    font-size: 11px;
    color: var(--color-text-3);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
}

.dl-part-speed {
    flex: none;
    width: 78px;
    text-align: right;
    font-size: 11px;
    color: var(--color-text-2);
    font-variant-numeric: tabular-nums;
}

.dl-part-empty {
    font-size: var(--base-size-small);
    color: var(--color-text-4);
}
</style>
