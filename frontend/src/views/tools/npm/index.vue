<template>
    <a-row class="npm-page" :gutter="6">
        <!-- 左侧：静态站点实例 + 功能入口 -->
        <a-col :span="6" class="side-col">
            <a-card class="side-card card-head-tool" size="small" :body-style="{ padding: 0 }">
                <template #title>静态站点服务</template>
                <template #extra>
                    <div class="srv-head-actions">
                        <a-button size="small" @click="openInstModal()">新建站点</a-button>
                    </div>
                </template>
                <!-- 引擎没起时先把话说明白：否则要等点「启动」撞一条管道错误才知道是 Docker 没跑 -->
                <common-docker-off class="srv-docker-off" :count="dockerInstCount" tool-label="静态站点" />
                <a-list :bordered="false" size="small">
                    <a-list-item v-for="inst in orderedInstances" :key="inst.id"
                        :class="{ 'inst-active': selectedId === inst.id, 'reorder-dragging': npmReo.isDragging(inst.id), 'reorder-over': npmReo.isOver(inst.id) }"
                        v-bind="npmReo.handlers(inst.id)" @click="selectInst(inst.id)" @dblclick="dblInst(inst)">
                        <a-space>
                            <com-badge :status="states[inst.id]?.running ? 'success' : 'normal'"
                                :animated="!!states[inst.id]?.running" />
                            <a-tooltip :content="inst.name + ':' + inst.port">
                                <span class="conn-name">{{ inst.name }}</span>
                            </a-tooltip>
                            <a-tag v-if="inst.mode === 'docker'" color="purple" size="small">{{ inst.image || 'Docker'
                            }}</a-tag>
                            <a-tag v-else color="arcoblue" size="small">node {{ inst.version }}</a-tag>
                        </a-space>
                        <template #actions>
                            <a-dropdown trigger="hover" @select="(v: any) => onInstAction(v, inst)">
                                <icon-more class="conn-more" @click.stop />
                                <template #content>
                                    <a-doption v-if="!states[inst.id]?.running" value="start"><icon-play-arrow />
                                        启动</a-doption>
                                    <a-doption v-else value="stop"><icon-pause /> 停止</a-doption>
                                    <a-doption value="restart"><icon-refresh /> 重启</a-doption>
                                    <a-doption value="open"><icon-launch /> 打开站点</a-doption>
                                    <a-doption value="edit"><icon-edit /> 编辑</a-doption>
                                    <a-doption value="del"><icon-delete /> 删除</a-doption>
                                </template>
                            </a-dropdown>
                        </template>
                    </a-list-item>
                    <a-list-item v-if="instances.length === 0">
                        <div class="conn-empty">暂无站点，点击右上"新建站点"</div>
                    </a-list-item>
                </a-list>
            </a-card>
            <a-card class="side-card tools-card card-head-tool" size="small" :body-style="{ padding: '10px' }">
                <template #title>npm 工具箱</template>
                <div class="toolbox">
                    <a-button long class="tool-btn" @click="openNpmConfig()">
                        <template #icon><icon-settings /></template>
                        <span class="tool-btn-t">npm 配置 / 镜像源</span>
                        <icon-right class="tool-btn-r" />
                    </a-button>
                    <a-button long class="tool-btn" @click="openVersionsModal()">
                        <template #icon><icon-storage /></template>
                        <span class="tool-btn-t">Node / npm 版本管理</span>
                        <icon-right class="tool-btn-r" />
                    </a-button>
                    <a-button long class="tool-btn" @click="openPkgs()">
                        <template #icon><icon-storage /></template>
                        <span class="tool-btn-t">全局包管理</span>
                        <icon-right class="tool-btn-r" />
                    </a-button>
                </div>
                <div class="reg-hint">当前 registry：{{ registryVal || '-' }}</div>
            </a-card>
        </a-col>

        <!-- 右侧：环境总览 / 站点详情 -->
        <a-col :span="18" class="main-col">
            <a-card class="main-panel card-head-tool" :body-style="{ padding: 0 }">
                <template #title>
                    <a-radio-group size="medium" v-model="rightMode" type="button">
                        <a-radio value="overview">环境总览</a-radio>
                        <a-radio value="detail" :disabled="!selectedId">站点详情</a-radio>
                    </a-radio-group>
                </template>
                <template #extra>
                    <a-space v-if="rightMode === 'detail' && selCfg">
                        <com-badge :status="selState?.running ? 'success' : 'normal'" :animated="!!selState?.running" />
                        <span>{{ selCfg?.name }}</span>
                        <a-tag v-if="isDockerSel" color="purple" size="small">{{ selState?.image || selCfg?.image ||
                            'Docker' }}</a-tag>
                        <a-tag color="arcoblue" size="small">:{{ selCfg?.port }}</a-tag>
                        <a-tag v-if="selCfg?.spa" size="small">SPA</a-tag>
                        <a-tag v-if="selCfg?.directory" size="small">列目录</a-tag>
                        <a-button size="small" :disabled="selState?.running || stopBusy" @click="doStart">启动</a-button>
                        <!-- 停止会断开正在服务的请求，先气泡确认；按钮转 loading 显示「停止中」 -->
                        <a-popconfirm :content="`停止站点「${selCfg?.name}」？正在处理的请求会中断。`" type="warning"
                            position="lt" :disabled="!selState?.running || stopBusy" @ok="doStop">
                            <a-button size="small" status="danger" :disabled="!selState?.running || stopBusy"
                                :loading="stopBusy">{{ stopBusy ? '停止中' : '停止' }}</a-button>
                        </a-popconfirm>
                        <a-tooltip :content="isDockerSel ? '容器模式：重启 = 重启容器（会短暂中断），改过的 nginx.conf 随之生效' : '停掉内置服务器后重新拉起'">
                            <a-button size="small" :disabled="stopBusy" @click="doRestart">重启</a-button>
                        </a-tooltip>
                        <a-button size="small" @click="openLogModal(selCfg!.id)">
                            <template #icon><icon-face-folder-open /></template>日志
                        </a-button>
                        <a-button size="small" :disabled="!selState?.running"
                            @click="openSite(selCfg?.port)">在新窗口打开</a-button>
                    </a-space>
                    <a-button v-else size="small" :loading="ovLoading" @click="loadOverview()">刷新</a-button>
                </template>

                <!-- 环境总览 -->
                <div v-show="rightMode === 'overview'" class="ov-body">
                    <a-spin :loading="ovLoading" style="width:100%">
                        <a-alert v-if="pathStatus?.systemHasNode" type="warning" show-icon class="ov-alert">
                            检测到系统 PATH 里也有 Node 目录。Windows 组合顺序为「系统 PATH + 用户 PATH」，系统项优先——只绑「用户
                            PATH」会被系统里的 Node 盖过，需选「系统 PATH（管理员）」才能真正生效。
                        </a-alert>

                        <a-row :gutter="8" class="ov-top">
                            <!-- 绑定现状与绑定操作合成一张卡：状态用 descriptions，不再五行散排 -->
                            <a-col :span="12">
                                <a-card class="ov-card" size="small">
                                    <template #title>生效 npm / PATH 绑定</template>
                                    <template #extra>
                                        <!-- PATH 绑定是系统现状，用圆点+文案，与各页运行状态一致 -->
                                        <span class="com-state" :class="pathStatus?.bound ? 'is-on' : 'is-dim'">
                                            <com-badge :status="pathStatus?.bound ? 'success' : 'normal'" />
                                            {{ pathStatus?.bound ? '已绑定' : '未绑定' }}
                                        </span>
                                    </template>
                                    <a-descriptions :column="1" size="small" :data="bindDesc" class="ov-desc" />
                                    <div v-if="pathStatus?.otherNodeDirs?.length" class="ov-others">
                                        <div class="ov-others-t">PATH 中其它 Node 目录（绑定时将移除）</div>
                                        <a-tooltip v-for="(d, i) in pathStatus.otherNodeDirs" :key="i" :content="d">
                                            <div class="ov-other-dir">{{ d }}</div>
                                        </a-tooltip>
                                    </div>
                                    <a-divider :margin="12" />
                                    <div class="ov-bind">
                                        <a-select v-model="bindDir" :options="bindDirOptions"
                                            placeholder="选择要绑定的 Node 目录" allow-search />
                                        <div class="ov-bind-row">
                                            <a-radio-group size="medium" v-model="bindScope" type="button">
                                                <a-radio value="user">用户PATH</a-radio>
                                                <a-radio value="system">系统PATH[管理员]</a-radio>
                                            </a-radio-group>
                                            <a-button size="small" :loading="bindBusy" :disabled="!bindDir"
                                                @click="doBindPath">强制绑定</a-button>
                                            <a-button size="small" v-if="pathStatus?.canUndo" status="warning"
                                                :loading="bindBusy" @click="doUnbindPath">撤销绑定</a-button>
                                        </div>
                                    </div>
                                    <div v-if="pathStatus?.note" class="ov-note">{{ pathStatus.note }}</div>
                                </a-card>
                            </a-col>

                            <!-- 本机 Node 与包管理器：从整屏宽改成右栏上下两张表 -->
                            <a-col :span="12">
                                <a-card class="ov-card" size="small" :body-style="{ padding: 0 }">
                                    <template #title>本机 Node</template>
                                    <template #extra><a-tag size="small">{{ localVersions.length }}</a-tag></template>
                                    <a-table :data="localVersions" size="small" :pagination="false" row-key="path"
                                        :scroll="{ y: 150 }" :columns="[
                                            { title: '版本', dataIndex: 'version', width: 84 },
                                            { title: 'npm', dataIndex: 'npmVer', width: 70 },
                                            { title: '来源', dataIndex: 'source', width: 62, slotName: 'source' },
                                            { title: '路径', dataIndex: 'path', ellipsis: true, tooltip: true },
                                        ]" :bordered="false">
                                        <template #source="{ record }">
                                            <a-tag size="small"
                                                :color="record.source === 'bundled' ? 'purple' : record.source === 'downloaded' ? 'green' : 'orange'">{{
                                                    srcLabel(record.source) }}</a-tag>
                                        </template>
                                    </a-table>
                                </a-card>
                                <a-card class="ov-card mt8" size="small" :body-style="{ padding: 0 }">
                                    <template #title>包管理器</template>
                                    <template #extra><a-tag size="small">{{ managers.length }}</a-tag></template>
                                    <a-table :data="managers" size="small" :pagination="false" row-key="id"
                                        :scroll="{ y: 150 }" :columns="[
                                            { title: '名称', dataIndex: 'name', width: 76, slotName: 'name' },
                                            { title: '版本', dataIndex: 'version', width: 70 },
                                            { title: '类型', dataIndex: 'kind', width: 96, slotName: 'kind' },
                                            { title: '入口', dataIndex: 'path', ellipsis: true, tooltip: true },
                                        ]" :bordered="false">
                                        <template #name="{ record }">
                                            <a-tag size="small" :color="mgrColor(record.name)">{{ record.name }}</a-tag>
                                        </template>
                                        <template #kind="{ record }">
                                            <span>{{ record.kind === 'node' && record.nodeVer ? '随 Node ' +
                                                record.nodeVer : (record.kind === 'global' ? '全局安装' : record.kind ===
                                                    'native' ? '原生' : record.kind) }}</span>
                                        </template>
                                    </a-table>
                                </a-card>
                            </a-col>
                        </a-row>

                        <!-- 全局包：按管理器计数 + registry -->
                        <a-card class="ov-card" size="small" style="margin-top: 8px">
                            <template #title>全局包</template>
                            <template #extra>
                                <a-space :size="8">
                                    <a-tag size="small" color="arcoblue">{{ packages.length }} 个</a-tag>
                                    <a-link size="small" @click="openPkgs()">管理<template
                                            #icon><icon-right /></template></a-link>
                                </a-space>
                            </template>
                            <a-row>
                                <a-col v-for="g in pkgStats" :key="g.title" :span="4">
                                    <a-statistic :value="g.count" :title="g.title"
                                        :value-style="{ fontSize: '20px' }" />
                                </a-col>
                            </a-row>
                            <div class="ov-note">当前 registry：<a-link size="small" copyable :content="registryVal">{{
                                registryVal
                                || '-' }}</a-link></div>
                        </a-card>
                    </a-spin>
                </div>

                <!-- 站点详情 -->
                <div v-show="rightMode === 'detail'" class="detail-body">
                    <div class="ov-inner">
                        <!-- 关键指标：版式对齐 nginx（彩色圆角图标 + 标签/数值同排 + 下方明细） -->
                        <a-row :gutter="[12, 12]" class="stat-row">
                            <a-col v-for="c in npmStatCards" :key="c.key" :xs="24" :sm="12" :md="8">
                                <common-stat-card :icon="c.icon" :tone="c.tone" :label="c.title" :value="c.value"
                                    :detail="c.sub" :tip="c.tip" :ready="c.ready" />
                            </a-col>
                        </a-row>

                        <!-- 快捷操作：日志弹窗已有，这里补齐高频入口 -->
                        <div class="quick-bar">
                            <a-button size="small" :disabled="!selState?.running" @click="openSite(selCfg?.port)">
                                <template #icon><icon-launch /></template>打开站点
                            </a-button>
                            <a-button size="small" @click="openLogModal(selCfg!.id)">
                                <template #icon><icon-face-folder-open /></template>查看日志
                            </a-button>
                        </div>

                        <!-- 详细信息：通用信息行（标签 | 值 | 操作），路径类可一键打开 -->
                        <div class="info-section">
                            <div class="info-title">站点信息</div>
                            <div class="info-list">
                                <common-info-row grey v-if="isDockerSel" label="容器"
                                    :tip="'容器 ID：' + (selState?.containerId || '-') + '（状态 ' + (selState?.containerState || '-') + '）'"
                                    :value="selState?.container || selCfg?.containerName || '-'" />
                                <common-info-row grey v-else label="进程 PID" :value="String(selState?.pid || '-')" />
                                <common-info-row grey label="站点目录" :value="selState?.docRoot || selCfg?.docRoot || '-'"
                                    action-text="打开" @action="openPath(selState?.docRoot || selCfg?.docRoot)" />
                                <common-info-row grey v-if="!isDockerSel" label="node.exe"
                                    :value="selState?.nodePath || '-'" action-text="打开" @action="openPath(selState?.nodePath)" />
                                <common-info-row grey v-else label="容器内挂载"
                                    value="站点 → /usr/share/nginx/html · 配置 → /go-tools/conf/nginx.conf · 日志 → /var/log/nginx" />
                                <common-info-row grey v-if="!isDockerSel" label="registry" :value="selState?.registry || '-'" />
                                <common-info-row grey label="运行模式" :value="selState?.mode === 'docker' ? 'Docker 容器' : '本地进程（内置 Go 服务器）'" />
                                <common-info-row grey label="站点行为"
                                    :value="`SPA ${selCfg?.spa ? '开' : '关'} · 列目录 ${selCfg?.directory ? '允许' : '禁止'} · 自动开浏览器 ${selCfg?.openBrowser ? '是' : '否'}`" />
                            </div>
                        </div>
                    </div>
                </div>
            </a-card>
        </a-col>
    </a-row>

    <!-- 新建/编辑站点实例：表单/镜像候选/引擎状态/端口探测/保存逻辑全部在子组件，保存成功后重拉实例列表 -->
    <instance-modal v-model:visible="instVisible" :edit="editingInst" :preset-image="presetImage"
        :instances="instances" :states="states" @saved="loadInstances" />

    <!-- npm 配置：配置项/registry 切换/原文编辑/同步用户全局全部在子组件，registry 变更回传本页显示 -->
    <npm-config-modal v-model:visible="cfgVisible" :registry="registryVal"
        @update:registry="(v: string) => registryVal = v" />

    <!-- 全局包管理：包管理器选择/过滤/装卸在子组件；managers/packages 由本页持有（总览也展示），打开时 prepare 重扫、装卸后 rescan -->
    <pkg-manage-modal v-model:visible="pkgsVisible" :managers="managers" :packages="packages"
        :mgr-loading="mgrLoading" :pkgs-loading="pkgsLoading" @prepare="onPkgsPrepare" @rescan="onPkgsRescan" />

    <!-- Node / npm 版本管理：Node 版本/npm 生效链/Docker 三页签与安装进度、生效链切换、任务日志全部在子组件。
         chain/localVersions/managers 等跨切态由本页持有（总览也要用），弹窗只读 + 通过事件请求重拉 -->
    <versions-modal v-model:visible="versionsVisible" :chain="chain" :instances="instances" :states="states"
        :local-versions="localVersions" :chain-loading="chainLoading" @create-instance="onCreateFromImage"
        @edit-instance="onEditInstanceById" @refresh-states="refreshStates" @refresh-versions="loadLocalVersions"
        @reload-chain="loadChain" @refresh-overview="loadOverview" @refresh-managers="loadManagers" />

    <!-- 站点访问日志（实时）：点击「日志」弹出，排版走通用 common-log-modal（数据与轮询仍在本页面） -->
    <common-log-modal v-model="logVisible" :title="`访问日志 · ${selCfg?.name || ''}`" :lines="logLines" :width="760"
        @close="stopLogPolling">
        <template #head>
            <span class="com-state" :class="selState?.running ? 'is-on' : 'is-dim'">
                <com-badge :status="selState?.running ? 'success' : 'normal'" :animated="!!selState?.running" />
                {{ selState?.running ? '运行中' : '已停止' }}
            </span>
            <a-tag color="arcoblue" size="small">:{{ selCfg?.port }}</a-tag>
            <!-- 容器模式的日志来自挂载到宿主机的 access.log，来源与本地不一样，先标清楚 -->
            <a-tag v-if="isDockerSel" size="small" color="purple">容器 access.log</a-tag>
        </template>
        <template #operations>
            <a-button size="small" @click="logLines = []">
                <template #icon><icon-delete /></template>清空
            </a-button>
        </template>
    </common-log-modal>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, onUnmounted, reactive, ref } from "vue";
