<template>
  <!-- 进程管理面板：从首页 CPU 卡片唤起。进程树 + 右键操作（结束/挂起/重启/属性/在线搜索）。
       引擎全在 Go（tools/procman），本页轮询 ProcSnapshot、按 ppid 组树、渲染虚拟表。 -->
  <div class="pm-shell">
    <common-title-bar>
      <template #title>
        <span class="pm-sub">共 {{ sum.procCount }} 个进程<template v-if="keyword"> · 匹配 {{ flatRows.length }}</template></span>
        <span class="pm-tip">右键或选中后按 Del / Shift+Del：结束 · 挂起 · 重启 · 看属性</span>
      </template>
      <template #extra>
        <!-- 能力边界写脸上：非 Windows 快照可看、操作不可用，免得右键点了没反应当故障 -->
        <a-tag v-if="!caps.nativeWindows" size="small" color="orange">非 Windows：仅可查看</a-tag>
      </template>
    </common-title-bar>
    <a-card class="pm-card">
      <!-- 头部左侧：常驻概览条（CPU 使用 / 提交负荷 / 进程数 / 物理内存），数值随轮询原地刷新 -->
      <template #title>
        <div class="pm-status">
          <span>CPU 使用: <b :class="cpuClass(sum.cpuPct)">{{ fmtCpu(sum.cpuPct) }}</b></span>
          <span>提交负荷: <b :class="loadClass(sum.commitPct)">{{ fmtPct(sum.commitPct) }}</b></span>
          <span>进程: <b>{{ sum.procCount }}</b></span>
          <span>物理内存使用: <b :class="loadClass(sum.memPct)">{{ fmtPct(sum.memPct) }}</b></span>
        </div>
      </template>
      <!-- 头部右侧：过滤 + 轮询间隔 + 展开/折叠/刷新（图标钮，与首页刷新同一形态） -->
      <template #extra>
        <a-space :size="8">
          <a-input-search v-model="keyword" class="pm-search" placeholder="名称或 PID" allow-clear size="small" />
          <a-tooltip content="自动刷新间隔（暂停时停轮询，仍可手动刷）" mini>
            <a-select v-model="intervalMs" size="small" style="width: 96px">
              <a-option :value="1000">1 秒</a-option>
              <a-option :value="2000">2 秒</a-option>
              <a-option :value="5000">5 秒</a-option>
              <a-option :value="0">暂停</a-option>
            </a-select>
          </a-tooltip>
          <a-tooltip content="展开全部" mini>
            <a-button size="small" @click="expandAll">
              <template #icon><icon-expand /></template>
            </a-button>
          </a-tooltip>
          <a-tooltip content="折叠全部" mini>
            <a-button size="small" @click="collapseAll">
              <template #icon><icon-shrink /></template>
            </a-button>
          </a-tooltip>
          <a-tooltip content="立即刷新" mini>
            <a-button size="small" :loading="loading" @click="refresh()">
              <template #icon><icon-refresh /></template>
            </a-button>
          </a-tooltip>
        </a-space>
      </template>

      <a-alert v-if="err" type="warning" show-icon class="pm-err" title="刷新失败，下表为上一次结果">{{ err }}</a-alert>

      <!-- 进程表：虚拟滚动扛几百行 × 高频刷新；单击选中（吃键盘快捷键）、双击看属性、右键弹自定义菜单 -->
      <a-table class="pm-table" :data="flatRows" :columns="columns" :loading="loading && procs.length === 0"
        :pagination="false" row-key="pid" size="small" :row-class="rowClass" :scroll="{ y: '100%', x: 920 }"
        :column-resizable="true" :virtual-list-props="{ height: '100%' }" @row-contextmenu="onContext"
        @row-click="onRowClick" @row-dblclick="(r: any) => openProps(r)">
        <!-- 表头排序态自己画（不用 Arco 内置 sortable，它会把树排碎）；caret 槽位常占宽，切换时表头不抖 -->
        <template #colTitle="{ column }">
          <span class="pm-th" :class="{ 'is-active': sortKey === String(column.dataIndex) }"
            :title="'按' + column.title + '排序'" @click="toggleSort(String(column.dataIndex))">
            <span class="pm-th-text">{{ column.title }}</span>
            <span class="pm-th-caret">
              <icon-caret-up v-if="sortKey === String(column.dataIndex) && sortDir === 'asc'" />
              <icon-caret-down v-else-if="sortKey === String(column.dataIndex)" />
            </span>
          </span>
        </template>
        <template #name="{ record }">
          <div class="pm-name" :style="{ paddingLeft: record.depth * 15 + 'px' }">
            <span class="pm-caret" :class="{ leaf: !record.hasChildren }"
              @click.stop="record.hasChildren && toggle(record.pid)">
              <icon-caret-right :class="{ open: !collapsed.has(record.pid) }" />
            </span>
            <img v-if="icons[record.pid]" :src="icons[record.pid]" class="pm-ic" alt="" />
            <span v-else class="pm-ic pm-ic-ph"></span>
            <span class="pm-pname" :class="{ 'is-sys': record._sys, 'is-self': record.pid === ownPid }">
              {{ record.name }}
            </span>
          </div>
        </template>
        <template #cpu="{ record }">
          <span class="pm-num" :class="cpuClass(record.cpuPct)">{{ fmtCpu(record.cpuPct) }}</span>
        </template>
        <template #priv="{ record }"><span class="pm-num">{{ fmtBytes(record.privateBytes) }}</span></template>
        <template #ws="{ record }"><span class="pm-num">{{ fmtBytes(record.workingSet) }}</span></template>
        <template #desc="{ record }">
          <a-tooltip v-if="record.description" :content="record.description" mini>
            <span class="pm-desc">{{ record.description }}</span>
          </a-tooltip>
          <span v-else class="pm-desc pm-muted">—</span>
        </template>
        <template #company="{ record }">
          <span class="pm-muted">{{ record.company || '—' }}</span>
        </template>
        <template #empty>
          <div class="pm-empty">{{ keyword ? '没有匹配的进程' : '未收到进程数据（引擎或绑定异常）' }}</div>
        </template>
      </a-table>

    </a-card>

    <!-- 右键菜单 -->
    <div v-if="menu.visible" ref="menuEl" class="pm-menu" :style="{ left: menu.x + 'px', top: menu.y + 'px' }"
      @contextmenu.prevent>
      <div class="pm-menu-head">
        <span class="pm-menu-title">{{ menu.row?.name }}</span>
        <span class="pm-menu-pid">PID {{ menu.row?.pid }}</span>
      </div>
      <div class="pm-menu-item danger" @click="doKill(false)">结束进程 <kbd>Del</kbd></div>
      <div class="pm-menu-item danger" :class="{ off: !menu.row?.hasChildren }" @click="doKill(true)">
        结束进程树 <kbd>Shift+Del</kbd>
      </div>
      <div class="pm-menu-sep"></div>
      <div class="pm-menu-item" @click="doSuspend(true)">挂起</div>
      <div class="pm-menu-item" @click="doSuspend(false)">恢复</div>
      <div class="pm-menu-item" @click="doRestart">重启</div>
      <div class="pm-menu-sep"></div>
      <div class="pm-menu-sub">
        <span>分配优先级</span>
        <div class="pm-submenu">
          <div class="pm-menu-item" @click="doPriority('high')">高</div>
          <div class="pm-menu-item" @click="doPriority('normal')">正常</div>
          <div class="pm-menu-item" @click="doPriority('low')">低</div>
        </div>
      </div>
      <div class="pm-menu-sep"></div>
      <div class="pm-menu-item" @click="openPath">打开文件位置</div>
      <div class="pm-menu-item" @click="doVt">在线搜索 (VirusTotal)</div>
      <div class="pm-menu-item" @click="openProps(menu.row!)">属性…</div>
    </div>

    <!-- 属性弹窗：字段行直接复用 common-info-row（标签 | 内容 | 右侧操作），长路径行内自带复制 -->
    <a-modal v-model:visible="props.visible" :title="props.row?.name + ' 属性'" :width="600" :footer="false">
      <div class="pm-props">
        <common-info-row label="名称" :value="props.row?.name || ''" />
        <common-info-row label="完整路径" :value="props.info?.exe || ''"
          action-text="复制" @action="copy(props.info?.exe)" />
        <common-info-row label="命令行" :value="props.info?.cmdline || ''" :grey="!props.info?.cmdline"
          action-text="复制" @action="copy(props.info?.cmdline)" />
        <common-info-row label="进程 ID" :value="props.row ? String(props.row.pid) : ''" />
        <common-info-row label="父进程"
          :value="(props.info?.parentName || '-') + ' (' + (props.row?.ppid ?? '-') + ')'" />
        <common-info-row label="用户" :value="props.info?.user || ''" />
        <common-info-row label="位数" :value="props.info?.bitness || ''" />
        <common-info-row label="线程数" :value="props.row ? String(props.row.threads) : ''" />
        <common-info-row label="描述" :value="props.info?.description || ''" />
        <common-info-row label="公司" :value="props.info?.company || ''" />
        <common-info-row label="启动时间" :value="fmtTime(props.row?.startedAt)" />
        <common-info-row label="资源占用" tip="CPU / 私有字节 / 工作集" grey
          :value="fmtCpu(props.row?.cpuPct) + ' / ' + fmtBytes(props.row?.privateBytes) + ' / ' + fmtBytes(props.row?.workingSet)" />
      </div>
      <div class="pm-props-ops">
        <a-button size="small" @click="doVtRow(props.row)">在线搜索</a-button>
        <a-button v-if="caps.nativeWindows" size="small" status="danger" @click="props.row && killRow(props.row, false)">
          结束进程
        </a-button>
      </div>
    </a-modal>
  </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { Message, Modal, type TableColumnData } from '@arco-design/web-vue';
