<template>
    <div class="cup">
        <!-- 拖拽/选择区 -->
        <div class="cup-drop" :class="{ on: dragOn }" @click="pick" @dragover.prevent="dragOn = true"
            @dragenter.prevent="dragOn = true" @dragleave.prevent="dragOn = false" @drop.prevent="onDrop">
            <input ref="fileEl" type="file" :accept="accept" :multiple="multiple" style="display: none"
                @change="onPick" />
            <div class="cup-drop-t1">{{ hint || '把文件拖到这里，或点击选择' }}</div>
            <div class="cup-drop-t2">大文件自动切片、可暂停续传（并发 {{ concurrency }}）</div>
        </div>

        <!-- 文件队列 -->
        <div v-if="list.length" class="cup-list">
            <div v-for="t in list" :key="t.uid" class="cup-row">
                <common-task-progress :name="t.file.name" :status="t.status" :percent="t.pct"
                    :speed="t.speed ? fmtRate(t.speed) : ''" :eta="etaOf(t)" :meta="metaOf(t)" variant="bare"
                    :divided="false" @cancel="cancel(t)" @resume="resume(t)" @retry="retry(t)" @open="open(t)">
                    <template #actions>
                        <a-button v-if="t.status === 'running'" size="mini" @click="pause(t)">暂停</a-button>
                        <a-button v-else-if="t.status === 'paused'" size="mini" type="primary" @click="resume(t)">继续</a-button>
                        <a-button v-else-if="t.status === 'error' || t.status === 'canceled'" size="mini" type="primary" @click="retry(t)">重试</a-button>
                        <a-button v-if="t.status === 'running' || t.status === 'paused'" size="mini" @click="cancel(t)">取消</a-button>
                        <a-button v-if="t.status === 'done'" size="mini" @click="remove(t)">移除</a-button>
                    </template>
                </common-task-progress>
            </div>
            <div class="cup-foot">
                <span class="cup-sum">{{ summary }}</span>
                <a-button size="mini" :disabled="!doneCount" @click="clearDone">清除已完成</a-button>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
/**
 * 公用上传面板（计划-局域网互传上传任务与切片队列 §6）。
 *
 * 拖拽区 + 文件队列 + 每行 common-task-progress；传输经 props.transport 注入，
 * UI 与传输解耦——任何「推到后端」的场景换一份 transport 即可复用。
 * 阈值以下走 transport.once（一次性上传，若提供），以上走 init/part/complete 切片，
 * 队列并发、暂停/续传、速度/ETA 都在本组件内管理（发送侧驱动，后端只接收分片）。
 *
 * transport 契约（返回 Promise，失败即 reject）：
 *   init(file)            → { sid, chunkSize, have }  建/续会话，回已收分片下标
 *   part(file, sid, idx, start, end) → void            上传第 idx 片（组件按 chunkSize 算好 [start,end)，transport 内部 file.slice）
 *   complete(file, sid)   → void                       分片齐后落定
 *   abort(sid?)           → void                       取消会话（无 sid 可空实现）
 *   once?(file)           → void                       小于阈值的整包上传
 */
import { computed, onUnmounted, ref } from "vue";
import type { ProgressStatus } from "@/utils/taskProgress";
import type { UploadInit, UploadTransport } from "./upload-panel";

interface UpTask {
    uid: string; file: File; size: number; sent: number; pct: number; speed: number;
    status: ProgressStatus; sid?: string; chunkSize: number; total: number;
    paused: boolean; canceled: boolean; resumed: boolean; err?: string;
    t0: number; b0: number;
}

const props = withDefaults(defineProps<{
    transport: UploadTransport;
    /** input accept，如 "image/*" 或 ".zip,.pdf" */
    accept?: string;
    multiple?: boolean;
    /** 切片阈值（字节），小于走 once（若有），大于走切片 */
    thresholdBytes?: number;
    /** 队列并发数 */
    concurrency?: number;
    /** 拖拽区主提示文案 */
    hint?: string;
    /** 添加文件后是否自动开始（默认 true） */
    autoStart?: boolean;
}>(), {
    multiple: true,
    thresholdBytes: 8 * 1024 * 1024,
    concurrency: 2,
    autoStart: true,
});

