<template>
  <div class="docker-page">
    <!-- ==================== 引擎状态（含安装 / 启动向导） ==================== -->
    <engine-card v-model:install-dir="installDir" v-model:source="source" v-model:url-input="urlInput"
      v-model:web-download="webDownload" :engine="engine" :engine-loading="engineLoading" :env="env"
      :env-loading="envLoading" :backend="backend" :verdict="verdict" :verdict-fix="verdictFix" :setup-busy="setupBusy"
      :setup-stage="setupStage" :setup-percent="setupPercent" :setup-note="setupNote" :setup-logs="setupLogs"
      :target-backend-label="targetBackendLabel" :probe-at="probeAt" @open-env="openEnv" @open-settings="openSettings"
      @open-switch="openSwitch" @refresh="manualRefresh" @stop-engine="stopEngine" @start-engine="startEngine"
      @abort-setup="abortSetup" @enable-wsl="enableWsl2" @update-wsl="updateWsl" @enable-hyperv="enableHyperV"
      @download-installer="downloadInstaller" @install-docker="installDocker" @clear-logs="setupLogs = []"
      @change-dir="applyInstallDir" />

    <!-- ==================== 容器 / 镜像 ==================== -->
    <a-card class="main-card card-head-tool" size="small" :body-style="{ padding: 0 }">
      <template #title>
        <a-radio-group v-model="tab" type="button" size="medium" class="tab-group" @change="onTab">
          <a-radio value="overview"><icon-apps /> 总览</a-radio>
          <a-radio value="container"><icon-list /> 容器（{{ containers.length }}）</a-radio>
          <a-radio value="image"><icon-storage /> 镜像（{{ images.length }}）</a-radio>
          <a-radio value="compose"><icon-layers /> 编排（{{ composes.length }}）</a-radio>
          <a-radio value="network"><icon-wifi /> 网络（{{ networks.length }}）</a-radio>
          <a-radio value="volume"><icon-archive /> 存储卷（{{ volumes.length }}）</a-radio>
        </a-radio-group>
      </template>
      <template #extra>
        <a-space class="card-extra" :size="6" wrap>
          <template v-if="tab === 'container'">
            <a-checkbox v-model="showAll" @change="loadContainers()">显示已停止</a-checkbox>
          </template>
          <template v-else-if="tab === 'image'">
            <!-- 搜索展开态：输入框独占工具条，其它操作先收起，避免挤成一团换行 -->
            <template v-if="searchOpen">
              <a-tag v-if="imgScope === 'remote'" size="small" color="arcoblue">线上</a-tag>
              <a-input-search v-if="imgScope === 'remote'" ref="remoteSearchRef" v-model="remoteQuery"
                class="search-box open" size="small" :placeholder="searchPlaceholder" allow-clear
                @search="onRemoteSearch" @keydown.esc="closeSearch" />
              <a-input v-else ref="pullInputRef" v-model="pullInput" class="pull-input open" size="small"
                placeholder="镜像名，多个用空格/逗号分隔" allow-clear @press-enter="pullImage"
                @keydown.esc="closeSearch">
                <template #prefix><icon-search /></template>
              </a-input>
              <a-button v-if="imgScope === 'local'" size="small" :disabled="!engine.available"
                @click="pullImage">
                <template #icon><icon-download /></template>拉取
              </a-button>
              <a-button size="small" @click="closeSearch">
                <template #icon><icon-close /></template>取消
              </a-button>
            </template>
            <!-- 收起态：只留视角 + 常态操作 + 搜索图标，默认一行放得下 -->
            <template v-else>
              <!-- 本地 / 线上视角：与表头同排，切视角只换列不换表 -->
              <a-radio-group :model-value="imgScope" type="button" size="medium" @change="onScope">
                <a-radio value="local"><icon-storage />本地（{{ images.length }}）</a-radio>
                <a-radio value="remote"><icon-cloud />线上</a-radio>
              </a-radio-group>
              <template v-if="imgScope === 'local'">
                <a-button size="small" :disabled="busyOther" @click="openImport">
                  <template #icon><icon-upload /></template>导入
                </a-button>
                <a-button size="small" :disabled="busyOther" @click="openBuild">
                  <template #icon><icon-tool /></template>构建
                </a-button>
                <a-popconfirm content="清理悬空镜像（<none>，且没有任何容器在用）？未被使用的带 tag 镜像不会被动。"
                  type="warning" position="lt" @ok="doPrune">
                  <a-button size="small" :disabled="busyOther">
                    <template #icon><icon-delete /></template>清理
                  </a-button>
                </a-popconfirm>
              </template>
              <a-select v-else v-model="remoteSource" size="small" class="src-select" :options="remoteSources"
                @change="onRemoteSource" />
              <!-- 搜索/拉取入口：点开才出输入框，不在常态占宽 -->
              <a-tooltip :content="imgScope === 'remote' ? '搜索线上镜像' : '按镜像名拉取（可一次多个）'" position="lt">
                <a-button size="small" @click="openSearch">
                  <template #icon><icon-search /></template>
                </a-button>
              </a-tooltip>
              <!-- 任务入口：拉取中也能关掉弹窗去干别的，这里随时回来 -->
              <a-button v-if="taskList.length" size="small" type="outline" @click="openTasks()">
                <template #icon><icon-download /></template>
                任务 {{ taskList.length }}<template v-if="runningCount">（{{ runningCount }} 进行中）</template>
              </a-button>
            </template>
          </template>
          <template v-else-if="tab === 'compose'">
            <!-- 编排是按容器标签聚合的，数量跟着容器走：这里只说口径，不放操作 -->
            <a-tooltip content="按 com.docker.compose.* 标签把容器归组成项目；Engine API 没有 compose 端点，也不解析 yml">
              <span class="res-note">聚合自容器标签</span>
            </a-tooltip>
          </template>
          <template v-else-if="tab === 'network'">
            <a-button size="small" :disabled="resBusy" @click="networkPanelRef?.open()">
              <template #icon><icon-plus /></template>新建网络
            </a-button>
          </template>
          <template v-else-if="tab === 'volume'">
            <a-button size="small" :disabled="resBusy" @click="volumePanelRef?.open()">
              <template #icon><icon-plus /></template>新建卷
            </a-button>
            <a-popconfirm content="清理没有任何容器引用的存储卷？被容器挂着的卷不会被动。" type="warning" position="lt"
              @ok="pruneVolumes">
              <a-button size="small" :disabled="resBusy">
                <template #icon><icon-delete /></template>清理未使用
              </a-button>
            </a-popconfirm>
          </template>
          <a-button size="small" :disabled="!engine.available" @click="refreshCurrent()">
            <template #icon><icon-refresh /></template>刷新
          </a-button>
        </a-space>
      </template>

      <!-- 未装引擎：列表区降级为一条说明，不再摆一大块空白 a-empty -->
      <div v-if="!engine.available" class="op-empty">
        <span class="op-empty-icon"><icon-storage /></span>
        <div class="op-empty-text">
          <div class="op-empty-title">引擎未就绪，列表暂不可用</div>
          <div class="op-empty-sub">容器、镜像、网络与存储卷都靠 Docker 引擎取数，启动后这里会自动加载</div>
        </div>
      </div>

      <template v-else>
        <!-- 后台轮询失败不清表、不连环弹 toast，只在这里亮一条 -->
        <a-alert v-if="listErr" type="warning" show-icon class="list-err" title="后台刷新失败，下表为上一次的结果">
          {{ listErr }}
        </a-alert>

        <!-- 总览：卡片视角，数据与下面几张表同源，不另打接口 -->
        <div v-show="tab === 'overview'" class="pane pane-scroll">
          <overview-panel :containers="containers" :images="images" :registries="registries" :networks="networks"
            :volumes="volumes" :composes="composes" :loading="listLoading" :engine-label="engineLabel"
            @refresh="refreshCurrent()" @open-monitor="openMonitor" @open-logs="openLogModal" @open-term="openTerm" />
        </div>

        <!-- 容器 -->
        <div v-show="tab === 'container'" class="pane">
          <container-table :data="containers" :loading="listLoading" :busy="ctrBusy" @start="doStart" @stop="doStop"
            @restart="doRestart" @rename="openRename" @logs="openLogModal" @term="openTerm" @monitor="openMonitor"
            @remove="doRemove" />
        </div>

        <!-- 镜像：本地 / 线上 两个视角共用一张表 -->
        <div v-show="tab === 'image'" class="pane">
          <image-table :images="images" :loading="listLoading" :scope="imgScope" :remote-rows="remoteRows"
            :remote-notice="remoteNotice" :remote-loading="remoteLoading" :remote-source="remoteSource"
            :registries="registries" @dismiss-notice="remoteNotice = null" @load-more="searchRemote(false)"
            @run="openRun" @push="openPush" @export-image="exportImage" @remove="removeImage"
            @pull-remote="pullRemote" />
        </div>

        <!-- 编排：按 compose 标签归组的项目（Engine API 没有 compose 端点） -->
        <div v-show="tab === 'compose'" class="pane">
          <compose-panel :projects="composes" :loading="compLoading" :busy="resBusy" :busy-on="compBusyOn" @action="composeAction" />
        </div>

        <!-- 网络 -->
        <div v-show="tab === 'network'" class="pane">
          <network-panel ref="networkPanelRef" :networks="networks" :loading="netLoading" @create="createNetwork"
            @remove="removeNetwork" />
        </div>

        <!-- 存储卷 -->
        <div v-show="tab === 'volume'" class="pane">
          <volume-panel ref="volumePanelRef" :volumes="volumes" :loading="volLoading" @create="createVolume"
            @remove="removeVolume" />
        </div>
      </template>
    </a-card>

    <!-- ==================== 环境体检 ==================== -->
    <env-check-modal v-model:visible="envVisible" :env="env" :docker-distro-text="dockerDistroText"
      :cur-backend="curBackend" :hyperv-tag="hypervTag" :env-notes="envNotes" :env-loading="envLoading"
      :setup-busy="setupBusy" :setup-note="setupNote" @refresh="loadEnv" @enable-wsl="enableWsl2"
      @update-wsl="updateWsl" />

    <!-- ==================== 镜像任务中心（拉取/推送/构建/导入/导出/清理，可并行） ==================== -->
    <task-center-modal v-model:visible="taskVisible" v-model:active-id="activeTaskId" :tasks="taskList"
      @cancel="cancelTask" @cancel-all="cancelAllTasks" @retry="retryTask" @clear="clearFinishedTasks" />

    <!-- ==================== 由镜像创建容器 ==================== -->
    <image-run-modal v-model:visible="runVisible" :image="runImage" @submit="doRun" />

    <!-- ==================== 推送 ==================== -->
    <image-push-modal v-model:visible="pushVisible" :image="pushImage" :target="pushTarget" @submit="doPush" />

    <!-- ==================== 导入镜像包 ==================== -->
    <image-import-modal v-model:visible="importVisible" @submit="doImport" />

    <!-- ==================== 构建镜像 ==================== -->
    <image-build-modal v-model:visible="buildVisible" @submit="doBuild" />

    <!-- ==================== 仓库与加速站 ==================== -->
    <docker-settings-modal v-model:visible="settingsVisible" @changed="onSettingsChanged"
      @engine-changed="onEngineChanged" />

    <!-- ==================== 容器日志 ==================== -->
    <container-log-modal v-model:visible="logVisible" v-model:follow="logFollow" :title="logTitle" :running="logRunning"
      :lines="logLines" @refresh="loadLogs" @clear="logLines = []" @toggle="toggleFollow" @close="closeLogModal" />

    <!-- ==================== 容器实时监控 ==================== -->
    <container-monitor-modal v-if="monVisible" v-model:visible="monVisible" :id="monInst?.id || ''" :name="monInst?.name || ''" />

    <!-- ==================== 容器终端（复用通用 term-console） ==================== -->
    <container-term-modal v-if="termVisible" v-model:visible="termVisible" :id="termInst?.id || ''" :name="termInst?.name || ''" />

    <!-- ==================== 重命名 ==================== -->
    <rename-modal v-model:visible="renameVisible" :current="renameOld" @submit="doRename" />

    <!-- ==================== 切换虚拟化后端 ==================== -->
    <switch-backend-modal v-model:visible="switchVisible" v-model:confirmed="switchConfirmed"
      :target-label="targetBackendLabel" :notes="switchNotes" :verdict="verdict" :verdict-fix="verdictFix"
      :busy="setupBusy" :percent="setupPercent" :stage="setupStage" :note="setupNote" :logs="setupLogs"
      @enable-wsl="enableWsl2" @update-wsl="updateWsl" @submit="submitSwitch" />

    <!-- 任务浮标：关掉任务弹窗后仍在跑（或有失败）时驻在右下角，点一下就回去看详情 -->
    <div v-if="fabVisible" class="task-fab" @click="openTasks()">
      <div class="tf-main">
        <div class="tf-head">
          <a-spin v-if="fabTask?.busy" :size="14" />
          <icon-close-circle-fill v-else class="tf-bad" />
          <a-tooltip :content="fabTask?.name || ''" :disabled="!fabTask?.name">
            <span class="tf-title mono">{{ fabTask?.name }}</span>
          </a-tooltip>
          <span class="tf-pct">{{ Math.round(fabTask?.percent || 0) }}%</span>
        </div>
        <div class="tf-bar">
          <i :class="{ bad: !fabTask?.busy }" :style="{ width: (fabTask?.percent || 0) + '%' }"></i>
        </div>
        <div class="tf-sub">
          {{ runningCount ? `${runningCount} 个进行中` : `${failedCount} 个任务失败` }}
        </div>
      </div>
      <icon-right class="tf-arrow" />
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, getCurrentInstance, nextTick, onMounted, onUnmounted, reactive, ref } from "vue";
import { Message } from "@arco-design/web-vue";
import { Events } from "@wailsio/runtime";
import CacheKey from "@/constants/cache-key";
import { getStorage, setStorage } from "@/utils";
import ComposePanel from "./components/ComposePanel.vue";
import ContainerLogModal from "./components/ContainerLogModal.vue";
import ContainerTable from "./components/ContainerTable.vue";
import DockerSettingsModal from "./components/DockerSettingsModal.vue";
import EngineCard from "./components/EngineCard.vue";
import EnvCheckModal from "./components/EnvCheckModal.vue";
import ImageBuildModal from "./components/ImageBuildModal.vue";
import ImageImportModal from "./components/ImageImportModal.vue";
import ImagePushModal from "./components/ImagePushModal.vue";
import ImageRunModal from "./components/ImageRunModal.vue";
import ImageTable from "./components/ImageTable.vue";
import NetworkPanel from "./components/NetworkPanel.vue";
import OverviewPanel from "./components/OverviewPanel.vue";
import RenameModal from "./components/RenameModal.vue";
import SwitchBackendModal from "./components/SwitchBackendModal.vue";
import TaskCenterModal from "./components/TaskCenterModal.vue";
import VolumePanel from "./components/VolumePanel.vue";
import {
  beLabel, fmtBytes, hostOnly, localRegistryOf,
  type ImageTask, type RunForm, type TaskOp,
} from "./helpers";

