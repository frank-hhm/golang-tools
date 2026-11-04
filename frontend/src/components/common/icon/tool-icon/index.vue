<template>
    <i v-if="fontCls" class="icon tb-icon" :class="fontCls" :style="style" />
    <component v-else-if="comp" :is="comp" class="tb-icon" :style="style" />
</template>

<script setup lang="ts">
import { computed, getCurrentInstance } from "vue";
import { arcoIconComp, isIconfontIcon } from "@/utils/tool-icon";

/**
 * common-tool-icon：把功能配置里的图标值渲染成图标，兼容两种来源。
 *  - 图标库（icon.json 的 font_class）：icon-xxx → <i class="icon icon-xxx">
 *  - Arco 图标名：camera / clock-circle（或已 Pascal 化的 IconCamera）→ 全局注册的 Arco 图标组件
 * size 传像素字号：字体图标与 Arco 图标都按 font-size 缩放；不传则跟随父级字号。
 * 图标值为空或认不出时什么都不画（由调用方决定兜底，例如退回应用 Logo）。
 */
const props = defineProps<{ name?: string; size?: number }>();

// Arco 图标是全量注册在应用实例上的，这里查注册表而不是直接交给 <component :is>：
// 名字写错时后者会渲染出一个解析失败的野标签并在控制台报错
const globalComponents = (getCurrentInstance() as any)?.appContext.components ?? {};

const style = computed(() => (props.size ? { fontSize: props.size + "px" } : undefined));
const fontCls = computed(() => (isIconfontIcon(props.name) ? String(props.name) : ""));
const comp = computed(() => {
    const name = fontCls.value ? null : arcoIconComp(props.name);
    return name && name in globalComponents ? name : null;
});
</script>
