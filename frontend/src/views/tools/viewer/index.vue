<template>
    <div class="viewer-page">
        <!-- ==================== 顶部路径栏（仅本机浏览态） ==================== -->
        <a-card v-if="!(docspaceOn && sideMode === 'docspace')" class="vp-bar card-head-tool" size="small" :body-style="{ padding: '8px 12px' }">
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
                <a-dropdown trigger="hover" position="bottom" @select="onRecentSelect">
                    <a-button size="small">
                        <template #icon><icon-history /></template>
                    </a-button>
                    <template #content>
                        <a-doption v-for="p in recentFiles" :key="p" :value="p" class="vp-recent-opt">
                            <common-file-icon :is-dir="false" :ext="extOf(p)" :size="14" />
                            <span class="vp-recent-name" :title="p">{{ baseName(p) }}</span>
                        </a-doption>
                        <a-doption v-if="!recentFiles.length" disabled :value="'__none__'">暂无最近文件</a-doption>
                    </template>
                </a-dropdown>
            </div>
        </a-card>

        <!-- ==================== 主体：左浏览 / 右预览 ==================== -->
        <div class="vp-body">
            <div class="vp-side">
                <!-- 左栏顶部：两态切换（总开关开着才出现） -->
                <div v-if="docspaceOn" class="vp-side-switch">
                    <a-radio-group v-model="sideMode" type="button" size="small">
                        <a-radio value="local">本机浏览</a-radio>
                        <a-radio value="docspace">文档中心</a-radio>
                    </a-radio-group>
                </div>

                <!-- 态一：本机浏览（DiskList，现状保留） -->
                <template v-if="!docspaceOn || sideMode === 'local'">
                    <div v-if="loading" class="vp-side-state"><a-spin /></div>
                    <div v-else-if="loadError" class="vp-side-state vp-err">{{ loadError }}</div>
                    <div v-else-if="!listing.entries || listing.entries.length === 0" class="vp-side-state">
                        <a-empty :image-size="48" description="空目录" />
                    </div>
                    <div v-else class="vp-side-list">
                        <div class="vp-side-sort">
                            <span class="vp-sort-label">{{ listing.entries.length }} 项</span>
                            <div class="vp-sort-group">
                                <a-button size="mini" :type="sortKey === 'name' ? 'primary' : 'outline'"
                                    @click="toggleSort('name')">名称</a-button>
                                <a-button size="mini" :type="sortKey === 'size' ? 'primary' : 'outline'"
                                    @click="toggleSort('size')">大小</a-button>
                            </div>
                        </div>
                        <div class="vp-list">
                            <div v-for="r in sortedEntries" :key="r.path" class="vp-item"
                                :class="{ active: !selectedManaged && selected && selected.path === r.path }" @click="onEntry(r, $event)">
                                <common-file-icon :is-dir="r.isDir" :ext="r.ext" :size="18" />
                                <span class="vp-item-name" :title="r.name">{{ r.name }}</span>
                                <span class="vp-item-meta">{{ r.isDir ? '' : fmtSize(r.size) }}</span>
                            </div>
                        </div>
                    </div>
                </template>

                <!-- 态二：文档中心（分类树 / 标签 / 搜索 / 条目） -->
                <div v-else class="vp-ds">
                    <div class="vp-ds-tools">
                        <a-input v-model="dsKeyword" size="small" placeholder="搜名称/路径/标签" allow-clear>
                            <template #prefix><icon-search /></template>
                        </a-input>
                        <a-select v-model="dsTagFilter" size="small" placeholder="标签" allow-clear>
                            <a-option v-for="t in dsTags" :key="t" :value="t" :label="t" />
                        </a-select>
                    </div>
                    <div class="vp-ds-tree">
                        <a-tree :data="dsTreeData" :selected-keys="dsSelectedCat" block-node size="small"
                            @select="onSelectDsCat" />
                    </div>
                    <div class="vp-ds-list">
                        <div v-if="dsLoading" class="vp-side-state"><a-spin /></div>
                        <div v-else-if="!dsFiltered.length" class="vp-side-state">无条目</div>
                        <div v-else class="vp-list">
                            <div v-for="it in dsFiltered" :key="it.id" class="vp-item"
                                :class="{ active: selectedManaged && selected && selected.id === it.id }" @click="dsOpen(it)">
                                <common-file-icon :is-dir="false" :ext="extOf(it.name)" :size="18" />
                                <span class="vp-item-name" :title="it.path">{{ it.name }}</span>
                                <span class="vp-item-meta">{{ it.src === 'local' ? '' : it.src }}</span>
                            </div>
                        </div>
                    </div>
                    <div class="vp-ds-foot">
                        <a-link @click="loadDocspace"><icon-refresh /> 刷新</a-link>
                        <a-link @click="gotoDocspacePage"><icon-folder /> 管理/回收站</a-link>
                    </div>
                </div>
            </div>

            <div class="vp-main">
                <!-- 已选中可编辑文本/代码：CodePane 自带文件栏（仅本机浏览或本机 local 托管条目） -->
                <code-pane v-if="selected && canCodeEdit" :key="selected.path"
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
                            <a-button v-if="backToArchive" size="mini" @click="returnToArchive">
                                <template #icon><icon-arrow-left /></template>返回压缩包
                            </a-button>
                            <template v-if="kind === 'image'">
                                <a-button size="mini" @click="stVisible = true">拼接…</a-button>
                                <a-button size="mini" @click="i2pVisible = true">转 PDF…</a-button>
                            </template>
                            <template v-if="kind === 'pdf'">
                                <a-button size="mini" @click="mergeVisible = true">合并…</a-button>
                                <a-button size="mini" @click="splitVisible = true">拆分…</a-button>
                            </template>
                            <a-button v-if="canOpenSystem" size="mini" @click="openSystem">
                                <template #icon><icon-launch /></template>系统程序打开
                            </a-button>
                            <a-button size="mini" @click="download">
                                <template #icon><icon-download /></template>下载
                            </a-button>
                        </a-space>
                    </div>
                    <a-alert v-if="legacyOffice" class="vp-legacy" type="warning" :closable="false">
                        这是旧版 Office（OLE）格式，浏览器无法解析预览，请用「系统程序打开」用本机 Office/WPS 编辑。
                    </a-alert>
                    <image-pane v-if="kind === 'image'" :url="mediaUrl" :name="selected.name"
                        :path="selectedPath" :can-write="canWriteLocal" @changed="reloadSelected" />
                    <pdf-pane v-else-if="kind === 'pdf'" :url="mediaUrl" :name="selected.name" />
                    <doc-pane v-else-if="kind === 'doc'" :url="mediaUrl" :path="selected.path"
                        :name="selected.name" :can-write="canWriteLocal" @changed="reloadSelected" />
                    <sheet-pane v-else-if="kind === 'sheet'" :url="mediaUrl" :path="selected.path"
                        :name="selected.name" :can-write="canWriteLocal" />
                    <archive-pane v-else-if="kind === 'archive'" :path="selected.path" :name="selected.name"
                        @open="openExtracted" />
                    <media-pane v-else-if="kind === 'audio' || kind === 'video'" :url="mediaUrl"
                        :name="selected.name" :kind="kind" />
                    <binary-pane v-else :url="mediaUrl" :name="selected.name" :size="selected.size || 0" />
                </template>

                <div v-else class="vp-empty">
                    <a-empty :description="docspaceOn && sideMode === 'docspace' ? '从左侧选一个文档中心的文件预览' : '从左侧选择一个文件查看'" />
                </div>
            </div>
        </div>

        <!-- 图片 / PDF 批量操作（仅本机文件：多选靠弹窗内的文件挑选器） -->
        <stitch-modal v-model:visible="stVisible" :paths="seedPaths" :start-dir="seedDir" @done="afterBatch" />
        <image-to-pdf-modal v-model:visible="i2pVisible" :paths="seedPaths" :start-dir="seedDir" @done="afterBatch" />
        <pdf-merge-modal v-model:visible="mergeVisible" :paths="seedPaths" :start-dir="seedDir" @done="afterBatch" />
        <pdf-split-modal v-model:visible="splitVisible" :paths="seedPaths" :start-dir="seedDir" @done="afterBatch" />
    </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Message } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import CodePane from "./components/CodePane.vue";
