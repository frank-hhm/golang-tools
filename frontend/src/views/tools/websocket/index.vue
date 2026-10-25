<template>
    <a-row class="ws-page" :gutter="6">
        <!-- 左侧约 30%：连接配置（上50%）+ 发送消息（下50%） -->
        <a-col :span="6" class="side-col">
            <a-row class="ws-pages">
                <a-col class="side-cols" :span="24">
                    <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                        <template #title>连接配置</template>
                        <template #extra>
                            <span class="com-state" :class="statusMeta.cls">
                                <com-badge :status="statusMeta.badge"
                                    :animated="status === 'open' || status === 'connecting'" />
                                {{ statusMeta.text }}
                            </span>
                        </template>
                        <div class="side-body">
                            <a-input v-model="url" placeholder="ws:// 或 wss:// 地址" :disabled="locked" allow-clear
                                @press-enter="toggleConnect">
                                <template #prefix><icon-link /></template>
                            </a-input>
                            <a-input v-model="protocols" placeholder="子协议（可选，逗号分隔）" :disabled="locked" allow-clear />
                            <a-button size="small" v-if="!locked" status="success" long @click="connect">
                                <template #icon><icon-play-arrow /></template>连接
                            </a-button>
                            <a-button size="small" v-else status="danger" long @click="disconnect">
                                <template #icon><icon-poweroff /></template>断开
                            </a-button>
                            <div class="switch-row">
                                <div class="switch-item">
                                    <span>自动重连</span>
                                    <a-switch v-model="autoReconnect" size="small" />
                                </div>
                                <div class="switch-item">
                                    <span>自动滚动</span>
                                    <a-switch v-model="autoScroll" size="small" />
                                </div>
                            </div>
                            <div class="stat-row">
                                <div class="stat">连接时长: <b>{{ duration }}</b></div>
                                <div class="stat">接收: <b class="in">{{ inCount }}</b></div>
                                <div class="stat">发送: <b class="out">{{ outCount }}</b></div>
                            </div>
                        </div>
                    </a-card>
                </a-col>
                <a-col class="side-cols mt6" :span="24">
                    <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                        <template #title>发送消息</template>
                        <template #extra>
                            <a-button size="small" status="success" :disabled="!connected || !sendText"
                                @click="send">
                                <template #icon><icon-send /></template>发送
                            </a-button>
                        </template>
                        <div class="side-body send-body">
                            <a-textarea v-model="sendText" :auto-size="{ minRows: 3, maxRows: 6 }"
                                placeholder="输入要发送的内容（支持文本 / JSON），Ctrl+Enter 快速发送" allow-clear
                                @press-enter.ctrl="send" />
                            <div class="send-actions">
                                <a-select :model-value="undefined" placeholder="插入最近发送" size="small"
                                    :disabled="!sendHistory.length" class="history-select" @change="onPickHistory">
                                    <a-option v-for="(h, i) in sendHistory" :key="i" :value="h">
                                        {{ h.length > 30 ? h.slice(0, 30) + '…' : h }}
                                    </a-option>
                                </a-select>
                                <a-button size="small" @click="sendText = ''" :disabled="!sendText">清空输入</a-button>
                            </div>
                        </div>
                    </a-card>
                </a-col>
            </a-row>
        </a-col>

        <!-- 右侧主区域：实时消息（列表 + 详情）铺满 -->
        <a-col :span="18" class="main-col">
            <a-card class="main-panel card-head-tool" :body-style="{ padding: 0 }">
                <template #title>实时消息</template>
                <template #extra>
                    <a-space :size="8">
                        <a-input v-model="msgSearch" placeholder="搜索消息" size="small" allow-clear class="search-input">
                            <template #prefix><icon-search /></template>
                        </a-input>
                        <a-select v-model="msgFilter" size="small" style="width: 100px">
                            <a-option value="all">全部消息</a-option>
                            <a-option value="in">接收</a-option>
                            <a-option value="out">发送</a-option>
                            <a-option value="sys">系统</a-option>
                            <a-option value="err">错误</a-option>
                        </a-select>
                        <a-button size="small" :disabled="!messages.length" @click="clearMessages">
                            <template #icon><icon-delete /></template>清除消息
                        </a-button>
                    </a-space>
                </template>
                <div class="msg-area">
                    <!-- 左：消息列表 -->
                    <div class="list-pane">
                        <div ref="logRef" class="msg-list">
                            <div v-for="m in filteredMessages" :key="m.id" class="msg-row"
                                :class="{ active: m.id === selectedMsgId }" @click="selectedMsgId = m.id">
                                <component :is="dirIcon(m.dir)" class="row-icon"
                                    :style="{ color: dirIconColor(m.dir) }" />
                                <span class="row-text">{{ preview(m) }}</span>
                                <span class="row-time">{{ m.time.slice(0, 8) }}</span>
                            </div>
                            <div v-if="!filteredMessages.length" class="msg-empty">暂无消息</div>
                        </div>
                    </div>

                    <!-- 右：消息详情 -->
                    <div class="detail-pane">
                        <template v-if="selectedMsg">
                            <div class="detail-toolbar">
                                <a-radio-group v-model="viewMode" type="button" size="medium">
                                    <a-radio value="pretty">美化</a-radio>
                                    <a-radio value="raw">原生</a-radio>
                                </a-radio-group>
                                <a-tag size="small" :color="dirColor(selectedMsg.dir)">{{ dirLabel(selectedMsg.dir)
                                    }}</a-tag>
                                <span class="detail-time">{{ selectedMsg.time }}</span>
                                <a-button size="small" style="margin-left: auto"
                                    @click="copyText(selectedMsg.data)">
                                    <template #icon><icon-copy /></template>复制
                                </a-button>
                            </div>
                            <div class="detail-body">
                                <div v-for="(line, i) in detailLines" :key="i" class="code-line">
                                    <span class="ln">{{ i + 1 }}</span><span class="lc">{{ line }}</span>
                                </div>
                            </div>
                        </template>
                        <a-empty v-else description="选择左侧消息查看详情" class="detail-empty" />
                    </div>
                </div>
            </a-card>
        </a-col>
    </a-row>
