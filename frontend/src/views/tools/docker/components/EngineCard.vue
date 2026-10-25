<template>
    <!-- 引擎状态：已连接时给出运行时事实；不可用时原地展开安装 / 启动向导。动作一律回传页面执行 -->
    <a-card class="engine-card card-head-tool" >
        <template #title>
            <a-space>
                <icon-computer /> Docker 引擎
                <!-- 引擎状态：圆点颜色同 a-badge（探测中/已连接/不可用），探测与在线时开启动效 -->
                <span class="com-state" :class="{
                    'is-on': !engineLoading && engine.available,
                    'is-off': !engineLoading && !engine.available
                }">
                    <com-badge :status="engineLoading ? 'processing' : (engine.available ? 'success' : 'danger')"
                        :animated="engineLoading || engine.available"></com-badge>
                    {{ engineLoading ? '探测中…' : (engine.available ? '已连接' : '不可用') }}
                </span>
                <a-tag v-if="engine.isDesktop" >Docker Desktop</a-tag>
                <a-tag v-if="engine.available" color="green">{{ engine.version }}</a-tag>
            </a-space>
        </template>
        <template #extra>
            <a-space :size="6">
                <a-button size="small" :loading="envLoading" @click="emit('open-env')">
                    <template #icon><icon-safe /></template>体检
                </a-button>
                <a-button size="small" @click="emit('open-settings')">
                    <template #icon><icon-settings /></template>仓库
                </a-button>
                <a-button size="small" v-if="caps.nativeWindows && engine.isDesktop" @click="emit('open-switch')">
                    <template #icon><icon-swap /></template>切换后端
                </a-button>
                <!-- 引擎在跑、或正在启动等待时都能叫停：会结束 Docker Desktop 进程并提权停 com.docker.service -->
                <a-popconfirm
                    :content="setupBusy === 'start' ? '停止正在进行的启动？已开始的拉取/启动会被中断。' : '停止 Docker 引擎？所有运行中的容器会被中断，Docker Desktop 与 com.docker.service 都会结束。'"
                    type="warning" position="lt" :disabled="setupBusy === 'stop'" @ok="emit('stop-engine')">
                    <a-button size="small"
                        v-if="caps.nativeWindows && (engine.available || setupBusy === 'start' || setupBusy === 'stop')"
                        status="danger" :loading="setupBusy === 'stop'" :disabled="setupBusy === 'stop'">
                        <template #icon><icon-poweroff /></template>{{ setupBusy === 'start' ? '停止启动' : (setupBusy === 'stop' ? '停止中' : '停止引擎') }}
                    </a-button>
                </a-popconfirm>
                <a-button size="small" :loading="engineLoading" @click="emit('refresh')">
                    <template #icon><icon-refresh /></template>刷新
                </a-button>
            </a-space>
        </template>

        <!-- 一句话结论：只在“配置想要的后端”与“引擎实际后端”不一致时出现（Docker Desktop 从不报错，只静默沿用） -->
        <a-alert v-if="verdict" type="warning" class="diag-alert">
            <template #title>{{ verdict }}</template>
            <div v-for="(f, i) in verdictFix" :key="'df' + i" class="diag-fix">{{ f }}</div>
        </a-alert>

        <!-- 运行时事实用一张描述表：字段对齐、长值不断行，比散排的色块好读 -->
        <a-descriptions v-if="engine.available" class="engine-desc" bordered size="small" :column="3"
            :label-style="{ width: '96px' }">
            <a-descriptions-item label="API 版本">{{ engine.apiVersion || '-' }}</a-descriptions-item>
            <a-descriptions-item label="OS / Arch">{{ engine.os || '-' }} / {{ engine.arch || '-' }}</a-descriptions-item>
            <a-descriptions-item label="内核">{{ engine.kernel || '-' }}</a-descriptions-item>
            <a-descriptions-item label="虚拟化后端">
                <!-- 悬停看完整证据：判定依据、内核串、真正生效的配置键、遗留旧键 -->
                <a-tooltip position="bottom" :content-style="{ maxWidth: '480px' }">
                    <template #content>
                        <div class="tip-lines">
                            <div class="lt-line">判定依据：{{ backend?.source || '未知' }}</div>
                            <div class="lt-line">内核串：{{ backend?.kernel || '-' }}</div>
                            <div v-if="backend?.settingKey" class="lt-line">
                                生效开关：{{ backend.settingKey }}={{ backend.settingVal || '未写' }}
                            </div>
                            <div v-if="backend?.settingsFile" class="lt-line">配置文件：{{ backend.settingsFile }}</div>
                            <div v-if="backend?.legacyVal" class="lt-line">
                                遗留旧键：wslEngineEnabled={{ backend.legacyVal }}（新版 Docker Desktop 不读它）
                            </div>
                        </div>
                    </template>
                    <b class="linkable">{{ beLabel(engine.backend) }}</b>
                </a-tooltip>
            </a-descriptions-item>
            <a-descriptions-item label="CPU">{{ engine.cpus ? engine.cpus + ' 核' : '-' }}</a-descriptions-item>
            <a-descriptions-item label="内存">{{ fmtBytes(engine.memTotal) }}</a-descriptions-item>
            <a-descriptions-item label="端点" :span="2">
                <span class="mono">{{ engine.endpoint || '-' }}</span>
                <span v-if="probeAt" class="probe-at">· 最近探测 {{ probeAt }}（每 5 秒自动跟踪）</span>
            </a-descriptions-item>
            <a-descriptions-item label="平台">{{ engine.platform || '-' }}</a-descriptions-item>
            <a-descriptions-item label="镜像加速器" :span="3">
                <span class="mono mirrors">{{ mirrorDesc }}</span>
            </a-descriptions-item>
        </a-descriptions>
        <div v-else class="engine-empty">
            <!-- 空态不用 a-empty 大插画：一块状态条 + 一行探测说明就够，下面直接接启动向导 -->
            <div class="off-state">
                <span class="off-icon"><icon-poweroff /></span>
                <div class="off-main">
                    <div class="off-title">无法连接 Docker 引擎</div>
                    <div class="off-sub">
                        <template v-if="probeAt">最近一次探测 {{ probeAt }}，每 5 秒自动重试</template>
                        <template v-else>正在探测引擎端点…</template>
                        <span class="off-sep">·</span>引擎起来后本面板自动接上，无需手动刷新
                    </div>
                </div>
            </div>
            <a-alert v-if="engine.error" type="error" class="setup-alert err-alert">{{ engine.error }}</a-alert>

            <!-- ---------- 安装 / 启动向导（仅 Windows 桌面版） ---------- -->
            <div v-if="caps.nativeWindows" class="setup-block">
                <div class="setup-head">
                    <span class="setup-title">
                        <icon-experiment /> {{ env.dockerInstalled ? '启动 Docker 引擎' : '安装 Docker Desktop' }}
                    </span>
                    <span class="setup-head-extra">
                        <a-tag size="small" :color="env.dockerInstalled ? 'orange' : 'gray'">
                            {{ env.dockerInstalled ? '已安装 · 未运行' : '未安装' }}
                        </a-tag>
                        <!-- 协议与代价不设勾选门禁，改为感叹号悬停如实提示 -->
                        <a-tooltip v-if="licenseNotes.length" position="bottom" :content-style="{ maxWidth: '460px' }">
                            <template #content>
                                <div class="tip-lines">
                                    <div v-for="(l, i) in licenseNotes" :key="'lt' + i" class="lt-line">{{ l }}</div>
                                </div>
                            </template>
                            <a-typography-text class="setup-warn" type="warning">
                                <icon-exclamation-circle />
                            </a-typography-text>
                        </a-tooltip>
                    </span>
                </div>

                <!-- 已安装只差拉起，不必再谈下载 -->
                <template v-if="env.dockerInstalled">
                    <!-- 一条信息行代替整块 alert：路径 + 现状 + 预期耗时，不占三行高度 -->
                    <div class="setup-hint">
                        <span class="hint-label">安装位置</span>
                        <a-tooltip :content="env.dockerExePath" :disabled="!env.dockerExePath">
                            <span class="mono hint-path">{{ env.dockerExePath }}</span>
                        </a-tooltip>
                        <span class="hint-sub">引擎未应答，拉起后等待就绪（首次约 1-2 分钟）</span>
                    </div>
                    <a-space :size="8" class="setup-row">
                        <a-button size="small" :loading="setupBusy === 'start'"
                            :disabled="setupBusy === 'start'" @click="emit('start-engine')">
                            <template #icon><icon-play-arrow /></template>启动引擎
                        </a-button>
                        <!-- 启动是要等 1-2 分钟的阻塞操作：等的时候得能叫停，否则用户只能去任务管理器 -->
                        <a-button size="small" v-if="setupBusy === 'start'" status="danger" @click="emit('stop-engine')">
                            <template #icon><icon-poweroff /></template>停止启动
                        </a-button>
                        <a-button size="small" v-if="!env.wslInstalled" :loading="setupBusy === 'wsl'"
                            :disabled="!!setupBusy" @click="emit('enable-wsl')">
                            <template #icon><icon-common /></template>启用 WSL2
                        </a-button>
                        <a-button size="small" v-else :loading="setupBusy === 'wslupd'" :disabled="!!setupBusy"
                            @click="emit('update-wsl')">
                            <template #icon><icon-common /></template>更新 WSL2
                        </a-button>
                        <a-button size="small" :disabled="!!setupBusy" @click="emit('open-switch')">
                            <template #icon><icon-swap /></template>切换到 {{ targetBackendLabel }}
                        </a-button>
                        <a-button size="small" v-if="!env.hypervInstalled && env.hypervSupported" 
                            :loading="setupBusy === 'hyperv'" :disabled="!!setupBusy" @click="emit('enable-hyperv')">
                            <template #icon><icon-common /></template>启用 Hyper-V
                        </a-button>
                    </a-space>
                    <div class="mode-hint">
                        切到 Hyper-V 前必须先把角色装上并重启计算机；切回 WSL2 只需改配置。两者对外都是同一条命名管道，容器管理功能不受影响。
                    </div>
                </template>

                <template v-else>
                    <!-- 存放目录：留空跟随设置页的插件目录；另存也只动这一个目录，不往系统下载里丢 -->
                    <div class="setup-row">
                        <span class="dir-label">存放目录</span>
                        <a-input :model-value="installDir" class="dir-input" size="small" allow-clear
                            :disabled="!!setupBusy" :placeholder="'留空使用默认（' + pluginsDir + '）'"
                            @update:model-value="(v: string) => emit('update:installDir', String(v ?? ''))"
                            @press-enter="emit('change-dir')" @clear="clearDir" />
                        <a-button size="small" :disabled="!!setupBusy" @click="chooseDir">
                            <template #icon><icon-folder /></template>选择目录
                        </a-button>
                        <a-button v-if="installDir" size="small" :disabled="!!setupBusy" @click="resetDir">恢复默认</a-button>
                    </div>
                    <div class="mode-hint">
                        当前落到 <span class="mono">{{ effectiveDir || '（默认插件目录）' }}</span>；安装包与未下完的缓存都只待在这个目录。
                    </div>
                    <a-alert v-if="dirBad" type="error" class="setup-alert">{{ dirBad }}</a-alert>

                    <a-radio-group :model-value="source" type="button" size="medium" class="setup-row"
                        @change="(v: any) => emit('update:source', v)">
                        <a-radio value="online"><icon-download /> 在线下载</a-radio>
                        <a-radio value="local" :disabled="!env.installerFile"><icon-folder /> 离线安装包</a-radio>
                    </a-radio-group>

                    <template v-if="source === 'online'">
                        <div class="setup-row">
                            <a-input :model-value="urlInput" size="small" allow-clear style="width: 340px"
                                placeholder="留空取官方最新版；也可填 build 号（165345）或完整直链"
                                @update:model-value="(v: string) => emit('update:urlInput', String(v ?? ''))"
                                @press-enter="probeInstaller" />
                            <a-button size="small" :loading="probeLoading" :disabled="!!setupBusy" @click="probeInstaller">
                                <template #icon><icon-search /></template>检测安装包
                            </a-button>
                        </div>
                        <a-tooltip :content="effectiveUrl" :disabled="!effectiveUrl">
                            <div class="url-line mono">{{ effectiveUrl }}</div>
                        </a-tooltip>
                        <div v-if="probeInfo" class="probe-ok">
                            <icon-safe /> 可下载：{{ probeInfo.name }} · {{ fmtBytes(probeInfo.size) }}
                        </div>
                        <a-alert v-if="probeErr" type="error" class="setup-alert">{{ probeErr }}</a-alert>
                        <a-checkbox :model-value="webDownload" class="setup-row"
                            @change="(v: any) => emit('update:webDownload', !!v)">
                            安装时让安装器自行联网拉取组件（--web-download，首次联网更多但本地包更小）
                        </a-checkbox>
                    </template>

                    <a-alert v-else type="normal" class="setup-alert">
                        将使用 <a-tooltip :content="env.installerFile" :disabled="!env.installerFile">
                            <span class="mono ellipsis">{{ env.installerFile }}</span>
                        </a-tooltip>
                        （{{ fmtBytes(env.installerSize) }}），跳过下载。
                    </a-alert>
                    <div class="mode-hint">
                        离线包下载后放入 <span class="mono">{{ effectiveDir }}</span> 再点「重新体检」即可选中。
                    </div>

                    <a-space :size="8" class="setup-row">
                        <a-button v-if="source === 'online'" size="small" :loading="setupBusy === 'download'"
                            :disabled="!!setupBusy" @click="emit('download-installer')">
                            <template #icon><icon-download /></template>先下载到本地
                        </a-button>
                        <a-button size="small" :disabled="!!setupBusy" @click="emit('install-docker')">
                            <template #icon><icon-experiment /></template>安装并启动引擎
                        </a-button>
                        <a-button v-if="!env.wslInstalled" size="small" :loading="setupBusy === 'wsl'"
                            :disabled="!!setupBusy" @click="emit('enable-wsl')">
                            <template #icon><icon-common /></template>启用 WSL2
                        </a-button>
                        <a-button v-else size="small" :loading="setupBusy === 'wslupd'" :disabled="!!setupBusy"
                            @click="emit('update-wsl')">
                            <template #icon><icon-common /></template>更新 WSL2
                        </a-button>
                        <a-tooltip v-if="setupBusy === 'download'" placement="top">
                            <template #content>
                                <div class="tip-lines">
                                    <div class="lt-line">取消会停止下载，并删掉已下载的半截文件（.part）</div>
                                    <div class="lt-line">网络中断不等于取消：那种情况下 .part 会保留以便续传</div>
                                </div>
                            </template>
                            <a-button size="small" status="danger" @click="emit('abort-setup')">取消下载</a-button>
                        </a-tooltip>
                    </a-space>
                    <div class="mode-hint">
                        安装会请求管理员权限并弹出安装器窗口；取消 UAC 即中止，不会留下半途状态。引擎就绪后本面板自动切回容器视图。
                    </div>
                </template>

                <div v-if="setupStage" class="setup-prog" :class="'st-' + setupStage">
                    <div class="prog-head">
                        <span class="prog-label">
                            <com-badge :status="setupStage === 'error' ? 'danger' : (setupStage === 'done' ? 'success' : 'processing')"
                                :animated="setupStage !== 'done' && setupStage !== 'error'" />
                            {{ setupStage === 'error' ? '执行失败' : (setupStage === 'done' ? '已完成' : '进行中') }}
                        </span>
                        <span class="prog-pct">{{ setupPercent }}%</span>
                    </div>
                    <!-- 百分比自己画：进度条自带文字会把 100% 撑成一条很长的绿条，显得很重 -->
                    <a-progress :percent="setupPercent / 100" size="small" :show-text="false"
                        :status="setupStage === 'error' ? 'danger' : (setupStage === 'done' ? 'success' : 'normal')" />
                    <div class="setup-note">{{ setupNote }}</div>
                </div>

                <div v-if="setupLogs.length" class="setup-logs">
                    <div class="setup-head">
                        <span class="setup-subtitle">
                            <icon-history /> 安装日志 <span class="count-pill">{{ setupLogs.length }}</span>
                        </span>
                        <a-button size="small" type="text" @click="emit('clear-logs')">
                            <template #icon><icon-delete /></template>清空
                        </a-button>
                    </div>
                    <div ref="logBoxRef" class="setup-log-box">
                        <div v-for="(l, i) in setupLogs" :key="'sl' + i" class="log-line">{{ l }}</div>
                    </div>
                </div>
            </div>

            <!-- 非桌面构建（Linux / Web 模式）：引擎交给系统包管理器和 systemd，面板只负责连上去 -->
            <div v-else class="setup-block">
                <div class="setup-head">
                    <span class="setup-title">
                        <icon-exclamation-circle /> 引擎由系统服务管理
                    </span>
                </div>
                <div class="mode-hint">
                    <div>本面板不接管引擎的安装与启停：装包、开机自启都交给系统，面板只通过
                        <span class="mono">{{ caps.os === 'windows' ? '命名管道' : 'unix socket' }}</span> 连上去。</div>
                    <div>1. 安装引擎：<span class="mono">yum install -y docker-ce</span>（或 apt 装 docker.io）</div>
                    <div>2. 启动并开机自启：<span class="mono">systemctl enable --now docker</span></div>
                    <div>3. 当前用户需在 docker 组内，或以 root 运行本面板，否则读不到 /var/run/docker.sock</div>
                </div>
            </div>
        </div>
    </a-card>
