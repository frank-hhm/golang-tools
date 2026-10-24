<template>
    <a-row class="hex-page" :gutter="6">
        <!-- 左侧约 30%：输入（上50%）+ 简介（下50%） -->
        <a-col :span="6" class="side-col">
            <a-row class="side-pages">
                <a-col class="side-cols" :span="24">
                    <a-card class="side-card" size="small" :body-style="{ padding: 0 }">
                        <template #title>输入</template>
                        <template #extra>
                            <a-tag size="small" color="arcoblue">{{ inputBase }} 进制</a-tag>
                        </template>
                        <div class="side-body">
                            <div class="field">
                                <div class="field-label">常用进制</div>
                                <a-radio-group v-model="inputBase" class="base-radios">
                                    <a-radio v-for="b in COMMON_BASES" :key="b" :value="b">{{ b }}</a-radio>
                                </a-radio-group>
                            </div>
                            <div class="field">
                                <div class="field-label">更多进制（2 ~ 62）</div>
                                <a-select v-model="inputBase" style="width: 100%">
                                    <a-option v-for="b in ALL_BASES" :key="b" :value="b">{{ b }}</a-option>
                                </a-select>
                            </div>
                            <div class="field">
                                <div class="field-label">数值</div>
                                <a-input v-model="inputStr" :placeholder="`请输入 ${inputBase} 进制数`" allow-clear
                                    @press-enter="convert" />
                            </div>
                            <a-button type="primary" status="success" long @click="convert">
                                <template #icon><icon-swap /></template>转换
                            </a-button>

                            <div class="field-label">二进制选项</div>
                            <div class="bin-opts">
                                <div class="bin-row">
                                    <a-checkbox v-model="padEnabled">补 0 至</a-checkbox>
                                    <a-select v-model="padBits" :disabled="!padEnabled" size="small" style="flex: 1">
                                        <a-option :value="8">8 位</a-option>
                                        <a-option :value="16">16 位</a-option>
                                        <a-option :value="32">32 位</a-option>
                                        <a-option :value="64">64 位</a-option>
                                    </a-select>
                                </div>
                                <a-radio-group v-model="endian" type="button" size="mini">
                                    <a-radio value="big">大端序</a-radio>
                                    <a-radio value="little">小端序</a-radio>
                                </a-radio-group>
                            </div>
                        </div>
                    </a-card>
                </a-col>
                <a-col class="side-cols mt6" :span="24">
                    <a-card class="side-card" size="small" :body-style="{ padding: 0 }">
                        <template #title>简介</template>
                        <div class="side-body">
                            <p class="intro">
                                进制其实是一种记数的方法，所以也称为进位记数法/值计数法，可以用有限的数字符号代表所有的数值。
                                可使用数字符号的数目称为基数（英文：radix）或底数，基数为 n，即可称 n 进制，简称 n 进制。
                                例如平常生活中我们经常用到的十进制，就是使用 10 个阿拉伯数字 0-9 进行记数，所以它的基数就是 10，称为十进制。
                            </p>
                        </div>
                    </a-card>
                </a-col>
            </a-row>
        </a-col>

        <!-- 右侧：转换结果表 -->
        <a-col :span="18" class="main-col">
            <a-card class="main-panel" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space :size="8">
                        <span>转换结果</span>
                        <a-tag size="small">共 {{ rows.length }} 项</a-tag>
                    </a-space>
                </template>
                <template #extra>
                    <a-tag size="small" color="gray">输入 {{ inputBase }} 进制</a-tag>
                </template>
                <div class="result-body">
                    <a-table class="result-table" :data="rows" :pagination="false" :loading="false" row-key="base"
                        :scroll="{ y: '100%' }"
                        :row-class="(record) => (record.base === inputBase ? 'row-active' : '')">
                        <template #columns>
                            <a-table-column title="进制" data-index="base" :width="80" />
                            <a-table-column title="结果">
                                <template #cell="{ record }">
                                    <a-input :model-value="record.value" readonly @click="selectAll">
                                        <template #append>
                                            <a-button :disabled="!record.value" @click="copyText(record.value)">
                                                <template #icon><icon-copy /></template>
                                            </a-button>
                                        </template>
                                    </a-input>
                                </template>
                            </a-table-column>
                            <a-table-column title="解释" data-index="explain" :width="240" />
                        </template>
                    </a-table>
                </div>
            </a-card>
        </a-col>
    </a-row>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import { Message } from "@arco-design/web-vue";

/** 常用输入进制（单选项） */
const COMMON_BASES = [2, 8, 10, 16, 32, 36, 58, 62];
/** 全部可选进制（更多下拉：2 ~ 62） */
const ALL_BASES = Array.from({ length: 61 }, (_, i) => i + 2);
/** 结果表展示的目标进制 */
const TARGET_BASES = [2, 8, 10, 16, 26, 32, 36, 52, 58, 62];

/** 各进制的解释 */
const EXPLAIN: Record<number, string> = {
    26: "小写字母",
    32: "数字 + 大写字母，不包含 ILOU 字符",
    36: "数字 + 小写字母",
    52: "大小写字母",
    58: "数字 + 大小写字母，不包含 0O1I 字符",
    62: "数字 + 大小写字母",
};

