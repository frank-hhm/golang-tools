<template>
    <!-- 加速站名单：已添加的用表格管，每行可以单独测「这个站到底能不能下下数据」 -->
    <div class="mt-root">
        <a-space :size="6" class="mt-add">
            <a-input v-model="addrInput" size="small" class="mono mt-addr" placeholder="https://docker.m.daocloud.io"
                allow-clear @press-enter="add" />
            <a-button size="small" @click="add">
                <template #icon><icon-plus /></template>添加
            </a-button>
        </a-space>

        <!-- scroll.x 必须有，否则操作列的 fixed="right" 不生效 -->
        <a-table class="mt-table" :data="rows" :pagination="false" size="small" row-key="url" :bordered="false"
            :scroll="{ x: '100%' }">
            <template #empty>
                <a-empty :image="false" description="未配置（直连 registry-1.docker.io，很容易超时）" />
            </template>
            <template #columns>
                <a-table-column title="#" :width="42">
                    <template #cell="{ rowIndex }">{{ rowIndex + 1 }}</template>
                </a-table-column>
                <a-table-column title="加速站地址" ellipsis tooltip>
                    <template #cell="{ record }">
                        <span class="mono">{{ record.url }}</span>
                    </template>
                </a-table-column>
                <a-table-column title="测速结论" :width="300">
                    <template #cell="{ record }">
                        <!-- 正在测的那一行先说「测速中」，否则用户会对着上一次的结论/未测等结果 -->
                        <span v-if="probing(record.url)" class="com-state is-busy">
                            <com-badge status="processing" :animated="true" />
                            测速中…
                        </span>
                        <a-tooltip v-else-if="record.p" position="tl" :content-style="{ maxWidth: '420px' }">
                            <template #content>
                                <div class="tip-lines">
                                    <div class="lt-line">{{ record.p.label }}</div>
                                    <div v-if="record.p.hint" class="lt-line">{{ record.p.hint }}</div>
                                    <div class="lt-line">取样：{{ record.p.sample || '-' }}（取最大的一层 {{
                                        fmtBytes(record.p.blobSize) }}，实测层 {{ record.p.layer || '-' }}）</div>
                                    <div class="lt-line">这一步走到：{{ stageText(record.p.stage) }}{{
                                        record.p.probedAt ? ' · ' + record.p.probedAt : '' }}</div>
                                </div>
                            </template>
                            <!-- 结论用圆点表达（颜色同 a-tag 的语义色），测得的数字沉到第二行，300px 也不会挤成一行 -->
                            <div class="verdict">
                                <span class="com-state" :class="gradeClass(record.p.grade)">
                                    <com-badge :status="gradeStatus(record.p.grade)" />
                                    {{ gradeText(record.p.grade) }}
                                </span>
                                <span class="nums">连通 {{ ms(record.p.connectMs) }} · 首字节 {{ ms(record.p.ttfbMs) }} ·
                                    {{ rate(record.p.mbps) }}</span>
                            </div>
                        </a-tooltip>
                        <span v-else class="com-state is-dim">
                            <com-badge status="normal" />
                            未测：点右侧「测速」
                        </span>
                    </template>
                </a-table-column>
                <!-- 固定列宽度按实际按钮排布给足：测速 / 上移 / 下移 / 删除 -->
                <a-table-column title="操作" :width="190" align="right" fixed="right">
                    <template #cell="{ record, rowIndex }">
                        <a-space>
                            <a-tooltip content="测这个站能不能下动数据" position="tl">
                                <a-button size="small" :loading="pending === record.url"
                                    :disabled="!!pending && pending !== record.url" @click="probeOne(record.url)">
                                    <template #icon><icon-thunderbolt /></template>测速
                                </a-button>
                            </a-tooltip>
                            <a-tooltip content="上移（第一位是线上默认来源）" position="tl">
                                <a-button size="small" :disabled="rowIndex === 0 || pending !== ''"
                                    @click="move(rowIndex, -1)">
                                    <template #icon><icon-arrow-up /></template>
                                </a-button>
                            </a-tooltip>
                            <a-tooltip content="下移" position="tl">
                                <a-button size="small"
                                    :disabled="rowIndex === rows.length - 1 || pending !== ''" @click="move(rowIndex, 1)">
                                    <template #icon><icon-arrow-down /></template>
                                </a-button>
                            </a-tooltip>
                            <a-popconfirm :content="`从 daemon.json 的加速站名单里移除「${record.url}」？`" type="warning"
                                position="lt" @ok="remove(rowIndex)">
                                <a-button size="small" status="danger">
                                    <template #icon><icon-delete /></template>
                                </a-button>
                            </a-popconfirm>
                        </a-space>
                    </template>
                </a-table-column>
            </template>
        </a-table>

        <a-space :size="6" class="mt-tools">
            <a-input v-model="sampleInput" size="small" class="mt-sample" placeholder="取样镜像，默认 library/alpine"
                allow-clear />
            <a-button size="small" :loading="pending === '*'" :disabled="!rows.length"
                @click="probeAll">
                <template #icon><icon-thunderbolt /></template>全部测速
            </a-button>
            <a-button size="small" :disabled="!graded().length" @click="sortBySpeed">
                <template #icon><icon-sort /></template>按测速结果排序
            </a-button>
            <a-button size="small" :disabled="!Object.keys(results).length" @click="results = {}">
                <template #icon><icon-refresh /></template>清掉结果
            </a-button>
        </a-space>

        <a-typography-text type="secondary" class="mt-tip">
            测速走的是 daemon 真正拉取的那条路径：/v2/ → 清单 → 层数据前 512KB，只读不写，也不动引擎配置。
            反复卡住某个镜像时，把左边的「取样镜像」填成它的名字（如 redis）再测，就能看出是不是这一层没人给数据。
            顺序即线上来源的顺序，第一行也是默认来源；改完记得保存并重启引擎。
        </a-typography-text>
    </div>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { fmtBytes } from "../helpers";