</template>

<script setup lang="ts">
import { computed, getCurrentInstance, nextTick, onMounted, ref, watch } from "vue";
import { Message } from "@arco-design/web-vue";
import { beLabel, fmtBytes } from "../helpers";
import { useCaps } from "@/utils/caps";

/** 平台能力：安装向导、WSL2、后端切换、引擎启停都只在能开原生窗口的桌面版里出现 */
const caps = useCaps();

const props = defineProps<{
    /** 引擎探测结果（页面持有，定时刷新） */
    engine: any;
    engineLoading: boolean;
    /** 环境体检结果：安装向导与后端事实都靠它 */
    env: any;
    envLoading: boolean;
    /** env.backend：配置意图 + 生效键 + 内核串 */
    backend: any;
    verdict: string;
    verdictFix: string[];
    /** 安装 / 切换后端的进度状态，由页面统一维护 */
    setupBusy: string;
    setupStage: string;
    setupPercent: number;
    setupNote: string;
    setupLogs: string[];
    /** 当前后端的对侧名字，用在「切换到 X」按钮上 */
    targetBackendLabel: string;
    /** 安装包存放目录（空 = 跟随插件目录），页面负责持久化 */
    installDir: string;
    source: "online" | "local";
    urlInput: string;
    webDownload: boolean;
    /** 最近一次引擎状态探测的时刻：证明面板一直在跟踪，而不是开局探一次就定终身 */
    probeAt: string;
}>();

