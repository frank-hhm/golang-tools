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
    /* 内容面只叠 fill-1 淡膜、不铺实色：父面板 .vp-main 由全局主题规则接管，铺 bg-2 会把主题纹理截断。 */
    background: var(--color-fill-1);
}
</style>
