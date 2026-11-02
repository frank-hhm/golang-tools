<template>
    <div class="ap-pane">
        <div v-if="loading" class="ap-state"><a-spin /><span>读取归档…</span></div>
        <div v-else-if="err" class="ap-state ap-err">
            <div>{{ err }}</div>
            <div class="ap-hint">若为远端或不支持的格式，请用「系统程序打开」或先取回本机。</div>
        </div>
        <template v-else>
            <div class="ap-bar">
                <span class="ap-count">{{ entries.length }} 个条目</span>
                <a-space :size="8">
                    <a-button size="mini" @click="reload"><icon-refresh />刷新</a-button>
                    <a-button size="mini" :loading="extracting" @click="extractAll">整包解压到同级目录</a-button>
                </a-space>
            </div>
            <div class="ap-list">
                <div v-if="!entries.length" class="ap-state">空归档</div>
                <div v-for="e in entries" :key="e.name" class="ap-row" :class="{ dir: e.isDir }"
                    @click="onPick(e)">
                    <common-file-icon :is-dir="e.isDir" :ext="extOf(e.dispPath)" :size="18" />
                    <span class="ap-name" :title="e.dispPath">{{ e.dispPath }}</span>
                    <span class="ap-size">{{ e.isDir ? '' : fmtSize(e.size) }}</span>
                </div>
            </div>
        </template>
    </div>
</template>

<script setup lang="ts">
/*
压缩包在线浏览（zip/tar.gz/tgz）：调后端 ArchiveListEntries 列条目，
点单个文件 → ArchiveExtractEntry 提取到临时目录 → emit('open', 临时路径) 交父级按类型再分发预览。
整包解压复用既有 ArchiveExtract（就地解到同级同名文件夹）。
远端/不支持格式：ListEntries 会失败，这里给出明确提示而非静默。
*/
import { getCurrentInstance, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";

const props = defineProps<{ path: string; name: string }>();
const emit = defineEmits<{ (e: "open", path: string): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

interface Entry { name: string; dispPath: string; isDir: boolean; size: number; modified: number }

const loading = ref(true);
const err = ref("");
const entries = ref<Entry[]>([]);
const extracting = ref(false);

function extOf(p: string): string {
    const i = p.lastIndexOf(".");
    return i >= 0 ? p.slice(i + 1).toLowerCase() : "";
}
function fmtSize(n: number): string {
    if (!n) return "";
    const u = ["B", "KB", "MB", "GB", "TB"];
    let i = 0, v = n;
    while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
    return (i === 0 ? v : v.toFixed(1)) + " " + u[i];
}

async function reload() {
    loading.value = true;
    err.value = "";
    entries.value = [];
    try {
        const res = await GoTools?.ArchiveListEntries(props.path);
        entries.value = (res as Entry[]) ?? [];
    } catch (e: any) {
        err.value = "无法读取归档：" + String(e?.message || e);
    } finally {
        loading.value = false;
    }
}

async function onPick(e: Entry) {
    if (e.isDir) return;
    try {
        const temp = await GoTools?.ArchiveExtractEntry(props.path, e.name);
        if (!temp) { Message.warning("提取失败：未返回路径"); return; }
        emit("open", String(temp));
    } catch (err2: any) {
        Message.error("提取失败：" + String(err2?.message || err2));
    }
}

async function extractAll() {
    extracting.value = true;
    try {
        await GoTools?.ArchiveExtract({ op: "extract", paths: [props.path], format: "", level: 0, outDir: "", name: "", dest: "" });
        Message.success("已开始解压，进度见任务中心");
    } catch (e: any) {
        Message.error("解压失败：" + String(e?.message || e));
    } finally {
        extracting.value = false;
    }
}

watch(() => props.path, reload, { immediate: true });
</script>

<style scoped>
.ap-pane {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}
.ap-bar {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 10px;
    border-bottom: 1px solid var(--color-border-2);
}
.ap-count {
    font-size: var(--base-size-12);
    color: var(--color-text-3);
}
.ap-list {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 4px;
}
.ap-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 8px;
    border-radius: 6px;
    cursor: pointer;
    min-width: 0;
}
.ap-row:hover {
    background: var(--color-fill-2);
}
.ap-row.dir {
    cursor: default;
    color: var(--color-text-2);
}
.ap-name {
    flex: 1;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-size: var(--base-size-14);
    color: var(--color-text-1);
}
.ap-size {
    flex: none;
    font-size: var(--base-size-12);
    color: var(--color-text-3);
}
.ap-state {
    flex: 1;
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 8px;
    color: var(--color-text-3);
}
.ap-err {
    color: rgb(var(--danger-6));
    padding: 0 16px;
    text-align: center;
}
.ap-hint {
    color: var(--color-text-3);
    font-size: var(--base-size-12);
}
</style>