const props = defineProps<{
    /** daemon.json 里的 registry-mirrors 草稿（未保存） */
    modelValue: string[];
}>();

const emit = defineEmits<{
    (e: "update:modelValue", v: string[]): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const addrInput = ref("");
const sampleInput = ref("");
/** 每个地址最近一次的探测结果（key 是地址原文，重排/删除都不会串行） */
const results = ref<Record<string, any>>({});
/** '' 空闲 / 具体地址 单测中 / "*" 全部测速中 */
const pending = ref("");

const rows = computed<any[]>(() =>
    (props.modelValue || []).map((url) => ({ url, p: results.value[url] || null }))
);

function add() {
    const v = addrInput.value.trim().replace(/\/+$/, "");
    if (!v) return;
    if (!/^https?:\/\//i.test(v)) {
        Message.warning("加速站要带协议，如 https://docker.m.daocloud.io");
        return;
    }
    if ((props.modelValue || []).includes(v)) {
        Message.info("名单里已经有这个地址");
        addrInput.value = "";
        return;
    }
    emit("update:modelValue", [...(props.modelValue || []), v]);
    addrInput.value = "";
}

function move(i: number, d: number) {
    const list = [...(props.modelValue || [])];
    const j = i + d;
    if (j < 0 || j >= list.length) return;
    [list[i], list[j]] = [list[j], list[i]];
    emit("update:modelValue", list);
}

function remove(i: number) {
    const list = [...(props.modelValue || [])];
    list.splice(i, 1);
    emit("update:modelValue", list);
}

async function probeOne(url: string) {
    pending.value = url;
    try {
        const p = await GoTools.DockerSrvProbeMirror(url, sampleInput.value.trim());
        results.value = { ...results.value, [url]: p };
        if (p?.grade === "fail" || p?.grade === "empty") Message.warning(`${hostOf(url)}：${p?.label || "探不通"}`);
        else Message.success(`${hostOf(url)}：${p?.label || "测完了"}`);
    } catch (e: any) {
        Message.error("测速失败: " + String(e?.message || e));
    }
    pending.value = "";
}

async function probeAll() {
    const list = [...(props.modelValue || [])];
    if (!list.length) return;
    pending.value = "*";
    try {
        const ps = (await GoTools.DockerSrvProbeMirrors(list, sampleInput.value.trim())) || [];
        const next: Record<string, any> = {};
        for (const p of ps) if (p?.url) next[p.url] = p;
        results.value = { ...results.value, ...next };
        const bad = list.filter((u) => ["fail", "empty"].includes(next[u]?.grade));
        if (bad.length) {
            Message.warning(`${list.length} 个站里有 ${bad.length} 个不能正常下载：${bad.map(hostOf).join("、")}`);
        } else {
            Message.success(`${list.length} 个站都能下到数据`);
        }
    } catch (e: any) {
        Message.error("测速失败: " + String(e?.message || e));
    }
    pending.value = "";
}

/** 显式点一下才重排：不背着自己改顺序，用户配的优先级才有据可查 */
function sortBySpeed() {
    const rank = (u: string) => {
        const g = results.value[u]?.grade;
        if (!g) return 9; // 没测过的排最后，不冒充好站
        if (g === "ok") return 1;
        if (g === "slow") return 2;
        if (g === "empty") return 3;
        return 4;
    };
    const rate = (u: string) => Number(results.value[u]?.mbps || 0);
    const list = [...(props.modelValue || [])].sort(
        (a, b) => rank(a) - rank(b) || rate(b) - rate(a) || a.localeCompare(b)
    );
    emit("update:modelValue", list);
    Message.info("已按测速结果排序：能下且快的在前，没测过的沉底");
}

const graded = () => (props.modelValue || []).filter((u) => !!results.value[u]);

/** 这一行是否正在测（单测命中它，或者“全部测速”铺开） */
function probing(url: string) {
    return pending.value === url || pending.value === "*";
}

function hostOf(u: string) {
    return String(u).replace(/^https?:\/\//i, "").replace(/\/+$/, "");
}

const gradeText = (g: string) =>
    g === "ok" ? "可下载" : g === "slow" ? "能下但慢" : g === "empty" ? "不给数据" : g === "fail" ? "不可用" : "未知";
/** 结论语义色：圆点与文字共用一套，替代原来的 a-tag color */
const gradeStatus = (g: string): "success" | "warning" | "danger" | "normal" =>
    g === "ok" ? "success" : g === "slow" ? "warning" : g === "fail" || g === "empty" ? "danger" : "normal";
const gradeClass = (g: string) =>
    g === "ok" ? "is-on" : g === "slow" ? "is-warn" : g === "fail" || g === "empty" ? "is-off" : "is-dim";

const stageText = (s: string) =>
    s === "url" ? "地址本身" : s === "connect" ? "/v2/ 连通" : s === "manifest" ? "取清单" : s === "blob" ? "下层数据" : "-";

const ms = (n: number) => (!n ? "-" : n >= 1000 ? (n / 1000).toFixed(1) + "s" : n + "ms");
const rate = (m: number) => (!m ? "-" : m.toFixed(1) + " Mbps");
</script>

<style scoped>
.mt-root {
    display: flex;
    flex-direction: column;
}

.mt-add {
    margin-bottom: 8px;
}

.mt-addr {
    flex: 1 1 300px;
    min-width: 220px;
}

/* 表格按内容给高：行少时不空出一大块，行多时跟着面板一起滚 */
.mt-table {
    flex: 0 0 auto;
}

.verdict {
    display: flex;
    flex-direction: column;
    gap: 2px;
}

.nums {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.mt-tools {
    margin-top: 8px;
}

.mt-sample {
    flex: 0 0 230px;
    width: 230px;
}

.mt-tip {
    display: block;
    margin-top: 8px;
    font-size: var(--base-size-small);
    line-height: 1.7;
}

.mono {
    font-family: Consolas, monospace;
}
</style>
