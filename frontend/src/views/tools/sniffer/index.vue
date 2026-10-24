<template>
    <a-row class="sniffer-page" :gutter="6">
        <!-- 左侧约 30%：抓包控制（上50%）+ 筛选（下50%） -->
        <a-col :span="5" class="side-col">
            <a-row class="side-pages">
                <a-col class="side-cols side-cols-top" :span="24">
                    <a-card class="side-card">
                        <template #title>
                            <a-space :size="6">
                                <a-tag size="small" :color="running ? 'green' : 'gray'">
                                    {{ running ? "捕获中" : "未开始" }}
                                </a-tag>
                                <div class="led" :class="{ on: running }"></div>
                                <div>抓包控制</div>
                            </a-space>
                        </template>
                        <template #extra>
                            <a-space>
                                <a-button size="small" :disabled="!flows.length" @click="clearAll">
                                    <template #icon><icon-delete /></template>
                                </a-button>
                                <a-button size="small" v-if="!running" @click="onStart">
                                    <template #icon><icon-play-arrow /></template>
                                </a-button>
                                <a-button size="small" v-else @click="onStop">
                                    <template #icon><icon-pause /></template>
                                </a-button>
                            </a-space>
                        </template>
                        <a-row :gutter="[12, 12]">
                            <a-col :span="24">
                                <a-input-number v-model="port" :min="1" :max="65535" :disabled="running" hide-button>
                                    <template #prefix>代理端口:</template>
                                </a-input-number>
                            </a-col>
                            <a-col :span="24">
                                <a-descriptions :column="2" size="medium" bordered class="stat-desc">
                                    <a-descriptions-item label="总数">{{ flows.length }}</a-descriptions-item>
                                    <a-descriptions-item label="显示">{{ filteredFlows.length }}</a-descriptions-item>
                                    <a-descriptions-item label="监听" :span="2">
                                        <span class="mono">127.0.0.1:{{ port }}</span>
                                    </a-descriptions-item>
                                </a-descriptions>
                            </a-col>
                            <a-col :span="24">
                                <div class="ca-tip">
                                    <icon-info-circle /> 开始后会安装本地根证书并临时接管系统代理（停止时自动还原），用于解密 HTTPS 流量。
                                </div>
                            </a-col>
                        </a-row>
                    </a-card>
                </a-col>
                <a-col class="side-cols  side-cols-bottom" :span="24">
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
                                <a-list :bordered="false" size="small" class="filter-list" hoverable>
                                    <a-list-item @click="selectedProcess = ''" :class="{ active: !selectedProcess }">
                                        <span class="filter-name">全部</span>
                                        <template #actions>
                                            <a-tag size="small" color="gray">{{ processFacet.length }}</a-tag>
                                        </template>
                                    </a-list-item>
                                    <a-list-item v-for="p in processList" :key="p.name" @click="toggleProcess(p.name)"
                                        :class="{ active: selectedProcess === p.name }">
                                        <span class="filter-name" :title="p.name">{{ p.name }}</span>
                                        <template #actions>
                                            <a-tag size="small" color="gray">{{ p.count }}</a-tag>
                                        </template>
                                    </a-list-item>
                                </a-list>
                            </div>
                            <div v-show="filterTab === 'domain'" class="filter-view">
                                <a-list :bordered="false" size="small" class="filter-list">
                                    <a-list-item @click="selectedDomain = ''" :class="{ active: !selectedDomain }">
                                        <span class="filter-name">全部</span>
                                        <template #actions>
                                            <a-tag size="small" color="gray">{{ domainFacet.length }}</a-tag>
                                        </template>
                                    </a-list-item>
                                    <a-list-item v-for="d in domainList" :key="d.name" @click="toggleDomain(d.name)"
                                        :class="{ active: selectedDomain === d.name }">
                                        <span class="filter-name" :title="d.name">{{ d.name }}</span>
                                        <template #actions>
                                            <a-tag size="small" color="gray">{{ d.count }}</a-tag>
                                        </template>
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
                                <a-tag size="small">共 {{ scopedFlows.length }}</a-tag>
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
                            <!-- 内容类型 / 请求方法 / 状态码快捷筛选：单选 radio，带命中数量，再选 All 取消 -->
                            <div class="type-filters">
                                <a-radio-group v-model="selectedKind" type="button">
                                    <a-radio value="">All<span class="pcnt">{{ scopedFlows.length }}</span></a-radio>
                                    <a-radio v-for="t in TYPE_TABS" :key="t.key" :value="t.key">
                                        {{ t.label }}<span v-if="kindCounts[t.key]" class="pcnt">{{ kindCounts[t.key]
                                        }}</span>
                                    </a-radio>
                                </a-radio-group>
                                <a-radio-group v-model="selectedMethod" type="button">
                                    <a-radio value="">All<span class="pcnt">{{ scopedFlows.length }}</span></a-radio>
                                    <a-radio v-for="m in methodList" :key="m.name" :value="m.name">
                                        {{ m.name }}<span v-if="m.count" class="pcnt">{{ m.count }}</span>
                                    </a-radio>
                                </a-radio-group>
                                <a-radio-group v-model="selectedStatus" type="button">
                                    <a-radio value="">All</a-radio>
                                    <a-radio v-for="s in STATUS_TABS" :key="s" :value="s">
                                        {{ s }}<span v-if="statusCounts[s]" class="pcnt">{{ statusCounts[s] }}</span>
                                    </a-radio>
                                </a-radio-group>
                            </div>
                            <div class="list-toolbar">
                                <a-input-search v-model="keyword" placeholder="搜索 URL / 域名" allow-clear
                                    class="list-search" />
                                <span class="count-tip">显示 {{ filteredFlows.length }} 条</span>
                            </div>
                            <a-table class="flow-table" :data="pagedFlows" row-key="id" size="small" :pagination="false"
                                :scroll="{ y: '100%', x: '100%', minWidth: 720 }" :bordered="{ cell: true }"
                                :column-resizable="true" :row-class="(r) => (r.id === selectedId ? 'row-selected' : '')"
                                @row-click="onRowClick">
                                <template #columns>
                                    <a-table-column title="ID" data-index="id" :width="56" />
                                    <a-table-column title="图标" :width="52" align="center">
                                        <template #cell="{ record }">
                                            <component :is="kindIcon(record)" class="kind-icon"
                                                :style="{ color: kindColor(record) }" :title="kindLabel(record)" />
                                        </template>
                                    </a-table-column>
                                    <a-table-column title="协议" data-index="protocol" :width="72">
                                        <template #cell="{ record }">
                                            <a-tag size="small" :color="protoColor(record.protocol)">
                                                {{ record.protocol }}
                                            </a-tag>
                                        </template>
                                    </a-table-column>
                                    <a-table-column title="方法" data-index="method" :width="80" />
                                    <a-table-column title="URL" data-index="url" :ellipsis="true" :tooltip="true" />
                                    <a-table-column title="应用程序" data-index="process" :width="110" :ellipsis="true"
                                        :tooltip="true" />
                                    <a-table-column title="状态" data-index="statusCode" :width="64">
                                        <template #cell="{ record }">
                                            <span :style="{ color: statusColor(record.statusCode) }">
                                                {{ record.statusCode }}
                                            </span>
                                        </template>
                                    </a-table-column>
                                    <a-table-column title="大小" :width="78">
                                        <template #cell="{ record }">
                                            <span class="text-grey">{{ formatSize(record.respSize) }}</span>
                                        </template>
                                    </a-table-column>
                                    <a-table-column title="耗时" :width="78">
                                        <template #cell="{ record }">
                                            <span class="text-grey">{{ record.duration }}ms</span>
                                        </template>
                                    </a-table-column>
                                </template>
                            </a-table>
                            <div class="pager">
                                <common-pager :list-page="listPage" @change="onPageChange" />
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
                                    <a-descriptions :column="1" bordered>
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
                                        <a-descriptions-item label="内容分类">{{ kindLabel(selectedFlow) || '-' }}
                                        </a-descriptions-item>
                                        <a-descriptions-item label="请求/响应大小">{{ formatSize(selectedFlow.reqSize) }} /
                                            {{ formatSize(selectedFlow.respSize) }}</a-descriptions-item>
                                        <a-descriptions-item label="耗时">{{ selectedFlow.duration }}
                                            ms</a-descriptions-item>
                                    </a-descriptions>
                                </div>

                                <div v-show="detailTab === 'reqHeaders'" class="detail-view">
                                    <a-descriptions v-if="headerEntries(selectedFlow.requestHeaders).length" :column="1"
                                        bordered>
                                        <a-descriptions-item v-for="hd in headerEntries(selectedFlow.requestHeaders)"
                                            :key="hd.k" :label="hd.k">
                                            <span class="hdr-val">{{ hd.v }}</span>
                                        </a-descriptions-item>
                                    </a-descriptions>
                                    <a-empty v-else description="（无请求头）" />
                                </div>

                                <div v-show="detailTab === 'reqBody'" class="detail-view detail-view-fill">
                                    <body-viewer :body="selectedFlow.requestBody"
                                        :content-type="reqContentType(selectedFlow)"
                                        :encoding="selectedFlow.reqBodyEncoding" :raw-size="selectedFlow.reqSize"
                                        :filename="bodyFilename(selectedFlow)" />
                                </div>

                                <div v-show="detailTab === 'respHeaders'" class="detail-view">
                                    <a-descriptions v-if="headerEntries(selectedFlow.responseHeaders).length"
                                        :column="1" bordered>
                                        <a-descriptions-item v-for="hd in headerEntries(selectedFlow.responseHeaders)"
                                            :key="hd.k" :label="hd.k">
                                            <span class="hdr-val">{{ hd.v }}</span>
                                        </a-descriptions-item>
                                    </a-descriptions>
                                    <a-empty v-else description="（无响应头）" />
                                </div>

                                <div v-show="detailTab === 'respBody'" class="detail-view detail-view-fill">
                                    <body-viewer :body="selectedFlow.responseBody"
                                        :content-type="selectedFlow.contentType"
                                        :encoding="selectedFlow.respBodyEncoding" :raw-size="selectedFlow.respSize"
                                        :truncated="selectedFlow.respTruncated"
                                        :filename="bodyFilename(selectedFlow)" />
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
import { useSetting } from "@/hooks/useSetting";
import { getStorage, setStorage } from "@/utils";
import CacheKey from "@/constants/cache-key";

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