import ImagePane from "./components/ImagePane.vue";
import PdfPane from "./components/PdfPane.vue";
import SheetPane from "./components/SheetPane.vue";
import MediaPane from "./components/MediaPane.vue";
import BinaryPane from "./components/BinaryPane.vue";
import ArchivePane from "./components/ArchivePane.vue";
import DocPane from "./components/DocPane.vue";
import StitchModal from "./components/edit/StitchModal.vue";
import Image2PdfModal from "./components/edit/Image2PdfModal.vue";
import PdfMergeModal from "./components/edit/PdfMergeModal.vue";
import PdfSplitModal from "./components/edit/PdfSplitModal.vue";
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
const router = useRouter();

const listing = reactive<any>({ path: "", parent: "", parts: [], entries: [] });
const addrInput = ref("");
const loading = ref(false);
const loadError = ref("");
const selected = ref<any>(null);
const kind = ref("");

// 左栏两态：本机浏览（DiskList）/ 文档中心（docspace）。docspaceOn 由总开关决定，
// 关掉时只留本机浏览（符合计划「不启用就不挂载」）。
type SideMode = "local" | "docspace";
const sideMode = ref<SideMode>("local");
const docspaceOn = ref(false);
const selectedManaged = ref(false);   // 当前右侧选中的是否来自文档中心（决定 mediaUrl 走 docref 端点）
const activeMedia = ref("");          // 托管条目的预览 URL（含 src/sid/path），由后端 DocSpacePreviewURL 拼装