/*
 * 两个重弹窗改成异步加载：
 *  - 实时监控：内联引 echarts/core + 图表模块，静态引入会把整个 echarts
 *    绑进 docker 这个 chunk（实测 1.5MB），而这只是“点开监控”才用得到的东西；
 *  - 容器终端：引 term-console（xterm + 适配器，300KB+）。
 * 配合模板上的 v-if（关掉就卸载）才真正省：不点就永远不下这两个包。
 */
const ContainerMonitorModal = defineAsyncComponent(() => import("./components/ContainerMonitorModal.vue"));
const ContainerTermModal = defineAsyncComponent(() => import("./components/ContainerTermModal.vue"));

const GoTools = (getCurrentInstance() as any)?.appContext.config.globalProperties.$GoTools;

/* ============================ 状态 ============================ */
const engine = ref<any>({ available: false, cpus: 0, memTotal: 0 });
const engineLoading = ref(false);
// 最近一次拿到引擎结论的时刻：面板上直说，免得用户分不清「这是刚探的」还是「开机探了一次」
const probeAt = ref("");
/** 引擎一句话标识（平台 + 版本），总览的资源概览右上角用；读不到就空着 */
const engineLabel = computed(() => {
  const e: any = engine.value || {};
  const platform = String(e.platform || "").trim();
  const ver = String(e.version || "").trim();
  if (platform) return platform;
  return ver ? `Docker Engine v${ver}` : "";
});
// 总览为首页：容器 / 镜像之外，编排、网络、存储卷各占一页
const tab = ref<"overview" | "container" | "image" | "compose" | "network" | "volume">("overview");
const showAll = ref(true);
const containers = ref<any[]>([]);
const images = ref<any[]>([]);
const listLoading = ref(false);
// 后台轮询状态：上一次结果签名 + 在飞标记 + 静默错误（走表格上方 a-alert，不弹 toast）
let pollSig: Record<"container" | "image", string> = { container: "", image: "" };
let pollInflight = false;
const listErr = ref("");

/* ---------- 网络 / 存储卷 / 编排 ---------- */
// 三份清单都来自 daemon：各自一份 loading，沉默轮询共用 pollInflight 让路标记
const networks = ref<any[]>([]);
const volumes = ref<any[]>([]);
const composes = ref<any[]>([]);
const netLoading = ref(false);
const volLoading = ref(false);
const compLoading = ref(false);
/** 网络 / 卷 / 编排的写操作进行中：按钮统一置灰，列表轮询也让路 */
const resBusy = ref(false);
// 新建表单挂在面板里，页面只通过 ref 打开它（表单状态不外溢到页面）
// 名字必须带 Ref 后缀：script setup 下 kebab 标签 network-panel / volume-panel 会优先拿同名变量当组件，
// 拿到的是 ref 的当前值（null）→ 整个面板渲染成空白
const networkPanelRef = ref<any>(null);
const volumePanelRef = ref<any>(null);

type ResKind = "network" | "volume" | "compose";
let resSig: Record<ResKind, string> = { network: "", volume: "", compose: "" };
const resLoading = { network: netLoading, volume: volLoading, compose: compLoading };
const resData = { network: networks, volume: volumes, compose: composes };

let offPull: any = null;
let offImageOp: any = null;
let offLog: any = null;
let offSetup: any = null;
let pollTimer: any = null;

/* ============================ 引擎 ============================ */
const guideLines = [
  "未检测到 Docker 引擎：请确认已安装 Docker Desktop 并处于运行状态（右下角鲸鱼图标为绿色）。",
  "本项目通过 Windows 命名管道 \\\\.\\pipe\\docker_engine 直连 Engine API，不依赖 docker 命令行。",
  "若引擎装在远端或自定义端点，可设置环境变量 DOCKER_HOST（支持 npipe://、unix://、tcp://）后重启本应用。",
  "Linux 容器需要 WSL2 后端：wsl --status 可确认；未安装时 Docker Desktop 安装向导会引导启用。",
];

async function loadEngine(withEnv = true) {
  engineLoading.value = true;
  try {
    engine.value = (await GoTools.DockerSrvEngineStatus()) || { available: false };
  } catch (e: any) {
    engine.value = { available: false, error: e?.message || String(e) };
  }
  engineLoading.value = false;
  probeAt.value = new Date().toLocaleTimeString();
  if (engine.value.available) refreshCurrent();
  // 已连接时也要体检：后端结论（配置键 vs 引擎内核串）靠它才算得出来
  if (withEnv) loadEnv();
}

/** 手动刷新：引擎与体检一起重新取（定时器不走这里，免得每 5 秒拉一次 wsl/powershell） */
async function manualRefresh() {
  await loadEngine(true);
}

/* ============================ 安装向导 ============================ */
const env = ref<any>({ notes: [] });
const envVisible = ref(false);
const envLoading = ref(false);
const source = ref<"online" | "local">("online");
const urlInput = ref("");
const webDownload = ref(false);
const setupBusy = ref("");
const setupStage = ref("");
const setupPercent = ref(0);
const setupNote = ref("");
const setupLogs = ref<string[]>([]);
// 另存目录：空 = 跟随设置页的插件目录（<插件目录>\docker）；只记住这一个偏好
const installDir = ref<string>(String(getStorage(CacheKey.DOCKER_INSTALLER_DIR) || ""));

// Go 侧体检文案优先（它知道真实环境）；取不到时退回通用引导，不至于空白
const envNotes = computed<string[]>(() => {
  const n = env.value?.notes || [];
  return n.length ? n : guideLines;
});

async function loadEnv() {
  envLoading.value = true;
  try {
    env.value = (await GoTools.DockerSrvCheckEnv(installDir.value.trim())) || { notes: [] };
  } catch (e: any) {
    env.value = { notes: ["体检失败: " + (e?.message || e)] };
  }
  // 离线包可能被删：选项失效就回到在线下载，避免对着空路径点安装
  if (source.value === "local" && !env.value.installerFile) source.value = "online";
  envLoading.value = false;
}

