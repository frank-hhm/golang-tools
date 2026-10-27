<template>
    <!-- 环境体检：只负责把体检结果摊开，重新体检 / 装 WSL2 都交回页面执行 -->
    <a-modal :visible="visible" title="环境体检与诊断" :width="760"
        @cancel="emit('update:visible', false)" @update:visible="(v: boolean) => emit('update:visible', v)">
        <!-- 操作按钮收进弹窗底部右侧（Arco footer 默认右对齐），正文只留体检信息与提示 -->
        <template #footer>
            <a-space :size="8">
                <a-button size="small" :loading="envLoading" @click="emit('refresh')">
                    <template #icon><icon-refresh /></template>重新体检
                </a-button>
                <a-button v-if="!env.wslInstalled" size="small" type="primary" :loading="setupBusy === 'wsl'"
                    :disabled="!!setupBusy" @click="emit('enable-wsl')">
                    <template #icon><icon-common /></template>启用 WSL2
                </a-button>
                <a-button v-else size="small" :loading="setupBusy === 'wslupd'" :disabled="!!setupBusy"
                    @click="emit('update-wsl')">
                    <template #icon><icon-common /></template>更新 WSL2
                </a-button>
            </a-space>
        </template>
        <div class="env-grid">
            <div class="env-item">
                <div>Docker Desktop</div>
                <div class="com-state" :class="env.dockerInstalled ? 'is-on' : 'is-off'">
                    <com-badge :status="env.dockerInstalled ? 'success' : 'danger'" />
                    {{ env.dockerInstalled ? ('已安装 ' + (env.dockerVersion || '-')) : '未安装' }}
                </div>
            </div>
            <div class="env-item">
                <div>WSL2</div>
                <div>
                    <!-- 悬停看原文与发行版清单：没发行版并不等于 WSL2 不可用 -->
                    <a-tooltip position="bottom" :content-style="{ maxWidth: '440px' }">
                        <template #content>
                            <div class="tip-lines">
                                <div class="lt-line">{{ env.wslStatus || '未探测' }}</div>
                                <div v-if="env.wslDistros?.length" class="lt-line">发行版：{{ env.wslDistros.join('、') }}</div>
                                <div class="lt-line">docker-desktop 发行版：{{ dockerDistroText }}</div>
                            </div>
                        </template>
                        <div class="com-state" :class="env.wslInstalled ? 'is-on' : 'is-warn'">
                            <com-badge :status="env.wslInstalled ? 'success' : 'warning'" />
                            {{ env.wslInstalled ? '已就绪' : '未就绪' }}
                        </div>
                    </a-tooltip>
                </div>
            </div>
            <div class="env-item">
                <div>引擎实际后端</div>
                <div class="com-state" :class="curBackend === 'hyperv' ? 'is-warn' : 'is-on'">
                    <com-badge :status="curBackend === 'hyperv' ? 'warning' : 'success'" />
                    {{ env.backend?.label || '未知' }}
                </div>
            </div>
            <div class="env-item">
                <div>Hyper-V 角色</div>
                <div class="com-state" :class="hypervTag.cls">
                    <com-badge :status="hypervTag.status" />{{ hypervTag.text }}
                </div>
            </div>
            <div class="env-item">
                <div>命名管道</div>
                <div class="com-state" :class="env.pipeExists ? 'is-on' : 'is-dim'">
                    <com-badge :status="env.pipeExists ? 'success' : 'normal'" />
                    {{ env.pipeExists ? '已监听' : '不存在' }}
                </div>
            </div>
            <div class="env-item">
                <div>离线安装包</div>
                <div class="com-state" :class="env.installerFile ? 'is-on' : 'is-dim'">
                    <com-badge :status="env.installerFile ? 'success' : 'normal'" />
                    {{ env.installerFile ? fmtBytes(env.installerSize) : '无' }}
                </div>
            </div>
        </div>
        <a-tooltip v-if="env.wslStatus" :content="env.wslStatus">
            <div class="env-raw mono">{{ env.wslStatus }}</div>
        </a-tooltip>
        <!-- 提示改用 a-alert：体检结论收进一条 info 提示，操作反馈单独一条 warning -->
        <a-alert v-if="envNotes.length" class="env-alert" type="warning" :show-icon="true">
            <div class="alert-lines">
                <div v-for="(g, i) in envNotes" :key="'n' + i" class="alert-line">{{ g }}</div>
            </div>
        </a-alert>
        <a-alert v-if="setupNote" class="env-alert" type="warning" :show-icon="true">
            <div class="alert-lines">
                <div class="alert-line">{{ setupNote }}</div>
            </div>
        </a-alert>
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

.env-alert {
    margin-top: var(--base-radius-default);
    align-items: flex-start;
}

.alert-lines {
    display: flex;
    flex-direction: column;
    gap: 2px;
}

.alert-line {
    font-size: var(--base-size-small);
    line-height: 1.7;
    word-break: break-word;
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
