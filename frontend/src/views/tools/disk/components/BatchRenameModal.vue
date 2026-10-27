<template>
    <!-- 批量重命名：按「固定文字 + 规则片段（自增 / 随机数字 / 随机字母，可多选）」给多个文件/目录改名，
         实时预览结果，任一行非法/重名则不允许应用。仅用于同一目录下的多项。 -->
    <a-modal :visible="visible" title="批量重命名" :width="780" :mask-closable="false" :ok-loading="applying"
        :ok-button-props="{ disabled: !canApply }" cancel-text="取消" ok-text="应用" :esc-to-close="!applying"
        :on-before-ok="apply" @cancel="close">
        <div class="br-wrap">
            <!-- 左侧：规则配置 -->
            <div class="br-form">
                <div class="br-tip">已选 <b>{{ entries.length }}</b> 项，按规则批量改名（保留各自扩展名）。</div>

                <div class="br-label">命名结构</div>
                <a-radio-group v-model="form.layout" size="mini">
                    <a-radio value="prefix">固定前缀 + 规则后缀</a-radio>
                    <a-radio value="suffix">规则前缀 + 固定后缀</a-radio>
                </a-radio-group>

                <div class="br-label">固定文字</div>
                <a-input v-model="form.fixed" size="small" allow-clear
                    :placeholder="form.layout === 'prefix' ? '如 报告_（作为前缀）' : '如 _备份（作为后缀）'" />

                <div class="br-label">规则片段（可多选，按下方顺序拼接）</div>
                <a-checkbox-group v-model="form.modes">
                    <a-checkbox value="inc">自增序号</a-checkbox>
                    <a-checkbox value="digits">随机数字</a-checkbox>
                    <a-checkbox value="letters">随机字母</a-checkbox>
                </a-checkbox-group>

                <!-- 选中片段的参数 -->
                <div v-if="form.modes.includes('inc')" class="br-opts">
                    <span class="br-opt">起始
                        <a-input-number v-model="form.incStart" :min="0" :max="999999" size="mini" :step="1"
                            :precision="0" style="width: 88px" />
                    </span>
                    <a-checkbox v-model="form.incPad" size="small">前导补零</a-checkbox>
                </div>
                <div v-if="form.modes.includes('digits')" class="br-opts">
                    <span class="br-opt">位数
                        <a-input-number v-model="form.digitsLen" :min="1" :max="16" size="mini" :step="1"
                            :precision="0" style="width: 72px" />
                    </span>
                </div>
                <div v-if="form.modes.includes('letters')" class="br-opts">
                    <span class="br-opt">位数
                        <a-input-number v-model="form.lettersLen" :min="1" :max="16" size="mini" :step="1"
                            :precision="0" style="width: 72px" />
                    </span>
                    <a-radio-group v-model="form.letterCase" size="mini">
                        <a-radio value="lower">小写</a-radio>
                        <a-radio value="upper">大写</a-radio>
                        <a-radio value="mixed">混合</a-radio>
                    </a-radio-group>
                </div>

                <div class="br-label">连接符</div>
                <a-input v-model="form.sep" size="small" :max-length="3" placeholder="可留空" style="width: 120px" />

                <div class="br-example">示例：<b>{{ exampleText }}</b></div>
            </div>

            <!-- 右侧：实时预览 -->
            <div class="br-preview">
                <div class="br-preview-head">
                    <span>预览</span>
                    <span v-if="errorCount" class="br-err-count">{{ errorCount }} 项有问题</span>
                    <span v-else-if="preview.length" class="br-ok-count">{{ preview.length }} 项将改名</span>
                </div>
                <div v-if="!form.modes.length && !form.fixed.trim()" class="br-empty">
                    请至少填写固定文字或选择一个规则片段
                </div>
                <div v-else-if="!preview.length" class="br-empty">没有需要改名的项目（新名与原名相同）</div>
                <table v-else class="br-table">
                    <thead>
                        <tr>
                            <th>原名称</th>
                            <th>新名称</th>
                            <th class="br-st">状态</th>
                        </tr>
                    </thead>
                    <tbody>
                        <tr v-for="r in preview" :key="r.path" :class="{ 'is-err': r.error }">
                            <td class="br-old" :title="r.oldName">{{ r.oldName }}</td>
                            <td class="br-new" :title="r.newName">{{ r.newName }}</td>
                            <td class="br-st">
                                <span v-if="r.error" class="br-msg">⚠ {{ r.error }}</span>
                                <span v-else class="br-msg ok">✓</span>
                            </td>
                        </tr>
                    </tbody>
                </table>
            </div>
        </div>
        <div class="modal-hint">
            新名保留各自扩展名（目录不加）；顺序即列表显示顺序。名称不能为空、不能含
            <code>\ / : * ? " &lt; &gt; |</code>，也不能以空格或点结尾，且不能与目录内其它项重名。
        </div>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, reactive, ref } from "vue";
