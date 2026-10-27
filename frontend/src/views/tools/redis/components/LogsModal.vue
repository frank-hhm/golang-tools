<template>
    <!-- 本地服务：日志/状态。排版走通用 common-log-modal，本组件自持日志与状态轮询（打开即轮询、关闭即停）。 -->
    <common-log-modal :model-value="visible"
        :title="inst ? `实例 ${inst.name}（:${inst.port}）` : '实例日志'" :lines="logsLines" :width="660"
        @update:model-value="(v: boolean) => emit('update:visible', v)" @close="stopPolling">
        <template #head>
            <div v-if="logsStatus" class="ls-grid">
                <div class="ls-item"><span>状态</span><b :class="{ ok: logsStatus.running }">{{ logsStatus.running ? '运行中'
                    : '已停止'
                        }}</b></div>
                <div class="ls-item"><span>版本</span><b>{{ logsStatus.version || '-' }}</b></div>
                <div class="ls-item"><span>运行模式</span><b>{{ logsStatus.mode === 'docker' ? 'Docker 容器' : '本地进程'
                        }}</b></div>
                <!-- 容器里 redis-server 的 pid 在虚拟机 namespace，报给看只会误导，改看容器名与容器状态 -->
                <div class="ls-item" v-if="logsStatus.mode === 'docker'">
                    <span>容器</span>
                    <a-tooltip :content="logsStatus.containerId || logsStatus.container || '-'">
                        <b>{{ logsStatus.container || '-' }}{{ logsStatus.containerState ? `（${logsStatus.containerState}）` :
                                '' }}</b>
                    </a-tooltip>
                </div>
                <div class="ls-item" v-else><span>PID</span><b>{{ logsStatus.pid || '-' }}</b></div>
                <div class="ls-item"><span>运行时长</span><b>{{ fmtUptimeS(logsStatus.uptimeSeconds) }}</b></div>
                <div class="ls-item"><span>已连客户端</span><b>{{ logsStatus.connectedClients ?? '-' }}</b></div>
                <div class="ls-item"><span>键总数</span><b>{{ logsStatus.totalKeys ?? '-' }}</b></div>
                <div class="ls-item"><span>内存使用</span><b>{{ logsStatus.usedMemoryHuman || '-' }}</b></div>
            </div>
        </template>
        <template #operations>
            <a-popconfirm content="确定清空该实例的日志？会真实截断日志文件，不可恢复。" type="warning" position="bl"
                @ok="clearServiceLogs">
                <a-button size="small"><template #icon><icon-delete /></template>清空</a-button>
            </a-popconfirm>
            <a-button size="small" :disabled="!logsStatus?.running" @click="emit('connect', inst)">
                <template #icon><icon-link /></template>连接此实例
            </a-button>
        </template>
    </common-log-modal>
</template>

<script setup lang="ts">
import { getCurrentInstance, onUnmounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { fmtUptimeS, type SrvInstanceConfig } from "../helpers";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const props = defineProps<{
    visible: boolean;
    inst: SrvInstanceConfig | null;
}>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    /** 「连接此实例」：交回页面走统一的 connectInstance（未启动会先启动） */
    (e: "connect", inst: SrvInstanceConfig | null): void;
}>();

const logsLines = ref<string[]>([]);
const logsStatus = ref<any>(null);
let logsTimer: any = null;

async function tick() {
    const id = props.inst?.id;
    if (!id) return;
    try {
        logsLines.value = (await GoTools.RedisSrvLogs(id, 300)) || [];
        logsStatus.value = await GoTools.RedisSrvStatus(id);
    } catch (e) { /* ignore */ }
}

function stopPolling() {
    if (logsTimer) { clearInterval(logsTimer); logsTimer = null; }
}

function startPolling() {
    stopPolling();
    tick();
    logsTimer = setInterval(tick, 2000);
}

/** 清空（气泡二次确认后调用）：真实截断 server.log + 清环形，再清前端视图并立即拉一次 */
async function clearServiceLogs() {
    logsLines.value = [];
    const id = props.inst?.id;
    if (id) {
        try {
            await GoTools.RedisSrvClearLogs(id);
            logsLines.value = (await GoTools.RedisSrvLogs(id, 300)) || [];
            Message.success("已清空实例日志");
        } catch (e) { /* ignore */ }
    }
}

watch(() => props.visible, (open) => {
    if (open) { logsLines.value = []; logsStatus.value = null; startPolling(); }
    else stopPolling();
});

onUnmounted(stopPolling);
</script>

<style scoped>
/* 日志/状态弹窗 */
.ls-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(140px, 1fr));
    gap: 8px;
    margin-bottom: var(--base-margin);
}

.ls-item {
    background: var(--color-fill-1);
    border-radius: var(--base-radius-default);
    padding: var(--base-radius-default) var(--base-radius-large);
    font-size: var(--base-size-small);
}

.ls-item span {
    color: var(--color-text-3);
    display: block;
}

.ls-item b {
    font-size: var(--base-size-small);
}

.ls-item b.ok {
    color: rgb(var(--green-6));
}
</style>
