<template>
    <!--
        透明主题实验台（隔离页，阶段一仅调参预览，不碰全局）。
        目的：在受控样例（卡片 / 浮层 / 弹窗）上验证「表面磨砂」观感——
        CSS backdrop-filter: blur + 半透明底 + 描边——确定后再另开计划铺全局（阶段二）。
        这里只改本页 scoped 的 CSS 变量，绝不影响主窗/其它页面/全局令牌。
    -->
    <layout-body title="透明主题实验台">
        <div class="glass-lab">
            <a-alert type="warning" class="lab-warn">
                本页是<strong>隔离实验台</strong>：调参只作用于下方预览区，不写全局令牌、不改主窗，也不建窗口级透明窗。
                窗口级 DWM Acrylic/Mica 曾因与无边框冲突致「灰纱/白跳」整体下线，本轮仍以 <strong>CSS 表面磨砂 + 现有 Solid 窗</strong> 探索，
                观感确认后再另开计划铺全局。
            </a-alert>

            <a-row :gutter="12">
                <!-- 左：调参 -->
                <a-col :span="8">
                    <a-card title="磨砂参数" :bordered="true" class="ctrl-card">
                        <div class="ctrl-row">
                            <span class="ctrl-label">底透明度</span>
                            <span class="ctrl-val">{{ Math.round(alpha * 100) }}%</span>
                        </div>
                        <a-slider v-model="alpha" :min="0" :max="1" :step="0.02" />

                        <div class="ctrl-row">
                            <span class="ctrl-label">模糊半径</span>
                            <span class="ctrl-val">{{ blur }}px</span>
                        </div>
                        <a-slider v-model="blur" :min="0" :max="40" :step="1" />

                        <div class="ctrl-row">
                            <span class="ctrl-label">描边强度</span>
                            <span class="ctrl-val">{{ Math.round(border * 100) }}%</span>
                        </div>
                        <a-slider v-model="border" :min="0" :max="1" :step="0.02" />

                        <div class="ctrl-row">
                            <span class="ctrl-label">背景图</span>
                        </div>
                        <a-radio-group v-model="bgKind" type="button" size="small">
                            <a-radio value="mesh">彩色网格</a-radio>
                            <a-radio value="photo">风景照</a-radio>
                            <a-radio value="plain">纯色</a-radio>
                        </a-radio-group>

                        <div class="ctrl-row">
                            <span class="ctrl-label">预览底色</span>
                        </div>
                        <a-radio-group v-model="tint" type="button" size="small">
                            <a-radio value="light">浅色</a-radio>
                            <a-radio value="dark">深色</a-radio>
                        </a-radio-group>

                        <a-space class="ctrl-actions">
                            <a-button size="small" @click="resetPreset">重置</a-button>
                            <a-button size="small" @click="applyPreset('soft')">柔和</a-button>
                            <a-button size="small" @click="applyPreset('strong')">强磨砂</a-button>
                        </a-space>
                    </a-card>

                    <a-card title="生成的令牌（阶段二消费）" :bordered="true" class="ctrl-card token-card">
                        <pre class="token-dump">{{ tokenDump }}</pre>
                        <span class="lab-tip">阶段二把这三枚写进 --app-glass-* 令牌，全局皮肤（.arco-card / .arco-modal / 浮层）统一读取。</span>
                    </a-card>
                </a-col>

                <!-- 右：实时预览 -->
                <a-col :span="16">
                    <div class="preview" :class="[`bg-${bgKind}`, `tint-${tint}`]" :style="previewVars">
                        <!-- 样例卡片 -->
                        <div class="glass-surface sample-card">
                            <div class="sc-title">表面磨砂卡片</div>
                            <p class="sc-body">
                                backdrop-filter 会模糊它<strong>底下</strong>的背景，所以请左右滚动预览区、或切换背景图，
                                观察模糊与半透明叠加后的可读性。文字对比是最重要的验收点。
                            </p>
                            <a-space>
                                <a-button type="primary" size="small">主操作</a-button>
                                <a-button size="small">次操作</a-button>
                            </a-space>
                        </div>

                        <!-- 样例浮层 -->
                        <div class="glass-surface sample-popover">
                            <div class="sp-item">浮层菜单项一</div>
                            <div class="sp-item">浮层菜单项二</div>
                            <div class="sp-item">浮层菜单项三</div>
                        </div>

                        <!-- 滚动内容，供磨砂表面叠加观察 -->
                        <div class="scroll-strip">
                            <div class="strip-block" v-for="n in 8" :key="n">区块 {{ n }}</div>
                        </div>

                        <a-button class="open-modal-btn" type="outline" size="small" @click="modalVisible = true">
                            预览磨砂弹窗
                        </a-button>
                    </div>
                </a-col>
            </a-row>

            <!-- 弹窗样例：teleport 到 body，用本页局部类单独套磨砂，不污染全局 .arco-modal -->
            <a-modal v-model:visible="modalVisible" title="磨砂弹窗预览" :width="420" wrap-class-name="glass-lab-modal"
                :esc-to-close="true">
                <p>这是实验台内的弹窗样例。真实铺全局时，弹窗是 teleport 到 body 的浮层，
                    磨砂需在非 scoped 的全局皮肤里对 .arco-modal 生效——这里先用局部类模拟观感。</p>
                <template #footer>
                    <a-button @click="modalVisible = false">关闭</a-button>
                </template>
            </a-modal>
        </div>
    </layout-body>
