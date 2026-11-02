<template>
    <div class="doc-pane">
        <div class="dp-bar">
            <a-tag v-if="mode === 'edit'" color="orange" size="small">编辑中 · 保真有限</a-tag>
            <span v-else class="dp-hint">只读预览；在线编辑仅保基本文字/标题/列表，复杂排版请用系统程序打开。</span>
            <a-space :size="8" class="dp-actions">
                <template v-if="mode === 'read'">
                    <a-button size="mini" @click="reload">重新读取</a-button>
                    <a-button v-if="canWrite" size="mini" type="primary" @click="enterEdit">在线编辑</a-button>
                </template>
                <template v-else>
                    <a-button size="mini" :disabled="saving" @click="cancelEdit">取消</a-button>
                    <a-button size="mini" type="primary" :loading="saving" @click="save">保存</a-button>
                </template>
            </a-space>
        </div>
        <div class="dp-body">
            <div v-if="loading" class="dp-state"><a-spin /><span>渲染文档…</span></div>
            <div v-else-if="err" class="dp-state dp-err">{{ err }}</div>
            <div v-show="mode === 'read' && !loading && !err" ref="readEl" class="dp-read"></div>
            <div v-show="mode === 'edit'" ref="editEl" class="dp-edit" contenteditable="true" spellcheck="false"></div>
        </div>
    </div>
</template>

