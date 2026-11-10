<template>
    <div class="img-pane">
        <div v-if="err" class="ip-state ip-err">{{ err }}</div>

        <!-- ============ 查看态 ============ -->
        <template v-else-if="mode === 'view'">
            <div class="ip-canvas" :class="{ actual }">
                <img :src="url" :alt="name" draggable="false" @load="onLoad" @error="onError" />
                <div v-if="loading" class="ip-mask"><a-spin /></div>
            </div>
            <div class="ip-tools">
                <a-tooltip :content="actual ? '适应窗口' : '原始尺寸'">
                    <a-button size="mini" @click="actual = !actual">
                        <template #icon><icon-fullscreen v-if="!actual" /><icon-launch v-else /></template>
                        {{ actual ? '实际大小' : '适应' }}
                    </a-button>
                </a-tooltip>
                <span v-if="natural" class="ip-dim">{{ natural }}</span>
                <a-button v-if="editable" size="mini" type="primary" @click="enterEdit">
                    <template #icon><icon-edit /></template>编辑
                </a-button>
            </div>
        </template>

        <!-- ============ 编辑态 ============ -->
        <div v-else class="ip-edit">
            <div class="ip-edit-main">
                <annotate-canvas ref="editorRef" :empty-text="emptyText" @change="onChange" />
            </div>
            <div class="ip-side">
                <div class="ip-block">
                    <div class="ip-h">信息</div>
                    <div class="ip-kv"><span>源文件</span><b>{{ fmtBytes(srcBytes) }}</b></div>
                    <div class="ip-kv"><span>编辑后</span><b>{{ curDim || '--' }}</b></div>
                    <div class="ip-kv"><span>输出</span><b>{{ outDim || '未生成' }}{{ outBytes ? ' · ' + fmtBytes(outBytes) : '' }}</b></div>
                    <div class="ip-note">超过 {{ fmtBytes(maxEdit) }} 的图片不在浏览器内编辑（内存所为）。</div>
                </div>

                <div class="ip-block">
                    <div class="ip-h">输出尺寸</div>
                    <a-radio-group v-model="sizing" type="button" size="mini" class="ip-seg">
                        <a-radio value="keep">原始</a-radio>
                        <a-radio value="percent">百分比</a-radio>
                        <a-radio value="abs">指定</a-radio>
                    </a-radio-group>
                    <div v-if="sizing === 'percent'" class="ip-row">
                        <a-slider v-model="percent" :min="10" :max="300" :step="5" class="ip-slider" />
                        <span class="ip-pct">{{ percent }}%</span>
                    </div>
                    <template v-else-if="sizing === 'abs'">
                        <div class="ip-row">
                            <span class="ip-lab">宽</span>
                            <a-input-number v-model="inW" :min="1" :max="20000" size="mini" class="ip-num"
                                hide-button @change="onWChange" />
                            <span class="ip-lab">高</span>
                            <a-input-number v-model="inH" :min="1" :max="20000" size="mini" class="ip-num"
                                hide-button @change="onHChange" />
                        </div>
                        <a-checkbox v-model="lockRatio" size="small">保持比例</a-checkbox>
                    </template>
                </div>

                <div class="ip-block">
                    <div class="ip-h">格式</div>
                    <a-radio-group v-model="fmt" size="mini" type="button" class="ip-seg">
                        <a-radio value="image/png">PNG</a-radio>
                        <a-radio value="image/jpeg">JPEG</a-radio>
                        <a-radio value="image/webp">WebP</a-radio>
                    </a-radio-group>
                    <div v-if="fmt !== 'image/png'" class="ip-row">
                        <a-slider v-model="quality" :min="30" :max="100" class="ip-slider" />
                        <span class="ip-pct">{{ quality }}</span>
                    </div>
                    <div v-else class="ip-note">PNG 无损、保留透明；JPEG/WebP 会把透明底铺成白色。</div>
                </div>

                <div class="ip-block">
                    <div class="ip-h">动作</div>
                    <div class="ip-acts">
                        <a-button size="mini" :loading="busy" @click="refreshOut">
                            <template #icon><icon-refresh /></template>生成预览
                        </a-button>
                        <a-button size="mini" type="primary" :disabled="!hasImg" @click="saveAs">
                            <template #icon><icon-save /></template>另存为…
                        </a-button>
                        <a-button v-if="canWrite" size="mini" status="warning" :disabled="!hasImg" @click="overwrite">
                            覆盖原图
                        </a-button>
                        <a-button v-if="nativeWindows" size="mini" :disabled="!hasImg" @click="copyIt">
                            <template #icon><icon-copy /></template>复制
                        </a-button>
                        <a-button size="mini" :disabled="!hasImg" @click="downloadIt">
                            <template #icon><icon-download /></template>下载
                        </a-button>
                        <a-button size="mini" :disabled="!hasImg" @click="openBase64">Base64</a-button>
                    </div>
                    <div v-if="previewUrl" class="ip-preview">
                        <img :src="previewUrl" alt="输出预览" />
                    </div>
                </div>

                <div class="ip-block">
                    <a-button size="mini" long @click="backToView">
                        <template #icon><icon-eye /></template>返回查看
                    </a-button>
                </div>
            </div>
        </div>

        <save-path-modal v-model:visible="saveVisible" :dir="outDir" :name="outName" title="编辑结果保存到…"
            @confirm="doSave" />

        <a-modal v-model:visible="b64Visible" title="图片 Base64" :width="680" :footer="false">
            <div class="ip-b64">
                <a-radio-group v-model="b64WithPrefix" size="small" type="button">
                    <a-radio :value="true">含 data 前缀</a-radio>
                    <a-radio :value="false">纯 base64 载荷</a-radio>
                </a-radio-group>
                <div class="ip-b64-meta">{{ fmtBytes(srcBytes) }} → 文本 {{ b64Len }} 字符</div>
                <a-textarea v-model="b64Text" :auto-size="{ minRows: 4, maxRows: 8 }" readonly class="ip-b64-text" />
                <div class="ip-b64-ops">
                    <a-button size="mini" @click="copyText">复制文本</a-button>
                    <a-button size="mini" @click="saveB64">存为 .txt</a-button>
                </div>
            </div>
        </a-modal>
    </div>
