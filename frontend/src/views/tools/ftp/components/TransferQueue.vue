<template>
  <div class="tq">
    <div class="tq-head">
      <div class="tq-title">
        <span class="tq-label">传输队列</span>
        <a-tag v-if="running" size="small" color="arcoblue">{{ running }} 进行中</a-tag>
        <a-tag v-if="failed" size="small" color="red">{{ failed }} 失败</a-tag>
        <a-tag v-if="done" size="small" color="green">{{ done }} 完成</a-tag>
        <span v-if="!transfers.length" class="dim">暂无任务</span>
      </div>
      <a-space :size="4">
        <a-button v-if="finished" size="mini" type="text" @click="emit('clear')">清空已完成</a-button>
        <a-button size="mini" type="text" @click="collapsed = !collapsed">
          <template #icon><component :is="collapsed ? 'icon-down' : 'icon-up'" /></template>
        </a-button>
      </a-space>
    </div>

    <div v-show="!collapsed" class="tq-body">
      <div v-for="t in transfers" :key="t.id" class="tq-row">
        <a-tag size="small" :color="t.dir === 'up' ? 'arcoblue' : 'purple'">{{ t.dir === 'up' ? '上传' : '下载' }}</a-tag>
        <a-tooltip :content="t.dir === 'up' ? `${t.local} → ${t.remote}` : `${t.remote} → ${t.local}`">
          <span class="tq-name">{{ t.name }}</span>
        </a-tooltip>
        <a-progress class="tq-bar" size="mini" :percent="percent(t)" :status="progressStatus(t)" :show-text="false" />
        <span class="tq-num">{{ percentText(t) }}</span>
        <span class="tq-num">{{ t.state === 'running' ? fmtSpeed(t.speed) : stateText(t.state) }}</span>
        <a-tooltip v-if="t.err" :content="t.err">
          <span class="tq-err">{{ t.err }}</span>
        </a-tooltip>
        <span v-else class="tq-err" />
        <a-space :size="2" class="tq-actions">
          <a-button v-if="t.state === 'queued' || t.state === 'running'" size="mini" type="text" status="danger"
            @click="emit('cancel', t.id)">取消</a-button>
          <a-button v-else-if="t.state === 'failed' || t.state === 'canceled'" size="mini" type="text"
            @click="emit('retry', t.id)">重试</a-button>
        </a-space>
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

function percent(t: FtpTransfer): number {
  if (t.total > 0) return Math.min(100, Math.round((t.done / t.total) * 100));
  return t.state === 'done' ? 100 : 0;
}

function percentText(t: FtpTransfer): string {
  if (t.total > 0) return `${fmtSize(t.done)} / ${fmtSize(t.total)}`;
  if (t.state === 'done') return '完成';
  return '—';
}

function progressStatus(t: FtpTransfer): 'normal' | 'success' | 'danger' {
  if (t.state === 'done') return 'success';
  if (t.state === 'failed') return 'danger';
  return 'normal';
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
  border-top: 1px solid var(--color-border-1);
  background: var(--color-bg-2);
}

.tq-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 5px 8px;
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

.dim {
  color: var(--color-text-3);
  font-size: 12px;
}

.tq-body {
  max-height: 156px;
  overflow: auto;
  border-top: 1px solid var(--color-border-1);
}

.tq-row {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 8px;
  border-bottom: 1px solid var(--color-border-1);
  font-size: 12px;
}

.tq-row:last-child {
  border-bottom: none;
}

.tq-name {
  flex: 0 1 220px;
  min-width: 80px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tq-bar {
  flex: 1;
  min-width: 80px;
  margin: 0;
}

.tq-num {
  flex: none;
  width: 108px;
  color: var(--color-text-3);
  text-align: right;
}

.tq-err {
  flex: 0 1 180px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  color: rgb(var(--danger-6));
}

.tq-actions {
  flex: none;
}
</style>
