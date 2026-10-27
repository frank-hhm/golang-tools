<template>
    <a-row class="nginx-page" :gutter="6">
        <!-- 左侧：实例列表 -->
        <a-col :span="7" class="side-col">
            <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                <template #title>
                    <span class="side-title">本地服务</span>
                    <span class="side-count">{{ instances.length }}</span>
                </template>
                <template #extra>
                    <div class="srv-head-actions">
                        <a-button size="small" type="primary" @click="openInstModal()">新建实例</a-button>
                        <a-button size="small" @click="openVersionsModal()">版本管理</a-button>
                    </div>
                </template>
                <!-- 引擎没起时先把话说明白：否则要等点「启动」撞一条管道错误才知道是 Docker 没跑 -->
                <common-docker-off class="srv-docker-off" :count="dockerInstCount" tool-label="Nginx" />
                <div class="inst-list">
                    <common-item-card v-for="inst in orderedInstances" :key="inst.id"
                        :active="selectedId === inst.id" @click="selectInst(inst.id)" @dblclick="dblInst(inst)"
                        v-bind="nginxReo.handlers(inst.id)"
                        :class="{ 'reorder-dragging': nginxReo.isDragging(inst.id), 'reorder-over': nginxReo.isOver(inst.id) }">
                        <template #status>
                            <com-badge :status="states[inst.id]?.running ? 'success' : 'normal'"
                                :animated="!!states[inst.id]?.running" />
                        </template>
                        <template #title>
                            <a-tooltip class="inst-name-tip" :content="inst.name + ':' + inst.port">
                                <span class="inst-name">{{ inst.name }}</span>
                            </a-tooltip>
                        </template>
                        <template #extra>
                            <a-dropdown trigger="hover" @select="(v: any) => onInstAction(v, inst)">
                                <icon-more class="inst-more" @click.stop />
                                <template #content>
                                    <a-doption v-if="!states[inst.id]?.running" value="start"><icon-play-arrow />
                                        启动</a-doption>
                                    <a-doption v-else value="stop"><icon-pause /> 停止</a-doption>
                                    <a-doption value="restart"><icon-refresh /> 重启</a-doption>
                                    <a-doption value="reload"><icon-sync /> 重载配置</a-doption>
                                    <a-doption value="check"><icon-safe /> 配置检查</a-doption>
                                    <a-doption value="conf"><icon-settings /> nginx.conf</a-doption>
                                    <a-doption value="open"><icon-launch /> 打开站点</a-doption>
                                    <a-doption value="logs"><icon-file /> 日志</a-doption>
                                    <a-doption value="edit"><icon-edit /> 编辑</a-doption>
                                    <a-doption value="del"><icon-delete /> 删除</a-doption>
                                </template>
                            </a-dropdown>
                        </template>
                        <div class="inst-line2">
                            <span class="inst-port">:{{ inst.port }}</span>
                            <a-tooltip v-if="inst.mode === 'docker'"
                                :content="imgMissing(inst.image)
                                    ? `镜像 ${inst.image} 本机还没有，启动时会先自动拉取（可能几分钟）`
                                    : `镜像 ${inst.image}：这就是该实例的 Nginx 版本`">
                                <a-tag :color="imgMissing(inst.image) ? 'orange' : 'purple'" size="small">Docker
                                    {{ imgTag(inst.image) }}</a-tag>
                            </a-tooltip>
                            <a-tag v-else color="arcoblue" size="small">v{{ inst.version }}</a-tag>
                            <a-tag v-if="inst.sslPort" color="orange" size="small">https</a-tag>
                            <a-tag v-if="inst.phpMode" color="green" size="small">PHP</a-tag>
                        </div>
                    </common-item-card>
                    <div v-if="instances.length === 0" class="inst-empty">暂无实例，点击右上"新建实例"</div>
                </div>
            </a-card>
        </a-col>

        <!-- 右侧：选中实例详情 + 日志 -->
        <a-col :span="17" class="main-col">
            <a-card v-if="!selectedId" class="main-panel empty-card card-head-tool">
                <a-empty description="从左侧选择一个 Nginx 实例查看状态与日志" />
            </a-card>
            <a-card v-else class="main-panel card-head-tool" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space>
                        <com-badge
                            :status="restartBusy ? 'processing' : (selState?.running ? 'success' : 'normal')"
                            :animated="!!selState?.running || restartBusy" />
                        <span>{{ selCfg?.name }}</span>
                        <a-tooltip v-if="isDockerSel" :content="selState?.image || selCfg?.image || ''">
                            <a-tag color="purple" size="small">Docker {{ imgTag(selState?.image || selCfg?.image || '') }}</a-tag>
                        </a-tooltip>
                        <a-tag v-else color="arcoblue" size="small">v{{ selState?.version || selCfg?.version }}</a-tag>
                        <a-tag color="arcoblue" size="small">:{{ selCfg?.port }}</a-tag>
                        <a-tag v-if="selCfg?.sslPort" color="orange" size="small">https :{{ selCfg.sslPort }}</a-tag>
                    </a-space>
                </template>
                <template #extra>
                    <a-space>
                        <a-button size="small" :disabled="selState?.running || stopBusy || restartBusy"
                            @click="doStart">启动</a-button>
                        <!-- 停止会断开正在服务的请求，先气泡确认；按钮转 loading 显示「停止中」 -->
                        <a-popconfirm :content="`停止实例「${selCfg?.name}」？正在处理的请求会中断。`" type="warning"
                            position="lt" :disabled="!selState?.running || stopBusy || restartBusy" @ok="doStop">
                            <a-button size="small" status="danger" :disabled="!selState?.running || stopBusy || restartBusy"
                                :loading="stopBusy">{{ stopBusy ? '停止中' : '停止' }}</a-button>
                        </a-popconfirm>
                        <!-- 重启 = 先停后起，期间状态会短暂翻转：按钮转 loading，并在面板顶部提示「正在重启」 -->
                        <a-button size="small" :disabled="stopBusy || restartBusy" :loading="restartBusy"
                            @click="doRestart">{{ restartBusy ? '重启中' : '重启' }}</a-button>
                        <a-tooltip
                            :content="isDockerSel ? '容器模式没有 nginx -s reload：重载 = 重启容器（会短暂中断），新配置随之生效' : 'nginx -s reload 平滑重载配置'">
                            <a-button size="small" :disabled="!selState?.running || stopBusy || restartBusy" @click="doReload">重载</a-button>
                        </a-tooltip>
                    </a-space>
                </template>
                <div class="ov-body">
                    <!-- 重启进行中的显式提示：状态会短暂翻转，先说清楚不是掉线 -->
                    <a-alert v-if="restartBusy" class="restart-tip" type="info" banner :closable="false">
                        正在重启实例「{{ selCfg?.name }}」，重启期间站点会短暂不可用，稍后自动恢复…
                    </a-alert>
                    <!-- 关键指标：版式对齐磁盘总览（彩色圆角图标 + 标签/数值同排 + 下方明细） -->
                    <a-row :gutter="[12, 12]" class="stat-row">
                        <a-col v-for="c in statCards" :key="c.key" :xs="24" :sm="12" :md="8">
                            <common-stat-card :icon="c.icon" :tone="c.tone" :label="c.title" :value="c.value"
                                :detail="c.sub" :tip="c.tip" :ready="c.ready" />
                        </a-col>
                    </a-row>

                    <!-- 快捷操作：日志改到弹窗里看，面板只留高频入口 -->
                    <div class="quick-bar">
                        <a-button size="small" :disabled="!selState?.running"
                            @click="openSite(selCfg?.port ?? 0)">
                            <template #icon><icon-launch /></template>打开站点
                        </a-button>
                        <a-button size="small" @click="openLogModal(selectedId)">
                            <template #icon><icon-face-folder-open /></template>查看日志
                        </a-button>
                        <a-button size="small" @click="openConf(selectedId)">
                            <template #icon><icon-settings /></template>nginx.conf
                        </a-button>
                        <a-button size="small" @click="doCheck(selectedId)">
                            <template #icon><icon-safe /></template>配置检查
                        </a-button>
                    </div>

                    <!-- 详细信息：通用信息行组件（标签 | 值 | 操作），路径类可一键打开对应目录 -->
                    <div class="info-section">
                        <div class="info-title">站点信息</div>
                        <div class="info-list">
                            <common-info-row grey v-if="isDockerSel" label="容器"
                                :tip="'容器 ID：' + (selState?.containerId || '-') + '（状态 ' + (selState?.containerState || '-') + '）'"
                                :value="selState?.container || '-'" />
                            <common-info-row grey  v-else label="Master PID" :value="String(selState?.pid || '-')" />
                            <common-info-row grey label="站点目录" :value="selState?.docRoot || '-'" action-text="打开"
                                @action="openPath(selState?.docRoot)" />
                            <common-info-row grey label="nginx.conf" :value="selState?.configPath || '-'" action-text="打开"
                                @action="openPath(selState?.configPath)" />
                            <common-info-row grey label="运行目录" tip="启动时 -p prefix 指定的目录：其中 conf/ 与 logs/ 与之相对"
                                :value="selState?.prefix || '-'" action-text="打开" @action="openPath(selState?.prefix)" />
                            <common-info-row grey v-if="selCfg?.domain || selState?.domain" label="绑定域名"
                                :value="selCfg?.domain || selState?.domain || ''" action-text="访问"
                                @action="openSite(selCfg?.port ?? 0)" />
                            <common-info-row grey v-if="selCfg?.phpMode || selState?.phpMode" label="PHP 后端"
                                :value="phpBackendText()" />
                        </div>
                    </div>
                </div>
            </a-card>
        </a-col>
    </a-row>

    <!-- 新建/编辑实例：拆到子组件，父只传可见性 + 编辑目标（或预置镜像），保存后 emit('saved') -->
    <InstFormModal v-model="instVisible" :instance="instEditTarget" :preset-image="instPresetImage"
        @saved="onInstSaved" />

    <!-- 版本管理：本地/线上/Docker 与安装进度都由子组件自管；跨弹窗动作（以镜像新建 / 去编辑）回传父组件 -->
    <VersionsModal v-model="versionsVisible" :instances="instances" :states="states"
        @changed="refreshStates" @new-from-image="onNewFromImage" @edit-instance="onEditInstanceById" />

    <!-- nginx.conf 文本编辑：子组件按 instanceId 拉取/保存，保存成功后刷新状态 -->
    <ConfModal v-model="confVisible" :instance-id="confInstId" :instance-name="cfgNameOf(confInstId)"
        :is-docker="confInstDocker" @saved="refreshStates" />

    <!-- 配置检查结果：只读展示 nginx -t 输出，内容由父组件传入 -->
    <CheckModal v-model="checkVisible" :output="checkOutput" />

    <!-- 错误/访问日志（实时）：点「查看日志」弹出，打开才轮询（轮询由子组件自管） -->
    <LogModal v-model="logVisible" :instance-id="selectedId" :instance-name="selCfg?.name || ''"
        :port="selCfg?.port || 0" :running="!!selState?.running" :is-docker="isDockerSel" />
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, onUnmounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { useRoute } from "vue-router";
import InstFormModal from "./components/InstFormModal.vue";
import VersionsModal from "./components/VersionsModal.vue";
import ConfModal from "./components/ConfModal.vue";
import CheckModal from "./components/CheckModal.vue";
import LogModal from "./components/LogModal.vue";
import { phpModeLabel, type InstConfig } from "./types";
import { useReorder, applyOrder, loadOrder } from "@/utils/reorder";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

