<template>
    <!-- 本地服务：版本管理（宽度 820：Docker 镜像表带上「使用实例」列后 680 会横向溢出） -->
    <!-- 固定高度：三个页签内容高矮不一，不钉住的话切一下弹窗就跳一次高度 -->
    <a-modal :visible="visible" title="Redis 版本管理" :width="820" :footer="false"
        :body-style="{ height: 'min(600px, calc(100vh - 220px))', overflowY: 'auto' }"
        @update:visible="(v: boolean) => emit('update:visible', v)">
        <a-space direction="vertical" fill :size="12">
            <!-- 类型切换：本地 / 线上 / Docker 只看一类，不用在长弹窗里找 -->
            <a-radio-group size="medium" v-model="verTab" type="button">
                <a-radio value="local">已安装版本 {{ localVersions.length }}</a-radio>
                <a-radio value="remote">{{ sysPkg ? '仓库可安装' : '在线可安装' }} {{ remoteVersions.length }}</a-radio>
                <a-radio value="docker">Docker 实例 {{ dockerInstCount }}</a-radio>
            </a-radio-group>
            <!-- 已安装版本 -->
            <div v-show="verTab === 'local'">
                <a-card size="small" :body-style="{ padding: '8px 12px' }">
                    <template #title><span class="ver-sec-title"><icon-storage /> 已安装版本</span></template>
                    <div class="ver-scroll">
                        <div v-for="v in localVersions" :key="v.version + v.source" class="ver-item">
                            <a-tag size="small"
                                :color="v.source === 'bundled' ? 'green' : (v.source === 'downloaded' ? 'arcoblue' : (v.source === 'system' ? 'gray' : 'orange'))">{{
                                    srcLabel(v.source) }}</a-tag>
                            <span class="ver-name">Redis {{ v.version }}</span>
                            <a-tooltip :content="v.path">
                                <span class="ver-path">{{ v.path }}</span>
                            </a-tooltip>
                            <a-popconfirm :content="`确定删除 Redis ${v.version}？若仍有运行中的实例引用该版本会被拒绝。`"
                                type="warning" position="lt" :disabled="v.source === 'bundled' || v.source === 'system'"
                                @ok="removeVersion(v)">
                                <a-button size="small" status="danger"
                                    :disabled="v.source === 'bundled' || v.source === 'system'">删除</a-button>
                            </a-popconfirm>
                        </div>
                        <a-empty v-if="localVersions.length === 0" :image="false"
                            description="尚未安装任何版本，可从下方在线安装或添加本机目录" />
                    </div>
                    <div class="ver-actions">
                        <a-input v-model="localDirInput" size="small" placeholder="本机 redis-server 所在目录" allow-clear />
                        <a-button size="small" @click="chooseLocalDir"><template
                                #icon><icon-folder /></template>选目录</a-button>
                        <a-button size="small" @click="addLocalDir">添加</a-button>
                    </div>
                    <div v-if="sysPkg" class="ver-hint">版本由发行版仓库提供：在上面选一个版本点「安装」即可（包管理器装到标准路径并注册服务）；
                        也可把已有的 redis-server 目录加进来
                    </div>
                    <div v-else class="ver-hint">离线安装：将 redis zip 放入 <span class="mono">{{ srvServersDir }}</span> 后点「安装」即可</div>
                </a-card>
            </div>

            <!-- Docker 镜像与容器：Docker 实例的「版本」就是镜像 tag（redis:7.4-alpine） -->
            <div v-show="verTab === 'docker'">
                <docker-images keyword="redis" tool-label="Redis" repo="redis" :presets="redisImagePresets"
                    :instances="srvInstances" :states="srvStates" :reload-key="verReload" @create="onCreate"
                    @use="(id: string) => emit('edit-instance', id)" @changed="emit('refresh-states')" />
            </div>

            <!-- 在线版本 -->
            <div v-show="verTab === 'remote'">
                <a-card size="small" :body-style="{ padding: '8px 12px' }">
                    <template #title>
                        <span class="ver-sec-title"><icon-cloud /> {{ sysPkg ? '仓库可安装（发行版包，装到系统标准路径）' : '在线版本（GitHub redis-windows 发行版，>=7.0）' }}</span>
                    </template>
                    <template #extra>
                        <a-button size="small" :loading="remoteLoading" @click="loadRemoteVersions">
                            <template #icon><icon-refresh /></template>刷新
                        </a-button>
                    </template>
                    <div class="ver-scroll">
                        <div v-for="r in remoteVersions" :key="r.version" class="ver-item">
                            <span class="ver-name">Redis {{ r.version }}</span>
                            <span class="ver-path">{{ remoteVerText(r) }}</span>
                            <a-button size="small"
                                :disabled="!!installProgress[r.version] || installedSet.has(r.version)"
                                @click="installVersion(r)">{{ installedSet.has(r.version) ? '已安装' : '安装' }}</a-button>
                        </div>
                        <a-empty v-if="remoteVersions.length === 0" :image="false"
                            :description="remoteLoading ? '加载中…' : '点「刷新」获取版本列表'" />
                    </div>
                    <div v-for="(p, ver) in installProgress" :key="'p' + ver" class="inst-prog">
                        <div class="inst-prog-row">
                            <span class="ver-name">{{ ver }}</span>
                            <a-progress :percent="(p.percent || 0) / 100" size="small"
                                :status="p.stage === 'error' ? 'danger' : 'normal'" style="flex: 1" />
                            <span class="inst-stage">{{ stageText(p) }}</span>
                            <a-button v-if="p.stage === 'error'" size="small" status="danger"
                                @click="retryInstall(ver as string)">
                                <template #icon><icon-refresh /></template>重试
                            </a-button>
                        </div>
                        <div v-if="p.stage === 'error' && p.note" class="inst-err">
                            <a-tooltip :content="p.note" position="tl">
                                <a-alert type="error" :show-icon="false">{{ p.note }}</a-alert>
                            </a-tooltip>
                        </div>
                    </div>
                </a-card>

                <!-- 安装日志 -->
                <a-card size="small" :body-style="{ padding: '8px 12px' }">
                    <template #title><span class="ver-sec-title"><icon-history /> 安装日志</span></template>
                    <template #extra>
                        <a-button size="small" @click="installLogs = []"><template
                                #icon><icon-delete /></template>清空</a-button>
                    </template>
                    <div class="ver-scroll inst-log">
                        <div v-for="(l, i) in installLogs" :key="i" class="inst-log-line">{{ l }}</div>
                        <a-empty v-if="installLogs.length === 0" :image="false" description="暂无安装记录" />
                    </div>
                </a-card>
            </div>
        </a-space>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, onUnmounted, reactive, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { remoteVerText, useSysPkg } from "@/utils/caps";
