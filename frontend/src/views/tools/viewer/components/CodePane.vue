<template>
    <div class="code-pane">
        <!-- 顶栏：文件信息 + dirty 红点 + 只读切换 / 放弃 / 保存 -->
        <div class="cp-bar">
            <div class="cp-file">
                <common-file-icon :is-dir="false" :ext="ext" :size="18" />
                <span class="cp-name" :title="path">{{ baseName }}</span>
                <span v-if="dirty" class="cp-dot" title="有未保存的修改"></span>
                <span class="cp-size">{{ sizeText }}</span>
            </div>
            <a-space :size="8">
                <a-tag v-if="readOnlyReason" color="orange" size="small">{{ readOnlyReason }}</a-tag>
                <a-checkbox v-if="!forcedReadonly" v-model="readOnly" size="small">只读</a-checkbox>
                <a-button size="small" :disabled="!dirty" @click="revert">放弃</a-button>
                <a-button size="small" type="primary" :loading="saving" :disabled="!dirty || editorReadonly"
                    @click="save">保存</a-button>
            </a-space>
        </div>

        <div v-if="loading" class="cp-state">
            <a-spin />
            <span>读取中…</span>
        </div>
        <div v-else-if="loadError" class="cp-state cp-err">{{ loadError }}</div>
        <div v-else class="cp-editor-wrap">
            <div ref="editorEl" class="cp-editor"></div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onBeforeUnmount, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Compartment, EditorState } from "@codemirror/state";
import { EditorView, keymap } from "@codemirror/view";
import { defaultKeymap, history, historyKeymap, indentWithTab } from "@codemirror/commands";
import { bracketMatching, syntaxHighlighting, defaultHighlightStyle } from "@codemirror/language";
import { closeBrackets } from "@codemirror/autocomplete";
import { highlightSelectionMatches } from "@codemirror/search";
import { basicSetup } from "codemirror";
import { oneDark } from "@codemirror/theme-one-dark";

