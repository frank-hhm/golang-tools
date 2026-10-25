<template>
    <div class="disk-page">
        <!-- ==================== 顶部磁盘总览 ==================== -->
        <disk-overview :drives="drives" :loading="drivesLoading" :active-root="driveRoot" :collapsed="ovCollapsed"
            @refresh="loadDrives" @toggle="ovCollapsed = !ovCollapsed" @jump="enter" />

        <!-- ==================== 文件浏览 ==================== -->
        <a-card class="file-card card-head-tool" size="small" :body-style="{ padding: 0 }">
            <template #title>
                <span class="file-title">文件浏览</span>
                <a-tag v-if="searchHits" color="arcoblue" size="small">搜索结果</a-tag>
            </template>
            <template #extra>
                <a-space :size="6">
                    <a-input v-model="keyword" size="small" class="fb-search" placeholder="按名称搜索，回车开始" allow-clear
                        @press-enter="doSearch" @clear="clearSearch">
                        <template #prefix><icon-search /></template>
                    </a-input>
                    <a-checkbox v-model="includeSub" :disabled="searching || loading">包含子目录</a-checkbox>
                    <a-button size="small" :loading="searching" :disabled="!keyword.trim()" @click="doSearch">
                        搜索
                    </a-button>
                    <a-button v-if="searchHits" size="small" @click="clearSearch">返回目录</a-button>
                </a-space>
            </template>

            <!-- 路径栏：上一级 / 主目录 / 面包屑 / 收藏 / 刷新 -->
            <div class="fb-path">
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
                <a-tooltip :content="isFav ? '取消收藏当前目录' : '收藏当前目录'">
                    <a-button size="small" @click="toggleFav">
                        <template #icon>
                            <icon-star-fill v-if="isFav" class="fav-on" />
                            <icon-star v-else />
                        </template>
                    </a-button>
                </a-tooltip>
                <a-tooltip content="刷新当前目录">
                    <a-button size="small" :loading="loading" @click="reload">
                        <template #icon><icon-refresh /></template>
                    </a-button>
                </a-tooltip>
            </div>

            <!-- 工具条 -->
            <div class="fb-bar">
                <a-dropdown trigger="click" @select="onUpload">
                    <a-button size="small" :disabled="busy">
                        <template #icon><icon-upload /></template>上传
                    </a-button>
                    <template #content>
                        <a-doption value="files"><icon-file /> 上传文件（复制进来）</a-doption>
                        <a-doption value="folder"><icon-folder /> 上传文件夹</a-doption>
                    </template>
                </a-dropdown>

                <a-dropdown trigger="click" @select="onCreate">
                    <a-button size="small">
                        <template #icon><icon-folder-add /></template>新建
                    </a-button>
                    <template #content>
                        <a-doption value="dir"><icon-folder-add /> 文件夹</a-doption>
                        <a-doption value="file"><icon-plus /> 空文件</a-doption>
                    </template>
                </a-dropdown>

                <a-button size="small" :disabled="!selectedPaths.length" @click="renameSelected">
                    <template #icon><icon-edit /></template>重命名
                </a-button>
                <a-button size="small" :disabled="!selectedPaths.length" @click="copySelected(false)">
                    <template #icon><icon-copy /></template>复制
                </a-button>
                <a-button size="small" :disabled="!selectedPaths.length" @click="copySelected(true)">
                    <template #icon><icon-scissor /></template>剪切
                </a-button>
                <a-tooltip :content="clipboard ? `粘贴 ${clipboard.paths.length} 项到当前目录` : '先复制或剪切要搬动的项目'">
                    <a-button size="small" :disabled="!canPaste" :loading="busy" @click="doPaste">
                        <template #icon><icon-paste /></template>粘贴
                    </a-button>
                </a-tooltip>

                <a-dropdown trigger="click" @select="onDeleteMenu">
                    <a-button size="small" status="danger" :disabled="!selectedPaths.length">
                        <template #icon><icon-delete /></template>删除
                    </a-button>
                    <template #content>
                        <a-doption value="trash"><icon-delete /> 移入回收站</a-doption>
                        <a-doption value="forever"><icon-exclamation-circle /> 永久删除</a-doption>
                    </template>
                </a-dropdown>

                <a-dropdown trigger="click" @select="(v: any) => enter(String(v || ''))">
                    <a-button size="small" :disabled="!favorites.length">
                        <template #icon><icon-star /></template>收藏夹
                    </a-button>
                    <template #content>
                        <a-doption v-for="p in favorites" :key="p" :value="p">
                            <icon-folder /> {{ baseName(p) || p }}
                        </a-doption>
                    </template>
                </a-dropdown>

                <a-dropdown trigger="click" @select="onMore">
                    <a-button size="small"><template #icon><icon-more /></template>更多</a-button>
                    <template #content>
                        <a-doption value="content"><icon-search /> 文件内容搜索</a-doption>
                        <a-doption value="props"><icon-info-circle /> 属性</a-doption>
                        <a-doption value="explorer"><icon-desktop /> 在资源管理器中打开</a-doption>
                        <a-doption value="shell"><icon-code /> 在此打开终端</a-doption>
                        <a-doption value="trash"><icon-archive /> 打开回收站</a-doption>
                    </template>
                </a-dropdown>

                <div class="bar-right">
                    <span v-if="clipboard" class="bar-clip">
                        剪贴板：{{ clipboard.mode === "cut" ? "剪切" : "复制" }} {{ clipboard.paths.length }} 项
                        <a-button size="mini" type="text" @click="clipboard = null">清空</a-button>
                    </span>
                    <a-radio-group v-model="view" type="button" size="mini">
                        <a-radio value="list"><icon-list /></a-radio>
                        <a-radio value="grid"><icon-apps /></a-radio>
                    </a-radio-group>
                </div>
            </div>

            <a-alert v-if="errMsg" type="error" class="fb-alert" closable @close="errMsg = ''">
                {{ errMsg }}
            </a-alert>

            <div class="fb-body">
                <!-- 列表视图 -->
                <a-table v-if="view === 'list'" class="fb-table" :data="pagedRows" :loading="loading || searching"
                    :pagination="false" size="small" row-key="path" :bordered="false"
                    :row-selection="{ type: 'checkbox', showCheckedAll: true }" v-model:selected-keys="selectedKeys"
                    :scroll="{ x: 1040, y: '100%' }" @row-dblclick="(record: any) => openEntry(record)">
                    <template #columns>
                        <a-table-column title="文件名称" :width="380">
                            <template #cell="{ record }">
                                <div class="name-cell">
                                    <span class="name-icon" :class="{ dir: record.isDir }">
                                        <component :is="'icon-' + iconOf(record)" />
                                    </span>
                                    <div class="name-box">
                                        <a-tooltip :content="record.name">
                                            <span class="name-text" :class="{ dir: record.isDir }"
                                                @click="openEntry(record)">{{ record.name }}</span>
                                        </a-tooltip>
                                        <!-- 搜索结果跨目录：把所在目录带上，不然光看名字找不到地方 -->
                                        <a-tooltip v-if="searchHits" :content="record.path">
                                            <span class="name-path">{{ parentOf(record.path) }}</span>
                                        </a-tooltip>
                                    </div>
                                    <a-tag v-if="record.link" size="small">链接</a-tag>
                                </div>
                            </template>
                        </a-table-column>
                        <a-table-column title="大小" :width="120" align="right">
                            <template #cell="{ record }">
                                <span v-if="!record.isDir" class="cell-size">{{ fmtSize(record.size) }}</span>
                                <a-tooltip :content="dirSizes[record.path] === undefined ? '递归统计整个目录的占用' : '点击重新计算'">
                                    <a-button v-if="dirSizes[record.path] === undefined" size="mini" type="text"
                                        :loading="!!sizeBusy[record.path]" @click.stop="calcSize(record)">
                                        计算
                                    </a-button>
                                    <span v-else class="cell-size is-link" @click.stop="calcSize(record)">
                                        {{ fmtSize(dirSizes[record.path]) }}
                                    </span>
                                </a-tooltip>
                            </template>
                        </a-table-column>
                        <a-table-column title="修改时间" data-index="modTime" :width="160" />
                        <a-table-column title="权限" :width="80">
                            <template #cell="{ record }">
                                <a-tooltip content="权限位来自文件模式；Windows 上只有只读/读写之分，仅供参考">
                                    <span class="cell-mode">{{ record.mode || "—" }}</span>
                                </a-tooltip>
                            </template>
                        </a-table-column>
                        <a-table-column title="操作" :width="260" fixed="right" align="right">
                            <template #cell="{ record }">
                                <a-space :size="2">
                                    <a-button size="mini" @click="openEntry(record)">打开</a-button>
                                    <a-button size="mini" @click="openNameModal('rename', record)">改名</a-button>
                                    <a-button size="mini" @click="copyOne(record)">复制</a-button>
                                    <a-dropdown trigger="click" @select="(v: any) => onRowMore(v, record)">
                                        <a-button size="mini"><icon-more /></a-button>
                                        <template #content>
                                            <a-doption value="cut"><icon-scissor /> 剪切</a-doption>
                                            <a-doption v-if="!record.isDir" value="download"><icon-download /> 另存为
                                            </a-doption>
                                            <a-doption value="size"><icon-storage /> 计算大小</a-doption>
                                            <a-doption value="props"><icon-info-circle /> 属性</a-doption>
                                            <a-doption value="explorer"><icon-desktop /> 在资源管理器中显示</a-doption>
                                            <a-doption value="shell"><icon-code /> 在此打开终端</a-doption>
                                            <a-doption value="delete"><icon-delete /> 删除到回收站</a-doption>
                                            <a-doption value="forever"><icon-exclamation-circle /> 永久删除</a-doption>
                                        </template>
                                    </a-dropdown>
                                </a-space>
                            </template>
                        </a-table-column>
                    </template>
                    <template #empty>
                        <a-empty :image="false" :description="searchHits ? '没有匹配的项目' : '这个目录是空的'" />
                    </template>
                </a-table>

                <!-- 网格视图：与资源管理器的大图标一致，单击选中、双击打开 -->
                <div v-else class="fb-grid">
                    <div v-if="!pagedRows.length" class="grid-empty">
                        <a-empty :image="false" :description="searchHits ? '没有匹配的项目' : '这个目录是空的'" />
                    </div>
                    <template v-else>
                        <div v-for="r in pagedRows" :key="r.path" class="g-item"
                            :class="{ sel: selectedKeys.includes(r.path) }" @click="toggleRow(r)"
                            @dblclick="openEntry(r)">
                            <span class="g-icon" :class="{ dir: r.isDir }">
                                <component :is="'icon-' + iconOf(r)" :size="34" />
                            </span>
                            <a-tooltip :content="r.name">
                                <div class="g-name">{{ r.name }}</div>
                            </a-tooltip>
                            <div class="g-meta">{{ r.isDir ? "文件夹" : fmtSize(r.size) }}</div>
                        </div>
                    </template>
                </div>
            </div>

            <!-- 页脚：条数统计 + 分页 -->
            <div class="fb-foot">
                <div class="foot-left">
                    <template v-if="searchHits">
                        <span>找到 {{ searchHits.length }} 项</span>
                        <span class="foot-dim">{{ includeSub ? "含子目录" : "仅当前层" }}</span>
                    </template>
                    <template v-else>
                        <span>共 {{ listing.dirs }} 个目录，{{ listing.files }} 个文件，文件大小 {{ fmtSize(listing.size) }}</span>
                        <a-tooltip v-if="listing.drive"
                            :content="`${listing.drive.root} 可用 ${fmtSize(listing.drive.free)}，共 ${fmtSize(listing.drive.total)}`">
                            <span class="foot-dim">盘符剩余 {{ fmtSize(listing.drive.free) }}</span>
                        </a-tooltip>
                    </template>
                    <a-button v-if="selectedKeys.length" size="mini" type="text" @click="selectedKeys = []">
                        已选 {{ selectedKeys.length }} 项 · 清空
                    </a-button>
                </div>
                <a-pagination v-model:current="page" v-model:page-size="pageSize" :total="sourceRows.length"
                    :page-size-options="[50, 100, 200, 500]" size="mini" show-total show-page-size show-jumper />
            </div>
        </a-card>

        <!-- ==================== 新建 / 重命名的名称输入 ==================== -->
        <a-modal v-model:visible="nameVisible" :title="nameTitle" :mask-closable="false" :ok-text="nameOk"
            cancel-text="取消" :on-before-ok="submitName">
            <a-input v-model="nameInput" :placeholder="nameMode === 'rename' ? '新名称' : '名称'" allow-clear
                @press-enter="submitName" />
            <div class="modal-hint">不能为空，不能包含 \ / : * ? " &lt; &gt; |，也不能以空格或点结尾</div>
        </a-modal>

        <!-- ==================== 属性 ==================== -->
        <a-modal v-model:visible="propVisible" :title="`属性：${prop.name || ''}`" :footer="false" width="640px">
            <a-descriptions :column="1" size="small" bordered :label-style="{ width: '110px' }">
                <a-descriptions-item label="名称">{{ prop.name }}</a-descriptions-item>
                <a-descriptions-item label="位置">{{ prop.path }}</a-descriptions-item>
                <a-descriptions-item label="类型">
                    {{ prop.isDir ? "文件夹" : prop.ext ? prop.ext.toUpperCase() + " 文件" : "文件" }}
                </a-descriptions-item>
                <a-descriptions-item label="大小">
                    <a-spin v-if="propSizeBusy" :size="14" />
                    <span v-else>{{ prop.size < 0 ? "统计失败（可能没有权限）" : fmtSize(prop.size) }}</span>
                </a-descriptions-item>
                <a-descriptions-item label="修改时间">{{ prop.modTime || "—" }}</a-descriptions-item>
                <a-descriptions-item label="权限位">{{ prop.mode || "—" }}</a-descriptions-item>
                <a-descriptions-item label="符号链接">{{ prop.link ? "是" : "否" }}</a-descriptions-item>
            </a-descriptions>
            <div class="prop-actions">
                <a-button size="small" @click="openExplorer(prop.path)">在资源管理器中显示</a-button>
                <a-button size="small" @click="openShell(prop.isDir ? prop.path : parentOf(prop.path))">
                    在此打开终端
                </a-button>
            </div>
        </a-modal>

        <!-- ==================== 文件内容搜索 ==================== -->
        <a-modal v-model:visible="contentVisible" :title="`内容搜索：${contentKw}`" :footer="false" width="920px"
            class="content-modal">
            <div class="cm-head">
                在 {{ current }} {{ includeSub ? "及其子目录" : "当前层" }}的文本文件里查找；跳过常见二进制与 2MB
                以上文件，最多返回 200 条。
            </div>
            <a-spin :loading="contentBusy" class="cm-body">
                <a-empty v-if="!contentBusy && !contentHits.length" :image="false" description="没有命中" />
                <a-table v-else :data="contentRows" :pagination="false" size="small" row-key="_k"
                    :scroll="{ y: 420 }">
                    <template #columns>
                        <a-table-column title="文件" :width="220" ellipsis tooltip>
                            <template #cell="{ record }">
                                <span class="name-text">{{ record.name }}</span>
                            </template>
                        </a-table-column>
                        <a-table-column title="行" data-index="line" :width="70" align="right" />
                        <a-table-column title="内容" data-index="text" ellipsis tooltip />
                        <a-table-column title="" :width="120" align="right">
                            <template #cell="{ record }">
                                <a-button size="mini" @click="openExplorer(record.path)">定位文件</a-button>
                            </template>
                        </a-table-column>
                    </template>
                </a-table>
            </a-spin>
        </a-modal>
    </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, reactive, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import DiskOverview from "./components/DiskOverview.vue";
