<template>
    <a-row class="sniffer-page" :gutter="6">
        <!-- 左侧约 30%：抓包控制（上50%）+ 筛选（下50%） -->
        <a-col :span="5" class="side-col">
            <a-row class="side-pages">
                <a-col class="side-cols" :span="24">
                    <a-card class="side-card" size="small" :body-style="{ padding: 0 }">
                        <template #title>
                            <a-space :size="6">
                                <span class="led" :class="{ on: running }"></span>
                                <span>抓包控制</span>
                            </a-space>
                        </template>
                        <template #extra>
                            <a-tag size="small" :color="running ? 'green' : 'gray'">
                                {{ running ? "捕获中" : "未开始" }}
                            </a-tag>
                        </template>
                        <div class="side-body">
                            <div class="field">
                                <div class="field-label">代理端口</div>
                                <a-input-number v-model="port" :min="1" :max="65535" :disabled="running" hide-button
                                    style="width: 100%" />
                            </div>
                            <div class="field-actions">
                                <a-button v-if="!running" type="primary" status="success" long @click="onStart">
                                    <template #icon><icon-play-arrow /></template>开始抓包
                                </a-button>
                                <a-button v-else status="danger" long @click="onStop">
                                    <template #icon><icon-pause /></template>停止抓包
                                </a-button>
                                <a-button long :disabled="!flows.length" @click="clearAll">
                                    <template #icon><icon-delete /></template>清空记录
                                </a-button>
                            </div>
                            <a-descriptions :column="2" size="small" bordered class="stat-desc">
                                <a-descriptions-item label="总数">{{ flows.length }}</a-descriptions-item>
                                <a-descriptions-item label="显示">{{ filteredFlows.length }}</a-descriptions-item>
                                <a-descriptions-item label="监听" :span="2">
                                    <span class="mono">127.0.0.1:{{ port }}</span>
                                </a-descriptions-item>
                            </a-descriptions>
                            <div class="ca-tip">
                                <icon-info-circle /> 开始后会安装本地根证书并临时接管系统代理（停止时自动还原），用于解密 HTTPS 流量。
                            </div>
                        </div>
                    </a-card>
                </a-col>
                <a-col class="side-cols mt6" :span="24">
                    <a-card class="side-card" size="small" :body-style="{ padding: 0 }">
                        <template #title>
                            <a-space>
                                <div>筛选</div>
                                <a-input-search v-model="sideSearch" placeholder="筛选应用 / 域名" size="small" allow-clear />
                            </a-space>
                        </template>
                        <template #extra>
                            <a-radio-group v-model="filterTab" type="button">
                                <a-radio value="app">应用</a-radio>
                                <a-radio value="domain">域名</a-radio>
                            </a-radio-group>
                        </template>
                        <div class="side-body filter-body">
                            <div v-show="filterTab === 'app'" class="filter-view">
                                <a-list :bordered="false" size="small" class="filter-list">
                                    <a-list-item @click="selectedProcess = ''">
                                        <div class="filter-item" :class="{ active: !selectedProcess }">
                                            <span class="filter-name">全部</span>
                                            <a-tag size="small" color="gray">{{ flows.length }}</a-tag>
                                        </div>
                                    </a-list-item>
                                    <a-list-item v-for="p in processList" :key="p.name" @click="toggleProcess(p.name)">
                                        <div class="filter-item" :class="{ active: selectedProcess === p.name }">
                                            <span class="filter-name" :title="p.name">{{ p.name }}</span>
                                            <a-tag size="small" color="gray">{{ p.count }}</a-tag>
                                        </div>
                                    </a-list-item>
                                </a-list>
                            </div>
                            <div v-show="filterTab === 'domain'" class="filter-view">
                                <a-list :bordered="false" size="small" class="filter-list">
                                    <a-list-item @click="selectedDomain = ''">
                                        <div class="filter-item" :class="{ active: !selectedDomain }">
                                            <span class="filter-name">全部</span>
                                            <a-tag size="small" color="gray">{{ flows.length }}</a-tag>
                                        </div>
                                    </a-list-item>
                                    <a-list-item v-for="d in domainList" :key="d.name" @click="toggleDomain(d.name)">
                                        <div class="filter-item" :class="{ active: selectedDomain === d.name }">
                                            <span class="filter-name" :title="d.name">{{ d.name }}</span>
                                            <a-tag size="small" color="gray">{{ d.count }}</a-tag>
                                        </div>
                                    </a-list-item>
                                </a-list>
                            </div>
                        </div>
                    </a-card>
                </a-col>
            </a-row>
        </a-col>

        <!-- 右侧：请求列表（左）+ 请求详情（右） -->
        <a-col :span="19" class="main-col">
            <a-row class="main-row">
                <a-col :span="12" class="pane-col">
                    <a-card class="main-panel" :body-style="{ padding: 0 }">
                        <template #title>
                            <a-space :size="8">
                                <span>请求列表</span>
                                <a-tag size="small">共 {{ flows.length }}</a-tag>
                            </a-space>
                        </template>
                        <template #extra>
                            <a-radio-group v-model="selectedProto" type="button">
                                <a-radio v-for="t in PROTO_TABS" :key="t.key" :value="t.key">
                                    {{ t.label }}<span v-if="t.key !== 'All'" class="pcnt">{{ protoCount(t.key)
                                        }}</span>
                                </a-radio>
                            </a-radio-group>
                        </template>
                        <div class="list-body">
                            <div class="list-toolbar">
                                <a-input-search v-model="keyword" placeholder="搜索 URL / 域名" size="small" allow-clear
                                    class="list-search" />
                                <span class="count-tip">显示 {{ filteredFlows.length }} 条</span>
                            </div>
                            <a-table class="flow-table" :data="pagedFlows" row-key="id" size="small" :pagination="false"
                                :scroll="{ y: '100%', x: 720 }" :column-resizable="true"  :bordered="{ wrapper: true, cell: true }"
                                :row-class="(r) => (r.id === selectedId ? 'row-selected' : '')" @row-click="onRowClick">
                                <template #columns>
                                    <a-table-column title="ID" data-index="id" :width="60" />
                                    <a-table-column title="协议" data-index="protocol" :width="70">
                                        <template #cell="{ record }">
                                            <a-tag size="small" :color="protoColor(record.protocol)">
                                                {{ record.protocol }}
                                            </a-tag>
                                        </template>
                                    </a-table-column>
                                    <a-table-column title="方法" data-index="method" :width="70" />
                                    <a-table-column title="URL" data-index="url" :ellipsis="true" :tooltip="true" />
                                    <a-table-column title="应用程序" data-index="process" :width="120" :ellipsis="true"
                                        :tooltip="true" />
                                    <a-table-column title="状态" data-index="statusCode" :width="70">
                                        <template #cell="{ record }">
                                            <span :style="{ color: statusColor(record.statusCode) }">
                                                {{ record.statusCode }}
                                            </span>
                                        </template>
                                    </a-table-column>
                                    <a-table-column title="大小" :width="90">
                                        <template #cell="{ record }">{{ formatSize(record.respSize) }}</template>
                                    </a-table-column>
                                    <a-table-column title="耗时" :width="80">
                                        <template #cell="{ record }">{{ record.duration }}ms</template>
                                    </a-table-column>
                                </template>
                            </a-table>
                            <div class="pager">
                                <a-pagination v-model:current="page" :total="filteredFlows.length" :page-size="pageSize"
                                    size="mini" show-total />
                            </div>
                        </div>
                    </a-card>
                </a-col>

                <a-col :span="12" class="pane-col">
                    <a-card class="main-panel" :body-style="{ padding: 0 }">
                        <template #title>请求详情</template>
                        <template #extra>
                            <a-space v-if="selectedFlow" :size="6">
                                <a-tag size="small" :color="protoColor(selectedFlow.protocol)">
                                    {{ selectedFlow.method }}
                                </a-tag>
                                <a-tag size="small" :style="{ color: statusColor(selectedFlow.statusCode) }">
                                    {{ selectedFlow.statusCode }}
                                </a-tag>
                            </a-space>
                        </template>
                        <div class="detail-body">
                            <template v-if="selectedFlow">
                                <div class="detail-head">
                                    <span class="detail-url" :title="selectedFlow.url">{{ selectedFlow.url }}</span>
                                </div>
                                <a-radio-group v-model="detailTab" type="button" class="detail-nav">
                                    <a-radio value="overview">概览</a-radio>
                                    <a-radio value="reqHeaders">请求头</a-radio>
                                    <a-radio value="reqBody">请求体</a-radio>
                                    <a-radio value="respHeaders">响应头</a-radio>
                                    <a-radio value="respBody">响应体</a-radio>
                                </a-radio-group>

                                <div v-show="detailTab === 'overview'" class="detail-view">
                                    <a-descriptions :column="1" size="small" bordered>
                                        <a-descriptions-item label="时间">{{ selectedFlow.time }}</a-descriptions-item>
                                        <a-descriptions-item label="协议">{{ selectedFlow.protocol
                                            }}</a-descriptions-item>
                                        <a-descriptions-item label="方法">{{ selectedFlow.method }}</a-descriptions-item>
                                        <a-descriptions-item label="域名">{{ selectedFlow.host }}</a-descriptions-item>
                                        <a-descriptions-item label="路径">{{ selectedFlow.path }}</a-descriptions-item>
                                        <a-descriptions-item label="状态">{{ selectedFlow.statusCode }}
                                            {{ selectedFlow.status }}</a-descriptions-item>
                                        <a-descriptions-item label="应用程序">{{ selectedFlow.process || '-' }} (PID
                                            {{ selectedFlow.pid }})</a-descriptions-item>
                                        <a-descriptions-item label="响应类型">
                                            {{ selectedFlow.contentType || '-' }}
                                        </a-descriptions-item>
                                        <a-descriptions-item label="请求/响应大小">{{ formatSize(selectedFlow.reqSize) }} /
                                            {{ formatSize(selectedFlow.respSize) }}</a-descriptions-item>
                                        <a-descriptions-item label="耗时">{{ selectedFlow.duration }}
                                            ms</a-descriptions-item>
                                    </a-descriptions>
                                </div>

                                <div v-show="detailTab === 'reqHeaders'" class="detail-view">
                                    <a-descriptions v-if="headerEntries(selectedFlow.requestHeaders).length" :column="1"
                                        size="small" bordered>
                                        <a-descriptions-item v-for="hd in headerEntries(selectedFlow.requestHeaders)"
                                            :key="hd.k" :label="hd.k">
                                            <span class="hdr-val">{{ hd.v }}</span>
                                        </a-descriptions-item>
                                    </a-descriptions>
                                    <a-empty v-else description="（无请求头）" />
                                </div>

                                <div v-show="detailTab === 'reqBody'" class="detail-view detail-view-fill">
                                    <body-viewer :body="selectedFlow.requestBody" />
                                </div>

                                <div v-show="detailTab === 'respHeaders'" class="detail-view">
                                    <a-descriptions v-if="headerEntries(selectedFlow.responseHeaders).length"
                                        :column="1" size="small" bordered>
                                        <a-descriptions-item v-for="hd in headerEntries(selectedFlow.responseHeaders)"
                                            :key="hd.k" :label="hd.k">
                                            <span class="hdr-val">{{ hd.v }}</span>
                                        </a-descriptions-item>
                                    </a-descriptions>
                                    <a-empty v-else description="（无响应头）" />
                                </div>

                                <div v-show="detailTab === 'respBody'" class="detail-view detail-view-fill">
                                    <body-viewer :body="selectedFlow.responseBody"
                                        :content-type="selectedFlow.contentType" />
                                </div>
                            </template>
                            <div v-else class="detail-empty">
                                <a-empty description="选择左侧请求查看详情" />
                            </div>
                        </div>
                    </a-card>
                </a-col>
            </a-row>
        </a-col>
    </a-row>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, onUnmounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import type { TableData } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import type { Flow } from "bindings/go-tools/tools/sniffer/models";