/** Docker 实例的版本就是镜像 tag：列表里只展示 tag（1.27-alpine），完整镜像名走 tooltip */
function imgTag(image: string): string {
    const s = String(image || "");
    const i = s.indexOf(":");
    return i >= 0 ? s.slice(i + 1) : (s || "Docker");
}

/* ============================ 状态 ============================ */
const instances = ref<InstConfig[]>([]);
/* 拖拽排序：实例列表顺序存入用户配置 nginx:list（与主页分开，随导出搬运） */
const nginxOrder = ref<string[]>([]);
const nginxReo = useReorder("nginx:list", {
    getVisible: () => instances.value.map((i) => i.id),
    onReordered: (nv) => { nginxOrder.value = nv; void GoTools?.SetListOrder?.("nginx:list", nv); },
});
const orderedInstances = computed(() => applyOrder(instances.value, nginxOrder.value, (i) => i.id));
const states = reactive<Record<string, any>>({});
const selectedId = ref("");
/** 日志弹窗可见性：轮询与内容都在 LogModal 内，父只开关 */
const logVisible = ref(false);
/** 重启进行中：nginx 重启是先停后起、状态会短暂翻转，用它驱动按钮 loading 与「正在重启」提示 */
const restartBusy = ref(false);
let srvTimer: any = null;
const route = useRoute();
let offSelInst: any = null;
let offOpenPanel: any = null;