</template>

<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";

/** 连接状态 */
type Status = "idle" | "connecting" | "open" | "closed";
/** 消息方向：接收 / 发送 / 系统 / 错误 */
type MsgDir = "in" | "out" | "sys" | "err";
interface WsMsg {
    id: number;
    dir: MsgDir;
    time: string;
    data: string;
}

/* ============ 表单与状态 ============ */
const url = ref("wss://echo.websocket.events");
const protocols = ref("");
const status = ref<Status>("idle");
const autoReconnect = ref(false);
const autoScroll = ref(true);

/* ============ 消息列表 / 详情状态 ============ */
const msgSearch = ref("");
const msgFilter = ref<"all" | MsgDir>("all");
const selectedMsgId = ref<number | null>(null);
const viewMode = ref<"pretty" | "raw">("pretty");

const messages = ref<WsMsg[]>([]);
const MAX_MSG = 500;

const sendText = ref("");
const sendHistory = ref<string[]>([]);

/** WebSocket 实例（非响应式，避免 Proxy 包裹导致原生方法异常） */
let ws: WebSocket | null = null;
let msgSeq = 0;
let manualClose = false;
let reconnectTimer: number | null = null;

const logRef = ref<HTMLElement | null>(null);

/* ============ 计时 ============ */
const connectedAt = ref(0);
const nowTick = ref(Date.now());
let tickTimer: number | null = null;
const startTick = () => {
    if (tickTimer) return;
    tickTimer = window.setInterval(() => (nowTick.value = Date.now()), 1000);
};
const stopTick = () => {
    if (tickTimer) {
        clearInterval(tickTimer);
        tickTimer = null;
    }
};

/* ============ 计算属性 ============ */
const connected = computed(() => status.value === "open");
const locked = computed(() => status.value === "connecting" || status.value === "open");

const statusMeta = computed<{
    badge: "success" | "warning" | "danger" | "normal";
    cls: string;
    text: string;
}>(() => {
    switch (status.value) {
        case "open":
            return { badge: "success", cls: "is-on", text: "已连接" };
        case "connecting":
            return { badge: "warning", cls: "is-warn", text: "连接中" };
        case "closed":
            return { badge: "normal", cls: "is-dim", text: "已断开" };
        default:
            return { badge: "danger", cls: "is-off", text: "未连接" };
    }
});

const duration = computed(() => {
    if (!connectedAt.value) return "00:00:00";
    const s = Math.max(0, Math.floor((nowTick.value - connectedAt.value) / 1000));
    const p = (n: number) => String(n).padStart(2, "0");
    return `${p(Math.floor(s / 3600))}:${p(Math.floor((s % 3600) / 60))}:${p(s % 60)}`;
});

const inCount = computed(() => messages.value.filter((m) => m.dir === "in").length);
const outCount = computed(() => messages.value.filter((m) => m.dir === "out").length);

/* ============ 展示辅助 ============ */
const dirLabel = (d: MsgDir) => ({ in: "接收", out: "发送", sys: "系统", err: "错误" }[d]);
const dirColor = (d: MsgDir) => ({ in: "arcoblue", out: "green", sys: "gray", err: "red" }[d]);

const nowTime = () => {
    const d = new Date();
    const p = (n: number, l = 2) => String(n).padStart(l, "0");
    return `${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}.${p(d.getMilliseconds(), 3)}`;
};

