<template>
    <!-- Docker 镜像 / 容器区：PHP/MySQL/Nginx/Redis/npm 的 Docker 实例，版本实际上就是镜像 tag。
         镜像页签把镜像当一等版本：看已有哪些、被哪些实例用着、快速拉新的、删没用的；
         容器页签直接管本工具创建的容器（启动/停止/重启/删除），不用跳到 Docker 管理面板。
         引擎没起时两页签都不可用，这里给足原因和「打开 Docker 面板」入口，不做静默兜底 -->
    <a-card class="card-head-tool dv-card" size="small" :body-style="{ padding: '8px 10px' }">
        <template #title>
            <a-space :size="4">
                <span class="dv-title"><icon-storage /> Docker {{ pane === 'image' ? '镜像' : '容器' }}</span>
                <a-tooltip :content="`${toolLabel} 的 Docker 实例版本由镜像 tag 决定（如 ${repo}:8.3）；被实例引用的镜像不可删除`">
                    <icon-question-circle class="dv-help" />
                </a-tooltip>
                <a-tag v-if="rows.length" size="small" color="arcoblue">{{ rows.length }} 个镜像</a-tag>
                <a-tag v-if="usedCount" size="small" color="green">{{ usedCount }} 个实例在用</a-tag>
                <a-tag v-if="runningCount" size="small" color="cyan">{{ runningCount }} 个容器在跑</a-tag>
            </a-space>
        </template>
        <template #extra>
            <a-space :size="6">
                <a-radio-group v-model="pane" size="small" type="button">
                    <a-radio value="image">镜像</a-radio>
                    <a-radio value="container">容器{{ containers.length ? ` ${containers.length}` : '' }}</a-radio>
                </a-radio-group>
                <a-button size="small" :loading="loading || cLoading" @click="refresh">
                    <template #icon><icon-refresh /></template>刷新
                </a-button>
                <!-- 一键进 Docker 管理面板：看全部镜像/容器/网络/卷、拉取任务中心都在那 -->
                <a-tooltip content="打开 Docker 管理面板（容器/镜像/网络/卷、拉取任务中心）">
                    <a-button size="small" class="dv-panel-btn" @click="openDockerPanel">
                        <template #icon><icon-launch /></template>
                    </a-button>
                </a-tooltip>
            </a-space>
        </template>

        <!-- 引擎没就绪：一行说清原因（端点串悬停看全）并给出面板入口与重试，不把卡片撑高 -->
        <div v-if="engineErr" class="dv-alert">
            <icon-exclamation-circle class="dv-alert-icon" />
            <a-tooltip position="bl">
                <!-- 端点串不带空格，直接当 tooltip 内容会拉成一条横向超长；拆行 + 硬断后才读得清 -->
                <template #content>
                    <div class="dv-alert-tip">
                        <div v-for="(l, i) in engineErrLines" :key="i">{{ l }}</div>
                    </div>
                </template>
                <span class="dv-alert-text">Docker 引擎未就绪，{{ repo }} 系镜像与容器都看不了</span>
            </a-tooltip>
            <a-space :size="4" class="dv-alert-ops">
                <a-tooltip content="去 Docker 面板启动引擎或拉镜像">
                    <a-button size="small" @click="openDockerPanel">Docker 面板</a-button>
                </a-tooltip>
                <a-button size="small" :loading="loading" @click="refresh">重试</a-button>
            </a-space>
        </div>

        <!-- ============ 镜像页签 ============ -->
        <div v-show="pane === 'image'">
            <!-- 拉取：可直接输 tag，也可点下面的常用 tag —— 没装过的点一下直接拉 -->
            <div class="dv-pull">
                <div class="dv-pull-main">
                    <a-input v-model="pullRef" size="small" class="dv-pull-input" allow-clear
                        :disabled="!!pulling || !!engineErr" :placeholder="`镜像名，如 ${repo}:8.3`" @press-enter="pull">
                        <template #prefix><icon-download /></template>
                    </a-input>
                    <a-button size="small" type="primary" :loading="!!pulling" :disabled="!pullRef.trim() || !!engineErr"
                        @click="pull">拉取</a-button>
                </div>
                <div class="dv-presets">
                    <span class="dv-presets-label">常用</span>
                    <a-tooltip v-for="p in presets" :key="p" :content="presetTip(p)">
                        <a-tag size="small" class="dv-preset"
                            :class="[installedSet.has(p) ? 'has' : 'miss', { on: pullRef === p }]"
                            @click="pickPreset(p)">
                            <icon-check-circle v-if="installedSet.has(p)" class="dv-preset-i" />
                            <icon-download v-else class="dv-preset-i" />{{ p }}
                        </a-tag>
                    </a-tooltip>
                </div>
            </div>
            <div v-if="pullNote" class="dv-note" :class="{ err: pullErr, busy: pulling }">
                <a-progress v-if="pulling" :percent="pullPct / 100" size="mini" :show-text="false" class="dv-bar" />
                <span class="dv-note-text">{{ pullNote }}</span>
            </div>

            <!-- 引擎起来了但容器全停着：直接在这里给一条「一键全部启动」的通道，不用切页签也不用去 Docker 面板 -->
            <div v-if="stoppedCount && !runningCount && !engineErr" class="dv-strip">
                <icon-pause-circle class="dv-strip-icon" />
                <span class="dv-strip-text">本工具已有 {{ stoppedCount }} 个容器，当前都是停止状态</span>
                <a-space :size="4">
                    <a-tooltip content="逐个启动本工具的容器（镜像、数据目录都不变）">
                        <a-button size="small" type="primary" :loading="startingAll" @click="startAll">一键全部启动</a-button>
                    </a-tooltip>
                    <a-button size="small" @click="pane = 'container'">看容器</a-button>
                </a-space>
            </div>

            <a-table :data="rows" :loading="loading" size="small" :pagination="false" row-key="image"
                :scroll="{ x: '100%', y: 210 }" :bordered="false" :columns="[
                    { title: '镜像（tag）', dataIndex: 'image', minWidth: 200, slotName: 'img' },
                    { title: '大小', dataIndex: 'size', width: 90, slotName: 'size' },
                    { title: '拉取于', dataIndex: 'created', width: 100 },
                    { title: '使用实例', dataIndex: 'usedBy', width: 190, slotName: 'used' },
                    { title: '操作', dataIndex: 'ops', width: 160, slotName: 'ops', fixed: 'right', align: 'right' },
                ]">
                <template #img="{ record }">
                    <span class="dv-img mono">{{ record.image }}</span>
                </template>
                <template #size="{ record }">{{ fmtSize(record.size) }}</template>
                <template #created="{ record }">{{ fmtAgo(record.created) }}</template>
                <!-- 使用实例：点实例名直接去改它的镜像（= 切版本），比在实例编辑里翻下拉快；
                     已停止且容器还在的，就地给一个启动按钮（引擎已起、只是没开容器的常见情形） -->
                <template #used="{ record }">
                    <a-space v-if="record.usedBy.length" :size="4" wrap>
                        <template v-for="u in record.usedBy.slice(0, 2)" :key="u.name">
                            <a-tooltip :content="useTip(u)">
                                <a-tag size="small" :color="u.running ? 'green' : 'gray'" class="dv-use"
                                    @click="emit('use', u.id)">{{ u.name }}</a-tag>
                            </a-tooltip>
                            <a-tooltip v-if="!u.running && u.container" content="容器还在，直接启动即可，不用去 Docker 面板">
                                <a-button size="mini" type="outline" status="success"
                                    :loading="opOf(u.container) === 'start'" :disabled="locked(u.container)"
                                    @click="actContainer('start', u.container)">
                                    <template #icon><icon-play-arrow /></template>启动
                                </a-button>
                            </a-tooltip>
                        </template>
                        <a-tooltip v-if="record.usedBy.length > 2"
                            :content="record.usedBy.slice(2).map((u) => u.name).join('、')">
                            <a-tag size="small">+{{ record.usedBy.length - 2 }}</a-tag>
                        </a-tooltip>
                    </a-space>
                    <span v-else class="dv-dim">未被使用</span>
                </template>
                <template #ops="{ record }">
                    <a-space :size="2">
                        <a-button size="small" @click="emit('create', record.image)">
                            <template #icon><icon-plus /></template>新建实例
                        </a-button>
                        <a-popconfirm :content="`删除镜像「${record.image}」？本机磁盘上的这一份会被移除，需要时可重新拉取。`" type="warning"
                            position="lt" :disabled="!!record.usedBy.length || removing === record.image"
                            @ok="removeImage(record)">
                            <a-tooltip
                                :content="record.usedBy.length ? `被 ${record.usedBy.map((u) => u.name).join('、')} 使用中，先改实例镜像再删` : ''">
                                <a-button size="small" status="danger" :disabled="!!record.usedBy.length"
                                    :loading="removing === record.image">删除</a-button>
                            </a-tooltip>
                        </a-popconfirm>
                    </a-space>
                </template>
                <template #empty>
                    <a-empty :image="false"
                        :description="engineErr ? 'Docker 引擎未运行，拉取与查看镜像都不可用' : `本机还没有 ${repo} 系镜像，点上方带 ↓ 的常用版本可直接拉取`" />
                </template>
            </a-table>
        </div>

        <!-- ============ 容器页签 ============ -->
        <div v-show="pane === 'container'">
            <a-table :data="containers" :loading="cLoading" size="small" :pagination="false" row-key="id"
                :scroll="{ x: 900, y: 210 }" :bordered="false" :columns="[
                    { title: '容器', dataIndex: 'name', minWidth: 210, slotName: 'cname' },
                    { title: '镜像（版本）', dataIndex: 'image', width: 160, slotName: 'cimg' },
                    { title: '状态', dataIndex: 'status', width: 130, slotName: 'cstate' },
                    { title: '端口映射', dataIndex: 'ports', width: 180, slotName: 'cports' },
                    { title: '创建', dataIndex: 'created', width: 90, slotName: 'ccreated' },
                    { title: '操作', dataIndex: 'ops', width: 210, slotName: 'cops', fixed: 'right', align: 'right' },
                ]">
                <template #cname="{ record }">
                    <span class="dv-cname-cell">
                        <com-badge :status="record.state === 'running' ? 'success' : 'normal'"
                            :animated="record.state === 'running'" />
                        <a-tooltip :content="record.name || '(未命名)'">
                            <span class="dv-cname">{{ record.name || '(未命名)' }}</span>
                        </a-tooltip>
                        <!-- 认得出所属实例：点名字直接去编辑该实例 -->
                        <a-tooltip v-if="instOf(record)" :content="`实例「${instOf(record).name}」，点击打开实例编辑`">
                            <a-tag size="small" color="purple" class="dv-use" @click="emit('use', instOf(record).id)">{{
                                instOf(record).name }}</a-tag>
                        </a-tooltip>
                    </span>
                </template>
                <template #cimg="{ record }">
                    <a-tooltip :content="record.image">
                        <span class="mono">{{ imgTag(record.image) || '-' }}</span>
                    </a-tooltip>
                </template>
                <template #cstate="{ record }">
                    <span :style="{ color: stateColor(record.state) }">{{ record.status || record.state || '-' }}</span>
                </template>
                <template #cports="{ record }">
                    <div class="dv-ports">
                        <span v-for="(p, i) in (record.ports || []).slice(0, 2)" :key="i" class="dv-port">
                            {{ p.ip || '0.0.0.0' }}:{{ p.publicPort || '-' }}→{{ p.privatePort }}/{{ p.type }}
                        </span>
                        <a-tooltip v-if="(record.ports || []).length > 2"
                            :content="record.ports.slice(2).map((p) => `${p.publicPort}→${p.privatePort}`).join('、')">
                            <span class="dv-port more">+{{ record.ports.length - 2 }}</span>
                        </a-tooltip>
                        <span v-if="!(record.ports || []).length" class="dv-dim">-</span>
                    </div>
                </template>
                <template #ccreated="{ record }">{{ fmtAgo(record.created) }}</template>
                <!-- 停止/重启/删除都会中断服务：统一气泡二次确认，动作中锁住整行避免连点 -->
                <template #cops="{ record }">
                    <a-space :size="2">
                        <a-button v-if="record.state !== 'running'" size="small" status="success"
                            :loading="opOf(record) === 'start'" :disabled="locked(record)"
                            @click="actContainer('start', record)">
                            <template #icon><icon-play-arrow /></template>{{ opOf(record) === 'start' ? '启动中' : '启动' }}
                        </a-button>
                        <a-popconfirm v-else :content="`停止容器「${record.name}」？正在处理的请求会中断。`" type="warning" position="lt"
                            :disabled="locked(record)" @ok="actContainer('stop', record)">
                            <a-button size="small" status="warning" :loading="opOf(record) === 'stop'"
                                :disabled="locked(record)"><template #icon><icon-pause /></template>{{
                                    opOf(record) === 'stop' ? '停止中' : '停止' }}</a-button>
                        </a-popconfirm>
                        <a-popconfirm :content="`重启容器「${record.name}」？会先停再起，期间不可访问。`" type="warning" position="lt"
                            :disabled="locked(record)" @ok="actContainer('restart', record)">
                            <a-button size="small" :loading="opOf(record) === 'restart'" :disabled="locked(record)">
                                <template #icon><icon-refresh /></template>{{ opOf(record) === 'restart' ? '重启中' : '重启'
                                }}
                            </a-button>
                        </a-popconfirm>
                        <a-popconfirm :content="`删除容器「${record.name}」？运行中会强制删除，实例配置与数据目录保留。`" type="warning"
                            position="lt" :disabled="locked(record)" @ok="actContainer('remove', record)">
                            <a-button size="small" status="danger" :loading="opOf(record) === 'remove'"
                                :disabled="locked(record)"><template #icon><icon-delete /></template>{{
                                    opOf(record) === 'remove' ? '删除中' : '删除' }}</a-button>
                        </a-popconfirm>
                    </a-space>
                </template>
                <template #empty>
                    <a-empty :image="false"
                        :description="engineErr ? 'Docker 引擎未运行，看不到容器' : `还没有 ${toolLabel} 容器：到「镜像」页签点新建实例，或把已有实例的运行模式改成 Docker`" />
                </template>
            </a-table>
            <div class="dv-hint">
                这里只列本工具（{{ containerTool }}）创建的容器；启停会同步实例状态，改镜像/端口仍要去实例编辑。
                <a-button size="small" @click="openDockerPanel">打开 Docker 面板<template
                        #icon><icon-right /></template></a-button>
                可看全部容器、日志与资源占用。
            </div>
        </div>
    </a-card>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, onMounted, onUnmounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import { useToolWindow } from "@/views/index/useToolWindow";