import { baseName, driveRootOf, extIcon, fmtSize } from "./helpers";

/**
 * 磁盘管理（资源管理器）
 *
 * 只做三件事：用总览看各盘用量、用面包屑在目录间走、用工具条搬运文件。
 * 所有读写都走 Go 侧 Disk* 绑定，页面不碰文件系统也不拼路径（拼错了就是删错地方）。
 */

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

/** 空目录兜底：模板里就不用到处判空 */
const emptyListing = () => ({
    path: "",
    parent: "",
    parts: [] as any[],
    entries: [] as any[],
    dirs: 0,
    files: 0,
    size: 0,
    drive: null as any,
});

/* ============================ 状态 ============================ */

const drives = ref<any[]>([]);
const drivesLoading = ref(false);
const ovCollapsed = ref(false);

const listing = ref<any>(emptyListing());
const current = ref("");
const loading = ref(false);
const busy = ref(false); // 上传/粘贴这类耗时操作，期间禁用按钮防连点
const errMsg = ref("");

const view = ref<"list" | "grid">("list");
const page = ref(1);
const pageSize = ref(100);

const keyword = ref("");
const includeSub = ref(false);
const searching = ref(false);
const searchHits = ref<any[] | null>(null); // 非 null = 正在看搜索结果

const selectedKeys = ref<(string | number)[]>([]);
const clipboard = ref<{ mode: "copy" | "cut"; paths: string[] } | null>(null);
const favorites = ref<string[]>([]);