const textExts = ["txt", "md", "log", "ini", "conf", "env", "json", "yaml", "yml", "js", "ts", "go", "py", "php", "sh", "bat", "css", "html"];
function extOf(name: string): string {
    const i = name.lastIndexOf(".");
    return i >= 0 ? name.slice(i + 1).toLowerCase() : "";
}

// 文本/代码编辑器只在「本机浏览」或「本机 local 源托管条目」下可用；
// SFTP 托管条目的 path 是本机读不到的，编辑会失败，因此退回按 URL 流式预览。
const canCodeEdit = computed(() => {
    if (!selected.value) return false;
    if (kind.value !== "text" && kind.value !== "code") return false;
    return !selectedManaged.value || selected.value.src === "local";
});

// 系统程序打开：仅 Windows 桌面构建有可靠外壳；Linux 面板多半无图形界面，按能力隐藏
const canOpenSystem = computed(() => !!caps.nativeWindows);

/* ---------------- 左栏排序与最近文件 ---------------- */
// 排序只影响本机浏览列表：目录始终在前，同名/同大小时按名称兜底，保证稳定。
type SortKey = "name" | "size";
const sortKey = ref<SortKey>((localStorage.getItem("viewer:sort") as SortKey) || "name");
function toggleSort(k: SortKey) {
    if (sortKey.value === k) {
        // 再点同一键翻转升降序（名称升/降，大小大/小）
        sortDesc.value = !sortDesc.value;
    } else {
        sortKey.value = k;
        sortDesc.value = false;
    }
    localStorage.setItem("viewer:sort", sortKey.value);
    localStorage.setItem("viewer:sort-desc", sortDesc.value ? "1" : "");
}
const sortDesc = ref(localStorage.getItem("viewer:sort-desc") === "1");
const sortedEntries = computed(() => {
    const list = [...(listing.entries || [])];
    const dir = sortDesc.value ? -1 : 1;
    list.sort((a: any, b: any) => {
        if (a.isDir !== b.isDir) return a.isDir ? -1 : 1;
        if (sortKey.value === "size") return dir * ((a.size || 0) - (b.size || 0));
        const an = String(a.name || "");
        const bn = String(b.name || "");
        return dir * an.localeCompare(bn, "zh-CN", { numeric: true, sensitivity: "base" });
    });
    return list;
});

