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
                <span class="sp-dim">{{ cur?.rows || 0 }} 行 × {{ cur?.cols || 0 }} 列</span>
                <a-space :size="8" class="sp-actions">
                    <a-tag v-if="canWrite" color="arcoblue" size="small">{{ dirty ? '未保存' : '只读预览' }}</a-tag>
                    <template v-if="canWrite && dirty">
                        <a-button size="mini" @click="discard">放弃修改</a-button>
                        <a-button size="mini" type="primary" :loading="saving" @click="save">保存</a-button>
                    </template>
                </a-space>
            </div>
            <div class="sp-grid">
                <table>
                    <thead>
                        <tr>
                            <th class="sp-rh"></th>
                            <th v-for="c in (cur?.cols || 0)" :key="c" class="sp-ch">{{ colName(c - 1) }}</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr v-for="(row, r) in cur?.data" :key="r">
                            <td class="sp-rh">{{ r + 1 }}</td>
                            <td v-for="c in (cur?.cols || 0)" :key="c" class="sp-cell"
                                :contenteditable="canWrite" :data-r="r" :data-c="c - 1"
                                @blur="onEdit" @keydown.enter.prevent="blockEnter">{{ cellText(row[c - 1]) }}</td>
                        </tr>
                    </tbody>
                </table>
                <div v-if="!cur || cur.rows === 0" class="sp-none">空表</div>
            </div>
        </template>
    </div>
</template>

<script setup lang="ts">
/*
表格预览/编辑（xls/xlsx/csv）：SheetJS(xlsx) 解析成二维数组渲染成可编辑网格。
xlsx 动态 import 成独立 chunk，不进主包。
可编辑仅限本机可写文件（canWrite）：保存时把各表重建为工作簿 → base64 → FileWriteBinary 写回；
远端（sftp 托管）条目 canWrite=false，退回只读预览（写回会落到读不到的本机路径）。
*/
import { computed, getCurrentInstance, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";

const props = defineProps<{ url: string; name: string; path: string; canWrite: boolean }>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

interface SheetModel { name: string; rows: number; cols: number; data: any[][] }

const loading = ref(true);
const err = ref("");
const sheets = ref<SheetModel[]>([]);
const active = ref("0");
const dirty = ref(false);
const saving = ref(false);
let wbRef: any = null; // 保留原 workbook 以便保存时复用未改动的表

const activeIdx = computed(() => {
    const i = Number(active.value);
    return Number.isFinite(i) && i >= 0 && i < sheets.value.length ? i : 0;
});
const cur = computed(() => sheets.value[activeIdx.value]);

function cellText(v: any): string {
    return v === null || v === undefined ? "" : String(v);
}
function colName(i: number): string {
    // 0→A ... 25→Z 26→AA（Excel 列名）
    let s = "", n = i;
    do { s = String.fromCharCode(65 + (n % 26)) + s; n = Math.floor(n / 26) - 1; } while (n >= 0);
    return s;
}
function blockEnter(e: KeyboardEvent) {
    (e.target as HTMLElement).blur();
}

// contenteditable 失焦：把文本写回当前表的数据模型并标脏
function onEdit(e: FocusEvent) {
    if (!props.canWrite) return;
    const td = e.target as HTMLElement;
    const r = Number(td.dataset.r), c = Number(td.dataset.c);
    const s = cur.value;
    if (!s || Number.isNaN(r) || Number.isNaN(c)) return;
    const text = td.textContent ?? "";
    const old = s.data[r]?.[c];
    if (cellText(old) === text) return;
    if (!s.data[r]) s.data[r] = [];
    // 尽量还原数字：纯数字串存数值，避免把金额/编号全变字符串
    s.data[r][c] = text !== "" && /^-?\d+(\.\d+)?$/.test(text) ? Number(text) : text;
    dirty.value = true;
}

function bookType(): string {
    const ext = (props.name.split(".").pop() || "").toLowerCase();
    if (ext === "csv") return "csv";
    if (ext === "xls") return "xls";
    return "xlsx";
}

async function load() {
    if (!props.url) return;
    loading.value = true;
    err.value = "";
    sheets.value = [];
    active.value = "0";
    dirty.value = false;
    try {
        const resp = await fetch(props.url);
        if (!resp.status) throw new Error(`HTTP ${resp.status}`);
        const buf = await resp.arrayBuffer();
        const mod: any = await import("xlsx");
        const XLSX = mod.default ?? mod;
        const wb = XLSX.read(new Uint8Array(buf), { type: "array" });
        wbRef = wb;
        sheets.value = wb.SheetNames.map((name: string) => {
            const ws: any = wb.Sheets[name];
            const data: any[][] = XLSX.utils.sheet_to_json(ws, { header: 1, raw: true, defval: "" });
            let cols = 0;
            for (const row of data) cols = Math.max(cols, row.length);
            return { name, rows: data.length, cols, data };
        });
        if (!sheets.value.length) err.value = "没有可显示的工作表";
    } catch (e: any) {
        err.value = "表格解析失败：" + String(e?.message || e);
    } finally {
        loading.value = false;
    }
}

function discard() {
    void load();
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

async function save() {
    if (!props.canWrite || !props.path) return;
    saving.value = true;
    try {
        const mod: any = await import("xlsx");
        const XLSX = mod.default ?? mod;
        const nb = XLSX.utils.book_new();
        for (const s of sheets.value) {
            XLSX.utils.book_append_sheet(nb, XLSX.utils.aoa_to_sheet(s.data), s.name || "Sheet1");
        }
        const out: ArrayBuffer = XLSX.write(nb, { type: "array", bookType: bookType() as any });
        await GoTools.FileWriteBinary(props.path, bufToBase64(out));
        dirty.value = false;
        Message.success("已保存");
    } catch (e: any) {
        Message.error("保存失败：" + String(e?.message || e));
    } finally {
        saving.value = false;
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
.sp-actions {
    flex: none;
    margin-left: auto;
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
.sp-grid table {
    border-collapse: collapse;
    font-size: var(--base-size-13);
}
.sp-rh,
.sp-ch {
    background: var(--color-fill-2);
    color: var(--color-text-3);
    font-weight: 600;
    text-align: center;
    position: sticky;
    z-index: 1;
    min-width: 40px;
    padding: 3px 8px;
    border: 1px solid var(--color-border-2);
}
.sp-ch {
    top: 0;
}
.sp-rh {
    left: 0;
}
.sp-cell {
    border: 1px solid var(--color-border-2);
    padding: 3px 8px;
    white-space: nowrap;
    max-width: 360px;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--color-text-1);
    min-width: 60px;
}
.sp-cell[contenteditable="true"]:focus {
    outline: 2px solid rgb(var(--primary-6));
    outline-offset: -2px;
    white-space: normal;
}
.sp-none {
    padding: var(--base-size-24);
    text-align: center;
    color: var(--color-text-3);
}
</style>
