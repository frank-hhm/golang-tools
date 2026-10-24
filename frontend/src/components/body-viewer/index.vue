<template>
    <div class="body-viewer">
        <div class="bv-toolbar">
            <a-space>
                <a-radio-group v-model="view" type="button">
                    <template v-if="isBinary">
                        <a-radio value="preview">预览</a-radio>
                        <a-radio value="hex">HEX</a-radio>
                    </template>
                    <template v-else>
                        <a-radio value="pretty">美化</a-radio>
                        <a-radio value="raw">原生</a-radio>
                        <a-radio value="preview">预览</a-radio>
                        <a-radio value="visual">可视化</a-radio>
                    </template>
                </a-radio-group>
                <a-select v-if="!isBinary" v-model="format" class="bv-format">
                    <a-option value="json">JSON</a-option>
                    <a-option value="xml">XML</a-option>
                    <a-option value="html">HTML</a-option>
                    <a-option value="text">Text</a-option>
                    <a-option value="auto">Auto</a-option>
                </a-select>
                <a-button size="small" :disabled="!body" @click="copy">
                    <template #icon><icon-copy /></template>复制
                </a-button>
                <a-button v-if="isBinary" size="small" @click="download">
                    <template #icon><icon-download /></template>下载
                </a-button>
                <span class="bv-meta">{{ formatLabel }} · {{ sizeText }}<template v-if="isBinary && truncated"> ·
                        已截断</template></span>
            </a-space>
        </div>

        <div class="bv-content">
            <a-empty v-if="!body" class="bv-empty" :description="emptyText" />
            <!-- 二进制体（base64）：图片/视频/音频/PDF 直接渲染，其余看 HEX 或下载 -->
            <div v-else-if="isBinary && view === 'hex'" class="bv-code">
                <pre class="bv-pre" v-text="hexText"></pre>
            </div>
            <div v-else-if="isBinary" class="bv-media">
                <a-alert v-if="truncated" type="warning" class="bv-trunc-tip">
                    响应体超过捕获上限已被截断，预览/播放可能不完整，可用 HEX 视图查看已捕获部分
                </a-alert>
                <img v-if="binaryKind === 'image'" :src="dataUrl" class="bv-img" alt="" />
                <video v-else-if="binaryKind === 'video'" :src="dataUrl" class="bv-video" controls></video>
                <audio v-else-if="binaryKind === 'audio'" :src="dataUrl" class="bv-audio" controls></audio>
                <iframe v-else-if="binaryKind === 'pdf'" :src="dataUrl" class="bv-frame"></iframe>
                <a-empty v-else :description="`该类型(${binaryKindLabel})不支持直接预览，可点「下载」保存到本地，或切 HEX 查看`" />
            </div>
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
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import type { TreeNodeData } from "@arco-design/web-vue";

const props = withDefaults(
    defineProps<{
        /** 原始报文（字符串；二进制体为 base64） */
        body?: string;
        /** 响应的 Content-Type，用于 Auto 识别 */
        contentType?: string;
        /** json / xml / html / text / auto */
        format?: string;
        /** pretty / raw / preview / visual */
        defaultView?: string;
        emptyText?: string;
        /** 报文体编码："" 文本 / "base64" 二进制 */
        encoding?: string;
        /** 原始字节大小（二进制体算 base64 前的真实大小，用于展示） */
        rawSize?: number;
        /** 后端捕获时是否截断过 */
        truncated?: boolean;
        /** 下载时的文件名 */
        filename?: string;
    }>(),
    {
        body: "",
        contentType: "",
        format: "auto",
        defaultView: "pretty",
        emptyText: "（空）",
        encoding: "",
        rawSize: 0,
        truncated: false,
        filename: "",
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

/* ============ 二进制体（图片/音视频/文件流） ============ */
type BinKind = "image" | "video" | "audio" | "pdf" | "font" | "binary";

const BIN_KIND_LABELS: Record<BinKind, string> = {
    image: "图片", video: "视频", audio: "音频", pdf: "PDF", font: "字体", binary: "二进制",
};

const EXT_BY_MIME: Record<string, string> = {
    "image/png": "png", "image/jpeg": "jpg", "image/gif": "gif", "image/webp": "webp",
    "image/bmp": "bmp", "image/avif": "avif", "video/mp4": "mp4", "video/webm": "webm",
    "audio/mpeg": "mp3", "audio/wav": "wav", "audio/ogg": "ogg", "audio/aac": "aac",
    "application/pdf": "pdf", "application/zip": "zip", "application/wasm": "wasm",
};

const isBinary = computed(() => props.encoding === "base64" && !!props.body);

const binaryKind = computed<BinKind>(() => {
    const t = (props.contentType || "").toLowerCase().split(";")[0].trim();
    if (t.startsWith("image/")) return "image";
    if (t.startsWith("video/")) return "video";
    if (t.startsWith("audio/")) return "audio";
    if (t === "application/pdf") return "pdf";
    if (t.startsWith("font/") || t.includes("font")) return "font";
    return "binary";
});

const binaryKindLabel = computed(() => BIN_KIND_LABELS[binaryKind.value]);

const mimeMain = computed(() => (props.contentType || "").split(";")[0].trim() || "application/octet-stream");
const dataUrl = computed(() => `data:${mimeMain.value};base64,${props.body || ""}`);

/** 二进制/文本切换时校正视图：二进制只剩 预览/HEX，文本不保留 HEX */
watch(isBinary, (b) => {
    if (b) {
        if (view.value !== "hex") view.value = "preview";
    } else if (view.value === "hex" || view.value === "preview") {
        view.value = "pretty";
    }
}, { immediate: true });

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
    // 体量过大时不做词法高亮；美化/原生两种视图都着色（原生仅不重排文本）
    if (tooLarge.value) return escapeHtml(t);
    const fmt = effectiveFormat.value;
    if (fmt === "json") return highlightJson(t);
    if (fmt === "html" || fmt === "xml") return highlightMarkup(t);
    return escapeHtml(t);
});