const {
    proxy: { $GoTools },
} = getCurrentInstance() as any;

/** 协议 Tab */
const PROTO_TABS = [
    { key: "All", label: "All" },
    { key: "HTTP", label: "HTTP" },
    { key: "HTTPS", label: "HTTPS" },
    { key: "WS", label: "WebSocket" },
];

/* ============ 状态 ============ */
const port = ref(8888);
const running = ref(false);
const flows = ref<Flow[]>([]);
const MAX_FLOWS = 3000;

const selectedProto = ref("All");
const selectedProcess = ref("");
const selectedDomain = ref("");
const keyword = ref("");
const sideSearch = ref("");
const filterTab = ref("app");
const detailTab = ref("overview");

const selectedId = ref<number | null>(null);
const page = ref(1);
const pageSize = 200;

let offEvent: (() => void) | null = null;

/* ============ 捕获控制 ============ */
const onStart = async () => {
    try {
        await $GoTools.StartSnifferProxy(port.value);
        running.value = true;
        Message.success("抓包已启动，系统代理已接管");
    } catch (e: any) {
        Message.error("启动失败：" + (e?.message || e));
    }
};

const onStop = async () => {
    try {
        await $GoTools.StopSnifferProxy();
        running.value = false;
        Message.success("抓包已停止，系统代理已还原");
    } catch (e: any) {
        Message.error("停止失败：" + (e?.message || e));
    }
};

