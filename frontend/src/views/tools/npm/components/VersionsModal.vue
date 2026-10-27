<template>
    <!-- Node / npm 版本管理：Node 版本、npm 生效链、Docker 三个页签。从主页面拆出，自持本地/线上版本、安装进度、生效链切换与任务日志。 -->
    <a-modal :visible="visible" title="Node / Npm 版本管理" :width="900" :footer="false" unmount-on-close
        :body-style="{ height: 'min(620px, calc(100vh - 200px))', overflow: 'auto' }"
        @update:visible="(v: boolean) => emit('update:visible', v)">
        <div class="ver-switch">
            <a-radio-group size="medium" v-model="verTab" type="button" @change="onVerTabChange">
                <a-radio value="node">Node 版本</a-radio>
                <a-radio value="chain">npm 生效链（命令行切换）</a-radio>
                <a-radio value="docker">Docker 镜像 / 容器</a-radio>
            </a-radio-group>
        </div>
        <div v-show="verTab === 'node'" class="ver-pane">
            <div class="ver-block">
                <div class="ver-title">
                    本地可用版本
                    <a-button size="small" :loading="scanning" @click="scanLocalNodes">扫描本机 Node</a-button>
                    <a-button size="small" @click="emit('refresh-versions')">刷新</a-button>
                </div>
                <a-table :data="localVersions" size="small" :pagination="false" row-key="version" :scroll="{ x: '100%' }"
                    :columns="[
                    { title: '版本', dataIndex: 'version', width: 110 },
                    { title: 'npm', dataIndex: 'npmVer', width: 90 },
                    { title: '来源', dataIndex: 'source', width: 90, slotName: 'source' },
                    { title: '路径', dataIndex: 'path', ellipsis: true, tooltip: true },
                    { title: '操作', dataIndex: 'ops', width: 90, slotName: 'ops', fixed: 'right', align: 'right' },
                ]">
                    <template #source="{ record }">
                        <a-tag size="small"
                            :color="record.source === 'bundled' ? 'purple' : record.source === 'downloaded' ? 'green' : 'orange'">{{
                                srcLabel(record.source) }}</a-tag>
                    </template>
                    <template #ops="{ record }">
                        <a-popconfirm :content="`确定删除 Node.js ${record.version}？若仍有站点引用该版本会被拒绝。`"
                            type="warning" position="lt" :disabled="record.source === 'bundled'"
                            @ok="removeVersion(record)">
                            <a-button size="small" status="danger" :disabled="record.source === 'bundled'">删除</a-button>
                        </a-popconfirm>
                    </template>
                </a-table>
                <a-row class="add-local" :gutter="8">
                    <a-col :span="16">
                        <a-input v-model="localDirInput" placeholder="注册本机已有 Node.js：填写 node.exe 所在目录" />
                    </a-col>
                    <a-col :span="4">
                        <a-button size="small" block @click="chooseLocalDir">浏览…</a-button>
                    </a-col>
                    <a-col :span="4">
                        <a-button size="small" block @click="addLocalDir">添加</a-button>
                    </a-col>
                </a-row>
            </div>
            <a-divider style="margin: 12px 0" />
            <div class="ver-block">
                <div class="ver-title">
                    线上版本
                    <a-button size="small" :loading="remoteLoading" @click="loadRemoteVersions">刷新</a-button>
                </div>
                <a-spin :loading="remoteLoading" style="width:100%">
                    <a-table :data="remoteVersions" size="small" :pagination="{ pageSize: 8 }" row-key="version"
                        :scroll="{ x: '100%' }"
                        :columns="[
                            { title: '版本', dataIndex: 'version', width: 110 },
                            { title: 'LTS', dataIndex: 'lts', width: 90, slotName: 'lts' },
                            { title: '包大小', dataIndex: 'size', width: 100, slotName: 'size' },
                            { title: '状态', dataIndex: 'st', width: 150, slotName: 'st' },
                            { title: '操作', dataIndex: 'ops', width: 100, slotName: 'ops', fixed: 'right', align: 'right' },
                        ]">
                        <template #lts="{ record }">
                            <a-tag v-if="record.lts" color="green" size="small">{{ record.lts }}</a-tag>
                        </template>
                        <template #size="{ record }">{{ fmtBytes(record.size) }}</template>
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
                                :disabled="!!installProgress[record.version]"
                                @click="installVersion(record)">安装</a-button>
                            <a-button v-else size="small" @click="emit('refresh-versions')">已安装</a-button>
                        </template>
                    </a-table>
                </a-spin>
                <div class="offline-tip">离线安装：将 node zip 放入 <a-link copyable :content="pluginsDirVal">{{
                    pluginsDirVal
                    || '…' }}</a-link> 后刷新本地版本</div>
            </div>
            <div v-if="installLogs.length" class="ver-block install-log">
                <div class="ver-title">任务日志</div>
                <div class="ls-log small">
                    <div v-for="(l, i) in installLogs" :key="i" class="ls-line">{{ l }}</div>
                </div>
            </div>
        </div>

        <div v-show="verTab === 'chain'" class="ver-pane">
            <a-spin :loading="chainLoading" style="width:100%">
                <div class="chain-hero">
                    <div class="chain-hero-l">
                        <div class="chain-ver">
                            <span class="chain-cap">cmd 里 <b>npm -v</b></span>
                            <b class="chain-big">{{ chain?.activeVersion || '—' }}</b>
                            <a-tag size="small" :color="chain?.activeFrom === 'override' ? 'orange' : 'arcoblue'">{{
                                layerName(chain?.activeFrom) }}</a-tag>
                        </div>
                        <div class="chain-lines">
                            <div class="ls-item"><span>转发壳</span>
                                <a-tooltip :content="chain?.activeShim || '-'" :disabled="!chain?.activeShim">
                                    <b class="ellipsis">{{ chain?.activeShim || '-' }}</b>
                                </a-tooltip>
                            </div>
                            <div class="ls-item"><span>node.exe</span>
                                <a-tooltip :content="chain?.activeNode || '-'" :disabled="!chain?.activeNode">
                                    <b class="ellipsis">{{ chain?.activeNode || '-' }}</b>
                                </a-tooltip>
                            </div>
                            <div class="ls-item"><span>全局 prefix</span>
                                <a-tooltip :content="chain?.activePrefix || '-'" :disabled="!chain?.activePrefix">
                                    <b class="ellipsis">{{ chain?.activePrefix || '-' }}</b>
                                </a-tooltip>
                            </div>
                        </div>
                    </div>
                    <a-space direction="vertical" :size="6">
                        <a-button size="small" :loading="chainLoading" @click="reloadChain()">
                            <template #icon><icon-refresh /></template>重新体检
                        </a-button>
                        <a-button size="small" :loading="relLoading" @click="ensureReleases(true)">
                            <template #icon><icon-history /></template>刷新版本清单
                        </a-button>
                    </a-space>
                </div>
                <div v-if="chainHints.length" class="chain-hints">
                    <div v-for="(h, i) in chainHints" :key="i" class="hint-line">• {{ h }}</div>
                </div>
                <div v-if="chainShadowed.length" class="offline-tip">PATH 里被压住的 npm 入口：{{
                    chainShadowed.join('、') }}</div>

                <div class="ver-title" style="margin-top:14px">npm 入口（点一行选择）</div>
                <a-table :data="chainEntries" size="small" :pagination="false" row-key="dir" @row-click="pickEntryRow"
                    :row-class="(r: any) => (r.dir === cliEntryDir ? 'row-active' : '')" :columns="[
                        { title: '目录', dataIndex: 'dir', ellipsis: true, tooltip: true },
                        { title: 'Node', dataIndex: 'version', width: 92 },
                        { title: 'PATH', dataIndex: 'pathIdx', width: 64, slotName: 'pidx' },
                        { title: '自带层', dataIndex: 'bundled', width: 90, slotName: 'bun' },
                        { title: '覆盖层', dataIndex: 'override', width: 90, slotName: 'ovr' },
                        { title: '实际生效', dataIndex: 'effective', width: 132, slotName: 'eff' },
                    ]">
                    <template #pidx="{ record }">
                        <span v-if="record.pathIdx >= 0">#{{ record.pathIdx }}</span>
                        <a-tag v-else size="small" color="gray">不在</a-tag>
                    </template>
                    <template #bun="{ record }">{{ record.bundled?.exists ? record.bundled.version : '-'
                    }}</template>
                    <template #ovr="{ record }">
                        <span v-if="record.override?.exists">{{ record.override.version }}</span>
                        <span v-else-if="record.override?.dir">无</span>
                        <span v-else>-</span>
                    </template>
                    <template #eff="{ record }">
                        <a-space :size="4">
                            <b>{{ record.effective || '-' }}</b>
                            <a-tag size="small" :color="record.effectiveFrom === 'override' ? 'orange' : 'arcoblue'">{{
                                layerName(record.effectiveFrom) }}</a-tag>
                        </a-space>
                    </template>
                </a-table>
                <div v-if="!chainEntries.length && !chainLoading" class="conn-empty">
                    没扫到带 npm 的目录，先到「Node 版本」页签扫描本机 Node
                </div>

                <template v-if="selEntry">
                    <div class="ver-title" style="margin-top:14px">切换目标</div>
                    <a-radio-group v-model="cliLayer" type="button" size="medium">
                        <a-radio value="override">全局 prefix 覆盖层</a-radio>
                        <a-radio value="bundled">Node 自带层</a-radio>
                    </a-radio-group>
                    <div class="layer-note">{{ layerNote }}</div>
                    <div class="detail-grid tight">
                        <div class="ls-item"><span>写入目录</span>
                            <a-tooltip :content="selLayer?.dir || '-'" :disabled="!selLayer?.dir">
                                <b class="ellipsis">{{ selLayer?.dir || '-' }}</b>
                            </a-tooltip>
                        </div>
                        <div class="ls-item"><span>该层现在</span><b>{{ selLayerNow }}</b></div>
                        <div class="ls-item"><span>该层出厂版本</span><b>{{ selLayerDefault || '-' }}
                                <a-link v-if="canRestoreDefault" size="mini"
                                    @click="cliVersion = selLayerDefault">用它</a-link></b></div>
                        <div class="ls-item"><span>可撤销</span><b>{{ selLayer?.hasPrev ? '有（上一步）' : '无' }}</b></div>
                    </div>
                    <a-row :gutter="6" align="center" style="margin-top:10px">
                        <a-col :span="11">
                            <a-select v-model="cliVersion" size="small" allow-search :loading="relLoading"
                                placeholder="要切到的 npm 版本" :options="releaseOptions" />
                        </a-col>
                        <a-col :span="6">
                            <a-space :size="4">
                                <a-switch v-model="onlyCompat" size="small" @change="pickDefaultVersion" />
                                <span class="chain-cap">只列兼容本 Node</span>
                            </a-space>
                        </a-col>
                        <a-col :span="7">
                            <a-space :size="6">
                                <a-button size="small" :disabled="!!cliBusy" @click="doSwitch()">切换</a-button>
                                <a-button size="small" :disabled="!selLayer?.hasPrev || !!cliBusy"
                                    @click="doUndo()">撤销</a-button>
                                <a-popconfirm v-if="cliLayer === 'override' && selEntry.override?.exists"
                                    content="移除覆盖层并恢复该 Node 自带的 npm？工具生成的 npm/npx 转发壳会一并清掉。"
                                    type="warning" position="lt" :disabled="!!cliBusy" @ok="doRemoveOverride()">
                                    <a-button size="small" status="danger" :disabled="!!cliBusy"
                                        :loading="cliBusy === 'npm@override'">移除覆盖层</a-button>
                                </a-popconfirm>
                            </a-space>
                        </a-col>
                    </a-row>
                    <div v-if="relNote" class="offline-tip">{{ relNote }}</div>
                    <div v-if="!relLoading && !releases.length" class="offline-tip">
                        版本清单为空：检查网络，或在「npm 配置」里换一个可达的 registry 后点「刷新版本清单」
                    </div>
                    <div v-else-if="onlyCompat && !releaseOptions.length" class="offline-tip">
                        当前 Node {{ selEntry.version || '?' }} 没有兼容的 npm 版本（关掉「只列兼容本 Node」可看全部）
                    </div>
                </template>

                <div v-if="cliBusy" class="cli-prog">
                    <span class="cli-name">{{ cliBusy }}</span>
                    <a-progress :percent="(cliProgress?.percent || 0) / 100" size="small" style="flex:1"
                        :status="cliProgress?.stage === 'error' ? 'danger' : 'normal'" />
                    <span class="cli-stage">{{ stageText(cliProgress) }}</span>
                </div>
                <div v-if="cliLogs.length" class="ver-block install-log">
                    <div class="ver-title">切换日志
                        <a-button size="small" @click="cliLogs = []">清空</a-button>
                    </div>
                    <div class="ls-log small">
                        <div v-for="(l, i) in cliLogs" :key="i" class="ls-line">{{ l }}</div>
                    </div>
                </div>
            </a-spin>
        </div>

        <!-- Docker 页签：静态站点的容器模式用 nginx 镜像承载，镜像 tag 就是它的「版本」 -->
        <div v-show="verTab === 'docker'" class="ver-pane">
            <docker-images keyword="nginx" tool-label="静态站点" repo="nginx" :presets="nginxImagePresets"
                container-tool="npm" :instances="instances" :states="states" @create="onCreate"
                @use="(id: string) => emit('edit-instance', id)" @changed="emit('refresh-states')" />
            <div class="layer-note">
                静态站点的 Docker 模式不跑 node.exe：站点目录挂到容器 /usr/share/nginx/html，SPA / 列目录开关写进容器
                nginx.conf，改完点「重启」即生效（不重建容器）。
            </div>
        </div>
    </a-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onBeforeUnmount, onMounted, reactive, ref, watch } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { restoreInstalls, runningTasks } from "@/store/modules/tasks";
