<template>
    <div class="browser-page">
        <!-- 顶栏注入全局标题栏：地址栏等控件必须留在网页区之外（WebView2 会盖住同一块屏幕区域），
             放进标题栏正好腾出整块网页区。#title 靠左吃掉剩余宽度，#extra 排在窗口按钮之前。 -->
        <common-title-bar>
            <template #title>
                <a-radio-group v-model="spaceId" type="button" size="small" class="space-group" @change="switchSpace">
                    <a-radio v-for="sp in spaces" :key="sp.id" :value="sp.id">
                        {{ sp.name || sp.id }}
                    </a-radio>
                </a-radio-group>

                <a-input v-model="addr" class="addr" size="small" placeholder="输入网址或搜索内容，回车打开"
                    allow-clear @press-enter="go(addr)" />

                <a-tooltip content="打开">
                    <a-button size="small" :loading="navigating" @click="go(addr)">
                        <template #icon><icon-arrow-right /></template>
                    </a-button>
                </a-tooltip>
            </template>

            <template #extra>
                <a-tag v-if="stats" size="small" class="stat-tag">
                    {{ stats.spaces }} 空间 / {{ stats.processes }} 进程 / {{ memText }}
                </a-tag>
                <a-tag v-if="proxyTag" size="small" color="arcoblue">{{ proxyTag }}</a-tag>

                <a-tooltip content="重新挂载网页区">
                    <a-button size="small" :loading="attaching" @click="reattach">
                        <template #icon><icon-refresh /></template>
                    </a-button>
                </a-tooltip>
                <a-tooltip content="开发者工具">
                    <a-button size="small" @click="devtools">
                        <template #icon><icon-code /></template>
                    </a-button>
                </a-tooltip>
                <a-tooltip content="资源占用">
                    <a-button size="small" @click="refreshStats">
                        <template #icon><icon-dashboard /></template>
                    </a-button>
                </a-tooltip>
            </template>
        </common-title-bar>

        <!-- 状态条：网页区遮住的地方不能放东西，所以状态一律显示在这里 -->
        <div class="status" :class="statusClass">{{ status }}</div>

        <!-- 网页区占位：真实像素由 Go 侧的 WebView2 画在上面，这里只负责量尺寸 -->
        <div ref="areaRef" class="area">
            <div v-if="!ready" class="hint">
                <a-spin :size="20" />
                <span>{{ hint }}</span>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onBeforeUnmount, onMounted, ref } from "vue";
import { Events, Window } from "@wailsio/runtime";

/**
 * 自研浏览器（M1 宿主验证页）
 *
 * 本页不加载任何网页：网页区是 Go 侧在"本窗口 HWND"上挂的第二个 WebView2 controller。
 * 前端只做三件事：量网页区矩形、画地址栏/空间栏、接收 Go 推来的状态。
 * 这样地址栏/标签栏是纯前端（想怎么改就怎么改），渲染内核是系统 WebView2（Chromium）。
 */
const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const areaRef = ref<HTMLElement | null>(null);
const spaces = ref<any[]>([]);
const spaceId = ref("");
const searchURL = ref("https://www.bing.com/search?q=%s");
const addr = ref("");
const runtimeVer = ref("");

const ready = ref(false);
const attaching = ref(false);
const navigating = ref(false);
const hint = ref("正在启动网页区…");
const status = ref("");
const statusLevel = ref<"info" | "ok" | "warn" | "error">("info");
const title = ref("");
const stats = ref<{ spaces: number; processes: number; rssBytes: number } | null>(null);

const memText = computed(() => {
    const b = stats.value?.rssBytes || 0;
    return b >= 1024 * 1024 * 1024 ? `${(b / 1024 / 1024 / 1024).toFixed(2)}GB` : `${(b / 1024 / 1024).toFixed(0)}MB`;
});
const statusClass = computed(() => `is-${statusLevel.value}`);
const proxyTag = computed(() => {
    const sp = spaces.value.find((s) => s.id === spaceId.value);
    return sp?.proxy ? `代理 ${sp.proxy}` : "";
});

/** 统一设置状态条 */
const setStatus = (text: string, level: "info" | "ok" | "warn" | "error" = "info") => {
    status.value = text;
    statusLevel.value = level;
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
    return searchURL.value.replace("%s", encodeURIComponent(v));
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
    attaching.value = true;
    if (showHint) hint.value = "正在启动网页区…";
    try {
        const name = await currentWindowName();
        await GoTools.BrowserAttach(spaceId.value, name, r.x, r.y, Math.max(80, r.w), Math.max(60, r.h));
        ready.value = true;
        setStatus(`网页区已就绪 · WebView2 ${runtimeVer.value || "未知版本"}`, "ok");
    } catch (e: any) {
        ready.value = false;
        const msg = String(e?.message || e);
        hint.value = msg;
        setStatus(`挂载失败：${msg}`, "error");
    } finally {
        attaching.value = false;
    }
};