/** 目录大小按需计算：Windows 上读目录大小要递归走一遍，不能每次都算 */
const dirSizes = reactive<Record<string, number>>({});
const sizeBusy = reactive<Record<string, boolean>>({});

/* 新建/改名 */
const nameVisible = ref(false);
const nameMode = ref<"dir" | "file" | "rename">("dir");
const nameInput = ref("");
const renameTarget = ref<any>(null);
const nameTitle = computed(() => ({ dir: "新建文件夹", file: "新建文件", rename: "重命名" })[nameMode.value]);
const nameOk = computed(() => (nameMode.value === "rename" ? "保存" : "创建"));

/* 属性 */
const propVisible = ref(false);
const prop = ref<any>({});
const propSizeBusy = ref(false);

/* 内容搜索 */
const contentVisible = ref(false);
const contentHits = ref<any[]>([]);
const contentBusy = ref(false);
const contentKw = ref("");
/** 同一个行号可能出现在多个文件里，拼一个稳定 key 给表格（不同行重复会被复用而串行） */
const contentRows = computed(() => contentHits.value.map((h) => ({ ...h, _k: `${h.path}:${h.line}` })));

/* ============================ 派生 ============================ */

const sourceRows = computed<any[]>(() => (searchHits.value ? searchHits.value : listing.value.entries) || []);
const pagedRows = computed(() => {
    const start = (page.value - 1) * pageSize.value;
    return sourceRows.value.slice(start, start + pageSize.value);
});
const selectedPaths = computed<string[]>(() => (selectedKeys.value || []).map(String));
const singleSel = computed<any>(() =>
    selectedPaths.value.length === 1 ? sourceRows.value.find((r) => r.path === selectedPaths.value[0]) || null : null
);
const canPaste = computed(() => !!clipboard.value && !!current.value && !busy.value);
const isFav = computed(() => favorites.value.some((p) => p.toLowerCase() === current.value.toLowerCase()));
const driveRoot = computed(() => driveRootOf(current.value));