import { srcLabel, stageText, fmtBytes, layerName, NGINX_IMAGE_PRESETS, type InstConfig } from "../helpers";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

const props = defineProps<{
    visible: boolean;
    /** 生效链数据：父页面持有（总览 PATH 绑定候选也读它），弹窗只读 */
    chain: any;
    instances: InstConfig[];
    states: Record<string, any>;
    /** 本机 Node 版本：父页面持有，装/删后请求父页面重拉 */
    localVersions: any[];
    chainLoading: boolean;
}>();

const emit = defineEmits<{
    (e: "update:visible", v: boolean): void;
    (e: "create-instance", image: string): void;
    (e: "edit-instance", id: string): void;
    (e: "refresh-states"): void;
    (e: "refresh-versions"): void;
    (e: "reload-chain"): void;
    (e: "refresh-overview"): void;
    (e: "refresh-managers"): void;
}>();

const nginxImagePresets = NGINX_IMAGE_PRESETS;

const verTab = ref<"node" | "chain" | "docker">("node");
const remoteVersions = ref<any[]>([]);
const remoteLoading = ref(false);
const installProgress = reactive<Record<string, any>>({});
const installLogs = ref<string[]>([]);
const localDirInput = ref("");
const pluginsDirVal = ref("");
const scanning = ref(false);

