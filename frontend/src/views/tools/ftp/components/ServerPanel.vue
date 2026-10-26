<template>
  <!-- 服务端：状态与监听配置 / 虚拟账号 / 在线连接 / 服务日志 -->
  <div class="srv-wrap">
    <a-row :gutter="6">
      <a-col :span="24">
        <a-card class="card-head-tool" size="small">
          <template #title>
            <a-space :size="6">
              <com-badge :status="running ? 'success' : 'normal'" :animated="running" />
              <span>SFTP 服务端</span>
              <a-tag size="small" :color="running ? 'green' : 'gray'">{{ running ? '运行中' : '已停止' }}</a-tag>
              <a-tag v-if="running" size="small" color="arcoblue">{{ status.addr }}</a-tag>
            </a-space>
          </template>
          <template #extra>
            <a-space :size="6">
              <a-button v-if="running" size="small" :loading="busy" @click="saveConfig">
                <template #icon><icon-save /></template>保存配置
              </a-button>
              <a-button size="small" type="primary" :loading="busy" @click="startOrStop">
                <template #icon><icon-poweroff /></template>{{ running ? '停止服务' : '启动服务' }}
              </a-button>
            </a-space>
          </template>

          <a-alert v-if="status.error" type="error" show-icon class="srv-alert" :content="status.error" />

          <a-form :model="form" layout="inline" class="srv-form">
            <a-form-item label="监听地址">
              <a-input v-model="form.host" size="small" style="width: 118px" placeholder="0.0.0.0" />
            </a-form-item>
            <a-form-item label="端口">
              <a-input-number v-model="form.port" size="small" :min="1" :max="65535" style="width: 100px" />
            </a-form-item>
            <a-form-item label="根目录">
              <a-input v-model="form.rootDir" size="small" class="root-input"
                :placeholder="status.rootDir || '默认 <data>/ftp/server/root'" />
              <a-button size="small" class="ml6" title="选择目录" @click="pickRoot">
                <template #icon><icon-folder /></template>
              </a-button>
              <a-button size="small" class="ml6" title="在文件管理器中打开" @click="openRoot">
                <template #icon><icon-launch /></template>
              </a-button>
            </a-form-item>
            <a-form-item>
              <a-checkbox v-model="form.autoStart">随应用启动</a-checkbox>
            </a-form-item>
          </a-form>

          <div class="srv-hint">
            <span class="dim">局域网地址：</span>
            <template v-if="lanAddrs.length">
              <a-tooltip v-for="ip in lanAddrs" :key="ip" content="点击复制连接地址">
                <a-tag class="addr-tag" @click="copyAddr(ip)">
                  sftp://{{ ip }}:{{ form.port }}<icon-copy class="copy-icon" />
                </a-tag>
              </a-tooltip>
            </template>
            <span v-else class="dim">未检测到局域网 IPv4 地址</span>
            <a-divider direction="vertical" />
            <a-button size="mini" :loading="busy" @click="allowFirewall">
              <template #icon><icon-fire /></template>防火墙放行 {{ form.port }}
            </a-button>
            <a-link :hoverable="false" class="dim" @click="removeFirewall">取消放行</a-link>
            <span class="dim">（Windows 会弹管理员授权）</span>
          </div>
        </a-card>
      </a-col>

      <a-col :span="14">
        <a-card class="card-head-tool" size="small" :body-style="{ padding: 0 }">
          <template #title>
            <a-space :size="6">
              <span>虚拟账号</span>
              <a-tag size="small" color="gray">{{ accounts.length }}</a-tag>
            </a-space>
          </template>
          <template #extra>
            <a-button size="small" @click="openAccount(null)">
              <template #icon><icon-plus /></template>添加账号
            </a-button>
          </template>

          <a-table :data="accounts" :pagination="false" :bordered="false" size="small" row-key="id"
            :scroll="{ y: 252 }">
            <template #columns>
              <a-table-column title="用户名" data-index="username" :width="120">
                <template #cell="{ record }">
                  <a-tooltip :content="record.username">
                    <span class="cell-ellipsis">{{ record.username }}</span>
                  </a-tooltip>
                </template>
              </a-table-column>
              <a-table-column title="目录" :width="96">
                <template #cell="{ record }">
                  <a-tooltip :content="record.rootDir === '.' ? '服务端根目录' : record.rootDir">
                    <span class="cell-ellipsis">{{ record.rootDir === '.' ? '/' : `/${record.rootDir}` }}</span>
                  </a-tooltip>
                </template>
              </a-table-column>
              <a-table-column title="权限" :width="72">
                <template #cell="{ record }">
                  <a-tag size="small" :color="record.readOnly ? 'orange' : 'green'">
                    {{ record.readOnly ? '只读' : '读写' }}
                  </a-tag>
                </template>
              </a-table-column>
              <a-table-column title="状态" :width="72">
                <template #cell="{ record }">
                  <a-tag size="small" :color="record.enabled ? 'arcoblue' : 'gray'">
                    {{ record.enabled ? '启用' : '禁用' }}
                  </a-tag>
                </template>
              </a-table-column>
              <a-table-column title="备注" data-index="note" ellipsis />
              <a-table-column title="操作" :width="96">
                <template #cell="{ record }">
                  <a-space :size="4">
                    <a-link @click="openAccount(record)">编辑</a-link>
                    <a-link status="danger" @click="removeAccount(record)">删除</a-link>
                  </a-space>
                </template>
              </a-table-column>
            </template>
          </a-table>
          <div v-if="!accounts.length" class="empty-line">还没有账号，点右上「添加账号」创建</div>
        </a-card>
      </a-col>

      <a-col :span="10">
        <a-card class="card-head-tool" size="small" :body-style="{ padding: 0 }">
          <template #title>
            <a-space :size="6">
              <span>在线连接</span>
              <a-tag size="small" :color="conns.length ? 'arcoblue' : 'gray'">{{ conns.length }}</a-tag>
            </a-space>
          </template>
          <template #extra>
            <a-button size="small" @click="reload">
              <template #icon><icon-refresh /></template>刷新
            </a-button>
          </template>

          <a-table v-if="conns.length" :data="conns" :pagination="false" :bordered="false" size="small"
            row-key="id" :scroll="{ y: 252 }">
            <template #columns>
              <a-table-column title="账号" data-index="username" :width="96" ellipsis />
              <a-table-column title="客户端" data-index="clientIp" :width="112" ellipsis />
              <a-table-column title="已连" :width="88">
                <template #cell="{ record }">{{ fmtDuration(now - record.connectedAt) }}</template>
              </a-table-column>
              <a-table-column title="操作" :width="64">
                <template #cell="{ record }">
                  <a-link status="danger" @click="kick(record)">断开</a-link>
                </template>
              </a-table-column>
            </template>
          </a-table>
          <div v-else class="empty-line">{{ running ? '暂无在线连接' : '服务未启动' }}</div>
        </a-card>
      </a-col>

      <a-col :span="24">
        <a-card class="card-head-tool" size="small">
          <template #title>服务日志</template>
          <template #extra>
            <a-space :size="6">
              <span class="dim">{{ logs.length }} 条</span>
              <a-button size="small" @click="reloadLogs">
                <template #icon><icon-refresh /></template>刷新
              </a-button>
              <a-button size="small" @click="logs = []">清空显示</a-button>
            </a-space>
          </template>
          <div ref="logBox" class="log-box">
            <div v-for="l in logs" :key="l.seq" class="log-line" :class="l.level">
              <span class="log-time">{{ l.time }}</span>
              <span class="log-text">{{ l.text }}</span>
            </div>
            <div v-if="!logs.length" class="empty-line">暂无日志</div>
          </div>
        </a-card>
      </a-col>
    </a-row>

    <account-modal v-model:visible="accountVisible" :account="editingAccount" @saved="onAccountsSaved" />
  </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { Events } from '@wailsio/runtime';
