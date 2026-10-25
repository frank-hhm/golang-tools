<template>
    <!-- 单个演示格：时钟 + 一句说明。colors 为空即跟随主题 -->
    <div class="clock-cell">
        <canvas-clock :size="size" :show-info="showInfo" v-bind="colors" />
        <a-tooltip :content="caption" :disabled="!caption">
            <p class="clock-caption">{{ caption }}</p>
        </a-tooltip>
    </div>
</template>
<script lang="ts">
export default {
    name: "clock-cell",
};
</script>
<script lang="ts" setup>
import CanvasClock from "@/components/common/canvas-clock/index.vue";
import type { ClockColors } from "./presets";

withDefaults(
    defineProps<{
        /** 表盘边长（px） */
        size: number;
        showInfo: boolean;
        /** 格子下方的说明文字 */
        caption: string;
        /** 六个颜色，缺省的项由组件回落到 Arco 主题变量 */
        colors?: Partial<ClockColors>;
    }>(),
    { colors: () => ({}) }
);
</script>
<style scoped>
.clock-cell {
    text-align: center;
}

.clock-caption {
    margin: 8px 0 0;
    color: var(--color-text-3);
    font-size: 12px;
    max-width: 320px;
    margin-left: auto;
    margin-right: auto;
}
</style>
