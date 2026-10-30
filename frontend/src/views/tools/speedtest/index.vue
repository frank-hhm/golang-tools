<template>
    <div class="st-wrap">
        <a-card class="st-page card-head-tool">
            <template #title>
                <a-space :size="8" wrap>
                    <icon-thunderbolt class="panel-icon" />
                    <a-tag v-for="item in overview" :key="item.label" size="small" color="gray">
                        <span class="st-meta-value">{{ item.value }}</span>
                    </a-tag>
                </a-space>
            </template>
            <template #extra>
                <a-space :size="8" wrap>
                    <a-typography-text type="secondary" size="small">单节点时长</a-typography-text>
                    <a-select v-model="duration" :disabled="anyRunning" size="small">
                        <a-option :value="2">2 秒</a-option>
                        <a-option :value="5">5 秒</a-option>
                        <a-option :value="10">10 秒</a-option>
                    </a-select>
                    <a-button size="small" :disabled="anyRunning" @click="testAll">
                        <template #icon><icon-play-arrow /></template>全部测速
                    </a-button>
                    <a-button size="small" v-if="anyRunning" status="danger" @click="stopAll">
                        <template #icon><icon-pause /></template>停止
                    </a-button>
                </a-space>
            </template>
            <div class="st-body">
                <!-- ============ 节点卡片 ============ -->
                <a-row v-if="nodes.length" :gutter="[12, 12]" class="st-nodes">
                    <a-col v-for="n in nodes" :key="n.id" :xs="24" :md="12" :xxl="8">
                        <a-card class="node-card" size="small" hoverable :body-style="{ padding: '14px' }">
                            <template #title>
                                <div class="node-head">
                                    <a-space :size="6" class="node-head-row">
                                        <a-tooltip :content="n.desc">
                                            <a-tag size="small" :color="kindColor(n.kind)">{{ kindLabel(n.kind)
                                            }}</a-tag>
                                        </a-tooltip>
                                        <span class="node-name">{{ n.name }}</span>
                                        <a-tooltip :content="n.downloadUrl">
                                            <a-typography-text type="secondary" size="small" code class="node-host">{{
                                                n.endpointLabel }}</a-typography-text>
                                        </a-tooltip>
                                        <!-- 运行状态点放在操作区，不插在名称与端点之间；跑完/出错带一句结论，颜色跟着圆点走 -->
                                        <com-badge v-if="n.status === 'running'" status="danger" :animated="true"
                                            title="测速中" />
                                        <span v-else-if="n.status === 'error'" class="com-state is-off">
                                            <com-badge status="danger" />失败
                                        </span>
                                        <span v-else-if="n.status === 'done'" class="com-state is-on">
                                            <com-badge status="success" />已完成
                                        </span>
                                    </a-space>
                                </div>
                            </template>
                            <template #extra>
                                <a-space :size="4">
                                    <a-button v-if="n.status !== 'running'" size="small"
                                        :disabled="anyRunning" @click="testNode(n)">
                                        <template #icon><icon-play-arrow /></template>
                                        {{ n.status === 'done' ? "重测" : "测速" }}
                                    </a-button>
                                    <a-button v-else size="small" status="danger" @click="cancel(n)">
                                        <template #icon><icon-pause /></template>停止
                                    </a-button>
                                    <a-dropdown v-if="n.kind === 'custom'" trigger="hover" @select="nodeMenu(n)">
                                        <a-button size="small">
                                            <template #icon><icon-more /></template>
                                        </a-button>
                                        <template #content>
                                            <a-doption value="edit">
                                                <template #icon><icon-edit /></template>编辑
                                            </a-doption>
                                            <a-doption value="remove">
                                                <template #icon><icon-delete /></template>删除
                                            </a-doption>
                                        </template>
                                    </a-dropdown>
                                </a-space>
                            </template>

                            <!-- 测试中：卡片上沿全宽细进度条，百分比到 100% 或测试结束即隐藏 -->
                            <div v-if="n.status === 'running'" class="node-loading">
                                <div class="node-loading-bar" :style="{ width: `${Math.round(runPct(n) * 100)}%` }" />
                            </div>

                            <!-- 三项指标：延迟 / 下载 / 上传 -->
                            <a-row :gutter="8" class="node-metrics">
                                <a-col :span="8">
                                    <a-statistic :value="n.latencyMs ?? undefined" :precision="0" placeholder="—"
                                        :value-style="metricStyle">
                                        <template #title>
                                            <a-space :size="4">
                                                <span>延迟</span>
                                                <com-badge v-if="busy(n, 'latency')" status="processing" :animated="true"
                                                    color="rgb(var(--arcoblue-6))" />
                                            </a-space>
                                        </template>
                                        <template #suffix><span class="unit">ms</span></template>
                                    </a-statistic>
                                </a-col>
                                <a-col :span="8">
                                    <a-statistic :value="dlVal(n)" :precision="1" placeholder="—"
                                        :value-style="metricStyle">
                                        <template #title>
                                            <a-space :size="4">
                                                <span>下载</span>
                                                <com-badge v-if="busy(n, 'download')" status="processing" :animated="true"
                                                    color="rgb(var(--arcoblue-6))" />
                                            </a-space>
                                        </template>
                                        <template #suffix><span class="unit">Mbps</span></template>
                                    </a-statistic>
                                </a-col>
                                <a-col :span="8">
                                    <a-statistic :value="ulVal(n)" :precision="1"
                                        :placeholder="n.uploadUrl ? '—' : '不支持'" :value-style="metricStyle">
                                        <template #title>
                                            <a-space :size="4">
                                                <span>上传</span>
                                                <com-badge v-if="busy(n, 'upload')" status="processing" :animated="true"
                                                    color="rgb(var(--arcoblue-6))" />
                                            </a-space>
                                        </template>
                                        <template v-if="n.uploadUrl" #suffix><span class="unit">Mbps</span></template>
                                    </a-statistic>
                                </a-col>
                            </a-row>

                            <!-- 底部：错误压成一行（悬浮看全文），完成时间沉到右下角 -->
                            <div class="node-foot">
                                <a-tooltip v-if="n.error" :content="n.error">
                                    <a-typography-text type="danger" class="node-err">
                                        <icon-exclamation-circle-fill /> {{ n.error }}
                                    </a-typography-text>
                                </a-tooltip>
                                <a-typography-text v-if="n.finishedAt" type="secondary" size="small" class="node-done">
                                    <icon-check-circle /> {{ n.finishedAt }} 完成
                                </a-typography-text>
                            </div>
                        </a-card>
                    </a-col>
                </a-row>
                <a-empty v-else description="暂无测速节点" />
            </div>

        </a-card>

        <!-- ============ 自定义端点：与节点卡片区并列，始终留在页面底部 ============ -->
        <a-card class="st-panel card-head-tool" size="small">
            <template #title>
                <div class="node-head">
                    <a-space :size="6">
                        <a-tag size="small" color="orange">自定义</a-tag>
                        <span class="node-name">自定义测速端点</span>
                        <a-tooltip
                            content="下载端点用浏览器流式读取测吞吐量；上传端点以分块 POST 发送测试数据（可复用 speed.cloudflare.com/__up）。两者都需返回 CORS 头，否则前端读不到响应。">
                            <icon-info-circle class="panel-icon" />
                        </a-tooltip>
                        <a-typography-text type="secondary" size="small" class="panel-sub">
                            填一个自己可用的下载/上传端点，上传地址可留空
                        </a-typography-text>
                    </a-space>
                </div>
            </template>
            <template #extra>
                <a-button v-if="editingCustomId" size="small" @click="cancelEdit">取消编辑</a-button>
            </template>

            <a-form ref="customFormRef" :model="customForm" class="st-form" layout="vertical"
                @submit-prevent="saveCustom">
                <a-row :gutter="12" align="end">
                    <a-col :xs="24" :md="6">
                        <a-form-item field="name" label="节点名称">
                            <a-input v-model="customForm.name" placeholder="如：我的服务器" allow-clear />
                        </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="10">
                        <a-form-item field="downloadUrl" label="下载地址" :rules="dlRules">
                            <a-input v-model="customForm.downloadUrl" placeholder="需支持 CORS 的大文件地址" allow-clear />
                        </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="5">
                        <a-form-item field="uploadUrl" label="上传地址（可选）">
                            <a-input v-model="customForm.uploadUrl" placeholder="分块 POST 端点" allow-clear />
                        </a-form-item>
                    </a-col>
                    <a-col :xs="24" :md="3">
                        <a-form-item hide-label>
                            <a-button size="small" html-type="submit" long>
                                <template #icon><icon-plus /></template>
                                {{ editingCustomId ? "保存" : "添加" }}
                            </a-button>
                        </a-form-item>
                    </a-col>
                </a-row>
            </a-form>
        </a-card>
    </div>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";

