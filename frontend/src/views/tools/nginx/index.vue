<template>
    <a-row class="nginx-page" :gutter="6">
        <!-- 左侧：实例列表 -->
        <a-col :span="7" class="side-col">
            <a-card class="side-card" size="small" :body-style="{ padding: 0 }">
                <template #title>本地服务</template>
                <template #extra>
                    <div class="srv-head-actions">
                        <a-button size="small" title="新建实例" @click="openInstModal()">新建实例</a-button>
                        <a-button size="small" title="版本管理" @click="openVersionsModal()">版本管理</a-button>
                    </div>
                </template>
                <a-list :bordered="false" size="small">
                    <a-list-item v-for="inst in instances" :key="inst.id"
                        :class="{ 'inst-active': selectedId === inst.id }" @click="selectInst(inst.id)">
                        <a-space>
                            <a-badge :status="states[inst.id]?.running ? 'success' : 'normal'" />
                            <span class="conn-name" :title="inst.name + ':' + inst.port">{{ inst.name }}</span>
                            <a-tag color="arcoblue" size="small">v{{ inst.version }}</a-tag>
                        </a-space>
                        <template #actions>
                            <a-dropdown trigger="hover" @select="(v: any) => onInstAction(v, inst)">
                                <icon-more class="conn-more" @click.stop />
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
                    </a-list-item>
                    <a-list-item v-if="instances.length === 0">
                        <div class="conn-empty">暂无实例，点击右上"新建实例"</div>
                    </a-list-item>
                </a-list>
            </a-card>
        </a-col>

        <!-- 右侧：选中实例详情 + 日志 -->
        <a-col :span="17" class="main-col">
            <a-card v-if="!selectedId" class="main-panel empty-card">
                <a-empty description="从左侧选择一个 Nginx 实例查看状态与日志" />
            </a-card>
            <a-card v-else class="main-panel" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space>
                        <a-badge :status="selState?.running ? 'success' : 'normal'" />
                        <span>{{ selCfg?.name }}</span>
                        <a-tag color="arcoblue" size="small">:{{ selCfg?.port }}</a-tag>
                        <a-tag v-if="selCfg?.sslPort" color="purple" size="small">https :{{ selCfg.sslPort }}</a-tag>
                    </a-space>
                </template>
                <template #extra>
                    <a-space>
                        <a-button size="small" type="primary" :disabled="selState?.running"
                            @click="doStart">启动</a-button>
                        <a-button size="small" status="danger" :disabled="!selState?.running"
                            @click="doStop">停止</a-button>
                        <a-button size="small" @click="doRestart">重启</a-button>
                        <a-button size="small" :disabled="!selState?.running" @click="doReload">重载</a-button>
                    </a-space>
                </template>
                <div class="detail-grid">
                    <div class="ls-item"><span>Nginx 版本</span><b>{{ selState?.version || selCfg?.version || '-' }}</b>
                    </div>
                    <div class="ls-item"><span>运行状态</span>
                        <b :class="selState?.running ? 'ok' : 'no'">{{ selState?.running ? '运行中' : '已停止' }}</b>
                    </div>
                    <div class="ls-item"><span>Master PID</span><b>{{ selState?.pid || '-' }}</b></div>
                    <div class="ls-item"><span>Worker 数</span><b>{{ selState?.workers ?? '-' }}</b></div>
                    <div class="ls-item"><span>运行时长</span><b>{{ fmtUptimeS(selState?.uptimeSeconds) }}</b></div>
                    <div class="ls-item"><span>req/s（估算）</span><b>{{ selState?.requestsPerSec ? selState.requestsPerSec.toFixed(2) : '-' }}</b></div>
                    <div class="ls-item" title="站点根目录"><span>站点目录</span><b class="ellipsis">{{
                        selState?.docRoot || '-' }}</b></div>
                    <div class="ls-item" title="conf 路径"><span>nginx.conf</span><b class="ellipsis">{{
                        selState?.configPath || '-' }}</b></div>
                    <div class="ls-item" title="实例运行目录（-p prefix）"><span>运行目录</span><b class="ellipsis">{{
                        selState?.prefix || '-' }}</b></div>
                </div>
                <div class="log-head">
                    <span>错误/访问日志（实时）</span>
                    <a-space :size="6">
                        <a-button size="mini" :disabled="!selState?.running" @click="openSite(selCfg?.port ?? 0)">
                            <template #icon><icon-launch /></template>打开站点
                        </a-button>
                        <a-button size="mini" :disabled="!selState?.running" @click="doReload">
                            <template #icon><icon-sync /></template>重载
                        </a-button>
                        <a-button size="mini" @click="openConf(selectedId)">
                            <template #icon><icon-settings /></template>nginx.conf
                        </a-button>
                        <a-button size="mini" @click="doCheck(selectedId)">
                            <template #icon><icon-safe /></template>配置检查
                        </a-button>
                        <a-button size="mini" @click="logLines = []">
                            <template #icon><icon-delete /></template>清空
                        </a-button>
                    </a-space>
                </div>
                <div ref="logBoxRef" class="ls-log">
                    <div v-for="(l, i) in logLines" :key="i" class="ls-line">{{ l }}</div>
                    <div v-if="logLines.length === 0" class="conn-empty">暂无日志</div>
                </div>
            </a-card>
        </a-col>
    </a-row>

    <!-- 新建/编辑实例 -->
    <a-modal v-model:visible="instVisible" :title="instForm.id ? '编辑实例' : '新建实例'" :width="520"
        @before-ok="saveInstance">
        <a-form :model="instForm" layout="vertical">
            <a-form-item field="name" label="实例名称" required>
                <a-input v-model="instForm.name" placeholder="如 site-dev" />
            </a-form-item>
            <a-form-item field="version" label="Nginx 版本" required>
                <a-select v-model="instForm.version" placeholder="选择已安装版本" :options="versionOptions" />
            </a-form-item>
            <a-form-item field="port" label="HTTP 端口">
                <a-input-number v-model="instForm.port" :min="1" :max="65535" @blur="checkPortTaken" />
            </a-form-item>
            <a-form-item field="sslPort" label="HTTPS 端口（0 = 不启用）">
                <a-input-number v-model="instForm.sslPort" :min="0" :max="65535" />
            </a-form-item>
            <a-form-item field="docRoot" label="站点根目录（留空 = 程序目录 data/www/实例名）">
                <a-input-search v-model="instForm.docRoot" placeholder="如 D:\www\site" search-button
                    button-text="选择目录" @click-btn="chooseDocRoot" />
            </a-form-item>
            <a-form-item field="workers" label="worker_processes（0 = auto）">
                <a-input-number v-model="instForm.workers" :min="0" :max="64" />
            </a-form-item>
            <a-form-item field="stubStatus" label="开放 /stub_status 状态页">
                <a-switch v-model="instForm.stubStatus" />
            </a-form-item>
        </a-form>
    </a-modal>

    <!-- 版本管理 -->
    <a-modal v-model:visible="versionsVisible" title="Nginx 版本管理" :width="780" :footer="false" unmount-on-close
        :body-style="{ padding: '12px 16px', maxHeight: '72vh', overflowY: 'auto' }">
        <div class="ver-block">
            <div class="ver-title">本地可用版本</div>
            <a-table :data="localVersions" size="small" :pagination="false" row-key="version" :scroll="{ y: 200 }"
                :columns="[
                    { title: '版本', dataIndex: 'version' },
                    { title: '来源', dataIndex: 'source', width: 90, slotName: 'source' },
                    { title: '路径', dataIndex: 'path', ellipsis: true, tooltip: true },
                    { title: '操作', dataIndex: 'ops', width: 90, slotName: 'ops' },
                ]">
                <template #source="{ record }">
                    <a-tag size="small" :color="record.source === 'bundled' ? 'purple' : record.source === 'downloaded' ? 'green' : 'orange'">{{ srcLabel(record.source) }}</a-tag>
                </template>
                <template #ops="{ record }">
                    <a-button size="mini" status="danger" :disabled="record.source === 'bundled'"
                        @click="removeVersion(record)">删除</a-button>
                </template>
            </a-table>
            <a-row class="add-local" :gutter="8">
                <a-col :span="16">
                    <a-input v-model="localDirInput" placeholder="注册本机已有 Nginx：填写 nginx.exe 所在目录" />
                </a-col>
                <a-col :span="4">
                    <a-button block @click="chooseLocalDir">浏览…</a-button>
                </a-col>
                <a-col :span="4">
                    <a-button type="primary" block @click="addLocalDir">添加</a-button>
                </a-col>
            </a-row>
        </div>
        <a-divider style="margin: 12px 0" />
        <div class="ver-block">
            <div class="ver-title">
                线上版本
                <a-button size="mini" :loading="remoteLoading" @click="loadRemoteVersions">刷新</a-button>
            </div>
            <a-spin :loading="remoteLoading" style="width:100%">
                <a-table :data="remoteVersions" size="small" :pagination="false" row-key="version" :scroll="{ y: 220 }"
                    :columns="[
                        { title: '版本', dataIndex: 'version', width: 110 },
                        { title: '产品线', dataIndex: 'release', width: 90 },
                        { title: '包大小', dataIndex: 'size', width: 100, slotName: 'size' },
                        { title: '状态', dataIndex: 'st', width: 150, slotName: 'st' },
                        { title: '操作', dataIndex: 'ops', width: 100, slotName: 'ops' },
                    ]">
                    <template #size="{ record }">{{ fmtBytes(record.size) }}</template>
                    <template #st="{ record }">
                        <a-tag v-if="installedSet.has(record.version)" color="green" size="small">已安装</a-tag>
                        <a-tag v-else-if="installProgress[record.version]" :color="installProgress[record.version].stage === 'error' ? 'red' : 'arcoblue'" size="small">
                            {{ stageText(installProgress[record.version]) }}
                        </a-tag>
                        <a-tag v-else size="small" color="gray">未安装</a-tag>
                    </template>
                    <template #ops="{ record }">
                        <a-button v-if="!installedSet.has(record.version)" size="mini" type="primary"
                            :disabled="!!installProgress[record.version]" @click="installVersion(record)">安装</a-button>
                        <a-button v-else size="mini" @click="loadLocalVersions">已安装</a-button>
                    </template>
                </a-table>
            </a-spin>
            <div class="offline-tip">离线安装：将 nginx zip 放入 <a-link copyable :content="pluginsDirVal">{{ pluginsDirVal || '…' }}</a-link> 后刷新本地版本</div>
        </div>
        <div v-if="installLogs.length" class="ver-block install-log">
            <div class="ver-title">安装日志</div>
            <div class="ls-log small">
                <div v-for="(l, i) in installLogs" :key="i" class="ls-line">{{ l }}</div>
            </div>
        </div>
    </a-modal>

    <!-- nginx.conf 文本编辑 -->
    <a-modal v-model:visible="confVisible" :title="`nginx.conf - ${cfgNameOf(confInstId)}`" :width="760">
        <a-textarea v-model="confText" :auto-size="{ minRows: 16, maxRows: 24 }" spellcheck="false" />
        <template #footer>
            <div class="ini-tip">保存前自动备份并执行配置检查，检查失败将回滚。</div>
            <a-space>
                <a-button @click="confVisible = false">取消</a-button>
                <a-button type="primary" @click="saveConfText">保存</a-button>
            </a-space>
        </template>
    </a-modal>

    <!-- 配置检查结果 -->
    <a-modal v-model:visible="checkVisible" title="nginx -t 配置检查" :width="600" :footer="false">
        <pre class="check-out">{{ checkOutput }}</pre>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onMounted, onUnmounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { useRoute } from "vue-router";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

