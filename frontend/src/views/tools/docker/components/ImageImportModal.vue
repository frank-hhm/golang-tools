<template>
    <!-- 导入镜像包：文件挑选自己调 Go 侧选择框，确定时把路径抛给页面 -->
    <a-modal :visible="visible" title="导入镜像包" :width="560" :mask-closable="false" :on-before-ok="submit"
        @cancel="onVisible(false)" @update:visible="onVisible">
        <a-form :model="form" layout="vertical">
            <a-form-item field="file" label="tar 包（docker save 产物）">
                <a-space :size="8" style="width: 100%">
                    <a-input v-model="form.file" class="mono" allow-clear placeholder="选择或粘贴路径" />
                    <a-button size="small" @click="chooseFile"><template #icon><icon-folder /></template>选择</a-button>
                </a-space>
            </a-form-item>
            <a-form-item field="ref" label="重命名为（留空沿用包内 tag）">
                <a-input v-model="form.ref" class="mono" allow-clear placeholder="myapp:1.0" />
            </a-form-item>
        </a-form>
    </a-modal>
</template>

<script setup lang="ts">
import { getCurrentInstance, reactive, watch } from "vue";
import { Message } from "@arco-design/web-vue";

const props = defineProps<{ visible: boolean }>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "submit", payload: { file: string; ref: string }): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const form = reactive({ file: "", ref: "" });

// 每次打开都清空：上一次的包路径不该跟着串过来
watch(
    () => props.visible,
    (v) => {
        if (!v) return;
        form.file = "";
        form.ref = "";
    },
    { immediate: true }
);

function onVisible(v: boolean) {
    emit("update:visible", v);
}

async function chooseFile() {
    try {
        const p = await GoTools.DockerSrvChooseFile("选择镜像包");
        if (p) form.file = p;
    } catch (e: any) {
        Message.warning("打开文件选择框失败: " + (e?.message || e));
    }
}

/** 没选包就不关弹窗，选好才交给页面起导入任务 */
function submit(): boolean {
    const file = form.file.trim();
    if (!file) {
        Message.warning("请选择要导入的 tar 包");
        return false;
    }
    emit("submit", { file, ref: form.ref.trim() });
    return true;
}
</script>

<style scoped>
.mono {
    font-family: Consolas, monospace;
}
</style>
