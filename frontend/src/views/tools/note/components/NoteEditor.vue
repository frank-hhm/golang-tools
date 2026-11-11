<template>
    <div class="ne-root">
        <!-- 顶栏：文件名 + dirty 红点 + 只读 / 放弃 / 保存，版式对齐文件查看器 CodePane -->
        <div class="ne-bar">
            <div class="ne-file">
                <common-file-icon :is-dir="false" :ext="ext" :size="18" />
                <span class="ne-name" :title="path">{{ baseName }}</span>
                <span v-if="dirty" class="ne-dot" title="有未保存的修改"></span>
                <a-tag size="small" :color="isMd ? 'arcoblue' : 'purple'">{{ isMd ? 'Markdown' : '富文本' }}</a-tag>
            </div>
            <a-space :size="8">
                <span v-if="!isMd" class="ne-hint">所见即所得；复杂排版建议用富文本格式，Markdown 往返对颜色/表格样式尽力保留。</span>
                <a-checkbox v-model="readOnly" size="small">只读</a-checkbox>
                <a-button size="small" :disabled="!dirty" @click="revert">放弃</a-button>
                <a-button size="small" type="primary" :loading="saving" :disabled="!dirty || readOnly" @click="save">保存</a-button>
            </a-space>
        </div>

        <div v-if="loadError" class="ne-state ne-err">{{ loadError }}</div>
        <div v-else-if="loading" class="ne-state"><a-spin /><span>读取笔记…</span></div>
        <div v-else class="ne-editor-wrap">
            <!-- 隐藏的原生工具栏：仅用于实例化默认菜单，供 NoteToolbar 复用菜单能力（exec/isActive/isDisabled/...） -->
            <Toolbar class="ne-toolbar-src" :editor="editorRef" :default-config="toolbarConfig" mode="default" />
            <!-- 自绘 a-* 工具栏：驱动编辑器全部默认菜单 -->
            <note-toolbar class="ne-toolbar" :editor="editorRef" :disabled="readOnly" />
            <Editor class="ne-editor" v-model="html" :default-config="editorConfig" mode="default"
                @on-created="onCreated" @on-change="onChange" />
        </div>
    </div>
</template>

<script setup lang="ts">
/**
 * 笔记编辑区：wangeditor 所见即所得，按文件后缀分派存储格式。
 * - .md：载入用 marked 把 Markdown 转 HTML 灌入；保存用 turndown 把编辑器 HTML 转回 Markdown。
 * - .html：直接读写 HTML，无损。
 * 读文本 / 写文本都走跨平台绑定 FileReadText / FileWriteText，Linux 面板浏览器同样可用。
 * marked / turndown 静态引入——本页是懒加载路由，构建期已按路由分包，不拖累其它入口。
 */
import { computed, getCurrentInstance, onBeforeUnmount, ref, shallowRef, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Editor, Toolbar } from "@wangeditor/editor-for-vue";
import type { IDomEditor, IEditorConfig, IToolbarConfig } from "@wangeditor/editor";
import NoteToolbar from "./NoteToolbar.vue";
import "@wangeditor/editor/dist/css/style.css";
import { marked } from "marked";
import TurndownService from "turndown";