/** 重新挂载：先关掉再挂（用于网页区白屏/被窗口操作弄丢时的自救） */
const reattach = async () => {
    try {
        await GoTools.BrowserClose(spaceId.value);
    } catch {
        /* 没开过就无所谓 */
    }
    ready.value = false;
    await attach();
};

/** 切空间：旧空间关掉（释放 profile 锁），新空间重新挂载 */
const switchSpace = async () => {
    ready.value = false;
    hint.value = "正在切换空间…";
    try {
        await GoTools.BrowserCloseAll();
    } catch {
        /* ignore */
    }
    await attach();
    refreshStats();
};

const go = async (raw?: string) => {
    const url = normalizeURL(String(raw ?? addr.value));
    if (!url) return;
    if (!ready.value) await attach();
    navigating.value = true;
    try {
        await GoTools.BrowserNavigate(spaceId.value, url);
        addr.value = url;
        setStatus(`正在打开 ${url}`, "info");
    } catch (e: any) {
        setStatus(`打开失败：${String(e?.message || e)}`, "error");
    } finally {
        navigating.value = false;
    }
};

const devtools = async () => {
    try {
        await GoTools.BrowserDevTools(spaceId.value);
    } catch (e: any) {
        setStatus(`打开开发者工具失败：${String(e?.message || e)}`, "error");
    }
};

const refreshStats = async () => {
    try {
        stats.value = await GoTools.BrowserStats();
    } catch {
        /* ignore */
    }
};

let offEvent: (() => void) | null = null;
let observer: ResizeObserver | null = null;

onMounted(async () => {
    const cfg = await GoTools.GetConfig();
    const bc = cfg?.browser || {};
    spaces.value = bc.spaces || [];
    spaceId.value = bc.defaultSpace || spaces.value[0]?.id || "default";
    if (bc.searchURL) searchURL.value = bc.searchURL;
    runtimeVer.value = (await GoTools.BrowserRuntime()) || "";
    if (!runtimeVer.value) {
        setStatus("本机没有可用的 WebView2 运行时，网页区无法启动", "error");
        hint.value = "未找到 WebView2 运行时";
        return;
    }

    offEvent = Events.On("browser:event", (ev: any) => {
        if (!ev || ev.spaceId !== spaceId.value) return;
        switch (ev.kind) {
            case "ready":
                ready.value = true;
                setStatus("网页区已就绪", "ok");
                break;
            case "title":
                title.value = ev.title || "";
                if (!addr.value) addr.value = "";
                setStatus(ev.title ? `已加载：${ev.title}` : "页面已加载", "ok");
                break;
            case "error":
                setStatus(`网页区错误：${ev.message}`, "error");
                break;
            case "crash":
                ready.value = false;
                hint.value = "渲染进程崩溃，点击刷新重新挂载";
                setStatus(`渲染进程崩溃：${ev.message}`, "error");
                break;
        }
    });

    observer = new ResizeObserver(() => pushBounds());
    if (areaRef.value) observer.observe(areaRef.value);
    window.addEventListener("resize", pushBounds);

    await attach();
    refreshStats();
});

onBeforeUnmount(() => {
    if (boundsRaf) cancelAnimationFrame(boundsRaf);
    observer?.disconnect();
    window.removeEventListener("resize", pushBounds);
    offEvent?.();
    // 窗口关闭/切走时释放 WebView2（不然进程与 profile 锁会一直挂着）
    GoTools?.BrowserClose(spaceId.value)?.catch?.(() => {});
});
</script>

<style scoped>
/* 顶栏已注入全局标题栏（common-title-bar），这里只留网页区之下的状态条与占位区 */
.browser-page {
    position: absolute;
    inset: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
    background: var(--color-bg-1);
}

/* 标题栏里的空间按钮组不参与拉伸，地址栏吃掉剩余宽度（窄窗口下最少留 120px） */
.space-group {
    flex: none;
}

.addr {
    flex: 1 1 auto;
    min-width: 120px;
}

/* 资源占用标签文字较长，窄窗口下允许截断，不把标题栏顶爆 */
.stat-tag {
    max-width: 220px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: none;
}

.status {
    flex: none;
    padding: 2px 8px;
    font-size: 12px;
    color: var(--color-text-3);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

.status.is-ok {
    color: rgb(var(--green-6));
}

.status.is-warn {
    color: rgb(var(--orange-6));
}

.status.is-error {
    color: rgb(var(--red-6));
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