</template>

<script lang="ts">
export default {
    name: "demo-glass",
};
</script>
<script lang="ts" setup>
import { computed, ref } from "vue";

/** 底透明度（磨砂表面自身底色不透明度，0 全透 → 1 实色） */
const alpha = ref(0.6);
/** backdrop-filter 模糊半径 px */
const blur = ref(16);
/** 描边不透明度 */
const border = ref(0.35);
/** 预览区背景：网格/照片/纯色，用来观察磨砂在背后的模糊表现 */
const bgKind = ref<"mesh" | "photo" | "plain">("mesh");
/** 预览区明暗基调：浅底用白磨砂、深底用黑磨砂 */
const tint = ref<"light" | "dark">("light");
const modalVisible = ref(false);

const presets = {
    soft: { alpha: 0.72, blur: 10, border: 0.25 },
    strong: { alpha: 0.45, blur: 28, border: 0.5 },
};

function applyPreset(name: keyof typeof presets) {
    const p = presets[name];
    alpha.value = p.alpha;
    blur.value = p.blur;
    border.value = p.border;
}
function resetPreset() {
    alpha.value = 0.6;
    blur.value = 16;
    border.value = 0.35;
}

/** 把参数投射为预览区局部 CSS 变量（只作用于本页 .glass-surface，不进全局令牌） */
const previewVars = computed(() => ({
    "--g-alpha": String(alpha.value),
    "--g-blur": `${blur.value}px`,
    "--g-border": String(border.value),
}));

/** 导出给阶段二的令牌草案（surface 模式下 theme.ts 会改写这三枚） */
const tokenDump = computed(() => {
    const fg = tint.value === "dark" ? `rgba(24, 26, 32, ${alpha.value.toFixed(2)})`
        : `rgba(255, 255, 255, ${alpha.value.toFixed(2)})`;
    return [
        `--app-glass-alpha: ${alpha.value.toFixed(2)};`,
        `--app-glass-blur: ${blur.value}px;`,
        `--app-glass-border: rgba(255, 255, 255, ${border.value.toFixed(2)});`,
        `/* 当前表面底色的实际取值 */`,
        `background: ${fg};`,
        `backdrop-filter: blur(${blur.value}px) saturate(1.4);`,
    ].join("\n");
});
</script>

<style scoped>
.glass-lab {
    padding: 4px;
}

.lab-warn {
    margin-bottom: 12px;
}

.ctrl-card {
    margin-bottom: 12px;
}

.ctrl-row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin: 8px 0 2px;
}

.ctrl-label {
    color: var(--color-text-1);
    font-size: var(--base-size-default);
}

.ctrl-val {
    color: rgb(var(--primary-6));
    font-variant-numeric: tabular-nums;
    font-size: var(--base-size-small);
}

.ctrl-actions {
    margin-top: 12px;
}

