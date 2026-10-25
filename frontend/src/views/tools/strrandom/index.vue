<template>
    <a-row class="str-page" :gutter="6">
        <!-- 左侧约 25%：生成设置 -->
        <a-col :span="6" class="side-col">
            <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                <template #title>生成设置</template>
                <template #extra>
                    <a-tag size="small">{{ charset.length }} 字符</a-tag>
                </template>
                <div class="side-body">
                    <div class="field">
                        <a-space>
                            <a-radio-group size="medium" v-model="lenMode" type="button">
                                <a-radio value="fixed">固定</a-radio>
                                <a-radio value="range">区间</a-radio>
                            </a-radio-group>
                        </a-space>
                        <a-input-number v-if="lenMode === 'fixed'" v-model="fixedLen" :min="1" :max="1000"
                            :precision="0" style="width: 100%" />
                        <a-row v-else :gutter="8" align="center">
                            <a-col :span="11">
                                <a-input-number v-model="minLen" :min="1" :max="1000" :precision="0"
                                    style="width: 100%" />
                            </a-col>
                            <a-col :span="2" class="tilde">~</a-col>
                            <a-col :span="11">
                                <a-input-number v-model="maxLen" :min="1" :max="1000" :precision="0"
                                    style="width: 100%" />
                            </a-col>
                        </a-row>
                    </div>

                    <div class="field">
                        <div class="field-label">字符类型</div>
                        <div class="checks">
                            <a-checkbox v-model="useDigits"><span class="text-grey">数字 0-9</span></a-checkbox>
                            <a-checkbox v-model="useLower"><span class="text-grey">小写字母 a-z</span></a-checkbox>
                            <a-checkbox v-model="useUpper"><span class="text-grey">大写字母 A-Z</span></a-checkbox>
                            <a-checkbox v-model="useSymbols"><span class="text-grey">常用符号 ~!@#$%^&amp;*()_+</span></a-checkbox>
                        </div>
                    </div>

                    <div class="field">
                        <div class="field-label">字符集（可手动编辑）</div>
                        <a-input v-model="charset" placeholder="生成时使用的字符集" allow-clear />
                    </div>

                    <div class="field">
                        <div class="field-label">生成数量</div>
                        <a-input-number v-model="count" :min="1" :max="1000" :precision="0" style="width: 100%" />
                    </div>

                    <a-button size="small" status="success" long class="gen-btn" @click="generate">
                        <template #icon><icon-refresh /></template>生成
                    </a-button>
                </div>
            </a-card>
        </a-col>

        <!-- 右侧：生成结果 -->
        <a-col :span="18" class="main-col">
            <a-card class="main-panel card-head-tool" :body-style="{ padding: 0 }">
                <template #title>
                    <a-space :size="8">
                        <span>生成结果</span>
                        <a-tag size="small">{{ resultLines }} 行</a-tag>
                        <a-tag size="small">{{ result.length }} 字符</a-tag>
                    </a-space>
                </template>
                <template #extra>
                    <a-button size="small" :disabled="!result" @click="copyText(result)">
                        <template #icon><icon-copy /></template>复制全部
                    </a-button>
                </template>
                <div class="panel-body">
                    <a-textarea v-if="result" v-model="result" class="fill-textarea" readonly />
                    <div v-else class="panel-empty">
                        <a-empty description="点击左侧「生成」后这里显示结果" />
                    </div>
                </div>
            </a-card>
        </a-col>
    </a-row>
</template>

<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";

/** 各类字符集常量 */
const DIGITS = "0123456789";
const LOWER = "abcdefghijklmnopqrstuvwxyz";
const UPPER = "ABCDEFGHIJKLMNOPQRSTUVWXYZ";
const SYMBOLS = "~!@#$%^&*()_+";

/** 表单状态 */
const lenMode = ref<"fixed" | "range">("fixed");
const fixedLen = ref(16);
const minLen = ref(16);
const maxLen = ref(20);
const count = ref(5);
const charset = ref("");
const result = ref("");

const useDigits = ref(true);
const useLower = ref(true);
const useUpper = ref(true);
const useSymbols = ref(true);

/** 结果行数（用于标题统计） */
const resultLines = computed(() => (result.value ? result.value.split("\n").length : 0));

/** 根据勾选项重建字符集（手动编辑字符集后再改动勾选会覆盖，属预期行为） */
const rebuildCharset = () => {
    let s = "";
    if (useDigits.value) s += DIGITS;
    if (useLower.value) s += LOWER;
    if (useUpper.value) s += UPPER;
    if (useSymbols.value) s += SYMBOLS;
    charset.value = s;
};

watch([useDigits, useLower, useUpper, useSymbols], rebuildCharset);
rebuildCharset();

/** 生成 [0, maxExclusive) 区间内的安全随机整数（优先使用 crypto） */
function randomInt(maxExclusive: number): number {
    if (maxExclusive <= 0) return 0;
    if (typeof crypto !== "undefined" && crypto.getRandomValues) {
        const range = 0x100000000;
        const limit = range - (range % maxExclusive);
        const arr = new Uint32Array(1);
        let x = 0;
        do {
            crypto.getRandomValues(arr);
            x = arr[0];
        } while (x >= limit);
        return x % maxExclusive;
    }
    return Math.floor(Math.random() * maxExclusive);
}

/** 生成随机字符串 */
const generate = () => {
    const pool = Array.from(charset.value);
    if (pool.length === 0) {
        result.value = "";
        Message.warning("字符集不能为空");
        return;
    }
    let min: number;
    let max: number;
    if (lenMode.value === "fixed") {
        min = Math.floor(fixedLen.value) || 1;
        if (min < 1) min = 1;
        max = min;
    } else {
        min = Math.floor(minLen.value) || 1;
        max = Math.floor(maxLen.value) || 1;
        if (min < 1) min = 1;
        if (max < min) {
            Message.warning("最大长度不能小于最小长度");
            return;
        }
    }
    const n = Math.min(Math.max(1, Math.floor(count.value) || 1), 1000);

    const out: string[] = [];
    for (let i = 0; i < n; i++) {
        const len = min + randomInt(max - min + 1);
        let s = "";
        for (let j = 0; j < len; j++) {
            s += pool[randomInt(pool.length)];
        }
        out.push(s);
    }
    result.value = out.join("\n");
};

/** 复制到剪贴板 */
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
.str-page {
    height: 100%;
    min-height: 0;
    flex-wrap: nowrap;
}

/* 左侧设置卡片：占满整列高度，内部滚动 */
.side-col {
    height: 100%;
    min-width: 0;
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
    gap: 6px;
}

.field-label {
    color: var(--color-text-2);
    font-size: var(--base-size-small);
    font-weight: 500;
}

.checks {
    display: flex;
    flex-direction: column;
    gap: 6px;
}

.tilde {
    text-align: center;
    color: var(--color-text-3);
}

.gen-btn {
    margin-top: auto;
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

.panel-body {
    flex: 1;
    min-height: 0;
    display: flex;
    padding: var(--base-padding);
}

.panel-empty {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
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