/* ============ 类型与常量 ============ */

type NodeKind = "global" | "cn" | "custom";
type Phase = "idle" | "latency" | "download" | "upload";
type NodeStatus = "idle" | "running" | "done" | "error";

/** 单个测速节点：声明式端点配置 + 测速状态/结果 */
interface SpeedNode {
    id: string;
    name: string;
    desc: string;
    kind: NodeKind;
    latencyUrl: string;
    downloadUrl: string;
    downloadHint: number; // 下载体积预估（字节），仅用于进度展示
    uploadUrl: string;    // 空 = 不支持上传测试
    uploadSize: number;   // 单次上传分块体积（字节）
    cors: boolean;        // 是否支持跨域读取响应（否则失败信息提示原因）
    status: NodeStatus;
    phase: Phase;
    latencyMs: number | null;
    downloadMbps: number | null;
    uploadMbps: number | null;
    liveRate: string;   // 测试中的实时速率展示（Mbps，采样平滑）
    progress: number;   // 当前阶段进度 0~1（下载阶段按预估体积换算）
    error: string;
    finishedAt: string;
    endpointLabel: string;
    ctrl: AbortController | null; // 进行中的请求控制器（停止用）
}

const MB = 1024 * 1024;

/** 内置节点基线（端点均已实测可达且返回 CORS 头，否则浏览器 fetch 读不到响应） */
const BUILTIN_NODES = [
    {
        id: "cloudflare", name: "Cloudflare", desc: "国际 · 上传/下载/延迟全测", kind: "global" as NodeKind,
        latencyUrl: "https://speed.cloudflare.com/__down?bytes=0",
        downloadUrl: "https://speed.cloudflare.com/__down?bytes=209715200", downloadHint: 200 * MB,
        uploadUrl: "https://speed.cloudflare.com/__up", uploadSize: 4 * MB, cors: true,
    },
    {
        id: "npmmirror", name: "阿里 CDN · npmmirror", desc: "国内 · 仅下载", kind: "cn" as NodeKind,
        latencyUrl: "https://registry.npmmirror.com/vue/latest",
        downloadUrl: "https://cdn.npmmirror.com/binaries/electron/v33.2.0/electron-v33.2.0-win32-x64.zip",
        downloadHint: 110 * MB, uploadUrl: "", uploadSize: 0, cors: true,
    },
    {
        id: "huaweicloud", name: "华为云镜像", desc: "国内 · 仅下载", kind: "cn" as NodeKind,
        latencyUrl: "https://mirrors.huaweicloud.com/nodejs/index.json",
        downloadUrl: "https://mirrors.huaweicloud.com/electron/33.2.0/electron-v33.2.0-win32-x64.zip",
        downloadHint: 110 * MB, uploadUrl: "", uploadSize: 0, cors: true,
    },
];

