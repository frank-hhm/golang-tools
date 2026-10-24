<template>
    <!-- 空白布局（原生新窗口）：不渲染卡片/标题，直接输出内容 -->
    <div v-if="blank" class="layout-body-main" :class="{ 'is-fill': isFill }" :style="styles">
        <div class="layout-body-main-content">
            <slot></slot>
        </div>
    </div>
    <div v-else class="layout-body-main" :class="{
        'is-fill': isFill,
        'has-bg': isBg,
        'nohas-bg': !isBg

    }" :style="styles">

        <div class="layout-body-main-header" v-if="title">
            <div class="layout-body-main-header-title">{{ title }}</div>
            <div>
                <slot name="right">

                </slot>
            </div>
        </div>
        <div class="layout-body-main-content">
            <slot></slot>
        </div>
    </div>
</template>
<script lang="ts">
export default {
    name: "LayoutBody",
};
</script>
<script lang="ts" setup>
import { computed, StyleValue } from "vue";
import { useRoute } from "vue-router";

const route = useRoute();

/** 当前路由是否为“空白布局”（原生新窗口），是则不渲染 LayoutBody 卡片 */
const blank = computed(() => !!route.meta?.blank);

const props = withDefaults(
    defineProps<{
        /** 高度：'full'（继承窗口/内容区高度）| CSS 高度字符串（如 '60vh'）| false（自适应） */
        height?: string | boolean;
        title?: string;
        isBg?: boolean;
    }>(),
    {
        height: false,
        title: "",
        isBg: true,
    }
);

// 是否铺满可用高度（继承外层窗口/内容区）
const isFill = computed(() => props.height === "full" || props.height === true);

const styles = computed<StyleValue>(() => {
    if (isFill.value) return "height: 100%";
    return [
        props.height ? `height:calc(${props.height} - 2px)` : "",
    ];
});
</script>
<style scoped lang="scss">
.layout-body-main {
    position: relative;
}

.layout-body-main.has-bg {
    background: var(--color-bg-2);
    border-radius: var(--base-radius-default);
}


/* 铺满高度：纵向 flex，头部固定，内容区占据剩余并可滚动 */
.layout-body-main.is-fill {
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.layout-body-main.is-fill .layout-body-main-header {
    flex: none;
}

.layout-body-main.is-fill .layout-body-main-content {
    min-height:100%;
}

.layout-body-main-header {
    min-height: 20px;
    border-bottom: 1px solid var(--color-border-1);
    padding: var(--base-padding);
    display: flex;
    align-items: center;
    justify-content: space-between;
}

.layout-body-main-header-title {
    /* font-size: var(--s); */
}

.layout-body-main.has-bg {
    .layout-body-main-content {
        background: var(--color-bg-1);
        padding: var(--base-padding);
    }
}
</style>