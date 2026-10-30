<template>
    <div class="sheet-pane">
        <div v-if="loading" class="sp-state"><a-spin /><span>解析中…</span></div>
        <div v-else-if="err" class="sp-state sp-err">{{ err }}</div>
        <template v-else>
            <div class="sp-bar">
                <a-tabs v-if="sheets.length > 1" :active-key="active" size="mini" @change="(k: any) => (active = String(k))"
                    class="sp-tabs">
                    <a-tab-pane v-for="(s, i) in sheets" :key="String(i)" :title="s.name" />
                </a-tabs>
                <span v-else-if="sheets.length === 1" class="sp-single">{{ sheets[0].name }}</span>
                <span class="sp-dim">{{ sheets[activeIdx]?.rows }} 行 × {{ sheets[activeIdx]?.cols }} 列</span>
            </div>
            <div class="sp-grid" v-html="sheets[activeIdx]?.html || '<div class=\'sp-none\'>空表</div>'"></div>
        </template>
    </div>
</template>

<script setup lang="ts">
/*
表格预览（xls/xlsx/csv）：SheetJS(xlsx) 解析 → sheet_to_html 渲染成只读表格。
xlsx 动态 import 成独立 chunk，不进主包。只读预览，不回写（编辑表格超出查看器定位）。
*/
import { computed, ref, watch } from "vue";

const props = defineProps<{ url: string; name: string }>();

interface SheetView { name: string; html: string; rows: number; cols: number }

const loading = ref(true);
const err = ref("");
const sheets = ref<SheetView[]>([]);
const active = ref("0");

const activeIdx = computed(() => {
    const i = Number(active.value);
    return Number.isFinite(i) && i >= 0 && i < sheets.value.length ? i : 0;
});

async function load() {
    if (!props.url) return;
    loading.value = true;
    err.value = "";
    sheets.value = [];
    active.value = "0";
    try {
        const resp = await fetch(props.url);
        if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
        const buf = await resp.arrayBuffer();
        const mod: any = await import("xlsx");
        const XLSX = mod.default ?? mod; // xlsx 是 CJS/UMD，命名导出与 default 都可能有，取齐
        const wb = XLSX.read(new Uint8Array(buf), { type: "array" });
        sheets.value = wb.SheetNames.map((name: string) => {
            const ws: any = wb.Sheets[name];
            const ref: string = ws?.["!ref"] || "";
            let rows = 0, cols = 0;
            if (ref) {
                const range = XLSX.utils.decode_range(ref);
                rows = range.e.r - range.s.r + 1;
                cols = range.e.c - range.s.c + 1;
            }
            return { name, rows, cols, html: XLSX.utils.sheet_to_html(ws, { header: "", footer: "" }) };
        });
        if (!sheets.value.length) err.value = "没有可显示的工作表";
    } catch (e: any) {
        err.value = "表格解析失败：" + String(e?.message || e);
    } finally {
        loading.value = false;
    }
}

watch(() => props.url, load, { immediate: true });
</script>

<style scoped>
.sheet-pane {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}
.sp-bar {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--base-size-12);
    padding: 4px 10px;
    border-bottom: 1px solid var(--color-border-2);
}
.sp-tabs {
    flex: 1;
    min-width: 0;
}
.sp-single {
    font-size: var(--base-size-14);
    color: var(--color-text-1);
    font-weight: 600;
}
.sp-dim {
    flex: none;
    font-size: var(--base-size-12);
    color: var(--color-text-3);
}
.sp-state {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 10px;
    color: var(--color-text-3);
}
.sp-err {
    color: rgb(var(--danger-6));
}
.sp-grid {
    flex: 1;
    min-height: 0;
    overflow: auto;
}
/* v-html 注入的原生 table 不受 scoped 约束，用 :deep 统一样式 */
.sp-grid :deep(table) {
    border-collapse: collapse;
    font-size: var(--base-size-13);
}
.sp-grid :deep(th),
.sp-grid :deep(td) {
    border: 1px solid var(--color-border-2);
    padding: 3px 8px;
    white-space: nowrap;
    max-width: 360px;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--color-text-1);
}
.sp-grid :deep(th) {
    background: var(--color-fill-2);
    position: sticky;
    top: 0;
    z-index: 1;
}
.sp-grid :deep(.sp-none) {
    padding: var(--base-size-24);
    text-align: center;
    color: var(--color-text-3);
}
</style>