const clearAll = () => {
    flows.value = [];
    selectedId.value = null;
};

const onFlow = (ev: any) => {
    const f = ev.data as Flow;
    if (!f) return;
    flows.value.push(f);
    if (flows.value.length > MAX_FLOWS) {
        flows.value.splice(0, flows.value.length - MAX_FLOWS);
    }
};

onMounted(async () => {
    offEvent = Events.On("sniffer:flow", onFlow);
    try {
        const st = await $GoTools.SnifferProxyStatus();
        if (Array.isArray(st)) {
            running.value = !!st[0];
            if (st[1]) port.value = st[1];
        }
    } catch {
        /* 忽略状态查询失败 */
    }
});

onUnmounted(() => {
    if (offEvent) offEvent();
});

/* ============ 筛选与统计 ============ */
const matchKeyword = (f: Flow) => {
    if (!keyword.value) return true;
    const k = keyword.value.toLowerCase();
    return f.url?.toLowerCase().includes(k) || f.host?.toLowerCase().includes(k);
};

const filteredFlows = computed(() =>
    flows.value.filter(
        (f) =>
            (selectedProto.value === "All" || f.protocol === selectedProto.value) &&
            (!selectedProcess.value || f.process === selectedProcess.value) &&
            (!selectedDomain.value || f.host === selectedDomain.value) &&
            matchKeyword(f)
    )
);