interface InstConfig { id: string; name: string; version: string; port: number; sslPort: number; docRoot: string; workers: number; stubStatus: boolean }

/* ============================ 状态 ============================ */
const instances = ref<InstConfig[]>([]);
const states = reactive<Record<string, any>>({});
const selectedId = ref("");
const logLines = ref<string[]>([]);
const logBoxRef = ref<HTMLElement | null>(null);
let logTimer: any = null;
let srvTimer: any = null;
const route = useRoute();
let offInstall: any = null;
let offSelInst: any = null;

const selCfg = computed(() => instances.value.find((i) => i.id === selectedId.value) || null);
const selState = computed(() => (selectedId.value ? states[selectedId.value] : null));

const instForm = reactive<InstConfig>({
    id: "", name: "", version: "", port: 8090, sslPort: 0, docRoot: "", workers: 0, stubStatus: true,
});
const instVisible = ref(false);

const versionsVisible = ref(false);
const localVersions = ref<any[]>([]);
const remoteVersions = ref<any[]>([]);
const remoteLoading = ref(false);
const installProgress = reactive<Record<string, any>>({});
const installLogs = ref<string[]>([]);
const localDirInput = ref("");
const pluginsDirVal = ref("");

const confVisible = ref(false);
const confInstId = ref("");
const confText = ref("");