</template>

<script setup lang="ts">
/*
图片预览 + 编辑工作台。

- 查看态：<img> 直指 /fileview/media 流式端点（不经 base64，大图不噎绑定层）。
- 编辑态：内嵌全局组件 annotate-canvas（与截图页同一套标注引擎），右侧输出面板做
  尺寸/格式/质量调节与「生成预览 / 另存 / 覆盖 / 复制 / 下载 / Base64」。
  处理全在浏览器：源经同源端点读成 dataURL（同源→画布不被污染→可导像素），
  产物经跨平台 FileWriteBinary 落盘；「复制」是 Windows 剪贴板能力，按 caps 显隐。
- 护栏：源图超过 maxEdit（与后端 MaxBinInline 同口径）不进编辑态，避免整张图灌进内存。
*/
import { computed, getCurrentInstance, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import SavePathModal from "./edit/SavePathModal.vue";
import {
    MAX_EDIT_BYTES, fmtBytes, baseName, stripExt, parentDir, joinPath,
    blobToDataURL, canvasToBlob, resizeCanvas, isLossy, fmtExt, writeBinary, downloadBlob, dataURLPayload,
} from "./edit/editx";
import { useCaps } from "@/utils/caps";

const props = withDefaults(defineProps<{ url: string; name: string; path?: string; canWrite?: boolean }>(),
    { path: "", canWrite: false });

const emit = defineEmits<{ (e: "changed"): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const caps = useCaps();
const nativeWindows = computed(() => !!caps.nativeWindows);

const loading = ref(true);
const err = ref("");
const actual = ref(false);
const natural = ref("");

const mode = ref<"view" | "edit">("view");
const editorRef = ref<any>(null);
const hasImg = ref(false);
const srcBytes = ref(0);
const emptyText = ref("正在载入图片…");

const sizing = ref<"keep" | "percent" | "abs">("keep");
const percent = ref(100);
const inW = ref(0);
const inH = ref(0);
const lockRatio = ref(true);
const fmt = ref("image/png");
const quality = ref(90);
const busy = ref(false);
const outW = ref(0);
const outH = ref(0);
const outBytes = ref(0);
const previewUrl = ref("");

const saveVisible = ref(false);
const outDir = ref("");
const outName = ref("");

const b64Visible = ref(false);
const b64WithPrefix = ref(true);
const b64Text = ref("");
const b64Payload = ref("");

let pendingBlob: Blob | null = null;

const maxEdit = MAX_EDIT_BYTES;
const editable = computed(() => !!props.url && !err.value);
const curDim = computed(() => {
    // hasImg 参与运算：标注/旋转/裁剪会 emit change，借此驱动尺寸重新取值
    void hasImg.value;
    const s = editorRef.value?.naturalSize?.();
    return s && s.w ? `${s.w} × ${s.h}` : "";
});
const outDim = computed(() => (outW.value && outH.value ? `${outW.value} × ${outH.value}` : ""));
const b64Len = computed(() => b64Text.value.length.toLocaleString());

function reset() {
    loading.value = true;
    err.value = "";
    actual.value = false;
    natural.value = "";
    mode.value = "view";
    hasImg.value = false;
    srcBytes.value = 0;
    revokePreview();
    pendingBlob = null;
    outBytes.value = 0;
    outW.value = 0;
    outH.value = 0;
}

function onLoad(e: Event) {
    loading.value = false;
    const img = e.target as HTMLImageElement;
    if (img?.naturalWidth) natural.value = `${img.naturalWidth} × ${img.naturalHeight}`;
}

function onError() {
    loading.value = false;
    err.value = "图片加载失败";
}

watch(() => props.url, reset, { immediate: true });

/* ---------------- 进入编辑：把同源流式端点读成 dataURL ---------------- */
async function enterEdit() {
    mode.value = "edit";
    emptyText.value = "正在载入图片…";
    try {
        const resp = await fetch(props.url);
        if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
        const blob = await resp.blob();
        if (blob.size > maxEdit) {
            Message.warning(`图片 ${fmtBytes(blob.size)} 超过浏览器编辑上限 ${fmtBytes(maxEdit)}，请用系统程序或先压缩`);
            mode.value = "view";
            return;
        }
        srcBytes.value = blob.size;
        const dataUrl = await blobToDataURL(blob);
        emptyText.value = "暂无图片";
        editorRef.value?.load(dataUrl);
    } catch (e: any) {
        err.value = "载入失败：" + String(e?.message || e);
        mode.value = "view";
    }
}

function onChange(has: boolean) {
    hasImg.value = has;
    // 底图变了（旋转/裁剪）就把「指定宽高」的基准重置为新尺寸
    const s = editorRef.value?.naturalSize?.();
    if (s?.w) {
        if (sizing.value === "abs" && lockRatio.value) { inW.value = s.w; inH.value = s.h; }
        if (sizing.value === "keep") { outW.value = s.w; outH.value = s.h; }
    }
    pendingBlob = null;
    outBytes.value = 0;
    revokePreview();
}

function backToView() {
    revokePreview();
    mode.value = "view";
}

function revokePreview() {
    if (previewUrl.value) URL.revokeObjectURL(previewUrl.value);
    previewUrl.value = "";
}

function onWChange(v?: number) {
    const s = editorRef.value?.naturalSize?.();
    if (!lockRatio.value || !s?.w || !v) return;
    inH.value = Math.max(1, Math.round(v * s.h / s.w));
}
function onHChange(v?: number) {
    const s = editorRef.value?.naturalSize?.();
    if (!lockRatio.value || !s?.h || !v) return;
    inW.value = Math.max(1, Math.round(v * s.w / s.h));
}

/** 目标像素（相对当前底图） */
function targetSize(): { w: number; h: number } {
    const s = editorRef.value?.naturalSize?.() || { w: 0, h: 0 };
    if (!s.w || !s.h) return { w: 0, h: 0 };
    if (sizing.value === "percent") {
        const k = percent.value / 100;
        return { w: Math.max(1, Math.round(s.w * k)), h: Math.max(1, Math.round(s.h * k)) };
    }
    if (sizing.value === "abs" && inW.value && inH.value) return { w: inW.value, h: inH.value };
    return { w: s.w, h: s.h };
}

/** 合成 + 缩放 + 编码；结果缓存在 pendingBlob 供各动作复用 */
async function build(force = false): Promise<Blob | null> {
    if (pendingBlob && !force) return pendingBlob;
    const flat: HTMLCanvasElement | null = editorRef.value?.exportCanvas?.() ?? null;
    if (!flat) { Message.warning("还没有可编辑的图片"); return null; }
    const { w, h } = targetSize();
    if (!w || !h) return null;
    busy.value = true;
    try {
        const src = (flat.width !== w || flat.height !== h)
            ? resizeCanvas(flat, w, h, isLossy(fmt.value) ? "#FFFFFF" : "")
            : flat;
        const blob = await canvasToBlob(src, fmt.value, isLossy(fmt.value) ? quality.value / 100 : undefined);
        pendingBlob = blob;
        outW.value = w; outH.value = h; outBytes.value = blob.size;
        return blob;
    } catch (e: any) {
        Message.error("生成失败：" + String(e?.message || e));
        return null;
    } finally {
        busy.value = false;
    }
}

async function refreshOut() {
    const blob = await build(true);
    if (!blob) return;
    revokePreview();
    previewUrl.value = URL.createObjectURL(blob);
    Message.success(`输出 ${outW.value}×${outH.value}，${fmtBytes(blob.size)}`);
}

/* ---------------- 落盘 / 复制 / 下载 / Base64 ---------------- */
function defaultName(): string {
    return `${stripExt(baseName(props.name || "image"))}-编辑.${fmtExt(fmt.value)}`;
}

async function saveAs() {
    const blob = await build();
    if (!blob) return;
    outDir.value = props.path ? parentDir(props.path) : "";
    outName.value = defaultName();
    saveVisible.value = true;
}

async function doSave(path: string) {
    const blob = pendingBlob;
    if (!blob) { Message.warning("请先生成预览或重新点「另存为」"); return; }
    try {
        await writeBinary(GoTools, path, blob);
        Message.success("已保存：" + path);
        if (props.path && path === props.path) emit("changed");
    } catch (e: any) {
        Message.error("保存失败：" + String(e?.message || e));
    }
}

function overwrite() {
    if (!props.path) { Message.warning("当前文件不是本机可写路径"); return; }
    Modal.warning({
        title: "覆盖原图",
        content: `将用编辑结果覆盖 ${props.path}，原图不可恢复。确定继续？`,
        okText: "覆盖",
        cancelText: "取消",
        hideCancel: false,
        onOk: async () => {
            const blob = await build();
            if (!blob) return;
            try {
                await writeBinary(GoTools, props.path, blob);
                Message.success("已覆盖原图");
                emit("changed");
            } catch (e: any) {
                Message.error("覆盖失败：" + String(e?.message || e));
            }
        },
    });
}

async function copyIt() {
    const blob = await build();
    if (!blob) return;
    try {
        await GoTools.CopyToClipboard(await blobToDataURL(blob));
        Message.success("图片已复制到剪贴板");
    } catch (e: any) {
        Message.error("复制失败：" + String(e?.message || e));
    }
}

async function downloadIt() {
    const blob = await build();
    if (!blob) return;
    downloadBlob(blob, defaultName());
}

async function openBase64() {
    const blob = await build();
    if (!blob) return;
    try {
        const url = await blobToDataURL(blob);
        b64Payload.value = dataURLPayload(url);
        b64Text.value = b64WithPrefix.value ? url : b64Payload.value;
        b64Visible.value = true;
    } catch (e: any) {
        Message.error("取 Base64 失败：" + String(e?.message || e));
    }
}

watch(b64WithPrefix, (v) => { b64Text.value = v ? `data:${fmt.value};base64,${b64Payload.value}` : b64Payload.value; });

async function copyText() {
    try {
        await navigator.clipboard.writeText(b64Text.value);
        Message.success("已复制文本");
    } catch {
        Message.warning("当前环境不允许网页写剪贴板，请手动全选复制");
    }
}

function saveB64() {
    const dir = props.path ? parentDir(props.path) : "";
    const name = `${stripExt(baseName(props.name || "image"))}.${fmtExt(fmt.value)}.base64.txt`;
    const text = b64Text.value;
    if (dir) {
        writeBinary(GoTools, joinPath(dir, name), new Blob([text], { type: "text/plain" }))
            .then(() => Message.success("已保存：" + joinPath(dir, name)))
            .catch((e: any) => Message.error("保存失败：" + String(e?.message || e)));
        return;
    }
    downloadBlob(new Blob([text], { type: "text/plain" }), name);
}
</script>

<style scoped>
.img-pane {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}
.ip-canvas {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: auto;
    padding: var(--base-size-12);
    /* 内容面只叠 fill-1 淡膜、不铺实色：父面板 .vp-main 由全局主题规则接管，铺 bg-2 会把主题纹理截断。 */
    background: var(--color-fill-1);
}
.ip-mask {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    /* 加载/错误遮罩与画布同面。 */
    background: var(--color-fill-1);
}
.ip-canvas img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
}
.ip-canvas.actual {
    align-items: flex-start;
    justify-content: flex-start;
}
.ip-canvas.actual img {
    max-width: none;
    max-height: none;
}
.ip-state {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--color-text-3);
}
.ip-err {
    color: rgb(var(--danger-6));
}
.ip-tools {
    position: absolute;
    right: var(--base-size-12);
    bottom: var(--base-size-12);
    display: flex;
    align-items: center;
    gap: var(--base-size-8);
    padding: 4px 8px;
    border-radius: var(--base-radius-8);
    background: var(--color-bg-2);
    box-shadow: 0 2px 8px rgb(0 0 0 / 12%);
}
.ip-dim {
    font-size: var(--base-size-12);
    color: var(--color-text-3);
}

