<template>
  <!-- 文件压缩：多选条目 → 格式/级别/输出位置 → 进度与历史。压缩与解压逻辑全在 Go（tools/archive） -->
  <div class="ar-shell">
    <a-card class="ar-card" :bordered="false">
      <template #title>
        <span class="ar-title">文件压缩</span>
        <span class="ar-sub">zip(Deflate) / tar.gz，与系统右键「压缩 (Tools)」同一套引擎</span>
      </template>

      <a-form :model="form" layout="vertical" :disabled="running" class="ar-form">
        <a-row :gutter="16">
          <a-col :span="24">
            <a-form-item field="op" label="操作">
              <a-radio-group v-model="form.op" type="button">
                <a-radio value="compress">压缩</a-radio>
                <a-radio value="extract">解压</a-radio>
              </a-radio-group>
            </a-form-item>
          </a-col>

          <a-col :span="24">
            <a-form-item :label="form.op === 'compress' ? '要压缩的条目' : '要解压的归档'">
              <!-- data-file-drop-target：Wails 的原生拖放标记，落下的真实路径由 Go 转成 archive:files-dropped -->
              <div class="ar-drop" ref="dropEl" :class="{ 'is-drop': dragOver }" data-file-drop-target="list"
                data-pane="list" @dragenter="dragOver = true" @dragleave="dragOver = false" @drop="onDomDrop">
                <div class="ar-drop-bar">
                  <a-button size="mini" @click="addFiles">
                    <template #icon><icon-file /></template>添加文件
                  </a-button>
                  <a-button size="mini" @click="addFolder">
                    <template #icon><icon-folder /></template>添加文件夹
                  </a-button>
                  <a-button size="mini" :disabled="!paths.length" @click="clearPaths">清空</a-button>
                  <span class="ar-drop-hint">也可以直接把文件/文件夹拖到这里</span>
                  <span v-if="preflight.files" class="ar-drop-size">
                    共 {{ preflight.files }} 个文件 · {{ fmtBytes(preflight.bytes) }}
                  </span>
                </div>

                <div v-if="paths.length" class="ar-list">
                  <div v-for="p in paths" :key="p" class="ar-item">
                    <common-file-icon class="ar-item-ic" :is-dir="isDirGuess(p)" :ext="extOf(p)" :size="16" />
                    <span class="ar-item-name">{{ baseName(p) }}</span>
                    <span class="ar-item-path">{{ p }}</span>
                    <icon-close class="ar-item-del" @click="removePath(p)" />
                  </div>
                </div>
                <div v-else class="ar-empty">
                  <a-empty :image-size="40" :description="form.op === 'compress' ? '还没有选择要压缩的条目' : '还没有选择要解压的归档'" />
                </div>
              </div>

              <!-- 解压：选中归档后先展包内清单（不整包解盘），用 ArchiveListEntries -->
              <div v-if="form.op === 'extract' && entryPreview.length" class="ar-entries">
                <div class="ar-entries-head">
                  <span>包内清单（共 {{ entryTotal }} 项）</span>
                  <a-link @click="entriesCollapsed = !entriesCollapsed">{{ entriesCollapsed ? '展开' : '收起' }}</a-link>
                </div>
                <div v-show="!entriesCollapsed" class="ar-entries-body">
                  <div v-for="(e, i) in entryPreview" :key="i" class="ar-entry">
                    <common-file-icon :is-dir="e.isDir" :ext="entryExt(e.dispPath)" :size="14" />
                    <span class="ar-entry-name">{{ e.dispPath }}</span>
                    <span class="ar-entry-size">{{ e.isDir ? '' : fmtBytes(e.size) }}</span>
                  </div>
                  <div v-if="entryTotal > entryPreview.length" class="ar-entry-more">…… 仅展示前 {{ entryPreview.length }} 项</div>
                </div>
              </div>
            </a-form-item>
          </a-col>

          <template v-if="form.op === 'compress'">
            <a-col :span="8">
              <a-form-item field="format" label="格式">
                <a-radio-group v-model="form.format" type="button">
                  <a-radio value="zip">zip</a-radio>
                  <a-radio value="targz">tar.gz</a-radio>
                </a-radio-group>
              </a-form-item>
            </a-col>
            <a-col :span="8">
              <a-form-item field="level" label="压缩级别" help="级别只影响体积与耗时，不影响能否解开">
                <a-select v-model="form.level">
                  <a-option :value="1">快速（1）</a-option>
                  <a-option :value="6">标准（6）</a-option>
                  <a-option :value="9">最高（9）</a-option>
                </a-select>
              </a-form-item>
            </a-col>
            <a-col :span="8">
              <a-form-item field="name" label="文件名" help="留空按命名规则自动取名">
                <a-input v-model="form.name" :placeholder="suggestBase || '自动'" allow-clear />
              </a-form-item>
            </a-col>
            <a-col :span="24">
              <a-form-item field="outDir" label="输出目录" help="留空就放在条目所在目录（多选时取公共父目录的上一级）">
                <a-input-group compact>
                  <a-input v-model="form.outDir" placeholder="默认就地输出" style="width: calc(100% - 80px)" />
                  <a-button style="width: 80px" @click="chooseOutDir">浏览</a-button>
                </a-input-group>
              </a-form-item>
            </a-col>
          </template>

          <a-col v-else :span="24">
            <a-form-item field="dest" label="解压到" help="留空解到归档同级的同名文件夹；已有同名文件夹时自动让开为「名称 (1)」，只写入、绝不合并或覆盖原目录">
              <a-input-group compact>
                <a-input v-model="form.dest" placeholder="默认解压到同名文件夹" style="width: calc(100% - 80px)" />
                <a-button style="width: 80px" @click="chooseDest">浏览</a-button>
              </a-input-group>
            </a-form-item>
          </a-col>
        </a-row>

        <div class="ar-preview" v-if="form.op === 'compress' && suggestPath">
          成品：<b>{{ suggestPath }}</b>
        </div>

        <div class="ar-actions">
          <a-button type="primary" :loading="running" @click="start">
            <template #icon><icon-play-arrow /></template>
            {{ running ? (form.op === 'compress' ? '压缩中…' : '解压中…') : (form.op === 'compress' ? '开始压缩' : '开始解压') }}
          </a-button>
          <a-button v-if="running" status="danger" @click="cancel">取消</a-button>
          <a-button v-if="doneOut" @click="openOut">
            <template #icon><icon-folder /></template>打开所在目录
          </a-button>
        </div>
      </a-form>

      <!-- 进度（复用全局任务进度行组件，与下载页/任务抽屉同一套口径） -->
      <div class="ar-progress">
        <common-task-progress variant="bare" size="small" :status="tpStatus" :name="tpName"
          :percent="percent * 100" :indeterminate="tpIndeterminate" :meta="tpMeta" :divided="false">
          <template #actions><!-- 取消/打开由上方表单按钮承担，这里只作进度展示 --></template>
        </common-task-progress>
      </div>

      <!-- 日志 -->
      <div class="ar-log">
        <div v-for="(l, i) in logs" :key="i" class="log-line" :class="l.status">
          <span class="log-time">{{ l.t }}</span>
          <span class="log-stage">{{ stageText(l.stage) }}</span>
          <span class="log-msg">{{ l.msg }}</span>
        </div>
        <div v-if="!logs.length" class="log-empty">暂无输出</div>
      </div>

      <!-- 本次会话的历史：右键发起的任务也会在这里露出来（同一个事件源） -->
      <div v-if="history.length" class="ar-history">
        <div class="ar-history-title">本次会话已完成</div>
        <div v-for="(h, i) in history" :key="i" class="ar-history-row">
          <span class="ar-history-name">{{ h.name }}</span>
          <span class="ar-history-meta">{{ h.files }} 项 · {{ fmtBytes(h.bytes) }} · {{ (h.elapsedMs / 1000).toFixed(1) }}s</span>
          <a-link @click="openInExplorer(h.out)">定位</a-link>
        </div>
      </div>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { Message } from '@arco-design/web-vue';
