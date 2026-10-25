<template>
    <!-- 环境体检：只负责把体检结果摊开，重新体检 / 装 WSL2 都交回页面执行 -->
    <a-modal :visible="visible" title="环境体检与诊断" :width="760" :footer="false"
        @cancel="emit('update:visible', false)" @update:visible="(v: boolean) => emit('update:visible', v)">
        <div class="env-grid">
            <div class="env-item">
                <span>Docker Desktop</span>
                <b class="com-state" :class="env.dockerInstalled ? 'is-on' : 'is-off'">
                    <com-badge :status="env.dockerInstalled ? 'success' : 'danger'" />
                    {{ env.dockerInstalled ? ('已安装 ' + (env.dockerVersion || '-')) : '未安装' }}
                </b>
            </div>
            <div class="env-item">
                <span>WSL2</span>
                <b>
                    <!-- 悬停看原文与发行版清单：没发行版并不等于 WSL2 不可用 -->
                    <a-tooltip position="bottom" :content-style="{ maxWidth: '440px' }">
                        <template #content>
                            <div class="tip-lines">
                                <div class="lt-line">{{ env.wslStatus || '未探测' }}</div>
                                <div v-if="env.wslDistros?.length" class="lt-line">发行版：{{ env.wslDistros.join('、') }}</div>
                                <div class="lt-line">docker-desktop 发行版：{{ dockerDistroText }}</div>
                            </div>
                        </template>
                        <span class="com-state" :class="env.wslInstalled ? 'is-on' : 'is-warn'">
                            <com-badge :status="env.wslInstalled ? 'success' : 'warning'" />
                            {{ env.wslInstalled ? '已就绪' : '未就绪' }}
                        </span>
                    </a-tooltip>
                </b>
            </div>
            <div class="env-item">
                <span>引擎实际后端</span>
                <b class="com-state" :class="curBackend === 'hyperv' ? 'is-warn' : 'is-on'">
                    <com-badge :status="curBackend === 'hyperv' ? 'warning' : 'success'" />
                    {{ env.backend?.label || '未知' }}
                </b>
            </div>
            <div class="env-item">
                <span>Hyper-V 角色</span>
                <b class="com-state" :class="hypervTag.cls">
                    <com-badge :status="hypervTag.status" />{{ hypervTag.text }}
                </b>
            </div>
            <div class="env-item">
                <span>命名管道</span>
                <b class="com-state" :class="env.pipeExists ? 'is-on' : 'is-dim'">
                    <com-badge :status="env.pipeExists ? 'success' : 'normal'" />
                    {{ env.pipeExists ? '已监听' : '不存在' }}
                </b>
            </div>
            <div class="env-item">
                <span>离线安装包</span>
                <b class="com-state" :class="env.installerFile ? 'is-on' : 'is-dim'">
                    <com-badge :status="env.installerFile ? 'success' : 'normal'" />
                    {{ env.installerFile ? fmtBytes(env.installerSize) : '无' }}
                </b>
            </div>
        </div>
        <a-tooltip v-if="env.wslStatus" :content="env.wslStatus">
            <div class="env-raw mono">{{ env.wslStatus }}</div>
        </a-tooltip>
        <div class="guide">
            <div v-for="(g, i) in envNotes" :key="'n' + i" class="guide-line">{{ g }}</div>
        </div>
        <div v-if="setupNote" class="setup-note">{{ setupNote }}</div>
        <a-space :size="8" class="setup-row">
            <a-button size="small" :loading="envLoading" @click="emit('refresh')">
                <template #icon><icon-refresh /></template>重新体检
            </a-button>
            <a-button v-if="!env.wslInstalled" size="small" :loading="setupBusy === 'wsl'" :disabled="!!setupBusy"
                @click="emit('enable-wsl')">
                <template #icon><icon-common /></template>启用 WSL2
            </a-button>
            <a-button v-else size="small" :loading="setupBusy === 'wslupd'" :disabled="!!setupBusy"
                @click="emit('update-wsl')">
                <template #icon><icon-common /></template>更新 WSL2
            </a-button>
        </a-space>
    </a-modal>
</template>

<script setup lang="ts">
import { fmtBytes } from "../helpers";

defineProps<{
    visible: boolean;
    /** Go 侧体检结果（原样透传，字段随后端演进） */
    env: any;
    dockerDistroText: string;
    curBackend: string;
    hypervTag: { text: string; status: "success" | "warning" | "normal"; cls: string };
    envNotes: string[];
    envLoading: boolean;
    setupBusy: string;
    setupNote: string;
}>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "refresh"): void;
    (e: "enable-wsl"): void;
    (e: "update-wsl"): void;
}>();
</script>

<style scoped>
.env-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(170px, 1fr));
    gap: 8px;
}

.env-item {
    background: var(--color-fill-1);
    border-radius: var(--base-radius-default);
    padding: var(--base-radius-default) var(--base-radius-large);
    font-size: var(--base-size-small);
    min-width: 0;
}

/* 左侧标签用子选择器限定：体检值里的圆点+文案是 span/b 混用，不能再被标签的灰色盖掉 */
.env-item > span {
    color: var(--color-text-3);
    margin-right: 6px;
}

.env-item > b {
    font-weight: 500;
}

.env-raw {
    margin-top: 6px;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
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

.setup-note {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    word-break: break-all;
    white-space: pre-wrap;
}

/* 悬停提示的多行正文（弹层虽 teleport 到 body，仍带本组件的 scoped 属性） */
.tip-lines .lt-line {
    display: block;
    line-height: 1.6;
}

.tip-lines .lt-line + .lt-line {
    margin-top: 4px;
}

.mono {
    font-family: Consolas, monospace;
}
</style>
