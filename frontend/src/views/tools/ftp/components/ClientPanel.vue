<template>
  <!-- 客户端工作区：左列连接记录 + 右侧双栏文件管理，骨架与终端连接页同一套 -->
  <a-row class="ftp-page" :gutter="6">
    <a-col :span="7" class="side-col">
      <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
        <template #title>
          <span class="side-title">远程连接</span>
          <span class="side-count">{{ connections.length }}</span>
        </template>
        <template #extra>
          <div class="srv-head-actions">
            <a-input-search v-model="keyword" size="small" placeholder="搜索名称/主机" allow-clear style="width: 124px" />
            <a-button size="small" @click="openForm(null)">
              <template #icon><icon-plus /></template>添加
            </a-button>
          </div>
        </template>

        <!-- 卡片式选择列表：与 nginx 左侧同一套 common-item-card（选中短竖条 + hover 投影），单击选中、双击连接 -->
        <div class="inst-list">
          <common-item-card v-for="c in filtered" :key="c.id"
            :active="activeConnId === c.id"
            @click="activeConnId = c.id" @dblclick="connect(c)">
            <template #status>
              <com-badge :status="isConnected(c.id) ? 'success' : 'normal'" :animated="isConnected(c.id)" />
            </template>
            <template #title>
              <a-tooltip class="inst-name-tip" :content="`${c.name} ${c.user}@${c.host}:${c.port}`">
                <span class="inst-name">{{ c.name }}</span>
              </a-tooltip>
            </template>
            <template #extra>
              <a-dropdown trigger="hover" @select="(v: any) => onAction(v, c)">
                <icon-more class="inst-more" @click.stop />
                <template #content>
                  <a-doption value="open"><icon-folder /> 打开文件管理</a-doption>
                  <a-doption value="edit"><icon-edit /> 编辑</a-doption>
                  <a-doption value="del"><icon-delete /> 删除</a-doption>
                </template>
              </a-dropdown>
            </template>
            <div class="inst-line2">
              <span class="inst-port">{{ c.user }}@{{ c.host }}:{{ c.port }}</span>
            </div>
          </common-item-card>
          <div v-if="!filtered.length" class="inst-empty">
            {{ loading ? '读取中…' : connections.length ? '没有匹配的记录' : '暂无记录，点右上「添加」新建' }}
          </div>
        </div>
      </a-card>
    </a-col>

    <a-col :span="17" class="main-col">
      <a-card v-if="!session" class="main-panel empty-card card-head-tool">
        <a-empty>
          <a-space direction="vertical" :size="10">
            <span>双击左侧服务器打开文件管理</span>
            <a-button size="small" :loading="connecting" @click="openForm(null)">点击添加 SFTP 连接</a-button>
          </a-space>
        </a-empty>
      </a-card>

      <a-card v-else class="main-panel card-head-tool" :body-style="{ padding: 0 }">
        <template #title>
          <a-space :size="6">
            <com-badge status="success" animated />
            <span>{{ session.title }}</span>
            <a-tag size="small" color="arcoblue">{{ session.user }}@{{ session.host }}:{{ session.port }}</a-tag>
          </a-space>
        </template>
        <template #extra>
          <a-space :size="6">
            <a-button size="small" :loading="connecting" @click="reconnect">
              <template #icon><icon-refresh /></template>重新连接
            </a-button>
            <a-button size="small" @click="closeSession">
              <template #icon><icon-poweroff /></template>断开
            </a-button>
          </a-space>
        </template>

        <div class="work-body">
          <div class="pane-cell">
            <file-pane ref="localPane" side="local" title="本地" :session-id="session.id" :upload-dir="remotePath"
              @path-change="() => {}" @drop-entry="(i: FtpDragItem, s: 'local' | 'remote') => onDropEntry(s, i)" />
          </div>
          <div class="pane-split" />
          <div class="pane-cell">
            <file-pane ref="remotePane" side="remote" title="远程" :session-id="session.id" :start-path="session.home"
              @preview="onPreview" @path-change="(p: string) => (remotePath = p)"
              @drop-entry="(i: FtpDragItem, s: 'local' | 'remote') => onDropEntry(s, i)" />
          </div>
        </div>

        <transfer-queue :transfers="transfers" @cancel="cancelTransfer" @retry="retryTransfer" @clear="clearFinished" />
      </a-card>
    </a-col>
  </a-row>

  <connection-form-modal v-model:visible="formVisible" :connection="editing" @saved="onSaved" />
  <preview-modal v-model:visible="previewVisible" :session-id="session?.id || ''" :entry="previewEntry" />
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onBeforeUnmount, onMounted, ref } from 'vue';
import { Events } from '@wailsio/runtime';
import { Message, Modal } from '@arco-design/web-vue';
import FilePane from './FilePane.vue';
import TransferQueue from './TransferQueue.vue';
import ConnectionFormModal from './ConnectionFormModal.vue';
import PreviewModal from './PreviewModal.vue';
import type { FtpConnection, FtpDragItem, FtpEntry, FtpSessionInfo, FtpTransfer } from '../types';

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

