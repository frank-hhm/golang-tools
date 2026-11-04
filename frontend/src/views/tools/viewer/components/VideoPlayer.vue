<template>
    <div ref="root" class="vp" tabindex="0" :class="{ 'vp-blank': !showCtrl && playing }" @mousemove="wake"
        @mouseleave="onLeave" @keydown="onKey">
        <video ref="media" class="vp-video" :class="'fit-' + fit" :src="url" preload="metadata" playsinline
            @click="toggle" @dblclick="toggleFs" @play="onPlay" @pause="onPause" @ended="onPause"
            @loadedmetadata="onLoadedMeta" @durationchange="onLoadedMeta" @loadstart="onLoadStart"
            @progress="onProgress" @waiting="buffering = true" @playing="buffering = false"
            @canplay="buffering = false"></video>

        <!-- 缓冲中转圈 / 暂停时居中磨砂大播放键 -->
        <div class="vp-center">
            <div v-if="buffering" class="vp-spin"></div>
            <button v-else-if="!playing" class="vp-fab" aria-label="播放" @click="toggle">
                <icon-play-arrow-fill />
            </button>
        </div>

        <!-- 底部控制区（B 站式）：贴底全宽进度条 + 渐变条排控件，鼠标静止自动隐藏 -->
        <div class="vp-bar" @dblclick.stop>
            <!-- 悬停预览缩略图：第二个 video 只当取景器（静音不自动播），时间到点即截帧 -->
            <div v-if="preview && hoverRatio !== null && duration" ref="previewBox" class="vp-preview"
                :style="{ left: previewLeft }">
                <video ref="previewMedia" class="vp-preview-video" :src="url" preload="auto" muted playsinline></video>
                <span class="vp-preview-time">{{ fmtTime(hoverRatio * duration) }}</span>
            </div>

            <seek-bar class="vp-seek" preview :ratio="progressRatio" :buffered="bufRatio" :duration="duration" tip
                @update:ratio="seekRatio" @hover-ratio="onHoverRatio" />

            <div class="vp-ctrls">
                <button class="vp-btn" :title="playing ? '暂停(空格)' : '播放(空格)'" @click="toggle">
                    <icon-pause v-if="playing" />
                    <icon-play-arrow v-else />
                </button>
                <button class="vp-btn" title="快进 10s" @click="nudge(10)">
                    <icon-forward />
                </button>
                <span class="vp-time">{{ fmtTime(current) }} <i>/</i> {{ fmtTime(duration) }}</span>
                <div class="vp-spacer"></div>

                <a-popover v-model:popup-visible="speedOpen" trigger="click" position="top" :content-style="popStyle"
                    content-class="vp-pop">
                    <button class="vp-txt" :class="{ on: rate !== 1 }" title="倍速播放">{{ rateLabel(rate) }}</button>
                    <template #content>
                        <div class="vp-menu">
                            <div v-for="s in SPEEDS_DESC" :key="s" class="vp-menu-item" :class="{ on: s === rate }"
                                @click="pickSpeed(s)">{{ s }}x</div>
                        </div>
                    </template>
                </a-popover>

                <a-popover trigger="hover" position="top" content-class="vp-pop"
                    :content-style="{ padding: '14px 12px' }">
                    <button class="vp-btn" :title="muted ? '取消静音(M)' : '静音(M)'" @click="toggleMute">
                        <icon-mute v-if="muted || volume === 0" />
                        <icon-sound v-else />
                    </button>
                    <template #content>
                        <div class="vol-pop">
                            <span class="vol-pct">{{ volPct }}</span>
                            <seek-bar vertical :ratio="muted ? 0 : volume" @update:ratio="setVolume" />
                        </div>
                    </template>
                </a-popover>

                <button v-if="pipSupported" class="vp-txt" :class="{ on: pipOn }" title="画中画小窗(X)"
                    @click="togglePip">小窗</button>
                <button class="vp-txt" :title="'画面比例：' + fitName + '（点按切换）'" @click="cycleFit">{{ fitName }}</button>

                <button class="vp-btn" :title="isFs ? '退出全屏(F)' : '全屏(F)'" @click="toggleFs">
                    <icon-fullscreen-exit v-if="isFs" />
                    <icon-fullscreen v-else />
                </button>
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
/*
视频播放器（B 站式自定义控件）：替换原生 <video controls>。
- 控制区贴底：全宽进度条压在渐变遮罩上（B 站蓝配色），不再是悬浮胶囊；
- 进度条悬停出预览缩略图（隐藏的第二路 video 只当取景器截帧 + 时间角标）；
- 倍速/音量改深色弹层（样式在本组件 :global 覆盖，不依赖 Arco 暗色 popover）；
- 「小窗」是系统级画中画（WebView2/Chrome 支持，不支持时按钮隐藏）；
- 「比例」在 适应/填充/裁切 三种 object-fit 间循环，长视频看宽屏、竖屏视频不憋黑边。
播放中鼠标静止 2.8s 自动隐藏控制条、动一下即回；暂停时居中大播放键。
键盘：空格/K 播放暂停，←/→ ±5s，L 快进 10s，↑/↓ 音量，M 静音，F 全屏，X 画中画。
播放内核状态见 mediaCore.ts，进度/音量条见 SeekBar.vue。
*/
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from "vue";
import { fmtTime, rateLabel, SPEEDS, useMediaCore } from "./mediaCore";
import SeekBar from "./SeekBar.vue";

