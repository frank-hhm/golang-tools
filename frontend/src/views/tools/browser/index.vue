<template>
    <div class="browser-page">
        <!-- 标题栏左：标签栏（页签式）。网页区是 Go 侧嵌在本窗口里的原生层，
             所以页签/地址栏都只能画在网页区之外。 -->
        <common-title-bar>
            <template #title>
                <div ref="tabsWrapRef" class="tabs-wrap">
                    <div v-for="t in tabs" :key="t.id" class="tab"
                        :class="{ active: t.id === activeTabId, suspended: t.suspended && t.id !== activeTabId }"
                        @click="onTabChange(t.id)">
                        <span class="tab-title">
                            <a-spin v-if="t.loading" :size="10" />
                            <span class="tab-text">{{ tabLabel(t) }}</span>
                        </span>
                        <a-tooltip content="关闭标签" :disabled="tabs.length <= 1">
                            <span class="tab-close" :class="{ disabled: tabs.length <= 1 }"
                                @click.stop="closeTabIfPossible(t.id)">
                                <icon-close />
                            </span>
                        </a-tooltip>
                    </div>
                    <a-tooltip content="新建标签（Ctrl+T）">
                        <span class="tab-add" @click="newTab"><icon-plus /></span>
                    </a-tooltip>
                </div>
            </template>

            <template #extra>
                <a-tooltip v-if="session.available" :content="`恢复上次 ${session.tabs} 个标签`">
                    <a-button size="small" @click="restoreSession">
                        <template #icon><icon-history /></template>
                    </a-button>
                </a-tooltip>
                <a-tooltip :content="statsTag || '资源占用（点击刷新）'">
                    <a-button size="small" @click="refreshStats">
                        <template #icon><icon-dashboard /></template>
                    </a-button>
                </a-tooltip>
                <a-tooltip content="开发者工具">
                    <a-button size="small" @click="devtools">
                        <template #icon><icon-code /></template>
                    </a-button>
                </a-tooltip>
                <a-tooltip content="浏览器设置">
                    <a-button size="small" @click="openSettings">
                        <template #icon><icon-settings /></template>
                    </a-button>
                </a-tooltip>
            </template>
        </common-title-bar>

        <!-- 导航条：空间 → 后退/前进/刷新 → 地址栏 -->
        <div class="bar">
            <div class="space-wrap">
                <a-select v-model="spaceId" class="space" size="small" :options="spaceOptions" @change="switchSpace" />
            </div>
            <a-tooltip content="后退">
                <a-button size="small" :disabled="!activeTab?.canGoBack" @click="back">
                    <template #icon><icon-left /></template>
                </a-button>
            </a-tooltip>
            <a-tooltip content="前进">
                <a-button size="small" :disabled="!activeTab?.canGoForward" @click="forward">
                    <template #icon><icon-right /></template>
                </a-button>
            </a-tooltip>
            <a-tooltip :content="activeTab?.loading ? '停止' : '刷新'">
                <a-button size="small" @click="reloadOrStop">
                    <template #icon>
                        <icon-close v-if="activeTab?.loading" />
                        <icon-refresh v-else />
                    </template>
                </a-button>
            </a-tooltip>

            <a-input ref="addrRef" v-model="addr" class="addr" size="small" allow-clear
                placeholder="输入网址或搜索内容，回车打开" @press-enter="go(addr)" />

            <a-tooltip content="打开">
                <a-button size="small" :loading="navigating" @click="go(addr)">
                    <template #icon><icon-arrow-right /></template>
                </a-button>
            </a-tooltip>
        </div>

        <!-- 网页区占位：真实像素由 Go 侧的 WebView2 画在上面，这里只负责量尺寸 -->
        <div ref="areaRef" class="area">
            <div v-if="!ready" class="hint">
                <a-spin :size="20" />
                <span>{{ hint }}</span>
            </div>
        </div>

        <browser-settings v-model:visible="settingsVisible" :config="config" @saved="onSettingsSaved" />
    </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Events, Window } from "@wailsio/runtime";
import BrowserSettings from "./BrowserSettings.vue";