const emit = defineEmits<{
    (e: "open-env"): void;
    (e: "open-settings"): void;
    (e: "open-switch"): void;
    (e: "refresh"): void;
    (e: "stop-engine"): void;
    (e: "start-engine"): void;
    (e: "abort-setup"): void;
    (e: "enable-wsl"): void;
    (e: "update-wsl"): void;
    (e: "enable-hyperv"): void;
    (e: "download-installer"): void;
    (e: "install-docker"): void;
    (e: "clear-logs"): void;
    /** 目录改完（回车 / 清空 / 选好），页面据此落盘并重新体检 */
    (e: "change-dir"): void;
    (e: "update:installDir", v: string): void;
    (e: "update:source", v: "online" | "local"): void;
    (e: "update:urlInput", v: string): void;
    (e: "update:webDownload", v: boolean): void;
}>();

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

/* 只在卡片里用的展示态：体检协议文本、插件目录、直链预览、探测结果 */
const licenseNotes = ref<string[]>([]);
const pluginsDir = ref("");
const effectiveUrl = ref("");
const probeInfo = ref<any>(null);
const probeErr = ref("");
const probeLoading = ref(false);
const logBoxRef = ref<HTMLElement | null>(null);

/** 日志追加后自动贴底：安装过程盯着的是最后一行，不该让用户手动滑 */
watch(() => props.setupLogs.length, () => {
    nextTick(() => {
        const el = logBoxRef.value;
        if (el) el.scrollTop = el.scrollHeight;
    });
});

