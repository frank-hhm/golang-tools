<template>
    <a-row class="enc-page" :gutter="6">
        <!-- 左侧约 25%：输入（上50%）+ 操作列表（下50%） -->
        <a-col :span="6" class="side-col">
            <a-row class="side-pages">
                <a-col class="side-cols" :span="24">
                    <a-card class="side-card" size="small" :body-style="{ padding: 0 }">
                        <template #title>输入文本</template>
                        <template #extra>
                            <a-tag size="small">{{ input.length }} 字符</a-tag>
                        </template>
                        <div class="panel-body">
                            <a-textarea v-model="input" class="fill-textarea" placeholder="请输入待处理的文字"
                                allow-clear />
                        </div>
                    </a-card>
                </a-col>
                <a-col class="side-cols mt6" :span="24">
                    <a-card class="side-card" size="small" :body-style="{ padding: 0 }">
                        <template #title>操作</template>
                        <template #extra>
                            <a-tag size="small">{{ operations.length }} 种</a-tag>
                        </template>
                        <div class="op-list">
                            <a-button v-for="op in operations" :key="op.label" size="small" long
                                @click="run(op.fn)">
                                {{ op.label }}
                            </a-button>
                        </div>
                    </a-card>
                </a-col>
            </a-row>
        </a-col>

        <!-- 右侧：转换结果 -->
        <a-col :span="18" class="main-col">
            <a-card class="main-panel" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space :size="8">
                        <span>转换结果</span>
                        <a-tag size="small">{{ output.length }} 字符</a-tag>
                    </a-space>
                </template>
                <template #extra>
                    <a-space :size="8">
                        <a-button size="small" :disabled="!output" @click="useAsInput">
                            <template #icon><icon-swap /></template>作为输入
                        </a-button>
                        <a-button size="small" :disabled="!output" @click="copyText(output)">
                            <template #icon><icon-copy /></template>复制
                        </a-button>
                        <a-button size="small" status="danger" :disabled="!output" @click="output = ''">
                            <template #icon><icon-delete /></template>清空
                        </a-button>
                    </a-space>
                </template>
                <div class="panel-body">
                    <a-textarea v-model="output" class="fill-textarea" placeholder="结果" readonly />
                </div>
            </a-card>
        </a-col>
    </a-row>
</template>

<script setup lang="ts">
import { ref } from "vue";
import { Message } from "@arco-design/web-vue";

const input = ref("");
const output = ref("");

/* ============ 复制到剪贴板 ============ */
const copyText = async (text: string) => {
    if (!text) return;
    try {
        await navigator.clipboard.writeText(text);
        Message.success("已复制到剪贴板");
    } catch {
        const ta = document.createElement("textarea");
        ta.value = text;
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        document.body.removeChild(ta);
        Message.success("已复制到剪贴板");
    }
};

/* ============ GBK 编码反查表（懒建，仅一次） ============ */
let gbkEncodeMap: Map<number, number[]> | null = null;
function getGbkMap(): Map<number, number[]> {
    if (gbkEncodeMap) return gbkEncodeMap;
    const map = new Map<number, number[]>();
    try {
        const dec = new TextDecoder("gbk");
        for (let hi = 0x81; hi <= 0xfe; hi++) {
            for (let lo = 0x40; lo <= 0xfe; lo++) {
                if (lo === 0x7f) continue;
                const s = dec.decode(new Uint8Array([hi, lo]));
                if (s.length === 1) {
                    const code = s.charCodeAt(0);
                    if (code !== 0xfffd && !map.has(code)) map.set(code, [hi, lo]);
                }
            }
        }
    } catch {
        /* 环境不支持 gbk 时返回空表，相关功能降级 */
    }
    gbkEncodeMap = map;
    return map;
}

/* ============ 基础编码工具函数 ============ */
function toUtf8Bytes(s: string): Uint8Array {
    return new TextEncoder().encode(s);
}