/* 生效链切换（命令行 npm -v）：本页签私有的选择/目标层/版本清单与进度 */
const cliEntryDir = ref("");
const cliLayer = ref("override");
const releases = ref<any[]>([]);
const relLoading = ref(false);
const relNote = ref("");
const relForNode = ref("");
const cliVersion = ref("");
const onlyCompat = ref(true);
const cliBusy = ref("");
const cliProgress = ref<any>(null);
const cliLogs = ref<string[]>([]);

const installedSet = computed(() => new Set<string>(props.localVersions.map((v: any) => v.version)));

const chainEntries = computed<any[]>(() => props.chain?.entries || []);
const chainHints = computed<string[]>(() => props.chain?.hints || []);
const chainShadowed = computed<string[]>(() => props.chain?.shadowed || []);

const selEntry = computed(() => chainEntries.value.find((e: any) => e.dir === cliEntryDir.value) || null);
const selLayer = computed(() => (selEntry.value ? selEntry.value[cliLayer.value === "override" ? "override" : "bundled"] : null));
const selNodeVer = computed(() => selEntry.value?.version || props.chain?.activeVersion || "");
const selLayerNow = computed(() => {
    const l = selLayer.value;
    if (!l?.exists) return "该层还不存在，切到这里等于新装一份";
    return `${l.version}${l.engines ? "　engines " + l.engines : ""}`;
});
const selLayerDefault = computed(() => (cliLayer.value === "override" ? selEntry.value?.overrideDefault : selEntry.value?.bundledDefault) || "");
const canRestoreDefault = computed(() => !!selLayerDefault.value && selLayerDefault.value !== selLayer.value?.version);
const releaseOptions = computed(() => {
    const cur = selLayer.value?.version || "";
    const list = onlyCompat.value ? releases.value.filter((r: any) => r.compatible) : releases.value;
    return list.map((r: any) => {
        const marks: string[] = [];
        if (r.tag) marks.push(r.tag);
        if (r.version === cur) marks.push("当前");
        if (!r.compatible) marks.push("需 Node " + (r.engines || "?"));
        return { label: r.version + (marks.length ? `（${marks.join(" · ")}）` : ""), value: r.version };
    });
});
const layerNote = computed(() => cliLayer.value === "override"
    ? "写进 npm prefix -g 的目录：Node 的 npm.cmd 每次执行都先跳去这里，所以切它不需要管理员、不用改 PATH，已经开着的 cmd 下一条命令就生效。多个 Node 共用同一个 prefix 时会一起变。"
    : "改这个 Node 目录里的 node_modules\\npm：只有这个 Node 会变，但工具里用它的地方（含静态站点服务）会跟着变；文件被跑着的 npm/node 占用时会切不动。");