/** 体检收进弹窗：平时不占地方，要看证据时再展开 */
function openEnv() {
  envVisible.value = true;
  loadEnv();
}

/** 改目录后重新体检：下载、离线包扫描、展示都跟着新目录走 */
function applyInstallDir() {
  const v = installDir.value.trim();
  installDir.value = v;
  if (v) setStorage(CacheKey.DOCKER_INSTALLER_DIR, v);
  else localStorage.removeItem(CacheKey.DOCKER_INSTALLER_DIR);
  loadEnv();
}

function pushSetupLog(s: string) {
  setupLogs.value.push(`[${new Date().toLocaleTimeString()}] ${s}`);
  if (setupLogs.value.length > 300) setupLogs.value.splice(0, setupLogs.value.length - 300);
}

/** docker:setup 事件 → 阶段、百分比与日志（负载 SetupProgress） */
function onSetupEvent(raw: any) {
  const p = raw?.data ?? raw;
  if (!p || !p.stage) return;
  setupStage.value = p.stage;
  if (p.note) {
    setupNote.value = p.note;
    pushSetupLog(p.note);
  }
  if (p.stage === "downloading") setupPercent.value = p.percent || 0;
  if (p.stage === "done") setupPercent.value = 100;
  if (p.stage === "error") {
    setupPercent.value = 0;
    setupBusy.value = "";
  }
}

async function downloadInstaller() {
  setupBusy.value = "download";
  setupStage.value = "downloading";
  setupPercent.value = 0;
  setupLogs.value = [];
  pushSetupLog("下载目录：" + (env.value?.installerDir || "默认插件目录"));
  try {
    const p = await GoTools.DockerSrvDownloadInstaller(urlInput.value.trim(), installDir.value.trim());
    Message.success("安装包已就绪");
    pushSetupLog("已就位: " + p);
    await loadEnv();
  } catch (e: any) {
    if (String(e?.message || e).includes("已取消")) Message.warning("下载已取消");
    else Message.error("下载失败: " + (e?.message || e));
  }
  if (setupBusy.value === "download") setupBusy.value = "";
}

async function installDocker() {
  setupBusy.value = "install";
  setupStage.value = "installing";
  setupPercent.value = 0;
  pushSetupLog("发起安装（需管理员权限，请在 UAC 与安装器窗口中确认）：安装器将以 --accept-license 静默执行…");
  const path = source.value === "local" ? (env.value.installerFile || "") : "";
  try {
    await GoTools.DockerSrvInstallDocker(path, urlInput.value.trim(), installDir.value.trim(), webDownload.value);
    Message.success("Docker 引擎已就绪");
    setupStage.value = "done";
    setupPercent.value = 100;
    await loadEngine(true);
  } catch (e: any) {
    Message.error("安装未完成: " + (e?.message || e));
    await loadEnv();
  }
  if (setupBusy.value === "install") setupBusy.value = "";
}

async function enableWsl2() {
  setupBusy.value = "wsl";
  setupStage.value = "info";
  pushSetupLog("即将提权启用 WSL2（wsl --install --no-distribution）…");
  try {
    await GoTools.DockerSrvEnableWsl2();
    Message.success("WSL2 处理已提交");
  } catch (e: any) {
    Message.warning("WSL2 未启用: " + (e?.message || e));
  }
  await loadEnv();
  if (setupBusy.value === "wsl") setupBusy.value = "";
}

// 组件在、但内核过旧/坏了时，Docker Desktop 不会报错，只是静默沿用 Hyper-V：
// 这个入口就为了那个场景（wsl --update）
async function updateWsl() {
  setupBusy.value = "wslupd";
  setupStage.value = "info";
  pushSetupLog("即将提权更新 WSL2（wsl --update）…");
  try {
    await GoTools.DockerSrvUpdateWsl();
    Message.success("WSL2 更新已提交");
  } catch (e: any) {
    Message.warning("WSL2 更新未成功: " + (e?.message || e));
  }
  await loadEnv();
  if (setupBusy.value === "wslupd") setupBusy.value = "";
}

/* ============================ 虚拟化后端 ============================ */
const switchVisible = ref(false);
const switchConfirmed = ref(false);

// 当前后端：优先信引擎上报的内核串（运行事实），其次信体检读到的配置（用户意图）
const curBackend = computed<string>(() => engine.value?.backend || env.value?.backend?.name || "");
const curBackendLabel = computed(() => env.value?.backend?.label || beLabel(curBackend.value) || "未知");
const targetBackendLabel = computed(() => (curBackend.value === "hyperv" ? "WSL2" : "Hyper-V"));

/** Hyper-V 角色：文字与圆点共用一套语义，组件里只负责排版 */
const hypervTag = computed<{ text: string; status: "success" | "warning" | "normal"; cls: string }>(() => {
  if (env.value?.hypervInstalled) {
    return env.value.hypervRunning
      ? { text: "已安装·正在跑", status: "success", cls: "is-on" }
      : { text: "已安装·未跑", status: "warning", cls: "is-warn" };
  }
  return env.value?.hypervSupported
    ? { text: "未安装", status: "warning", cls: "is-warn" }
    : { text: "系统不支持", status: "normal", cls: "is-dim" };
});

const wslEngineKey = "wslEngineEnabled";

// 体检得到的后端事实（Go 侧已算好：配置意图、真正生效的键、遗留旧键、内核串）
const backend = computed<any>(() => env.value?.backend || null);

// 一句话结论：只有“配置想要的后端 ≠ 引擎实际后端”时 Go 才会给；
// 一致时为空，不唠叨（Docker Desktop 从不报错，只会静默沿用旧后端，所以这条必须显眼）
const verdict = computed<string>(() => backend.value?.verdict || "");
const verdictFix = computed<string[]>(() => backend.value?.fix || []);

const dockerDistroText = computed<string>(() => {
  switch (env.value?.wslDockerDistro) {
    case "running": return "正在运行";
    case "stopped": return "已停止";
    case "installing": return "正在安装";
    default: return "不存在（切到 WSL2 时由 Docker Desktop 自建）";
  }
});

// 切换代价不可逆，所有后果在弹窗里逐条列清，不只问一句“确定吗”
const switchNotes = computed<string[]>(() => {
  const b = backend.value;
  const n: string[] = [`当前后端：${curBackendLabel.value} → 目标后端：${targetBackendLabel.value}。`];
  if (targetBackendLabel.value === "Hyper-V") {
    if (!env.value?.hypervInstalled) {
      n.push("Hyper-V 角色尚未安装：先点「启用 Hyper-V」并重启计算机，否则切换后引擎起不来。");
    } else if (!env.value?.hypervRunning) {
      n.push("Hyper-V 角色已安装但管理程序未在跑：切换后长时间不起就重启计算机再试。");
    }
    n.push("Hyper-V 后端下 Windows 目录要走 File sharing 授权，chown/AIO 语义与 WSL2 不同；MySQL 这类数据库建议改用数据卷。");
  } else if (!env.value?.wslInstalled) {
    n.push("WSL2 组件尚未就绪（见体检里的 wsl 原文）：先点「更新 WSL2」再切，否则引擎同样起不来。");
  } else if (!(env.value?.wslDistros || []).length) {
    n.push("WSL 里还没有任何发行版：这不影响 WSL2 后端，Docker Desktop 会自建 docker-desktop。");
  }
  if (b?.settingsFile) {
    n.push(`将改写 ${b.settingsFile} 里的 ${b.settingKey}（原文件备份为 .gobak，其余配置项不动）。`);
    if (b.legacyVal) {
      n.push(`文件里遗留的旧键 ${wslEngineKey}=${b.legacyVal} 会同步成同一意图：新版 Docker Desktop 本来就不读它。`);
    }
  }
  n.push("流程：停止 Docker Desktop → 改配置 → 回读校验没被回写 → 重新拉起并等引擎就绪（可能几分钟）。");
  n.push("引擎起来后还会用内核串核验实际后端：若被回落成另一个后端会明确报错，不会只提示切换成功。");
  return n;
});

async function openSwitch() {
  switchConfirmed.value = false;
  switchVisible.value = true;
  // 引擎在跑时不会自动体检，这里补一次，保证弹窗里的后端与角色状态是新的
  loadEnv();
}

/** 切换后端：成功才关弹窗，失败留在弹窗里看完整过程 */
async function submitSwitch() {
  if (!switchConfirmed.value) {
    Message.warning("请先勾选知晓切换影响");
    return;
  }
  setupBusy.value = "backend";
  setupStage.value = "info";
  setupPercent.value = 0;
  const to = targetBackendLabel.value;
  pushSetupLog(`发起后端切换 → ${to}（会先停止 Docker Desktop）…`);
  let refreshed = false;
  try {
    await GoTools.DockerSrvSwitchBackend(to === "WSL2", true);
    // 后端方法只在内核串核验不过时才报错；这里再比一次面板上的后端，
    // 避开“提示切换成功、标签还是 Hyper-V”的矛盾
    await loadEngine(true);
    refreshed = true;
    const got = beLabel(engine.value?.backend || env.value?.backend?.name || "");
    if (got === to) {
      Message.success("已切换为 " + to + " 后端（内核串核验通过）");
      switchVisible.value = false;
      return;
    }
    Message.warning(`配置已改为 ${to}，但引擎实际识别为 ${got}：以后端标签为准，完整结论留在弹窗里`);
  } catch (e: any) {
    const msg = String(e?.message || e);
    // 核验不过时原文很长，弹条短的，完整结论留在弹窗里
    Message.error(msg.length > 90 ? "后端未切成功：完整原因见弹窗里的过程说明" : "切换未完成: " + msg);
  } finally {
    if (setupBusy.value === "backend") setupBusy.value = "";
    if (!refreshed) await loadEngine(true);
  }
}

async function enableHyperV() {
  setupBusy.value = "hyperv";
  setupStage.value = "info";
  pushSetupLog("即将提权启用 Hyper-V（dism /enable-feature Microsoft-Hyper-V-All），完成后需重启计算机…");
  try {
    await GoTools.DockerSrvEnableHyperV(true);
    Message.success("Hyper-V 组件已提交启用，请重启计算机后再切换后端");
  } catch (e: any) {
    Message.warning("Hyper-V 未启用: " + (e?.message || e));
  }
  await loadEnv();
  if (setupBusy.value === "hyperv") setupBusy.value = "";
}

