<template>
  <!-- 两栏骨架与 PHP 管理页同款：左卡片「我的连接」，右卡片「当前终端」 -->
  <a-row class="term-page" :gutter="6">
    <!-- 左：我的连接（单击选中，双击进终端） -->
    <a-col :span="7" class="side-col">
      <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
        <template #title>我的连接</template>
        <template #extra>
          <div class="srv-head-actions">
            <a-input-search v-model="keyword" size="small" placeholder="搜索 IP/备注" allow-clear
              style="width: 110px" />
            <a-button size="small" type="text" @click="openForm(null)">
              <template #icon><icon-plus /></template>
            </a-button>
            <a-dropdown trigger="click" @select="(v: any) => onToolAction(String(v))">
              <a-button size="small" type="text"><icon-settings /></a-button>
              <template #content>
                <a-doption value="cred"><icon-lock /> 凭据管理</a-doption>
                <a-doption value="key"><icon-safe /> 主机密钥</a-doption>
                <a-doption value="sshgen"><icon-command /> 生成 SSH</a-doption>
                <a-doption value="import"><icon-import /> 导入 SSH 配置</a-doption>
              </template>
            </a-dropdown>
          </div>
        </template>

        <div class="conn-filter">
          <a-select v-model="groupFilter" size="mini" placeholder="分组" allow-clear style="width: 100px">
            <a-option value="__none">未分组</a-option>
            <a-option v-for="g in groups" :key="g" :value="g">{{ g }}</a-option>
          </a-select>
          <a-select v-model="tagFilter" size="mini" placeholder="标签" allow-clear style="width: 100px">
            <a-option v-for="t in allTags" :key="t" :value="t">{{ t }}</a-option>
          </a-select>
          <a-typography-text type="secondary" class="conn-count">{{ filtered.length }}/{{ hosts.length }}</a-typography-text>
        </div>

        <div class="conn-list">
          <!-- 选中高亮跟着右侧卡片走：当前会话的主机永远与右边对得上。胶囊菜单项与设置页左侧分类导航同口径 -->
          <div v-for="h in filtered" :key="h.id" class="conn-item"
            :class="{ 'inst-active': (activeTab?.hostId || activeHostId) === h.id }" @click="activeHostId = h.id"
            @dblclick="connect(h)">
            <!-- 配color时用自定义色小圈，否则回退存活/普通状态 -->
            <span v-if="h.color" class="color-dot" :style="{ background: h.color }" />
            <com-badge v-else :status="alive(h.id) ? 'success' : 'normal'" :animated="alive(h.id)" />
            <a-tooltip :content="isLocal(h) ? h.name : `${h.name} ${credUserOf(h)}@${h.address}:${h.port}`">
              <span class="conn-name">{{ h.name }}</span>
            </a-tooltip>
            <!-- 参考设置页菜单：地址用等宽次要文字，不再铺亮蓝标签底块，颜色只留给选中竖条；本地项显示 shell 标签 -->
            <span class="conn-addr">{{ isLocal(h) ? h.shell : h.address }}</span>
            <a-tag v-for="t in (h.tags || []).slice(0, 2)" :key="t" size="small">{{ t }}</a-tag>
            <a-dropdown trigger="hover" @select="(v: any) => onHostAction(v, h)">
              <icon-more class="conn-more" @click.stop />
              <template #content>
                <a-doption value="open"><icon-code /> 打开终端</a-doption>
                <a-doption v-if="!isLocal(h)" value="sftp"><icon-storage /> 在 SFTP 中打开</a-doption>
                <a-doption v-if="!isLocal(h)" value="edit"><icon-edit /> 编辑</a-doption>
                <a-doption v-if="!isLocal(h)" value="del" style="color: rgb(var(--red-6))"><icon-delete /> 删除</a-doption>
              </template>
            </a-dropdown>
          </div>
          <div v-if="!filtered.length" class="conn-empty">{{ loading ? '读取中…' : hosts.length ? '没有匹配的服务器' : '暂无记录，点右上「添加」新建' }}</div>
        </div>
      </a-card>
    </a-col>

    <!-- 右：终端。卡片头就是当前连接（多会话时点名字切换），正文由终端铺满 -->
    <a-col :span="17" class="main-col">
      <a-card v-if="!tabs.length" class="main-panel empty-card card-head-tool">
        <a-empty>
          <a-space direction="vertical" :size="10">
            <span>双击左侧服务器打开终端界面</span>
            <a-button size="small" @click="openForm(null)">点击添加服务器</a-button>
          </a-space>
        </a-empty>
      </a-card>

      <a-card v-else class="main-panel card-head-tool" :body-style="{ padding: 0 }">
        <template #title>
          <a-space :size="6">
            <com-badge :status="alive(activeTab?.hostId || '') ? 'success' : 'normal'"
              :animated="alive(activeTab?.hostId || '')" />
            <!-- 多个会话时，标题里的名字就是切换入口（只有一个会话就不出现箭头） -->
            <a-dropdown v-if="tabs.length > 1" trigger="click" @select="switchTab">
              <span class="sess-name">
                {{ activeHost?.name || '终端' }}
                <icon-down />
              </span>
              <template #content>
                <a-doption v-for="t in tabs" :key="t.key" :value="t.key">
                  {{ t.dead ? t.title + '（已断开）' : t.title }}
                </a-doption>
              </template>
            </a-dropdown>
            <span v-else>{{ activeHost?.name || '终端' }}</span>
            <a-tag v-if="activeHost" size="small" color="arcoblue">
              {{ isLocal(activeHost) ? activeHost.shell : `${credUserOf(activeHost)}@${activeHost.address}:${activeHost.port}` }}
            </a-tag>
            <a-tag v-if="activeTab?.dead" size="small" color="gray">已断开</a-tag>
          </a-space>
        </template>
        <template #extra>
          <a-space :size="6">
            <a-button v-if="activeTab?.dead" size="small" @click="reconnectActive">
              <template #icon><icon-refresh /></template>重新连接
            </a-button>
            <a-button size="small" @click="closeTab(activeKey)">
              <template #icon><icon-poweroff /></template>断开当前
            </a-button>
            <a-button size="small" @click="closeAllTabs">
              <template #icon><icon-close /></template>全部断开
            </a-button>
          </a-space>
        </template>

        <!-- 终端直接用 v-show 留在 DOM 里，切走只是隐藏，回来还是同一条会话 -->
        <div class="term-panes">
          <div v-for="t in tabs" v-show="t.key === activeKey" :key="t.key" class="term-pane">
            <term-console :open-session="(cols: number, rows: number) => openSession(t, cols, rows)"
              @ready="onReady(t)" @exit="(reason: string) => onExit(t, reason)" />
          </div>
        </div>
      </a-card>
    </a-col>
  </a-row>

  <host-form-modal v-model:visible="formVisible" :host="editingHost" @saved="onSaved" />
  <credential-modal v-model:visible="credVisible" @changed="credentials = $event" />
  <host-key-modal v-model:visible="keyVisible" />
  <ssh-gen-modal v-model:visible="sshGenVisible" :context="sshGenCtx" />
  <import-config-modal v-model:visible="importVisible" @imported="onImported" />
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, ref } from 'vue';
import { useRoute } from 'vue-router';
import { Message, Modal } from '@arco-design/web-vue';
import TermConsole from '@/components/business/term-console/index.vue';
import { useToolWindow } from '@/views/index/useToolWindow';
import HostFormModal from './components/HostFormModal.vue';
import CredentialModal from './components/CredentialModal.vue';
import HostKeyModal from './components/HostKeyModal.vue';
import ImportConfigModal from './components/ImportConfigModal.vue';
import type { TermCredential, TermHost, TermTab } from './types';

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const route = useRoute();
const { openToolWindow } = useToolWindow();