/**
 * 自研浏览器（M2）
 *
 * 本页不加载任何网页：网页区是 Go 侧在"本窗口 HWND"上挂的 WebView2 原生层。
 * 前端只负责：页签栏、地址栏、导航按钮、空间切换、设置抽屉、以及接住 Go 推来的状态。
 *
 * 层级（Go 侧）：空间宿主窗口 → 标签宿主窗口 → WebView2 controller。
 * 一个空间 = 一份 WebView2 environment（独立 profile + 独立代理），空间内多标签共享登录态。
 */
const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

interface TabInfo {
    id: string;
    title: string;
    url: string;
    active: boolean;
    loading: boolean;
    canGoBack: boolean;
    canGoForward: boolean;
    suspended: boolean;
    error?: string;
}

const areaRef = ref<HTMLElement | null>(null);
const tabsWrapRef = ref<HTMLElement | null>(null);
const addrRef = ref<any>(null);
const addr = ref("");
const tabs = ref<TabInfo[]>([]);
const activeTabId = ref("");
const config = ref<any>({ spaces: [], searchURL: "https://www.bing.com/search?q=%s", maxSpaces: 5 });
const spaceId = ref("");
const runtimeVer = ref("");

const ready = ref(false);
const navigating = ref(false);
const hint = ref("正在启动网页区…");
const stats = ref<{ spaces: number; tabs: number; processes: number; rssBytes: number } | null>(null);
const settingsVisible = ref(false);
const session = ref<{ available: boolean; tabs: number; savedAt?: string }>({ available: false, tabs: 0 });

const activeTab = computed(() => tabs.value.find((t) => t.id === activeTabId.value) || tabs.value.find((t) => t.active) || null);
const spaceOptions = computed(() => (config.value.spaces || []).map((s: any) => ({ label: s.name || s.id, value: s.id })));
const memText = computed(() => {
    const b = stats.value?.rssBytes || 0;
    return b >= 1024 * 1024 * 1024 ? `${(b / 1024 / 1024 / 1024).toFixed(2)}GB` : `${(b / 1024 / 1024).toFixed(0)}MB`;
});
const statsTag = computed(() => {
    if (!stats.value) return "";
    return `${stats.value.tabs} 标签 / ${stats.value.processes} 进程 / ${memText.value}`;
});

/** 页签显示的标题：页面标题优先，退化成域名，再退化成"新标签页" */
const tabLabel = (t: TabInfo): string => {
    const title = (t.title || "").trim();
    if (title) return title;
    const url = (t.url || "").trim();
    if (!url || url === "about:blank") return "新标签页";
    try {
        return new URL(url).host || url;
    } catch {
        return url;
    }
};

/** 网页区矩形：CSS 像素、相对窗口客户区，正好等于 Go 侧要的 DIP 坐标 */
const measure = (): { x: number; y: number; w: number; h: number } | null => {
    const el = areaRef.value;
    if (!el) return null;
    const r = el.getBoundingClientRect();
    return { x: Math.round(r.left), y: Math.round(r.top), w: Math.round(r.width), h: Math.round(r.height) };
};

/** 当前窗口名（Go 侧据此找到 HWND）；取不到就留空，由后端退回单例窗口名 */
const currentWindowName = async (): Promise<string> => {
    try {
        return (await Window.Name()) || "";
    } catch {
        return "";
    }
};

/** 把地址输入整理成可导航 URL：像域名的补 https，其它走搜索模板 */
const normalizeURL = (raw: string): string => {
    const v = raw.trim();
    if (!v) return "";
    if (/^(https?:|about:|file:|data:|chrome:|edge:|view-source:)/i.test(v)) return v;
    if (/^[\w.-]+\.[a-z]{2,}(:\d+)?(\/|$)/i.test(v) && !/\s/.test(v)) return "https://" + v;
    return String(config.value.searchURL || "https://www.bing.com/search?q=%s").replace("%s", encodeURIComponent(v));
};

