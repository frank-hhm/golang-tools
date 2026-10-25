<template>
    <!-- 页面级 Docker 引擎告示：只在「本机确实有 Docker 实例」且「引擎不可用」时出现。
         没有这块时，用户只在点「启动」那一步才撞到一条管道错误，看不出是引擎没起；
         这里把原因摊开，并直接给出 Docker 面板入口（启动引擎、看容器都在那边）。 -->
    <!-- 左侧卡片只有 300 上下宽，文案一律按「一行装得下」来排：结论上顶、影响另起一行淡色、
         完整错误串和面板说明全进 tooltip；按钮自己排布不走 #action，免得分不到宽度被截掉 -->
    <a-alert v-if="shown" type="warning" class="do-alert">
        <div class="do-line">
            <a-tooltip position="bl">
                <template #content>
                    <div class="do-tip">
                        <div v-for="(l, i) in tipLines" :key="i">{{ l }}</div>
                    </div>
                </template>
                <span class="do-text">Docker 引擎未运行</span>
            </a-tooltip>
            <a-space :size="4" class="do-ops">
                <a-tooltip content="打开 Docker 管理面板：启动引擎、看容器与镜像">
                    <a-button size="mini" @click="openPanel">打开面板</a-button>
                </a-tooltip>
                <a-tooltip content="再探一次引擎状态（不点也会每 15 秒自己探）">
                    <a-button size="mini" :loading="probing" @click="probe">重试</a-button>
                </a-tooltip>
            </a-space>
        </div>
        <div class="do-sub">{{ subText }}</div>
    </a-alert>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, onUnmounted, ref, watch } from "vue";
import { useToolWindow } from "@/views/index/useToolWindow";

const props = withDefaults(defineProps<{
    /** 本页面里 Docker 模式的实例数：为 0 时既不探测也不显示，免得给纯本地用户添噪 */
    count?: number;
    /** 工具名，只用于文案（PHP / MySQL / Redis / Nginx / 静态站点） */
    toolLabel?: string;
}>(), { count: 0, toolLabel: "" });

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const { openToolWindow } = useToolWindow();

const avail = ref(true);
const reason = ref("");
const probing = ref(false);
let timer: any = null;

const shown = computed(() => props.count > 0 && !avail.value);
const subText = computed(() =>
    `${props.toolLabel || "本工具"}的 ${props.count} 个 Docker 实例启动/重启都会失败，已建好的容器也不会跟着起来`);

/** 错误串拆行显示：结论一行、端点明细一行，再补上影响和自恢复说明（窄卡片里放不下长句） */
const tipLines = computed(() => {
    const raw = reason.value || "Docker 引擎无应答";
    const lines: string[] = [];
    const i = raw.indexOf("（端点");
    if (i > 0) {
        lines.push(raw.slice(0, i));
        lines.push(raw.slice(i + 1).replace(/[）]+$/g, ""));
    } else {
        lines.push(raw);
    }
    lines.push(subText.value);
    lines.push("Docker Desktop（或 Docker Engine）起来后本条会自动消失");
    return lines;
});

/** 探一次引擎状态；EngineStatus 不可达时也正常返回（available=false + 可读 error） */
async function probe() {
    if (probing.value) return;
    probing.value = true;
    try {
        const info = await GoTools?.DockerSrvEngineStatus();
        avail.value = !!info?.available;
        reason.value = info?.available ? "" : String(info?.error || "Docker 引擎不可用");
    } catch (e: any) {
        avail.value = false;
        reason.value = String(e?.message || e);
    } finally {
        probing.value = false;
        syncTimer();
    }
}

/** 去 Docker 管理面板：引擎启动、镜像拉取、容器管理都在那边 */
function openPanel() {
    openToolWindow("/tools/docker", "Docker管理", { single: true, width: 1200, height: 800 });
}

/** 只在「正显示着」的时候轮询：引擎一恢复告示就自己消失；不显示时零开销，不给命名管道添活 */
function syncTimer() {
    if (shown.value && !timer) timer = setInterval(probe, 15000);
    else if (!shown.value && timer) { clearInterval(timer); timer = null; }
}

function onFocus() { if (shown.value) probe(); }

watch(shown, syncTimer);
/** 实例从 0 变成有（刚把某个实例改成 Docker 模式）时立刻补一次探测 */
watch(() => props.count, (n, o) => { if (n > 0 && !o) probe(); });

onMounted(() => {
    if (props.count > 0) probe();
    window.addEventListener("focus", onFocus);
});
onUnmounted(() => {
    if (timer) clearInterval(timer);
    window.removeEventListener("focus", onFocus);
});
</script>

<style scoped>
.do-alert {
    margin-bottom: 8px;
}

/* 一行：结论吃掉剩余宽度并截断，按钮靠右不参与压缩 */
.do-line {
    display: flex;
    align-items: center;
    gap: 8px;
}

.do-text {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--base-size-small);
    cursor: help;
}

.do-ops {
    flex: none;
}

.do-sub {
    margin-top: 2px;
    font-size: var(--base-size-smaller);
    color: var(--color-text-2);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

/* tooltip 里的长错误串（带管道端点路径）要能换行，不能拉成一横条 */
.do-tip {
    max-width: 420px;
    line-height: 1.7;
    font-size: var(--base-size-small);
    overflow-wrap: anywhere;
}
</style>
