<template>
    <a-row class="shot-page" :gutter="6">
        <!-- 左侧约 25%：截图操作 -->
        <a-col :span="5" class="side-col">
            <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                <template #title>截图操作</template>
                <template #extra>
                    <a-tag size="small" :color="hasImage ? 'green' : 'gray'">
                        {{ hasImage ? "已有截图" : "暂无截图" }}
                    </a-tag>
                </template>
                <div class="side-body">
                    <a-button size="small" status="success" long @click="recapture">
                        <template #icon><icon-camera /></template>重新截图
                    </a-button>
                    <a-button size="small" long :disabled="!hasImage" @click="copyImage">
                        <template #icon><icon-copy /></template>复制图片
                    </a-button>
                    <a-button size="small" long :disabled="!hasImage" @click="saveFile">
                        <template #icon><icon-save /></template>另存为…
                    </a-button>
                    <a-button size="small" long :disabled="!hasImage" @click="pinIt">
                        <template #icon><icon-pushpin /></template>图钉置顶
                    </a-button>
                    <a-divider style="margin: 4px 0" />
                    <div class="tip">
                        预览区已内置标注工具：矩形 / 椭圆 / 箭头 / 画笔 / 马赛克 / 文字，
                        并可撤销、旋转、缩放、裁剪。改完「复制 / 另存 / 图钉」拿到的都是叠加标注后的成品。
                    </div>
                </div>
            </a-card>
        </a-col>

        <!-- 右侧：可编辑预览（标注编辑器为截图页与文件查看器共用的 annotate-canvas） -->
        <a-col :span="19" class="main-col">
            <a-card class="main-panel card-head-tool" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space :size="8">
                        <span>截图预览</span>
                        <a-tag size="small">PNG</a-tag>
                    </a-space>
                </template>

                <annotate-canvas ref="editorRef" empty-text="暂无截图，点击左侧「重新截图」进行区域捕获"
                    @change="hasImage = $event" />
            </a-card>
        </a-col>
    </a-row>
</template>

<script setup lang="ts">
/**
 * 截图页：取图 + 标注编辑 + 桌面落盘。
 *
 * 标注编辑器（工具栏/画布/旋转/裁剪/文字）抽到全局组件 annotate-canvas，
 * 与文件查看器的图片编辑模式共用一套引擎（captureDraw.ts）。本页只留
 * 「与截图这件事相关」的部分：区域截图、共享存储回读、另存 / 复制 / 图钉
 * 这三个 Windows 专属落盘动作（见 services/app/capture.go）。
 *
 * 选区浮层（views/widget/captureOverlay）不走本页，它直接用 captureDraw，
 * 与本次重构无关。
 */
import { getCurrentInstance, onMounted, onUnmounted, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { sysNotify } from "@/utils/notify";

const {
    proxy: { $GoTools },
} = getCurrentInstance() as any;

/** 与首页事件约定的共享存储键（同源多窗口共享 localStorage） */
const STORAGE_KEY = "tool:screenshot";

const editorRef = ref<any>(null);
const hasImage = ref(false);

/* ---------------- 桌面落盘 ---------------- */
async function saveFile() {
    if (!hasImage.value) return;
    const data = editorRef.value?.exportDataURL() as string;
    if (!data) return;
    try {
        const path = await $GoTools.SaveCaptureFile(data);
        if (path) {
            // 保存成功：浮层/窗口可能已不在前台，走系统原生通知（右下角 Toast）
            sysNotify("截图已保存", path);
        }
    } catch (e: any) {
        Message.error("保存失败：" + (e?.message || e));
    }
}

async function copyImage() {
    if (!hasImage.value) return;
    const data = editorRef.value?.exportDataURL() as string;
    if (!data) return;
    try {
        await $GoTools.CopyToClipboard(data);
        Message.success("图片已复制到剪贴板");
    } catch (e: any) {
        Message.error("复制失败：" + (e?.message || e));
    }
}

async function pinIt() {
    if (!hasImage.value) return;
    const data = editorRef.value?.exportDataURL() as string;
    if (!data) return;
    try {
        // 图钉出现在屏幕中央附近：给一个合理尺寸，后端按 dpr 换算几何
        const { w: iw, h: ih } = editorRef.value?.naturalSize() as { w: number; h: number };
        const w = Math.min(iw, 900);
        const h = ih ? Math.round(w * ih / iw) : 400;
        await $GoTools.OpenPinnedImage(data, 120, 120, w, h);
    } catch (e: any) {
        Message.error("图钉失败：" + (e?.message || e));
    }
}

/** 重新触发区域截图（走选区浮层，成品由浮层写回共享存储并广播） */
const recapture = async () => {
    try {
        if (typeof $GoTools?.OpenCaptureOverlay === "function") {
            await $GoTools.OpenCaptureOverlay();
            return;
        }
        const data = await $GoTools.CaptureSelectedRegion("png");
        if (!data) return;
        editorRef.value?.load(data);
        Message.success("截图完成");
    } catch (e: any) {
        Message.error("截图失败：" + (e?.message || e));
    }
};

/* ---------------- 生命周期 ---------------- */
const reloadFromStorage = () => {
    let raw = "";
    try { raw = localStorage.getItem(STORAGE_KEY) || ""; } catch { /* */ }
    if (raw) editorRef.value?.load(raw);
};

let offUpdated: (() => void) | null = null;
onMounted(() => {
    reloadFromStorage();
    try {
        const r = Events.On("screenshot:updated", () => reloadFromStorage());
        if (typeof r === "function") offUpdated = r as () => void;
    } catch { /* */ }
});
onUnmounted(() => {
    offUpdated?.();
    offUpdated = null;
});
</script>

<style scoped>
/* 页面根容器：a-row 铺满工具窗口内容区 */
.shot-page {
    height: 100%;
    min-height: 0;
    flex-wrap: nowrap;
}

.side-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.side-card {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.side-card :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.side-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--base-padding);
    padding: var(--base-padding);
    overflow-y: auto;
}

.tip {
    margin-top: auto;
    color: var(--color-text-3);
    font-size: var(--base-size-small);
    line-height: 1.8;
}

.main-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.main-panel {
    width: 100%;
    height: 100%;
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    background: var(--color-bg-2);
}

.main-panel :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}
</style>