let boundsRaf = 0;
/** 尺寸变化后合并到一帧再推给 Go，避免拖动缩放时把消息队列打满 */
const pushBounds = () => {
    if (!ready.value || boundsRaf) return;
    boundsRaf = requestAnimationFrame(async () => {
        boundsRaf = 0;
        const r = measure();
        if (!r || !spaceId.value) return;
        try {
            await GoTools.BrowserSetBounds(spaceId.value, r.x, r.y, r.w, r.h);
        } catch {
            /* 尺寸更新失败不影响使用，下一帧会重试 */
        }
    });
};

/** 挂载网页区（首次会创建 WebView2 环境，约 200~400ms） */
const attach = async (showHint = true) => {
    const r = measure();
    if (!r) return;
    if (showHint) hint.value = "正在启动网页区…";
    try {
        const name = await currentWindowName();
        await GoTools.BrowserAttach(spaceId.value, name, r.x, r.y, Math.max(80, r.w), Math.max(60, r.h));
        ready.value = true;
        await syncTabs();
    } catch (e: any) {
        ready.value = false;
        const msg = String(e?.message || e);
        hint.value = msg;
        Message.error(`挂载失败：${msg}`);
    }
};

/** 从 Go 侧拉一次标签快照（事件是增量，这里用于初始化/兜底） */
const syncTabs = async () => {
    try {
        const st = await GoTools.BrowserSpaceState(spaceId.value);
        applyTabs(st?.tabs || [], st?.activeTab || "");
    } catch {
        /* 空间还没建好时忽略 */
    }
};

/** 用快照覆盖标签状态，并把地址栏同步成活动标签的地址 */
const applyTabs = (list: TabInfo[], active: string) => {
    const prev = activeTabId.value;
    tabs.value = list;
    if (active) activeTabId.value = active;
    else if (!tabs.value.some((t) => t.id === activeTabId.value)) activeTabId.value = tabs.value[0]?.id || "";
    setAddrFromActive();
    if (prev !== activeTabId.value) scrollActiveIntoView();
};

/** 焦点是否还在地址框里：用户在输入时不要被状态回灌覆盖 */
const addrFocused = (): boolean => {
    const el = addrRef.value?.$el as HTMLElement | null | undefined;
    return !!el && !!document.activeElement && el.contains(document.activeElement);
};

/** 活动标签的地址回填地址栏；新标签（about:blank/空）要清空，避免残留上一个页签的地址 */
const setAddrFromActive = () => {
    if (addrFocused()) return;
    const url = activeTab.value?.url || "";
    addr.value = url && url !== "about:blank" ? url : "";
};

/** 激活页签后把它滚进可视区（标签多到溢出时） */
const scrollActiveIntoView = () => {
    nextTick(() => {
        const el = tabsWrapRef.value?.querySelector(".tab.active") as HTMLElement | null;
        el?.scrollIntoView({ block: "nearest", inline: "nearest" });
    });
};

const patchTab = (tabId: string, patch: Partial<TabInfo>) => {
    const t = tabs.value.find((x) => x.id === tabId);
    if (t) Object.assign(t, patch);
    if (tabId === activeTabId.value) setAddrFromActive();
};

const go = async (raw?: string) => {
    const url = normalizeURL(String(raw ?? addr.value));
    if (!url) return;
    if (!ready.value) await attach();
    navigating.value = true;
    try {
        await GoTools.BrowserNavigate(spaceId.value, url);
        addr.value = url;
    } catch (e: any) {
        Message.error(`打开失败：${String(e?.message || e)}`);
    } finally {
        navigating.value = false;
    }
};

const back = async () => {
    try {
        await GoTools.BrowserBack(spaceId.value);
    } catch (e: any) {
        Message.error(String(e?.message || e));
    }
};

const forward = async () => {
    try {
        await GoTools.BrowserForward(spaceId.value);
    } catch (e: any) {
        Message.error(String(e?.message || e));
    }
};

const reloadOrStop = async () => {
    try {
        if (activeTab.value?.loading) await GoTools.BrowserStop(spaceId.value);
        else await GoTools.BrowserReload(spaceId.value);
    } catch (e: any) {
        Message.error(String(e?.message || e));
    }
};

