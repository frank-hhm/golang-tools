<template>
    <a-row class="serial-page" :gutter="6">
        <!-- 左侧约 30%：连接配置（上50%）+ 收发选项（下50%） -->
        <a-col :span="6" class="side-col">
            <a-row class="serial-pages">
                <a-col class="side-cols" :span="24">
                    <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                        <template #title>
                            <a-space :size="6">
                                <com-badge :status="st.opened ? 'success' : 'danger'" :animated="st.opened"
                                    :title="st.opened ? '已连接' : '未连接'"></com-badge>
                                <span>连接配置</span>
                            </a-space>
                        </template>
                        <div class="side-body">
                            <div class="field">
                                <div class="field-label">端口 / 模式</div>
                                <a-select v-model="st.selected" size="small" style="width: 100%" :disabled="st.opened"
                                    @change="onSelectChange">
                                    <a-option v-for="p in ports" :key="p.value" :value="p.value">{{ p.label }}</a-option>
                                    <a-option value="TCPClient">TCPClient</a-option>
                                    <a-option value="TCPServer">TCPServer</a-option>
                                    <a-option value="UDP">UDP</a-option>
                                </a-select>
                            </div>

                            <div class="field-actions">
                                <a-button v-if="!st.opened" status="success" size="small" long
                                    @click="openConn">
                                    <template #icon><icon-play-arrow /></template>{{ openLabel }}
                                </a-button>
                                <a-button v-else status="danger" size="small" long @click="closeConn">
                                    <template #icon><icon-poweroff /></template>{{ closeLabel }}
                                </a-button>
                                <a-space v-if="mode === 'serial'" :size="6">
                                    <a-button size="small" @click="loadPorts" :disabled="st.opened">
                                        <template #icon><icon-refresh /></template>刷新
                                    </a-button>
                                    <a-button size="small" @click="settingVisible = true" :disabled="st.opened">
                                        <template #icon><icon-settings /></template>串口设置
                                    </a-button>
                                </a-space>
                            </div>

                            <!-- 串口参数 -->
                            <template v-if="mode === 'serial'">
                                <div class="field">
                                    <div class="field-label">波特率</div>
                                    <a-select v-model="st.baud" size="small" style="width: 100%" :disabled="st.opened">
                                        <a-option v-for="b in BAUDS" :key="b" :value="b">{{ b }}</a-option>
                                    </a-select>
                                </div>
                                <div class="field-inline">
                                    <a-checkbox v-model="st.rts" @change="applyControlBits">RTS</a-checkbox>
                                    <a-checkbox v-model="st.dtr" @change="applyControlBits">DTR</a-checkbox>
                                    <a-tag size="small" color="gray">{{ st.dataBits }}-{{ st.parity }}-{{ st.stopBits }}</a-tag>
                                </div>
                            </template>

                            <!-- 网络参数 -->
                            <template v-else>
                                <div v-if="mode !== 'tcpserver'" class="field">
                                    <div class="field-label">远程地址</div>
                                    <a-input v-model="st.remoteAddr" size="small" style="width: 100%"
                                        :disabled="st.opened" placeholder="192.168.1.20" />
                                    <a-input-number v-model="st.remotePort" size="small" style="width: 100%; margin-top: 6px"
                                        :min="1" :max="65535" :disabled="st.opened" hide-button />
                                </div>
                                <div class="field">
                                    <div class="field-label">本地地址</div>
                                    <a-select v-model="st.localAddr" size="small" style="width: 100%"
                                        :disabled="st.opened" allow-search placeholder="本地IP">
                                        <a-option v-for="ip in localIPs" :key="ip" :value="ip">{{ ip }}</a-option>
                                    </a-select>
                                    <a-input-number v-model="st.localPort" size="small" style="width: 100%; margin-top: 6px"
                                        :min="0" :max="65535" :disabled="st.opened" hide-button />
                                </div>
                            </template>
                        </div>
                    </a-card>
                </a-col>
                <a-col class="side-cols mt6" :span="24">
                    <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                        <template #title>收发选项</template>
                        <div class="side-body">
                            <div class="opt-list">
                                <a-checkbox v-model="st.hexDisplay">HEX显示</a-checkbox>
                                <a-checkbox v-model="st.hexSend">HEX发送</a-checkbox>
                                <a-checkbox v-model="st.addCRLF">加回车换行</a-checkbox>
                                <a-checkbox v-model="st.timestamp">加时间戳和分包显示</a-checkbox>
                                <a-checkbox v-model="st.timerSend">定时发送</a-checkbox>
                            </div>
                            <div class="field-row">
                                <a-input-number v-model="st.timerInterval" size="mini" :min="50" :step="100"
                                    :disabled="!st.timerSend" hide-button style="width: 90px" />
                                <span class="unit">ms/次</span>
                            </div>
                            <div class="field">
                                <div class="field-label">加校验</div>
                                <a-select v-model="st.checksum" size="mini" style="width: 100%">
                                    <a-option v-for="c in CHECKSUM_OPTIONS" :key="c.value" :value="c.value">
                                        {{ c.label }}
                                    </a-option>
                                </a-select>
                            </div>
                        </div>
                    </a-card>
                </a-col>
            </a-row>
        </a-col>

        <!-- 右侧主区域：数据收发（接收铺满 + 底部发送） -->
        <a-col :span="18" class="main-col">
            <a-card class="main-panel card-head-tool" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space :size="8">
                        <span>数据收发</span>
                        <a-tag color="arcoblue" size="small">RX: {{ st.rx }}</a-tag>
                        <a-tag color="green" size="small">TX: {{ st.tx }}</a-tag>
                        <span v-if="st.label" class="conn-tip">{{ st.label }}</span>
                    </a-space>
                </template>
                <template #extra>
                    <a-space :size="8">
                        <a-button size="small" @click="clearRecv">
                            <template #icon><icon-delete /></template>清空接收
                        </a-button>
                        <a-button size="small" :disabled="!recvLines.length" @click="exportRecv">
                            <template #icon><icon-download /></template>保存数据
                        </a-button>
                    </a-space>
                </template>
                <div class="main-body">
                    <div ref="recvRef" class="recv-body" :class="{ hex: st.hexDisplay }">{{ recvText }}</div>
                    <div class="send-area">
                        <div class="field-label">发送数据</div>
                        <a-textarea v-model="sendText" :auto-size="{ minRows: 2, maxRows: 6 }"
                            :placeholder="st.hexSend ? '以 HEX 发送，例如：AA AA 1E AA A6 00 …' : '输入要发送的文本，Ctrl+Enter 发送'"
                            allow-clear @press-enter.ctrl="doSend" />
                        <div class="send-ops">
                            <a-space :size="10">
                                <a-button size="small" @click="sendText = ''" :disabled="!sendText">清空</a-button>
                                <a-button size="small" @click="loadHistory" :disabled="!history.length">
                                    <template #icon><icon-history /></template>历史
                                </a-button>
                            </a-space>
                            <a-button size="small" status="success" :disabled="!st.opened || !sendText"
                                @click="doSend">
                                <template #icon><icon-send /></template>发 送
                            </a-button>
                        </div>
                    </div>
                </div>
            </a-card>
        </a-col>
    </a-row>

    <!-- 串口设置弹窗 -->
    <a-modal v-model:visible="settingVisible" title="串口设置" :width="360" :footer="false">
        <a-form :model="st" layout="vertical" auto-width>
            <a-form-item field="dataBits" label="数据位">
                <a-select v-model="st.dataBits">
                    <a-option v-for="d in [5, 6, 7, 8]" :key="d" :value="d">{{ d }}</a-option>
                </a-select>
            </a-form-item>
            <a-form-item field="parity" label="校验位">
                <a-select v-model="st.parity">
                    <a-option value="none">None</a-option>
                    <a-option value="odd">Odd</a-option>
                    <a-option value="even">Even</a-option>
                    <a-option value="mark">Mark</a-option>
                    <a-option value="space">Space</a-option>
                </a-select>
            </a-form-item>
            <a-form-item field="stopBits" label="停止位">
                <a-select v-model="st.stopBits">
                    <a-option :value="1">1</a-option>
                    <a-option :value="2">2</a-option>
                </a-select>
            </a-form-item>
        </a-form>
    </a-modal>

    <!-- 发送历史下拉 -->
    <a-drawer v-model:visible="historyVisible" title="发送历史" :width="360" :footer="false" placement="right">
        <div v-for="(h, i) in history" :key="i" class="history-item" @click="pickHistory(h)">
            {{ h }}
        </div>
        <a-empty v-if="!history.length" description="暂无历史" />
    </a-drawer>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import {
    appendChecksum,
    bytesToHex,
    bytesToText,
    CHECKSUM_OPTIONS,
    type ChecksumKind,
    hexToBytes,
    nowStamp,
    textToBytes,
} from "./serialUtils";

