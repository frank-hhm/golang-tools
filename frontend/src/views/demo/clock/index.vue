<template>
    <!-- common-canvas-clock 组件演示：默认紫粉 / 青绿配色 / 小尺寸 三个实例 + 交互控制 -->
    <layout-body title="Canvas 时钟组件 Demo">
        <div class="clock-demo">
            <a-card title="common-canvas-clock 时钟组件" :bordered="true">
                <template #extra>
                    <a-space>
                        <a-switch v-model:checked="showInfo" size="small" />
                        <span class="hint">盘面信息（日期/星期/年份/毫秒）</span>
                        <a-slider v-model="size" :min="120" :max="420" :step="10" style="width: 140px" />
                        <span class="hint">{{ size }}px</span>
                    </a-space>
                </template>
                <a-space size="large" wrap>
                    <div class="cell">
                        <common-canvas-clock :size="size" :show-info="showInfo" />
                        <p>默认配色（发光秒针 + 表盘内阴影）</p>
                    </div>
                    <div class="cell">
                        <!-- 换套 ring/face/num 颜色即可整体变色，内阴影随 ring 颜色自动着色 -->
                        <common-canvas-clock :size="size" :show-info="showInfo" ring-color="#0b3d4d" face-color="#0f172a"
                            num-color="#7dd3fc" hour-color="#94d8ea" minute-color="#38bdf8" second-color="#fbbf24" />
                        <p>暗黑青绿表盘</p>
                    </div>
                    <div class="cell">
                        <common-canvas-clock :size="Math.round(size * 0.5)" :show-info="showInfo" ring-color="#33291b"
                            num-color="#8d6e63" hour-color="#4e342e" minute-color="#8d6e63" second-color="#ef5350" />
                        <p>小尺寸（全部元素等比缩放）</p>
                    </div>
                </a-space>
            </a-card>
            <a-typography-paragraph type="secondary">
                用法：&lt;common-canvas-clock :size="320" :show-info="true" ring-color / face-color / num-color /
                hour-color / minute-color / second-color /&gt;。全局注册组件，零依赖；秒针连续平滑带光晕，
                日期/星期/年份水印/百分秒每帧跑动；画布按 devicePixelRatio 渲染，高分屏不发虚。
            </a-typography-paragraph>
        </div>
    </layout-body>
</template>
<script lang="ts">
export default {
    name: "demo-clock",
};
</script>
<script lang="ts" setup>
import { ref } from "vue";

const size = ref(280);
const showInfo = ref(true);
</script>
<style scoped>
.clock-demo {
    padding: 4px;
}
.clock-demo .cell {
    text-align: center;
}
.clock-demo .cell p {
    margin: 8px 0 0;
    color: var(--color-text-3);
    font-size: 12px;
}
.clock-demo .hint {
    color: var(--color-text-3);
    font-size: 12px;
    white-space: nowrap;
}
</style>
