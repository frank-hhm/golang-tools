<template>
    <a-modal :visible="visible" title="图片拼接" :width="720" :mask-closable="false" ok-text="生成并保存"
        :ok-loading="busy" @cancel="close" @ok="run">
        <div class="st">
            <file-picker v-model:paths="picked" :accept="imgExts" :start-dir="startDir" />
            <div class="st-row">
                <span class="st-label">排布</span>
                <a-radio-group v-model="layout" type="button" size="small">
                    <a-radio value="v">纵向</a-radio>
                    <a-radio value="h">横向</a-radio>
                    <a-radio value="grid">宫格</a-radio>
                </a-radio-group>
                <span class="st-label">尺寸基准</span>
                <a-radio-group v-model="sizing" type="button" size="small">
                    <a-radio value="natural">原尺寸</a-radio>
                    <a-radio value="uniform">统一长边</a-radio>
                </a-radio-group>
                <a-input-number v-if="sizing === 'uniform'" v-model="longEdge" :min="64" :max="8192" size="small"
                    class="st-num" hide-button />
            </div>
            <div class="st-row">
                <span class="st-label">间距</span>
                <a-input-number v-model="gap" :min="0" :max="200" size="small" class="st-num" hide-button />
                <span class="st-label">背景</span>
                <a-input v-model="bg" size="small" class="st-bg" placeholder="#FFFFFF" />
                <span class="st-label">格式</span>
                <a-select v-model="fmt" size="small" class="st-fmt">
                    <a-option value="image/png">PNG（无损）</a-option>
                    <a-option value="image/jpeg">JPEG</a-option>
                    <a-option value="image/webp">WebP</a-option>
                </a-select>
                <template v-if="fmt !== 'image/png'">
                    <span class="st-label">质量</span>
                    <a-slider v-model="quality" :min="30" :max="100" class="st-q" />
                </template>
            </div>
            <div class="st-tip">
                <icon-exclamation-circle />
                <span>
                    最多 {{ maxCount }} 张，画布上限约 {{ maxPixels / 1e6 }} 百万像素（浏览器内存所为）；
                    只重编码 PNG/JPEG/WebP，源图由浏览器解码，bmp/gif 等也能拼。
                </span>
            </div>
            <div v-if="hint" class="st-hint">{{ hint }}</div>
        </div>
        <save-path-modal v-model:visible="saveVisible" :dir="outDir" :name="outName" title="拼接结果保存到…"
            @confirm="writeOut" />
    </a-modal>
</template>