function base64Encode(s: string): string {
    const bytes = toUtf8Bytes(s);
    let bin = "";
    for (const b of bytes) bin += String.fromCharCode(b);
    return btoa(bin);
}

function base64Decode(s: string): string {
    const bin = atob(s.trim().replace(/\s+/g, ""));
    const bytes = Uint8Array.from(bin, (c) => c.charCodeAt(0));
    return new TextDecoder().decode(bytes);
}

function urlEncodeGbk(s: string): string {
    const map = getGbkMap();
    let out = "";
    for (const ch of s) {
        const cp = ch.codePointAt(0)!;
        if (cp < 0x80) {
            out += encodeURIComponent(ch);
            continue;
        }
        const bytes = map.get(cp);
        if (bytes) {
            for (const b of bytes) out += "%" + b.toString(16).toUpperCase().padStart(2, "0");
        } else {
            out += encodeURIComponent(ch);
        }
    }
    return out;
}

function urlDecodeGbk(s: string): string {
    const bytes: number[] = [];
    for (let i = 0; i < s.length; i++) {
        const c = s[i];
        if (c === "%" && i + 2 < s.length && /^[0-9a-fA-F]{2}$/.test(s.substr(i + 1, 2))) {
            bytes.push(parseInt(s.substr(i + 1, 2), 16));
            i += 2;
            continue;
        }
        if (c === "+") {
            bytes.push(0x20);
            continue;
        }
        const code = s.charCodeAt(i);
        if (code < 0x80) {
            bytes.push(code);
        } else {
            for (const b of toUtf8Bytes(c)) bytes.push(b);
        }
    }
    try {
        return new TextDecoder("gbk").decode(Uint8Array.from(bytes));
    } catch {
        return new TextDecoder().decode(Uint8Array.from(bytes));
    }
}

