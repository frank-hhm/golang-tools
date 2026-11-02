<template>
    <div class="fp">
        <div class="fp-bar">
            <a-tooltip content="返回上一级">
                <a-button size="mini" :disabled="!parent" @click="enter(parent)">
                    <template #icon><icon-arrow-up /></template>
                </a-button>
            </a-tooltip>
            <a-tooltip content="回到用户主目录">
                <a-button size="mini" @click="home">
                    <template #icon><icon-home /></template>
                </a-button>
            </a-tooltip>
            <a-input v-model="addr" size="mini" class="fp-addr" placeholder="输入目录，回车跳转" @press-enter="enter(addr)">
                <template #prefix><icon-search /></template>
            </a-input>
            <a-tooltip content="刷新">
                <a-button size="mini" :loading="loading" @click="reload">
                    <template #icon><icon-refresh /></template>
                </a-button>
            </a-tooltip>
            <a-button v-if="pickDir" size="mini" type="outline" @click="chooseHere">选此目录</a-button>
        </div>

        <div class="fp-list">
            <div v-if="loading" class="fp-state"><a-spin /></div>
            <div v-else-if="error" class="fp-state fp-err">{{ error }}</div>
            <div v-else-if="!rows.length" class="fp-state">此目录下没有可选文件</div>
            <template v-else>
                <div v-for="r in rows" :key="r.path" class="fp-row">
                    <template v-if="r.isDir">
                        <div class="fp-item" @click="enter(r.path)">
                            <common-file-icon :is-dir="true" :size="16" />
                            <span class="fp-name" :title="r.path">{{ r.name }}</span>
                            <icon-right />
                        </div>
                    </template>
                    <template v-else>
                        <div class="fp-item" :class="{ on: isOn(r.path) }" @click="toggle(r)">
                            <a-checkbox v-if="multiple" :model-value="isOn(r.path)" @click.stop @change="toggle(r)" />
                            <common-file-icon :is-dir="false" :ext="r.ext" :size="16" />
                            <span class="fp-name" :title="r.path">{{ r.name }}</span>
                            <span class="fp-size">{{ fmtBytes(r.size || 0) }}</span>
                            <a-button v-if="!multiple" size="mini" type="text" @click.stop="toggle(r)">选</a-button>
                        </div>
                    </template>
                </div>
            </template>
        </div>

        <div v-if="multiple && picked.length" class="fp-picked">
            <div class="fp-picked-head">
                <span>已选 {{ picked.length }} 个（按此顺序处理）</span>
                <a-link size="mini" @click="clearAll">清空</a-link>
            </div>
            <div v-for="(p, i) in picked" :key="p" class="fp-chip">
                <span class="fp-chip-name" :title="p">{{ baseName(p) }}</span>
                <a-button size="mini" type="text" :disabled="i === 0" @click="move(i, -1)">
                    <template #icon><icon-arrow-up /></template>
                </a-button>
                <a-button size="mini" type="text" :disabled="i === picked.length - 1" @click="move(i, 1)">
                    <template #icon><icon-arrow-down /></template>
                </a-button>
                <a-button size="mini" type="text" status="danger" @click="removeAt(i)">
                    <template #icon><icon-close /></template>
                </a-button>
            </div>
        </div>
        <div v-else-if="!multiple && picked.length" class="fp-picked">
            <div class="fp-chip">
                <span class="fp-chip-name" :title="picked[0]">{{ picked[0] }}</span>
                <a-button size="mini" type="text" status="danger" @click="clearAll">
                    <template #icon><icon-close /></template>
                </a-button>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
/*
编辑弹窗共用的「本机文件挑选器」：左下浏览 DiskList，按 accept 扩展名过滤文件，
多选时给出有序已选列表（拼接/合并的顺序就是它），pickDir 模式则把当前目录交回宿主。
批量处理只认本机路径（远端/云盘文件请先取回本机），这点与查看器左栏一致。
*/
import { getCurrentInstance, ref, watch } from "vue";
import { fmtBytes, baseName } from "./editx";

