<template>
    <!-- 推送镜像：本地引用只读，目标引用可改；预填值由页面算好传进来 -->
    <a-modal :visible="visible" title="推送镜像" :width="560" :mask-closable="false" :on-before-ok="submit"
        @cancel="onVisible(false)" @update:visible="onVisible">
        <a-form :model="form" layout="vertical">
            <a-form-item field="image" label="本地镜像">
                <a-input v-model="form.image" class="mono" disabled />
            </a-form-item>
            <a-form-item field="target" label="目标引用（仓库/命名空间/名称:tag）">
                <a-input v-model="form.target" class="mono" allow-clear placeholder="harbor.corp.local/ns/app:1.0" />
            </a-form-item>
        </a-form>
        <a-alert type="normal" class="setup-alert">
            推送前需先给镜像打上目标 tag；若找不到登录信息，先去「仓库」里登记地址与账号（密码用 Windows DPAPI 加密保存在本机）。
        </a-alert>
    </a-modal>
</template>

<script setup lang="ts">
import { reactive, watch } from "vue";
import { Message } from "@arco-design/web-vue";

const props = defineProps<{
    visible: boolean;
    /** 要推送的本地镜像引用 */
    image: string;
    /** 页面按登记仓库算好的默认目标 */
    target: string;
}>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "submit", payload: { image: string; target: string }): void;
}>();

const form = reactive({ image: "", target: "" });

watch(
    () => props.visible,
    (v) => {
        if (!v) return;
        form.image = props.image;
        form.target = props.target;
    },
    { immediate: true }
);

function onVisible(v: boolean) {
    emit("update:visible", v);
}

/** 目标引用不合格就不关弹窗，合格才交给页面起推送任务 */
function submit(): boolean {
    if (!form.image.trim()) {
        Message.warning("本地镜像不能为空");
        return false;
    }
    const target = form.target.trim();
    if (!target.includes("/")) {
        Message.warning("目标引用要带仓库地址，如 harbor.corp.local/ns/app:1.0");
        return false;
    }
    emit("submit", { image: form.image.trim(), target });
    return true;
}
</script>

<style scoped>
.setup-alert {
    margin-bottom: var(--base-radius-default);
}

.mono {
    font-family: Consolas, monospace;
}
</style>
