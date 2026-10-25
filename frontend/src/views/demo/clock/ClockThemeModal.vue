<template>
    <!-- 自定义配色弹窗：改草稿，点「应用」才回写给页面；取消不影响已有配色 -->
    <a-modal :visible="visible" title="自定义配色" :width="480" :mask-closable="false" ok-text="应用" @ok="apply"
        @cancel="close" @update:visible="onVisible">
        <div class="tc-tip">
            颜色缺省时会跟随 Arco 主题；这里六项都显式给值，暗黑模式下也保持不变。
        </div>
        <div v-for="f in COLOR_FIELDS" :key="f.key" class="tc-row">
            <span class="tc-label">{{ f.label }}</span>
            <a-color-picker v-model="draft[f.key]" size="small" show-text disabled-alpha class="tc-picker" />
            <span class="tc-hint">{{ f.hint }}</span>
        </div>
        <a-space :size="8" class="tc-actions">
            <a-button size="small" @click="reset">
                <template #icon><icon-refresh /></template>恢复默认
            </a-button>
            <span class="tc-hint">改完点「应用」生效</span>
        </a-space>
        <!-- 小尺寸实时预览：边调边看，不用来回关弹窗 -->
        <div class="tc-preview">
            <canvas-clock :size="150" :show-info="false" v-bind="draft" />
        </div>
    </a-modal>
</template>
<script lang="ts">
export default {
    name: "clock-theme-modal",
};
</script>
<script lang="ts" setup>
import { reactive, watch } from "vue";
import CanvasClock from "@/components/common/canvas-clock/index.vue";
import { COLOR_FIELDS, CUSTOM_DEFAULT, type ClockColors } from "./presets";

const props = defineProps<{
    visible: boolean;
    /** 页面当前生效的配色 */
    colors: ClockColors;
}>();

const emit = defineEmits<{
    (e: "update:visible", value: boolean): void;
    (e: "apply", value: ClockColors): void;
}>();

// 草稿：弹窗内的改动不直接改页面，取消即丢
const draft = reactive<ClockColors>({ ...CUSTOM_DEFAULT });

// 每次打开都拿页面的当前配色重置草稿，避免上次没应用的残留
watch(
    () => props.visible,
    (v) => {
        if (v) Object.assign(draft, props.colors);
    }
);

function close() {
    emit("update:visible", false);
}

function onVisible(v: boolean) {
    emit("update:visible", v);
}

function reset() {
    Object.assign(draft, CUSTOM_DEFAULT);
}

function apply() {
    emit("apply", { ...draft });
    close();
}
</script>
<style scoped>
.tc-tip {
    margin-bottom: 12px;
    color: var(--color-text-3);
    font-size: 12px;
}

.tc-row {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 4px 0;
}

.tc-label {
    width: 88px;
    flex-shrink: 0;
    color: var(--color-text-2);
    font-size: 13px;
}

.tc-picker {
    width: 128px;
    flex-shrink: 0;
}

.tc-hint {
    color: var(--color-text-3);
    font-size: 12px;
}

.tc-actions {
    margin-top: 8px;
}

.tc-preview {
    display: flex;
    justify-content: center;
    margin-top: 12px;
    padding: 12px;
    border-radius: var(--border-radius-medium);
    background: var(--color-fill-1);
}
</style>
