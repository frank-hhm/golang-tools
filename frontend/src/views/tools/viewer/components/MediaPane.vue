<template>
    <div class="media-pane">
        <video-player v-if="isVideo" :url="url" :name="name" />
        <audio-player v-else :url="url" :name="name" />
    </div>
</template>

<script setup lang="ts">
/*
音视频播放分发：视频交 VideoPlayer、音频交 AudioPlayer（均为抖音式自定义控件）。
两者都指 /fileview/media 流式端点，拖动进度靠后端 ServeContent 的 Range/206，
边下边播，整段不经绑定层。共享播放状态见 mediaCore.ts，进度/音量条见 SeekBar.vue。
*/
import { computed } from "vue";
import VideoPlayer from "./VideoPlayer.vue";
import AudioPlayer from "./AudioPlayer.vue";

const props = defineProps<{ url: string; name: string; kind: string }>();

const isVideo = computed(() => props.kind === "video");
</script>

<style scoped>
.media-pane {
    flex: 1;
    min-height: 0;
    display: flex;
    /* 内容面只叠 fill-1 淡膜、不铺实色：父面板 .vp-main 由全局主题规则接管，铺 bg-2 会把主题纹理截断。 */
    background: var(--color-fill-1);
    overflow: hidden;
}
</style>