const checkVisible = ref(false);
const checkOutput = ref("");

const versionOptions = computed(() => localVersions.value.map((v: any) => ({ label: `${v.version}（${srcLabel(v.source)}）`, value: v.version })));
const installedSet = computed(() => new Set<string>(localVersions.value.map((v: any) => v.version)));

/* ============================ 小工具 ============================ */
function srcLabel(s: string): string {
    return s === "bundled" ? "内置" : s === "downloaded" ? "下载" : "本机";
}
function stageText(p: any): string {
    if (!p) return "";
    if (p.stage === "downloading") return `下载中 ${p.percent || 0}%`;
    if (p.stage === "extracting") return "解压中";
    if (p.stage === "done") return "完成";
    if (p.stage === "info") return "准备中";
    return "失败";
}
function fmtBytes(b: number): string {
    if (!b) return "-";
    const units = ["B", "KB", "MB", "GB"]; let i = 0; let v = b;
    while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
    return v.toFixed(i > 0 ? 1 : 0) + " " + units[i];
}
function fmtUptimeS(sec?: number): string {
    if (!sec || sec <= 0) return "-";
    const d = Math.floor(sec / 86400); const h2 = Math.floor((sec % 86400) / 3600);
    const m = Math.floor((sec % 3600) / 60);
    if (d > 0) return `${d}d ${h2}h ${m}m`; if (h2 > 0) return `${h2}h ${m}m`; return `${m}m`;
}
function cfgNameOf(id: string): string {
    return instances.value.find((i) => i.id === id)?.name || id;
}
function pushInstallLog(txt: string) {
    if (!txt) return;
    const t = new Date();
    const pad = (n: number) => n.toString().padStart(2, "0");
    installLogs.value.push(`[${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}] ${txt}`);
    if (installLogs.value.length > 200) installLogs.value.splice(0, installLogs.value.length - 200);
}

