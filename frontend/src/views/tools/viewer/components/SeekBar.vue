<template>
    <div ref="el" class="seek" :class="{ 'seek-lg': large, 'seek-v': vertical }" @pointerdown="onDown"
        @pointermove="onMove" @pointerup="onUp" @pointercancel="onUp" @pointerleave="onLeave">
        <div class="seek-track">
            <div v-if="!vertical" class="seek-buffered" :style="{ width: pct(buffered) }"></div>
            <div class="seek-fill" :style="vertical ? { height: pct(ratio) } : { width: pct(ratio) }"></div>
            <div class="seek-knob" :style="vertical ? { bottom: pct(ratio) } : { left: pct(ratio) }"
                :class="{ on: dragging }"></div>
        </div>
        <div v-if="tip && !vertical && hover !== null && !preview" class="seek-tip" :style="{ left: pct(hover) }">
            {{ fmtTime(hover * (duration || 0)) }}
        </div>
    </div>
</template>

<script setup lang="ts">
/*
可拖拽进度/音量条：pointer 事件 + setPointerCapture，按下即跳转、拖动实时跟随，
支持点击定位与「快进」式拖动。ratio / buffered 都是 0..1，父组件负责换算成秒。
vertical=true 时渲染成竖向条（音量弹层用，自底向上），指针按 Y 轴换算。
tip=true 且传 duration 时，悬停显示目标时间点气泡；preview=true 时气泡让位
给父组件的悬停预览缩略图（VideoPlayer 用），并把悬停位置 (0..1|null)
通过 hover-ratio 事件报给父组件。
配色走 --sb-track/--sb-buffered/--sb-fill/--sb-knob 四个 CSS 变量，默认是深底
（半透明白）配色；浅色卡片里由父组件覆盖成 Arco 语义变量，自动跟随明暗主题。
*/
import { ref } from "vue";
import { fmtTime } from "./mediaCore";

const props = defineProps<{
    ratio: number;
    buffered?: number;
    large?: boolean;
    vertical?: boolean;
    tip?: boolean;
    preview?: boolean;
    duration?: number;
}>();
const emit = defineEmits<{
    (e: "update:ratio", v: number): void;
    (e: "hover-ratio", v: number | null): void;
}>();

const el = ref<HTMLElement | null>(null);
const dragging = ref(false);
const hover = ref<number | null>(null);

function clamp(n: number | undefined) {
    return Math.max(0, Math.min(1, n || 0));
}
function pct(n: number | undefined) {
    return clamp(n) * 100 + "%";
}
function ratioFrom(e: PointerEvent) {
    const node = el.value;
    if (!node) return null;
    const rect = node.getBoundingClientRect();
    if (props.vertical) {
        if (rect.height <= 0) return null;
        return clamp((rect.bottom - e.clientY) / rect.height);
    }
    if (rect.width <= 0) return null;
    return clamp((e.clientX - rect.left) / rect.width);
}
function onDown(e: PointerEvent) {
    dragging.value = true;
    el.value?.setPointerCapture(e.pointerId);
    const r = ratioFrom(e);
    if (r !== null) emit("update:ratio", r);
}
function onMove(e: PointerEvent) {
    const r = ratioFrom(e);
    if (dragging.value && r !== null) emit("update:ratio", r);
    if (!props.vertical) {
        if (props.tip) hover.value = r;
        emit("hover-ratio", r);
    }
}
function onUp(e: PointerEvent) {
    if (!dragging.value) return;
    dragging.value = false;
    try {
        el.value?.releasePointerCapture(e.pointerId);
    } catch {
        /* 已释放则忽略 */
    }
}
function onLeave() {
    hover.value = null;
    emit("hover-ratio", null);
}
</script>

<style scoped>
.seek {
    position: relative;
    padding: 6px 0;
    cursor: pointer;
    touch-action: none;
    flex: 1;
    min-width: 0;
}
.seek-lg {
    padding: 9px 0;
}
/* 贴底全宽（B 站式）：拉大命中区，轨道保持细线 */
.seek-plain {
    flex: none;
    width: 100%;
    padding: 8px 0 6px;
}
.seek-track {
    position: relative;
    height: 4px;
    border-radius: 2px;
    background: var(--sb-track, rgba(255, 255, 255, 0.28));
    transition: height 0.12s ease;
}
.seek-lg .seek-track {
    height: 6px;
}
.seek:not(.seek-v):hover .seek-track {
    height: 6px;
}
.seek-buffered {
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    border-radius: 2px;
    background: var(--sb-buffered, rgba(255, 255, 255, 0.42));
}
.seek-fill {
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    border-radius: 2px;
    background: var(--sb-fill, rgb(var(--primary-6)));
}
.seek-knob {
    position: absolute;
    top: 50%;
    width: 12px;
    height: 12px;
    border-radius: 50%;
    background: var(--sb-knob, #fff);
    box-shadow: 0 1px 3px rgba(0, 0, 0, 0.35);
    transform: translate(-50%, -50%) scale(0);
    transition: transform 0.12s ease;
}
.seek:hover .seek-knob,
.seek-knob.on {
    transform: translate(-50%, -50%) scale(1);
}
.seek-tip {
    position: absolute;
    bottom: 100%;
    transform: translateX(-50%);
    margin-bottom: 12px;
    padding: 3px 7px;
    border-radius: var(--base-radius-small);
    background: rgba(0, 0, 0, 0.82);
    color: #fff;
    font-size: var(--base-size-small);
    line-height: 1.4;
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
    pointer-events: none;
}

/* 竖向：音量弹层，自底向上填充 */
.seek-v {
    flex: none;
    width: 22px;
    height: 96px;
    padding: 0;
}
.seek-v .seek-track {
    width: 4px;
    height: 100%;
    margin: 0 auto;
    transition: width 0.12s ease;
}
.seek-v:hover .seek-track {
    width: 6px;
}
.seek-v .seek-fill {
    top: auto;
    bottom: 0;
    left: 0;
    right: 0;
    width: auto;
}
.seek-v .seek-knob {
    top: auto;
    left: 50%;
    transform: translate(-50%, 50%) scale(0);
}
.seek-v:hover .seek-knob,
.seek-v .seek-knob.on {
    transform: translate(-50%, 50%) scale(1);
}
</style>