import { IconRefresh, IconCaretRight, IconCaretUp, IconCaretDown, IconExpand, IconShrink } from '@arco-design/web-vue/es/icon';
import { useCaps } from '@/utils/caps';

interface ProcRow {
  pid: number; ppid: number; name: string; exe: string;
  cpuPct: number; privateBytes: number; workingSet: number;
  description: string; company: string; startedAt: number; threads: number;
}
interface ViewRow extends ProcRow { depth: number; hasChildren: boolean; _sys: boolean; }
interface Summary { cpuPct: number; memPct: number; commitPct: number; procCount: number; ownPid: number; }
interface ProcInfo extends ProcRow { cmdline: string; user: string; parentName: string; bitness: string; }

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const caps = useCaps();

const procs = ref<ProcRow[]>([]);
const sum = ref<Summary>({ cpuPct: 0, memPct: 0, commitPct: 0, procCount: 0, ownPid: 0 });
const ownPid = computed(() => sum.value.ownPid);
const icons = reactive<Record<number, string>>({});
const collapsed = ref<Set<number>>(new Set());
const keyword = ref('');
const intervalMs = ref(2000);
const loading = ref(false);
const err = ref('');

/**
 * dataIndex 在这页里兼两个身份：Arco 只给有 dataIndex 的列挂拖宽手柄，
 * 同时它也当排序键用（数值列直接取字段，文本列 localeCompare）。
 */
