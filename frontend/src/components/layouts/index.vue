<template>
    <div class="basic-layout">
        <!-- 自绘标题栏：无边框主窗口下由前端提供标题、设置入口与关闭按钮 -->
        <div class="win-titlebar">
            <div class="tb-title">{{ appTitle }}</div>
            <div class="tb-actions mr12">
                <a-space>
                    <a-switch size="small" v-model:checked="themeMode" @change="toggleTheme" />
                    <a-button size="small" @click="openSettings"><icon-settings /></a-button>
                    <a-button size="small" status="danger" title="关闭" @click="close"><icon-close /></a-button>
                </a-space>
            </div>
        </div>
        <div class="layout-content">
            <router-view />
        </div>
    </div>
</template>
<script setup lang="ts">
import { getCurrentInstance, onMounted, onUnmounted, ref } from "vue";
import { Events, Window } from "@wailsio/runtime";
import { useToolWindow } from "@/views/index/useToolWindow";
import { applyTheme, currentTheme } from "@/utils/theme";

/**
 * 主窗口基础布局（无边框）。
 * 顶部为自绘标题栏：左侧应用名，右侧暗黑切换开关、“设置”入口与“关闭”按钮
 * （与工具窗口 BlankLayout 样式一致；关闭主窗口由后端 WindowClosing 钩子拦截，隐藏到托盘而非退出）。
 */
const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const { openToolWindow } = useToolWindow();

const appTitle = ref("Tools");

// 当前主题模式：与 BlankLayout 同款开关逻辑（浅色/暗黑快速切换，“跟随系统”按实际深浅落地）
const themeMode = ref(currentTheme());

const toggleTheme = () => {
    // auto 模式下按系统实际偏好落地为固定主题，再在浅色/暗黑间切换
    const dark = themeMode.value === "dark" || (themeMode.value === "auto" && window.matchMedia("(prefers-color-scheme: dark)").matches);
    const next = dark ? "light" : "dark";
    applyTheme(next);
    themeMode.value = next;
    // 同步写入后端配置，浮动球原生菜单据此跟随浅/暗黑
    try { GoTools?.SetAppTheme?.(next); } catch { /* */ }
};

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

.win-titlebar {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    height: 40px;
    z-index: 1000;
    display: flex;
    align-items: center;
    justify-content: space-between;
    /* 与 BlankLayout 一致：随主题变化的标题栏配色 */
    background: var(--color-bg-1);
    color: var(--color-text-1);
    user-select: none;
    -webkit-app-region: drag;
    app-region: drag;
    border-bottom: 1px solid var(--color-border-1);
}

.tb-title {
    padding-left: 14px;
    font-size: var(--base-size-medium);
    font-weight: bold;
}

.tb-actions {
    display: flex;
    align-items: center;
    -webkit-app-region: no-drag;
    app-region: no-drag;
}

.tb-btn {
    width: 44px;
    height: 40px;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    font-size: var(--base-size-default);
}
.tb-btn:hover {
    background: rgba(var(--primary-6), 0.12);
}
.tb-close:hover {
    background: rgb(var(--red-6));
    color: #fff;
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