async function startEngine() {
  setupBusy.value = "start";
  setupStage.value = "starting";
  setupPercent.value = 0;
  try {
    await GoTools.DockerSrvStartEngine();
    Message.success("Docker 引擎已就绪");
    await loadEngine(true);
  } catch (e: any) {
    const msg = String(e?.message || e);
    // 用户点了「停止启动」也会走到这里，那不是故障
    if (msg.includes("已取消")) Message.warning("已停止启动");
    else Message.error("引擎未就绪: " + msg);
    await loadEnv();
  }
  if (setupBusy.value === "start") setupBusy.value = "";
}

async function abortSetup() {
  try {
    await GoTools.DockerSrvAbortSetup();
    pushSetupLog("已请求取消下载，正在停止并清理未下完的临时文件…");
  } catch (e: any) {
    Message.warning(String(e?.message || e));
  }
}

/* ============================ 列表 ============================ */
function onTab(v: string | number | boolean) {
  tab.value = v as any;
  refreshCurrent();
}

function refreshCurrent(silent = false) {
  if (!engine.value.available) return;
  // 总览同时用容器 / 镜像 / 网络 / 卷 / 编排五份清单：顺序拉，避免互相抢同一个在飞标记
  if (tab.value === "overview") return refreshOverview(silent);
  if (tab.value === "container") return loadContainers(silent);
  if (tab.value === "image") return loadImages(silent);
  if (tab.value === "compose") return loadRes("compose", silent);
  if (tab.value === "network") return loadRes("network", silent);
  if (tab.value === "volume") return loadRes("volume", silent);
}

async function refreshOverview(silent = false) {
  await loadContainers(silent);
  await loadImages(silent);
  // 资源概览卡与三个 tab 的计数都靠这三份，总览页一并拉到
  await loadRes("network", silent);
  await loadRes("volume", silent);
  await loadRes("compose", silent);
}

// 引擎离线时的静默探活：EngineStatus 不可达只回错误信息，不会报 Binding 错，
// 所以轮询在“不可用”状态下改走它，而不是每 5 秒调一次列表接口。
// 两个方向都要跟上：用户从托盘退出 Docker Desktop 时，面板也得在 5 秒内转成“不可用”。
let probeInflight = false;
async function probeEngine() {
  if (probeInflight) return;
  probeInflight = true;
  const wasUp = !!engine.value.available;
  try {
    const info = (await GoTools.DockerSrvEngineStatus()) || { available: false };
    engine.value = info;
    probeAt.value = new Date().toLocaleTimeString();
    if (info.available && !wasUp) {
      Message.success("Docker 引擎已恢复连接");
      refreshCurrent();
      loadEnv();
    } else if (!info.available && wasUp) {
      // 从“已连接”掉回“不可用”：列表留着（旧行至少还是事实），只把断开说明换上去
      listErr.value = info.error || "引擎已断开应答";
    }
  } catch {
    /* 探活本身报错（绑定调用异常）：只记时刻，下一轮再试 */
    probeAt.value = new Date().toLocaleTimeString();
  } finally {
    probeInflight = false;
  }
}

/** silent = 定时轮询：不亮 loading（遮罩每几秒淡入淡出就是闪），且数据没变就不换数组引用 */
async function loadList(kind: "container" | "image", silent = false) {
  if (silent && (pollInflight || listLoading.value)) return; // 手动/操作后的请求优先，轮询给它让路
  if (silent) pollInflight = true; else listLoading.value = true;
  const label = kind === "container" ? "容器" : "镜像";
  try {
    const rows = kind === "container"
      ? (await GoTools.DockerSrvListContainers(showAll.value)) || []
      : (await GoTools.DockerSrvListImages()) || [];
    listErr.value = "";
    let sig = "";
    try { sig = JSON.stringify(rows); } catch { sig = ""; } // 拿不到签名就当变了，宁可多刷一次
    if (silent && sig && sig === pollSig[kind]) return;
    pollSig[kind] = sig;
    // 镜像表一个镜像多个 tag 会拆成多行，短 ID 重复会让 keyed diff 错乱，补个合成键
    if (kind === "container") containers.value = rows;
    else images.value = rows.map((r: any) => ({ ...r, _key: `${r.repo}:${r.tag}@${r.digest}` }));
  } catch (e: any) {
    const msg = String(e?.message || e);
    if (silent) {
      // 后端切换/重启引擎时 daemon 会短暂下线：直接转成“不可用”，由定时器改去探活，
      // 免得每 5 秒往终端刷一条 Binding 调用失败
      listErr.value = msg;
      engine.value = { ...engine.value, available: false, error: msg };
      probeAt.value = new Date().toLocaleTimeString();
      return;
    }
    pollSig[kind] = "";
    if (kind === "container") containers.value = []; else images.value = [];
    Message.error(`获取${label}列表失败: ` + msg);
  } finally {
    if (silent) pollInflight = false; else listLoading.value = false;
  }
}

function loadContainers(silent = false) { return loadList("container", silent); }
function loadImages(silent = false) { return loadList("image", silent); }

/* ============================ 网络 / 存储卷 / 编排 ============================ */
/** 三类资源共一个壳：静默轮询不亮 loading、数据没变就不换数组；失败只亮提示不清表 */
async function loadRes(kind: ResKind, silent = false) {
  const loading = resLoading[kind];
  if (silent && (pollInflight || loading.value)) return;
  if (silent) pollInflight = true; else loading.value = true;
  const label = kind === "network" ? "网络" : kind === "volume" ? "存储卷" : "编排项目";
  try {
    const rows = kind === "network"
      ? await GoTools.DockerSrvListNetworks()
      : kind === "volume"
        ? await GoTools.DockerSrvListVolumes()
        : await GoTools.DockerSrvListComposeProjects();
    const list: any[] = Array.isArray(rows) ? rows : [];
    listErr.value = "";
    let sig = "";
    try { sig = JSON.stringify(list); } catch { sig = ""; }
    if (silent && sig && sig === resSig[kind]) return;
    resSig[kind] = sig;
    resData[kind].value = list;
  } catch (e: any) {
    const msg = String(e?.message || e);
    // 失败一律不动旧数据（daemon 抖动时表格不该凭空清空）；静默轮询只亮提示，不弹 toast
    if (silent) { listErr.value = msg; return; }
    resSig[kind] = "";
    Message.error(`获取${label}列表失败: ` + msg);
  } finally {
    if (silent) pollInflight = false; else loading.value = false;
  }
}

async function createNetwork(spec: { name: string; driver: string; subnet: string; internal: boolean }) {
  resBusy.value = true;
  try {
    await GoTools.DockerSrvCreateNetwork(spec);
    Message.success(`网络 ${spec.name} 已创建`);
  } catch (e: any) { Message.error("创建网络失败: " + (e?.message || e)); }
  finally { resBusy.value = false; }
  loadRes("network");
}

async function removeNetwork(rec: any) {
  resBusy.value = true;
  try {
    await GoTools.DockerSrvRemoveNetwork(rec.id);
    Message.success(`网络 ${rec.name} 已删除`);
  } catch (e: any) { Message.error("删除网络失败: " + (e?.message || e)); }
  finally { resBusy.value = false; }
  loadRes("network");
}

async function createVolume(spec: { name: string; driver: string }) {
  resBusy.value = true;
  try {
    await GoTools.DockerSrvCreateVolume(spec);
    Message.success(`存储卷 ${spec.name} 已创建`);
  } catch (e: any) { Message.error("创建存储卷失败: " + (e?.message || e)); }
  finally { resBusy.value = false; }
  loadRes("volume");
}

async function removeVolume(rec: any) {
  resBusy.value = true;
  try {
    await GoTools.DockerSrvRemoveVolume(rec.name);
    Message.success(`存储卷 ${rec.name} 已删除`);
  } catch (e: any) { Message.error("删除存储卷失败: " + (e?.message || e)); }
  finally { resBusy.value = false; }
  loadRes("volume");
}

async function pruneVolumes() {
  resBusy.value = true;
  try {
    const rep = await GoTools.DockerSrvPruneVolumes();
    const n = (rep?.deleted || []).length;
    Message.success(n ? `已清理 ${n} 个未使用的卷，回收 ${fmtBytes(rep.space)}` : "没有未使用的存储卷");
  } catch (e: any) { Message.error("清理失败: " + (e?.message || e)); }
  finally { resBusy.value = false; }
  loadRes("volume");
}

const COMPOSE_VERB: Record<string, string> = { start: "启动", stop: "停止", restart: "重启", down: "移除" };
/** 正在执行的编排动作：只让当前项目的按钮显示「停止中/移除中」 */
const compBusyOn = ref<{ name: string; action: string } | null>(null);

/** 批量动作逐个容器执行（Go 侧汇总文案）：失败不无条件报错，全部失败才算失败 */
async function composeAction(p: any, action: "start" | "stop" | "restart" | "down") {
  if (resBusy.value) return;
  const verb = COMPOSE_VERB[action] || action;
  resBusy.value = true;
  compBusyOn.value = { name: p.name, action };
  try {
    const msg = await GoTools.DockerSrvComposeAction(p.name, action);
    Message.success(msg || `已${verb} ${p.name}`);
  } catch (e: any) {
    Message.error(`${verb}失败: ` + (e?.message || e));
  } finally {
    resBusy.value = false;
    compBusyOn.value = null;
  }
  // 编排动的是容器：项目与容器两份清单都要重拉
  await Promise.all([loadRes("compose"), loadContainers()]);
}

/* ============================ 容器操作 ============================ */
/** 容器动作进行中：暂停列表轮询，别在 daemon 忙着建/起容器时再去戳它 */
let containerOpBusy = false;
/** 单容器动作进度：id → start/stop/restart/remove。按钮据此显示「停止中」并锁住整行，防止连点 */
const ctrBusy = reactive<Record<string, string>>({});

/** 同一个容器同时只允许一个动作：并发下发会互相抢锁，docker 直接报 conflict */
async function runCtrOp(c: any, op: "start" | "stop" | "restart" | "remove", fn: () => Promise<any>) {
  if (ctrBusy[c.id]) return;
  ctrBusy[c.id] = op;
  containerOpBusy = true;
  try {
    await fn();
    return true;
  } catch (e: any) {
    Message.error(`${CTR_VERB[op]}失败: ` + (e?.message || e));
    return false;
  } finally {
    delete ctrBusy[c.id];
    containerOpBusy = false;
  }
}

const CTR_VERB: Record<string, string> = { start: "启动", stop: "停止", restart: "重启", remove: "删除" };

