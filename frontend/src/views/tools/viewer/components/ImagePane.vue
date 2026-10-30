<template>
    <div class="img-pane">
        <div v-if="err" class="ip-state ip-err">{{ err }}</div>
        <!-- img 常驻（只受 err 门控）：若把它放进 v-if="!loading" 分支，
             loading 只能靠 img 的 @load 置假 → img 永不渲染 → 永久转圈。 -->
        <div v-else class="ip-canvas" :class="{ actual: actual }">
            <img :src="url" :alt="name" draggable="false" @load="onLoad" @error="onError" />
            <div v-if="loading" class="ip-mask"><a-spin /></div>
        </div>
        <div class="ip-tools">
            <a-tooltip :content="actual ? '适应窗口' : '原始尺寸'">
                <a-button size="mini" @click="actual = !actual">
                    <template #icon><icon-full-screen v-if="!actual" /><icon-launch v-else /></template>
                    {{ actual ? '实际大小' : '适应' }}
                </a-button>
            </a-tooltip>
            <span v-if="natural" class="ip-dim">{{ natural }}</span>
        </div>
    </div>
</template>

<script setup lang="ts">
/*
图片预览：直接指 /fileview/media 流式端点，不经 base64，大图也不噎绑定层。
WebView2 与 server 构建都同源可加载。默认适应窗口，可切原始尺寸（外层滚动）。
*/
import { ref, watch } from "vue";

const props = defineProps<{ url: string; name: string }>();

const loading = ref(true);
const err = ref("");
const actual = ref(false);
const natural = ref("");

function reset() {
    loading.value = true;
    err.value = "";
    actual.value = false;
    natural.value = "";
}

function onLoad(e: Event) {
    loading.value = false;
    const img = e.target as HTMLImageElement;
    if (img?.naturalWidth) natural.value = `${img.naturalWidth} × ${img.naturalHeight}`;
}

function onError() {
    loading.value = false;
    err.value = "图片加载失败";
}

watch(() => props.url, reset, { immediate: true });
</script>

<style scoped>
.img-pane {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}
.ip-canvas {
    position: relative;
    flex: 1;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    overflow: auto;
    padding: var(--base-size-12);
    background: var(--color-fill-1);
}
.ip-mask {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--color-fill-1);
}
.ip-canvas img {
    max-width: 100%;
    max-height: 100%;
    object-fit: contain;
}
.ip-canvas.actual {
    align-items: flex-start;
    justify-content: flex-start;
}
.ip-canvas.actual img {
    max-width: none;
    max-height: none;
}
.ip-state {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--color-text-3);
}
.ip-err {
    color: rgb(var(--danger-6));
}
.ip-tools {
    position: absolute;
    right: var(--base-size-12);
    bottom: var(--base-size-12);
    display: flex;
    align-items: center;
    gap: var(--base-size-8);
    padding: 4px 8px;
    border-radius: var(--base-radius-8);
    background: var(--color-bg-2);
    box-shadow: 0 2px 8px rgb(0 0 0 / 12%);
}
.ip-dim {
    font-size: var(--base-size-12);
    color: var(--color-text-3);
}
</style>
