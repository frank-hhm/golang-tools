<template>
  <!-- 端口管理面板：从首页「网络」卡片唤起。本机 TCP/UDP 端口 ↔ 归属进程 的对照表。
       引擎在 Go（tools/portman），本页轮询 PortSnapshot；结束进程/图标/定位/在线搜索复用 procman 的绑定。 -->
  <div class="pt-shell">
    <common-title-bar>
      <template #title>
        <span class="pt-sub">监听 {{ sum.listenCount }} 个端口 · 被 {{ sum.procCount }} 个进程占用</span>
        <span class="pt-tip">右键行：结束进程 · 打开文件位置 · 在线搜索；双击看详情</span>
      </template>
      <template #extra>
        <a-space :size="6">
          <!-- 本机地址：想知道「监听在 0.0.0.0 上到底是哪个网卡 IP」时不用再去 cmd 查 -->
          <a-tooltip v-if="sum.hosts?.length" :content="'本机地址：' + sum.hosts.join(' / ')" mini>
            <a-tag size="small">{{ sum.hosts?.[0] }}</a-tag>
          </a-tooltip>
          <!-- 能力边界写脸上：非管理员看不到别的用户/系统进程的 socket 归属，照实说，不猜 -->
          <a-tooltip v-if="unknownCount"
            content="这些行没有归属进程信息：以管理员身份重新打开本工具即可看全" mini>
            <a-tag size="small" color="orange">{{ unknownCount }} 行归属未知</a-tag>
          </a-tooltip>
        </a-space>
      </template>
    </common-title-bar>

    <a-card class="pt-card">
      <!-- 头部左侧：常驻概览条，数值随轮询原地刷新 -->
      <template #title>
        <div class="pt-status">
          <span>监听端口: <b>{{ sum.listenCount }}</b></span>
          <span>已连接: <b>{{ sum.connCount }}</b></span>
          <span>占用进程: <b>{{ sum.procCount }}</b></span>
          <span>本次显示: <b>{{ rows.length }}</b> / {{ all.length }}</span>
        </div>
      </template>
      <!-- 头部右侧：搜索 + 两个过滤维度 + 轮询间隔 + 手动刷新 -->
      <template #extra>
        <a-space :size="8">
          <a-input-search v-model="keyword" class="pt-search" placeholder="端口 / PID / 进程 / 地址" allow-clear
            size="small" />
          <a-select v-model="viewFilter" size="small" style="width: 92px">
            <a-option value="listen">仅监听</a-option>
            <a-option value="conn">仅连接</a-option>
            <a-option value="all">全部</a-option>
          </a-select>
          <a-select v-model="protoFilter" size="small" style="width: 82px">
            <a-option value="all">不限协议</a-option>
            <a-option value="tcp">TCP</a-option>
            <a-option value="udp">UDP</a-option>
          </a-select>
          <a-tooltip content="自动刷新间隔（暂停时停轮询，仍可手动刷）" mini>
            <a-select v-model="intervalMs" size="small" style="width: 96px">
              <a-option :value="1000">1 秒</a-option>
              <a-option :value="2000">2 秒</a-option>
              <a-option :value="5000">5 秒</a-option>
              <a-option :value="0">暂停</a-option>
            </a-select>
          </a-tooltip>
          <a-tooltip content="立即刷新" mini>
            <a-button size="small" :loading="loading" @click="refresh()">
              <template #icon><icon-refresh /></template>
            </a-button>
          </a-tooltip>
        </a-space>
      </template>

      <a-alert v-if="err" type="warning" show-icon class="pt-err" title="刷新失败，下表为上一次结果">{{ err }}</a-alert>

      <!-- 连接数上百是常态：虚拟滚动 + 横向滚动都开，列宽可拖 -->
      <a-table class="pt-table" :data="rows" :columns="columns" :loading="loading && all.length === 0"
        :pagination="false" row-key="_k" size="small" :row-class="rowClass" :scroll="{ y: '100%', x: 1160 }"
        :column-resizable="true" :virtual-list-props="{ height: '100%' }" @row-contextmenu="onContext"
        @row-click="onRowClick" @row-dblclick="(r: any) => openDetail(r)">
        <!-- 表头排序态自己画（与进程管理同一套形态），caret 槽位常占宽，切换时表头不抖 -->
        <template #colTitle="{ column }">
          <span class="pt-th" :class="{ 'is-active': sortKey === String(column.dataIndex) }"
            :title="'按' + column.title + '排序'" @click="toggleSort(String(column.dataIndex))">
            <span class="pt-th-text">{{ column.title }}</span>
            <span class="pt-th-caret">
              <icon-caret-up v-if="sortKey === String(column.dataIndex) && sortDir === 'asc'" />
              <icon-caret-down v-else-if="sortKey === String(column.dataIndex)" />
            </span>
          </span>
        </template>
        <template #proto="{ record }">
          <span class="pt-proto" :class="record.proto">{{ record.proto.toUpperCase() }}</span>
        </template>
        <template #local="{ record }">
          <span class="pt-addr">{{ record.laddr || '—' }}<i>:{{ record.lport }}</i></span>
        </template>
        <template #remote="{ record }">
          <span v-if="record.raddr || record.rport" class="pt-addr">{{ record.raddr }}<i>:{{ record.rport }}</i></span>
          <span v-else class="pt-muted">—</span>
        </template>
        <template #state="{ record }">
          <span class="pt-state" :class="'st-' + stateClass(record.state)">{{ record.state }}</span>
        </template>
        <template #name="{ record }">
          <div class="pt-name">
            <img v-if="record.exe && icons[record.exe]" :src="icons[record.exe]" class="pt-ic" alt="" />
            <span v-else class="pt-ic pt-ic-ph"></span>
            <span class="pt-pname" :class="{ 'is-unknown': !record.pid }">{{ record.name }}</span>
          </div>
        </template>
        <template #pid="{ record }">
          <span class="pt-num">{{ record.pid || '—' }}</span>
        </template>
        <template #exe="{ record }">
          <a-tooltip v-if="record.exe" :content="record.exe" mini>
            <span class="pt-exe">{{ record.exe }}</span>
          </a-tooltip>
          <span v-else class="pt-muted">—</span>
        </template>
        <template #empty>
          <div class="pt-empty">{{ all.length ? '没有匹配的端口' : '未收到端口数据（引擎或绑定异常）' }}</div>
        </template>
      </a-table>
    </a-card>

    <!-- 右键菜单：这里手里只有 exe 路径，定位/在线搜索走「按 exe」的那三个绑定 -->
    <div v-if="menu.visible" ref="menuEl" class="pt-menu" :style="{ left: menu.x + 'px', top: menu.y + 'px' }"
      @contextmenu.prevent>
      <div class="pt-menu-head">
        <span class="pt-menu-title">{{ menu.row ? menu.row.proto.toUpperCase() + ':' + menu.row.lport : '' }}</span>
        <span class="pt-menu-pid">{{ menu.row?.name }}</span>
      </div>
      <div class="pt-menu-item" @click="copyLocal">复制本地地址</div>
      <div class="pt-menu-sep"></div>
      <div class="pt-menu-item danger" :class="{ off: !canKill(menu.row) }" @click="doKill">结束占用进程</div>
      <div class="pt-menu-item" :class="{ off: !menu.row?.exe }" @click="doOpenPath">打开文件位置</div>
      <div class="pt-menu-item" :class="{ off: !menu.row?.exe }" @click="doVt">在线搜索 (VirusTotal)</div>
      <div class="pt-menu-sep"></div>
      <div class="pt-menu-item" @click="openDetail(menu.row!)">详情…</div>
    </div>

    <!-- 详情弹窗：字段行直接复用 common-info-row（标签 | 内容 | 右侧操作） -->
    <a-modal v-model:visible="detail.visible" :title="detailTitle" :width="600" :footer="false">
      <div class="pt-detail">
        <common-info-row label="协议" :value="detail.row ? detail.row.proto.toUpperCase() : ''" />
        <common-info-row label="本地地址" :value="detail.row ? detail.row.laddr + ':' + detail.row.lport : ''"
          action-text="复制" @action="copy(detail.row ? detail.row.laddr + ':' + detail.row.lport : '')" />
        <common-info-row label="远程地址" :grey="!detail.row?.rport"
          :value="(detail.row?.raddr || detail.row?.rport) ? detail.row!.raddr + ':' + detail.row!.rport : '—（未连接）'" />
        <common-info-row label="状态" :value="detail.row?.state || ''" />
        <common-info-row label="进程" :value="detail.row?.name || ''" :grey="!detail.row?.pid" />
        <common-info-row label="进程 ID" :value="detail.row?.pid ? String(detail.row.pid) : '—'" />
        <common-info-row label="可执行文件" :grey="!detail.row?.exe" :value="detail.row?.exe || '（未取到路径，可能是权限不足）'"
          :action-text="detail.row?.exe ? '复制' : ''" @action="copy(detail.row?.exe)" />
        <common-info-row v-if="detail.row && !detail.row.pid" label="归属" grey
          value="看不到归属进程：以管理员身份重新打开本工具即可" />
      </div>
      <div class="pt-detail-ops">
        <a-button size="small" :disabled="!detail.row?.exe" @click="detail.row && doVtOf(detail.row)">在线搜索</a-button>
        <a-button size="small" :disabled="!detail.row?.exe" @click="detail.row && openPathOf(detail.row)">打开文件位置</a-button>
        <a-button v-if="caps.nativeWindows" size="small" status="danger" :disabled="!canKill(detail.row)"
          @click="detail.row && killRow(detail.row)">结束占用进程</a-button>
      </div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { Message, Modal, type TableColumnData } from '@arco-design/web-vue';
