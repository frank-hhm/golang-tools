<template>
    <div class="ap" tabindex="0" @keydown="onKey">
        <audio ref="media" :src="url" preload="metadata" :loop="loop" @play="onPlay" @pause="onPause"
            @ended="onEnded" @loadedmetadata="onLoadedMeta" @durationchange="onLoadedMeta" @loadstart="onLoadStart"
            @progress="onProgress"></audio>

        <!-- 封面主色氛围背景：取不到主色就保持主题原底 -->
        <div v-if="tint" class="ap-tint" :style="{ background: tintBg }"></div>

        <div class="ap-body">
            <div class="ap-art" :class="{ lift: playing, lit: !!tag?.cover }">
                <img v-if="tag?.cover" :src="tag.cover.url" :alt="showTitle" />
                <div v-else class="ap-art-ph"><icon-music /></div>
            </div>

            <div class="ap-info">
                <div class="ap-meta">
                    <div class="ap-title" :title="showTitle">{{ showTitle }}</div>
                    <div class="ap-artist" :title="showArtist">{{ showArtist }}</div>
                </div>
                <button class="ap-icon ap-heart" :class="{ on: liked }" :title="liked ? '取消收藏' : '收藏'"
                    @click="toggleLike">
                    <icon-heart-fill v-if="liked" />
                    <icon-heart v-else />
                </button>
            </div>

            <div class="ap-progress">
                <seek-bar large :ratio="progressRatio" :buffered="bufRatio" :duration="duration" tip
                    @update:ratio="seekRatio" />
                <div class="ap-time">
                    <span>{{ fmtTime(current) }}</span>
                    <span>{{ fmtRemain(current, duration) }}</span>
                </div>
            </div>

            <div class="ap-ctrls">
                <button class="ap-icon" :class="{ on: loop }" :title="loop ? '取消单曲循环' : '单曲循环'"
                    @click="loop = !loop">
                    <icon-repeat />
                </button>
                <button class="ap-icon" title="上一首（单文件播放）" disabled>
                    <icon-skip-back />
                </button>
                <button class="ap-play" :title="playing ? '暂停(空格)' : '播放(空格)'" @click="toggle">
                    <icon-pause v-if="playing" />
                    <icon-play-arrow v-else />
                </button>
                <button class="ap-icon" title="下一首（单文件播放）" disabled>
                    <icon-skip-forward />
                </button>
                <button class="ap-icon" title="播放列表（单文件播放）" disabled>
                    <icon-list />
                </button>
            </div>

            <div class="ap-aux">
                <a-dropdown trigger="click" @select="onSpeed">
                    <button class="ap-aux-btn" title="倍速播放">{{ rateLabel(rate) }}</button>
                    <template #content>
                        <a-doption v-for="s in SPEEDS" :key="s" :value="s" :class="{ 'ap-on': s === rate }">
                            {{ s }}x
                        </a-doption>
                    </template>
                </a-dropdown>
                <a-popover trigger="hover" position="top" :content-style="{ padding: '10px 14px', minWidth: '168px' }">
                    <button class="ap-aux-btn" :title="muted ? '取消静音(M)' : '静音(M)'" @click="toggleMute">
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
音频播放器（Apple Music 式全屏音乐页皮肤）：
- 大封面居中，播放时轻微放大；封面/标题/艺术家从 MP3 的 ID3 标签里取
  （id3tag.ts 走 Range 局部读，不整段下载；无标签回退文件名 + 渐变占位）；
- 背景用封面主色做氛围渐变（取不到主色就不加层，深浅主题都走语义变量）；
- 信息行 = 标题 + 艺术家 + 收藏心（localStorage 按文件名记忆）；
- 控制行 = 循环 / 上一首 / 播放暂停 / 下一首 / 列表，单文件播放下三个队列位
  保留视觉但禁用（title 说明），循环是真功能（audio.loop）；
- 进度条右侧时间改剩余负数显示（-2:42 式），左已播右剩余。
倍速与音量收进底部辅助行。键盘空格播放/暂停、←/→ ±5s、M 静音。
播放内核状态见 mediaCore.ts，进度/音量条见 SeekBar.vue。
*/
import { computed, onBeforeUnmount, ref, watch } from "vue";
import { fmtTime, rateLabel, SPEEDS, useMediaCore } from "./mediaCore";
import { parseID3, releaseID3, fmtRemain, type ID3Result } from "./id3tag";
import SeekBar from "./SeekBar.vue";

const props = defineProps<{ url: string; name: string }>();

const media = ref<HTMLMediaElement | null>(null);
const {
    playing, current, duration, bufferedEnd, volume, muted, rate,
    onPlay, onPause, onLoadedMeta, onLoadStart, onProgress,
    toggle, seekRatio, nudge, setVolume, toggleMute, setRate,
} = useMediaCore(media);

// —— 显示与标签 ——
const tag = ref<ID3Result | null>(null);
const showTitle = computed(() => tag.value?.title || props.name.replace(/\.[^.]+$/, ""));
const showArtist = computed(() => {
    const t = tag.value;
    if (t?.artist && t.album) return `${t.artist} — ${t.album}`;
    return t?.artist || "未知艺术家";
});
const tint = computed(() => tag.value?.tint || "");
const tintBg = computed(() =>
    `radial-gradient(120% 90% at 50% 0%, ${tint.value.replace("rgb(", "rgba(").replace(")", "")}, 0.42) 0%, transparent 62%), ${tint.value}`
);

