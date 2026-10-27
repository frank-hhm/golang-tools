<template>
    <!-- 全局包管理（多包管理器）：从主页面拆出，自持包管理器选择 / 过滤 / 装卸动作与任务日志。 -->
    <!-- managers / packages 由父页面持有（总览也要展示），装/卸后请求父页面重扫 -->
    <a-modal :visible="visible" title="全局包管理" :width="860" :footer="false" unmount-on-close
        :body-style="{ height: 'min(600px, calc(100vh - 200px))', overflow: 'auto' }"
        @update:visible="(v: boolean) => emit('update:visible', v)">
        <div class="mgr-hint">
            <template v-if="mgrHintLines">
                <div v-for="(h, i) in mgrHintLines" :key="i" class="mgr-hint-row">
                    <span class="mgr-hint-label">{{ h.label }}</span>
                    <a-tooltip :content="h.value">
                        <span class="mgr-hint-val">{{ h.value }}</span>
                    </a-tooltip>
                </div>
            </template>
            <div v-else class="mgr-hint-empty">{{ mgrHint }}</div>
        </div>
        <a-row :gutter="8" align="center" style="margin-bottom:6px">
            <a-col :span="10">
                <a-space>
                    <div class="mgr-label">装/卸用</div>
                    <a-select v-model="pkgMgrId" :options="mgrOptions" size="small" placeholder="包管理器"
                        :loading="mgrLoading" allow-search />
                </a-space>
            </a-col>
            <a-col :span="10">
                <a-input v-model="pkgInstallName" size="small" :placeholder="installHint" @press-enter="installPkg" />
            </a-col>
            <a-col :span="4">
                <a-space>
                    <a-button size="small" block :disabled="!!pkgBusy" @click="installPkg">安装</a-button>
                    <a-button size="small" block :loading="pkgsLoading" @click="scan">扫描</a-button>
                </a-space>
            </a-col>
        </a-row>
        <a-input v-model="pkgFilter" size="small" placeholder="按包名过滤" allow-clear
            style="width:220px;margin-bottom:8px" />
        <a-spin :loading="pkgsLoading" style="width:100%">
            <a-table :data="filteredPackages" size="small" :pagination="false" row-key="path" :scroll="{ x: '100%', y: 300 }"
                :columns="[
                    { title: '包名', dataIndex: 'name', width: 200, ellipsis: true, tooltip: true },
                    { title: '版本', dataIndex: 'version', width: 96 },
                    { title: '管理器', dataIndex: 'manager', width: 84, slotName: 'manager' },
                    { title: 'CLI', dataIndex: 'hasBin', width: 56, slotName: 'bin' },
                    { title: '说明', dataIndex: 'description', ellipsis: true, tooltip: true },
                    { title: '操作', dataIndex: 'ops', width: 100, slotName: 'ops', fixed: 'right', align: 'right' },
                ]">
                <template #manager="{ record }">
                    <a-tag size="small" :color="mgrColor(record.manager)">{{ record.manager }}</a-tag>
                </template>
                <template #bin="{ record }">
                    <com-badge v-if="record.hasBin" status="success" />
                </template>
                <template #ops="{ record }">
                    <a-tooltip v-if="record.builtin" content="随 Node 发行，不可单独卸载">
                        <a-button size="small" disabled>发行件</a-button>
                    </a-tooltip>
                    <a-popconfirm v-else :content="`确定卸载 ${record.name}@${record.version}？这个包由 ${record.manager} 安装。`"
                        type="warning" position="lt" :disabled="!!pkgBusy" @ok="uninstallPkg(record)">
                        <a-button size="small" status="danger" :disabled="!!pkgBusy"
                            :loading="pkgBusy === `卸载 ${record.name}`">卸载</a-button>
                    </a-popconfirm>
                </template>
                <template #empty>
                    <div v-if="!pkgsLoading && filteredPackages.length === 0" class="conn-empty">
                        {{ packages.length ? '没有匹配过滤的包' : '未扫到全局包（各管理器的全局安装根都是空的）' }}
                    </div>
                </template>
            </a-table>
        </a-spin>
        <div v-if="pkgBusy" class="offline-tip">{{ pkgBusy }} …（进度见任务日志）</div>
        <div v-if="installLogs.length" class="ver-block install-log">
            <div class="ver-title">任务日志</div>
            <div class="ls-log small">
                <div v-for="(l, i) in installLogs" :key="i" class="ls-line">{{ l }}</div>
            </div>
        </div>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onBeforeUnmount, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { mgrColor } from "../helpers";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const props = defineProps<{
    visible: boolean;
    /** 包管理器列表：父页面持有（总览也展示） */
    managers: any[];
    /** 全局包列表：父页面持有（总览按管理器计数） */
    packages: any[];
    mgrLoading: boolean;
    pkgsLoading: boolean;
}>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    /** 打开弹窗 / 装包后需要父页面重扫管理器与全局包、刷新本机版本 */
    (e: "prepare"): void;
    (e: "rescan"): void;
}>();

