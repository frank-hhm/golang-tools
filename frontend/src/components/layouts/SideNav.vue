<template>
    <!--
        左侧固定导航（仅 Web 运行模式）：桌面版靠"点图标开原生新窗口"切换工具，
        Web 模式没有窗口可开，全部在一个页面里，必须有一条常驻入口。
        数据源与首页快捷操作一致（config.tools），所以设置页里增删排序会同步反映到这里。
    -->
    <aside class="side-nav">
        <div class="nav-brand">
            <div class="brand-badge">
                <img v-if="logo" :src="logo" class="brand-logo" alt="" />
                <span v-else class="brand-glyph">{{ initial(title) }}</span>
            </div>
            <span class="brand-text" :title="title">{{ title }}</span>
        </div>
        <nav class="nav-list">
            <div class="nav-item" :class="{ 'is-active': isActive('/index') }" @click="go('/index')"
                @mouseenter="prefetchRoute('/index')" @pointerdown="prefetchRoute('/index')">
                <icon-home class="nav-icon" />
                <span class="nav-text">首页</span>
            </div>

            <div class="nav-group">工具</div>

            <div v-for="t in tools" :key="t.id || t.path" class="nav-item"
                :class="{ 'is-active': isActive(t.path) }" @click="go(t.path)"
                @mouseenter="prefetchRoute(t.path)" @focusin="prefetchRoute(t.path)"
                @pointerdown="prefetchRoute(t.path)">
                <i v-if="isIconfontIcon(t.icon)" class="icon nav-icon" :class="t.icon" />
                <component v-else :is="iconComp(t.icon) || 'IconApps'" class="nav-icon" />
                <span class="nav-text" :title="t.name">{{ t.name }}</span>
            </div>

            <div v-if="!tools.length" class="nav-empty">暂无可用的工具</div>
        </nav>

        <!--
            底部账号区：Web 模式下这里就是当前登录的面板账号。
            桌面版没有登录概念（panel 为空），整块不渲染。
        -->
        <div v-if="panel" class="nav-user">
            <span class="user-avatar">{{ initial(panel.username) }}</span>
            <span class="user-meta">
                <span class="user-name" :title="panel.username">{{ panel.username || "admin" }}</span>
                <span class="user-sub">{{ panel.enabled ? "已登录" : "未开启验证" }}</span>
            </span>
            <!-- 未开启登录保护时没有"登录态"可退，按钮也隐藏：点它只会被 /login 弹回首页 -->
            <a-tooltip v-if="panel.enabled" content="退出登录" position="top">
                <span class="user-exit" @click="onLogout">
                    <icon-export class="user-exit-icon" />
                </span>
            </a-tooltip>
        </div>
    </aside>
</template>

<script setup lang="ts">
import { getCurrentInstance, onMounted, onUnmounted, ref } from "vue";
import { useRoute, useRouter } from "vue-router";
import { Events } from "@wailsio/runtime";
import { arcoIconComp as iconComp, isIconfontIcon } from "@/utils/tool-icon";
import { navToolsFromConfig, type NavTool } from "@/utils/navtools";
import { prefetchRoute } from "@/router/prefetch";
import { useCaps } from "@/utils/caps";
import { getPanelInfo, logoutPanel, type PanelInfo } from "@/utils/panel";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const route = useRoute();
const router = useRouter();
const caps = useCaps();

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
/** 当前登录的面板账号（仅 Web/服务模式有值，桌面版恒为空） */
const panel = ref<PanelInfo | null>(null);

onMounted(() => {
    void load();
    // 设置页保存工具列表后同步侧栏（增删 / 改名 / 换图标 / 调顺序）
    try {
        const ret = Events.On("config:saved", () => { void load(); });
        if (typeof ret === "function") off = ret as () => void;
    } catch { /* 非 Wails 环境没有事件总线 */ }
    // 只有 Web/服务模式才有面板账号；桌面版这块接口不存在，不去请求
    if (!caps.nativeWindows) {
        getPanelInfo().then((info) => { panel.value = info; }).catch(() => { /* 未登录由全局守卫接管 */ });
    }
});
onUnmounted(() => { off?.(); });

/* ===================== 底部账号区 ===================== */

/** 头像取首字母：中英文都能出一个字符，空值兜底问号 */
function initial(v?: string): string {
    const s = (v || "").trim();
    return s ? s.charAt(0).toUpperCase() : "?";
}

async function onLogout() {
    panel.value = null;
    await logoutPanel();
}
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

/* 品牌徽章：有 Logo 时做圆角容器，没 Logo 时用首字母渐变块顶位 */
.brand-badge {
    flex: none;
    width: 26px;
    height: 26px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 8px;
    background: linear-gradient(135deg, rgb(var(--primary-5)), rgb(var(--primary-7)));
    box-shadow: 0 2px 8px rgba(var(--primary-6), 0.28);
    overflow: hidden;
}

.brand-badge .brand-logo {
    width: 100%;
    height: 100%;
    object-fit: cover;
}

.brand-glyph {
    font-size: 14px;
    font-weight: 700;
    color: #fff;
    line-height: 1;
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
    position: relative;
    display: flex;
    align-items: center;
    gap: 10px;
    height: 36px;
    margin-bottom: 2px;
    padding: 0 10px;
    border-radius: var(--border-radius-medium);
    color: var(--color-text-2);
    cursor: pointer;
    user-select: none;
    transition: background-color 0.15s, color 0.15s;
}

/* 激活项左侧的竖条：用伪元素做，不占布局宽度，切换时靠 scaleY 过渡 */
.nav-item::before {
    content: "";
    position: absolute;
    left: 0;
    top: 50%;
    width: 3px;
    height: 18px;
    margin-top: -9px;
    border-radius: 0 3px 3px 0;
    background: rgb(var(--primary-6));
    transform: scaleY(0);
    transition: transform 0.18s ease;
}

.nav-item:hover {
    background: var(--color-fill-2);
    color: var(--color-text-1);
}

.nav-item.is-active {
    background: linear-gradient(90deg, rgba(var(--primary-6), 0.14), rgba(var(--primary-6), 0.04));
    color: rgb(var(--primary-6));
    font-weight: 500;
}

.nav-item.is-active::before {
    transform: scaleY(1);
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

/* ===================== 底部账号区 ===================== */
.nav-user {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 10px 12px;
    border-top: 1px solid var(--color-border-1);
    background: var(--color-fill-1);
}

.user-avatar {
    flex: none;
    width: 28px;
    height: 28px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: 50%;
    background: linear-gradient(135deg, rgb(var(--primary-5)), rgb(var(--primary-7)));
    color: #fff;
    font-size: 13px;
    font-weight: 600;
    line-height: 1;
    box-shadow: 0 2px 6px rgba(var(--primary-6), 0.3);
}

.user-meta {
    flex: 1 1 auto;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
}

.user-name {
    font-size: 12px;
    color: var(--color-text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.user-sub {
    font-size: 11px;
    color: var(--color-text-3);
}

.user-exit {
    flex: none;
    width: 26px;
    height: 26px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: var(--border-radius-medium);
    color: var(--color-text-3);
    cursor: pointer;
    transition: background-color 0.15s, color 0.15s;
}

.user-exit:hover {
    background: rgba(var(--danger-6), 0.12);
    color: rgb(var(--danger-6));
}

.user-exit-icon {
    font-size: 15px;
}
</style>
