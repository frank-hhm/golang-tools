<template>
    <div class="body-viewer">
        <div class="bv-toolbar">
            <a-radio-group v-model="view" type="button" size="mini">
                <a-radio value="pretty">美化</a-radio>
                <a-radio value="raw">原生</a-radio>
                <a-radio value="preview">预览</a-radio>
                <a-radio value="visual">可视化</a-radio>
            </a-radio-group>
            <a-select v-model="format" size="mini" class="bv-format">
                <a-option value="json">JSON</a-option>
                <a-option value="xml">XML</a-option>
                <a-option value="html">HTML</a-option>
                <a-option value="text">Text</a-option>
                <a-option value="auto">Auto</a-option>
            </a-select>
            <a-button size="mini" type="text" :disabled="!body" @click="copy">
                <template #icon><icon-copy /></template>复制
            </a-button>
            <span class="bv-meta">{{ formatLabel }} · {{ sizeText }}</span>
        </div>

        <div class="bv-content">
            <a-empty v-if="!body" class="bv-empty" :description="emptyText" />
            <div v-else-if="view === 'pretty' || view === 'raw'" class="bv-code">
                <div v-if="!tooLarge" class="bv-gutter">
                    <div v-for="n in lineCount" :key="n">{{ n }}</div>
                </div>
                <pre class="bv-pre" v-html="codeHtml"></pre>
            </div>
            <div v-else-if="view === 'preview'" class="bv-preview">
                <iframe v-if="effectiveFormat === 'html'" class="bv-frame" :srcdoc="rawText" sandbox=""
                    referrerpolicy="no-referrer"></iframe>
                <a-empty v-else description="该格式暂不支持预览渲染" />
            </div>
            <div v-else class="bv-visual">
                <a-tree v-if="treeData.length" :key="treeVersion" :data="treeData" size="small" block-node
                    :default-expanded-keys="rootKeys" />
                <a-empty v-else description="该格式暂不支持可视化" />
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import type { TreeNodeData } from "@arco-design/web-vue";

const props = withDefaults(
    defineProps<{
        /** 原始报文（字符串） */
        body?: string;
        /** 响应的 Content-Type，用于 Auto 识别 */
        contentType?: string;
        /** json / xml / html / text / auto */
        format?: string;
        /** pretty / raw / preview / visual */
        defaultView?: string;
        emptyText?: string;
    }>(),
    {
        body: "",
        contentType: "",
        format: "auto",
        defaultView: "pretty",
        emptyText: "（空）",
    }
);

/** 体量过大时跳过美化与高亮，避免卡顿 */
const MAX_HIGHLIGHT = 300_000;
const MAX_TREE_NODES = 2000;
const VOID_TAGS = new Set([
    "area", "base", "br", "col", "embed", "hr", "img", "input",
    "link", "meta", "param", "source", "track", "wbr",
]);

const view = ref<string>(props.defaultView);
const format = ref<string>(props.format);

/** 统一换行，便于行号与 pre 渲染 */
const rawText = computed(() => (props.body || "").replace(/\r\n?/g, "\n"));
const tooLarge = computed(() => rawText.value.length > MAX_HIGHLIGHT);

const effectiveFormat = computed<string>(() => {
    const f = format.value;
    if (f === "json" || f === "xml" || f === "html" || f === "text") return f;
    return detectFormat(rawText.value, props.contentType || "");
});

function detectFormat(body: string, contentType: string): "json" | "xml" | "html" | "text" {
    const b = (body || "").trim();
    const ct = (contentType || "").toLowerCase();
    if (!b) return "text";
    if (ct.includes("json") || b.startsWith("{") || b.startsWith("[")) {
        try {
            JSON.parse(b);
            return "json";
        } catch {
            /* 不是合法 JSON，继续探测 */
        }
    }
    if (ct.includes("xml") || b.startsWith("<?xml")) return "xml";
    if (ct.includes("html") || /^<!doctype\s+html/i.test(b) || /^<html[\s>]/i.test(b)) return "html";
    if (/^<[a-z!/]/i.test(b)) return "html";
    return "text";
}