/** 引擎配了哪些镜像加速器：拉取超时时的第一手线索 */
const mirrorList = computed<string[]>(() => props.engine?.registryMirrors ?? []);
const mirrorDesc = computed<string>(() =>
    mirrorList.value.length ? mirrorList.value.join("  |  ") : "未配置（直连 registry-1.docker.io，很容易超时）"
);

// 实际生效目录由 Go 侧体检回吐（含绝对化与默认回落），前端不自己拼路径
const effectiveDir = computed<string>(() => props.env?.installerDir || "");
// 目录不可用时 Go 侧会写进 Notes（不能静默回落，否则用户以为包下到了他选的目录）
const dirBad = computed<string>(() =>
    ((props.env?.notes || []) as string[]).find((s) => s.includes("安装包目录")) || ""
);

/** 直链预览：换算规则在 Go 侧，前端只负责展示最终要下的那个地址 */
async function refreshUrlPreview() {
    try {
        effectiveUrl.value = await GoTools.DockerSrvBuildInstallerURL(props.urlInput.trim());
    } catch { effectiveUrl.value = ""; }
}
watch(() => props.urlInput, refreshUrlPreview);

async function probeInstaller() {
    probeLoading.value = true;
    probeInfo.value = null;
    probeErr.value = "";
    try {
        probeInfo.value = await GoTools.DockerSrvProbeInstaller(props.urlInput.trim());
        Message.success("安装包可下载：" + probeInfo.value.name);
    } catch (e: any) {
        probeErr.value = String(e?.message || e);
    }
    probeLoading.value = false;
}

