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
                本页是<strong>隔离实验台</strong>：调参只作用于下方预览区，不写全局令牌、不改主窗。
                本页预览用 <strong>CSS 表面磨砂 / 仿 Acrylic</strong>（假背景）；选「背景」里的<strong>原生 Acrylic/Mica/Tabbed</strong> 会另开独立调试窗，
                那里才是真·DWM 桌面模糊。观感确认后再另开计划铺全局。
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
                            <span class="ctrl-label">投影强度</span>
                            <span class="ctrl-val">{{ Math.round(shadow * 100) }}%</span>
                        </div>
                        <a-slider v-model="shadow" :min="0" :max="0.5" :step="0.02" />

                        <div class="ctrl-row">
                            <span class="ctrl-label">材质</span>
                        </div>
                        <a-radio-group v-model="material" type="button" size="small">
                            <a-radio value="frosted">普通磨砂</a-radio>
                            <a-radio value="acrylic">Acrylic 仿</a-radio>
                        </a-radio-group>
                        <template v-if="material === 'acrylic'">
                            <div class="ctrl-row">
                                <span class="ctrl-label">噪点强度</span>
                                <span class="ctrl-val">{{ Math.round(noise * 1000) / 10 }}%</span>
                            </div>
                            <a-slider v-model="noise" :min="0" :max="0.12" :step="0.005" />
                        </template>

                        <div class="ctrl-row">
                            <span class="ctrl-label">背景 / 预览载体</span>
                        </div>
                        <a-radio-group v-model="bgKind" type="button" size="small">
                            <a-radio value="mesh">彩色网格</a-radio>
                            <a-radio value="photo">风景照</a-radio>
                            <a-radio value="plain">纯色</a-radio>
                            <a-radio value="dark">暗夜纹理</a-radio>
                            <a-radio value="theme">跟随主题背景</a-radio>
                            <template v-if="canNativeWin">
                                <a-radio value="acrylic">原生Acrylic</a-radio>
                                <a-radio value="mica">原生Mica</a-radio>
                                <a-radio value="tabbed">原生Tabbed</a-radio>
                                <a-radio value="transparent">逐像素透明</a-radio>
                            </template>
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
                            <a-button type="primary" size="small" @click="applyAsGlobalTheme">应用为全局主题</a-button>
                            <a-button v-if="canNativeWin" size="small" @click="readDebugParams">读取调试窗参数</a-button>
                        </a-space>
                        <span class="lab-tip">「应用为全局主题」把当前材质参数 + 当前背景档写进全局主题（设置-主题可继续微调、自动保存），全站卡片/弹窗/抽屉即时生效。</span>
                        <span v-if="canNativeWin" class="lab-tip">选「背景」里的原生材质即开一扇独立调试窗（真·DWM 桌面模糊），可同时开多扇并排对比；调好后回本页“读取调试窗参数”核对。</span>
                        <span v-if="canNativeWin && !hasSystemBackdrop" class="lab-tip lab-warn">Acrylic/Mica/Tabbed 需 Win11 22621+，本机原生窗将回退为系统旧式模糊（仍糊桌面、无噪点 / 调色）。</span>
                    </a-card>

                    <a-card title="生成的令牌（阶段二消费）" :bordered="true" class="ctrl-card token-card">
                        <pre class="token-dump">{{ tokenDump }}</pre>
                        <span class="lab-tip">阶段二把这三枚写进 --app-glass-* 令牌，全局皮肤（.arco-card / .arco-modal / 浮层）统一读取。</span>
                    </a-card>
                </a-col>

                <!-- 右：实时预览 -->
                <a-col :span="16">
                    <div class="preview" :class="[isNativeBg ? 'bg-native' : `bg-${bgKind}`, `tint-${tint}`, `mat-${material}`]" :style="previewVars">
                        <template v-if="isNativeBg">
                            <div class="glass-surface native-hint">
                                <div class="nh-title">已打开「{{ NATIVE_LABEL[bgKind] }}」原生调试窗</div>
                                <p class="nh-body">
                                    真·DWM 桌面模糊是<strong>整窗口级</strong>属性，只能开在独立原生窗里，无法嵌进本页这块预览区。
                                    请切到那扇窗查看真实桌面模糊 + 当前叠加材质（色调 / 不透明度 / 描边 / 圆角 / 噪点）。
                                </p>
                                <p v-if="bgKind !== 'transparent' && !hasSystemBackdrop" class="nh-warn">
                                    本机低于 Win11 22621，原生窗将回退为系统旧式模糊（仍糊桌面、无噪点 / 调色）。
                                </p>
                                <a-button size="small" type="outline" @click="openDebugWin(bgKind)">重新打开该窗</a-button>
                            </div>
                        </template>
                        <template v-else>
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
                        </template>
                    </div>
                </a-col>
            </a-row>

            <!-- 弹窗样例：teleport 到 body，用本页局部类单独套磨砂，不污染全局 .arco-modal -->
            <a-modal v-model:visible="modalVisible" title="磨砂弹窗预览" :width="420"
                :wrap-class-name="material === 'acrylic' ? 'glass-lab-modal mat-acrylic' : 'glass-lab-modal'"
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
import { computed, getCurrentInstance, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { useCaps } from "@/utils/caps";
import { applyThemeConfig, currentThemeConfig, type BackgroundType } from "@/utils/theme";

const { proxy: { $GoTools } } = getCurrentInstance() as any;
/** 有原生窗口能力才显示“开调试窗”（Web/Linux 隐藏）；走响应式 caps，加载完自动显影 */
const canNativeWin = computed(() => useCaps().nativeWindows);
/** 能否用 Win11 系统级 backdrop；false 时非透明材质会回退旧式模糊，按钮标“回退” */
const hasSystemBackdrop = computed(() => useCaps().systemBackdrop);

function openDebugWin(mode: string) {
    void $GoTools?.OpenGlassDebugWindow?.(900, 640, mode);
}
function readDebugParams() {
    try {
        const raw = localStorage.getItem("app-glass-debug");
        if (!raw) return;
        const p = JSON.parse(raw);
        if (typeof p.alpha === "number") alpha.value = p.alpha;
        if (typeof p.blur === "number") blur.value = p.blur;
        if (typeof p.border === "number") border.value = p.border;
    } catch {
        /* 忽略非法缓存 */
    }
}

/** 底透明度（磨砂表面自身底色不透明度，0 全透 → 1 实色） */
const alpha = ref(0.6);
/** backdrop-filter 模糊半径 px */
const blur = ref(0);
/** 描边不透明度 */
const border = ref(0);
/** 投影强度（表面 box-shadow 的不透明度，0 无 → 0.5 浓） */
const shadow = ref(0.18);
/** 背景 / 预览载体：mesh/photo/plain 走本页 CSS；acrylic/mica/tabbed/transparent 开原生窗（真 DWM） */
type BgKind = "mesh" | "photo" | "plain" | "dark" | "theme" | "acrylic" | "mica" | "tabbed" | "transparent";
const NATIVE_LABEL: Record<string, string> = {
    acrylic: "原生 Acrylic",
    mica: "原生 Mica",
    tabbed: "原生 Tabbed",
    transparent: "逐像素透明",
};
const bgKind = ref<BgKind>("dark");
const isNativeBg = computed(() => bgKind.value in NATIVE_LABEL);
watch(bgKind, (v) => {
    if (v in NATIVE_LABEL) openDebugWin(v);
});
/** 预览区明暗基调：浅底用白磨砂、深底用黑磨砂 */
const tint = ref<"light" | "dark">("light");
const modalVisible = ref(false);
/** 材质：普通磨砂（只 blur+tint）还是 CSS 仿 Acrylic（再叠一层灰度噪点） */
const material = ref<"frosted" | "acrylic">("acrylic");
/** 噪点不透明度（Acrylic 的标志性颗粒，0 无 → 0.12 明显） */
const noise = ref(0.12);

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
    blur.value = 0;
    border.value = 0;
    shadow.value = 0.18;
    material.value = "acrylic";
    noise.value = 0.12;
    bgKind.value = "dark";
    tint.value = "light";
}