// 由父组件只传文件路径，读取/编辑/保存全在这里完成（遵循子组件自管状态、父只给上下文的约定）。
const props = defineProps<{ path: string }>();
const emit = defineEmits<{ (e: "saved"): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const editorEl = ref<HTMLElement>();
const loading = ref(false);
const saving = ref(false);
const loadError = ref("");
const dirty = ref(false);
const readOnly = ref(false);
const forcedReadonly = ref(false);
const readOnlyReason = ref("");
const sizeBytes = ref(0);

let view: EditorView | null = null;
let savedText = "";

const ext = computed(() => (props.path.split(".").pop() || "").toLowerCase());
const baseName = computed(() => props.path.replace(/^.*[\\/]/, ""));
const sizeText = computed(() => fmtSize(sizeBytes.value));
const editorReadonly = computed(() => forcedReadonly.value || readOnly.value);

function fmtSize(n: number): string {
    if (!n) return "";
    const u = ["B", "KB", "MB", "GB"];
    let i = 0, v = n;
    while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
    return (i === 0 ? v : v.toFixed(1)) + " " + u[i];
}

// 按扩展名动态加载语言包：每个语言各自成 chunk，避免把全部语法打进主包（构建已警告 >500kB）。
async function loadLanguage(e: string): Promise<any> {
    switch (e) {
        case "js":
        case "jsx":
            return (await import("@codemirror/lang-javascript")).javascript();
        case "ts":
            return (await import("@codemirror/lang-javascript")).javascript({ typescript: true });
        case "tsx":
            return (await import("@codemirror/lang-javascript")).javascript({ typescript: true, jsx: true });
        case "vue":
            return (await import("@codemirror/lang-vue")).vue();
        case "html":
        case "htm":
            return (await import("@codemirror/lang-html")).html();
        case "css":
            return (await import("@codemirror/lang-css")).css();
        case "json":
            return (await import("@codemirror/lang-json")).json();
        case "md":
            return (await import("@codemirror/lang-markdown")).markdown();
        case "py":
            return (await import("@codemirror/lang-python")).python();
        case "php":
            return (await import("@codemirror/lang-php")).php();
        case "go":
            return (await import("@codemirror/lang-go")).go();
        case "yaml":
        case "yml":
            return (await import("@codemirror/lang-yaml")).yaml();
        default:
            return null;
    }
}

// 只读用一个 Compartment 承载，切换只重配这个槽，不重建编辑器、不丢光标与历史。
const readonlyComp = new Compartment();

function readonlyExt(on: boolean) {
    return [EditorState.readOnly.of(on), EditorView.editable.of(!on)];
}

async function mountEditor(text: string) {
    destroyEditor();
    const lang = await loadLanguage(ext.value);
    const base: any[] = [
        basicSetup,
        oneDark,
        syntaxHighlighting(defaultHighlightStyle, { fallback: true }),
        bracketMatching(),
        closeBrackets(),
        highlightSelectionMatches(),
        history(),
        EditorView.lineWrapping,
        keymap.of([...defaultKeymap, ...historyKeymap, indentWithTab]),
        readonlyComp.of(readonlyExt(editorReadonly.value)),
        EditorView.updateListener.of((up) => {
            if (up.docChanged) dirty.value = up.state.doc.toString() !== savedText;
        }),
    ];
    if (lang) base.push(lang);
    const state = EditorState.create({ doc: text, extensions: base });
    view = new EditorView({ state, parent: editorEl.value! });
}

function destroyEditor() {
    if (view) { view.destroy(); view = null; }
}

watch(readOnly, (on) => {
    view?.dispatch({ effects: readonlyComp.reconfigure(readonlyExt(forcedReadonly.value || on)) });
});

async function load() {
    if (!props.path) { destroyEditor(); return; }
    loading.value = true;
    loadError.value = "";
    dirty.value = false;
    forcedReadonly.value = false;
    readOnlyReason.value = "";
    readOnly.value = false;
    try {
        const r: any = await GoTools.FileReadText(props.path);
        sizeBytes.value = r?.size ?? 0;
        savedText = r?.content ?? "";
        // 二进制：不可编辑，只给开头小段供查看
        if (r?.binary) {
            forcedReadonly.value = true;
            readOnlyReason.value = "二进制文件，只读";
        } else if (r?.truncated) {
            // 截断内容直接保存会丢掉尾部，锁成只读并提示走系统程序打开
            forcedReadonly.value = true;
            readOnlyReason.value = "文件过大已截断，只读";
        }
        // 先把 loading 置假让 .cp-editor-wrap 进入 DOM，否则 editorEl 还是 undefined，
        // mountEditor 会把 CodeMirror 建在一个没挂到文档的节点上（顶栏在、内容空白）。
        loading.value = false;
        await nextTick();
        await mountEditor(savedText);
    } catch (e: any) {
        loadError.value = String(e?.message || e || "读取失败");
    } finally {
        loading.value = false;
    }
}

async function save() {
    if (!view || !dirty.value || editorReadonly.value) return;
    const text = view.state.doc.toString();
    saving.value = true;
    try {
        await GoTools.FileWriteText(props.path, text);
        savedText = text;
        dirty.value = false;
        Message.success("已保存：" + baseName.value);
        emit("saved");
    } catch (e: any) {
        Message.error("保存失败：" + String(e?.message || e));
    } finally {
        saving.value = false;
    }
}

function revert() {
    if (!view) return;
    view.dispatch({ changes: { from: 0, to: view.state.doc.length, insert: savedText } });
    dirty.value = false;
}

watch(() => props.path, load, { immediate: true });
onBeforeUnmount(destroyEditor);
</script>

<style scoped>
.code-pane {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
}
.cp-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 6px 10px;
    border-bottom: 1px solid var(--color-border-2);
    flex: none;
}
.cp-file {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
}
.cp-name {
    font-size: var(--base-size-14);
    color: var(--color-text-1);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 360px;
}
.cp-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: rgb(var(--danger-6));
    flex: none;
}
.cp-size {
    font-size: var(--base-size-12);
    color: var(--color-text-3);
    flex: none;
}
.cp-state {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    color: var(--color-text-3);
}
.cp-err {
    color: rgb(var(--danger-6));
}
.cp-editor-wrap {
    flex: 1;
    min-height: 0;
    overflow: hidden;
}
.cp-editor {
    height: 100%;
    overflow: auto;
}
/* CodeMirror 默认字号偏小，贴合本项目阅读习惯 */
.cp-editor :deep(.cm-editor) {
    height: 100%;
    font-size: 13px;
}
</style>