import { Message, Modal } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { useRoute } from "vue-router";
import { useReorder, applyOrder, loadOrder } from "@/utils/reorder";
import { srcLabel, mgrColor, scopeLabel, buildNpmStatCards, type InstConfig } from "./helpers";
import InstanceModal from "./components/InstanceModal.vue";
import NpmConfigModal from "./components/NpmConfigModal.vue";
import PkgManageModal from "./components/PkgManageModal.vue";
import VersionsModal from "./components/VersionsModal.vue";

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

/* ============================ 状态 ============================ */
const instances = ref<InstConfig[]>([]);
/* 拖拽排序：站点列表顺序存入用户配置 npm:list（与主页分开，随导出搬运） */
const npmOrder = ref<string[]>([]);
const npmReo = useReorder("npm:list", {
    getVisible: () => instances.value.map((i) => i.id),
    onReordered: (nv) => { npmOrder.value = nv; void GoTools?.SetListOrder?.("npm:list", nv); },
});
const orderedInstances = computed(() => applyOrder(instances.value, npmOrder.value, (i) => i.id));
const states = reactive<Record<string, any>>({});
const selectedId = ref("");
const logLines = ref<string[]>([]);
const logVisible = ref(false);
let logTimer: any = null;
let srvTimer: any = null;
const route = useRoute();
let offSelInst: any = null;
let offOpenPanel: any = null;