const props = defineProps<{
    /** 镜像 repo 过滤关键字（php / mysql / nginx / redis） */
    keyword: string;
    /** 展示名（PHP / MySQL / Nginx），进标题与提示文案 */
    toolLabel: string;
    /** 默认仓库名，拉取输入框的占位与常用 tag 前缀 */
    repo: string;
    /** 常用 tag（完整 ref）：已安装的点一下填入输入框，没装过的点一下直接拉 */
    presets?: string[];
    /** 该页面的实例清单：用来标注镜像被哪些实例引用（引用中的不可删） */
    instances?: any[];
    /** 实例运行态（id → { running }）：区分「引用中」和「正在跑」，别把停止的实例当成运行中 */
    states?: Record<string, any>;
    /** 外部刷新信号：版本管理弹窗每次打开自增一次，弹窗不销毁时也能刷新 */
    reloadKey?: number;
    /** 容器按哪个工具标签过滤（docker 标签 Tools）；npm 站点用 nginx 镜像但标签是 npm，故可与 keyword 不同 */
    containerTool?: string;
}>();

const emit = defineEmits<{
    (e: "create", image: string): void;
    (e: "use", id: string): void;
    (e: "changed"): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;
const { openToolWindow } = useToolWindow();

/** 本工具容器的标签值：默认跟镜像关键字走 */
const containerTool = computed(() => (props.containerTool || props.keyword).toLowerCase());

const pane = ref<"image" | "container">("image");
const loading = ref(false);
const engineErr = ref("");
/** 错误串拆行：「结论（端点：…（候选…））」拆成两行，悬停时不用在一横条里找重点 */
const engineErrLines = computed(() => {
    const raw = engineErr.value;
    const i = raw.indexOf("（端点");
    if (i > 0) return [raw.slice(0, i), raw.slice(i + 1).replace(/[）]+$/g, "")];
    return [raw];
});
const images = ref<any[]>([]);
const pullRef = ref("");
const pulling = ref("");
const pullPct = ref(0);
const pullNote = ref("");
const pullErr = ref(false);
const removing = ref("");
let offPull: any = null;

const containers = ref<any[]>([]);
const cLoading = ref(false);
/** 容器行内正在跑的动作：id → start/stop/restart/remove */
const cBusy = ref<Record<string, string>>({});
/** 一键全部启动进行中 */
const startingAll = ref(false);
let cTimer: any = null;

/** 容器按所属实例 ID 建索引：镜像行上要能直接看到“这个实例的容器还在、只是停了” */
const contByOwner = computed(() => {
    const m: Record<string, any> = {};
    for (const c of containers.value) if (c.ownerId) m[c.ownerId] = c;
    return m;
});

/** 只留本工具系镜像，按 tag 排好；再挂上「被哪些实例用着、谁在跑、容器还在不在」 */
const rows = computed(() => {
    const list = images.value
        .filter((im) => im.tag && im.tag !== "<none>" && String(im.repo || "").toLowerCase().includes(props.keyword))
        .map((im) => {
            const image = `${im.repo}:${im.tag}`;
            const owned = (props.instances || []).filter((i) => i.mode === "docker" && i.image === image);
            return {
                image, size: im.size, created: im.created,
                usedBy: owned.map((i) => ({
                    id: i.id, name: i.name || i.id,
                    running: !!props.states?.[i.id]?.running,
                    container: contByOwner.value[i.id] || null,
                })),
            };
        })
        .sort((a, b) => a.image.localeCompare(b.image, "en", { numeric: true }));
    return list;
});

/** 实例 tag 的悬停文案：区分「容器在只是停了」和「压根还没建过容器」，两者的下一步不一样 */
function useTip(u: any): string {
    if (u.running) return `${u.name}：运行中，点击切换该实例的镜像版本`;
    if (u.container) return `${u.name}：已停止（容器还在），右侧可一键启动；点名字改镜像版本`;
    return `${u.name}：已停止，还没建过容器，首次启动时会自动创建`;
}

/** 标题上的概览：有多少镜像真正被容器用着 */
const usedCount = computed(() => rows.value.filter((r) => r.usedBy.length).length);
const runningCount = computed(() => containers.value.filter((c) => c.state === "running").length);
/** 停着的容器数：引擎已起但一个都没跑时，镜像页签上给一键拉起入口 */
const stoppedCount = computed(() => containers.value.filter((c) => c.state !== "running").length);

/** 本机已装的「镜像:tag」集合：不按 keyword 筛（mysql 页的候选里还有 mariadb/percona） */
const installedSet = computed(() => new Set<string>(images.value.map((im) => `${im.repo}:${im.tag}`)));

function presetTip(p: string): string {
    if (!installedSet.value.has(p)) return `${p} 本机还没有，点击直接拉取`;
    return `${p} 已安装，点击填入输入框（可改成同系列其它 tag 再拉）`;
}

/** 容器 → 实例：创建时打的 Tools.instance 标签就是实例 ID */
function instOf(c: any): any {
    if (!c?.ownerId) return null;
    return (props.instances || []).find((i) => i.id === c.ownerId) || null;
}

function opOf(c: any): string { return cBusy.value[c.id] || ""; }
function locked(c: any): boolean { return !!opOf(c); }

/** 镜像展示只留 tag（= 版本），完整镜像名走 tooltip */
function imgTag(image: string): string {
    const s = String(image || "");
    const i = s.lastIndexOf(":");
    return i > 0 && !s.slice(i).includes("/") ? s.slice(i + 1) : s;
}

function stateColor(s: string): string {
    if (s === "running") return "rgb(var(--success-6))";
    if (s === "paused") return "rgb(var(--warning-6))";
    if (s === "restarting") return "rgb(var(--primary-6))";
    return "var(--color-text-3)";
}

async function load() {
    loading.value = true;
    try {
        images.value = (await GoTools.DockerSrvListImages()) || [];
        engineErr.value = "";
    } catch (e: any) {
        images.value = [];
        // 引擎没起时 ListImages 直接抛错：留一份原因给告警条，容器页签同样不可用
        engineErr.value = String(e?.message || e);
    } finally {
        loading.value = false;
    }
}

async function loadContainers() {
    cLoading.value = true;
    try {
        const all = (await GoTools.DockerSrvListContainers(true)) || [];
        containers.value = all.filter((c: any) =>
            String(c.ownerTool || "").toLowerCase() === containerTool.value
            || String(c.name || "").toLowerCase().startsWith(`Tools-${containerTool.value}-`));
        engineErr.value = "";
    } catch (e: any) {
        containers.value = [];
        engineErr.value = String(e?.message || e);
    } finally {
        cLoading.value = false;
    }
}

function refresh() { load(); loadContainers(); }

/** 常用 tag：已安装 → 只填入输入框；未安装 → 直接开始拉取（快速安装） */
function pickPreset(p: string) {
    if (pulling.value) return;
    pullRef.value = p;
    if (installedSet.value.has(p)) return;
    if (engineErr.value) { Message.warning("Docker 引擎未就绪，先启动 Docker Desktop 再拉取"); return; }
    // 预置 tag 是页面自己给的（mysql 页含 mariadb/percona），不再走关键字拦截
    startPull(p, false);
}

/** 输入框 / 回车拉取：只认本工具系镜像，防误拉一堆不相干的 */
function pull() {
    const image = pullRef.value.trim();
    if (!image || pulling.value) return;
    startPull(image, true);
}

async function startPull(image: string, checkKeyword: boolean) {
    if (!image || pulling.value) return;
    if (images.value.some((im) => `${im.repo}:${im.tag}` === image)) {
        Message.info(`${image} 已在本机，重新拉取只会更新这一份`);
    }
    if (!image.includes(":")) { Message.warning("请带上 tag，如 " + props.repo + ":8.3"); return; }
    if (checkKeyword && !String(image).toLowerCase().includes(props.keyword)) {
        Message.warning(`这不是 ${props.toolLabel} 镜像，本面板只管 ${props.keyword} 系镜像`);
        return;
    }
    pulling.value = image;
    pullPct.value = 0;
    pullErr.value = false;
    pullNote.value = `准备拉取 ${image}…`;
    try {
        await GoTools.DockerSrvPullImage(image);
        pullNote.value = `${image} 已拉取完成`;
        Message.success(`${image} 已就绪`);
        await load();
    } catch (e: any) {
        pullErr.value = true;
        pullNote.value = `拉取失败：${e?.message || e}`;
    } finally {
        pulling.value = "";
    }
}

async function removeImage(record: any) {
    if (removing.value) return;
    removing.value = record.image;
    try {
        await GoTools.DockerSrvRemoveImage(record.image, false);
        Message.success(`已删除 ${record.image}`);
        await load();
    } catch (e: any) {
        Message.error("删除失败: " + (e?.message || e));
    } finally {
        removing.value = "";
    }
}

/** 容器动作：本组件直接调 Docker 接口，成功后回抛 changed 让页面刷新实例状态 */
async function actContainer(act: string, c: any) {
    if (cBusy.value[c.id]) return;
    cBusy.value = { ...cBusy.value, [c.id]: act };
    try {
        if (act === "start") await GoTools.DockerSrvStartContainer(c.id);
        else if (act === "stop") await GoTools.DockerSrvStopContainer(c.id, 10);
        else if (act === "restart") await GoTools.DockerSrvRestartContainer(c.id);
        else if (act === "remove") await GoTools.DockerSrvRemoveContainer(c.id, true);
        Message.success(`${c.name || c.id.slice(0, 12)} ${actText(act)}`);
        await loadContainers();
        emit("changed");
    } catch (e: any) {
        Message.error(`${actText(act)}失败: ${e?.message || e}`);
        await loadContainers();
        emit("changed");
    } finally {
        const next = { ...cBusy.value };
        delete next[c.id];
        cBusy.value = next;
    }
}

function actText(act: string): string {
    return act === "start" ? "已启动" : act === "stop" ? "已停止" : act === "restart" ? "已重启" : "已删除";
}

/** 一键拉起本工具一切停着的容器：逐个跑（不并发，免得引擎侧抢资源），成败分开报 */
async function startAll() {
    const list = containers.value.filter((c: any) => c.state !== "running");
    if (!list.length || startingAll.value) return;
    startingAll.value = true;
    let ok = 0;
    const errs: string[] = [];
    for (const c of list) {
        try { await GoTools.DockerSrvStartContainer(c.id); ok++; }
        catch (e: any) { errs.push(`${c.name || String(c.id).slice(0, 12)}：${e?.message || e}`); }
    }
    startingAll.value = false;
    if (ok) Message.success(`已启动 ${ok} 个容器`);
    if (errs.length) Message.error(`${errs.length} 个启动失败：${errs[0]}`);
    await loadContainers();
    emit("changed");
}

/** 打开 Docker 管理窗口（独立 webview，单例复用）：看全部容器/镜像/网络/卷与拉取任务中心 */
function openDockerPanel() {
    openToolWindow("/tools/docker", "Docker管理", { single: true, width: 1200, height: 800 });
}

/** 拉取进度走全局 docker:pull 事件，只认当前这次拉取的镜像 */
function onPullEvent(raw: any) {
    const p = raw?.data ?? raw;
    if (!p || !p.image || !pulling.value || p.image !== pulling.value) return;
    if (p.stage === "done") { pullNote.value = `${p.image} 已拉取完成`; return; }
    if (p.stage === "error") { pullErr.value = true; pullNote.value = `${p.image} 拉取失败：${p.note || ""}`; return; }
    if (p.stage === "canceled") { pullNote.value = `${p.image} 已取消`; return; }
    const pct = Number(p.overall || p.percent || 0);
    if (pct > 0) pullPct.value = pct;
    const bits = [p.status || "", pct ? `${pct}%` : ""].filter(Boolean).join(" ");
    pullNote.value = `拉取 ${p.image}${p.layer ? ` · ${String(p.layer).slice(0, 12)}` : ""}${bits ? ` · ${bits}` : ""}`;
}

function fmtSize(n: number): string {
    if (!n || n <= 0) return "-";
    const units = ["B", "KB", "MB", "GB", "TB"];
    let v = n, i = 0;
    while (v >= 1024 && i < units.length - 1) { v /= 1024; i++; }
    return `${v >= 100 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`;
}

function fmtAgo(unixSec: number): string {
    if (!unixSec) return "-";
    const s = Math.max(0, Math.floor(Date.now() / 1000 - unixSec));
    if (s < 60) return `${s} 秒前`;
    if (s < 3600) return `${Math.floor(s / 60)} 分钟前`;
    if (s < 86400) return `${Math.floor(s / 3600)} 小时前`;
    return `${Math.floor(s / 86400)} 天前`;
}

/** 容器页签开着时轻量轮询状态；切回镜像或组件卸载就停 */
function syncTimer() {
    if (pane.value === "container" && !cTimer) cTimer = setInterval(loadContainers, 4000);
    else if (pane.value !== "container" && cTimer) { clearInterval(cTimer); cTimer = null; }
}
watch(pane, syncTimer);

onMounted(() => {
    refresh();
    offPull = Events.On("docker:pull", onPullEvent);
});
watch(() => props.reloadKey, () => { if (props.reloadKey) refresh(); });
onUnmounted(() => {
    if (offPull) offPull();
    if (cTimer) clearInterval(cTimer);
});
</script>

<style scoped>
.dv-card {
    margin-top: 4px;
}

.dv-title {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-weight: 500;
}

.dv-help {
    color: var(--color-text-3);
    cursor: help;
    margin-left: 2px;
}

.dv-panel-btn {
    padding: 0 6px;
}

.dv-pull {
    display: flex;
    flex-direction: column;
    gap: 8px;
    margin-bottom: 8px;
}

/* 拉取动作行：输入框 + 按钮成组，不再和常用 tag 挤在同一条 flex-wrap 里错位换行 */
.dv-pull-main {
    display: flex;
    align-items: center;
    gap: 8px;
}

.dv-pull-input {
    width: 260px;
    max-width: 100%;
}

.dv-presets {
    display: flex;
    align-items: center;
    gap: 6px;
    flex-wrap: wrap;
    min-width: 0;
}

.dv-presets-label {
    flex: none;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

/* 常用 tag 胶囊：浅底圆角，hover 提一档，选中实心主色、已装绿底，一眼分清待拉/已装 */
.dv-preset {
    cursor: pointer;
    border-radius: 999px;
    padding: 0 10px;
    background: var(--color-fill-1);
    border-color: transparent;
    transition: background 0.15s ease, color 0.15s ease;
}

.dv-preset:hover {
    background: var(--color-fill-2);
}

.dv-preset.on {
    background: rgb(var(--primary-6));
    border-color: rgb(var(--primary-6));
    color: #fff;
}

/* 已装 = 绿、待拉 = 常规：一眼看出哪些常用版本还得下载 */
.dv-preset.has {
    color: rgb(var(--success-6));
    background: var(--color-success-light-1);
    border-color: transparent;
}

.dv-preset.has.on {
    color: #fff;
    background: rgb(var(--primary-6));
}

.dv-preset-i {
    margin-right: 4px;
    font-size: 12px;
    vertical-align: -2px;
}

/* 拉取进度/结果条：常态浅底，拉取中主色底、失败危险色底，让状态一眼可辨 */
.dv-note {
    display: flex;
    align-items: center;
    gap: 8px;
    margin-bottom: 8px;
    padding: 6px 10px;
    border-radius: var(--base-radius-default);
    background: var(--color-fill-1);
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

.dv-note.busy {
    background: var(--color-primary-light-1);
    color: rgb(var(--primary-7));
}

.dv-note.err {
    background: var(--color-danger-light-1);
    color: rgb(var(--danger-6));
}

.dv-bar {
    width: 140px;
    flex: none;
}

.dv-note-text {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.dv-alert {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 8px;
    padding: 6px 10px;
    border: 1px solid rgba(var(--warning-6), 0.2);
    border-radius: var(--base-radius-default);
    background: rgba(var(--warning-6), 0.09);
}

.dv-alert-icon {
    flex: none;
    font-size: 13px;
    color: rgb(var(--warning-6));
}

.dv-alert-text {
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    cursor: help;
}

.dv-alert-ops {
    flex: none;
}

/* 悬停浮层：限宽 + 硬断，免得管道路径把气泡拉成一条横穿屏幕的长条 */
.dv-alert-tip {
    max-width: 420px;
    line-height: 1.7;
    font-size: var(--base-size-small);
    overflow-wrap: anywhere;
}

/* 容器全停着时的引导条：语气比引擎未就绪轻一档，只做事不报错 */
.dv-strip {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-bottom: 8px;
    padding: 6px 10px;
    border: 1px solid rgba(var(--primary-6), 0.2);
    border-radius: var(--base-radius-default);
    background: rgba(var(--primary-6), 0.07);
}

.dv-strip-icon {
    flex: none;
    font-size: 13px;
    color: rgb(var(--primary-6));
}

.dv-strip-text {
    flex: 1;
    min-width: 0;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

.dv-img {
    font-weight: 500;
}

.dv-dim {
    color: var(--color-text-3);
}

.dv-use {
    cursor: pointer;
}

/* 容器名一格内自己排布（a-space 会把每项包成 item，长名控不住宽度） */
.dv-cname-cell {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    min-width: 0;
}

.dv-cname {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
}

.dv-ports {
    display: flex;
    flex-direction: column;
    line-height: 1.5;
}

.dv-port {
    font-family: Consolas, monospace;
    font-size: 12px;
    color: var(--color-text-2);
}

.dv-port.more {
    color: var(--color-text-3);
}

.dv-hint {
    margin-top: 6px;
    font-size: var(--base-size-small);
    line-height: 1.6;
    color: var(--color-text-3);
}

.dv-hint :deep(.arco-link) {
    padding: 0;
    font-size: inherit;
    line-height: inherit;
}

.mono {
    font-family: Consolas, monospace;
}
</style>
