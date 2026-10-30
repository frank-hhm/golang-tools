<template>
    <div class="bin-pane">
        <div v-if="loading" class="bp-state"><a-spin /><span>读取中…</span></div>
        <div v-else-if="err" class="bp-state bp-err">{{ err }}</div>
        <template v-else>
            <div class="bp-bar">
                <a-tag size="small" color="gray">HEX</a-tag>
                <span class="bp-dim">
                    显示前 {{ fmt(shown) }}<template v-if="truncated"> / 共 {{ fmt(size) }}（已截断）</template>
                </span>
            </div>
            <pre class="bp-hex">{{ dump }}</pre>
        </template>
    </div>
</template>

<script setup lang="ts">
/*
二进制/未知类型：只读 HEX 视图。用 Range 请求只取文件头一段（256KB 封顶），
不把整个文件灌进内存；大文件一律走右上角「下载」。不解析、不回写。
*/
import { ref, watch } from "vue";

const props = defineProps<{ url: string; name: string; size: number }>();

const CAP = 256 * 1024;

const loading = ref(true);
const err = ref("");
const dump = ref("");
const shown = ref(0);
const truncated = ref(false);

function fmt(n: number): string {
    if (!n) return "0 B";
    const u = ["B", "KB", "MB", "GB", "TB"];
    let i = 0, v = n;
    while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
    return (i === 0 ? v : v.toFixed(1)) + " " + u[i];
}

const hex2 = (b: number) => b.toString(16).padStart(2, "0");

async function load() {
    if (!props.url) return;
    loading.value = true;
    err.value = "";
    dump.value = "";
    shown.value = 0;
    truncated.value = false;
    try {
        const resp = await fetch(props.url, { headers: { Range: `bytes=0-${CAP - 1}` } });
        if (!resp.ok && resp.status !== 206) throw new Error(`HTTP ${resp.status}`);
        const buf = new Uint8Array(await resp.arrayBuffer());
        shown.value = buf.length;
        truncated.value = props.size > buf.length;
        const lines: string[] = [];
        for (let off = 0; off < buf.length; off += 16) {
            const slice = buf.subarray(off, Math.min(off + 16, buf.length));
            let hex = "";
            let ascii = "";
            for (let i = 0; i < 16; i++) {
                if (i < slice.length) {
                    hex += hex2(slice[i]) + " ";
                    const c = slice[i];
                    ascii += c >= 0x20 && c <= 0x7e ? String.fromCharCode(c) : ".";
                } else {
                    hex += "   ";
                }
                if (i === 7) hex += " ";
            }
            lines.push(`${off.toString(16).padStart(8, "0")}  ${hex} |${ascii}|`);
        }
        dump.value = lines.join("\n");
    } catch (e: any) {
        err.value = "读取失败：" + String(e?.message || e);
    } finally {
        loading.value = false;
    }
}

watch(() => props.url, load, { immediate: true });
</script>

<style scoped>
.bin-pane {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}
.bp-bar {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--base-size-8);
    padding: 6px 10px;
    border-bottom: 1px solid var(--color-border-2);
}
.bp-dim {
    font-size: var(--base-size-12);
    color: var(--color-text-3);
}
.bp-state {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    color: var(--color-text-3);
}
.bp-err {
    color: rgb(var(--danger-6));
}
.bp-hex {
    flex: 1;
    min-height: 0;
    overflow: auto;
    margin: 0;
    padding: var(--base-size-12);
    font-family: var(--code-font-family, Consolas, "Courier New", monospace);
    font-size: 12px;
    line-height: 1.6;
    color: var(--color-text-1);
    white-space: pre;
    background: var(--color-fill-1);
}
</style>
