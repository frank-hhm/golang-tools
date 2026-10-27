<template>
    <div class="basic-layout" :class="{ 'is-web': !caps.nativeWindows }">
        <!-- 路由切换进度条：Web 模式下页面 chunk 要下载，远程服务器上不给反馈就像卡死 -->
        <div class="route-progress" :class="{ 'is-on': routeLoading }"></div>
        <!-- 自绘标题栏：复用公共 WinTitleBar（拖动/no-drag 与样式均在组件内），这里仅提供主窗口特有按钮 -->
        <WinTitleBar :title="barTitle">
            <!-- Web 模式没有窗口：Logo 已由侧栏品牌区承担，顶部只留当前页名 -->
            <template v-if="caps.nativeWindows" #icon>
                <img v-if="appLogo" :src="appLogo" class="tb-logo" alt="" />
            </template>
            <common-theme-switch />
            <!-- 下载任务入口：常驻标题栏，有任务时才亮徽标（进行中数 / 未读结果数）；
                 左下角浮标只在有任务时出现，这里是不依赖浮标的固定落点 -->
            <a-badge :count="hubBadgeCount" :max-count="99"
                :class="{ 'tb-hub-idle': !hubActive }">
                <a-tooltip :content="hubRunning ? `下载任务：${hubRunning} 个进行中` : '下载任务'">
                    <a-button size="small" @click="openTaskHub"><icon-download /></a-button>
                </a-tooltip>
            </a-badge>
            <a-tooltip content="设置">
                <a-button size="small" @click="openSettings"><icon-settings /></a-button>
            </a-tooltip>
            <!-- 关闭按钮只在真有窗口时给：Web 模式下没有窗口可关，Window.Close() 只会报错 -->
            <a-tooltip v-if="caps.nativeWindows" content="关闭">
                <a-button size="small" status="danger" @click="close"><icon-close /></a-button>
            </a-tooltip>
        </WinTitleBar>
        <!-- Web 模式没有窗口可开，工具切换全部靠左侧固定导航 -->
        <SideNav v-if="!caps.nativeWindows" :title="appTitle" :logo="appLogo" />
        <div class="layout-content">
            <router-view />
        </div>
        <!-- 安装/下载任务浮标：任务在 Go 侧独立跑，这里保证离开页面也能看到进度 -->
        <task-hub ref="taskHubRef" />
    </div>
</template>
<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, onUnmounted, ref } from "vue";
import { useRoute } from "vue-router";
import { Events, Window } from "@wailsio/runtime";
import { useToolWindow } from "@/views/index/useToolWindow";
import { useCaps } from "@/utils/caps";
import { useTasksStoreHook } from "@/store/modules/tasks";
import { routeLoading } from "@/router/permission";
import WinTitleBar from "@/components/layouts/WinTitleBar.vue";
import SideNav from "@/components/layouts/SideNav.vue";
import TaskHub from "@/components/business/task-hub/index.vue";

/**
 * 主窗口基础布局（无边框）。
 * 顶部为自绘标题栏：左侧应用 Logo + 应用名，右侧暗黑切换开关、“设置”入口与“关闭”按钮
 * （与工具窗口 BlankLayout 样式一致；关闭主窗口由后端 WindowClosing 钩子拦截，隐藏到托盘而非退出）。
 */
const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const { openToolWindow } = useToolWindow();
/** 平台能力：决定显示侧边导航、以及顶部要不要给窗口按钮 */
const caps = useCaps();
const route = useRoute();

const appTitle = ref("Tools");
// 应用 Logo：自定义路径优先、否则内置默认，由后端统一返回 base64（同浮动球用的那张）
const appLogo = ref("");

/**
 * 标题栏文字：桌面版就是应用名（窗口标题）；
 * Web 模式的应用名已由侧栏品牌区承担，顶部改显当前页面名，否则一路翻页顶部永远同一个词。
 */
const barTitle = computed(() => {
    if (caps.nativeWindows) return appTitle.value;
    const page = String(route.meta?.menu_name || "");
    return page || appTitle.value;
});

function openSettings() {
    openToolWindow("/tools/settings", "设置", { single: true });
}
const close = () => Window.Close();

/* 标题栏下载入口：徽标数据直接读全局任务台账，点开即打开任务抽屉（抽屉内部会标已读） */
const tasksStore = useTasksStoreHook();
const taskHubRef = ref<InstanceType<typeof TaskHub> | null>(null);
const hubRunning = computed(() => tasksStore.runningCount);
const hubActive = computed(() => hubRunning.value > 0 || tasksStore.unseenCount > 0);
const hubBadgeCount = computed(() => hubRunning.value || tasksStore.unseenCount);

function openTaskHub() {
    (taskHubRef.value as any)?.open?.();
}

async function loadTitle() {
    try {
        const cfg = await GoTools?.GetConfig();
        if (cfg?.general?.appName) appTitle.value = cfg.general.appName;
    } catch { /* */ }
}

async function loadLogo() {
    try {
        appLogo.value = (await GoTools?.GetAppLogo()) || "";
    } catch { /* 取不到就不画 Logo，标题栏文字仍在 */ }
}

let off: (() => void) | null = null;
onMounted(() => {
    loadTitle();
    loadLogo();
    // 设置页改名/换 Logo 后即时同步标题栏
    try {
        const r = Events.On("config:saved", () => { void loadTitle(); void loadLogo(); });
        if (typeof r === "function") off = r as () => void;
    } catch { /* */ }
});
onUnmounted(() => { off?.(); });
</script>
<style scoped>
.basic-layout {
    /* 标题栏高度与侧边导航宽度：两处布局计算共用，改一处即可 */
    --titlebar-h: 40px;
    --side-nav-w: 0px;
    min-height: 100vh;
    display: flex;
    flex-direction: column;
}

/* 顶部进度条：只在路由解析期间出现，走一条流动高光 */
.route-progress {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    height: 2px;
    z-index: 2000;
    opacity: 0;
    pointer-events: none;
    transition: opacity 0.18s ease;
    background: linear-gradient(90deg, transparent, rgb(var(--primary-6)), rgb(var(--primary-4)), transparent);
    background-size: 40% 100%;
    background-repeat: no-repeat;
    animation: route-flow 0.9s linear infinite;
}

.route-progress.is-on {
    opacity: 1;
}

@keyframes route-flow {
    from { background-position: -40% 0; }
    to { background-position: 140% 0; }
}

/* Web 运行模式：顶部保留主题与设置，左侧让给固定导航 */
.basic-layout.is-web {
    --side-nav-w: 208px;
}

/* 顶部栏左端让开侧栏，由侧栏的品牌区占位，视觉上连成一整列 */
.basic-layout.is-web :deep(.win-titlebar) {
    left: var(--side-nav-w);
}

.layout-content {
    left: var(--side-nav-w);
    top: var(--titlebar-h);
    position: fixed;
    width: calc(100% - var(--side-nav-w) - (2*var(--base-padding)));
    height: calc(100% - var(--titlebar-h) - (2*var(--base-padding)));
    padding: var(--base-padding);
    background: var(--color-bg-1);
    overflow-y: scroll;
}

/* 无任务时徽标不亮但按钮常驻：入口位置固定，不随任务有无跳动 */
.tb-hub-idle :deep(.arco-badge-number),
.tb-hub-idle :deep(.arco-badge-dot) {
    display: none;
}
</style>