const {
    proxy: { $GoTools },
} = getCurrentInstance() as any;

const BAUDS = [1200, 2400, 4800, 9600, 19200, 38400, 57600, 115200, 230400, 460800, 921600];

/** 统一状态对象，便于模板深层访问 */
const st = reactive({
    selected: "",
    opened: false,
    label: "",
    // 串口
    baud: 115200,
    dataBits: 8,
    parity: "none",
    stopBits: 1,
    rts: false,
    dtr: true,
    // 网络
    remoteAddr: "192.168.1.20",
    remotePort: 8234,
    localAddr: "",
    localPort: 777,
    // 选项
    hexDisplay: true,
    hexSend: false,
    addCRLF: false,
    timestamp: true,
    timerSend: false,
    timerInterval: 2000,
    checksum: "none" as ChecksumKind,
    // 计数
    rx: 0,
    tx: 0,
});

const ports = ref<{ value: string; label: string }[]>([]);
const localIPs = ref<string[]>([]);
const sendText = ref("");
const settingVisible = ref(false);
const historyVisible = ref(false);
const history = ref<string[]>([]);
const recvRef = ref<HTMLElement | null>(null);

interface RecvLine {
    time: string;
    bytes: number[];
    dir: "in" | "out";
}
const recvLines = ref<RecvLine[]>([]);
const MAX_LINES = 2000;