/* ============ 状态 ============ */
const inputBase = ref(62);
const inputStr = ref("");
const resultValue = ref<bigint | null>(null);

/** 二进制专属选项 */
const padEnabled = ref(false);
const padBits = ref(32);
const endian = ref<"big" | "little">("big");

/* ============ 字符集（不同进制采用业界常用字母表） ============ */
function getAlphabet(base: number): string {
    switch (base) {
        case 16:
            return "0123456789abcdef";
        case 26:
            return "abcdefghijklmnopqrstuvwxyz";
        case 32:
            return "0123456789ABCDEFGHJKMNPQRSTVWXYZ"; // Crockford Base32
        case 36:
            return "0123456789abcdefghijklmnopqrstuvwxyz";
        case 52:
            return "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ";
        case 58:
            return "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"; // Bitcoin Base58
        case 62:
            return "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz";
        default:
            return "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ".slice(0, base);
    }
}

/** 字符串 -> BigInt（按输入进制解析） */
function parseToBigInt(str: string, base: number): bigint | null {
    const alpha = getAlphabet(base);
    const map = new Map<string, number>();
    for (let i = 0; i < alpha.length; i++) map.set(alpha[i], i);
    // 小写字母表进制（如 16/36）额外接受大写输入
    for (const [k, v] of Array.from(map)) {
        if (k >= "a" && k <= "z") map.set(k.toUpperCase(), v);
    }
    let s = str.trim();
    if (!s) return null;
    let neg = false;
    if (s[0] === "-") {
        neg = true;
        s = s.slice(1);
    }
    if (!s) return null;
    const b = BigInt(base);
    let result = 0n;
    for (const ch of s) {
        const v = map.get(ch);
        if (v === undefined) return null;
        result = result * b + BigInt(v);
    }
    return neg ? -result : result;
}

/** BigInt -> 指定进制字符串 */
function formatBigInt(value: bigint, base: number): string {
    const alpha = getAlphabet(base);
    if (value === 0n) return alpha[0];
    const neg = value < 0n;
    let v = neg ? -value : value;
    const b = BigInt(base);
    let out = "";
    while (v > 0n) {
        out = alpha[Number(v % b)] + out;
        v /= b;
    }
    return (neg ? "-" : "") + out;
}

/** 二进制结果（含补位与字节序处理） */
function formatBinary(value: bigint): string {
    let bin = formatBigInt(value, 2);
    if (padEnabled.value && bin.length < padBits.value) {
        bin = bin.padStart(padBits.value, "0");
    }
    if (endian.value === "little") {
        let b = bin;
        const rem = b.length % 8;
        if (rem) b = b.padStart(b.length + (8 - rem), "0");
        const bytes = b.match(/.{8}/g) || [];
        bin = bytes.reverse().join("");
    }
    return bin;
}

/* ============ 表格数据（响应式） ============ */
const rows = computed(() =>
    TARGET_BASES.map((base) => {
        let value = "";
        if (resultValue.value !== null) {
            value = base === 2 ? formatBinary(resultValue.value) : formatBigInt(resultValue.value, base);
        }
        return { base, value, explain: EXPLAIN[base] ?? "" };
    })
);

/* ============ 交互 ============ */
const convert = () => {
    const v = parseToBigInt(inputStr.value, inputBase.value);
    if (v === null) {
        resultValue.value = null;
        Message.error(`请输入合法的 ${inputBase.value} 进制数`);
        return;
    }
    resultValue.value = v;
};

const selectAll = (e: Event) => (e.target as HTMLInputElement)?.select?.();

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
</script>

<style scoped>
/* 页面根容器：a-row 铺满工具窗口内容区 */
.hex-page {
    height: 100%;
    min-height: 0;
    flex-wrap: nowrap;
    background: var(--color-fill-1);
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
    display: flex;
    flex-direction: column;
    gap: var(--base-padding);
    padding: var(--base-padding);
    overflow-y: auto;
}

.field {
    display: flex;
    flex-direction: column;
    gap: 4px;
}

.field-label {
    color: var(--color-text-2);
    font-size: var(--base-size-small);
    font-weight: 500;
}

.base-radios {
    display: flex;
    flex-wrap: wrap;
    gap: 4px var(--base-padding);
}

.base-radios :deep(.arco-radio) {
    margin-right: 0;
}

.bin-opts {
    display: flex;
    flex-direction: column;
    gap: var(--base-radius-default);
}

.bin-row {
    display: flex;
    align-items: center;
    gap: var(--base-radius-default);
}

.intro {
    color: var(--color-text-2);
    font-size: var(--base-size-small);
    line-height: 1.8;
    margin: 0;
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

.result-body {
    flex: 1;
    min-height: 0;
    display: flex;
    flex-direction: column;
    padding: var(--base-padding);
}

.result-table {
    flex: 1;
    min-height: 0;
    overflow: hidden;
}

/* a-table 内部逐层撑满：表头固定、表体滚动 */
.result-table :deep(.arco-spin) {
    height: 100%;
    display: flex;
    flex-direction: column;
}

.result-table :deep(.arco-table-container) {
    flex: 1;
    min-height: 0;
}

.result-table :deep(.arco-table-body) {
    flex: 1;
    min-height: 0;
}

:deep(.row-active) {
    background-color: rgba(var(--cyan-6), 0.1);
}
</style>
