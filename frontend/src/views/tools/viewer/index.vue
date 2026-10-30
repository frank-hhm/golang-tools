<template>
    <div class="viewer-page">
        <!-- ==================== 顶部路径栏 ==================== -->
        <a-card class="vp-bar card-head-tool" size="small" :body-style="{ padding: '8px 12px' }">
            <div class="vp-path">
                <a-tooltip content="返回上一级">
                    <a-button size="small" :disabled="!listing.parent" @click="goUp">
                        <template #icon><icon-arrow-up /></template>
                    </a-button>
                </a-tooltip>
                <a-tooltip content="回到用户主目录">
                    <a-button size="small" @click="goHome">
                        <template #icon><icon-home /></template>
                    </a-button>
                </a-tooltip>
                <a-breadcrumb class="crumbs">
                    <a-breadcrumb-item v-for="(p, i) in listing.parts" :key="p.path">
                        <a-tooltip :content="p.path" :disabled="i === listing.parts.length - 1">
                            <span class="crumb" :class="{ cur: i === listing.parts.length - 1 }" @click="enter(p.path)">
                                {{ p.name }}
                            </span>
                        </a-tooltip>
                    </a-breadcrumb-item>
                </a-breadcrumb>
                <a-input v-model="addrInput" size="small" class="vp-addr" placeholder="粘贴或输入路径，回车跳转"
                    @press-enter="enter(addrInput)">
                    <template #prefix><icon-search /></template>
                </a-input>
                <a-tooltip content="刷新当前目录">
                    <a-button size="small" :loading="loading" @click="reload">
                        <template #icon><icon-refresh /></template>
                    </a-button>
                </a-tooltip>
            </div>
        </a-card>

        <!-- ==================== 主体：左浏览 / 右预览 ==================== -->
        <div class="vp-body">
            <div class="vp-side">
                <div v-if="loading" class="vp-side-state"><a-spin /></div>
                <div v-else-if="loadError" class="vp-side-state vp-err">{{ loadError }}</div>
                <div v-else-if="!listing.entries || listing.entries.length === 0" class="vp-side-state">空目录</div>
                <div v-else class="vp-list">
                    <div v-for="r in listing.entries" :key="r.path" class="vp-item"
                        :class="{ active: selected && selected.path === r.path }" @click="onEntry(r, $event)">
                        <common-file-icon :is-dir="r.isDir" :ext="r.ext" :size="18" />
                        <span class="vp-item-name" :title="r.name">{{ r.name }}</span>
                        <span class="vp-item-meta">{{ r.isDir ? '' : fmtSize(r.size) }}</span>
                    </div>
                </div>
            </div>

            <div class="vp-main">
                <!-- 已选中可编辑文本/代码：CodePane 自带文件栏 -->
                <code-pane v-if="selected && (kind === 'text' || kind === 'code')" :key="selected.path"
                    :path="selected.path" />

                <!-- 富类型：共享文件栏（名称 + 系统打开 / 下载）+ 对应 pane -->
                <template v-else-if="selected">
                    <div class="vp-file-bar">
                        <div class="vp-fb-file">
                            <common-file-icon :is-dir="false" :ext="selected.ext" :size="18" />
                            <span class="vp-fb-name" :title="selected.path">{{ selected.name }}</span>
                            <span class="vp-fb-size">{{ fmtSize(selected.size) }}</span>
                        </div>
                        <a-space :size="8">
                            <a-button v-if="canOpenSystem" size="mini" @click="openSystem">
                                <template #icon><icon-launch /></template>系统程序打开
                            </a-button>
                            <a-button size="mini" @click="download">
                                <template #icon><icon-download /></template>下载
                            </a-button>
                        </a-space>
                    </div>
                    <image-pane v-if="kind === 'image'" :url="mediaUrl" :name="selected.name" />
                    <pdf-pane v-else-if="kind === 'pdf'" :url="mediaUrl" :name="selected.name" />
                    <sheet-pane v-else-if="kind === 'sheet'" :url="mediaUrl" :name="selected.name" />
                    <media-pane v-else-if="kind === 'audio' || kind === 'video'" :url="mediaUrl"
                        :name="selected.name" :kind="kind" />
                    <binary-pane v-else :url="mediaUrl" :name="selected.name" :size="selected.size || 0" />
                </template>

                <div v-else class="vp-empty">
                    <a-empty description="从左侧选择一个文件查看" />
                </div>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, onUnmounted, reactive, ref } from "vue";
