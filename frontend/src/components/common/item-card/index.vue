<template>
  <!-- 列表实体卡：一条带边框/圆角的卡片，承载"一个可点选的条目"。
       左侧选中短竖条（active 时点亮）+ 一行头部（#status 状态点 | #title 主标题 | #extra 右侧操作）
       + 下方正文（默认插槽，如端口/标签、进度、备注等，内部样式由调用方自持）。
       Nginx 实例列表、任务抽屉条目等"列表里的一项卡片"共用这套外壳。 -->
  <div class="item-card" :class="{ 'is-active': active, 'is-error': error, 'is-static': !clickable }"
    :style="{ padding: pad }">
    <span class="item-rail"></span>
    <div class="item-body">
      <div class="item-head">
        <span v-if="$slots.status" class="item-status"><slot name="status" /></span>
        <span class="item-title"><slot name="title" /></span>
        <span v-if="$slots.extra" class="item-extra"><slot name="extra" /></span>
      </div>
      <div v-if="$slots.default" class="item-main"><slot /></div>
    </div>
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  /** 选中态：左侧短竖条点亮 + 抬升投影（不改边框/背景） */
  active?: boolean;
  /** 可点选：给出 pointer 光标；纯展示卡片传 false */
  clickable?: boolean;
  /** 错误态：danger 描边（不铺背景） */
  error?: boolean;
  /** 卡片内边距，默认与实例列表一致；紧凑卡片可自行收紧 */
  pad?: string;
}>(), {
  active: false,
  clickable: true,
  error: false,
  pad: "8px 10px 8px 12px",
});
</script>

<style scoped>
.item-card {
  position: relative;
  display: flex;
  padding: 8px 10px 8px 12px;
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-1);
  border-radius: var(--base-radius-default);
  cursor: pointer;
  overflow: hidden;
  transition: box-shadow 0.2s, border-color 0.2s;
}

.item-card.is-static {
  cursor: default;
}

/* hover 只加一点投影，不加深 border-color、不改 background */
.item-card:hover {
  box-shadow: var(--shadow-1-center);
}

/* 选中：不改边框/背景，用按需的投影抬升 + 左侧短竖条表达 */
.item-card.is-active {
  box-shadow: var(--shadow-2-bottom);
}

/* 错误态：只用 danger 描边标识，不铺背景（与选中/hover 一致：状态不改 background） */
.item-card.is-error {
  border-color: rgba(var(--danger-6), 0.55);
}

/* 左侧选中指示条：短、垂直居中、不铺满高度 */
.item-rail {
  position: absolute;
  left: 0;
  top: 50%;
  transform: translateY(-60%);
  width: 3px;
  height: 30%;
  border-radius:var(--base-radius-default);
  background: transparent;
  transition: background 0.2s;
}

.item-card.is-active .item-rail {
  background: rgb(var(--primary-6));
}

.item-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 4px;
}

.item-head {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.item-status {
  flex: none;
  display: inline-flex;
  align-items: center;
}

.item-title {
  flex: 1;
  min-width: 0;
  display: flex;
  align-items: center;
  gap: 6px;
  color: var(--color-text-1);
}

.item-extra {
  flex: none;
  display: inline-flex;
  align-items: center;
  gap: 6px;
}

.item-main {
  min-width: 0;
}
</style>
