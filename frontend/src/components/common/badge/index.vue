<template>
    <!-- 状态圆点：颜色同 a-badge 的 status 语义色；animated 开启模糊光晕扩张。
         根元素自带一圈出血（bleed）并用负 margin 抵消占位，
         这样光晕在卡片 header 这类 overflow:hidden 容器里也能完整扩散，不会只剩半圈；
         同时抬一层 z-index，避免光晕被紧邻的文字/标签盖住半边。
         注：放进卡片标题栏（#title / #extra）时，Arco 那两个容器的 overflow:hidden 仍会裁掉贴边的光晕，
         styles/theme/style.scss 里对 .card-head-tool 的头栏做了同样的外扩照顾 -->
    <!-- 悬停提示走 a-tooltip（泡泡挂在 body 上，不会被父级 overflow 裁），没有 title 时整体禁用 -->
    <a-tooltip :content="title" :disabled="!title">
        <span class="com-badge" :class="[`com-badge-${status}`, { 'is-animated': animated }]" :style="rootStyle">
            <span class="com-badge-dot"></span>
        </span>
    </a-tooltip>
</template>
<script lang="ts">
export default {
    name: "com-badge",
};
</script>
<script lang="ts" setup>
import { computed } from "vue";

const props = withDefaults(defineProps<{
    /** 状态语义色，对应 a-badge 的 status */
    status?: "normal" | "processing" | "success" | "warning" | "danger";
    /** 圆点直径（px） */
    size?: number;
    /** 模糊背景扩张动画：用于"正在运行/连接中"这类持续态 */
    animated?: boolean;
    /** 悬停提示（渲染为 a-tooltip，不是原生 title） */
    title?: string;
    /** 自定义颜色，覆盖 status 语义色 */
    color?: string;
    /**
     * 出血范围（px）：光晕扩散的最大余量。
     * 根元素按 size + bleed*2 占位，再用负 margin 收回，布局间距不变，
     * 但给光晕留出了不被父级 overflow 裁掉的空间。
     */
    bleed?: number;
}>(), {
    status: "normal",
    size: 8,
    animated: false,
    title: "",
    color: "",
    bleed: 7,
});

/** 尺寸、出血与自定义色通过 CSS 变量下发，语义色仍由类名控制，避免内联样式盖掉主题变量 */
const rootStyle = computed<Record<string, string>>(() => {
    const style: Record<string, string> = {
        "--com-badge-size": `${props.size}px`,
        "--com-badge-bleed": `${props.bleed}px`,
    };
    if (props.color) style["--com-badge-color"] = props.color;
    return style;
});
</script>
<style scoped>
.com-badge {
    position: relative;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    vertical-align: middle;
    /* 相邻兄弟节点默认后绘制，会把光晕盖成半圈，这里抬到同级之上 */
    z-index: 1;
    width: calc(var(--com-badge-size, 8px) + var(--com-badge-bleed, 7px) * 2);
    height: calc(var(--com-badge-size, 8px) + var(--com-badge-bleed, 7px) * 2);
    /* 出血区不参与布局：等价于一个 size 大小的行内元素 */
    margin: calc(var(--com-badge-bleed, 7px) * -1);
}

.com-badge-dot {
    position: relative;
    display: block;
    width: var(--com-badge-size, 8px);
    height: var(--com-badge-size, 8px);
    border-radius: 50%;
    background: var(--com-badge-color, var(--color-fill-4));
}

.com-badge-normal {
    --com-badge-color: var(--color-fill-4);
}

.com-badge-processing {
    --com-badge-color: rgb(var(--primary-6));
}

.com-badge-success {
    --com-badge-color: rgb(var(--success-6));
}

.com-badge-warning {
    --com-badge-color: rgb(var(--warning-6));
}

.com-badge-danger {
    --com-badge-color: rgb(var(--danger-6));
}

/* 光晕：两层错开半个周期，任意时刻都至少有一圈可见，不会出现"闪空"的断档；
   blur 固定、最大直径 20.8px（8px × 2.1 + 2px 模糊），正好落在根元素 22px 的出血区内，
   父级 overflow:hidden 也裁不到；只动 transform/opacity，走合成层 */
.com-badge.is-animated .com-badge-dot::before,
.com-badge.is-animated .com-badge-dot::after {
    content: "";
    position: absolute;
    inset: 0;
    border-radius: 50%;
    background: var(--com-badge-color);
    filter: blur(2px);
    opacity: 0;
    pointer-events: none;
    will-change: transform, opacity;
    animation: com-badge-wave 1.8s ease-out infinite;
}

/* 负延迟：第二圈一上来就从周期中段接上，首帧不会先空半拍 */
.com-badge.is-animated .com-badge-dot::before {
    animation-delay: -0.9s;
}

@keyframes com-badge-wave {
    0% {
        transform: scale(1);
        opacity: 0.45;
    }

    60% {
        transform: scale(1.7);
        opacity: 0.22;
    }

    100% {
        transform: scale(2.1);
        opacity: 0;
    }
}

/* 系统开启「减少动效」时只留静态点 */
@media (prefers-reduced-motion: reduce) {

    .com-badge.is-animated .com-badge-dot::before,
    .com-badge.is-animated .com-badge-dot::after {
        animation: none;
        opacity: 0;
    }
}
</style>