/** localStorage 键：自定义节点持久化 */
const LS_CUSTOM = "Tools.speedtest.custom";

/* ============ 指标排版（统一的正文色，浅/暗黑自动跟随） ============ */

const metricStyle = { fontSize: "15px", fontWeight: "500", color: "var(--color-text-1)" };

/* ============ 节点状态 ============ */

const nodes = ref<SpeedNode[]>(BUILTIN_NODES.map((b) => ({
    ...b,
    endpointLabel: hostOf(b.downloadUrl),
    status: "idle", phase: "idle",
    latencyMs: null, downloadMbps: null, uploadMbps: null,
    liveRate: "0", progress: 0, error: "", finishedAt: "", ctrl: null,
})));

function hostOf(url: string): string {
    try { return new URL(url).host; } catch { return url; }
}

const anyRunning = computed(() => nodes.value.some((n) => n.status === "running"));

const kindLabel = (k: NodeKind) => (k === "global" ? "国际" : k === "cn" ? "国内" : "自定义");
const kindColor = (k: NodeKind) => (k === "global" ? "arcoblue" : k === "cn" ? "green" : "orange");

/** 该指标是否正在测量中（驱动 a-badge 呼吸点） */
const busy = (n: SpeedNode, p: Phase) => n.status === "running" && n.phase === p;