const emit = defineEmits<{ (e: "done", name: string): void; (e: "added", n: number): void }>();

const fileEl = ref<HTMLInputElement | null>(null);
const dragOn = ref(false);
const list = ref<UpTask[]>([]);
let active = 0;
let seq = 0;

function pick() { fileEl.value?.click(); }
function onPick(e: Event) { add((e.target as HTMLInputElement).files); (fileEl.value as HTMLInputElement).value = ""; }
function onDrop(e: DragEvent) { dragOn.value = false; add(e.dataTransfer?.files); }

function add(fl?: FileList | null) {
    if (!fl || !fl.length) return;
    const arr = props.multiple ? Array.from(fl) : [fl[0]];
    for (const f of arr) {
        list.value.push({
            uid: `u${++seq}`, file: f, size: f.size, sent: 0, pct: 0, speed: 0,
            status: "idle", chunkSize: 0, total: 0, paused: false, canceled: false, resumed: false, t0: 0, b0: 0,
        });
    }
    emit("added", arr.length);
    if (props.autoStart) pump();
}

const doneCount = computed(() => list.value.filter((t) => t.status === "done" || t.status === "error" || t.status === "canceled").length);
const summary = computed(() => {
    const total = list.value.length;
    const done = list.value.filter((t) => t.status === "done").length;
    return `共 ${total} 个 · 完成 ${done} · 进行中 ${list.value.filter((t) => t.status === "running" || t.status === "paused").length}`;
});

/* ---------------- 队列调度 ---------------- */
function pump() {
    while (active < props.concurrency) {
        const t = list.value.find((x) => x.status === "idle");
        if (!t) break;
        active++;
        run(t).finally(() => { active--; pump(); });
    }
}

async function run(t: UpTask) {
    if (t.canceled) return;
    t.status = "running";
    try {
        if (t.size < props.thresholdBytes && props.transport.once) {
            await props.transport.once(t.file);
            t.sent = t.size; t.pct = 100;
        } else {
            await runChunked(t);
        }
        if (t.canceled) { t.status = "canceled"; return; }
        t.status = "done";
        emit("done", t.file.name);
    } catch (e: any) {
        if (t.canceled) { t.status = "canceled"; return; }
        t.err = String(e?.message ?? e ?? "上传失败");
        t.status = "error";
    }
}

function chunkLen(t: UpTask, idx: number): number {
    const off = idx * t.chunkSize;
    if (off >= t.size) return 0;
    return Math.min(t.chunkSize, t.size - off);
}

async function runChunked(t: UpTask) {
    const init = await props.transport.init(t.file);
    if (t.canceled) throw new Error("canceled");
    t.sid = init.sid; t.chunkSize = init.chunkSize; t.total = Math.ceil(t.size / t.chunkSize) || 1;
    t.sent = 0;
    const have = new Set<number>(init.have || []);
    have.forEach((i) => { t.sent += chunkLen(t, i); });
    t.pct = pctOf(t);
    t.status = "running";
    // 逐片：跳过已收；每片前检查暂停/取消
    for (let i = 0; i < t.total; i++) {
        if (t.canceled) throw new Error("canceled");
        if (have.has(i)) continue;
        while (t.paused && !t.canceled && !t.resumed) { await sleep(250); }
        if (t.canceled) throw new Error("canceled");
        if (t.paused && !t.resumed) { /* 继续会置 resumed */ }
        const start = i * t.chunkSize;
        const end = Math.min(start + t.chunkSize, t.size);
        await props.transport.part(t.file, t.sid, i, start, end);
        t.sent += chunkLen(t, i);
        t.pct = pctOf(t);
        tick(t);
    }
    await props.transport.complete(t.file, t.sid);
    t.sent = t.size; t.pct = 100; t.speed = 0;
}

