<template>
    <a-row class="ts-page" :gutter="6">
        <!-- 左侧约 25%：当前时间戳 + 说明 -->
        <a-col :span="6" class="side-col">
            <a-row class="side-pages">
                <a-col class="side-cols" :span="24">
                    <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                        <template #title>当前时间戳</template>
                        <template #extra>
                            <span class="com-state" :class="paused ? 'is-dim' : 'is-on'">
                                <com-badge :status="paused ? 'normal' : 'success'" :animated="!paused" />
                                {{ paused ? "已暂停" : "运行中" }}
                            </span>
                        </template>
                        <div class="now-body">
                            <div class="now-value">
                                <span class="now-num">{{ displayNow }}</span>
                                <a-tag size="small" :color="nowUnit === 'ms' ? 'arcoblue' : 'purple'">
                                    {{ nowUnit === "ms" ? "毫秒" : "秒" }}
                                </a-tag>
                            </div>
                            <div class="now-date">{{ nowText }}</div>
                            <a-space :size="8" wrap>
                                <a-button size="small" @click="toggleUnit">
                                    <template #icon><icon-swap /></template>切换单位
                                </a-button>
                                <a-button size="small" @click="copyNow">
                                    <template #icon><icon-copy /></template>复制
                                </a-button>
                                <a-button size="small" :status="paused ? 'success' : 'danger'"
                                    @click="togglePause">
                                    <template #icon>
                                        <icon-play-circle v-if="paused" />
                                        <icon-pause-circle v-else />
                                    </template>
                                    {{ paused ? "继续" : "暂停" }}
                                </a-button>
                            </a-space>
                        </div>
                    </a-card>
                </a-col>
                <a-col class="side-cols mt6" :span="24">
                    <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                        <template #title>说明</template>
                        <div class="side-body">
                            <ul class="tips">
                                <li>支持秒（s）与毫秒（ms）两种单位，可随时切换。</li>
                                <li>时区换算基于浏览器 Intl 实现，无需联网。</li>
                                <li>单个转换支持双向互转，结果可直接复制。</li>
                                <li>批量转换每行一个时间戳，输出「原值 → 日期时间」。</li>
                            </ul>
                        </div>
                    </a-card>
                </a-col>
            </a-row>
        </a-col>

        <!-- 右侧：单个 / 批量转换 -->
        <a-col :span="18" class="main-col">
            <a-card class="main-panel card-head-tool" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space :size="8">
                        <span>时间戳转换</span>
                        <a-tag size="small">{{ view === "single" ? "单个转换" : "批量转换" }}</a-tag>
                    </a-space>
                </template>
                <template #extra>
                    <a-radio-group v-model="view" type="button" size="medium">
                        <a-radio value="single">单个转换</a-radio>
                        <a-radio value="batch">批量转换</a-radio>
                    </a-radio-group>
                </template>
                <div class="main-body">
                    <!-- 单个转换：左右两块面板 -->
                    <a-row v-show="view === 'single'" :gutter="6" class="fill-row">
                        <a-col :span="12" class="pane-col">
                            <a-card class="pane-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                                <template #title>
                                    <span class="pane-title"><icon-clock-circle /> 时间戳 → 日期时间</span>
                                </template>
                                <template #extra>
                                    <a-tag size="small">{{ tsUnit === "s" ? "秒" : "毫秒" }}</a-tag>
                                </template>
                                <div class="pane-body">
                                    <div class="field">
                                        <div class="field-label">时间戳</div>
                                        <a-input v-model="tsInput" placeholder="请输入时间戳" allow-clear
                                            @press-enter="tsToDate" />
                                    </div>
                                    <a-row :gutter="8">
                                        <a-col :span="12">
                                            <a-select v-model="tsUnit" style="width: 100%">
                                                <a-option value="s">秒 (s)</a-option>
                                                <a-option value="ms">毫秒 (ms)</a-option>
                                            </a-select>
                                        </a-col>
                                        <a-col :span="12">
                                            <a-select v-model="tsTz" style="width: 100%">
                                                <a-option v-for="tz in TZ_LIST" :key="tz" :value="tz">{{ tz }}</a-option>
                                            </a-select>
                                        </a-col>
                                    </a-row>
                                    <a-space :size="8">
                                        <a-button size="small" @click="fillCurrent">填入当前</a-button>
                                        <a-button size="small" status="success" @click="tsToDate">转换</a-button>
                                    </a-space>
                                    <div class="pane-result">
                                        <div class="field-label">转换结果</div>
                                        <a-input :model-value="tsResult" placeholder="转换结果" readonly class="result-input">
                                            <template #append>
                                                <a-button size="small" :disabled="!tsResult" @click="copyText(tsResult)">
                                                    <template #icon><icon-copy /></template>
                                                </a-button>
                                            </template>
                                        </a-input>
                                    </div>
                                </div>
                            </a-card>
                        </a-col>
                        <a-col :span="12" class="pane-col">
                            <a-card class="pane-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                                <template #title>
                                    <span class="pane-title"><icon-calendar /> 日期时间 → 时间戳</span>
                                </template>
                                <template #extra>
                                    <a-tag size="small">{{ dateUnit === "s" ? "秒" : "毫秒" }}</a-tag>
                                </template>
                                <div class="pane-body">
                                    <div class="field">
                                        <div class="field-label">日期时间</div>
                                        <a-input v-model="dateInput" placeholder="YYYY-MM-DD HH:mm:ss" allow-clear
                                            @press-enter="dateToTs" />
                                    </div>
                                    <a-row :gutter="8">
                                        <a-col :span="12">
                                            <a-select v-model="dateUnit" style="width: 100%" @change="dateToTs">
                                                <a-option value="s">秒 (s)</a-option>
                                                <a-option value="ms">毫秒 (ms)</a-option>
                                            </a-select>
                                        </a-col>
                                        <a-col :span="12">
                                            <a-select v-model="dateTz" style="width: 100%">
                                                <a-option v-for="tz in TZ_LIST" :key="tz" :value="tz">{{ tz }}</a-option>
                                            </a-select>
                                        </a-col>
                                    </a-row>
                                    <a-space :size="8">
                                        <a-button size="small" status="success" @click="dateToTs">转换</a-button>
                                    </a-space>
                                    <div class="pane-result">
                                        <div class="field-label">转换结果</div>
                                        <a-input :model-value="dateResult" placeholder="转换结果" readonly class="result-input">
                                            <template #append>
                                                <a-button size="small" :disabled="!dateResult" @click="copyText(dateResult)">
                                                    <template #icon><icon-copy /></template>
                                                </a-button>
                                            </template>
                                        </a-input>
                                    </div>
                                </div>
                            </a-card>
                        </a-col>
                    </a-row>

                    <!-- 批量转换 -->
                    <div v-show="view === 'batch'" class="batch-view">
                        <div class="batch-toolbar">
                            <a-space :size="8" wrap>
                                <a-select v-model="batchUnit" style="width: 110px">
                                    <a-option value="s">秒 (s)</a-option>
                                    <a-option value="ms">毫秒 (ms)</a-option>
                                </a-select>
                                <a-select v-model="batchTz" style="width: 170px">
                                    <a-option v-for="tz in TZ_LIST" :key="tz" :value="tz">{{ tz }}</a-option>
                                </a-select>
                                <a-button size="small" status="success" @click="batchConvert">
                                    <template #icon><icon-swap /></template>批量转换
                                </a-button>
                            </a-space>
                            <span class="batch-tip">每行一个时间戳</span>
                        </div>
                        <a-row :gutter="6" class="fill-row">
                            <a-col :span="12" class="pane-col">
                                <a-card class="pane-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                                    <template #title>
                                        <span class="pane-title">批量时间戳</span>
                                    </template>
                                    <template #extra>
                                        <a-tag size="small">{{ batchLines }} 行</a-tag>
                                    </template>
                                    <div class="pane-body pane-body-fill">
                                        <a-textarea v-model="batchInput" class="fill-textarea"
                                            placeholder="1792556334&#10;1792556416638" allow-clear />
                                    </div>
                                </a-card>
                            </a-col>
                            <a-col :span="12" class="pane-col">
                                <a-card class="pane-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                                    <template #title>
                                        <span class="pane-title">转换结果</span>
                                    </template>
                                    <template #extra>
                                        <a-button size="small" :disabled="!batchOutput"
                                            @click="copyText(batchOutput)">
                                            <template #icon><icon-copy /></template>复制结果
                                        </a-button>
                                    </template>
                                    <div class="pane-body pane-body-fill">
                                        <a-textarea v-model="batchOutput" class="fill-textarea" placeholder="转换结果"
                                            readonly />
                                    </div>
                                </a-card>
                            </a-col>
                        </a-row>
                    </div>
                </div>
            </a-card>
        </a-col>
    </a-row>
