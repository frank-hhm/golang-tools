<template>
    <!-- 通用日志弹窗：只负责「标题 + 可滚动日志区 + 自动贴底 + 底部左右两区」这套排版。
         数据与轮询/跟随等一律留在调用方：调用方传 lines，按需往下面的插槽里塞自己的内容。
         插槽约定：
           #alert     —— 正文最上方的横幅（放 a-alert，如环境未就绪 / 重要提醒）
           #head      —— 日志区上方的一行状态标记（如 运行中 / 端口 / 容器 / 行数）
           #tip       —— 底部左侧：提示文案或次要标记
           #operations—— 底部右侧：操作按钮（清空 / 刷新 / 连接 …） -->
    <a-modal :visible="modelValue" :title="title" :width="width" :footer="hasFooter" :unmount-on-close="unmountOnClose"
        @update:visible="onVisible" @close="emit('close')">
        <div class="logm-body">
            <div v-if="$slots.alert" class="logm-alert"><slot name="alert" /></div>
            <div v-if="$slots.head" class="logm-head"><slot name="head" /></div>
            <div ref="boxRef" class="logm-log" :style="height ? { height } : undefined">
                <div v-for="(l, i) in lines" :key="i" class="logm-line">{{ l }}</div>
                <div v-if="lines.length === 0" class="logm-empty">{{ emptyText }}</div>
            </div>
        </div>
        <template v-if="hasFooter" #footer>
            <div class="logm-footer">
                <div class="logm-tip"><slot name="tip" /></div>
                <div class="logm-ops"><slot name="operations" /></div>
            </div>
        </template>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, nextTick, useSlots, ref, watch } from "vue";

const props = withDefaults(defineProps<{
    /** 可见性（v-model） */
    modelValue: boolean;
    /** 弹窗标题 */
    title: string;
    /** 日志行：由调用方拉取/轮询后传入，本组件只管渲染与贴底 */
    lines: string[];
    /** 弹窗宽度 */
    width?: number;
    /** 空态文案 */
    emptyText?: string;
    /** 日志区高度覆盖（留空用默认 min(420px, calc(100vh - 260px))） */
    height?: string;
    /** 关闭即卸载（默认 true：每次打开都是干净的重挂） */
    unmountOnClose?: boolean;
}>(), { width: 760, emptyText: "暂无日志", height: "", unmountOnClose: true });

const emit = defineEmits<{
    (e: "update:modelValue", v: boolean): void;
    /** 弹窗关闭（用户点 X / 遮罩 / ESC）：调用方据此停轮询等 */
    (e: "close"): void;
}>();

const slots = useSlots();
/** 只有传了 tip 或 operations 才渲染底部栏，否则整条 footer 收起 */
const hasFooter = computed(() => !!(slots.tip || slots.operations));

function onVisible(v: boolean) {
    emit("update:modelValue", v);
}

const boxRef = ref<HTMLElement | null>(null);
/** 新行进来就贴底：日志的意义就是看最后几行 */
function scrollBottom() {
    nextTick(() => { if (boxRef.value) boxRef.value.scrollTop = boxRef.value.scrollHeight; });
}
watch(() => props.lines.length, scrollBottom);
watch(() => props.modelValue, (v) => { if (v) scrollBottom(); });
</script>

<style scoped>
.logm-body {
    display: flex;
    flex-direction: column;
    gap: var(--base-margin);
}

.logm-alert :deep(.arco-alert) {
    margin: 0;
}

/* 状态标记行：调用方塞进来的标记成组靠左排（状态点 + 文案 + 端口/容器标签…），
   窄屏自动换行；需要靠右的次要信息由调用方自行 margin-left:auto */
.logm-head {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: var(--base-padding);
    font-size: var(--base-size-small);
}

/* 固定高度 + 内部滚动：短窗口下随视口收缩 */
.logm-log {
    height: min(420px, calc(100vh - 260px));
    overflow-y: auto;
    background: var(--color-fill-1);
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-default);
    padding: var(--base-padding) var(--base-radius-large);
    font-family: Consolas, Menlo, monospace;
    font-size: var(--base-size-small);
    color: var(--color-text-1);
    line-height: 1.5;
}

.logm-line {
    white-space: pre-wrap;
    word-break: break-all;
    padding: calc(var(--base-padding) / 2);
}

.logm-empty {
    color: var(--color-text-3);
    text-align: center;
    font-size: var(--base-size-small);
    padding: var(--base-padding);
}

/* 底部：左提示 / 右操作 */
.logm-footer {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--base-padding);
    width: 100%;
}

.logm-tip {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: var(--base-padding);
    color: var(--color-text-3);
    font-size: var(--base-size-small);
}

.logm-ops {
    flex: none;
    display: flex;
    align-items: center;
    gap: 6px;
}
</style>
