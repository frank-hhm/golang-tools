<template>
    <!-- 公共自绘标题栏：无边框窗口下由前端提供标题与右侧窗口按钮。整栏可拖动（app-region: drag），
         右侧动作区 no-drag 保证按钮可点。工具窗（BlankLayout）与图钉窗共用此组件，保证样式一致。 -->
    <div class="win-titlebar">
        <div class="tb-title">
            <slot name="icon" />
            <span>{{ title }}</span>
        </div>
        <div class="tb-actions">
            <a-space>
                <slot />
            </a-space>
        </div>
    </div>
</template>

<script setup lang="ts">
defineProps<{ title: string }>();
</script>

<style scoped>
.win-titlebar {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    height: 40px;
    z-index: 1000;
    display: flex;
    align-items: center;
    justify-content: space-between;
    background: var(--color-bg-1);
    color: var(--color-text-1);
    user-select: none;
    -webkit-app-region: drag;
    app-region: drag;
    border-bottom: 1px solid var(--color-border-1);
}

.tb-title {
    display: flex;
    align-items: center;
    gap: 6px;
    padding-left: 14px;
    font-size: var(--base-size-medium);
    font-weight: bold;
}

.tb-actions {
    display: flex;
    align-items: center;
    -webkit-app-region: no-drag;
    app-region: no-drag;
    margin-right: var(--base-padding);
}
</style>