const newTab = async () => {
    try {
        const id = await GoTools.BrowserNewTab(spaceId.value, "", true);
        if (id) {
            activeTabId.value = id;
            await syncTabs();
            addr.value = ""; // 新标签是空页，地址栏不能留着上一个页签的地址
            scheduleStats();
            scrollActiveIntoView();
            nextTick(() => addrRef.value?.focus?.());
        }
    } catch (e: any) {
        Message.error(`新建标签失败：${String(e?.message || e)}`);
    }
};

const closeTab = async (tabId: string) => {
    try {
        await GoTools.BrowserCloseTab(spaceId.value, tabId);
        await syncTabs();
        scheduleStats();
    } catch (e: any) {
        Message.error(`关闭标签失败：${String(e?.message || e)}`);
    }
};

/** 只剩一个标签时不给关（Go 侧也会自动补一个，这里直接不响应） */
const closeTabIfPossible = (tabId: string) => {
    if (tabs.value.length <= 1) return;
    closeTab(tabId);
};

const onTabChange = async (tabId: string) => {
    const id = String(tabId || "");
    if (!id) return;
    const switching = id !== activeTabId.value;
    if (switching) activeTabId.value = id; // 先按点击高亮，不等后端往返
    try {
        await GoTools.BrowserActivateTab(spaceId.value, id);
        await syncTabs();
        if (switching) scrollActiveIntoView();
    } catch (e: any) {
        Message.error(String(e?.message || e));
        await syncTabs();
    }
};

/** 切空间：旧空间关掉（释放 profile 锁与内存），新空间重新挂载 */
const switchSpace = async () => {
    ready.value = false;
    tabs.value = [];
    addr.value = "";
    hint.value = "正在切换空间…";
    try {
        await GoTools.BrowserCloseAll();
    } catch {
        /* ignore */
    }
    await attach();
    refreshStats();
};

/** 网页区里按下的宿主级快捷键（Go 侧注入脚本捕获后回传） */
const handleShortcut = async (action: string) => {
    switch (action) {
        case "newtab":
            await newTab();
            break;
        case "closetab":
            if (activeTabId.value) await closeTab(activeTabId.value);
            break;
        case "reload":
            await reloadOrStop();
            break;
        case "back":
            await back();
            break;
        case "forward":
            await forward();
            break;
        case "focusurl":
            // 先把 Win32 键盘焦点从网页区换回前端，再聚焦地址栏
            try {
                await GoTools.BrowserFocusShell(spaceId.value);
            } catch {
                /* ignore */
            }
            setTimeout(() => {
                addrRef.value?.focus?.();
                addrRef.value?.select?.();
            }, 30);
            break;
        case "nexttab":
        case "prevtab":
            cycleTab(action === "nexttab" ? 1 : -1);
            break;
    }
};

const cycleTab = (delta: number) => {
    const list = tabs.value;
    if (list.length < 2) return;
    const idx = list.findIndex((t) => t.id === activeTabId.value);
    const next = list[(idx + delta + list.length) % list.length];
    if (next) onTabChange(next.id);
};

const devtools = async () => {
    try {
        await GoTools.BrowserDevTools(spaceId.value);
    } catch (e: any) {
        Message.error(`打开开发者工具失败：${String(e?.message || e)}`);
    }
};

const refreshStats = async () => {
    try {
        stats.value = await GoTools.BrowserStats();
    } catch {
        /* ignore */
    }
};

let statsTimer = 0;
/** 标签结构变化后刷新资源统计：多次变化合并成一次，统计要枚举进程不能太勤 */
const scheduleStats = () => {
    if (statsTimer) clearTimeout(statsTimer);
    statsTimer = window.setTimeout(() => {
        statsTimer = 0;
        refreshStats();
    }, 500);
};

const openSettings = async () => {
    try {
        config.value = await GoTools.BrowserConfig();
    } catch {
        /* ignore */
    }
    settingsVisible.value = true;
};

