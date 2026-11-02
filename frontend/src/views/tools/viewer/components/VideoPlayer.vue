<template>
    <div ref="root" class="vp" tabindex="0" :class="{ 'vp-blank': !showCtrl && playing }" @mousemove="wake"
        @mouseleave="onLeave" @keydown="onKey">
        <video ref="media" class="vp-video" :src="url" preload="metadata" playsinline @click="toggle"
            @dblclick="toggleFs" @play="onPlay" @pause="onPause" @ended="onPause" @loadedmetadata="onLoadedMeta"
            @durationchange="onLoadedMeta" @loadstart="onLoadStart" @progress="onProgress"
            @waiting="buffering = true" @playing="buffering = false" @canplay="buffering = false"></video>

        <!-- 缓冲中转圈 / 暂停时居中磨砂大播放键（抖音式） -->
        <div class="vp-center">
            <div v-if="buffering" class="vp-spin"></div>
            <button v-else-if="!playing" class="vp-fab" aria-label="播放" @click="toggle">
                <icon-play-arrow-fill />
            </button>
        </div>

        <!-- 底部控制条：进度 + 一排操作，鼠标静止自动隐藏 -->
        <div class="vp-bar" @dblclick.stop>
            <seek-bar :ratio="progressRatio" :buffered="bufRatio" :duration="duration" tip
                @update:ratio="seekRatio" />
            <div class="vp-ctrls">
                <button class="vp-btn" :title="playing ? '暂停(空格)' : '播放(空格)'" @click="toggle">
                    <icon-pause v-if="playing" />
                    <icon-play-arrow v-else />
                </button>
                <span class="vp-time">{{ fmtTime(current) }} <i>/</i> {{ fmtTime(duration) }}</span>
                <div class="vp-spacer"></div>

                <a-dropdown trigger="click" @select="onSpeed">
                    <button class="vp-btn vp-speed" title="倍速播放">{{ rateLabel(rate) }}</button>
                    <template #content>
                        <a-doption v-for="s in SPEEDS" :key="s" :value="s" :class="{ 'vp-on': s === rate }">
                            {{ s }}x
                        </a-doption>
                    </template>
                </a-dropdown>

                <a-popover trigger="hover" position="top" :content-style="{ padding: '10px 14px', minWidth: '168px' }">
                    <button class="vp-btn" :title="muted ? '取消静音(M)' : '静音(M)'" @click="toggleMute">
                        <icon-mute v-if="muted || volume === 0" />
                        <icon-sound v-else />
                    </button>
                    <template #content>
                        <div class="vol-pop">
                            <a-slider class="vol-slider" :model-value="volPercent" :min="0" :max="100" :step="1"
                                @change="onVol" />
                            <span class="vol-pct">{{ volPct }}</span>
                        </div>
                    </template>
                </a-popover>

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
视频播放器（抖音式自定义控件）：替换原生 <video controls>。
进度条可点可拖（快进），带缓冲回显；倍速下拉、音量条+静音、全屏；
暂停时居中大播放键，播放中鼠标静止 2.8s 自动隐藏控制条、动一下即回。
键盘：空格/K 播放暂停，←/→ ±5s，↑/↓ 音量，M 静音，F 全屏。
播放内核状态见 mediaCore.ts，进度/音量条见 SeekBar.vue。
*/
import { computed, onBeforeUnmount, onMounted, ref } from "vue";
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
const volPercent = computed(() => Math.round((muted.value ? 0 : volume.value) * 100));
const buffering = ref(false);
function onVol(v: number | number[]) {
    const n = Array.isArray(v) ? v[0] : v;
    setVolume((n || 0) / 100);
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

function onSpeed(v: string | number | Record<string, any> | undefined) {
    const n = Number(v);
    if (n > 0) setRate(n);
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
    }
    wake();
}

onMounted(() => {
    document.addEventListener("fullscreenchange", onFsChange);
    wake();
});
onBeforeUnmount(() => {
    document.removeEventListener("fullscreenchange", onFsChange);
    if (hideTimer) clearTimeout(hideTimer);
});
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
    object-fit: contain;
    cursor: pointer;
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
.vp-bar {
    position: absolute;
    left: 12px;
    right: 12px;
    bottom: 12px;
    padding: 6px 12px;
    border-radius: var(--base-radius-large);
    background: rgba(15, 18, 23, 0.62);
    backdrop-filter: blur(12px);
    -webkit-backdrop-filter: blur(12px);
    border: 1px solid rgba(255, 255, 255, 0.08);
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
.vp-ctrls {
    display: flex;
    align-items: center;
    gap: 6px;
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
    font-size: var(--base-size-large);
    cursor: pointer;
    transition: background 0.15s ease;
}
.vp-btn:hover {
    background: rgba(255, 255, 255, 0.16);
}
.vp-btn> :deep(svg) {
    font-size: 18px;
}
.vp-speed {
    font-variant-numeric: tabular-nums;
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
.vol-pop {
    display: flex;
    align-items: center;
    gap: 10px;
    width: 100%;
}
.vol-slider {
    flex: 1;
    min-width: 110px;
}
.vol-pct {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    min-width: 34px;
    text-align: right;
    font-variant-numeric: tabular-nums;
}
.vp-on {
    color: rgb(var(--primary-6));
    font-weight: 600;
}
</style>