const selCfg = computed(() => instances.value.find((i) => i.id === selectedId.value) || null);
const selState = computed(() => (selectedId.value ? states[selectedId.value] : null));
/** 当前站点是不是容器模式：配置优先（列表里就能看到刚改完还没启动的站点），状态兜底 */
const isDockerSel = computed(() => (selCfg.value?.mode || selState.value?.mode) === "docker");

/* 站点详情指标卡：格式化逻辑在 helpers，随 selState/selCfg 变化重算 */
const npmStatCards = computed(() => buildNpmStatCards(selState.value, selCfg.value));

/* 弹窗可见性：实例/配置/包管理/版本管理四个弹窗的 UI 逻辑已各自拆到 components/*.vue */
const instVisible = ref(false);
const versionsVisible = ref(false);
const cfgVisible = ref(false);
const pkgsVisible = ref(false);
/* 传给实例弹窗的编辑目标 / 从版本管理带入的镜像 */
const editingInst = ref<InstConfig | null>(null);
const presetImage = ref("");

/* 跨弹窗共享态：既被环境总览读取，又作为 props 下发给子弹窗 */
const localVersions = ref<any[]>([]);
const registryVal = ref("");
const managers = ref<any[]>([]);
const mgrLoading = ref(false);
const packages = ref<any[]>([]);
const pkgsLoading = ref(false);
const chain = ref<any>(null);
const chainLoading = ref(false);