</template>

<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { Message } from "@arco-design/web-vue";

/** 常用时区列表 */
const TZ_LIST = [
    "Asia/Shanghai",
    "UTC",
    "Asia/Singapore",
    "Asia/Tokyo",
    "Asia/Kolkata",
    "Asia/Dubai",
    "Europe/London",
    "Europe/Paris",
    "Europe/Moscow",
    "America/New_York",
    "America/Chicago",
    "America/Los_Angeles",
    "Australia/Sydney",
];

/* ============ 时区转换核心（基于 Intl，纯前端实现） ============ */

/** 给定 UTC 毫秒，返回该时刻在指定时区相对 UTC 的偏移（分钟） */
function tzOffsetMinutes(utcMs: number, tz: string): number {
    const dtf = new Intl.DateTimeFormat("en-US", {
        timeZone: tz,
        hourCycle: "h23",
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
    });
    const p: Record<string, string> = {};
    for (const part of dtf.formatToParts(new Date(utcMs))) {
        if (part.type !== "literal") p[part.type] = part.value;
    }
    const asUTC = Date.UTC(+p.year, +p.month - 1, +p.day, +p.hour, +p.minute, +p.second);
    return Math.round((asUTC - utcMs) / 60000);
}

/** 将 UTC 毫秒格式化为指定时区的 "YYYY-MM-DD HH:mm:ss" */
function formatInTz(utcMs: number, tz: string): string {
    const dtf = new Intl.DateTimeFormat("zh-CN", {
        timeZone: tz,
        hourCycle: "h23",
        year: "numeric",
        month: "2-digit",
        day: "2-digit",
        hour: "2-digit",
        minute: "2-digit",
        second: "2-digit",
    });
    const p: Record<string, string> = {};
    for (const part of dtf.formatToParts(new Date(utcMs))) {
        if (part.type !== "literal") p[part.type] = part.value;
    }
    return `${p.year}-${p.month}-${p.day} ${p.hour}:${p.minute}:${p.second}`;
}

