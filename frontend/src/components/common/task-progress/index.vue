<template>
    <div class="ctp" :class="[`ctp--${variant}`, { 'ctp--divided': divided, 'ctp--clickable': clickable }]"
        @click="clickable && emit('click')">
        <!-- 头行：名称 ↔ 速度（无名称且无额外内容时整行不渲染，交给外层卡片头） -->
        <div v-if="hasHeader" class="ctp-head">
            <div class="ctp-name">
                <slot name="name">
                    <span class="ctp-name-text" :title="name">{{ name }}</span>
                </slot>
            </div>
            <div class="ctp-extra">
                <slot name="extra">
                    <span v-if="speed" class="ctp-speed">{{ speed }}</span>
                    <span v-else-if="showPercent && percent != null" class="ctp-speed">{{ Math.round(percent) }}%</span>
                </slot>
            </div>
        </div>

        <!-- 进度行：整宽 -->
        <div class="ctp-bar">
            <a-progress :percent="barPercent" :status="arcStatus" :size="size" :show-text="showText"
                :class="{ 'ctp-indeterminate': indeterminate }">
                <template v-if="showText" #text>
                    <slot name="text">
                        <span v-if="indeterminate" class="ctp-pct">{{ meta || '处理中…' }}</span>
                        <span v-else class="ctp-pct">{{ Math.round(percent || 0) }}%</span>
                    </slot>
                </template>
            </a-progress>
        </div>

        <!-- 备注行（无底部内容时单独整行）：阶段 / 失败原因 / 已传量 -->
        <div v-if="hasMeta && !hasFooter" class="ctp-meta" :class="{ 'ctp-meta--err': status === 'error' && !$slots.meta }">
            <slot name="meta">
                <span class="ctp-meta-text" :title="meta">{{ meta }}</span>
            </slot>
        </div>

        <!-- 底部行：备注与左 ETA / 右操作合一行（有备注时不再多撑一行，整行更紧凑） -->
        <div v-if="hasMeta && hasFooter" class="ctp-row">
            <div class="ctp-row-meta" :class="{ 'ctp-meta--err': status === 'error' && !$slots.meta }">
                <slot name="meta">
                    <span class="ctp-meta-text" :title="meta">{{ meta }}</span>
                </slot>
            </div>
            <div class="ctp-row-foot">
                <div class="ctp-left">
                    <slot name="left">
                        <span v-if="eta" class="ctp-eta">{{ eta }}</span>
                    </slot>
                </div>
                <div class="ctp-actions">
                    <slot name="actions">
                        <a-button v-if="status === 'running' || status === 'stalled'" size="mini"
                            @click="emit('cancel')">
                            取消
                        </a-button>
                        <a-button v-else-if="status === 'paused'" size="mini" type="primary"
                            @click="emit('resume')">
                            继续
                        </a-button>
                        <a-button v-else-if="status === 'error' || status === 'canceled'" size="mini" type="primary"
                            @click="emit('retry')">
                            重试
                        </a-button>
                        <a-button v-else-if="status === 'done'" size="mini" @click="emit('open')">
                            打开
                        </a-button>
                    </slot>
                </div>
            </div>
        </div>

        <!-- 无备注时：底部行退回「左 ETA / 右操作」独占一行 -->
        <div v-else-if="hasFooter" class="ctp-foot">
            <div class="ctp-left">
                <slot name="left">
                    <span v-if="eta" class="ctp-eta">{{ eta }}</span>
                </slot>
            </div>
            <div class="ctp-actions">
                <slot name="actions">
                    <a-button v-if="status === 'running' || status === 'stalled'" size="mini" @click="emit('cancel')">
                        取消
                    </a-button>
                    <a-button v-else-if="status === 'paused'" size="mini" type="primary" @click="emit('resume')">
                        继续
                    </a-button>
                    <a-button v-else-if="status === 'error' || status === 'canceled'" size="mini" type="primary"
                        @click="emit('retry')">
                        重试
                    </a-button>
                    <a-button v-else-if="status === 'done'" size="mini" @click="emit('open')">
                        打开
                    </a-button>
                </slot>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
/**
 * 全局任务/下载进度行。
 *
 * 三段纵排（头行 名称↔速度 / 整宽进度条 / 底部行 左 ETA 右操作）；有备注（meta 或 #meta）时，
 * 备注与底部行合成一行：左备注 / 右 ETA+操作，不再单独撑一行。
 * 纯 UI：不内置卡片外壳（外壳归 common-item-card）、不发起后端调用（动作只 emit，交调用方）。
 * 状态→色统一映射到 Arco a-progress 的 normal|success|warning|danger。
 * 名称为空时头行不渲染——嵌进 common-item-card 里时名称由卡片头承担，这里只补进度/底部行。
 */
import { computed, useSlots } from "vue";

type ProgressStatus = "idle" | "running" | "done" | "error" | "canceled" | "stalled" | "paused";

