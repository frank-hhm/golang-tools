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

  <a-drawer v-model:visible="open" :width="430" placement="right" :unmount-on-close="true"
    :footer="finishedCount > 0">
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

    <common-item-card v-for="t in list" :key="t.id" class="th-item" :clickable="false"
      :error="t.status === 'error'" pad="10px">
      <template #status>
        <span class="th-dot" :class="'dot-' + t.status"></span>
      </template>
      <template #title>
        <span class="th-tool">{{ toolLabel(t.tool) }}</span>
        <span class="th-title" :title="t.title">{{ t.title }}</span>
      </template>
      <template #extra>
        <a-tag size="small" :color="tagColor(t)">{{ statusText(t) }}</a-tag>
      </template>

      <a-progress v-if="t.status === 'running' || t.status === 'done'" :percent="pct(t) / 100" size="small"
        :show-text="false" :status="t.status === 'done' ? 'success' : 'normal'"
        :class="{ 'is-indeterminate': t.status === 'running' && pct(t) === 0 }" />

      <div class="th-note-wrap">
        <div class="th-note" :class="{ 'is-err': t.status === 'error', 'is-open': openedNotes.has(t.id) }"
          :title="t.note">{{ t.note || stageText(t.stage) || "—" }}</div>
        <a-link v-if="(t.note || '').length > 60" class="th-note-toggle" size="mini"
          @click="toggleNote(t.id)">{{ openedNotes.has(t.id) ? '收起' : '展开' }}</a-link>
      </div>

      <div class="th-foot">
        <span class="th-time">{{ t.status === "running" ? since(t.startedAt) + " 前开始" : since(t.updatedAt) + " 前" }}</span>
        <a-link class="th-link" @click="goTool(t)">
          <icon-location />打开页面
        </a-link>
        <a-link class="th-link" @click="toggleLog(t.id)">
          <component :is="logsOf.has(t.id) ? IconUp : IconDown" />
          {{ logsOf.has(t.id) ? '收起日志' : '日志' }}
        </a-link>
        <a-link v-if="t.status !== 'running'" class="th-link th-link-del" @click="st.dismiss(t.id)">
          <icon-delete />移除
        </a-link>
      </div>

      <div v-if="logsOf.has(t.id)" class="th-logs">
        <div v-for="(l, i) in logsOf.get(t.id)" :key="i" class="th-log-line">{{ l }}</div>
        <div v-if="!logsOf.get(t.id)?.length" class="th-log-line is-dim">暂无进度明细</div>
      </div>
    </common-item-card>

    <!-- 有已结束任务才亮出底栏；按钮留在文档流里，抽屉会自动隐藏空白 footer -->
    <template #footer>
      <a-space fill>
        <a-button @click="st.clearFinished()">清理已结束（{{ finishedCount }}）</a-button>
      </a-space>
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
import { computed, getCurrentInstance, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { Events } from "@wailsio/runtime";
import { IconDown, IconUp } from "@arco-design/web-vue/es/icon";
import type { Task } from "bindings/Tools/services/tasks/index";
import { ensureTasks, useTasksStoreHook } from "@/store/modules/tasks";
import { isNativeWindows } from "@/utils/caps";
import { useToolWindow } from "@/views/index/useToolWindow";

const st = useTasksStoreHook();
const { openToolWindow } = useToolWindow();
const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
/** 工具 key → 原生新窗口选项（single/尺寸…）：首次点「打开页面」时从配置懒加载一次 */
const toolWinMeta = ref<Record<string, any>>({});
let toolWinMetaLoaded = false;

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
/** 展开长备注（失败原因）的任务 id：默认两行截断，全文靠这里展开 */
const openedNotes = reactive(new Set<string>());

function toggleNote(id: string) {
  if (openedNotes.has(id)) openedNotes.delete(id);
  else openedNotes.add(id);
}

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

/**
 * 任务种类 → 目标页需要额外弹出的面板：装版本 / 装扩展 都落到「版本管理」弹窗看进度；
 * setup / pull / op 直接进工具主页即可（Docker 引擎向导、镜像任务中心都在页内常驻）。
 */
function panelOfKind(kind: string): string {
  return kind === "install" || kind === "ext" ? "versions" : "";
}

/**
 * 点「打开页面」：另开对应服务的工具窗口（与主页点击服务卡一致），而不是把主窗口 itself 导航走。
 * openToolWindow 在原生模式开独立单例窗口、Web 模式自动回退成页面内路由。
 * 装版本/扩展类任务再带 ?open=versions / 发 tool:open-panel，让目标页弹出「版本管理」。
 */
async function goTool(t: Task) {
  open.value = false;
  const meta = await resolveToolWin(t.tool);
  const panel = panelOfKind(t.kind);
  const path = `/tools/${t.tool}` + (panel ? `?open=${panel}` : "");
  openToolWindow(path, meta?.name || toolLabel(t.tool), {
    single: meta?.single ?? true,
    fullscreen: !!meta?.fullscreen,
    disableFullscreen: !!meta?.disableFullscreen,
    width: meta?.width,
    height: meta?.height,
  });
  // 单例窗口已开着时，?open= 不会重新触发挂载：再发一次事件让它弹出面板（与 tool:select-inst 同理）
  if (panel) {
    try { Events.Emit("tool:open-panel", { type: t.tool, panel }); } catch { /* */ }
  }
}

/** 取该工具的窗口元数据（名称/单例/尺寸）；配置只读一次，失败回空由调用方兜底 */
async function resolveToolWin(tool: string) {
  if (!toolWinMetaLoaded) {
    try {
      const cfg = await GoTools?.GetConfig();
      const map: Record<string, any> = {};
      for (const x of cfg?.tools || []) {
        if (x?.key) map[x.key] = {
          name: x.name, single: !!x.single, fullscreen: !!x.fullscreen,
          disableFullscreen: !!x.disableFullscreen, width: x.width, height: x.height,
        };
      }
      toolWinMeta.value = map;
    } catch (e) { /* 拿不到配置就用兜底值 */ }
    toolWinMetaLoaded = true;
  }
  return toolWinMeta.value[tool];
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

/** 对外入口：标题栏的下载按钮经 ref 调用，与点浮标等效 */
defineExpose({ open: () => { open.value = true; } });
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
  border: 1px solid var(--color-border-1);
  border-radius: var(--base-radius-default);
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
  animation: th-fab-breathe 2.4s ease-in-out infinite;
}

/* 失败浮标轻微呼吸：只动光晕不动尺寸，提醒但不骚扰 */
@keyframes th-fab-breathe {
  0%,
  100% {
    box-shadow: 0 6px 20px rgba(0, 0, 0, 0.12);
  }

  50% {
    box-shadow: 0 6px 20px rgba(0, 0, 0, 0.12), 0 0 0 4px rgba(var(--danger-6), 0.14);
  }
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

/* 卡片外壳（边框/圆角/背景/错误态描边）由 common-item-card 提供，这里只留条目间距 */
.th-item {
  margin-bottom: 10px;
}

/* 状态点：颜色说话，失败红点带一圈光晕，一眼扫出哪条出了问题 */
.th-dot {
  flex: none;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--color-text-4);
}

.dot-running {
  background: rgb(var(--primary-6));
  animation: th-dot-pulse 1.4s ease-in-out infinite;
}

.dot-done {
  background: rgb(var(--success-6));
}

.dot-error {
  background: rgb(var(--danger-6));
  box-shadow: 0 0 0 3px rgba(var(--danger-6), 0.16);
}

.dot-canceled {
  background: var(--color-text-4);
}

@keyframes th-dot-pulse {
  0%,
  100% {
    box-shadow: 0 0 0 0 rgba(var(--primary-6), 0.35);
  }

  50% {
    box-shadow: 0 0 0 4px rgba(var(--primary-6), 0);
  }
}

/* 下载中但还没收到百分比：进度条画成无限滑动的亮带，比停在 0% 的灰条诚实 */
.is-indeterminate :deep(.arco-progress-line-bar) {
  position: relative;
  width: 36% !important;
  animation: th-slide 1.3s ease-in-out infinite;
}

@keyframes th-slide {
  0% {
    left: -36%;
  }

  100% {
    left: 100%;
  }
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

.th-note-wrap {
  margin-top: 6px;
  display: flex;
  align-items: flex-start;
  gap: 6px;
}

/* 备注默认两行截断：失败原因再长也不把卡片撑爆，悬停看全文、展开看完整 */
.th-note {
  flex: 1;
  min-width: 0;
  font-size: var(--base-size-small);
  color: var(--color-text-2);
  line-height: 1.6;
  display: -webkit-box;
  -webkit-box-orient: vertical;
  -webkit-line-clamp: 2;
  line-clamp: 2;
  overflow: hidden;
  word-break: break-all;
}

.th-note.is-open {
  -webkit-line-clamp: unset;
  line-clamp: unset;
  display: block;
}

.th-note.is-err {
  color: var(--color-text-1);
  padding: 4px 8px;
  border-left: 2px solid rgb(var(--danger-6));
  border-radius: 0 var(--base-radius-small) var(--base-radius-small) 0;
  background: rgba(var(--danger-6), 0.08);
}

.th-note-toggle {
  flex: none;
  font-size: 11px;
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
  display: inline-flex;
  align-items: center;
  gap: 3px;
  flex: none;
}

.th-link :deep(.arco-icon) {
  font-size: 12px;
}

.th-link-del {
  color: var(--color-text-3);
}

.th-link-del:hover {
  color: rgb(var(--danger-6));
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

</style>
