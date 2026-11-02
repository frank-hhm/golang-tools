<template>
    <a-modal :visible="visible" title="PDF 拆分" :width="720" :mask-closable="false" ok-text="拆分并保存"
        :ok-loading="busy" @cancel="close" @ok="run">
        <div class="ps">
            <file-picker :paths="picked" :accept="['pdf']" :multiple="false" :start-dir="startDir"
                @update:paths="onPath" />
            <div class="ps-row">
                <span class="ps-label">拆法</span>
                <a-radio-group v-model="mode" type="button" size="small">
                    <a-radio value="each">每页一个文件</a-radio>
                    <a-radio value="chunk">每 N 页一段</a-radio>
                    <a-radio value="extract">抽出指定页</a-radio>
                </a-radio-group>
                <a-input-number v-if="mode === 'chunk'" v-model="chunkSize" :min="1" :max="500" size="small"
                    class="ps-num" hide-button />
                <a-input v-if="mode === 'extract'" v-model="ranges" size="small" class="ps-ranges"
                    placeholder="页码，如 1-3,5,8-10" />
            </div>
            <div class="ps-row">
                <span class="ps-label">命名</span>
                <a-input v-model="prefix" size="small" class="ps-prefix" placeholder="输出文件名前缀，留空用原文件名" />
                <a-checkbox v-model="alsoDownload">同时下载到浏览器</a-checkbox>
            </div>
            <div class="ps-tip">
                <icon-exclamation-circle />
                <span>
                    产物默认写回原文件所在目录（同名直接覆盖）；页码范围支持逗号与区间，越界自动丢弃。
                    {{ pageCount ? `当前文档共 ${pageCount} 页。` : "选中文档后会自动读取页数。" }}
                </span>
            </div>
            <div v-if="hint" class="ps-hint">{{ hint }}</div>
        </div>
    </a-modal>
</template>

<script setup lang="ts">
/*
PDF 拆分：单份本机 PDF → 按「每页 / 每 N 页 / 指定页」切分成多份 → 逐份 FileWriteBinary 落盘（可选同时下载）。
只搬页面，不保留书签/大纲；加密文档尝试 ignoreEncryption 读取，读不动就如实报错。
*/
import { getCurrentInstance, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import FilePicker from "./FilePicker.vue";
import { baseName, stripExt, joinPath, parentDir, fetchLocalFile, writeBinary, downloadBlob, parseRanges } from "./editx";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const props = withDefaults(defineProps<{ visible: boolean; paths?: string[]; startDir?: string }>(),
    { paths: () => [], startDir: "" });
const emit = defineEmits<{ (e: "update:visible", v: boolean): void; (e: "done", paths: string[]): void }>();

const picked = ref<string[]>([]);
const mode = ref<"each" | "chunk" | "extract">("each");
const chunkSize = ref(1);
const ranges = ref("");
const prefix = ref("");
const alsoDownload = ref(false);
const busy = ref(false);
const hint = ref("");
const pageCount = ref(0);
const outDir = ref("");

function onPath(v: string[]) {
    picked.value = v;
    pageCount.value = 0;
    if (v[0]) {
        outDir.value = parentDir(v[0]);
        prefix.value = stripExt(baseName(v[0]));
        void readCount(v[0]);
    }
}

async function readCount(p: string) {
    try {
        const { PDFDocument } = await import("pdf-lib");
        const doc = await PDFDocument.load(await fetchLocalFile(p), { ignoreEncryption: true });
        pageCount.value = doc.getPageCount();
    } catch { /* 读不到就不显示页数，run 时再如实报错 */ }
}

watch(() => props.visible, (v) => { if (v) { hint.value = ""; onPath([...props.paths]); } });

function close() { emit("update:visible", false); }

async function run() {
    const src = picked.value[0];
    if (!src) { Message.warning("先选一个 PDF"); return; }
    busy.value = true;
    try {
        const { PDFDocument } = await import("pdf-lib");
        const doc = await PDFDocument.load(await fetchLocalFile(src), { ignoreEncryption: true });
        const total = doc.getPageCount();
        pageCount.value = total;

        // 组页：0 基页索引分组
        const groups: number[][] = [];
        if (mode.value === "each") {
            for (let i = 0; i < total; i++) groups.push([i]);
        } else if (mode.value === "chunk") {
            const n = Math.max(1, chunkSize.value);
            for (let i = 0; i < total; i += n) groups.push(Array.from({ length: Math.min(n, total - i) }, (_, k) => i + k));
        } else {
            const list = parseRanges(ranges.value, total);
            if (!list.length) { hint.value = "页码范围没解析出任何页，例：1-3,5"; return; }
            groups.push(list.map((p) => p - 1));
        }

        const base = prefix.value?.trim() || stripExt(baseName(src));
        const pad = String(groups.length).length || 1;
        const written: string[] = [];
        for (let gi = 0; gi < groups.length; gi++) {
            const out = await PDFDocument.create();
            const copied = await out.copyPages(doc, groups[gi]);
            copied.forEach((pg: any) => out.addPage(pg));
            const buf = (await out.save()).buffer as ArrayBuffer;
            const name = groups.length === 1 && mode.value === "extract"
                ? `${base}-选页.pdf`
                : `${base}-${String(gi + 1).padStart(Math.max(2, pad), "0")}.pdf`;
            const path = joinPath(outDir.value, name);
            await writeBinary(GoTools, path, buf);
            written.push(path);
            if (alsoDownload.value) downloadBlob(buf, name);
        }
        hint.value = `已输出 ${written.length} 个文件到 ${outDir.value}`;
        Message.success(`拆分完成：${written.length} 个文件`);
        emit("done", written);
        close();
    } catch (e: any) {
        Message.error("拆分失败：" + String(e?.message || e));
    } finally {
        busy.value = false;
    }
}
</script>

<style scoped>
.ps {
    display: flex;
    flex-direction: column;
    gap: 8px;
}
.ps-row {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
}
.ps-label {
    color: var(--color-text-3);
    font-size: var(--base-size-12);
}
.ps-num {
    width: 92px;
}
.ps-ranges {
    width: 220px;
}
.ps-prefix {
    width: 220px;
}
.ps-tip,
.ps-hint {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    font-size: var(--base-size-12);
    color: var(--color-text-3);
    line-height: 1.6;
}
.ps-hint {
    color: rgb(var(--primary-6));
}
</style>
