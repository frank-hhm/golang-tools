<template>
    <div class="blank-layout">
        <!-- 自绘标题栏：无边框窗口下由前端提供标题、视图菜单与窗口按钮（公共组件） -->
        <WinTitleBar :title="title">
            <template #icon>
                <common-tool-icon v-if="toolIcon" :name="toolIcon" :size="18" />
                <img v-else-if="appLogo" :src="appLogo" class="tb-logo" alt="" />
            </template>
            <common-theme-switch />
            <a-tooltip content="刷新">
                <a-button size="small" @click="reload"><icon-refresh /></a-button>
            </a-tooltip>
            <a-tooltip v-if="!isSettingsPage" content="设置">
                <a-button size="small" @click="toggleSettings"><icon-settings /></a-button>
            </a-tooltip>
            <a-tooltip v-if="!noFullscreen" content="全屏">
                <a-button size="small" @click="toggleFullscreen"><icon-fullscreen /></a-button>
            </a-tooltip>
            <a-tooltip content="最小化">
                <a-button size="small" @click="minimise"><icon-minus /></a-button>
            </a-tooltip>
            <a-tooltip content="关闭">
                <a-button size="small" @click="close"><icon-close /></a-button>
            </a-tooltip>
        </WinTitleBar>
        <div class="win-content" :class="{ 'no-pad': noPadding }">
            <router-view />
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { Window } from "@wailsio/runtime";
import { useToolWindow } from "@/views/index/useToolWindow";
import { DEFAULT_TOOL_ICONS, normalizeToolIcon } from "@/utils/tool-icon";
import WinTitleBar from "@/components/layouts/WinTitleBar.vue";

/**
 * 空白布局：用于 Wails 原生新窗口（/win 前缀）。
 * 采用无边框窗口，标题栏自绘：左侧工具图标 + 标题，右侧“视图”菜单与最小化/最大化/关闭按钮。
 * 全屏时该标题栏随内容铺满显示器，菜单依旧可见。
 */
const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const route = useRoute();
const { openToolWindow } = useToolWindow();
const title = computed(() => (route.meta?.menu_name as string) || "go-tools");

/** 标题栏图标：用该工具在功能配置里的图标（含用户自定义），配置里没有再退内置默认表 */
const toolIcon = ref("");
/** 默认表也没收录（如 demo 路由）时拿应用 Logo 兜底，不让标题栏左侧空着一块 */
const appLogo = ref("");

async function loadIcon() {
    // 工具窗路由带 /win 前缀，而功能配置里存的是 /tools/xxx，比对前先去掉
    const path = route.path.replace(/^\/win(?=\/)/, "");
    const key = path.split("/").filter(Boolean).pop() || "";
    try {
        const cfg = await GoTools?.GetConfig();
        const hit = (cfg?.tools || []).find((t: any) => t.path === path);
        toolIcon.value = normalizeToolIcon(hit?.icon) || DEFAULT_TOOL_ICONS[key] || "";
        if (!toolIcon.value) appLogo.value = (await GoTools?.GetAppLogo()) || "";
    } catch {
        /* 图标只是标题栏的装饰，读不到配置就不画，不影响窗口使用 */
    }
}

onMounted(loadIcon);

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
