<template>
  <div class="rec-widget">
    <!-- 等待录制状态同步 -->
    <div v-if="phase === 'starting'" class="row">
      <div class="row-main">
        <a-spin :size="14" />
        <span class="state">录制启动中…</span>
      </div>
    </div>

    <!-- 未开始：已就绪，等点「开始」才真启动录制 -->
    <div v-else-if="phase === 'ready'" class="row">
      <div class="row-main">
        <span class="dot idle"></span>
        <span class="time">00:00</span>
        <span class="state">{{ isRegion ? "区域" : "屏幕" }}录制未开始</span>
      </div>
      <div class="row-btns">
        <a-button status="success" size="small" class="btn-no-drag" @click="cmd('start')">● 开始</a-button>
        <a-tooltip content="放弃录制">
          <a-button size="small" class="btn-no-drag" @click="cmd('cancel')">✕</a-button>
        </a-tooltip>
      </div>
    </div>

    <!-- 录制中 / 已暂停：状态信息靠左、操作按钮靠右，相位切换不拖动左侧 -->
    <div v-else-if="phase === 'recording' || phase === 'paused'" class="row">
      <div class="row-main">
        <span class="dot" :class="phase === 'recording' ? 'blink' : 'paused'"></span>
        <span class="time">{{ fmtTime(elapsed) }}</span>
        <span class="state">{{ phase === "recording" ? (isRegion ? "区域录制中" : "屏幕录制中") : "已暂停" }}</span>
      </div>
      <div class="row-btns">
        <a-button v-if="phase === 'recording'" size="small" class="btn-no-drag" @click="cmd('pause')">暂停</a-button>
        <a-button v-else size="small" class="btn-no-drag" @click="cmd('resume')">继续</a-button>
        <a-button status="danger" size="small" class="btn-no-drag" @click="cmd('stop')">停止</a-button>
        <a-button size="small" class="btn-no-drag" @click="cmd('cancel')">✕</a-button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onUnmounted, ref } from "vue";
import { useRoute } from "vue-router";
import { Events } from "@wailsio/runtime";

const route = useRoute();
const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

type Phase = "starting" | "ready" | "recording" | "paused" | "closed";
const phase = ref<Phase>("starting");
const elapsed = ref(0);

const isRegion = computed(() => String(route.query.mode ?? "screen") === "region");

let offStatus: any = null;

function fmtTime(s: number) {
  const m = Math.floor(s / 60);
  return `${String(m).padStart(2, "0")}:${String(s % 60).padStart(2, "0")}`;
}

function cmd(action: string) {
  try {
    GoTools.VideoCmd(action);
  } catch { /* ignore */ }
  // 本地立即反馈，等待状态事件校准
  if (action === "start" && phase.value === "ready") phase.value = "recording";
  if (action === "pause" && phase.value === "recording") phase.value = "paused";
  if (action === "resume" && phase.value === "paused") phase.value = "recording";
  // 停止不在此关闭：工具窗 finalize 后会统一关闭小窗并恢复主窗口
  if (action === "cancel") { phase.value = "closed"; finish(); }
}

/** 退出小窗：通知 Go 关小窗并恢复工具窗 */
function finish() {
  try {
    GoTools.VideoWidgetDone("");
  } catch {
    window.close();
  }
}

// 接收录制工具窗通过 Go 转发的状态：{state, elapsed, note}（仅控制器状态，保存流程已移至主窗口）
offStatus = Events.On("video:status", (ev: any) => {
  const d = ev?.data ?? ev;
  if (!d || !d.state) return;
  const s = String(d.state);
  if (s === "closed") { phase.value = "closed"; finish(); return; }
  if (s === "ready" || s === "recording" || s === "paused") {
    phase.value = s as Phase;
    if (typeof d.elapsed === "number") elapsed.value = d.elapsed;
  }
});

onUnmounted(() => {
  if (offStatus) { try { offStatus(); } catch { /* ignore */ } }
});
</script>

<style scoped>
.rec-widget {
  width: 100vw;
  height: 100vh;
  /* 背景遵从主题：默认/native 档铺实色 bg-2、纹理档透出窗口固定底 .theme-bg、玻璃档铺磨砂 tint，
     三档联动统一由 styles/theme/tokens.scss 的 .rec-widget 规则接管，此处不写 background
     （scoped 属性选择器会与全局同特异性且注入在后，把联动规则盖掉）。 */
  color: var(--color-text-1);
  display: flex;
  align-items: center;
  /* 背景可拖动小窗（需后端 NonClientRegionSupport） */
  app-region: drag;
  -webkit-app-region: drag;
  user-select: none;
  overflow: hidden;
}

.row {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 8px;
  padding: 0 var(--base-padding);
  width: 100%;
}

/* 状态信息组（左）与按钮组（右）各成一个 flex 子组：左锚起点、右锚终点，
   中间弹性空隙吸收各相位文字/按钮宽度变化，左侧不因 phase 切换而偏移 */
.row-main,
.row-btns {
  display: flex;
  align-items: center;
  gap: 8px;
}
.row-btns {
  flex: none;
}

.btn-no-drag,
:deep(.arco-btn) {
  app-region: no-drag;
  -webkit-app-region: no-drag;
}

.time {
  font-family: var(--code-font-family);
  font-size: var(--base-size-large);
  font-weight: 600;
  /* 录制计时用告警红（语义色，随明暗/主题调色板走），在浅底纹理与暗底上都保持可读 */
  color: var(--app-danger);
  font-variant-numeric: tabular-nums;
}

.state {
  font-size: var(--base-size-small);
  color: var(--color-text-2);
  white-space: nowrap;
}

.dot {
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: #f53f3f;
  flex: none;
}

.dot.paused {
  background: #ffb65a;
}

.dot.idle {
  background: #6b6b72;
}

.dot.blink {
  animation: blink 1s infinite;
}

@keyframes blink {

  0%,
  100% {
    opacity: 1;
  }

  50% {
    opacity: 0.2;
  }
}
</style>