/** 目录内容变少（删除、搜索）时把页码拉回有效范围，别停在空白页 */
watch(sourceRows, (rows) => {
    const max = Math.max(1, Math.ceil(rows.length / pageSize.value));
    if (page.value > max) page.value = max;
});

/* ============================ 小工具 ============================ */

const errText = (e: any) => (typeof e === "string" ? e : e?.message || String(e || "未知错误"));

function iconOf(r: any): string {
    return r?.isDir ? "folder" : extIcon(r?.ext || "");
}

/** "C:\\a\\b.txt" → "C:\\a"（盘符根要保留反斜杠） */
function parentOf(p: string): string {
    const s = String(p || "").replace(/[\\/]+$/, "");
    const i = Math.max(s.lastIndexOf("\\"), s.lastIndexOf("/"));
    if (i < 0) return s;
    const dir = s.slice(0, i);
    return /^[A-Za-z]:$/.test(dir) ? dir + "\\" : dir;
}

/* ============================ 数据 ============================ */

async function loadDrives() {
    drivesLoading.value = true;
    try {
        drives.value = (await GoTools.DiskDrives()) || [];
    } catch (e) {
        errMsg.value = `读取盘符失败：${errText(e)}`;
    } finally {
        drivesLoading.value = false;
    }
}

async function loadFavorites() {
    try {
        favorites.value = (await GoTools.DiskFavorites()) || [];
    } catch {
        // 收藏读不到不影响浏览，静默即可
    }
}