/* 编辑态：左画布右输出面板 */
.ip-edit {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: 8px;
    padding: 8px;
}
.ip-edit-main {
    flex: 1;
    min-width: 0;
    display: flex;
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-8);
    overflow: hidden;
}
.ip-side {
    width: 260px;
    flex: none;
    overflow: auto;
    display: flex;
    flex-direction: column;
    gap: 8px;
}
.ip-block {
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-8);
    padding: 8px;
    background: var(--color-bg-1);
    display: flex;
    flex-direction: column;
    gap: 6px;
}
.ip-h {
    font-size: var(--base-size-12);
    color: var(--color-text-3);
}
.ip-kv {
    display: flex;
    justify-content: space-between;
    font-size: var(--base-size-12);
    color: var(--color-text-2);
}
.ip-kv b {
    font-weight: 600;
    color: var(--color-text-1);
}
.ip-note {
    font-size: var(--base-size-12);
    color: var(--color-text-3);
    line-height: 1.6;
}
.ip-row {
    display: flex;
    align-items: center;
    gap: 6px;
}
.ip-lab {
    font-size: var(--base-size-12);
    color: var(--color-text-3);
}
.ip-seg {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
}
.ip-slider {
    flex: 1;
}
.ip-pct {
    width: 42px;
    text-align: right;
    font-size: var(--base-size-12);
    color: var(--color-text-2);
}
.ip-num {
    width: 84px;
}
.ip-acts {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
}
.ip-preview {
    border: 1px dashed var(--color-border-3);
    border-radius: 6px;
    padding: 4px;
    max-height: 180px;
    overflow: auto;
}
.ip-preview img {
    display: block;
    max-width: 100%;
}
.ip-b64 {
    display: flex;
    flex-direction: column;
    gap: 8px;
}
.ip-b64-meta {
    font-size: var(--base-size-12);
    color: var(--color-text-3);
}
.ip-b64-text :deep(textarea) {
    font-family: var(--code-font-family);
    font-size: var(--base-size-12);
    word-break: break-all;
}
.ip-b64-ops {
    display: flex;
    gap: 6px;
    justify-content: flex-end;
}
</style>