/* ==================== 连接记录 ==================== */
const hosts = ref<TermHost[]>([]);
const credentials = ref<TermCredential[]>([]);
const loading = ref(false);
const keyword = ref('');
const activeHostId = ref('');
const groupFilter = ref('');
const tagFilter = ref('');

/** 凭据管理/主机密钥/生成 SSH/导入配置 四个弹窗 */
const credVisible = ref(false);
const keyVisible = ref(false);
const importVisible = ref(false);
const sshGenVisible = ref(false);

function onToolAction(v: string) {
  if (v === 'cred') credVisible.value = true;
  else if (v === 'key') keyVisible.value = true;
  else if (v === 'sshgen') sshGenVisible.value = true;
  else if (v === 'import') importVisible.value = true;
}

/* SSH 生成助手：列表里选中了主机就把地址/账号预填进片段与部署 tab，未选中则空开 */
const sshGenCtx = computed(() => {
  const h = hosts.value.find((x) => x.id === activeHostId.value);
  if (!h) return {};
  const u = credUserOf(h);
  return { address: h.address, port: h.port || 22, user: u === '?' ? '' : u, keyPath: h.keyPath };
});

/** 所有非空分组（去重） */
const groups = computed(() => {
  const set = new Set<string>();
  for (const h of hosts.value) if (h.group) set.add(h.group);
  return [...set];
});