// 最近文件：只记本机浏览打开过的文件，localStorage 存最近 10 条，不占后端。
const RECENT_KEY = "viewer:recent";
const RECENT_MAX = 10;
const recentFiles = ref<string[]>((() => {
    try { return JSON.parse(localStorage.getItem(RECENT_KEY) || "[]"); } catch { return []; }
})());
function pushRecent(path: string) {
    if (!path) return;
    recentFiles.value = [path, ...recentFiles.value.filter((p) => p !== path)].slice(0, RECENT_MAX);
    localStorage.setItem(RECENT_KEY, JSON.stringify(recentFiles.value));
}
function onRecentSelect(v: any) {
    const p = String(v || "");
    if (p && p !== "__none__") void openPath(p);
}
function baseName(p: string): string {
    const i = Math.max(p.lastIndexOf("\\"), p.lastIndexOf("/"));
    return i >= 0 ? p.slice(i + 1) : p;
}

// 在线写回/编辑只在「本机浏览」或「本机 local 源托管条目」下可用（需真实可写本机路径）。
const canWriteLocal = computed(() => !!selected.value && (!selectedManaged.value || selected.value.src === "local"));

/* ---------------- 图片 / PDF 批量操作弹窗 ---------------- */
const stVisible = ref(false);
const i2pVisible = ref(false);
const mergeVisible = ref(false);
const splitVisible = ref(false);

// 当前选中文件的本机路径：托管条目只有 src=local 时路径才真能读，否则空（弹窗里自己选）
const selectedPath = computed<string>(() => {
    const s = selected.value;
    if (!s || !s.path) return "";
    if (selectedManaged.value && s.src !== "local") return "";
    return String(s.path);
});
const seedDir = computed(() => (selectedPath.value ? selectedPath.value.replace(/[\\/][^\\/]+$/, "") : ""));
const seedPaths = computed(() => (selectedPath.value ? [selectedPath.value] : []));

// 产物落在当前目录，刷新本机列表才能看得见（文档中心态下左栏不是磁盘列表，不刷）
function afterBatch() { if (sideMode.value === "local") reload(); }

// 旧版/浏览器不可解析的 Office 格式（docx 归入 doc，这里的 doc/ppt/pptx 都落到 binary）：
// 给出明确提示引导走本机程序，而不是丢一个十六进制窗口让用户以为坏了。
const legacyOffice = computed(() => {
    if (!selected.value || kind.value !== "binary") return false;
    return ["doc", "ppt", "pptx", "pps"].includes(String(selected.value.ext || "").toLowerCase());
});

// 从压缩包提取到临时目录后，记住来源压缩包条目，供「返回压缩包」还原视图。
const backToArchive = ref<any>(null);