/** 当前连接模式：serial / tcpclient / tcpserver / udp */
const mode = computed(() => {
    const s = st.selected;
    if (s === "TCPClient") return "tcpclient";
    if (s === "TCPServer") return "tcpserver";
    if (s === "UDP") return "udp";
    return "serial";
});

const openLabel = computed(
    () =>
        ({ serial: "打开串口", tcpclient: "连接", tcpserver: "侦听", udp: "打开端口" }[mode.value] || "打开")
);
const closeLabel = computed(() => (mode.value === "tcpserver" ? "停止侦听" : "关闭") + (mode.value === "serial" ? "串口" : ""));

/** 接收区文本渲染 */
const recvText = computed(() =>
    recvLines.value
        .map((l) => {
            const body = st.hexDisplay ? bytesToHex(l.bytes) : bytesToText(l.bytes);
            const head = (st.timestamp ? `[${l.time}] ` : "") + (l.dir === "out" ? "→ " : "");
            return head + body;
        })
        .join("\n")
);

/* ============ 加载串口列表 / 本机 IP ============ */
async function loadPorts() {
    try {
        const res = await $GoTools.GetSerialPorts();
        ports.value = (res || []).map((p: any) => ({
            value: p.port_name,
            label: `${p.port_name} ${p.description}`,
        }));
        if (!st.selected && ports.value.length) st.selected = ports.value[0].value;
    } catch (e: any) {
        Message.error("获取串口列表失败：" + (e?.message || e));
    }
}