function addSlash(s: string): string {
    return s.replace(/['"\\]/g, (m) => "\\" + m);
}

function stripSlash(s: string): string {
    return s.replace(/\\(['"\\])/g, "$1");
}

function encodeHtml(s: string): string {
    const map: Record<string, string> = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };
    return s.replace(/[&<>"']/g, (c) => map[c]);
}

function decodeHtml(s: string): string {
    const map: Record<string, string> = {
        "&amp;": "&", "&lt;": "<", "&gt;": ">", "&quot;": '"', "&#39;": "'", "&apos;": "'",
    };
    return s.replace(/&(?:amp|lt|gt|quot|#39|apos);/g, (m) => map[m] ?? m);
}

/* ============ MD5（纯 TS，UTF-8 字节） ============ */
function md5(str: string): string {
    const K = new Int32Array(64);
    for (let i = 0; i < 64; i++) K[i] = Math.floor(Math.abs(Math.sin(i + 1)) * 4294967296);
    const S = [
        7, 12, 17, 22, 7, 12, 17, 22, 7, 12, 17, 22, 7, 12, 17, 22,
        5, 9, 14, 20, 5, 9, 14, 20, 5, 9, 14, 20, 5, 9, 14, 20,
        4, 11, 16, 23, 4, 11, 16, 23, 4, 11, 16, 23, 4, 11, 16, 23,
        6, 10, 15, 21, 6, 10, 15, 21, 6, 10, 15, 21, 6, 10, 15, 21,
    ];
    const bytes = toUtf8Bytes(str);
    const len = bytes.length;
    const bitLen = len * 8;
    const total = Math.ceil((len + 9) / 64) * 64;
    const msg = new Uint8Array(total);
    msg.set(bytes);
    msg[len] = 0x80;
    const dv = new DataView(msg.buffer);
    dv.setUint32(total - 8, bitLen >>> 0, true);
    dv.setUint32(total - 4, Math.floor(bitLen / 4294967296), true);
    let a0 = 0x67452301, b0 = 0xefcdab89, c0 = 0x98badcfe, d0 = 0x10325476;
    const M = new Int32Array(16);
    for (let off = 0; off < total; off += 64) {
        for (let i = 0; i < 16; i++) M[i] = dv.getInt32(off + i * 4, true);
        let A = a0, B = b0, C = c0, D = d0;
        for (let i = 0; i < 64; i++) {
            let F, g;
            if (i < 16) { F = (B & C) | (~B & D); g = i; }
            else if (i < 32) { F = (D & B) | (~D & C); g = (5 * i + 1) % 16; }
            else if (i < 48) { F = B ^ C ^ D; g = (3 * i + 5) % 16; }
            else { F = C ^ (B | ~D); g = (7 * i) % 16; }
            F = (F + A + K[i] + M[g]) | 0;
            A = D; D = C; C = B;
            B = (B + ((F << S[i]) | (F >>> (32 - S[i])))) | 0;
        }
        a0 = (a0 + A) | 0; b0 = (b0 + B) | 0; c0 = (c0 + C) | 0; d0 = (d0 + D) | 0;
    }
    const out = new Uint8Array(16);
    const odv = new DataView(out.buffer);
    odv.setInt32(0, a0, true); odv.setInt32(4, b0, true); odv.setInt32(8, c0, true); odv.setInt32(12, d0, true);
    return Array.from(out).map((b) => b.toString(16).padStart(2, "0")).join("");
}

/* ============ 操作定义（顺序与图示一致） ============ */
interface Operation { label: string; fn: (s: string) => string; }

const operations: Operation[] = [
    { label: "Base64 Encode", fn: base64Encode },
    { label: "Base64 Decode", fn: base64Decode },
    { label: "Url Encode (utf-8)", fn: (s) => encodeURIComponent(s) },
    { label: "Url Decode (utf-8)", fn: (s) => decodeURIComponent(s) },
    { label: "Url Encode (gbk)", fn: urlEncodeGbk },
    { label: "Url Decode (gbk)", fn: urlDecodeGbk },
    { label: "Add Slash", fn: addSlash },
    { label: "Strip Slash", fn: stripSlash },
    { label: "Encode Html Special Chars", fn: encodeHtml },
    { label: "Decode Html Special Chars", fn: decodeHtml },
    { label: "MD5", fn: md5 },
];

const run = (fn: (s: string) => string) => {
    if (!input.value) {
        Message.warning("请先输入文本");
        return;
    }
    try {
        output.value = fn(input.value);
    } catch {
        output.value = "";
        Message.error("转换失败，请检查输入是否合法");
    }
};

const useAsInput = () => {
    input.value = output.value;
    output.value = "";
};
</script>

<style scoped>
/* 页面根容器：a-row 铺满工具窗口内容区 */
.enc-page {
    height: 100%;
    min-height: 0;
    flex-wrap: nowrap;
    background: var(--color-fill-1);
}

/* 左侧列：上下两张卡片各占 50% 高 */
.side-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.side-pages {
    height: calc(100% - 6px);
}

.side-cols {
    height: 50%;
    width: 100%;
    display: flex;
    flex-direction: column;
}

.side-card {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.side-card :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

/* 输入/结果文本域：填满卡片并内部滚动 */
.panel-body {
    flex: 1;
    min-height: 0;
    display: flex;
    padding: var(--base-padding);
}

.fill-textarea {
    flex: 1;
    min-height: 0;
    height: 100%;
}

.fill-textarea :deep(.arco-textarea) {
    height: 100%;
    resize: none;
    font-family: Consolas, Monaco, "Courier New", monospace;
    font-size: var(--base-size-small);
}

/* 操作按钮列表 */
.op-list {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: var(--base-padding);
    overflow-y: auto;
}

/* 右侧主列 */
.main-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.main-panel {
    width: 100%;
    height: 100%;
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    background: var(--color-bg-2);
}

.main-panel :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}
</style>