/** 设置保存后：代理/UA 是 environment 级参数，重建当前空间才生效 */
const onSettingsSaved = async () => {
    const r = measure();
    if (!r || !ready.value) return;
    try {
        const name = await currentWindowName();
        await GoTools.BrowserRebuild(spaceId.value, name, r.x, r.y, Math.max(80, r.w), Math.max(60, r.h));
        await syncTabs();
        Message.success("设置已保存，网页区已按新设置重建");
    } catch (e: any) {
        Message.error(`重建网页区失败：${String(e?.message || e)}`);
    }
};

const restoreSession = async () => {
    try {
        const n = await GoTools.BrowserRestoreSession(spaceId.value);
        await syncTabs();
        Message.success(`已恢复 ${n} 个标签`);
        session.value = { available: false, tabs: 0 };
    } catch (e: any) {
        Message.error(`恢复失败：${String(e?.message || e)}`);
    }
};

/** 设置抽屉开关时显隐原生网页区：它是真窗口，永远画在 HTML 之上，不藏就看不到抽屉 */
watch(settingsVisible, async (open) => {
    if (!spaceId.value) return;
    try {
        await GoTools.BrowserSetVisible(spaceId.value, !open);
    } catch {
        /* 空间还没建好时无需要藏 */
    }
});

/**
 * 前端自己拿到按键时（焦点在地址栏/按钮上）走同一套快捷键。
 * 焦点在网页区里时按键被 WebView2 吃掉，由 Go 注入的脚本 postMessage 回传，
 * 两边最终都汇到 handleShortcut。
 */
const onKeyDown = (e: KeyboardEvent) => {
    const ctrl = e.ctrlKey || e.metaKey;
    if (ctrl && !e.altKey) {
        switch (e.key.toLowerCase()) {
            case "t":
                e.preventDefault();
                handleShortcut("newtab");
                return;
            case "w":
                e.preventDefault();
                handleShortcut("closetab");
                return;
            case "l":
                e.preventDefault();
                handleShortcut("focusurl");
                return;
            case "r":
                e.preventDefault();
                handleShortcut("reload");
                return;
            case "tab":
                e.preventDefault();
                handleShortcut(e.shiftKey ? "prevtab" : "nexttab");
                return;
        }
    }
    if (e.altKey && !ctrl) {
        if (e.key === "ArrowLeft") {
            e.preventDefault();
            handleShortcut("back");
            return;
        }
        if (e.key === "ArrowRight") {
            e.preventDefault();
            handleShortcut("forward");
            return;
        }
    }
    if (e.key === "F5") {
        e.preventDefault();
        handleShortcut("reload");
    }
};

let offEvent: (() => void) | null = null;
let observer: ResizeObserver | null = null;

onMounted(async () => {
    config.value = await GoTools.BrowserConfig();
    spaceId.value = config.value.defaultSpace || config.value.spaces?.[0]?.id || "default";
    runtimeVer.value = (await GoTools.BrowserRuntime()) || "";
    if (!runtimeVer.value) {
        hint.value = "未找到 WebView2 运行时";
        Message.error("本机没有可用的 WebView2 运行时，网页区无法启动");
        return;
    }

    try {
        session.value = await GoTools.BrowserSessionInfo();
    } catch {
        /* ignore */
    }

    offEvent = Events.On("browser:event", (raw: any) => {
        // Wails 的 On 回调拿到的是 { name, data, sender }，真正的内容在 data 里
        const ev: any = raw?.data ?? raw;
        if (!ev || ev.spaceId !== spaceId.value) return;
        switch (ev.kind) {
            case "ready":
                ready.value = true;
                break;
            case "tabs":
                applyTabs(ev.tabs || [], ev.activeTab || "");
                scheduleStats();
                break;
            case "state":
                patchTab(ev.tabId, { url: ev.url, title: ev.title });
                break;
            case "title":
                patchTab(ev.tabId, { title: ev.title });
                break;
            case "shortcut":
                handleShortcut(ev.message);
                break;
            case "newWindow":
                refreshStats();
                break;
            case "error":
                Message.error(`网页区错误：${ev.message}`);
                break;
            case "crash":
                ready.value = false;
                hint.value = "渲染进程崩溃，点击刷新重新挂载";
                Message.error(`渲染进程崩溃：${ev.message}`);
                break;
        }
    });

    observer = new ResizeObserver(() => pushBounds());
    if (areaRef.value) observer.observe(areaRef.value);
    window.addEventListener("resize", pushBounds);
    window.addEventListener("keydown", onKeyDown, true);

    await attach();
    refreshStats();
});

