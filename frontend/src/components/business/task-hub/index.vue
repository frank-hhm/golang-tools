<template>
  <!-- 全局任务浮标：左下角常驻，跟在哪个页面无关 -->
  <div v-if="st.hasTasks" class="task-hub">
    <div class="th-fab" :class="{ 'is-bad': isBad }" @click="open = true">
      <a-spin v-if="st.runningCount" :size="14" />
      <icon-close-circle-fill v-else-if="isBad" class="th-fab-bad" />
      <icon-check-circle-fill v-else class="th-fab-ok" />
      <div class="th-fab-body">
        <div class="th-fab-line">
          <span class="th-fab-text">{{ fabText }}</span>
          <span v-if="st.runningCount" class="th-fab-pct">{{ topPercent }}%</span>
        </div>
        <div v-if="st.runningCount" class="th-fab-bar">
          <i :style="{ width: topPercent + '%' }"></i>
        </div>
        <div v-else class="th-fab-sub">{{ topTask?.title }}</div>
      </div>
      <icon-right class="th-fab-arrow" />
    </div>
  </div>

  <a-drawer v-model:visible="open" :width="430" :footer="false" placement="right" :unmount-on-close="true">
    <template #title>
      <div class="th-drawer-title">
        <span>安装任务</span>
        <span class="th-drawer-sub">
          {{ st.runningCount ? `${st.runningCount} 个进行中` : "没有进行中的任务" }}
          <template v-if="st.failed.length">· {{ st.failed.length }} 个失败</template>
        </span>
      </div>
    </template>

    <!-- 面板模式下浏览器是唯一的通知出口：权限要用户点一下才给 -->
    <div v-if="needNotifyHint" class="th-hint">
      <span>任务完成后可在系统通知栏提醒（浏览器当前未授权）</span>
      <a-button size="mini" type="outline" @click="askNotify">开启通知</a-button>
    </div>

    <a-empty v-if="list.length === 0" description="暂无任务：装版本、拉镜像时这里会显示进度" />

    <div v-for="t in list" :key="t.id" class="th-item" :class="'is-' + t.status">
      <div class="th-head">
        <span class="th-tool">{{ toolLabel(t.tool) }}</span>
        <span class="th-title" :title="t.title">{{ t.title }}</span>
        <a-tag size="small" :color="tagColor(t)">{{ statusText(t) }}</a-tag>
      </div>

      <a-progress :percent="pct(t) / 100" size="small" :show-text="false"
        :status="t.status === 'error' ? 'danger' : t.status === 'done' ? 'success' : 'normal'" />

      <div class="th-note" :title="t.note">{{ t.note || stageText(t.stage) || "—" }}</div>

      <div class="th-foot">
        <span class="th-time">{{ t.status === "running" ? since(t.startedAt) + " 前开始" : since(t.updatedAt) + " 前" }}</span>
        <a-link class="th-link" @click="goTool(t)">打开页面</a-link>
        <a-link class="th-link" @click="toggleLog(t.id)">{{ logsOf.has(t.id) ? "收起" : "日志" }}</a-link>
        <a-link v-if="t.status !== 'running'" class="th-link" @click="st.dismiss(t.id)">移除</a-link>
      </div>

      <div v-if="logsOf.has(t.id)" class="th-logs">
        <div v-for="(l, i) in logsOf.get(t.id)" :key="i" class="th-log-line">{{ l }}</div>
        <div v-if="!logsOf.get(t.id)?.length" class="th-log-line is-dim">暂无进度明细</div>
      </div>
    </div>

    <template v-if="finishedCount > 0">
      <div class="th-clear">
        <a-button size="mini" long @click="st.clearFinished()">清理已结束（{{ finishedCount }}）</a-button>
      </div>
    </template>
  </a-drawer>
</template>

<script setup lang="ts">
/**
 * 全局任务浮标 + 抽屉。
 *
 * 安装/下载任务在 Go 侧是独立 goroutine，页面卸载不影响它继续跑；这个组件挂在
 * 主窗口布局上，所以切到设置页、切工具、甚至关掉工具窗口，都能在这里看到
 * "还在装什么、装到哪了"，点开还能回原页面。
 *
 * 通知：桌面版由 Go 弹 Windows 系统原生通知（右下角 Toast）；面板/浏览器版走 Notification（见 store）。
 */
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";
import type { Task } from "bindings/go-tools/services/tasks/index";
import { ensureTasks, useTasksStoreHook } from "@/store/modules/tasks";
import { isNativeWindows } from "@/utils/caps";

const st = useTasksStoreHook();
const router = useRouter();