/* ==================== 连接记录 ==================== */

const connections = ref<FtpConnection[]>([]);
const loading = ref(false);
const keyword = ref('');
const activeConnId = ref('');
/** 当前真正连着的那条记录：列表高亮（activeConnId）与连接状态（connectedId）必须分开，
 *  否则点选另一条记录会把圆点点到没连的项上 */
const connectedId = ref('');

const filtered = computed(() => {
  const k = keyword.value.trim().toLowerCase();
  if (!k) return connections.value;
  return connections.value.filter(
    (c) => (c.name || '').toLowerCase().includes(k) || (c.host || '').toLowerCase().includes(k),
  );
});

async function loadConnections() {
  loading.value = true;
  try {
    connections.value = (await GoTools?.FtpListConnections()) || [];
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '读取连接记录失败'), position: 'top', duration: 3000 });
  } finally {
    loading.value = false;
  }
}

function isConnected(id: string): boolean {
  return !!session.value && connectedId.value === id;
}

/* ==================== 表单 ==================== */

const formVisible = ref(false);
const editing = ref<FtpConnection | null>(null);

function openForm(c: FtpConnection | null) {
  editing.value = c;
  formVisible.value = true;
}

function onSaved(list: FtpConnection[], id: string) {
  connections.value = list;
  const c = list.find((x) => x.id === id);
  if (c) void connect(c);
}

function onAction(v: string, c: FtpConnection) {
  if (v === 'open') void connect(c);
  else if (v === 'edit') openForm(c);
  else if (v === 'del') confirmRemove(c);
}

function confirmRemove(c: FtpConnection) {
  Modal.warning({
    title: '删除连接记录',
    content: `确定删除连接记录「${c.name}」？`,
    hideCancel: false,
    onOk: async () => {
      try {
        connections.value = (await GoTools?.FtpDeleteConnection(c.id)) || [];
        if (activeConnId.value === c.id) await closeSession();
      } catch (e: any) {
        Message.error({ content: String(e?.message || e || '删除失败'), position: 'top', duration: 3000 });
      }
    },
  });
}

/* ==================== 会话 ==================== */

const session = ref<FtpSessionInfo | null>(null);
const connecting = ref(false);
const remotePath = ref('');
const localPane = ref<any>(null);
const remotePane = ref<any>(null);

async function connect(c: FtpConnection) {
  if (session.value && connectedId.value === c.id) return;
  if (session.value) await closeSession();
  connecting.value = true;
  activeConnId.value = c.id;
  try {
    const info: FtpSessionInfo = await GoTools.FtpOpenSession(c.id);
    if (!info?.id) throw new Error('建立 SFTP 会话失败');
    session.value = info;
    connectedId.value = c.id;
    remotePath.value = info.home;
    transfers.value = (await GoTools.FtpListTransfers()) || [];
  } catch (e: any) {
    session.value = null;
    connectedId.value = '';
    Message.error({ content: String(e?.message || e || '连接失败'), position: 'top', duration: 4000 });
  } finally {
    connecting.value = false;
  }
}

async function closeSession() {
  const id = session.value?.id;
  session.value = null;
  connectedId.value = '';
  remotePath.value = '';
  previewVisible.value = false;
  if (id) {
    try {
      await GoTools.FtpCloseSession(id);
    } catch {
      /* 会话可能已随远端断开，忽略 */
    }
  }
}

async function reconnect() {
  const c = connections.value.find((x) => x.id === connectedId.value);
  if (!c) return;
  await closeSession();
  await connect(c);
}

/* ==================== 预览 ==================== */

const previewVisible = ref(false);
const previewEntry = ref<FtpEntry | null>(null);

function onPreview(entry: FtpEntry) {
  previewEntry.value = entry;
  previewVisible.value = true;
}

/* ==================== 跨栏拖拽传输 ==================== */