async function chooseDir() {
    try {
        const dir = await GoTools.ChooseDir("选择安装包存放目录");
        if (!dir) return;
        emit("update:installDir", dir);
        emit("change-dir");
    } catch (e: any) {
        Message.warning("打开目录选择器失败: " + (e?.message || e));
    }
}

function resetDir() {
    emit("update:installDir", "");
    emit("change-dir");
}

/** 点清空：不赌 Arco 的 update 与 clear 谁先到，先把值置空再让页面落盘 */
function clearDir() {
    emit("update:installDir", "");
    emit("change-dir");
}

onMounted(() => {
    refreshUrlPreview();
    GoTools.DockerSrvLicenseNotes?.()
        .then((n: any) => { licenseNotes.value = n || []; })
        .catch(() => { /* 取不到就不展协议文本 */ });
    GoTools.DockerSrvPluginsDir?.()
        .then((d: any) => { pluginsDir.value = d || ""; })
        .catch(() => { /* 取不到就只显示“默认插件目录” */ });
});
</script>

<style scoped>
.engine-card {
    display: flex;
    flex-direction: column;
}

/* 引擎信息：描述表本身的底色/边框就够区分字段，这里只压行高与长值 */
.engine-desc :deep(.arco-descriptions-item-label),
.engine-desc :deep(.arco-descriptions-item-value) {
    font-size: var(--base-size-small);
}