const props = defineProps<{ path: string; name: string }>();
const emit = defineEmits<{ (e: "saved"): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const loading = ref(true);
const loadError = ref("");
const saving = ref(false);
const readOnly = ref(false);
const dirty = ref(false);

const html = ref("");
// 编辑器实例必须用 shallowRef，深层响应会把 wangeditor 内部大量对象包成 Proxy 直接崩溃
const editorRef = shallowRef<IDomEditor>();

const ext = computed(() => (props.path.split(".").pop() || "").toLowerCase());
const isMd = computed(() => ext.value === "md");
const baseName = computed(() => props.name || props.path.replace(/^.*[\\/]/, ""));

const toolbarConfig: Partial<IToolbarConfig> = {};
const editorConfig: Partial<IEditorConfig> = {
    placeholder: "开始输入…",
    // 图片不走服务端上传：小于 5MB 直接转 base64 内联，落进笔记实体本身
    MENU_CONF: {
        uploadImage: { base64LimitSize: 5 * 1024 * 1024 },
    },
};

const turndown = new TurndownService({ headingStyle: "atx", codeBlockStyle: "fenced" });

function onCreated(editor: IDomEditor) {
    editorRef.value = editor;
    applyReadonly();
}

function onChange() {
    // 载入期间的程序化赋值不算用户改动，靠 loading 门控过滤
    if (!loading.value) dirty.value = true;
}

function applyReadonly() {
    const ed = editorRef.value;
    if (!ed) return;
    try {
        if (readOnly.value) ed.disable(); else ed.enable();
    } catch { /* 编辑器尚未就绪，忽略 */ }
}

watch(readOnly, applyReadonly);

/** 把磁盘文本转成编辑器 HTML：md 走 marked，html 原样。 */
async function toEditorHtml(text: string): Promise<string> {
    if (!isMd.value) return text;
    // marked 默认同步返回字符串；断言收窄类型
    return await Promise.resolve(marked.parse(text || "") as string);
}

/** 把编辑器 HTML 转成磁盘文本：md 走 turndown，html 原样。 */
function toDiskText(editorHtml: string): string {
    if (!isMd.value) return editorHtml;
    return turndown.turndown(editorHtml || "");
}

async function load() {
    loadError.value = "";
    dirty.value = false;
    readOnly.value = false;
    if (!props.path) { loading.value = true; return; }
    loading.value = true;
    try {
        const r: any = await GoTools?.FileReadText(props.path);
        const text = String(r?.content ?? "");
        // loading 期间禁止 onChange 置 dirty（程序化赋值）
        html.value = await toEditorHtml(text);
    } catch (e: any) {
        loadError.value = String(e?.message ?? e ?? "读取失败");
    } finally {
        loading.value = false;
        applyReadonly();
    }
}

async function save() {
    if (!props.path || !editorRef.value) return;
    saving.value = true;
    try {
        const out = toDiskText(editorRef.value.getHtml());
        await GoTools?.FileWriteText(props.path, out);
        dirty.value = false;
        Message.success("已保存");
        emit("saved");
    } catch (e: any) {
        Message.error(`保存失败：${String(e?.message ?? e ?? e)}`);
    } finally {
        saving.value = false;
    }
}

async function revert() {
    await load();
}

watch(() => props.path, () => void load(), { immediate: true });

onBeforeUnmount(() => {
    const ed = editorRef.value;
    if (ed) ed.destroy();
});
</script>

<style scoped>
.ne-root {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-width: 0;
    height: 100%;
    min-height: 0;
}

.ne-bar {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: calc(var(--base-padding) * 0.5) var(--base-padding);
    border-bottom: 1px solid var(--color-border-1);
    flex-wrap: wrap;
}

.ne-file {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
}

.ne-name {
    max-width: 260px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--color-text-1);
    font-weight: 600;
}

.ne-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: rgb(var(--danger-6));
    flex-shrink: 0;
}

.ne-hint {
    font-size: 12px;
    color: var(--color-text-3);
}

.ne-state {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 8px;
    color: var(--color-text-3);
}

.ne-err {
    color: rgb(var(--danger-6));
}

.ne-editor-wrap {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    /* 不再嵌一层带边框的卡片：np-main 已是唯一容器，工具栏 + 编辑面直接铺满，
       只有编辑面内部滚动 */
    overflow: hidden;

    /* wangeditor 工具栏/下拉/面板/模态/分隔线/悬停选中态全部吃这套 --w-e-* 变量：
       把它们映射到主题令牌，编辑器控件就随明暗、描边、密度联动，观感对齐站内 a-* 组件 */
    --w-e-toolbar-bg-color: var(--color-fill-1);
    --w-e-toolbar-color: var(--color-text-2);
    --w-e-toolbar-active-color: var(--color-text-1);
    --w-e-toolbar-active-bg-color: var(--color-fill-3);
    --w-e-toolbar-disabled-color: rgb(var(--gray-5));
    --w-e-toolbar-border-color: var(--color-border-2);
    --w-e-textarea-bg-color: var(--color-bg-2);
    --w-e-textarea-color: var(--color-text-1);
    --w-e-textarea-border-color: var(--color-border-2);
    --w-e-textarea-slight-border-color: var(--color-border-1);
    --w-e-textarea-slight-color: var(--color-text-3);
    --w-e-textarea-slight-bg-color: var(--color-fill-2);
    --w-e-textarea-selected-border-color: rgb(var(--primary-3));
    --w-e-textarea-handler-bg-color: rgb(var(--primary-6));
    --w-e-modal-button-bg-color: var(--color-fill-2);
    --w-e-modal-button-border-color: var(--color-border-2);
}