/* ============================ 数据加载 ============================ */
async function loadLocalVersions() {
    try { localVersions.value = (await GoTools.NginxSrvListVersions()) || []; }
    catch (e) { localVersions.value = []; }
}
async function loadInstances() {
    try { instances.value = (await GoTools.NginxSrvListInstances()) || []; }
    catch (e) { instances.value = []; }
    if (!selectedId.value && instances.value.length) selectInst(instances.value[0].id);
    refreshStates();
}
async function refreshStates() {
    for (const inst of instances.value) {
        try {
            states[inst.id] = await GoTools.NginxSrvStatus(inst.id);
            if (selectedId.value === inst.id && !logTimer) startLogPolling();
        } catch (e) { /* ignore */ }
    }
}
function selectInst(id: string) {
    selectedId.value = id;
    logLines.value = [];
    startLogPolling();
    refreshStates();
}

/* ============================ 日志轮询 ============================ */
async function pollLogs() {
    if (!selectedId.value) return;
    try {
        const lines = (await GoTools.NginxSrvLogs(selectedId.value, 300)) || [];
        if (lines.join("\n") !== logLines.value.join("\n")) {
            logLines.value = lines;
            nextTick(() => { if (logBoxRef.value) logBoxRef.value.scrollTop = logBoxRef.value.scrollHeight; });
        }
    } catch (e) { /* ignore */ }
}
function startLogPolling() {
    if (logTimer) clearInterval(logTimer);
    pollLogs();
    logTimer = setInterval(pollLogs, 2000);
}

