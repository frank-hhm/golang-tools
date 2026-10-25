<template>
    <!-- 公共自绘标题栏：无边框窗口下由前端提供标题与右侧窗口按钮。整栏可拖动（app-region: drag），
         右侧动作区 no-drag 保证按钮可点。工具窗（BlankLayout）与图钉窗共用此组件，保证样式一致。
         #title / #extra 是给页面注入的两处挂载点（布局层也能直接传 slot 用）：
         #title 紧跟标题靠左，#extra 在全局按钮之前靠右，页面侧经 common-title-bar Teleport 进来。 -->
    <div class="win-titlebar">
        <div class="tb-title">
            <slot name="icon" />
            <span class="tb-text">{{ title }}</span>
            <div ref="titleSlotRef" class="tb-title-slot"><slot name="title" /></div>
        </div>
        <div class="tb-right">
            <div ref="extraSlotRef" class="tb-extra"><slot name="extra" /></div>
            <div class="tb-actions">
                <a-space>
                    <slot />
                </a-space>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from "vue";
import { registerTitleBarMounts } from "@/components/layouts/title-bar";

defineProps<{ title: string }>();

const titleSlotRef = ref<HTMLElement | null>(null);
const extraSlotRef = ref<HTMLElement | null>(null);
let unregister: (() => void) | null = null;

onMounted(() => {
    // 两个挂载点缺一就不登记：宁可让页面注入内容原位渲染（common-title-bar 有兜底），
    // 也不要把内容 Teleport 进一个不存在的容器里悄悄丢掉
    if (titleSlotRef.value && extraSlotRef.value) {
        unregister = registerTitleBarMounts(titleSlotRef.value, extraSlotRef.value);
    }
});
onBeforeUnmount(() => { unregister?.(); unregister = null; });
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
    flex: 1 1 auto;
    min-width: 0;
    padding-left: 14px;
    font-size: var(--base-size-medium);
    font-weight: bold;
}

.tb-text {
    flex: none;
    white-space: nowrap;
}

/* 页面注入区（左）：吃掉标题右侧的剩余宽度，内部控件按各自需要再分 */
.tb-title-slot {
    display: flex;
    align-items: center;
    gap: 8px;
    flex: 1 1 auto;
    min-width: 0;
    margin-left: 8px;
    overflow: hidden;
    font-weight: normal;
}

.tb-right {
    display: flex;
    align-items: center;
    gap: 10px;
    flex: none;
    min-width: 0;
}

/* 页面注入区（右）：排在窗口按钮前面 */
.tb-extra {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    overflow: hidden;
}

/* 注入区里是页面的控件（输入框、标签等）：允许选中文字 */
.tb-title-slot,
.tb-extra {
    -webkit-user-select: text;
    user-select: text;
}

/* 容器本身继续继承整栏的 drag（否则 #title 这个 flex:1 的空档会把标题栏中段
   变成拖不动的死区），只把真正塞进来的控件设为 no-drag 保证可点可输入 */
.tb-title-slot :deep(*),
.tb-extra :deep(*) {
    -webkit-app-region: no-drag;
    app-region: no-drag;
}

.tb-actions {
    display: flex;
    align-items: center;
    -webkit-app-region: no-drag;
    app-region: no-drag;
    margin-right: var(--base-padding);
}
</style>