const selCfg = computed(() => instances.value.find((i) => i.id === selectedId.value) || null);
const selState = computed(() => (selectedId.value ? states[selectedId.value] : null));
/** 当前实例是不是容器模式：配置优先（列表里就能看到刚改完还没启动的实例），状态兜底 */
const isDockerSel = computed(() => (selCfg.value?.mode || selState.value?.mode) === "docker");

/** 右侧指标卡：版式对齐磁盘总览，把运行态/时长/worker/请求速率/端口/版本收成一排卡片 */
const statCards = computed(() => {
    const st = selState.value, c = selCfg.value;
    if (!c) return [] as any[];
    const running = !!st?.running;
    const docker = (c.mode || st?.mode) === "docker";
    return [
        {
            key: "state", title: "运行状态",
            value: restartBusy.value ? "正在重启" : (running ? "运行中" : "已停止"),
            sub: docker ? (st?.container || "容器") : (st?.pid ? "PID " + st.pid : "未启动"),
            icon: "server", tone: running ? "green" : "gray", ready: running,
            tip: restartBusy.value ? "重启进行中：先停后起，站点会短暂不可用"
                : (running ? "端口已监听，进程在跑" : "实例当前没有对外服务"),
        },
        {
            key: "uptime", title: "运行时长", value: fmtUptimeS(st?.uptimeSeconds),
            sub: running ? "自本次启动累计" : "实例未运行", icon: "uptime", tone: "blue", ready: running,
            tip: "进程存活时长；重启后归零",
        },
        {
            key: "worker", title: "Worker 进程", value: docker ? "—" : (st?.workers ?? "—"),
            sub: docker ? "容器内不计" : "nginx worker_processes", icon: "cpu", tone: "cyan", ready: !docker && running,
            tip: docker ? "容器里的 nginx 进程不在本机进程列表，从容器外统不到" : "本机 nginx 的 worker 进程数（含 master）",
        },
        {
            key: "rps", title: "请求速率", value: st?.requestsPerSec ? st.requestsPerSec.toFixed(2) : "—",
            sub: "req/s · access.log 估算", icon: "gauge", tone: "orange", ready: running && !!st?.requestsPerSec,
            tip: "按 access.log 行数 / 运行秒数粗估，非精确实时监控",
        },
        {
            key: "port", title: "HTTP 端口", value: c.port ? ":" + c.port : "—",
            sub: c.sslPort ? "HTTPS :" + c.sslPort : "未启用 HTTPS", icon: "plug", tone: "purple", ready: !!c.port,
            tip: c.sslPort ? "HTTP 与 HTTPS 监听端口" : "仅 HTTP；启用 HTTPS 需在配置里加 ssl server 块",
        },
        {
            key: "ver", title: docker ? "镜像" : "Nginx 版本", value: docker ? imgTag(st?.image || c.image || "") : (st?.version || c.version || "—"),
            sub: docker ? "容器镜像 tag" : "本机插件版本", icon: "tag", tone: "red", ready: docker ? !!(st?.image || c.image) : !!(st?.version || c.version),
            tip: docker ? "Docker 实例的版本由镜像 tag 决定" : "nginx -v 对应的本机安装版本",
        },
    ];
});