.token-card pre {
    margin: 0;
    padding: 10px;
    border-radius: var(--base-radius-default);
    background: var(--color-fill-2);
    color: var(--color-text-1);
    font-family: Consolas, Monaco, "Courier New", monospace;
    font-size: var(--base-size-small);
    line-height: 1.7;
    white-space: pre-wrap;
    word-break: break-all;
}

.lab-tip {
    display: block;
    margin-top: 8px;
    color: var(--color-text-3);
    font-size: var(--base-size-small);
    line-height: 1.6;
}

/* 预览区：一套彩色/照片背景铺底，上面叠磨砂表面 */
.preview {
    position: relative;
    min-height: 560px;
    padding: 24px;
    border-radius: var(--base-radius-large);
    overflow: hidden;
    border: 1px solid var(--color-border-2);
}

.preview.bg-mesh {
    background:
        radial-gradient(circle at 20% 20%, #ff6b6b, transparent 45%),
        radial-gradient(circle at 80% 30%, #4ecdc4, transparent 45%),
        radial-gradient(circle at 40% 80%, #5470c6, transparent 45%),
        radial-gradient(circle at 90% 85%, #f7b32b, transparent 40%),
        linear-gradient(135deg, #a8edea, #fed6e3);
}

.preview.bg-photo {
    background: linear-gradient(160deg, #3a1c71, #d76d77, #ffaf7b);
}

.preview.bg-plain {
    background: linear-gradient(160deg, #cfd9df, #e2ebf0);
}

.preview.tint-dark {
    filter: none;
}

/* 磨砂表面：本页局部变量驱动，绝不写全局 */
.glass-surface {
    border-radius: var(--base-radius-large);
    border: 1px solid rgba(255, 255, 255, var(--g-border));
    backdrop-filter: blur(var(--g-blur)) saturate(1.4);
    -webkit-backdrop-filter: blur(var(--g-blur)) saturate(1.4);
    box-shadow: 0 8px 32px rgba(0, 0, 0, 0.18);
}

.tint-light .glass-surface {
    background: rgba(255, 255, 255, var(--g-alpha));
    color: #1d2129;
}

.tint-dark .glass-surface {
    background: rgba(24, 26, 32, var(--g-alpha));
    color: #f2f3f5;
}

.sample-card {
    max-width: 420px;
    padding: 20px;
}

.sc-title {
    font-weight: 600;
    font-size: var(--base-size-large);
    margin-bottom: 8px;
}

.sc-body {
    line-height: 1.8;
    margin-bottom: 12px;
    opacity: 0.92;
}

.sample-popover {
    position: absolute;
    right: 40px;
    top: 60px;
    width: 180px;
    padding: 8px;
}

.sp-item {
    padding: 10px 12px;
    border-radius: var(--base-radius-default);
    font-size: var(--base-size-default);
}

.sp-item:hover {
    background: rgba(128, 128, 128, 0.18);
}

.scroll-strip {
    display: flex;
    gap: 12px;
    margin-top: 24px;
    overflow-x: auto;
    padding-bottom: 12px;
}

.strip-block {
    flex: 0 0 160px;
    height: 120px;
    display: flex;
    align-items: center;
    justify-content: center;
    border-radius: var(--base-radius-default);
    background: rgba(128, 128, 128, 0.22);
    color: #fff;
    font-size: var(--base-size-default);
    text-shadow: 0 1px 2px rgba(0, 0, 0, 0.4);
}

.open-modal-btn {
    margin-top: 16px;
}
</style>

<style>
/* 弹窗 teleport 到 body，scoped 触不到，故用全局非 scoped 块 + 专属 wrap-class 限定，
   只给实验台弹窗套磨砂，不影响其它页面的 .arco-modal。 */
.glass-lab-modal .arco-modal {
    background: rgba(255, 255, 255, 0.55);
    backdrop-filter: blur(20px) saturate(1.5);
    -webkit-backdrop-filter: blur(20px) saturate(1.5);
    border: 1px solid rgba(255, 255, 255, 0.4);
}

body[arco-theme="dark"] .glass-lab-modal .arco-modal {
    background: rgba(28, 30, 38, 0.6);
    border-color: rgba(255, 255, 255, 0.12);
}
</style>