const columns: TableColumnData[] = [
  { title: '名称', dataIndex: 'name', slotName: 'name', width: 300, ellipsis: false, titleSlotName: 'colTitle' },
  { title: 'CPU', dataIndex: 'cpuPct', slotName: 'cpu', width: 80, align: 'right', titleSlotName: 'colTitle' },
  { title: '私有字节', dataIndex: 'privateBytes', slotName: 'priv', width: 110, align: 'right', titleSlotName: 'colTitle' },
  { title: '工作集', dataIndex: 'workingSet', slotName: 'ws', width: 110, align: 'right', titleSlotName: 'colTitle' },
  { title: 'PID', dataIndex: 'pid', width: 90, align: 'right', titleSlotName: 'colTitle' },
  { title: '描述', dataIndex: 'description', slotName: 'desc', width: 200, ellipsis: true, titleSlotName: 'colTitle' },
  { title: '公司', dataIndex: 'company', slotName: 'company', width: 160, ellipsis: true, titleSlotName: 'colTitle' },
];

/* ==================== 排序（只排同级，不拆树） ==================== */
type SortDir = 'asc' | 'desc';
const sortKey = ref('name');
const sortDir = ref<SortDir>('asc');
const NUMERIC_KEYS = new Set(['cpuPct', 'privateBytes', 'workingSet', 'pid']);