function pctOf(t: UpTask): number {
    if (!t.size) return 0;
    const p = Math.round((t.sent * 100) / t.size);
    return t.sent >= t.size ? 100 : Math.min(99, p);
}
function tick(t: UpTask) {
    const now = (performance && performance.now) ? performance.now() : Date.now();
    if (!t.t0) { t.t0 = now; t.b0 = 0; }
    const dt = (now - t.t0) / 1000;
    if (dt >= 0.5) { t.speed = (t.sent - t.b0) / dt; t.t0 = now; t.b0 = t.sent; }
}
function sleep(ms: number) { return new Promise((r) => setTimeout(r, ms)); }

/* ---------------- 行内操作 ---------------- */
function pause(t: UpTask) { if (t.status === "running") { t.paused = true; t.status = "paused"; } }
function resume(t: UpTask) { if (t.status === "paused") { t.resumed = true; t.paused = false; t.status = "running"; } }
function cancel(t: UpTask) {
    t.canceled = true; t.paused = false; t.resumed = false;
    props.transport.abort(t.sid).catch(() => { /* 无 sid 或已收尾，忽略 */ });
    t.status = "canceled";
}
function retry(t: UpTask) {
    t.canceled = false; t.paused = false; t.resumed = false; t.err = "";
    t.sent = 0; t.pct = 0; t.speed = 0; t.t0 = 0; t.b0 = 0;
    t.status = "idle";
    pump();
}
function open(t: UpTask) { emit("done", t.file.name); }
function remove(t: UpTask) { list.value = list.value.filter((x) => x.uid !== t.uid); }
function clearDone() { list.value = list.value.filter((x) => x.status !== "done" && x.status !== "canceled" && x.status !== "error"); }

/* ---------------- 文案 ---------------- */
function fmtBytes(n: number): string {
    if (!n || n < 0) return "0 B";
    const u = ["B", "KB", "MB", "GB", "TB"]; let i = 0, v = n;
    while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
    return `${v.toFixed(i === 0 ? 0 : 1)} ${u[i]}`;
}
function fmtRate(bps: number): string { return `${fmtBytes(bps)}/s`; }
function etaOf(t: UpTask): string {
    if (!t.speed || t.speed <= 0 || t.status !== "running") return "";
    const left = t.size - t.sent;
    if (left <= 0) return "";
    const s = Math.round(left / t.speed);
    if (s < 60) return `约 ${s} 秒`;
    const m = Math.floor(s / 60);
    if (m < 60) return `约 ${m} 分钟`;
    return `约 ${Math.floor(m / 60)} 小时`;
}
function metaOf(t: UpTask): string {
    if (t.status === "error") return t.err || "失败";
    if (t.status === "canceled") return "已取消";
    if (t.status === "paused") return `已暂停 ${fmtBytes(t.sent)} / ${fmtBytes(t.size)}`;
    if (t.status === "done") return `已接收 ${fmtBytes(t.size)}`;
    if (t.size >= props.thresholdBytes && t.sid) return `切片续传 ${fmtBytes(t.sent)} / ${fmtBytes(t.size)}`;
    return `${fmtBytes(t.sent)} / ${fmtBytes(t.size)}`;
}

// 暴露给父组件：外部可经 ref 调用 add 注入文件（如从其它页拖入）
defineExpose({ add, clearDone });

onUnmounted(() => { /* 运行中的分片循环会在 canceled/组件销毁后自然停止 */ });
</script>

<style scoped>
.cup {
    display: flex;
    flex-direction: column;
    gap: 8px;
    width: 100%;
}

.cup-drop {
    border: 2px dashed var(--color-border-3);
    border-radius: var(--base-radius-medium);
    padding: 22px 16px;
    text-align: center;
    cursor: pointer;
    background: var(--color-fill-1);
    transition: all .18s ease;
}

.cup-drop:hover,
.cup-drop.on {
    border-color: rgb(var(--primary-6));
    background: var(--color-primary-light-1);
}

.cup-drop-t1 {
    font-size: var(--base-size-default);
    color: var(--color-text-1);
    font-weight: 500;
}

.cup-drop-t2 {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    margin-top: 4px;
}

.cup-list {
    display: flex;
    flex-direction: column;
    gap: 2px;
}

.cup-row {
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-small);
    padding: 6px 10px;
    background: var(--color-bg-2);
}

.cup-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-top: 4px;
}

.cup-sum {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}
</style>
