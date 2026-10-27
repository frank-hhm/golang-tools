<template>
  <!-- 指标卡：彩色圆角图标 + 标签/数值同排 + 下方明细。
       首页 / Nginx 状态 / Docker 总览 / 磁盘总览共用同一版式，数据由调用方经 props 传入。
       图标默认走 common-stat-icon（自绘 SVG，可随 value 做数据驱动动画）；
       需要换成别的图形时用 #icon 插槽自行填。未就绪（没取到数据）时虚线收边 + 压暗。 -->
  <div class="stat-card" :class="{ 'is-off': !ready }">
    <div class="stat-circle" :class="'tone-' + tone">
      <slot name="icon">
        <common-stat-icon v-if="icon" :name="icon" :value="iconValue" :size="iconSize" :label="label" />
      </slot>
    </div>
    <div class="stat-info">
      <div class="stat-head">
        <span class="stat-label">{{ label }}</span>
        <span class="stat-big">
          <slot name="value">{{ value }}</slot>
          <span v-if="unit" class="stat-unit">{{ unit }}</span>
        </span>
      </div>
      <a-tooltip :content="tip || detail" :disabled="!tip">
        <div class="stat-detail">{{ detail || '\u00a0' }}</div>
      </a-tooltip>
    </div>
  </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
  /** common-stat-icon 图标名（cpu/mem/uptime/go/disk/net/container/…）；不传则需自带 #icon 插槽 */
  icon?: string;
  /** 传给自绘图标的进度值 0-100（CPU 脉冲频率 / 内存液位 / 磁盘进度） */
  iconValue?: number;
  /** 自绘图标像素尺寸 */
  iconSize?: number;
  /** 图标底色语义：blue / cyan / purple / orange / green / red / gray */
  tone?: string;
  /** 左上标签 */
  label: string;
  /** 右上大号数值 */
  value?: string | number;
  /** 数值后的小号单位（如 GB / %） */
  unit?: string;
  /** 下方明细小字 */
  detail?: string;
  /** 明细悬停提示；不传则不加 tooltip */
  tip?: string;
  /** 数据是否就绪；false 时虚线收边 + 压暗，表示"没取到"而不是真的 0 */
  ready?: boolean;
}>(), {
  icon: "",
  iconValue: undefined,
  iconSize: 24,
  tone: "blue",
  value: "",
  unit: "",
  detail: "",
  tip: "",
  ready: true,
});
</script>

<style scoped>
.stat-card {
  display: flex;
  align-items: center;
  gap: var(--base-padding);
  height: 100%;
  padding: var(--base-padding);
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-1);
  border-radius: var(--base-radius-default);
  transition: box-shadow 0.2s;
}

.stat-card:hover {
  box-shadow: var(--shadow-2-bottom);
}

/* 没取到数据：虚线描边 + 压暗，一眼看出不是真的 0 */
.stat-card.is-off {
  opacity: 0.62;
  border-style: dashed;
}

.stat-circle {
  width: 44px;
  height: 44px;
  border-radius: var(--base-radius-default);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.tone-blue {
  background: rgba(var(--arcoblue-6), 0.08);
  color: rgb(var(--arcoblue-6));
}

.tone-cyan {
  background: rgba(var(--cyan-6), 0.08);
  color: rgb(var(--cyan-6));
}

.tone-purple {
  background: rgba(var(--purple-6), 0.08);
  color: rgb(var(--purple-6));
}

.tone-orange {
  background: rgba(var(--orange-6), 0.08);
  color: rgb(var(--orange-6));
}

.tone-green {
  background: rgba(var(--green-6), 0.08);
  color: rgb(var(--green-6));
}

.tone-red {
  background: rgba(var(--red-6), 0.08);
  color: rgb(var(--red-6));
}

.tone-gray {
  background: var(--color-fill-2);
  color: var(--color-text-3);
}

.stat-info {
  flex: 1;
  min-width: 0;
}

.stat-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.stat-label {
  font-size: var(--base-size-small);
  color: var(--color-text-3);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.stat-big {
  font-weight: bold;
  font-size: var(--base-size-default);
  line-height: 1.2;
  color: var(--color-text-1);
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}

.stat-unit {
  font-size: var(--base-size-small);
  font-weight: 400;
  color: var(--color-text-3);
  margin-left: var(--base-radius-small);
}

.stat-detail {
  font-size: var(--base-size-small);
  color: var(--color-text-4);
  margin-top: calc(var(--base-margin) / 2);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>