/** 筛选条件变化后回到第一页，避免停留在越界空页 */
watch([selectedProto, selectedProcess, selectedDomain, keyword], () => {
    page.value = 1;
});

const pagedFlows = computed(() => {
    const start = (page.value - 1) * pageSize;
    return filteredFlows.value.slice(start, start + pageSize);
});

const processList = computed(() => buildCount("process"));
const domainList = computed(() => {
    const kw = sideSearch.value.toLowerCase();
    return buildCount("host").filter((d) => !kw || d.name.toLowerCase().includes(kw));
});

function buildCount(field: "process" | "host") {
    const map = new Map<string, number>();
    for (const f of flows.value) {
        const key = (f[field] || "").trim() || "(未知)";
        map.set(key, (map.get(key) || 0) + 1);
    }
    const kw = field === "process" ? sideSearch.value.toLowerCase() : "";
    let arr = Array.from(map, ([name, count]) => ({ name, count }));
    if (kw) arr = arr.filter((x) => x.name.toLowerCase().includes(kw));
    return arr.sort((a, b) => b.count - a.count);
}

const protoCount = (key: string) => {
    if (key === "All") return flows.value.length;
    return flows.value.filter((f) => f.protocol === key).length;
};

const selectedFlow = computed(() => flows.value.find((f) => f.id === selectedId.value) || null);

const onRowClick = (record: TableData) => {
    selectedId.value = (record as Flow).id;
};

const toggleProcess = (name: string) => (selectedProcess.value = selectedProcess.value === name ? "" : name);
const toggleDomain = (name: string) => (selectedDomain.value = selectedDomain.value === name ? "" : name);

/* ============ 展示辅助 ============ */
const formatSize = (n: number) => {
    if (!n) return "0 B";
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    return `${(n / 1024 / 1024).toFixed(1)} MB`;
};

const protoColor = (p: string) => (p === "HTTPS" ? "green" : p === "WS" ? "purple" : "arcoblue");

const statusColor = (code: number) => {
    if (!code) return "var(--color-text-3)";
    if (code < 300) return "rgb(var(--green-6))";
    if (code < 400) return "rgb(var(--arcoblue-6))";
    if (code < 500) return "rgb(var(--orange-6))";
    return "rgb(var(--red-6))";
};

/* ============ 详情美化辅助 ============ */
type Hdrs = Record<string, string[] | null | undefined> | null | undefined;

/** 把请求/响应头映射转为 [{ 名, 值 }] 列表 */
const headerEntries = (hObj: Hdrs) =>
    Object.entries(hObj || {}).map(([k, v]) => ({ k, v: (v || []).join(", ") }));