async function doStart(c: any) {
  const ok = await runCtrOp(c, "start", () => GoTools.DockerSrvStartContainer(c.id));
  if (ok) Message.success(`容器 ${c.name} 已启动`);
  loadContainers();
}

async function doStop(c: any) {
  const ok = await runCtrOp(c, "stop", () => GoTools.DockerSrvStopContainer(c.id, 10));
  if (ok) Message.success(`容器 ${c.name} 已停止`);
  loadContainers();
}

async function doRestart(c: any) {
  const ok = await runCtrOp(c, "restart", () => GoTools.DockerSrvRestartContainer(c.id));
  if (ok) Message.success(`容器 ${c.name} 已重启`);
  loadContainers();
}

async function doRemove(c: any) {
  const ok = await runCtrOp(c, "remove", () => GoTools.DockerSrvRemoveContainer(c.id, true));
  if (ok) Message.success("容器已删除");
  if (logInst.value?.id === c.id) closeLogModal();
  loadContainers();
}

const renameVisible = ref(false);
/** 弹窗里显示的「当前」名字（真实提交由 doRename 完成） */
const renameOld = ref("");
let renameId = "";
function openRename(c: any) {
  renameId = c.id; renameOld.value = c.name;
  renameVisible.value = true;
}
async function doRename(name: string) {
  try {
    await GoTools.DockerSrvRenameContainer(renameId, name);
    Message.success("已重命名"); renameVisible.value = false; loadContainers();
  } catch (e: any) { Message.error("重命名失败: " + (e?.message || e)); }
}

/* ============================ 镜像：本地 / 线上 ============================ */
const pullInput = ref("");

/* ---------- 任务中心：拉取可并行，其余任务串行 ---------- */
const tasks = reactive<Record<string, ImageTask>>({});
const taskIds = ref<string[]>([]);
const taskVisible = ref(false);
const activeTaskId = ref("");

const taskList = computed<ImageTask[]>(() => taskIds.value.map((id) => tasks[id]).filter(Boolean));
const runningTasks = computed(() => taskList.value.filter((t) => t.busy));
const runningCount = computed(() => runningTasks.value.length);
const failedCount = computed(() => taskList.value.filter((t) => !t.busy && t.failed).length);
const activeTask = computed<ImageTask | undefined>(() => tasks[activeTaskId.value]);
// 导入 / 构建 / 清理会把整包数据推给 daemon，串行更稳；拉取不受影响
const busyOther = computed(() => taskList.value.some((t) => t.busy && t.op !== "pull"));
// 浮标只在「还没处理完」时出现，全部结束自动收起，不长期占屏
const fabTask = computed<ImageTask | undefined>(
  () => runningTasks.value[runningTasks.value.length - 1] || taskList.value.find((t) => !t.busy && t.failed)
);
const fabVisible = computed(() => !taskVisible.value && !!fabTask.value);

function removeTask(id: string) {
  delete tasks[id];
  taskIds.value = taskIds.value.filter((k) => k !== id);
}

function resetTask(t: ImageTask) {
  t.busy = true;
  t.failed = false;
  t.canceled = false;
  t.stalled = false;
  t.percent = 0;
  t.stage = "";
  t.stageText = "";
  t.note = "";
  t.bytes = 0;
  t.total = 0;
  t.layers = 0;
  t.doneLayers = 0;
  t.logs = [];
  t.speed = "";
  t.startedAt = Date.now();
  t.endedAt = 0;
  t.rate = { t: 0, b: 0 };
  for (const k of Object.keys(t.layerMap)) delete t.layerMap[k];
}

/** 同一个 op + 名字只留一条：重跑时沿用原记录，后端同名任务会先把旧流断掉 */
function ensureTask(op: TaskOp, name: string, cancelKey = `${op} ${name}`): ImageTask {
  const id = `${op}|${name}`;
  const exists = tasks[id];
  if (exists) {
    exists.cancelKey = cancelKey;
    resetTask(exists);
    return exists;
  }
  tasks[id] = {
    id, op, name, cancelKey, busy: true, failed: false, canceled: false, stalled: false, percent: 0,
    stage: "", stageText: "", note: "", bytes: 0, total: 0, layers: 0, doneLayers: 0,
    logs: [], speed: "", startedAt: Date.now(), endedAt: 0, layerMap: {},
  };
  taskIds.value = [...taskIds.value, id];
  // 只留最近 20 条，长时间使用后清单不会无限长
  if (taskIds.value.length > 20) {
    const drop = taskIds.value.find((k) => !tasks[k]?.busy);
    if (drop) removeTask(drop);
  }
  return tasks[id];
}

function openTasks(id?: string) {
  const target = id || (tasks[activeTaskId.value] ? "" : taskIds.value[taskIds.value.length - 1]);
  if (target) activeTaskId.value = target;
  taskVisible.value = true;
}

/** 速率按 0.8 秒采样：事件可能一秒来几十条，逐条算会把数字抖成一团 */
function trackBytes(t: ImageTask, bytes: number) {
  const now = Date.now();
  const prev = t.rate;
  if (!prev || !prev.t) {
    t.rate = { t: now, b: bytes };
    return;
  }
  const dt = now - prev.t;
  if (dt < 800) return;
  const db = bytes - prev.b;
  if (db > 0) t.speed = fmtBytes(db / (dt / 1000)) + "/s";
  t.rate = { t: now, b: bytes };
}

function pushLog(t: ImageTask, s: string) {
  if (!s) return;
  t.logs.push(`[${new Date().toLocaleTimeString()}] ${s}`);
  if (t.logs.length > 200) t.logs.splice(0, t.logs.length - 200);
}

/** 取消与失败的区分在 Go 侧已完成：这里只决定提示语气 */
function taskError(t: ImageTask, e: any) {
  const msg = String(e?.message || e);
  t.note = msg;
  t.stalled = false;
  if (msg.includes("已取消")) {
    t.canceled = true;
    Message.warning(`${t.name} 已取消`);
    return;
  }
  t.failed = true;
  Message.error(msg.length > 60 ? `${t.name} 失败：原因与下一步见「镜像任务」` : `${t.name} 失败: ${msg}`);
}

function finishTask(t: ImageTask) {
  t.busy = false;
  t.stalled = false;
  t.endedAt = Date.now();
  t.speed = "";
  t.stage = "";
  t.stageText = "";
  if (!t.failed && !t.canceled && t.percent < 100) t.percent = 100;
  // 导入/构建会凭空多一个镜像，推送/导出/清理会改本地列表，统一重拉一次
  loadImages();
}

/** 推送/导入/构建/导出/清理的公共壳：建任务 → 跑 → 收尾刷新 */
async function runSimpleTask(op: TaskOp, name: string, fn: (t: ImageTask) => Promise<void>, retry?: () => void) {
  const t = ensureTask(op, name, op === "prune" ? "prune images" : undefined);
  t.retry = retry;
  activeTaskId.value = t.id;
  taskVisible.value = true;
  t.stage = "running";
  try {
    await fn(t);
  } catch (e: any) {
    taskError(t, e);
  } finally {
    finishTask(t);
  }
}

/** 后端已受理取消：先把任务落成「已取消」，不等 canceled 事件回来
 *  （事件与绑定调用的返回先后不保证，晚到的 done 也不该再把它写成完成） */
function markCanceled(names: string) {
  if (!names) return;
  const hit = new Set(names.split("、"));
  for (const t of taskList.value) {
    if (!t.busy || !hit.has(t.cancelKey)) continue;
    t.canceled = true;
    t.stalled = false;
    t.note = t.note || "已取消";
    t.speed = "";
  }
}

async function cancelTask(id?: string) {
  const t = id ? tasks[id] : activeTask.value;
  try {
    const n = await GoTools.DockerSrvCancelImageOp(t?.cancelKey || "");
    markCanceled(n);
    Message.info(n ? `正在取消：${n}` : "任务已结束");
  } catch (e: any) {
    Message.warning("取消失败: " + (e?.message || e));
  }
}

async function cancelAllTasks() {
  try {
    const n = await GoTools.DockerSrvCancelImageOp("");
    markCanceled(n);
    Message.info(n ? `正在取消：${n}` : "当前没有进行中的任务");
  } catch (e: any) {
    Message.warning("取消失败: " + (e?.message || e));
  }
}

function clearFinishedTasks() {
  for (const id of [...taskIds.value]) {
    if (tasks[id] && !tasks[id].busy) removeTask(id);
  }
  if (!tasks[activeTaskId.value]) activeTaskId.value = taskIds.value[taskIds.value.length - 1] || "";
  if (!taskIds.value.length) taskVisible.value = false;
}

function retryTask(id: string) {
  void doRetry(id);
}

/** 正在跑的后端调用：不进 reactive（Promise 被代理后 then 会失效），重试前靠它等旧调用真正结束 */
const pendingCalls = new Map<string, Promise<any>>();

/** 重试：失败/取消的直接重跑；「疑似卡住」的还在跑，得先把旧流断掉、等它真正结束，
 *  否则旧流后到的 canceled 事件会把新任务也标成已取消 */
async function doRetry(id: string) {
  const t = tasks[id];
  if (!t?.retry) return;
  if (t.busy) {
    Message.info(`正在断掉卡住的连接并重新拉取 ${t.name}…`);
    try {
      const n = await GoTools.DockerSrvCancelImageOp(t.cancelKey);
      markCanceled(n);
    } catch { /* 已经结束就不管 */ }
    const p = pendingCalls.get(id);
    if (p) await Promise.race([p.catch(() => { }), new Promise((r) => setTimeout(r, 5000))]);
    // 兜底：旧流 5 秒内没收尾也放行，新任务会让后端把同名旧 op 顶掉
    if (t.busy) t.busy = false;
  }
  t.retry();
}

/* ---------- 拉取 ---------- */
function pullImage() {
  const raw = pullInput.value.trim();
  if (!raw) { Message.warning("请输入镜像名，如 mysql:8.4"); return; }
  // 一次能粘多个：空格 / 逗号 / 分号 / 换行都当分隔符，各自开一条任务
  const refs = raw.split(/[\s,，;；]+/).filter(Boolean);
  pullInput.value = "";
  for (const r of refs) void startPull(r);
}

function pullRemote(rec: any) {
  const name = String(rec?.name || "").trim();
  if (!name) return;
  void startPull(name);
}