/* 弹窗可见性：各弹窗内部自管数据与表单，父只留开关 + 打开时的上下文 */
const instVisible = ref(false);
/** 编辑目标：传入实例即编辑，null 即新建 */
const instEditTarget = ref<InstConfig | null>(null);
/** 新建时预置为 Docker 模式并选定的镜像（版本管理「以镜像新建实例」用） */
const instPresetImage = ref("");

const versionsVisible = ref(false);
/** 有多少实例跑在容器里（它们的版本由镜像 tag 决定，不占本机版本库） */
const dockerInstCount = computed(() => instances.value.filter((i) => i.mode === "docker").length);

const confVisible = ref(false);
const confInstId = ref("");
/** 配置弹窗里的提示按模式分流：容器模式既不能 nginx -t 也不会回滚 */
const confInstDocker = computed(() => instances.value.find((i) => i.id === confInstId.value)?.mode === "docker");

const checkVisible = ref(false);
const checkOutput = ref("");

/* 镜像清单只用来标记「实例引用的镜像本机已没有」（表单里的候选由 InstFormModal 自管） */
const dockerImageList = ref<string[]>([]);

/** 只刷新镜像清单（不写表单）：用来标记实例引用的镜像本机是否还在 */
async function probeDockerImages() {
    try { dockerImageList.value = (await GoTools.NginxSrvDockerImages()) || []; }
    catch (e) { /* 引擎未就绪就不做校验，免得误报 */ }
}
/** 镜像清单非空且不含它 → 悬空引用（镜像被删了，实例还指着） */
function imgMissing(image: string): boolean {
    return !!image && dockerImageList.value.length > 0 && !dockerImageList.value.includes(image);
}