const pkgMgrId = ref("");
const pkgInstallName = ref("");
const pkgBusy = ref("");
const pkgFilter = ref("");
const installLogs = ref<string[]>([]);

/* 包管理器：同一个名字可能有多份（每个 Node 自带一份 npm），靠 id 区分 */
const mgrOptions = computed(() => props.managers
    .filter((m: any) => m.name !== "corepack")
    .map((m: any) => ({
        label: `${m.name}${m.version ? " " + m.version : ""}${m.kind === "node" && m.nodeVer ? "（Node " + m.nodeVer + "）" : ""}`,
        value: m.id,
    })));
const curMgr = computed(() => props.managers.find((m: any) => m.id === pkgMgrId.value) || null);
const mgrHint = computed(() => props.managers.length ? "" : "未发现本机包管理器，先到「Node / npm 版本管理」扫描本机 Node");
// 把“当前管理器 / 入口 / 扫描目录”拆成多行结构化展示，避免 <br/> 在文本里被转义
const mgrHintLines = computed(() => {
    const m = curMgr.value;
    if (!m) return null;
    const roots = (m.roots || []) as string[];
    return [
        { label: "当前管理器", value: `${m.name} ${m.version || ""}`.trim() },
        { label: "入口", value: m.path || "-" },
        { label: "扫描目录", value: roots.length ? roots.join("、") : "（尚无全局安装目录，装过包后才有）" },
    ];
});
// 各家安装语法不同，把占位提示写成对应命令，避免用户以为都是 npm i -g
const installHint = computed(() => {
    const n = String(curMgr.value?.name || "npm").toLowerCase();
    const cmd = n === "yarn" ? "yarn global add" : (n === "pnpm" || n === "bun" || n === "cnpm") ? `${n} add -g` : "npm i -g";
    return `要全局安装的包名（${cmd}），如 axios`;
});
const filteredPackages = computed(() => {
    const q = pkgFilter.value.trim().toLowerCase();
    if (!q) return props.packages;
    return props.packages.filter((p: any) => String(p.name || "").toLowerCase().includes(q));
});

// managers 变化后校正默认选中：默认选 npm（多份 npm 时取版本最高的那份），没有 npm 就退而求其次
watch(() => props.managers, (list) => {
    if (!list.some((m: any) => m.id === pkgMgrId.value)) {
        pkgMgrId.value = list.find((m: any) => m.name === "npm")?.id || list[0]?.id || "";
    }
}, { immediate: true });

function pushInstallLog(txt: string) {
    if (!txt) return;
    const t = new Date();
    const pad = (n: number) => n.toString().padStart(2, "0");
    installLogs.value.push(`[${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}] ${txt}`);
    if (installLogs.value.length > 200) installLogs.value.splice(0, installLogs.value.length - 200);
}