onBeforeUnmount(() => {
    if (boundsRaf) cancelAnimationFrame(boundsRaf);
    if (statsTimer) clearTimeout(statsTimer);
    observer?.disconnect();
    window.removeEventListener("resize", pushBounds);
    window.removeEventListener("keydown", onKeyDown, true);
    offEvent?.();
    // 窗口关闭/切走时释放 WebView2（不然进程与 profile 锁会一直挂着）
    GoTools?.BrowserClose(spaceId.value)?.catch?.(() => { });
});
</script>

<style scoped>
/* 铺满内容区：position:absolute + inset:0 会覆盖 BlankLayout 的 padding，
   保证量出来的矩形正好等于窗口客户区里的网页区。 */
.browser-page {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    background: var(--color-bg-1);
}

/* 页签栏塞在 40px 的标题栏里：高度压到 26px，横向溢出就滚动但藏掉滚动条 */
.tabs-wrap {
    display: flex;
    align-items: center;
    gap: 2px;
    flex: 1 1 auto;
    min-width: 0;
    height: 26px;
    overflow-x: auto;
    overflow-y: hidden;
    scrollbar-width: none;
}

.tabs-wrap::-webkit-scrollbar {
    display: none;
}

/* 自绘页签：高亮只由 activeTabId 决定，不依赖组件内部选中态 */
.tab {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    flex: 0 0 auto;
    max-width: 180px;
    height: 26px;
    padding: 0 6px 0 8px;
    border-radius: var(--border-radius-small);
    color: var(--color-text-2);
    cursor: pointer;
    transition: background-color 0.15s, color 0.15s;
}

.tab:hover {
    background: var(--color-fill-2);
}

.tab.active {
    background: var(--color-fill-3);
    color: var(--color-text-1);
    font-weight: 500;
}

/* 非活动且已挂起的标签淡一点，提示它已被 TrySuspend 挂起 */
.tab.suspended .tab-text {
    opacity: 0.6;
}

.tab-title {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    min-width: 0;
    max-width: 140px;
}

.tab-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.tab-close {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 16px;
    height: 16px;
    border-radius: 3px;
    font-size: 12px;
    color: var(--color-text-3);
}

.tab-close:hover {
    background: var(--color-fill-3);
    color: var(--color-text-1);
}

/* 只剩一个标签时不给关（Go 侧也会自动补一个），按钮占位但隐藏 */
.tab-close.disabled {
    visibility: hidden;
}

.tab-add {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    flex: none;
    width: 24px;
    height: 24px;
    border-radius: var(--border-radius-small);
    font-size: 14px;
    color: var(--color-text-2);
    cursor: pointer;
}

.tab-add:hover {
    background: var(--color-fill-2);
    color: var(--color-text-1);
}

.bar {
    flex: none;
    display: flex;
    align-items: center;
    flex-wrap: nowrap;
    gap: 6px;
    padding: 6px;
    border-bottom: 1px solid var(--color-border-2);
    /* 溢出就藏起来：否则地址框获得焦点时父级会把导航条横向滚过去，左边空间下拉被切掉 */
    overflow: hidden;
}

/* 空间下拉用定宽容器夹住：Arco 的 .arco-select 自带 width:100%，
   直接给组件本身写宽度容易被内部样式压回去，外层定宽最稳 */
.space-wrap {
    flex: 0 0 120px;
    width: 120px;
    min-width: 0;
}

.space-wrap :deep(.arco-select) {
    width: 100%;
}

.bar .addr {
    flex: 1 1 auto;
    min-width: 100px;
}

/* 网页区占位：Go 侧的 WebView2 会用真实像素盖在同一块屏幕区域上 */
.area {
    flex: 1;
    min-height: 0;
    position: relative;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--color-fill-1);
}

.hint {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
    font-size: 12px;
    color: var(--color-text-3);
}
</style>