/** 进入目录（空字符串会被忽略，避免误把当前目录清掉） */
async function enter(p: string) {
    const path = String(p || "").trim();
    if (!path) return;
    loading.value = true;
    errMsg.value = "";
    try {
        const res = await GoTools.DiskList(path);
        listing.value = res || emptyListing();
        current.value = res?.path || path;
        searchHits.value = null;
        keyword.value = "";
        selectedKeys.value = [];
        page.value = 1;
    } catch (e) {
        errMsg.value = `打不开 ${path}：${errText(e)}`;
    } finally {
        loading.value = false;
    }
}

/** 刷新：搜索结果里点刷新就按同样条件重搜，否则重读当前目录；顺手更新盘符容量 */
async function reload() {
    const kw = keyword.value.trim();
    if (searchHits.value && kw) await doSearch();
    else await enter(current.value || listing.value.path);
    loadDrives();
}

function goUp() {
    if (listing.value.parent) enter(listing.value.parent);
}

async function goHome() {
    try {
        await enter(await GoTools.DiskHome());
    } catch (e) {
        errMsg.value = `找不到用户主目录：${errText(e)}`;
    }
}

/* ============================ 打开 / 定位 ============================ */

async function openEntry(r: any) {
    if (!r) return;
    if (r.isDir) {
        enter(r.path);
        return;
    }
    try {
        await GoTools.DiskOpenFile(r.path);
    } catch (e) {
        Message.error(`打开失败：${errText(e)}`);
    }
}

async function openExplorer(p?: string) {
    const target = String(p || current.value || "").trim();
    if (!target) return;
    try {
        await GoTools.DiskOpenInExplorer(target);
    } catch (e) {
        Message.error(`打开资源管理器失败：${errText(e)}`);
    }
}

async function openShell(p?: string) {
    const target = String(p || current.value || "").trim();
    if (!target) return;
    try {
        await GoTools.DiskOpenShell(target);
    } catch (e) {
        Message.error(`打开终端失败：${errText(e)}`);
    }
}

