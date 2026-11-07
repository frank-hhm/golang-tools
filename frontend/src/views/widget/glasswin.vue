<template>
    <!--
        桌面模糊材质 A/B 调试窗（专用隔离窗，由后端 OpenGlassDebugWindow(mode) 开）。
        每种材质各开一个独立窗口，可同时打开并排对比：同叠加层、比材质。
          - transparent：逐像素透明，桌面从页面未着色处透出、但不模糊（backdrop-filter 只糊页内）。
          - acrylic/mica/tabbed：BackgroundTypeTranslucent + DWM 系统 backdrop，糊的是真实桌面。
        本窗叠加层（色调/不透明度/描边/圆角）实时导出 --app-glass-* 取值供回报；参数经
        localStorage 跨窗共享，于是并排的几扇窗比的是材质本身、不是叠加差异。
        诚实说明：低于 Win11 22621 时系统 backdrop 不可用，Wails 自动回退旧式模糊（仍糊桌面、无噪点/调色）。
    -->
    <div class="gw-wrap">
        <div class="gw-card glass-surface">
            <div class="gw-bar">
                <span class="gw-title">透明调试窗 · {{ modeLabel }}</span>
                <button class="gw-close" title="关闭" @click="closeWin">
                    <icon-close />
                </button>
            </div>

            <div class="gw-body">
                <p v-if="isTransparent" class="gw-note">
                    逐像素透明：卡片之外直接透出<strong>未模糊</strong>的桌面，用来判断叠加层透不透。
                    下方「模糊半径」只作用于右侧那块页内彩色底图——浏览器糊不了真实桌面，只能糊页内内容。
                </p>
                <p v-else class="gw-note">
                    {{ modeLabel }}：整扇窗的背景就是<strong>系统级桌面模糊</strong>（DWM backdrop），卡片是浮在其上的一层叠加。
                    卡片之外透出的就是被模糊的真实桌面，直接看材质观感。
                </p>

                <div v-if="showFallbackWarn" class="gw-warn">
                    本机低于 Win11 22621，此项由系统回退为旧式模糊（仍糊桌面，但无噪点 / 调色），观感与真机 Win11 有差。
                </div>

                <div class="gw-grid">
                    <div class="gw-ctrl">
                        <div class="row"><span class="lab">叠加色调</span></div>
                        <a-color-picker v-model="tintColor" :show-alpha="false" size="small" />

                        <div class="row"><span class="lab">叠加不透明度</span><span class="val">{{ pct(alpha) }}</span></div>
                        <a-slider v-model="alpha" :min="0" :max="1" :step="0.02" />

                        <div class="row"><span class="lab">描边强度</span><span class="val">{{ pct(border) }}</span></div>
                        <a-slider v-model="border" :min="0" :max="1" :step="0.02" />

                        <div class="row"><span class="lab">噪点强度</span><span class="val">{{ Math.round(noise * 1000) / 10 }}%</span></div>
                        <a-slider v-model="noise" :min="0" :max="0.12" :step="0.005" />

                        <div class="row"><span class="lab">圆角</span></div>
                        <a-radio-group v-model="radius" type="button" size="small">
                            <a-radio value="none">直角</a-radio>
                            <a-radio value="small">小</a-radio>
                            <a-radio value="default">标准</a-radio>
                            <a-radio value="large">大</a-radio>
                        </a-radio-group>

                        <template v-if="isTransparent">
                            <div class="row"><span class="lab">模糊半径（仅页内）</span><span class="val">{{ blur }}px</span></div>
                            <a-slider v-model="blur" :min="0" :max="40" :step="1" />
                        </template>
                    </div>

                    <div v-if="isTransparent" class="gw-observe">
                        <div class="canvas">
                            <div class="blur-chip glass-surface">磨砂浮层<br />（看它背后的彩色被模糊）</div>
                        </div>
                        <span class="cap">页内彩色底图：观察模糊半径用</span>
                    </div>
                </div>

                <div class="gw-samples">
                    <div class="sm-title">样例表面（当前叠加材质 · 背景为真实桌面模糊）</div>
                    <div class="sm-row">
                        <div class="glass-surface sm-card">
                            <div class="sm-card-title">卡片</div>
                            <p class="sm-card-body">文字可读性验收：桌面被 DWM 模糊，卡片只是浮在其上的一层叠加。</p>
                            <a-space>
                                <a-button size="small" type="primary">主操作</a-button>
                                <a-button size="small">次操作</a-button>
                            </a-space>
                        </div>
                        <div class="glass-surface sm-menu">
                            <div class="sm-item">浮层菜单项一</div>
                            <div class="sm-item">浮层菜单项二</div>
                            <div class="sm-item">浮层菜单项三</div>
                        </div>
                    </div>
                </div>

                <pre class="gw-dump">{{ dump }}</pre>
                <a-space class="gw-actions">
                    <a-button size="small" type="primary" @click="copy">复制参数</a-button>
                </a-space>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { Window } from "@wailsio/runtime";
