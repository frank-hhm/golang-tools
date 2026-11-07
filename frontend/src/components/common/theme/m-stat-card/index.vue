<template>
  <!--
    m-stat-card：项目统一自绘指标卡外壳（m-* 家族成员，与 m-card / m-button 同一套约定），
    版式承接 common-stat-card：彩色圆角图标 + 标签/数值同排 + 下方明细，
    首页 / Nginx 状态 / Docker 总览 / 磁盘总览这类「一格一个指标」的卡片共用。
    自有 m-stat-* 类名，不复用旧 stat-card* 类；度量全走密度联动变量（--base-padding /
    --base-margin / --base-radius-default / --base-size-*），随主题密度/字号/圆角自动缩放。
    图标默认走 common-stat-icon（自绘 SVG，可随 value 做数据驱动动画）；
    需要换成别的图形时用 #icon 插槽自行填。未就绪（没取到数据）时虚线收边 + 压暗。

    API：
      props  icon / iconValue / iconSize / tone(blue|cyan|purple|orange|green|red|gray)
             label / value / unit / detail / tip / ready / inheritBg
      slots  icon / value
      events 无（点击等交互由调用方经 attrs 透传挂在根元素上）

    背景系统约定（与 m-card 一致）：inheritBg 控制「是否继承上级背景」。
      - false（默认）：普通指标卡，自身铺底；开玻璃时吃磨砂 tint。
      - true：卡片不铺底、不吃玻璃磨砂、描边/投影透明，让父级与全局主题背景透过来。
  -->
  <div class="m-stat-card" :class="{ 'is-off': !ready, 'm-stat-inherit': inheritBg }">
    <div class="m-stat-icon" :class="'tone-' + tone">
      <slot name="icon">
        <common-stat-icon v-if="icon" :name="icon" :value="iconValue" :size="iconSize" :label="label" />
      </slot>
    </div>
    <div class="m-stat-info">
      <div class="m-stat-head">
        <span class="m-stat-label">{{ label }}</span>
        <span class="m-stat-big">
          <slot name="value">{{ value }}</slot>
          <span v-if="unit" class="m-stat-unit">{{ unit }}</span>
        </span>
      </div>
      <a-tooltip :content="tip || detail" :disabled="!tip">
        <div class="m-stat-detail">{{ detail || '\u00a0' }}</div>
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
  /** 图标底色语义：blue / cyan / purple / orange / green / red / gray；
      blue 为品牌蓝 arcoblue，别名 arcoblue 等价（m-svc-card 的 tone-blue 是天蓝 blue-6，要同色传 arcoblue） */
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
  /** 是否继承上级背景：true 时卡片透明、不铺底、不吃玻璃磨砂，露出父级/全局主题背景 */
  inheritBg?: boolean;
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
  inheritBg: false,
});
</script>

<style scoped>
.m-stat-card {
  display: flex;
  align-items: center;
  gap: var(--base-padding);
  padding: var(--base-padding);
  background: var(--color-bg-2);
  border: 1px solid var(--color-border-1);
  border-radius: var(--base-radius-default);
  transition: box-shadow 0.2s;
}

.m-stat-card:hover {
  box-shadow: var(--shadow-2-bottom);
}

/* 没取到数据：虚线描边 + 压暗，一眼看出不是真的 0 */
.m-stat-card.is-off {
  opacity: 0.62;
  border-style: dashed;
}

.m-stat-icon {
  width: 44px;
  height: 44px;
  border-radius: var(--base-radius-default);
  display: flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
}

.tone-blue,
.tone-arcoblue {
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

.m-stat-info {
  flex: 1;
  min-width: 0;
}

.m-stat-head {
  display: flex;
  align-items: baseline;
  justify-content: space-between;
  gap: 8px;
}

.m-stat-label {
  font-size: var(--base-size-small);
  color: var(--color-text-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.m-stat-big {
  font-weight: bold;
  font-size: var(--base-size-default);
  line-height: 1.2;
  color: var(--color-text-1);
  white-space: nowrap;
  font-variant-numeric: tabular-nums;
}

.m-stat-unit {
  font-size: var(--base-size-small);
  font-weight: 400;
  color: var(--color-text-3);
  margin-left: var(--base-radius-small);
}

.m-stat-detail {
  font-size: var(--base-size-small);
  color: var(--color-text-4);
  margin-top: calc(var(--base-margin) / 2);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
</style>

<!-- 非 scoped：inheritBg 是对全站玻璃皮肤（tokens.scss 里 body.app-glass .m-stat-card）的显式退出，
     需稳定压过带 body 祖先类的高特异性规则，故用 !important 明确「本卡片不铺任何底」。 -->
<style>
.m-stat-card.m-stat-inherit {
  background: transparent !important;
  border-color: transparent !important;
  box-shadow: none !important;
  backdrop-filter: none !important;
  -webkit-backdrop-filter: none !important;
}
</style>