import { Message, Modal } from '@arco-design/web-vue';
import AccountModal from './AccountModal.vue';
import type { FtpAccount, FtpServerConfig, FtpServerLog, FtpServerStatus } from '../types';

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const status = ref<FtpServerStatus>({
  state: 'stopped', host: '0.0.0.0', port: 2222, addr: '', rootDir: '', autoStart: false,
  startedAt: 0, error: '', accounts: 0, conns: [], localIps: [],
});
const form = reactive<FtpServerConfig>({ host: '0.0.0.0', port: 2222, rootDir: '', autoStart: false });
const accounts = ref<FtpAccount[]>([]);
const logs = ref<FtpServerLog[]>([]);
const busy = ref(false);

const running = computed(() => status.value.state === 'running');
const conns = computed(() => status.value.conns || []);
const lanAddrs = computed(() => status.value.localIps || []);

/** 在线时长每秒刷新一次就够，没必要跟着连接数事件走 */
const now = ref(Math.floor(Date.now() / 1000));
let tickTimer: number | undefined;

/* ==================== 读取 ==================== */

function syncForm(s: FtpServerStatus) {
  form.host = s.host || '0.0.0.0';
  form.port = s.port || 2222;
  form.rootDir = s.rootDir || '';
  form.autoStart = !!s.autoStart;
}