<script setup lang="ts">
/*
图片拼接：多选本机图片 → 纵/横/宫格画布合成 → 目标格式编码 → FileWriteBinary 落盘。
张数与像素上限是硬护栏（浏览器 canvas 会 OOM / 触发空白画布），超出直接拒绝并说明原因。
*/
import { getCurrentInstance, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import FilePicker from "./FilePicker.vue";
import SavePathModal from "./SavePathModal.vue";
import { fmtBytes, loadImgFromMedia, parentDir, baseName, stripExt, canvasToBlob, isLossy, fmtExt, writeBinary } from "./editx";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const props = withDefaults(defineProps<{
    visible: boolean;
    paths?: string[];
    startDir?: string;
}>(), { paths: () => [], startDir: "" });

const emit = defineEmits<{ (e: "update:visible", v: boolean): void; (e: "done", path: string): void }>();

const imgExts = ["png", "jpg", "jpeg", "gif", "bmp", "webp", "ico", "svg"];
const maxCount = 20;
const maxPixels = 25_000_000;

const picked = ref<string[]>([...props.paths]);
const layout = ref<"v" | "h" | "grid">("v");
const sizing = ref<"natural" | "uniform">("uniform");
const longEdge = ref(1080);
const gap = ref(8);
const bg = ref("#FFFFFF");
const fmt = ref("image/png");
const quality = ref(90);
const busy = ref(false);
const hint = ref("");

const saveVisible = ref(false);
const outDir = ref("");
const outName = ref("");
let pending: Blob | null = null;

watch(() => props.visible, (v) => {
    if (v) {
        picked.value = [...props.paths];
        hint.value = "";
    }
});

function close() { emit("update:visible", false); }

async function run() {
    if (picked.value.length < 2) { Message.warning("至少选 2 张图"); return; }
    if (picked.value.length > maxCount) { Message.warning(`一次最多 ${maxCount} 张`); return; }
    busy.value = true;
    hint.value = "";
    try {
        const imgs: HTMLImageElement[] = [];
        for (const p of picked.value) imgs.push(await loadImgFromMedia(p));
        // 统一尺寸基准：长边夹到 longEdge（只缩不放，避免糊）
        const sizes = imgs.map((im) => {
            const w = im.naturalWidth, h = im.naturalHeight;
            if (!w || !h) throw new Error(`读不到图片尺寸：${baseName(im.src)}`);
            if (sizing.value !== "uniform") return { w, h };
            const s = Math.min(1, longEdge.value / Math.max(w, h));
            return { w: Math.max(1, Math.round(w * s)), h: Math.max(1, Math.round(h * s)) };
        });
        const g = gap.value;
        let W = 0, H = 0;
        const boxes: { x: number; y: number; w: number; h: number }[] = [];
        if (layout.value === "v") {
            W = Math.max(...sizes.map((s) => s.w));
            H = sizes.reduce((a, s) => a + s.h, 0) + g * (sizes.length - 1);
            let y = 0;
            sizes.forEach((s) => { boxes.push({ x: Math.round((W - s.w) / 2), y, w: s.w, h: s.h }); y += s.h + g; });
        } else if (layout.value === "h") {
            H = Math.max(...sizes.map((s) => s.h));
            W = sizes.reduce((a, s) => a + s.w, 0) + g * (sizes.length - 1);
            let x = 0;
            sizes.forEach((s) => { boxes.push({ x, y: Math.round((H - s.h) / 2), w: s.w, h: s.h }); x += s.w + g; });
        } else {
            const cols = Math.ceil(Math.sqrt(sizes.length));
            const rows = Math.ceil(sizes.length / cols);
            const cw = Math.max(...sizes.map((s) => s.w));
            const ch = Math.max(...sizes.map((s) => s.h));
            W = cols * cw + (cols - 1) * g;
            H = rows * ch + (rows - 1) * g;
            sizes.forEach((s, i) => {
                const cx = i % cols, cy = Math.floor(i / cols);
                boxes.push({
                    x: cx * (cw + g) + Math.round((cw - s.w) / 2),
                    y: cy * (ch + g) + Math.round((ch - s.h) / 2),
                    w: s.w, h: s.h,
                });
            });
        }
        if (W * H > maxPixels) {
            hint.value = `合成后 ${W}×${H}（约 ${(W * H / 1e6).toFixed(1)} 百万像素）超出上限，请减小统一长边或减少张数`;
            return;
        }
        const c = document.createElement("canvas");
        c.width = W; c.height = H;
        const ctx = c.getContext("2d");
        if (!ctx) throw new Error("画布不可用");
        ctx.fillStyle = bg.value || "#FFFFFF";
        ctx.fillRect(0, 0, W, H);
        imgs.forEach((im, i) => {
            const b = boxes[i];
            ctx.imageSmoothingQuality = "high";
            ctx.drawImage(im, b.x, b.y, b.w, b.h);
        });
        const q = isLossy(fmt.value) ? quality.value / 100 : undefined;
        pending = await canvasToBlob(c, fmt.value, q);
        const first = picked.value[0];
        outDir.value = parentDir(first);
        outName.value = `${stripExt(baseName(first))}-拼接-${picked.value.length}张.${fmtExt(fmt.value)}`;
        hint.value = `合成 ${W}×${H}，产物 ${fmtBytes(pending.size)}，选择保存位置`;
        saveVisible.value = true;
    } catch (e: any) {
        Message.error("拼接失败：" + String(e?.message || e));
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
.st {
    display: flex;
    flex-direction: column;
    gap: 8px;
}
.st-row {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 6px;
}
.st-label {
    color: var(--color-text-3);
    font-size: var(--base-size-12);
}
.st-num {
    width: 92px;
}
.st-bg {
    width: 100px;
}
.st-fmt {
    width: 140px;
}
.st-q {
    width: 120px;
}
.st-tip,
.st-hint {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    font-size: var(--base-size-12);
    color: var(--color-text-3);
    line-height: 1.6;
}
.st-hint {
    color: rgb(var(--primary-6));
}
</style>