import { Events } from '@wailsio/runtime';

/**
 * 文件压缩页。压缩/解压的编排都在 Go（tools/archive），本页只发任务、订阅 archive:progress。
 *
 * 与右键那条路的分工：这里带用户当场选的格式/级别/输出位置；右键直接走设置里的默认值。
 * 两者最终都进同一个任务号体系，进度事件按 jobId 过滤，右键发起的包也会出现在这里。
 */
const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const form = reactive({
  op: 'compress' as 'compress' | 'extract',
  format: 'zip' as 'zip' | 'targz',
  level: 6,
  name: '',
  outDir: '',
  dest: '',
});

const paths = ref<string[]>([]);
const running = ref(false);
const currentJob = ref('');
const dragOver = ref(false);
const dropEl = ref<HTMLElement | null>(null);

const preflight = reactive({ files: 0, bytes: 0 });
const suggestPath = ref('');
const suggestBase = ref('');
const doneOut = ref('');

const stats = reactive({ stage: '', percent: 0, bytes: 0, total: 0, files: 0, skipped: 0 });

interface LogLine { t: string; stage: string; msg: string; status: string }
const logs = ref<LogLine[]>([]);
const MAX_LOG = 300;

interface HistoryRow { name: string; out: string; files: number; bytes: number; elapsedMs: number }
const history = ref<HistoryRow[]>([]);