/* ============================ 小工具 ============================ */
function fmtUptimeS(sec?: number): string {
    if (!sec || sec <= 0) return "-";
    const d = Math.floor(sec / 86400); const h2 = Math.floor((sec % 86400) / 3600);
    const m = Math.floor((sec % 3600) / 60);
    if (d > 0) return `${d}d ${h2}h ${m}m`; if (h2 > 0) return `${h2}h ${m}m`; return `${m}m`;
}
function cfgNameOf(id: string): string {
    return instances.value.find((i) => i.id === id)?.name || id;
}

/* ============================ 数据加载 ============================ */
async function loadInstances() {
    try { instances.value = (await GoTools.NginxSrvListInstances()) || []; }
    catch (e) { instances.value = []; }
    if (!selectedId.value && instances.value.length) selectInst(instances.value[0].id);
    refreshStates();
}
async function refreshStates() {
    // 并发问：实例串行 await 就是 N 次往返，Web 模式跑在远程服务器上时尤其明显
    await Promise.all(instances.value.map(async (inst) => {
        try {
            states[inst.id] = await GoTools.NginxSrvStatus(inst.id);
        } catch (e) { /* 单个实例失败不影响其它实例 */ }
    }));
}
function selectInst(id: string) {
    selectedId.value = id;
    refreshStates();
    // 点行只做选中/看详情，不自动启动；启动走双击行或明确的「启动」按钮（doStart → startInstById）
}
/** 双击行：未启动实例直接拉起（与「启动」按钮同一条路）；已启动只选中 */
function dblInst(inst: any) {
    if (!states[inst.id]?.running) startInstById(inst.id, inst.name);
}

