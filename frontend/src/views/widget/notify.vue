<template>
  <div class="notify-wrap">
    <div class="notify-card" :class="level">
      <!-- 头部：应用图标 + 名称 + ⋯ +  -->
      <div class="nc-head">
        <span class="nc-app-ic">G</span>
        <span class="nc-app">Tools</span>
        <a-tooltip content="更多">
          <span class="nc-more btn-no-drag">⋯</span>
        </a-tooltip>
        <a-tooltip content="关闭">
          <span class="nc-close btn-no-drag" @click="close">✕</span>
        </a-tooltip>
      </div>
      <!-- 正文：标题 + 内容 -->
      <div class="nc-title">{{ title }}</div>
      <div class="nc-content">{{ content }}</div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance } from "vue";
import { useRoute } from "vue-router";

const route = useRoute();
const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const title = computed(() => String(route.query.title ?? "通知"));
const content = computed(() => String(route.query.content ?? ""));
// 预留等级样式（info 默认，success/warning/error 可后续扩展）
const level = computed(() => String(route.query.level ?? "info"));

function close() {
  try {
    GoTools?.NotifyClose();
  } catch {
    window.close();
  }
}
</script>

<style scoped>
.notify-wrap {
  width: 100vw;
  height: 100vh;
  box-sizing: border-box;
  padding: var(--base-padding);
  background: transparent;
  /* 背景可拖动通知卡 */
  app-region: drag;
  -webkit-app-region: drag;
  user-select: none;
  overflow: hidden;
}
.notify-card {
  width: 100%;
  height: 100%;
  box-sizing: border-box;
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-2);
  border-radius: var(--base-radius-large);
  box-shadow: 0 6px 24px rgba(0, 0, 0, 0.14);
  padding: var(--base-padding);
  display: flex;
  flex-direction: column;
}
.nc-head {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-bottom: var(--base-margin);
}
.nc-app-ic {
  width: 18px;
  height: 18px;
  border-radius: var(--base-radius-default);
  background: linear-gradient(135deg, rgb(var(--arcoblue-6)), rgb(var(--blue-6)));
  color: #fff;
  font-size: var(--base-size-small);
  font-weight: 700;
  display: flex;
  align-items: center;
  justify-content: center;
  flex: none;
}
.nc-app {
  font-size: var(--base-size-small);
  color: var(--color-text-2);
  flex: 1;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.nc-more,
.nc-close {
  flex: none;
  padding: var(--base-radius-small);
  border-radius: var(--base-radius-default);
  font-size: var(--base-size-small);
  color: var(--color-text-2);
  cursor: pointer;
}
.nc-more:hover,
.nc-close:hover {
  background: var(--color-fill-2);
  color: var(--color-text-1);
}
.nc-title {
  font-size: var(--base-size-default);
  font-weight: 600;
  color: var(--color-text-1);
  margin-bottom: var(--base-margin);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.nc-content {
  font-size: var(--base-size-small);
  color: var(--color-text-2);
  line-height: 1.5;
  flex: 1;
  display: -webkit-box;
  -webkit-line-clamp: 3;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.btn-no-drag {
  app-region: no-drag;
  -webkit-app-region: no-drag;
}
</style>