/* ============ 内容类型分类（筛选 chips + 列表图标） ============ */
type KindKey = "json" | "xml" | "text" | "html" | "js" | "image" | "media" | "binary";

const TYPE_TABS: { key: KindKey; label: string }[] = [
    { key: "json", label: "JSON" },
    { key: "xml", label: "XML" },
    { key: "text", label: "文本" },
    { key: "html", label: "HTML" },
    { key: "js", label: "JS" },
    { key: "image", label: "图片" },
    { key: "media", label: "媒体" },
    { key: "binary", label: "二进制" },
];
const STATUS_TABS = ["3xx", "4xx", "5xx"] as const;

const KIND_LABELS: Record<KindKey, string> = {
    json: "JSON", xml: "XML", text: "文本", html: "HTML", js: "JS/CSS",
    image: "图片", media: "媒体", binary: "二进制",
};

const KIND_ICONS: Record<KindKey, string> = {
    json: "icon-code",
    xml: "icon-code-block",
    text: "icon-file",
    html: "icon-launch",
    js: "icon-code-square",
    image: "icon-file-image",
    media: "icon-file-video",
    binary: "icon-storage",
};

const KIND_COLORS: Record<KindKey, string> = {
    json: "rgb(var(--green-6))",
    xml: "rgb(var(--orange-6))",
    text: "var(--color-text-3)",
    html: "rgb(var(--red-6))",
    js: "rgb(var(--gold-6))",
    image: "rgb(var(--purple-6))",
    media: "rgb(var(--pinkpurple-6))",
    binary: "rgb(var(--arcoblue-6))",
};