/** 显示值：测量中取实时速率，否则取结果；undefined 交给 a-statistic 的 placeholder */
const dlVal = (n: SpeedNode) => (busy(n, "download") ? Number(n.liveRate) : n.downloadMbps ?? undefined);
const ulVal = (n: SpeedNode) => (busy(n, "upload") ? Number(n.liveRate) : n.uploadMbps ?? undefined);

/** 顶部进度条用：三阶段压成一条单调递增的曲线（延迟 6%，下载 6%~68%，上传 68%~100%） */
const runPct = (n: SpeedNode) => {
    if (n.phase === "latency") return 0.06;
    if (n.phase === "download") return 0.06 + n.progress * 0.62;
    if (n.phase === "upload") return 0.68 + n.progress * 0.32;
    return 0;
};

/** 标题栏元信息 */
const overview = computed(() => [
    { label: "出口 IP", value: meta.ip || "—" },
    { label: "运营商", value: meta.isp || "—" },
    { label: "出口节点", value: meta.node || "—" },
]);

/* ============ 本机出口信息（Cloudflare meta，失败静默） ============ */

const meta = reactive({ ip: "", isp: "", node: "" });

async function loadMeta() {
    try {
        const r = await fetch("https://speed.cloudflare.com/meta", { cache: "no-store" });
        const j = await r.json();
        meta.ip = j.clientIp ?? "";
        meta.isp = j.asOrganization ?? "";
        meta.node = j.city ? `${j.country ?? ""} · ${j.city}` : "";
    } catch { /* 内网/屏蔽环境下不提示，仅不展示 */ }
}

/* ============ 测速核心 ============ */

/** 时间戳差（performance.now 单调时钟） */
const elapsed = (t0: number) => performance.now() - t0;

/** crypto.getRandomValues 单次上限 65536 字节，分块填满大缓冲 */
function randomBytes(size: number) {
    const buf = new Uint8Array(size);
    for (let off = 0; off < size; off += 65536) {
        crypto.getRandomValues(buf.subarray(off, Math.min(off + 65536, size)));
    }
    return buf;
}

/** 带进度回调的流式读取；durationMs 非空则到时主动中断（长文件测速） */
async function streamBody(
    resp: Response,
    ctrl: AbortController,
    onProgress: (bytes: number, seconds: number) => void,
    durationMs: number | null = null,
): Promise<{ bytes: number; seconds: number; timeUp: boolean }> {
    const reader = resp.body?.getReader();
    if (!reader) throw new Error("响应体不可读");
    const start = performance.now();
    let timer: ReturnType<typeof setInterval> | null = null;
    if (durationMs) {
        timer = setInterval(() => {
            if (elapsed(start) >= durationMs) ctrl.abort();
        }, 50);
    }
    let bytes = 0;
    try {
        for (; ;) {
            const { done, value } = await reader.read();
            if (done) break;
            bytes += value?.byteLength ?? 0;
            onProgress(bytes, elapsed(start) / 1000);
        }
    } catch (e) {
        // AbortError = 主动到时截断/用户停止，不算失败；reader.cancel 会连带中断底层下载
        if (!(e instanceof DOMException && e.name === "AbortError")) throw e;
    } finally {
        if (timer) clearInterval(timer);
        try { reader.cancel(); } catch { /* 已结束 */ }
        if (durationMs) {
            const cl = Number(resp.headers.get("Content-Length") ?? 0);
            return { bytes, seconds: elapsed(start) / 1000, timeUp: !(cl && bytes < cl) };
        }
    }
    return { bytes, seconds: elapsed(start) / 1000, timeUp: false };
}

/** 下载测速：流式读取 durationMs 毫秒后中断，按吞吐量算 Mbps */
async function measureDownload(n: SpeedNode, durationMs: number): Promise<number> {
    const ctrl = new AbortController();
    n.ctrl = ctrl;
    const sep = n.downloadUrl.includes("?") ? "&" : "?";
    const url = `${n.downloadUrl}${sep}_t=${Math.random().toString(36).slice(2)}`;
    const resp = await fetch(url, { signal: ctrl.signal, cache: "no-store" });
    if (!resp.ok) throw new Error(`下载端点 HTTP ${resp.status}`);
    let last = 0;
    const { bytes, seconds, timeUp } = await streamBody(resp, ctrl, (b, s) => {
        n.progress = Math.min(1, b / Math.max(n.downloadHint, 1));
        if (b - last >= MB / 2) {
            last = b;
            n.liveRate = ((b * 8 / 1e6) / Math.max(s, 0.05)).toFixed(1);
        }
    }, durationMs);
    if (bytes === 0) throw new Error("未收到任何数据");
    // 远端秒断（如镜像无 CORS / 返回错误页）时 bytes 很小但 timeUp 为真，结果不可信
    if (timeUp && bytes < 64 * 1024) throw new Error(n.cors ? "下载数据过少，端点可能不可达" : "该端点不支持跨域读取，无法测速");
    return (bytes * 8 / 1e6) / seconds;
}