/** 把一栏拖起的文件放到另一栏：
 *  - 从本地拖起、落到远程栏 → 上传到远程当前目录
 *  - 从远程拖起、落到本地栏 → 下载到本地当前目录（不弹选择框）
 * sourceSide 是 FilePane 带回来的「拖起那侧」，不是落点那侧。 */
function onDropEntry(sourceSide: 'local' | 'remote', item: FtpDragItem) {
  if (!session.value) {
    Message.warning({ content: '请先连接服务器再拖拽传输', position: 'top', duration: 3000 });
    return;
  }
  if (sourceSide === 'local') void remotePane.value?.uploadPaths?.([item.path]);
  else void localPane.value?.downloadPaths?.([item.path]);
}

/* ==================== 传输队列 ==================== */

const transfers = ref<FtpTransfer[]>([]);
let refreshTimer: number | undefined;

function upsertTransfer(t: FtpTransfer) {
  if (!t?.id) return;
  const i = transfers.value.findIndex((x) => x.id === t.id);
  if (i >= 0) transfers.value[i] = t;
  else transfers.value.unshift(t);
  if (t.state === 'done') scheduleRefresh();
}

/** 传输完成后合并成一次刷新，避免目录里 100 个文件刷 100 次列表 */
function scheduleRefresh() {
  if (refreshTimer) window.clearTimeout(refreshTimer);
  refreshTimer = window.setTimeout(() => {
    refreshTimer = undefined;
    void localPane.value?.refresh?.();
    void remotePane.value?.refresh?.();
  }, 800);
}

async function cancelTransfer(id: string) {
  try {
    await GoTools.FtpCancelTransfer(id);
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '取消失败'), position: 'top', duration: 3000 });
  }
}

async function retryTransfer(id: string) {
  try {
    const t = await GoTools.FtpRetryTransfer(id);
    if (t?.id) upsertTransfer(t);
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '重试失败'), position: 'top', duration: 3000 });
  }
}

function clearFinished() {
  transfers.value = transfers.value.filter((t) => t.state === 'running' || t.state === 'queued');
}

/* ==================== 事件 ==================== */

let offTransfer: (() => void) | null = null;
let offSession: (() => void) | null = null;
let offDrop: (() => void) | null = null;

onMounted(async () => {
  await loadConnections();
  offTransfer = Events.On('ftp:transfer', (raw: any) => {
    // Wails v3 的事件负载在 data 里，直接拿 raw 会读不到字段
    const t = raw?.data ?? raw;
    if (t?.id) upsertTransfer(t as FtpTransfer);
  });
  offSession = Events.On('ftp:session', (raw: any) => {
    const ev = raw?.data ?? raw;
    if (!ev?.sessionId || ev.sessionId !== session.value?.id) return;
    session.value = null;
    connectedId.value = '';
    remotePath.value = '';
    Message.warning({ content: ev.reason || '连接已断开', position: 'top', duration: 4000 });
  });
  // 原生拖放上传：Go 把落下的真实路径 + 目标栏（pane）转回来，按栏路由到对应 FilePane
  offDrop = Events.On('ftp:files-dropped', (raw: any) => {
    const p = raw?.data ?? raw;
    const files: string[] = p?.files || [];
    if (!files.length) return;
    if (!session.value) {
      Message.warning({ content: '请先连接服务器再拖放上传', position: 'top', duration: 3000 });
      return;
    }
    const target = p?.pane === 'local' ? localPane.value : remotePane.value;
    void target?.handleDrop?.(files);
  });
});

onBeforeUnmount(() => {
  offTransfer?.();
  offSession?.();
  offDrop?.();
  if (refreshTimer) window.clearTimeout(refreshTimer);
  void closeSession();
});
</script>

<style scoped>
.ftp-page {
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

.side-card,
.main-panel {
  height: 100%;
  display: flex;
  flex-direction: column;
}

.side-card :deep(.arco-card-body) {
  flex: 1;
  overflow: auto;
}

.main-panel :deep(.arco-card-body) {
  flex: 1;
  min-height: 0;
  overflow: hidden;
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

/* 左侧连接列表：卡片式条目，选中态用左侧短竖条 + 按需投影（外壳由 common-item-card 提供） */
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

/* 双栏占满卡片正文，传输队列固定在底部 */
.work-body {
  flex: 1;
  min-height: 0;
  display: flex;
}

.pane-cell {
  flex: 1;
  min-width: 0;
  height: 100%;
}

.pane-split {
  width: 1px;
  background: var(--color-border-1);
}
</style>
