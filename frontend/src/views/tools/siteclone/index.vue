<template>
  <!-- 仿站工具：输入 URL，抓「仅主页」或「整站」的 HTML/CSS/JS/图片，落地为可离线打开的本地副本 -->
  <div class="sc-shell">
    <a-card class="sc-card" :bordered="false">
      <template #title>
        <span class="sc-title">仿站工具</span>
        <span class="sc-sub">扒站 / 整站下载，可选离线改写为本地相对路径</span>
      </template>

      <a-form :model="form" layout="vertical" :disabled="running" class="sc-form">
        <a-row :gutter="16">
          <a-col :span="24">
            <a-form-item field="url" label="目标 URL">
              <a-input v-model="form.url" placeholder="https://example.com" allow-clear @press-enter="start">
                <template #prefix><icon-link /></template>
              </a-input>
            </a-form-item>
          </a-col>

          <a-col :span="12">
            <a-form-item field="mode" label="抓取范围"
              :help="form.mode === 'page' ? '只抓入口页及其引用的资源' : '在同域内跟链接逐页抓取；资源允许来自 CDN'">
              <a-radio-group v-model="form.mode" type="button">
                <a-radio value="page">仅主页</a-radio>
                <a-radio value="site">整站（同域）</a-radio>
              </a-radio-group>
            </a-form-item>
          </a-col>

          <a-col :span="12">
            <a-form-item field="outDir" label="输出目录">
              <a-input-group compact>
                <a-input v-model="form.outDir" placeholder="选择保存下载内容的本地目录" style="width: calc(100% - 80px)" />
                <a-button style="width: 80px" @click="chooseDir">浏览</a-button>
              </a-input-group>
            </a-form-item>
          </a-col>

          <template v-if="form.mode === 'site'">
            <a-col :span="8">
              <a-form-item field="maxDepth" label="最大深度">
                <a-input-number v-model="form.maxDepth" :min="1" :max="5" hide-button>
                  <template #suffix>层</template>
                </a-input-number>
              </a-form-item>
            </a-col>
            <a-col :span="8">
              <a-form-item field="maxPages" label="最大页数">
                <a-input-number v-model="form.maxPages" :min="1" :max="2000" hide-button />
              </a-form-item>
            </a-col>
          </template>

          <a-col :span="8">
            <a-form-item field="concurrency" label="并发数">
              <a-input-number v-model="form.concurrency" :min="1" :max="16" hide-button />
            </a-form-item>
          </a-col>
          <a-col :span="8">
            <a-form-item field="timeoutSec" label="超时">
              <a-input-number v-model="form.timeoutSec" :min="5" :max="120" hide-button>
                <template #suffix>秒</template>
              </a-input-number>
            </a-form-item>
          </a-col>
          <a-col :span="8">
            <a-form-item field="rewrite" label="离线改写">
              <a-switch v-model="form.rewrite" type="round" />
              <span class="hint inline">{{ form.rewrite ? '链接改写为本地路径' : '保留原始链接' }}</span>
            </a-form-item>
          </a-col>

          <a-col :span="24">
            <a-form-item field="userAgent" label="User-Agent">
              <a-input v-model="form.userAgent" placeholder="留空用默认" allow-clear />
            </a-form-item>
          </a-col>

          <a-col :span="24">
            <a-form-item field="proxy" label="代理"
              :help="form.proxy ? '本次抓取经该代理访问目站' : '留空直连（可从共享代理池选一条）'">
              <a-select v-model="form.proxy" size="small" allow-clear placeholder="直连（不走代理）" allow-search>
                <a-option v-for="p in proxyPool" :key="p.id" :value="p.url">{{ p.name || p.url }}</a-option>
              </a-select>
            </a-form-item>
          </a-col>
        </a-row>

        <div class="sc-actions">
          <a-button type="primary" :loading="running" @click="start">
            <template #icon><icon-play-arrow /></template>{{ running ? '抓取中…' : '开始抓取' }}
          </a-button>
          <a-button v-if="running" status="danger" @click="cancel">取消</a-button>
          <a-button v-if="result?.root" @click="openDir">
            <template #icon><icon-folder /></template>打开目录
          </a-button>
        </div>
      </a-form>

      <!-- 进度（复用全局任务进度行组件，与下载页/任务抽屉同一套口径） -->
      <div class="sc-progress">
        <common-task-progress variant="bare" size="small" :status="tpStatus" :name="tpName"
          :percent="percent * 100" :indeterminate="tpIndeterminate" :meta="tpMeta" :divided="false">
          <template #actions><!-- 取消/打开目录由上方表单按钮承担 --></template>
        </common-task-progress>
      </div>

      <!-- 结果统计：只在有产出时出现（复用首页同款 common-stat-card） -->
      <a-row v-if="stats.files || stats.bytes || failCount" :gutter="[12, 12]" class="sc-cards">
        <a-col :xs="12" :sm="12" :md="6">
          <common-stat-card tone="blue" label="页数" :value="pagesValue"><template #icon><icon-apps /></template></common-stat-card>
        </a-col>
        <a-col :xs="12" :sm="12" :md="6">
          <common-stat-card tone="cyan" label="文件" :value="stats.files"><template #icon><icon-file /></template></common-stat-card>
        </a-col>
        <a-col :xs="12" :sm="12" :md="6">
          <common-stat-card tone="green" label="大小" :value="fmtBytes(stats.bytes)"><template #icon><icon-storage /></template></common-stat-card>
        </a-col>
        <a-col :xs="12" :sm="12" :md="6">
          <common-stat-card :tone="failCount ? 'red' : 'gray'" label="失败资源" :value="failCount"
            :ready="!!failCount" :detail="failCount ? '点下方复制清单' : '无'">
            <template #icon><icon-exclamation-circle /></template>
          </common-stat-card>
        </a-col>
      </a-row>

      <!-- 失败汇总：有失败资源时给一条可复制清单的提示，方便二次补抓 -->
      <a-alert v-if="failCount" class="sc-fail-alert" type="warning" :closable="false">
        <template #content>
          <div class="sc-fail-row">
            <span>本次有 {{ failCount }} 个资源抓取失败</span>
            <a-space :size="6">
              <a-button size="mini" @click="filterFails = !filterFails">{{ filterFails ? '看全部日志' : '只看失败' }}</a-button>
              <a-button size="mini" @click="copyFails"><template #icon><icon-copy /></template>复制失败清单</a-button>
            </a-space>
          </div>
        </template>
      </a-alert>

      <!-- 日志 -->
      <div class="sc-log">
        <div v-for="(l, i) in shownLogs" :key="i" class="log-line" :class="l.status">
          <span class="log-time">{{ l.t }}</span>
          <span class="log-stage">{{ stageText(l.stage) }}</span>
          <span class="log-msg">{{ l.url || l.msg }}</span>
          <span v-if="l.status === 'fail'" class="log-err">{{ l.msg }}</span>
        </div>
        <div v-if="!shownLogs.length" class="log-empty">
          <a-empty :image-size="48" :description="filterFails ? '没有失败记录' : '暂无输出'" />
        </div>
      </div>

      <!-- 本次会话历史：已完成的抓取列在下面，便于反复抓多个站时回看落点 -->
      <div v-if="history.length" class="sc-history">
        <div class="sc-history-title">本次会话已完成</div>
        <div v-for="(h, i) in history" :key="i" class="sc-history-row">
          <span class="sc-history-name" :title="h.url">{{ h.url }}</span>
          <span class="sc-history-meta">文件 {{ h.files }} · {{ fmtBytes(h.bytes) }}<template v-if="h.pages"> · 页 {{ h.pages }}</template></span>
          <a-link @click="openInExplorer(h.root)">定位</a-link>
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
 * 仿站工具页。抓取逻辑全在 Go（tools/siteclone），本页只发任务、订阅 siteclone:progress、展示进度。
 */