defineProps<{ url: string; name: string }>();

const media = ref<HTMLMediaElement | null>(null);
const root = ref<HTMLElement | null>(null);
const {
    playing, current, duration, bufferedEnd, volume, muted, rate,
    onPlay, onPause, onLoadedMeta, onLoadStart, onProgress,
    toggle, seekRatio, nudge, setVolume, toggleMute, setRate,
} = useMediaCore(media);

const progressRatio = computed(() => (duration.value ? current.value / duration.value : 0));
const bufRatio = computed(() => (duration.value ? Math.min(bufferedEnd.value / duration.value, 1) : 0));
const volPct = computed(() => Math.round((muted.value ? 0 : volume.value) * 100) + "%");
const buffering = ref(false);

// —— 倍速弹层（B 站式降序竖列）——
const SPEEDS_DESC = [...SPEEDS].sort((a, b) => b - a);
const speedOpen = ref(false);
const popStyle = { padding: "6px", borderRadius: "8px" };
function pickSpeed(s: number) {
    setRate(s);
    speedOpen.value = false;
}

// —— 画面比例（object-fit 三态循环）——
const FITS = [
    { fit: "contain", name: "适应" },
    { fit: "cover", name: "裁切" },
    { fit: "fill", name: "拉伸" },
] as const;
const fitIdx = ref(0);
const fit = computed(() => FITS[fitIdx.value].fit);
const fitName = computed(() => FITS[fitIdx.value].name);
function cycleFit() {
    fitIdx.value = (fitIdx.value + 1) % FITS.length;
}

// —— 悬停预览缩略图 ——
const preview = ref(false);
const hoverRatio = ref<number | null>(null);
const previewMedia = ref<HTMLVideoElement | null>(null);
const previewBox = ref<HTMLElement | null>(null);
const previewLeft = ref("0px");
let seekDebounce = 0;
function onHoverRatio(r: number | null) {
    hoverRatio.value = r;
    if (r === null || !previewMedia.value || !duration.value) return;
    const el = previewMedia.value;
    // 缩略图 seek 很贵：悬停停稳 120ms 后才跳一次
    if (seekDebounce) clearTimeout(seekDebounce);
    seekDebounce = window.setTimeout(() => {
        try {
            el.currentTime = Math.min(r * duration.value, Math.max(0, duration.value - 0.1));
        } catch { /* readyState 不够时赋值被忽略，下次悬停再试 */ }
    }, 120);
}
function placePreview() {
    const box = previewBox.value;
    const host = root.value;
    if (!box || !host) return;
    const half = box.offsetWidth / 2;
    const x = (hoverRatio.value || 0) * host.clientWidth;
    previewLeft.value = Math.max(half + 8, Math.min(host.clientWidth - half - 8, x)) + "px";
}

