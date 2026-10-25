<template>
    <!-- 用法说明弹窗：参数表与要点，页面底部不再堆一长段文字 -->
    <a-modal :visible="visible" title="common-canvas-clock 用法" :width="620" :footer="false"
        @update:visible="onVisible">
        <pre class="uc-code">{{ USAGE_SNIPPET }}</pre>
        <div class="uc-lines">
            <div v-for="(line, i) in USAGE_LINES" :key="i" class="uc-line">{{ line }}</div>
        </div>
        <a-divider :margin="12" />
        <div class="uc-props">
            <div v-for="row in PROPS" :key="row.name" class="uc-prop">
                <span class="uc-name mono">{{ row.name }}</span>
                <span class="uc-type mono">{{ row.type }}</span>
                <span class="uc-desc">{{ row.desc }}</span>
            </div>
        </div>
    </a-modal>
</template>
<script lang="ts">
export default {
    name: "clock-usage-modal",
};
</script>
<script lang="ts" setup>
import { USAGE_LINES, USAGE_SNIPPET } from "./presets";

defineProps<{ visible: boolean }>();

const emit = defineEmits<{ (e: "update:visible", value: boolean): void }>();

function onVisible(v: boolean) {
    emit("update:visible", v);
}

/** 参数表：与 canvas-clock 的 props 一一对应 */
const PROPS = [
    { name: "size", type: "number = 320", desc: "表盘边长（px，正方形），所有元素按它等比缩放" },
    { name: "show-info", type: "boolean = true", desc: "盘面上的日期、星期、年份水印与百分秒" },
    { name: "ring-color", type: "string = 主题", desc: "最外圈圆环，表盘内阴影随它着色" },
    { name: "face-color", type: "string = 主题", desc: "表盘底色" },
    { name: "num-color", type: "string = 主题", desc: "刻度数字与日期时间文字" },
    { name: "hour-color", type: "string = 主题", desc: "时针" },
    { name: "minute-color", type: "string = 主题", desc: "分针" },
    { name: "second-color", type: "string = 主题", desc: "秒针（含光晕与中心轴点）" },
];
</script>
<style scoped>
.mono {
    font-family: Consolas, monospace;
}

.uc-code {
    margin: 0 0 12px;
    padding: 10px 12px;
    border-radius: var(--border-radius-medium);
    background: var(--color-fill-1);
    color: var(--color-text-1);
    font-family: Consolas, monospace;
    font-size: 12px;
    line-height: 1.7;
    white-space: pre-wrap;
}

.uc-lines {
    display: flex;
    flex-direction: column;
    gap: 6px;
}

.uc-line {
    position: relative;
    padding-left: 14px;
    color: var(--color-text-2);
    font-size: 13px;
    line-height: 1.6;
}

.uc-line::before {
    content: "";
    position: absolute;
    left: 2px;
    top: 9px;
    width: 5px;
    height: 5px;
    border-radius: 50%;
    background: rgb(var(--primary-6));
}

.uc-props {
    display: flex;
    flex-direction: column;
    gap: 4px;
}

.uc-prop {
    display: flex;
    align-items: baseline;
    gap: 10px;
    font-size: 12px;
}

.uc-name {
    width: 108px;
    flex-shrink: 0;
    color: rgb(var(--primary-6));
}

.uc-type {
    width: 120px;
    flex-shrink: 0;
    color: var(--color-text-3);
}

.uc-desc {
    flex: 1;
    min-width: 0;
    color: var(--color-text-2);
}
</style>
