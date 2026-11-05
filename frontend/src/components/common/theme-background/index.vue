<template>
    <!--
        全局主题背景层：一张铺满视口的固定底，z-index 置于内容之下、由 body.app-bg-<type> 类驱动绘制。
        本组件只负责提供这个 DOM 节点与定位；五种 CSS 背景的画法与「底色让路」都写在全局 tokens.scss
        （需按 body 祖先类选择，scoped 触不到），切换主题时靠 body 类即时生效，无需 JS 参与。
        native 档（真·DWM 桌面模糊）不画任何东西，透出的是真实桌面。
    -->
    <div class="theme-bg" aria-hidden="true"></div>
</template>

<script lang="ts" setup></script>

<style scoped>
.theme-bg {
    position: fixed;
    inset: 0;
    z-index: -1;
    pointer-events: none;
    background-repeat: no-repeat;
    background-position: center;
    background-size: cover;
}
</style>