import { Message } from "@arco-design/web-vue";

/**
 * 批量重命名弹窗（纯前端拼规则 + 逐个调 DiskRename）。
 * 结构：固定文字（前缀或后缀）+ 规则片段（自增 / 随机数字 / 随机字母，可多选，按固定顺序拼接）。
 */

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const props = withDefaults(defineProps<{
    visible: boolean;
    /** 待改名的条目：{ path, name, isDir }，按列表显示顺序 */
    entries: any[];
    /** 当前目录（仅提示用；同目录才允许批量改名） */
    dir?: string;
    /** 目录内全部条目名，用于重名校验 */
    existingNames?: string[];
}>(), { dir: "", existingNames: () => [] });

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "done", n: number): void;
}>();

const form = reactive({
    layout: "prefix" as "prefix" | "suffix",
    fixed: "",
    modes: ["inc"] as ("inc" | "digits" | "letters")[],
    incStart: 1,
    incPad: true,
    digitsLen: 4,
    lettersLen: 4,
    letterCase: "lower" as "lower" | "upper" | "mixed",
    sep: "_",
});

const DIGITS = "0123456789";
const LETTERS_LOWER = "abcdefghijklmnopqrstuvwxyz";
const LETTERS_UPPER = LETTERS_LOWER.toUpperCase();

function pick(chars: string, n: number): string {
    let s = "";
    for (let i = 0; i < n; i++) s += chars[Math.floor(Math.random() * chars.length)];
    return s;
}

/** 拆基名与扩展名：目录、点开头隐藏文件、无扩展名的都整体作基名 */
function splitName(name: string, isDir: boolean): { base: string; ext: string } {
    if (isDir) return { base: name, ext: "" };
    const dot = name.lastIndexOf(".");
    if (dot <= 0) return { base: name, ext: "" };
    return { base: name.slice(0, dot), ext: name.slice(dot) };
}

const letterSet = computed(() =>
    form.letterCase === "upper" ? LETTERS_UPPER : form.letterCase === "mixed" ? LETTERS_LOWER + LETTERS_UPPER : LETTERS_LOWER
);

// 自增位数：按参与改名的行数与起始值决定（不依赖 preview，避免循环依赖）
const incWidth = computed(() => {
    const maxVal = (Number(form.incStart) || 0) + Math.max(0, props.entries.length - 1);
    return String(maxVal).length;
});

/** 用连接符拼接两段：某段为空时不留下多余连接符 */
function join(sep: string, a: string, b: string): string {
    if (!a) return b;
    if (!b) return a;
    return a + sep + b;
}

/** 规则片段：自增用行号 seq；随机数字/字母按位数现取（可多选时按 自增→数字→字母 顺序拼接） */
function varPart(seq: number, cs: string): string {
    const parts: string[] = [];
    if (form.modes.includes("inc")) {
        let s = String(seq);
        if (form.incPad && s.length < incWidth.value) s = s.padStart(incWidth.value, "0");
        parts.push(s);
    }
    if (form.modes.includes("digits")) parts.push(pick(DIGITS, Math.max(1, Number(form.digitsLen) || 1)));
    if (form.modes.includes("letters")) parts.push(pick(cs, Math.max(1, Number(form.lettersLen) || 1)));
    return parts.join(form.sep);
}

