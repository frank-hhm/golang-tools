<template>
  <!-- 指标卡自绘图标：stroke 全走 currentColor，配色由外层 .stat-circle-* 的 color 决定，
       因此浅色/暗黑切换不需要额外处理。动画只用 transform / opacity / stroke-dasharray 类属性。 -->
  <svg class="stat-icon" :class="'si-' + name" :width="size" :height="size" viewBox="0 0 24 24" fill="none"
    stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" role="img"
    :aria-label="label || name" :style="cssVars">
    <!-- CPU：外框 + 引脚 + 核心；核心呼吸频率随占用率变快 -->
    <template v-if="name === 'cpu'">
      <rect class="si-line" x="4.8" y="4.8" width="14.4" height="14.4" rx="2.8" />
      <path class="si-line si-dim"
        d="M9.2 2.4v2.4M14.8 2.4v2.4M9.2 19.2v2.4M14.8 19.2v2.4M2.4 9.2h2.4M2.4 14.8h2.4M19.2 9.2h2.4M19.2 14.8h2.4" />
      <rect class="si-core" x="9" y="9" width="6" height="6" rx="1.4" />
    </template>

    <!-- 内存：一条 DIMM（内部分隔线 + 底部金手指），液位从左往右灌满表示占用 -->
    <template v-else-if="name === 'mem'">
      <rect class="si-bar" x="3.6" y="7.2" width="16.8" height="8.8" rx="1.2" />
      <rect class="si-line" x="2.6" y="6.4" width="18.8" height="10.4" rx="2" />
      <path class="si-line si-dim" d="M3.4 11.4h17.2" />
      <path class="si-line si-dim" d="M6.6 16.8v2.4M10.4 16.8v2.4M14.2 16.8v2.4M18 16.8v2.4" />
    </template>

    <!-- 运行时间：秒表（顶钮 + 侧钮 + 四向刻度），指针缓慢扫动 -->
    <template v-else-if="name === 'uptime'">
      <circle class="si-line" cx="12" cy="13.4" r="7.4" />
      <path class="si-line" d="M9.4 2.6h5.2M12 2.6v3.4" />
      <path class="si-line si-dim" d="M17.2 8.2l1.9-1.9" />
      <path class="si-line si-dim" d="M12 7.4v-1.4M18.8 13.4h-1.4M12 19.4v1.4M5.2 13.4h1.4" />
      <path class="si-sweep" d="M12 13.4V9.2" />
      <circle class="si-node" cx="12" cy="13.4" r=".9" />
    </template>

    <!-- Go 运行时：调度轨道 + 三个巡游的协程点 -->
    <template v-else-if="name === 'go'">
      <circle class="si-line si-dim" cx="12" cy="12" r="8.6" stroke-dasharray="1.3 3.1" />
      <g class="si-orbit">
        <circle class="si-node" cx="12" cy="3.4" r="1.8" />
        <circle class="si-node si-dim" cx="19.5" cy="16.4" r="1.3" />
        <circle class="si-node si-dim" cx="4.9" cy="15.8" r="1" />
      </g>
      <circle class="si-line" cx="12" cy="12" r="2.6" />
    </template>

    <!-- 磁盘：存储圆柱（顶面淡填充做体积），内部液位从底往上涨表示占用 -->
    <template v-else-if="name === 'disk'">
      <defs>
        <clipPath :id="clipId">
          <path d="M4.8 6.4v11.4c0 1.55 3.22 2.8 7.2 2.8s7.2-1.25 7.2-2.8V6.4z" />
        </clipPath>
      </defs>
      <!-- 液位 rect 的底边与柱体底面齐平，高度等于柱体可见高度，scaleY 才与百分比一一对应 -->
      <rect class="si-cyl-fill" :clip-path="'url(#' + clipId + ')'" x="4.8" y="6.4" width="14.4" height="14.2" />
      <ellipse class="si-cap si-line" cx="12" cy="6.4" rx="7.2" ry="2.8" />
      <path class="si-line"
        d="M4.8 6.4v11.4c0 1.55 3.22 2.8 7.2 2.8s7.2-1.25 7.2-2.8V6.4" />
    </template>

    <!-- 网络：地球 + 外圈两道对流弧（上传/下载绕行） -->
    <template v-else-if="name === 'net'">
      <circle class="si-line" cx="12" cy="12" r="6.2" />
      <path class="si-line si-dim" d="M5.8 12h12.4" />
      <path class="si-line si-dim" d="M12 5.8c-2.6 3.4-2.6 8.9 0 12.4M12 5.8c2.6 3.4 2.6 8.9 0 12.4" />
      <g class="si-flow">
        <path class="si-line" d="M12 2.4a9.6 9.6 0 0 1 8.3 4.8" />
        <path class="si-line" d="M12 21.6a9.6 9.6 0 0 1-8.3-4.8" />
      </g>
    </template>

    <!-- Docker 总览：容器（外框 + 内部实例，运行比例越高心跳越快） -->
    <template v-else-if="name === 'container'">
      <rect class="si-line" x="4.2" y="5.4" width="15.6" height="13" rx="2.6" />
      <rect class="si-core" x="8.8" y="8.8" width="6.4" height="6.4" rx="1.4" />
      <path class="si-line si-dim" d="M7.8 18.4v2.4M16.2 18.4v2.4" />
    </template>

    <!-- Docker 总览：编排（层堆叠，逐层上浮） -->
    <template v-else-if="name === 'compose'">
      <path class="si-layer" d="M4.6 8.2 12 4.6l7.4 3.6L12 11.8z" />
      <path class="si-layer si-dim" d="M4.6 12.6 12 16.2l7.4-3.6" />
      <path class="si-layer si-dim" d="M4.6 17 12 20.6l7.4-3.6" />
    </template>

    <!-- Docker 总览：镜像（三层镜像层，自上而下逐层亮起） -->
    <template v-else-if="name === 'image'">
      <rect class="si-plate" x="3.8" y="5.2" width="16.4" height="4.4" rx="1.4" />
      <rect class="si-plate" x="3.8" y="9.8" width="16.4" height="4.4" rx="1.4" />
      <rect class="si-plate" x="3.8" y="14.4" width="16.4" height="4.4" rx="1.4" />
    </template>

    <!-- Docker 总览：网络（三节点拓扑，节点依次亮起） -->
    <template v-else-if="name === 'network'">
      <path class="si-line si-net-link" d="M12 7.2 6.6 15.8M12 7.2l5.4 8.6M7.9 17.4h8.2" />
      <circle class="si-node si-net-node" cx="12" cy="5.4" r="2" />
      <circle class="si-node si-net-node" cx="5.6" cy="17.6" r="2" />
      <circle class="si-node si-net-node" cx="18.4" cy="17.6" r="2" />
    </template>

    <!-- Docker 总览：存储卷（盒子 + 竖棱，液位按在用比例从左涨） -->
    <template v-else-if="name === 'volume'">
      <rect class="si-bar" x="4.4" y="6.4" width="15.2" height="12" rx="1.4" />
      <rect class="si-line" x="4" y="4.8" width="16" height="14.2" rx="2.4" />
      <path class="si-line si-dim" d="M8.6 4.8v14.2M15.4 4.8v14.2" />
    </template>

    <!-- Docker 总览：镜像仓库（云 + 推拉箭头，箭头上下浮动） -->
    <template v-else-if="name === 'registry'">
      <path class="si-line" d="M7 17.6h10.2a3.6 3.6 0 0 0 .3-7.2 5.4 5.4 0 0 0-10.4-.4A3.4 3.4 0 0 0 7 17.6z" />
      <path class="si-arrow" d="M12 14.6V9.6M10.2 11.4 12 9.6l1.8 1.8" />
    </template>

    <!-- Nginx 运行状态：机架服务器（两层机身 + 状态灯 + 通风栅） -->
    <template v-else-if="name === 'server'">
      <rect class="si-line" x="4.4" y="4.6" width="15.2" height="6" rx="1.6" />
      <rect class="si-line" x="4.4" y="13.4" width="15.2" height="6" rx="1.6" />
      <circle class="si-node" cx="8" cy="7.6" r="1.1" />
      <circle class="si-node" cx="8" cy="16.4" r="1.1" />
      <path class="si-line si-dim" d="M12.4 7.6h4.4M12.4 16.4h4.4" />
    </template>

    <!-- Nginx 请求速率：仪表盘（半弧刻度 + 往复摆动的指针） -->
    <template v-else-if="name === 'gauge'">
      <path class="si-line" d="M4.2 16.6a8 8 0 1 1 15.6 0" />
      <path class="si-line si-dim" d="M4.2 16.6h2M17.8 16.6h-2M12 6.6v2" />
      <path class="si-needle" d="M12 16.6 15.8 10.4" />
      <circle class="si-node" cx="12" cy="16.6" r="1.2" />
    </template>

    <!-- Nginx HTTP 端口：插头（两脚 + 插头体 + 下方线缆） -->
    <template v-else-if="name === 'plug'">
      <path class="si-line" d="M9 3.4v4.2M15 3.4v4.2" />
      <path class="si-line" d="M6.4 7.6h11.2v3a5.6 5.6 0 0 1-11.2 0z" />
      <path class="si-line" d="M12 16.2v4.4" />
    </template>

    <!-- Nginx 版本：标签（带孔的价格牌轮廓） -->
    <template v-else-if="name === 'tag'">
      <path class="si-line"
        d="M4.4 11.2V5.4a1 1 0 0 1 1-1h5.8l7.4 7.4a1.4 1.4 0 0 1 0 2l-4.8 4.8a1.4 1.4 0 0 1-2 0z" />
      <circle class="si-node" cx="8.3" cy="8.3" r="1.3" />
    </template>
  </svg>