const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

interface ScForm {
  url: string;
  mode: 'page' | 'site';
  maxDepth: number;
  maxPages: number;
  concurrency: number;
  timeoutSec: number;
  userAgent: string;
  proxy: string;
  outDir: string;
  rewrite: boolean;
}

const form = reactive<ScForm>({
  url: '',
  mode: 'page',
  maxDepth: 3,
  maxPages: 200,
  concurrency: 6,
  timeoutSec: 20,
  userAgent: 'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36',
  proxy: '',
  outDir: '',
  rewrite: true,
});

// 共享代理池（config.Download.ProxyPool）：只读选一条喂进本次抓取，与下载页/浏览器同源
const proxyPool = ref<{ id: string; name: string; url: string }[]>([]);

const running = ref(false);
const currentJob = ref('');
const result = ref<{ root: string } | null>(null);
const stats = reactive({ pagesDone: 0, pagesTotal: 0, files: 0, bytes: 0 });

interface LogLine { t: string; stage: string; url: string; msg: string; status: string }
const logs = ref<LogLine[]>([]);
const MAX_LOG = 400;

// 失败只看开关 + 派生统计：fail 行聚成可复制清单，不重跑只提醒用户二次补抓。
const filterFails = ref(false);
const shownLogs = computed(() => (filterFails.value ? logs.value.filter((l) => l.status === 'fail') : logs.value));
const failUrls = computed(() => logs.value.filter((l) => l.status === 'fail').map((l) => l.url || l.msg).filter(Boolean));
const failCount = computed(() => failUrls.value.length);
// 页数展示：整站且已知总数时 “已完成/总数”，否则只给已完成数。
const pagesValue = computed(() =>
  form.mode === 'site' && stats.pagesTotal > 0 ? `${stats.pagesDone}/${stats.pagesTotal}` : String(stats.pagesDone));