let offProgress: any = null;
let offDrop: any = null;
let scanTimer: number | null = null;

const errText = (e: any) => String(e?.message || e || '操作失败');

const percent = computed(() => {
  if (doneOut.value) return 1;
  // 解压的百分比恒为 0（Go 侧不猜）：让界面画不定进度条，而不是一个假百分比
  if (form.op === 'extract') return running.value ? 0.5 : 0;
  return Math.min(0.99, (stats.percent || 0) / 100);
});
const statusText = computed(() => {
  if (running.value) return stageText(stats.stage) || '进行中…';
  if (doneOut.value) return '已完成';
  return '空闲';
});

// 统一进度行（common-task-progress）的派生态：把本页 running/doneOut/stage 映射成组件语义。
const tpStatus = computed<'idle' | 'running' | 'done' | 'error' | 'canceled'>(() => {
  if (doneOut.value) return 'done';
  if (stats.stage === 'error') return 'error';
  if (stats.stage === 'canceled') return 'canceled';
  if (running.value) return 'running';
  return 'idle';
});
// 解压拿不到真百分比（Go 侧不猜），运行中画不定进度亮带而非假 50%。
const tpIndeterminate = computed(() => running.value && form.op === 'extract');
const tpName = computed(() => (form.op === 'compress' ? '压缩' : '解压') + (paths.value.length ? `：${baseName(paths.value[0])}` : ''));
const tpMeta = computed(() => {
  const parts: string[] = [statusText.value];
  if (stats.files) parts.push(`已处理 ${stats.files} 项`);
  parts.push(`${fmtBytes(stats.bytes)}${stats.total ? ' / ' + fmtBytes(stats.total) : ''}`);
  if (stats.skipped) parts.push(`跳过 ${stats.skipped} 项（读不动或符号链接）`);
  return parts.join(' · ');
});

function stageText(s: string): string {
  return ({
    start: '开始', scan: '统计', writing: '压缩', extracting: '解压',
    done: '完成', error: '错误', canceled: '取消',
  } as Record<string, string>)[s] || s;
}

