import { onBeforeUnmount, ref, type Ref } from "vue";

/*
音视频播放共享内核：把「播放/暂停、进度、缓冲、音量、静音、倍速」这些与
具体皮肤无关的状态与处理器抽出来，VideoPlayer / AudioPlayer 各自只负责布局。
元素事件一律在模板里绑定（@play/@pause/…），这样切换文件时 :src 变了、
甚至元素被重建，监听也不会丢（不依赖 addEventListener 的一次性挂载）。
*/

/** 秒 → m:ss / h:mm:ss，非法值兜底 0:00。 */
export function fmtTime(s: number): string {
    if (!isFinite(s) || s < 0) s = 0;
    const h = Math.floor(s / 3600);
    const m = Math.floor((s % 3600) / 60);
    const sec = Math.floor(s % 60);
    const ss = String(sec).padStart(2, "0");
    if (h > 0) return `${h}:${String(m).padStart(2, "0")}:${ss}`;
    return `${m}:${ss}`;
}

/** 常用倍速档位。 */
export const SPEEDS = [0.5, 0.75, 1, 1.25, 1.5, 2];

/** 倍速展示：1 → "1x"，1.25 → "1.25x"。 */
export function rateLabel(r: number): string {
    return (Number.isInteger(r) ? String(r) : String(r)) + "x";
}

export function useMediaCore(media: Ref<HTMLMediaElement | null>) {
    const playing = ref(false);
    const current = ref(0);
    const duration = ref(0);
    const bufferedEnd = ref(0);
    const volume = ref(1);
    const muted = ref(false);
    const rate = ref(1);
    const ready = ref(false);

    let raf = 0;
    function loop() {
        const el = media.value;
        if (el) current.value = el.currentTime;
        raf = requestAnimationFrame(loop);
    }
    function startLoop() {
        if (!raf) raf = requestAnimationFrame(loop);
    }
    function stopLoop() {
        if (raf) {
            cancelAnimationFrame(raf);
            raf = 0;
        }
    }

    // —— 模板事件处理器 ——
    function onPlay() {
        playing.value = true;
        startLoop();
    }
    function onPause() {
        playing.value = false;
        stopLoop();
        const el = media.value;
        if (el) current.value = el.currentTime;
    }
    function onLoadedMeta() {
        const el = media.value;
        if (!el) return;
        duration.value = isFinite(el.duration) ? el.duration : 0;
        bufferedEnd.value = 0;
        ready.value = true;
        el.playbackRate = rate.value;
        el.volume = volume.value;
        el.muted = muted.value;
    }
    function onLoadStart() {
        ready.value = false;
        playing.value = false;
        current.value = 0;
        duration.value = 0;
        bufferedEnd.value = 0;
    }
    function onProgress() {
        const el = media.value;
        if (!el) return;
        try {
            if (el.buffered.length) bufferedEnd.value = el.buffered.end(el.buffered.length - 1);
        } catch {
            /* 某些时刻 buffered 会抛 IndexSizeError，忽略即可 */
        }
    }

    // —— 操作 ——
    function toggle() {
        const el = media.value;
        if (!el) return;
        if (el.paused) void el.play().catch(() => {});
        else el.pause();
    }
    function seekRatio(r: number) {
        const el = media.value;
        if (!el || !isFinite(duration.value) || duration.value <= 0) return;
        r = Math.max(0, Math.min(1, r));
        const t = r * duration.value;
        el.currentTime = t;
        current.value = t;
    }
    function nudge(delta: number) {
        const el = media.value;
        if (!el || !isFinite(duration.value)) return;
        seekRatio((el.currentTime + delta) / duration.value);
    }
    function setVolume(v: number) {
        v = Math.max(0, Math.min(1, v));
        volume.value = v;
        const el = media.value;
        if (el) {
            el.volume = v;
            el.muted = v === 0;
            muted.value = v === 0;
        }
    }
    function toggleMute() {
        const el = media.value;
        muted.value = !muted.value;
        if (el) el.muted = muted.value;
    }
    function setRate(r: number) {
        rate.value = r;
        const el = media.value;
        if (el) el.playbackRate = r;
    }

    onBeforeUnmount(stopLoop);

    return {
        playing,
        current,
        duration,
        bufferedEnd,
        volume,
        muted,
        rate,
        ready,
        onPlay,
        onPause,
        onLoadedMeta,
        onLoadStart,
        onProgress,
        toggle,
        seekRatio,
        nudge,
        setVolume,
        toggleMute,
        setRate,
    };
}
