<template>
  <!-- 压缩前的可选项小窗：无边框透明置顶，由后端 ArchivePanelOpen 点亮（「发送到」那条走它） -->
  <div class="ap-wrap" @click.self="dismiss">
    <div class="ap-card">
      <div class="ap-head">
        <span class="ap-title">压缩选项</span>
        <span class="ap-count">{{ paths.length }} 项</span>
        <span class="ap-close" title="关闭（Esc）" @click="dismiss">
          <icon-close />
        </span>
      </div>

      <div class="ap-files">
        <div v-for="p in paths" :key="p" class="ap-file">
          <span class="ap-file-name">{{ baseName(p) }}</span>
          <span class="ap-file-dir">{{ dirName(p) }}</span>
          <icon-close class="ap-file-del" @click="removePath(p)" />
        </div>
        <div v-if="!paths.length" class="ap-empty">没有收到要压缩的条目</div>
      </div>

      <div class="ap-row">
        <span class="ap-label">格式</span>
        <div class="ap-chips">
          <div class="ap-chip" :class="{ 'is-on': format === 'zip' }" @click="format = 'zip'">zip</div>
          <div class="ap-chip" :class="{ 'is-on': format === 'targz' }" @click="format = 'targz'">tar.gz</div>
        </div>
      </div>

      <div class="ap-row">
        <span class="ap-label">级别</span>
        <div class="ap-chips">
          <div v-for="l in LEVELS" :key="l.value" class="ap-chip" :class="{ 'is-on': level === l.value }"
            @click="level = l.value">{{ l.label }}</div>
        </div>
      </div>

      <div class="ap-row">
        <span class="ap-label">名称</span>
        <input v-model="name" class="ap-input" :placeholder="suggestBase || '自动命名'" spellcheck="false" />
      </div>

      <div class="ap-row">
        <span class="ap-label">输出</span>
        <input v-model="outDir" class="ap-input" placeholder="就地输出（条目所在目录）" spellcheck="false" />
        <span class="ap-browse" @click="chooseOutDir">浏览</span>
      </div>

      <div v-if="suggestPath" class="ap-suggest">成品：{{ suggestPath }}</div>

      <div class="ap-foot">
        <template v-if="!running">
          <span class="ap-meta">{{ sizeText }}</span>
          <div class="ap-btns">
            <button class="ap-btn" @click="dismiss">取消</button>
            <button class="ap-btn primary" :disabled="!paths.length" @click="start">开始压缩</button>
          </div>
        </template>
        <template v-else>
          <div class="ap-run">
            <div class="ap-bar"><i :style="{ width: pct + '%' }"></i></div>
            <span class="ap-run-text">{{ runText }}</span>
          </div>
          <div class="ap-btns">
            <button class="ap-btn" @click="cancel">取消任务</button>
          </div>
        </template>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
/**
 * 压缩前的可选项小窗。
 *
 * 窗口本身由 Go 侧建与点亮（services/app/launcher.go 里那套口径：透明背景 +
 * 关掉无边框装饰），这里只收条目、问选项、发任务。压缩逻辑一律不在前端重做，
 * 与主窗口工具页、右键那条路共用同一个 Go 引擎。
 *
 * 条目来源有两个：--archive-panel 带进来的（冷建时页面 mount 后主动取一次），
 * 以及窗口复用时 Go 发的 archive:show。
 */