/** 相对时间需要“现在”：每秒拨一下，浮标上的“x 秒前开始”才是活的 */
const now = ref(Date.now());
let timer: number | null = null;

function startTimer() {
  stopTimer();
  now.value = Date.now();
  timer = window.setInterval(() => { now.value = Date.now(); }, 1000);
}
function stopTimer() {
  if (timer != null) { clearInterval(timer); timer = null; }
}

onMounted(() => {
  void ensureTasks();
});
onUnmounted(stopTimer);

const open = ref(false);
/** 展开日志的任务 id（默认全收起，避免抽屉一开就是一大坨） */
const logsOf = reactive(new Map<string, string[]>());

/** 打开抽屉就算“看过了”：未读状态下浮标才是红的（不把失败永久钉在那里） */
watch(open, (v) => { if (v) st.markSeen(); });
// 抽屉开着的时候任务结束：用户正看着列表，不算未读
watch(() => st.unseenCount, () => { if (open.value) st.markSeen(); });

/**
 * 每秒拨表只为相对时间（“x 分钟前”）。没任务在跑、抽屉也没开时没人看这些数字，
 * 停掉定时器——它挂在主窗布局上，主窗口隐藏到托盘后仍在空转，白占（机器吃紧时
 * 每秒一次的重渲染也不划算）。任务结束时补拨一次，保证最后显示的那一屏是准的。
 */
watch(
  [() => st.runningCount, open],
  ([running, drawer]) => {
    if (running > 0 || drawer) startTimer();
    else stopTimer();
  },
  { immediate: true },
);

const list = computed(() => st.tasks);
const finishedCount = computed(() => st.tasks.filter((t) => t.status !== "running").length);
/** 浮标上那句主文案取"最近更新"的那笔进行中任务；没在跑就取最新一条未读结果 */
const topTask = computed<Task | undefined>(() => {
  if (st.runningCount) {
    return [...st.running].sort((a, b) => b.updatedAt - a.updatedAt)[0];
  }
  return [...st.unseen].sort((a, b) => b.updatedAt - a.updatedAt)[0];
});
const topPercent = computed(() => Math.round(pct(topTask.value)));
/** 未读里有失败才标红，免得一个早已看过的失败把浮标永远染红 */
const isBad = computed(() => !st.runningCount && st.unseen.some((t) => t.status === "error"));
const fabText = computed(() => {
  if (st.runningCount) return st.runningCount > 1 ? `${st.runningCount} 个任务进行中` : "正在安装";
  const bad = st.unseen.filter((t) => t.status === "error").length;
  if (bad) return `${bad} 个任务失败`;
  return `${st.unseenCount} 个任务已完成`;
});

/** 面板模式下浏览器通知没授权才提示（桌面版 Go 自己弹，不打扰；已拒绝的也留个入口） */
const needNotifyHint = computed(() => !isNativeWindows() && (st.notifyPerm === "default" || st.notifyPerm === "denied"));

async function askNotify() {
  await st.enableNotify();
}

function pct(t?: Task): number {
  if (!t) return 0;
  if (t.status === "done") return 100;
  return Math.max(0, Math.min(100, Math.round(t.percent || 0)));
}

const STAGE_TEXT: Record<string, string> = {
  info: "准备中",
  downloading: "下载中",
  extracting: "解压中",
  installing: "安装中",
  starting: "启动中",
  stalled: "等待响应",
  done: "完成",
  error: "失败",
  canceled: "已取消",
};

function stageText(stage?: string): string {
  return STAGE_TEXT[String(stage || "")] || "";
}

function statusText(t: Task): string {
  if (t.status === "done") return "完成";
  if (t.status === "error") return "失败";
  if (t.status === "canceled") return "已取消";
  return stageText(t.stage) || "进行中";
}

function tagColor(t: Task): string {
  if (t.status === "error") return "red";
  if (t.status === "done") return "green";
  if (t.status === "canceled") return "gray";
  return "arcoblue";
}

const TOOL_LABEL: Record<string, string> = {
  docker: "Docker",
  mysql: "MySQL",
  php: "PHP",
  redis: "Redis",
  nginx: "Nginx",
  npm: "npm",
};

function toolLabel(tool: string): string {
  return TOOL_LABEL[tool] || tool;
}

/** 只切主窗口路由：原生模式下这是主窗口，不影响已经开着的工具窗口 */
function goTool(t: Task) {
  const path = `/tools/${t.tool}`;
  open.value = false;
  router.push(path).catch(() => { /* 同名路由重复点击 */ });
}