import { IconRefresh, IconCaretUp, IconCaretDown } from '@arco-design/web-vue/es/icon';
import { useCaps } from '@/utils/caps';

interface PortRow {
  proto: string; listen: boolean;
  laddr: string; lport: number;
  raddr: string; rport: number;
  state: string; pid: number; name: string; exe: string;
}
interface Summary { listenCount: number; connCount: number; procCount: number; hosts: string[] | null; }
/** 端口行没有天然主键（同一进程可能报出两条一模一样的行），这里补一个内容指纹当 row-key */
interface ViewRow extends PortRow { _k: string; }

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const caps = useCaps();

const all = ref<PortRow[]>([]);
const sum = ref<Summary>({ listenCount: 0, connCount: 0, procCount: 0, hosts: [] });
/** 图标按 exe 缓存（同一条 exe 可能占几十行，按 pid 缓存会重复要） */
const icons = reactive<Record<string, string>>({});
const keyword = ref('');
const viewFilter = ref<'listen' | 'conn' | 'all'>('listen');
const protoFilter = ref<'all' | 'tcp' | 'udp'>('all');
const intervalMs = ref(2000);
const loading = ref(false);
const err = ref('');

/**
 * dataIndex 在这页里兼两个身份：Arco 只给有 dataIndex 的列挂拖宽手柄，
 * 同时它也当排序键用（数值列直接取字段，文本列 localeCompare）。
 * 本地/远程两列按端口号排（字符串排会把 80 排在 9 后面）。
 */