/** CSS 假背景档→全局背景主题档的映射（原生档不在此列，不改动全局背景） */
const BG_TO_TYPE: Partial<Record<BgKind, BackgroundType>> = {
    mesh: "mesh",
    photo: "photo",
    plain: "plain",
    dark: "dark",
};
/** 把当前调参 + 当前背景档写进全局主题（localStorage + 后端配置），全站即时生效 */
function applyAsGlobalTheme() {
    const cfg = currentThemeConfig();
    cfg.glass = {
        on: true,
        material: material.value,
        alpha: Math.round(alpha.value * 100),
        blur: Math.round(blur.value),
        border: Math.round(border.value * 100),
        shadow: Math.round(shadow.value * 100),
        noise: Math.round(noise.value * 1000),
    };
    const mapped = BG_TO_TYPE[bgKind.value];
    if (mapped) cfg.background = { type: mapped, image: "" };
    applyThemeConfig(cfg);
    Message.success("已套用为全局主题，可在「设置-主题」继续微调");
}

/** 把参数投射为预览区局部 CSS 变量（只作用于本页 .glass-surface，不进全局令牌） */
const previewVars = computed(() => ({
    "--g-alpha": String(alpha.value),
    "--g-blur": `${blur.value}px`,
    "--g-border": String(border.value),
    "--g-noise": String(noise.value),
    "--g-shadow": String(shadow.value),
}));