/** 所有标签（去重） */
const allTags = computed(() => {
  const set = new Set<string>();
  for (const h of hosts.value) for (const t of h.tags || []) set.add(t);
  return [...set];
});

/** 主机展示账号：自身旧字段优先，否则取关联凭据的账号 */
function credUserOf(h: TermHost): string {
  if (h.user) return h.user;
  if (h.credID) {
    const c = credentials.value.find((x) => x.id === h.credID);
    if (c?.user) return c.user;
  }
  return '?';
}

/** 是否本地默认项（CMD / PowerShell）：不走地址/凭据/SFTP，菜单只留「打开终端」 */
function isLocal(h: TermHost | null | undefined): boolean {
  return !!h && h.kind === 'local';
}

const filtered = computed(() => {
  const k = keyword.value.trim().toLowerCase();
  return hosts.value.filter((h) => {
    if (groupFilter.value) {
      if (groupFilter.value === '__none') {
        if (h.group) return false;
      } else if (h.group !== groupFilter.value) {
        return false;
      }
    }
    if (tagFilter.value && !(h.tags || []).includes(tagFilter.value)) return false;
    if (k) {
      const hay = `${h.name || ''} ${h.address || ''} ${h.note || ''}`.toLowerCase();
      if (!hay.includes(k)) return false;
    }
    return true;
  });
});

async function loadHosts() {
  loading.value = true;
  try {
    hosts.value = (await GoTools?.TermListHosts()) || [];
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '读取连接记录失败'), position: 'top', duration: 3000 });
  } finally {
    loading.value = false;
  }
}

async function loadCredentials() {
  try {
    credentials.value = (await GoTools?.TermListCredentials()) || [];
  } catch {
    credentials.value = [];
  }
}

/** 导入配置后刷新列表 */
function onImported(list: TermHost[]) {
  hosts.value = list && list.length ? list : hosts.value;
  loadCredentials();
}

/* ==================== 表单 ==================== */
const formVisible = ref(false);
const editingHost = ref<TermHost | null>(null);

function openForm(h: TermHost | null) {
  editingHost.value = h;
  formVisible.value = true;
}

/** 保存后：刷新列表并直接连上去（「保存并连接」） */
function onSaved(list: TermHost[], id: string) {
  hosts.value = list;
  const h = list.find((x) => x.id === id);
  if (h) connect(h);
}

/** 列表项的 ⋯ 菜单：动作都落在页面上，列表自身不处理业务 */
function onHostAction(v: string, h: TermHost) {
  if (v === 'open') connect(h);
  else if (v === 'sftp') void openInSftp(h);
  else if (v === 'edit') openForm(h);
  else if (v === 'del') confirmRemove(h);
}

