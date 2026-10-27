<template>
    <!-- 错误/访问日志（实时）弹窗：从主页面拆出，打开才轮询、关闭即停。
         排版交给通用 common-log-modal，本组件只管按 instanceId 拉日志 + 填 nginx 专属的状态/操作插槽。 -->
    <common-log-modal v-model="visible" :title="`日志 · ${instanceName}`" :lines="logLines" :width="780"
        @close="stopPolling">
        <template #tip>
            <a-space>
                error.log + access.log 尾部，每 2s 自动刷新
            </a-space>
        </template>
        <template #operations>
            <a-button size="small" @click="logLines = []">
                <template #icon><icon-delete /></template>清空
            </a-button>
        </template>
    </common-log-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onUnmounted, ref, watch } from "vue";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const props = defineProps<{
    /** 可见性（v-model） */
    modelValue: boolean;
    /** 目标实例 id：轮询取它的日志 */
    instanceId: string;
    /** 实例名（标题用） */
    instanceName: string;
    /** HTTP 端口（头部标签） */
    port: number;
    /** 是否运行中（头部状态点） */
    running: boolean;
    /** 是否容器模式（标签文案） */
    isDocker: boolean;
}>();

const emit = defineEmits<{ (e: "update:modelValue", v: boolean): void }>();

const visible = computed({
    get: () => props.modelValue,
    set: (v: boolean) => emit("update:modelValue", v),
});

const logLines = ref<string[]>([]);
let logTimer: any = null;

async function pollLogs() {
    if (!props.instanceId) return;
    try {
        const lines = (await GoTools.NginxSrvLogs(props.instanceId, 300)) || [];
        if (lines.join("\n") !== logLines.value.join("\n")) logLines.value = lines;
    } catch (e) { /* ignore */ }
}
function startPolling() {
    stopPolling();
    logLines.value = [];
    pollLogs();
    logTimer = setInterval(pollLogs, 2000);
}
function stopPolling() {
    if (logTimer) { clearInterval(logTimer); logTimer = null; }
}

// 打开即轮询，关闭即停（common-log-modal 关闭事件 + 卸载双兜底）
watch(() => props.modelValue, (open) => {
    if (open) startPolling();
    else stopPolling();
});
onUnmounted(stopPolling);
</script>