const columns: TableColumnData[] = [
  { title: '协议', dataIndex: 'proto', slotName: 'proto', width: 72, titleSlotName: 'colTitle' },
  { title: '本地地址', dataIndex: 'lport', slotName: 'local', width: 180, titleSlotName: 'colTitle' },
  { title: '状态', dataIndex: 'state', slotName: 'state', width: 128, titleSlotName: 'colTitle' },
  { title: '远程地址', dataIndex: 'rport', slotName: 'remote', width: 180, titleSlotName: 'colTitle' },
  { title: '进程', dataIndex: 'name', slotName: 'name', width: 210, ellipsis: true, titleSlotName: 'colTitle' },
  { title: 'PID', dataIndex: 'pid', slotName: 'pid', width: 84, align: 'right', titleSlotName: 'colTitle' },
  { title: '可执行文件', dataIndex: 'exe', slotName: 'exe', width: 300, ellipsis: true, titleSlotName: 'colTitle' },
];

/* ==================== 过滤 + 排序 ==================== */
const NUMERIC_KEYS = new Set(['lport', 'rport', 'pid']);
type SortDir = 'asc' | 'desc';
const sortKey = ref('lport');
const sortDir = ref<SortDir>('asc');

function cmpRows(a: PortRow, b: PortRow): number {
  const dir = sortDir.value === 'asc' ? 1 : -1;
  const key = sortKey.value as keyof PortRow;
  let r: number;
  if (NUMERIC_KEYS.has(sortKey.value)) r = (Number(a[key]) - Number(b[key])) * dir;
  else r = String(a[key] ?? '').localeCompare(String(b[key] ?? '')) * dir;
  if (r) return r;
  // 同值稳定兜底（不随方向翻转）：否则每轮轮询都会看到同端口的几行在来回跳
  return a.proto.localeCompare(b.proto) || a.pid - b.pid || a.rport - b.rport;
}