function pushInstallLog(txt: string) {
    if (!txt) return;
    const t = new Date();
    const pad = (n: number) => n.toString().padStart(2, "0");
    installLogs.value.push(`[${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}] ${txt}`);
    if (installLogs.value.length > 200) installLogs.value.splice(0, installLogs.value.length - 200);
}
function pushCliLog(txt: string) {
    if (!txt) return;
    const t = new Date();
    const pad = (n: number) => n.toString().padStart(2, "0");
    cliLogs.value.push(`[${pad(t.getHours())}:${pad(t.getMinutes())}:${pad(t.getSeconds())}] ${txt}`);
    if (cliLogs.value.length > 200) cliLogs.value.splice(0, cliLogs.value.length - 200);
}

function reloadChain() { emit("reload-chain"); }

/* ============================ Node 版本管理 ============================ */
async function loadRemoteVersions() {
    remoteLoading.value = true;
    try { remoteVersions.value = (await GoTools.NpmSrvRemoteVersions()) || []; }
    catch (e: any) { Message.error("获取远程版本失败: " + (e?.message || e)); }
    remoteLoading.value = false;
}
async function installVersion(r: any) {
    try {
        pushInstallLog(`提交安装请求：Node.js ${r.version}`);
        await GoTools.NpmSrvInstall(r.version);
        installProgress[r.version] = { stage: "downloading", percent: 0 };
    } catch (e: any) {
        pushInstallLog(`安装请求被拒绝：${e?.message || e}`);
        Message.error("安装失败: " + (e?.message || e));
    }
}
async function removeVersion(v: any) {
    try { await GoTools.NpmSrvRemoveVersion(v.version); Message.success("已删除"); emit("refresh-versions"); }
    catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
}
async function chooseLocalDir() {
    try { const dir = await GoTools.NpmSrvChooseDir(); if (dir) localDirInput.value = dir; }
    catch (e) { /* cancel */ }
}
async function addLocalDir() {
    const dir = localDirInput.value.trim();
    if (!dir) { Message.warning("请先选择或填写目录"); return; }
    try { await GoTools.NpmSrvAddLocal(dir); Message.success("已添加本机目录"); localDirInput.value = ""; emit("refresh-versions"); }
    catch (e: any) { Message.error("添加失败: " + (e?.message || e)); }
}
// 扫描本机已有 Node（PATH / 注册表 / nvm、fnm、volta 版本仓库）并登记为「本机」来源
async function scanLocalNodes() {
    scanning.value = true;
    try {
        const added = (await GoTools.NpmSrvScanLocal()) || [];
        const list = Array.isArray(added) ? added : (added?.result1 || []);
        emit("refresh-versions");
        if (list.length) {
            const detail = list.map((v: any) => `${v.version}（${v.path}）`).join("；");
            Message.success(`新登记 ${list.length} 个本机 Node：${list.map((v: any) => v.version).join("、")}`);
            pushInstallLog(`扫描本机 Node：新登记 ${list.length} 个 - ${detail}`);
        } else {
            Message.info("扫描完成，未发现新的本机 Node.js");
        }
    }
    catch (e: any) { Message.error("扫描失败: " + (e?.message || e)); }
    finally { scanning.value = false; }
}