/** 上传测速：分块 POST 直到时长用尽，按已确认字节总量算 Mbps（单次大块会不可控地跑满带宽） */
async function measureUpload(n: SpeedNode, durationMs: number): Promise<number> {
    const ctrl = new AbortController();
    n.ctrl = ctrl;
    const chunk = n.uploadSize;
    const body = new Blob([randomBytes(chunk)]);
    const start = performance.now();
    let bytes = 0;
    for (; ;) {
        if (elapsed(start) >= durationMs) break;
        const resp = await fetch(n.uploadUrl, {
            method: "POST", body, signal: ctrl.signal, cache: "no-store",
        });
        if (!resp.ok) throw new Error(`上传端点 HTTP ${resp.status}`);
        await resp.arrayBuffer();
        bytes += chunk;
        n.progress = Math.min(1, elapsed(start) / durationMs);
        n.liveRate = ((bytes * 8 / 1e6) / (elapsed(start) / 1000)).toFixed(1);
    }
    const sec = elapsed(start) / 1000;
    if (bytes === 0) throw new Error("上传未发出任何完整请求");
    return (bytes * 8 / 1e6) / sec;
}

/** 延迟测速：小体积请求 5 次取中位数（可被“停止”中断） */
async function measureLatency(n: SpeedNode): Promise<number> {
    const ctrl = new AbortController();
    n.ctrl = ctrl;
    const samples: number[] = [];
    for (let i = 0; i < 5; i++) {
        if (n.status !== "running") break;
        const t0 = performance.now();
        try {
            const resp = await fetch(`${n.latencyUrl}${n.latencyUrl.includes("?") ? "&" : "?"}_t=${i}`, {
                cache: "no-store", signal: ctrl.signal,
            });
            await resp.arrayBuffer();
            samples.push(elapsed(t0));
        } catch (e) {
            if (e instanceof DOMException && e.name === "AbortError") throw e;
            /* 单次失败丢弃 */
        }
    }
    if (!samples.length) throw new Error("延迟测量失败：端点不可达");
    samples.sort((a, b) => a - b);
    return samples[Math.floor(samples.length / 2)];
}

/** 单节点完整流程：延迟 → 下载 → 上传，各指标独立容错 */
async function testNode(n: SpeedNode) {
    if (n.status === "running" || anyRunning.value) return;
    Object.assign(n, { status: "running" as NodeStatus, phase: "idle" as Phase, error: "", liveRate: "0", progress: 0 });
    try {
        n.phase = "latency";
        n.latencyMs = await measureLatency(n);

        n.phase = "download";
        n.downloadMbps = await measureDownload(n, duration.value * 1000);

        if (n.uploadUrl) {
            n.phase = "upload";
            n.liveRate = "0";
            n.progress = 0;
            n.uploadMbps = await measureUpload(n, duration.value * 1000);
        } else {
            n.uploadMbps = null;
        }
        n.status = "done";
        n.finishedAt = new Date().toLocaleTimeString("zh-CN", { hour12: false });
    } catch (e) {
        // 用户主动停止不算错误
        if (e instanceof DOMException && e.name === "AbortError") {
            n.status = "idle";
        } else {
            n.status = "error";
            n.error = e instanceof Error ? e.message : String(e);
        }
    } finally {
        n.phase = "idle";
        n.progress = 0;
        n.ctrl = null;
    }
}

/** 依次测完所有节点（同一时间只占带宽，结果可横向对比） */
async function testAll() {
    for (const n of nodes.value) {
        if (n.status === "running") continue;
        await testNode(n);
    }
}

function cancel(n: SpeedNode) {
    n.ctrl?.abort();
}

function stopAll() {
    nodes.value.forEach((n) => n.ctrl?.abort());
}

/* ============ 全局参数 ============ */

const duration = ref(5); // 每节点下载测速时长（秒）

/* ============ 自定义节点增删改 ============ */

