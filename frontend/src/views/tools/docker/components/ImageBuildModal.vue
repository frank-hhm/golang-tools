<template>
    <!-- 构建镜像：上下文目录由 Go 侧目录选择器挑，确定时把三项参数抛给页面 -->
    <a-modal :visible="visible" title="构建镜像" :width="560" :mask-closable="false" :on-before-ok="submit"
        @cancel="onVisible(false)" @update:visible="onVisible">
        <a-form :model="form" layout="vertical">
            <a-form-item field="dir" label="构建上下文目录（需含 Dockerfile）">
                <a-space :size="8" style="width: 100%">
                    <a-input v-model="form.dir" class="mono" allow-clear placeholder="选择目录" />
                    <a-button size="small" @click="chooseDir"><template #icon><icon-folder /></template>选择</a-button>
                </a-space>
            </a-form-item>
            <a-row :gutter="12">
                <a-col :span="12">
                    <a-form-item field="dockerfile" label="Dockerfile 文件名">
                        <a-input v-model="form.dockerfile" placeholder="Dockerfile" />
                    </a-form-item>
                </a-col>
                <a-col :span="12">
                    <a-form-item field="tag" label="镜像标签">
                        <a-input v-model="form.tag" class="mono" placeholder="myapp:1.0" />
                    </a-form-item>
                </a-col>
            </a-row>
        </a-form>
        <a-alert type="normal" class="setup-alert">
            上下文会整个打包上传给引擎（自动跳过 .git / node_modules / .idea）；构建日志逐步显示，可随时取消。
        </a-alert>
    </a-modal>
</template>

<script setup lang="ts">
import { getCurrentInstance, reactive, watch } from "vue";
import { Message } from "@arco-design/web-vue";

const props = defineProps<{ visible: boolean }>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "submit", payload: { dir: string; dockerfile: string; tag: string }): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const form = reactive({ dir: "", dockerfile: "Dockerfile", tag: "" });

// 每次打开都回到干净状态：目录与标签不跨次复用
watch(
    () => props.visible,
    (v) => {
        if (!v) return;
        form.dir = "";
        form.dockerfile = "Dockerfile";
        form.tag = "";
    },
    { immediate: true }
);

function onVisible(v: boolean) {
    emit("update:visible", v);
}

async function chooseDir() {
    try {
        const d = await GoTools.ChooseDir("选择构建上下文目录");
        if (d) form.dir = d;
    } catch (e: any) {
        Message.warning("打开目录选择器失败: " + (e?.message || e));
    }
}

/** 目录或标签缺一个就拦在弹窗里，合格才交给页面起构建任务 */
function submit(): boolean {
    const dir = form.dir.trim();
    const tag = form.tag.trim();
    if (!dir) {
        Message.warning("请选择构建上下文目录");
        return false;
    }
    if (!tag) {
        Message.warning("请填写镜像标签，如 myapp:1.0");
        return false;
    }
    emit("submit", { dir, dockerfile: form.dockerfile.trim() || "Dockerfile", tag });
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
