<template>
    <!-- 弹窗里的状态提示：吸在 body 顶部，滚表单时始终可见；只占一行，长文本走 tooltip。
         以前塞在表单项中间，一条带端点串的错误会把弹窗撑高、把后面的字段全推下去 -->
    <div v-if="text" class="mt-tip" :class="'tone-' + type" :style="bleedStyle">
        <icon-exclamation-circle class="mt-icon" />
        <a-tooltip position="bl" :disabled="!text">
            <!-- 单行装不下的部分（端点明细、修复步骤等）全放浮层，悬停一次看全 -->
            <template #content>
                <div class="mt-detail">
                    <div v-for="(l, i) in detailLines" :key="i" class="mt-detail-line">{{ l }}</div>
                </div>
            </template>
            <span class="mt-text">{{ text }}</span>
        </a-tooltip>
        <span v-if="$slots.default" class="mt-extra"><slot /></span>
    </div>
</template>

<script setup lang="ts">
import { computed } from "vue";

/**
 * 吸顶提示条。
 * 依赖父级滚动容器是 a-modal 的 body（Arco 默认 padding 24px 20px），
 * 负边距就是用来吃掉这段内边距，让提示条贴住标题栏、滚动时不被内容从上方挤过。
 * 若某个弹窗自定义了 body padding，用 bleedX / bleedY 把负边距对齐过去。
 */
const props = withDefaults(defineProps<{
    /** 提示正文；空串/undefined 时不渲染 */
    text?: string;
    /** 语气：warning 环境未就绪、error 操作失败、info 纯说明 */
    type?: "warning" | "error" | "info";
    /** 附在正文之后的行（如修复步骤），只在浮层里展开，不占提示条高度 */
    lines?: string[];
    /** 父级 body 的左右内边距，负边距要跟它对齐；Arco 默认 20px */
    bleedX?: number;
    /** 父级 body 的上内边距；Arco 默认 24px */
    bleedY?: number;
}>(), { text: "", type: "warning", lines: () => [], bleedX: 20, bleedY: 24 });

const bleedStyle = computed(() => ({
    "--mt-bleed-x": `${props.bleedX}px`,
    "--mt-bleed-y": `${props.bleedY}px`,
}));

/**
 * 浮层里按行排：Docker 那条错误是「结论（端点：npipе://\\.\pipe\docker_engine（候选…））」一串到底，
 * 直接塞进去只能靠折行，读不出重点；从「（端点」拆开后结论一行、端点明细一行。
 */
const detailLines = computed(() => {
    const out: string[] = [];
    const raw = props.text || "";
    const i = raw.indexOf("（端点");
    if (i > 0) {
        out.push(raw.slice(0, i), raw.slice(i + 1).replace(/[）]+$/g, ""));
    } else if (raw) {
        out.push(raw);
    }
    return out.concat(props.lines || []);
});
</script>

<style scoped>
.mt-tip {
    /* 吃掉 a-modal-body 的内边距（由 bleedX/bleedY 覆盖），滚动时贴住标题栏下沿 */
    position: sticky;
    top: 0;
    z-index: 5;
    display: flex;
    align-items: center;
    gap: 8px;
    margin: calc(-1 * var(--mt-bleed-y)) calc(-1 * var(--mt-bleed-x)) 14px;
    padding: 9px var(--mt-bleed-x);
    font-size: var(--base-size-small);
    line-height: 1.5;
    border-bottom: 1px solid rgba(var(--warning-6), 0.28);
    background: rgba(var(--warning-6), 0.09);
}

.mt-icon {
    flex: none;
    font-size: 14px;
    color: rgb(var(--warning-6));
}

/* 单行截断：提示条高度固定，不随错误串长度变化；完整内容悬停看 */
.mt-text {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--color-text-1);
    cursor: help;
}

.mt-extra {
    flex: none;
    display: inline-flex;
    align-items: center;
    gap: 6px;
}

.mt-detail-line {
    /* 管道端点这类无空格长串（npipе://\\.\pipe\docker_engine）必须允许硬断，否则浮层会撑成一条横向超长 */
    overflow-wrap: anywhere;
}

.mt-detail {
    max-width: 420px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    font-size: var(--base-size-small);
    line-height: 1.6;
    white-space: normal;
}

.tone-error {
    border-bottom-color: rgba(var(--danger-6), 0.28);
    background: rgba(var(--danger-6), 0.09);
}

.tone-error .mt-icon {
    color: rgb(var(--danger-6));
}

.tone-info {
    border-bottom-color: var(--color-border-1);
    background: var(--color-fill-1);
}

.tone-info .mt-icon {
    color: rgb(var(--primary-6));
}
</style>
