<template>
    <div class="pin">
        <!-- 公共标题栏（同取色器等工具窗）：左图钉图标+标题，右复制/最小化/关闭。整栏可拖动 -->
        <WinTitleBar title="图钉">
            <template #icon><icon-pushpin /></template>
            <a-button size="small" @click="copy"><icon-copy /></a-button>
            <a-button size="small" @click="minimise"><icon-minus /></a-button>
            <a-button size="small" @click="close"><icon-close /></a-button>
        </WinTitleBar>
        <div class="pin-body">
            <img v-if="src" :src="src" class="pin-img" draggable="false" alt="" />
        </div>
    </div>
</template>

<script setup lang="ts">
import { getCurrentInstance, onBeforeUnmount, onMounted, ref } from "vue";
import { useRoute } from "vue-router";
import { Message } from "@arco-design/web-vue";
import { Window } from "@wailsio/runtime";
import WinTitleBar from "@/components/layouts/WinTitleBar.vue";

const route = useRoute();
const { proxy }: any = getCurrentInstance() as any;
const $GoTools = proxy?.$GoTools;

const id = Number(route.query.id ?? 0);
const src = ref("");

onMounted(async () => {
    try {
        src.value = await $GoTools.GetPinnedImage(id);
    } catch {
        /* 取图失败：留个空窗，用户可直接关闭 */
    }
});

async function copy() {
    if (!src.value) return;
    try {
        await $GoTools.CopyToClipboard(src.value);
        Message.success("已复制到剪贴板");
    } catch (e: any) {
        Message.error("复制失败：" + (e?.message || e));
    }
}

function minimise() {
    Window.Minimise();
}

function close() {
    try {
        $GoTools.ClosePinnedImage(id);
    } catch {
        /* 窗口已销毁 */
    }
}

function onKey(e: KeyboardEvent) {
    if (e.key === "Escape") close();
}
onMounted(() => window.addEventListener("keydown", onKey));
onBeforeUnmount(() => window.removeEventListener("keydown", onKey));
</script>

<style scoped>
/* 无边框窗：外框阴影由 Wails frameless 装饰提供（同工具窗），内容 = 公共标题栏(40px) + 图片 */
.pin {
    position: fixed;
    inset: 0;
    background: var(--color-bg-1);
    user-select: none;
    overflow: hidden;
}

.pin-body {
    position: absolute;
    top: 40px;
    left: 0;
    right: 0;
    bottom: 0;
    display: flex;
    align-items: center;
    justify-content: center;
}

.pin-img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
    display: block;
    pointer-events: none;
    -webkit-app-region: drag;
}
</style>