/* 环境总览 / 站点详情模式切换 + PATH 绑定 */
const rightMode = ref<"overview" | "detail">("overview");
const ovLoading = ref(false);
const pathStatus = ref<any>(null);
const bindDir = ref("");
const bindBusy = ref(false);
const bindScope = ref<"user" | "system">("user");

/** 站点里 Docker 模式的实例数：大于 0 时页面才需要挂 Docker 引擎告示 */
const dockerInstCount = computed(() => instances.value.filter((i) => i.mode === "docker").length);

/* 环境总览：绑定候选来自生效链里含 node.exe 的入口目录；全局包按管理器计数 */
const bindDirOptions = computed(() => ((chain.value?.entries || []) as any[])
    .filter((e: any) => e.hasNode)
    .map((e: any) => ({
        label: `node ${e.version || "?"}（npm ${e.effective || e.bundled?.version || "-"}）　${e.dir}`,
        value: e.dir,
    })));
const pkgByManager = computed(() => {
    const m: Record<string, number> = {};
    for (const p of packages.value) m[p.manager] = (m[p.manager] || 0) + 1;
    return Object.entries(m).map(([manager, count]) => ({ manager, count })).sort((a, b) => b.count - a.count);
});
/* 绑定现状：交给 a-descriptions 渲染，数组顺序即展示顺序 */
const bindDesc = computed(() => ([
    { label: '生效作用域', value: scopeLabel(pathStatus.value?.boundScope) },
    { label: '绑定 Node', value: pathStatus.value?.boundVersion || '-' },
    { label: '生效 npm', value: pathStatus.value?.boundNpm || '-' },
    { label: '绑定目录', value: pathStatus.value?.boundDir || '-' },
]));
/* 全局包计数：总数打头，后面按管理器分组 */
const pkgStats = computed(() => [
    { title: '总数', count: packages.value.length },
    ...pkgByManager.value.map((g: any) => ({ title: g.manager, count: g.count })),
]);

