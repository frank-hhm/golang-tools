<template>
  <a-card class="hist-card card-head-tool" size="small" :body-style="{ padding: 0 }">
    <template #title>
      <a-space :size="8">
        <span>提交历史</span>
        <a-tag v-if="followPath" color="purple" size="small">文件历史</a-tag>
        <a-typography-text type="secondary" class="hist-count">
          {{ loading ? '读取中…' : `${list.length} 笔` }}
        </a-typography-text>
      </a-space>
    </template>
    <template #extra>
      <a-space :size="6">
        <a-button v-if="followPath" size="mini" @click="exitFollow">返回全历史</a-button>
        <a-button size="mini" :loading="loading" @click="load">刷新</a-button>
      </a-space>
    </template>

    <div v-if="!repo" class="hist-state">未选择仓库</div>
    <div v-else-if="err" class="hist-state">
      <span class="hist-err">{{ err }}</span>
      <a-button size="mini" @click="load">重试</a-button>
    </div>
    <div v-else-if="loading && !list.length" class="hist-state">读取提交历史…</div>
    <div v-else-if="!list.length" class="hist-state">该仓库还没有提交</div>

    <template v-else>
      <!-- 虚拟滚动列表：固定行高，只渲染视口内若干行；canvas 覆盖在最左图区自绘 DAG -->
      <div ref="bodyEl" class="hist-body">
        <canvas ref="canvasEl" class="hist-canvas"></canvas>
        <div ref="scrollEl" class="hist-scroll" @scroll="onScroll">
          <div class="hist-inner" :style="{ height: totalH + 'px' }">
            <div v-for="row in visibleRows" :key="row.c.hash" class="hist-row"
              :class="{ sel: row.c.hash === selected?.hash }" :style="{ top: row.i * ROW_H + 'px', paddingLeft: graphW + 'px' }"
              @click="select(row.c)">
              <span v-if="row.badge" class="hist-ref head">{{ row.badge }}</span>
              <span class="hist-subject" :title="row.c.subject">{{ row.c.subject || '（无说明）' }}</span>
              <span class="hist-meta">
                <span v-for="(r, i) in row.refs" :key="i" class="hist-ref" :class="r.kind">{{ r.text }}</span>
                <span class="hist-hash">{{ row.c.short }}</span>
                <span class="hist-who">{{ row.c.author }}</span>
                <span class="hist-when">{{ rel(row.c.time) }}</span>
              </span>
            </div>
          </div>
        </div>
      </div>

      <!-- 选中提交的改动文件清单（双击文件进 diff；「历史」按钮切 --follow 单文件历史） -->
      <div v-if="selected" class="hist-detail">
        <div class="hd-head">
          <span class="hd-title">{{ subjOf(selected) }}</span>
          <a-typography-text type="secondary" class="hd-hash">{{ selected.short }}</a-typography-text>
          <a-dropdown trigger="click" size="small" @select="(v: any) => onOpSelect(String(v))">
            <a-button size="mini" :loading="opBusy"><icon-more /></a-button>
            <template #content>
              <a-doption value="checkout"><icon-export /> 检出此提交</a-doption>
              <a-doption value="branch"><icon-branch /> 在此建分支</a-doption>
              <a-doption value="cherry"><icon-copy /> Cherry-pick</a-doption>
              <a-doption value="revert"><icon-undo /> Revert（反向提交）</a-doption>
              <a-doption value="reset-soft"><icon-refresh /> Reset soft</a-doption>
              <a-doption value="reset-mixed"><icon-refresh /> Reset mixed</a-doption>
              <a-doption value="reset-hard" class="op-danger"><icon-exclamation-circle-fill /> Reset hard（丢弃改动）</a-doption>
            </template>
          </a-dropdown>
        </div>
        <div v-if="filesLoading" class="hist-fstate">读取该笔改动…</div>
        <div v-else-if="filesErr" class="hist-fstate">{{ filesErr }}</div>
        <div v-else-if="!files.length" class="hist-fstate">没有文件改动（合并提交或空提交）</div>
        <div v-else class="hd-files">
          <div v-for="f in files" :key="f.path" class="hist-file" @dblclick="emit('diff', { commit: selected.hash, path: f.path })">
            <span class="hf-dot" :class="fstate(f)"></span>
            <span class="hist-fpath" :title="f.path">{{ f.path }}</span>
            <span v-if="f.binary" class="hist-fbin">二进制</span>
            <template v-else>
              <span class="hf-bar" :title="`+${f.added} / -${f.deleted}`">
                <i class="hf-add" :style="{ width: addPct(f) + '%' }"></i>
                <i class="hf-del" :style="{ width: 100 - addPct(f) + '%' }"></i>
              </span>
              <span class="hist-fnum"><i class="add">+{{ f.added }}</i><i class="del">-{{ f.deleted }}</i></span>
            </template>
            <span class="hd-fhis">
              <a-link class="hf-link" @click.stop="openBlame(f.path)">追溯</a-link>
              <a-link class="hf-link" @click.stop="followFile(f.path)">历史</a-link>
            </span>
          </div>
        </div>
      </div>
    </template>

    <a-modal v-model:visible="branchVisible" title="在此提交建分支" :ok-loading="opBusy" @ok="confirmBranch">
      <a-input v-model="branchName" placeholder="新分支名" @press-enter="confirmBranch" />
      <a-typography-text type="secondary" class="hd-tip">
        将从 {{ selected?.short }} 建分支并立即切过去。
      </a-typography-text>
    </a-modal>
  </a-card>
