<template>
    <div class="blank-layout">
        <!-- 自绘标题栏：无边框窗口下由前端提供标题、视图菜单与窗口按钮（公共组件） -->
        <WinTitleBar :title="title">
            <common-theme-switch />
            <a-button size="small" @click="reload"><icon-refresh /></a-button>
            <a-button size="small" v-if="!isSettingsPage" @click="toggleSettings"><icon-settings /></a-button>
            <a-button size="small" v-if="!noFullscreen" @click="toggleFullscreen"><icon-fullscreen /></a-button>
            <a-button size="small" @click="minimise"><icon-minus /></a-button>
            <a-button size="small" @click="close"><icon-close /></a-button>
        </WinTitleBar>
        <div class="win-content" :class="{ 'no-pad': noPadding }">
            <router-view />
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed } from "vue";
import { useRoute } from "vue-router";
import { Window } from "@wailsio/runtime";
import { useToolWindow } from "@/views/index/useToolWindow";
import WinTitleBar from "@/components/layouts/WinTitleBar.vue";

/**
 * 空白布局：用于 Wails 原生新窗口（/win 前缀）。
 * 采用无边框窗口，标题栏自绘：左侧标题，右侧“视图”菜单与最小化/最大化/关闭按钮。
 * 全屏时该标题栏随内容铺满显示器，菜单依旧可见。
 */
const route = useRoute();
const { openToolWindow } = useToolWindow();
const title = computed(() => (route.meta?.menu_name as string) || "go-tools");

// 当前页即设置页时，视图菜单不再重复展示“设置”入口
const isSettingsPage = computed(() => route.path.includes("/tools/settings"));
// 工具配置“禁用全屏”（URL query fs=0）：隐藏最大化按钮与“切换全屏”菜单
const noFullscreen = computed(() => String(route.query.fs ?? "") === "0");
// 内容区内边距：默认给；取色器等需要内容铺满边缘的工具页去掉 padding
const noPadding = computed(() => route.path.includes("/tools/colorpicker"));

const reload = () => location.reload();
// 用最大化实现“铺满显示器”，避免排他全屏把自绘标题栏顶出屏幕
const toggleFullscreen = () => Window.ToggleMaximise();
const minimise = () => Window.Minimise();
const close = () => Window.Close();

// 打开设置：走单例工具窗口，不在当前窗口跳转；设置窗口已存在时由后端直接复用（显示+取消最小化+聚焦）
const toggleSettings = () => openToolWindow("/tools/settings", "设置", { single: true });

</script>

<style scoped>
.blank-layout {
    position: fixed;
    left: 0;
    top: 0;
    width: 100%;
    height: 100%;
    background: var(--color-bg-1);
}

.win-content {
    position: absolute;
    top: 40px;
    left: 0;
    right: 0;
    bottom: 0;
    padding: var(--base-padding);
    overflow-y: auto;
}

/* 取色器等铺满型工具页：去掉内容区留白 */
.win-content.no-pad {
    padding: 0;
}
</style>