interface HistoryRow { url: string; root: string; files: number; bytes: number; pages: string }
const history = ref<HistoryRow[]>([]);

async function copyFails() {
  if (!failUrls.value.length) return;
  try {
    await navigator.clipboard.writeText(failUrls.value.join('\n'));
    Message.success({ content: `已复制 ${failUrls.value.length} 条失败资源 URL`, position: 'top', duration: 2000 });
  } catch {
    Message.warning({ content: '当前环境不支持写入剪贴板', position: 'top', duration: 2500 });
  }
}

let offEvent: any = null;

const errText = (e: any) => String(e?.message || e || '操作失败');

const percent = computed(() => {
  if (result.value) return 1;
  if (stats.pagesTotal > 0) return Math.min(0.99, stats.pagesDone / stats.pagesTotal);
  return running.value ? 0.05 : 0;
});
const statusText = computed(() => {
  if (running.value) return '抓取中…';
  if (result.value) return '已完成';
  return '空闲';
});

// 终态阶段（done/canceled/error）回填给统一进度行判定颜色/动作；start 时清空。
const finalStage = ref('');
const tpStatus = computed<'idle' | 'running' | 'done' | 'error' | 'canceled'>(() => {
  if (result.value) return 'done';
  if (finalStage.value === 'error') return 'error';
  if (finalStage.value === 'canceled') return 'canceled';
  if (running.value) return 'running';
  return 'idle';
});
// 整站模式总页数未知时画不定进度，不编 5%。
const tpIndeterminate = computed(() => running.value && stats.pagesTotal <= 0);
const tpName = computed(() => '仿站：' + (form.url || '未填 URL'));
const tpMeta = computed(() => {
  const parts: string[] = [statusText.value];
  if (form.mode === 'site') parts.push(`页数 ${stats.pagesDone}/${stats.pagesTotal || '?'}`);
  parts.push(`文件 ${stats.files}`, `大小 ${fmtBytes(stats.bytes)}`);
  return parts.join(' · ');
});

function stageText(s: string): string {
  return ({ start: '开始', fetch: '下载', parse: '解析', asset: '资源', rewrite: '改写', done: '完成', error: '错误', canceled: '取消' } as Record<string, string>)[s] || s;
}

function fmtBytes(n: number): string {
  if (!n) return '0 B';
  const u = ['B', 'KB', 'MB', 'GB', 'TB'];
  let v = n, i = 0;
  while (v >= 1024 && i < u.length - 1) { v /= 1024; i++; }
  return `${v.toFixed(i === 0 ? 0 : 1)} ${u[i]}`;
}

function pushLog(l: LogLine) {
  logs.value.unshift(l);
  if (logs.value.length > MAX_LOG) logs.value.length = MAX_LOG;
}

function onProgress(raw: any) {
  const p = raw?.data ?? raw;
  if (!p) return;
  if (currentJob.value && p.jobId && p.jobId !== currentJob.value) return;

  stats.pagesDone = p.pagesDone || 0;
  stats.pagesTotal = p.pagesTotal || 0;
  stats.files = p.files || 0;
  stats.bytes = p.bytes || 0;

  if (p.stage === 'done' || p.stage === 'canceled') {
    running.value = false;
    finalStage.value = p.stage;
    result.value = p.root ? { root: p.root } : result.value;
    if (p.stage === 'done' && p.root) {
      history.value.unshift({
        url: form.url.trim(),
        root: p.root,
        files: p.files || stats.files,
        bytes: p.bytes || stats.bytes,
        pages: form.mode === 'site' ? String(p.pagesDone || stats.pagesDone) : '',
      });
      if (history.value.length > 30) history.value.length = 30;
    }
    pushLog({ t: now(), stage: p.stage, url: '', msg: p.root ? `输出目录：${p.root}` : (p.msg || ''), status: 'ok' });
    return;
  }
  if (p.stage === 'error' && p.status === 'fail') {
    running.value = false;
    finalStage.value = 'error';
    Message.error({ content: p.msg || '抓取失败', position: 'top', duration: 4000 });
  }
  pushLog({ t: now(), stage: p.stage, url: p.url || '', msg: p.msg || '', status: p.status || '' });
}

function now(): string {
  return new Date().toLocaleTimeString('zh-CN', { hour12: false });
}

async function chooseDir() {
  try {
    const dir = await GoTools.FtpChooseLocalFolder('选择仿站输出目录');
    if (dir) form.outDir = dir;
  } catch (e) {
    Message.error({ content: errText(e), position: 'top', duration: 3000 });
  }
}

