<template>
    <div class="tpd">
        <!-- 形态开关：切了不用重开页面，逐个对照省事 -->
        <a-space wrap class="tpd-ctrl">
            <span class="tpd-ctrl-label">variant</span>
            <a-radio-group v-model="variant" size="mini" type="button">
                <a-radio value="plain">plain 独立块</a-radio>
                <a-radio value="bare">bare 嵌卡片</a-radio>
            </a-radio-group>
            <span class="tpd-ctrl-label">size</span>
            <a-radio-group v-model="size" size="mini" type="button">
                <a-radio value="medium">medium</a-radio>
                <a-radio value="small">small</a-radio>
            </a-radio-group>
            <a-checkbox v-model="divided">divided 分隔线</a-checkbox>
            <a-checkbox v-model="showText">show-text 条内文案</a-checkbox>
        </a-space>

        <a-card class="tpd-card" title="实时模拟：进度 / 速度 / ETA 自己走">
            <template #extra>
                <a-button size="mini" :disabled="live.running" @click="startLive">
                    {{ live.done ? "重跑" : "开始" }}
                </a-button>
            </template>
            <common-task-progress v-bind="liveProps" @cancel="cancelLive" @retry="startLive" @open="onOpen">
                <template #extra>
                    <span class="tpd-extra">{{ live.speedText }}</span>
                </template>
            </common-task-progress>
            <div class="tpd-tip">
                running 时 ETA 由「已下 / 总量 / 速率」实时估算，并与备注合成一行（左备注 / 右 ETA+操作）；点「取消」变
canceled（默认按钮切「重试」），跑完变 done（切「打开」）。
            </div>
        </a-card>

        <a-card class="tpd-card" title="状态矩阵：六态 + 不定进度">
            <div class="tpd-rows">
                <common-task-progress v-for="c in cases" :key="c.name" :name="c.name" :status="c.status"
                    :percent="c.percent" :speed="c.speed" :eta="c.eta" :meta="c.meta" :indeterminate="c.indeterminate"
                    :variant="variant" :divided="divided" :size="size" :show-text="showText" @cancel="noop('cancel')"
                    @retry="noop('retry')" @open="noop('open')" />
            </div>
            <div class="tpd-tip">
                idle 不渲染底部操作行（无动作可给）；stalled / canceled 走 warning，error 走 danger，done 走 success。
            </div>
        </a-card>

        <a-card class="tpd-card" title="插槽与兜底：无名称 / 自定义 #meta / 错误描边">
            <div class="tpd-rows">
                <!-- 名称为空：头行整体不渲染，名称交给外层卡片头（task-hub 抽屉就是这种用法） -->
                <common-item-card class="tpd-item" :clickable="false" pad="10px">
                    <template #status>
                        <com-badge status="processing" animated />
                    </template>
                    <template #title>
                        <span class="tpd-item-title">名称由卡片头承担（variant=bare）</span>
                    </template>
                    <common-task-progress status="running" :percent="46" :divided="false" variant="bare" size="small"
                        :show-text="false" @cancel="noop('cancel')">
                        <template #left>
                            <span class="tpd-extra">12 秒前开始</span>
                        </template>
                        <template #actions>
                            <a-link @click="noop('open-page')">打开页面</a-link>
                            <a-link @click="noop('log')">日志</a-link>
                        </template>
                    </common-task-progress>
                </common-item-card>

                <!-- #meta 由调用方接管时，组件不再叠加自身错误描边，避免双重框 -->
                <common-task-progress name="自定义 #meta（插槽内自绘，无错误描边）" status="error" :percent="62"
                    :eta="''" :variant="variant" :divided="divided" :size="size" :show-text="showText"
                    @retry="noop('retry')">
                    <template #meta>
                        <span class="tpd-slot-meta">
                            连接被对端重置 · 已重试 2 次 · 可点右侧「重试」再来一轮
                        </span>
                    </template>
                </common-task-progress>

                <!-- 不传 #meta 的错误态：组件自带左红条 + 浅红底 -->
                <common-task-progress name="meta 兜底的错误态（组件自带描边）" status="error" :percent="88"
                    meta="Get &quot;/pkg/big.tar.gz&quot;: unexpected EOF" :variant="variant" :divided="divided"
                    :size="size" :show-text="showText" @retry="noop('retry')" />
            </div>
        </a-card>
    </div>
</template>

<script setup lang="ts">
/**
 * common-task-progress 的测试台（仅 dev 入口可达，打包后主页入口隐藏）。
 *
 * 覆盖：六态（idle/running/stalled/done/error/canceled）+ 不定进度、plain/bare 两形态、
 * divided 开关、show-text 开关、#meta/#left/#actions 插槽、无名称时头行不渲染、
 * 以及一条会自己走的实时任务（验证 speed / ETA / 默认动作切换）。
 * 纯前端，不碰后端。
 */