async function loadStatus() {
  try {
    status.value = await GoTools.FtpServerStatus();
    syncForm(status.value);
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '读取服务端状态失败'), position: 'top', duration: 3000 });
  }
}

async function loadAccounts() {
  try {
    accounts.value = (await GoTools.FtpListAccounts()) || [];
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '读取账号失败'), position: 'top', duration: 3000 });
  }
}

async function reloadLogs() {
  try {
    logs.value = (await GoTools.FtpServerLogs()) || [];
    scrollLogs();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '读取日志失败'), position: 'top', duration: 3000 });
  }
}

async function reload() {
  await Promise.all([loadStatus(), loadAccounts()]);
}

/* ==================== 启停与配置 ==================== */

async function startOrStop() {
  if (busy.value) return;
  if (running.value) {
    Modal.warning({
      title: '停止服务',
      content: '停止后所有在线连接会被断开，确定继续？',
      hideCancel: false,
      onOk: async () => {
        busy.value = true;
        try {
          status.value = await GoTools.FtpServerStop();
          Message.success({ content: '服务已停止', position: 'top', duration: 2000 });
        } catch (e: any) {
          Message.error({ content: String(e?.message || e || '停止失败'), position: 'top', duration: 3000 });
        } finally {
          busy.value = false;
        }
      },
    });
    return;
  }
  busy.value = true;
  try {
    status.value = await GoTools.FtpServerStart();
    syncForm(status.value);
    Message.success({ content: `服务已启动：${status.value.addr}`, position: 'top', duration: 2500 });
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '启动失败'), position: 'top', duration: 4000 });
    await loadStatus();
  } finally {
    busy.value = false;
  }
}

async function saveConfig() {
  if (!form.port || form.port < 1 || form.port > 65535) {
    Message.error({ content: '端口需在 1-65535 之间', position: 'top', duration: 2500 });
    return;
  }
  busy.value = true;
  try {
    const wasRunning = running.value;
    status.value = await GoTools.FtpServerSaveConfig({ ...form });
    syncForm(status.value);
    if (wasRunning) {
      Message.success({ content: '配置已保存，服务已按新配置重启', position: 'top', duration: 2500 });
    } else {
      Message.success({ content: '配置已保存', position: 'top', duration: 2000 });
    }
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '保存失败'), position: 'top', duration: 4000 });
    await loadStatus();
  } finally {
    busy.value = false;
  }
}

async function pickRoot() {
  try {
    const picked = await GoTools.FtpChooseServerRoot();
    if (picked) form.rootDir = picked;
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '选择目录失败'), position: 'top', duration: 3000 });
  }
}

async function openRoot() {
  try {
    await GoTools.FtpOpenServerRoot(form.rootDir || '');
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '打开目录失败'), position: 'top', duration: 3000 });
  }
}

async function allowFirewall() {
  busy.value = true;
  try {
    await GoTools.FtpFirewallAllow(form.port);
    Message.success({ content: `已放行 TCP ${form.port}`, position: 'top', duration: 2500 });
  } catch (e: any) {
    Message.warning({ content: String(e?.message || e || '放行失败'), position: 'top', duration: 4000 });
  } finally {
    busy.value = false;
  }
}

async function removeFirewall() {
  try {
    await GoTools.FtpFirewallRemove(form.port);
    Message.success({ content: `已删除 TCP ${form.port} 的放行规则`, position: 'top', duration: 2500 });
  } catch (e: any) {
    Message.warning({ content: String(e?.message || e || '删除失败'), position: 'top', duration: 4000 });
  }
}

async function copyAddr(ip: string) {
  const url = `sftp://${ip}:${form.port}`;
  try {
    if (navigator.clipboard?.writeText) {
      await navigator.clipboard.writeText(url);
    } else {
      fallbackCopy(url);
    }
    Message.success({ content: '连接地址已复制', position: 'top', duration: 1600 });
  } catch {
    // 剪贴板被拦时至少把地址展示出来，用户可手抄
    Message.info({ content: url, position: 'top', duration: 4000 });
  }
}

/** WebView 里 navigator.clipboard 不可用时的兜底：隐藏 textarea + execCommand */
function fallbackCopy(text: string) {
  const el = document.createElement('textarea');
  el.value = text;
  el.style.position = 'fixed';
  el.style.opacity = '0';
  document.body.appendChild(el);
  el.select();
  document.execCommand('copy');
  document.body.removeChild(el);
}

/* ==================== 账号 ==================== */

const accountVisible = ref(false);
const editingAccount = ref<FtpAccount | null>(null);