function toggleSort(key: string) {
  if (sortKey.value === key) {
    sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc';
    return;
  }
  sortKey.value = key;
  // 数值列第一次点先看大的在上面，文本列从 A→Z
  sortDir.value = NUMERIC_KEYS.has(key) ? 'desc' : 'asc';
}

function hitKeyword(r: PortRow, kw: string): boolean {
  return String(r.lport).includes(kw) || String(r.rport).includes(kw) || String(r.pid) === kw
    || r.name.toLowerCase().includes(kw) || r.laddr.toLowerCase().includes(kw)
    || r.raddr.toLowerCase().includes(kw) || r.exe.toLowerCase().includes(kw)
    || r.state.toLowerCase().includes(kw);
}

const rows = computed<ViewRow[]>(() => {
  const kw = keyword.value.trim().toLowerCase();
  const list = all.value.filter((r) => {
    if (protoFilter.value !== 'all' && r.proto !== protoFilter.value) return false;
    if (viewFilter.value === 'listen' && !r.listen) return false;
    if (viewFilter.value === 'conn' && r.listen) return false;
    return !kw || hitKeyword(r, kw);
  }).sort(cmpRows);
  // 指纹相同的重复行加序号后缀：虚拟列表拿重复 key 会渲染错乱
  const seen = new Map<string, number>();
  return list.map((r) => {
    const base = `${r.proto}|${r.laddr}|${r.lport}|${r.raddr}|${r.rport}|${r.state}|${r.pid}`;
    const n = (seen.get(base) || 0) + 1;
    seen.set(base, n);
    return { ...r, _k: n > 1 ? `${base}#${n}` : base };
  });
});

/** 归属未知（非管理员看不到的那些行）的行数，直接摆在标题栏当能力提示 */
const unknownCount = computed(() => all.value.reduce((n, r) => n + (r.pid ? 0 : 1), 0));

/* ==================== 快照轮询 ==================== */
let timer: number | null = null;
/**
 * 拉一次快照。silent = 后台轮询：不亮刷新按钮的 loading，否则每 2 秒闪一次。
 * 失败只抬出错误横幅并保留旧表，不把列表抹空（用户可能正右键在某行上）。
 */
async function refresh(silent = false) {
  if (typeof GoTools?.PortSnapshot !== 'function') return;
  if (!silent) loading.value = true;
  try {
    const snap: any = await GoTools.PortSnapshot();
    all.value = snap?.rows || [];
    sum.value = snap?.sum || sum.value;
    err.value = '';
    void loadIcons();
  } catch (e: any) {
    err.value = String(e?.message || e);
  } finally {
    if (!silent) loading.value = false;
  }
}
function poll() { return refresh(true); }
function restartTimer() {
  if (timer != null) { clearInterval(timer); timer = null; }
  if (intervalMs.value > 0) timer = window.setInterval(poll, intervalMs.value);
}
// 轮询间隔下拉框改完立即生效（只挂一次定时器它就是个摆设）；0 = 暂停轮询
watch(intervalMs, restartTimer);
// 过滤/排序后新冒出来的行也要补图标
watch(rows, () => { void loadIcons(); });