/* ============================ 小工具 ============================ */
function openSite(port?: number) {
    if (!port) return;
    window.open(`http://127.0.0.1:${port}/`, "_blank");
}
/** 用系统默认程序打开文件或所在目录；属桌面能力，失败给提示 */
async function openPath(p?: string) {
    if (!p) { Message.warning("路径为空，无法打开"); return; }
    try { await GoTools.DiskOpenFile(p); }
    catch (e: any) { Message.error("打开失败：" + String(e?.message || e)); }
}

/* ============================ 站点实例 ============================ */
async function loadLocalVersions() {
    try { localVersions.value = (await GoTools.NpmSrvListVersions()) || []; }
    catch (e) { localVersions.value = []; }
}
async function loadInstances() {
    try { instances.value = (await GoTools.NpmSrvListInstances()) || []; }
    catch (e) { instances.value = []; }
    if (!selectedId.value && instances.value.length) selectInst(instances.value[0].id);
    refreshStates();
}
async function refreshStates() {
    // 并发问：实例串行 await 就是 N 次往返，Web 模式跑在远程服务器上时尤其明显
    await Promise.all(instances.value.map(async (inst) => {
        try {
            states[inst.id] = await GoTools.NpmSrvStatus(inst.id);
            if (states[inst.id]?.registry) registryVal.value = states[inst.id].registry;
        } catch (e) { /* 单个实例失败不影响其它实例 */ }
    }));
}
function selectInst(id: string) {
    selectedId.value = id;
    rightMode.value = "detail";
    refreshStates();
    // 点行只做选中/看详情，不自动启动；启动走双击行或明确的「启动」按钮（doStart → startInstById）
}
/** 双击行：未启动站点直接拉起（与「启动」按钮同一条路）；已启动只选中 */
function dblInst(inst: InstConfig) {
    if (!states[inst.id]?.running) startInstById(inst.id, inst.name);
}
async function pollLogs() {
    if (!selectedId.value) return;
    try {
        const lines = (await GoTools.NpmSrvLogs(selectedId.value, 300)) || [];
        // 贴底滚动交给 common-log-modal（watch lines.length）；这里只在内容有变时整体替换
        if (lines.join("\n") !== logLines.value.join("\n")) logLines.value = lines;
    } catch (e) { /* ignore */ }
}
function startLogPolling() {
    if (logTimer) clearInterval(logTimer);
    pollLogs();
    logTimer = setInterval(pollLogs, 2000);
}
function stopLogPolling() {
    if (logTimer) { clearInterval(logTimer); logTimer = null; }
}
// 点击「日志」：确保选中该站点后打开日志弹窗并开启轮询（关闭时 stopLogPolling）
function openLogModal(id?: string) {
    const target = id || selectedId.value;
    if (!target) { Message.warning("请先选择站点"); return; }
    if (target !== selectedId.value) { selectedId.value = target; rightMode.value = "detail"; }
    logLines.value = [];
    logVisible.value = true;
    startLogPolling();
}
/** 正在启动中的站点 id：防止连点重复拉起（StartInstance 对已运行会报错，故只在未运行时调用） */
const startingIds = new Set<string>();
async function startInstById(id: string, name: string) {
    if (!id || startingIds.has(id) || stopBusy.value) return;
    startingIds.add(id);
    // 容器模式首次启动可能要拉镜像，先说一声否则像卡住了
    if (instances.value.find((i) => i.id === id)?.mode === "docker") Message.info("正在启动容器（首次会自动拉镜像，可能几十秒）");
    try { await GoTools.NpmSrvStart(id); Message.success(`站点 ${name} 已启动`); }
    catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
    finally { startingIds.delete(id); refreshStates(); }
}
async function doStart() {
    const c = selCfg.value; if (!c) return;
    await startInstById(c.id, c.name);
}
/** 停止中：停止要等内置服务器/容器退出，期间锁定按钮防连点 */
const stopBusy = ref(false);
async function doStop() {
    const c = selCfg.value; if (!c) return;
    if (stopBusy.value) return;
    stopBusy.value = true;
    try { await GoTools.NpmSrvStop(c.id); Message.success(`站点 ${c.name} 已停止`); }
    catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
    finally { stopBusy.value = false; }
    refreshStates();
}
async function doRestart() {
    const c = selCfg.value; if (!c) return;
    try {
        await GoTools.NpmSrvRestart(c.id);
        Message.success(c.mode === "docker" ? `站点 ${c.name} 容器已重启，改过的 nginx.conf 随之生效` : `站点 ${c.name} 已重启`);
    }
    catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
    refreshStates();
}
async function onInstAction(v: string, inst: InstConfig) {
    if (v === "start") {
        try { await GoTools.NpmSrvStart(inst.id); Message.success(`站点 ${inst.name} 已启动`); }
        catch (e: any) { Message.error("启动失败: " + (e?.message || e)); }
        refreshStates();
    } else if (v === "stop") {
        try { await GoTools.NpmSrvStop(inst.id); Message.success(`站点 ${inst.name} 已停止`); }
        catch (e: any) { Message.error("停止失败: " + (e?.message || e)); }
        refreshStates();
    } else if (v === "restart") {
        try {
            await GoTools.NpmSrvRestart(inst.id);
            Message.success(inst.mode === "docker" ? `站点 ${inst.name} 容器已重启，改过的 nginx.conf 随之生效` : `站点 ${inst.name} 已重启`);
        }
        catch (e: any) { Message.error("重启失败: " + (e?.message || e)); }
        refreshStates();
    } else if (v === "open") {
        openSite(inst.port);
    } else if (v === "logs") {
        openLogModal(inst.id);
    } else if (v === "edit") {
        openInstModal(inst);
    } else if (v === "del") {
        Modal.warning({
            title: "删除站点", content: `确定删除站点「${inst.name}」？运行中会先停止；站点目录保留。`, hideCancel: false,
            onOk: async () => {
                try {
                    await GoTools.NpmSrvRemoveInstance(inst.id);
                    delete states[inst.id];
                    if (selectedId.value === inst.id) selectedId.value = "";
                    await loadInstances();
                } catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
            },
        });
    }
}
/* 打开实例弹窗：只设编辑目标 + 可见性，表单/引擎/端口探测/保存全部交给 InstanceModal */
function openInstModal(inst?: InstConfig) {
    presetImage.value = "";
    editingInst.value = inst || null;
    instVisible.value = true;
}
/** 版本管理 Docker 页签点「新建实例」：带着镜像开一个容器模式站点（镜像/模式初始化在 InstanceModal） */
function onCreateFromImage(image: string) {
    editingInst.value = null;
    presetImage.value = image;
    instVisible.value = true;
}
/** 版本管理里点实例名：关掉版本弹窗去编辑该实例（换镜像 = 换版本） */
function onEditInstanceById(id: string) {
    const inst = instances.value.find((i) => i.id === id);
    if (!inst) return;
    versionsVisible.value = false;
    openInstModal(inst);
}