function fmtBytes(n: number): string {
  if (!n) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = n, i = 0;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${u[i]}`;
}

function baseName(p: string): string {
  const i = Math.max(p.lastIndexOf('\\'), p.lastIndexOf('/'));
  return i >= 0 ? p.slice(i + 1) : p;
}

function now(): string {
  return new Date().toLocaleTimeString('zh-CN', { hour12: false });
}

function pushLog(l: LogLine) {
  logs.value.unshift(l);
  if (logs.value.length > MAX_LOG) logs.value.length = MAX_LOG;
}

/* ============================ 条目维护 ============================ */

function appendPaths(list: string[]) {
  const seen = new Set(paths.value);
  // 解压一次只做一个归档（Go 侧同口径：多个包解到同一目录会互相覆盖），故只留最后一个
  if (form.op === 'extract') {
    paths.value = list.filter((p) => !seen.has(p)).slice(-1);
  } else {
    paths.value = paths.value.concat(list.filter((p) => !seen.has(p)));
  }
  refreshHints();
}

function removePath(p: string) {
  paths.value = paths.value.filter((x) => x !== p);
  refreshHints();
}

function clearPaths() {
  paths.value = [];
  refreshHints();
}

async function addFiles() {
  try {
    const picked = await GoTools.ArchiveChooseFiles();
    if (picked?.length) appendPaths(picked as string[]);
  } catch (e) {
    Message.error({ content: errText(e), position: 'top', duration: 3000 });
  }
}

async function addFolder() {
  try {
    const dir = await GoTools.ChooseDir('选择要压缩的文件夹');
    if (dir) appendPaths([dir]);
  } catch (e) {
    Message.error({ content: errText(e), position: 'top', duration: 3000 });
  }
}

async function chooseOutDir() {
  try {
    const dir = await GoTools.ChooseDir('选择压缩输出目录');
    if (dir) form.outDir = dir;
  } catch (e) {
    Message.error({ content: errText(e), position: 'top', duration: 3000 });
  }
}

async function chooseDest() {
  try {
    const dir = await GoTools.ChooseDir('选择解压目标目录');
    if (dir) form.dest = dir;
  } catch (e) {
    Message.error({ content: errText(e), position: 'top', duration: 3000 });
  }
}

/** 页内拖拽：浏览器在 WebView2 里读不到本机文件的真实路径（dataTransfer 只有文件名），
 *  所以本区的真正入口是 Go 的原生拖放（archive:files-dropped）；这里只处理从
 *  页面其他位置拖过来的纯文本路径（比如从历史行拖过来）。 */
function onDomDrop(ev: DragEvent) {
  dragOver.value = false;
  if (ev.dataTransfer?.files?.length) return; // 本机文件：原生拖放会另发事件，不在这儿重复收
  const text = ev.dataTransfer?.getData('text/plain');
  if (!text) return;
  const list = text.split('\n').map((s) => s.trim()).filter((s) => s.includes('\\') || s.includes('/'));
  if (list.length) appendPaths(list);
}

/* ============================ 预扫描与命名提示 ============================ */

/** 条目变了别马上扫盘：十万文件的目录每次按键都扫一边会把界面拖死 */
function refreshHints() {
  if (scanTimer !== null) window.clearTimeout(scanTimer);
  scanTimer = window.setTimeout(scanHints, 250);
}

async function scanHints() {
  scanTimer = null;
  if (!paths.value.length) {
    preflight.files = 0;
    preflight.bytes = 0;
    suggestPath.value = '';
    suggestBase.value = '';
    return;
  }
  if (form.op === 'extract') {
    preflight.files = 1;
    preflight.bytes = 0;
    return;
  }
  try {
    const pf = await GoTools.ArchiveScan(paths.value);
    preflight.files = pf?.files || 0;
    preflight.bytes = pf?.bytes || 0;
  } catch {
    preflight.files = 0;
    preflight.bytes = 0;
  }
  try {
    const out = await GoTools.ArchiveSuggestName(buildOptions());
    suggestPath.value = out || '';
    suggestBase.value = out ? baseName(out) : '';
  } catch {
    suggestPath.value = '';
    suggestBase.value = '';
  }
}

function buildOptions() {
  return {
    op: form.op,
    paths: paths.value,
    format: form.format,
    level: form.level,
    outDir: form.outDir.trim(),
    name: form.name.trim(),
    dest: form.dest.trim(),
  };
}

/* ============================ 条目图标与包内清单 ============================ */

function extOf(p: string): string {
  const b = baseName(p);
  const i = b.lastIndexOf('.');
  return i > 0 ? b.slice(i + 1).toLowerCase() : '';
}
// 归档名/包内路径取最后一段的扩展名。
function entryExt(disp: string): string {
  const b = disp.replace(/[\\/]+$/, '').split(/[\\/]/).pop() || '';
  const i = b.lastIndexOf('.');
  return i > 0 ? b.slice(i + 1).toLowerCase() : '';
}
// 条目列表只有字符串路径、无从表中知道目录/文件；用“无扩展名则视为目录”启发式贴图标，
// 属显示层面的近似，不影响任何实际操作（实际类型由 Go 侧扫描定）。
function isDirGuess(p: string): boolean {
  return extOf(p) === '';
}

interface ArcEntry { dispPath: string; isDir: boolean; size: number }
const entries = ref<ArcEntry[]>([]);
const entriesCollapsed = ref(false);
const entryTotal = computed(() => entries.value.length);
const ENTRY_PREVIEW = 100;
const entryPreview = computed(() => entries.value.slice(0, ENTRY_PREVIEW));

let entryTimer: number | null = null;
// 选中归档（解压态且恰好一个包）时延迟拉包内清单；多选/压缩态清空。
watch([() => form.op, paths], () => {
  if (entryTimer !== null) window.clearTimeout(entryTimer);
  if (form.op !== 'extract' || paths.value.length !== 1) {
    entries.value = [];
    return;
  }
  entryTimer = window.setTimeout(async () => {
    const p = paths.value[0];
    try {
      const list = await GoTools.ArchiveListEntries(p);
      // 拉回来时可能已改了选择：确认还是同一个包才赋值
      if (form.op === 'extract' && paths.value.length === 1 && paths.value[0] === p) {
        entries.value = (list || []) as ArcEntry[];
      }
    } catch {
      entries.value = []; // 不支持的格式（如 rar/7z）静默不展清单
    }
  }, 250);
}, { deep: true });

/* ============================ 任务 ============================ */

async function start() {
  if (!paths.value.length) {
    Message.warning({ content: form.op === 'compress' ? '先添加要压缩的条目' : '先选择要解压的归档', position: 'top' });
    return;
  }
  running.value = true;
  doneOut.value = '';
  // 先把上一笔的任务号清掉：留着它，这一笔的进度会被当成「别人的包」整批筛掉。
  // 解压几个小文件只要几毫秒，终态事件往往比下面那个 await 的返回值更早到，
  // 错过之后界面再也没有机会自己结束（实测就是把按钮永远卡在「解压中…」）。
  currentJob.value = '';
  Object.assign(stats, { stage: 'start', percent: 0, bytes: 0, total: 0, files: 0, skipped: 0 });
  try {
    const opts = buildOptions();
    const id = form.op === 'compress'
      ? await GoTools.ArchiveCompress(opts)
      : await GoTools.ArchiveExtract(opts);
    currentJob.value = id || '';
  } catch (e) {
    running.value = false;
    Message.error({ content: errText(e), position: 'top', duration: 4000 });
  }
}

async function cancel() {
  if (!currentJob.value) {
    running.value = false;
    return;
  }
  try {
    // 返回 false = 这一笔其实早就结束了（多半是终态事件没赶上）：本地直接收回。
    // 取消一个已结束的任务按定义不会再有事件回来，不收回按钮就死在这里。
    if (!(await GoTools.ArchiveCancel(currentJob.value))) {
      running.value = false;
      Message.warning({ content: '这一笔已经结束了', position: 'top', duration: 2500 });
    }
  } catch (e) {
    running.value = false;
    Message.error({ content: errText(e), position: 'top', duration: 3000 });
  }
}

function onProgress(raw: any) {
  const p = raw?.data ?? raw;
  if (!p) return;
  // 号还没定（刚发起，或极快任务的终态事件跑到了返回值前面）时先认下这一笔；
  // 定了之后只认同一个 jobId 的包：右键并发发起的别的任务不往这条进度条上叠。
  if (!currentJob.value && p.jobId) currentJob.value = p.jobId;
  if (currentJob.value && p.jobId && p.jobId !== currentJob.value) return;

  stats.stage = p.stage || stats.stage;
  stats.percent = p.percent ?? stats.percent;
  stats.bytes = p.bytes ?? stats.bytes;
  stats.total = p.total ?? stats.total;
  stats.files = p.files ?? stats.files;
  stats.skipped = p.skipped?.length ?? stats.skipped;

  if (p.msg) pushLog({ t: now(), stage: p.stage, msg: p.msg, status: p.status || '' });

  if (p.stage === 'done') {
    running.value = false;
    doneOut.value = p.out || '';
    history.value.unshift({
      name: p.name || baseName(p.out || ''),
      out: p.out || '',
      files: p.files || 0,
      bytes: p.bytes || 0,
      elapsedMs: p.elapsedMs || 0,
    });
    if (history.value.length > 30) history.value.length = 30;
    // 压缩成功额外报个压缩率（写入体积/原始体积），让“划不划算”一眼可见。
    let ratio = '';
    if (form.op === 'compress' && p.total > 0 && p.bytes > 0 && p.bytes < p.total) {
      ratio = ` · 压缩率 ${((1 - p.bytes / p.total) * 100).toFixed(0)}%`;
    }
    Message.success({ content: `${p.name || ''} 已完成${ratio}`, position: 'top', duration: 3000 });
    return;
  }
  if (p.stage === 'canceled') {
    running.value = false;
    Message.warning({ content: '已取消', position: 'top', duration: 2000 });
    return;
  }
  if (p.stage === 'error') {
    running.value = false;
    Message.error({ content: p.msg || '任务失败', position: 'top', duration: 4000 });
  }
}

async function openInExplorer(p: string) {
  if (!p) return;
  try {
    await GoTools.DiskOpenInExplorer(p);
  } catch (e) {
    Message.error({ content: errText(e), position: 'top', duration: 3000 });
  }
}

function openOut() {
  openInExplorer(doneOut.value);
}

/* ============================ 生命周期 ============================ */

onMounted(() => {
  offProgress = Events.On('archive:progress', onProgress);
  // 原生拖放：Go 把落下的真实路径转回来（与文件传输页同一套机制）
  // 「发送到 → 压缩 (Tools)…」那条小窗开工后，主窗口这页把成品接着看下去
  offDrop = Events.On('archive:files-dropped', (raw: any) => {
    const p = raw?.data ?? raw;
    const files: string[] = p?.files || [];
    if (files.length) appendPaths(files);
  });
});

onBeforeUnmount(() => {
  offProgress?.();
  offDrop?.();
  if (scanTimer !== null) window.clearTimeout(scanTimer);
  if (entryTimer !== null) window.clearTimeout(entryTimer);
});
</script>

<style scoped>
.ar-shell {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding: 8px;
  box-sizing: border-box;
}

.ar-card {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.ar-card :deep(.arco-card-body) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: auto;
}

.ar-title {
  font-weight: 600;
}

.ar-sub {
  margin-left: 10px;
  color: rgb(var(--gray-5));
  font-size: var(--base-size-small);
}

.ar-form {
  flex: none;
}

.ar-drop {
  width: 100%;
  border: 1px dashed var(--color-border-1);
  border-radius: var(--base-radius-default);
  background: var(--color-fill-1);
  padding: 8px;
  box-sizing: border-box;
}

.ar-drop.file-drop-target-active {
  outline: 2px dashed rgb(var(--primary-6));
  outline-offset: -2px;
  background: var(--color-primary-light-1);
}

.ar-drop.is-drop {
  border-color: rgb(var(--primary-6));
}

.ar-drop-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}

.ar-drop-hint {
  color: rgb(var(--gray-5));
  font-size: var(--base-size-small);
}

.ar-drop-size {
  margin-left: auto;
  color: var(--color-text-2);
  font-size: var(--base-size-small);
}

.ar-list {
  margin-top: 8px;
  max-height: 180px;
  overflow: auto;
}

.ar-item {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 2px 4px;
  font-size: var(--base-size-small);
  border-radius: var(--base-radius-default);
}

.ar-item-ic {
  flex: none;
}

.ar-item:hover {
  background: var(--color-fill-2);
}

.ar-item-name {
  flex: none;
  max-width: 220px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--color-text-1);
}

.ar-item-path {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: rgb(var(--gray-5));
}

.ar-item-del {
  flex: none;
  color: rgb(var(--gray-5));
  cursor: pointer;
}

.ar-item-del:hover {
  color: rgb(var(--red-6));
}

.ar-empty {
  margin-top: 8px;
  color: rgb(var(--gray-5));
  font-size: var(--base-size-small);
  text-align: center;
  padding: 8px 0;
}

/* 解压：包内清单预览 */
.ar-entries {
  margin-top: 8px;
  border: 1px solid var(--color-border-1);
  border-radius: var(--base-radius-default);
  background: var(--color-bg-1);
  padding: 6px 8px;
}

.ar-entries-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: var(--base-size-small);
  color: rgb(var(--gray-5));
}

.ar-entries-body {
  margin-top: 4px;
  max-height: 200px;
  overflow: auto;
}

.ar-entry {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 1px 4px;
  font-size: var(--base-size-small);
}

.ar-entry-name {
  flex: 1;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--color-text-1);
}

.ar-entry-size {
  flex: none;
  color: rgb(var(--gray-5));
}

.ar-entry-more {
  padding: 2px 4px;
  color: rgb(var(--gray-5));
  font-size: var(--base-size-small);
}

.ar-preview {
  margin: 4px 0 8px;
  color: var(--color-text-2);
  font-size: 12px;
  word-break: break-all;
}

.ar-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 4px 0 12px;
}

.ar-progress {
  flex: none;
  margin-bottom: 8px;
}

.ar-stats {
  display: flex;
  gap: 18px;
  margin-top: 6px;
  color: var(--color-text-2);
  font-size: 12px;
}

.ar-log {
  flex: 1;
  min-height: 110px;
  overflow: auto;
  border: 1px solid var(--color-border-1);
  border-radius: var(--base-radius-default);
  background: var(--color-fill-1);
  padding: 6px 8px;
  font-size: var(--base-size-small);
  line-height: 1.7;
}

.log-line {
  display: flex;
  gap: 8px;
  white-space: nowrap;
}

.log-line.fail .log-msg {
  color: rgb(var(--red-6));
}

.log-time {
  color: rgb(var(--gray-5));
  flex: none;
}

.log-stage {
  color: rgb(var(--primary-6));
  flex: none;
  width: 38px;
}

.log-msg {
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--color-text-2);
}

.log-empty {
  color: rgb(var(--gray-5));
  text-align: center;
  padding-top: 32px;
}

.ar-history {
  flex: none;
  margin-top: 10px;
  border-top: 1px solid var(--color-border-1);
  padding-top: 8px;
}

.ar-history-title {
  font-size: var(--base-size-small);
  color: rgb(var(--gray-5));
  margin-bottom: 4px;
}

.ar-history-row {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: var(--base-size-small);
  line-height: 1.9;
}

.ar-history-name {
  max-width: 320px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--color-text-1);
}

.ar-history-meta {
  color: var(--color-text-3);
}
</style>