const BAD_CHARS = /[\\/:*?"<>|]/;
const RESERVED = /^(con|prn|aux|nul|com[1-9]|lpt[1-9])$/i;

function nameError(n: string): string {
    const t = n.trim();
    if (!t) return "名称为空";
    if (BAD_CHARS.test(t)) return "含非法字符";
    if (/[ .]$/.test(n)) return "以空格或点结尾";
    if ([...n].length > 200) return "名称过长";
    if (RESERVED.test(t.split(".")[0])) return "Windows 保留名";
    return "";
}

type PreviewRow = { path: string; oldName: string; newName: string; error: string };

const preview = computed<PreviewRow[]>(() => {
    const rows: PreviewRow[] = [];
    const sep = form.sep;
    const fixed = form.fixed;
    const cs = letterSet.value;
    const hasRand = form.modes.includes("digits") || form.modes.includes("letters");
    // 重名判定：目录内未被本次改名占用的名字 + 本次已生成的新名
    const renaming = new Set(props.entries.map((e) => String(e.name).toLowerCase()));
    const taken = new Set<string>();
    for (const nm of props.existingNames || []) {
        if (!renaming.has(String(nm).toLowerCase())) taken.add(String(nm).toLowerCase());
    }
    let seq = Number(form.incStart) || 0;
    for (const e of props.entries || []) {
        const path = String(e.path);
        const oldName = String(e.name);
        const { ext } = splitName(oldName, !!e.isDir);
        let nb = "";
        let error = "";
        for (let attempt = 0; ; attempt++) {
            const vp = varPart(seq, cs);
            nb = (form.layout === "prefix" ? join(sep, fixed, vp) : join(sep, vp, fixed)) + ext;
            error = nameError(nb);
            // 重名且含随机片段：重取随机再试（自增本身保证唯一，无需重试）
            if (!error && taken.has(nb.toLowerCase()) && hasRand && attempt < 60) continue;
            break;
        }
        if (nb === oldName) continue; // 新名与原名一致：跳过
        if (!error && taken.has(nb.toLowerCase())) error = "重名"; // 无随机片段时重名无法避，标红
        if (error) {
            rows.push({ path, oldName, newName: nb, error });
        } else {
            taken.add(nb.toLowerCase());
            seq++;
            rows.push({ path, oldName, newName: nb, error: "" });
        }
    }
    return rows;
});

const exampleText = computed(() => {
    const vp = ["inc", "digits", "letters"].filter((m) => form.modes.includes(m as any))
        .map((m) => (m === "inc" ? "01" : m === "digits" ? "4821" : "kf")).join(form.sep);
    const sample = form.layout === "prefix" ? join(form.sep, form.fixed || "前缀", vp || "…") : join(form.sep, vp || "…", form.fixed || "后缀");
    return sample + ".txt";
});

const errorCount = computed(() => preview.value.filter((r) => r.error).length);
const applying = ref(false);
const canApply = computed(() => preview.value.length > 0 && errorCount.value === 0 && !applying.value);

function close() {
    emit("update:visible", false);
}

async function apply(): Promise<boolean> {
    if (!canApply.value) {
        if (!preview.value.length) Message.warning("没有可应用的改名");
        else if (errorCount.value) Message.warning("请先修正标红的项目");
        return false;
    }
    applying.value = true;
    let ok = 0;
    const fails: string[] = [];
    for (const r of preview.value) {
        try {
            await GoTools.DiskRename(r.path, r.newName);
            ok++;
        } catch (e: any) {
            fails.push(`${r.oldName} → ${r.newName}：${e?.message || e}`);
        }
    }
    applying.value = false;
    if (ok) Message.success(`已重命名 ${ok} 项`);
    if (fails.length) Message.error(`${fails.length} 项失败：${fails[0]}${fails.length > 1 ? " 等" : ""}`);
    if (ok) emit("done", ok);
    return fails.length === 0;
}
</script>

<style scoped>
.br-wrap {
    display: flex;
    gap: 16px;
    align-items: stretch;
}

.br-form {
    width: 288px;
    flex: none;
    border-right: 1px solid var(--color-border-2);
    padding-right: 16px;
}

.br-tip {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    margin-bottom: 8px;
}

.br-tip b {
    color: var(--color-text-1);
}

.br-label {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    margin: 10px 0 4px;
}

.br-opts {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-top: 4px;
    flex-wrap: wrap;
}

.br-opt {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

.br-example {
    margin-top: 12px;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

.br-example b {
    color: rgb(var(--arcoblue-6));
    word-break: break-all;
}

.br-preview {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
}

.br-preview-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    margin-bottom: 6px;
}

.br-ok-count {
    color: rgb(var(--green-6));
}

.br-err-count {
    color: rgb(var(--red-6));
}

.br-empty {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    color: var(--color-text-4);
    font-size: var(--base-size-small);
    border: 1px dashed var(--color-border-2);
    border-radius: 4px;
    min-height: 120px;
}

.br-table {
    width: 100%;
    border-collapse: collapse;
    font-size: var(--base-size-small);
    table-layout: fixed;
}

.br-table thead th {
    text-align: left;
    color: var(--color-text-3);
    font-weight: 500;
    padding: 4px 8px;
    border-bottom: 1px solid var(--color-border-2);
}

.br-table tbody td {
    padding: 4px 8px;
    border-bottom: 1px solid var(--color-fill-3);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.br-table th.br-st,
.br-table td.br-st {
    width: 96px;
    text-align: center;
}

.br-old {
    color: var(--color-text-3);
}

.br-new {
    color: var(--color-text-1);
}

.br-table tr.is-err .br-new {
    color: rgb(var(--red-6));
}

.br-msg {
    color: rgb(var(--green-6));
}

.br-msg.ok {
    color: rgb(var(--green-6));
}

tr.is-err .br-msg {
    color: rgb(var(--red-6));
}

.modal-hint {
    margin-top: 12px;
    font-size: var(--base-size-small);
    color: var(--color-text-4);
}

.modal-hint code {
    background: var(--color-fill-2);
    padding: 0 4px;
    border-radius: 3px;
}
</style>