import { useCaps } from "@/utils/caps";

type GlassMode = "transparent" | "acrylic" | "mica" | "tabbed";

const MODE_LABEL: Record<GlassMode, string> = {
    transparent: "逐像素透明",
    acrylic: "Acrylic 亚克力",
    mica: "Mica 云母",
    tabbed: "Tabbed 自动",
};
const RADIUS_MAP: Record<string, string> = {
    none: "0px",
    small: "6px",
    default: "12px",
    large: "20px",
};

/** 从 URL 片段里的 ?mode= 解析当前窗材质（后端建窗时带上） */
function parseMode(): GlassMode {
    const m = /mode=([a-z]+)/i.exec(window.location.hash);
    const v = (m?.[1] || "").toLowerCase();
    return v === "acrylic" || v === "mica" || v === "tabbed" ? (v as GlassMode) : "transparent";
}

const mode = ref<GlassMode>(parseMode());
const modeLabel = computed(() => MODE_LABEL[mode.value]);
const isTransparent = computed(() => mode.value === "transparent");
/** 非透明材质 + 本机不支持系统 backdrop → 提示回退 */
const showFallbackWarn = computed(() => !isTransparent.value && !useCaps().systemBackdrop);

/** 叠加层基色 */
const tintColor = ref("#ffffff");
/** 叠加层不透明度（0 全透 → 1 实色） */
const alpha = ref(0.5);
/** 描边不透明度 */
const border = ref(0.35);
/** 圆角档 */
const radius = ref<"none" | "small" | "default" | "large">("default");
/** backdrop-filter 模糊半径 px（只作用于页内内容，仅 transparent 模式有意义） */
const blur = ref(16);
/** 噪点不透明度（叠加层的亚克力颗粒，0 无 → 0.12 明显） */
const noise = ref(0.04);

const pct = (v: number) => `${Math.round(v * 100)}%`;

/** #rgb/#rrggbb → rgba(r,g,b,a) */
function hexToRgba(hex: string, a: number): string {
    const h = (hex || "").replace("#", "");
    const n = h.length === 3 ? h.split("").map((c) => c + c).join("") : h.padEnd(6, "0").slice(0, 6);
    const r = parseInt(n.slice(0, 2), 16) || 0;
    const g = parseInt(n.slice(2, 4), 16) || 0;
    const b = parseInt(n.slice(4, 6), 16) || 0;
    return `rgba(${r}, ${g}, ${b}, ${a.toFixed(2)})`;
}

/** 供 CSS v-bind 用的字符串（避免在 style 里写模板字面量） */
const surfaceBg = computed(() => hexToRgba(tintColor.value, alpha.value));
const borderStr = computed(() => `rgba(255, 255, 255, ${border.value.toFixed(2)})`);
const radiusPx = computed(() => RADIUS_MAP[radius.value] || "12px");
const blurPx = computed(() => `${blur.value}px`);
const noiseOpacity = computed(() => String(noise.value));