// 图标 best-effort：只取当前可见行、按 exe 去重、限并发（串行 await 会被一轮几百个往返拖死）
const ICON_CONC = 6;
let iconBusy = false;
async function loadIcons() {
  if (iconBusy || !caps.nativeWindows || typeof GoTools?.PortIcon !== 'function') return;
  const pend = [...new Set(rows.value.map((r) => r.exe).filter((e) => e && icons[e] === undefined))];
  if (!pend.length) return;
  iconBusy = true;
  try {
    let i = 0;
    await Promise.all(Array.from({ length: Math.min(ICON_CONC, pend.length) }, async () => {
      while (i < pend.length) {
        const exe = pend[i++];
        if (icons[exe] !== undefined) continue;
        try { icons[exe] = (await GoTools.PortIcon(exe)) || ''; }
        catch { icons[exe] = ''; }
      }
    }));
  } finally { iconBusy = false; }
}

/* ==================== 右键菜单 ==================== */
const menu = reactive<{ visible: boolean; x: number; y: number; row: ViewRow | null }>({
  visible: false, x: 0, y: 0, row: null,
});
const menuEl = ref<HTMLElement | null>(null);
function onContext(record: any, ev: any) {
  ev.preventDefault();
  menu.row = record;
  selected.value = record?._k ?? null;
  void showMenu(ev.clientX, ev.clientY);
}
/** 先按点击位置显示，再量真实尺寸夹回视口 */
async function showMenu(x: number, y: number) {
  menu.x = x; menu.y = y; menu.visible = true;
  await nextTick();
  const el = menuEl.value;
  if (!el) return;
  menu.x = Math.max(4, Math.min(x, window.innerWidth - el.offsetWidth - 4));
  menu.y = Math.max(4, Math.min(y, window.innerHeight - el.offsetHeight - 4));
}
function closeMenu() { menu.visible = false; menu.row = null; }
/** 表格之外的右键也要收菜单 */
function onWinContext(ev: MouseEvent) {
  if (!(ev.target as HTMLElement).closest('.pt-table')) closeMenu();
}

/* ==================== 选中行 + 键盘快捷键 ==================== */
const selected = ref<string | null>(null);
const activeRow = computed<ViewRow | null>(
  () => rows.value.find((r) => r._k === selected.value) || null,
);
function onRowClick(record: any) { selected.value = record?._k ?? null; }
/** Del = 结束选中行的占用进程（和菜单里那条是同一个动作，同样要确认） */
function onKeydown(ev: KeyboardEvent) {
  if (ev.key === 'Escape') { closeMenu(); return; }
  if (ev.key !== 'Delete' && ev.key !== 'Backspace') return;
  if (detail.visible) return; // 详情弹窗开着时不抢键
  const t = ev.target as HTMLElement | null;
  if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
  const row = menu.row || activeRow.value;
  if (!row) return;
  ev.preventDefault();
  closeMenu();
  killRow(row);
}

/* ==================== 操作（结束进程复用 procman 的 ProcKill） ==================== */
async function run(fn: () => Promise<any>, okText: string) {
  try { await fn(); Message.success({ content: okText, position: 'top' }); setTimeout(poll, 400); }
  catch (e: any) { Message.error({ content: String(e?.message || e), position: 'top', duration: 5000 }); }
}
/** 结束进程在这页是「隔空杀人」：端口背后可能是数据库、浏览器，一律先确认 */
function canKill(row: PortRow | null | undefined): boolean {
  return !!row && !!row.pid && caps.nativeWindows;
}
function killRow(row: PortRow) {
  if (!canKill(row)) {
    Message.warning(row?.pid ? '当前构建不支持结束进程' : '该行没有归属进程信息');
    return;
  }
  Modal.warning({
    title: '结束占用进程',
    hideCancel: false,
    content: `将结束「${row.name}」(PID ${row.pid})，它是 ${row.proto.toUpperCase()}:${row.lport} 的持有者。`
      + '该进程的其他连接也会一起断，可能丢失未保存数据。确定继续？',
    onOk: () => run(() => GoTools.ProcKill(row.pid, false), '已结束进程'),
  });
}
function doKill() { const row = menu.row; closeMenu(); if (row) killRow(row); }
function openPathOf(row: PortRow) {
  if (!row.exe) { Message.warning('未取到可执行文件路径（可能是权限不足）'); return; }
  run(() => GoTools.PortOpenPath(row.exe), '已在文件管理器中定位');
}
function doOpenPath() { const row = menu.row; closeMenu(); if (row) openPathOf(row); }
function doVtOf(row: PortRow) {
  if (!row.exe) { Message.warning('未取到可执行文件路径（可能是权限不足）'); return; }
  run(() => GoTools.PortOpenVt(row.exe), '已在浏览器打开 VirusTotal');
}
function doVt() { const row = menu.row; closeMenu(); if (row) doVtOf(row); }
function copyLocal() {
  const row = menu.row;
  closeMenu();
  if (row) copy(`${row.laddr}:${row.lport}`);
}
function copy(text?: string) {
  if (!text) return;
  navigator.clipboard?.writeText(text).then(() => Message.success('已复制')).catch(() => { });
}