/** JSON 美化，失败则原样返回 */
const pretty = (s: string) => {
    try {
        return JSON.stringify(JSON.parse(s), null, 2);
    } catch {
        return s;
    }
};

/* ============ 消息列表 / 详情计算 ============ */
const filteredMessages = computed(() => {
    const kw = msgSearch.value.trim().toLowerCase();
    return messages.value.filter(
        (m) =>
            (msgFilter.value === "all" || m.dir === msgFilter.value) &&
            (!kw || m.data.toLowerCase().includes(kw) || m.time.includes(kw))
    );
});
const selectedMsg = computed(() => messages.value.find((m) => m.id === selectedMsgId.value) || null);
const detailLines = computed(() => {
    const m = selectedMsg.value;
    if (!m) return [];
    const text =
        viewMode.value === "pretty" && (m.dir === "in" || m.dir === "out") ? pretty(m.data) : m.data;
    return text.split("\n");
});

const dirIcon = (d: MsgDir) =>
    ({ in: "icon-arrow-down", out: "icon-arrow-up", sys: "icon-check-circle", err: "icon-close-circle" }[d]);
const dirIconColor = (d: MsgDir) =>
    ({ in: "rgb(var(--primary-6))", out: "rgb(var(--green-6))", sys: "rgb(var(--green-6))", err: "rgb(var(--red-6))" }[d]);

/** 列表行预览：单行摘要 */
const preview = (m: WsMsg) => {
    const one = m.data.replace(/\s+/g, " ").trim();
    return one.length > 80 ? one.slice(0, 80) + "…" : one;
};

const copyText = async (text: string) => {
    try {
        await navigator.clipboard.writeText(text);
        Message.success("已复制到剪贴板");
    } catch {
        Message.error("复制失败");
    }
};

const push = (dir: MsgDir, data: string) => {
    const id = ++msgSeq;
    messages.value.push({ id, dir, time: nowTime(), data });
    if (selectedMsgId.value === null) selectedMsgId.value = id;
    if (messages.value.length > MAX_MSG) {
        messages.value.splice(0, messages.value.length - MAX_MSG);
    }
};

/* ============ 连接控制 ============ */
const connect = () => {
    if (ws) return;
    const u = url.value.trim();
    if (!u) {
        Message.warning("请输入 WebSocket 地址");
        return;
    }
    if (!/^wss?:\/\//i.test(u)) {
        Message.warning("地址需以 ws:// 或 wss:// 开头");
        return;
    }
    manualClose = false;
    status.value = "connecting";
    push("sys", `正在连接 ${u} ...`);
    try {
        const protos = protocols.value
            .split(",")
            .map((x) => x.trim())
            .filter(Boolean);
        ws = protos.length ? new WebSocket(u, protos) : new WebSocket(u);
        ws.binaryType = "arraybuffer";
    } catch (e: any) {
        status.value = "idle";
        ws = null;
        push("err", "创建连接失败：" + (e?.message || e));
        Message.error("创建连接失败");
        return;
    }

    ws.onopen = () => {
        status.value = "open";
        connectedAt.value = Date.now();
        nowTick.value = Date.now();
        startTick();
        push("sys", "连接已建立");
        Message.success("已连接");
    };
    ws.onmessage = (ev: MessageEvent) => {
        if (typeof ev.data === "string") {
            push("in", ev.data);
        } else if (ev.data instanceof ArrayBuffer) {
            push("in", `[二进制数据 ${ev.data.byteLength} bytes]`);
        } else {
            push("in", "[二进制数据]");
        }
    };
    ws.onerror = () => {
        push("err", "连接发生错误");
    };
    ws.onclose = (ev: CloseEvent) => {
        status.value = "closed";
        connectedAt.value = 0;
        stopTick();
        ws = null;
        const abnormal = !manualClose && ev.code !== 1000;
        push(abnormal ? "err" : "sys", `连接已关闭（code=${ev.code}${ev.reason ? "，" + ev.reason : ""}）`);
        if (autoReconnect.value && !manualClose) {
            scheduleReconnect();
        }
    };
};

const disconnect = () => {
    manualClose = true;
    if (reconnectTimer) {
        clearTimeout(reconnectTimer);
        reconnectTimer = null;
    }
    if (ws) {
        ws.close();
    } else {
        status.value = "idle";
    }
};

const toggleConnect = () => (locked.value ? disconnect() : connect());

const scheduleReconnect = () => {
    push("sys", "3 秒后自动重连...");
    reconnectTimer = window.setTimeout(() => {
        reconnectTimer = null;
        if (autoReconnect.value && !manualClose) connect();
    }, 3000);
};