// —— 画中画 ——
const pipSupported = typeof document !== "undefined" && "pictureInPictureEnabled" in document;
const pipOn = ref(false);
async function togglePip() {
    const el = media.value as HTMLVideoElement | null;
    if (!el) return;
    try {
        if (document.pictureInPictureElement) await document.exitPictureInPicture();
        else await el.requestPictureInPicture();
    } catch { /* 手势上下文丢失等偶发拒绝，忽略 */ }
    wake();
}
function onPipChange() {
    pipOn.value = !!document.pictureInPictureElement;
}

// —— 控制条自动隐藏 ——
const showCtrl = ref(true);
let hideTimer = 0;
function wake() {
    showCtrl.value = true;
    if (hideTimer) clearTimeout(hideTimer);
    hideTimer = window.setTimeout(() => {
        if (playing.value) showCtrl.value = false;
    }, 2800);
}
function onLeave() {
    if (playing.value) showCtrl.value = false;
}

// —— 全屏 ——
const isFs = ref(false);
function onFsChange() {
    isFs.value = !!document.fullscreenElement;
}
function toggleFs() {
    const el = root.value;
    if (!el) return;
    if (document.fullscreenElement) void document.exitFullscreen().catch(() => {});
    else void el.requestFullscreen?.().catch(() => {});
}

// —— 键盘 ——
function onKey(e: KeyboardEvent) {
    const k = e.key.toLowerCase();
    if (k === " " || k === "k") {
        e.preventDefault();
        toggle();
    } else if (k === "arrowright") {
        nudge(5);
    } else if (k === "arrowleft") {
        nudge(-5);
    } else if (k === "l") {
        nudge(10);
    } else if (k === "arrowup") {
        e.preventDefault();
        setVolume(volume.value + 0.1);
    } else if (k === "arrowdown") {
        e.preventDefault();
        setVolume(volume.value - 0.1);
    } else if (k === "m") {
        toggleMute();
    } else if (k === "f") {
        toggleFs();
    } else if (k === "x") {
        togglePip();
    }
    wake();
}

onMounted(() => {
    document.addEventListener("fullscreenchange", onFsChange);
    media.value?.addEventListener("enterpictureinpicture", onPipChange);
    media.value?.addEventListener("leavepictureinpicture", onPipChange);
    wake();
});
onBeforeUnmount(() => {
    document.removeEventListener("fullscreenchange", onFsChange);
    media.value?.removeEventListener("enterpictureinpicture", onPipChange);
    media.value?.removeEventListener("leavepictureinpicture", onPipChange);
    if (hideTimer) clearTimeout(hideTimer);
    if (seekDebounce) clearTimeout(seekDebounce);
});

// 预览框挂载后测宽定位
watch(hoverRatio, () => nextTick(placePreview));
</script>

<style scoped>
.vp {
    position: relative;
    width: 100%;
    height: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    background: #000;
    overflow: hidden;
    outline: none;
}
.vp-video {
    width: 100%;
    height: 100%;
    cursor: pointer;
}
.fit-contain {
    object-fit: contain;
}
.fit-cover {
    object-fit: cover;
}
.fit-fill {
    object-fit: fill;
}
.vp-center {
    position: absolute;
    inset: 0;
    display: flex;
    align-items: center;
    justify-content: center;
    pointer-events: none;
}
.vp-fab {
    pointer-events: auto;
    width: 72px;
    height: 72px;
    border-radius: 50%;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(0, 0, 0, 0.42);
    backdrop-filter: blur(6px);
    -webkit-backdrop-filter: blur(6px);
    border: 1px solid rgba(255, 255, 255, 0.2);
    color: #fff;
    cursor: pointer;
    transition: transform 0.15s ease, background 0.15s ease;
}
.vp-fab:hover {
    transform: scale(1.06);
    background: rgba(0, 0, 0, 0.56);
}
.vp-fab> :deep(svg) {
    font-size: 34px;
    margin-left: 4px;
}
.vp-spin {
    pointer-events: none;
    width: 46px;
    height: 46px;
    border-radius: 50%;
    border: 3px solid rgba(255, 255, 255, 0.25);
    border-top-color: #fff;
    animation: vp-spin 0.8s linear infinite;
}
@keyframes vp-spin {
    to {
        transform: rotate(360deg);
    }
}