/* ============================ 版本管理入口 ============================ */
/* 弹窗内所有版本/生效链/安装逻辑都在 VersionsModal；本页只负责开框（?open=versions / 工具箱入口） */
function openVersionsModal() {
    versionsVisible.value = true;
}

/* ============================ npm 配置入口 ============================ */
/* 配置项/registry/原文编辑/同步用户全局都在 NpmConfigModal；本页只开框，registry 变更由弹窗回传 */
function openNpmConfig() {
    cfgVisible.value = true;
}

/* ============================ 全局包管理 ============================ */
/* managers/packages 由本页持有（总览也展示）；装/卸动作在 PkgManageModal，这里只做开框与数据加载 */
function openPkgs() {
    pkgsVisible.value = true;
}
/** 弹窗打开时准备数据：管理器依附于 Node，先保证版本列表就绪再列管理器、扫全局包 */
async function onPkgsPrepare() {
    await loadLocalVersions();
    await loadManagers();
    scanPkgs();
}
/** 装/卸完成后重扫全局包 */
function onPkgsRescan() {
    scanPkgs();
}
async function loadManagers() {
    mgrLoading.value = true;
    try { managers.value = (await GoTools.NpmPackageManagers()) || []; }
    catch (e) { managers.value = []; }
    mgrLoading.value = false;
}
// 一次扫完全部管理器的全局安装根（直读磁盘 package.json，不起子进程）
async function scanPkgs() {
    pkgsLoading.value = true;
    try { packages.value = (await GoTools.NpmScanGlobalPackages()) || []; }
    catch (e: any) {
        packages.value = [];
        Message.error("扫描全局包失败: " + String(e?.message || e).slice(0, 200));
    }
    pkgsLoading.value = false;
}