async function openTrash() {
    try {
        await GoTools.DiskOpenTrash();
    } catch (e) {
        Message.error(`打开回收站失败：${errText(e)}`);
    }
}

/* ============================ 新建 / 改名 ============================ */

function openNameModal(mode: "dir" | "file" | "rename", target?: any) {
    nameMode.value = mode;
    renameTarget.value = target || null;
    nameInput.value = mode === "rename" ? target?.name || "" : "";
    nameVisible.value = true;
}

function renameSelected() {
    if (!singleSel.value) {
        Message.warning("请先选中一个项目");
        return;
    }
    openNameModal("rename", singleSel.value);
}

/** 返回 false 时不关弹窗，错误信息留在 Message 里，用户可以改个名字再试 */
async function submitName(): Promise<boolean> {
    const name = nameInput.value.trim();
    if (!name) {
        Message.warning("请输入名称");
        return false;
    }
    try {
        if (nameMode.value === "dir") {
            await GoTools.DiskMkdir(current.value, name);
        } else if (nameMode.value === "file") {
            await GoTools.DiskNewFile(current.value, name);
        } else {
            await GoTools.DiskRename(renameTarget.value?.path, name);
        }
        Message.success(nameMode.value === "rename" ? "已重命名" : "已创建");
        // 回车与点「确定」走同一条路：这里主动关一下，回车也能收起弹窗
        nameVisible.value = false;
        await reload();
        return true;
    } catch (e) {
        Message.error(errText(e));
        return false;
    }
}

/* ============================ 复制 / 剪切 / 粘贴 / 上传 ============================ */

function copySelected(cut: boolean) {
    const paths = selectedPaths.value;
    if (!paths.length) {
        Message.warning("请先选中要处理的项目");
        return;
    }
    clipboard.value = { mode: cut ? "cut" : "copy", paths: [...paths] };
    Message.success(`已${cut ? "剪切" : "复制"} ${paths.length} 项，切到目标目录后点「粘贴」`);
}

function copyOne(r: any) {
    clipboard.value = { mode: "copy", paths: [r.path] };
    Message.success(`已复制「${r.name}」，切到目标目录后点「粘贴」`);
}

async function doPaste() {
    const cb = clipboard.value;
    if (!cb || !current.value) return;
    busy.value = true;
    try {
        const n =
            cb.mode === "cut"
                ? await GoTools.DiskMove(cb.paths, current.value)
                : await GoTools.DiskCopy(cb.paths, current.value);
        Message.success(`已${cb.mode === "cut" ? "移动" : "复制"} ${n} 项到当前目录`);
        if (cb.mode === "cut") clipboard.value = null;
        await reload();
    } catch (e) {
        errMsg.value = `${cb.mode === "cut" ? "移动" : "复制"}失败：${errText(e)}`;
    } finally {
        busy.value = false;
    }
}

async function doUpload(kind: "files" | "folder") {
    if (!current.value) return;
    busy.value = true;
    try {
        if (kind === "files") {
            const picked = await GoTools.DiskUpload(current.value);
            if (picked?.length) {
                Message.success(`已复制 ${picked.length} 个文件到当前目录`);
                await reload();
            }
        } else {
            const picked = await GoTools.DiskUploadFolder(current.value);
            if (picked) {
                Message.success(`已复制文件夹「${baseName(picked)}」到当前目录`);
                await reload();
            }
        }
    } catch (e) {
        errMsg.value = `上传失败：${errText(e)}`;
    } finally {
        busy.value = false;
    }
}

async function downloadOne(r: any) {
    if (!r || r.isDir) {
        Message.warning("请选中一个文件（文件夹请用「复制」搬到目标目录）");
        return;
    }
    try {
        const saved = await GoTools.DiskDownload(r.path);
        if (saved) Message.success(`已另存为 ${saved}`);
    } catch (e) {
        Message.error(errText(e));
    }
}

/* ============================ 删除 ============================ */

function doDelete(permanent: boolean, paths?: string[]) {
    const list = paths && paths.length ? paths : selectedPaths.value;
    if (!list.length) {
        Message.warning("请先选中要删除的项目");
        return;
    }
    const names = list.map(baseName);
    const brief = names.slice(0, 5).join("、") + (names.length > 5 ? ` 等 ${names.length} 项` : "");
    Modal.warning({
        title: permanent ? "永久删除" : "删除到回收站",
        content: permanent
            ? `将直接删除 ${brief}；不进回收站，无法恢复。`
            : `将把 ${brief} 移入系统回收站，需要时可以还原。`,
        okText: permanent ? "永久删除" : "移入回收站",
        hideCancel: false,
        onOk: async () => {
            try {
                const n = await GoTools.DiskDelete(list, permanent);
                Message.success(`已删除 ${n} 项`);
                selectedKeys.value = [];
                await reload();
            } catch (e) {
                errMsg.value = `删除失败：${errText(e)}`;
            }
        },
    });
}