// 富类型预览统一指这个流式端点（图片/PDF/音视频/表格/HEX 都同源加载，不经 base64）。
// 本机浏览走旧 ?path=；文档中心托管条目走后端拼好的 docref 版 URL（含 src/sid/path）。
const mediaUrl = computed(() => {
    if (!selected.value) return "";
    if (selectedManaged.value) return activeMedia.value;
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
    pushRecent(r.path);
    selectedManaged.value = false;
    activeMedia.value = "";
    backToArchive.value = null;
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

/* ===================== 文档中心左栏（分类树 / 标签 / 搜索 / 条目） ===================== */
interface DsCategory { id: string; name: string; parent?: string; sort: number; }
interface DsItem { id: string; src: string; sid?: string; path: string; name: string; category?: string; tags?: string[]; }
const dsLoading = ref(false);
const dsCategories = ref<DsCategory[]>([]);
const dsItems = ref<DsItem[]>([]);
const dsTags = ref<string[]>([]);
const dsSelectedCat = ref<string[]>(["__all__"]);
const dsKeyword = ref("");
const dsTagFilter = ref<string>("");

const dsTreeData = computed(() => {
    const inCat = (id: string) => (id === "__all__" ? dsItems.value.length : dsItems.value.filter((it) => it.category === id).length);
    const nodes: any[] = [
        { key: "__all__", title: `全部（${inCat("__all__")}）` },
        { key: "__uncat__", title: `未分类（${dsItems.value.filter((it) => !it.category).length}）` },
    ];
    const byParent = (p: string) => dsCategories.value.filter((c) => (c.parent || "") === p);
    const build = (parent: string, depth: number): any[] =>
        byParent(parent).map((c) => ({
            key: c.id,
            title: `${c.name}（${inCat(c.id)}）`,
            children: depth < 5 ? build(c.id, depth + 1) : undefined,
        }));
    nodes.push(...build("", 0));
    return nodes;
});

const dsFiltered = computed(() => {
    let list = dsItems.value;
    const sel = dsSelectedCat.value[0];
    if (sel && sel !== "__all__") {
        list = sel === "__uncat__" ? list.filter((it) => !it.category) : list.filter((it) => it.category === sel);
    }
    if (dsTagFilter.value) list = list.filter((it) => (it.tags ?? []).includes(dsTagFilter.value));
    const kw = dsKeyword.value.trim().toLowerCase();
    if (kw) {
        list = list.filter((it) =>
            it.name.toLowerCase().includes(kw) ||
            it.path.toLowerCase().includes(kw) ||
            (it.tags ?? []).some((t) => t.toLowerCase().includes(kw)));
    }
    return list;
});

async function loadDocspace() {
    dsLoading.value = true;
    try {
        const [cats, list, tags] = await Promise.all([
            GoTools?.DocSpaceCategories(),
            GoTools?.DocSpaceItems(),
            GoTools?.DocSpaceAllTags(),
        ]);
        dsCategories.value = (cats as DsCategory[]) ?? [];
        dsItems.value = (list as DsItem[]) ?? [];
        dsTags.value = (tags as string[]) ?? [];
    } catch {
        /* 静默 */
    } finally {
        dsLoading.value = false;
    }
}

async function dsOpen(it: DsItem) {
    await openManaged(it.src, it.sid || "", it.path, it.name, it.id);
}

// 统一的托管/远端打开：凭 (src, sid, path) 三元组经 docref 拼预览 URL 开文件，
// 不依赖文档中心条目 id（FTP 页传来的远端文件没有条目 id，sid 直接复用会话）。
async function openManaged(src: string, sid: string, path: string, name: string, id = "") {
    backToArchive.value = null;
    selectedManaged.value = true;
    selected.value = { id, path, name, ext: extOf(name), size: 0, isDir: false, src, sid };
    kind.value = "";
    activeMedia.value = "";
    try {
        const url = await GoTools?.DocSpacePreviewURL(src, sid, path, false);
        activeMedia.value = String(url || "");
    } catch { /* 拼 URL 失败则富预览不可用 */ }
    try {
        const res = await GoTools.FileClassify(path);
        kind.value = Array.isArray(res) ? res[0] : (res?.kind || "");
    } catch {
        kind.value = textExts.includes(extOf(name)) ? "text" : "";
    }
    if (src !== "local" && (kind.value === "text" || kind.value === "code")) {
        Message.info("远端文本不支持在线编辑，已按流式预览打开；如需编辑请先取回本机");
    }
}

function onSelectDsCat(keys: (string | number)[]) {
    dsSelectedCat.value = keys.length ? [String(keys[0])] : ["__all__"];
}

// 从压缩包提取出的临时文件：当作本机文件重新分发到对应 pane（文本/图片/表格…）。
async function openExtracted(tempPath: string) {
    if (!tempPath) return;
    // 只在第一次从压缩包下钻时记来源，避免“提取→提取”嵌套时丢失上一层
    if (!backToArchive.value && selected.value && kind.value === "archive") {
        backToArchive.value = { ...selected.value };
    }
    const base = tempPath.replace(/^.*[\\/]/, "");
    const i = base.lastIndexOf(".");
    selectedManaged.value = false;
    activeMedia.value = "";
    selected.value = { path: tempPath, name: base, ext: i >= 0 ? base.slice(i + 1).toLowerCase() : "", size: 0, isDir: false };
    kind.value = "";
    try {
        const res = await GoTools.FileClassify(tempPath);
        kind.value = Array.isArray(res) ? res[0] : (res?.kind || "");
    } catch {
        kind.value = textExts.includes(extOf(base)) ? "text" : "";
    }
}

function returnToArchive() {
    if (!backToArchive.value) return;
    selected.value = backToArchive.value;
    backToArchive.value = null;
    kind.value = "archive";
}

// DocPane 写回后刷新当前选中（重新分类 + 重拉预览 URL，让渲染跟着新内容走）。
async function reloadSelected() {
    if (!selected.value) return;
    try {
        const res = await GoTools.FileClassify(selected.value.path);
        kind.value = Array.isArray(res) ? res[0] : (res?.kind || kind.value);
    } catch { /* 保持原 kind */ }
    if (selectedManaged.value) {
        try {
            const url = await GoTools?.DocSpacePreviewURL(selected.value.src, selected.value.sid || "", selected.value.path, false);
            activeMedia.value = String(url || "");
        } catch { /* 保留旧 URL */ }
    }
}

function gotoDocspacePage() {
    void router.push("/tools/docspace");
}

// 切到文档中心态时，若尚未加载过（初次读开关失败或后台新增条目）补一次拉取
watch(sideMode, (m) => { if (m === "docspace" && !dsItems.value.length) void loadDocspace(); });

async function openSystem() {
    if (!selected.value) return;
    try {
        await GoTools.FileOpenSystem(selected.value.path);
    } catch (e: any) {
        Message.warning(String(e?.message || e || "打开失败"));
    }
}

async function download() {
    if (!selected.value) return;
    if (selectedManaged.value) {
        try {
            const u = await GoTools?.DocSpacePreviewURL(selected.value.src, selected.value.sid || "", selected.value.path, true);
            window.open(String(u || ""), "_blank");
        } catch (e: any) {
            Message.warning(String(e?.message || e || "下载失败"));
        }
        return;
    }
    const p = encodeURIComponent(selected.value.path);
    const n = encodeURIComponent(selected.value.name || "");
    window.open(`/fileview/media?path=${p}&name=${n}&dl=1`, "_blank");
}

// 定位到一个路径：目录则进入，文件先进其所在目录再选中。供初次挂载与事件复用共用。
async function openPath(q: string) {
    if (!q) return;
    // 来自磁盘/FTP 的“打开”都是本机路径，回到本机浏览态
    selectedManaged.value = false;
    if (docspaceOn.value) sideMode.value = "local";
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

// 统一的“外部打开”入口：带 src+sid 的走托管/远端（docref 流式，远端音视频可拖进度），
// 否则按本机路径浏览。磁盘/FTP 页与初次挂载都汇到这里。
async function openFromQuery(src: string, sid: string, path: string, name: string) {
    if (src && src !== "local" && sid && path) {
        const base = name || path.replace(/^.*[\\/]/, "");
        await openManaged(src, sid, path, base);
        return;
    }
    if (path) await openPath(path);
}

onMounted(async () => {
    // 单例窗口已开着时，再次从磁盘/FTP“打开”不会重新挂载，靠事件导航到新文件
    try {
        offOpenPath = Events.On("viewer:open-path", (ev: any) => {
            const p = ev?.data ?? ev;
            if (p?.path) void openFromQuery(String(p.src || ""), String(p.sid || ""), String(p.path), String(p.name || ""));
        });
    } catch { /* 非 Wails 环境忽略 */ }

    // 读文档中心总开关：开着才展示“文档中心”左栏态（不启用就不加载）
    try {
        const s = await GoTools?.DocSpaceSettings();
        docspaceOn.value = s ? !!s.enabled : false;
    } catch { docspaceOn.value = false; }
    if (docspaceOn.value) void loadDocspace();

    const q: any = route.query;
    if (q.path) await openFromQuery(String(q.src || ""), String(q.sid || ""), String(q.path), String(q.name || ""));
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
    border: 1px solid var(--color-border-1);
    border-radius: var(--base-radius-default);
    background: var(--color-bg-1);
    overflow: hidden;
    display: flex;
    flex-direction: column;
}
.vp-side-switch {
    flex: none;
    padding: 8px;
    border-bottom: 1px solid var(--color-border-1);
}
.vp-ds {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}
.vp-ds-tools {
    flex: none;
    display: flex;
    gap: 6px;
    padding: 8px;
}
.vp-ds-tools .arco-input-wrapper {
    flex: 1;
    min-width: 0;
}
.vp-ds-tree {
    flex: none;
    max-height: 34%;
    overflow: auto;
    padding: 0 8px 8px;
    border-bottom: 1px solid var(--color-border-1);
}
.vp-ds-list {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 4px;
}
.vp-ds-foot {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 10px;
    border-top: 1px solid var(--color-border-1);
}
.vp-side-state {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: rgb(var(--gray-5));
}
.vp-err {
    color: rgb(var(--danger-6));
    padding: 0 12px;
    text-align: center;
}
.vp-side-list {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}
.vp-side-sort {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 4px 8px;
    border-bottom: 1px solid var(--color-border-1);
}
.vp-sort-label {
    color: rgb(var(--gray-5));
    font-size: var(--base-size-small);
}
.vp-sort-group {
    display: flex;
    gap: 4px;
}
.vp-list {
    flex: 1;
    overflow: auto;
    padding: 4px;
}
.vp-item {
    position: relative;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 5px 8px;
    border-radius: var(--base-radius-default);
    cursor: pointer;
    min-width: 0;
}
.vp-item:hover {
    background: var(--color-fill-2);
}
/* 选中态：中性淡底 + 左侧短竖条（不加深边框 / 不加投影），与磁盘管理、Git、docspace 一套语言 */
.vp-item.active {
    background: var(--color-fill-3);
}
.vp-item::after {
    content: "";
    position: absolute;
    left: 0;
    top: 50%;
    transform: translateY(-50%);
    width: 3px;
    height: 55%;
    border-radius: 0 var(--base-radius-small) var(--base-radius-small) 0;
    background: transparent;
}
.vp-item.active::after {
    background: rgb(var(--primary-6));
}
.vp-item-name {
    flex: 1;
    min-width: 0;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--color-text-1);
    font-size: var(--base-size-large);
}
.vp-item-meta {
    flex: none;
    color: rgb(var(--gray-5));
    font-size: var(--base-size-small);
}
.vp-main {
    flex: 1;
    min-width: 0;
    min-height: 0;
    border: 1px solid var(--color-border-1);
    border-radius: var(--base-radius-default);
    background: var(--color-bg-1);
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
    border-bottom: 1px solid var(--color-border-1);
}
.vp-fb-file {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
}
.vp-legacy {
    flex: none;
    margin: 8px 10px 0;
}
.vp-fb-name {
    font-size: var(--base-size-large);
    color: var(--color-text-1);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 420px;
}
.vp-fb-size {
    flex: none;
    font-size: var(--base-size-small);
    color: rgb(var(--gray-5));
}
.vp-recent-opt {
    display: flex;
    align-items: center;
    gap: 6px;
    max-width: 320px;
}
.vp-recent-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}
</style>
