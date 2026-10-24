<template>
    <div class="basic-layout">
        <!-- 自绘标题栏：复用公共 WinTitleBar（拖动/no-drag 与样式均在组件内），这里仅提供主窗口特有按钮 -->
        <WinTitleBar :title="appTitle">
            <common-theme-switch />
            <a-button size="small" @click="openSettings"><icon-settings /></a-button>
            <a-button size="small" status="danger" title="关闭" @click="close"><icon-close /></a-button>
        </WinTitleBar>
        <div class="layout-content">
            <router-view />
        </div>
    </div>
</template>
<script setup lang="ts">
import { getCurrentInstance, onMounted, onUnmounted, ref } from "vue";
import { Events, Window } from "@wailsio/runtime";
import { useToolWindow } from "@/views/index/useToolWindow";
import WinTitleBar from "@/components/layouts/WinTitleBar.vue";

/**
 * 主窗口基础布局（无边框）。
 * 顶部为自绘标题栏：左侧应用名，右侧暗黑切换开关、“设置”入口与“关闭”按钮
 * （与工具窗口 BlankLayout 样式一致；关闭主窗口由后端 WindowClosing 钩子拦截，隐藏到托盘而非退出）。
 */
const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const { openToolWindow } = useToolWindow();

const appTitle = ref("Tools");

function openSettings() {
    openToolWindow("/tools/settings", "设置", { single: true });
}
const close = () => Window.Close();

async function loadTitle() {
    try {
        const cfg = await GoTools?.GetConfig();
        if (cfg?.general?.appName) appTitle.value = cfg.general.appName;
    } catch { /* */ }
}

let off: (() => void) | null = null;
onMounted(() => {
    loadTitle();
    // 设置页改名后即时同步标题栏
    try {
        const r = Events.On("config:saved", () => { void loadTitle(); });
        if (typeof r === "function") off = r as () => void;
    } catch { /* */ }
});
onUnmounted(() => { off?.(); });
</script>
<style scoped>
.basic-layout {
    min-height: 100vh;
    display: flex;
    flex-direction: column;
}

.layout-content {
    left: 0;
    top: 40px;
    position: fixed;
    width: calc(100% - (2*var(--base-padding)));
    height: calc(100% - 40px - (2*var(--base-padding)));
    padding: var(--base-padding);
    background: var(--color-bg-body);
    overflow-y: scroll;
}
</style>