/* ============================ 实例操作 ============================ */
async function doStart() {
    const c = selCfg.value; if (!c) return;
    try { await GoTools.NginxSrvStart(c.id); Message.success(`实例 ${c.name} 已启动`); }
    catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
    refreshStates();
}
async function doStop() {
    const c = selCfg.value; if (!c) return;
    try { await GoTools.NginxSrvStop(c.id); Message.success(`实例 ${c.name} 已停止`); }
    catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
    refreshStates();
}
async function doRestart() {
    const c = selCfg.value; if (!c) return;
    try { await GoTools.NginxSrvRestart(c.id); Message.success(`实例 ${c.name} 已重启`); }
    catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
    refreshStates();
}
async function doReload() {
    const c = selCfg.value; if (!c) return;
    try { await GoTools.NginxSrvReload(c.id); Message.success("配置已平滑重载"); }
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
        try { await GoTools.NginxSrvRestart(inst.id); Message.success(`实例 ${inst.name} 已重启`); }
        catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
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
        selectInst(inst.id);
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

function openInstModal(inst?: InstConfig) {
    if (inst) { Object.assign(instForm, { ...inst }); }
    else {
        Object.assign(instForm, { id: "", name: "", version: localVersions.value[0]?.version || "", port: 8090, sslPort: 0, docRoot: "", workers: 0, stubStatus: true });
    }
    if (localVersions.value.length === 0) loadLocalVersions();
    instVisible.value = true;
}
async function checkPortTaken() {
    if (!instForm.port) return;
    try {
        const suggest = await GoTools.NginxSrvSuggestPort(instForm.port);
        if (suggest && suggest !== instForm.port) Message.warning(`端口 ${instForm.port} 已被占用，建议改为 ${suggest}`);
    } catch (e) { /* ignore */ }
}
async function chooseDocRoot() {
    try { const dir = await GoTools.NginxSrvChooseDir(); if (dir) instForm.docRoot = dir; }
    catch (e) { /* cancel */ }
}
async function saveInstance(): Promise<boolean> {
    if (!instForm.name) { Message.warning("请填写实例名称"); return false; }
    if (!instForm.version) { Message.warning("请选择 Nginx 版本"); return false; }
    try {
        await GoTools.NginxSrvSaveInstance({ ...instForm });
        Message.success("已保存，首次启动会按表单生成 nginx.conf"); await loadInstances(); return true;
    } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); return false; }
}

/* ============================ 版本管理 ============================ */
async function openVersionsModal() {
    versionsVisible.value = true;
    loadLocalVersions();
    if (!pluginsDirVal.value) {
        try { pluginsDirVal.value = (await GoTools.NginxSrvPluginsDir()) || ""; } catch (e) { /* ignore */ }
    }
    if (remoteVersions.value.length === 0) loadRemoteVersions();
}
async function loadRemoteVersions() {
    remoteLoading.value = true;
    try { remoteVersions.value = (await GoTools.NginxSrvRemoteVersions()) || []; }
    catch (e: any) { Message.error("获取远程版本失败: " + (e?.message || e)); }
    remoteLoading.value = false;
}
async function installVersion(r: any) {
    try {
        pushInstallLog(`提交安装请求：Nginx ${r.version}`);
        await GoTools.NginxSrvInstall(r.version);
        installProgress[r.version] = { stage: "downloading", percent: 0 };
    } catch (e: any) {
        pushInstallLog(`安装请求被拒绝：${e?.message || e}`);
        Message.error("安装失败: " + (e?.message || e));
    }
}
function removeVersion(v: any) {
    Modal.warning({
        title: "删除版本",
        content: `确定删除 Nginx ${v.version}？若仍有运行中的实例引用该版本会被拒绝。`,
        hideCancel: false,
        onOk: async () => {
            try { await GoTools.NginxSrvRemoveVersion(v.version); Message.success("已删除"); loadLocalVersions(); }
            catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
        },
    });
}
async function chooseLocalDir() {
    try { const dir = await GoTools.NginxSrvChooseDir(); if (dir) localDirInput.value = dir; }
    catch (e) { /* cancel */ }
}
async function addLocalDir() {
    const dir = localDirInput.value.trim();
    if (!dir) { Message.warning("请先选择或填写目录"); return; }
    try { await GoTools.NginxSrvAddLocal(dir); Message.success("已添加本机目录"); localDirInput.value = ""; loadLocalVersions(); }
    catch (e: any) { Message.error("添加失败: " + (e?.message || e)); }
}

/* ============================ nginx.conf ============================ */
async function openConf(id: string) {
    confInstId.value = id;
    confText.value = "";
    confVisible.value = true;
    try { confText.value = (await GoTools.NginxSrvGetConfigText(id)) || ""; }
    catch (e: any) { Message.error("读取 nginx.conf 失败: " + (e?.message || e)); confVisible.value = false; }
}
async function saveConfText() {
    try {
        await GoTools.NginxSrvSaveConfigText(confInstId.value, confText.value);
        Message.success("nginx.conf 已保存；运行中实例建议执行一次重载");
        confVisible.value = false;
    } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); }
}

