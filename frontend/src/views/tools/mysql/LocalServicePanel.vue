<template>
  <div class="local-service">
    <a-card class="side-card" size="small" :body-style="{ padding: 0 }">
      <template #title>本地服务</template>
      <template #extra>
        <div class="srv-head-actions">
          <a-button size="small" title="新建实例" @click="openInstModal()">新建实例</a-button>
          <a-button size="small" title="版本管理" @click="openVersionsModal()">版本管理</a-button>
        </div>
      </template>
      <a-list :bordered="false" size="small">
        <a-list-item v-for="inst in srvInstances" :key="inst.id">
          <a-space>
            <a-badge :status="srvStates[inst.id]?.running ? 'success' : 'normal'" />
            <span class="conn-name" :title="inst.name + ':' + inst.port">{{ inst.name }}</span>
            <a-tag color="arcoblue">v{{ inst.version }}</a-tag>
          </a-space>
          <template #actions>
            <a-space :size="2">
              <a-dropdown trigger="hover" @select="(v: any) => onSrvAction(v, inst)">
                <icon-more class="conn-more" @click.stop />
                <template #content>
                  <a-doption v-if="!srvStates[inst.id]?.running" value="start"><icon-play-arrow /> 启动</a-doption>
                  <a-doption v-else value="stop"><icon-pause /> 停止</a-doption>
                  <a-doption value="logs"><icon-list /> 日志/状态</a-doption>
                  <a-doption value="connect"><icon-link /> 连接</a-doption>
                  <a-doption value="edit"><icon-edit /> 编辑</a-doption>
                </template>
              </a-dropdown>
              <a-popconfirm :content="`确定删除实例「${inst.name}」？运行中会先停止；数据目录保留。`" type="warning" position="lt"
                @ok="removeInstance(inst)">
                <a-button size="mini" type="text" status="danger"><icon-delete /></a-button>
              </a-popconfirm>
            </a-space>
          </template>
        </a-list-item>
        <a-list-item v-if="srvInstances.length === 0">
          <a-empty class="srv-empty" description="暂无实例，点 + 新建" />
        </a-list-item>
      </a-list>
    </a-card>

    <!-- 版本管理（对齐 Redis：a-space vertical fill + 三张 a-card，标题带图标、操作移 #extra、空态用 a-empty） -->
    <a-modal v-model:visible="versionsVisible" title="MySQL 版本管理" :width="680" :footer="false">
      <a-space direction="vertical" fill :size="12">
        <!-- 已安装版本 -->
        <a-card size="small" :body-style="{ padding: '8px 12px' }">
          <template #title><span class="ver-sec-title"><icon-storage /> 已安装版本</span></template>
          <div class="ver-scroll">
            <div v-for="v in localVersions" :key="v.version + v.source" class="ver-item">
              <a-tag size="small"
                :color="v.source === 'bundled' ? 'green' : (v.source === 'downloaded' ? 'arcoblue' : 'orange')">{{
                  srcLabel(v.source) }}</a-tag>
              <span class="ver-name">MySQL {{ v.version }}</span>
              <span class="ver-path" :title="v.path">{{ v.path }}</span>
              <a-popconfirm :content="`确定删除 MySQL ${v.version}？若仍有实例引用该版本会被拒绝。`" type="warning" position="lt"
                @ok="removeVersion(v)">
                <a-button size="mini" type="text" status="danger" :disabled="v.source === 'bundled'">删除</a-button>
              </a-popconfirm>
            </div>
            <a-empty v-if="localVersions.length === 0" :image="false" description="尚未安装任何版本，可从下方在线安装或添加本机目录" />
          </div>
          <div class="ver-actions">
            <a-input v-model="localDirInput" size="small" placeholder="本机 mysqld.exe 所在目录" allow-clear />
            <a-button size="small" @click="chooseLocalDir"><template #icon><icon-folder /></template>选目录</a-button>
            <a-button size="small" type="primary" @click="addLocalDir">添加</a-button>
          </div>
          <div class="ver-hint">离线安装：将 mysql-[ver]-winx64.zip 放入 <span class="mono">{{ srvPluginsDir }}</span> 后点「安装」即可
          </div>
        </a-card>

        <!-- 在线版本 -->
        <a-card size="small" :body-style="{ padding: '8px 12px' }">
          <template #title>
            <span class="ver-sec-title"><icon-cloud /> 在线版本（cdn.mysql.com 官方 zip，包体约 230MB / 解压约 1.5GB）</span>
          </template>
          <template #extra>
            <a-button size="mini" type="text" :loading="remoteLoading" @click="loadRemoteVersions">
              <template #icon><icon-refresh /></template>刷新
            </a-button>
          </template>
          <div class="ver-scroll">
            <div v-for="r in remoteVersions" :key="r.version" class="ver-item">
              <span class="ver-name">MySQL {{ r.version }}</span>
              <span class="ver-path">{{ r.size ? (r.size / 1024 / 1024).toFixed(0) + ' MB' : '' }}</span>
              <a-button size="mini" type="primary"
                :disabled="!!installProgress[r.version] || installedSet.has(r.version)" @click="installVersion(r)">{{
                  installedSet.has(r.version) ? '已安装' : '安装' }}</a-button>
            </div>
            <a-empty v-if="remoteVersions.length === 0" :image="false"
              :description="remoteLoading ? '加载中…' : '点「刷新」获取版本列表'" />
          </div>
          <div v-for="(p, ver) in installProgress" :key="'p' + ver" class="inst-prog">
            <div class="inst-prog-row">
              <span class="ver-name">{{ ver }}</span>
              <a-progress :percent="(p.percent || 0) / 100" size="small"
                :status="p.stage === 'error' ? 'danger' : 'normal'" style="flex: 1" />
              <span class="inst-stage">{{ stageText(p) }}</span>
              <a-button v-if="p.stage === 'error'" size="mini" status="danger" @click="retryInstall(ver as string)">
                <template #icon><icon-refresh /></template>重试
              </a-button>
            </div>
            <div v-if="p.stage === 'error' && p.note" class="inst-err">{{ p.note }}</div>
          </div>
        </a-card>

        <!-- 安装日志 -->
        <a-card size="small" :body-style="{ padding: '8px 12px' }">
          <template #title><span class="ver-sec-title"><icon-history /> 安装日志</span></template>
          <template #extra>
            <a-button size="mini" type="text" @click="installLogs = []"><template
                #icon><icon-delete /></template>清空</a-button>
          </template>
          <div class="ver-scroll inst-log">
            <div v-for="(l, i) in installLogs" :key="i" class="inst-log-line">{{ l }}</div>
            <a-empty v-if="installLogs.length === 0" :image="false" description="暂无安装记录" />
          </div>
        </a-card>
      </a-space>
    </a-modal>

    <!-- 实例编辑 -->
    <a-modal v-model:visible="instVisible" :title="instForm.id ? '编辑实例' : '新建实例'" :width="460" @ok="saveInstance">
      <a-form :model="instForm" layout="vertical">
        <a-form-item field="name" label="实例名称">
          <a-input v-model="instForm.name" placeholder="例如 local-dev" />
        </a-form-item>
        <a-form-item field="version" label="MySQL 版本">
          <a-select v-model="instForm.version" placeholder="需已安装，可在「版本管理」获取">
            <a-option v-for="v in localVersions" :key="v.version" :value="v.version">
              {{ v.version }}（{{ srcLabel(v.source) }}）
            </a-option>
          </a-select>
        </a-form-item>
        <a-row :gutter="12">
          <a-col :span="12">
            <a-form-item field="port" label="端口">
              <a-input-number v-model="instForm.port" :min="1" :max="65535" @blur="checkPortTaken" />
            </a-form-item>
          </a-col>
          <a-col :span="12">
            <a-form-item field="maxConnections" label="最大连接数(0=默认151)">
              <a-input-number v-model="instForm.maxConnections" :min="0" />
            </a-form-item>
          </a-col>
        </a-row>
        <a-form-item field="rootPassword" label="root 密码（留空=空密码）">
          <a-input-password v-model="instForm.rootPassword" placeholder="留空则不设置" />
        </a-form-item>
      </a-form>
    </a-modal>

    <!-- 日志/状态 -->
    <a-modal v-model:visible="logsVisible" :title="logsInst ? `实例 ${logsInst.name}（:${logsInst.port}）` : '实例日志'"
      :width="660" :footer="false">
      <div v-if="logsStatus" class="ls-grid">
        <div class="ls-item"><span>状态</span><b :class="{ ok: logsStatus.running }">{{ logsStatus.running ? '运行中' : '已停止'
        }}</b></div>
        <div class="ls-item"><span>版本</span><b>{{ logsStatus.version || '-' }}</b></div>
        <div class="ls-item"><span>PID</span><b>{{ logsStatus.pid || '-' }}</b></div>
        <div class="ls-item"><span>运行时长</span><b>{{ fmtUptimeS(logsStatus.uptimeSeconds) }}</b></div>
        <div class="ls-item"><span>线程连接</span><b>{{ logsStatus.threadsConnected ?? '-' }}</b></div>
        <div class="ls-item"><span>线程活跃</span><b>{{ logsStatus.threadsRunning ?? '-' }}</b></div>
        <div class="ls-item"><span>最大连接</span><b>{{ logsStatus.maxConnections ?? '-' }}</b></div>
      </div>
      <div ref="logBoxRef" class="ls-log">
        <div v-for="(l, i) in logsLines" :key="i" class="ls-line">{{ l }}</div>
        <div v-if="logsLines.length === 0" class="empty-tip">暂无日志</div>
      </div>
      <div class="ls-actions">
        <a-button size="mini" @click="logsLines = []"><template #icon><icon-delete /></template>清空</a-button>
        <a-button size="mini" type="outline" :disabled="!logsStatus?.running" @click="connectInstance(logsInst)">
          <template #icon><icon-link /></template>连接此实例
        </a-button>
      </div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onMounted, onUnmounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { useRoute } from "vue-router";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

