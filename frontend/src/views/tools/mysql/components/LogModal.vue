<template>
    <!-- 错误日志（mysql.err，实时）弹窗：从实例总览拆出，打开才轮询、关闭即停。
         排版交给通用 common-log-modal，本组件只管按 instanceId 拉日志 + 填 MySQL 专属状态/操作插槽。 -->
    <common-log-modal v-model="visible" :title="`错误日志 · ${instanceName}`" :lines="logLines" :width="800"
        @close="stopPolling">
        <template #head>
            <a-space :size="6">
                <com-badge :status="running ? 'success' : 'normal'" :animated="running" />
                <span>{{ running ? "运行中" : "已停止" }}</span>
                <a-tag size="small" color="arcoblue">:{{ port }}</a-tag>
                <a-tag v-if="isDocker" size="small" color="purple">Docker</a-tag>
            </a-space>
        </template>
        <template #tip>
            <a-space>
                mysql.err 尾部，每 2s 自动刷新
            </a-space>
        </template>
        <template #operations>
            <a-popconfirm content="确定清空该实例的错误日志？会真实截断 mysql.err 文件，不可恢复。" type="warning"
                position="bl" @ok="clearLog">
                <a-button size="small">
                    <template #icon><icon-delete /></template>清空
                </a-button>
            </a-popconfirm>
        </template>
    </common-log-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onUnmounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const props = defineProps<{
    /** 可见性（v-model） */
    modelValue: boolean;
    /** 目标实例 id：轮询取它的错误日志 */
    instanceId: string;
    /** 实例名（标题用） */
    instanceName: string;
    /** 端口（头部标签） */
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
let logInflight = false;
/** 日志接口连续失败后的静默期：引擎离线时不要每 2 秒刷一条错误 */
let logQuietUntil = 0;

async function pollLogs() {
    if (!props.instanceId || logInflight || Date.now() < logQuietUntil) return;
    logInflight = true;
    try {
        const lines = (await GoTools.MySQLSrvLogs(props.instanceId, 300)) || [];
        logQuietUntil = 0;
        if (lines.join("\n") !== logLines.value.join("\n")) logLines.value = lines;
    } catch (e) {
        logQuietUntil = Date.now() + 15000;
    } finally {
        logInflight = false;
    }
}
function startPolling() {
    stopPolling();
    logLines.value = [];
    logQuietUntil = 0;
    pollLogs();
    logTimer = setInterval(pollLogs, 2000);
}
function stopPolling() {
    if (logTimer) { clearInterval(logTimer); logTimer = null; }
}
/** 清空（气泡二次确认后调用）：真实截断 mysql.err + 归零截止偏移，再清前端视图并立即刷一次 */
async function clearLog() {
    logLines.value = [];
    if (!props.instanceId) return;
    try {
        await GoTools.MySQLSrvClearLogs(props.instanceId);
        await pollLogs();
        Message.success("已清空错误日志");
    } catch (e) { /* ignore */ }
}

// 打开即轮询，关闭即停（common-log-modal 关闭事件 + 卸载双兜底）
watch(() => props.modelValue, (open) => {
    if (open) startPolling();
    else stopPolling();
});
onUnmounted(stopPolling);
</script>