</template>

<script setup lang="ts">
/**
 * 提交历史面板（计划 P2）：canvas 自绘 DAG + 虚拟滚动 + --follow 单文件历史。
 *
 * lane 由引擎 tools/gitcmd/log.go 单遍布局算好（前端只画不算）：每个提交带 lane 列号与
 * parents 哈希，这里据此在固定行高的虚拟列表左图上，为可见行画节点圆点与到父节点的连线。
 * 虚拟滚动只渲染视口内行，200 笔也只画窗口里的连线（引擎默认取 200 笔）。
 *
 * 如实说明：canvas DAG 的连线走向/对齐需要人工在 GUI 里瞄一眼（本机无图形回归环境），
 * vue-tsc + vite build 只保证类型与打包正确。点击节点看该笔文件清单，双击文件进 DiffPane。
 */
import { computed, getCurrentInstance, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { Message, Modal } from '@arco-design/web-vue';
import type { Commit, FileDiff } from 'bindings/Tools/tools/gitcmd/index';

const props = defineProps<{ repo: string }>();
const emit = defineEmits<{
  (e: 'diff', opts: { commit: string; path: string }): void;
  (e: 'changed'): void;
  (e: 'blame', path: string): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const list = ref<Commit[]>([]);
const loading = ref(false);
const err = ref('');
const selected = ref<Commit | null>(null);
const files = ref<FileDiff[]>([]);
const filesLoading = ref(false);
const filesErr = ref('');
const followPath = ref('');

const bodyEl = ref<HTMLElement>();
const scrollEl = ref<HTMLElement>();
const canvasEl = ref<HTMLCanvasElement>();

/* ---------- 布局常量 ---------- */
const ROW_H = 30; // 固定行高，虚拟滚动的基石
const LANE_STEP = 14; // 每条 lane 的水平间距
const GRAPH_PAD = 10; // 图区左边距
const BUFFER = 8; // 视口上下各多渲染的行数

/* ---------- lane 配色（取 Arco 变量，不写死十六进制） ---------- */
const LANE_VARS = ['--arcoblue-6', '--green-6', '--orangered-6', '--purple-6', '--cyan-6', '--pinkpurple-6', '--gold-6', '--lime-6'];
function laneColor(lane: number): string {
  return `rgb(var(${LANE_VARS[Math.abs(lane || 0) % LANE_VARS.length]}))`;
}
function laneX(lane: number): number {
  return GRAPH_PAD + Math.abs(lane || 0) * LANE_STEP + LANE_STEP / 2;
}

const maxLane = computed(() => {
  let m = 0;
  for (const c of list.value) if ((c.lane || 0) > m) m = c.lane;
  return m;
});
const graphW = computed(() => GRAPH_PAD + (maxLane.value + 1) * LANE_STEP + 6);
const totalH = computed(() => list.value.length * ROW_H);

/* ---------- 虚拟窗口 ---------- */
const scrollTop = ref(0);
const bodyH = ref(600);

type Row = { c: Commit; i: number; refs: { text: string; kind: string }[]; badge: string };
const visibleRows = computed<Row[]>(() => {
  const arr = list.value;
  const start = Math.max(0, Math.floor(scrollTop.value / ROW_H) - BUFFER);
  const end = Math.min(arr.length, Math.ceil((scrollTop.value + bodyH.value) / ROW_H) + BUFFER);
  const rows: Row[] = [];
  for (let i = start; i < end; i++) {
    const c = arr[i];
    const refs = refsOf(c.refs);
    const head = refs.find((r) => r.kind === 'head');
    rows.push({ c, i, refs: refs.filter((r) => r.kind !== 'head'), badge: head ? head.text : '' });
  }
  return rows;
});

/** decorate 串切成标签；HEAD 单独提出当徽标 */
function refsOf(s: string): { text: string; kind: string }[] {
  if (!s) return [];
  return s
    .split(',')
    .map((t) => t.trim())
    .filter(Boolean)
    .map((t) => {
      if (t.startsWith('tag:')) return { text: t.slice(4).trim(), kind: 'tag' };
      if (t.startsWith('HEAD')) return { text: t.replace(/\s*->\s*/, ' → '), kind: 'head' };
      if (t.includes('/')) return { text: t, kind: 'remote' };
      return { text: t, kind: 'local' };
    });
}

function subjOf(c: Commit | null): string {
  return c?.subject || '（无说明）';
}

/** 由 numstat 的增删数推一个文件态色点（无 name-status 只能靠启发）：
 *  纯新增→绿、纯删除→红、又加又删→橙（最常见的是修改）、二进制→灰 */
function fstate(f: FileDiff): string {
  if (f.binary) return 'bin';
  if (f.added > 0 && !f.deleted) return 'add';
  if (f.deleted > 0 && !f.added) return 'del';
  if (f.added > 0 && f.deleted > 0) return 'mod';
  return 'none';
}
/** 差异条里绿色（新增）占比，供按增删比例铺条；全 0 时退中点 */
function addPct(f: FileDiff): number {
  const t = (f.added || 0) + (f.deleted || 0);
  if (!t) return 50;
  return Math.round(((f.added || 0) / t) * 100);
}

function rel(ts: number): string {
  if (!ts) return '';
  const d = Math.floor(Date.now() / 1000) - ts;
  if (d < 60) return '刚刚';
  if (d < 3600) return `${Math.floor(d / 60)} 分钟前`;
  if (d < 86400) return `${Math.floor(d / 3600)} 小时前`;
  if (d < 86400 * 30) return `${Math.floor(d / 86400)} 天前`;
  const dt = new Date(ts * 1000);
  const p = (n: number) => String(n).padStart(2, '0');
  return `${dt.getFullYear()}-${p(dt.getMonth() + 1)}-${p(dt.getDate())}`;
}

/* ---------- canvas DAG 自绘 ---------- */
// hash -> {行号, lane}，用于把每条父边连到父节点所在行
function buildIndex() {
  const m = new Map<string, { i: number; lane: number }>();
  list.value.forEach((c, i) => m.set(c.hash, { i, lane: c.lane || 0 }));
  return m;
}

function draw() {
  const canvas = canvasEl.value;
  const ctx = canvas?.getContext('2d');
  if (!canvas || !ctx) return;
  const dpr = window.devicePixelRatio || 1;
  const w = graphW.value;
  const h = bodyH.value;
  if (canvas.width !== Math.round(w * dpr) || canvas.height !== Math.round(h * dpr)) {
    canvas.width = Math.round(w * dpr);
    canvas.height = Math.round(h * dpr);
    canvas.style.width = w + 'px';
    canvas.style.height = h + 'px';
  }
  ctx.setTransform(dpr, 0, 0, dpr, 0, 0);
  ctx.clearRect(0, 0, w, h);

  const idx = buildIndex();
  const arr = list.value;
  const start = Math.max(0, Math.floor(scrollTop.value / ROW_H) - 1);
  const end = Math.min(arr.length, Math.ceil((scrollTop.value + h) / ROW_H) + 1);

  // 1) 先画边（在节点下方）
  ctx.lineWidth = 1.4;
  for (let i = start; i < end; i++) {
    const c = arr[i];
    const y = i * ROW_H - scrollTop.value + ROW_H / 2;
    const x = laneX(c.lane || 0);
    for (const p of c.parents || []) {
      const t = idx.get(p);
      if (!t) continue; // 父在窗口/结果集外（截断/已合并出窗口）——不画悬空边
      const ty = t.i * ROW_H - scrollTop.value + ROW_H / 2;
      const tx = laneX(t.lane);
      ctx.strokeStyle = laneColor(c.lane || 0);
      ctx.beginPath();
      ctx.moveTo(x, y);
      if (tx === x) ctx.lineTo(tx, ty);
      else ctx.quadraticCurveTo(tx, y, tx, ty); // 折向父列
      ctx.stroke();
    }
  }

  // 2) 再画节点（空心圆：选中或带 ref 用实心）
  for (let i = start; i < end; i++) {
    const c = arr[i];
    const y = i * ROW_H - scrollTop.value + ROW_H / 2;
    const x = laneX(c.lane || 0);
    const col = laneColor(c.lane || 0);
    const solid = c.hash === selected.value?.hash || (c.refs || '').length > 0;
    ctx.beginPath();
    ctx.arc(x, y, 4, 0, Math.PI * 2);
    if (solid) {
      ctx.fillStyle = col;
      ctx.fill();
    } else {
      ctx.strokeStyle = col;
      ctx.lineWidth = 1.6;
      ctx.stroke();
    }
  }
}

function onScroll() {
  if (scrollEl.value) scrollTop.value = scrollEl.value.scrollTop;
  requestAnimationFrame(draw);
}

let ro: ResizeObserver | null = null;
function measure() {
  if (bodyEl.value) {
    bodyH.value = bodyEl.value.clientHeight || 600;
    draw();
  }
}

/* ---------- 选中 / 加载 ---------- */
async function select(c: Commit) {
  if (selected.value?.hash === c.hash) {
    selected.value = null;
    files.value = [];
    return;
  }
  selected.value = c;
  files.value = [];
  filesErr.value = '';
  filesLoading.value = true;
  try {
    files.value = (await GoTools.GitDiffFile(props.repo, { staged: false, commit: c.hash, path: '' })) || [];
  } catch (e: any) {
    filesErr.value = String(e?.message || e || '读取该笔改动失败');
  } finally {
    filesLoading.value = false;
  }
  await nextTick();
  draw();
}

async function load() {
  selected.value = null;
  files.value = [];
  scrollTop.value = 0;
  if (scrollEl.value) scrollEl.value.scrollTop = 0;
  if (!props.repo) {
    list.value = [];
    return;
  }
  loading.value = true;
  err.value = '';
  try {
    // limit 传 0 让引擎取默认 200；followPath 非空 = 只看该文件历史（引擎附 --follow）
    list.value = (await GoTools.GitLog(props.repo, '', followPath.value, 0)) || [];
  } catch (e: any) {
    list.value = [];
    err.value = String(e?.message || e || '读取提交历史失败');
  } finally {
    loading.value = false;
    await nextTick();
    measure();
  }
}

function followFile(p: string) {
  followPath.value = p;
  load();
}
function exitFollow() {
  followPath.value = '';
  load();
}

/* ---------- 历史节点上的变更操作（计划 P2，破坏性带二次确认） ---------- */
const opBusy = ref(false);
const branchVisible = ref(false);
const branchName = ref('');

function needSel(): boolean {
  if (!selected.value) {
    Message.warning('先选中一笔提交');
    return false;
  }
  return true;
}
async function runOp(fn: () => Promise<unknown>, okText: string) {
  opBusy.value = true;
  try {
    await fn();
    Message.success({ content: okText, position: 'top', duration: 2000 });
    emit('changed');
    await load();
  } catch (e: any) {
    Message.error({ content: String(e?.message || e || '操作失败'), position: 'top', duration: 5000 });
  } finally {
    opBusy.value = false;
  }
}

function onOpSelect(v: string) {
  if (!needSel()) return;
  const hash = selected.value!.hash;
  if (v === 'checkout') {
    Modal.confirm({
      title: '检出此提交',
      content: `将进入游离 HEAD（${selected.value!.short}），后续提交不属于任何分支。确认？`,
      okText: '检出',
      onOk: () => runOp(() => GoTools.GitCheckout(props.repo, hash), '已检出 ' + selected.value?.short),
    });
  } else if (v === 'branch') {
    branchName.value = '';
    branchVisible.value = true;
  } else if (v === 'cherry') {
    runOp(() => GoTools.GitCherryPick(props.repo, hash), '已 cherry-pick ' + selected.value?.short);
  } else if (v === 'revert') {
    runOp(() => GoTools.GitRevert(props.repo, hash), '已 revert ' + selected.value?.short);
  } else if (v === 'reset-soft' || v === 'reset-mixed' || v === 'reset-hard') {
    const mode = v.split('-')[1];
    doReset(mode, hash);
  }
}

async function doReset(mode: string, hash: string) {
  const short = selected.value?.short || '';
  if (mode !== 'hard') {
    const label = mode === 'soft' ? '软（保留暂存与工作区）' : '混合（撤销提交，保留工作区改动）';
    Modal.confirm({
      title: `Reset ${mode} → ${short}`,
      content: `当前分支将回退到 ${short}，模式：${label}。确认？`,
      okText: '执行',
      onOk: () => runOp(() => GoTools.GitReset(props.repo, mode, hash), `已 reset --${mode}`),
    });
    return;
  }
  // hard 会丢弃工作区改动：先查有多少未提交改动，二次确认里如实显示
  let dirty = 0;
  try {
    const s: any = await GoTools.GitStatus(props.repo);
    dirty = (s?.staged?.length || 0) + (s?.unstaged?.length || 0) + (s?.untracked?.length || 0) + (s?.conflicted?.length || 0);
  } catch {
    /* 查不到就不报数量，仍要求确认 */
  }
  Modal.confirm({
    title: `⚠ Reset --hard → ${short}`,
    content: `此操作将丢弃工作区与暂存区的 ${dirty} 项改动，且不可撤销。分支将强制回退到 ${short}。确认执行？`,
    okText: '我已了解，执行 hard',
    okButtonProps: { status: 'danger' },
    onOk: () => runOp(() => GoTools.GitReset(props.repo, 'hard', hash), '已 reset --hard'),
  });
}

async function confirmBranch() {
  const name = branchName.value.trim();
  if (!name || !selected.value) return;
  await runOp(
    () => GoTools.GitCreateBranchAt(props.repo, name, selected.value!.hash, true),
    `已在 ${selected.value.short} 建分支 ${name}`
  );
  branchVisible.value = false;
}

function openBlame(p: string) {
  emit('blame', p);
}

watch(() => props.repo, () => {
  followPath.value = '';
  load();
}, { immediate: true });

onMounted(() => {
  measure();
  if (bodyEl.value && typeof ResizeObserver !== 'undefined') {
    ro = new ResizeObserver(measure);
    ro.observe(bodyEl.value);
  }
  requestAnimationFrame(draw);
});
onBeforeUnmount(() => {
  if (ro) { ro.disconnect(); ro = null; }
});

defineExpose({ load });
</script>

<style scoped>
.hist-card {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  background: var(--color-bg-1);
  border-radius: var(--base-radius-default, 8px);
}
.hist-card :deep(.arco-card-header) {
  flex: none;
}
.hist-card :deep(.arco-card-body) {
  display: flex;
  flex: 1;
  flex-direction: column;
  min-height: 0;
  overflow: hidden;
}
.hist-count {
  font-size: 12px;
  font-weight: 400;
}
.hist-body {
  position: relative;
  flex: 1;
  min-height: 0;
  overflow: hidden;
}
.hist-canvas {
  position: absolute;
  top: 0;
  left: 0;
  z-index: 2;
  pointer-events: none;
}
.hist-scroll {
  position: absolute;
  inset: 0;
  overflow: auto;
}
.hist-inner {
  position: relative;
  width: 100%;
}
.hist-row {
  position: absolute;
  left: 0;
  right: 0;
  height: 30px;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  gap: 8px;
  padding-right: 8px;
  border-radius: var(--base-radius-small);
  cursor: pointer;
  z-index: 1;
}
.hist-row:hover {
  background: var(--color-fill-2);
}
/* 选中行：左缘短竖条 + 轻底 + 标题点亮主色（与仓库列表/实例条同一套选中态口径） */
.hist-row.sel {
  background: var(--color-fill-2);
}
.hist-row.sel::before {
  content: "";
  position: absolute;
  top: 50%;
  left: 0;
  width: 3px;
  height: 16px;
  margin-top: -8px;
  border-radius: 0 var(--base-radius-small) var(--base-radius-small) 0;
  background: var(--app-select-rail);
}
.hist-row.sel .hist-subject {
  color: rgb(var(--primary-6));
  font-weight: 600;
}
.hist-subject {
  min-width: 0;
  overflow: hidden;
  color: var(--color-text-1);
  font-size: 12px;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 0 1 auto;
  max-width: 46%;
}
.hist-meta {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
  overflow: hidden;
  color: rgb(var(--gray-5));
  font-size: 11px;
  white-space: nowrap;
}
.hist-ref {
  flex: none;
  padding: 0 5px;
  border-radius: var(--base-radius-small);
  font-size: 11px;
}
.hist-ref.local {
  background: rgba(var(--arcoblue-6), 0.12);
  color: rgb(var(--arcoblue-6));
}
.hist-ref.remote {
  background: rgba(var(--green-6), 0.12);
  color: rgb(var(--green-6));
}
.hist-ref.tag {
  background: rgba(var(--purple-6), 0.12);
  color: rgb(var(--purple-6));
}
.hist-ref.head {
  background: rgba(var(--orangered-6), 0.14);
  color: rgb(var(--orangered-6));
}
.hist-hash {
  flex: none;
  font-family: var(--code-font-family);
}
.hist-detail {
  flex: none;
  max-height: 40%;
  display: flex;
  flex-direction: column;
  border-top: 1px solid var(--color-border-1);
  padding: 4px 8px 6px;
}
.hd-head {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 2px 0 4px;
}
.hd-title {
  color: var(--color-text-1);
  font-size: 12px;
  font-weight: 600;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.hd-hash {
  flex: none;
  font-family: var(--code-font-family);
  font-size: 11px;
}
.hd-tip {
  display: block;
  margin-top: 8px;
  font-size: 12px;
}
.op-danger {
  color: rgb(var(--red-6));
}
.hd-files {
  overflow: auto;
  min-height: 0;
}
.hist-file {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 3px 6px 3px 4px;
  border-radius: var(--base-radius-small);
  font-size: 12px;
  cursor: pointer;
}
.hist-file:hover {
  background: var(--color-fill-2);
}
/* 文件态色点：呼应变更区的状态色语言，一眼扫出这笔改了哪类文件 */
.hf-dot {
  flex: none;
  width: 7px;
  height: 7px;
  border-radius: var(--border-radius-full);
  background: rgb(var(--gray-4));
}
.hf-dot.add {
  background: rgb(var(--green-6));
}
.hf-dot.del {
  background: rgb(var(--red-6));
}
.hf-dot.mod {
  background: rgb(var(--orangered-6));
}
.hf-dot.bin,
.hf-dot.none {
  background: rgb(var(--gray-4));
}
.hist-fpath {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  color: var(--color-text-1);
  text-overflow: ellipsis;
  white-space: nowrap;
}
/* 增删比例条：定宽槽，绿段宽=新增占比、红段=删除占比，悬停看精确数 */
.hf-bar {
  flex: none;
  display: inline-flex;
  width: 52px;
  height: 6px;
  overflow: hidden;
  border-radius: var(--border-radius-full);
  background: var(--color-fill-2);
}
.hf-add,
.hf-del {
  display: block;
  height: 100%;
}
.hf-add {
  background: rgb(var(--green-6));
}
.hf-del {
  background: rgb(var(--red-6));
}
.hd-fhis {
  flex: none;
  display: inline-flex;
  gap: 6px;
  opacity: 0;
  transition: opacity 0.12s ease;
}
.hist-file:hover .hd-fhis {
  opacity: 1;
}
.hf-link {
  font-size: 11px;
}
.hist-fbin {
  flex: none;
  color: rgb(var(--gray-5));
  font-size: 11px;
}
.hist-fnum {
  flex: none;
  width: 62px;
  text-align: right;
  font-family: var(--code-font-family);
  font-size: 11px;
}
.hist-fnum .add {
  margin-right: 4px;
  color: rgb(var(--green-6));
  font-style: normal;
}
.hist-fnum .del {
  color: rgb(var(--red-6));
  font-style: normal;
}
.hist-state,
.hist-fstate {
  padding: 14px;
  color: rgb(var(--gray-5));
  font-size: 12px;
  text-align: center;
}
.hist-state {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
}
.hist-err {
  color: rgb(var(--red-6));
}
</style>