/* ============================ 环境总览 ============================ */
/* chain / chainLoading / 总览各 computed 已在上方共享态区声明，scopeLabel 由 helpers 导入 */
async function loadOverview() {
    ovLoading.value = true;
    try {
        const [ch, ps, reg] = await Promise.all([
            GoTools.NpmCliChain().catch(() => null),
            GoTools.NpmPathStatus().catch(() => null),
            GoTools.NpmGetRegistry().catch(() => ""),
        ]);
        chain.value = ch;
        pathStatus.value = ps;
        if (reg) registryVal.value = reg;
        await Promise.all([loadLocalVersions(), loadManagers(), scanPkgs()]);
    } finally {
        ovLoading.value = false;
    }
    if (!bindDirOptions.value.some((o: any) => o.value === bindDir.value)) bindDir.value = "";
}
function doBindPath() {
    const dir = bindDir.value;
    if (!dir) { Message.warning("请选择要绑定的 Node 目录"); return; }
    const sys = bindScope.value === "system";
    Modal.warning({
        title: sys ? "强制绑定到系统 PATH（需管理员）" : "强制绑定到用户 PATH", hideCancel: false, okText: "绑定",
        content: sys
            ? "将把选定 Node 目录前置到「系统 PATH」并移除系统 PATH 中其它 Node 目录，使所有用户的 cmd 里 npm 都命中它。会弹出 UAC 管理员授权；首次操作自动备份，可一键撤销。新开终端生效。确定继续？"
            : "将把选定 Node 目录前置到「当前用户 PATH」并移除用户 PATH 中其它 Node 目录（免管理员）。注意：若系统 PATH 里也有 Node，系统项会盖过用户绑定——那种情况请改选「系统 PATH（管理员）」。首次操作自动备份，可撤销。新开终端生效。确定继续？",
        onOk: async () => {
            bindBusy.value = true;
            try { await GoTools.NpmPathBind(dir, sys); Message.success("已绑定，新开终端生效"); }
            catch (e: any) { Message.error("绑定失败: " + (e?.message || e)); }
            bindBusy.value = false;
            await loadOverview();
        },
    });
}
function doUnbindPath() {
    Modal.warning({
        title: "撤销 PATH 绑定", hideCancel: false,
        content: "将恢复首次绑定前备份的用户 PATH 并删除备份。新开终端生效。确定继续？",
        onOk: async () => {
            bindBusy.value = true;
            try { const msg = await GoTools.NpmPathUnbind(); Message.success(msg || "已撤销"); }
            catch (e: any) { Message.error("撤销失败: " + (e?.message || e)); }
            bindBusy.value = false;
            await loadOverview();
        },
    });
}
/* 生效链体检：chain 由本页持有（总览 PATH 候选 + VersionsModal 都读它），切换细节都在 VersionsModal */
async function loadChain() {
    chainLoading.value = true;
    try { chain.value = await GoTools.NpmCliChain(); }
    catch (e: any) {
        chain.value = null;
        Message.error("生效链体检失败: " + String(e?.message || e).slice(0, 220));
    }
    chainLoading.value = false;
}