import { computed, getCurrentInstance, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { Events } from '@wailsio/runtime';

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const LEVELS = [
  { value: 1, label: '快速' },
  { value: 6, label: '标准' },
  { value: 9, label: '最高' },
];

const paths = ref<string[]>([]);
const format = ref<'zip' | 'targz'>('zip');
const level = ref(6);
const name = ref('');
const outDir = ref('');

const suggestPath = ref('');
const suggestBase = ref('');
const preflight = ref({ files: 0, bytes: 0 });

const running = ref(false);
const jobId = ref('');
const pct = ref(0);
const runText = ref('');
const finished = ref('');

let offShow: (() => void) | undefined;
let offProgress: (() => void) | undefined;
let hintTimer: number | null = null;

const baseName = (p: string) => p.split(/[\\/]/).filter(Boolean).pop() || p;
const dirName = (p: string) => {
  const i = Math.max(p.lastIndexOf('\\'), p.lastIndexOf('/'));
  return i > 0 ? p.slice(0, i) : '';
};

function fmtBytes(n: number): string {
  if (!n) return '';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = n, i = 0;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${u[i]}`;
}

const sizeText = computed(() => {
  if (!preflight.value.files) return '';
  return `${preflight.value.files} 个文件 · ${fmtBytes(preflight.value.bytes)}`;
});

function options() {
  return {
    op: 'compress',
    paths: paths.value,
    format: format.value,
    level: level.value,
    outDir: outDir.value.trim(),
    name: name.value.trim(),
    dest: '',
  };
}

/** 条目或选项一变就重算提示：扫盘要读目录树，防一手连点 */
function refreshHints() {
  if (hintTimer !== null) window.clearTimeout(hintTimer);
  hintTimer = window.setTimeout(async () => {
    hintTimer = null;
    if (!paths.value.length) {
      suggestPath.value = '';
      suggestBase.value = '';
      preflight.value = { files: 0, bytes: 0 };
      return;
    }
    try {
      const pf = await GoTools?.ArchiveScan(paths.value);
      preflight.value = { files: pf?.files || 0, bytes: pf?.bytes || 0 };
    } catch { /* 扫不动就只显示名字，不拦用户干活 */ }
    try {
      const out = await GoTools?.ArchiveSuggestName(options());
      suggestPath.value = out || '';
      suggestBase.value = out ? baseName(out) : '';
    } catch {
      suggestPath.value = '';
      suggestBase.value = '';
    }
  }, 220);
}

function setPaths(list: string[]) {
  paths.value = list.filter(Boolean);
  finished.value = '';
  runText.value = '';
  pct.value = 0;
  refreshHints();
}

function removePath(p: string) {
  paths.value = paths.value.filter((x) => x !== p);
  refreshHints();
}

async function chooseOutDir() {
  try {
    const dir = await GoTools?.ChooseDir('选择压缩输出目录');
    if (dir) outDir.value = dir;
  } catch { /* 取消对话框是常态，不提示 */ }
}

async function start() {
  if (!paths.value.length) return;
  running.value = true;
  // 先清上一笔的号：留着它会把这一笔的进度整批筛掉（毫秒级任务的终态事件跑得比 await 返回值还早）
  jobId.value = '';
  pct.value = 0;
  runText.value = '开始…';
  finished.value = '';
  try {
    jobId.value = (await GoTools?.ArchiveCompress(options())) || '';
  } catch (e: any) {
    running.value = false;
    runText.value = String(e?.message || e || '发起失败');
  }
}

async function cancel() {
  try {
    // false = 这一笔其实已经结束了：本地收回，否则取消一个已结束的任务不会再有任何事件回来
    if (!(await GoTools?.ArchiveCancel(jobId.value))) {
      running.value = false;
      runText.value = '这一笔已经结束了';
    }
  } catch {
    running.value = false;
  }
}

function dismiss() {
  running.value = false;
  try { GoTools?.ArchivePanelHide(); } catch { /* Go 没就绪时收起失败不影响本地状态 */ }
}

function onProgress(raw: any) {
  const p = raw?.data ?? raw;
  if (!p) return;
  // 号还没定时先认下这一笔，之后只认同一个 jobId（与工具页同一套口径）
  if (!jobId.value && p.jobId) jobId.value = p.jobId;
  if (jobId.value && p.jobId && p.jobId !== jobId.value) return;
  if (p.percent) pct.value = Math.min(99, p.percent);
  if (p.msg) runText.value = p.msg;
  if (p.stage === 'done') {
    pct.value = 100;
    finished.value = p.out || '';
    runText.value = `${p.name || ''} 已完成`;
    // 让用户看清"成功了"再收起，别一完任务就把窗口抽走
    window.setTimeout(dismiss, 1600);
    return;
  }
  if (p.stage === 'error' || p.stage === 'canceled') {
    running.value = false;
    runText.value = p.msg || (p.stage === 'canceled' ? '已取消' : '压缩失败');
  }
}

async function loadPending() {
  try {
    const pending = await GoTools?.ArchivePanelPending();
    if (pending?.length) setPaths(pending as string[]);
  } catch { /* 非桌面构建里没有这个绑定 */ }
}

onMounted(() => {
  offShow = Events.On('archive:show', (raw: any) => {
    const p = raw?.data ?? raw;
    const list = Array.isArray(p) ? p : (p?.paths || []);
    setPaths(list as string[]);
    // 每次呼出都回到默认选项：这是"顺手压一下"的窗，上次选的最高级别不该跟着过来
    name.value = '';
    outDir.value = '';
  });
  offProgress = Events.On('archive:progress', onProgress);
  window.addEventListener('keydown', onKeydown);
  loadPending();
});

onBeforeUnmount(() => {
  offShow?.();
  offProgress?.();
  window.removeEventListener('keydown', onKeydown);
  if (hintTimer !== null) window.clearTimeout(hintTimer);
});

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') {
    e.preventDefault();
    dismiss();
    return;
  }
  if (e.key === 'Enter' && !running.value && paths.value.length) {
    e.preventDefault();
    start();
  }
}

// 格式与级别变更也要重算成品名（扩展名跟着格式走）
watch([format, outDir, name], refreshHints);
</script>

<style scoped>
/* 窗口本身完全透明（Go 侧已关掉无边框装饰，不留 DWM 玻璃底板）：
   四周留白给卡片投影腾地方，点击空白处即收起 */
.ap-wrap {
  width: 100vw;
  height: 100vh;
  box-sizing: border-box;
  padding: 12px 12px 22px;
  display: flex;
  justify-content: center;
  align-items: flex-start;
  background: transparent;
  font-family: "Microsoft YaHei", "Segoe UI", sans-serif;
}

.ap-card {
  width: 100%;
  max-height: 100%;
  display: flex;
  flex-direction: column;
  gap: 8px;
  padding: 12px 14px;
  box-sizing: border-box;
  background: var(--color-bg-1, #fff);
  border: 1px solid var(--color-border-2, #e5e6eb);
  border-radius: var(--base-radius-default);
  box-shadow: 0 6px 22px rgba(0, 0, 0, 0.22);
  overflow: hidden;
  user-select: none;
}

.ap-head {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: none;
}

.ap-title {
  font-size: var(--base-size-large);
  font-weight: 600;
  color: var(--color-text-1, #1d2129);
}

.ap-count {
  font-size: var(--base-size-small);
  color: var(--color-text-3, #86909c);
}

.ap-close {
  margin-left: auto;
  display: flex;
  align-items: center;
  color: var(--color-text-3, #86909c);
  cursor: pointer;
}

.ap-close:hover {
  color: var(--color-text-1, #1d2129);
}

.ap-files {
  flex: none;
  max-height: 122px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.ap-file {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: var(--base-size-small);
  line-height: 1.7;
  border-radius: var(--base-radius-default);
  padding: 1px 4px;
}

.ap-file:hover {
  box-shadow: 0 1px 6px rgba(0, 0, 0, 0.1);
}

.ap-file-name {
  flex: none;
  max-width: 40%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--color-text-1, #1d2129);
}

.ap-file-dir {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--color-text-3, #86909c);
}

.ap-file-del {
  flex: none;
  color: var(--color-text-4, #c9cdd4);
  cursor: pointer;
}

.ap-file-del:hover {
  color: rgb(var(--red-6, 245, 63, 63));
}

.ap-empty {
  font-size: var(--base-size-small);
  color: var(--color-text-4, #c9cdd4);
  padding: 6px 0;
}

.ap-row {
  display: flex;
  align-items: center;
  gap: 8px;
  flex: none;
}

.ap-label {
  flex: none;
  width: 34px;
  font-size: var(--base-size-small);
  color: var(--color-text-3, #86909c);
}

.ap-chips {
  display: flex;
  gap: 6px;
}

/* 选中态不铺灰底（整块灰压在透明窗上很脏）：描边点亮 + 一根左侧短竖条抬起来 */
.ap-chip {
  position: relative;
  padding: 2px 12px;
  font-size: var(--base-size-small);
  color: var(--color-text-2, #4e5969);
  border: 1px solid var(--color-border-2, #e5e6eb);
  border-radius: var(--base-radius-default);
  cursor: pointer;
}

.ap-chip.is-on {
  color: rgb(var(--primary-6, 22, 93, 255));
  border-color: rgb(var(--primary-6, 22, 93, 255));
  box-shadow: 0 1px 6px rgba(0, 0, 0, 0.12);
}

.ap-chip.is-on::before {
  content: "";
  position: absolute;
  left: 3px;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 12px;
  border-radius: 2px;
  background: rgb(var(--primary-6, 22, 93, 255));
}

.ap-input {
  flex: 1;
  min-width: 0;
  height: 26px;
  padding: 0 8px;
  border: 1px solid var(--color-border-2, #e5e6eb);
  border-radius: var(--base-radius-default);
  background: transparent;
  outline: none;
  font-size: var(--base-size-small);
  color: var(--color-text-1, #1d2129);
}

.ap-input::placeholder {
  color: var(--color-text-4, #c9cdd4);
}

.ap-browse {
  flex: none;
  font-size: var(--base-size-small);
  color: rgb(var(--primary-6, 22, 93, 255));
  cursor: pointer;
}

.ap-suggest {
  flex: none;
  font-size: var(--base-size-small);
  color: var(--color-text-3, #86909c);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ap-foot {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: none;
  padding-top: 2px;
}

.ap-meta {
  font-size: var(--base-size-small);
  color: var(--color-text-3, #86909c);
}

.ap-btns {
  margin-left: auto;
  display: flex;
  gap: 8px;
}

.ap-btn {
  padding: 3px 14px;
  font-size: var(--base-size-small);
  border: 1px solid var(--color-border-2, #e5e6eb);
  border-radius: var(--base-radius-default);
  background: transparent;
  color: var(--color-text-2, #4e5969);
  cursor: pointer;
}

.ap-btn.primary {
  border-color: rgb(var(--primary-6, 22, 93, 255));
  background: rgb(var(--primary-6, 22, 93, 255));
  color: #fff;
}

.ap-btn.primary:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}

.ap-run {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.ap-bar {
  height: 4px;
  border-radius: 2px;
  background: var(--color-fill-2, #f2f3f5);
  overflow: hidden;
}

.ap-bar i {
  display: block;
  height: 100%;
  background: rgb(var(--primary-6, 22, 93, 255));
  transition: width 0.2s ease;
}

.ap-run-text {
  font-size: var(--base-size-small);
  color: var(--color-text-3, #86909c);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
</style>