interface SrvInstanceConfig {
  id: string; name: string; version: string; port: number;
  rootPassword: string; maxConnections: number
}

interface SrvConnConfig {
  id: string; name: string; host: string; port: number;
  user: string; password: string; database: string
}

const emit = defineEmits<{ (e: "connect", cfg: SrvConnConfig): void }>();

const srvInstances = ref<SrvInstanceConfig[]>([]);
const srvStates = reactive<Record<string, any>>({});
const localVersions = ref<any[]>([]);
const remoteVersions = ref<any[]>([]);
const remoteLoading = ref(false);
const installProgress = reactive<Record<string, any>>({});
const installLogs = ref<string[]>([]);
const versionsVisible = ref(false);
const instVisible = ref(false);
const logsVisible = ref(false);
const localDirInput = ref("");
const srvPluginsDir = ref("");
const logsInst = ref<SrvInstanceConfig | null>(null);
const logsStatus = ref<any>(null);
const logsLines = ref<string[]>([]);
const logBoxRef = ref<HTMLElement | null>(null);

const instForm = reactive<SrvInstanceConfig>({
  id: "", name: "", version: "", port: 3306, rootPassword: "", maxConnections: 0,
});

const installedSet = computed(() => new Set<string>(localVersions.value.map((v: any) => v.version)));