.engine-desc .mirrors {
    word-break: break-all;
}

/* 后端诊断结论：紧贴引擎信息，不抢戏但一定看得见 */
.diag-alert {
    margin-bottom: 8px;
}

.diag-fix {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    line-height: 1.7;
    padding-left: 12px;
    position: relative;
}

.diag-fix::before {
    content: "→";
    position: absolute;
    left: 0;
    color: var(--color-text-4);
}

/* 可悬停看详情的值：给出这是“能点/能看”的提示 */
.linkable {
    font-weight: 500;
    border-bottom: 1px dashed var(--color-text-4);
    cursor: help;
}

/* 引擎不可用：状态条取代 a-empty 大插画，高度至少省一半 */
.engine-empty {
    padding: 2px 0;
}

.off-state {
    display: flex;
    align-items: center;
    gap: 12px;
    padding: 12px 14px;
    margin-bottom: 10px;
    border: 1px solid var(--color-border-1);
    border-radius: var(--base-radius-default);
    background: var(--color-fill-1);
}

.off-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 36px;
    height: 36px;
    flex: none;
    border-radius: 50%;
    font-size: 18px;
    color: rgb(var(--danger-6));
    background: rgba(var(--danger-6), 0.12);
}

.off-title {
    font-size: var(--base-size-default);
    font-weight: 500;
    color: var(--color-text-1);
}