import { computed, onUnmounted, reactive, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { clampPercent, etaFromBytes, normalizeStatus } from "@/utils/taskProgress";

const variant = ref<"plain" | "bare">("plain");
const size = ref<"small" | "medium">("medium");
const divided = ref(true);
const showText = ref(true);

/** 状态矩阵：字段留空的项由组件按缺省处理（如 idle 无动作、不定进度无百分比） */
const cases: Array<{
    name: string;
    status: "idle" | "running" | "done" | "error" | "canceled" | "stalled";
    percent?: number;
    speed?: string;
    eta?: string;
    meta?: string;
    indeterminate?: boolean;
}> = [
        { name: "idle · 排队中（无底部操作行）", status: "idle", percent: 0, meta: "等待前面的任务让出并发槽" },
        { name: "running · 正常推进", status: "running", percent: 46, speed: "1.24 MB/s", eta: "大约还需 3 分钟", meta: "分块 5/12 · 已校验 3 块" },
        { name: "running · 不定进度（还没探到总大小）", status: "running", indeterminate: true, speed: "820 KB/s", meta: "已下 12.4 MB · 总大小未知" },
        { name: "stalled · 疑似卡住（warning）", status: "stalled", percent: 62, speed: "0 B/s", eta: "大约还需 —", meta: "45 秒没有新字节，检查代理与网络" },
        { name: "done · 完成（success + 默认「打开」）", status: "done", percent: 100, speed: "0 B/s", meta: "共 128.6 MB · 用时 1 分 42 秒" },
        { name: "error · 失败（danger + 默认「重试」）", status: "error", percent: 62, meta: "HTTP 503 · Service Unavailable" },
        { name: "canceled · 已取消（warning）", status: "canceled", percent: 21, meta: "手动取消，已下部分保留在 .gtdl 续传记录里" },
    ];

/* ==================== 实时模拟 ==================== */
const TOTAL = 20 * 1024 * 1024;
const live = reactive({
    running: false,
    done: false,
    downloaded: 0,
    speed: 0,
    speedText: "—",
});
let timer: number | null = null;

const liveProps = computed(() => ({
    name: "gtdl-setup-linux-amd64.tar.gz",
    status: normalizeStatus(live.done ? "done" : live.running ? "running" : "idle"),
    percent: clampPercent((live.downloaded / TOTAL) * 100) ?? 0,
    meta: `已下 ${(live.downloaded / 1048576).toFixed(1)} MB / 20 MB`,
    eta: etaFromBytes(live.downloaded, TOTAL, live.speed),
    variant: variant.value,
    divided: divided.value,
    size: size.value,
    showText: showText.value,
}));

/** 每 200ms 推进一段，速率带抖动，让 ETA 与速度文案真的在变 */
function startLive() {
    stopTimer();
    live.running = true;
    live.done = false;
    live.downloaded = 0;
    live.speed = 0;
    live.speedText = "—";
    let last = performance.now();
    timer = window.setInterval(() => {
        const now = performance.now();
        const dt = (now - last) / 1000;
        last = now;
        const step = (1.4 + Math.random() * 0.9) * 1048576 * dt;
        live.downloaded = Math.min(TOTAL, live.downloaded + step);
        live.speed = step / Math.max(dt, 0.001);
        live.speedText = `${(live.speed / 1048576).toFixed(2)} MB/s`;
        if (live.downloaded >= TOTAL) {
            live.running = false;
            live.done = true;
            live.speedText = "完成";
            stopTimer();
        }
    }, 200);
}

function cancelLive() {
    stopTimer();
    live.running = false;
    live.done = false;
    live.speed = 0;
    live.speedText = "已取消";
    Message.info("已取消（组件只 emit，动作由调用方处理）");
}

function stopTimer() {
    if (timer != null) { clearInterval(timer); timer = null; }
}

function onOpen() {
    Message.success("done 态默认按钮：只 emit open，落盘/打开目录由调用方决定");
}

function noop(what: string) {
    Message.info(`事件已收到：${what}`);
}

startLive();
onUnmounted(stopTimer);
</script>

<style scoped>
.tpd {
    display: flex;
    flex-direction: column;
    gap: 12px;
}

.tpd-ctrl {
    align-items: center;
}

.tpd-ctrl-label {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.tpd-card {
    border-radius: var(--base-radius-default);
}

.tpd-rows {
    display: flex;
    flex-direction: column;
    gap: 10px;
}

.tpd-tip {
    margin-top: 10px;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    line-height: 1.6;
}

.tpd-extra {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    font-variant-numeric: tabular-nums;
}

.tpd-item-title {
    font-weight: 600;
    color: var(--color-text-1);
}

.tpd-slot-meta {
    color: rgb(var(--danger-6));
}
</style>