/* ============================ 生命周期 ============================ */
onMounted(async () => {
    await loadInstances();
    npmOrder.value = await loadOrder("npm:list");
    // 主页点击服务实例进入（?inst=）：选中该站点展示详情
    const wantId = String(route.query.inst ?? "");
    if (wantId && instances.value.some((i) => i.id === wantId)) selectInst(wantId);
    // 下载抽屉点「装版本」进入（?open=versions）：冷启动直接弹版本管理
    if (String(route.query.open ?? "") === "versions") openVersionsModal();
    loadLocalVersions();
    loadOverview();
    srvTimer = setInterval(refreshStates, 3000);
    // 单例窗口已打开时，主页再次点击其他实例 → 通过事件切换
    offSelInst = Events.On("tool:select-inst", (ev: any) => {
        const p = ev?.data ?? ev;
        if (p?.type !== "npm" || !p?.id) return;
        if (instances.value.some((i) => i.id === String(p.id))) selectInst(String(p.id));
    });
    // 单例窗口已开着时，抽屉再点装任务 → 通过事件弹出版本管理
    offOpenPanel = Events.On("tool:open-panel", (ev: any) => {
        const p = ev?.data ?? ev;
        if (p?.type === "npm" && p?.panel === "versions") openVersionsModal();
    });
});
onUnmounted(() => {
    if (offSelInst) offSelInst();
    if (offOpenPanel) offOpenPanel();
    if (srvTimer) clearInterval(srvTimer);
    if (logTimer) clearInterval(logTimer);
});
</script>

<style scoped>
.npm-page {
    height: 100%;
    min-height: 0;
    flex-wrap: nowrap;
}

.side-col,
.main-col {
    height: 100%;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 6px;
}

.side-card {
    display: flex;
    flex-direction: column;
}

.side-card:first-child {
    flex: 1;
    min-height: 0;
}

.side-card:first-child :deep(.arco-card-body) {
    flex: 1;
    overflow: auto;
}

.tools-card {
    flex-shrink: 0;
}

.toolbox {
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.toolbox :deep(.tool-btn) {
    justify-content: flex-start;
}

.tool-btn-t {
    text-align: left;
}

.tool-btn-r {
    margin-left: auto;
    color: var(--color-text-3);
    font-size: 12px;
}

.reg-hint {
    margin-top: 8px;
    font-size: 12px;
    color: var(--color-text-3);
    word-break: break-all;
}

.main-panel {
    height: 100%;
    display: flex;
    flex-direction: column;
}

.main-panel :deep(.arco-card-body) {
    flex: 1;
    overflow: auto;
    display: flex;
    flex-direction: column;
}

.empty-card :deep(.arco-card-body) {
    align-items: center;
    justify-content: center;
}

.srv-head-actions {
    display: flex;
    gap: 6px;
}

.conn-name {
    max-width: 130px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.conn-more {
    cursor: pointer;
    color: var(--color-text-3);
}

.conn-empty {
    color: var(--color-text-3);
    font-size: 12px;
    padding: 6px 0;
}

.inst-active {
    background: var(--color-primary-light-1);
    cursor: pointer;
}

:deep(.arco-list-item) {
    cursor: pointer;
}

/* 环境总览 */
.ov-body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 12px 14px;
}

.detail-body {
    flex: 1;
    min-height: 0;
    overflow: auto;
    display: flex;
    flex-direction: column;
}

/* 站点详情主体：纵向堆叠指标卡 / 快捷条 / 信息行（外层 detail-body 负责滚动） */
.ov-inner {
    display: flex;
    flex-direction: column;
    gap: 16px;
    padding: 14px;
}

.stat-row {
    margin: 0;
}

.quick-bar {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
}

/* 站点信息：长文本省略 + tooltip，单列信息行 */
.info-section {
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.info-title {
    font-size: var(--base-size-default);
    font-weight: 600;
    color: var(--color-text-1);
}

.info-list {
    display: flex;
    flex-direction: column;
}

/* 环境总览：改成卡片分区，卡内自己分层，不再靠分隔线 + 裸标题堆出一条长页 */
.ov-alert {
    margin-bottom: 8px;
}

.ov-top {
    align-items: stretch;
}

.ov-card {
    border-radius: 6px;
}

.ov-card :deep(.arco-card-header-title) {
    font-size: 13px;
    font-weight: 600;
}

.ov-card :deep(.arco-card-body) {
    padding: 10px 12px;
}

.ov-desc :deep(.arco-descriptions-item-label) {
    color: var(--color-text-3);
    width: 88px;
    white-space: nowrap;
}

.ov-desc :deep(.arco-descriptions-item-value) {
    word-break: break-all;
}

.ov-others {
    margin-top: 8px;
    font-size: 12px;
    color: var(--color-text-3);
}

.ov-others-t {
    margin-bottom: 4px;
}

.ov-other-dir {
    font-family: Consolas, Menlo, monospace;
    word-break: break-all;
    padding-left: 8px;
}

.ov-bind {
    display: flex;
    flex-direction: column;
    gap: 8px;
}

.ov-bind-row {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 6px;
}

.ov-note {
    margin-top: 8px;
    font-size: 12px;
    line-height: 1.6;
    color: var(--color-text-3);
    word-break: break-all;
}


</style>