/** after 用于「从线上镜像直接创建容器」：拉完接着建，不必让用户点两次 */
async function startPull(name: string, after?: () => Promise<void>) {
  const ref = name.trim();
  if (!ref) return;
  // 同一个镜像不重复开流：直接把详情切过去
  const running = runningTasks.value.find((t) => t.op === "pull" && t.name === ref);
  if (running) {
    openTasks(running.id);
    Message.info(`${ref} 正在拉取中`);
    return;
  }
  const t = ensureTask("pull", ref);
  // 每次启动自增：重试时旧调用的 finally 不能再把新任务的进度/状态改回去
  const run = (t.runId = (t.runId || 0) + 1);
  t.retry = () => void startPull(ref, after);
  activeTaskId.value = t.id;
  taskVisible.value = true;
  t.stage = "connecting";
  t.stageText = "正在连接仓库…";
  const call = GoTools.DockerSrvPullImage(ref);
  pendingCalls.set(t.id, call);
  try {
    await call;
    if (t.runId !== run) return;
    // 取消在 Go 侧已不当失败（返回 nil）：取消态来自事件或 cancelTask 的预标记，
    // 这时只提示“已取消”，不弹成功、也不再接着 after（如拉完建容器）
    if (t.canceled) {
      if (!t.note) t.note = "已取消";
      Message.warning(`${ref} 已取消`);
      return;
    }
    if (t.failed) return;
    t.note = `镜像 ${ref} 已就绪`;
    Message.success(`镜像 ${ref} 拉取完成`);
    if (after) await after();
  } catch (e: any) {
    if (t.runId === run) taskError(t, e);
  } finally {
    if (t.runId === run) {
      pendingCalls.delete(t.id);
      finishTask(t);
    }
  }
}

async function removeImage(rec: any) {
  // 悬空镜像没有可用的 repo:tag，只能按 ID 删
  const ref = rec.tag === "<none>" ? rec.id : `${rec.repo}:${rec.tag}`;
  try {
    await GoTools.DockerSrvRemoveImage(ref, false);
    Message.success("镜像已删除"); loadImages();
  }
  catch (e: any) { Message.error("删除失败: " + (e?.message || e)); }
}

/** docker:pull 事件 → 对应任务的整体进度 + 分层明细（按镜像引用归档，多个拉取各归各的） */
function onPullEvent(raw: any) {
  const p = raw?.data ?? raw;
  if (!p || !p.image) return;
  const t = tasks[`pull|${p.image}`];
  // 不是本页发起的拉取（如 RunImage 的自动拉取）或已结束的旧任务就不认，
  // 否则清单里会冒幽灵进度、旧记录还会被重新写一遍
  if (!t || !t.busy) return;
  const note = p.note || "";
  if (p.stage === "info") { if (note) { t.note = note; pushLog(t, note); } return; }
  if (p.stage === "done") {
    t.stalled = false;
    t.percent = 100;
    t.doneLayers = t.layers;
    for (const k of Object.keys(t.layerMap)) t.layerMap[k] = { ...t.layerMap[k], status: "done", percent: 100, text: "完成" };
    if (note) { t.note = note; pushLog(t, note); }
    return;
  }
  if (p.stage === "error") {
    t.failed = true; t.stalled = false;
    if (note) { t.note = note; pushLog(t, note); }
    return;
  }
  if (p.stage === "canceled") {
    t.canceled = true; t.stalled = false;
    if (note) { t.note = note; pushLog(t, note); }
    return;
  }
  // 后端报「长时间没新进度」：任务还在跑，但先给提示 + 开放重试（取消按钮也还在）；
  // 同时存进任务日志，关掉提示后还能回看
  if (p.stage === "stalled") {
    t.stalled = true;
    if (note) { t.note = note; pushLog(t, note); }
    return;
  }
  // 进度真的在涨才收起「卡住」提示：daemon 重试同一层时会反复发同样的消息
  const grew = (typeof p.bytes === "number" && p.bytes > t.bytes) ||
    (typeof p.overall === "number" && p.overall > t.percent);
  if (t.stalled && grew) { t.stalled = false; t.note = ""; }
  if (typeof p.overall === "number" && p.overall > 0) t.percent = p.overall;
  if (typeof p.total === "number" && p.total > 0) t.total = p.total;
  if (typeof p.bytes === "number" && p.bytes > 0) { t.bytes = p.bytes; trackBytes(t, p.bytes); }
  if (p.layers) t.layers = p.layers;
  if (typeof p.layersDone === "number") t.doneLayers = p.layersDone;
  if (p.status) t.stageText = p.status;
  // "Pulling from library/mysql" 带的是 tag 不是层，不能当一行塞进分层表
  if (String(p.status || "").startsWith("Pulling from")) return;
  // 没有层 ID 的状态（如 "Digest: sha256:…"）也不是一层
  if (!p.layer) return;
  const key = p.layer;
  const done = p.status === "Download complete" || p.status === "Pull complete" || p.status === "Already exists";
  t.layerMap[key] = {
    key,
    layer: key.slice(0, 12),
    // 完成态的 progressDetail 会被 daemon 清空，按 0 画会变成“已完成却 0%”
    percent: done ? 100 : p.percent || 0,
    status: done ? "done" : "running",
    text: p.status || (p.percent ? p.percent + "%" : "处理中"),
  };
}

/** docker:imageop 事件 → 推送/构建/导入/导出的整体进度与日志 */
function onImageOpEvent(raw: any) {
  const p = raw?.data ?? raw;
  if (!p || !p.op) return;
  const t = opTask(p.op, p.image);
  if (!t || !t.busy) return;
  const note = p.note || "";
  if (p.stage === "done") {
    t.percent = 100;
    if (note) { t.note = note; pushLog(t, note); }
    return;
  }
  if (p.stage === "error") { t.failed = true; if (note) t.note = note; return; }
  if (p.stage === "info") { if (note) { t.note = note; pushLog(t, note); } return; }
  if (typeof p.percent === "number" && p.percent > 0) t.percent = p.percent;
  if (typeof p.total === "number" && p.total > 0) t.total = p.total;
  if (typeof p.bytes === "number" && p.bytes > 0) { t.bytes = p.bytes; trackBytes(t, p.bytes); }
  if (p.status) t.stageText = p.status;
  if (note) pushLog(t, note);
}

/** 事件里的 Image 与任务名对不上时（清理报的是 "images"）退化为「同类型在跑的任务」 */
function opTask(op: string, image: string): ImageTask | undefined {
  const byName = tasks[`${op}|${image}`];
  if (byName) return byName;
  return taskList.value.find((t) => t.op === op && t.busy);
}

/* ---------- 本地 / 线上视角 ---------- */
/** 线上视角表格上沿的一条提示：错误、空结果、操作引导都用它 */
interface RemoteNotice {
  type: "error" | "warning" | "info";
  title: string;
  text: string;
}

const imgScope = ref<"local" | "remote">("local");

/* 镜像工具条的搜索态：默认收起只露一个放大镜，点开才出输入框并把其它操作让位，
   否则输入框 + 拉取 + 导入/构建/清理 一起抢宽度，会把按钮挤到第二行 */
const searchOpen = ref(false);
const pullInputRef = ref<any>(null);
const remoteSearchRef = ref<any>(null);

function openSearch() {
  searchOpen.value = true;
  // 展开后直接聚焦：点图标就是想打字，别再让用户点一次输入框
  nextTick(() => {
    const el = imgScope.value === "remote" ? remoteSearchRef.value : pullInputRef.value;
    el?.focus?.();
  });
}

function closeSearch() {
  searchOpen.value = false;
}
const remoteSource = ref("hub");
const remoteQuery = ref("");
const remoteRows = ref<any[]>([]);
/** 线上视角的表格上沿提示（错误 / 引导共用）：内容由页面给，组件只负责贴在表头之上 */
const remoteNotice = ref<RemoteNotice | null>(null);
const remoteLoading = ref(false);
const remotePage = ref(1);
// 私有仓库没有分页：一次取全，再按关键词在前端过滤
let remoteAll: any[] = [];

const registries = ref<any[]>([]);
/** daemon.json 的 registry-mirrors：按用户配置的顺序排到「线上来源」最前面 */
const mirrors = ref<string[]>([]);
/** 用户动过来源下拉后，就不再自动选中默认项 */
let sourceTouched = false;

/** 加速站来源用 mirror:<url> 编码，才能和「hub」、仓库 id 区分开 */
const MIRROR_PREFIX = "mirror:";
const mirrorValue = (url: string) => MIRROR_PREFIX + String(url).replace(/\/+$/, "");
const mirrorOf = (src: string) =>
  String(src || "").startsWith(MIRROR_PREFIX) ? String(src).slice(MIRROR_PREFIX.length) : "";

const remoteSources = computed<any[]>(() => {
  // 我配置的加速站排最前（配置顺序即展示顺序），其次才是 Hub 搜索与私有仓库
  const out: any[] = mirrors.value.map((m) => ({ label: `${hostOnly(m)}（加速）`, value: mirrorValue(m) }));
  out.push({ label: "Docker Hub（搜索）", value: "hub" });
  for (const r of registries.value) {
    if (/index\.docker\.io|registry-1\.docker\.io/i.test(String(r.url))) continue;
    out.push({ label: `${r.name || "仓库"}（${hostOnly(String(r.url))}）`, value: r.id });
  }
  return out;
});

function onScope(v: any) {
  imgScope.value = v as "local" | "remote";
  if (imgScope.value === "remote" && !remoteRows.value.length) searchRemote(true);
}

/** 来源是加速站：检索框的用途从“搜”变成“按名字直达拉取”，提示语跟着变 */
const mirrorMode = computed(() => !!mirrorOf(remoteSource.value));
const searchPlaceholder = computed(() =>
  mirrorMode.value ? "输入镜像名直达拉取，如 redis" : "搜索镜像，如 mysql / redis");

/** 来源下拉：组件只管回传选中的来源，状态与检索都留在页面 */
function onRemoteSource(v?: any) {
  if (v) remoteSource.value = String(v);
  sourceTouched = true;
  remoteRows.value = [];
  remoteNotice.value = null;
  remoteAll = [];
  searchRemote(true);
}

/** 搜索框回车：先收下最新关键词（输入事件可能还在路上），再从头检索 */
function onRemoteSearch(v: string) {
  remoteQuery.value = String(v ?? remoteQuery.value);
  searchRemote(true);
}

/**
 * 加速站只代理 docker.io 的拉取（registry-mirrors），没有关键词搜索接口：
 * 把输入当成镜像名，拼成「加速站主机/镜像名」给出可直接拉取的候选。
 */
