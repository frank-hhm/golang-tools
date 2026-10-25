<template>
    <!-- 重命名容器：只回传新名字，接口调用与列表刷新留在页面 -->
    <a-modal :visible="visible" title="重命名容器" :width="420" :on-before-ok="submit"
        @cancel="onVisible(false)" @update:visible="onVisible">
        <a-form :model="form" layout="vertical">
            <a-form-item field="name" label="容器名称">
                <a-input v-model="form.name" placeholder="仅字母/数字/下划线/中划线" />
            </a-form-item>
            <div class="rename-tip">当前：{{ current }}</div>
        </a-form>
    </a-modal>
</template>

<script setup lang="ts">
import { reactive, watch } from "vue";
import { Message } from "@arco-design/web-vue";

const props = defineProps<{
    visible: boolean;
    /** 容器现在的名字 */
    current: string;
}>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "submit", name: string): void;
}>();

const form = reactive({ name: "" });

watch(
    () => props.visible,
    (v) => {
        if (!v) return;
        form.name = props.current;
    },
    { immediate: true }
);

function onVisible(v: boolean) {
    emit("update:visible", v);
}

/** 空名字不关弹窗（原名字还在，改了不如不改） */
function submit(): boolean {
    const name = form.name.trim();
    if (!name) {
        Message.warning("名称不能为空");
        return false;
    }
    emit("submit", name);
    return true;
}
</script>

<style scoped>
.rename-tip {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}
</style>