import { useRoute } from "vue-router";
import { Message } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import CodePane from "./components/CodePane.vue";
import ImagePane from "./components/ImagePane.vue";
import PdfPane from "./components/PdfPane.vue";
import SheetPane from "./components/SheetPane.vue";
import MediaPane from "./components/MediaPane.vue";
import BinaryPane from "./components/BinaryPane.vue";
import { useCaps } from "@/utils/caps";

/*
文件查看器工具页（/tools/viewer）

左栏复用磁盘管理的 DiskList 浏览本机文件，右栏按类型分发：
- 文本/代码 → CodePane（CodeMirror6 编辑 + 保存）
- 图片/PDF/表格/音视频/二进制 → 各自只读预览 pane（统一走 /fileview/media 流式端点）

后端能力在 tools/fileview：读文本/写文本/读二进制/写二进制/Range 流式端点。
作为“默认打开方式”被磁盘/FTP 本地列表调用时：新窗口靠 ?path= 定位，
单例窗口已开着时靠 viewer:open-path 事件导航到新文件。
*/

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const caps = useCaps();
const route = useRoute();

const listing = reactive<any>({ path: "", parent: "", parts: [], entries: [] });
const addrInput = ref("");
const loading = ref(false);
const loadError = ref("");
const selected = ref<any>(null);
const kind = ref("");

// 系统程序打开：仅 Windows 桌面构建有可靠外壳；Linux 面板多半无图形界面，按能力隐藏
const canOpenSystem = computed(() => !!caps.nativeWindows);

// 富类型预览统一指这个流式端点（图片/PDF/音视频/表格/HEX 都同源加载，不经 base64）
const mediaUrl = computed(() => {
    if (!selected.value) return "";
    const p = encodeURIComponent(selected.value.path || "");
    const n = encodeURIComponent(selected.value.name || "");
    return `/fileview/media?path=${p}&name=${n}`;
});

function fmtSize(n: number): string {
    if (!n) return "";
    const u = ["B", "KB", "MB", "GB", "TB"];
    let i = 0, v = n;
    while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
    return (i === 0 ? v : v.toFixed(1)) + " " + u[i];
}

async function enter(path: string) {
    if (!path) return;
    loading.value = true;
    loadError.value = "";
    try {
        const res: any = await GoTools.DiskList(path);
        Object.assign(listing, res || {});
        addrInput.value = listing.path || "";
        // 切目录后清掉右栏选中（属于上一个目录）
        if (selected.value && !String(selected.value.path).startsWith(String(listing.path))) {
            selected.value = null;
            kind.value = "";
        }
    } catch (e: any) {
        loadError.value = String(e?.message || e || "读取目录失败");
    } finally {
        loading.value = false;
    }
}

function goUp() { if (listing.parent) enter(listing.parent); }
function reload() { enter(listing.path || addrInput.value); }

async function goHome() {
    const h = await GoTools.DiskHome();
    enter(h);
}

async function onEntry(r: any, ev: MouseEvent) {
    if (r.isDir) { enter(r.path); return; }
    ev.stopPropagation();
    selected.value = r;
    kind.value = "";
    try {
        const res = await GoTools.FileClassify(r.path);
        kind.value = Array.isArray(res) ? res[0] : (res?.kind || "");
    } catch {
        // 判定失败退回按扩展名粗判，至少文本/代码能进编辑器
        kind.value = ["txt", "md", "log", "ini", "conf", "env"].includes(r.ext) ? "text" : "";
    }
}