/**
 * 同级比较器：不能用 Arco 内置 sortable——它排的是已扫平的 flatRows，
 * 父亲与孩子会被拆到不相邻的位置，树形直接碎掉。改成在 childrenMap 里排每个兄弟组。
 * 同值一律按 pid 兜底（不随方向翻转），否则每轮轮询都会看到行在跳位。
 */
function cmpSiblings(a: ProcRow, b: ProcRow): number {
  const key = sortKey.value as keyof ProcRow;
  let r: number;
  if (NUMERIC_KEYS.has(sortKey.value)) {
    r = Number(a[key]) - Number(b[key]);
  } else {
    r = String(a[key] ?? '').localeCompare(String(b[key] ?? ''));
  }
  return r * (sortDir.value === 'asc' ? 1 : -1) || a.pid - b.pid;
}

function toggleSort(key: string) {
  if (sortKey.value === key) {
    sortDir.value = sortDir.value === 'asc' ? 'desc' : 'asc';
    return;
  }
  sortKey.value = key;
  // 数值列第一次点先看大的在上面（找吃 CPU/内存的进程），文本列从 A→Z
  sortDir.value = NUMERIC_KEYS.has(key) ? 'desc' : 'asc';
}

/* ==================== 组树 + 扁平化 ==================== */
/**
 * 虚拟根：父不存在的孤儿、以及自父节点（Windows 的 System Idle Process 是 pid 0/ppid 0）
 * 都挂到这个键上。绝不能直接用 0 当根桶：那样 pid 0 既在根列表里、又是自己要找的孩子，
 * 从根桶起走递归会无限套娃直接爆栈（表现为表格空）。
 */
const VROOT = Number.MIN_SAFE_INTEGER;

// 子表：ppid -> 子进程[]（挂不到真实父上的进虚拟根）
const childrenMap = computed(() => {
  const present = new Set(procs.value.map((p) => p.pid));
  const m = new Map<number, ProcRow[]>();
  for (const p of procs.value) {
    const parent = present.has(p.ppid) && p.ppid !== p.pid ? p.ppid : VROOT;
    if (!m.has(parent)) m.set(parent, []);
    m.get(parent)!.push(p);
  }
  for (const arr of m.values()) arr.sort(cmpSiblings); // 每个兄弟组各自排，父子相邻关系不变
  return m;
});

// 命中集合：关键词命中的进程 + 其全部祖先（保证树路径可见）
const visibleSet = computed<Set<number> | null>(() => {
  const kw = keyword.value.trim().toLowerCase();
  if (!kw) return null;
  const keep = new Set<number>();
  const byPid = new Map(procs.value.map((p) => [p.pid, p]));
  for (const p of procs.value) {
    const hit = p.name.toLowerCase().includes(kw) || String(p.pid) === kw;
    if (!hit) continue;
    let cur: ProcRow | undefined = p;
    const guard = new Set<number>();
    while (cur && !guard.has(cur.pid)) {
      keep.add(cur.pid);
      guard.add(cur.pid);
      cur = byPid.get(cur.ppid);
    }
  }
  return keep;
});