/* ============================ 日志 ============================ */
/** 点「查看日志」：确保选中该实例后打开日志弹窗（轮询由 LogModal 自管） */
function openLogModal(id?: string) {
    const target = id || selectedId.value;
    if (!target) { Message.warning("请先选择一个实例"); return; }
    if (target !== selectedId.value) selectInst(target);
    logVisible.value = true;
}

/* ============================ 实例操作 ============================ */
/** 停止中：停止要等守护进程退出（容器模式更慢），期间锁定按钮防连点 */
const stopBusy = ref(false);
/** 正在启动中的实例 id：防止连点重复拉起（StartInstance 对已运行会报错，故只在未运行时调用） */
const startingIds = new Set<string>();
async function startInstById(id: string, name: string) {
    if (!id || startingIds.has(id)) return;
    startingIds.add(id);
    try { await GoTools.NginxSrvStart(id); Message.success(`实例 ${name} 已启动`); }
    catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
    finally { startingIds.delete(id); refreshStates(); }
}
async function doStart() {
    const c = selCfg.value; if (!c) return;
    await startInstById(c.id, c.name);
}
async function doStop() {
    const c = selCfg.value; if (!c) return;
    if (stopBusy.value) return;
    stopBusy.value = true;
    try { await GoTools.NginxSrvStop(c.id); Message.success(`实例 ${c.name} 已停止`); }
    catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
    finally { stopBusy.value = false; }
    refreshStates();
}
async function doRestart() {
    const c = selCfg.value; if (!c || restartBusy.value) return;
    restartBusy.value = true;
    try { await GoTools.NginxSrvRestart(c.id); Message.success(`实例 ${c.name} 已重启`); }
    catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
    finally { restartBusy.value = false; }
    refreshStates();
}
async function doReload() {
    const c = selCfg.value; if (!c) return;
    const docker = (states[c.id]?.mode || c.mode) === "docker";
    try { await GoTools.NginxSrvReload(c.id); Message.success(docker ? "容器已重启，新配置已生效" : "配置已平滑重载"); }
    catch (e: any) { Message.error("重载失败: " + (e?.message || e)); }
    refreshStates();
}
async function doCheck(id: string) {
    try {
        checkOutput.value = await GoTools.NginxSrvCheckConfig(id);
    } catch (e: any) {
        checkOutput.value = String(e?.message || e);
    }
    checkVisible.value = true;
}
function openSite(port: number) {
    window.open(`http://127.0.0.1:${port}/`, "_blank");
}
/** 用系统默认程序打开文件或所在目录（Windows 资源管理器 / 默认编辑器）；属桌面能力，失败给提示 */
async function openPath(p?: string) {
    if (!p) { Message.warning("路径为空，无法打开"); return; }
    try { await GoTools.DiskOpenFile(p); }
    catch (e: any) { Message.error("打开失败：" + String(e?.message || e)); }
}
/** PHP 后端一行文案：接入方式 · 版本 :端口 */
function phpBackendText(): string {
    const m = selCfg.value?.phpMode || selState.value?.phpMode || "";
    if (!m) return "-";
    const v = selCfg.value?.phpVersion || selState.value?.phpVersion || "";
    const p = selCfg.value?.phpPort || selState.value?.phpPort || 0;
    return `${phpModeLabel(m)} · ${v}${p ? " :" + p : ""}`;
}
async function onInstAction(v: string, inst: InstConfig) {
    if (v === "start") {
        try { await GoTools.NginxSrvStart(inst.id); Message.success(`实例 ${inst.name} 已启动`); }
        catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
        refreshStates();
    } else if (v === "stop") {
        try { await GoTools.NginxSrvStop(inst.id); Message.success(`实例 ${inst.name} 已停止`); }
        catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
        refreshStates();
    } else if (v === "restart") {
        restartBusy.value = true;
        try { await GoTools.NginxSrvRestart(inst.id); Message.success(`实例 ${inst.name} 已重启`); }
        catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
        finally { restartBusy.value = false; }
        refreshStates();
    } else if (v === "reload") {
        doReload();
    } else if (v === "check") {
        doCheck(inst.id);
    } else if (v === "conf") {
        openConf(inst.id);
    } else if (v === "open") {
        openSite(inst.port);
    } else if (v === "logs") {
        openLogModal(inst.id);
    } else if (v === "edit") {
        openInstModal(inst);
    } else if (v === "del") {
        Modal.warning({
            title: "删除实例", content: `确定删除实例「${inst.name}」？运行中会先停止；站点目录保留。`, hideCancel: false,
            onOk: async () => {
                try {
                    await GoTools.NginxSrvRemoveInstance(inst.id);
                    delete states[inst.id];
                    if (selectedId.value === inst.id) selectedId.value = "";
                    await loadInstances();
                } catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
            },
        });
    }
}

