<template>
    <div class="ap" tabindex="0" @keydown="onKey">
        <audio ref="media" :src="url" preload="metadata" @play="onPlay" @pause="onPause" @ended="onPause"
            @loadedmetadata="onLoadedMeta" @durationchange="onLoadedMeta" @loadstart="onLoadStart"
            @progress="onProgress"></audio>

        <div class="ap-card">
            <div class="ap-art" :class="{ spin: playing }">
                <div class="ap-disc"><icon-music /></div>
                <div class="ap-hole"></div>
            </div>

            <div class="ap-title" :title="name">{{ name }}</div>

            <seek-bar class="ap-seek" large :ratio="progressRatio" :buffered="bufRatio" :duration="duration" tip
                @update:ratio="seekRatio" />
            <div class="ap-time">
                <span>{{ fmtTime(current) }}</span>
                <span>{{ fmtTime(duration) }}</span>
            </div>

            <div class="ap-ctrls">
                <a-dropdown trigger="click" @select="onSpeed">
                    <button class="ap-btn ap-speed" title="倍速播放">{{ rateLabel(rate) }}</button>
                    <template #content>
                        <a-doption v-for="s in SPEEDS" :key="s" :value="s" :class="{ 'ap-on': s === rate }">
                            {{ s }}x
                        </a-doption>
                    </template>
                </a-dropdown>

                <button class="ap-play" :title="playing ? '暂停(空格)' : '播放(空格)'" @click="toggle">
                    <icon-pause v-if="playing" />
                    <icon-play-arrow v-else />
                </button>

                <a-popover trigger="hover" position="top" :content-style="{ padding: '10px 14px', minWidth: '168px' }">
                    <button class="ap-btn" :title="muted ? '取消静音(M)' : '静音(M)'" @click="toggleMute">
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
            </div>
        </div>
    </div>
</template>

<script setup lang="ts">
/*
音频播放器（音乐 App 式自定义控件）：替换原生 <audio controls>。
唱片卡配色全部走 Arco 语义变量，浅色/暗黑（body[arco-theme]）自动换肤；
封面随播放旋转，大播放键居中，进度条可点可拖（快进）+ 悬停时间气泡、
时间左右对齐，倍速下拉、音量条+静音（悬停上方弹出）。键盘空格播放/暂停、←/→ ±5s、M 静音。
播放内核状态见 mediaCore.ts，进度/音量条见 SeekBar.vue。
*/
import { computed, ref } from "vue";
import { fmtTime, rateLabel, SPEEDS, useMediaCore } from "./mediaCore";
import SeekBar from "./SeekBar.vue";

defineProps<{ url: string; name: string }>();

const media = ref<HTMLMediaElement | null>(null);
const {
    playing, current, duration, bufferedEnd, volume, muted, rate,
    onPlay, onPause, onLoadedMeta, onLoadStart, onProgress,
    toggle, seekRatio, nudge, setVolume, toggleMute, setRate,
} = useMediaCore(media);

const progressRatio = computed(() => (duration.value ? current.value / duration.value : 0));
const bufRatio = computed(() => (duration.value ? Math.min(bufferedEnd.value / duration.value, 1) : 0));
const volPct = computed(() => Math.round((muted.value ? 0 : volume.value) * 100) + "%");
const volPercent = computed(() => Math.round((muted.value ? 0 : volume.value) * 100));
function onVol(v: number | number[]) {
    const n = Array.isArray(v) ? v[0] : v;
    setVolume((n || 0) / 100);
}

function onSpeed(v: string | number | Record<string, any> | undefined) {
    const n = Number(v);
    if (n > 0) setRate(n);
}

function onKey(e: KeyboardEvent) {
    const k = e.key.toLowerCase();
    if (k === " " || k === "k") {
        e.preventDefault();
        toggle();
    } else if (k === "arrowright") {
        nudge(5);
    } else if (k === "arrowleft") {
        nudge(-5);
    } else if (k === "m") {
        toggleMute();
    }
}
</script>

<style scoped>
.ap {
    flex: 1;
    min-height: 0;
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    padding: var(--base-padding);
    overflow: auto;
    outline: none;
}
.ap-card {
    width: 100%;
    max-width: 420px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--base-margin);
    padding: 28px var(--base-size-large-xx);
    border-radius: var(--base-radius-large-x);
    background: var(--color-bg-2);
    border: 1px solid var(--color-border-2);
    box-shadow: 0 18px 44px rgba(0, 0, 0, 0.16);
    color: var(--color-text-1);
}
.ap-seek {
    --sb-track: rgba(128, 132, 140, 0.32);
    --sb-buffered: rgba(128, 132, 140, 0.5);
    --sb-knob: rgb(var(--primary-6));
}
.ap-art {
    position: relative;
    width: 148px;
    height: 148px;
    border-radius: 50%;
    background: repeating-radial-gradient(circle at 50% 50%, rgba(255, 255, 255, 0.05) 0 2px, transparent 2px 5px),
        radial-gradient(circle at 50% 38%, #3a3f4b, #16181e 72%);
    box-shadow: 0 10px 26px rgba(0, 0, 0, 0.4), inset 0 0 0 6px rgba(255, 255, 255, 0.04);
    display: flex;
    align-items: center;
    justify-content: center;
    animation: ap-rotate 12s linear infinite;
    animation-play-state: paused;
}
.ap-art.spin {
    animation-play-state: running;
}
.ap-disc {
    width: 92px;
    height: 92px;
    border-radius: 50%;
    background: linear-gradient(135deg, rgb(var(--primary-6)), #4080ff);
    display: flex;
    align-items: center;
    justify-content: center;
    box-shadow: 0 6px 16px rgba(22, 93, 255, 0.4);
}
.ap-disc> :deep(svg) {
    font-size: 40px;
    color: #fff;
}
.ap-hole {
    position: absolute;
    width: 16px;
    height: 16px;
    border-radius: 50%;
    background: var(--color-bg-2);
    box-shadow: 0 0 0 4px rgba(255, 255, 255, 0.08);
}
@keyframes ap-rotate {
    to {
        transform: rotate(360deg);
    }
}
.ap-title {
    max-width: 100%;
    font-size: var(--base-size-large);
    font-weight: 600;
    text-align: center;
    word-break: break-all;
    line-height: 1.5;
}
.ap-time {
    width: 100%;
    display: flex;
    justify-content: space-between;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    font-variant-numeric: tabular-nums;
    margin-top: -6px;
}
.ap-ctrls {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--base-margin);
    margin-top: 4px;
}
.ap-play {
    width: 56px;
    height: 56px;
    border: 0;
    border-radius: 50%;
    background: linear-gradient(135deg, rgb(var(--primary-6)), #4080ff);
    color: #fff;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    box-shadow: 0 8px 20px rgba(22, 93, 255, 0.42);
    transition: transform 0.15s ease;
}
.ap-play:hover {
    transform: scale(1.06);
}
.ap-play> :deep(svg) {
    font-size: 26px;
}
.ap-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 32px;
    height: 32px;
    padding: 0 6px;
    border: 0;
    border-radius: var(--base-radius-default);
    background: transparent;
    color: var(--color-text-1);
    cursor: pointer;
    transition: background 0.15s ease;
}
.ap-btn:hover {
    background: var(--color-fill-2);
}
.ap-btn> :deep(svg) {
    font-size: 18px;
}
.ap-speed {
    font-size: var(--base-size-large);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
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
.ap-on {
    color: rgb(var(--primary-6));
    font-weight: 600;
}
</style>