let offInstall: any = null;
const route = useRoute();
let offSelInst: any = null;
let srvTimer: any = null;

function pushInstallLog(txt: string) {
  if (!txt) return;
  const t = new Date();
  const pad = (n: number) => n.toString().padStart(2, "0");
  installLogs.value.push(`[${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}] ${txt}`);
  if (installLogs.value.length > 200) installLogs.value.splice(0, installLogs.value.length - 200);
}

function srcLabel(s: string): string {
  return s === "bundled" ? "内置" : s === "downloaded" ? "下载" : "本机";
}

function stageText(p: any): string {
  if (!p) return "";
  if (p.stage === "downloading") return `下载中 ${p.percent || 0}%`;
  if (p.stage === "extracting") return `解压中 ${p.percent || 0}%`;
  if (p.stage === "done") return "完成";
  return "失败";
}

function fmtUptimeS(sec: number): string {
  if (!sec || sec <= 0) return "-";
  const d = Math.floor(sec / 86400); const h2 = Math.floor((sec % 86400) / 3600);
  const m = Math.floor((sec % 3600) / 60); const s = sec % 60;
  if (d > 0) return `${d}d ${h2}h ${m}m`; if (h2 > 0) return `${h2}h ${m}m`; return `${m}m ${s}s`;
}