/** 将指定时区的墙上时间 "YYYY-MM-DD HH:mm:ss" 解析为 UTC 毫秒 */
function dateTimeToTsMs(wall: string, tz: string): number | null {
    const m = wall.trim().match(/^(\d{4})-(\d{1,2})-(\d{1,2})[ T](\d{1,2}):(\d{1,2}):(\d{1,2})$/);
    if (!m) return null;
    const y = +m[1], mo = +m[2], d = +m[3], hh = +m[4], mm = +m[5], ss = +m[6];
    const wallAsUTC = Date.UTC(y, mo - 1, d, hh, mm, ss);
    // 两次校正以兼容夏令时边界
    let off = tzOffsetMinutes(wallAsUTC, tz);
    let utc = wallAsUTC - off * 60000;
    off = tzOffsetMinutes(utc, tz);
    utc = wallAsUTC - off * 60000;
    return utc;
}

/* ============ 复制到剪贴板 ============ */
const copyText = async (text: string) => {
    if (!text) return;
    try {
        await navigator.clipboard.writeText(text);
        Message.success("已复制到剪贴板");
    } catch {
        const ta = document.createElement("textarea");
        ta.value = text;
        ta.style.position = "fixed";
        ta.style.opacity = "0";
        document.body.appendChild(ta);
        ta.select();
        document.execCommand("copy");
        document.body.removeChild(ta);
        Message.success("已复制到剪贴板");
    }
};

/* ============ 子视图 ============ */
const view = ref("single");

/* ============ 当前时间戳 ============ */
const nowMs = ref(Date.now());
const nowUnit = ref<"ms" | "s">("ms");
const paused = ref(false);
let timer: ReturnType<typeof setInterval> | null = null;

const displayNow = computed(() =>
    nowUnit.value === "ms" ? String(nowMs.value) : String(Math.floor(nowMs.value / 1000))
);

const startTick = () => {
    stopTick();
    timer = setInterval(() => (nowMs.value = Date.now()), 100);
};
const stopTick = () => {
    if (timer) {
        clearInterval(timer);
        timer = null;
    }
};
const toggleUnit = () => (nowUnit.value = nowUnit.value === "ms" ? "s" : "ms");
const togglePause = () => {
    paused.value = !paused.value;
    paused.value ? stopTick() : startTick();
};
const copyNow = () => copyText(displayNow.value);

onMounted(startTick);
onUnmounted(stopTick);

/* ============ 时间戳 -> 日期时间 ============ */
const tsInput = ref("");
const tsUnit = ref<"s" | "ms">("s");
const tsTz = ref("Asia/Shanghai");
const tsResult = ref("");

/** 当前时间在所选时区下的可读格式 */
const nowText = computed(() => formatInTz(nowMs.value, tsTz.value));

const tsToDate = () => {
    const raw = tsInput.value.trim();
    if (!/^\d+$/.test(raw)) {
        tsResult.value = "";
        Message.warning("请输入合法的时间戳（纯数字）");
        return;
    }
    const ms = tsUnit.value === "s" ? Number(raw) * 1000 : Number(raw);
    tsResult.value = formatInTz(ms, tsTz.value);
};

const fillCurrent = () => {
    tsInput.value = nowUnit.value === "ms" ? String(nowMs.value) : String(Math.floor(nowMs.value / 1000));
    tsUnit.value = nowUnit.value;
    tsToDate();
};