function since(ts: number): string {
  if (!ts) return "";
  const s = Math.max(0, Math.floor((now.value - ts) / 1000));
  if (s < 60) return `${s} 秒`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m} 分${s % 60} 秒`;
  const h = Math.floor(m / 60);
  return `${h} 小时${m % 60} 分`;
}

function toggleLog(id: string) {
  if (logsOf.has(id)) { logsOf.delete(id); return; }
  const t = st.tasks.find((x) => x.id === id);
  logsOf.set(id, (t?.logs || []).slice(-80));
}
</script>

<style scoped>
/* 浮标：左下角，避开 docker 页右下角的镜像任务浮标 */
.task-hub {
  position: fixed;
  left: calc(var(--side-nav-w, 0px) + 18px);
  bottom: 18px;
  z-index: 1500;
}

.th-fab {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 208px;
  max-width: 320px;
  padding: 8px 10px;
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: var(--base-radius-large);
  box-shadow: 0 6px 20px rgba(0, 0, 0, 0.12);
  cursor: pointer;
  user-select: none;
  transition: box-shadow 0.18s ease, transform 0.18s ease;
}

.th-fab:hover {
  box-shadow: 0 8px 26px rgba(0, 0, 0, 0.2);
  transform: translateY(-1px);
}

.th-fab.is-bad {
  border-color: rgb(var(--danger-6));
}

.th-fab-bad {
  color: rgb(var(--danger-6));
  font-size: 15px;
}

.th-fab-ok {
  color: rgb(var(--success-6));
  font-size: 15px;
}

.th-fab-body {
  flex: 1;
  min-width: 0;
}

.th-fab-line {
  display: flex;
  align-items: center;
  gap: 6px;
}

.th-fab-text {
  flex: 1;
  font-size: var(--base-size-small);
  color: var(--color-text-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.th-fab-pct {
  font-size: var(--base-size-small);
  color: var(--color-text-2);
  font-variant-numeric: tabular-nums;
}

.th-fab-bar {
  margin-top: 5px;
  height: 3px;
  border-radius: 2px;
  background: var(--color-fill-2);
  overflow: hidden;
}

.th-fab-bar i {
  display: block;
  height: 100%;
  background: rgb(var(--primary-6));
  transition: width 0.3s ease;
}

.th-fab-sub {
  margin-top: 3px;
  font-size: 11px;
  color: var(--color-text-3);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.th-fab-arrow {
  color: var(--color-text-3);
  font-size: 12px;
}

.th-drawer-title {
  display: flex;
  align-items: baseline;
  gap: 8px;
}

.th-drawer-sub {
  font-size: var(--base-size-small);
  color: var(--color-text-3);
  font-weight: 400;
}

.th-hint {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  margin-bottom: var(--base-margin);
  padding: 6px 10px;
  font-size: var(--base-size-small);
  color: var(--color-text-2);
  background: rgb(var(--arcoblue-1));
  border-radius: var(--base-radius-default);
}

.th-item {
  padding: 10px;
  margin-bottom: 10px;
  border: 1px solid var(--color-border-2);
  border-radius: var(--base-radius-default);
  background: var(--color-bg-2);
}

.th-item.is-error {
  border-color: rgb(var(--danger-6));
}

.th-head {
  display: flex;
  align-items: center;
  gap: 6px;
  margin-bottom: 8px;
}

.th-tool {
  flex: none;
  font-size: 11px;
  color: var(--color-text-3);
  padding: 1px 5px;
  border-radius: var(--base-radius-small);
  background: var(--color-fill-2);
}

.th-title {
  flex: 1;
  min-width: 0;
  font-size: var(--base-size-default);
  color: var(--color-text-1);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.th-note {
  margin-top: 6px;
  font-size: var(--base-size-small);
  color: var(--color-text-2);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

.th-foot {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 6px;
  font-size: 11px;
  color: var(--color-text-3);
}

.th-time {
  flex: 1;
  font-variant-numeric: tabular-nums;
}

.th-link {
  font-size: 11px;
}

.th-logs {
  margin-top: 8px;
  max-height: 180px;
  overflow-y: auto;
  padding: 6px 8px;
  background: var(--color-fill-1);
  border-radius: var(--base-radius-default);
  font-family: var(--gt-font-mono, ui-monospace, SFMono-Regular, Menlo, Consolas, monospace);
  font-size: 11px;
  line-height: 1.6;
}

.th-log-line {
  color: var(--color-text-2);
  word-break: break-all;
}

.th-log-line.is-dim {
  color: var(--color-text-4);
}

.th-clear {
  margin-top: 4px;
}
</style>