/* ============================ 新建/编辑实例（弹窗子组件的父侧胶水） ============================ */
function openInstModal(inst?: InstConfig) {
    instEditTarget.value = inst || null;
    instPresetImage.value = "";
    instVisible.value = true;
}
/** 版本管理里「以镜像新建实例」：关版本弹窗，开新建弹窗并预置 Docker 镜像 */
function onNewFromImage(image: string) {
    versionsVisible.value = false;
    instEditTarget.value = null;
    instPresetImage.value = image;
    instVisible.value = true;
}
/** 版本管理里「去编辑实例」：关版本弹窗，选中该实例并打开编辑弹窗 */
function onEditInstanceById(id: string) {
    const inst = instances.value.find((i) => i.id === id);
    if (!inst) return;
    versionsVisible.value = false;
    selectInst(id);
    openInstModal(inst);
}
/** 新建/编辑保存成功：重载列表；若切了版本/镜像且实例在跑，提示重启才生效 */
function onInstSaved(payload: { config: InstConfig; switched: boolean }) {
    const wasRunning = !!payload.config.id && !!states[payload.config.id]?.running;
    loadInstances();
    if (payload.switched && wasRunning) setTimeout(() => askRestartAfterSwitch(payload.config), 240);
}

/** 切了版本/镜像又没重启：跑着的还是旧版本，问一句要不要现在就换过去 */
function askRestartAfterSwitch(inst: InstConfig) {
    const what = inst.mode === "docker" ? `镜像 ${inst.image}` : `Nginx ${inst.version}`;
    Modal.confirm({
        title: "版本已切换",
        content: `${what} 已写入配置，但实例「${inst.name}」还在跑旧版本；重启后才会生效（期间会短暂中断服务）。`,
        okText: "立即重启",
        cancelText: "稍后手动重启",
        onOk: async () => {
            try {
                await GoTools.NginxSrvRestart(inst.id);
                Message.success(`实例 ${inst.name} 已重启，新版本已生效`);
            } catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
            refreshStates();
        },
    });
}

/* ============================ 版本管理 ============================ */
function openVersionsModal() {
    versionsVisible.value = true;
}

/* ============================ nginx.conf ============================ */
function openConf(id: string) {
    confInstId.value = id;
    confVisible.value = true;
}