import { restoreInstalls } from "@/store/modules/tasks";
import { srcLabel, stageText, REDIS_IMAGE_PRESETS, type SrvInstanceConfig } from "../helpers";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const props = defineProps<{
    visible: boolean;
    srvInstances: SrvInstanceConfig[];
    srvStates: Record<string, any>;
}>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "create-instance", image: string): void;
    (e: "edit-instance", id: string): void;
    (e: "refresh-states"): void;
}>();

const sysPkg = useSysPkg();
const redisImagePresets = REDIS_IMAGE_PRESETS;

const localVersions = ref<any[]>([]);
const remoteVersions = ref<any[]>([]);
const remoteLoading = ref(false);
const installProgress = reactive<Record<string, any>>({});
const installLogs = ref<string[]>([]);
const verReload = ref(0);
const verTab = ref<"local" | "remote" | "docker">("local");
const localDirInput = ref("");
const srvServersDir = ref("");

const dockerInstCount = computed(() => props.srvInstances.filter((i: any) => i.mode === "docker").length);
const installedSet = computed(() => new Set<string>(localVersions.value.map((v: any) => v.version)));

function pushInstallLog(txt: string) {
    if (!txt) return;
    const t = new Date();
    const pad = (n: number) => n.toString().padStart(2, "0");
    installLogs.value.push(`[${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}] ${txt}`);
    if (installLogs.value.length > 200) installLogs.value.splice(0, installLogs.value.length - 200);
}

async function loadLocalVersions() {
    try { localVersions.value = (await GoTools.RedisSrvListVersions()) || []; }
    catch (e) { localVersions.value = []; }
}

async function openReset() {
    verTab.value = "local";
    verReload.value++;
    loadLocalVersions();
    if (!srvServersDir.value) {
        try { srvServersDir.value = (await GoTools.RedisSrvServersDir()) || ""; } catch (e) { /* ignore */ }
    }
}

async function loadRemoteVersions() {
    remoteLoading.value = true;
    try { remoteVersions.value = (await GoTools.RedisSrvRemoteVersions()) || []; }
    catch (e: any) { Message.error("获取远程版本失败: " + (e?.message || e)); }
    remoteLoading.value = false;
}

async function installVersion(r: any) {
    try {
        pushInstallLog(`提交安装请求：Redis ${r.version}`);
        await GoTools.RedisSrvInstall(r.version);
        installProgress[r.version] = { stage: "downloading", percent: 0 };
    } catch (e: any) {
        pushInstallLog(`安装请求被拒绝：${e?.message || e}`);
        Message.error("安装失败: " + (e?.message || e));
    }
}