<script setup lang="ts">
/*
docx 预览与在线编辑（kind = doc）：
- 只读：docx-preview 动态 import 忠实渲染（独立 chunk，不进主包）。
- 编辑：mammoth 把 docx 转 HTML 放进 contenteditable；保存时遍历 DOM 用 `docx`（纯 JS，
  浏览器安全）重建为**合法 OOXML** .docx，再 base64 → FileWriteBinary 写回，emit('changed')。
  这是「保真有限」的往返：段落/标题/列表/粗斜体下划线能保住，表格尽量，图片/域代码/母版/批注会丢——
  UI 已明示，复杂文档仍走父级「系统程序打开」。
- 非本机可写文件（sftp 托管）canWrite=false，只读。
*/
import { getCurrentInstance, nextTick, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";

const props = defineProps<{ url: string; path: string; name: string; canWrite: boolean }>();
const emit = defineEmits<{ (e: "changed"): void }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const loading = ref(true);
const err = ref("");
const mode = ref<"read" | "edit">("read");
const saving = ref(false);
const readEl = ref<HTMLElement | null>(null);
const editEl = ref<HTMLElement | null>(null);
let rawBuffer: ArrayBuffer | null = null;

async function ensureBuffer(): Promise<ArrayBuffer | null> {
    if (rawBuffer) return rawBuffer;
    if (!props.url) return null;
    const resp = await fetch(props.url);
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
    rawBuffer = await resp.arrayBuffer();
    return rawBuffer;
}

async function renderRead() {
    loading.value = true;
    err.value = "";
    try {
        const buf = await ensureBuffer();
        if (!buf) throw new Error("无内容");
        const mod: any = await import("docx-preview");
        const render = mod.render || mod.default?.render;
        await nextTick();
        if (readEl.value) {
            readEl.value.innerHTML = "";
            await render(buf, readEl.value, { inWrapper: true, ignoreLastRenderedPageBreak: true, experimental: false });
        }
    } catch (e: any) {
        err.value = "文档渲染失败：" + String(e?.message || e);
    } finally {
        loading.value = false;
    }
}

async function enterEdit() {
    loading.value = true;
    err.value = "";
    try {
        const buf = await ensureBuffer();
        if (!buf) throw new Error("无内容");
        const m: any = await import("mammoth");
        const mammoth = m.default ?? m;
        const { value } = await mammoth.convertToHtml({ arrayBuffer: buf });
        mode.value = "edit";
        await nextTick();
        if (editEl.value) editEl.value.innerHTML = value || "<p></p>";
    } catch (e: any) {
        err.value = "转入编辑失败：" + String(e?.message || e);
        mode.value = "read";
    } finally {
        loading.value = false;
    }
}

function cancelEdit() {
    mode.value = "read";
    void renderRead();
}

// 手动“重新读取”：用本机程序（系统打开）改完 docx 后，清缓存重拉最新字节回读。
async function reload() {
    rawBuffer = null;
    mode.value = "read";
    if (!props.url) return;
    loading.value = true;
    err.value = "";
    try {
        const resp = await fetch(props.url + (props.url.includes("?") ? "&" : "?") + "_r=" + Date.now());
        if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
        rawBuffer = await resp.arrayBuffer();
    } catch (e: any) {
        err.value = "重新读取失败：" + String(e?.message || e);
    } finally {
        loading.value = false;
    }
    await renderRead();
}

function bufToBase64(buf: ArrayBuffer): string {
    const bytes = new Uint8Array(buf);
    let bin = "";
    const chunk = 0x8000;
    for (let i = 0; i < bytes.length; i += chunk) {
        bin += String.fromCharCode(...bytes.subarray(i, i + chunk));
    }
    return btoa(bin);
}

// 遍历一个块内的行内节点，产出带粗/斜/下划线标记的 TextRun
function inlineRuns(doc: any, node: Node, fmt: { b?: boolean; i?: boolean; u?: boolean } = {}): any[] {
    const runs: any[] = [];
    node.childNodes.forEach((child) => {
        if (child.nodeType === Node.TEXT_NODE) {
            const t = child.textContent || "";
            if (t) runs.push(new doc.TextRun({ text: t, bold: fmt.b, italics: fmt.i, underline: fmt.u ? {} : undefined }));
        } else if (child.nodeType === Node.ELEMENT_NODE) {
            const el = child as HTMLElement;
            const tag = el.tagName.toLowerCase();
            if (tag === "br") runs.push(new doc.TextRun({ text: "", break: 1 }));
            else runs.push(...inlineRuns(doc, el, {
                b: fmt.b || tag === "strong" || tag === "b",
                i: fmt.i || tag === "em" || tag === "i",
                u: fmt.u || tag === "u",
            }));
        }
    });
    return runs;
}

// contenteditable DOM → docx children（段落/标题/列表/表格，其余按纯文本段落兜底）
function domToChildren(doc: any, root: HTMLElement): any[] {
    const children: any[] = [];
    const headingMap: Record<string, any> = {
        h1: doc.HeadingLevel.HEADING_1, h2: doc.HeadingLevel.HEADING_2, h3: doc.HeadingLevel.HEADING_3,
        h4: doc.HeadingLevel.HEADING_4, h5: doc.HeadingLevel.HEADING_5, h6: doc.HeadingLevel.HEADING_6,
    };
    root.childNodes.forEach((node) => {
        if (node.nodeType === Node.TEXT_NODE) {
            const t = (node.textContent || "").trim();
            if (t) children.push(new doc.Paragraph({ children: [new doc.TextRun(t)] }));
            return;
        }
        if (node.nodeType !== Node.ELEMENT_NODE) return;
        const el = node as HTMLElement;
        const tag = el.tagName.toLowerCase();
        if (headingMap[tag]) {
            children.push(new doc.Paragraph({ heading: headingMap[tag], children: inlineRuns(doc, el) }));
        } else if (tag === "ul" || tag === "ol") {
            el.querySelectorAll(":scope > li").forEach((li) => {
                children.push(new doc.Paragraph({
                    children: inlineRuns(doc, li),
                    bullet: tag === "ul" ? { level: 0 } : undefined,
                    numbering: tag === "ol" ? { level: 0, reference: "doc-num" } : undefined,
                }));
            });
        } else if (tag === "table") {
            const rows = Array.from(el.querySelectorAll("tr")).map((tr) => new doc.TableRow({
                children: Array.from(tr.querySelectorAll("td,th")).map((td) => new doc.TableCell({
                    children: [new doc.Paragraph({ children: inlineRuns(doc, td as HTMLElement) })],
                })),
            }));
            if (rows.length) children.push(new doc.Table({ rows }));
        } else if (tag === "p" || tag === "div") {
            const runs = inlineRuns(doc, el);
            children.push(new doc.Paragraph({ children: runs.length ? runs : [new doc.TextRun("")] }));
        } else {
            const runs = inlineRuns(doc, el);
            if (runs.length) children.push(new doc.Paragraph({ children: runs }));
        }
    });
    return children;
}

async function save() {
    if (!editEl.value || !props.path) return;
    if (editEl.value.innerText.trim().length === 0) {
        Message.warning("内容为空，未保存");
        return;
    }
    saving.value = true;
    try {
        const mod: any = await import("docx");
        const doc = new mod.Document({
            numbering: { config: [{ reference: "doc-num", levels: [{ level: 0, format: mod.LevelFormat.DECIMAL, text: "%1." }] }] },
            sections: [{ children: domToChildren(mod, editEl.value) }],
        });
        const out: ArrayBuffer = await mod.Packer.toArrayBuffer(doc);
        await GoTools.FileWriteBinary(props.path, bufToBase64(out));
        rawBuffer = out; // 用新内容覆盖缓存，回到只读即渲染最新版
        mode.value = "read";
        await renderRead();
        emit("changed");
        Message.success("已保存（在线编辑保真有限，请核对排版）");
    } catch (e: any) {
        Message.error("保存失败：" + String(e?.message || e));
    } finally {
        saving.value = false;
    }
}

function reset() {
    rawBuffer = null;
    mode.value = "read";
    void renderRead();
}

watch(() => props.url, reset, { immediate: true });
</script>

<style scoped>
.doc-pane {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}
.dp-bar {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--base-size-12);
    padding: 6px 10px;
    border-bottom: 1px solid var(--color-border-2);
}
.dp-hint {
    font-size: var(--base-size-12);
    color: var(--color-text-3);
}
.dp-actions {
    flex: none;
    margin-left: auto;
}
.dp-body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    background: var(--color-fill-1);
}
.dp-state {
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    color: var(--color-text-3);
}
.dp-err {
    color: rgb(var(--danger-6));
    padding: 0 16px;
    text-align: center;
}
.dp-read {
    min-height: 100%;
}
.dp-edit {
    min-height: 100%;
    padding: 24px 32px;
    background: var(--color-bg-3);
    color: var(--color-text-1);
    line-height: 1.7;
    outline: none;
}
.dp-edit:focus {
    box-shadow: inset 0 0 0 2px rgb(var(--primary-3));
}
.dp-edit :deep(p),
.dp-edit :deep(h1),
.dp-edit :deep(h2),
.dp-edit :deep(h3),
.dp-edit :deep(ul),
.dp-edit :deep(ol) {
    margin: 0 0 8px;
}
.dp-edit :deep(table) {
    border-collapse: collapse;
    margin: 0 0 8px;
}
.dp-edit :deep(td),
.dp-edit :deep(th) {
    border: 1px solid var(--color-border-2);
    padding: 3px 8px;
}
</style>
