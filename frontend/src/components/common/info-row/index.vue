<template>
    <!-- 通用信息行：标签 | 内容 | 右侧操作，行间细分隔线（参考"账号安全设置"式列表）。
         纯 UI 组件，供站点信息 / 后续其它详情面板复用。
         字体 / 圆角 / 间距一律取 index.scss 的 :root 变量，不写死 px。 -->
    <div class="info-row">
        <div class="ir-label">
            <span class="ir-label-text">{{ label }}</span>
            <a-tooltip v-if="tip" :content="tip">
                <icon-question-circle class="ir-tip" />
            </a-tooltip>
        </div>
        <div class="ir-content" :class="{ 'text-grey': grey }">
            <slot>
                <a-tooltip v-if="value" :content="value">
                    <b class="ir-value">{{ value }}</b>
                </a-tooltip>
                <span v-else class="ir-dash">-</span>
            </slot>
        </div>
        <div class="ir-action">
            <slot name="action">
                <!-- 行内操作用 a-button（遵循按钮规范：不用 type=text） -->
                <a-button v-if="actionText" size="mini" @click="emit('action')">{{ actionText }}</a-button>
            </slot>
        </div>
    </div>
</template>

<script setup lang="ts">
withDefaults(defineProps<{
    /** 左侧标签 */
    label: string;
    /** 标签后的 ? 提示（悬停气泡），用于解释字段含义 */
    tip?: string;
    /** 简单文本值（默认内容）：自动省略 + tooltip；需要自定义内容时用默认插槽覆盖 */
    value?: string;
    /** 右侧操作链接文字（如"打开""修改"）；留空则不显示操作 */
    actionText?: string;
    /** 内容是否置灰（如占位、不可用、次要信息）；true 时给 ir-content 加 text-grey */
    grey?: boolean;
}>(), { tip: "", value: "", actionText: "", grey: false });

const emit = defineEmits<{ (e: "action"): void }>();
</script>

<style scoped>
.info-row {
    display: flex;
    align-items: center;
    gap: var(--base-padding);
    padding: var(--base-padding) 0;
    border-bottom: 1px solid var(--color-border-1);
    font-size: var(--base-size-default);
}

.ir-label {
    flex: none;
    width: 96px;
    display: flex;
    align-items: center;
    gap: 4px;
    color: var(--color-text-2);
}

.ir-label-text {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.ir-content {
    flex: 1;
    min-width: 0;
    color: var(--color-text-1);
}

/* 置灰：内容整体转次要色（含默认 value，靠 ir-value 继承） */
.ir-content.text-grey {
    color: var(--color-text-3);
}

.ir-value {
    display: block;
    font-weight: 500;
    color: inherit;
    line-height: 1.5;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.ir-dash {
    color: var(--color-text-4);
}

.ir-action {
    flex: none;
    display: flex;
    align-items: center;
}

.ir-tip {
    color: var(--color-text-3);
    font-size: var(--base-size-small);
    cursor: help;
}
</style>