/* ==================== 详情弹窗 ==================== */
const detail = reactive<{ visible: boolean; row: ViewRow | null }>({ visible: false, row: null });
const detailTitle = computed(() =>
  detail.row ? `${detail.row.proto.toUpperCase()}:${detail.row.lport} 详情` : '端口详情');
function openDetail(record: ViewRow) {
  closeMenu();
  detail.row = record;
  detail.visible = true;
}

/* ==================== 展示 ==================== */
function stateClass(s: string): string {
  if (s === 'LISTEN') return 'listen';
  if (s === 'ESTABLISHED') return 'estab';
  if (s === '-') return 'none';
  if (s.indexOf('WAIT') >= 0 || s.indexOf('CLOSE') >= 0) return 'closing';
  return 'other';
}
function rowClass(record: ViewRow): string {
  const cls: string[] = [];
  if (!record.pid) cls.push('row-unknown');
  if (record._k === selected.value) cls.push('row-active');
  return cls.join(' ');
}

onMounted(() => {
  refresh(); restartTimer();
  // 监听挂在挂载/卸载两头：工具窗关开多次也不会叠上一层死监听
  window.addEventListener('click', closeMenu);
  window.addEventListener('contextmenu', onWinContext);
  window.addEventListener('blur', closeMenu);
  window.addEventListener('keydown', onKeydown);
});
onBeforeUnmount(() => {
  if (timer != null) clearInterval(timer);
  window.removeEventListener('click', closeMenu);
  window.removeEventListener('contextmenu', onWinContext);
  window.removeEventListener('blur', closeMenu);
  window.removeEventListener('keydown', onKeydown);
});
</script>

<style scoped>
.pt-shell {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  box-sizing: border-box;
}

