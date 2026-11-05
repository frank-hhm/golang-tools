<template>
  <!--
    m-card：项目统一自绘卡片外壳，不依赖 a-card 组件，也不复用 arco-card* 类名（全站后续都走 m-card）。
    DOM 结构与 API 搬照 a-card，但用一套自己的 m-card* 命名与自带样式；玻璃皮肤与 card-head-tool
    约定已在 tokens.scss / style.scss 里同步挂上 .m-card，视觉与旧 a-card 对齐。

    API（对齐 a-card）：
      props  title / extra / bordered / hoverable / loading / size / headerStyle / bodyStyle
      slots  title / extra / cover / default / actions
      events 无（a-card 本身不 emit）

    背景系统约定：inheritBg 控制「是否继承上级背景」。
      - false（默认）：普通卡片，自身铺底；开玻璃时吃磨砂 tint。
      - true：卡片不铺底、不吃玻璃磨砂、描边/投影透明，让父级容器与全局主题背景直接透过来。
    后续新增模板组件都应带上「是否继承上级背景」这一维度，背景才能在各层统一穿透。
  -->
  <div :class="cardClass">
    <div v-if="hasHeader" :class="headerClass" :style="headerStyle">
      <div v-if="hasTitle" class="m-card-header-title">
        <slot name="title">{{ title }}</slot>
      </div>
      <div v-if="hasExtra" class="m-card-header-extra">
        <slot name="extra">{{ extra }}</slot>
      </div>
    </div>

    <div v-if="$slots.cover" class="m-card-cover">
      <slot name="cover" />
    </div>

    <div class="m-card-body" :style="bodyStyle">
      <a-spin v-if="loading" />
      <template v-else>
        <slot />
        <div v-if="$slots.actions" class="m-card-actions">
          <div class="m-card-actions-right">
            <slot name="actions" />
          </div>
        </div>
      </template>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, useSlots } from "vue";
import type { CSSProperties } from "vue";

const props = withDefaults(
  defineProps<{
    /** 标题（字符串版，也可用 #title 插槽覆盖） */
    title?: string;
    /** 头部右侧内容（字符串版，也可用 #extra 插槽覆盖） */
    extra?: string;
    /** 是否显示边框 */
    bordered?: boolean;
    /** 悬停时抬升投影 */
    hoverable?: boolean;
    /** 加载态：正文区显示居中 Spin */
    loading?: boolean;
    /** 尺寸：small 收紧内边距与头部高度 */
    size?: "medium" | "small";
    /** 头部行内样式 */
    headerStyle?: CSSProperties;
    /** 正文行内样式 */
    bodyStyle?: CSSProperties;
    /** 是否继承上级背景：true 时卡片透明、不铺底、不吃玻璃磨砂，露出父级/全局主题背景 */
    inheritBg?: boolean;
  }>(),
  {
    bordered: true,
    hoverable: false,
    loading: false,
    inheritBg: false,
  },
);

const slots = useSlots();

// a-card 用 useSize 合并全局尺寸，这里只保留 small / medium 两档语义
const mergedSize = computed(() => (props.size === "small" ? "small" : "medium"));
const hasTitle = computed(() => !!slots.title || props.title !== undefined);
const hasExtra = computed(() => !!slots.extra || props.extra !== undefined);
const hasHeader = computed(() => hasTitle.value || hasExtra.value);

const cardClass = computed(() => [
  "m-card",
  `m-card-size-${mergedSize.value}`,
  {
    "m-card-loading": props.loading,
    "m-card-bordered": props.bordered,
    "m-card-hoverable": props.hoverable,
    "m-card-inherit": props.inheritBg,
  },
]);

const headerClass = computed(() => [
  "m-card-header",
  { "m-card-header-no-title": !hasTitle.value },
]);
</script>

<style scoped>
.m-card {
  position: relative;
  box-sizing: border-box;
  color: var(--color-text-1);
  background: var(--color-bg-2);
  border-radius: var(--base-radius-default);
  transition: box-shadow 0.2s cubic-bezier(0, 0, 1, 1);
}

.m-card-bordered {
  border: 1px solid var(--color-border-1);
}

.m-card-size-medium,
.m-card-size-small {
  font-size: var(--base-size-large);
}

.m-card-header {
  position: relative;
  display: flex;
  align-items: center;
  justify-content: space-between;
  box-sizing: border-box;
  overflow: hidden;
  min-height: 46px;
  padding: 10px 16px;
  border-bottom: 1px solid var(--color-border-1);
}

.m-card-size-small .m-card-header {
  min-height: 40px;
  padding: 8px 16px;
}

/* 无标题时留一个空块，配合 space-between 把 extra 推到右侧 */
.m-card-header-no-title::before {
  display: block;
  content: " ";
}

.m-card-header-title {
  flex: 1;
  color: var(--color-text-1);
  font-weight: 500;
  font-size: var(--base-size-large-x);
  line-height: 1.5715;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.m-card-header-extra {
  color: var(--color-text-2);
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}

.m-card-cover {
  overflow: hidden;
  border-radius: var(--base-radius-default) var(--base-radius-default) 0 0;
}

.m-card-cover > * {
  display: block;
  width: 100%;
}

.m-card-body {
  padding: 16px;
  color: var(--color-text-2);
}

.m-card-size-small .m-card-body {
  padding: 12px 16px;
}

.m-card-loading .m-card-body {
  overflow: hidden;
  text-align: center;
}

.m-card-actions {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 16px;
}

.m-card-actions-right {
  display: flex;
  align-items: center;
}

.m-card-hoverable:hover {
  box-shadow: var(--shadow-2-bottom);
}
</style>

<!-- 非 scoped：inheritBg 是对全站玻璃皮肤（tokens.scss 里 body.app-glass .m-card）的显式退出，
     需稳定压过带 body 祖先类的高特异性规则，故用 !important 明确「本卡片不铺任何底」。 -->
<style>
.m-card.m-card-inherit {
  background: transparent !important;
  border-color: transparent !important;
  box-shadow: none !important;
  backdrop-filter: none !important;
  -webkit-backdrop-filter: none !important;
}
</style>