/* ============ 发送 ============ */
const send = () => {
    if (!ws || status.value !== "open") {
        Message.warning("未连接，无法发送");
        return;
    }
    const t = sendText.value;
    if (!t) return;
    try {
        ws.send(t);
        push("out", t);
        sendHistory.value = [t, ...sendHistory.value.filter((x) => x !== t)].slice(0, 20);
    } catch (e: any) {
        push("err", "发送失败：" + (e?.message || e));
    }
};

const onPickHistory = (v: any) => {
    if (v != null) sendText.value = String(v);
};

const clearMessages = () => {
    messages.value = [];
    selectedMsgId.value = null;
};

/* ============ 自动滚动 ============ */
watch(
    () => messages.value.length,
    async () => {
        if (!autoScroll.value) return;
        await nextTick();
        if (logRef.value) logRef.value.scrollTop = logRef.value.scrollHeight;
    }
);

/* ============ 卸载清理 ============ */
onUnmounted(() => {
    manualClose = true;
    if (reconnectTimer) clearTimeout(reconnectTimer);
    stopTick();
    if (ws) {
        ws.onclose = null;
        ws.close();
        ws = null;
    }
});
</script>

<style scoped>
/* 页面根容器：a-row 铺满工具窗口内容区（.win-content / .layout-content 有确定高度） */
.ws-page {
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

.ws-pages {
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

/* 左侧卡片内容：纵向排列，超出可滚动 */
.side-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--base-padding);
    padding: var(--base-padding);
    overflow-y: auto;
}

.switch-row {
    display: flex;
    flex-direction: column;
    gap: var(--base-radius-default);
}

.switch-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    color: var(--color-text-2);
    font-size: var(--base-size-small);
}

.stat-row {
    display: flex;
    flex-direction: column;
    gap: 4px;
    margin-top: auto;
}

.stat {
    color: var(--color-text-2);
    font-size: var(--base-size-small);
}

.stat b {
    color: var(--color-text-1);
    font-variant-numeric: tabular-nums;
}

.stat b.in {
    color: rgb(var(--primary-6));
}

.stat b.out {
    color: rgb(var(--green-6));
}

/* 发送卡片：输入后操作行贴底 */
.send-body {
    gap: var(--base-radius-large);
}

.send-actions {
    display: flex;
    align-items: center;
    gap: var(--base-radius-default);
    margin-top: auto;
}

.history-select {
    flex: 1;
    min-width: 0;
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

.search-input {
    width: 180px;
}

/* 消息区：列表 + 详情，铺满主卡片 */
.msg-area {
    flex: 1;
    min-height: 0;
    display: flex;
    gap: var(--base-padding);
    padding: var(--base-padding);
}

.list-pane {
    flex: 0 0 46%;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-large);
    overflow: hidden;
    min-width: 0;
}

.msg-list {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
}

.msg-row {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: var(--base-padding) var(--base-radius-large);
    cursor: pointer;
    border-bottom: 1px solid var(--color-border-1);
    font-size: var(--base-size-small);
}

.msg-row:hover {
    background: var(--color-fill-2);
}

.msg-row.active {
    background: var(--color-primary-light-1);
}

.row-icon {
    flex: 0 0 auto;
    font-size: var(--base-size-default);
}

.row-text {
    flex: 1;
    min-width: 0;
    color: var(--color-text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.row-time {
    flex: 0 0 auto;
    color: var(--color-text-3);
    font-size: var(--base-size-small);
    font-variant-numeric: tabular-nums;
}

.msg-empty {
    color: var(--color-text-3);
    text-align: center;
    padding: 40px 0;
    font-size: var(--base-size-small);
}

.detail-pane {
    flex: 1;
    display: flex;
    flex-direction: column;
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-large);
    overflow: hidden;
    min-width: 0;
}

.detail-toolbar {
    display: flex;
    align-items: center;
    gap: 10px;
    padding: var(--base-padding) var(--base-radius-large);
    border-bottom: 1px solid var(--color-border-2);
}

.detail-time {
    color: var(--color-text-3);
    font-size: var(--base-size-small);
}

.detail-body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    background: #1d2129;
    padding: var(--base-padding) 0;
    font-family: Consolas, Monaco, "Courier New", monospace;
    font-size: var(--base-size-small);
    line-height: 1.6;
}

.code-line {
    display: flex;
}

.ln {
    flex: 0 0 44px;
    text-align: right;
    padding-right: var(--base-padding);
    color: var(--color-text-3);
    user-select: none;
}

.lc {
    flex: 1;
    color: #e6e6e6;
    white-space: pre-wrap;
    word-break: break-all;
    padding-right: var(--base-padding);
}

.detail-empty {
    margin: auto;
}
</style>