.pt-card {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.pt-card :deep(.arco-card-body) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* 标题栏注入区已自带间距与 gap，这里只定字号与颜色 */
.pt-sub {
  color: var(--color-text-2);
  font-size: var(--base-size-small);
  white-space: nowrap;
}

.pt-tip {
  flex: 1;
  min-width: 0;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pt-search {
  width: 200px;
}

.pt-err {
  margin-bottom: 8px;
  flex: none;
}

.pt-table {
  flex: 1;
  min-height: 0;
}

/* 表头排序：整块标题可点，caret 槽位常占宽，激活时只改颜色不推标题 */
.pt-th {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  cursor: pointer;
  user-select: none;
}

.pt-th:hover {
  color: var(--color-text-1);
}

.pt-th.is-active {
  color: rgb(var(--primary-6));
}

.pt-th-caret {
  display: inline-flex;
  width: 12px;
  justify-content: center;
  font-size: var(--base-size-small);
}

/* 拖宽手柄 Arco 只给了 8px 隐形条，hover 时补一条可见竖线，否则没人知道能拖 */
.pt-table :deep(.arco-table-column-handle::after) {
  content: '';
  position: absolute;
  top: 6px;
  bottom: 6px;
  right: 3px;
  width: 2px;
  border-radius: var(--base-radius-small);
  background: transparent;
}

.pt-table :deep(.arco-table-column-handle:hover::after) {
  background: rgb(var(--primary-3));
}

/* 卡片头左侧概览条：等宽数字防轮询时数值抖宽，项间用细分隔线 */
.pt-status {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0 var(--base-padding);
  color: var(--color-text-3);
  font-size: var(--base-size-small);
  font-variant-numeric: tabular-nums;
}

.pt-status span+span {
  padding-left: var(--base-padding);
  border-left: 1px solid var(--color-border-2);
}

.pt-status b {
  color: var(--color-text-1);
  font-weight: 500;
  margin-left: 2px;
}

.pt-proto {
  font-size: var(--base-size-small);
  color: var(--color-text-2);
}

.pt-proto.udp {
  color: rgb(var(--cyan-6));
}

.pt-addr {
  font-variant-numeric: tabular-nums;
  white-space: nowrap;
}

.pt-addr i {
  font-style: normal;
  color: var(--color-text-1);
  font-weight: 500;
}

.pt-state {
  font-size: var(--base-size-small);
  color: var(--color-text-2);
}

.pt-state.st-listen {
  color: rgb(var(--primary-6));
}

.pt-state.st-estab {
  color: rgb(var(--green-6));
}

.pt-state.st-closing {
  color: rgb(var(--orange-6));
}

.pt-state.st-none {
  color: var(--color-text-4);
}

.pt-name {
  display: flex;
  align-items: center;
  gap: 5px;
  white-space: nowrap;
  overflow: hidden;
}

.pt-ic {
  width: 16px;
  height: 16px;
  flex: none;
  object-fit: contain;
}

.pt-ic-ph {
  display: inline-block;
}

.pt-pname {
  overflow: hidden;
  text-overflow: ellipsis;
}

.pt-pname.is-unknown {
  color: var(--color-text-4);
}

.pt-num {
  font-variant-numeric: tabular-nums;
}

.pt-exe {
  color: var(--color-text-3);
  font-size: var(--base-size-small);
}

.pt-muted {
  color: var(--color-text-4);
}

.pt-empty {
  color: var(--color-text-3);
}

/* 归属未知的行整体压暗，一眼看出「这几行不是没进程，是看不见」 */
.pt-table :deep(.row-unknown) {
  color: var(--color-text-3);
  background: var(--color-fill-1);
}

/* 选中行按公共选中态规范：首列左侧短竖条，不加深边框也不改背景 */
.pt-table :deep(.row-active > td:first-child) {
  box-shadow: inset 3px 0 0 rgb(var(--primary-6));
}

/* 右键菜单（与进程管理同一套形态） */
.pt-menu {
  position: fixed;
  z-index: 2000;
  min-width: 200px;
  background: var(--color-bg-popup);
  border: 1px solid var(--color-border-2);
  border-radius: var(--base-radius-default);
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.16);
  padding: 4px 0;
  user-select: none;
}

.pt-menu-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  gap: 12px;
  padding: 6px 14px;
  border-bottom: 1px solid var(--color-border-2);
  margin-bottom: 4px;
}

.pt-menu-title {
  font-weight: 600;
  font-size: var(--base-size-large);
  font-variant-numeric: tabular-nums;
}

.pt-menu-pid {
  color: var(--color-text-3);
  font-size: var(--base-size-small);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pt-menu-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 6px 14px;
  font-size: var(--base-size-large);
  color: var(--color-text-1);
  cursor: pointer;
}

.pt-menu-item:hover {
  background: var(--color-fill-2);
}

.pt-menu-item.danger {
  color: rgb(var(--red-6));
}

.pt-menu-item.off {
  color: var(--color-text-4);
  pointer-events: none;
}

.pt-menu-sep {
  height: 1px;
  background: var(--color-border-2);
  margin: 4px 0;
}

/* 详情弹窗：字段行复用 common-info-row，只在这里去掉末行分隔线并限制高度可滚 */
.pt-detail {
  max-height: 58vh;
  overflow: auto;
}

.pt-detail :deep(.info-row:last-child) {
  border-bottom: none;
}

.pt-detail-ops {
  display: flex;
  gap: var(--base-margin);
  margin-top: var(--base-margin);
}
</style>