.ne-toolbar {
    flex: none;
    border-bottom: 1px solid var(--color-border-1);
}

/* 隐藏的原生工具栏：只用来实例化菜单，不展示 */
.ne-toolbar-src {
    display: none;
}

.ne-editor {
    flex: 1;
    min-height: 0;
    overflow-y: hidden;
}

/* 书写面：底色走 --w-e-textarea-bg-color（默认/纹理档 bg-2 实底）；玻璃档透出 np-main 卡片磨砂 */
.ne-editor-wrap :deep(.w-e-text-container) {
    color: var(--color-text-1);
    /* 去掉 wangeditor 自带的硬边框（不随主题「描边」变化），层次只靠主题分隔线表达 */
    border: none !important;
}

body.app-glass .ne-editor-wrap :deep(.w-e-text-container) {
    background-color: transparent;
}

/* 工具栏底部分隔线：接主题描边，随「描边=0」隐去 */
.ne-editor-wrap :deep(.w-e-toolbar) {
    border-bottom: 1px solid var(--color-border-1) !important;
}

.ne-editor-wrap :deep(.w-e-bar) {
    font-size: var(--base-font-size);
}
.ne-editor-wrap :deep(.w-e-bar-item) {
    padding: 0;
    height: 32px;
}

/* 工具栏按钮对齐 a-* 观感：圆角 + 轻过渡；hover 底色由 --w-e-toolbar-active-bg-color 驱动 */
.ne-editor-wrap :deep(.w-e-bar-item button) {
    border-radius: var(--base-radius-default);
    transition: background-color 0.15s ease, color 0.15s ease;
}

/* 选中态（光标所在段落对应的菜单）用主色淡底 + 主色图标，贴近 Arco 激活态 */
.ne-editor-wrap :deep(.w-e-bar-item .active),
.ne-editor-wrap :deep(.w-e-bar-item button.active) {
    background-color: rgba(var(--primary-6), 0.14);
    color: rgb(var(--primary-6));
}

.ne-editor-wrap :deep(.w-e-bar-item .active svg),
.ne-editor-wrap :deep(.w-e-bar-item button.active svg) {
    fill: rgb(var(--primary-6));
}

/* 分隔线随控件收细、颜色接主题描边（默认 40px 通栏太硬） */
.ne-editor-wrap :deep(.w-e-bar-divider) {
    align-self: center;
    height: 20px;
    background-color: var(--color-border-2);
}

/* 弹出面板（下拉/取色/表情/表格/分组/模态/悬浮条）统一对齐 Arco a-dropdown / a-select 弹层：
   实色 bg-5 表面 + 描边 + 圆角 + 投影，随明暗翻面（之前只吃 wangeditor 默认白底，不跟主题） */
.ne-editor-wrap :deep(.w-e-select-list),
.ne-editor-wrap :deep(.w-e-drop-panel),
.ne-editor-wrap :deep(.w-e-bar-item-group .w-e-bar-item-menus-container),
.ne-editor-wrap :deep(.w-e-modal),
.ne-editor-wrap :deep(.w-e-hover-bar) {
    background-color: var(--color-bg-5);
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-default);
    box-shadow: var(--shadow-2-bottom);
    color: var(--color-text-1);
}

/* 下拉条目：文字接主题，hover 用 fill-2 轻底，当前选中项用主色淡底（同 a-select） */
.ne-editor-wrap :deep(.w-e-select-list ul li) {
    color: var(--color-text-1);
}

.ne-editor-wrap :deep(.w-e-select-list ul li:hover) {
    background-color: var(--color-fill-2);
}

.ne-editor-wrap :deep(.w-e-select-list ul .selected) {
    background-color: rgba(var(--primary-6), 0.14);
    color: rgb(var(--primary-6));
}

.ne-editor-wrap :deep(.w-e-select-list ul .selected svg) {
    fill: rgb(var(--primary-6));
}
</style>