/** 导出给阶段二的令牌草案（surface 模式下 theme.ts 会改写这三枚） */
const tokenDump = computed(() => {
    const fg = tint.value === "dark" ? `rgba(24, 26, 32, ${alpha.value.toFixed(2)})`
        : `rgba(255, 255, 255, ${alpha.value.toFixed(2)})`;
    const lines = [
        `--app-glass-material: ${material.value};`,
        `--app-glass-alpha: ${alpha.value.toFixed(2)};`,
        `--app-glass-blur: ${blur.value}px;`,
        `--app-glass-border: rgba(255, 255, 255, ${border.value.toFixed(2)});`,
        `--app-glass-shadow: ${shadow.value.toFixed(2)};`,
    ];
    if (material.value === "acrylic") {
        lines.push(`--app-glass-noise: ${noise.value.toFixed(3)};`);
    }
    lines.push(`/* 当前表面底色的实际取值 */`);
    lines.push(`background: ${fg};`);
    lines.push(`backdrop-filter: blur(${blur.value}px) saturate(1.4);`);
    lines.push(`box-shadow: 0 8px 32px rgba(0, 0, 0, ${shadow.value.toFixed(2)});`);
    if (material.value === "acrylic") {
        lines.push(`/* Acrylic 仿：再叠一层灰度噪点（::after，mix-blend-mode: overlay），opacity: ${noise.value.toFixed(3)} */`);
    }
    return lines.join("\n");
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

.lab-warn {
    color: rgb(var(--warning-6));
}

/* 回退角标：材质在本机不支持时挂在按钮文字右上角 */
.fb-badge {
    margin-left: 4px;
    padding: 0 4px;
    border-radius: 8px;
    background: rgba(var(--warning-6), 0.16);
    color: rgb(var(--warning-6));
    font-size: 10px;
    line-height: 16px;
    vertical-align: middle;
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

/* 跟随主题背景：预览区自身不画底，直接透出全局 .theme-bg（未设全局背景时就是普通页面） */
.preview.bg-theme {
    background: transparent;
}

/* 暗夜纹理：跟随本页「预览底色」浅/深切换——浅色给近白纹理，深色给暗色纹理 */
.preview.tint-light.bg-dark {
    background:
        repeating-linear-gradient(45deg, rgba(0, 0, 0, 0.05) 0 2px, transparent 2px 7px),
        linear-gradient(160deg, #f5f7fa, #dfe4ec);
}

.preview.tint-dark.bg-dark {
    background:
        repeating-linear-gradient(45deg, rgba(255, 255, 255, 0.03) 0 2px, transparent 2px 7px),
        linear-gradient(160deg, #2a2d34, #16181d);
}

.preview.tint-dark {
    filter: none;
}

/* 磨砂表面：本页局部变量驱动，绝不写全局 */
.glass-surface {
    position: relative;
    border-radius: var(--base-radius-large);
    border: 1px solid rgba(255, 255, 255, var(--g-border));
    backdrop-filter: blur(var(--g-blur)) saturate(1.4);
    -webkit-backdrop-filter: blur(var(--g-blur)) saturate(1.4);
    box-shadow: 0 8px 32px rgba(0, 0, 0, var(--g-shadow, 0.18));
}

/* Acrylic 仿：在表面叠一层灰度噪点（feTurbulence），overlay 混合，只盖表面不拦点击 */
.mat-acrylic .glass-surface::after {
    content: "";
    position: absolute;
    inset: 0;
    border-radius: inherit;
    pointer-events: none;
    opacity: var(--g-noise, 0.05);
    mix-blend-mode: overlay;
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='140' height='140'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='2' stitchTiles='stitch'/%3E%3CfeColorMatrix type='saturate' values='0'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E");
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

/* 原生背景态：预览区无法内嵌真桌面模糊，改显中性底 + 引导卡 */
.preview.bg-native {
    background: linear-gradient(160deg, #2b2f3a, #3a3f4d);
}

.native-hint {
    max-width: 520px;
    margin: 60px auto;
    padding: 24px;
}

.nh-title {
    font-weight: 600;
    font-size: var(--base-size-large);
    margin-bottom: 10px;
}

.nh-body {
    line-height: 1.8;
    margin-bottom: 10px;
    opacity: 0.92;
}

.nh-warn {
    color: rgb(var(--warning-6));
    line-height: 1.7;
    margin-bottom: 10px;
    font-size: var(--base-size-small);
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

/* Acrylic 仿：实验台弹窗也叠同层噪点（scoped 触不到 teleport 的 body，故写在全局块） */
.glass-lab-modal.mat-acrylic .arco-modal {
    position: relative;
}

.glass-lab-modal.mat-acrylic .arco-modal::after {
    content: "";
    position: absolute;
    inset: 0;
    border-radius: inherit;
    pointer-events: none;
    opacity: 0.05;
    mix-blend-mode: overlay;
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='140' height='140'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='2' stitchTiles='stitch'/%3E%3CfeColorMatrix type='saturate' values='0'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E");
}
</style>