/* ============================ 生命周期 ============================ */
onMounted(async () => {
    await loadInstances();
    nginxOrder.value = await loadOrder("nginx:list");
    // 镜像清单不进表单，只用来标记「实例引用的镜像本机已没有」
    probeDockerImages();
    // 主页点击服务实例进入（?inst=）：选中该实例展示详情
    const wantId = String(route.query.inst ?? "");
    if (wantId && instances.value.some((i) => i.id === wantId)) selectInst(wantId);
    // 下载抽屉点「装版本」进入（?open=versions）：冷启动直接弹版本管理
    if (String(route.query.open ?? "") === "versions") openVersionsModal();
    srvTimer = setInterval(refreshStates, 3000);
    // 单例窗口已打开时，主页再次点击其他实例 → 通过事件切换
    offSelInst = Events.On("tool:select-inst", (ev: any) => {
        const p = ev?.data ?? ev;
        if (p?.type !== "nginx" || !p?.id) return;
        if (instances.value.some((i) => i.id === String(p.id))) selectInst(String(p.id));
    });
    // 单例窗口已开着时，抽屉再点装任务 → 通过事件弹出版本管理
    offOpenPanel = Events.On("tool:open-panel", (ev: any) => {
        const p = ev?.data ?? ev;
        if (p?.type === "nginx" && p?.panel === "versions") openVersionsModal();
    });
});
onUnmounted(() => {
    if (offSelInst) offSelInst();
    if (offOpenPanel) offOpenPanel();
    if (srvTimer) clearInterval(srvTimer);
});
</script>

<style scoped>
.nginx-page {
    height: 100%;
    min-height: 0;
    flex-wrap: nowrap;
}

.side-col,
.main-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.side-card {
    height: 100%;
    display: flex;
    flex-direction: column;
}

.side-card :deep(.arco-card-body) {
    flex: 1;
    overflow: auto;
}

.main-panel {
    height: 100%;
    display: flex;
    flex-direction: column;
}

.main-panel :deep(.arco-card-body) {
    flex: 1;
    overflow: auto;
    display: flex;
    flex-direction: column;
}

.empty-card :deep(.arco-card-body) {
    align-items: center;
    justify-content: center;
}

.srv-head-actions {
    display: flex;
    gap: 6px;
}

/* 左侧实例列表：卡片式条目，选中态用左侧短竖条 + 按需投影 */
.side-title {
    font-size: var(--base-size-default);
    font-weight: 600;
}

.side-count {
    margin-left: calc(var(--base-margin) / 2);
    padding: 0 calc(var(--base-padding) / 2);
    border-radius: var(--base-radius-default);
    background: var(--color-fill-2);
    color: var(--color-text-3);
    font-size: var(--base-size-small);
    line-height: 16px;
    vertical-align: 1px;
}

.inst-list {
    display: flex;
    flex-direction: column;
    gap: 6px;
    padding: calc(var(--base-padding) / 2);
}

/* 卡片外壳（边框/圆角/选中竖条/hover 投影）由 common-item-card 提供，这里只留标题内容样式 */
.inst-name-tip {
    flex: 1;
    min-width: 0;
}

.inst-name {
    display: block;
    font-weight: 600;
    color: var(--color-text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.inst-more {
    cursor: pointer;
    color: var(--color-text-3);
    flex: none;
}

.inst-more:hover {
    color: rgb(var(--primary-6));
}

.inst-line2 {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
}

.inst-port {
    font-family: Consolas, Menlo, monospace;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.inst-empty {
    color: var(--color-text-3);
    font-size: var(--base-size-small);
    text-align: center;
    padding: 20px 0;
}

/* 右侧指标卡：与磁盘总览同一套（彩色圆角图标 + 标签/数值同排 + 下方明细） */
.ov-body {
    padding: 14px;
    display: flex;
    flex-direction: column;
    gap: 16px;
}

.stat-row {
    margin: 0;
}

/* 重启提示条：与下方指标卡留一点间距 */
.restart-tip {
    margin-bottom: 4px;
}

.quick-bar {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
}

/* 站点信息：长文本省略 + tooltip，单列信息行 */
.info-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.info-title {
    font-size: var(--base-size-default);
    font-weight: 600;
    color: var(--color-text-1);
}

.info-list {
    display: flex;
    flex-direction: column;
}
</style>
