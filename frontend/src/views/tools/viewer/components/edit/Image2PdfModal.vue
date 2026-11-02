<template>
    <a-modal :visible="visible" title="图片转 PDF" :width="720" :mask-closable="false" ok-text="生成并保存"
        :ok-loading="busy" @cancel="close" @ok="run">
        <div class="i2p">
            <file-picker v-model:paths="picked" :accept="imgExts" :start-dir="startDir" />
            <div class="i2p-row">
                <span class="i2p-label">页面</span>
                <a-radio-group v-model="pageSize" type="button" size="small">
                    <a-radio value="auto">按图片尺寸</a-radio>
                    <a-radio value="a4">A4</a-radio>
                    <a-radio value="letter">Letter</a-radio>
                </a-radio-group>
                <span class="i2p-label">页边距</span>
                <a-input-number v-model="margin" :min="0" :max="120" :disabled="pageSize === 'auto'" size="small"
                    class="i2p-num" hide-button />
                <span class="i2p-label">编码</span>
                <a-select v-model="mode" size="small" class="i2p-mode">
                    <a-option value="jpeg">JPEG（体积小）</a-option>
                    <a-option value="keep">保留 PNG 无损</a-option>
                </a-select>
                <template v-if="mode === 'jpeg'">
                    <span class="i2p-label">质量</span>
                    <a-slider v-model="quality" :min="30" :max="100" class="i2p-q" />
                </template>
            </div>
            <div class="i2p-tip">
                <icon-exclamation-circle />
                <span>
                    一图片一 PDF 页（顺序就是上面已选列表）；webp/gif/bmp/svg 先由浏览器解码再编码为 JPEG/PNG 嵌入。
                    最多 {{ maxCount }} 张。
                </span>
            </div>
            <div v-if="hint" class="i2p-hint">{{ hint }}</div>
        </div>
        <save-path-modal v-model:visible="saveVisible" :dir="outDir" :name="outName" title="PDF 保存到…"
            @confirm="writeOut" />
    </a-modal>
</template>

<script setup lang="ts">
/*
图片转 PDF：全部在浏览器用 pdf-lib（dynamic import，独立 chunk）成页，产物经 FileWriteBinary 落盘。
页面尺寸两种口径：按图片尺寸（px→pt 固定 96dpi 换算）或固定纸型（等比居中放置，可留页边距）。
*/
import { getCurrentInstance, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import FilePicker from "./FilePicker.vue";
import SavePathModal from "./SavePathModal.vue";
import { fmtBytes, loadImgFromMedia, parentDir, baseName, stripExt, encodeImage, writeBinary } from "./editx";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const props = withDefaults(defineProps<{ visible: boolean; paths?: string[]; startDir?: string }>(),
    { paths: () => [], startDir: "" });
const emit = defineEmits<{ (e: "update:visible", v: boolean): void; (e: "done", path: string): void }>();

const imgExts = ["png", "jpg", "jpeg", "gif", "bmp", "webp", "svg"];
const maxCount = 60;
/** 96dpi 的像素 → PDF 点（1pt = 1/72inch） */
const PX2PT = 72 / 96;
const PAPERS: Record<string, [number, number]> = { a4: [595.28, 841.89], letter: [612, 792] };

const picked = ref<string[]>([...props.paths]);
const pageSize = ref<"auto" | "a4" | "letter">("auto");
const margin = ref(24);
const mode = ref<"jpeg" | "keep">("jpeg");
const quality = ref(85);
const busy = ref(false);
const hint = ref("");

const saveVisible = ref(false);
const outDir = ref("");
const outName = ref("");
let pending: ArrayBuffer | null = null;

watch(() => props.visible, (v) => { if (v) { picked.value = [...props.paths]; hint.value = ""; } });

function close() { emit("update:visible", false); }

async function run() {
    if (!picked.value.length) { Message.warning("至少选 1 张图"); return; }
    if (picked.value.length > maxCount) { Message.warning(`一次最多 ${maxCount} 张`); return; }
    busy.value = true;
    try {
        const { PDFDocument } = await import("pdf-lib");
        const doc = await PDFDocument.create();
        for (const p of picked.value) {
            const im = await loadImgFromMedia(p);
            const iw = im.naturalWidth, ih = im.naturalHeight;
            if (!iw || !ih) throw new Error(`读不到尺寸：${baseName(p)}`);
            const wantPng = mode.value === "keep" && /\.png$/i.test(p);
            const blob = await encodeImage(im, wantPng ? "image/png" : "image/jpeg", wantPng ? undefined : quality.value / 100);
            const buf = await blob.arrayBuffer();
            const imgObj = wantPng ? await doc.embedPng(buf) : await doc.embedJpg(buf);

            let pw: number, ph: number, drawW: number, drawH: number, x: number, y: number;
            if (pageSize.value === "auto") {
                pw = iw * PX2PT; ph = ih * PX2PT;
                drawW = pw; drawH = ph; x = 0; y = 0;
            } else {
                const [aw, ah] = PAPERS[pageSize.value];
                const land = iw > ih;
                pw = land ? ah : aw; ph = land ? aw : ah;
                const m = margin.value;
                const maxW = Math.max(1, pw - 2 * m), maxH = Math.max(1, ph - 2 * m);
                const s = Math.min(maxW / imgObj.width, maxH / imgObj.height);
                drawW = imgObj.width * s; drawH = imgObj.height * s;
                x = (pw - drawW) / 2; y = (ph - drawH) / 2;
            }
            const page = doc.addPage([pw, ph]);
            page.drawImage(imgObj, { x, y, width: drawW, height: drawH });
        }
        pending = (await doc.save()).buffer as ArrayBuffer;
        outDir.value = parentDir(picked.value[0]);
        outName.value = picked.value.length === 1
            ? `${stripExt(baseName(picked.value[0]))}.pdf`
            : `${stripExt(baseName(picked.value[0]))}-等${picked.value.length}页.pdf`;
        hint.value = `已生成 ${picked.value.length} 页，产物 ${fmtBytes(pending.byteLength)}，选择保存位置`;
        saveVisible.value = true;
    } catch (e: any) {
        Message.error("转 PDF 失败：" + String(e?.message || e));
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
.i2p {
    display: flex;
    flex-direction: column;
    gap: 8px;
}
.i2p-row {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
}
.i2p-label {
    color: var(--color-text-3);
    font-size: var(--base-size-12);
}
.i2p-num {
    width: 92px;
}
.i2p-mode {
    width: 160px;
}
.i2p-q {
    width: 120px;
}
.i2p-tip,
.i2p-hint {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    font-size: var(--base-size-12);
    color: var(--color-text-3);
    line-height: 1.6;
}
.i2p-hint {
    color: rgb(var(--primary-6));
}
</style>