/* ============ 日期时间 -> 时间戳 ============ */
const dateInput = ref("");
const dateTz = ref("Asia/Shanghai");
const dateUnit = ref<"s" | "ms">("s");
const dateResult = ref("");

const dateToTs = () => {
    const ms = dateTimeToTsMs(dateInput.value, dateTz.value);
    if (ms === null) {
        dateResult.value = "";
        Message.warning("请输入合法日期时间，格式 YYYY-MM-DD HH:mm:ss");
        return;
    }
    dateResult.value = dateUnit.value === "s" ? String(Math.floor(ms / 1000)) : String(ms);
};

/* ============ 批量转换 ============ */
const batchInput = ref("");
const batchUnit = ref<"s" | "ms">("s");
const batchTz = ref("Asia/Shanghai");
const batchOutput = ref("");

/** 批量输入的非空行数 */
const batchLines = computed(() => batchInput.value.split(/\r?\n/).filter((s) => s.trim()).length);

const batchConvert = () => {
    const lines = batchInput.value.split(/\r?\n/).map((s) => s.trim()).filter(Boolean);
    if (lines.length === 0) {
        batchOutput.value = "";
        Message.warning("请输入至少一个时间戳");
        return;
    }
    batchOutput.value = lines
        .map((line) => {
            if (!/^\d+$/.test(line)) return `${line}\t->\t无效的时间戳`;
            const ms = batchUnit.value === "s" ? Number(line) * 1000 : Number(line);
            return `${line}\t->\t${formatInTz(ms, batchTz.value)}`;
        })
        .join("\n");
};
</script>

<style scoped>
/* 页面根容器：a-row 铺满工具窗口内容区 */
.ts-page {
    height: 100%;
    min-height: 0;
    flex-wrap: nowrap;
}

/* 左侧列：上下两张卡片各占 50% 高 */
.side-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.side-pages {
    height: calc(100% - 6px);
}

.side-cols {
    height: 50%;
    width: 100%;
    display: flex;
    flex-direction: column;
}

.side-card {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
}

.side-card :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.side-body {
    flex: 1;
    min-height: 0;
    padding: var(--base-padding);
    overflow-y: auto;
}

/* 当前时间戳 */
.now-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: var(--base-padding);
    padding: var(--base-padding);
    overflow-y: auto;
}

.now-value {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
}

.now-num {
    font-size: 24px;
    font-weight: 700;
    line-height: 1.2;
    color: var(--color-text-1);
    font-variant-numeric: tabular-nums;
    word-break: break-all;
}

.now-date {
    color: var(--color-text-3);
    font-size: var(--base-size-small);
}

.tips {
    margin: 0;
    padding-left: 16px;
    color: var(--color-text-2);
    font-size: var(--base-size-small);
    line-height: 2;
}

/* 右侧主列 */
.main-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.main-panel {
    width: 100%;
    height: 100%;
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    background: var(--color-bg-2);
}

.main-panel :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.main-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: var(--base-padding);
}

.fill-row {
    flex: 1;
    min-height: 0;
    flex-wrap: nowrap;
}

.pane-col {
    height: 100%;
    min-width: 0;
}

/* 单个/批量转换面板（嵌套 a-card） */
.pane-card {
    height: 100%;
    display: flex;
    flex-direction: column;
}

.pane-card :deep(.arco-card-body) {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    overflow: hidden;
}

.pane-title {
    display: flex;
    align-items: center;
    gap: 6px;
    font-weight: 600;
    color: var(--color-text-1);
}

.pane-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: var(--base-padding);
    padding: var(--base-padding);
    overflow-y: auto;
}

.pane-body-fill {
    overflow: hidden;
}

.field {
    display: flex;
    flex-direction: column;
    gap: 4px;
}

.field-label {
    color: var(--color-text-2);
    font-size: var(--base-size-small);
}

.pane-result {
    margin-top: auto;
    display: flex;
    flex-direction: column;
    gap: 4px;
}

.result-input :deep(.arco-input) {
    font-family: Consolas, Monaco, "Courier New", monospace;
    font-size: var(--base-size-small);
}

/* 批量转换 */
.batch-view {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.batch-toolbar {
    flex: none;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    flex-wrap: wrap;
}

.batch-tip {
    color: var(--color-text-3);
    font-size: var(--base-size-small);
}

.fill-textarea {
    flex: 1;
    min-height: 0;
    height: 100%;
}

.fill-textarea :deep(.arco-textarea) {
    height: 100%;
    resize: none;
    font-family: Consolas, Monaco, "Courier New", monospace;
    font-size: var(--base-size-small);
}
</style>
