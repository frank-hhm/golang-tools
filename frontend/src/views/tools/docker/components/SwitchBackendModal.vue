<template>
    <!-- 切换虚拟化后端：代价逐条列清，确认与执行都留在页面，这里只负责呈现 -->
    <a-modal :visible="visible" :title="'切换虚拟化后端 → ' + targetLabel" :width="560" :mask-closable="false"
        :body-style="{ maxHeight: '72vh' }" :ok-loading="busy === 'backend'" :on-before-ok="onBeforeOk"
        @cancel="emit('update:visible', false)"
        @update:visible="(v: boolean) => emit('update:visible', v)">
        <!-- 后端诊断是“一句话结论 + 若干修复步骤”：吸顶占一行，步骤收进浮层，不把弹窗撑高 -->
        <common-modal-tip v-if="verdict" type="error" :text="verdict" :lines="verdictFix" />
        <a-alert type="warning" class="setup-alert">
            切换会新建一个空的 Linux 虚拟机：现有容器与已拉取的镜像不会保留
            （本工具 bind mount 的数据目录不受影响，但容器要重建）。
        </a-alert>
        <div class="guide">
            <div v-for="(l, i) in notes" :key="'sw' + i" class="guide-line">{{ l }}</div>
        </div>
        <div v-if="targetLabel === 'WSL2'" class="setup-row">
            <a-button size="small" :loading="busy === 'wsl'" :disabled="!!busy" @click="emit('enable-wsl')">
                <template #icon><icon-common /></template>启用 WSL2
            </a-button>
            <a-button size="small" :loading="busy === 'wslupd'" :disabled="!!busy" @click="emit('update-wsl')">
                <template #icon><icon-common /></template>更新 WSL2
            </a-button>
            <a-typography-text type="secondary" class="mode-hint">
                切不过去且引擎一直跑在 Hyper-V 时，通常是 WSL 内核过旧或坏了，先更新再切
            </a-typography-text>
        </div>
        <a-checkbox :model-value="confirmed" class="setup-agree"
            @change="(v: boolean | (string | number | boolean)[]) => emit('update:confirmed', !!v)">
            我已知晓上述影响，同意停止 Docker Desktop 并改写后端配置
        </a-checkbox>
        <div v-if="busy === 'backend'" class="setup-prog">
            <a-progress :percent="percent / 100" size="small"
                :status="stage === 'error' ? 'danger' : (stage === 'done' ? 'success' : 'normal')" />
            <div class="setup-note">{{ note }}</div>
        </div>
        <div v-else-if="logs.length" class="setup-note">{{ logs[logs.length - 1] }}</div>
        <template #ok-text>确认切换</template>
    </a-modal>
</template>

<script setup lang="ts">
defineProps<{
    visible: boolean;
    /** 目标后端名（WSL2 / Hyper-V） */
    targetLabel: string;
    notes: string[];
    verdict: string;
    verdictFix: string[];
    busy: string;
    percent: number;
    stage: string;
    note: string;
    logs: string[];
    confirmed: boolean;
}>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "update:confirmed", v: boolean): void;
    (e: "enable-wsl"): void;
    (e: "update-wsl"): void;
    (e: "submit"): void;
}>();

/**
 * 不交给弹窗自己关：切换要停引擎、改配置、重启再核验，
 * 失败时得让用户留在弹窗里看完整过程，所以关闭时机由页面决定。
 */
function onBeforeOk() {
    emit("submit");
    return false;
}
</script>

<style scoped>
.setup-alert {
    margin-bottom: var(--base-radius-default);
}

.guide {
    margin-top: var(--base-radius-default);
}

.guide-line {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    line-height: 1.8;
    padding-left: 12px;
    position: relative;
}

.guide-line::before {
    content: "·";
    position: absolute;
    left: 2px;
    color: var(--color-text-4);
}

.setup-row {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    margin: var(--base-radius-default) 0;
}

.mode-hint {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    line-height: 1.7;
}

/* 弹窗里的确认勾选：切换后端的代价不可逆，必须显式确认 */
.setup-agree {
    display: block;
    margin-top: 10px;
    font-weight: 500;
}

.setup-prog {
    margin-top: var(--base-radius-default);
}

.setup-note {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    word-break: break-all;
    white-space: pre-wrap;
}
</style>