/* ============================ npm 生效链切换 ============================ */
// 默认落在覆盖层 —— 用户要的是「cmd 里的 npm 变」，而且它不碰 Node 本体
function applyLayerDefault() {
    const e = selEntry.value;
    if (!e) return;
    cliLayer.value = e.override?.dir ? "override" : "bundled";
    cliVersion.value = "";
    pickDefaultVersion();
}
function pickEntryRow(record: any) {
    if (!record?.dir || record.dir === cliEntryDir.value) return;
    cliEntryDir.value = record.dir;
    applyLayerDefault();
    ensureReleases();
}
async function ensureReleases(force = false) {
    const nv = selNodeVer.value;
    if (!force && relForNode.value === nv && releases.value.length) { pickDefaultVersion(); return; }
    relLoading.value = true;
    try {
        const r: any = force ? await GoTools.NpmCliRefreshReleases(nv) : await GoTools.NpmCliReleases(nv);
        releases.value = r?.releases || [];
        relNote.value = r?.note || "";
        relForNode.value = nv;
        pickDefaultVersion();
    } catch (e: any) {
        releases.value = [];
        Message.error("获取 npm 版本清单失败: " + String(e?.message || e).slice(0, 220));
    }
    relLoading.value = false;
}
// 预选最新可用版，但不选成当前这份了（不然一键就白做）
function pickDefaultVersion() {
    const opts = releaseOptions.value;
    if (cliVersion.value && opts.some((o: any) => o.value === cliVersion.value)) return;
    const cur = selLayer.value?.version || "";
    cliVersion.value = opts.find((o: any) => o.value !== cur)?.value || opts[0]?.value || "";
}
function doSwitch() {
    const e = selEntry.value, l = selLayer.value;
    if (!e || !l?.dir) { Message.warning("先选一个 npm 入口与目标层"); return; }
    const v = cliVersion.value.trim();
    if (!v) { Message.warning("选择要切到的 npm 版本"); return; }
    if (l.exists && l.version === v) { Message.info(`该层已经是 npm ${v}`); return; }
    const bad = releases.value.find((r: any) => r.version === v && !r.compatible);
    Modal.warning({
        title: cliLayer.value === "override" ? "往覆盖层装 npm" : "改这个 Node 自带的 npm",
        hideCancel: false,
        content: `${layerName(cliLayer.value)}：npm ${l.exists ? l.version : "（尚无）"} → ${v}　｜　目录 ${l.dir}`
            + (bad ? `　｜　注意：${bad.why}，装完会实测 npm -v，跑不通自动回滚` : ""),
        onOk: async () => {
            try {
                cliBusy.value = `npm@${v}`;
                cliProgress.value = { stage: "downloading", percent: 0 };
                pushCliLog(`提交切换：${layerName(cliLayer.value)} → npm ${v}`);
                await GoTools.NpmCliSwitch(cliLayer.value, cliEntryDir.value, v);
            } catch (err: any) {
                cliBusy.value = "";
                cliProgress.value = null;
                Message.error("切换失败: " + String(err?.message || err).slice(0, 220));
            }
        },
    });
}
function doUndo() {
    const l = selLayer.value;
    if (!l?.dir) return;
    Modal.warning({
        title: "撤销上一步切换", hideCancel: false,
        content: `把${layerName(cliLayer.value)}的 npm 换回切换前留存的旧版本（目录 ${l.dir}）。`,
        onOk: async () => {
            try {
                cliBusy.value = `npm@undo`;
                cliProgress.value = { stage: "info", percent: 0 };
                pushCliLog(`提交撤销：${layerName(cliLayer.value)}`);
                await GoTools.NpmCliUndoSwitch(cliLayer.value, cliEntryDir.value);
            } catch (e: any) {
                cliBusy.value = "";
                cliProgress.value = null;
                Message.error("撤销失败: " + String(e?.message || e).slice(0, 220));
            }
        },
    });
}
async function doRemoveOverride() {
    const e = selEntry.value;
    if (!e?.override?.dir) return;
    try {
        cliBusy.value = "npm@override";
        cliProgress.value = { stage: "info", percent: 0 };
        pushCliLog("提交移除覆盖层");
        await GoTools.NpmCliRemoveOverride(cliEntryDir.value);
    } catch (err: any) {
        cliBusy.value = "";
        cliProgress.value = null;
        Message.error("移除失败: " + String(err?.message || err).slice(0, 220));
    }
}

