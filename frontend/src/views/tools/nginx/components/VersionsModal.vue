<template>
    <!-- 版本管理弹窗：从主页面拆出，内部自管本地/线上/Docker 三类版本与安装进度。
         安装事件监听 + 任务台账补录都在本组件（unmount-on-close，每次打开重新挂载即重新对账）。
         跨弹窗动作（以镜像新建实例 / 去编辑实例）通过 emit 交回父组件。 -->
    <a-modal v-model:visible="visible" title="Nginx 版本管理" :width="780" :footer="false" unmount-on-close
        :body-style="{ padding: '12px 16px', height: 'min(600px, calc(100vh - 220px))', overflowY: 'auto' }">
        <!-- 类型切换：本地 / 线上 / Docker 只看一类，不用在长弹窗里找 -->
        <a-radio-group class="ver-tab" size="medium" v-model="verTab" type="button">
            <a-radio value="local">本地版本 {{ localVersions.length }}</a-radio>
            <a-radio value="remote">{{ sysPkg ? '仓库可安装' : '线上可安装' }} {{ remoteVersions.length }}</a-radio>
            <a-radio value="docker">Docker 实例 {{ dockerInstCount }}</a-radio>
        </a-radio-group>
        <div v-show="verTab === 'local'" class="ver-block">
            <div class="ver-title">本地可用版本</div>
            <a-table :data="localVersions" size="small" :pagination="false" row-key="version"
                :scroll="{ x: '100%', y: 200 }" :columns="[
                    { title: '版本', dataIndex: 'version' },
                    { title: '来源', dataIndex: 'source', width: 90, slotName: 'source' },
                    { title: '路径', dataIndex: 'path', ellipsis: true, tooltip: true },
                    { title: '操作', dataIndex: 'ops', width: 90, slotName: 'ops', fixed: 'right', align: 'right' },
                ]">
                <template #source="{ record }">
                    <a-tag size="small"
                        :color="record.source === 'bundled' ? 'purple' : record.source === 'downloaded' ? 'green' : record.source === 'system' ? 'gray' : 'orange'">{{
                        srcLabel(record.source) }}</a-tag>
                </template>
                <template #ops="{ record }">
                    <a-popconfirm :content="`确定删除 Nginx ${record.version}？若仍有实例引用该版本会被拒绝。`" type="warning" position="lt"
                        :disabled="record.source === 'bundled' || record.source === 'system'"
                        @ok="removeVersion(record)">
                        <a-button size="small" status="danger"
                            :disabled="record.source === 'bundled' || record.source === 'system'">删除</a-button>
                    </a-popconfirm>
                </template>
            </a-table>
            <a-row class="add-local" :gutter="8">
                <a-col :span="16">
                    <a-space>
                        <a-input v-model="localDirInput" placeholder="注册本机已有 Nginx：填写 nginx 所在目录" />
                        <a-button @click="chooseLocalDir">浏览…</a-button>
                        <a-button @click="addLocalDir">添加</a-button>
                    </a-space>
                </a-col>
            </a-row>
        </div>
        <!-- Docker 镜像与容器：Docker 实例的「版本」就是镜像 tag（nginx:1.27-alpine） -->
        <div v-show="verTab === 'docker'">
            <docker-images keyword="nginx" tool-label="Nginx" repo="nginx" :presets="nginxImagePresets"
                :instances="instances" :states="states" @create="(img: string) => emit('new-from-image', img)"
                @use="(id: string) => emit('edit-instance', id)" @changed="emit('changed')" />
        </div>
        <div v-show="verTab === 'remote'" class="ver-block">
            <div class="ver-title">
                {{ sysPkg ? '仓库可安装版本（发行版包）' : '线上版本' }}
                <a-button size="small" :loading="remoteLoading" @click="loadRemoteVersions">刷新</a-button>
            </div>
            <a-spin :loading="remoteLoading" style="width:100%">
                <a-table :data="remoteVersions" size="small" :pagination="false" row-key="version"
                    :scroll="{ x: '100%', y: 220 }" :columns="[
                        { title: '版本', dataIndex: 'version', width: 110 },
                        { title: '产品线', dataIndex: 'release', width: 90 },
                        { title: sysPkg ? '包名' : '包大小', dataIndex: 'size', width: 100, slotName: 'size' },
                        { title: '状态', dataIndex: 'st', width: 150, slotName: 'st' },
                        { title: '操作', dataIndex: 'ops', width: 100, slotName: 'ops', fixed: 'right', align: 'right' },
                    ]">
                    <template #size="{ record }">{{ remoteVerText(record) }}</template>
                    <template #st="{ record }">
                        <span v-if="installedSet.has(record.version)" class="com-state is-on">
                            <com-badge status="success" />已安装
                        </span>
                        <span v-else-if="installProgress[record.version]" class="com-state"
                            :class="installProgress[record.version].stage === 'error' ? 'is-off' : 'is-busy'">
                            <com-badge
                                :status="installProgress[record.version].stage === 'error' ? 'danger' : 'processing'"
                                :animated="installProgress[record.version].stage !== 'error'" />
                            {{ stageText(installProgress[record.version]) }}
                        </span>
                        <span v-else class="com-state is-dim">
                            <com-badge status="normal" />未安装
                        </span>
                    </template>
                    <template #ops="{ record }">
                        <a-button v-if="!installedSet.has(record.version)" size="small"
                            :disabled="!!installProgress[record.version]" @click="installVersion(record)">安装</a-button>
                        <a-button v-else size="small" @click="loadLocalVersions">已安装</a-button>
                    </template>
                </a-table>
            </a-spin>
            <div class="offline-tip">离线安装：将 nginx zip 放入 <a-link copyable :content="pluginsDirVal">{{ pluginsDirVal ||
                    '…'
                    }}</a-link> 后刷新本地版本</div>
        </div>
        <div v-if="installLogs.length" class="ver-block install-log">
            <div class="ver-title">安装日志</div>
            <div class="ls-log small">
                <div v-for="(l, i) in installLogs" :key="i" class="ls-line">{{ l }}</div>
            </div>
        </div>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { remoteVerText, useSysPkg } from "@/utils/caps";