/** 导出给回报的令牌草案 */
const dump = computed(() => {
    const lines = [
        `/* 桌面模糊材质 A/B：mode=${mode.value} */`,
        `--app-glass-mode: ${mode.value};`,
        `--app-glass-tint: ${tintColor.value};`,
        `--app-glass-alpha: ${alpha.value.toFixed(2)};`,
        `--app-glass-border: ${borderStr.value};`,
        `--app-glass-radius: ${radiusPx.value};`,
        `--app-glass-noise: ${noise.value.toFixed(3)};`,
    ];
    if (isTransparent.value) {
        lines.push(`--app-glass-blur: ${blur.value}px;`);
        lines.push(`/* 表面实际取值（逐像素：backdrop-filter 只糊页内） */`);
        lines.push(`background: ${surfaceBg.value};`);
        lines.push(`backdrop-filter: blur(${blur.value}px) saturate(1.4);`);
    } else {
        lines.push(`/* 表面实际取值（桌面模糊由 DWM backdrop 提供，卡片只叠一层 tint） */`);
        lines.push(`background: ${surfaceBg.value};`);
    }
    return lines.join("\n");
});

const STORE_KEY = "app-glass-debug";
function persist() {
    try {
        localStorage.setItem(
            STORE_KEY,
            JSON.stringify({
                tintColor: tintColor.value,
                alpha: alpha.value,
                blur: blur.value,
                border: border.value,
                radius: radius.value,
                noise: noise.value,
            }),
        );
    } catch {
        /* 忽略存储异常 */
    }
}
watch([tintColor, alpha, blur, border, radius, noise], persist);

onMounted(() => {
    try {
        const raw = localStorage.getItem(STORE_KEY);
        if (raw) {
            const p = JSON.parse(raw);
            if (typeof p.tintColor === "string") tintColor.value = p.tintColor;
            if (typeof p.alpha === "number") alpha.value = p.alpha;
            if (typeof p.blur === "number") blur.value = p.blur;
            if (typeof p.border === "number") border.value = p.border;
            if (p.radius in RADIUS_MAP) radius.value = p.radius;
            if (typeof p.noise === "number") noise.value = p.noise;
        }
    } catch {
        /* 忽略非法缓存 */
    }
    persist();
});

async function copy() {
    try {
        await navigator.clipboard.writeText(dump.value);
    } catch {
        /* 剪贴板不可用时忽略 */
    }
}

function closeWin() {
    try {
        Window.Close();
    } catch {
        /* 非桌面环境没有原生 Window */
    }
}
</script>

<style scoped>
/* 根容器铺满视口且完全透明：transparent 透出桌面、translucent 透出 DWM 模糊背景 */
.gw-wrap {
    width: 100vw;
    height: 100vh;
    box-sizing: border-box;
    padding: 48px;
    display: flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    font-family: "Microsoft YaHei", "Segoe UI", sans-serif;
}

/* 叠加表面：本页局部变量驱动 */
.glass-surface {
    position: relative;
    border-radius: v-bind(radiusPx);
    border: 1px solid v-bind(borderStr);
    background: v-bind(surfaceBg);
    box-shadow: 0 10px 40px rgba(0, 0, 0, 0.22);
    color: rgb(29, 33, 41);
}

/* 亚克力颗粒：叠一层灰度噪点，overlay 混合，只盖表面不拦交互 */
.glass-surface::after {
    content: "";
    position: absolute;
    inset: 0;
    border-radius: inherit;
    pointer-events: none;
    opacity: v-bind(noiseOpacity);
    mix-blend-mode: overlay;
    background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='140' height='140'%3E%3Cfilter id='n'%3E%3CfeTurbulence type='fractalNoise' baseFrequency='0.8' numOctaves='2' stitchTiles='stitch'/%3E%3CfeColorMatrix type='saturate' values='0'/%3E%3C/filter%3E%3Crect width='100%25' height='100%25' filter='url(%23n)'/%3E%3C/svg%3E");
}

.gw-card {
    width: 720px;
    max-width: 100%;
    overflow: hidden;
}

/* 自绘头部：整条可拖，关闭按钮 no-drag */
.gw-bar {
    height: 40px;
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 0 8px 0 14px;
    -webkit-app-region: drag;
    app-region: drag;
    border-bottom: 1px solid rgba(128, 128, 128, 0.18);
}