function mirrorRows(q: string, mirror: string): any[] {
  const host = hostOnly(mirror).split("/")[0];
  const name = q
    .replace(/^https?:\/\//i, "")
    .replace(/^(index\.docker\.io|registry-1\.docker\.io|docker\.io)\//i, "")
    .replace(/\/+$/, "");
  const repo = name.includes("/") ? name : `library/${name}`;
  return [
    { name: `${host}/${repo}`, source: "加速站", description: `经 ${host} 拉取，落地后本地镜像名就是它` },
    { name: repo, source: "Docker Hub", description: "原始名：仅在引擎已配 registry-mirrors 或能直连 docker.io 时可用" },
  ];
}

async function searchRemote(reset = true) {
  const mirror = mirrorOf(remoteSource.value);
  if (reset) {
    remotePage.value = 1;
    remoteRows.value = [];
    remoteNotice.value = null;
  } else {
    remotePage.value += 1;
  }
  // 加速站：没有索引接口，给提示 + 直达拉取，不去打 hub.docker.com
  if (mirror) {
    const q = remoteQuery.value.trim();
    if (!q) {
      remoteNotice.value = {
        type: "info",
        title: "经加速站直达拉取",
        text: `输入镜像名后回车，如 redis / bitnami/redis。加速站没有搜索接口，这里只把名字拼成 ${hostOnly(mirror).split("/")[0]}/… 供直接拉取。`,
      };
      return;
    }
    remoteRows.value = mirrorRows(q, mirror);
    return;
  }
  if (remoteSource.value === "hub" && !remoteQuery.value.trim()) {
    remoteNotice.value = { type: "info", title: "搜索 Docker Hub", text: "输入关键词后回车，如 mysql / redis / nginx" };
    remoteRows.value = [];
    return;
  }
  remoteLoading.value = true;
  try {
    if (remoteSource.value === "hub") {
      const rows = (await GoTools.DockerSrvSearchHub(remoteQuery.value.trim(), remotePage.value)) || [];
      remoteRows.value = reset ? rows : remoteRows.value.concat(rows);
      if (reset && !rows.length) {
        remoteNotice.value = { type: "warning", title: "没有搜到匹配的镜像", text: "换个关键词试试，或把来源切到自己的加速站直达拉取。" };
      }
    } else {
      if (reset) remoteAll = (await GoTools.DockerSrvRegistryRepos(remoteSource.value)) || [];
      const q = remoteQuery.value.trim().toLowerCase();
      remoteRows.value = remoteAll.filter((r) => !q || String(r.name).toLowerCase().includes(q));
      if (reset && !remoteAll.length) {
        remoteNotice.value = { type: "warning", title: "该仓库没有可列出的镜像", text: "目录是空的，或当前账号没有 catalog 权限。" };
      }
    }
  } catch (e: any) {
    remoteNotice.value = {
      type: "error",
      title: remoteSource.value === "hub" ? "连不上 Docker Hub" : "读取仓库目录失败",
      text: String(e?.message || e),
    };
    if (reset) remoteRows.value = [];
  }
  remoteLoading.value = false;
}

function refOf(rec: any): string {
  if (!rec) return "";
  if (rec.repo) return `${rec.repo}:${rec.tag}`;
  return String(rec.name || "");
}

/* ---------- 创建容器 ---------- */
const runVisible = ref(false);
/** 弹窗打开时要填进「镜像」的引用（悬空镜像用 ID） */
const runImage = ref("");

function openRun(rec: any) {
  // 悬空镜像只能按 ID 建容器
  runImage.value = rec?.tag === "<none>" ? String(rec.id || "") : refOf(rec);
  runVisible.value = true;
}

async function createRun(f: RunForm) {
  const id = await GoTools.DockerSrvRunImage({
    image: f.image,
    name: f.name,
    containerPort: Number(f.containerPort) || 0,
    hostPort: Number(f.hostPort) || 0,
    env: f.env,
    autoStart: f.autoStart,
  });
  Message.success(`容器已创建${f.autoStart ? "并启动" : ""}${id ? "（" + String(id).slice(0, 12) + "）" : ""}`);
  tab.value = "container";
  loadContainers();
}

async function doRun(f: RunForm) {
  // 线上镜像多数还没落地：先拉取，成功后自动接着建（一个弹窗看完全程）
  const local = images.value.some((r: any) => `${r.repo}:${r.tag}` === f.image);
  if (!local) {
    await startPull(f.image, () => createRun(f));
    return;
  }
  try {
    await createRun(f);
  } catch (e: any) {
    Message.error("创建容器失败: " + (e?.message || e));
  }
}

/* ---------- 推送 ---------- */
const pushVisible = ref(false);
/** 弹窗预填：要推的本地镜像 + 按登记仓库算出的默认目标 */
const pushImage = ref("");
const pushTarget = ref("");

function openPush(rec: any) {
  const image = refOf(rec);
  pushImage.value = image;
  const [repo, tag] = image.split(":");
  const base = String(repo || "").split("/").pop() || "image";
  // 已带仓库地址的沿用；否则用第一个登记仓库的 地址/命名空间/名称:tag 预填
  if (localRegistryOf(String(repo || ""), registries.value) && String(repo).includes("/")) {
    pushTarget.value = image;
  } else {
    const r = registries.value[0];
    const host = r ? hostOnly(String(r.url)).split("/")[0] : "";
    const ns = r?.namespace ? `/${r.namespace}` : "";
    pushTarget.value = host ? `${host}${ns}/${base}:${tag || "latest"}` : image;
  }
  pushVisible.value = true;
}

async function doPush(p: { image: string; target: string }) {
  const run = () => runSimpleTask("push", p.target, async (t) => {
    await GoTools.DockerSrvPushImageTo(p.image, p.target);
    t.note = "已推送 " + p.target;
    Message.success("镜像已推送");
  }, run);
  await run();
}

/* ---------- 导入 ---------- */
const importVisible = ref(false);

function openImport() {
  importVisible.value = true;
}

async function doImport(p: { file: string; ref: string }) {
  // 任务名与后端 beginOp 的 "import <ref 或文件名>" 对齐，取消时才能精确命中
  const name = p.ref || p.file.split(/[\\/]/).pop() || p.file;
  const run = () => runSimpleTask("import", name, async (t) => {
    const got = await GoTools.DockerSrvImportImage(p.file, p.ref);
    t.note = "已导入 " + (got || p.ref || "镜像");
    Message.success("镜像已导入");
  }, run);
  await run();
}

/* ---------- 构建 ---------- */
const buildVisible = ref(false);

function openBuild() {
  buildVisible.value = true;
}

async function doBuild(p: { dir: string; dockerfile: string; tag: string }) {
  const run = () => runSimpleTask("build", p.tag, async (t) => {
    const got = await GoTools.DockerSrvBuildImage(p.dir, p.dockerfile, p.tag);
    t.note = "已构建 " + (got || p.tag);
    Message.success("镜像构建完成");
  }, run);
  await run();
}

/* ---------- 导出 / 清理 ---------- */
function defaultTarName(ref: string): string {
  return String(ref).replace(/[/\\:]/g, "_") + ".tar";
}

async function exportImage(rec: any) {
  const ref = refOf(rec);
  if (!ref) return;
  let out = "";
  try {
    out = await GoTools.DockerSrvChooseSaveFile("导出镜像 " + ref, defaultTarName(ref));
  } catch (e: any) {
    Message.warning("打开保存框失败: " + (e?.message || e));
    return;
  }
  if (!out) return; // 用户取消
  const run = () => runSimpleTask("export", ref, async (t) => {
    const p = await GoTools.DockerSrvExportImage(ref, out);
    t.note = "已导出到 " + p;
    Message.success("镜像已导出");
  }, run);
  await run();
}

async function doPrune() {
  const run = () => runSimpleTask("prune", "悬空镜像", async (t) => {
    const rep = await GoTools.DockerSrvPruneImages(false);
    const n = (rep?.deleted || []).length;
    t.note = n ? `已删除 ${n} 个悬空镜像，回收 ${fmtBytes(rep.space)}` : "没有可清理的悬空镜像";
    Message.success(t.note);
  }, run);
  await run();
}

/* ============================ 仓库与加速站 ============================ */
// 弹窗本体在 components/DockerSettingsModal.vue；页面只留一份仓库列表：
// 线上镜像的来源下拉与推送的默认目标靠它
const settingsVisible = ref(false);

function openSettings() {
  settingsVisible.value = true;
}

async function loadRegistries() {
  try {
    registries.value = (await GoTools.DockerSrvListRegistries()) || [];
  } catch (e: any) {
    Message.warning("读取仓库列表失败: " + (e?.message || e));
  }
  // 选中的仓库可能在设置弹窗里被删了：来源退回第一项，否则下拉框指着一个不存在的 id
  if (!remoteSource.value.startsWith(MIRROR_PREFIX) && remoteSource.value !== "hub"
    && !registries.value.some((r: any) => r.id === remoteSource.value)) {
    remoteSource.value = remoteSources.value[0]?.value || "hub";
    remoteRows.value = [];
    remoteAll = [];
    remoteNotice.value = null;
  }
}

/** daemon.json 里的加速站：读不到就当没配，不影响 Hub 搜索与私有仓库 */
async function loadMirrors() {
  try {
    const cfg = (await GoTools.DockerSrvDaemonConfig()) || {};
    mirrors.value = Array.isArray(cfg.mirrors) ? cfg.mirrors.filter(Boolean) : [];
  } catch {
    mirrors.value = [];
  }
  // 选中的加速站在设置里被删了：退回 Hub，不让下拉悬空
  if (mirrorOf(remoteSource.value) && !mirrors.value.some((m) => mirrorValue(m) === remoteSource.value)) {
    remoteSource.value = "hub";
    remoteRows.value = [];
    remoteAll = [];
    remoteNotice.value = null;
  }
  // 用户没动过下拉时，默认就是「我配置的第一个加速站」
  if (!sourceTouched && mirrors.value.length) remoteSource.value = mirrorValue(mirrors.value[0]);
}

/** 设置弹窗改过仓库 / 加速站：两份列表都要重取 */
function onSettingsChanged() {
  loadRegistries();
  loadMirrors();
}

/** 设置弹窗里重启过引擎：重探引擎并刷新镜像列表 */
function onEngineChanged() {
  loadEngine(true);
  loadImages();
}

/** 停止引擎（也用于叫停“启动引擎”的等待） */
async function stopEngine() {
  const wasStarting = setupBusy.value === "start";
  setupBusy.value = "stop";
  setupStage.value = "stopping";
  try {
    await GoTools.DockerSrvStopEngine();
    Message.success(wasStarting ? "已停止启动，Docker Desktop 已关闭" : "Docker 引擎已停止");
  } catch (e: any) {
    Message.error("停止失败: " + (e?.message || e));
  }
  await loadEngine(true);
  if (setupBusy.value === "stop") setupBusy.value = "";
}

/* ============================ 容器日志 ============================ */
const logVisible = ref(false);
const logInst = ref<any>(null);
const logLines = ref<string[]>([]);
const logFollow = ref(true);
const logRunning = ref(false);

const logTitle = computed(() => logInst.value ? `容器日志 · ${logInst.value.name}` : "容器日志");

/* ============================ 容器实时监控 ============================ */
// 弹窗自己按秒采样，页面只管把容器身份传过去
const monVisible = ref(false);
const monInst = ref<any>(null);

function openMonitor(c: any) {
  monInst.value = c;
  monVisible.value = true;
}

/* ============================ 容器终端 ============================ */
// 弹窗内部建会话（docker exec），页面只负责把容器身份传过去
const termVisible = ref(false);
const termInst = ref<any>(null);

function openTerm(c: any) {
  termInst.value = c;
  termVisible.value = true;
}

async function openLogModal(c: any) {
  logInst.value = c;
  logLines.value = [];
  logVisible.value = true;
  await loadLogs();
  if (logFollow.value) startFollow();
}

async function loadLogs() {
  const c = logInst.value;
  if (!c) return;
  try {
    if (logRunning.value) { logLines.value = (await GoTools.DockerSrvLogsBuffered(c.id, 500)) || []; }
    else { logLines.value = (await GoTools.DockerSrvContainerLogs(c.id, 500)) || []; }
  } catch (e: any) { Message.error("读取日志失败: " + (e?.message || e)); }
}

async function startFollow() {
  const c = logInst.value;
  if (!c || logRunning.value) return;
  try {
    await GoTools.DockerSrvLogsFollow(c.id);
    logRunning.value = true;
  } catch (e: any) {
    logFollow.value = false;
    Message.warning("开启实时跟随失败，改用轮询：" + (e?.message || e));
  }
}

function stopFollow() {
  const c = logInst.value;
  if (c && logRunning.value) { try { GoTools.DockerSrvLogsStop(c.id); } catch (e) { /* ignore */ } }
  logRunning.value = false;
}

async function toggleFollow() {
  if (logFollow.value) { await loadLogs(); if (logFollow.value) startFollow(); }
  else stopFollow();
}

function closeLogModal() {
  stopFollow();
  logVisible.value = false;
  logInst.value = null;
  logLines.value = [];
  logPending = [];
  if (logFlushTimer) { clearTimeout(logFlushTimer); logFlushTimer = null; }
}

/** docker:serverlog 事件 → 先入缓冲，最多 100ms 合并一次渲染。
 * 容器刷屏时逐行 push 会触发上千次 Vue 更新，白白吃掉一整颗核 */
let logPending: string[] = [];
let logFlushTimer: any = null;

function flushLogLines() {
  logFlushTimer = null;
  if (!logPending.length) return;
  const batch = logPending;
  logPending = [];
  if (!logVisible.value) return; // 弹窗已关：丢掉这批缓冲
  const next = logLines.value.concat(batch);
  logLines.value = next.length > 1000 ? next.slice(next.length - 1000) : next;
}

function onLogEvent(raw: any) {
  const p = raw?.data ?? raw;
  if (!p || !p.line) return;
  if (!logVisible.value || logInst.value?.id !== p.id) return;
  logPending.push(`[${p.time}] ${p.line}`);
  if (logPending.length > 2000) logPending.splice(0, logPending.length - 2000); // 兜底：缓冲不无限涨
  if (!logFlushTimer) logFlushTimer = setTimeout(flushLogLines, 100);
}

/* ============================ 生命周期 ============================ */
onMounted(() => {
  loadEngine();
  loadRegistries();
  loadMirrors();
  offPull = Events.On("docker:pull", onPullEvent);
  offImageOp = Events.On("docker:imageop", onImageOpEvent);
  offLog = Events.On("docker:serverlog", onLogEvent);
  offSetup = Events.On("docker:setup", onSetupEvent);
  // 列表定时刷新：安装/切后端/拉取/看日志期间全部跳过（那时 daemon 会短暂下线，
  // 每 5 秒一次的失败调用只会往终端刷 ERR）。
  // 但“引擎已离线”这件事必须一直探：不跟着日志弹窗、任务状态走，
  // 否则从托盘关掉 Desktop 后，面板会因为某个无关开关而再也不探
  pollTimer = setInterval(() => {
    if (setupBusy.value) return;
    if (!engine.value.available) {
      probeEngine();
      return;
    }
    if (logVisible.value || monVisible.value || runningCount.value || containerOpBusy || resBusy.value) return;
    refreshCurrent(true);
  }, 5000);
});

onUnmounted(() => {
  stopFollow();
  if (logFlushTimer) { clearTimeout(logFlushTimer); logFlushTimer = null; }
  if (pollTimer) clearInterval(pollTimer);
  if (offPull) offPull();
  if (offImageOp) offImageOp();
  if (offLog) offLog();
  if (offSetup) offSetup();
});
</script>

<style scoped>
.docker-page {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

/* 引擎卡片 / 容器表 / 镜像表各自的样式都跟着组件走了，这里只留外层容器 */
.main-card {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
}

.main-card :deep(.arco-card-body) {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* 标签行与下面工具条之间给一条发丝线，两个层级才分得开 */
.main-card :deep(.arco-card-header) {
  border-bottom: 1px solid var(--color-border-1);
  align-items: center;
}

/* 头部右侧就是工具条（原来贴在表格上方那条搬到了这里）：
   左侧视角不占多余宽度，右侧拿剩下的位置，挤不下一行时自己折行 */
.main-card :deep(.arco-card-header-title) {
  flex: none;
  margin-right: 12px;
}

.main-card :deep(.arco-card-header-extra) {
  flex: 1 1 auto;
  min-width: 0;
  overflow: visible;
}

.card-extra {
  justify-content: flex-end;
}

/* 视角按钮不许被挤成竖排字（“本/地”） */
.card-extra :deep(.arco-radio-group) {
  flex: none;
  white-space: nowrap;
}

/* 来源下拉：长域名也不抖；搜索框吃剩余宽度但不无限拉长 */
.src-select {
  flex: 0 0 200px;
  width: 200px;
}

.search-box {
  flex: 1 1 200px;
  min-width: 150px;
  max-width: 320px;
}

.pull-input {
  flex: 0 0 200px;
  width: 200px;
}

/* 搜索展开态：其它操作已收起，输入框给足宽度；父级是 a-space 的 block item，
   这里靠固定宽 + max-width 兜底，flex 不会被解析 */
.search-box.open,
.pull-input.open {
  flex: none;
  width: 360px;
  max-width: 46vw;
  min-width: 180px;
}

/* 后台刷新失败的静默提示：不参与 flex 拉伸，免得顶得表格高度跳动 */
.list-err {
  flex: none;
  margin-bottom: 8px;
}

/* 五六个 tab 在窄窗口要能换行，不能挤成一团 */
.tab-group {
  display: flex;
  flex-wrap: wrap;
  gap: 4px;
}

/* 表格所在的视口：子组件自己负责把表格撑满这一层 */
.pane {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
}

.pane :deep(.arco-table-container) {
  flex: 1;
}

/* 总览是长内容（概览卡 + 容器卡片墙）：整块自己滚，不把页面撑高 */
.pane-scroll {
  display: block;
  overflow-y: auto;
}

.op-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 10px;
  padding: 28px 0;
  border-radius: var(--base-radius-large);
}

.op-empty-icon {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  flex: none;
  border-radius: 50%;
  font-size: 16px;
  color: var(--color-text-3);
  background: var(--color-fill-2);
}

.op-empty-title {
  font-size: var(--base-size-default);
  color: var(--color-text-2);
}

.op-empty-sub {
  margin-top: 2px;
  font-size: var(--base-size-small);
  color: var(--color-text-3);
}

/* 工具条里的口径说明：右对齐小字，不抢按钮的位置 */
.res-note {
  font-size: var(--base-size-small);
  color: var(--color-text-3);
  white-space: nowrap;
}

/* 任务浮标：固定在窗口右下角，压在内容之上，不挡表格操作列 */
.task-fab {
  position: fixed;
  right: 20px;
  bottom: 20px;
  z-index: 100;
  display: flex;
  align-items: center;
  gap: 8px;
  width: 276px;
  padding: 9px 12px 10px;
  border: 1px solid var(--color-border-2);
  border-radius: var(--border-radius-medium);
  background: var(--color-bg-2);
  box-shadow: 0 4px 14px rgb(0 0 0 / 12%);
  cursor: pointer;
  transition: transform 0.15s, box-shadow 0.15s;
}

.task-fab:hover {
  transform: translateY(-2px);
  box-shadow: 0 8px 22px rgb(0 0 0 / 18%);
}

.tf-main {
  flex: 1;
  min-width: 0;
}

.tf-head {
  display: flex;
  align-items: center;
  gap: 6px;
  min-width: 0;
}

.tf-title {
  flex: 1;
  min-width: 0;
  font-size: var(--base-size-small);
  color: var(--color-text-1);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.tf-pct {
  flex: none;
  font-size: var(--base-size-small);
  color: var(--color-text-2);
  font-variant-numeric: tabular-nums;
}

/* 0% 时也留一条底轨：不再只剩一个孤零零的圆点 */
.tf-bar {
  position: relative;
  height: 4px;
  margin: 6px 0 4px;
  border-radius: 2px;
  background: var(--color-fill-3);
  overflow: hidden;
}

.tf-bar>i {
  position: absolute;
  top: 0;
  bottom: 0;
  left: 0;
  border-radius: inherit;
  background: rgb(var(--arcoblue-6));
  transition: width 0.25s ease;
}

.tf-bar>i.bad {
  background: rgb(var(--red-6));
}

.tf-sub {
  font-size: 12px;
  color: var(--color-text-3);
  font-variant-numeric: tabular-nums;
}

.tf-bad {
  color: rgb(var(--red-6));
  font-size: 14px;
}

.tf-arrow {
  flex: none;
  color: var(--color-text-3);
}

/* 单值等宽：浮标里的镜像名会变长变短，别的字符宽度不跟着跳 */
.mono {
  font-family: Consolas, monospace;
}
</style>