/* ============ 状态 ============ */
const port = ref(8888);
const running = ref(false);
const flows = ref<Flow[]>([]);
const MAX_FLOWS = 3000;

const selectedProto = ref("All");
const selectedProcess = ref("");
const selectedDomain = ref("");
const selectedKind = ref<KindKey | "">("");
const selectedMethod = ref("");
const selectedStatus = ref<(typeof STATUS_TABS)[number] | "">("");
const keyword = ref("");
const sideSearch = ref("");
const filterTab = ref("app");
const detailTab = ref("overview");

const selectedId = ref<number | null>(null);
const curPage = ref(1);
const pageSize = ref(useSetting().PageLimit.value || 10);

let offEvent: (() => void) | null = null;

/* ============ 筛选状态持久化 ============ */
/** 窗口关闭重开后恢复上次的筛选/视图状态（同源 localStorage 跨窗口共享） */
const restoreFilterState = () => {
    const s = getStorage(CacheKey.SNIFFER_FILTER);
    if (!s) return;
    if (typeof s.port === "number" && s.port > 0) port.value = s.port;
    selectedProto.value = s.proto || "All";
    selectedKind.value = s.kind || "";
    selectedMethod.value = s.method || "";
    selectedStatus.value = s.status || "";
    keyword.value = s.keyword || "";
    filterTab.value = s.tab || "app";
    selectedProcess.value = s.process || "";
    selectedDomain.value = s.domain || "";
    detailTab.value = s.detail || "overview";
};