function isSys(p: ProcRow) {
  return p.ppid === 4 || p.ppid === 0 || /svchost|system|csrss|wininit|services/i.test(p.name);
}

const flatRows = computed<ViewRow[]>(() => {
  const keep = visibleSet.value;
  const out: ViewRow[] = [];
  const done = new Set<number>(); // 数据意外成环（父指回祖先）时只输出一份，不递归爆栈
  const walk = (pid: number, node: ProcRow, depth: number) => {
    if (keep && !keep.has(pid)) return;
    if (done.has(pid)) return;
    done.add(pid);
    const kids = childrenMap.value.get(pid) || [];
    const visibleKids = keep ? kids.filter((k) => keep.has(k.pid)) : kids;
    out.push({ ...node, depth, hasChildren: kids.length > 0, _sys: isSys(node) });
    const isCollapsed = keep ? false : collapsed.value.has(pid); // 过滤时强制展开
    if (!isCollapsed) for (const k of visibleKids) walk(k.pid, k, depth + 1);
  };
  for (const root of childrenMap.value.get(VROOT) || []) walk(root.pid, root, 0);
  // 兜底：父链成环时从虚拟根走不到入口，也要补到表里，不能整个进程凭空消失
  for (const p of procs.value) walk(p.pid, p, 0);
  return out;
});

/* ==================== 快照轮询 ==================== */
let timer: number | null = null;
/**
 * 拉一次快照。silent = 后台轮询：不亮刷新按钮的 loading，否则每 2 秒闪一次。
 * 失败只抬出错误横幅并保留旧表，不把列表抹空（用户可能正右键在某行上）。
 */
async function refresh(silent = false) {
  if (typeof GoTools?.ProcSnapshot !== 'function') return;
  if (!silent) loading.value = true;
  try {
    const snap: any = await GoTools.ProcSnapshot();
    procs.value = snap?.procs || [];
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
// 展开 / 折叠 / 过滤后新冒出来的行也要补图标
watch(flatRows, () => { void loadIcons(); });

// 图标 best-effort：只取当前可见行、限并发（串行 await 会被一轮几百个往返拖死）
const ICON_CONC = 6;
let iconBusy = false;
async function loadIcons() {
  if (iconBusy || !caps.nativeWindows || typeof GoTools?.ProcIcon !== 'function') return;
  const pend = flatRows.value.filter((r) => icons[r.pid] === undefined).map((r) => r.pid);
  if (!pend.length) return;
  iconBusy = true;
  try {
    let i = 0;
    await Promise.all(Array.from({ length: Math.min(ICON_CONC, pend.length) }, async () => {
      while (i < pend.length) {
        const pid = pend[i++];
        if (icons[pid] !== undefined) continue;
        try { icons[pid] = (await GoTools.ProcIcon(pid)) || ''; }
        catch { icons[pid] = ''; }
      }
    }));
  } finally { iconBusy = false; }
}

/* ==================== 展开/折叠 ==================== */
function toggle(pid: number) {
  const s = new Set(collapsed.value);
  s.has(pid) ? s.delete(pid) : s.add(pid);
  collapsed.value = s;
}
function expandAll() { collapsed.value = new Set(); }
function collapseAll() {
  const s = new Set<number>();
  for (const [ppid, kids] of childrenMap.value) if (ppid !== VROOT && kids.length) s.add(ppid);
  collapsed.value = s;
}

/* ==================== 右键菜单 ==================== */
const menu = reactive<{ visible: boolean; x: number; y: number; row: ViewRow | null }>({
  visible: false, x: 0, y: 0, row: null,
});
const menuEl = ref<HTMLElement | null>(null);
function onContext(record: any, ev: any) {
  ev.preventDefault();
  // 非 Windows（含 web 模式）没有操作能力：右键退化为看属性，不弹一个全是死链的菜单
  if (!caps.nativeWindows) { openProps(record); return; }
  menu.row = record;
  selected.value = record.pid;
  void showMenu(ev.clientX, ev.clientY);
}
/** 先按点击位置显示，再量真实尺寸夹回视口：菜单高度不固定（优先级是飞出子菜单） */
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
  if (!(ev.target as HTMLElement).closest('.pm-table')) closeMenu();
}

/* ==================== 选中行 + 键盘快捷键 ==================== */
const selected = ref<number | null>(null);
const activeRow = computed<ViewRow | null>(() => flatRows.value.find((r) => r.pid === selected.value) || null);
function onRowClick(record: any) { selected.value = record?.pid ?? null; }
/** Del = 结束进程、Shift+Del = 结束进程树（菜单上标的就是这两个键，得真能用） */
function onKeydown(ev: KeyboardEvent) {
  if (ev.key === 'Escape') { closeMenu(); return; }
  if (ev.key !== 'Delete' && ev.key !== 'Backspace') return;
  if (props.visible) return; // 属性弹窗开着时不抢键
  const t = ev.target as HTMLElement | null;
  if (t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.isContentEditable)) return;
  const row = menu.row || activeRow.value;
  if (!row) return;
  ev.preventDefault();
  closeMenu();
  killRow(row, ev.shiftKey);
}