</template>

<script setup lang="ts">
import { computed } from "vue";

/** 磁盘圆柱的液位靠 clipPath 裁切，同页存在多个实例时 id 不能重复 */
const clipId = `si-cyl-${Math.random().toString(36).slice(2, 8)}`;

const props = withDefaults(defineProps<{
  /** 图标名：cpu / mem / uptime / go / disk / net（首页指标），
   *  container / compose / image / network / volume / registry（Docker 总览） */
  name: string;
  /** 像素尺寸（首页 stat-circle 用 26） */
  size?: number;
  /** 数据驱动进度值 0-100：CPU 脉冲频率 / 内存液位 / 磁盘进度条；不传则按 0 处理 */
  value?: number;
  /** 无障碍标签，缺省用图标名 */
  label?: string;
}>(), { size: 24, value: undefined, label: "" });

/** 进度值收敛到 0-100，非数字按 0（未就绪时不画填充，比画满更诚实） */
const pct = computed(() => {
  const v = Number(props.value);
  return Number.isFinite(v) ? Math.min(100, Math.max(0, v)) : 0;
});

const cssVars = computed<Record<string, string>>(() => ({
  // 液位 / 进度条比例（0-1），由 CSS 的 scaleY / scaleX 消费
  "--si-fill": (pct.value / 100).toFixed(3),
  "--si-p": (pct.value / 100).toFixed(3),
  // CPU 核心呼吸周期：占用越高跳得越快（2s → 0.8s）
  "--si-pulse": `${(2 - (pct.value / 100) * 1.2).toFixed(2)}s`,
}));
</script>