/* —— 底部控制区：贴底全宽 + 上滑遮罩 —— */
.vp-bar {
    position: absolute;
    left: 0;
    right: 0;
    bottom: 0;
    padding: 26px var(--base-size-large-x) 8px;
    background: linear-gradient(to top, rgba(0, 0, 0, 0.72), rgba(0, 0, 0, 0.35) 60%, transparent);
    opacity: 1;
    transform: translateY(0);
    transition: opacity 0.25s ease, transform 0.25s ease;
}
.vp-blank .vp-bar,
.vp-blank .vp-center {
    opacity: 0;
    transform: translateY(8px);
    pointer-events: none;
}
.vp-seek {
    --sb-track: rgba(255, 255, 255, 0.24);
    --sb-buffered: rgba(255, 255, 255, 0.45);
    --sb-fill: #00aeec;
    --sb-knob: #fff;
}
.vp-ctrls {
    display: flex;
    align-items: center;
    gap: 4px;
    margin-top: 2px;
}
.vp-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 30px;
    height: 30px;
    padding: 0 6px;
    border: 0;
    border-radius: var(--base-radius-default);
    background: transparent;
    color: #fff;
    cursor: pointer;
    transition: background 0.15s ease;
}
.vp-btn:hover {
    background: rgba(255, 255, 255, 0.16);
}
.vp-btn> :deep(svg) {
    font-size: 18px;
}
.vp-txt {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    height: 30px;
    padding: 0 8px;
    border: 0;
    border-radius: var(--base-radius-default);
    background: transparent;
    color: rgba(255, 255, 255, 0.92);
    font-size: var(--base-size-default);
    font-variant-numeric: tabular-nums;
    cursor: pointer;
    transition: background 0.15s ease;
}
.vp-txt:hover {
    background: rgba(255, 255, 255, 0.16);
}
.vp-txt.on {
    color: #00aeec;
    font-weight: 600;
}
.vp-time {
    font-size: var(--base-size-small);
    color: rgba(255, 255, 255, 0.92);
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
    padding: 0 4px;
}
.vp-time i {
    font-style: normal;
    opacity: 0.6;
}
.vp-spacer {
    flex: 1;
    min-width: 0;
}

/* —— 悬停预览缩略图 —— */
.vp-preview {
    position: absolute;
    bottom: 100%;
    transform: translateX(-50%);
    margin-bottom: 18px;
    padding: 4px;
    background: rgba(0, 0, 0, 0.85);
    border-radius: var(--base-radius-large);
    box-shadow: var(--base-shadow, 0 8px 24px rgba(0, 0, 0, 0.5));
    pointer-events: none;
}
.vp-preview-video {
    display: block;
    width: 176px;
    height: 99px;
    border-radius: var(--base-radius-default);
    background: #000;
    object-fit: cover;
}
.vp-preview-time {
    position: absolute;
    right: 10px;
    bottom: 8px;
    padding: 1px 6px;
    border-radius: var(--base-radius-small);
    background: rgba(0, 0, 0, 0.72);
    color: #fff;
    font-size: var(--base-size-small);
    font-variant-numeric: tabular-nums;
}

/* —— 深色弹层（popover 气泡挂 body 下，用 :global 覆盖；不依赖 Arco theme=dark）—— */
:global(.vp-pop) {
    background: rgba(18, 20, 24, 0.94);
    border-radius: 8px;
    color: #fff;
    box-shadow: 0 8px 24px rgba(0, 0, 0, 0.45);
}
:global(.vp-pop .arco-trigger-popup-arrow) {
    background: rgba(18, 20, 24, 0.94);
}
.vp-menu {
    display: flex;
    flex-direction: column;
    min-width: 66px;
}
.vp-menu-item {
    padding: 7px 14px;
    text-align: center;
    color: rgba(255, 255, 255, 0.88);
    font-size: var(--base-size-default);
    font-variant-numeric: tabular-nums;
    border-radius: var(--base-radius-default);
    cursor: pointer;
    transition: background 0.12s ease;
}
.vp-menu-item:hover {
    background: rgba(255, 255, 255, 0.12);
}
.vp-menu-item.on {
    color: #00aeec;
    font-weight: 600;
}
.vol-pop {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 8px;
}
.vol-pct {
    font-size: var(--base-size-small);
    color: #fff;
    font-variant-numeric: tabular-nums;
}
</style>