async function loadLocalIPs() {
    try {
        const ips = await $GoTools.GetLocalIPs();
        localIPs.value = ["0.0.0.0", ...(ips || [])];
        if (!st.localAddr) st.localAddr = "0.0.0.0";
    } catch {
        localIPs.value = ["0.0.0.0"];
        st.localAddr = "0.0.0.0";
    }
}

function onSelectChange() {
    // 切换端口/模式时，若已连接先关闭
    if (st.opened) closeConn();
}

/* ============ 连接控制 ============ */
function buildCfg() {
    return {
        port: st.selected,
        baud: st.baud,
        dataBits: st.dataBits,
        parity: st.parity,
        stopBits: st.stopBits,
        rts: st.rts,
        dtr: st.dtr,
        localAddr: st.localAddr,
        localPort: st.localPort,
        remoteAddr: st.remoteAddr,
        remotePort: st.remotePort,
    };
}

async function openConn() {
    const cfg = buildCfg();
    try {
        if (mode.value === "serial") await $GoTools.OpenSerial(cfg);
        else if (mode.value === "tcpclient") await $GoTools.OpenTCPClient(cfg);
        else if (mode.value === "tcpserver") await $GoTools.ListenTCPServer(cfg);
        else if (mode.value === "udp") await $GoTools.OpenUDP(cfg);
        st.opened = true;
        st.label = cfgLabel();
        Message.success(openLabel.value + "成功");
        if (st.timerSend) startTimer();
    } catch (e: any) {
        st.opened = false;
        Message.error("打开失败：" + (e?.message || e));
    }
}

function cfgLabel() {
    if (mode.value === "serial") return `${st.selected} @${st.baud}`;
    if (mode.value === "tcpclient") return `→ ${st.remoteAddr}:${st.remotePort}`;
    if (mode.value === "tcpserver") return `侦听 ${st.localAddr || "0.0.0.0"}:${st.localPort}`;
    return `UDP ${st.localAddr || "0.0.0.0"}:${st.localPort}`;
}

async function closeConn() {
    stopTimer();
    try {
        await $GoTools.CloseConnection();
    } catch {
        /* 忽略 */
    }
    st.opened = false;
    st.label = "";
}

async function applyControlBits() {
    if (mode.value !== "serial" || !st.opened) return;
    try {
        await $GoTools.SetSerialControlBits(st.rts, st.dtr);
    } catch (e: any) {
        Message.error("控制位设置失败：" + (e?.message || e));
    }
}

/* ============ 发送 ============ */
function buildSendBytes(): number[] {
    let bytes = st.hexSend ? hexToBytes(sendText.value) : textToBytes(sendText.value);
    if (st.addCRLF) bytes = [...bytes, 0x0d, 0x0a];
    bytes = appendChecksum(bytes, st.checksum);
    return bytes;
}

async function doSend() {
    if (!st.opened) {
        Message.warning("请先打开连接");
        return;
    }
    const bytes = buildSendBytes();
    if (!bytes.length) return;
    try {
        await $GoTools.SendSerialHex(bytesToHex(bytes).replace(/ /g, ""));
        st.tx += bytes.length;
        pushLine({ time: nowStamp(), bytes, dir: "out" });
        rememberHistory();
    } catch (e: any) {
        Message.error("发送失败：" + (e?.message || e));
    }
}

function rememberHistory() {
    const t = sendText.value.trim();
    if (t) history.value = [t, ...history.value.filter((x) => x !== t)].slice(0, 30);
}

function loadHistory() {
    historyVisible.value = true;
}
function pickHistory(h: string) {
    sendText.value = h;
    historyVisible.value = false;
}

/* ============ 定时发送 ============ */
let timer: number | null = null;
function startTimer() {
    stopTimer();
    if (!st.timerSend || !st.opened) return;
    timer = window.setInterval(() => doSend(), Math.max(50, st.timerInterval));
}
function stopTimer() {
    if (timer) {
        clearInterval(timer);
        timer = null;
    }
}
watch(
    () => st.timerSend,
    (v) => (v ? startTimer() : stopTimer())
);
watch(() => st.timerInterval, () => st.timerSend && startTimer());

