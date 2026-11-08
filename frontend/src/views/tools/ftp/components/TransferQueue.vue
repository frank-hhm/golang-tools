<template>
  <div class="tq">
    <div class="tq-head">
      <div class="tq-title">
        <span class="tq-label">传输队列</span>
        <a-tag v-if="running" size="small" color="arcoblue">{{ running }} 进行中</a-tag>
        <a-tag v-if="failed" size="small" color="red">{{ failed }} 失败</a-tag>
        <a-tag v-if="done" size="small" color="green">{{ done }} 完成</a-tag>
      </div>
      <a-space :size="4">
        <a-button v-if="failed" size="mini" class="btn-quiet" @click="retryAll">重试全部失败</a-button>
        <a-button v-if="finished" size="mini" class="btn-quiet" @click="emit('clear')">清空已完成</a-button>
        <a-button size="mini" class="btn-quiet" @click="collapsed = !collapsed">
          <template #icon><component :is="collapsed ? 'icon-down' : 'icon-up'" /></template>
        </a-button>
      </a-space>
    </div>

    <!-- 展开/收起靠 grid-template-rows 0fr↔1fr 做高度过渡：内容高度不固定
         （空态 96px、多任务到 156px 封顶）也能平滑收放，不用写死 max-height -->
    <div class="tq-collapse" :class="{ 'is-collapsed': collapsed }">
      <div class="tq-collapse-inner">
        <div class="tq-body">
          <div v-if="!transfers.length" class="tq-empty">
            <div class="tq-empty-box">
              <icon-clock-circle class="tq-empty-ic" />
              <span class="tq-empty-t">暂无传输任务</span>
              <span class="tq-empty-sub">在文件列表选中后点「上传 / 下载」即可加入队列</span>
            </div>
          </div>
          <div v-for="t in transfers" :key="t.id" class="tq-row">
            <common-task-progress variant="bare" size="small" :status="tpStatus(t)" :percent="percent(t)"
              :show-text="false" :divided="false" :speed="t.state === 'running' ? fmtSpeed(t.speed) : ''">
              <template #name>
                <a-tag size="small" :color="t.dir === 'up' ? 'arcoblue' : 'purple'">{{ t.dir === 'up' ? '上传' : '下载' }}</a-tag>
                <a-tooltip :content="t.dir === 'up' ? `${t.local} → ${t.remote}` : `${t.remote} → ${t.local}`">
                  <span class="tq-name">{{ t.name }}</span>
                </a-tooltip>
                <a-tooltip v-if="t.err" :content="t.err">
                  <span class="tq-err"><icon-exclamation-circle-fill /> {{ t.err }}</span>
                </a-tooltip>
              </template>
              <template #extra>
                <span class="tq-num">{{ percentText(t) }}</span>
                <span class="tq-state" :class="'s-' + t.state">{{ stateText(t.state) }}</span>
              </template>
              <template #actions>
                <a-button v-if="t.state === 'queued' || t.state === 'running'" size="mini" class="btn-quiet" status="danger"
                  @click="emit('cancel', t.id)">取消</a-button>
                <a-button v-else-if="t.state === 'failed' || t.state === 'canceled'" size="mini" class="btn-quiet"
                  @click="emit('retry', t.id)">重试</a-button>
              </template>
            </common-task-progress>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { fmtSize } from '@/views/tools/disk/helpers';
import type { FtpTransfer } from '../types';

const props = defineProps<{ transfers: FtpTransfer[] }>();
const emit = defineEmits<{
  (e: 'cancel', id: string): void;
  (e: 'retry', id: string): void;
  (e: 'clear'): void;
}>();

const collapsed = ref(false);

const running = computed(() => props.transfers.filter((t) => t.state === 'running' || t.state === 'queued').length);
const failed = computed(() => props.transfers.filter((t) => t.state === 'failed').length);
const done = computed(() => props.transfers.filter((t) => t.state === 'done').length);
const finished = computed(() => props.transfers.some((t) => t.state === 'done' || t.state === 'failed' || t.state === 'canceled'));

// 重试全部失败/取消项：逐个 emit retry，父页按 id 调 FtpRetryTransfer。
function retryAll() {
  props.transfers
    .filter((t) => t.state === 'failed' || t.state === 'canceled')
    .forEach((t) => emit('retry', t.id));
}

function percent(t: FtpTransfer): number {
  if (t.total > 0) return Math.min(100, Math.round((t.done / t.total) * 100));
  return t.state === 'done' ? 100 : 0;
}

function percentText(t: FtpTransfer): string {
  if (t.total > 0) return `${fmtSize(t.done)} / ${fmtSize(t.total)}`;
  if (t.state === 'done') return '完成';
  return '—';
}