const customFormRef = ref();
const customForm = reactive({ name: "", downloadUrl: "", uploadUrl: "" });
const editingCustomId = ref("");

/** 下载端点校验：必填 + 必须 http(s)，交给 a-form-item 展示 */
const dlRules = [
    { required: true, message: "请填写下载地址" },
    { match: /^https?:\/\//i, message: "需以 http:// 或 https:// 开头" },
];

interface CustomDto { id: string; name: string; downloadUrl: string; uploadUrl: string }

function loadCustom(): CustomDto[] {
    try { return JSON.parse(localStorage.getItem(LS_CUSTOM) ?? "[]"); } catch { return []; }
}
function persistCustom() {
    const list = nodes.value
        .filter((n) => n.kind === "custom")
        .map((n) => ({ id: n.id, name: n.name, downloadUrl: n.downloadUrl, uploadUrl: n.uploadUrl }));
    localStorage.setItem(LS_CUSTOM, JSON.stringify(list));
}

/** 由端点配置生成一个完整节点对象（内置/自定义共用） */
function makeNode(pick: Pick<SpeedNode, "id" | "name" | "desc" | "kind" | "latencyUrl" | "downloadUrl" | "downloadHint" | "uploadUrl" | "uploadSize" | "cors">): SpeedNode {
    return {
        ...pick,
        endpointLabel: hostOf(pick.downloadUrl),
        status: "idle", phase: "idle",
        latencyMs: null, downloadMbps: null, uploadMbps: null,
        liveRate: "0", progress: 0, error: "", finishedAt: "", ctrl: null,
    };
}

/** 挂载时把存量自定义节点合并进节点列表 */
function mergeCustomNodes() {
    for (const c of loadCustom()) {
        nodes.value.push(makeNode({
            id: c.id, name: c.name || "自定义节点", desc: "自定义端点", kind: "custom",
            latencyUrl: c.downloadUrl, downloadUrl: c.downloadUrl, downloadHint: 100 * MB,
            uploadUrl: c.uploadUrl, uploadSize: 4 * MB, cors: true,
        }));
    }
}

async function saveCustom() {
    const errs = await customFormRef.value?.validate();
    if (errs) return;
    const dl = customForm.downloadUrl.trim();
    if (editingCustomId.value) {
        const n = nodes.value.find((x) => x.id === editingCustomId.value);
        if (n) {
            Object.assign(n, makeNode({
                id: n.id, name: customForm.name.trim() || n.name, desc: "自定义端点", kind: "custom",
                latencyUrl: dl, downloadUrl: dl, downloadHint: 100 * MB,
                uploadUrl: customForm.uploadUrl.trim(), uploadSize: 4 * MB, cors: true,
            }));
        }
        editingCustomId.value = "";
    } else {
        nodes.value.push(makeNode({
            id: "custom-" + Date.now(), name: customForm.name.trim() || "自定义节点", desc: "自定义端点",
            kind: "custom", latencyUrl: dl, downloadUrl: dl, downloadHint: 100 * MB,
            uploadUrl: customForm.uploadUrl.trim(), uploadSize: 4 * MB, cors: true,
        }));
    }
    customForm.name = ""; customForm.downloadUrl = ""; customForm.uploadUrl = "";
    customFormRef.value?.clearValidate();
    persistCustom();
    Message.success("自定义节点已保存");
}

function editCustom(n: SpeedNode) {
    editingCustomId.value = n.id;
    customForm.name = n.name;
    customForm.downloadUrl = n.downloadUrl;
    customForm.uploadUrl = n.uploadUrl;
    customFormRef.value?.clearValidate();
}

function cancelEdit() {
    editingCustomId.value = "";
    customForm.name = ""; customForm.downloadUrl = ""; customForm.uploadUrl = "";
    customFormRef.value?.clearValidate();
}

function removeCustom(n: SpeedNode) {
    Modal.confirm({
        title: "删除自定义节点",
        content: `确定删除「${n.name}」？`,
        okText: "删除",
        cancelText: "取消",
        onOk: () => {
            nodes.value = nodes.value.filter((x) => x.id !== n.id);
            if (editingCustomId.value === n.id) cancelEdit();
            persistCustom();
        },
    });
}

/** 卡片右上角「更多」菜单分发（nodeMenu 返回一个 (v: unknown) 处理器，兼容 a-dropdown 的 select 签名） */
function onNodeMenu(n: SpeedNode, v: unknown) {
    if (v === "edit") editCustom(n);
    else if (v === "remove") removeCustom(n);
}
const nodeMenu = (n: SpeedNode) => (v: unknown) => onNodeMenu(n, v);

/* ============ 生命周期 ============ */

onMounted(() => {
    mergeCustomNodes();
    loadMeta();
});

onUnmounted(() => {
    stopAll();
});
</script>

<style scoped>
/* 页面容器：满高列布局，节点卡片区吃满剩余高度，底部表单卡恒在屏内 */
.st-wrap {
    display: flex;
    flex-direction: column;
    gap: var(--base-padding);
    box-sizing: border-box;
    height: 100%;
    min-height: 0;
}

.st-page {
    display: flex;
    flex: 1;
    flex-direction: column;
    box-sizing: border-box;
    min-height: 0;
}

.st-page :deep(.arco-card-body) {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-height: 0;
}

/* 节点卡片区：自己滑动，但隐藏滚动条 */
.st-body {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: var(--base-padding);
    min-height: 0;
    overflow-y: auto;
    scrollbar-width: none;
}

.st-body::-webkit-scrollbar {
    display: none;
}

.st-panel {
    flex: none;
}

/* 面板标题下的说明行：单行省略，与节点卡片的端点行同一层级 */
.panel-sub {
    display: block;
    overflow: hidden;
    font-size: 12px;
    line-height: 18px;
    white-space: nowrap;
    text-overflow: ellipsis;
}

/* 自定义端点表单：标签小字与卡片指标标题对齐 */
.st-form :deep(.arco-form-item) {
    margin-bottom: 0;
}

.st-form :deep(.arco-form-label-item > label) {
    font-size: 12px;
    color: var(--color-text-3);
}

.panel-icon {
    color: rgb(var(--color-text-1));
}

/* 标题栏元信息：名称后的灰底标签，值加粗以区分标签名 */
.st-meta-value {
    margin-left: 4px;
    font-weight: 600;
    color: var(--color-text-1);
}

/* 节点卡片固定高度：进度条 / 对比条 / 错误信息切换都不改变卡片总高 */
.st-nodes {
    align-items: stretch;
    flex: none;
}

.node-card {
    display: flex;
    flex-direction: column;
    height: 150px;
}

/* a-card size=small 的 header 默认固定 40px 且单行，放开以容纳名称行 + 说明行 */
.node-card :deep(.arco-card-header) {
    height: auto;
    min-height: 54px;
}

.node-card :deep(.arco-card-header-title) {
    overflow: hidden;
}

.node-card :deep(.arco-card-body) {
    display: flex;
    flex: 1;
    flex-direction: column;
    gap: 8px;
    min-height: 0;
}

.node-name {
    font-weight: 600;
    color: var(--color-text-1);
}

/* 标题区：名称行 + 说明/端点行（上提到卡片标题里，正文只剩指标与条） */
.node-head {
    display: flex;
    flex-direction: column;
    gap: 2px;
    min-width: 0;
}

.node-host {
    display: inline-block;
    flex: 0 1 auto;
    min-width: 0;
    overflow: hidden;
    font-size: 12px;
    line-height: 18px;
    white-space: nowrap;
    text-overflow: ellipsis;
}

.node-metrics {
    flex: none;
}

/* 指标标题与数值贴合一点，卡片整体更紧凑 */
.node-metrics :deep(.arco-statistic-title) {
    margin-bottom: 4px;
}

/* 测试中：贴着卡片上沿的全宽细进度条 */
.node-loading {
    position: absolute;
    top: 0;
    right: 0;
    left: 0;
    z-index: 1;
    height: 2px;
    overflow: hidden;
    background: var(--color-fill-2);
}

.node-loading-bar {
    height: 100%;
    background: var(--color-text-1);
    transition: width 0.25s linear;
}

.unit {
    font-size: 12px;
    color: var(--color-text-3);
}

/* 底部：错误限一行省略（完整内容看 tooltip），完成时间靠右下 */
.node-foot {
    display: flex;
    flex: 1;
    flex-direction: column;
    justify-content: flex-end;
    gap: 2px;
    min-height: 0;
    overflow: hidden;
}

.node-err {
    display: block;
    overflow: hidden;
    font-size: 12px;
    line-height: 18px;
    white-space: nowrap;
    text-overflow: ellipsis;
}

.node-done {
    align-self: flex-end;
}
</style>
