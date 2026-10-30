<template>
    <div class="pdf-pane">
        <iframe class="pdf-frame" :src="url" :title="name" frameborder="0"></iframe>
    </div>
</template>

<script setup lang="ts">
/*
PDF 预览：交给浏览器/WebView2 内置的 PDF 查看器，指 /fileview/media 流式端点
（ServeContent 带 Range，大 PDF 也能翻页）。不引 pdf.js，省一大块包体。
个别无内置查看器的环境，用右上角「下载 / 系统程序打开」兜底。
*/
defineProps<{ url: string; name: string }>();
</script>

<style scoped>
.pdf-pane {
    flex: 1;
    min-height: 0;
    display: flex;
}
.pdf-frame {
    flex: 1;
    width: 100%;
    height: 100%;
    border: 0;
    background: var(--color-fill-1);
}
</style>