/* ============ 接收 ============ */
function pushLine(line: RecvLine) {
    recvLines.value.push(line);
    if (recvLines.value.length > MAX_LINES) {
        recvLines.value.splice(0, recvLines.value.length - MAX_LINES);
    }
    scrollToBottom();
}

async function scrollToBottom() {
    await nextTick();
    if (recvRef.value) recvRef.value.scrollTop = recvRef.value.scrollHeight;
}

const onData = (ev: any) => {
    const d = ev.data;
    if (!d || !d.hex) return;
    const bytes = hexToBytes(d.hex);
    st.rx += bytes.length;
    pushLine({ time: d.time || nowStamp(), bytes, dir: "in" });
};

const onStatus = (ev: any) => {
    const s = ev.data;
    if (!s) return;
    if (s.note) Message.info(s.note);
    if (!s.opened && st.opened) {
        st.opened = false;
        st.label = "";
        stopTimer();
    }
};

function clearRecv() {
    recvLines.value = [];
    st.rx = 0;
    st.tx = 0;
}

/* ============ 导出数据 ============ */
function exportRecv() {
    const content = recvLines.value
        .map((l) => `${l.time}  ${l.dir === "out" ? "→" : "←"}  ${st.hexDisplay ? bytesToHex(l.bytes) : bytesToText(l.bytes)}`)
        .join("\n");
    const blob = new Blob([content], { type: "text/plain;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `serial-recv-${Date.now()}.txt`;
    a.click();
    URL.revokeObjectURL(url);
}

/* ============ 生命周期 ============ */
let offData: (() => void) | null = null;
let offStatus: (() => void) | null = null;

onMounted(async () => {
    offData = Events.On("serial:data", onData);
    offStatus = Events.On("serial:status", onStatus);
    await Promise.all([loadPorts(), loadLocalIPs()]);
    try {
        const s = await $GoTools.ConnectionStatus();
        if (s && s.opened) {
            st.opened = true;
            st.label = s.label;
        }
    } catch {
        /* 忽略 */
    }
});

onUnmounted(() => {
    stopTimer();
    if (offData) offData();
    if (offStatus) offStatus();
    if (st.opened) $GoTools.CloseConnection().catch(() => {});
});
</script>

<style scoped>
/* 页面根容器：a-row 铺满工具窗口内容区（.win-content / .layout-content 有确定高度） */
.serial-page {
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

.serial-pages {
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

.field-inline {
    display: flex;
    align-items: center;
    gap: var(--base-padding);
    flex-wrap: wrap;
}

.opt-list {
    display: flex;
    flex-direction: column;
    gap: var(--base-radius-default);
}

.field-row {
    display: flex;
    align-items: center;
    gap: 6px;
}

.unit {
    color: var(--color-text-3);
    font-size: var(--base-size-small);
}

.conn-tip {
    color: var(--color-text-3);
    font-size: var(--base-size-small);
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

/* 主区域内容：接收区铺满 + 底部发送区 */
.main-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--base-padding);
    padding: var(--base-padding);
}

.recv-body {
    flex: 1;
    min-height: 120px;
    overflow-y: auto;
    background: #1d2129;
    color: #e6e6e6;
    border-radius: var(--base-radius-default);
    padding: var(--base-padding) var(--base-radius-large);
    font-size: var(--base-size-small);
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-all;
    font-family: Consolas, Monaco, "Courier New", monospace;
}

.recv-body.hex {
    color: #ffd591;
}

.send-area {
    display: flex;
    flex-direction: column;
    gap: var(--base-radius-default);
    flex: none;
}

.send-ops {
    display: flex;
    align-items: center;
    justify-content: space-between;
}

.history-item {
    padding: var(--base-padding) var(--base-radius-large);
    border-bottom: 1px solid var(--color-border-2);
    cursor: pointer;
    font-family: Consolas, Monaco, monospace;
    font-size: var(--base-size-small);
    word-break: break-all;
}

.history-item:hover {
    background: var(--color-fill-2);
}
</style>