</script>

<style scoped>
/* 页面根容器：a-row 铺满工具窗口内容区（.win-content / .layout-content 有确定高度） */
.sniffer-page {
    height: 100%;
    min-height: 0;
    flex-wrap: nowrap;
    background: var(--color-fill-1);
}

/* 左侧列（a-col，约 30% 宽）：上下两张卡片各占 50% 高 */
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

/* 左侧卡片：标题走 #title、操作走 #extra，body 弹性填充 */
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

.side-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--base-padding);
    padding: var(--base-padding);
    overflow-y: auto;
}

.field {
    display: flex;
    flex-direction: column;
    gap: 4px;
}

.field-label {
    color: var(--color-text-2);
    font-size: var(--base-size-small);
    font-weight: 500;
}

.field-actions {
    display: flex;
    flex-direction: column;
    gap: var(--base-radius-default);
}

.led {
    display: inline-block;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: rgb(var(--red-6));
    box-shadow: 0 0 4px rgba(245, 63, 63, 0.6);
    vertical-align: middle;
}

.led.on {
    background: rgb(var(--green-6));
    box-shadow: 0 0 6px rgba(0, 180, 42, 0.7);
}

.stat-desc :deep(.arco-descriptions-item-label) {
    font-size: var(--base-size-small);
}

.stat-desc :deep(.arco-descriptions-item-value) {
    font-size: var(--base-size-small);
}

.mono {
    font-family: Consolas, Monaco, "Courier New", monospace;
}

.ca-tip {
    margin-top: auto;
    color: var(--color-text-3);
    font-size: var(--base-size-small);
    line-height: 1.6;
}

/* 筛选卡片：搜索 + 应用/域名列表 */
.filter-body {
    gap: var(--base-radius-default);
    padding: 0;
}

.filter-view {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.filter-list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
}

.filter-list :deep(.arco-list-item) {
    padding: 0;
    cursor: pointer;
}

.filter-list :deep(.arco-list-item:hover) {
    background: var(--color-fill-2);
}

.filter-item {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap:var(--base-padding);
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

.filter-item.active {
    color: rgb(var(--primary-6));
}

.filter-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

/* 右侧主列：请求列表 + 请求详情 */
.main-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.main-row {
    flex: 1;
    min-height: 0;
}

.pane-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

/* 列表与详情之间的 6px 间距（与 redis 的 gutter 一致） */
.pane-col+.pane-col {
    padding-left: 6px;
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

/* 请求列表：工具栏 + 表格 + 分页 */
.list-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.list-toolbar {
    flex: none;
    display: flex;
    align-items: center;
    gap: var(--base-padding);
    padding: var(--base-padding);
    border-bottom: 1px solid var(--color-border-1);
}

.list-search {
    flex: 1;
    min-width: 0;
}

.count-tip {
    flex: none;
    color: var(--color-text-3);
    font-size: var(--base-size-small);
}

.pcnt {
    margin-left: 4px;
    opacity: 0.8;
}

.flow-table {
    flex: 1;
    min-height: 0;
    overflow: hidden;
}

/* a-table 内部逐层撑满：表头固定、表体滚动 */
.flow-table :deep(.arco-spin) {
    height: 100%;
    display: flex;
    flex-direction: column;
}

.flow-table :deep(.arco-table-container) {
    flex: 1;
    min-height: 0;
}

.flow-table :deep(.arco-table-body) {
    flex: 1;
    min-height: 0;
}

:deep(.row-selected) {
    background-color: var(--color-primary-light-1) !important;
}

.pager {
    flex: none;
    padding: 6px var(--base-padding);
    border-top: 1px solid var(--color-border-1);
    display: flex;
    justify-content: flex-end;
}

/* 请求详情：URL + 子视图切换 + 可滚动内容 */
.detail-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--base-radius-default);
    padding: var(--base-padding);
}

.detail-head {
    flex: none;
}

.detail-url {
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--base-size-small);
    color: var(--color-text-1);
}

.detail-nav {
    flex: none;
}

.detail-view {
    flex: 1;
    min-height: 0;
    overflow: auto;
}

/* 报文视图：交给 body-viewer 自己滚动（行号栏与内容同步） */
.detail-view-fill {
    overflow: hidden;
    display: flex;
    flex-direction: column;
}

.detail-empty {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
}

.hdr-val {
    word-break: break-all;
    white-space: pre-wrap;
}
</style>