async function start() {
  if (!form.url.trim()) { Message.warning({ content: '请先填写要抓取的 URL', position: 'top' }); return; }
  if (!form.outDir.trim()) { Message.warning({ content: '请先选择输出目录', position: 'top' }); return; }
  running.value = true;
  result.value = null;
  finalStage.value = '';
  logs.value = [];
  Object.assign(stats, { pagesDone: 0, pagesTotal: 0, files: 0, bytes: 0 });
  try {
    const jobId = await GoTools.SiteCloneStart({
      url: form.url.trim(),
      mode: form.mode,
      maxDepth: form.maxDepth,
      maxPages: form.maxPages,
      concurrency: form.concurrency,
      timeoutSec: form.timeoutSec,
      userAgent: form.userAgent,
      proxy: form.proxy,
      outDir: form.outDir.trim(),
      rewrite: form.rewrite,
    });
    currentJob.value = jobId || '';
  } catch (e) {
    running.value = false;
    Message.error({ content: errText(e), position: 'top', duration: 4000 });
  }
}

async function cancel() {
  try {
    await GoTools.SiteCloneCancel();
  } catch (e) {
    Message.error({ content: errText(e), position: 'top', duration: 3000 });
  }
}

async function openDir() {
  if (!result.value?.root) return;
  openInExplorer(result.value.root);
}

async function openInExplorer(p: string) {
  if (!p) return;
  try {
    await GoTools.DiskOpenInExplorer(p);
  } catch (e) {
    Message.error({ content: errText(e), position: 'top', duration: 3000 });
  }
}

/* ==================== 表单记忆 ==================== */
// 只持久化偏好的抓取参数（URL 不记，避免下次默认去抓上一个站）。
const FORM_KEY = 'siteclone:form';
function restoreForm() {
  try {
    const raw = localStorage.getItem(FORM_KEY);
    if (!raw) return;
    const saved = JSON.parse(raw);
    for (const k of ['mode', 'maxDepth', 'maxPages', 'concurrency', 'timeoutSec', 'userAgent', 'proxy', 'outDir', 'rewrite'] as const) {
      if (saved[k] !== undefined) (form as any)[k] = saved[k];
    }
  } catch {
    /* 损坏则忽略，用默认 */
  }
}
watch(form, (v) => {
  const { url: _url, ...rest } = v;
  try {
    localStorage.setItem(FORM_KEY, JSON.stringify(rest));
  } catch {
    /* 存不下不阻断表单 */
  }
}, { deep: true });

onMounted(() => {
  restoreForm();
  offEvent = Events.On('siteclone:progress', onProgress);
  loadProxyPool();
});

/** 载入共享代理池（只读），供代理下拉选；后端未连时静默不阻断表单 */
async function loadProxyPool() {
  try {
    const st = await GoTools?.DownloadSettings();
    proxyPool.value = (st?.proxyPool ?? []).map((p: any) => ({ id: String(p.id ?? ''), name: String(p.name ?? ''), url: String(p.url ?? '') }));
  } catch {
    /* 静默 */
  }
}
onBeforeUnmount(() => {
  offEvent?.();
});
</script>

<style scoped>
.sc-shell {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding: 8px;
  box-sizing: border-box;
}

.sc-card {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.sc-card :deep(.arco-card-body) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: auto;
}

.sc-title {
  font-weight: 600;
}

.sc-sub {
  margin-left: 10px;
  color: rgb(var(--gray-5));
  font-size: var(--base-size-small);
}

.sc-form {
  flex: none;
}

.hint {
  color: rgb(var(--gray-5));
  font-size: var(--base-size-small);
  margin-top: 4px;
}

.hint.inline {
  margin-top: 0;
  margin-left: 8px;
}

.sc-actions {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 4px 0 12px;
}

.sc-progress {
  flex: none;
  margin-bottom: 8px;
}

.sc-cards {
  flex: none;
  margin: 2px 0 10px;
}

.sc-fail-alert {
  flex: none;
  margin-bottom: 8px;
}

.sc-fail-row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
}

.sc-log {
  flex: 1;
  min-height: 120px;
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
  width: 34px;
}

.log-msg {
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--color-text-2);
}

.log-err {
  color: rgb(var(--red-6));
}

.log-empty {
  color: rgb(var(--gray-5));
  text-align: center;
  padding-top: 24px;
}

.sc-history {
  flex: none;
  margin-top: 10px;
  border-top: 1px solid var(--color-border-1);
  padding-top: 8px;
}

.sc-history-title {
  font-size: var(--base-size-small);
  color: rgb(var(--gray-5));
  margin-bottom: 4px;
}

.sc-history-row {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: var(--base-size-small);
  line-height: 1.9;
}

.sc-history-name {
  max-width: 360px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--color-text-1);
}

.sc-history-meta {
  color: rgb(var(--gray-5));
}
</style>
