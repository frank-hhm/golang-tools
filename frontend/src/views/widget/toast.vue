<template>
  <div class="toast" :class="level">
    <span class="ic">{{ iconChar }}</span>
    <div class="msg">{{ text }}</div>
    <span class="close" @click="close">✕</span>
    <div class="progress" :style="{ animationDuration: dur + 'ms' }"></div>
  </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, onUnmounted } from "vue";
import { useRoute } from "vue-router";

const route = useRoute();
const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const level = computed(() => String(route.query.level ?? "info"));
const text = computed(() => String(route.query.text ?? ""));
const dur = 4000; // 与进度条动画时长一致

const iconMap: Record<string, string> = { info: "ℹ", success: "✔", warning: "⚠", error: "✖" };
const iconChar = computed(() => iconMap[level.value] ?? "ℹ");

let timer = 0;

function close() {
  try {
    GoTools?.ToastClose();
  } catch {
    window.close();
  }
}

onMounted(() => {
  timer = window.setTimeout(close, dur);
});
onUnmounted(() => {
  if (timer) clearTimeout(timer);
});
</script>

<style scoped>
.toast {
  position: relative;
  width: 100vw;
  height: 100vh;
  box-sizing: border-box;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 0 var(--base-padding);
  background: #fff;
  border-radius: var(--base-radius-large);
  border: 1px solid #e5e6eb;
  box-shadow: 0 4px 16px rgba(0, 0, 0, 0.12);
  overflow: hidden;
  user-select: none;
  /* 无边框窗口内可拖动 */
  app-region: drag;
  -webkit-app-region: drag;
}
.ic {
  font-size: var(--base-size-large);
  font-weight: 700;
  flex: none;
}
.msg {
  flex: 1;
  font-size: var(--base-size-small);
  color: #1d2129;
  line-height: 1.4;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.close {
  flex: none;
  color: #86909c;
  cursor: pointer;
  font-size: var(--base-size-small);
  padding: var(--base-radius-small);
  app-region: no-drag;
  -webkit-app-region: no-drag;
}
.close:hover { color: #1d2129; }

/* 等级配色（对齐 Arco Message） */
.info .ic { color: rgb(var(--blue-6)); }
.success .ic { color: rgb(var(--green-6)); }
.warning .ic { color: rgb(var(--orange-6)); }
.error .ic { color: rgb(var(--red-6)); }
.info .progress { background: rgb(var(--blue-6)); }
.success .progress { background: rgb(var(--green-6)); }
.warning .progress { background: rgb(var(--orange-6)); }
.error .progress { background: rgb(var(--red-6)); }

.progress {
  position: absolute;
  left: 0;
  bottom: 0;
  height: 3px;
  width: 100%;
  transform-origin: left;
  animation: shrink linear forwards;
}
@keyframes shrink {
  from { transform: scaleX(1); }
  to { transform: scaleX(0); }
}
</style>