/** 切到生效链页签才体检一次：PATH、prefix 都可能被外部改动，但开框不必抢这几毫秒 */
function onVerTabChange(k: any) {
    if (k !== "chain") return;
    // 总览页已经拉过一次生效链时不重拉；版本清单自身带缓存，直接调安全
    if (!props.chain) reloadChain();
    ensureReleases();
}

function onCreate(image: string) {
    emit("create-instance", image);
    emit("update:visible", false);
}

// chain prop 更新后校正默认入口：PATH 顺序里第一个匹配的、否则第一行
watch(chainEntries, (es) => {
    if (!es.some((e: any) => e.dir === cliEntryDir.value)) {
        cliEntryDir.value = es.find((e: any) => e.dir === props.chain?.activeDir)?.dir || es[0]?.dir || "";
        applyLayerDefault();
    }
});

// 打开时重置：默认 Node 页签、拉线上版本与插件目录（与原来 openVersionsModal 时序一致）
watch(() => props.visible, async (open) => {
    if (!open) return;
    verTab.value = "node";
    emit("refresh-versions");
    if (!pluginsDirVal.value) {
        try { pluginsDirVal.value = (await GoTools.NpmSrvPluginsDir()) || ""; } catch (e) { /* ignore */ }
    }
    if (remoteVersions.value.length === 0) loadRemoteVersions();
});