function openAccount(a: FtpAccount | null) {
  editingAccount.value = a;
  accountVisible.value = true;
}

async function onAccountsSaved(list: FtpAccount[]) {
  accounts.value = list;
  await loadStatus();
}

function removeAccount(a: FtpAccount) {
  Modal.warning({
    title: '删除账号',
    content: `确定删除账号「${a.username}」？该账号的在线连接会被立刻断开，目录内容不会删除。`,
    hideCancel: false,
    onOk: async () => {
      try {
        accounts.value = await GoTools.FtpDeleteAccount(a.id);
        await loadStatus();
      } catch (e: any) {
        Message.error({ content: String(e?.message || e || '删除失败'), position: 'top', duration: 3000 });
      }
    },
  });
}

/* ==================== 在线连接 ==================== */

function kick(c: { id: string; username: string; clientIp: string }) {
  Modal.warning({
    title: '断开连接',
    content: `断开 ${c.username}@${c.clientIp} 的连接？`,
    hideCancel: false,
    onOk: async () => {
      try {
        await GoTools.FtpKickServerConn(c.id);
        await loadStatus();
      } catch (e: any) {
        Message.error({ content: String(e?.message || e || '断开失败'), position: 'top', duration: 3000 });
      }
    },
  });
}

/* ==================== 日志 ==================== */

const logBox = ref<HTMLElement | null>(null);

function scrollLogs() {
  void nextTick(() => {
    const el = logBox.value;
    if (el) el.scrollTop = el.scrollHeight;
  });
}

function appendLog(l: FtpServerLog) {
  if (!l?.seq) return;
  if (logs.value.some((x) => x.seq === l.seq)) return;
  logs.value.push(l);
  if (logs.value.length > 300) logs.value.splice(0, logs.value.length - 300);
  scrollLogs();
}

/* ==================== 事件 ==================== */

let offServer: (() => void) | null = null;
let offLog: (() => void) | null = null;

onMounted(async () => {
  await reload();
  await reloadLogs();
  offServer = Events.On('ftp:server', (raw: any) => {
    const st = raw?.data ?? raw;
    if (!st?.state) return;
    status.value = st as FtpServerStatus;
  });
  offLog = Events.On('ftp:serverlog', (raw: any) => {
    const l = raw?.data ?? raw;
    if (l?.seq) appendLog(l as FtpServerLog);
  });
  tickTimer = window.setInterval(() => (now.value = Math.floor(Date.now() / 1000)), 1000);
});

onBeforeUnmount(() => {
  offServer?.();
  offLog?.();
  if (tickTimer) window.clearInterval(tickTimer);
});

/* ==================== 展示辅助 ==================== */

function fmtDuration(sec: number): string {
  if (!Number.isFinite(sec) || sec < 0) return '—';
  if (sec < 60) return `${sec} 秒`;
  if (sec < 3600) return `${Math.floor(sec / 60)} 分钟`;
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  return m ? `${h} 小时 ${m} 分` : `${h} 小时`;
}
</script>

<style scoped>
.srv-wrap {
  height: 100%;
  overflow: auto;
}

.srv-wrap :deep(.arco-row) {
  row-gap: 6px;
}

.srv-alert {
  margin-bottom: 8px;
}

.srv-form {
  row-gap: 6px;
}

.root-input {
  width: 260px;
}

.ml6 {
  margin-left: 6px;
}

.srv-hint {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 6px;
  margin-top: 6px;
  font-size: 12px;
}

.addr-tag {
  cursor: pointer;
  font-family: Consolas, 'Cascadia Mono', monospace;
}

.copy-icon {
  margin-left: 4px;
  font-size: 11px;
}

.cell-ellipsis {
  display: inline-block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  vertical-align: bottom;
}

.empty-line {
  padding: 14px 12px;
  color: var(--color-text-3);
  font-size: 12px;
  text-align: center;
}

.log-box {
  height: 176px;
  overflow: auto;
  padding: 8px 10px;
  background: var(--color-fill-1);
  border-radius: 4px;
  font-family: Consolas, 'Cascadia Mono', 'Microsoft YaHei', monospace;
  font-size: 12px;
  line-height: 1.7;
}

.log-line {
  display: flex;
  gap: 8px;
  white-space: pre-wrap;
  word-break: break-all;
}

.log-time {
  flex: none;
  color: var(--color-text-3);
}

.log-line.warn .log-text {
  color: rgb(var(--warning-6));
}

.log-line.error .log-text {
  color: rgb(var(--danger-6));
}

.dim {
  color: var(--color-text-3);
}
</style>
