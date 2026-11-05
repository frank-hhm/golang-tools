<template>
    <ThemeBackground v-if="showThemeBg" />
    <router-view />
</template>

<script lang="ts" setup>
import { computed } from "vue";
import { useRoute } from "vue-router";
import ThemeBackground from "@/components/common/theme-background/index.vue";

// 全局主题背景层(.theme-bg)是 z-index:-1 的固定底，会给每个窗口铺一层应用背景。
// 但截图选区浮层、图钉置顶、逐像素透明调试窗这类「无边框透明窗」要靠窗口透明显示
// 冻结帧 / 钉住的截图 / 真实桌面——这层背景会盖住它们（截图点确定擦帧后就闪出主题背景）。
// 故这类页面在路由 meta 上标 noThemeBg，App 据此不挂载背景层。
const route = useRoute();
const showThemeBg = computed(() => route.meta.noThemeBg !== true);
</script>

<style>
#nprogress .bar {
    background: rgba(var(--primary-6)) !important;
}

#nprogress .peg {
    box-shadow: 0 0 10px rgba(var(--primary-6)), 0 0 5px rgba(var(--primary-6)) !important;
}
</style>