.off-sub {
    margin-top: 2px;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    line-height: 1.6;
}

.off-sep {
    margin: 0 6px;
    color: var(--color-text-4);
}

/* 引擎原始报错（候选端点、命名管道）很长：限高滚动，不把卡片撑开 */
.err-alert :deep(.arco-alert-content) {
    max-height: 66px;
    overflow-y: auto;
    word-break: break-all;
}

/* 探活时刻：小字附在后面，只为说明「一直在跟踪」，不抢主体信息 */
.probe-at {
    margin-left: 6px;
    font-size: var(--base-size-small);
    color: var(--color-text-3);
}

/* 安装向导：整块做成浅底面板，与上方状态条分层 */
.setup-block {
    margin-top: 10px;
    padding: 12px 14px;
    border: 1px solid var(--color-border-1);
    border-radius: var(--base-radius-default);
    background: var(--color-fill-1);
}

.setup-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    margin-bottom: var(--base-radius-default);
}

.setup-head-extra {
    display: inline-flex;
    align-items: center;
    gap: 8px;
}

.setup-title {
    font-size: var(--base-size-default);
    font-weight: 500;
    color: var(--color-text-1);
}

.setup-subtitle {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

.count-pill {
    padding: 0 6px;
    border-radius: 8px;
    background: var(--color-fill-2);
    color: var(--color-text-3);
    font-size: 11px;
    line-height: 16px;
}

/* 已安装提示：单行信息条，路径过长就截断，悬停看全 */
.setup-hint {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    padding: 6px 10px;
    margin-bottom: 8px;
    border: 1px solid var(--color-border-1);
    border-radius: var(--base-radius-default);
    background: var(--color-bg-2);
    font-size: var(--base-size-small);
}

.hint-label {
    color: var(--color-text-3);
    flex: none;
}

.hint-path {
    max-width: 420px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    color: var(--color-text-2);
}

.hint-sub {
    color: var(--color-text-3);
}

.setup-alert {
    margin-bottom: var(--base-radius-default);
}

.setup-row {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    margin: var(--base-radius-default) 0;
}

.url-line {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    background: var(--color-fill-1);
    border-radius: var(--base-radius-small);
    padding: 2px var(--base-radius-default);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

.probe-ok {
    margin-top: 6px;
    font-size: var(--base-size-small);
    color: rgb(var(--green-6));
}

/* 提示触发器：预警色交给 a-typography-text type="warning" 跟随主题，这里只管排布 */
.setup-warn {
    margin-left: 6px;
    vertical-align: -2px;
    cursor: help;
}

/* 悬停提示的多行正文（弹层虽 teleport 到 body，仍带本组件的 scoped 属性） */
.tip-lines .lt-line {
    display: block;
    line-height: 1.6;
}

.tip-lines .lt-line + .lt-line {
    margin-top: 4px;
}

.mode-hint {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    line-height: 1.7;
}

.ellipsis {
    display: inline-block;
    max-width: 320px;
    vertical-align: bottom;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
}

.setup-prog {
    margin-top: 10px;
    padding: 8px 10px;
    border: 1px solid var(--color-border-1);
    border-radius: var(--base-radius-default);
    background: var(--color-bg-2);
}

.prog-head {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 4px;
}

.prog-label {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
}

.prog-pct {
    font-size: var(--base-size-small);
    color: var(--color-text-3);
    font-variant-numeric: tabular-nums;
}

.setup-note {
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    word-break: break-all;
    white-space: pre-wrap;
}

.setup-logs {
    margin-top: 10px;
}

.setup-log-box {
    height: 120px;
    overflow-y: auto;
    background: var(--color-bg-2);
    border: 1px solid var(--color-border-1);
    border-radius: var(--base-radius-default);
    padding: 6px 10px;
}

.log-line {
    font-family: Consolas, monospace;
    font-size: var(--base-size-small);
    color: var(--color-text-2);
    line-height: 1.7;
    word-break: break-all;
    white-space: pre-wrap;
}

/* 整行可读：悬停给一条浅底，长日志里定位不跑偏 */
.log-line:hover {
    background: var(--color-fill-2);
    border-radius: 3px;
}

.mono {
    font-family: Consolas, monospace;
}
</style>