let offInstall: any = null;
let offNpmVer: any = null;
// 弹窗常驻挂载：安装进度、生效链切换进度与断线恢复都在此订阅（overview 相关刷新交给父页面）
onMounted(() => {
    // 离开页面时还在装/还在切版本：从任务台账把页面自己的进度补回来
    restoreInstalls("npm", "install", installProgress, pushInstallLog);
    runningTasks("npm", "npmver").then((list) => {
        const t = list[0];
        if (!t) return;
        cliBusy.value = t.name;
        cliProgress.value = { stage: t.stage, percent: t.percent, note: t.note };
        pushCliLog(`接管未完成的切换：${t.title}（${t.percent}%）`);
    });
    offInstall = Events.On("npm:install", (ev: any) => {
        const p = ev?.data ?? ev;
        if (!p || !p.version) return;
        if (p.stage === "info") {
            pushInstallLog(p.note || "");
        } else if (p.stage === "done") {
            delete installProgress[p.version];
            pushInstallLog(`完成：${p.note || p.version}`);
            Message.success(`${p.version} 任务完成`);
            emit("refresh-versions");
        } else if (p.stage === "error") {
            if (installProgress[p.version]) installProgress[p.version] = p;
            pushInstallLog(`失败 ${p.version}：${p.note || "未知错误"}`);
            Modal.error({ title: "任务失败", content: `${p.version}：${p.note || "未知错误"}` });
        } else {
            installProgress[p.version] = p;
        }
    });
    offNpmVer = Events.On("npm:npmver", async (ev: any) => {
        const p = ev?.data ?? ev;
        if (!p || !p.version) return;
        if (p.stage === "info") {
            pushCliLog(p.note || "");
        } else if (p.stage === "done") {
            cliBusy.value = "";
            cliProgress.value = null;
            pushCliLog(`完成：${p.note || p.version}`);
            Message.success(`npm 已切到 ${p.version.replace("npm@", "")}`);
            reloadChain();
            if (cliVersion.value === selLayer.value?.version) cliVersion.value = "";
            pickDefaultVersion();
            // 面板里的 npm 版本、包管理器列表、全局包归属全部跟着变
            emit("refresh-versions");
            emit("refresh-managers");
            emit("refresh-overview");
        } else if (p.stage === "error") {
            cliBusy.value = "";
            cliProgress.value = null;
            pushCliLog(`失败：${p.note || "未知错误"}`);
            // 失败可能只回了一半（自动回滚也试过），重新体检一次拿真实现场
            reloadChain();
            Modal.error({ title: "npm 切换失败", content: String(p.note || "未知错误").slice(0, 400) });
        } else {
            cliProgress.value = p;
        }
    });
});
onBeforeUnmount(() => {
    if (offInstall) offInstall();
    if (offNpmVer) offNpmVer();
});
</script>

