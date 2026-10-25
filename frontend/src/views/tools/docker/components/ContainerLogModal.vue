<template>
    <!-- 容器日志：数据与跟随开关都在页面，这里只管排版和滚到底 -->
    <a-modal :visible="visible" :title="title" :width="760" :footer="false" @cancel="onClose" @close="onClose">
        <div class="log-head">
            <a-space :size="8">
                <!-- 跟随/停止用圆点表达，与容器列表的状态点同一套语义色 -->
                <span class="com-state" :class="running ? 'is-on' : 'is-dim'">
                    <com-badge :status="running ? 'success' : 'normal'" :animated="running" />
                    {{ running ? '跟随中' : '已停止' }}
                </span>
                <span class="log-count">{{ lines.length }} 行</span>
            </a-space>
            <a-space :size="6">
                <a-switch :model-value="follow" size="small" @change="onFollowChange" />
                <span class="follow-label">实时跟随</span>
                <a-button size="small" @click="emit('clear')">
                    <template #icon><icon-delete /></template>清空
                </a-button>
                <a-button size="small" type="outline" @click="emit('refresh')">
                    <template #icon><icon-refresh /></template>刷新
                </a-button>
            </a-space>
        </div>
        <div ref="boxRef" class="ls-log modal">
            <div v-for="(l, i) in lines" :key="i" class="ls-line">{{ l }}</div>
            <div v-if="lines.length === 0" class="empty-tip">暂无日志</div>
        </div>
    </a-modal>
</template>

<script setup lang="ts">
import { nextTick, ref, watch } from "vue";

const props = defineProps<{
    visible: boolean;
    title: string;
    /** 后端是否在实时跟随（决定标签颜色） */
    running: boolean;
    lines: string[];
    follow: boolean;
}>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "update:follow", v: boolean): void;
    (e: "refresh"): void;
    (e: "clear"): void;
    /** 跟随开关被拨动（页面据最新的 follow 决定开/停跟随） */
    (e: "toggle"): void;
    (e: "close"): void;
}>();

const boxRef = ref<HTMLElement | null>(null);

/** 新行进来就贴底：日志的意义就是看最后几行 */
function scrollBottom() {
    nextTick(() => {
        if (boxRef.value) boxRef.value.scrollTop = boxRef.value.scrollHeight;
    });
}

watch(() => props.lines.length, scrollBottom);
watch(() => props.visible, (v) => { if (v) scrollBottom(); });

function onFollowChange(v: string | number | boolean) {
    emit("update:follow", !!v);
    emit("toggle");
}

function onClose() {
    emit("update:visible", false);
    emit("close");
}
</script>

<style scoped>
.log-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: var(--base-radius-default);
}

.log-count {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.follow-label {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    margin-right: 6px;
}

/* 固定高度 + 内部滚动，遵循弹窗规范（短窗口下随视口收缩） */
.ls-log.modal {
    height: min(420px, calc(100vh - 260px));
    overflow-y: auto;
    background: var(--color-fill-1);
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-default);
    padding: var(--base-padding) var(--base-radius-large);
    font-family: Consolas, monospace;
    font-size: var(--base-size-small);
    color: var(--color-text-1);
    line-height: 1.5;
}

.ls-line {
    white-space: pre-wrap;
    word-break: break-all;
}

.empty-tip {
    color: var(--color-text-3);
    text-align: center;
    font-size: var(--base-size-small);
    padding: var(--base-padding);
}
</style>