// ftp 传输态 → 统一进度行 status（queued→idle、running→running、done→done、failed→error、canceled→canceled）。
function tpStatus(t: FtpTransfer): 'idle' | 'running' | 'done' | 'error' | 'canceled' {
  switch (t.state) {
    case 'running': return 'running';
    case 'done': return 'done';
    case 'failed': return 'error';
    case 'canceled': return 'canceled';
    default: return 'idle';
  }
}

function stateText(state: FtpTransfer['state']): string {
  switch (state) {
    case 'queued':
      return '排队中';
    case 'running':
      return '传输中';
    case 'done':
      return '已完成';
    case 'failed':
      return '失败';
    default:
      return '已取消';
  }
}

function fmtSpeed(n: number): string {
  if (!n || n <= 0) return '0 B/s';
  return `${fmtSize(n)}/s`;
}
</script>

<style scoped>
.tq {
  border-top: 1px solid var(--color-border-2);
  background: var(--color-fill-1); /* 整块做成内凹托盘：与上方 bg-2 文件栏拉开一档 */
}

/* 头部：直接坐在托盘上，与正文之间靠下面的分隔线区分 */
.tq-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 6px 10px;
}

.tq-title {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.tq-label {
  font-size: 12px;
  font-weight: 600;
}

/* 展开/收起：grid-template-rows 0fr↔1fr 过渡，内容高度不固定也能平滑收放 */
.tq-collapse {
  display: grid;
  grid-template-rows: 1fr;
  transition: grid-template-rows 0.28s cubic-bezier(0.34, 0.06, 0.2, 1);
}

.tq-collapse.is-collapsed {
  grid-template-rows: 0fr;
}

.tq-collapse-inner {
  overflow: hidden;
  min-height: 0;
  opacity: 1;
  transition: opacity 0.22s ease;
}

.tq-collapse.is-collapsed .tq-collapse-inner {
  opacity: 0;
}

@media (prefers-reduced-motion: reduce) {

  .tq-collapse,
  .tq-collapse-inner {
    transition: none;
  }
}

.tq-body {
  max-height: 156px;
  overflow: auto;
  border-top: 1px solid var(--color-border-2);
}

/* 空态：占满 min-height，居中一个虚线圆角托盘，图标 + 主提示 + 副说明，
   不再是扫平背景上一行孤字 */
.tq-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  min-height: 96px;
  padding: 12px;
}

.tq-empty-box {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 5px;
  width: 100%;
  max-width: 380px;
  padding: 16px 12px;
  border: 1px dashed var(--color-border-2);
  border-radius: var(--base-radius-default);
  background: var(--color-fill-2); /* 比托盘略深一档的内凹占位，不再用高对比 bg-2 */
  color: var(--color-text-3);
  font-size: 12px;
}

.tq-empty-ic {
  font-size: 22px;
  color: var(--color-text-4, #c9cdd4);
}

.tq-empty-t {
  color: var(--color-text-2);
  font-weight: 600;
}

.tq-empty-sub {
  font-size: var(--base-size-small);
  color: var(--color-text-4, #c9cdd4);
}

.tq-row {
  padding: 6px 10px;
  border-bottom: 1px solid var(--color-border-1);
  font-size: 12px;
  transition: background-color 0.15s;
}

.tq-row:hover {
  background: var(--color-fill-2);
}

.tq-row:last-child {
  border-bottom: none;
}

/* 第一行：标签 + 名称 + 错误 + 弹性占位 + 字节数 + 状态 + 操作 */
.tq-line {
  display: flex;
  align-items: center;
  gap: 8px;
}

.tq-name {
  flex: 0 1 260px;
  min-width: 80px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: var(--color-text-1);
}

.tq-flex {
  flex: 1 1 auto;
  min-width: 0;
}

/* 第二行：整宽进度条 */
.tq-bar {
  margin-top: 5px;
}

.tq-num {
  flex: none;
  color: var(--color-text-3);
  text-align: right;
}

.tq-state {
  flex: none;
  width: 64px;
  text-align: right;
  color: var(--color-text-3);
}

.tq-state.s-done {
  color: rgb(var(--success-6));
}

.tq-state.s-failed {
  color: rgb(var(--danger-6));
}

.tq-err {
  display: inline-flex;
  align-items: center;
  gap: 3px;
  flex: 0 1 200px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: rgb(var(--danger-6));
}

.tq-actions {
  flex: none;
}

/* 弱化操作按钮：替代被禁用的 type="text"，常态无底无框，hover 只变字色不加深边框/底色 */
.arco-btn.btn-quiet {
  background: transparent;
  border-color: transparent;
  color: var(--color-text-2);
  padding: 0 4px;
}

.arco-btn.btn-quiet:hover {
  background: transparent;
  border-color: transparent;
  color: var(--color-text-1);
}

.arco-btn.btn-quiet.arco-btn-status-danger {
  color: rgb(var(--red-6));
}

.arco-btn.btn-quiet.arco-btn-status-danger:hover {
  background: transparent;
  border-color: transparent;
  color: rgb(var(--red-5));
}
</style>
