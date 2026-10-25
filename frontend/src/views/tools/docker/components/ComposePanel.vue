<template>
    <!-- 编排视角：按 compose 标签归组出来的项目卡片墙，操作只回传项目名与动作，接口调用留在页面 -->
    <div class="cp-root">
        <div v-if="!projects.length" class="cp-empty">
            <a-empty :image="false" description="没有检测到编排项目">
                <div class="cp-empty-tip">
                    用 docker compose 启动的容器会带着 com.docker.compose.* 标签，面板据此把它们归成一个项目；
                    直接用 docker run 起的容器只出现在「容器」页。
                </div>
            </a-empty>
        </div>

        <div v-else class="cp-grid">
            <div v-for="p in projects" :key="p.name" class="cp-card">
                <div class="cp-head">
                    <com-badge :status="badgeOf(p)" :animated="p.status === 'running'" />
                    <a-tooltip :content="p.name">
                        <span class="cp-name">{{ p.name }}</span>
                    </a-tooltip>
                    <span class="cp-count">{{ p.running }}/{{ p.total }}</span>
                    <a-tag size="small" :color="tagColor(p)">{{ statusText(p) }}</a-tag>
                </div>

                <div class="cp-sub">
                    <icon-folder class="cp-ico" />
                    <a-tooltip :content="p.workingDir || '工作目录未知'">
                        <span class="cp-path mono">{{ p.workingDir || '-' }}</span>
                    </a-tooltip>
                </div>

                <div class="cp-svcs">
                    <div v-for="s in p.services" :key="s.id" class="cp-svc">
                        <com-badge :status="s.state === 'running' ? 'success' : 'normal'" />
                        <a-tooltip :content="s.name">
                            <span class="cp-svc-name">{{ s.name }}</span>
                        </a-tooltip>
                        <a-tooltip :content="s.container || s.id">
                            <span class="cp-svc-ct mono">{{ s.container || s.id }}</span>
                        </a-tooltip>
                        <a-tooltip :content="s.status || s.state">
                            <span class="cp-svc-st">{{ s.status || s.state }}</span>
                        </a-tooltip>
                    </div>
                </div>

                <div class="cp-foot">
                    <a-button size="small" :loading="busy" :disabled="busy || p.running >= p.total"
                        @click="emit('action', p, 'start')">
                        <template #icon><icon-play-arrow /></template>启动
                    </a-button>
                    <a-button size="small" :loading="busy" :disabled="busy || !p.running"
                        @click="emit('action', p, 'stop')">
                        <template #icon><icon-pause /></template>停止
                    </a-button>
                    <a-button size="small" :loading="busy" :disabled="busy" @click="emit('action', p, 'restart')">
                        <template #icon><icon-refresh /></template>重启
                    </a-button>
                    <a-popconfirm :content="`移除项目「${p.name}」的 ${p.total} 个容器？容器会先停再删，卷与网络不动。`"
                        type="warning" position="lt" @ok="emit('action', p, 'down')">
                        <a-button size="small" status="danger" :loading="busy" :disabled="busy">
                            <template #icon><icon-delete /></template>移除容器
                        </a-button>
                    </a-popconfirm>
                    <span class="cp-time">创建于 {{ fmtUnix(p.created) }}</span>
                </div>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { fmtUnix } from "../helpers";

defineProps<{
    projects: any[];
    loading: boolean;
    /** 有批量操作在跑：按钮统一置灰，避免对着同一个项目连点 */
    busy: boolean;
}>();

const emit = defineEmits<{
    (e: "action", project: any, action: "start" | "stop" | "restart" | "down"): void;
}>();

function badgeOf(p: any): "success" | "warning" | "normal" {
    if (p.status === "running") return "success";
    if (p.status === "partial") return "warning";
    return "normal";
}

function statusText(p: any): string {
    if (p.status === "running") return "全部运行";
    if (p.status === "partial") return "部分运行";
    return "已停止";
}

function tagColor(p: any): string {
    if (p.status === "running") return "green";
    if (p.status === "partial") return "orange";
    return "gray";
}
</script>

<style scoped>
.cp-root {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: 12px;
}

.cp-empty {
    padding: 32px 0;
    text-align: center;
}

.cp-empty-tip {
    margin-top: 8px;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    line-height: 1.7;
    max-width: 560px;
    margin-left: auto;
    margin-right: auto;
    text-align: left;
}

.cp-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(360px, 1fr));
    gap: var(--base-margin);
    align-content: start;
}

.cp-card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: var(--base-padding);
    background: var(--color-fill-1);
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-default);
}

.cp-head {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
}

.cp-name {
    flex: 1 1 auto;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-weight: 600;
    color: var(--color-text-1);
}

.cp-count {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    font-variant-numeric: tabular-nums;
}

.cp-sub {
    display: flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.cp-ico {
    flex: none;
}

.cp-path {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

/* 服务明细：服务名 / 容器名 / 状态 三列，容器名吃掉剩余宽度 */
.cp-svcs {
    display: flex;
    flex-direction: column;
    gap: 4px;
    padding: 8px 0;
    border-top: 1px dashed var(--color-border-2);
    border-bottom: 1px dashed var(--color-border-2);
    max-height: 190px;
    overflow-y: auto;
}

.cp-svc {
    display: grid;
    grid-template-columns: auto minmax(60px, 0.6fr) minmax(90px, 1fr) minmax(60px, 0.7fr);
    align-items: center;
    gap: 8px;
    font-size: var(--base-size-small);
}

.cp-svc-name {
    color: var(--color-text-1);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.cp-svc-ct,
.cp-svc-st {
    color: var(--color-text-3);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.cp-foot {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
}

.cp-time {
    margin-left: auto;
    font-size: var(--base-size-small);
    color: var(--color-text-4);
}

.mono {
    font-family: Consolas, monospace;
}
</style>