// openInSftp 把终端主机导入为 SFTP 连接并开文件传输页自动连接（P2 互通）：
// 共享凭据只引用同一 credID，一处改密两端生效；后端 TermOpenInSFTP 返回新建连接 id。
async function openInSftp(h: TermHost) {
  try {
    const connId = String(await GoTools?.TermOpenInSFTP(h.id) ?? '');
    if (!connId) {
      Message.warning('未能导入 SFTP 连接');
      return;
    }
    await openToolWindow(`/tools/ftp?conn=${encodeURIComponent(connId)}`, '文件传输', { single: true });
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '在 SFTP 中打开失败'), position: 'top', duration: 3000 });
  }
}

function confirmRemove(h: TermHost) {
  Modal.warning({
    title: '删除连接记录',
    content: `确定删除连接记录「${h.name}」？这条记录上的终端会一并断开。`,
    hideCancel: false,
    onOk: () => removeHost(h),
  });
}

async function removeHost(h: TermHost) {
  try {
    hosts.value = (await GoTools?.TermDeleteHost(h.id)) || [];
    // 记录没了，对应的终端标签也收掉
    tabs.value = tabs.value.filter((t) => t.hostId !== h.id);
    syncActive();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '删除失败'), position: 'top', duration: 3000 });
  }
}

/* ==================== 终端会话 ==================== */
const tabs = ref<TermTab[]>([]);
const activeKey = ref('');
let tabSeq = 0;

const activeTab = computed(() => tabs.value.find((t) => t.key === activeKey.value) || null);
const activeHost = computed(() => hosts.value.find((h) => h.id === activeTab.value?.hostId) || null);

/** 这条记录对应的会话（没有则 undefined） */
function tabOf(hostId: string) {
  return tabs.value.find((t) => t.hostId === hostId);
}

/** 会话是否还活着：连上了且没断开，用来点亮列表/标题的圆点 */
function alive(hostId: string) {
  const t = tabOf(hostId);
  return !!t && !!t.ready && !t.dead;
}

function connect(h: TermHost) {
  activeHostId.value = h.id;
  const exist = tabOf(h.id);
  if (exist) {
    activeKey.value = exist.key;
    return;
  }
  tabSeq += 1;
  const tab: TermTab = {
    key: `tab${tabSeq}`,
    hostId: h.id,
    title: isLocal(h) ? h.name : `${h.name} [${h.address}]`,
  };
  tabs.value.push(tab);
  activeKey.value = tab.key;
}

/** 交给 term-console 的建会话回调：组件只管行列，不关心是 SSH 还是容器 */
async function openSession(t: TermTab, cols: number, rows: number) {
  const h = hosts.value.find((x) => x.id === t.hostId);
  const info = isLocal(h)
    ? await GoTools.TermOpenLocal(t.hostId, cols, rows)
    : await GoTools.TermOpenSSH(t.hostId, cols, rows);
  if (!info?.id) throw new Error('创建终端会话失败');
  return info as { id: string };
}

function onReady(t: TermTab) {
  t.ready = true;
  t.dead = false;
}

function onExit(t: TermTab, reason: string) {
  // 连都没连上（密码错、端口不通）：关掉这个空标签，别留一块黑屏
  if (!t.ready) {
    closeTab(t.key);
    Message.error({ content: reason || '连接失败', position: 'top', duration: 3000 });
    return;
  }
  if (t.dead) return;
  t.dead = true;
  Message.warning({ content: reason || '连接已断开', position: 'top', duration: 3000 });
}

/** 断开的会话原地重连：关掉旧标签再建一条，等于重建会话 */
function reconnectActive() {
  const t = activeTab.value;
  if (!t) return;
  const h = hosts.value.find((x) => x.id === t.hostId);
  if (!h) return;
  closeTab(t.key);
  connect(h);
}

/** 标题里的会话切换（只有多会话时才出现这个下拉） */
function switchTab(key: any) {
  if (key === undefined || key === null) return;
  activeKey.value = String(key);
}