<style scoped>
.ver-switch {
    margin-bottom: 12px;
}

.ver-pane {
    min-height: 0;
}

.conn-empty {
    color: var(--color-text-3);
    font-size: 12px;
    padding: 6px 0;
}

.offline-tip {
    margin-top: 8px;
    font-size: 12px;
    color: var(--color-text-3);
    word-break: break-all;
}

.add-local {
    margin-top: 10px;
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

.install-log {
    margin-top: 12px;
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

.ls-item {
    display: flex;
    justify-content: space-between;
    font-size: 12px;
    gap: 8px;
}

.ls-item span {
    color: var(--color-text-3);
    flex-shrink: 0;
}

.ls-item b {
    font-weight: 500;
    text-align: right;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.chain-hero {
    display: flex;
    justify-content: space-between;
    align-items: flex-start;
    gap: 12px;
    padding: 10px 12px;
    border: 1px solid var(--color-border-2);
    border-radius: 6px;
    background: var(--color-fill-1);
}

.chain-hero-l {
    min-width: 0;
    flex: 1;
}

.chain-ver {
    display: flex;
    align-items: baseline;
    gap: 8px;
    flex-wrap: wrap;
}

.chain-cap {
    font-size: 12px;
    color: var(--color-text-3);
}

.chain-big {
    font-size: 22px;
    font-weight: 600;
    font-family: Consolas, Menlo, monospace;
    color: rgb(var(--primary-6));
}

.chain-lines {
    margin-top: 6px;
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-width: 600px;
}

.chain-hints {
    margin-top: 8px;
    display: flex;
    flex-direction: column;
    gap: 4px;
}

.hint-line {
    font-size: 12px;
    color: var(--color-text-2);
    line-height: 1.6;
}

.layer-note {
    margin: 8px 0 4px;
    font-size: 12px;
    line-height: 1.6;
    color: var(--color-text-3);
}

.detail-grid.tight {
    border-bottom: none;
    padding: 4px 0;
    grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
}

:deep(.row-active) {
    background: var(--color-primary-light-1);
}

.cli-prog {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-top: 12px;
}

.cli-name {
    font-size: 12px;
    font-family: Consolas, Menlo, monospace;
    min-width: 96px;
}

.cli-stage {
    font-size: 12px;
    color: var(--color-text-3);
    width: 92px;
    text-align: right;
}
</style>
