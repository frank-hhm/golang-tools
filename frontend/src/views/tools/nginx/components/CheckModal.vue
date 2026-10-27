<template>
    <!-- 配置检查结果弹窗：只读展示 nginx -t 输出，内容由父组件传入。 -->
    <a-modal v-model:visible="visible" title="nginx -t 配置检查" :width="600" :footer="false">
        <pre class="check-out">{{ output }}</pre>
    </a-modal>
</template>

<script setup lang="ts">
import { computed } from "vue";

const props = defineProps<{
    /** 可见性（v-model） */
    modelValue: boolean;
    /** 检查输出文本 */
    output: string;
}>();

const emit = defineEmits<{ (e: "update:modelValue", v: boolean): void }>();

const visible = computed({
    get: () => props.modelValue,
    set: (v: boolean) => emit("update:modelValue", v),
});
</script>

<style scoped>
.check-out {
    background: var(--color-fill-2);
    padding: 10px;
    border-radius: var(--base-radius-default);
    font-size: var(--base-size-small);
    white-space: pre-wrap;
    word-break: break-all;
    margin: 0;
}
</style>