async function loadLocalVersions() {
  try { localVersions.value = (await GoTools.MySQLSrvListVersions()) || []; }
  catch (e) { localVersions.value = []; }
}

async function loadSrvInstances() {
  try { srvInstances.value = (await GoTools.MySQLSrvListInstances()) || []; }
  catch (e) { srvInstances.value = []; }
  refreshSrvStates();
}

async function refreshSrvStates() {
  for (const inst of srvInstances.value) {
    try {
      const status = await GoTools.MySQLSrvStatus(inst.id);
      srvStates[inst.id] = status;
      if (logsVisible.value && logsInst.value?.id === inst.id) logsStatus.value = status;
    } catch (e) { /* ignore */ }
  }
}

async function openVersionsModal() {
  versionsVisible.value = true;
  loadLocalVersions();
  if (!srvPluginsDir.value) {
    try { srvPluginsDir.value = (await GoTools.MySQLSrvPluginsDir()) || ""; } catch (e) { /* ignore */ }
  }
}

async function loadRemoteVersions() {
  remoteLoading.value = true;
  try { remoteVersions.value = (await GoTools.MySQLSrvRemoteVersions()) || []; }
  catch (e: any) { Message.error("获取远程版本失败: " + (e?.message || e)); }
  remoteLoading.value = false;
}

async function installVersion(r: any) {
  try {
    pushInstallLog(`提交安装请求：MySQL ${r.version}`);
    await GoTools.MySQLSrvInstall(r.version);
    installProgress[r.version] = { stage: "downloading", percent: 0 };
  } catch (e: any) {
    pushInstallLog(`安装请求被拒绝：${e?.message || e}`);
    Message.error("安装失败: " + (e?.message || e));
  }
}

function retryInstall(ver: string) {
  delete installProgress[ver];
  installVersion({ version: ver });
}

async function removeVersion(v: any) {
  try { await GoTools.MySQLSrvRemoveVersion(v.version); Message.success("已删除"); loadLocalVersions(); }
  catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
}

async function chooseLocalDir() {
  try { const dir = await GoTools.MySQLSrvChooseDir(); if (dir) localDirInput.value = dir; }
  catch (e) { /* cancel */ }
}

async function addLocalDir() {
  const dir = localDirInput.value.trim();
  if (!dir) { Message.warning("请先选择或填写目录"); return; }
  try { await GoTools.MySQLSrvAddLocal(dir); Message.success("已添加本机目录"); localDirInput.value = ""; loadLocalVersions(); }
  catch (e: any) { Message.error("添加失败: " + (e?.message || e)); }
}

function openInstModal(inst?: SrvInstanceConfig) {
  if (inst) { Object.assign(instForm, { ...inst }); }
  else { Object.assign(instForm, { id: "", name: "", version: localVersions.value[0]?.version || "", port: 3306, rootPassword: "", maxConnections: 0 }); }
  if (localVersions.value.length === 0) loadLocalVersions();
  instVisible.value = true;
}

async function checkPortTaken() {
  if (!instForm.port) return;
  try {
    const suggest = await GoTools.MySQLSrvSuggestPort(instForm.port);
    if (suggest && suggest !== instForm.port) Message.warning(`端口 ${instForm.port} 已被占用，建议改为 ${suggest}`);
  } catch (e) { /* ignore */ }
}

async function saveInstance() {
  if (!instForm.name) { Message.warning("请填写实例名称"); return; }
  if (!instForm.version) { Message.warning("请选择 MySQL 版本"); return; }
  try {
    await GoTools.MySQLSrvSaveInstance({ ...instForm });
    instVisible.value = false; Message.success("已保存"); await loadSrvInstances();
  } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); }
}

async function onSrvAction(v: string, inst: SrvInstanceConfig) {
  if (v === "start") {
    try { await GoTools.MySQLSrvStart(inst.id); Message.success(`实例 ${inst.name} 已启动`); }
    catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
    refreshSrvStates();
  } else if (v === "stop") {
    try { await GoTools.MySQLSrvStop(inst.id); Message.success(`实例 ${inst.name} 已停止`); }
    catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
    refreshSrvStates();
  } else if (v === "logs") {
    openLogs(inst);
  } else if (v === "connect") {
    connectInstance(inst);
  } else if (v === "edit") {
    openInstModal(inst);
  }
}

