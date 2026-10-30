<template>
    <!-- 暗黑模式切换开关：主窗口与 /win 工具窗（BlankLayout，含设置页窗口）标题栏共用。
         带日/月图标指示当前深浅；跨窗口切换时经 storage 事件自动同步开关状态。 -->
    <a-tooltip content="切换浅色/暗黑主题">
        <a-switch size="small" :checked="dark" @change="toggle" type="round">
            <template #checked-icon><icon-moon /></template>
            <template #unchecked-icon><icon-sun /></template>
        </a-switch>
    </a-tooltip>
</template>
<script lang="ts">
export default {
    name: "common-theme-switch",
};
</script>
<script lang="ts" setup>
import { computed, getCurrentInstance, onBeforeUnmount, onMounted, ref } from "vue";
import { applyTheme, currentTheme, isDark, THEME_STORAGE_KEY, type ThemeMode } from "@/utils/theme";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

// 记忆完整模式（light/dark/auto），开关仅展示/切换深浅结果
const mode = ref<ThemeMode>(currentTheme());
const dark = computed(() => isDark(mode.value));

/** 浅色/暗黑快速切换：“跟随系统”按系统实际偏好落地为固定主题后再切换 */
function toggle() {
    const next: ThemeMode = dark.value ? "light" : "dark";
    mode.value = next;
    applyTheme(next);
    // 同步写入后端配置，浮动球原生菜单据此跟随浅/暗黑
    try { GoTools?.SetAppTheme?.(next); } catch { /* */ }
}

/** 其他窗口（含设置页主题单选）修改主题时，storage 事件广播同步本窗口开关 */
const onStorage = (e: StorageEvent) => {
    if (e.key === THEME_STORAGE_KEY && e.newValue) mode.value = e.newValue as ThemeMode;
};
onMounted(() => window.addEventListener("storage", onStorage));
onBeforeUnmount(() => window.removeEventListener("storage", onStorage));
</script>
