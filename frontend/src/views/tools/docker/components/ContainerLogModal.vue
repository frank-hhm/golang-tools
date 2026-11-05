<template>
    <!-- 容器日志：数据与跟随开关都在页面，这里只管填 nginx/docker 专属的状态与操作，排版交给 common-log-modal -->
    <common-log-modal :model-value="visible" :title="title" :lines="lines" :width="760"
        @update:model-value="onVisible" @close="emit('close')">
        <template #head>
            <a-space :size="8">
                <!-- 跟随/停止用圆点表达，与容器列表的状态点同一套语义色 -->
                <span class="com-state" :class="running ? 'is-on' : 'is-dim'">
                    <com-badge :status="running ? 'success' : 'normal'" :animated="running" />
                    {{ running ? '跟随中' : '已停止' }}
                </span>
                <span class="log-count">{{ lines.length }} 行</span>
            </a-space>
        </template>
        <template #operations>
            <a-switch :model-value="follow" size="small" @change="onFollowChange" />
            <span class="follow-label">实时跟随</span>
            <a-popconfirm content="确定清空容器日志？将只保留清空之后新产生的日志（Docker Desktop 下容器历史日志无法从本地截断）。" type="warning"
                position="bl" @ok="emit('clear')">
                <a-button size="small">
                    <template #icon><icon-delete /></template>清空
                </a-button>
            </a-popconfirm>
            <a-button size="small" @click="emit('refresh')">
                <template #icon><icon-refresh /></template>刷新
            </a-button>
        </template>
    </common-log-modal>
</template>

<script setup lang="ts">
defineProps<{
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

function onVisible(v: boolean) {
    emit("update:visible", v);
}

function onFollowChange(v: string | number | boolean) {
    emit("update:follow", !!v);
    emit("toggle");
}
</script>

<style scoped>
.log-count {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.follow-label {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    margin-right: 6px;
}
</style>
