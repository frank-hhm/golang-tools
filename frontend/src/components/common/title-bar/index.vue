<template>
    <!-- 页面 → 全局标题栏的注入桥：把 #title / #extra 的内容搬到标题栏的两处挂载点上。
         挂载点由 WinTitleBar 登记（见 @/components/layouts/title-bar）。
         标题栏还没挂载、或本窗口根本没有自绘标题栏（如截图小窗）时，内容原位渲染，
         宁可位置不对也不静默丢掉。 -->
    <template v-if="$slots.title">
        <Teleport v-if="titleMount" :to="titleMount">
            <slot name="title" />
        </Teleport>
        <slot v-else name="title" />
    </template>
    <template v-if="$slots.extra">
        <Teleport v-if="extraMount" :to="extraMount">
            <slot name="extra" />
        </Teleport>
        <slot v-else name="extra" />
    </template>
</template>

<script setup lang="ts">
import { useTitleBarMounts } from "@/components/layouts/title-bar";

/**
 * common-title-bar：让页面往全局标题栏插内容，左右各一处。
 *
 *   <common-title-bar>
 *       <template #title> 靠左、紧跟窗口标题，可占满剩余宽度（地址栏 / 页签 / 面包屑）</template>
 *       <template #extra> 靠右、在全局按钮之前（本工具自己的按钮 / 状态标签）</template>
 *   </common-title-bar>
 *
 * 内容仍属于本页组件：作用域样式、事件、响应式状态都照常工作（Teleport 只换渲染位置）；
 * 页面卸载时注入内容自动消失，不需要手动清理。
 * 两处都在 40px 高的标题栏里，控件请用 size="small"，并自带 shrink 行为。
 */
const { titleMount, extraMount } = useTitleBarMounts();
</script>
