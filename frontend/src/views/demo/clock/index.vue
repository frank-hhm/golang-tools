<template>
    <!-- common-canvas-clock 组件演示：控制条 + 若干演示格 + 两个弹窗组件 -->
    <layout-body title="Canvas 时钟组件 Demo">
        <div class="clock-demo">
            <a-card title="common-canvas-clock 时钟组件" :bordered="true">
                <template #extra>
                    <clock-toolbar :size="size" :show-info="showInfo" @update:size="size = $event"
                        @update:show-info="showInfo = $event" @open-theme="themeVisible = true"
                        @open-usage="usageVisible = true" />
                </template>
                <a-space size="large" wrap>
                    <!-- 预设格：颜色为空即跟随主题，显式传值则固定配色 -->
                    <clock-cell v-for="p in presets" :key="p.key" :size="cellSize(p.scale)" :show-info="showInfo"
                        :colors="p.colors" :caption="p.caption" />
                    <!-- 自定义配色格：工具栏「自定义配色」改的就是它 -->
                    <clock-cell :size="size" :show-info="showInfo" :colors="customColors" caption="自定义配色（工具栏可改）" />
                </a-space>
            </a-card>
            <div class="clock-tip">
                颜色缺省时自动跟随 Arco 主题（右上角开关切暗黑即可看换肤），显式传值则固定配色。
                <a-link @click="usageVisible = true">查看完整参数</a-link>
            </div>
        </div>

        <clock-theme-modal v-model:visible="themeVisible" :colors="customColors" @apply="onApplyTheme" />
        <clock-usage-modal v-model:visible="usageVisible" />
    </layout-body>
</template>
<script lang="ts">
export default {
    name: "demo-clock",
};
</script>
<script lang="ts" setup>
import { reactive, ref } from "vue";
import ClockCell from "./ClockCell.vue";
import ClockToolbar from "./ClockToolbar.vue";
import ClockThemeModal from "./ClockThemeModal.vue";
import ClockUsageModal from "./ClockUsageModal.vue";
import { CLOCK_PRESETS, CUSTOM_DEFAULT, type ClockColors } from "./presets";

const size = ref(280);
const showInfo = ref(true);

const presets = CLOCK_PRESETS;
// 自定义那一格的颜色：弹窗点「应用」后整体替换
const customColors = reactive<ClockColors>({ ...CUSTOM_DEFAULT });

const themeVisible = ref(false);
const usageVisible = ref(false);

/** 小尺寸演示格按比例缩，但不缩到看不清刻度 */
function cellSize(scale: number) {
    return Math.max(100, Math.round(size.value * scale));
}

function onApplyTheme(colors: ClockColors) {
    Object.assign(customColors, colors);
}
</script>
<style scoped>
.clock-demo {
    padding: 4px;
}

.clock-tip {
    margin-top: 12px;
    color: var(--color-text-3);
    font-size: 12px;
}
</style>