<style scoped>
.stat-icon {
  display: block;
  flex-shrink: 0;
  overflow: visible;
}

/* 辅助线：经线、引脚、刻度这类次要笔画压暗，避免小尺寸下发糊 */
.si-dim {
  opacity: 0.34;
}

/* 实心圆点：协程节点、表盘刻度、磁盘主轴 */
.si-node {
  fill: currentColor;
  stroke: none;
}

/* 比例填充块：fill 覆盖继承来的描边，靠 transform 表达占用比例 */
.si-bar,
.si-cyl-fill {
  fill: currentColor;
  stroke: none;
  opacity: 0.3;
  transition: transform 0.55s cubic-bezier(0.4, 0, 0.2, 1);
}

/* 内存条：从左往右灌满 */
.si-bar {
  transform-box: fill-box;
  transform-origin: left;
  transform: scaleX(var(--si-p, 0));
}

/* 磁盘圆柱：从底往上灌满，超出柱体的部分由组件内 clipPath 裁掉 */
.si-cyl-fill {
  transform-box: fill-box;
  transform-origin: bottom;
  transform: scaleY(var(--si-fill, 0));
}

/* 圆柱顶面：淡填充做出体积感 */
.si-cap {
  fill: currentColor;
  fill-opacity: 0.14;
}

