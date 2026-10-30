<template>
    <div class="media-pane">
        <video v-if="isVideo" class="mp-el" :src="url" controls preload="metadata"></video>
        <div v-else class="mp-audio-wrap">
            <div class="mp-audio-icon"><icon-music /></div>
            <audio class="mp-audio" :src="url" controls preload="metadata"></audio>
            <div class="mp-name" :title="name">{{ name }}</div>
        </div>
    </div>
</template>

<script setup lang="ts">
/*
音视频播放：原生 <video>/<audio> 指 /fileview/media 流式端点。
拖动进度条靠后端 ServeContent 的 Range/206 支持，边下边播，整段不经绑定层。
*/
import { computed } from "vue";

const props = defineProps<{ url: string; name: string; kind: string }>();

const isVideo = computed(() => props.kind === "video");
</script>

<style scoped>
.media-pane {
    flex: 1;
    min-height: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    background: var(--color-fill-1);
    overflow: hidden;
}
.mp-el {
    max-width: 100%;
    max-height: 100%;
    outline: none;
}
.mp-audio-wrap {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--base-size-16);
    padding: var(--base-size-24);
    width: 100%;
    max-width: 520px;
}
.mp-audio-icon {
    font-size: 48px;
    color: var(--color-text-3);
}
.mp-audio {
    width: 100%;
}
.mp-name {
    font-size: var(--base-size-14);
    color: var(--color-text-2);
    word-break: break-all;
    text-align: center;
}
</style>