import { restoreInstalls } from "@/store/modules/tasks";
import type { InstConfig } from "../types";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const sysPkg = useSysPkg();

const props = defineProps<{
    /** 可见性（v-model） */
    modelValue: boolean;
    /** 实例与状态表：透传给 docker-images，并统计 Docker 实例数 */
    instances: InstConfig[];
    states: Record<string, any>;
}>();

const emit = defineEmits<{
    (e: "update:modelValue", v: boolean): void;
    /** 状态有变（如容器实例增删）：父组件刷新状态 */
    (e: "changed"): void;
    /** 以某镜像新建实例：父组件打开新建弹窗并预置 Docker */
    (e: "new-from-image", image: string): void;
    /** 去编辑某实例：父组件打开编辑弹窗 */
    (e: "edit-instance", id: string): void;
}>();

const visible = computed({
    get: () => props.modelValue,
    set: (v: boolean) => emit("update:modelValue", v),
});

const verTab = ref<"local" | "remote" | "docker">("local");
/** 版本管理里 Docker 镜像的常用 tag：alpine 体积小、且镜像内目录约定完整 */
const nginxImagePresets = ["nginx:alpine", "nginx:stable-alpine", "nginx:1.27-alpine", "nginx:1.26-alpine", "nginx:alpine-slim", "nginx:1.27"];
/** 有多少实例跑在容器里（它们的版本由镜像 tag 决定，不占本机版本库） */
const dockerInstCount = computed(() => props.instances.filter((i) => i.mode === "docker").length);

const localVersions = ref<any[]>([]);
const remoteVersions = ref<any[]>([]);
const remoteLoading = ref(false);
const installProgress = reactive<Record<string, any>>({});
const installLogs = ref<string[]>([]);
const localDirInput = ref("");
const pluginsDirVal = ref("");
const installedSet = computed(() => new Set<string>(localVersions.value.map((v: any) => v.version)));

function srcLabel(s: string): string {
    return s === "bundled" ? "内置" : s === "downloaded" ? "下载" : s === "system" ? "系统包" : "本机";
}
function stageText(p: any): string {
    if (!p) return "";
    if (p.stage === "downloading") return `下载中 ${p.percent || 0}%`;
    if (p.stage === "extracting") return "解压中";
    if (p.stage === "done") return "完成";
    if (p.stage === "info") return "准备中";
    return "失败";
}
function pushInstallLog(txt: string) {
    if (!txt) return;
    const t = new Date();
    const pad = (n: number) => n.toString().padStart(2, "0");
    installLogs.value.push(`[${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}] ${txt}`);
    if (installLogs.value.length > 200) installLogs.value.splice(0, installLogs.value.length - 200);
}