.si-core {
  /* 芯片内核带一点填充，26px 下才不至于比原来的字形图标空 */
  fill: currentColor;
  fill-opacity: 0.16;
  animation: si-pulse var(--si-pulse, 1.8s) ease-in-out infinite;
  transform-box: fill-box;
  transform-origin: center;
}

.si-sweep {
  animation: si-spin 8s linear infinite;
  transform-origin: 12px 13.4px;
}

/* 编排：层堆叠，逐层错开上浮 */
.si-layer {
  animation: si-float 2.8s ease-in-out infinite;
}

.si-layer:nth-of-type(2) {
  animation-delay: 0.2s;
}

.si-layer:nth-of-type(3) {
  animation-delay: 0.4s;
}

/* 镜像：三层自上而下依次亮起，像逐层拉取 */
.si-plate {
  animation: si-twinkle 2.6s ease-in-out infinite;
}

.si-plate:nth-of-type(2) {
  animation-delay: 0.35s;
}

.si-plate:nth-of-type(3) {
  animation-delay: 0.7s;
}

/* 网络：节点依次亮起，像数据包在节点间转发 */
.si-net-node {
  animation: si-twinkle 2.4s ease-in-out infinite;
}

/* 拓扑连线：有容器接入的网络越多，连线越亮 */
.si-net-link {
  opacity: calc(0.18 + var(--si-fill, 0) * 0.55);
  transition: opacity 0.5s ease;
}

.si-net-node:nth-of-type(2) {
  animation-delay: 0.4s;
}

.si-net-node:nth-of-type(3) {
  animation-delay: 0.8s;
}

/* 仓库：箭头上下往复（push / pull） */
.si-arrow {
  animation: si-bob 2.4s ease-in-out infinite;
}

.si-orbit {
  animation: si-spin 7s linear infinite;
  transform-origin: 12px 12px;
}

.si-flow {
  animation: si-spin 5.5s linear infinite;
  transform-origin: 12px 12px;
}

@keyframes si-pulse {
  0%,
  100% {
    opacity: 0.32;
    transform: scale(0.78);
  }

  50% {
    opacity: 1;
    transform: scale(1);
  }
}

@keyframes si-float {
  0%,
  100% {
    transform: translateY(0);
  }

  50% {
    transform: translateY(-1.1px);
  }
}

@keyframes si-twinkle {
  0%,
  100% {
    opacity: 0.3;
  }

  50% {
    opacity: 1;
  }
}

@keyframes si-bob {
  0%,
  100% {
    transform: translateY(0.8px);
  }

  50% {
    transform: translateY(-0.8px);
  }
}

/* 仪表盘指针：绕轴心小幅往复摆动，像实时转速表 */
.si-needle {
  animation: si-sway 2.6s ease-in-out infinite;
  transform-box: fill-box;
  transform-origin: right bottom;
}

@keyframes si-sway {

  0%,
  100% {
    transform: rotate(-16deg);
  }

  50% {
    transform: rotate(20deg);
  }
}

@keyframes si-spin {
  to {
    transform: rotate(360deg);
  }
}

/* 系统开了「减少动态效果」就全部停掉，数据仍然通过填充比例表达 */
@media (prefers-reduced-motion: reduce) {
  .stat-icon * {
    animation: none !important;
    transition: none !important;
  }
}
</style>