function closeTab(key: string | number) {
  const idx = tabs.value.findIndex((t) => t.key === key);
  if (idx < 0) return;
  tabs.value.splice(idx, 1);
  if (activeKey.value === key) {
    const next = tabs.value[idx] || tabs.value[idx - 1];
    activeKey.value = next ? next.key : '';
  }
}

function closeAllTabs() {
  tabs.value = [];
  activeKey.value = '';
}

/** 标签被批量清掉后把活动键修正到还存在的标签上 */
function syncActive() {
  if (tabs.value.some((t) => t.key === activeKey.value)) return;
  activeKey.value = tabs.value[0]?.key || '';
}

onMounted(async () => {
  await loadHosts();
  loadCredentials();
  // 跨页互跳：文件传输页「在终端中打开」带来的主机 id，选中即连
  const hostQ = String(route.query?.host ?? '');
  if (hostQ) {
    const hit = hosts.value.find((x) => x.id === hostQ);
    if (hit) connect(hit);
  }
});
</script>

<style scoped>
/* 左右各一张卡片，卡片头放操作（与 PHP/MySQL 管理页同一套骨架） */
.term-page {
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

/* 终端要自己吃满剩余高度，不能跟着内容滚 */
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
  align-items: center;
}

.conn-filter {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 6px 8px;
  border-bottom: 1px solid var(--color-border-2);
}

.conn-count {
  margin-left: auto;
  font-size: var(--base-size-small);
}

/* 主机列表：胶囊菜单项，与设置页左侧分类导航（.cat-item）/凭据弹窗同口径：
   选中中性轻底 fill-2 + 加粗 + 左侧主色短竖条，hover fill-1，主色只走竖条不铺底 */
.conn-list {
  display: flex;
  flex-direction: column;
  gap: 4px; /* 纵向间距由 flex gap 统一给，不用项 margin（相邻会折叠） */
  padding: 6px 8px;
}

.conn-item {
  position: relative;
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  /* 左侧多留 4px：com-badge 用 -7px 负 margin 抵出血，会把状态点往左拉，
     12px 不够会让圆点贴住左侧选中竖条，16px 后点与竖条拉开距离 */
  padding: 8px 10px 8px 16px;
  border-radius: var(--base-radius-default);
  color: var(--color-text-2);
  cursor: pointer;
  transition: background-color 0.15s, color 0.15s;
}

/* 选中态的左侧短竖条：3px、高 55%，常态透明只留过渡 */
.conn-item::before {
  content: "";
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 55%;
  border-radius: 0 3px 3px 0;
  background: transparent;
  transition: background-color 0.15s;
}

.conn-item:hover {
  background: var(--color-fill-1);
  color: var(--color-text-1);
}

.inst-active,
.inst-active:hover {
  background: var(--color-fill-2);
  color: var(--color-text-1);
}

.inst-active::before {
  background: var(--app-select-rail);
}

.inst-active .conn-name {
  font-weight: 600;
}

.color-dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  display: inline-block;
  flex: none;
}

.conn-name {
  max-width: 118px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

/* 地址：等宽次要文字，与 FTP 列 inst-port 同款，避免彩色标签抢过选中态 */
.conn-addr {
  flex: none;
  font-family: var(--code-font-family);
  font-size: var(--base-size-small);
  color: var(--color-text-3);
}

.conn-more {
  flex: none;
  margin-left: auto;
  cursor: pointer;
  color: var(--color-text-3);
}

.conn-more:hover {
  color: var(--color-text-1);
}

.conn-empty {
  width: 100%;
  text-align: center;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
  padding: 24px 0;
}

/* 多会话时标题里的名字就是切换入口 */
.sess-name {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  cursor: pointer;
}

/* 所有会话都留在 DOM 里（v-show），只显示当前那条；终端自己按容器高度重算行列 */
.term-panes {
  flex: 1;
  min-height: 0;
}

.term-pane {
  height: 100%;
}
</style>