const formatLabel = computed(() => {
    if (isBinary.value) return `${binaryKindLabel.value} · 二进制`;
    const f = effectiveFormat.value.toUpperCase();
    return format.value === "auto" ? `${f} · 自动` : f;
});

const sizeText = computed(() => {
    const n = isBinary.value
        ? (props.rawSize > 0 ? props.rawSize : Math.floor(((props.body || "").length * 3) / 4))
        : new TextEncoder().encode(props.body || "").length;
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / 1024 / 1024).toFixed(1)} MB`;
});

/* ============ HEX 视图（仅展示前 64KB） ============ */
const HEX_LIMIT = 64 * 1024;

function base64ToBytes(b64: string): Uint8Array {
    try {
        const bin = atob(b64);
        const arr = new Uint8Array(bin.length);
        for (let i = 0; i < bin.length; i++) arr[i] = bin.charCodeAt(i);
        return arr;
    } catch {
        return new Uint8Array();
    }
}

const hexText = computed(() => {
    if (!isBinary.value) return "";
    const bytes = base64ToBytes(props.body || "");
    const n = Math.min(bytes.length, HEX_LIMIT);
    const lines: string[] = [];
    for (let off = 0; off < n; off += 16) {
        let hexPart = "";
        let asciiPart = "";
        for (let i = 0; i < 16; i++) {
            if (i === 8) hexPart += " ";
            const b = bytes[off + i];
            if (b === undefined) {
                hexPart += "   ";
                continue;
            }
            hexPart += b.toString(16).padStart(2, "0") + " ";
            asciiPart += b >= 32 && b < 127 ? String.fromCharCode(b) : ".";
        }
        lines.push(`${off.toString(16).padStart(8, "0")}  ${hexPart} |${asciiPart}|`);
    }
    if (bytes.length > n) lines.push(`… 共 ${bytes.length} 字节，仅显示前 ${n} 字节`);
    return lines.join("\n");
});

/** 二进制体下载：data URL + a[download] */
function download() {
    const name = props.filename || `response.${EXT_BY_MIME[mimeMain.value.toLowerCase()] || "bin"}`;
    const a = document.createElement("a");
    a.href = dataUrl.value;
    a.download = name;
    a.click();
}

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
        /(&lt;!--[\s\S]*?--&gt;)|(&lt;\/?)([\w:.-]+)([\s\S]*?)(\/?(?:&gt;))/g,
        (m: string, comment?: string, open?: string, tag?: string, rest?: string, close?: string) => {
            if (comment) return `<span class="tok-com">${comment}</span>`;
            return `<span class="tok-punc">${open}</span><span class="tok-tag">${tag}</span>${highlightAttrs(rest || "")}<span class="tok-punc">${close}</span>`;
        }
    );
}

/** 标签属性高亮：属性名 / 等号 / 属性值分别着色 */
function highlightAttrs(rest: string): string {
    return rest.replace(
        /([^\s=/>]+)(=)("[^"]*"|'[^']*'|[^\s>]+)/g,
        (m: string, name: string, eq: string, val: string) =>
            `<span class="tok-attr">${name}</span>` +
            `<span class="tok-punc">${eq}</span>` +
            `<span class="tok-str">${val}</span>`
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

/* 代码视图：左侧行号 + 右侧内容，整体横纵向滚动；配色由局部 CSS 变量驱动，浅/暗各一套 */
.bv-code {
    /* 浅色（GitHub Light 取向） */
    --bv-bg: #fbfcfd;
    --bv-gutter-bg: #f2f3f5;
    --bv-gutter-text: #86909c;
    --bv-border: #e5e6eb;
    --bv-text: #24292f;
    --bv-key: #0b61a4;
    --bv-str: #a31515;
    --bv-num: #098658;
    --bv-lit: #6f42c1;
    --bv-tag: #22863a;
    --bv-attr: #6f42c1;
    --bv-punc: #6a737d;
    --bv-com: #8b949e;
    --bv-pad: 8px;
    --bv-line: 1.6;

    flex: 1;
    min-height: 0;
    display: flex;
    overflow: auto;
    background: var(--bv-bg);
    border: 1px solid var(--bv-border);
    border-radius: var(--base-radius-default);
}

/* 暗黑：跟随 Arco 的 body[arco-theme='dark'] 同步切换（VS Code Dark 取向）
   注：祖先选择器不参与作用域，scoped 编译后为 body[arco-theme="dark"] .bv-code[data-v-xxx] */
body[arco-theme="dark"] .bv-code {
    --bv-bg: #17171a;
    --bv-gutter-bg: #1d1d20;
    --bv-gutter-text: #6b7280;
    --bv-border: #2b2b30;
    --bv-text: #e6e6e6;
    --bv-key: #79c0ff;
    --bv-str: #7ec699;
    --bv-num: #f08d49;
    --bv-lit: #cc99cd;
    --bv-tag: #e06c75;
    --bv-attr: #f2c97d;
    --bv-punc: #9aa4b2;
    --bv-com: #7d8590;
}

.bv-gutter {
    flex: none;
    position: sticky;
    left: 0;
    z-index: 1;
    padding: var(--bv-pad) 10px;
    text-align: right;
    color: var(--bv-gutter-text);
    background: var(--bv-gutter-bg);
    border-right: 1px solid var(--bv-border);
    font-family: Consolas, Monaco, "Courier New", monospace;
    font-size: var(--base-size-small);
    line-height: var(--bv-line);
    user-select: none;
}

.bv-pre {
    flex: 1;
    margin: 0;
    padding: var(--bv-pad) 12px;
    color: var(--bv-text);
    font-family: Consolas, Monaco, "Courier New", monospace;
    font-size: var(--base-size-small);
    line-height: var(--bv-line);
    white-space: pre;
    tab-size: 2;
}

/* 语法着色粒度：键/属性名、字符串、数字、字面量、标签名、标点、注释 */
.bv-pre :deep(.tok-key) {
    color: var(--bv-key);
    font-weight: 500;
}

.bv-pre :deep(.tok-str) {
    color: var(--bv-str);
}

.bv-pre :deep(.tok-num) {
    color: var(--bv-num);
}

.bv-pre :deep(.tok-lit) {
    color: var(--bv-lit);
}

.bv-pre :deep(.tok-tag) {
    color: var(--bv-tag);
    font-weight: 500;
}

.bv-pre :deep(.tok-attr) {
    color: var(--bv-attr);
}

.bv-pre :deep(.tok-punc) {
    color: var(--bv-punc);
}

.bv-pre :deep(.tok-com) {
    color: var(--bv-com);
    font-style: italic;
}

.bv-preview {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

/* 二进制预览：图片居中自适应（浅色底衬透明图），视频/音频占满宽度 */
.bv-media {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--base-radius-default);
    padding: var(--base-padding);
    overflow: auto;
}

.bv-media :deep(.arco-empty) {
    margin: auto;
}

.bv-trunc-tip {
    flex: none;
    width: 100%;
}

.bv-img {
    flex: none;
    margin: auto;
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    background: var(--color-fill-2);
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-default);
}

.bv-video {
    width: 100%;
    max-height: 100%;
    margin: auto 0;
    background: #000;
    border-radius: var(--base-radius-default);
}

.bv-audio {
    width: 100%;
    margin: auto 0;
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