const props = withDefaults(defineProps<{
    /** 已选文件路径（v-model） */
    paths?: string[];
    /** 允许的扩展名（小写、不含点）；空表示不限 */
    accept?: string[];
    /** 多选 / 单选 */
    multiple?: boolean;
    /** 目录选择模式：点目录进入，「选此目录」把当前目录交回宿主 */
    pickDir?: boolean;
    /** 初始定位目录（一般传当前选中文件所在目录） */
    startDir?: string;
}>(), {
    paths: () => [],
    accept: () => [],
    multiple: true,
    pickDir: false,
    startDir: "",
});

const emit = defineEmits<{
    (e: "update:paths", v: string[]): void;
    (e: "dir", v: string): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const addr = ref("");
const parent = ref("");
const rows = ref<any[]>([]);
const loading = ref(false);
const error = ref("");
const picked = ref<string[]>([...props.paths]);

watch(() => props.paths, (v) => { if (v.join("\n") !== picked.value.join("\n")) picked.value = [...v]; });
watch(picked, (v) => emit("update:paths", [...v]), { deep: true });

function isOn(p: string) { return picked.value.includes(p); }

function acceptExt(r: any): boolean {
    if (!props.accept.length) return true;
    return props.accept.includes(String(r.ext || "").toLowerCase());
}

async function enter(path: string) {
    if (!path) return;
    loading.value = true;
    error.value = "";
    try {
        const res: any = await GoTools.DiskList(path);
        parent.value = res?.parent || "";
        const all: any[] = res?.entries || [];
        rows.value = [
            ...all.filter((e) => e.isDir).sort((a, b) => String(a.name).localeCompare(String(b.name))),
            ...all.filter((e) => !e.isDir && acceptExt(e)).sort((a, b) => String(a.name).localeCompare(String(b.name))),
        ];
        addr.value = res?.path || path;
        emit("dir", addr.value);
    } catch (e: any) {
        error.value = String(e?.message || e || "读取目录失败");
        rows.value = [];
    } finally {
        loading.value = false;
    }
}

async function home() {
    try { enter(String(await GoTools.DiskHome()) || ""); } catch { /* */ }
}

function reload() { enter(addr.value); }

function toggle(r: any) {
    if (props.multiple) {
        picked.value = isOn(r.path) ? picked.value.filter((p) => p !== r.path) : [...picked.value, r.path];
        return;
    }
    picked.value = isOn(r.path) ? [] : [r.path];
}

function removeAt(i: number) {
    const next = [...picked.value];
    next.splice(i, 1);
    picked.value = next;
}

function move(i: number, d: number) {
    const j = i + d;
    if (j < 0 || j >= picked.value.length) return;
    const next = [...picked.value];
    [next[i], next[j]] = [next[j], next[i]];
    picked.value = next;
}

function clearAll() { picked.value = []; }

function chooseHere() { if (addr.value) emit("dir", addr.value); }

watch(() => props.startDir, (d) => { if (d && !addr.value) enter(d); }, { immediate: true });
watch(() => props.pickDir, (v) => { if (v && !addr.value) void home(); });
</script>

<style scoped>
.fp {
    display: flex;
    flex-direction: column;
    gap: 6px;
    min-height: 0;
}
.fp-bar {
    display: flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
}
.fp-addr {
    flex: 1;
    min-width: 0;
}
.fp-list {
    height: 240px;
    overflow: auto;
    border: 1px solid var(--color-border-2);
    border-radius: 6px;
    background: var(--color-bg-1);
    padding: 2px;
}
.fp-state {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    color: var(--color-text-3);
}
.fp-err {
    color: rgb(var(--danger-6));
    padding: 0 10px;
    text-align: center;
}
.fp-item {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 3px 6px;
    border-radius: 4px;
    cursor: pointer;
    min-width: 0;
}
.fp-item:hover {
    background: var(--color-fill-2);
}
.fp-item.on {
    background: var(--color-primary-light-1);
}
.fp-name {
    flex: 1;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}
.fp-size {
    flex: none;
    color: var(--color-text-3);
    font-size: var(--base-size-12);
}
.fp-picked {
    border: 1px dashed var(--color-border-3);
    border-radius: 6px;
    padding: 4px 6px;
    max-height: 140px;
    overflow: auto;
}
.fp-picked-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    color: var(--color-text-3);
    font-size: var(--base-size-12);
}
.fp-chip {
    display: flex;
    align-items: center;
    gap: 2px;
}
.fp-chip-name {
    flex: 1;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-size: var(--base-size-12);
}
</style>