function retryInstall(ver: string) {
    delete installProgress[ver];
    installVersion({ version: ver });
}

async function removeVersion(v: any) {
    try { await GoTools.RedisSrvRemoveVersion(v.version); Message.success("已删除"); loadLocalVersions(); }
    catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
}

async function chooseLocalDir() {
    try { const dir = await GoTools.RedisSrvChooseDir(); if (dir) localDirInput.value = dir; }
    catch (e) { /* cancel */ }
}

async function addLocalDir() {
    const dir = localDirInput.value.trim();
    if (!dir) { Message.warning("请先选择或填写目录"); return; }
    try { await GoTools.RedisSrvAddLocal(dir); Message.success("已添加本机目录"); localDirInput.value = ""; loadLocalVersions(); }
    catch (e: any) { Message.error("添加失败: " + (e?.message || e)); }
}

/** 版本管理里点「新建实例」：关掉本弹窗，通知页面以该镜像开一个 Docker 实例 */
function onCreate(image: string) {
    emit("update:visible", false);
    emit("create-instance", image);
}

let offInstall: any = null;
onMounted(() => {
    loadLocalVersions();
    // 离开页面时安装还在跑：从任务台账把进度表与日志补回来，免得回来看到可点的"安装"
    restoreInstalls("redis", "install", installProgress, pushInstallLog);
    offInstall = Events.On("redis:install", (ev: any) => {
        const p = ev?.data ?? ev;
        if (!p || !p.version) return;
        if (p.stage === "info") {
            pushInstallLog(p.note || "");
        } else if (p.stage === "done") {
            delete installProgress[p.version];
            pushInstallLog(`安装完成 Redis ${p.version}` + (p.note ? "：" + p.note : ""));
            Message.success(`Redis ${p.version} 安装完成`);
            loadLocalVersions();
        } else if (p.stage === "error") {
            installProgress[p.version] = p;
            pushInstallLog(`安装失败 ${p.version}：${p.note || "未知错误"}（可将 redis zip 放入插件目录后离线安装）`);
            Modal.error({
                title: "安装失败",
                content: `${p.version}：${p.note || "未知错误"}。可将 redis zip 放入插件目录后重试（离线安装）。`,
            });
        } else {
            installProgress[p.version] = p;
        }
    });
});
onUnmounted(() => { if (offInstall) offInstall(); });

watch(() => props.visible, (open) => { if (open) openReset(); });
</script>

<style scoped>
/* ---- 版本管理弹窗 ---- */
.ver-sec-title {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-weight: 600;
    font-size: var(--base-size-default);
}

.ver-scroll {
    max-height: 170px;
    overflow-y: auto;
}

.ver-item {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: var(--base-radius-default) var(--base-padding);
    border-radius: var(--base-radius-default);
    font-size: var(--base-size-small);
}

.ver-item:hover {
    background: var(--color-fill-2);
}

.ver-name {
    font-weight: 500;
    min-width: 100px;
}

.ver-path {
    flex: 1;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--color-text-3);
    font-size: var(--base-size-small);
}

.ver-actions {
    display: flex;
    gap: 8px;
    align-items: center;
    margin-top: var(--base-padding);
}

.ver-actions :deep(.arco-input-wrapper) {
    flex: 1;
}

.ver-hint {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    margin-top: var(--base-radius-default);
}

.mono {
    font-family: Consolas, monospace;
    background: var(--color-fill-2);
    padding: var(--base-radius-small) var(--base-radius-default);
    border-radius: var(--base-radius-small);
}

/* 安装进度 */
.inst-prog {
    margin-top: var(--base-padding);
}

.inst-prog-row {
    display: flex;
    align-items: center;
    gap: 8px;
}

.inst-stage {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    min-width: 80px;
}

/* 安装失败提示：内容是 a-alert，这里只管间距，颜色交给 alert 的类型色 */
.inst-err {
    padding: var(--base-radius-default) 0 0 0;
    word-break: break-all;
    white-space: pre-wrap;
}

/* 安装失败原因常带着下载地址：两行截断，不把版本弹窗撑高；完整内容悬停看 */
.inst-err :deep(.arco-alert-content) {
    display: -webkit-box;
    -webkit-line-clamp: 2;
    -webkit-box-orient: vertical;
    overflow: hidden;
    white-space: pre-wrap;
}

/* 安装日志：随主题自适应的控制台风格面板 */
.inst-log {
    max-height: 130px;
    background: var(--color-fill-1);
    border: 1px solid var(--color-border-2);
    border-radius: var(--base-radius-default);
    padding: var(--base-radius-default) var(--base-radius-large);
}

.inst-log-line {
    font-family: Consolas, monospace;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    line-height: 1.6;
    white-space: pre-wrap;
    word-break: break-all;
}
</style>