.gw-title {
    font-weight: 600;
    font-size: var(--base-size-default);
}

.gw-close {
    -webkit-app-region: no-drag;
    app-region: no-drag;
    width: 30px;
    height: 30px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    border: none;
    border-radius: var(--base-radius-default);
    background: transparent;
    color: inherit;
    cursor: pointer;
    font-size: var(--base-size-large);
}

.gw-close:hover {
    background: rgba(227, 93, 91, 0.85);
    color: #fff;
}

.gw-body {
    padding: 16px 18px 18px;
}

.gw-note {
    margin: 0 0 12px;
    line-height: 1.7;
    font-size: var(--base-size-small);
    opacity: 0.9;
}

.gw-warn {
    margin: 0 0 12px;
    padding: 8px 10px;
    border-radius: var(--base-radius-default);
    background: rgba(255, 186, 20, 0.18);
    border: 1px solid rgba(255, 186, 20, 0.5);
    color: rgb(120, 82, 0);
    line-height: 1.6;
    font-size: var(--base-size-small);
}

.gw-grid {
    display: flex;
    gap: 18px;
    align-items: stretch;
}

.gw-ctrl {
    flex: 1 1 0;
    min-width: 0;
}

.gw-ctrl .row {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin: 8px 0 2px;
    font-size: var(--base-size-default);
}

.gw-ctrl .lab {
    opacity: 0.9;
}

.gw-ctrl .val {
    color: rgb(var(--primary-6));
    font-variant-numeric: tabular-nums;
    font-size: var(--base-size-small);
}

.gw-observe {
    flex: 0 0 260px;
    display: flex;
    flex-direction: column;
    gap: 6px;
}

.canvas {
    position: relative;
    flex: 1;
    border-radius: var(--base-radius-default);
    overflow: hidden;
    background:
        radial-gradient(circle at 22% 24%, #ff6b6b, transparent 42%),
        radial-gradient(circle at 78% 30%, #4ecdc4, transparent 44%),
        radial-gradient(circle at 40% 82%, #5470c6, transparent 46%),
        radial-gradient(circle at 88% 84%, #f7b32b, transparent 40%),
        linear-gradient(135deg, #a8edea, #fed6e3);
}

.blur-chip {
    position: absolute;
    left: 24px;
    bottom: 24px;
    width: 150px;
    padding: 12px;
    text-align: center;
    font-size: var(--base-size-small);
    line-height: 1.6;
}

.gw-observe .cap {
    font-size: var(--base-size-small);
    opacity: 0.75;
}

.gw-dump {
    margin: 14px 0 0;
    padding: 10px;
    border-radius: var(--base-radius-default);
    background: rgba(128, 128, 128, 0.16);
    font-family: var(--code-font-family);
    font-size: var(--base-size-small);
    line-height: 1.7;
    white-space: pre-wrap;
    word-break: break-all;
}

.gw-actions {
    margin-top: 12px;
}

/* 样例表面：卡片 + 浮层菜单，均复用 .glass-surface 材质，叠在真桌面模糊上 */
.gw-samples {
    margin-top: 16px;
}

.sm-title {
    font-size: var(--base-size-small);
    opacity: 0.8;
    margin-bottom: 8px;
}

.sm-row {
    display: flex;
    gap: 12px;
    align-items: stretch;
}

.sm-card {
    flex: 1 1 0;
    min-width: 0;
    padding: 14px;
}

.sm-card-title {
    font-weight: 600;
    margin-bottom: 6px;
}

.sm-card-body {
    margin: 0 0 10px;
    line-height: 1.7;
    font-size: var(--base-size-small);
    opacity: 0.9;
}

.sm-menu {
    flex: 0 0 160px;
    padding: 8px;
}

.sm-item {
    padding: 8px 10px;
    border-radius: var(--base-radius-default);
    font-size: var(--base-size-small);
}

.sm-item:hover {
    background: rgba(128, 128, 128, 0.18);
}
</style>
