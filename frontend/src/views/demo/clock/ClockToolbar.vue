<template>
    <!-- 卡片右上角控制条：样式、尺寸、盘面信息，以及两个弹窗入口 -->
    <a-space :size="10" wrap>
        <theme-switch />
        <a-switch :model-value="showInfo" size="small" @update:model-value="onInfo" />
        <span class="hint">盘面信息（日期/星期/年份/毫秒）</span>
        <a-slider :model-value="size" :min="120" :max="420" :step="10" style="width: 140px"
            @update:model-value="onSize" />
        <span class="hint hint-size">{{ size }}px</span>
        <a-button size="small" type="outline" @click="emit('open-theme')">
            <template #icon><icon-settings /></template>自定义配色
        </a-button>
        <a-button size="small" @click="emit('open-usage')">
            <template #icon><icon-info-circle /></template>用法
        </a-button>
    </a-space>
</template>
<script lang="ts">
export default {
    name: "clock-toolbar",
};
</script>
<script lang="ts" setup>
import ThemeSwitch from "@/components/common/theme-switch/index.vue";

defineProps<{
    /** 当前表盘边长（px），由父级持有 */
    size: number;
    /** 是否显示盘面信息 */
    showInfo: boolean;
}>();

const emit = defineEmits<{
    (e: "update:size", value: number): void;
    (e: "update:showInfo", value: boolean): void;
    (e: "open-theme"): void;
    (e: "open-usage"): void;
}>();

/** 滑杆在区间模式下回的是数组，这里只取第一段 */
function onSize(value: number | number[]) {
    const v = Array.isArray(value) ? value[0] : value;
    if (typeof v === "number" && !Number.isNaN(v)) emit("update:size", v);
}

function onInfo(value: string | number | boolean) {
    emit("update:showInfo", Boolean(value));
}
</script>
<style scoped>
.hint {
    color: var(--color-text-3);
    font-size: 12px;
    white-space: nowrap;
}

/* 尺寸数字固定宽度，拖动时后面的按钮不会跟着抖 */
.hint-size {
    display: inline-block;
    width: 44px;
}
</style>