const props = withDefaults(defineProps<{
    /** 任务名（左，单行省略）；留空则不渲染头行，交由外层卡片头显示名称 */
    name?: string;
    /** 任务状态 */
    status?: ProgressStatus;
    /** 0–100；undefined = 不定进度 */
    percent?: number;
    /** 头行右侧速度，如 "1.2 MB/s" */
    speed?: string;
    /** 底部行左侧剩余时间，如 "大约还需 3 分钟" */
    eta?: string;
    /** 备注行：阶段 / 失败原因 / "已下 3.2 MB/10 MB" */
    meta?: string;
    /** a-progress 尺寸 */
    size?: "small" | "medium";
    /** 进度条内是否显示文案 */
    showText?: boolean;
    /** 头行内没有进度条文案时，是否用右侧数字兜底显示百分比 */
    showPercent?: boolean;
    /** 底部分隔线 */
    divided?: boolean;
    /** plain=独立块自带内边距；bare=嵌进 common-item-card 内不加额外外框 */
    variant?: "plain" | "bare";
    /** 整行可点（emit click） */
    clickable?: boolean;
    /** 显式不定进度（下载中但还没百分比）：画滑动亮带而非停在 0% */
    indeterminate?: boolean;
}>(), {
    status: "running",
    size: "medium",
    showText: true,
    showPercent: true,
    divided: true,
    variant: "plain",
    clickable: false,
    indeterminate: false,
});

const emit = defineEmits<{
    (e: "click"): void;
    (e: "cancel"): void;
    (e: "retry"): void;
    (e: "resume"): void;
    (e: "open"): void;
}>();

const slots = useSlots();

/** 状态 → Arco a-progress status：running→normal、done→success、error→danger、canceled/stalled/paused→warning */
const arcStatus = computed(() => {
    switch (props.status) {
        case "done": return "success";
        case "error": return "danger";
        case "canceled": return "warning";
        case "stalled": return "warning";
        case "paused": return "warning";
        case "running":
        case "idle":
        default: return "normal";
    }
});

/** a-progress 收 0–1；不定进度铺 0（靠 CSS 亮带表现） */
const barPercent = computed(() => {
    if (props.indeterminate) return 0;
    const p = props.percent;
    if (p == null) return 0;
    return Math.max(0, Math.min(1, p / 100));
});

const hasHeader = computed(() =>
    !!props.name || !!slots.name || !!slots.extra || !!props.speed);
const hasMeta = computed(() => !!props.meta || !!slots.meta);
/** 有备注时底部内容并进备注行（左备注 / 右 ETA+操作），不再单独撑一行 */
const hasFooter = computed(() =>
    !!props.eta || !!slots.left || !!slots.actions ||
    props.status === "running" || props.status === "stalled" || props.status === "paused" ||
    props.status === "error" || props.status === "canceled" || props.status === "done");
</script>

<style scoped>
.ctp {
    display: flex;
    flex-direction: column;
    gap: 6px;
    width: 100%;
    min-width: 0;
}

.ctp--plain {
    padding: 8px 10px;
}

.ctp--bare {
    padding: 0;
}

.ctp--divided {
    border-bottom: 1px solid var(--color-border-2);
    padding-bottom: 8px;
}

.ctp--clickable {
    cursor: pointer;
}

/* 头行：名称↔速度 */
.ctp-head,
.ctp-foot {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
}

.ctp-name {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 6px;
}

.ctp-name-text {
    min-width: 0;
    font-size: var(--base-size-default);
    color: var(--color-text-1);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

.ctp-extra {
    flex: none;
    display: flex;
    align-items: center;
    gap: 8px;
}

.ctp-speed,
.ctp-pct {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
}

/* 进度行整宽 */
.ctp-bar {
    width: 100%;
}

/* 备注行 */
.ctp-meta {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    line-height: 1.6;
    word-break: break-all;
}

.ctp-meta--err {
    color: var(--color-text-1);
    padding: 4px 8px;
    border-left: 2px solid rgb(var(--danger-6));
    border-radius: 0 var(--base-radius-small) var(--base-radius-small) 0;
    background: rgba(var(--danger-6), 0.08);
}

/* 底部行：左 ETA / 右操作 */
.ctp-left {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 11px;
    color: var(--color-text-3);
}

.ctp-eta {
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

.ctp-actions {
    flex: none;
    display: flex;
    align-items: center;
    gap: 10px;
}

/* 合并行：左备注（可挤压省略）/ 右 ETA+操作（靠内容宽度） */
.ctp-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    min-width: 0;
}

.ctp-row-meta {
    flex: 1;
    min-width: 0;
    /* 备注插槽可能给多个子节点（正文 + 展开链接），横向排不撑高；子节点默认占满宽度 */
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    line-height: 1.6;
}

.ctp-row-meta>* {
    flex: 1;
    min-width: 0;
}

.ctp-row-foot {
    flex: none;
    display: flex;
    align-items: center;
    gap: 12px;
    min-width: 0;
}

.ctp-row .ctp-left {
    flex: none;
}

/* 不定进度：满宽底槽 + 无限滑动亮带，比停在 0% 的灰条诚实 */
.ctp-indeterminate :deep(.arco-progress-line-bar) {
    position: relative;
    width: 36% !important;
    animation: ctp-slide 1.3s ease-in-out infinite;
}

@keyframes ctp-slide {
    0% {
        left: -36%;
    }

    100% {
        left: 100%;
    }
}
</style>