/* ============================ 生命周期 ============================ */
onMounted(async () => {
    await loadInstances();
    // 主页点击服务实例进入（?inst=）：选中该实例展示详情
    const wantId = String(route.query.inst ?? "");
    if (wantId && instances.value.some((i) => i.id === wantId)) selectInst(wantId);
    loadLocalVersions();
    srvTimer = setInterval(refreshStates, 3000);
    // 单例窗口已打开时，主页再次点击其他实例 → 通过事件切换
    offSelInst = Events.On("tool:select-inst", (ev: any) => {
        const p = ev?.data ?? ev;
        if (p?.type !== "nginx" || !p?.id) return;
        if (instances.value.some((i) => i.id === String(p.id))) selectInst(String(p.id));
    });
    offInstall = Events.On("nginx:install", (ev: any) => {
        const p = ev?.data ?? ev;
        if (!p || !p.version) return;
        if (p.stage === "info") {
            pushInstallLog(p.note || "");
        } else if (p.stage === "done") {
            delete installProgress[p.version];
            pushInstallLog(`安装完成 Nginx ${p.version}`);
            Message.success(`Nginx ${p.version} 安装完成`);
            loadLocalVersions();
        } else if (p.stage === "error") {
            installProgress[p.version] = p;
            pushInstallLog(`安装失败 ${p.version}：${p.note || "未知错误"}（可将 nginx zip 放入插件目录后离线安装）`);
            Modal.error({
                title: "安装失败",
                content: `${p.version}：${p.note || "未知错误"}。可将 nginx zip 放入插件目录后重试（离线安装）。`,
            });
        } else {
            installProgress[p.version] = p;
        }
    });
});
onUnmounted(() => {
    if (offInstall) offInstall();
    if (offSelInst) offSelInst();
    if (srvTimer) clearInterval(srvTimer);
    if (logTimer) clearInterval(logTimer);
});
</script>

<style scoped>
.nginx-page {
    height: 100%;
    min-height: 0;
    flex-wrap: nowrap;
    background: var(--color-fill-1);
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

.conn-name {
    max-width: 140px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.conn-more {
    cursor: pointer;
    color: var(--color-text-3);
}

.conn-empty {
    color: var(--color-text-3);
    font-size: 12px;
    padding: 6px 0;
}

.inst-active {
    background: var(--color-primary-light-1);
    cursor: pointer;
}

:deep(.arco-list-item) {
    cursor: pointer;
}

.detail-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(240px, 1fr));
    gap: 6px 16px;
    padding: 10px 14px;
    border-bottom: 1px solid var(--color-border-2);
}

.ls-item {
    display: flex;
    justify-content: space-between;
    font-size: 12px;
    gap: 8px;
}

.ls-item span {
    color: var(--color-text-3);
    flex-shrink: 0;
}

.ls-item b {
    font-weight: 500;
    text-align: right;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.ls-item b.ok {
    color: rgb(var(--green-6));
}

.ls-item b.no {
    color: var(--color-text-3);
}

.log-head {
    display: flex;
    justify-content: space-between;
    align-items: center;
    padding: 8px 14px 4px;
    font-size: 12px;
    color: var(--color-text-3);
}

.ls-log {
    flex: 1;
    min-height: 160px;
    overflow: auto;
    background: var(--color-fill-2);
    margin: 0 14px 12px;
    padding: 8px;
    border-radius: 4px;
    font-family: Consolas, Menlo, monospace;
    font-size: 12px;
}

.ls-log.small {
    max-height: 140px;
    min-height: 60px;
    margin: 0;
}

.ls-line {
    white-space: pre-wrap;
    word-break: break-all;
    line-height: 1.6;
}

.ver-block .ver-title {
    display: flex;
    align-items: center;
    gap: 8px;
    font-weight: 600;
    font-size: 13px;
    margin-bottom: 8px;
}

.add-local {
    margin-top: 10px;
}

.offline-tip {
    margin-top: 8px;
    font-size: 12px;
    color: var(--color-text-3);
    word-break: break-all;
}

.install-log {
    margin-top: 12px;
}

.ini-tip {
    float: left;
    font-size: 12px;
    color: var(--color-text-3);
    line-height: 32px;
}

.check-out {
    background: var(--color-fill-2);
    padding: 10px;
    border-radius: 4px;
    font-size: 12px;
    white-space: pre-wrap;
    word-break: break-all;
    margin: 0;
}
</style>