/* ============================ 搜索 ============================ */

async function doSearch() {
    const kw = keyword.value.trim();
    if (!kw) {
        clearSearch();
        return;
    }
    searching.value = true;
    errMsg.value = "";
    try {
        const hits = (await GoTools.DiskSearch(current.value, kw, includeSub.value)) || [];
        searchHits.value = hits;
        page.value = 1;
        selectedKeys.value = [];
        if (!hits.length) Message.info(`当前目录下没有匹配「${kw}」的项目`);
    } catch (e) {
        errMsg.value = `搜索失败：${errText(e)}`;
    } finally {
        searching.value = false;
    }
}

function clearSearch() {
    searchHits.value = null;
    keyword.value = "";
    page.value = 1;
}

async function doSearchContent() {
    const kw = keyword.value.trim();
    if (!kw) {
        Message.warning("先在右上角输入要搜索的关键字，再点「文件内容搜索」");
        return;
    }
    contentKw.value = kw;
    contentHits.value = [];
    contentVisible.value = true;
    contentBusy.value = true;
    try {
        contentHits.value = (await GoTools.DiskSearchContent(current.value, kw, includeSub.value)) || [];
    } catch (e) {
        Message.error(`内容搜索失败：${errText(e)}`);
    } finally {
        contentBusy.value = false;
    }
}

/* ============================ 大小 / 属性 ============================ */

async function calcSize(r: any) {
    if (!r || sizeBusy[r.path]) return;
    sizeBusy[r.path] = true;
    try {
        dirSizes[r.path] = await GoTools.DiskDirSize(r.path);
    } catch (e) {
        Message.error(`统计大小失败：${errText(e)}`);
    } finally {
        sizeBusy[r.path] = false;
    }
}

async function showProps(r: any) {
    if (!r) {
        Message.warning("请先选中一个项目");
        return;
    }
    prop.value = { ...r };
    propVisible.value = true;
    if (!r.isDir) return;
    // 目录大小要递归遍历，打开属性后再补上，别让弹窗空着等
    propSizeBusy.value = true;
    try {
        prop.value.size = await GoTools.DiskDirSize(r.path);
    } catch {
        prop.value.size = -1;
    } finally {
        propSizeBusy.value = false;
    }
}

/* ============================ 收藏 ============================ */

async function saveFavorites() {
    try {
        await GoTools.DiskSaveFavorites([...favorites.value]);
    } catch (e) {
        errMsg.value = `保存收藏失败：${errText(e)}`;
    }
}

async function toggleFav() {
    const cur = current.value;
    if (!cur) return;
    const i = favorites.value.findIndex((p) => p.toLowerCase() === cur.toLowerCase());
    if (i >= 0) favorites.value.splice(i, 1);
    else favorites.value.push(cur);
    Message.success(i >= 0 ? "已取消收藏" : "已收藏当前目录");
    await saveFavorites();
}

/* ============================ 工具条回调 ============================ */

function onUpload(v: any) {
    doUpload(v === "folder" ? "folder" : "files");
}

function onCreate(v: any) {
    openNameModal(v === "file" ? "file" : "dir");
}

function onDeleteMenu(v: any) {
    doDelete(v === "forever");
}

function onMore(v: any) {
    if (v === "content") doSearchContent();
    else if (v === "props") showProps(singleSel.value);
    else if (v === "explorer") openExplorer();
    else if (v === "shell") openShell();
    else if (v === "trash") openTrash();
}

function onRowMore(v: any, r: any) {
    if (v === "cut") {
        clipboard.value = { mode: "cut", paths: [r.path] };
        Message.success(`已剪切「${r.name}」，切到目标目录后点「粘贴」`);
    } else if (v === "download") downloadOne(r);
    else if (v === "size") calcSize(r);
    else if (v === "props") showProps(r);
    else if (v === "explorer") openExplorer(r.path);
    else if (v === "shell") openShell(r.isDir ? r.path : parentOf(r.path));
    else if (v === "delete") doDelete(false, [r.path]);
    else if (v === "forever") doDelete(true, [r.path]);
}

/** 网格视图没有复选框，单击切换选中 */
function toggleRow(r: any) {
    const i = selectedKeys.value.indexOf(r.path);
    if (i >= 0) selectedKeys.value.splice(i, 1);
    else selectedKeys.value.push(r.path);
}