/** 删除实例（确认由 Popconfirm 承担） */
async function removeInstance(inst: SrvInstanceConfig) {
  try {
    await GoTools.MySQLSrvRemoveInstance(inst.id);
    delete srvStates[inst.id];
    await loadSrvInstances();
    Message.success("实例已删除");
  } catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
}

/** 实例连接交给父页面打开（需要合并已保存连接） */
function connectInstance(inst: SrvInstanceConfig | null) {
  if (!inst) return;
  if (!srvStates[inst.id]?.running) { Message.warning("请先启动实例"); return; }
  emit("connect", {
    id: "srv-" + inst.id, name: `${inst.name}(本地)`,
    host: "127.0.0.1", port: inst.port, user: "root", password: inst.rootPassword, database: "",
  });
}

let logsTimer: any = null;
async function openLogs(inst: SrvInstanceConfig) {
  logsInst.value = inst;
  logsLines.value = [];
  logsStatus.value = srvStates[inst.id] || null;
  logsVisible.value = true;
  try { logsLines.value = (await GoTools.MySQLSrvLogs(inst.id, 300)) || []; } catch (e) { /* ignore */ }
  if (logsTimer) clearInterval(logsTimer);
  logsTimer = setInterval(async () => {
    if (!logsVisible.value) { clearInterval(logsTimer); logsTimer = null; return; }
    try {
      logsLines.value = (await GoTools.MySQLSrvLogs(inst.id, 300)) || [];
      nextTick(() => { if (logBoxRef.value) logBoxRef.value.scrollTop = logBoxRef.value.scrollHeight; });
    } catch (e) { /* ignore */ }
  }, 2000);
}

onMounted(async () => {
  await loadSrvInstances();
  // 主页点击服务实例进入（?inst=）：直接打开该实例的日志/状态详情
  const wantId = String(route.query.inst ?? "");
  if (wantId) {
    const target = srvInstances.value.find((i) => i.id === wantId);
    if (target) openLogs(target);
  }
  loadLocalVersions();
  srvTimer = setInterval(refreshSrvStates, 3000);
  // 单例窗口已打开时，主页再次点击其他实例 → 通过事件切换
  offSelInst = Events.On("tool:select-inst", (ev: any) => {
    const p = ev?.data ?? ev;
    if (p?.type !== "mysql" || !p?.id) return;
    const target = srvInstances.value.find((i) => i.id === String(p.id));
    if (target) openLogs(target);
  });
  offInstall = Events.On("mysql:install", (ev: any) => {
    const p = ev?.data ?? ev;
    if (!p || !p.version) return;
    if (p.stage === "info") {
      pushInstallLog(p.note || "");
    } else if (p.stage === "done") {
      delete installProgress[p.version];
      pushInstallLog(`安装完成 MySQL ${p.version}` + (p.note ? "：" + p.note : ""));
      Message.success(`MySQL ${p.version} 安装完成`);
      loadLocalVersions();
    } else if (p.stage === "error") {
      installProgress[p.version] = p;
      pushInstallLog(`安装失败 ${p.version}：${p.note || "未知错误"}（可将 mysql zip 放入插件目录后离线安装）`);
      Modal.error({
        title: "安装失败",
        content: `${p.version}：${p.note || "未知错误"}。可将 mysql zip 放入插件目录后重试（离线安装）。`,
      });
    } else {
      installProgress[p.version] = p;
    }
  });
});

onUnmounted(() => {
  if (srvTimer) clearInterval(srvTimer);
  if (logsTimer) clearInterval(logsTimer);
  if (offInstall) offInstall();
  if (offSelInst) offSelInst();
});
</script>