async function openSystem() {
    if (!selected.value) return;
    try {
        await GoTools.FileOpenSystem(selected.value.path);
    } catch (e: any) {
        Message.warning(String(e?.message || e || "打开失败"));
    }
}

function download() {
    if (!selected.value) return;
    const p = encodeURIComponent(selected.value.path);
    const n = encodeURIComponent(selected.value.name || "");
    window.open(`/fileview/media?path=${p}&name=${n}&dl=1`, "_blank");
}

// 定位到一个路径：目录则进入，文件先进其所在目录再选中。供初次挂载与事件复用共用。
async function openPath(q: string) {
    if (!q) return;
    // 传进来的可能是文件：先进其所在目录，再选中它
    const isFile = /[\\/][^\\/]+\.[^\\/]+$/.test(q);
    if (isFile) {
        const dir = q.replace(/[\\/][^\\/]+$/, "");
        await enter(dir || q);
        const base = q.replace(/^.*[\\/]/, "");
        const hit = (listing.entries || []).find((e: any) => !e.isDir && e.name === base);
        if (hit) onEntry(hit, new MouseEvent("click"));
    } else {
        await enter(q);
    }
}

let offOpenPath: any = null;

onMounted(async () => {
    // 单例窗口已开着时，再次从磁盘/FTP“打开”不会重新挂载，靠事件导航到新文件
    try {
        offOpenPath = Events.On("viewer:open-path", (ev: any) => {
            const p = ev?.data ?? ev;
            if (p?.path) openPath(String(p.path));
        });
    } catch { /* 非 Wails 环境忽略 */ }

    const q = String(route.query.path || "");
    if (q) await openPath(q);
    else await goHome();
});

onUnmounted(() => {
    if (offOpenPath) offOpenPath();
});
</script>

<style scoped>
.viewer-page {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
    gap: 10px;
}
.vp-bar {
    flex: none;
}
.vp-path {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
}
.crumbs {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    white-space: nowrap;
}
.crumb {
    cursor: pointer;
    color: var(--color-text-2);
}
.crumb:hover {
    color: rgb(var(--primary-6));
}
.crumb.cur {
    font-weight: 600;
    color: var(--color-text-1);
    cursor: default;
}
.vp-addr {
    width: 260px;
    flex: none;
}
.vp-body {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: 10px;
}
.vp-side {
    width: 320px;
    flex: none;
    min-height: 0;
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-8);
    background: var(--color-bg-2);
    overflow: hidden;
    display: flex;
    flex-direction: column;
}
.vp-side-state {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--color-text-3);
}
.vp-err {
    color: rgb(var(--danger-6));
    padding: 0 12px;
    text-align: center;
}
.vp-list {
    flex: 1;
    overflow: auto;
    padding: 4px;
}
.vp-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 8px;
    border-radius: 6px;
    cursor: pointer;
    min-width: 0;
}
.vp-item:hover {
    background: var(--color-fill-2);
}
.vp-item.active {
    background: var(--color-primary-light-1);
}
.vp-item-name {
    flex: 1;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--color-text-1);
    font-size: var(--base-size-14);
}
.vp-item-meta {
    flex: none;
    color: var(--color-text-3);
    font-size: var(--base-size-12);
}
.vp-main {
    flex: 1;
    min-width: 0;
    min-height: 0;
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-8);
    background: var(--color-bg-2);
    overflow: hidden;
    display: flex;
    flex-direction: column;
}
.vp-empty {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
}
.vp-file-bar {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding: 6px 10px;
    border-bottom: 1px solid var(--color-border-2);
}
.vp-fb-file {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
}
.vp-fb-name {
    font-size: var(--base-size-14);
    color: var(--color-text-1);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 420px;
}
.vp-fb-size {
    flex: none;
    font-size: var(--base-size-12);
    color: var(--color-text-3);
}
</style>
