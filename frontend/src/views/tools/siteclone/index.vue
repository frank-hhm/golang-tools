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

      <!-- 进度 -->
      <div class="sc-progress">
        <a-progress :percent="percent" :status="progressStatus" size="small" />
        <div class="sc-stats">
          <span>状态：<b>{{ statusText }}</b></span>
          <span v-if="form.mode === 'site'">页数：{{ stats.pagesDone }} / {{ stats.pagesTotal || '?' }}</span>
          <span>文件：{{ stats.files }}</span>
          <span>大小：{{ fmtBytes(stats.bytes) }}</span>
        </div>
      </div>

      <!-- 日志 -->
      <div class="sc-log">
        <div v-for="(l, i) in logs" :key="i" class="log-line" :class="l.status">
          <span class="log-time">{{ l.t }}</span>
          <span class="log-stage">{{ stageText(l.stage) }}</span>
          <span class="log-msg">{{ l.url || l.msg }}</span>
          <span v-if="l.status === 'fail'" class="log-err">{{ l.msg }}</span>
        </div>
        <div v-if="!logs.length" class="log-empty">暂无输出</div>
      </div>
    </a-card>
  </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
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
  outDir: '',
  rewrite: true,
});

const running = ref(false);
const currentJob = ref('');
const result = ref<{ root: string } | null>(null);
const stats = reactive({ pagesDone: 0, pagesTotal: 0, files: 0, bytes: 0 });

interface LogLine { t: string; stage: string; url: string; msg: string; status: string }
const logs = ref<LogLine[]>([]);
const MAX_LOG = 400;

let offEvent: any = null;

const errText = (e: any) => String(e?.message || e || '操作失败');

const percent = computed(() => {
  if (result.value) return 1;
  if (stats.pagesTotal > 0) return Math.min(0.99, stats.pagesDone / stats.pagesTotal);
  return running.value ? 0.05 : 0;
});
const progressStatus = computed<'normal' | 'success' | 'warning' | 'danger'>(() => {
  if (result.value) return 'success';
  return 'normal';
});
const statusText = computed(() => {
  if (running.value) return '抓取中…';
  if (result.value) return '已完成';
  return '空闲';
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
    result.value = p.root ? { root: p.root } : result.value;
    pushLog({ t: now(), stage: p.stage, url: '', msg: p.root ? `输出目录：${p.root}` : (p.msg || ''), status: 'ok' });
    return;
  }
  if (p.stage === 'error' && p.status === 'fail') {
    running.value = false;
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
  try {
    await GoTools.DiskOpenInExplorer(result.value.root);
  } catch (e) {
    Message.error({ content: errText(e), position: 'top', duration: 3000 });
  }
}

onMounted(() => {
  offEvent = Events.On('siteclone:progress', onProgress);
});
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
  color: var(--color-text-3);
  font-size: 12px;
}

.sc-form {
  flex: none;
}

.hint {
  color: var(--color-text-3);
  font-size: 12px;
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

.sc-stats {
  display: flex;
  gap: 18px;
  margin-top: 6px;
  color: var(--color-text-2);
  font-size: 12px;
}

.sc-log {
  flex: 1;
  min-height: 120px;
  overflow: auto;
  border: 1px solid var(--color-border-2);
  border-radius: var(--border-radius-small);
  background: var(--color-fill-1);
  padding: 6px 8px;
  font-size: 12px;
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
  color: var(--color-text-4);
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
  color: var(--color-text-4);
  text-align: center;
  padding-top: 40px;
}
</style>