async function loadLocalVersions() {
    try { localVersions.value = (await GoTools.NginxSrvListVersions()) || []; }
    catch (e) { localVersions.value = []; }
}
async function loadRemoteVersions() {
    remoteLoading.value = true;
    try { remoteVersions.value = (await GoTools.NginxSrvRemoteVersions()) || []; }
    catch (e: any) { Message.error("获取远程版本失败: " + (e?.message || e)); }
    remoteLoading.value = false;
}
async function installVersion(r: any) {
    try {
        pushInstallLog(`提交安装请求：Nginx ${r.version}`);
        await GoTools.NginxSrvInstall(r.version);
        installProgress[r.version] = { stage: "downloading", percent: 0 };
    } catch (e: any) {
        pushInstallLog(`安装请求被拒绝：${e?.message || e}`);
        Message.error("安装失败: " + (e?.message || e));
    }
}
async function removeVersion(v: any) {
    try { await GoTools.NginxSrvRemoveVersion(v.version); Message.success("已删除"); loadLocalVersions(); }
    catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
}
async function chooseLocalDir() {
    try { const dir = await GoTools.NginxSrvChooseDir(); if (dir) localDirInput.value = dir; }
    catch (e) { /* cancel */ }
}
async function addLocalDir() {
    const dir = localDirInput.value.trim();
    if (!dir) { Message.warning("请先选择或填写目录"); return; }
    try { await GoTools.NginxSrvAddLocal(dir); Message.success("已添加本机目录"); localDirInput.value = ""; loadLocalVersions(); }
    catch (e: any) { Message.error("添加失败: " + (e?.message || e)); }
}

/* 打开时初始化：切回本地页、按需拉本地/线上版本与插件目录 */
watch(() => props.modelValue, async (open) => {
    if (!open) return;
    verTab.value = "local";
    loadLocalVersions();
    if (!pluginsDirVal.value) {
        try { pluginsDirVal.value = (await GoTools.NginxSrvPluginsDir()) || ""; } catch (e) { /* ignore */ }
    }
    if (remoteVersions.value.length === 0) loadRemoteVersions();
});

/* 安装进度事件：本组件挂载期间监听；离开再回来靠 restoreInstalls 从任务台账补录 */
let offInstall: any = null;
onMounted(() => {
    restoreInstalls("nginx", "install", installProgress, pushInstallLog);
    offInstall = Events.On("nginx:install", (ev: any) => {
        const p = ev?.data ?? ev;
        if (!p || !p.version) return;
        if (p.stage === "info") {
            pushInstallLog(p.note || "");
        } else if (p.stage === "done") {
            delete installProgress[p.version];
            pushInstallLog(`安装完成 Nginx ${p.version}`);
            Message.success(`Nginx ${p.version} 安装完成`);
            loadLocalVersions();
        } else if (p.stage === "error") {
            installProgress[p.version] = p;
            pushInstallLog(`安装失败 ${p.version}：${p.note || "未知错误"}（可将 nginx zip 放入插件目录后离线安装）`);
            Modal.error({
                title: "安装失败",
                content: `${p.version}：${p.note || "未知错误"}。可将 nginx zip 放入插件目录后重试（离线安装）。`,
            });
        } else {
            installProgress[p.version] = p;
        }
    });
});
onUnmounted(() => {
    if (offInstall) offInstall();
});
</script>

<style scoped>
/* 版本管理弹窗的类型切换：与下方内容留一行间距（原本靠分隔线撑开的节奏） */
.ver-tab {
    display: flex;
    margin-bottom: 12px;
}

.ver-block .ver-title {
    display: flex;
    align-items: center;
    gap: 8px;
    font-weight: 600;
    font-size: var(--base-size-default);
    margin-bottom: 8px;
}

.add-local {
    margin-top: 10px;
}

.offline-tip {
    margin-top: 8px;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    word-break: break-all;
}

.install-log {
    margin-top: 12px;
}

/* 安装日志：紧凑滚动区 */
.ls-log {
    overflow: auto;
    background: var(--color-fill-2);
    padding: 8px;
    border-radius: var(--base-radius-default);
    font-family: Consolas, Menlo, monospace;
    font-size: var(--base-size-small);
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