function scan() { emit("rescan"); }

async function installPkg() {
    const pkg = pkgInstallName.value.trim();
    if (!pkg) { Message.warning("请填写包名"); return; }
    if (!pkgMgrId.value) { Message.warning("请选择包管理器"); return; }
    try {
        pkgBusy.value = `${curMgr.value?.name || ""} 全局安装 ${pkg}`;
        await GoTools.NpmInstallWith(pkgMgrId.value, pkg);
        pkgInstallName.value = "";
    } catch (e: any) {
        pkgBusy.value = "";
        Message.error("安装失败: " + (e?.message || e));
    }
}
/** 卸载全局包：气泡确认后直接下发，pkgBusy 用作行内 loading 文案 */
async function uninstallPkg(rec: any) {
    try {
        pkgBusy.value = `卸载 ${rec.name}`;
        // 传包的实际目录，后端据此反查当初装它的管理器，避免多份 npm 卸错
        await GoTools.NpmUninstallWith(pkgMgrId.value, rec.name, rec.path);
    } catch (e: any) {
        pkgBusy.value = "";
        Message.error("卸载失败: " + (e?.message || e));
    }
}

let offInstall: any = null;
// 弹窗常驻挂载：订阅 npm:install 结束装卸态 pkgBusy 并请求父页面重扫（任务日志与提示由版本弹窗统一给）
offInstall = Events.On("npm:install", (ev: any) => {
    const p = ev?.data ?? ev;
    if (!p || !p.version) return;
    if (p.stage === "done") {
        pkgBusy.value = "";
        pushInstallLog(`完成：${p.note || p.version}`);
        emit("rescan");
    } else if (p.stage === "error") {
        pkgBusy.value = "";
        pushInstallLog(`失败 ${p.version}：${p.note || "未知错误"}`);
        emit("rescan");
    }
});

watch(() => props.visible, (open) => { if (open) emit("prepare"); });
onBeforeUnmount(() => { if (offInstall) offInstall(); });
</script>

<style scoped>
.mgr-label {
    font-size: 12px;
    color: var(--color-text-3);
    text-align: right;
    padding-right: 2px;
}

.offline-tip {
    margin-top: 8px;
    font-size: 12px;
    color: var(--color-text-3);
    word-break: break-all;
}

/* 全局包管理：当前管理器提示（多行结构化） */
.mgr-hint {
    background: var(--color-fill-2);
    border: 1px solid var(--color-border-2);
    border-radius: 4px;
    padding: 8px 10px;
    margin-bottom: 10px;
    font-size: 12px;
}

.mgr-hint-row {
    display: flex;
    align-items: baseline;
    gap: 8px;
    line-height: 1.9;
}

.mgr-hint-label {
    flex-shrink: 0;
    width: 68px;
    color: var(--color-text-3);
}

.mgr-hint-val {
    flex: 1;
    min-width: 0;
    color: var(--color-text-1);
    font-family: Consolas, Menlo, monospace;
    word-break: break-all;
}

.mgr-hint-empty {
    color: var(--color-text-3);
}

.conn-empty {
    color: var(--color-text-3);
    font-size: 12px;
    padding: 6px 0;
}

.install-log {
    margin-top: 12px;
}

.ver-block .ver-title,
.ver-title {
    display: flex;
    align-items: center;
    gap: 8px;
    font-weight: 600;
    font-size: 13px;
    margin-bottom: 8px;
}

.ls-log {
    flex: 1;
    min-height: 160px;
    overflow: auto;
    background: var(--color-fill-2);
    margin: 0 14px 12px;
    padding: 8px;
    border-radius: 4px;
    font-family: Consolas, Menlo, monospace;
    font-size: 12px;
}

.ls-log.small {
    max-height: 140px;
    min-height: 60px;
    margin: 0;
}

.ls-line {
    white-space: pre-wrap;
    word-break: break-all;
    line-height: 1.6;
}
</style>