/* ============================ 初始化 ============================ */

onMounted(async () => {
    await loadDrives();
    loadFavorites();
    try {
        await enter(await GoTools.DiskHome());
    } catch (e) {
        errMsg.value = `找不到默认目录，请从上方盘符点进去：${errText(e)}`;
    }
});
</script>

<style scoped>
.disk-page {
    height: 100%;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
}

/* 文件卡吃掉剩余高度：三段（路径/工具条/列表）+ 页脚，只有列表区滚动 */
.file-card {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.file-card :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.file-card :deep(.arco-card-header) {
    border-bottom: 1px solid var(--color-border-1);
    align-items: center;
}

.file-card :deep(.arco-card-header-title) {
    flex: none;
    margin-right: 12px;
}

.file-card :deep(.arco-card-header-extra) {
    flex: 1 1 auto;
    min-width: 0;
    justify-content: flex-end;
}

.file-title {
    font-size: var(--base-size-default);
    font-weight: 600;
}

.fb-search {
    width: 220px;
}

/* 路径栏 */
.fb-path {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 8px 12px;
    border-bottom: 1px solid var(--color-border-1);
    min-height: 0;
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
    cursor: default;
    color: var(--color-text-1);
    font-weight: 600;
}

.crumb.cur:hover {
    color: var(--color-text-1);
}

.fav-on {
    color: rgb(var(--orange-6));
}

/* 工具条 */
.fb-bar {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    padding: 8px 12px;
    border-bottom: 1px solid var(--color-border-1);
}

.bar-right {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: 6px;
}

.bar-clip {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.fb-alert {
    margin: 8px 12px 0;
}

/* 列表区：只剩这一块滚动 */
.fb-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: 0 12px;
}

.fb-body :deep(.arco-table-container) {
    flex: 1;
}

.fb-table {
    flex: 1;
    min-height: 0;
}

/* 名称列：图标 + 名字 + 可选副标题；宽度由列宽控住，长名省略号 */
.name-cell {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
}

.name-icon {
    flex: none;
    display: flex;
    align-items: center;
    color: var(--color-text-3);
}

.name-icon.dir {
    color: rgb(var(--orange-6));
}

.name-box {
    flex: 1 1 auto;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.name-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--color-text-1);
}

.name-text.dir {
    cursor: pointer;
    font-weight: 500;
}

.name-text.dir:hover {
    color: rgb(var(--primary-6));
}

.name-path {
    font-size: var(--base-size-small);
    color: var(--color-text-4);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.cell-size {
    font-variant-numeric: tabular-nums;
    color: var(--color-text-2);
}

.cell-size.is-link {
    cursor: pointer;
    color: rgb(var(--primary-6));
}

.cell-mode {
    font-family: Consolas, monospace;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

/* 网格视图 */
.fb-grid {
    flex: 1;
    min-height: 0;
    overflow: auto;
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(112px, 1fr));
    gap: 8px;
    align-content: start;
    padding: 10px 0;
}

.grid-empty {
    grid-column: 1 / -1;
    padding: 32px 0;
    display: flex;
    justify-content: center;
}

.g-item {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 4px;
    padding: 10px 8px;
    border: 1px solid transparent;
    border-radius: var(--base-radius-default);
    cursor: pointer;
    user-select: none;
    transition: background 0.2s, border-color 0.2s;
}

.g-item:hover {
    background: var(--color-fill-1);
}

.g-item.sel {
    border-color: rgb(var(--primary-6));
    background: rgba(var(--primary-6), 0.06);
}

.g-icon {
    color: var(--color-text-3);
}

.g-icon.dir {
    color: rgb(var(--orange-6));
}

.g-name {
    width: 100%;
    text-align: center;
    font-size: var(--base-size-small);
    color: var(--color-text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.g-meta {
    font-size: var(--base-size-small);
    color: var(--color-text-4);
    font-variant-numeric: tabular-nums;
}

/* 页脚 */
.fb-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    flex-wrap: wrap;
    padding: 6px 12px;
    border-top: 1px solid var(--color-border-1);
}

.foot-left {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    min-width: 0;
}

.foot-dim {
    color: var(--color-text-4);
}

/* 弹窗 */
.modal-hint {
    margin-top: 8px;
    font-size: var(--base-size-small);
    color: var(--color-text-4);
}

.prop-actions {
    display: flex;
    justify-content: flex-end;
    gap: 8px;
    margin-top: 12px;
}

.cm-head {
    margin-bottom: 10px;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.cm-body {
    display: block;
}
</style>