<style scoped>
.local-service {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

/* 卡片撑满左栏，body 弹性填充供列表滚动 */
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

.side-card :deep(.arco-list) {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
}

.srv-head-actions {
  display: flex;
  align-items: center;
  gap: 2px;
}

.conn-name {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.conn-more {
  color: var(--color-text-3);
}

/* 空状态：覆盖 a-list 内 .arco-empty 的横排居中，改为图标在上、文字在下 */
.srv-empty,
.side-card :deep(.arco-list-content .arco-empty) {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 8px;
  height: auto;
  padding: 20px 8px;
}

.srv-empty :deep(.arco-empty-image),
.side-card :deep(.arco-list-content .arco-empty .arco-empty-image) {
  display: block;
  margin-bottom: 0;
  font-size: 40px;
  line-height: 1;
}

.srv-empty :deep(.arco-empty-description),
.side-card :deep(.arco-list-content .arco-empty .arco-empty-description) {
  display: block;
  font-size: var(--base-size-small);
  color: var(--color-text-3);
  line-height: 1.6;
  text-align: center;
  white-space: normal;
}

.empty-tip {
  color: var(--color-text-3);
  text-align: center;
  font-size: var(--base-size-small);
  padding: var(--base-padding);
}

/* ---- 版本管理弹窗 ---- */
.ver-sec-title {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-weight: 600;
  font-size: var(--base-size-default);
}

.ver-scroll {
  max-height: 170px;
  overflow-y: auto;
}

.ver-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: var(--base-radius-default) var(--base-padding);
  border-radius: var(--base-radius-default);
  font-size: var(--base-size-small);
}

.ver-item:hover {
  background: var(--color-fill-2);
}

.ver-name {
  font-weight: 500;
  min-width: 100px;
}

.ver-path {
  flex: 1;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
}

.ver-actions {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-top: var(--base-padding);
}

.ver-actions :deep(.arco-input-wrapper) {
  flex: 1;
}

.ver-hint {
  font-size: var(--base-size-small);
  color: var(--color-text-3);
  margin-top: var(--base-radius-default);
}

.mono {
  font-family: Consolas, monospace;
  background: var(--color-fill-2);
  padding: var(--base-radius-small) var(--base-radius-default);
  border-radius: var(--base-radius-small);
}

/* 安装进度 */
.inst-prog {
  margin-top: var(--base-padding);
}

.inst-prog-row {
  display: flex;
  align-items: center;
  gap: 8px;
}

.inst-stage {
  font-size: var(--base-size-small);
  color: var(--color-text-2);
  min-width: 80px;
}

.inst-err {
  font-size: var(--base-size-small);
  color: rgb(var(--red-6));
  padding: var(--base-radius-default) 0 0 0;
  word-break: break-all;
  white-space: pre-wrap;
}

/* 安装日志：随主题自适应的控制台风格面板 */
.inst-log {
  max-height: 130px;
  background: var(--color-fill-1);
  border: 1px solid var(--color-border-2);
  border-radius: var(--base-radius-default);
  padding: var(--base-radius-default) var(--base-radius-large);
}

.inst-log-line {
  font-family: Consolas, monospace;
  font-size: var(--base-size-small);
  color: var(--color-text-2);
  line-height: 1.6;
  white-space: pre-wrap;
  word-break: break-all;
}

/* 日志/状态弹窗 */
.ls-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
  gap: 8px;
  margin-bottom: var(--base-margin);
}

.ls-item {
  background: var(--color-fill-1);
  border-radius: var(--base-radius-default);
  padding: var(--base-radius-default) var(--base-radius-large);
  font-size: var(--base-size-small);
}

.ls-item span {
  color: var(--color-text-3);
  display: block;
}

.ls-item b {
  font-size: var(--base-size-small);
}

.ls-item b.ok {
  color: rgb(var(--green-6));
}

.ls-log {
  background: var(--color-fill-1);
  border: 1px solid var(--color-border-2);
  border-radius: var(--base-radius-default);
  padding: var(--base-padding) var(--base-radius-large);
  max-height: 320px;
  overflow-y: auto;
  font-family: Consolas, monospace;
  font-size: var(--base-size-small);
  color: var(--color-text-1);
  line-height: 1.5;
}

.ls-line {
  white-space: pre-wrap;
  word-break: break-all;
}

.ls-actions {
  display: flex;
  gap: 8px;
  margin-top: var(--base-radius-large);
}
</style>
