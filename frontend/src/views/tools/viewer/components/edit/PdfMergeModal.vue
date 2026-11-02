<template>
    <a-modal :visible="visible" title="PDF 合并" :width="720" :mask-closable="false" ok-text="生成并保存"
        :ok-loading="busy" @cancel="close" @ok="run">
        <div class="pm">
            <file-picker v-model:paths="picked" :accept="['pdf']" :start-dir="startDir" />
            <div class="pm-tip">
                <icon-exclamation-circle />
                <span>
                    按「已选列表」的顺序拼接页面，加密过的 PDF 会尝试忽略口令读取；整份文档都在浏览器内存里处理，合计超过 {{ fmtBytes(maxTotal) }} 请改用系统工具。
                </span>
            </div>
            <div v-if="hint" class="pm-hint">{{ hint }}</div>
        </div>
        <save-path-modal v-model:visible="saveVisible" :dir="outDir" :name="outName" title="合并结果保存到…"
            @confirm="writeOut" />
    </a-modal>
</template>

<script setup lang="ts">
/*
PDF 合并：多选本机 PDF → pdf-lib copyPages 依序搬页 → save → FileWriteBinary 落盘。
只搬页面与资源，不保留书签/表单域等附加结构（pdf-lib 的 copyPages 语义），需要保真请用系统工具。
*/
import { getCurrentInstance, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import FilePicker from "./FilePicker.vue";
import SavePathModal from "./SavePathModal.vue";
import { fmtBytes, parentDir, baseName, stripExt, fetchLocalFile, writeBinary, MAX_EDIT_BYTES } from "./editx";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const props = withDefaults(defineProps<{ visible: boolean; paths?: string[]; startDir?: string }>(),
    { paths: () => [], startDir: "" });
const emit = defineEmits<{ (e: "update:visible", v: boolean): void; (e: "done", path: string): void }>();

const maxTotal = MAX_EDIT_BYTES * 4;

const picked = ref<string[]>([...props.paths]);
const busy = ref(false);
const hint = ref("");
const saveVisible = ref(false);
const outDir = ref("");
const outName = ref("");
let pending: ArrayBuffer | null = null;

watch(() => props.visible, (v) => { if (v) { picked.value = [...props.paths]; hint.value = ""; } });

function close() { emit("update:visible", false); }

async function run() {
    if (picked.value.length < 2) { Message.warning("至少选 2 个 PDF"); return; }
    busy.value = true;
    try {
        const { PDFDocument } = await import("pdf-lib");
        const out = await PDFDocument.create();
        let total = 0, pages = 0;
        for (const p of picked.value) {
            const buf = await fetchLocalFile(p);
            total += buf.byteLength;
            if (total > maxTotal) throw new Error(`合计 ${fmtBytes(total)} 超过浏览器处理上限 ${fmtBytes(maxTotal)}`);
            const src = await PDFDocument.load(buf, { ignoreEncryption: true });
            const copied = await out.copyPages(src, src.getPageIndices());
            copied.forEach((pg: any) => out.addPage(pg));
            pages += copied.length;
        }
        pending = (await out.save()).buffer as ArrayBuffer;
        outDir.value = parentDir(picked.value[0]);
        outName.value = `${stripExt(baseName(picked.value[0]))}-合并${picked.value.length}份.pdf`;
        hint.value = `已合并 ${pages} 页，产物 ${fmtBytes(pending.byteLength)}，选择保存位置`;
        saveVisible.value = true;
    } catch (e: any) {
        Message.error("合并失败：" + String(e?.message || e));
    } finally {
        busy.value = false;
    }
}

async function writeOut(path: string) {
    if (!pending) return;
    try {
        await writeBinary(GoTools, path, pending);
        Message.success("已保存：" + path);
        emit("done", path);
        close();
    } catch (e: any) {
        Message.error("保存失败：" + String(e?.message || e));
    } finally {
        pending = null;
    }
}
</script>

<style scoped>
.pm {
    display: flex;
    flex-direction: column;
    gap: 8px;
}
.pm-tip,
.pm-hint {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    font-size: var(--base-size-12);
    color: var(--color-text-3);
    line-height: 1.6;
}
.pm-hint {
    color: rgb(var(--primary-6));
}
</style>