async function run(fn: () => Promise<any>, okText: string) {
  try { await fn(); Message.success({ content: okText, position: 'top' }); setTimeout(poll, 400); }
  catch (e: any) { Message.error({ content: String(e?.message || e), position: 'top', duration: 5000 }); }
}
/** 结束进程：tree=true 连整棵子树，先二次确认（可能掉未保存的数据） */
function killRow(row: ViewRow, tree: boolean) {
  const go = () => run(() => GoTools.ProcKill(row.pid, tree), tree ? '已结束进程树' : '已结束进程');
  if (tree) {
    Modal.warning({
      title: '结束进程树', hideCancel: false,
      content: `将结束「${row.name}」(PID ${row.pid}) 及其全部子进程，可能丢失未保存数据。确定继续？`,
      onOk: go,
    });
  } else go();
}
function doKill(tree: boolean) {
  const row = menu.row; if (!row) return;
  closeMenu();
  killRow(row, tree);
}
function doSuspend(on: boolean) {
  const row = menu.row; if (!row) return;
  closeMenu();
  run(() => GoTools.ProcSuspend(row.pid, on), on ? '已挂起' : '已恢复');
}
function doRestart() {
  const row = menu.row; if (!row) return;
  closeMenu();
  Modal.warning({
    title: '重启进程', hideCancel: false,
    content: `将先结束再重新启动「${row.name}」(PID ${row.pid})。确定继续？`,
    onOk: () => run(() => GoTools.ProcRestart(row.pid), '已重启'),
  });
}
function doPriority(cls: string) {
  const row = menu.row; if (!row) return;
  closeMenu();
  const label = cls === 'high' ? '高' : cls === 'low' ? '低' : '正常';
  run(() => GoTools.ProcSetPriority(row.pid, cls), '优先级已设为「' + label + '」');
}
function openPath() {
  const row = menu.row; if (!row) return;
  closeMenu();
  run(() => GoTools.ProcOpenPath(row.pid), '已在资源管理器中定位');
}
function doVt() { const row = menu.row; closeMenu(); if (row) doVtRow(row); }
function doVtRow(row: any) {
  if (!row) return;
  run(() => GoTools.ProcOpenVt(row.pid), '已在浏览器打开 VirusTotal');
}