/** 美化后的文本（JSON 缩进 / HTML、XML 逐层缩进 / 其它原样） */
const prettyText = computed(() => {
    const b = rawText.value;
    if (!b) return "";
    if (tooLarge.value) return b;
    const fmt = effectiveFormat.value;
    if (fmt === "json") {
        try {
            return JSON.stringify(JSON.parse(b), null, 2);
        } catch {
            return b;
        }
    }
    if (fmt === "html" || fmt === "xml") return formatMarkup(b, fmt) || b;
    return b;
});

/** 当前代码视图对应的纯文本 */
const codeText = computed(() => (view.value === "raw" ? rawText.value : prettyText.value));
const lineCount = computed(() => Math.max(1, codeText.value.split("\n").length));

const codeHtml = computed(() => {
    const t = codeText.value;
    if (!t) return "";
    if (tooLarge.value || view.value === "raw") return escapeHtml(t);
    const fmt = effectiveFormat.value;
    if (fmt === "json") return highlightJson(t);
    if (fmt === "html" || fmt === "xml") return highlightMarkup(t);
    return escapeHtml(t);
});

const formatLabel = computed(() => {
    const f = effectiveFormat.value.toUpperCase();
    return format.value === "auto" ? `${f} · 自动` : f;
});

const sizeText = computed(() => {
    const n = new TextEncoder().encode(props.body || "").length;
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / 1024 / 1024).toFixed(1)} MB`;
});

/* ============ 可视化树 ============ */
const treeData = computed<TreeNodeData[]>(() => {
    if (tooLarge.value) return [];
    const fmt = effectiveFormat.value;
    if (fmt === "json") {
        try {
            return buildJsonTree(JSON.parse(rawText.value), "", "root", { n: 0 });
        } catch {
            return [];
        }
    }
    if (fmt === "html" || fmt === "xml") return buildMarkupTree(rawText.value, fmt);
    return [];
});

const rootKeys = computed(() =>
    treeData.value
        .map((n) => n.key)
        .filter((k): k is string | number => k !== undefined)
);

/** 报文变化时重建树，让默认展开重新生效 */
const treeVersion = computed(
    () => `${effectiveFormat.value}:${rawText.value.length}:${rawText.value.slice(0, 32)}`
);

function buildJsonTree(value: unknown, key: string, path: string, budget: { n: number }): TreeNodeData[] {
    if (budget.n > MAX_TREE_NODES) return [];
    budget.n++;
    if (value === null || typeof value !== "object") {
        const prefix = key ? `${key}: ` : "";
        return [{ key: path, title: prefix + formatPrimitive(value) }];
    }
    const isArr = Array.isArray(value);
    const entries: Array<[string, unknown]> = isArr
        ? (value as unknown[]).map((v, i) => [String(i), v] as [string, unknown])
        : Object.entries(value as Record<string, unknown>);
    const children = entries.flatMap(([k, v]) => buildJsonTree(v, k, `${path}\u0001${k}`, budget));
    const label = `${key || "root"}${isArr ? ` [${entries.length}]` : ` {${entries.length}}`}`;
    return [{ key: path, title: label, children }];
}

function formatPrimitive(v: unknown) {
    if (typeof v === "string") {
        const s = v.length > 160 ? v.slice(0, 160) + "…" : v;
        return `"${s}"`;
    }
    return String(v);
}

interface ParsedMarkup {
    nodes: Node[];
    ok: boolean;
}

function parseMarkup(text: string, fmt: "html" | "xml"): ParsedMarkup {
    try {
        if (fmt === "html") {
            const doc = new DOMParser().parseFromString(text, "text/html");
            const isDoc = /<html[\s>]/i.test(text) || /<!doctype/i.test(text);
            return {
                nodes: isDoc
                    ? (doc.documentElement ? [doc.documentElement] : [])
                    : Array.from(doc.body.childNodes),
                ok: true,
            };
        }
        const doc = new DOMParser().parseFromString(text, "text/xml");
        if (doc.getElementsByTagName("parsererror").length) return { nodes: [], ok: false };
        return { nodes: doc.documentElement ? [doc.documentElement] : [], ok: true };
    } catch {
        return { nodes: [], ok: false };
    }
}

/** HTML / XML 逐层缩进美化；解析失败时返回空串（由调用方回退原文） */
function formatMarkup(text: string, fmt: "html" | "xml"): string {
    const { nodes, ok } = parseMarkup(text, fmt);
    if (!ok) return "";
    const out: string[] = [];
    const padOf = (d: number) => "  ".repeat(d);

    const walk = (node: Node, depth: number) => {
        if (node.nodeType === Node.TEXT_NODE) {
            const t = (node.textContent || "").replace(/\s+/g, " ").trim();
            if (t) out.push(padOf(depth) + t);
            return;
        }
        if (node.nodeType === Node.COMMENT_NODE) {
            out.push(padOf(depth) + `<!--${node.textContent || ""}-->`);
            return;
        }
        if (node.nodeType !== Node.ELEMENT_NODE) return;

        const el = node as Element;
        const tag = el.tagName.toLowerCase();
        const attrs = Array.from(el.attributes).map((a) => ` ${a.name}="${a.value}"`).join("");
        const pad = padOf(depth);
        const kids = Array.from(el.childNodes).filter(
            (n) =>
                n.nodeType === Node.ELEMENT_NODE ||
                (n.nodeType === Node.TEXT_NODE && !!(n.textContent || "").trim())
        );

        if (!kids.length) {
            out.push(fmt === "html" && VOID_TAGS.has(tag) ? `${pad}<${tag}${attrs}>` : `${pad}<${tag}${attrs}></${tag}>`);
            return;
        }

        // 内联脚本/样式/预格式化内容保留原始换行
        if (["script", "style", "pre", "textarea"].includes(tag)) {
            out.push(`${pad}<${tag}${attrs}>`);
            (el.textContent || "").split("\n").forEach((l) => out.push(padOf(depth + 1) + l));
            out.push(`${pad}</${tag}>`);
            return;
        }

        // 纯文本子节点合成一行，如 <h1>404 Not Found</h1>
        if (kids.every((n) => n.nodeType === Node.TEXT_NODE)) {
            const t = kids.map((n) => (n.textContent || "").replace(/\s+/g, " ").trim()).join(" ");
            out.push(`${pad}<${tag}${attrs}>${t}</${tag}>`);
            return;
        }

        out.push(`${pad}<${tag}${attrs}>`);
        el.childNodes.forEach((n) => walk(n, depth + 1));
        out.push(`${pad}</${tag}>`);
    };

    nodes.forEach((n) => walk(n, 0));
    return out.join("\n");
}

function buildMarkupTree(text: string, fmt: "html" | "xml"): TreeNodeData[] {
    const { nodes, ok } = parseMarkup(text, fmt);
    if (!ok) return [];
    let seq = 0;

    const conv = (node: Node): TreeNodeData[] => {
        if (seq > MAX_TREE_NODES) return [];
        if (node.nodeType === Node.TEXT_NODE) {
            const t = (node.textContent || "").replace(/\s+/g, " ").trim();
            if (!t) return [];
            seq++;
            return [{ key: `t${seq}`, title: t.length > 200 ? t.slice(0, 200) + "…" : t }];
        }
        if (node.nodeType === Node.COMMENT_NODE) {
            seq++;
            return [{ key: `m${seq}`, title: `<!--${node.textContent || ""}-->` }];
        }
        if (node.nodeType !== Node.ELEMENT_NODE) return [];

        const el = node as Element;
        const tag = el.tagName.toLowerCase();
        const hasElementChild = Array.from(el.childNodes).some((n) => n.nodeType === Node.ELEMENT_NODE);
        seq++;
        if (!hasElementChild) {
            const t = (el.textContent || "").replace(/\s+/g, " ").trim();
            const shown = t.length > 200 ? t.slice(0, 200) + "…" : t;
            return [{ key: `e${seq}`, title: shown ? `<${tag}> ${shown}` : `<${tag}>` }];
        }
        return [{
            key: `e${seq}`,
            title: `<${tag}>`,
            children: Array.from(el.childNodes).flatMap(conv),
        }];
    };

    return nodes.flatMap(conv);
}

/* ============ 语法高亮（先生成纯文本，再按 token 包裹 span） ============ */
const escapeHtml = (s: string) =>
    s.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

function highlightJson(text: string): string {
    return escapeHtml(text).replace(
        /("(?:\\.|[^"\\])*")(\s*:)?|\b(true|false|null)\b|(-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?)/g,
        (m: string, str?: string, colon?: string, lit?: string, num?: string) => {
            if (str) {
                return colon
                    ? `<span class="tok-key">${str}</span>${colon}`
                    : `<span class="tok-str">${str}</span>`;
            }
            if (lit) return `<span class="tok-lit">${lit}</span>`;
            if (num) return `<span class="tok-num">${num}</span>`;
            return m;
        }
    );
}

function highlightMarkup(text: string): string {
    return escapeHtml(text).replace(
        /(&lt;!--[\s\S]*?--&gt;)|(&lt;\/?)([\w:.-]+)([\s\S]*?)(\/?&gt;)/g,
        (m: string, comment?: string, open?: string, tag?: string, rest?: string, close?: string) => {
            if (comment) return `<span class="tok-com">${comment}</span>`;
            return `<span class="tok-punc">${open}</span><span class="tok-tag">${tag}</span>${rest}<span class="tok-punc">${close}</span>`;
        }
    );
}

async function copy() {
    try {
        await navigator.clipboard.writeText(codeText.value);
        Message.success("已复制到剪贴板");
    } catch {
        Message.error("复制失败");
    }
}
</script>

<style scoped>
.body-viewer {
    height: 100%;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.bv-toolbar {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--base-radius-large);
    flex-wrap: wrap;
    padding-bottom: var(--base-radius-default);
}

.bv-format {
    width: 110px;
}

.bv-meta {
    margin-left: auto;
    color: var(--color-text-3);
    font-size: var(--base-size-small);
}

.bv-content {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.bv-empty {
    margin: auto;
}

/* 代码视图：左侧行号 + 右侧内容，整体横纵向滚动 */
.bv-code {
    flex: 1;
    min-height: 0;
    display: flex;
    overflow: auto;
    background: #1d2129;
    border-radius: var(--base-radius-default);
}

.bv-gutter {
    flex: none;
    position: sticky;
    left: 0;
    z-index: 1;
    padding: var(--base-padding) 8px;
    text-align: right;
    color: #6b7280;
    background: #171a1f;
    border-right: 1px solid #2b3038;
    font-family: Consolas, Monaco, "Courier New", monospace;
    font-size: var(--base-size-small);
    line-height: 1.6;
    user-select: none;
}

.bv-pre {
    flex: 1;
    margin: 0;
    padding: var(--base-padding);
    color: #e6e6e6;
    font-family: Consolas, Monaco, "Courier New", monospace;
    font-size: var(--base-size-small);
    line-height: 1.6;
    white-space: pre;
    tab-size: 2;
}

.bv-pre :deep(.tok-key) {
    color: #f2c97d;
}

.bv-pre :deep(.tok-str) {
    color: #7ec699;
}

.bv-pre :deep(.tok-num) {
    color: #f08d49;
}

.bv-pre :deep(.tok-lit) {
    color: #cc99cd;
}

.bv-pre :deep(.tok-tag) {
    color: #e06c75;
}

.bv-pre :deep(.tok-punc) {
    color: #9aa4b2;
}

.bv-pre :deep(.tok-com) {
    color: #6b7280;
    font-style: italic;
}

.bv-preview {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.bv-preview :deep(.arco-empty),
.bv-visual :deep(.arco-empty) {
    margin: auto;
}

.bv-frame {
    flex: 1;
    width: 100%;
    min-height: 0;
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-default);
    background: #fff;
}

.bv-visual {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: auto;
}

.bv-visual :deep(.arco-tree) {
    background: transparent;
}
</style>
