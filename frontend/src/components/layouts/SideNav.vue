<template>
    <!--
        左侧固定导航（仅 Web 运行模式）：桌面版靠"点图标开原生新窗口"切换工具，
        Web 模式没有窗口可开，全部在一个页面里，必须有一条常驻入口。
        数据源与首页快捷操作一致（config.tools），所以设置页里增删排序会同步反映到这里。
    -->
    <aside class="side-nav">
        <div class="nav-brand">
            <img v-if="logo" :src="logo" class="brand-logo" alt="" />
            <span class="brand-text" :title="title">{{ title }}</span>
        </div>
        <nav class="nav-list">
            <div class="nav-item" :class="{ 'is-active': isActive('/index') }" @click="go('/index')">
                <icon-home class="nav-icon" />
                <span class="nav-text">首页</span>
            </div>

            <div class="nav-group">工具</div>

            <div v-for="t in tools" :key="t.id || t.path" class="nav-item"
                :class="{ 'is-active': isActive(t.path) }" @click="go(t.path)">
                <i v-if="isIconfontIcon(t.icon)" class="icon nav-icon" :class="t.icon" />
                <component v-else :is="iconComp(t.icon) || 'IconApps'" class="nav-icon" />
                <span class="nav-text" :title="t.name">{{ t.name }}</span>
            </div>

            <div v-if="!tools.length" class="nav-empty">暂无可用的工具</div>
        </nav>
    </aside>
</template>

<script setup lang="ts">
import { getCurrentInstance, onMounted, onUnmounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Events } from "@wailsio/runtime";
import { arcoIconComp as iconComp, isIconfontIcon } from "@/utils/tool-icon";
import { navToolsFromConfig, type NavTool } from "@/utils/navtools";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const route = useRoute();
const router = useRouter();

// 品牌区的名称与 Logo 由布局层传下来，避免两处各读一次配置
defineProps<{ title: string; logo?: string }>();

const tools = ref<NavTool[]>([]);

/** 当前路由是否命中该项：带上子路径判断，工具页内部跳转时父入口仍保持高亮 */
const isActive = (path: string) => route.path === path || route.path.startsWith(path + "/");

function go(path: string) {
    if (isActive(path)) return;
    void router.push(path);
}

async function load() {
    try {
        tools.value = navToolsFromConfig(await GoTools?.GetConfig());
    } catch {
        // 取不到配置就先空着，不影响其他导航项
    }
}

let off: (() => void) | null = null;
onMounted(() => {
    void load();
    // 设置页保存工具列表后同步侧栏（增删 / 改名 / 换图标 / 调顺序）
    try {
        const ret = Events.On("config:saved", () => { void load(); });
        if (typeof ret === "function") off = ret as () => void;
    } catch { /* 非 Wails 环境没有事件总线 */ }
});
onUnmounted(() => { off?.(); });
</script>

<style scoped>
.side-nav {
    position: fixed;
    top: 0;
    left: 0;
    bottom: 0;
    width: var(--side-nav-w);
    z-index: 1100; /* 要高过标题栏 z-index:1000，否则左端一截会被它盖住 */
    background: var(--color-bg-2);
    border-right: 1px solid var(--color-border-1);
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

/* 品牌区：高度对齐标题栏，让视觉上变成一整列 */
.nav-brand {
    flex: none;
    height: var(--titlebar-h);
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 0 14px;
    border-bottom: 1px solid var(--color-border-1);
    overflow: hidden;
}

.brand-logo {
    flex: none;
    width: 20px;
    height: 20px;
    object-fit: contain;
}

.brand-text {
    font-size: var(--base-size-medium);
    font-weight: bold;
    color: var(--color-text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.nav-list {
    flex: 1 1 auto;
    overflow-y: auto;
    padding: 8px;
}

.nav-item {
    display: flex;
    align-items: center;
    gap: 10px;
    height: 36px;
    padding: 0 10px;
    border-radius: var(--border-radius-medium);
    color: var(--color-text-2);
    cursor: pointer;
    user-select: none;
    transition: background-color 0.15s, color 0.15s;
}

.nav-item:hover {
    background: var(--color-fill-2);
    color: var(--color-text-1);
}

.nav-item.is-active {
    background: rgba(var(--primary-6), 0.1);
    color: rgb(var(--primary-6));
    font-weight: 500;
}

.nav-icon {
    flex: none;
    width: 18px;
    height: 18px;
    font-size: 16px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
}

.nav-text {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: 13px;
}

/* 分组标题：与小号大写字母区分开，用弱化色压住，不和导航项抢注意力 */
.nav-group {
    margin: 12px 10px 6px;
    font-size: 12px;
    color: var(--color-text-3);
    user-select: none;
}

.nav-empty {
    margin: 8px 10px;
    font-size: 12px;
    color: var(--color-text-3);
}
</style>