/* ==================== 属性弹窗 ==================== */
const props = reactive<{ visible: boolean; row: ViewRow | null; info: ProcInfo | null }>({
  visible: false, row: null, info: null,
});
async function openProps(record: ViewRow) {
  closeMenu();
  props.row = record; props.info = null; props.visible = true;
  try { props.info = await GoTools.ProcInfo(record.pid); }
  catch (e: any) { Message.error('读取属性失败: ' + (e?.message || e)); }
}
function copy(text?: string) {
  if (!text) return;
  navigator.clipboard?.writeText(text).then(() => Message.success('已复制')).catch(() => { });
}

/* ==================== 格式化 ==================== */
function fmtBytes(n?: number): string {
  if (!n) return '0 K';
  const k = n / 1024;
  if (k < 1024) return round1(k) + ' K';
  const m = k / 1024;
  if (m < 1024) return round1(m) + ' M';
  return round1(m / 1024) + ' G';
}
function fmtCpu(n?: number): string { return (n || 0) < 0.01 ? '< 0.01' : round1(n || 0) + '%'; }
function fmtPct(n?: number): string { return round1(n || 0) + '%'; }
function round1(n: number): string { return (Math.round(n * 10) / 10).toFixed(1); }
function fmtTime(ms?: number): string { return ms ? new Date(ms).toLocaleString() : '—'; }
function cpuClass(n: number): string { return n >= 50 ? 'cpu-hot' : n >= 10 ? 'cpu-warm' : ''; }
/** 内存/提交负荷这类百分比配色：接近饱和标红，偏高标橙 */
function loadClass(n: number): string { return n >= 90 ? 'cpu-hot' : n >= 75 ? 'cpu-warm' : ''; }
function rowClass(record: ViewRow): string {
  const cls: string[] = [];
  if (record.pid === ownPid.value) cls.push('row-self');
  else if (record._sys) cls.push('row-sys');
  if (record.pid === selected.value) cls.push('row-active');
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
.pm-shell {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  box-sizing: border-box;
}

.pm-card {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.pm-card :deep(.arco-card-body) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* 标题栏注入区已自带间距与 gap，这里只定字号与颜色 */
.pm-sub {
  color: var(--color-text-2);
  font-size: var(--base-size-small);
  white-space: nowrap;
}

.pm-tip {
  flex: 1;
  min-width: 0;
  color: var(--color-text-3);
  font-size: var(--base-size-small);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pm-search {
  width: 180px;
}

.pm-err {
  margin-bottom: 8px;
  flex: none;
}

.pm-table {
  flex: 1;
  min-height: 0;
}

/* 表头排序：整块标题可点，caret 槽位常占宽，激活时只改颜色不推标题 */
.pm-th {
  display: inline-flex;
  align-items: center;
  gap: 2px;
  cursor: pointer;
  user-select: none;
}

.pm-th:hover {
  color: var(--color-text-1);
}

.pm-th.is-active {
  color: rgb(var(--primary-6));
}

.pm-th-caret {
  display: inline-flex;
  width: 12px;
  justify-content: center;
  font-size: var(--base-size-small);
}

/* 拖宽手柄 Arco 只给了 8px 隐形条，hover 时补一条可见竖线，否则没人知道能拖 */
.pm-table :deep(.arco-table-column-handle::after) {
  content: '';
  position: absolute;
  top: 6px;
  bottom: 6px;
  right: 3px;
  width: 2px;
  border-radius: var(--base-radius-small);
  background: transparent;
}

.pm-table :deep(.arco-table-column-handle:hover::after) {
  background: rgb(var(--primary-3));
}

.pm-name {
  display: flex;
  align-items: center;
  gap: 5px;
  white-space: nowrap;
}

.pm-caret {
  width: 14px;
  display: inline-flex;
  justify-content: center;
  cursor: pointer;
  color: var(--color-text-3);
}

.pm-caret.leaf {
  cursor: default;
  visibility: hidden;
}

.pm-caret :deep(.arco-icon) {
  transition: transform 0.15s;
  font-size: 12px;
}

.pm-caret :deep(.open) {
  transform: rotate(90deg);
}

.pm-ic {
  width: 16px;
  height: 16px;
  flex: none;
  object-fit: contain;
}

.pm-ic-ph {
  display: inline-block;
}

.pm-pname {
  overflow: hidden;
  text-overflow: ellipsis;
}

.pm-pname.is-sys {
  color: var(--color-text-2);
}

.pm-pname.is-self {
  font-weight: 600;
  color: rgb(var(--primary-6));
}

.pm-num {
  font-variant-numeric: tabular-nums;
}

.pm-desc {
  color: var(--color-text-2);
}

.pm-muted {
  color: var(--color-text-3);
}

.cpu-hot {
  color: rgb(var(--red-6));
  font-weight: 600;
}

.cpu-warm {
  color: rgb(var(--orange-6));
}

.pm-table :deep(.row-sys) {
  background: var(--color-fill-1);
}

.pm-table :deep(.row-self) {
  background: var(--color-primary-light-1);
}

/* 选中行按公共选中态规范：首列左侧短竖条，不加深边框也不改背景 */
.pm-table :deep(.row-active > td:first-child) {
  box-shadow: inset 3px 0 0 rgb(var(--primary-6));
}

/* 卡片头左侧概览条：等宽数字防轮询时数值抖宽，项间用细分隔线 */
.pm-status {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 0 var(--base-padding);
  color: var(--color-text-3);
  font-size: var(--base-size-small);
  font-variant-numeric: tabular-nums;
}

.pm-status span+span {
  padding-left: var(--base-padding);
  border-left: 1px solid var(--color-border-2);
}

.pm-status b {
  color: var(--color-text-1);
  font-weight: 500;
  margin-left: 2px;
}

/* 右键菜单 */
.pm-menu {
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

.pm-menu-head {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 6px 14px;
  border-bottom: 1px solid var(--color-border-2);
  margin-bottom: 4px;
}

.pm-menu-title {
  font-weight: 600;
  font-size: 13px;
  max-width: 160px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.pm-menu-pid {
  color: var(--color-text-3);
  font-size: 12px;
}

.pm-menu-item {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  padding: 6px 14px;
  font-size: 13px;
  color: var(--color-text-1);
  cursor: pointer;
}

.pm-menu-item:hover {
  background: var(--color-fill-2);
}

.pm-menu-item.danger {
  color: rgb(var(--red-6));
}

.pm-menu-item.off {
  color: var(--color-text-4);
  pointer-events: none;
}

.pm-menu-item kbd {
  font-size: 11px;
  color: var(--color-text-3);
}

.pm-menu-sep {
  height: 1px;
  background: var(--color-border-2);
  margin: 4px 0;
}

.pm-menu-sub {
  position: relative;
  padding: 6px 14px;
  font-size: 13px;
  cursor: pointer;
  display: flex;
  justify-content: space-between;
}

.pm-menu-sub:hover {
  background: var(--color-fill-2);
}

.pm-menu-sub::after {
  content: '›';
  color: var(--color-text-3);
}

.pm-submenu {
  display: none;
  position: absolute;
  left: 100%;
  top: -4px;
  min-width: 96px;
  background: var(--color-bg-popup);
  border: 1px solid var(--color-border-2);
  border-radius: var(--base-radius-default);
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.16);
  padding: 4px 0;
}

.pm-menu-sub:hover .pm-submenu {
  display: block;
}

/* 属性弹窗：字段行复用 common-info-row，只在这里去掉末行分隔线并限制高度可滚 */
.pm-props {
  max-height: 58vh;
  overflow: auto;
}

.pm-props :deep(.info-row:last-child) {
  border-bottom: none;
}

.pm-props-ops {
  display: flex;
  gap: var(--base-margin);
  margin-top: var(--base-margin);
}
</style>