watch(
    [port, selectedProto, selectedKind, selectedMethod, selectedStatus, keyword, filterTab, selectedProcess, selectedDomain, detailTab],
    () => {
        setStorage(CacheKey.SNIFFER_FILTER, {
            port: port.value, proto: selectedProto.value, kind: selectedKind.value,
            method: selectedMethod.value,
            status: selectedStatus.value, keyword: keyword.value, tab: filterTab.value,
            process: selectedProcess.value, domain: selectedDomain.value, detail: detailTab.value,
        });
    }
);

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
        // 停止即清：后台缓冲已随服务停止销毁，视图同步清空保持一致
        flows.value = [];
        selectedId.value = null;
        Message.success("抓包已停止，系统代理已还原，抓取数据已清空");
    } catch (e: any) {
        Message.error("停止失败：" + (e?.message || e));
    }
};

const clearAll = async () => {
    flows.value = [];
    selectedId.value = null;
    // 同步清后端缓冲，否则重开窗口时旧数据会被回填复现
    try {
        await $GoTools.ClearSnifferFlows();
    } catch {
        /* 忽略 */
    }
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
    // 先恢复上次筛选状态，再注册监听：页面关闭期间 Go 侧持续缓冲，重开不丢数据
    restoreFilterState();
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
    // 回填历史：与订阅后已到达的实时事件按 id 去重（id 仅在单次服务会话内唯一）
    if (running.value) {
        try {
            const hist = ((await $GoTools.GetSnifferFlows()) || []) as Flow[];
            if (hist.length) {
                const cur = new Set(flows.value.map((f) => f.id));
                const merged = hist.filter((f) => !cur.has(f.id)).concat(flows.value);
                flows.value = merged.length > MAX_FLOWS ? merged.slice(merged.length - MAX_FLOWS) : merged;
            }
        } catch {
            /* 回填失败不影响实时捕获 */
        }
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

/**
 * 筛选联动范围（分面计数）：
 * - protoBase：命中应用 + 域名，供协议 Tab 计数（忽略协议自身选择）；
 * - scopedFlows：再叠加协议选择，作为类型/方法/状态计数与列表「共 N」的口径；
 * - processFacet / domainFacet：各自忽略自身维度的选择（否则选中某应用后其他应用计数会被清零，无法切换）。
 * 空进程/空域名统一归一为 (未知)，与列表项显示值一致，保证点击可筛选。
 */
const procOf = (f: Flow) => (f.process || "").trim() || "(未知)";
const hostOf = (f: Flow) => (f.host || "").trim() || "(未知)";

const protoBase = computed(() =>
    flows.value.filter(
        (f) =>
            (!selectedProcess.value || procOf(f) === selectedProcess.value) &&
            (!selectedDomain.value || hostOf(f) === selectedDomain.value)
    )
);
const scopedFlows = computed(() =>
    protoBase.value.filter(
        (f) => selectedProto.value === "All" || f.protocol === selectedProto.value
    )
);
const processFacet = computed(() =>
    flows.value.filter((f) => !selectedDomain.value || hostOf(f) === selectedDomain.value)
);
const domainFacet = computed(() =>
    flows.value.filter((f) => !selectedProcess.value || procOf(f) === selectedProcess.value)
);

/** 请求方法归一（空方法兜底为其他），选项动态取自当前筛选范围内实际出现的方法 */
const methodOf = (f: Flow) => (f.method || "").toUpperCase() || "其他";
const methodList = computed(() => {
    const m = new Map<string, number>();
    for (const f of scopedFlows.value) {
        const k = methodOf(f);
        m.set(k, (m.get(k) || 0) + 1);
    }
    return Array.from(m, ([name, count]) => ({ name, count })).sort(
        (a, b) => b.count - a.count || a.name.localeCompare(b.name)
    );
});

const filteredFlows = computed(() =>
    scopedFlows.value.filter(
        (f) =>
            (!selectedKind.value || kindOf(f) === selectedKind.value) &&
            (!selectedMethod.value || methodOf(f) === selectedMethod.value) &&
            (!selectedStatus.value || statusClass(f.statusCode) === selectedStatus.value) &&
            matchKeyword(f)
    )
);

/** 筛选条件变化后回到第一页，避免停留在越界空页 */
watch([selectedProto, selectedProcess, selectedDomain, selectedKind, selectedMethod, selectedStatus, keyword], () => {
    curPage.value = 1;
});

const pagedFlows = computed(() => {
    const start = (curPage.value - 1) * pageSize.value;
    return filteredFlows.value.slice(start, start + pageSize.value);
});

/** 分页组件数据源 */
const listPage = computed(() => ({
    total: filteredFlows.value.length,
    page: curPage.value,
    limit: pageSize.value,
}));

/** 分页组件页码 / 每页条数变化 */
const onPageChange = (val: { page: number; limit: number }) => {
    curPage.value = val.page;
    pageSize.value = val.limit;
};

const processList = computed(() => buildCount("process", processFacet.value));
const domainList = computed(() => {
    const kw = sideSearch.value.toLowerCase();
    return buildCount("host", domainFacet.value).filter((d) => !kw || d.name.toLowerCase().includes(kw));
});

function buildCount(field: "process" | "host", source: Flow[]) {
    const map = new Map<string, number>();
    for (const f of source) {
        const key = (f[field] || "").trim() || "(未知)";
        map.set(key, (map.get(key) || 0) + 1);
    }
    const kw = field === "process" ? sideSearch.value.toLowerCase() : "";
    let arr = Array.from(map, ([name, count]) => ({ name, count }));
    if (kw) arr = arr.filter((x) => x.name.toLowerCase().includes(kw));
    return arr.sort((a, b) => b.count - a.count);
}

/** 协议命中数：跟随应用/域名筛选，忽略协议自身选择 */
const protoCount = (key: string) => {
    if (key === "All") return protoBase.value.length;
    return protoBase.value.filter((f) => f.protocol === key).length;
};

/** 各内容类型/状态码的命中数：跟随应用/域名/协议筛选（scopedFlows 口径） */
const kindCounts = computed(() => {
    const m: Partial<Record<KindKey, number>> = {};
    for (const f of scopedFlows.value) {
        const k = kindOf(f);
        if (k) m[k] = (m[k] || 0) + 1;
    }
    return m;
});

const statusCounts = computed(() => {
    const m: Record<string, number> = {};
    for (const f of scopedFlows.value) {
        const s = statusClass(f.statusCode);
        if (s) m[s] = (m[s] || 0) + 1;
    }
    return m;
});

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

/** 按响应 Content-Type（及体编码）归类到筛选分类；无法归类返回空串 */
function kindOf(f: Flow): KindKey | "" {
    const main = (f.contentType || "").toLowerCase().split(";")[0].trim();
    if (!main) return "";
    if (main === "image/svg+xml") return "image";
    if (main.includes("json")) return "json";
    if (main.includes("xml")) return "xml";
    if (main === "text/html" || main === "application/xhtml+xml") return "html";
    if (/(javascript|ecmascript|css)/.test(main)) return "js";
    if (main.startsWith("image/")) return "image";
    if (main.startsWith("video/") || main.startsWith("audio/")) return "media";
    if (main.startsWith("text/")) return "text";
    if (f.respBodyEncoding === "base64") return "binary";
    if (/(pdf|zip|octet-stream|protobuf|grpc|wasm|font|x-)/.test(main)) return "binary";
    return "";
}

/** 状态码大类（3xx/4xx/5xx），其余返回空串 */
const statusClass = (code: number) =>
    code >= 300 && code < 400 ? "3xx" : code >= 400 && code < 500 ? "4xx" : code >= 500 ? "5xx" : "";

const kindLabel = (f: Flow) => {
    const k = kindOf(f);
    if (!k) return "";
    if (k === "media") {
        const main = (f.contentType || "").toLowerCase();
        if (main.startsWith("audio")) return "音频";
        if (main.startsWith("image")) return "图片";
        return "视频";
    }
    if (k === "binary" && (f.contentType || "").toLowerCase().includes("pdf")) return "PDF";
    return KIND_LABELS[k];
};

const kindIcon = (f: Flow) => {
    const k = kindOf(f);
    if (!k) return "icon-link";
    if (k === "media") {
        const main = (f.contentType || "").toLowerCase();
        if (main.startsWith("audio")) return "icon-file-audio";
        if (main.startsWith("image")) return "icon-file-image";
        return "icon-file-video";
    }
    if (k === "binary" && (f.contentType || "").toLowerCase().includes("pdf")) return "icon-file-pdf";
    return KIND_ICONS[k];
};

const kindColor = (f: Flow) => {
    const k = kindOf(f);
    return k ? KIND_COLORS[k] : "var(--color-text-3)";
};

/** 取请求头的 Content-Type（头名大小写不敏感） */
const reqContentType = (f: Flow) => {
    const hdrs = f.requestHeaders || {};
    for (const k of Object.keys(hdrs)) {
        if (k.toLowerCase() === "content-type") return (hdrs[k] || [])[0] || "";
    }
    return "";
};

/** 二进制体下载文件名：取 URL 路径末段 */
const bodyFilename = (f: Flow) => {
    try {
        const seg = new URL(f.url).pathname.split("/").filter(Boolean).pop();
        return seg ? decodeURIComponent(seg) : "";
    } catch {
        return "";
    }
};

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
}

/* 左侧列（a-col，约 30% 宽）：上下两张卡片各占 50% 高 */
.side-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.side-pages {
    height: 100%;
}

.side-cols {
    width: 100%;
    display: flex;
    flex-direction: column;
}

.side-cols-top {

    height: 240px;
}

.side-cols-bottom {
    height: calc(100% - 240px);
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

.filter-list :deep(.arco-list-item:hover),
.filter-list :deep(.arco-list-item.active) {
    background: var(--color-fill-2);
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

/* 内容类型/状态码快捷筛选：两枚单选 radio 组，项多时自动换行 */
.type-filters {
    flex: none;
    display: flex;
    align-items: flex-start;
    flex-wrap: wrap;
    gap: 6px 12px;
    padding: var(--base-radius-default) var(--base-padding) 0;
}

.type-filters :deep(.arco-radio-group) {
    display: flex;
    flex-wrap: wrap;
    gap: 4px;
}

/* 换行后连体边框观感不佳，改成每项独立小圆角胶囊 */
.type-filters :deep(.arco-radio-button) {
    border-radius: var(--base-radius-small) !important;
}

/* 列表类型图标 */
.kind-icon {
    font-size: 16px;
    vertical-align: middle;
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

/* 列挤压兜底：Arco 单元格默认 word-break: break-all，列一窄就会把「协议/方法/OPTIONS」
   逐字竖排；固定单行显示，宽度不足时交给横向滚动，而不是压缩列宽 */
.flow-table :deep(.arco-table-th),
.flow-table :deep(.arco-table-td) {
    white-space: nowrap;
}

/* 选中行高亮：row-class 加在 tr 上，但 Arco 的 td 自带背景色会盖住 tr 背景，
   必须直接覆盖 td；同时压掉 hover 背景，避免鼠标滑过时选中高亮被替换 */
.flow-table :deep(.arco-table-tr.row-selected .arco-table-td) {
    background-color:var(--color-fill-2) !important;
}

/* 行可点击查看详情，给出指针提示 */
.flow-table :deep(.arco-table-tr) {
    cursor: pointer;
}

.pager {
    flex: none;
    padding: var(--base-padding);
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
