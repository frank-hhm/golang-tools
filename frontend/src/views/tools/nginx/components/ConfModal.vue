<template>
    <!-- nginx.conf 文本编辑弹窗：打开时按 instanceId 拉取文本，保存走后端（后端自动备份 + 校验 + 回滚）。 -->
    <a-modal v-model:visible="visible" :title="`nginx.conf - ${instanceName}`" :width="760">
        <a-textarea v-model="confText" :auto-size="{ minRows: 16, maxRows: 24 }" spellcheck="false" />
        <template #footer>
            <div class="ini-tip">{{ isDocker ? '容器模式跑不了 nginx -t：保存后点「重载」（重启容器）生效，起不来时看日志。' : '保存前自动备份并执行配置检查，检查失败将回滚。' }}</div>
            <a-space>
                <a-button size="small" @click="visible = false">取消</a-button>
                <a-button size="small" @click="saveConfText">保存</a-button>
            </a-space>
        </template>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const props = defineProps<{
    /** 可见性（v-model） */
    modelValue: boolean;
    /** 目标实例 id */
    instanceId: string;
    /** 实例名（标题用） */
    instanceName: string;
    /** 是否容器模式（页脚提示文案不同） */
    isDocker: boolean;
}>();

const emit = defineEmits<{
    (e: "update:modelValue", v: boolean): void;
    /** 保存成功：父组件可刷新状态 */
    (e: "saved"): void;
}>();

const visible = computed({
    get: () => props.modelValue,
    set: (v: boolean) => emit("update:modelValue", v),
});

const confText = ref("");

/* 打开（或切换实例）时读取该实例的 nginx.conf */
watch(() => [props.modelValue, props.instanceId] as const, async ([open, id]) => {
    if (!open || !id) return;
    confText.value = "";
    try { confText.value = (await GoTools.NginxSrvGetConfigText(id)) || ""; }
    catch (e: any) { Message.error("读取 nginx.conf 失败: " + (e?.message || e)); visible.value = false; }
});

async function saveConfText() {
    try {
        await GoTools.NginxSrvSaveConfigText(props.instanceId, confText.value);
        Message.success(props.isDocker
            ? "nginx.conf 已保存；容器模式点「重载」（重启容器）后生效"
            : "nginx.conf 已保存；运行中实例建议执行一次重载");
        visible.value = false;
        emit("saved");
    } catch (e: any) { Message.error("保存失败: " + (e?.message || e)); }
}
</script>

<style scoped>
.ini-tip {
    float: left;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    line-height: 32px;
}
</style>