async function loadTag(u: string) {
    releaseID3(tag.value);
    tag.value = null;
    if (!u) return;
    try { u = decodeURIComponent(u); } catch { /* 非法编码按原样判 */ }
    if (!/\.mp3(\?|$)/i.test(u)) return; // 只有真 mp3 才值得去读标签（ID3 是 mp3 专属容器）
    try {
        tag.value = await parseID3(u);
    } catch { /* 无标签/不支持 Range：回退占位 */ }
}
watch(() => props.url, loadTag, { immediate: true });
onBeforeUnmount(() => releaseID3(tag.value));

// —— 单曲循环 / 收藏 ——
const loop = ref(false);
const likeKey = computed(() => "mp3-like:" + props.name);
const liked = ref(false);
try { liked.value = localStorage.getItem(likeKey.value) === "1"; } catch { /* 隐私模式忽略 */ }
function toggleLike() {
    liked.value = !liked.value;
    try { localStorage.setItem(likeKey.value, liked.value ? "1" : "0"); } catch { /* 存不上只影响本次 */ }
}

function onEnded() {
    if (!loop.value) onPause();
}

// —— 进度与辅助控件 ——
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
    position: relative;
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
.ap-tint {
    position: absolute;
    inset: 0;
    opacity: 0.16;
    pointer-events: none;
}
body[arco-theme="dark"] .ap-tint {
    opacity: 0.3;
}
.ap-body {
    position: relative;
    width: 100%;
    max-width: 420px;
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: var(--base-margin);
}

/* —— 封面 —— */
.ap-art {
    width: 232px;
    height: 232px;
    border-radius: var(--base-radius-large-x);
    overflow: hidden;
    background: linear-gradient(135deg, rgb(var(--primary-6)), #4080ff);
    box-shadow: 0 18px 44px rgba(0, 0, 0, 0.28);
    display: flex;
    align-items: center;
    justify-content: center;
    transition: transform 0.45s ease, box-shadow 0.45s ease;
}
.ap-art.lift {
    transform: scale(1.035);
    box-shadow: 0 26px 60px rgba(0, 0, 0, 0.38);
}
.ap-art img {
    width: 100%;
    height: 100%;
    object-fit: cover;
    display: block;
}
.ap-art-ph {
    display: flex;
    align-items: center;
    justify-content: center;
}
.ap-art-ph> :deep(svg) {
    font-size: 64px;
    color: #fff;
}

/* —— 信息行 —— */
.ap-info {
    width: 100%;
    display: flex;
    align-items: center;
    gap: var(--base-margin);
}
.ap-meta {
    flex: 1;
    min-width: 0;
}
.ap-title {
    font-size: var(--base-size-large-x);
    font-weight: 600;
    line-height: 1.4;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--color-text-1);
}
.ap-artist {
    font-size: var(--base-size-default);
    margin-top: 2px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    color: var(--color-text-3);
}
.ap-heart {
    flex: none;
}
.ap-heart.on {
    color: rgb(var(--danger-6));
}

/* —— 进度 —— */
.ap-progress {
    --sb-track: rgba(128, 132, 140, 0.32);
    --sb-buffered: rgba(128, 132, 140, 0.5);
    --sb-knob: rgb(var(--primary-6));
    width: 100%;
}
.ap-time {
    width: 100%;
    display: flex;
    justify-content: space-between;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    font-variant-numeric: tabular-nums;
    margin-top: -4px;
}

/* —— 控制行 —— */
.ap-ctrls {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--base-size-large);
    margin-top: 4px;
}
.ap-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    border: 0;
    border-radius: var(--base-radius-default);
    background: transparent;
    color: var(--color-text-2);
    cursor: pointer;
    transition: background 0.15s ease, color 0.15s ease;
}
.ap-icon:hover:not([disabled]) {
    background: var(--color-fill-2);
    color: var(--color-text-1);
}
.ap-icon> :deep(svg) {
    font-size: 20px;
}
.ap-icon[disabled] {
    cursor: default;
    opacity: 0.4;
}
.ap-icon.on {
    color: rgb(var(--primary-6));
}
.ap-play {
    width: 60px;
    height: 60px;
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
    font-size: 28px;
}

/* —— 辅助行（倍速/音量）—— */
.ap-aux {
    width: 100%;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: var(--base-margin);
    margin-top: 2px;
}
.ap-aux-btn {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    min-width: 32px;
    height: 28px;
    padding: 0 8px;
    border: 0;
    border-radius: var(--base-radius-default);
    background: transparent;
    color: var(--color-text-3);
    cursor: pointer;
    font-size: var(--base-size-small);
    font-weight: 600;
    font-variant-numeric: tabular-nums;
    transition: background 0.15s ease;
}
.ap-aux-btn:hover {
    background: var(--color-fill-2);
}
.ap-aux-btn> :deep(svg) {
    font-size: 16px;
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
