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
              style="width: 124px" />
            <a-button size="small" @click="openForm(null)">
              <template #icon><icon-plus /></template>添加
            </a-button>
          </div>
        </template>

        <a-list :bordered="false" size="small">
          <!-- 选中高亮跟着右侧卡片走：当前会话的主机永远与右边对得上 -->
          <a-list-item v-for="h in filtered" :key="h.id" :class="{ 'inst-active': (activeTab?.hostId || activeHostId) === h.id }"
            @click="activeHostId = h.id" @dblclick="connect(h)">
            <a-space>
              <!-- 圆点只在「这条记录有存活的会话」时点亮，和实例列表一套语义 -->
              <com-badge :status="alive(h.id) ? 'success' : 'normal'" :animated="alive(h.id)" />
              <a-tooltip :content="`${h.name} ${h.user}@${h.address}:${h.port}`">
                <span class="conn-name">{{ h.name }}</span>
              </a-tooltip>
              <!-- 列表窄：标签只放地址，端口在悬停提示里（和原来 [IP] 的写法一致） -->
              <a-tag size="small" color="arcoblue">{{ h.address }}</a-tag>
            </a-space>
            <template #actions>
              <a-dropdown trigger="hover" @select="(v: any) => onHostAction(v, h)">
                <icon-more class="conn-more" @click.stop />
                <template #content>
                  <a-doption value="open"><icon-code /> 打开终端</a-doption>
                  <a-doption value="edit"><icon-edit /> 编辑</a-doption>
                  <a-doption value="del"><icon-delete /> 删除</a-doption>
                </template>
              </a-dropdown>
            </template>
          </a-list-item>
          <a-list-item v-if="!filtered.length">
            <div class="conn-empty">{{ loading ? '读取中…' : hosts.length ? '没有匹配的服务器' : '暂无记录，点右上「添加」新建' }}</div>
          </a-list-item>
        </a-list>
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
              {{ activeHost.user }}@{{ activeHost.address }}:{{ activeHost.port }}
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
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, ref } from 'vue';
import { Message, Modal } from '@arco-design/web-vue';
import TermConsole from '@/components/business/term-console/index.vue';
import HostFormModal from './components/HostFormModal.vue';
import type { TermHost, TermTab } from './types';

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

/* ==================== 连接记录 ==================== */
const hosts = ref<TermHost[]>([]);
const loading = ref(false);
const keyword = ref('');
const activeHostId = ref('');

const filtered = computed(() => {
  const k = keyword.value.trim().toLowerCase();
  if (!k) return hosts.value;
  return hosts.value.filter(
    (h) => (h.name || '').toLowerCase().includes(k) || (h.address || '').toLowerCase().includes(k),
  );
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

/** 列表项的 ⋯ 菜单：三个动作都落在页面上，列表自身不处理业务 */
function onHostAction(v: string, h: TermHost) {
  if (v === 'open') connect(h);
  else if (v === 'edit') openForm(h);
  else if (v === 'del') confirmRemove(h);
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
    title: `${h.name} [${h.address}]`,
  };
  tabs.value.push(tab);
  activeKey.value = tab.key;
}

/** 交给 term-console 的建会话回调：组件只管行列，不关心是 SSH 还是容器 */
async function openSession(t: TermTab, cols: number, rows: number) {
  const info = await GoTools.TermOpenSSH(t.hostId, cols, rows);
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

onMounted(loadHosts);
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
}

.conn-name {
  max-width: 118px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.conn-more {
  cursor: pointer;
  color: var(--color-text-3);
}

.conn-empty {
  width: 100%;
  text-align: center;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
  padding: 6px 0;
}

.inst-active {
  background: var(--color-primary-light-1);
}

:deep(.arco-list-item) {
  cursor: pointer;
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
